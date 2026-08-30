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
	DurationMinutes *int   `json:"duration_minutes"`
}

const maxChannelMonitorSmartScheduleManualRateLimitCooldownMinutes = 300

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
	if request.DurationMinutes == nil || *request.DurationMinutes < 0 ||
		*request.DurationMinutes > maxChannelMonitorSmartScheduleManualRateLimitCooldownMinutes {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "429 限制暂停时间必须在 0 到 300 分钟之间",
		})
		return
	}
	result, err := service.UpdateChannelRateLimitBypass(
		c.Request.Context(), channelId, modelName, *request.DurationMinutes*60,
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
			"duration_minutes": *request.DurationMinutes,
			"bypass_until":     result.BypassUntil,
		})
	}
	common.ApiSuccess(c, gin.H{
		"channel_id":       channelId,
		"group":            group,
		"model":            modelName,
		"duration_minutes": *request.DurationMinutes,
		"bypass_until":     result.BypassUntil,
		"changed":          result.Changed,
	})
}
