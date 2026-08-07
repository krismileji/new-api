package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateChannelMonitorSmartScheduleGroupPauseAndResume(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyWeight, []string{"model-a", "model-b"}, 5, 80, 30,
			),
		),
	})
	priority := int64(80)
	weight := uint(100)
	require.NoError(t, db.Create(&model.Channel{
		Id: 2701, Name: "pause api", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a,model-b", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 2701, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 2701, Group: "vip", Model: "model-b", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)

	pauseContext, pauseRecorder := newChannelMonitorControllerContext(
		t,
		http.MethodPut,
		"/api/channel_monitor/channel/2701/schedule/group/pause",
		map[string]any{"group": " vip ", "duration_minutes": 45},
	)
	pauseContext.AddParam("id", "2701")
	UpdateChannelMonitorSmartScheduleGroupPause(pauseContext)
	require.Equal(t, http.StatusOK, pauseRecorder.Code)

	var pauseResponse struct {
		Success bool `json:"success"`
		Data    struct {
			ChannelId       int    `json:"channel_id"`
			Group           string `json:"group"`
			DurationMinutes int    `json:"duration_minutes"`
			PausedUntil     int64  `json:"paused_until"`
			AffectedRoutes  int    `json:"affected_routes"`
			Changed         bool   `json:"changed"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(pauseRecorder.Body.Bytes(), &pauseResponse))
	assert.True(t, pauseResponse.Success)
	assert.Equal(t, 2701, pauseResponse.Data.ChannelId)
	assert.Equal(t, "vip", pauseResponse.Data.Group)
	assert.Equal(t, 45, pauseResponse.Data.DurationMinutes)
	assert.Equal(t, 2, pauseResponse.Data.AffectedRoutes)
	assert.True(t, pauseResponse.Data.Changed)
	assert.Greater(t, pauseResponse.Data.PausedUntil, common.GetTimestamp())

	var stored model.ChannelSmartScheduleGroupPause
	require.NoError(t, db.Where("channel_id = ? AND group_name = ?", 2701, "vip").First(&stored).Error)
	assert.Equal(t, pauseResponse.Data.PausedUntil, stored.PausedUntil)

	resumeContext, resumeRecorder := newChannelMonitorControllerContext(
		t,
		http.MethodPut,
		"/api/channel_monitor/channel/2701/schedule/group/pause",
		map[string]any{"group": "vip", "duration_minutes": 0},
	)
	resumeContext.AddParam("id", "2701")
	UpdateChannelMonitorSmartScheduleGroupPause(resumeContext)
	require.Equal(t, http.StatusOK, resumeRecorder.Code)

	var remaining int64
	require.NoError(t, db.Model(&model.ChannelSmartScheduleGroupPause{}).
		Where("channel_id = ? AND group_name = ?", 2701, "vip").
		Count(&remaining).Error)
	assert.Zero(t, remaining)
}

func TestUpdateChannelMonitorSmartScheduleGroupPauseRejectsInvalidDuration(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	for _, durationMinutes := range []int{-1, model.ChannelSmartScheduleGroupPauseMaxMinutes + 1} {
		context, recorder := newChannelMonitorControllerContext(
			t,
			http.MethodPut,
			"/api/channel_monitor/channel/2702/schedule/group/pause",
			map[string]any{"group": "vip", "duration_minutes": durationMinutes},
		)
		context.AddParam("id", "2702")
		UpdateChannelMonitorSmartScheduleGroupPause(context)
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	}
}
