package controller

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChannelMonitorSmartScheduleRoutesKeepsRatioPoolMetricsWithoutActualTraffic(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy("vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 1, 90, 30)
	usePersistedChannelMonitorOptions(t, db, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})
	priority := int64(100)
	require.NoError(t, db.Create(&model.Channel{Id: 9481, Name: "traffic", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a"}).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 9481, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 100}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{ChannelId: 9481, GroupName: "vip", ModelName: "model-a", ParticipationSet: true}).Error)
	for _, testCase := range []struct {
		name        string
		metricsFail bool
	}{
		{name: "available_metrics_preserve_business_performance"},
		{name: "redis_failure_preserves_routes", metricsFail: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			c, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/schedule", nil)
			getChannelMonitorSmartScheduleRoutes(c, func(_ context.Context, _ []model.ChannelSmartScheduleRoute,
				_ map[string]channelSmartSchedulePolicy, _, _ int64,
			) (map[channelSmartScheduleRouteKey]channelSmartScheduleRealtimeRouteMetrics, error) {
				if testCase.metricsFail {
					return nil, errors.New("test redis unavailable")
				}
				return map[channelSmartScheduleRouteKey]channelSmartScheduleRealtimeRouteMetrics{
					{channelId: 9481, group: "vip", model: "model-a"}: {
						businessPerformance: &model.ChannelMonitorRoutePerformanceMetric{
							ChannelId: 9481, GroupName: "vip", ModelName: "model-a", SampleCount: 2,
						},
					},
				}, nil
			})
			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool           `json:"success"`
				Data    map[string]any `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.True(t, response.Success)
			assert.Len(t, response.Data["routes"], 1)
			assert.Contains(t, response.Data, "route_snapshot")
			assert.NotContains(t, response.Data, "actual_traffic")
			assert.NotContains(t, response.Data, "actual_traffic_scope")
			if testCase.metricsFail {
				assert.Equal(t, "实时请求统计暂不可用", response.Data["metrics_error"])
				assert.Empty(t, response.Data["business_performance_items"])
			} else {
				assert.Empty(t, response.Data["metrics_error"])
				metrics, ok := response.Data["business_performance_items"].([]any)
				require.True(t, ok)
				require.Len(t, metrics, 1)
				metric, ok := metrics[0].(map[string]any)
				require.True(t, ok)
				assert.EqualValues(t, 2, metric["sample_count"])
			}
		})
	}
}
