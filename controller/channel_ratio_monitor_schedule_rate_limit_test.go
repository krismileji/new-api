package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateChannelMonitorSmartScheduleRateLimitCooldownAndClear(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
	})
	common.RedisEnabled = false
	common.RDB = nil
	service.ClearChannelRateLimitBypasses()
	t.Cleanup(service.ClearChannelRateLimitBypasses)

	pauseContext, pauseRecorder := newChannelMonitorControllerContext(
		t,
		http.MethodPut,
		"/api/channel_monitor/channel/2801/schedule/route/rate-limit-cooldown",
		map[string]any{
			"group":            " vip ",
			"model":            " model-a ",
			"duration_minutes": 2,
		},
	)
	pauseContext.AddParam("id", "2801")
	UpdateChannelMonitorSmartScheduleRateLimitCooldown(pauseContext)
	require.Equal(t, http.StatusOK, pauseRecorder.Code)

	var pauseResponse struct {
		Success bool `json:"success"`
		Data    struct {
			ChannelId       int    `json:"channel_id"`
			Group           string `json:"group"`
			Model           string `json:"model"`
			DurationMinutes int    `json:"duration_minutes"`
			BypassUntil     int64  `json:"bypass_until"`
			Changed         bool   `json:"changed"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(pauseRecorder.Body.Bytes(), &pauseResponse))
	assert.True(t, pauseResponse.Success)
	assert.Equal(t, 2801, pauseResponse.Data.ChannelId)
	assert.Equal(t, "vip", pauseResponse.Data.Group)
	assert.Equal(t, "model-a", pauseResponse.Data.Model)
	assert.Equal(t, 2, pauseResponse.Data.DurationMinutes)
	assert.Greater(t, pauseResponse.Data.BypassUntil, common.GetTimestamp())
	assert.True(t, pauseResponse.Data.Changed)
	assert.Zero(t, service.ChannelRateLimitCooldownUntilMatching(2801, "model-a"))
	assert.Greater(t, service.ChannelRateLimitBypassUntilMatching(2801, "model-a"), common.GetTimestamp())

	clearContext, clearRecorder := newChannelMonitorControllerContext(
		t,
		http.MethodPut,
		"/api/channel_monitor/channel/2801/schedule/route/rate-limit-cooldown",
		map[string]any{
			"group":            "vip",
			"model":            "model-a",
			"duration_minutes": 0,
		},
	)
	clearContext.AddParam("id", "2801")
	UpdateChannelMonitorSmartScheduleRateLimitCooldown(clearContext)
	require.Equal(t, http.StatusOK, clearRecorder.Code)
	assert.Zero(t, service.ChannelRateLimitBypassUntilMatching(2801, "model-a"))
}

func TestUpdateChannelMonitorSmartScheduleRateLimitCooldownRejectsInvalidDuration(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	for _, durationMinutes := range []int{-1, maxChannelMonitorSmartScheduleManualRateLimitCooldownMinutes + 1} {
		context, recorder := newChannelMonitorControllerContext(
			t,
			http.MethodPut,
			"/api/channel_monitor/channel/2802/schedule/route/rate-limit-cooldown",
			map[string]any{"group": "vip", "model": "model-a", "duration_minutes": durationMinutes},
		)
		context.AddParam("id", "2802")
		UpdateChannelMonitorSmartScheduleRateLimitCooldown(context)
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	}
}
