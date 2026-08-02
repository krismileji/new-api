package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type channelSmartScheduleManualRoutingRequest struct {
	Group    string `json:"group"`
	Model    string `json:"model"`
	Priority *int64 `json:"priority"`
	Weight   *uint  `json:"weight"`
}

func UpdateChannelMonitorSmartScheduleManualRouting(c *gin.Context) {
	channelId, ok := channelSmartScheduleRouteChannelId(c)
	if !ok {
		return
	}
	var request channelSmartScheduleManualRoutingRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	group, modelName, ok := normalizeChannelSmartScheduleRouteRequest(c, request.Group, request.Model)
	if !ok {
		return
	}
	if request.Priority == nil || request.Weight == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请同时提供人工优先级和权重"})
		return
	}
	if *request.Priority < 0 || *request.Priority > model.ChannelSmartScheduleManualRoutingMaxValue {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "人工优先级必须在 0 到 2147483647 之间"})
		return
	}
	if uint64(*request.Weight) > model.ChannelSmartScheduleManualRoutingMaxValue {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "人工权重必须在 0 到 2147483647 之间"})
		return
	}
	if err := model.InitializeChannelSmartScheduleRouteStates(); err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := model.SaveChannelSmartScheduleManualRouting(
		channelId,
		group,
		modelName,
		*request.Priority,
		*request.Weight,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if result.RoutingChanged {
		model.InitChannelCache()
	}
	recordManageAudit(c, "channel.monitor_smart_schedule_manual_routing_update", map[string]interface{}{
		"id": channelId, "group": group, "model": modelName,
		"priority": result.Priority, "weight": result.Weight,
		"routing_changed": result.RoutingChanged,
	})
	common.ApiSuccess(c, gin.H{
		"channel_id":      channelId,
		"group":           group,
		"model":           modelName,
		"priority":        result.Priority,
		"weight":          result.Weight,
		"routing_changed": result.RoutingChanged,
	})
}
