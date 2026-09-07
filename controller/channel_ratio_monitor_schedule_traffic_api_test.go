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

func TestGetChannelMonitorSmartScheduleTrafficLoadsRatioOnlyPoolAndKeepsRoutesOnRedisFailure(t *testing.T) {
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
	for _, fail := range []bool{false, true} {
		c, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/schedule", nil)
		called := false
		getChannelMonitorSmartScheduleRoutes(c, func(_ context.Context, routes []model.ChannelSmartScheduleRoute,
			_ map[string]channelSmartSchedulePolicy, start, end int64,
		) (map[channelSmartScheduleRouteKey]channelSmartScheduleRealtimeRouteMetrics, error) {
			called = true
			require.Len(t, routes, 1)
			if fail {
				return nil, errors.New("test redis unavailable")
			}
			return map[channelSmartScheduleRouteKey]channelSmartScheduleRealtimeRouteMetrics{
				{channelId: 9481, group: "vip", model: "model-a"}: {
					traffic: &channelSmartScheduleTraffic{ChannelID: 9481, Group: "vip", Model: "model-a", WindowStart: start, WindowEnd: end,
						Available: true, Complete: true, AttemptCount: 2, FinalSuccessCount: 1},
				},
			}, nil
		})
		require.True(t, called)
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				Routes       []channelSmartScheduleRouteResponse `json:"routes"`
				Traffic      []channelSmartScheduleTraffic       `json:"actual_traffic"`
				MetricsError string                              `json:"metrics_error"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		assert.True(t, response.Success)
		assert.Len(t, response.Data.Routes, 1)
		if fail {
			assert.NotEmpty(t, response.Data.MetricsError)
			assert.Empty(t, response.Data.Traffic)
		} else {
			require.Len(t, response.Data.Traffic, 1)
			assert.EqualValues(t, 2, response.Data.Traffic[0].AttemptCount)
			assert.EqualValues(t, 1, response.Data.Traffic[0].FinalSuccessCount)
		}
	}
}
