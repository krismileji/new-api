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
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelMonitorCustomActionResetDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelMonitorCustomActionRefreshDB(t, engine)
			now := time.Date(2026, 9, 13, 16, 1, 0, 0, time.UTC)
			for index, tc := range []struct {
				name            string
				actionID        string
				upstreamType    string
				status          string
				requestDay      string
				requestAttempts int
				lastAttemptDiff int64
				missingState    bool
				wantError       string
			}{
				{name: "按规则时区清零并保留冷却触发状态和其他规则"},
				{name: "正在执行时拒绝重置", status: "running", wantError: "接口正在执行"},
				{name: "不允许重置未保存规则", actionID: "missing", wantError: "触发规则不存在"},
				{name: "非自定义上游拒绝重置", upstreamType: "new_api", wantError: "已保存的自定义上游规则"},
				{name: "确认期间跨日拒绝重置昨日次数", requestDay: "2026-09-13", wantError: "统计日期已变化"},
				{name: "确认期间次数变化拒绝重置", requestAttempts: 2, wantError: "调用记录已变化"},
				{name: "相同次数但登记时间变化拒绝重置", lastAttemptDiff: -60, wantError: "调用记录已变化"},
				{name: "缺少执行记录拒绝重置", missingState: true, wantError: "调用记录已变化"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					channelID := 200 + index
					monitor, _, states := createChannelMonitorCustomActionResetFixture(t, db, channelID, now, "Asia/Shanghai", "https://upstream.example")
					request := service.ChannelMonitorCustomActionResetRequest{Day: "2026-09-14", Attempts: 1, LastAttempt: now.Add(-30 * time.Second).Unix()}
					actionID := "reset"
					if tc.actionID != "" {
						actionID = tc.actionID
					}
					if tc.upstreamType != "" {
						require.NoError(t, db.Model(&monitor).Update("upstream_type", tc.upstreamType).Error)
					}
					if tc.status != "" {
						state := states["reset"]
						state.Status = tc.status
						states["reset"] = state
					}
					if tc.missingState {
						delete(states, "reset")
					}
					require.NoError(t, model.UpdateChannelMonitorCustomActionState(t.Context(), channelID, nil, func(_ model.ChannelRatioMonitor, current map[string]model.ChannelMonitorCustomActionState) (bool, error) {
						clear(current)
						for id, state := range states {
							current[id] = state
						}
						return true, nil
					}))
					if tc.requestDay != "" {
						request.Day = tc.requestDay
					}
					if tc.requestAttempts != 0 {
						request.Attempts = tc.requestAttempts
					}
					request.LastAttempt += tc.lastAttemptDiff
					result, err := service.ResetChannelMonitorCustomActionAttempts(t.Context(), channelID, actionID, request, func() time.Time { return now })
					if tc.wantError != "" {
						require.ErrorContains(t, err, tc.wantError)
					} else {
						require.NoError(t, err)
						state := states["reset"]
						state.Attempts = 0
						states["reset"] = state
						assert.Equal(t, state, result)
					}
					saved, err := model.GetChannelMonitorCustomActionStates(channelID)
					require.NoError(t, err)
					assert.Equal(t, states, saved, "只允许清零目标规则的计数")
					unchanged, err := model.GetChannelRatioMonitor(channelID)
					require.NoError(t, err)
					assert.Equal(t, monitor.CustomUpstreamConfig, unchanged.CustomUpstreamConfig)
					assert.Equal(t, monitor.UpstreamRevision, unchanged.UpstreamRevision)
				})
			}
		})
	}
}

func TestChannelMonitorCustomActionResetAndSaveDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelMonitorCustomActionRefreshDB(t, engine)
			disableChannelMonitorSSRFProtection(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = w.Write([]byte(`{"success":true}`))
			}))
			defer server.Close()
			now := time.Now()
			zone := fmt.Sprintf("Etc/GMT%+d", now.UTC().Hour()-12)
			monitor, config, states := createChannelMonitorCustomActionResetFixture(t, db, 81, now, zone, server.URL)
			for _, limit := range []int{3, 1} {
				config.Actions[0].DailyLimit = limit
				ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/upstream", gin.H{"type": "custom", "base_url": server.URL, "custom_config": config})
				ctx.Params = gin.Params{{Key: "id", Value: "81"}}
				SaveChannelMonitorUpstreamConfig(ctx)
				var response struct {
					Success bool `json:"success"`
				}
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
				require.True(t, response.Success, recorder.Body.String())
				saved, err := model.GetChannelMonitorCustomActionStates(81)
				require.NoError(t, err)
				assert.Equal(t, states, saved, "提高或降低上限后保存均保留执行状态")
			}
			state := states["reset"]
			request := service.ChannelMonitorCustomActionResetRequest{Day: state.Day, Attempts: state.Attempts, LastAttempt: state.LastAttempt}
			ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/reset-count", request)
			ctx.Params = gin.Params{{Key: "id", Value: "81"}, {Key: "action_id", Value: "reset"}}
			ResetChannelMonitorCustomActionAttempts(ctx)
			var response struct {
				Success bool                                  `json:"success"`
				Data    model.ChannelMonitorCustomActionState `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success, recorder.Body.String())
			state.Attempts = 0
			assert.Equal(t, state, response.Data)
			assert.Zero(t, calls.Load(), "保存和重置计数都不执行上游接口")
			// Saving configuration invalidates the previous metric sample.
			require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).Where("channel_id = ?", 81).Update("upstream_balance", 5).Error)
			current, err := model.GetChannelRatioMonitor(monitor.ChannelId)
			require.NoError(t, err)
			require.NotNil(t, current.UpstreamBalance)
			succeeded, err := service.RunChannelMonitorCustomActions(t.Context(), current, "balance", *current.UpstreamBalance, "", time.Second, true)
			require.NoError(t, err)
			assert.False(t, succeeded)
			assert.Zero(t, calls.Load(), "计数清零仍须等待指标恢复到条件外")
			// Rearming does not waive the existing cooldown.
			require.NoError(t, model.UpdateChannelMonitorCustomActionState(t.Context(), 81, nil, func(_ model.ChannelRatioMonitor, current map[string]model.ChannelMonitorCustomActionState) (bool, error) {
				state.Triggered = false
				current["reset"] = state
				return true, nil
			}))
			succeeded, err = service.RunChannelMonitorCustomActions(t.Context(), current, "balance", *current.UpstreamBalance, "", time.Second, true)
			require.NoError(t, err)
			assert.False(t, succeeded)
			assert.Zero(t, calls.Load(), "计数清零不能跳过冷却时间")
		})
	}
}

func createChannelMonitorCustomActionResetFixture(t *testing.T, db *gorm.DB, channelID int, now time.Time, timezone, baseURL string) (model.ChannelRatioMonitor, service.ChannelMonitorCustomUpstreamConfig, map[string]model.ChannelMonitorCustomActionState) {
	t.Helper()
	threshold, balance := 10.0, 5.0
	action := service.ChannelMonitorCustomAction{
		ID: "reset", Name: "余额重置", Enabled: true, Metric: "balance", Operator: "lt", Threshold: &threshold,
		Timezone: timezone, StartTime: "00:00", EndTime: "23:00", DailyLimit: 1, CooldownMinutes: 60,
		Request: service.ChannelMonitorCustomRequestConfig{Method: "POST", Path: "/reset", BodyType: "none"},
	}
	other := action
	other.ID, other.Enabled = "other", false
	config := service.ChannelMonitorCustomUpstreamConfig{
		Ratio:                    service.ChannelMonitorCustomMetricConfig{Source: "http", Request: &service.ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/metrics"}, Result: &service.ChannelMonitorCustomResultConfig{ResponseType: "json", ValuePath: "ratio", Multiplier: 1}},
		Balance:                  service.ChannelMonitorCustomMetricConfig{Source: "http", Result: &service.ChannelMonitorCustomResultConfig{ResponseType: "json", ValuePath: "balance", Multiplier: 1}},
		BalanceReuseRatioRequest: true, Actions: []service.ChannelMonitorCustomAction{action, other},
	}
	raw, err := service.MarshalChannelMonitorCustomUpstreamConfig(config)
	require.NoError(t, err)
	channel := model.Channel{Id: channelID, Name: "计数测试", Key: "test-key", Group: "default", Models: "test-model", Status: common.ChannelStatusEnabled, BaseURL: &baseURL}
	require.NoError(t, db.Create(&channel).Error)
	monitor := model.ChannelRatioMonitor{ChannelId: channelID, UpstreamType: "custom", UpstreamBaseURL: baseURL, UpstreamRevision: 1, CustomUpstreamConfig: raw, UpstreamBalance: &balance, Ratio: 1}
	require.NoError(t, db.Create(&monitor).Error)
	location, err := time.LoadLocation(timezone)
	require.NoError(t, err)
	state := model.ChannelMonitorCustomActionState{Day: now.In(location).Format("2006-01-02"), Attempts: 1, LastAttempt: now.Add(-30 * time.Second).Unix(), AttemptID: "attempt-1", Triggered: true, LastValue: balance, Status: "failed", Message: "失败或结果未知，不会重试"}
	states := map[string]model.ChannelMonitorCustomActionState{"reset": state, "other": state}
	require.NoError(t, model.UpdateChannelMonitorCustomActionState(t.Context(), channelID, nil, func(_ model.ChannelRatioMonitor, current map[string]model.ChannelMonitorCustomActionState) (bool, error) {
		for id, state := range states {
			current[id] = state
		}
		return true, nil
	}))
	return monitor, config, states
}
