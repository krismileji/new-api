package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newResponsesWSAdmissionTest(t *testing.T, calls *atomic.Int32) *responsesWSBillingTest {
	t.Helper()
	fixture := newResponsesWSBillingTest(t, `tier("base", p * 2)`, func(ws *websocket.Conn, _ *http.Request) {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
			id := calls.Add(1)
			body := fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_%d","status":"completed","usage":{"input_tokens":20,"output_tokens":5}}}`, id)
			if err := ws.WriteMessage(websocket.TextMessage, []byte(body)); err != nil {
				return
			}
		}
	})
	useChannelMonitorOptionMap(t, map[string]string{channelMonitorChannelConcurrencyWaitSecondsOption: "0"})
	service.ResetChannelDailyCostSnapshotCache()
	t.Cleanup(service.ResetChannelDailyCostSnapshotCache)
	t.Cleanup(func() {
		fixture.closeAndWait(t)
		require.NoError(t, service.FlushChannelDailyCostEvents())
	})
	return fixture
}

func TestResponsesWSDownstreamConcurrencyAdmission(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, reuse := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared=%t/reuse=%t", shared, reuse), func(t *testing.T) {
				var calls atomic.Int32
				fixture := newResponsesWSAdmissionTest(t, &calls)
				heldChannel := fixture.channel.Id
				if shared {
					require.NoError(t, model.DB.AutoMigrate(&model.ChannelLimitGroup{}, &model.ChannelLimitGroupTier{}, &model.ChannelLimitGroupMember{}, &model.ChannelLimitGroupRevision{}))
					peer := &model.Channel{Name: "shared-limit-peer", Key: "test", Status: common.ChannelStatusEnabled}
					require.NoError(t, model.DB.Create(peer).Error)
					t.Cleanup(func() { require.NoError(t, model.DB.Delete(peer).Error) })
					group := &model.ChannelLimitGroup{Name: "responses-shared", Enabled: true, ConcurrencyLimit: 1,
						Tiers:   []model.ChannelLimitGroupTier{{Priority: 0}},
						Members: []model.ChannelLimitGroupMember{{ChannelID: fixture.channel.Id}, {ChannelID: peer.Id}}}
					require.NoError(t, service.SaveChannelLimitGroup(t.Context(), group, false))
					t.Cleanup(func() { require.NoError(t, service.SaveChannelLimitGroup(context.Background(), group, true)) })
					heldChannel = peer.Id
				} else {
					_, err := service.SaveChannelConcurrencyLimit(t.Context(), fixture.channel.Id, 1)
					require.NoError(t, err)
				}
				payload := []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)
				if reuse {
					require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, payload))
					require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
				}
				held, acquired, _, err := service.AcquireChannelConcurrency(t.Context(), heldChannel)
				require.NoError(t, err)
				require.True(t, acquired)
				t.Cleanup(held.Release)
				beforeCalls := calls.Load()
				require.NoError(t, model.DB.First(fixture.user, fixture.user.Id).Error)
				beforeQuota := fixture.user.Quota
				before, err := service.GetChannelConcurrencySnapshotWithRPM(t.Context())
				require.NoError(t, err)
				require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, payload))
				result := readResponsesWSTestEvent(t, fixture.client)
				assert.Equal(t, "error", result["type"])
				assert.Equal(t, float64(http.StatusTooManyRequests), result["status"])
				assert.Equal(t, beforeCalls, calls.Load())
				require.NoError(t, model.DB.First(fixture.user, fixture.user.Id).Error)
				assert.Equal(t, beforeQuota, fixture.user.Quota)
				after, err := service.GetChannelConcurrencySnapshotWithRPM(t.Context())
				require.NoError(t, err)
				assert.Equal(t, before[fixture.channel.Id].CurrentRPM, after[fixture.channel.Id].CurrentRPM)
				held.Release()
				require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, payload))
				require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
				assert.Equal(t, beforeCalls+1, calls.Load())
				after, err = service.GetChannelConcurrencySnapshotWithRPM(t.Context())
				require.NoError(t, err)
				assert.Zero(t, after[fixture.channel.Id].Active, "terminal delivery follows lease release")
				assert.Equal(t, before[fixture.channel.Id].CurrentRPM+1, after[fixture.channel.Id].CurrentRPM)
			})
		}
	}
}

func addResponsesWSAdmissionChannel(t *testing.T, source *model.Channel, name string, priority int64, enabled bool) *model.Channel {
	t.Helper()
	channel := *source
	channel.Id, channel.Name, channel.Key = 0, name, name+"-key"
	channel.Priority = common.GetPointer(priority)
	channel.ChannelInfo = model.ChannelInfo{}
	settings := channel.GetSetting()
	settings.ResponsesWebSocketEnabled = enabled
	channel.SetSetting(settings)
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{ChannelId: channel.Id, Model: "ws-billing", Group: "default", Enabled: true, Priority: channel.Priority}).Error)
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Delete(&channel).Error)
	})
	return &channel
}

func TestResponsesWSDownstreamFallbackKeepsFiltersAndConnectionLock(t *testing.T) {
	var calls atomic.Int32
	fixture := newResponsesWSAdmissionTest(t, &calls)
	busy := addResponsesWSAdmissionChannel(t, fixture.channel, "busy", 200, true)
	disabled := addResponsesWSAdmissionChannel(t, fixture.channel, "ws-disabled", 100, false)
	_, err := service.SaveChannelConcurrencyLimit(t.Context(), busy.Id, 1)
	require.NoError(t, err)
	held, acquired, _, err := service.AcquireChannelConcurrency(t.Context(), busy.Id)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(held.Release)
	payload := []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, payload))
	require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("token_id = ? AND type = ?", fixture.token.Id, model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, fixture.channel.Id, logs[0].ChannelId)
	snapshot, err := service.GetChannelConcurrencySnapshotWithRPM(t.Context())
	require.NoError(t, err)
	assert.Zero(t, snapshot[disabled.Id].CurrentRPM, "fallback must retain the WebSocket opt-in filter")
	held.Release()
	_, err = service.SaveChannelConcurrencyLimit(t.Context(), fixture.channel.Id, 1)
	require.NoError(t, err)
	locked, acquired, _, err := service.AcquireChannelConcurrency(t.Context(), fixture.channel.Id)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(locked.Release)
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, payload))
	rejection := readResponsesWSTestEvent(t, fixture.client)
	assert.Equal(t, float64(http.StatusTooManyRequests), rejection["status"])
	assert.Equal(t, int32(1), fixture.connections.Load(), "a reused connection must never reroute to a free alternative")
	assert.Equal(t, int32(1), calls.Load())
}

func TestResponsesWSDownstreamDialRetryReleasesAdmission(t *testing.T) {
	var calls, handshakes atomic.Int32
	fixture := newResponsesWSAdmissionTest(t, &calls)
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handshakes.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(failed.Close)
	broken := addResponsesWSAdmissionChannel(t, fixture.channel, "dial-failure", 200, true)
	require.NoError(t, model.DB.Model(broken).Update("base_url", failed.URL).Error)
	previousRetry := common.RetryTimes
	common.RetryTimes = 1
	t.Cleanup(func() { common.RetryTimes = previousRetry })
	_, err := service.SaveChannelConcurrencyLimit(t.Context(), broken.Id, 1)
	require.NoError(t, err)
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)))
	require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
	assert.Equal(t, int32(1), handshakes.Load())
	assert.Equal(t, int32(1), calls.Load())
	snapshot, err := service.GetChannelConcurrencySnapshotWithRPM(t.Context())
	require.NoError(t, err)
	assert.Zero(t, snapshot[broken.Id].Active)
	assert.Zero(t, snapshot[fixture.channel.Id].Active)
	assert.Equal(t, 1, snapshot[broken.Id].CurrentRPM)
	assert.Equal(t, 1, snapshot[fixture.channel.Id].CurrentRPM)
	fixture.closeAndWait(t)
	assertResponsesWSAccounting(t, fixture, []int{20})
	require.NoError(t, service.FlushChannelDailyCostEvents())
	var failedCosts int64
	require.NoError(t, model.DB.Model(&model.ChannelDailyCost{}).Where("channel_id = ?", broken.Id).Count(&failedCosts).Error)
	assert.Zero(t, failedCosts, "a rejected handshake never sent a generation")
}

func TestResponsesWSDownstreamSetupFailureReleasesWithoutCost(t *testing.T) {
	var calls atomic.Int32
	fixture := newResponsesWSAdmissionTest(t, &calls)
	require.NoError(t, model.DB.Model(fixture.channel).Update("model_mapping", "invalid-json").Error)
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)))
	require.Equal(t, "error", readResponsesWSTestEvent(t, fixture.client)["type"])
	assert.Zero(t, calls.Load())
	assert.Zero(t, fixture.connections.Load())
	snapshot, err := service.GetChannelConcurrencySnapshotWithRPM(t.Context())
	require.NoError(t, err)
	assert.Zero(t, snapshot[fixture.channel.Id].Active)
	assert.Equal(t, 1, snapshot[fixture.channel.Id].CurrentRPM)
	fixture.closeAndWait(t)
	require.NoError(t, service.FlushChannelDailyCostEvents())
	var costs int64
	require.NoError(t, model.DB.Model(&model.ChannelDailyCost{}).Where("channel_id = ?", fixture.channel.Id).Count(&costs).Error)
	assert.Zero(t, costs)
	assertResponsesWSAccounting(t, fixture, nil)
}

func TestResponsesWSDownstreamRejectedCreateHasOneUnresolvedCost(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("base", p * 2)`, func(ws *websocket.Conn, _ *http.Request) {
		if _, _, err := ws.ReadMessage(); err != nil {
			return
		}
		if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"rejected"}}`)); err != nil {
			return
		}
		_, _, _ = ws.ReadMessage()
	})
	t.Cleanup(func() {
		fixture.closeAndWait(t)
		require.NoError(t, service.FlushChannelDailyCostEvents())
	})
	service.ResetChannelDailyCostSnapshotCache()
	t.Cleanup(service.ResetChannelDailyCostSnapshotCache)
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)))
	result := readResponsesWSTestEvent(t, fixture.client)
	assert.Equal(t, "error", result["type"])
	assert.Equal(t, float64(http.StatusBadRequest), result["status"])
	fixture.closeAndWait(t)
	require.NoError(t, service.FlushChannelDailyCostEvents())
	var cost model.ChannelDailyCost
	require.NoError(t, model.DB.Where("channel_id = ?", fixture.channel.Id).First(&cost).Error)
	assert.Zero(t, cost.SettledCount)
	assert.Zero(t, cost.CostNanoCNY)
	assert.Equal(t, int64(1), cost.UnresolvedCount)
	snapshot, err := service.GetChannelConcurrencySnapshotWithRPM(t.Context())
	require.NoError(t, err)
	assert.Zero(t, snapshot[fixture.channel.Id].Active)
	assert.Equal(t, 1, snapshot[fixture.channel.Id].CurrentRPM)
	assertResponsesWSAccounting(t, fixture, nil)
}

func TestResponsesWSDownstreamAdmissionReleasedOnBillingFailure(t *testing.T) {
	var calls atomic.Int32
	fixture := newResponsesWSAdmissionTest(t, &calls)
	_, err := service.SaveChannelConcurrencyLimit(t.Context(), fixture.channel.Id, 1)
	require.NoError(t, err)
	require.NoError(t, model.DecreaseUserQuota(fixture.user.Id, 100000, true))
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)))
	require.Equal(t, "error", readResponsesWSTestEvent(t, fixture.client)["type"])
	assert.Zero(t, calls.Load())
	assert.Zero(t, fixture.connections.Load())
	snapshot, err := service.GetChannelConcurrencySnapshotWithRPM(t.Context())
	require.NoError(t, err)
	assert.Zero(t, snapshot[fixture.channel.Id].Active)
	assert.Equal(t, 1, snapshot[fixture.channel.Id].CurrentRPM, "admitted requests keep their RPM even when billing fails")
}

func TestResponsesWSDownstreamDisconnectReleasesAdmission(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
		if _, _, err := ws.ReadMessage(); err != nil {
			return
		}
		if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"disconnect","status":"in_progress"}}`)); err != nil {
			return
		}
		_, _, _ = ws.ReadMessage()
	})
	t.Cleanup(func() {
		fixture.closeAndWait(t)
		require.NoError(t, service.FlushChannelDailyCostEvents())
	})
	_, err := service.SaveChannelConcurrencyLimit(t.Context(), fixture.channel.Id, 1)
	require.NoError(t, err)
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)))
	require.Equal(t, "response.created", readResponsesWSTestEvent(t, fixture.client)["type"])
	snapshot, err := service.GetChannelConcurrencySnapshotWithRPM(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, snapshot[fixture.channel.Id].Active)
	fixture.closeAndWait(t)
	snapshot, err = service.GetChannelConcurrencySnapshotWithRPM(t.Context())
	require.NoError(t, err)
	assert.Zero(t, snapshot[fixture.channel.Id].Active)
	assert.Equal(t, 1, snapshot[fixture.channel.Id].CurrentRPM)
	assertResponsesWSAccounting(t, fixture, []int{1000})
}

func TestResponsesWSDownstreamTokenRevocationStopsGeneration(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
		if _, _, err := ws.ReadMessage(); err != nil {
			return
		}
		if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"revoked","status":"in_progress"}}`)); err != nil {
			return
		}
		_, _, _ = ws.ReadMessage()
	})
	t.Setenv("TOKEN_AUTO_DISABLE_JOURNAL_DIR", t.TempDir())
	require.NoError(t, model.DB.AutoMigrate(&model.TokenAutoDisableConfig{}, &model.TokenAutoDisableRecord{}))
	workerCtx, stopWorker := context.WithCancel(context.Background())
	require.NoError(t, service.InitTokenAutoDisable(workerCtx))
	t.Cleanup(func() {
		fixture.closeAndWait(t)
		require.NoError(t, service.FlushChannelDailyCostEvents())
		stopWorker()
		require.NoError(t, model.DB.Where("token_id = ?", fixture.token.Id).Delete(&model.TokenAutoDisableRecord{}).Error)
		require.NoError(t, model.DB.Where("id = ?", 1).Delete(&model.TokenAutoDisableConfig{}).Error)
		resetCtx, stopReset := context.WithCancel(context.Background())
		require.NoError(t, service.InitTokenAutoDisable(resetCtx))
		stopReset()
	})
	_, err := service.SaveTokenAutoDisableSettings(t.Context(), service.TokenAutoDisableSettings{Enabled: true,
		Rules: []service.TokenAutoDisableRule{{Id: "policy", Name: "测试规则", Enabled: true, StatusCodes: []int{403}, Keywords: []string{"policy"}, ResponseStatus: 451, ResponseMessage: "此 Key 已禁用"}}})
	require.NoError(t, err)
	_, err = service.SaveChannelConcurrencyLimit(t.Context(), fixture.channel.Id, 1)
	require.NoError(t, err)
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","stream_id":"protected","model":"ws-billing","input":"hello"}`)))
	require.Equal(t, "response.created", readResponsesWSTestEvent(t, fixture.client)["type"])
	triggerCtx, unregister, denied := service.RegisterTokenProtection(t.Context(), fixture.token, "another-request")
	require.Nil(t, denied)
	defer unregister()
	service.ObserveTokenAutoDisableError(triggerCtx, fixture.channel.Id, 403, "policy")
	result := readResponsesWSTestEvent(t, fixture.client)
	assert.Equal(t, "error", result["type"])
	assert.Equal(t, "protected", result["stream_id"])
	assert.Equal(t, float64(451), result["status"])
	require.NoError(t, fixture.client.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, _, err = fixture.client.ReadMessage()
	require.Error(t, err, "revocation must end the upstream generation and the socket")
	fixture.closeAndWait(t)
	snapshot, err := service.GetChannelConcurrencySnapshotWithRPM(t.Context())
	require.NoError(t, err)
	assert.Zero(t, snapshot[fixture.channel.Id].Active)
	assert.Equal(t, 1, snapshot[fixture.channel.Id].CurrentRPM)
	assertResponsesWSAccounting(t, fixture, []int{1000})
}

func TestResponsesWSDownstreamCostAndBalanceFollowEachCreate(t *testing.T) {
	finish := make(chan struct{})
	fixture := newResponsesWSBillingTest(t, `tier("base", p * 2)`, func(ws *websocket.Conn, _ *http.Request) {
		for index := 1; ; index++ {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
			created := fmt.Sprintf(`{"type":"response.created","response":{"id":"cost_%d","status":"in_progress"}}`, index)
			if err := ws.WriteMessage(websocket.TextMessage, []byte(created)); err != nil {
				return
			}
			select {
			case <-finish:
			case <-t.Context().Done():
				return
			}
			completed := fmt.Sprintf(`{"type":"response.completed","response":{"id":"cost_%d","status":"completed","usage":{"input_tokens":20,"output_tokens":5}}}`, index)
			if err := ws.WriteMessage(websocket.TextMessage, []byte(completed)); err != nil {
				return
			}
		}
	})
	versionQuery := "SELECT version()"
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		versionQuery = "SELECT sqlite_version()"
	}
	var mainVersion, logVersion string
	require.NoError(t, model.DB.Raw(versionQuery).Scan(&mainVersion).Error)
	require.NoError(t, model.LOG_DB.Raw(versionQuery).Scan(&logVersion).Error)
	t.Logf("database versions: main=%s log=%s separate_connections=%t", mainVersion, logVersion, model.DB != model.LOG_DB)
	previousWrite, previousRead, previousConsumer := common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer
	common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer = common.RDB, common.RDB, common.RDB
	service.ResetChannelDailyCostSnapshotCache()
	t.Cleanup(func() {
		common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer = previousWrite, previousRead, previousConsumer
		service.ResetChannelDailyCostSnapshotCache()
	})
	monitor := model.ChannelRatioMonitor{ChannelId: fixture.channel.Id, Ratio: 4, UpdatedTime: common.GetTimestamp(), UpstreamType: "new-api", UpstreamRevision: 1}
	require.NoError(t, model.DB.Create(&monitor).Error)
	t.Cleanup(func() { require.NoError(t, model.DB.Delete(&monitor).Error) })
	sync, err := service.BeginChannelBalanceSync(t.Context(), monitor)
	require.NoError(t, err)
	_, err = service.CommitChannelBalanceSync(t.Context(), sync, 24, true)
	require.NoError(t, err)
	runtime, err := service.StartChannelDailyCostOutboxRuntime()
	require.NoError(t, err)
	t.Cleanup(func() {
		fixture.closeAndWait(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, runtime.Stop(ctx))
	})
	config := service.ChannelBalanceConfigForMonitor(monitor)
	for index, totalCost := range []int64{160_000, 480_000} {
		require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)))
		require.Equal(t, "response.created", readResponsesWSTestEvent(t, fixture.client)["type"])
		estimate, err := service.GetChannelBalanceEstimate(t.Context(), config)
		require.NoError(t, err)
		assert.Equal(t, int64(1), estimate.InFlightCount)
		assert.Equal(t, int64(1), estimate.UnknownCount, "a WebSocket generation cannot use an HTTP output budget")
		assert.Zero(t, estimate.BudgetCount)
		if index == 0 {
			require.NoError(t, model.DB.Model(&monitor).Update("ratio", 8).Error)
			service.InvalidateChannelDailyCostSnapshot(fixture.channel.Id)
		}
		finish <- struct{}{}
		require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
		estimate, err = service.GetChannelBalanceEstimate(t.Context(), config)
		require.NoError(t, err)
		assert.Zero(t, estimate.InFlightCount)
		assert.Zero(t, estimate.AverageCount)
		assert.InDelta(t, float64(totalCost)/1_000_000_000, estimate.CompletedConsumption, 0.00000001)
		var cost model.ChannelDailyCost
		require.Eventually(t, func() bool {
			if service.FlushChannelDailyCostOutbox(t.Context()) != nil {
				return false
			}
			return model.DB.Where("channel_id = ?", fixture.channel.Id).First(&cost).Error == nil && cost.SettledCount == int64(index+1)
		}, 3*time.Second, 10*time.Millisecond, "the reliable cost event must reach the real database ledger")
		assert.Equal(t, totalCost, cost.CostNanoCNY)
		assert.Zero(t, cost.UnresolvedCount)
	}
	fixture.closeAndWait(t)
	assertResponsesWSAccounting(t, fixture, []int{20, 20})
}

func TestResponsesWSDownstreamRPMCountsEachCreate(t *testing.T) {
	var calls atomic.Int32
	fixture := newResponsesWSAdmissionTest(t, &calls)
	_, err := service.SaveChannelConcurrencyLimits(t.Context(), fixture.channel.Id, common.GetPointer(1), common.GetPointer(2))
	require.NoError(t, err)
	payload := []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)
	for index := 0; index < 2; index++ {
		require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, payload))
		require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
	}
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, payload))
	result := readResponsesWSTestEvent(t, fixture.client)
	assert.Equal(t, "error", result["type"])
	assert.Equal(t, float64(http.StatusTooManyRequests), result["status"])
	assert.Equal(t, int32(2), calls.Load())
	assert.Equal(t, int32(1), fixture.connections.Load())
	snapshot, err := service.GetChannelConcurrencySnapshotWithRPM(t.Context())
	require.NoError(t, err)
	assert.Zero(t, snapshot[fixture.channel.Id].Active)
	assert.Equal(t, 2, snapshot[fixture.channel.Id].CurrentRPM)
	fixture.closeAndWait(t)
	assertResponsesWSAccounting(t, fixture, []int{20, 20})
}

func TestResponsesWSDownstreamLocalResponse(t *testing.T) {
	for _, reuse := range []bool{false, true} {
		for _, maxOutput := range []int{1, 100} {
			t.Run(fmt.Sprintf("reuse=%t/max=%d", reuse, maxOutput), func(t *testing.T) {
				var calls atomic.Int32
				fixture := newResponsesWSAdmissionTest(t, &calls)
				if reuse {
					require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)))
					require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
				}
				_, _, err := model.SaveChannelProbePolicy(t.Context(), fixture.channel.Id, model.ChannelProbePolicy{
					AutoProbeDisabled: true, SmallInputResponseEnabled: true, SmallInputThresholdTokens: 1000,
					SmallInputResponseText: "这是本地返回的固定响应。",
				})
				require.NoError(t, err)
				beforeCalls, beforeConnections := calls.Load(), fixture.connections.Load()
				before, err := service.GetChannelConcurrencySnapshotWithRPM(t.Context())
				require.NoError(t, err)
				require.NoError(t, model.DB.First(fixture.user, fixture.user.Id).Error)
				beforeQuota := fixture.user.Quota
				body := fmt.Sprintf(`{"type":"response.create","stream_id":"local-stream","model":"ws-billing","input":"hi","max_output_tokens":%d}`, maxOutput)
				require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(body)))
				var events []string
				for {
					event := readResponsesWSTestEvent(t, fixture.client)
					require.Equal(t, "local-stream", event["stream_id"])
					kind, ok := event["type"].(string)
					require.True(t, ok)
					events = append(events, kind)
					if kind == "response.completed" || kind == "response.incomplete" {
						want := "response.completed"
						if maxOutput == 1 {
							want = "response.incomplete"
						}
						assert.Equal(t, want, kind)
						break
					}
					require.NotEqual(t, "error", kind)
				}
				assert.Contains(t, events, "response.output_text.delta")
				assert.Equal(t, beforeCalls, calls.Load())
				assert.Equal(t, beforeConnections, fixture.connections.Load())
				after, err := service.GetChannelConcurrencySnapshotWithRPM(t.Context())
				require.NoError(t, err)
				assert.Zero(t, after[fixture.channel.Id].Active)
				assert.Equal(t, before[fixture.channel.Id].CurrentRPM, after[fixture.channel.Id].CurrentRPM)
				require.NoError(t, model.DB.First(fixture.user, fixture.user.Id).Error)
				assert.Equal(t, beforeQuota, fixture.user.Quota)
			})
		}
	}
}
