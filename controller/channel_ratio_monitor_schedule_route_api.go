package controller

import (
	"errors"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

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
	Group                    string `json:"group"`
	Model                    string `json:"model"`
	DurationMinutes          *int   `json:"duration_minutes"`
	AllowStabilityDegrade    *bool  `json:"allow_stability_degrade"`
	ConfirmStabilityOverride bool   `json:"confirm_stability_override"`
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
	economicSnapshot, err := model.GetChannelSmartScheduleEconomicSnapshot()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	monitorByChannel := make(map[int]model.ChannelRatioMonitor, len(economicSnapshot.Monitors))
	for _, monitor := range economicSnapshot.Monitors {
		monitorByChannel[monitor.ChannelId] = monitor
	}
	groupRatios := economicSnapshot.GroupRatios
	for index := range routes {
		monitor, monitorAvailable := monitorByChannel[routes[index].ChannelId]
		groupRatio, groupRatioAvailable := groupRatios[routes[index].Group]
		economics := channelSmartScheduleClassifyEconomics(
			monitor,
			monitorAvailable,
			groupRatio,
			groupRatioAvailable,
		)
		routes[index].CostRatio = economics.CostRatio
		routes[index].GroupRatio = economics.GroupRatio
		routes[index].GrossMargin = economics.GrossMargin
		routes[index].EconomicRole = economics.EconomicRole
	}
	requestedAt := time.Now()
	generatedAt := requestedAt.Unix()
	if !settings.SmartScheduleEnabled || len(settings.SmartScheduleGroupPolicies) == 0 {
		common.ApiSuccess(c, gin.H{
			"generated_at":                generatedAt,
			"performance_window_minutes":  settings.SmartSchedulePerformanceWindowMinutes,
			"stability_window_minutes":    settings.SmartScheduleStabilityWindowMinutes,
			"sample_scope":                model.ChannelSmartScheduleSampleScopeChannelModel,
			"enabled":                     settings.SmartScheduleEnabled,
			"routes":                      routes,
			"performance_items":           []model.ChannelMonitorRoutePerformanceMetric{},
			"stability_metrics_available": false,
			"stability_items":             []model.ChannelMonitorRouteStabilityMetric{},
		})
		return
	}
	if err := service.EnsureChannelMonitorAggregationFresh(c.Request.Context(), requestedAt); err != nil {
		common.ApiError(c, err)
		return
	}
	performanceStart := generatedAt - int64(settings.SmartSchedulePerformanceWindowMinutes*60)
	stabilityStart := generatedAt - int64(settings.SmartScheduleStabilityWindowMinutes*60)
	performanceMetrics, err := model.GetChannelMonitorRoutePerformanceMetrics(
		c.Request.Context(), performanceStart, generatedAt,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	performanceByModel := make(map[channelSmartScheduleModelKey]model.ChannelMonitorRoutePerformanceMetric, len(performanceMetrics))
	for _, metric := range performanceMetrics {
		key := channelSmartScheduleModelKey{
			channelId: metric.ChannelId,
			model:     ratio_setting.FormatMatchingModelName(metric.ModelName),
		}
		performanceByModel[key] = metric
	}
	performanceByRoute := make([]model.ChannelMonitorRoutePerformanceMetric, 0, len(routes))
	policyByGroup := make(map[string]channelSmartSchedulePolicy, len(settings.SmartScheduleGroupPolicies))
	probeMetricsAvailable := false
	sharedMetricsAvailable := false
	for _, configured := range settings.SmartScheduleGroupPolicies {
		policy := configured.policy()
		policyByGroup[configured.Group] = policy
		probeMetricsAvailable = probeMetricsAvailable ||
			policy.SampleMode == channelMonitorSmartScheduleSampleProbe ||
			(policy.StabilityEnabled && policy.DegradedProbeEnabled)
	}
	logStabilityAvailable := common.LogConsumeEnabled && constant.ErrorLogEnabled
	stabilityByModel := make(map[channelSmartScheduleModelKey]model.ChannelMonitorRouteStabilityMetric)
	jitterByModel := make(map[channelSmartScheduleModelKey]model.ChannelMonitorRoutePerformanceMetric)
	stabilityByRoute := make(map[channelSmartScheduleRouteKey]model.ChannelMonitorRouteStabilityMetric)
	if logStabilityAvailable {
		var stabilityMetrics []model.ChannelMonitorRouteStabilityMetric
		stabilityMetrics, err = model.GetChannelMonitorRouteStabilityMetrics(
			c.Request.Context(), stabilityStart, generatedAt,
		)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		for _, metric := range stabilityMetrics {
			key := channelSmartScheduleModelKey{
				channelId: metric.ChannelId,
				model:     ratio_setting.FormatMatchingModelName(metric.ModelName),
			}
			stabilityByModel[key] = metric
		}
		jitterMetrics, jitterErr := model.GetChannelMonitorRoutePerformanceMetrics(
			c.Request.Context(), stabilityStart, generatedAt,
		)
		if jitterErr != nil {
			common.ApiError(c, jitterErr)
			return
		}
		for _, metric := range jitterMetrics {
			key := channelSmartScheduleModelKey{
				channelId: metric.ChannelId,
				model:     ratio_setting.FormatMatchingModelName(metric.ModelName),
			}
			jitterByModel[key] = metric
		}
	}
	for _, route := range routes {
		policy, configured := policyByGroup[route.Group]
		if !configured || (len(policy.Models) > 0 && !slices.Contains(policy.Models, route.Model)) {
			continue
		}
		normalizedModelName := ratio_setting.FormatMatchingModelName(route.Model)
		modelKey := channelSmartScheduleModelKey{channelId: route.ChannelId, model: normalizedModelName}
		key := channelSmartScheduleRouteKey{channelId: route.ChannelId, group: route.Group, model: route.Model}
		if metric, exists := performanceByModel[modelKey]; exists {
			metric.GroupName = route.Group
			metric.ModelName = route.Model
			performanceByRoute = append(performanceByRoute, metric)
		}
		sharedMetricsAvailable = sharedMetricsAvailable ||
			route.SharedSamples.MetricsSince(stabilityStart).SampleCount > 0
		metric, hasMetric := stabilityByModel[modelKey]
		var performance *channelSmartSchedulePerformance
		if hasMetric {
			performance = channelSmartScheduleSetStabilityMetric(performance, metric)
		}
		if jitterMetric, exists := jitterByModel[modelKey]; exists {
			performance = channelSmartScheduleSetPerformanceMetric(performance, jitterMetric)
		}
		windowStart := stabilityStart
		probingWindowReset := policy.StabilityEnabled &&
			route.State.StabilityState == model.ChannelSmartScheduleStabilityProbing &&
			route.State.StabilitySince > stabilityStart
		if probingWindowReset {
			windowStart = route.State.StabilitySince
			performance = nil
			if logStabilityAvailable {
				metric, err = model.GetChannelMonitorRouteStabilityMetric(
					c.Request.Context(), windowStart, route.ChannelId, normalizedModelName,
				)
				if err != nil {
					common.ApiError(c, err)
					return
				}
				performance = channelSmartScheduleSetStabilityMetric(nil, metric)
			}
			if policy.JitterEnabled {
				performanceMetric, performanceErr := model.GetChannelMonitorRoutePerformanceMetric(
					c.Request.Context(), windowStart, route.ChannelId, normalizedModelName,
				)
				if performanceErr != nil {
					common.ApiError(c, performanceErr)
					return
				}
				performance = channelSmartScheduleSetPerformanceMetric(performance, performanceMetric)
			}
		}
		performance = channelSmartScheduleMergeSharedSamplePerformance(
			performance, route.SharedSamples, windowStart,
		)
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
		channelSmartScheduleApplyJitterMeasurement(performance, policy)
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
			GroupCount:                    performance.SampleGroupCount,
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
			JitterAvailable:    performance.JitterAvailable,
			FirstTokenP50Ms:    performance.FirstTokenP50Ms,
			FirstTokenP95Ms:    performance.FirstTokenP95Ms,
			JitterThresholdMs:  performance.JitterThresholdMs,
			JitterSampleCount:  performance.JitterSampleCount,
			JitterSlowCount:    performance.JitterSlowCount,
			JitterAllowedCount: performance.JitterAllowedCount,
			JitterPenalty:      performance.JitterPenalty,
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
	sort.Slice(performanceByRoute, func(i int, j int) bool {
		if performanceByRoute[i].GroupName != performanceByRoute[j].GroupName {
			return performanceByRoute[i].GroupName < performanceByRoute[j].GroupName
		}
		if performanceByRoute[i].ModelName != performanceByRoute[j].ModelName {
			return performanceByRoute[i].ModelName < performanceByRoute[j].ModelName
		}
		return performanceByRoute[i].ChannelId < performanceByRoute[j].ChannelId
	})
	common.ApiSuccess(c, gin.H{
		"generated_at":                generatedAt,
		"performance_window_minutes":  settings.SmartSchedulePerformanceWindowMinutes,
		"stability_window_minutes":    settings.SmartScheduleStabilityWindowMinutes,
		"sample_scope":                model.ChannelSmartScheduleSampleScopeChannelModel,
		"enabled":                     settings.SmartScheduleEnabled,
		"routes":                      routes,
		"performance_items":           performanceByRoute,
		"stability_metrics_available": logStabilityAvailable || probeMetricsAvailable || sharedMetricsAvailable,
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
			DurationMinutes:           *request.DurationMinutes,
			AllowStabilityDegrade:     allowStabilityDegrade,
			ConfirmStabilityOverride:  request.ConfirmStabilityOverride,
			StabilityFallbackPriority: channelMonitorSmartScheduleBaselinePriority,
			StabilityFallbackWeight:   channelMonitorSmartScheduleMinWeight,
		},
	)
	if err != nil {
		if errors.Is(err, model.ErrChannelSmartScheduleRouteStabilityProtected) {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"code":    "smart_schedule_route_stability_confirmation_required",
				"message": "该分组和模型路由处于稳定性保护状态，确认解除保护后可固定为主渠道",
				"data":    nil,
			})
			return
		}
		common.ApiError(c, err)
		return
	}
	if result.StabilityProtectionCleared {
		clearChannelSmartScheduleRuntimeHealth(channelId, modelName, result.ObservationSince)
	}
	model.InitChannelCache()
	var taskResponse any
	settings := getChannelMonitorSettings()
	if settings.SmartScheduleEnabled && len(settings.SmartScheduleGroupPolicies) > 0 {
		if task, _, enqueueErr := service.EnqueueRequiredSystemTask(channelMonitorSmartScheduleTaskType, nil); enqueueErr == nil {
			taskResponse = task.ToResponse()
		}
	}
	recordManageAudit(c, "channel.monitor_smart_schedule_config_update", map[string]interface{}{
		"id": channelId, "group": group, "model": modelName,
		"duration_minutes":             *request.DurationMinutes,
		"allow_stability_degrade":      allowStabilityDegrade,
		"confirm_stability_override":   request.ConfirmStabilityOverride,
		"stability_protection_cleared": result.StabilityProtectionCleared,
		"manual_primary_until":         result.State.ManualPrimaryUntil,
	})
	common.ApiSuccess(c, gin.H{
		"channel_id":                   channelId,
		"group":                        group,
		"model":                        modelName,
		"duration_minutes":             *request.DurationMinutes,
		"allow_stability_degrade":      result.State.ManualPrimaryAllowStabilityDegrade,
		"manual_primary_until":         result.State.ManualPrimaryUntil,
		"stability_protection_cleared": result.StabilityProtectionCleared,
		"routing_changed":              result.RoutingChanged,
		"task":                         taskResponse,
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
	model.InitChannelCache()
	if routingChanged {
		_ = requestChannelSmartScheduleRun(c.Request.Context())
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
	model.InitChannelCache()
	if result.Updated > 0 {
		_ = requestChannelSmartScheduleRun(c.Request.Context())
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
		clearChannelSmartScheduleRuntimeHealth(channelId, modelName, result.ObservationSince)
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
