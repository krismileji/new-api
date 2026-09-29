package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wsmanager"
	"github.com/QuantumNous/new-api/service"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupResponsesMonitorDisable(t *testing.T, channel *model.Channel, trigger string) func() {
	t.Helper()
	monitor := model.ChannelRatioMonitor{ChannelId: channel.Id, UpstreamRevision: 1, BalanceAutoDisableThreshold: common.GetPointer(10.0)}
	if trigger == "update-failure" {
		failure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
		t.Cleanup(failure.Close)
		monitor.UpstreamType, monitor.UpstreamBaseURL, monitor.UpstreamAuthType, monitor.UpstreamGroup = service.NewAPIUpstreamType, failure.URL, service.NewAPIUpstreamAuthPublic, "default"
		monitor.UpstreamBalanceSyncDisabled = true
		disableChannelMonitorSSRFProtection(t)
		useChannelMonitorOptionMap(t, map[string]string{channelMonitorAutoDisableOnUpdateFailureOption: "true", channelMonitorAutoUpdateRetryCountOption: "0", channelMonitorEmailNotificationOption: "false"})
	}
	require.NoError(t, model.DB.Create(&monitor).Error)
	t.Cleanup(func() { require.NoError(t, model.DB.Delete(&monitor).Error) })
	return func() {
		switch trigger {
		case "low-balance":
			changed, err := autoDisableChannelMonitorAtEffectiveBalance(monitor, channel, 1, 1, 0)
			require.NoError(t, err)
			require.True(t, changed)
		case "cost-policy":
			_, _, disabled, _, err := applyChannelMonitorPolicyPlan(t.Context(), channelMonitorPolicyPlan{
				DisableChannelIds: []int{channel.Id}, DisableChannelRevisions: map[int]int64{channel.Id: 1},
				DisableChannelStatuses: map[int]model.ChannelMonitorStatusSnapshot{channel.Id: model.CaptureChannelMonitorStatus(channel)},
			})
			require.NoError(t, err)
			require.Equal(t, []int{channel.Id}, disabled)
		case "update-failure":
			summary, err := runChannelRatioMonitorTaskOnce(t.Context(), nil, nil)
			require.NoError(t, err)
			require.Equal(t, 1, summary.ChannelsDisabled)
		}
		persisted, err := model.GetChannelById(channel.Id, false)
		require.NoError(t, err)
		require.Equal(t, common.ChannelStatusAutoDisabled, persisted.Status)
	}
}

func TestResponsesWSMonitorDrainLifecycle(t *testing.T) {
	for _, trigger := range []string{"low-balance", "cost-policy", "update-failure"} {
		for _, terminal := range []string{"completed", "cancelled", "failed", "idle"} {
			t.Run(trigger+"/"+terminal, func(t *testing.T) {
				release := make(chan struct{})
				var releaseOnce sync.Once
				t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
				var creates atomic.Int32
				fixture := newResponsesWSBillingTest(t, `tier("usage", p * 2)`, func(ws *websocket.Conn, _ *http.Request) {
					if _, _, err := ws.ReadMessage(); err != nil {
						return
					}
					creates.Add(1)
					if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"drain","status":"in_progress"}}`)); err != nil {
						return
					}
					if terminal != "idle" {
						select {
						case <-release:
						case <-time.After(5 * time.Second):
							return
						}
					}
					status := terminal
					if status == "idle" {
						status = "completed"
					}
					if err := ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"response.%s","response":{"id":"drain","status":%q,"usage":{"input_tokens":20,"output_tokens":5}}}`, status, status))); err != nil {
						return
					}
					for {
						if _, _, err := ws.ReadMessage(); err != nil {
							return
						}
						creates.Add(1)
					}
				})
				disable := setupResponsesMonitorDisable(t, fixture.channel, trigger)
				t.Cleanup(func() { fixture.closeAndWait(t); require.NoError(t, service.FlushChannelDailyCostEvents()) })
				_, err := service.SaveChannelConcurrencyLimit(t.Context(), fixture.channel.Id, 1)
				require.NoError(t, err)
				payload := []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)
				require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, payload))
				require.Equal(t, "response.created", readResponsesWSTestEvent(t, fixture.client)["type"])
				if terminal == "idle" {
					require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
				}
				disable()
				if terminal != "idle" {
					require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, payload))
					rejection := readResponsesWSTestEvent(t, fixture.client)
					assert.Equal(t, "error", rejection["type"])
					assert.Equal(t, float64(http.StatusConflict), rejection["status"])
					snapshot, err := service.GetChannelConcurrencySnapshotWithRPM(t.Context())
					require.NoError(t, err)
					assert.EqualValues(t, 1, snapshot[fixture.channel.Id].Active)
					releaseOnce.Do(func() { close(release) })
					require.Equal(t, "response."+terminal, readResponsesWSTestEvent(t, fixture.client)["type"])
				}
				require.NoError(t, fixture.client.SetReadDeadline(time.Now().Add(3*time.Second)))
				_, _, err = fixture.client.ReadMessage()
				require.True(t, websocket.IsCloseError(err, websocket.ClosePolicyViolation), "expected terminal followed by 1008: %v", err)
				fixture.closeAndWait(t)
				assert.Equal(t, int32(1), creates.Load())
				snapshot, err := service.GetChannelConcurrencySnapshotWithRPM(t.Context())
				require.NoError(t, err)
				assert.Zero(t, snapshot[fixture.channel.Id].Active)
				assertResponsesWSAccounting(t, fixture, []int{20})
				require.NoError(t, service.FlushChannelDailyCostEvents())
				var cost model.ChannelDailyCost
				require.NoError(t, model.DB.Where("channel_id = ?", fixture.channel.Id).First(&cost).Error)
				assert.EqualValues(t, 1, cost.SettledCount+cost.UnresolvedCount, "one generation must produce one cost record")
			})
		}
	}
}

func TestResponsesWSMonitorStaleDisableDoesNotDrain(t *testing.T) {
	for _, trigger := range []string{"low-balance", "cost-policy"} {
		t.Run(trigger, func(t *testing.T) {
			var calls atomic.Int32
			fixture := newResponsesWSAdmissionTest(t, &calls)
			monitor := model.ChannelRatioMonitor{ChannelId: fixture.channel.Id, UpstreamRevision: 2, BalanceAutoDisableThreshold: common.GetPointer(10.0)}
			require.NoError(t, model.DB.Create(&monitor).Error)
			t.Cleanup(func() { require.NoError(t, model.DB.Delete(&monitor).Error) })
			payload := []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)
			require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, payload))
			require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
			if trigger == "low-balance" {
				monitor.UpstreamRevision = 1
				changed, err := autoDisableChannelMonitorAtEffectiveBalance(monitor, fixture.channel, 1, 1, 0)
				require.NoError(t, err)
				assert.False(t, changed)
			} else {
				_, _, disabled, _, err := applyChannelMonitorPolicyPlan(t.Context(), channelMonitorPolicyPlan{
					DisableChannelIds: []int{fixture.channel.Id}, DisableChannelRevisions: map[int]int64{fixture.channel.Id: 1},
					DisableChannelStatuses: map[int]model.ChannelMonitorStatusSnapshot{fixture.channel.Id: model.CaptureChannelMonitorStatus(fixture.channel)},
				})
				require.NoError(t, err)
				assert.Empty(t, disabled)
			}
			require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, payload))
			require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
			assert.Equal(t, int32(2), calls.Load())
			assertResponsesWSAccounting(t, fixture, []int{20, 20})
		})
	}
}

func TestResponsesWSMonitorDrainStillAllowsImmediateClose(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("usage", p * 2)`, func(ws *websocket.Conn, _ *http.Request) {
		if _, _, err := ws.ReadMessage(); err != nil {
			return
		}
		_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"drain","status":"in_progress"}}`))
		_, _, _ = ws.ReadMessage()
	})
	t.Cleanup(func() { fixture.closeAndWait(t); require.NoError(t, service.FlushChannelDailyCostEvents()) })
	disable := setupResponsesMonitorDisable(t, fixture.channel, "low-balance")
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)))
	require.Equal(t, "response.created", readResponsesWSTestEvent(t, fixture.client)["type"])
	disable()
	assert.Equal(t, 1, wsmanager.CloseChannel(fixture.channel.Id, "管理员关闭"))
	_, _, err := fixture.client.ReadMessage()
	require.True(t, websocket.IsCloseError(err, websocket.ClosePolicyViolation), "%v", err)
	fixture.closeAndWait(t)
	assertResponsesWSAccounting(t, fixture, []int{0})
}

func TestResponsesWSMonitorDrainRefundsRejectedPendingCreate(t *testing.T) {
	admitted, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
		if _, _, err := ws.ReadMessage(); err != nil {
			return
		}
		close(admitted)
		select {
		case <-release:
		case <-time.After(5 * time.Second):
			return
		}
		_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","status":400,"error":{"type":"invalid_request_error","code":"invalid_request","message":"rejected"}}`))
		_, _, _ = ws.ReadMessage()
	})
	t.Cleanup(func() { fixture.closeAndWait(t); require.NoError(t, service.FlushChannelDailyCostEvents()) })
	disable := setupResponsesMonitorDisable(t, fixture.channel, "low-balance")
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)))
	select {
	case <-admitted:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream did not receive create")
	}
	require.NoError(t, model.DB.First(fixture.token, fixture.token.Id).Error)
	assert.Equal(t, 2000, fixture.token.RemainQuota, "fixed-price request reserved 1000 before disable")
	disable()
	once.Do(func() { close(release) })
	rejection := readResponsesWSTestEvent(t, fixture.client)
	assert.Equal(t, "error", rejection["type"])
	assert.Equal(t, float64(400), rejection["status"])
	_, _, err := fixture.client.ReadMessage()
	require.True(t, websocket.IsCloseError(err, websocket.ClosePolicyViolation), "%v", err)
	fixture.closeAndWait(t)
	// Refund runs asynchronously; socket completion is not its database barrier.
	require.Eventually(t, func() bool {
		var user model.User
		var token model.Token
		return model.DB.First(&user, fixture.user.Id).Error == nil && model.DB.First(&token, fixture.token.Id).Error == nil && user.Quota == 100000 && token.RemainQuota == 3000
	}, 3*time.Second, 10*time.Millisecond)
	assertResponsesWSAccounting(t, fixture, nil)
}
