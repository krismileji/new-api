package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type channelSmartScheduleRateLimitCooldownRequest struct {
	Group           string `json:"group"`
	Model           string `json:"model"`
	DurationSeconds *int   `json:"duration_seconds"`
}

func UpdateChannelMonitorSmartScheduleRateLimitCooldown(c *gin.Context) {
	channelId, ok := channelSmartScheduleRouteChannelId(c)
	if !ok {
		return
	}
	var request channelSmartScheduleRateLimitCooldownRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	group, modelName, ok := normalizeChannelSmartScheduleRouteRequest(c, request.Group, request.Model)
	if !ok {
		return
	}
	if request.DurationSeconds == nil || *request.DurationSeconds < 0 ||
		*request.DurationSeconds > maxChannelMonitorSmartScheduleRateLimitCooldownSeconds {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "429 暂停时间必须在 0 到 300 秒之间",
		})
		return
	}
	result, err := service.UpdateChannelRateLimitCooldown(
		c.Request.Context(), channelId, modelName, *request.DurationSeconds,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if result.Changed {
		model.InitChannelCache()
		_ = requestChannelSmartScheduleRun(c.Request.Context())
		recordManageAudit(c, "channel.monitor_smart_schedule_rate_limit_cooldown_update", map[string]interface{}{
			"id":               channelId,
			"group":            group,
			"model":            modelName,
			"duration_seconds": *request.DurationSeconds,
			"cooldown_until":   result.CooldownUntil,
		})
	}
	common.ApiSuccess(c, gin.H{
		"channel_id":       channelId,
		"group":            group,
		"model":            modelName,
		"duration_seconds": *request.DurationSeconds,
		"cooldown_until":   result.CooldownUntil,
		"changed":          result.Changed,
	})
}
