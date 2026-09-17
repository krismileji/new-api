package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpstreamAutomationLinkedRefreshDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			for _, test := range []struct {
				name            string
				upstreamBalance int64
				channelFetchErr bool
				wantCalls       int32
				wantStatus      string
				wantMessage     string
				wantWarning     string
			}{
				{"重置成功且预估不完整", 0, false, 1, "succeeded", "规则 余额重置 执行成功，已复查上游指标", "渠道 67 余额预估尚不完整"},
				{"真实余额充足且预估不完整", 100, false, 0, "checked", "检查完成，未执行接口", "渠道 67 余额预估尚不完整"},
				{"重置成功且关联渠道查询失败", 0, true, 1, "succeeded", "规则 余额重置 执行成功，已复查上游指标", "渠道 67 上游指标刷新失败"},
			} {
				t.Run(test.name, func(t *testing.T) {
					db := setupChannelMonitorCustomActionRefreshDB(t, engine)
					disableChannelMonitorSSRFProtection(t)
					useChannelMonitorOptionMap(t, map[string]string{channelMonitorAutoEnableOnBalanceRecoveryOption: "true", "GroupRatio": `{"default":1}`})
					previous := ratio_setting.GroupRatio2JSONString()
					require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
					t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previous)) })

					var balance atomic.Int64
					balance.Store(test.upstreamBalance)
					var metricsAvailable atomic.Bool
					var calls atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						switch r.URL.Path {
						case "/reset":
							calls.Add(1)
							balance.Store(100)
							_, _ = w.Write([]byte(`{"success":true}`))
							return
						case "/balance":
							if !metricsAvailable.Load() {
								http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
								return
							}
						case "/channel-balance":
							if test.channelFetchErr {
								http.Error(w, "channel unavailable", http.StatusServiceUnavailable)
								return
							}
						default:
							http.NotFound(w, r)
							return
						}
						_, _ = fmt.Fprintf(w, `{"balance":%d}`, balance.Load())
					}))
					defer server.Close()

					config := automationTestConfig(server.URL)
					config.IntervalMinutes = 2
					config.ChannelIDs = []int{67}
					channelConfig := config.CustomConfig
					channelConfig.Actions = nil
					channelRequest := *channelConfig.Balance.Request
					channelRequest.Path = "/channel-balance"
					channelConfig.Balance.Request = &channelRequest
					raw, err := service.MarshalChannelMonitorCustomUpstreamConfig(channelConfig)
					require.NoError(t, err)
					channel := model.Channel{Id: 67, Name: "关联渠道", Status: common.ChannelStatusAutoDisabled, Group: "default", Key: "test"}
					channel.SetOtherInfo(map[string]any{"status_reason": "渠道监控：上游余额 0 低于自动禁用阈值 5"})
					require.NoError(t, db.Create(&channel).Error)
					require.NoError(t, db.Create(&model.ChannelRatioMonitor{
						ChannelId: 67, Ratio: 0.5, UpdatedTime: 1, UpstreamType: "custom", UpstreamBaseURL: server.URL,
						CustomUpstreamConfig: raw, UpstreamRatioSyncDisabled: true,
						BalanceWarningThreshold: common.GetPointer(20.0), BalanceAutoDisableThreshold: common.GetPointer(5.0),
					}).Error)
					view, err := service.SaveUpstreamAutomation(t.Context(), config)
					require.NoError(t, err)
					now := time.Date(2026, 9, 18, 4, 0, 0, 0, time.UTC)
					clock := func() time.Time { return now }

					view, err = service.RunUpstreamAutomation(t.Context(), view.ID, true, clock, refreshUpstreamAutomationChannels)
					require.Error(t, err)
					assert.Equal(t, "fetch_failed", view.State.Status)
					assert.Equal(t, 1, view.State.Failures)
					assert.Greater(t, view.State.NextCheck, now.Unix()+120)
					assert.Zero(t, calls.Load())

					metricsAvailable.Store(true)
					now = time.Unix(view.State.NextCheck, 0)
					view, err = service.RunUpstreamAutomation(t.Context(), view.ID, false, clock, refreshUpstreamAutomationChannels)
					require.NoError(t, err)
					assert.Equal(t, test.wantStatus, view.State.Status)
					assert.Contains(t, view.State.Message, test.wantMessage)
					assert.Contains(t, view.State.Message, "关联渠道刷新提示："+test.wantWarning)
					assert.Zero(t, view.State.Failures)
					assert.Equal(t, now.Unix()+120, view.State.NextCheck)
					assert.Equal(t, test.wantCalls, calls.Load())
					assert.EqualValues(t, test.wantCalls, view.State.Actions["reset"].Attempts)
					require.NotNil(t, view.State.Balance)
					assert.Equal(t, 100.0, *view.State.Balance)
					require.NotEmpty(t, view.State.History)
					assert.Equal(t, test.wantStatus, view.State.History[len(view.State.History)-1].Status)
					assert.Equal(t, view.State.Message, view.State.History[len(view.State.History)-1].Message)

					now = now.Add(2 * time.Minute)
					view, err = service.RunUpstreamAutomation(t.Context(), view.ID, false, clock, refreshUpstreamAutomationChannels)
					require.NoError(t, err, "关联渠道的异常不应推迟下一次独立检查")
					assert.Equal(t, "checked", view.State.Status)
					assert.Equal(t, "当前指标不满足条件", view.State.Actions["reset"].SkipReason)
					assert.Equal(t, test.wantCalls, calls.Load(), "渠道刷新异常不应重放已经成功的重置")
					assert.Equal(t, now.Unix()+120, view.State.NextCheck)
					stored, err := model.GetChannelById(67, false)
					require.NoError(t, err)
					assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status, "预估不完整或查询失败时仍禁止自动恢复渠道")
					if !test.channelFetchErr {
						monitor, err := model.GetChannelRatioMonitor(67)
						require.NoError(t, err)
						require.NotNil(t, monitor.UpstreamBalance)
						assert.Equal(t, 100.0, *monitor.UpstreamBalance, "上游真实余额仍应正常保存")
						assert.Empty(t, monitor.LastBalanceError)
					}
				})
			}
		})
	}
}
