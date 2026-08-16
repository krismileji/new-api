package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type channelSmartScheduleGroupPauseRequest struct {
	Group           string `json:"group"`
	Model           string `json:"model"`
	DurationMinutes *int   `json:"duration_minutes"`
}

func UpdateChannelMonitorSmartScheduleGroupPause(c *gin.Context) {
	channelId, ok := channelSmartScheduleRouteChannelId(c)
	if !ok {
		return
	}
	var request channelSmartScheduleGroupPauseRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	group, modelName, ok := normalizeChannelSmartScheduleRouteRequest(c, request.Group, request.Model)
	if !ok {
		return
	}
	if request.DurationMinutes == nil || *request.DurationMinutes < 0 ||
		*request.DurationMinutes > model.ChannelSmartScheduleGroupPauseMaxMinutes {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "路由流量暂停时间必须在 0 到 525600 分钟之间",
		})
		return
	}
	if err := model.InitializeChannelSmartScheduleRouteStates(); err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := model.SaveChannelSmartScheduleGroupPause(
		channelId,
		group,
		modelName,
		*request.DurationMinutes,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if result.Changed {
		model.InitChannelCache()
		_ = requestChannelSmartScheduleRun(c.Request.Context())
		recordManageAudit(c, "channel.monitor_smart_schedule_group_pause_update", map[string]interface{}{
			"id": channelId, "group": group, "model": modelName,
			"duration_minutes": *request.DurationMinutes,
			"paused_until":     result.PausedUntil,
			"affected_routes":  result.AffectedRoutes,
		})
	}
	common.ApiSuccess(c, gin.H{
		"channel_id":       channelId,
		"group":            group,
		"model":            modelName,
		"duration_minutes": *request.DurationMinutes,
		"paused_until":     result.PausedUntil,
		"affected_routes":  result.AffectedRoutes,
		"changed":          result.Changed,
	})
}
