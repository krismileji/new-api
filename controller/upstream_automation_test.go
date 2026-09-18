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
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func automationTestConfig(baseURL string) service.UpstreamAutomationConfig {
	threshold, ratio, balance := 10.0, 0.5, 0.0
	return service.UpstreamAutomationConfig{
		Name: "独立余额重置", Enabled: true, BaseURL: baseURL, IntervalMinutes: 1, RequestTimeout: 5,
		CustomConfig: service.ChannelMonitorCustomUpstreamConfig{
			Ratio:   service.ChannelMonitorCustomMetricConfig{Source: "fixed", FixedValue: &ratio},
			Balance: service.ChannelMonitorCustomMetricConfig{Source: "http", Request: &service.ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/balance"}, Result: &service.ChannelMonitorCustomResultConfig{ResponseType: "json", ValuePath: "balance", Multiplier: 1}, FixedValue: &balance},
			Actions: []service.ChannelMonitorCustomAction{{ID: "reset", Name: "余额重置", Enabled: true, TriggerMode: "repeat", Metric: "balance", Operator: "lt", Threshold: &threshold, Timezone: "Asia/Shanghai", StartTime: "00:00", EndTime: "23:00", DailyLimit: 3, CooldownMinutes: 1, Request: service.ChannelMonitorCustomRequestConfig{Method: "POST", Path: "/reset", BodyType: "none"}, SuccessPath: "success", SuccessValue: "true"}},
		},
	}
}

func TestUpstreamAutomationDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			t.Run("独立检查不受渠道和每日边界影响并遵守冷却限额", func(t *testing.T) {
				db := setupChannelMonitorCustomActionRefreshDB(t, engine)
				disableChannelMonitorSSRFProtection(t)
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/reset" {
						calls.Add(1)
						_, _ = w.Write([]byte(`{"success":true}`))
						return
					}
					_, _ = w.Write([]byte(`{"balance":0}`))
				}))
				defer server.Close()
				config := automationTestConfig(server.URL)
				config.CustomConfig.Actions[0].DailyLimit = 2
				view, err := service.SaveUpstreamAutomation(t.Context(), config)
				require.NoError(t, err)
				now := time.Date(2026, 9, 17, 4, 0, 0, 0, time.UTC)
				clock := func() time.Time { return now }
				view, err = service.RunUpstreamAutomation(t.Context(), view.ID, true, clock, nil)
				require.NoError(t, err)
				assert.EqualValues(t, 1, calls.Load())
				require.NoError(t, db.AutoMigrate(&model.SystemTask{}))
				require.NoError(t, db.AutoMigrate(&model.SystemTask{}))
				view, err = service.RunUpstreamAutomation(t.Context(), view.ID, true, clock, nil)
				require.NoError(t, err)
				assert.Equal(t, "尚在冷却时间内", view.State.Actions["reset"].SkipReason)
				now = now.Add(time.Minute)
				_, err = service.RunUpstreamAutomation(t.Context(), view.ID, true, clock, nil)
				require.NoError(t, err)
				assert.EqualValues(t, 2, calls.Load(), "持续满足时冷却结束后仍可执行")
				now = now.Add(time.Minute)
				view, err = service.RunUpstreamAutomation(t.Context(), view.ID, true, clock, nil)
				require.NoError(t, err)
				assert.Equal(t, "当日调用次数已用完", view.State.Actions["reset"].SkipReason)
				now = now.Add(24 * time.Hour)
				view, err = service.RunUpstreamAutomation(t.Context(), view.ID, true, clock, nil)
				require.NoError(t, err)
				assert.EqualValues(t, 3, calls.Load())
				assert.Equal(t, 1, view.State.Actions["reset"].Attempts)
			})
			t.Run("查询故障恢复后继续执行且多渠道只调用一次", func(t *testing.T) {
				db := setupChannelMonitorCustomActionRefreshDB(t, engine)
				// Automatic recovery needs a complete estimate even without a warning threshold.
				client := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
				originalWrite, originalRead, originalRDB, originalEnabled := common.RDBMonitorWrite, common.RDBMonitorRead, common.RDB, common.RedisEnabled
				common.RDBMonitorWrite, common.RDBMonitorRead, common.RDB, common.RedisEnabled = client, client, client, true
				t.Cleanup(func() {
					common.RDBMonitorWrite, common.RDBMonitorRead, common.RDB, common.RedisEnabled = originalWrite, originalRead, originalRDB, originalEnabled
					assert.NoError(t, client.Close())
				})
				disableChannelMonitorSSRFProtection(t)
				useChannelMonitorOptionMap(t, map[string]string{channelMonitorAutoEnableOnBalanceRecoveryOption: "true", "GroupRatio": `{"default":1}`})
				previous := ratio_setting.GroupRatio2JSONString()
				require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
				t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previous)) })
				var healthy atomic.Bool
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !healthy.Load() {
						http.Error(w, "unavailable", 503)
						return
					}
					if r.URL.Path == "/reset" {
						calls.Add(1)
						_, _ = w.Write([]byte(`{"success":true}`))
						return
					}
					balance := 0
					if calls.Load() > 0 {
						balance = 100
					}
					_, _ = fmt.Fprintf(w, `{"balance":%d}`, balance)
				}))
				defer server.Close()
				config := automationTestConfig(server.URL)
				config.ChannelIDs = []int{81, 82, 83, 84}
				channelConfig := config.CustomConfig
				channelConfig.Actions = nil
				raw, err := service.MarshalChannelMonitorCustomUpstreamConfig(channelConfig)
				require.NoError(t, err)
				threshold := 10.0
				for _, id := range config.ChannelIDs {
					status := common.ChannelStatusAutoDisabled
					if id == 83 {
						status = common.ChannelStatusManuallyDisabled
					}
					channel := model.Channel{Id: id, Name: fmt.Sprint(id), Status: status, Group: "default", Key: "test"}
					channel.SetOtherInfo(map[string]any{"status_reason": "渠道监控：上游余额 0 低于自动禁用阈值 10"})
					require.NoError(t, db.Create(&channel).Error)
					updatedTime := int64(1)
					if id == 84 {
						updatedTime = 0
					}
					require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: id, Ratio: 0.5, UpdatedTime: updatedTime, UpstreamType: "custom", UpstreamBaseURL: server.URL, CustomUpstreamConfig: raw, UpstreamRatioSyncDisabled: true, BalanceConsecutiveFailures: 100, LastBalanceError: "旧查询失败", BalanceAutoDisableThreshold: &threshold}).Error)
				}
				require.NoError(t, service.ReloadChannelConcurrencyLimits(t.Context()))
				view, err := service.SaveUpstreamAutomation(t.Context(), config)
				require.NoError(t, err)
				now := time.Date(2026, 9, 17, 4, 0, 0, 0, time.UTC)
				clock := func() time.Time { return now }
				view, err = service.RunUpstreamAutomation(t.Context(), view.ID, true, clock, refreshUpstreamAutomationChannels)
				require.Error(t, err)
				assert.Equal(t, "fetch_failed", view.State.Status)
				assert.Greater(t, view.State.NextCheck, now.Unix())
				healthy.Store(true)
				now = time.Unix(view.State.NextCheck, 0)
				view, err = service.RunUpstreamAutomation(t.Context(), view.ID, false, clock, refreshUpstreamAutomationChannels)
				require.NoError(t, err)
				assert.EqualValues(t, 1, calls.Load())
				for _, id := range config.ChannelIDs {
					monitor, err := model.GetChannelRatioMonitor(id)
					require.NoError(t, err)
					require.NotNil(t, monitor.UpstreamBalance)
					assert.Equal(t, 100.0, *monitor.UpstreamBalance)
					assert.Zero(t, monitor.BalanceConsecutiveFailures)
					channel, err := model.GetChannelById(id, false)
					require.NoError(t, err)
					want := common.ChannelStatusEnabled
					if id == 83 {
						want = common.ChannelStatusManuallyDisabled
					}
					if id == 84 {
						want = common.ChannelStatusAutoDisabled
					}
					assert.Equal(t, want, channel.Status)
				}
			})
			t.Run("失败结果必须确认且并发检查不重放", func(t *testing.T) {
				setupChannelMonitorCustomActionRefreshDB(t, engine)
				disableChannelMonitorSSRFProtection(t)
				entered, release := make(chan struct{}), make(chan struct{})
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/reset" {
						if calls.Add(1) == 1 {
							close(entered)
							<-release
						}
						http.Error(w, "unknown", 503)
						return
					}
					_, _ = w.Write([]byte(`{"balance":0}`))
				}))
				defer server.Close()
				view, err := service.SaveUpstreamAutomation(t.Context(), automationTestConfig(server.URL))
				require.NoError(t, err)
				now := time.Date(2026, 9, 17, 4, 0, 0, 0, time.UTC)
				clock := func() time.Time { return now }
				var firstErr error
				var wg sync.WaitGroup
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, firstErr = service.RunUpstreamAutomation(t.Context(), view.ID, true, clock, nil)
				}()
				<-entered
				_, err = service.RunUpstreamAutomation(t.Context(), view.ID, true, clock, nil)
				assert.ErrorIs(t, err, service.ErrUpstreamAutomationNotDue)
				close(release)
				wg.Wait()
				require.Error(t, firstErr)
				now = now.Add(2 * time.Minute)
				view, err = service.RunUpstreamAutomation(t.Context(), view.ID, true, clock, nil)
				require.NoError(t, err)
				assert.EqualValues(t, 1, calls.Load())
				assert.Equal(t, "上次执行结果待人工确认", view.State.Actions["reset"].SkipReason)
				require.Error(t, service.AcknowledgeUpstreamAutomationAction(t.Context(), view.ID, "reset", "stale", view.Revision))
				require.NoError(t, service.AcknowledgeUpstreamAutomationAction(t.Context(), view.ID, "reset", view.State.Actions["reset"].AttemptID, view.Revision))
				view, err = service.RunUpstreamAutomation(t.Context(), view.ID, true, clock, nil)
				require.Error(t, err)
				assert.EqualValues(t, 2, calls.Load())
				assert.Equal(t, 2, view.State.Actions["reset"].Attempts)
			})
			t.Run("旧规则迁移两次保留凭据计数且不再由渠道触发", func(t *testing.T) {
				db := setupChannelMonitorCustomActionRefreshDB(t, engine)
				config := automationTestConfig("https://upstream.example")
				config.CustomConfig.Actions[0].TriggerMode = ""
				config.CustomConfig.Actions[0].Request.Headers = []service.ChannelMonitorCustomKeyValue{{Key: "Authorization", Value: "secret-test", Secret: true}}
				raw, err := service.MarshalChannelMonitorCustomUpstreamConfig(config.CustomConfig)
				require.NoError(t, err)
				require.NoError(t, db.Create(&model.Channel{Id: 81, Name: "旧渠道", Status: common.ChannelStatusAutoDisabled}).Error)
				require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 81, UpstreamType: "custom", UpstreamRevision: 2, UpstreamBaseURL: config.BaseURL, CustomUpstreamConfig: raw}).Error)
				oldState := model.ChannelMonitorCustomActionState{Triggered: true, Day: "2026-09-17", Attempts: 2, LastAttempt: 100, AttemptID: "previous", Status: "failed"}
				require.NoError(t, model.UpdateChannelMonitorCustomActionState(t.Context(), 81, nil, func(_ model.ChannelRatioMonitor, states map[string]model.ChannelMonitorCustomActionState) (bool, error) {
					states["reset"] = oldState
					return true, nil
				}))
				for range 2 {
					require.NoError(t, db.AutoMigrate(&model.ChannelRatioMonitor{}, &model.SystemTask{}))
					require.NoError(t, service.MigrateUpstreamAutomations(t.Context(), 3))
				}
				rows, err := model.ListUpstreamAutomations(t.Context())
				require.NoError(t, err)
				require.Len(t, rows, 1)
				view, err := service.UpstreamAutomationResponse(rows[0])
				require.NoError(t, err)
				assert.Equal(t, 3, view.IntervalMinutes, "迁移保留原有检查频率")
				assert.Equal(t, 2, view.State.Actions["reset"].Attempts)
				assert.True(t, view.State.Actions["reset"].NeedsConfirmation)
				encoded, err := common.Marshal(view)
				require.NoError(t, err)
				assert.NotContains(t, string(encoded), "secret-test")
				assert.Nil(t, rows[0].ToResponse().Payload)
				saved, err := service.SaveUpstreamAutomation(t.Context(), view.UpstreamAutomationConfig)
				require.NoError(t, err)
				row, err := model.GetUpstreamAutomation(t.Context(), saved.ID)
				require.NoError(t, err)
				assert.Contains(t, row.Payload, "secret-test")
				monitor, err := model.GetChannelRatioMonitor(81)
				require.NoError(t, err)
				remaining, err := service.ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
				require.NoError(t, err)
				assert.Empty(t, remaining.Actions)
				assert.EqualValues(t, 3, monitor.UpstreamRevision)
				require.NoError(t, model.DeleteUpstreamAutomation(t.Context(), saved.ID, saved.Revision))
				_, err = model.SaveChannelRatioUpstreamConfig(81, "custom", config.BaseURL, "", "custom", 0, "", model.ChannelRatioUpstreamOptions{CustomUpstreamConfig: raw})
				require.ErrorContains(t, err, "已迁移", "删除独立任务后，旧页面也不能重新写入渠道规则")
			})
			t.Run("发送前凭据失败可重试且首次满足模式不会跨日重放", func(t *testing.T) {
				setupChannelMonitorCustomActionRefreshDB(t, engine)
				disableChannelMonitorSSRFProtection(t)
				var ready atomic.Bool
				var calls, logins atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/login":
						logins.Add(1)
						if !ready.Load() {
							http.Error(w, "unavailable", 503)
							return
						}
						assert.Equal(t, "login-password", r.Header.Get("X-Password"))
						_, _ = w.Write([]byte(`{"token":"runtime-token"}`))
					case "/reset":
						assert.Equal(t, "Bearer runtime-token", r.Header.Get("Authorization"))
						calls.Add(1)
						_, _ = w.Write([]byte(`{"success":true}`))
					default:
						_, _ = w.Write([]byte(`{"balance":0}`))
					}
				}))
				defer server.Close()
				config := automationTestConfig(server.URL)
				config.CustomConfig.VariableRequests = automationTestVariables()
				config.CustomConfig.Actions[0].TriggerMode = "edge"
				config.CustomConfig.Actions[0].Request.Headers = []service.ChannelMonitorCustomKeyValue{{Key: "Authorization", ValueTemplate: "Bearer {{token}}"}}
				view, err := service.SaveUpstreamAutomation(t.Context(), config)
				require.NoError(t, err)
				now := time.Date(2026, 9, 17, 4, 0, 0, 0, time.UTC)
				clock := func() time.Time { return now }
				view, err = service.RunUpstreamAutomation(t.Context(), view.ID, true, clock, nil)
				require.Error(t, err)
				assert.False(t, view.State.Actions["reset"].NeedsConfirmation)
				assert.False(t, view.State.Actions["reset"].Triggered)
				assert.Zero(t, view.State.Actions["reset"].Attempts)
				assert.Zero(t, calls.Load())
				ready.Store(true)
				now = time.Unix(view.State.NextCheck, 0)
				view, err = service.RunUpstreamAutomation(t.Context(), view.ID, false, clock, nil)
				require.NoError(t, err)
				assert.EqualValues(t, 1, calls.Load())
				assert.Equal(t, 1, view.State.Actions["reset"].Attempts)
				stored, err := model.GetUpstreamAutomation(t.Context(), view.ID)
				require.NoError(t, err)
				assert.Contains(t, stored.Payload, "runtime-token")
				encoded, err := common.Marshal(view)
				require.NoError(t, err)
				assert.NotContains(t, string(encoded), "runtime-token")
				assert.NotContains(t, string(encoded), "login-password")
				now = now.Add(24 * time.Hour)
				view, err = service.RunUpstreamAutomation(t.Context(), view.ID, false, clock, nil)
				require.NoError(t, err)
				assert.Equal(t, "首次满足模式：等待指标退出触发条件", view.State.Actions["reset"].SkipReason)
				assert.EqualValues(t, 1, calls.Load())
				assert.EqualValues(t, 2, logins.Load())
			})
			t.Run("独立任务共享凭据刷新并保护引用和编辑版本", func(t *testing.T) {
				db := setupChannelMonitorCustomActionRefreshDB(t, engine)
				require.NoError(t, db.AutoMigrate(&model.ChannelMonitorVariableGroup{}))
				t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&model.ChannelMonitorVariableGroup{})) })
				disableChannelMonitorSSRFProtection(t)
				var logins atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/login" {
						logins.Add(1)
						_, _ = w.Write([]byte(`{"token":"shared-token"}`))
						return
					}
					assert.Equal(t, "Bearer shared-token", r.Header.Get("Authorization"))
					_, _ = w.Write([]byte(`{"balance":100}`))
				}))
				defer server.Close()
				group, err := service.SaveChannelMonitorVariableGroup(t.Context(), service.ChannelMonitorVariableGroupConfig{Name: "账户登录", BaseURL: server.URL, RequestTimeout: 5, VariableRequests: automationTestVariables()})
				require.NoError(t, err)
				config := automationTestConfig(server.URL)
				config.CustomConfig.VariableGroupID = group.ID
				config.CustomConfig.Balance.Request.Headers = []service.ChannelMonitorCustomKeyValue{{Key: "Authorization", ValueTemplate: "Bearer {{token}}"}}
				view, err := service.SaveUpstreamAutomation(t.Context(), config)
				require.NoError(t, err)
				clock := func() time.Time { return time.Date(2026, 9, 17, 4, 0, 0, 0, time.UTC) }
				view, err = service.RunUpstreamAutomation(t.Context(), view.ID, true, clock, nil)
				require.NoError(t, err)
				assert.EqualValues(t, 1, logins.Load())
				storedGroup, err := model.GetChannelMonitorVariableGroup(t.Context(), group.ID)
				require.NoError(t, err)
				assert.Contains(t, storedGroup.Config, "shared-token")
				group, err = service.ChannelMonitorVariableGroupView(storedGroup)
				require.NoError(t, err)
				require.ErrorContains(t, model.DeleteChannelMonitorVariableGroup(t.Context(), group.ID, group.Revision), "上游自动任务引用")
				broken := group
				broken.VariableRequests = automationTestVariables()
				broken.VariableRequests[0].Variables[0].Name = "renamed"
				_, err = service.SaveChannelMonitorVariableGroup(t.Context(), broken)
				require.ErrorContains(t, err, "仍在使用")
				group.Name = "新的账户名称"
				_, err = service.SaveChannelMonitorVariableGroup(t.Context(), group)
				require.NoError(t, err)
				_, err = service.SaveUpstreamAutomation(t.Context(), view.UpstreamAutomationConfig)
				require.ErrorContains(t, err, "配置已变化")
				view, err = service.RunUpstreamAutomation(t.Context(), view.ID, true, clock, nil)
				require.NoError(t, err)
				assert.EqualValues(t, 2, view.Revision)
				assert.EqualValues(t, 1, logins.Load(), "后续检查复用已经持久化的共享凭据")
			})
		})
	}
}

func automationTestVariables() []service.ChannelMonitorCustomVariableRequest {
	return []service.ChannelMonitorCustomVariableRequest{{
		ID: "login", Name: "获取凭据", RefreshPolicy: "on_failure", ResponseType: "json",
		Request:   service.ChannelMonitorCustomRequestConfig{Method: "POST", Path: "/login", Headers: []service.ChannelMonitorCustomKeyValue{{Key: "X-Password", Value: "login-password", Secret: true}}},
		Variables: []service.ChannelMonitorCustomVariable{{Name: "token", ValuePath: "token"}},
	}}
}
