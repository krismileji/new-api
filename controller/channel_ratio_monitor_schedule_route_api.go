package controller

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type channelSmartScheduleRouteConfigRequest struct {
	Group    string `json:"group"`
	Model    string `json:"model"`
	Excluded *bool  `json:"excluded"`
}

type channelSmartScheduleRouteRequest struct {
	Group string `json:"group"`
	Model string `json:"model"`
}

func GetChannelMonitorSmartScheduleRoutes(c *gin.Context) {
	if err := initializeChannelSmartScheduleParticipation(); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InitializeChannelSmartScheduleRouteStates(); err != nil {
		common.ApiError(c, err)
		return
	}
	settings := getChannelMonitorSettings()
	routes, err := model.GetChannelSmartScheduleRoutes()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	generatedAt := common.GetTimestamp()
	startTimestamp := generatedAt - int64(settings.SmartSchedulePerformanceMinutes*60)
	performanceMetrics, err := model.GetChannelMonitorRoutePerformanceMetrics(
		c.Request.Context(), startTimestamp, generatedAt,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	stabilityAvailable := common.LogConsumeEnabled && constant.ErrorLogEnabled
	stabilityMetrics := make([]model.ChannelMonitorRouteStabilityMetric, 0)
	if stabilityAvailable {
		stabilityMetrics, err = model.GetChannelMonitorRouteStabilityMetrics(
			c.Request.Context(), startTimestamp, generatedAt,
		)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	common.ApiSuccess(c, gin.H{
		"generated_at":                generatedAt,
		"range_minutes":               settings.SmartSchedulePerformanceMinutes,
		"enabled":                     settings.SmartScheduleEnabled,
		"routes":                      routes,
		"performance_items":           performanceMetrics,
		"stability_metrics_available": stabilityAvailable,
		"stability_items":             stabilityMetrics,
	})
}

func UpdateChannelMonitorSmartScheduleRouteConfig(c *gin.Context) {
	channelId, ok := channelSmartScheduleRouteChannelId(c)
	if !ok {
		return
	}
	var request channelSmartScheduleRouteConfigRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	group, modelName, ok := normalizeChannelSmartScheduleRouteRequest(c, request.Group, request.Model)
	if !ok {
		return
	}
	if request.Excluded == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请提供要更新的调度设置"})
		return
	}
	if err := initializeChannelSmartScheduleParticipation(); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InitializeChannelSmartScheduleRouteStates(); err != nil {
		common.ApiError(c, err)
		return
	}
	state, err := model.SaveChannelSmartScheduleRouteConfig(channelId, group, modelName, *request.Excluded)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "channel.monitor_smart_schedule_route_config_update", map[string]interface{}{
		"id": channelId, "group": group, "model": modelName, "excluded": *request.Excluded,
	})
	common.ApiSuccess(c, gin.H{
		"channel_id": channelId,
		"group":      group,
		"model":      modelName,
		"excluded":   state.Excluded,
	})
}

func UpdateChannelMonitorSmartScheduleChannelConfig(c *gin.Context) {
	channelId, ok := channelSmartScheduleRouteChannelId(c)
	if !ok {
		return
	}
	if _, err := model.GetChannelById(channelId, false); err != nil {
		common.ApiError(c, err)
		return
	}
	var request channelSmartScheduleRouteConfigRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	if request.Excluded == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请提供要更新的调度设置"})
		return
	}
	if err := initializeChannelSmartScheduleParticipation(); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InitializeChannelSmartScheduleRouteStates(); err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := model.SaveChannelSmartScheduleChannelConfig(channelId, *request.Excluded)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "channel.monitor_smart_schedule_channel_config_update", map[string]interface{}{
		"id": channelId, "excluded": *request.Excluded,
		"total": result.Total, "updated": result.Updated,
	})
	common.ApiSuccess(c, result)
}

func ClearChannelMonitorSmartScheduleRouteStability(c *gin.Context) {
	channelId, ok := channelSmartScheduleRouteChannelId(c)
	if !ok {
		return
	}
	var request channelSmartScheduleRouteRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	group, modelName, ok := normalizeChannelSmartScheduleRouteRequest(c, request.Group, request.Model)
	if !ok {
		return
	}
	if err := initializeChannelSmartScheduleParticipation(); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InitializeChannelSmartScheduleRouteStates(); err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := model.ClearChannelSmartScheduleRouteStability(
		channelId, group, modelName,
		channelMonitorSmartScheduleBaselinePriority,
		channelMonitorSmartScheduleMinWeight,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if result.Cleared {
		model.InitChannelCache()
	}
	recordManageAudit(c, "channel.monitor_smart_schedule_route_stability_clear", map[string]interface{}{
		"id": channelId, "group": group, "model": modelName,
		"previous_state": result.PreviousState, "cleared": result.Cleared,
		"priority": result.Priority, "weight": result.Weight,
	})
	common.ApiSuccess(c, gin.H{
		"cleared":        result.Cleared,
		"previous_state": result.PreviousState,
		"priority":       result.Priority,
		"weight":         result.Weight,
	})
}

func channelSmartScheduleRouteChannelId(c *gin.Context) (int, bool) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		common.ApiErrorMsg(c, "无效的渠道 ID")
		return 0, false
	}
	return channelId, true
}

func normalizeChannelSmartScheduleRouteRequest(c *gin.Context, group string, modelName string) (string, string, bool) {
	group = strings.TrimSpace(group)
	modelName = strings.TrimSpace(modelName)
	if group == "" || utf8.RuneCountInString(group) > maxChannelMonitorSmartScheduleGroupLength {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "分组名称无效"})
		return "", "", false
	}
	if modelName == "" || utf8.RuneCountInString(modelName) > maxChannelMonitorSmartScheduleModelLength {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "模型名称无效"})
		return "", "", false
	}
	return group, modelName, true
}
