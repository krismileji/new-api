package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorBalanceOrphanRecoveryDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			for _, manual := range []bool{false, true} {
				t.Run(fmt.Sprintf("manual_disable_%t", manual), func(t *testing.T) {
					db := setupChannelMonitorBalanceSafetyDB(t, engine)
					disableChannelMonitorSSRFProtection(t)
					useChannelMonitorOptionMap(t, map[string]string{
						channelMonitorAutoEnableOnBalanceRecoveryOption: "true",
						channelMonitorAutoUpdateRetryCountOption:        "0",
					})
					previousRatios := ratio_setting.GroupRatio2JSONString()
					require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
					t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousRatios)) })
					channel := model.Channel{Id: 9042, Name: "残留余额占用", Group: "vip", Models: "gpt-5.5", Status: common.ChannelStatusAutoDisabled}
					if manual {
						channel.Status = common.ChannelStatusManuallyDisabled
					}
					channel.SetOtherInfo(map[string]any{"status_reason": "渠道监控：上游余额 0 低于自动禁用阈值 1"})
					require.NoError(t, db.Create(&channel).Error)
					require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Group: "vip", Model: "gpt-5.5", Enabled: false}).Error)
					custom, err := service.MarshalChannelMonitorCustomUpstreamConfig(service.ChannelMonitorCustomUpstreamConfig{
						Ratio:   service.ChannelMonitorCustomMetricConfig{Source: service.ChannelMonitorCustomSourceFixed, FixedValue: common.GetPointer(0.5)},
						Balance: service.ChannelMonitorCustomMetricConfig{Source: service.ChannelMonitorCustomSourceFixed, FixedValue: common.GetPointer(49.740274)},
					})
					require.NoError(t, err)
					monitor := model.ChannelRatioMonitor{
						ChannelId: channel.Id, UpstreamRevision: 28, Ratio: 0.5, UpdatedTime: 1,
						UpstreamType: service.CustomUpstreamType, UpstreamAuthType: service.CustomUpstreamAuthType,
						UpstreamBaseURL: "https://custom.example", CustomUpstreamConfig: custom, UpstreamRatioSyncDisabled: true,
						BalanceWarningThreshold: common.GetPointer(5.0), BalanceAutoDisableThreshold: common.GetPointer(1.0),
					}
					require.NoError(t, db.Create(&monitor).Error)
					initial, applied, err := recordChannelMonitorBalanceUpdate(t.Context(), monitor, common.GetPointer(0.0), "")
					require.NoError(t, err)
					require.True(t, applied)
					require.NotNil(t, initial.Estimate)
					config := service.ChannelBalanceConfigForMonitor(monitor)
					root := fmt.Sprintf("channel_balance:{%d}:%s", channel.Id, config.Account)
					client := common.RedisMonitorWriteClient()
					started := time.Now().Add(-6 * time.Hour).UnixMilli()
					// Match the production records, including the legacy recovery marker
					// that proves these are ordinary synchronous requests.
					for i := range 6 {
						id := fmt.Sprintf("orphan-%d", i)
						amount, source := "364", "average"
						if i >= 4 {
							amount, source = "0", "unknown"
						}
						encoded, err := common.Marshal(map[string]any{
							"epoch": initial.Estimate.Epoch, "status": "active", "source": source,
							"amount": amount, "known": i < 4, "started": fmt.Sprint(started), "samples": 6,
						})
						require.NoError(t, err)
						require.NoError(t, client.Set(t.Context(), root+":attempt:"+id, encoded, 42*time.Hour).Err())
						require.NoError(t, client.ZAdd(t.Context(), root+":active", &redis.Z{Score: float64(started), Member: id}).Err())
						encoded, err = common.Marshal(map[string]any{"Config": config, "CompletionUncertain": false})
						require.NoError(t, err)
						require.NoError(t, client.Set(t.Context(), fmt.Sprintf("channel_balance:{%d}:recovery:%s", channel.Id, id), encoded, 42*time.Hour).Err())
					}
					require.NoError(t, client.HSet(t.Context(), root+":state", "active", 6, "unknown_active", 2, "average_active", 4, "inflight", 1456, "applied_decision", "low").Err())
					require.True(t, service.ChannelBalanceHasIdleRequestCoverage(t.Context(), channel.Id))

					summary, err := runChannelRatioMonitorTaskOnce(t.Context(), nil, nil)
					require.NoError(t, err)
					assert.Equal(t, 1, summary.BalanceUpdated)
					estimate, err := service.GetChannelBalanceEstimate(t.Context(), config)
					require.NoError(t, err)
					assert.True(t, estimate.Complete)
					assert.Zero(t, estimate.InFlightCount)
					assert.Zero(t, estimate.UnknownCount)
					assert.Zero(t, estimate.InFlightConsumption)
					assert.Equal(t, 49.740274, estimate.EstimatedBalance)
					stored, err := model.GetChannelById(channel.Id, true)
					require.NoError(t, err)
					var ability model.Ability
					require.NoError(t, db.First(&ability, "channel_id = ?", channel.Id).Error)
					if manual {
						assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
						assert.Zero(t, summary.ChannelsEnabled)
						assert.False(t, ability.Enabled)
					} else {
						assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
						assert.Empty(t, stored.GetOtherInfo()["status_reason"])
						assert.Equal(t, 1, summary.ChannelsEnabled)
						assert.True(t, ability.Enabled)
					}
				})
			}
		})
	}
}

func TestChannelBalanceDirectProbeTracksUntilTransportEnds(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			withSelfUseModeEnabled(t)
			service.InitHttpClient()
			t.Cleanup(func() { require.NoError(t, service.FlushChannelDailyCostEvents()) })
			user := model.User{Username: "balance-probe", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000}
			require.NoError(t, db.Create(&user).Error)
			const channelID = 9043
			observedIdle := make(chan bool, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				observedIdle <- service.ChannelBalanceHasIdleRequestCoverage(r.Context(), channelID)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				body := `{"error":{"message":"test failure","type":"test_error"}}`
				if status == http.StatusOK {
					body = `{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"gpt-3.5-turbo","choices":[{"index":0,"message":{"role":"assistant","content":"Hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
				}
				_, err := w.Write([]byte(body))
				assert.NoError(t, err)
			}))
			t.Cleanup(server.Close)
			channel := model.Channel{Id: channelID, Type: constant.ChannelTypeOpenAI, Key: "test-key", Name: "余额探测租约", Status: common.ChannelStatusEnabled,
				BaseURL: common.GetPointer(server.URL), Models: "gpt-3.5-turbo", Group: "default"}
			require.NoError(t, db.Create(&channel).Error)
			require.True(t, service.ChannelBalanceHasIdleRequestCoverage(t.Context(), channelID))
			result := testChannel(t.Context(), &channel, user.Id, "gpt-3.5-turbo", "", false)
			if status == http.StatusOK {
				require.NoError(t, result.localErr)
			} else {
				require.Error(t, result.localErr)
			}
			select {
			case idle := <-observedIdle:
				assert.False(t, idle, "an active direct probe must prevent orphan recovery")
			default:
				t.Fatal("upstream probe was not dispatched")
			}
			assert.True(t, service.ChannelBalanceHasIdleRequestCoverage(t.Context(), channelID), "both success and failure release the probe lease")
		})
	}
}
