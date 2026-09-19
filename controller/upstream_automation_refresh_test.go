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
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
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
				{"重置成功且预估不可用仍恢复", 0, false, 1, "succeeded", "规则 余额重置 执行成功，已复查上游指标", ""},
				{"真实余额充足且预估不可用仍恢复", 100, false, 0, "checked", "检查完成，未执行接口", ""},
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
					if test.wantWarning != "" {
						assert.Contains(t, view.State.Message, "关联渠道刷新提示："+test.wantWarning)
					} else {
						assert.Equal(t, test.wantMessage, view.State.Message)
					}
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
					if test.channelFetchErr {
						assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status, "渠道查询失败时不能恢复")
					} else {
						assert.Equal(t, common.ChannelStatusEnabled, stored.Status, "按复查的上游余额恢复，无需等待预估")
					}
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

func TestUpstreamAutomationBalanceRecoveryDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			for _, mode := range []string{"standalone", "linked_account", "account"} {
				t.Run(mode, func(t *testing.T) {
					for _, test := range []struct {
						name             string
						refreshedBalance int64
						manualDisabled   bool
						recoveryDisabled bool
						actionFailed     bool
						refreshFailed    bool
						wantStatus       string
						wantChannel      int
					}{
						{name: "请求未结束时余额恢复仍启用", refreshedBalance: 100, wantStatus: "succeeded", wantChannel: common.ChannelStatusEnabled},
						{name: "余额恰好达到阈值", refreshedBalance: 5, wantStatus: "succeeded", wantChannel: common.ChannelStatusEnabled},
						{name: "接口成功但余额仍不足", refreshedBalance: 4, wantStatus: "succeeded", wantChannel: common.ChannelStatusAutoDisabled},
						{name: "手动停用保持停用", refreshedBalance: 100, manualDisabled: true, wantStatus: "succeeded", wantChannel: common.ChannelStatusManuallyDisabled},
						{name: "关闭自动恢复", refreshedBalance: 100, recoveryDisabled: true, wantStatus: "succeeded", wantChannel: common.ChannelStatusAutoDisabled},
						{name: "接口失败不恢复", refreshedBalance: 100, actionFailed: true, wantStatus: "action_failed", wantChannel: common.ChannelStatusAutoDisabled},
						{name: "复查失败不恢复", refreshedBalance: 100, refreshFailed: true, wantStatus: "refresh_failed", wantChannel: common.ChannelStatusAutoDisabled},
					} {
						t.Run(test.name, func(t *testing.T) {
							db := setupChannelMonitorCustomActionRefreshDB(t, engine)
							disableChannelMonitorSSRFProtection(t)
							useChannelMonitorOptionMap(t, map[string]string{channelMonitorAutoEnableOnBalanceRecoveryOption: fmt.Sprint(!test.recoveryDisabled), "GroupRatio": `{"default":1}`})
							previous := ratio_setting.GroupRatio2JSONString()
							require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
							t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previous)) })

							var calls, polls atomic.Int32
							server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
								switch r.URL.Path {
								case "/reset":
									calls.Add(1)
									if test.actionFailed {
										http.Error(w, "reset failed", http.StatusServiceUnavailable)
										return
									}
									_, _ = w.Write([]byte(`{"success":true}`))
								case "/balance":
									polls.Add(1)
									balance := int64(0)
									if calls.Load() > 0 {
										if test.refreshFailed {
											http.Error(w, "balance unavailable", http.StatusServiceUnavailable)
											return
										}
										balance = test.refreshedBalance
									}
									_, _ = fmt.Fprintf(w, `{"balance":%d}`, balance)
								default:
									http.NotFound(w, r)
								}
							}))
							defer server.Close()
							config := automationTestConfig(server.URL)
							config.ChannelIDs = []int{67}
							custom := config.CustomConfig
							custom.Actions = nil
							raw, err := service.MarshalChannelMonitorCustomUpstreamConfig(custom)
							require.NoError(t, err)
							channel := model.Channel{Id: 67, Name: "余额恢复", Status: common.ChannelStatusAutoDisabled, Group: "default", Key: "test"}
							if test.manualDisabled {
								channel.Status = common.ChannelStatusManuallyDisabled
							}
							channel.SetOtherInfo(map[string]any{"status_reason": "渠道监控：上游余额 0 低于自动禁用阈值 5"})
							require.NoError(t, db.Create(&channel).Error)
							require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Group: "default", Model: "test-model", Enabled: false}).Error)
							monitor := model.ChannelRatioMonitor{ChannelId: channel.Id, Ratio: 0.5, UpdatedTime: 1,
								UpstreamType: "custom", UpstreamBaseURL: server.URL, CustomUpstreamConfig: raw,
								UpstreamRatioSyncDisabled: true, BalanceAutoDisableThreshold: common.GetPointer(5.0)}
							require.NoError(t, db.Create(&monitor).Error)
							if mode != "standalone" {
								account := createAutomationTestAccount(t, server.URL)
								previous := account
								settings, err := common.Marshal(model.ChannelMonitorAccountSettingsFromMonitor(monitor))
								require.NoError(t, err)
								account.Settings = string(settings)
								members, err := model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, &previous, []int{channel.Id})
								require.NoError(t, err)
								require.Len(t, members, 1)
								monitor = members[0]
								if mode == "account" {
									config.AccountID = account.ID
								}
							}
							require.NotNil(t, monitor.BalanceAutoDisableThreshold)

							client := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
							oldWrite, oldRead, oldRDB, oldEnabled := common.RDBMonitorWrite, common.RDBMonitorRead, common.RDB, common.RedisEnabled
							common.RDBMonitorWrite, common.RDBMonitorRead, common.RDB, common.RedisEnabled = client, client, client, true
							t.Cleanup(func() {
								common.RDBMonitorWrite, common.RDBMonitorRead, common.RDB, common.RedisEnabled = oldWrite, oldRead, oldRDB, oldEnabled
								assert.NoError(t, client.Close())
							})
							require.NoError(t, service.ReloadChannelConcurrencyLimits(t.Context()))
							lease, err := service.AcquireChannelBalanceProbeLease(t.Context(), channel.Id)
							require.NoError(t, err)
							require.NotNil(t, lease)
							defer lease.Release()
							require.False(t, service.ChannelBalanceHasIdleRequestCoverage(t.Context(), channel.Id))

							view, err := service.SaveUpstreamAutomation(t.Context(), config)
							require.NoError(t, err)
							clock := func() time.Time { return time.Date(2026, 9, 20, 4, 0, 0, 0, time.UTC) }
							view, err = service.RunUpstreamAutomation(t.Context(), view.ID, true, clock, refreshUpstreamAutomationChannels)
							if test.actionFailed || test.refreshFailed {
								require.Error(t, err)
							} else {
								require.NoError(t, err)
								assert.NotContains(t, view.State.Message, "关联渠道刷新提示")
								require.NotNil(t, view.State.Balance)
								assert.Equal(t, float64(test.refreshedBalance), *view.State.Balance)
								estimate, err := service.GetChannelBalanceEstimate(t.Context(), service.ChannelBalanceConfigForMonitor(monitor))
								require.NoError(t, err)
								assert.False(t, estimate.Complete, "进行中的请求仍使预估覆盖不完整")
								if mode == "account" {
									assert.EqualValues(t, 2, polls.Load(), "账户只需触发前、触发后各查询一次")
								}
							}
							assert.Equal(t, test.wantStatus, view.State.Status)
							assert.EqualValues(t, 1, calls.Load())
							stored, err := model.GetChannelById(channel.Id, true)
							require.NoError(t, err)
							assert.Equal(t, test.wantChannel, stored.Status)
							var ability model.Ability
							require.NoError(t, db.First(&ability, "channel_id = ?", channel.Id).Error)
							assert.Equal(t, test.wantChannel == common.ChannelStatusEnabled, ability.Enabled, "恢复渠道同时恢复路由能力")
						})
					}
				})
			}
		})
	}
}
