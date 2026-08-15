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
	Group                    string `json:"group"`
	Model                    string `json:"model"`
	DurationMinutes          *int   `json:"duration_minutes"`
	AllowStabilityDegrade    *bool  `json:"allow_stability_degrade"`
	ConfirmStabilityOverride bool   `json:"confirm_stability_override"`
}

func GetChannelMonitorSmartScheduleRoutes(c *gin.Context) {
	includeMetrics := true
	if rawMetrics, configured := c.GetQuery("metrics"); configured {
		parsed, err := strconv.ParseBool(rawMetrics)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "metrics 参数必须是布尔值"})
			return
		}
		includeMetrics = parsed
	}
	settings := getChannelMonitorSettings()
	var err error
	var routes []model.ChannelSmartScheduleRoute
	loadMetrics := includeMetrics && settings.SmartScheduleEnabled && len(settings.SmartScheduleGroupPolicies) > 0
	if loadMetrics {
		routes, err = model.GetChannelSmartScheduleRoutes()
	} else {
		routes, err = model.GetChannelSmartScheduleRouteSummaries()
	}
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
	responseRoutes := channelSmartScheduleRouteResponses(routes)
	if !loadMetrics {
		common.ApiSuccess(c, gin.H{
			"generated_at":                  generatedAt,
			"performance_window_minutes":    settings.SmartSchedulePerformanceWindowMinutes,
			"stability_window_minutes":      settings.SmartScheduleStabilityWindowMinutes,
			"sample_scope":                  model.ChannelSmartScheduleSampleScopeChannelModel,
			"enabled":                       settings.SmartScheduleEnabled,
			"metrics_included":              false,
			"metric_coverage":               nil,
			"routes":                        responseRoutes,
			"sample_items":                  []channelSmartScheduleSampleItem{},
			"business_performance_items":    []model.ChannelMonitorRoutePerformanceMetric{},
			"performance_metrics_available": false,
			"performance_items":             []model.ChannelMonitorRoutePerformanceMetric{},
			"stability_metrics_available":   false,
			"stability_items":               []model.ChannelMonitorRouteStabilityMetric{},
		})
		return
	}
	performanceStart := generatedAt - int64(settings.SmartSchedulePerformanceWindowMinutes*60)
	stabilityStart := generatedAt - int64(settings.SmartScheduleStabilityWindowMinutes*60)
	policyByGroup := make(map[string]channelSmartSchedulePolicy, len(settings.SmartScheduleGroupPolicies))
	selectedRoutes := make([]model.ChannelSmartScheduleRoute, 0, len(routes))
	needsPerformance := false
	needsStability := false
	for _, configured := range settings.SmartScheduleGroupPolicies {
		policy := configured.policy()
		policyByGroup[configured.Group] = policy
	}
	for _, route := range routes {
		policy, configured := policyByGroup[route.Group]
		if !configured || (len(policy.Models) > 0 && !slices.Contains(policy.Models, route.Model)) {
			continue
		}
		selectedRoutes = append(selectedRoutes, route)
		needsPerformance = needsPerformance || policy.needsPerformance()
		needsStability = needsStability || policy.StabilityEnabled || policy.AdaptiveSamplingEnabled
	}
	businessPerformanceByRoute := make([]model.ChannelMonitorRoutePerformanceMetric, 0, len(selectedRoutes))
	performanceByRoute := make([]model.ChannelMonitorRoutePerformanceMetric, 0, len(selectedRoutes))
	stabilityByRoute := make(map[channelSmartScheduleRouteKey]model.ChannelMonitorRouteStabilityMetric)
	sampleItems := make([]channelSmartScheduleSampleItem, 0, len(selectedRoutes))
	snapshots := make([]service.ChannelMonitorRealtimeSnapshot, 0, len(selectedRoutes))
	combinedSnapshot := service.ChannelMonitorRealtimeSnapshot{}
	for _, route := range selectedRoutes {
		policy := policyByGroup[route.Group]
		key := channelSmartScheduleRouteKey{channelId: route.ChannelId, group: route.Group, model: route.Model}
		view := channelSmartScheduleRealtimeRouteMetricView(
			route, policy, performanceStart, stabilityStart,
		)
		if view.performance != nil {
			performanceByRoute = append(performanceByRoute, *view.performance)
		}
		if view.businessPerformance != nil {
			businessPerformanceByRoute = append(businessPerformanceByRoute, *view.businessPerformance)
		}
		if view.stability != nil {
			stabilityByRoute[key] = *view.stability
		}
		sampleItems = append(sampleItems, view.sampleItem)
		if view.snapshot.CoverageStart > 0 {
			snapshots = append(snapshots, view.snapshot)
		}
		if view.snapshot.WindowStart > 0 {
			channelSmartScheduleMergeRealtimeSnapshot(&combinedSnapshot, view.snapshot)
		}
	}
	metricCoverage := channelSmartScheduleRealtimeMetricCoverage(generatedAt, settings, snapshots)
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
	sort.Slice(businessPerformanceByRoute, func(i int, j int) bool {
		if businessPerformanceByRoute[i].GroupName != businessPerformanceByRoute[j].GroupName {
			return businessPerformanceByRoute[i].GroupName < businessPerformanceByRoute[j].GroupName
		}
		if businessPerformanceByRoute[i].ModelName != businessPerformanceByRoute[j].ModelName {
			return businessPerformanceByRoute[i].ModelName < businessPerformanceByRoute[j].ModelName
		}
		return businessPerformanceByRoute[i].ChannelId < businessPerformanceByRoute[j].ChannelId
	})
	channelSmartScheduleApplyCurrentWindowScores(
		responseRoutes,
		selectedRoutes,
		policyByGroup,
		stabilityStart,
		generatedAt,
	)
	queueStats := service.GetChannelMonitorEventQueueStats()
	projectionStartedAt := service.GetChannelMonitorRealtimeProjectionStartedAt()
	windowIncomplete := !metricCoverage.PerformanceWindowComplete || !metricCoverage.StabilityWindowComplete
	common.ApiSuccess(c, gin.H{
		"generated_at":                  generatedAt,
		"window_start":                  combinedSnapshot.WindowStart,
		"window_end":                    combinedSnapshot.WindowEnd,
		"data_cutoff_at":                combinedSnapshot.DataCutoffAt,
		"processed_at":                  combinedSnapshot.ProcessedAt,
		"projection_started_at":         projectionStartedAt,
		"event_watermark":               combinedSnapshot.EventWatermark,
		"queue_depth":                   queueStats.QueueDepth,
		"realtime_degraded":             windowIncomplete || queueStats.DroppedEvents > 0 || queueStats.FailedEvents > 0,
		"performance_window_minutes":    settings.SmartSchedulePerformanceWindowMinutes,
		"stability_window_minutes":      settings.SmartScheduleStabilityWindowMinutes,
		"sample_scope":                  model.ChannelSmartScheduleSampleScopeChannelModel,
		"enabled":                       settings.SmartScheduleEnabled,
		"metrics_included":              true,
		"metric_coverage":               metricCoverage,
		"routes":                        responseRoutes,
		"sample_items":                  sampleItems,
		"business_performance_items":    businessPerformanceByRoute,
		"performance_metrics_available": needsPerformance,
		"performance_items":             performanceByRoute,
		"stability_metrics_available":   needsStability,
		"stability_items":               stabilityMetrics,
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

func ClearChannelMonitorSmartScheduleRouteExploration(c *gin.Context) {
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
	result, err := model.ClearChannelSmartScheduleRouteExploration(channelId, group, modelName)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if result.Cleared {
		model.InitChannelCache()
	}
	recordManageAudit(c, "channel.monitor_smart_schedule_route_exploration_clear", map[string]interface{}{
		"id": channelId, "group": group, "model": modelName,
		"previous_kind": result.PreviousKind, "cleared": result.Cleared,
		"priority": result.Priority, "weight": result.Weight,
	})
	common.ApiSuccess(c, gin.H{
		"cleared":       result.Cleared,
		"previous_kind": result.PreviousKind,
		"priority":      result.Priority,
		"weight":        result.Weight,
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
