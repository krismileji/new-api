package controller

import (
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

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

type channelSmartScheduleRoutePrimaryRequest struct {
	Group                 string `json:"group"`
	Model                 string `json:"model"`
	DurationMinutes       *int   `json:"duration_minutes"`
	AllowStabilityDegrade *bool  `json:"allow_stability_degrade"`
}

func GetChannelMonitorSmartScheduleRoutes(c *gin.Context) {
	if err := model.InitializeChannelSmartScheduleRouteStates(); err != nil {
		common.ApiError(c, err)
		return
	}
	routingChanged, err := model.ClearExpiredChannelSmartScheduleRoutePrimaries(common.GetTimestamp())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if routingChanged {
		model.InitChannelCache()
	}
	settings := getChannelMonitorSettings()
	routes, err := model.GetChannelSmartScheduleRoutes()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	generatedAt := common.GetTimestamp()
	if !settings.SmartScheduleEnabled || len(settings.SmartScheduleGroupPolicies) == 0 {
		common.ApiSuccess(c, gin.H{
			"generated_at":                generatedAt,
			"range_minutes":               settings.SmartSchedulePerformanceMinutes,
			"enabled":                     settings.SmartScheduleEnabled,
			"routes":                      routes,
			"performance_items":           []model.ChannelMonitorRoutePerformanceMetric{},
			"stability_metrics_available": false,
			"stability_items":             []model.ChannelMonitorRouteStabilityMetric{},
		})
		return
	}
	startTimestamp := generatedAt - int64(settings.SmartSchedulePerformanceMinutes*60)
	performanceMetrics, err := model.GetChannelMonitorRoutePerformanceMetrics(
		c.Request.Context(), startTimestamp, generatedAt,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	performanceByRoute := make(map[channelSmartScheduleRouteKey]model.ChannelMonitorRoutePerformanceMetric, len(performanceMetrics))
	for _, metric := range performanceMetrics {
		key := channelSmartScheduleRouteKey{
			channelId: metric.ChannelId, group: metric.GroupName, model: metric.ModelName,
		}
		performanceByRoute[key] = metric
	}
	policyByGroup := make(map[string]channelSmartSchedulePolicy, len(settings.SmartScheduleGroupPolicies))
	probeMetricsAvailable := false
	manualMetricsAvailable := false
	for _, configured := range settings.SmartScheduleGroupPolicies {
		policy := configured.policy()
		policyByGroup[configured.Group] = policy
		probeMetricsAvailable = probeMetricsAvailable || policy.SampleMode == channelMonitorSmartScheduleSampleProbe
	}
	logStabilityAvailable := common.LogConsumeEnabled && constant.ErrorLogEnabled
	stabilityByRoute := make(map[channelSmartScheduleRouteKey]model.ChannelMonitorRouteStabilityMetric)
	if logStabilityAvailable {
		var stabilityMetrics []model.ChannelMonitorRouteStabilityMetric
		stabilityMetrics, err = model.GetChannelMonitorRouteStabilityMetrics(
			c.Request.Context(), startTimestamp, generatedAt,
		)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		for _, metric := range stabilityMetrics {
			key := channelSmartScheduleRouteKey{
				channelId: metric.ChannelId, group: metric.GroupName, model: metric.ModelName,
			}
			stabilityByRoute[key] = metric
		}
	}
	for _, route := range routes {
		policy, configured := policyByGroup[route.Group]
		if !configured || (len(policy.Models) > 0 && !slices.Contains(policy.Models, route.Model)) {
			continue
		}
		key := channelSmartScheduleRouteKey{channelId: route.ChannelId, group: route.Group, model: route.Model}
		manualMetricsAvailable = manualMetricsAvailable ||
			route.State.ManualTestMetricsSince(startTimestamp).SampleCount > 0
		metric, hasMetric := stabilityByRoute[key]
		var performance *channelSmartSchedulePerformance
		if performanceMetric, exists := performanceByRoute[key]; exists {
			performance = channelSmartScheduleSetPerformanceMetric(nil, performanceMetric)
		}
		if hasMetric {
			performance = channelSmartScheduleSetStabilityMetric(performance, metric)
		}
		windowStart := startTimestamp
		if policy.StabilityEnabled && route.State.StabilitySince > startTimestamp {
			windowStart = route.State.StabilitySince
			performance = nil
			if logStabilityAvailable {
				metric, err = model.GetChannelMonitorRouteStabilityMetric(
					c.Request.Context(), windowStart, route.ChannelId, route.Group, route.Model,
				)
				if err != nil {
					common.ApiError(c, err)
					return
				}
				performance = channelSmartScheduleSetStabilityMetric(nil, metric)
			}
			if policy.JitterEnabled {
				performanceMetric, performanceErr := model.GetChannelMonitorRoutePerformanceMetric(
					c.Request.Context(), windowStart, route.ChannelId, route.Group, route.Model,
				)
				if performanceErr != nil {
					common.ApiError(c, performanceErr)
					return
				}
				performance = channelSmartScheduleSetPerformanceMetric(performance, performanceMetric)
			}
		}
		if policy.SampleMode == channelMonitorSmartScheduleSampleProbe {
			performance = channelSmartScheduleMergeProbePerformance(performance, route.State, windowStart)
		} else {
			performance = channelSmartScheduleMergeProbePerformance(
				performance, route.State, windowStart, model.ChannelSmartScheduleSampleSourceManualTest,
			)
		}
		if performance == nil {
			continue
		}
		performance.Stability, performance.StabilitySampleCount = channelSmartScheduleStabilityScore(
			performance.StabilitySuccessCount,
			performance.StabilityFailureCount,
			performance.StabilityFinalFailureCount,
			performance.StabilityFailureDurationBuckets,
			policy,
		)
		channelSmartScheduleApplyJitterMeasurement(performance, route.State, policy)
		if performance.StabilitySampleCount <= 0 {
			delete(stabilityByRoute, key)
			continue
		}
		averageRetryFailureDurationMs := 0.0
		if performance.StabilityRetryFailureCount > 0 {
			averageRetryFailureDurationMs = performance.StabilityRetryFailureDurationTotalMs /
				float64(performance.StabilityRetryFailureCount)
		}
		metric = model.ChannelMonitorRouteStabilityMetric{
			ChannelId:                     route.ChannelId,
			GroupName:                     route.Group,
			ModelName:                     route.Model,
			SuccessCount:                  performance.StabilitySuccessCount,
			FailureCount:                  performance.StabilityFailureCount,
			FinalFailureCount:             performance.StabilityFinalFailureCount,
			RetryFailureCount:             performance.StabilityRetryFailureCount,
			SampleCount:                   performance.StabilitySampleCount,
			AverageRetryFailureDurationMs: averageRetryFailureDurationMs,
			RetryFailureDurationBuckets: append(
				[]model.ChannelMonitorFailureDurationBucket(nil),
				performance.StabilityFailureDurationBuckets...,
			),
			JitterAvailable:      performance.JitterAvailable,
			FirstTokenBaselineMs: performance.JitterBaselineMs,
			FirstTokenP50Ms:      performance.FirstTokenP50Ms,
			FirstTokenP95Ms:      performance.FirstTokenP95Ms,
			JitterThresholdMs:    performance.JitterThresholdMs,
			JitterSampleCount:    performance.JitterSampleCount,
			JitterSlowCount:      performance.JitterSlowCount,
			JitterAllowedCount:   performance.JitterAllowedCount,
			JitterPenalty:        performance.JitterPenalty,
		}
		metric.SuccessRate = float64(metric.SuccessCount) / float64(metric.SampleCount)
		if policy.StabilityEnabled {
			metric.StabilityScore = performance.Stability
		}
		stabilityByRoute[key] = metric
	}
	stabilityMetrics := make([]model.ChannelMonitorRouteStabilityMetric, 0, len(stabilityByRoute))
	for _, metric := range stabilityByRoute {
		stabilityMetrics = append(stabilityMetrics, metric)
	}
	sort.Slice(stabilityMetrics, func(i int, j int) bool {
		if stabilityMetrics[i].GroupName != stabilityMetrics[j].GroupName {
			return stabilityMetrics[i].GroupName < stabilityMetrics[j].GroupName
		}
		if stabilityMetrics[i].ModelName != stabilityMetrics[j].ModelName {
			return stabilityMetrics[i].ModelName < stabilityMetrics[j].ModelName
		}
		return stabilityMetrics[i].ChannelId < stabilityMetrics[j].ChannelId
	})
	common.ApiSuccess(c, gin.H{
		"generated_at":                generatedAt,
		"range_minutes":               settings.SmartSchedulePerformanceMinutes,
		"enabled":                     settings.SmartScheduleEnabled,
		"routes":                      routes,
		"performance_items":           performanceMetrics,
		"stability_metrics_available": logStabilityAvailable || probeMetricsAvailable || manualMetricsAvailable,
		"stability_items":             stabilityMetrics,
	})
}

func UpdateChannelMonitorSmartScheduleRoutePrimary(c *gin.Context) {
	channelId, ok := channelSmartScheduleRouteChannelId(c)
	if !ok {
		return
	}
	var request channelSmartScheduleRoutePrimaryRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	group, modelName, ok := normalizeChannelSmartScheduleRouteRequest(c, request.Group, request.Model)
	if !ok {
		return
	}
	if request.DurationMinutes == nil || *request.DurationMinutes < 0 ||
		*request.DurationMinutes > model.ChannelSmartScheduleManualPrimaryMaxMinutes {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "主渠道固定时间必须在 0 到 525600 分钟之间",
		})
		return
	}
	if err := model.InitializeChannelSmartScheduleRouteStates(); err != nil {
		common.ApiError(c, err)
		return
	}
	allowStabilityDegrade := true
	if request.AllowStabilityDegrade != nil {
		allowStabilityDegrade = *request.AllowStabilityDegrade
	}
	result, err := model.SaveChannelSmartScheduleRoutePrimary(
		channelId, group, modelName, model.ChannelSmartScheduleRoutePrimaryOptions{
			DurationMinutes:       *request.DurationMinutes,
			AllowStabilityDegrade: allowStabilityDegrade,
		},
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if result.RoutingChanged {
		model.InitChannelCache()
	}
	var taskResponse any
	settings := getChannelMonitorSettings()
	if settings.SmartScheduleEnabled && len(settings.SmartScheduleGroupPolicies) > 0 {
		if task, _, enqueueErr := service.EnqueueSystemTask(channelMonitorSmartScheduleTaskType, nil); enqueueErr == nil {
			taskResponse = task.ToResponse()
		}
	}
	recordManageAudit(c, "channel.monitor_smart_schedule_config_update", map[string]interface{}{
		"id": channelId, "group": group, "model": modelName,
		"duration_minutes":        *request.DurationMinutes,
		"allow_stability_degrade": allowStabilityDegrade,
		"manual_primary_until":    result.State.ManualPrimaryUntil,
	})
	common.ApiSuccess(c, gin.H{
		"channel_id":              channelId,
		"group":                   group,
		"model":                   modelName,
		"duration_minutes":        *request.DurationMinutes,
		"allow_stability_degrade": result.State.ManualPrimaryAllowStabilityDegrade,
		"manual_primary_until":    result.State.ManualPrimaryUntil,
		"routing_changed":         result.RoutingChanged,
		"task":                    taskResponse,
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
	if err := model.InitializeChannelSmartScheduleRouteStates(); err != nil {
		common.ApiError(c, err)
		return
	}
	state, routingChanged, err := model.SaveChannelSmartScheduleRouteConfig(channelId, group, modelName, *request.Excluded)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if routingChanged {
		model.InitChannelCache()
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
	if err := model.InitializeChannelSmartScheduleRouteStates(); err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := model.SaveChannelSmartScheduleChannelConfig(channelId, *request.Excluded)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if result.RoutingChanged {
		model.InitChannelCache()
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
