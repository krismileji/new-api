package controller

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type channelSmartScheduleRouteKey struct {
	channelId int
	group     string
	model     string
}

type channelSmartScheduleRoutePoolKey struct {
	group string
	model string
}

type channelSmartScheduleModelKey struct {
	channelId int
	model     string
}

type channelSmartScheduleRouteDirectAction struct {
	key                    channelSmartScheduleRouteKey
	currentPriority        int64
	currentWeight          uint
	targetPriority         int64
	targetWeight           uint
	status                 string
	message                string
	stability              *model.ChannelSmartScheduleStabilityUpdate
	runtimeProtectionClear bool
	routingSnapshot        *model.ChannelSmartScheduleRoutingSnapshotUpdate
	reapplyManualPrimary   bool
}

func channelSmartScheduleSetPerformanceMetric(
	performance *channelSmartSchedulePerformance,
	metric model.ChannelMonitorRoutePerformanceMetric,
) *channelSmartSchedulePerformance {
	if performance == nil {
		performance = &channelSmartSchedulePerformance{}
	}
	performance.SampleGroupCount = max(performance.SampleGroupCount, metric.GroupCount)
	performance.FirstTokenSampleCount = metric.FirstTokenSampleCount
	performance.FirstTokenDurationSampleCount = metric.FirstTokenDurationSampleCount
	performance.TPSSampleCount = metric.TPSSampleCount
	performance.AverageFirstTokenMs = metric.AverageFirstTokenMs
	performance.WinsorizedAverageFirstTokenMs = metric.WinsorizedAverageFirstTokenMs
	performance.FirstTokenP50Ms = metric.FirstTokenP50Ms
	performance.FirstTokenP95Ms = metric.FirstTokenP95Ms
	performance.FirstTokenDurationBuckets = append(
		[]model.ChannelMonitorDurationBucket(nil), metric.FirstTokenDurationBuckets...,
	)
	performance.AverageTPS = metric.AverageTPS
	return performance
}

func channelSmartScheduleStabilityDescription(performance *channelSmartSchedulePerformance) string {
	if performance == nil || performance.Stability == nil || performance.StabilitySampleCount <= 0 {
		return "稳定性统计不可用"
	}
	successRate := 0.0
	if performance.StabilitySampleCount > 0 {
		successRate = float64(performance.StabilitySuccessCount) / float64(performance.StabilitySampleCount) * 100
	}
	averageRetryFailureMs := 0.0
	if performance.StabilityRetryFailureCount > 0 {
		averageRetryFailureMs = performance.StabilityRetryFailureDurationTotalMs /
			float64(performance.StabilityRetryFailureCount)
	}
	description := fmt.Sprintf(
		"稳定性得分 %.1f%%，原始成功率 %.1f%%，最终失败 %d 次、重试失败 %d 次（平均 %.0f ms）",
		*performance.Stability*100,
		successRate,
		performance.StabilityFinalFailureCount,
		performance.StabilityRetryFailureCount,
		averageRetryFailureMs,
	)
	if performance.JitterAvailable && performance.JitterBaselineMs != nil &&
		performance.JitterThresholdMs != nil {
		p50Ms := 0.0
		if performance.FirstTokenP50Ms != nil {
			p50Ms = *performance.FirstTokenP50Ms
		}
		p95Ms := 0.0
		if performance.FirstTokenP95Ms != nil {
			p95Ms = *performance.FirstTokenP95Ms
		}
		description += fmt.Sprintf(
			"；首字抖动基准 %.0f ms、P50 %.0f ms、P95 %.0f ms、慢阈值 %.0f ms、慢成功 %d/%d（容忍 %d，处罚 %.0f）",
			*performance.JitterBaselineMs,
			p50Ms,
			p95Ms,
			*performance.JitterThresholdMs,
			performance.JitterSlowCount,
			performance.JitterSampleCount,
			performance.JitterAllowedCount,
			performance.JitterPenalty,
		)
	}
	return description
}

func channelSmartScheduleSetStabilityMetric(
	performance *channelSmartSchedulePerformance,
	metric model.ChannelMonitorRouteStabilityMetric,
) *channelSmartSchedulePerformance {
	if performance == nil {
		performance = &channelSmartSchedulePerformance{}
	}
	performance.SampleGroupCount = max(performance.SampleGroupCount, metric.GroupCount)
	performance.Stability = nil
	performance.StabilitySampleCount = 0
	performance.StabilitySuccessCount = metric.SuccessCount
	performance.StabilityFailureCount = metric.FailureCount
	performance.StabilityFinalFailureCount = metric.FinalFailureCount
	performance.StabilityRetryFailureCount = metric.RetryFailureCount
	performance.StabilityRetryFailureDurationTotalMs = metric.AverageRetryFailureDurationMs * float64(metric.RetryFailureCount)
	performance.StabilityFailureDurationBuckets = append(
		[]model.ChannelMonitorFailureDurationBucket(nil), metric.RetryFailureDurationBuckets...,
	)
	return performance
}

func channelSmartScheduleClonePerformance(
	performance *channelSmartSchedulePerformance,
) *channelSmartSchedulePerformance {
	if performance == nil {
		return nil
	}
	cloned := *performance
	cloned.FirstTokenDurationBuckets = append(
		[]model.ChannelMonitorDurationBucket(nil), performance.FirstTokenDurationBuckets...,
	)
	cloned.StabilityFailureDurationBuckets = append(
		[]model.ChannelMonitorFailureDurationBucket(nil), performance.StabilityFailureDurationBuckets...,
	)
	return &cloned
}

func channelSmartScheduleCombineWindowPerformance(
	business *channelSmartSchedulePerformance,
	stability *channelSmartSchedulePerformance,
) *channelSmartSchedulePerformance {
	if business == nil && stability == nil {
		return nil
	}
	combined := channelSmartScheduleClonePerformance(business)
	if combined == nil {
		combined = &channelSmartSchedulePerformance{}
	}
	if stability == nil {
		return combined
	}
	combined.SampleGroupCount = max(combined.SampleGroupCount, stability.SampleGroupCount)
	combined.StabilitySampleCount = stability.StabilitySampleCount
	combined.Stability = stability.Stability
	combined.StabilitySuccessCount = stability.StabilitySuccessCount
	combined.StabilityFailureCount = stability.StabilityFailureCount
	combined.StabilityFinalFailureCount = stability.StabilityFinalFailureCount
	combined.StabilityRetryFailureCount = stability.StabilityRetryFailureCount
	combined.StabilityRetryFailureDurationTotalMs = stability.StabilityRetryFailureDurationTotalMs
	combined.StabilityFailureDurationBuckets = append(
		[]model.ChannelMonitorFailureDurationBucket(nil),
		stability.StabilityFailureDurationBuckets...,
	)
	combined.FirstTokenDurationSampleCount = stability.FirstTokenDurationSampleCount
	combined.FirstTokenP50Ms = stability.FirstTokenP50Ms
	combined.FirstTokenP95Ms = stability.FirstTokenP95Ms
	combined.FirstTokenDurationBuckets = append(
		[]model.ChannelMonitorDurationBucket(nil),
		stability.FirstTokenDurationBuckets...,
	)
	return combined
}

func channelSmartScheduleApplyJitterMeasurement(
	performance *channelSmartSchedulePerformance,
	state model.ChannelSmartScheduleRouteState,
	policy channelSmartSchedulePolicy,
) {
	if performance == nil || state.JitterBaselineFirstTokenMs == nil ||
		performance.FirstTokenDurationSampleCount < int64(policy.MinSamples) {
		return
	}
	measurement := channelSmartScheduleMeasureJitter(
		performance.FirstTokenDurationBuckets,
		*state.JitterBaselineFirstTokenMs,
		policy.MinSamples,
		policy,
	)
	performance.JitterAvailable = measurement.Available
	baselineMs := measurement.BaselineMs
	performance.JitterBaselineMs = &baselineMs
	if measurement.ThresholdMs > 0 {
		thresholdMs := measurement.ThresholdMs
		performance.JitterThresholdMs = &thresholdMs
	}
	performance.JitterSampleCount = measurement.SampleCount
	performance.JitterSlowCount = measurement.SlowCount
	performance.JitterAllowedCount = measurement.AllowedCount
	performance.JitterPenalty = measurement.Penalty
	if measurement.Available {
		performance.Stability = channelSmartScheduleApplyJitterPenalty(
			performance.Stability,
			performance.StabilitySampleCount,
			measurement.Penalty,
		)
	}
}

func channelSmartScheduleJitterBaselineUpdate(
	performance *channelSmartSchedulePerformance,
	state model.ChannelSmartScheduleRouteState,
	policy channelSmartSchedulePolicy,
	now int64,
) *model.ChannelSmartScheduleJitterUpdate {
	if performance == nil || !policy.StabilityEnabled || !policy.JitterEnabled ||
		state.StabilityState != "" || performance.FirstTokenP50Ms == nil ||
		performance.FirstTokenDurationSampleCount < int64(policy.MinSamples) || performance.Stability == nil ||
		*performance.Stability < policy.RecoveryStabilityScore/channelMonitorScorePercentageTotal ||
		(performance.JitterAvailable && performance.JitterPenalty > 0) {
		return nil
	}
	baselineMs, changed := channelSmartScheduleLearnJitterBaseline(
		state.JitterBaselineFirstTokenMs,
		state.JitterBaselineUpdatedAt,
		*performance.FirstTokenP50Ms,
		now,
		policy.JitterBaselineMinutes,
	)
	if !changed {
		return nil
	}
	return &model.ChannelSmartScheduleJitterUpdate{
		BaselineFirstTokenMs: &baselineMs,
		BaselineUpdatedAt:    now,
	}
}

func runChannelSmartScheduleByRouteOnce(
	ctx context.Context,
	reportProgress func(processed, total int),
	forceReset bool,
	settings channelMonitorSettings,
	result channelSmartScheduleTaskResult,
) (channelSmartScheduleTaskResult, error) {
	if err := model.InitializeChannelSmartScheduleRouteStates(); err != nil {
		return result, err
	}
	manualRoutingChanged, err := model.ClearExpiredChannelSmartScheduleRoutePrimaries(common.GetTimestamp())
	if err != nil {
		return result, err
	}
	cacheDirty := manualRoutingChanged
	defer func() {
		if cacheDirty {
			model.InitChannelCache()
		}
	}()
	controlRevision, err := model.GetChannelSmartScheduleControlRevision()
	if err != nil {
		return result, err
	}
	routes, err := model.GetChannelSmartScheduleRoutes()
	if err != nil {
		return result, err
	}
	policyByGroup := make(map[string]channelSmartSchedulePolicy, len(settings.SmartScheduleGroupPolicies))
	for _, groupPolicy := range settings.SmartScheduleGroupPolicies {
		policyByGroup[groupPolicy.Group] = groupPolicy.policy()
	}
	selectedRoutes := make([]model.ChannelSmartScheduleRoute, 0, len(routes))
	for _, route := range routes {
		policy, configured := policyByGroup[route.Group]
		if !configured {
			continue
		}
		if len(policy.Models) > 0 && !slices.Contains(policy.Models, route.Model) {
			continue
		}
		selectedRoutes = append(selectedRoutes, route)
	}
	result.Total = len(selectedRoutes)
	if result.Total == 0 {
		reportProgress(0, 0)
		return result, nil
	}

	monitors, err := model.GetChannelRatioMonitors()
	if err != nil {
		return result, err
	}
	monitorByChannel := make(map[int]model.ChannelRatioMonitor, len(monitors))
	for _, monitor := range monitors {
		monitorByChannel[monitor.ChannelId] = monitor
	}

	needsPerformance := false
	needsJitterPerformance := false
	needsStability := false
	for _, route := range selectedRoutes {
		policy := policyByGroup[route.Group]
		needsPerformance = needsPerformance || policy.needsPerformance()
		needsJitterPerformance = needsJitterPerformance ||
			(policy.StabilityEnabled && policy.JitterEnabled)
		needsStability = needsStability || policy.StabilityEnabled
	}
	now := common.GetTimestamp()
	performanceStart := now - int64(settings.SmartSchedulePerformanceWindowMinutes*60)
	stabilityStart := now - int64(settings.SmartScheduleStabilityWindowMinutes*60)
	performanceByModel := make(map[channelSmartScheduleModelKey]*channelSmartSchedulePerformance)
	if needsPerformance {
		metrics, metricErr := model.GetChannelMonitorRoutePerformanceMetrics(ctx, performanceStart, now)
		if metricErr != nil {
			return result, metricErr
		}
		for _, metric := range metrics {
			key := channelSmartScheduleModelKey{
				channelId: metric.ChannelId,
				model:     ratio_setting.FormatMatchingModelName(metric.ModelName),
			}
			performanceByModel[key] = channelSmartScheduleSetPerformanceMetric(nil, metric)
		}
	}
	stabilityPerformanceByModel := make(map[channelSmartScheduleModelKey]*channelSmartSchedulePerformance)
	if needsJitterPerformance {
		metrics, metricErr := model.GetChannelMonitorRoutePerformanceMetrics(ctx, stabilityStart, now)
		if metricErr != nil {
			return result, metricErr
		}
		for _, metric := range metrics {
			key := channelSmartScheduleModelKey{
				channelId: metric.ChannelId,
				model:     ratio_setting.FormatMatchingModelName(metric.ModelName),
			}
			stabilityPerformanceByModel[key] = channelSmartScheduleSetPerformanceMetric(nil, metric)
		}
	}
	logStabilityAvailable := common.LogConsumeEnabled && constant.ErrorLogEnabled
	if needsStability && logStabilityAvailable {
		metrics, metricErr := model.GetChannelMonitorRouteStabilityMetrics(ctx, stabilityStart, now)
		if metricErr != nil {
			return result, metricErr
		}
		for _, metric := range metrics {
			key := channelSmartScheduleModelKey{
				channelId: metric.ChannelId,
				model:     ratio_setting.FormatMatchingModelName(metric.ModelName),
			}
			stabilityPerformanceByModel[key] = channelSmartScheduleSetStabilityMetric(
				stabilityPerformanceByModel[key], metric,
			)
		}
	}

	poolCandidates := make(map[channelSmartScheduleRoutePoolKey][]channelSmartScheduleCandidate)
	poolRoutes := make(map[channelSmartScheduleRoutePoolKey][]model.ChannelSmartScheduleRoute)
	routeKeyByPoolChannel := make(map[channelSmartScheduleRoutePoolKey]map[int]channelSmartScheduleRouteKey)
	directActions := make([]channelSmartScheduleRouteDirectAction, 0)
	statusUpdates := make([]model.ChannelSmartScheduleRouteResultUpdate, 0, len(selectedRoutes))
	stabilityUpdates := make(map[channelSmartScheduleRouteKey]*model.ChannelSmartScheduleStabilityUpdate)
	jitterUpdates := make(map[channelSmartScheduleRouteKey]*model.ChannelSmartScheduleJitterUpdate)
	scoreDetailsByRoute := make(map[channelSmartScheduleRouteKey]*model.ChannelSmartScheduleScoreDetails, len(selectedRoutes))
	routeByKey := make(map[channelSmartScheduleRouteKey]model.ChannelSmartScheduleRoute, len(selectedRoutes))
	for _, route := range selectedRoutes {
		policy := policyByGroup[route.Group]
		poolKey := channelSmartScheduleRoutePoolKey{group: route.Group, model: route.Model}
		poolRoutes[poolKey] = append(poolRoutes[poolKey], route)
		manualPrimary := route.State.ManualPrimaryUntil > now
		degradeStabilityScore := policy.DegradeStabilityScore / 100
		recoveryStabilityScore := policy.RecoveryStabilityScore / 100
		key := channelSmartScheduleRouteKey{channelId: route.ChannelId, group: route.Group, model: route.Model}
		modelKey := channelSmartScheduleModelKey{
			channelId: route.ChannelId,
			model:     ratio_setting.FormatMatchingModelName(route.Model),
		}
		routeByKey[key] = route
		scoreDetailsByRoute[key] = channelSmartScheduleNewScoreDetails(
			channelSmartScheduleCandidate{ChannelId: route.ChannelId, ManualPrimary: manualPrimary},
			policy.Strategy, policy.StabilityEnabled, policy.ApplyMode, policy.MinSamples,
			forceReset, policy.Scoring,
		)
		routeStabilityAvailable := logStabilityAvailable ||
			policy.SampleMode == channelMonitorSmartScheduleSampleProbe ||
			route.SharedSamples.MetricsSince(stabilityStart).SampleCount > 0
		currentPriority := route.Priority
		currentWeight := route.Weight
		if route.State.TemporaryTrafficKind != "" && route.State.BaseRank > 0 {
			currentPriority = route.State.BasePriority
			currentWeight = route.State.BaseWeight
		}
		if forceReset && route.ChannelStatus == common.ChannelStatusEnabled && route.Enabled && route.State.Participates() && route.State.StabilityState == "" {
			currentPriority = channelMonitorSmartScheduleBaselinePriority
			currentWeight = channelMonitorSmartScheduleMinWeight
		}
		if route.ChannelStatus != common.ChannelStatusEnabled || !route.Enabled || !route.State.Participates() {
			reason := "该分组和模型路由未参与智能调度"
			if route.ChannelStatus != common.ChannelStatusEnabled {
				reason = "渠道已禁用，未参与本次调度"
			} else if !route.Enabled {
				reason = "该分组和模型路由已禁用，未参与本次调度"
			}
			result.Skipped++
			channelSmartScheduleSetAdjustmentReason(scoreDetailsByRoute[key], reason)
			result.recordAdjustment(channelSmartScheduleTaskAdjustment{
				ChannelId: route.ChannelId, ChannelName: route.ChannelName,
				Group: route.Group, Model: route.Model,
				Action:      channelSmartScheduleAdjustmentSkipped,
				OldPriority: route.Priority, NewPriority: route.Priority,
				OldWeight: route.Weight, NewWeight: route.Weight,
				ScoreDetails:                       scoreDetailsByRoute[key],
				Reason:                             reason,
				ManualPrimary:                      manualPrimary,
				ManualPrimaryUntil:                 route.State.ManualPrimaryUntil,
				ManualPrimaryAllowStabilityDegrade: route.State.ManualPrimaryAllowStabilityDegrade,
			})
			continue
		}
		runtimeProtectionActive := route.State.RuntimeProtectionUntil > now
		runtimeProtectionExpired := route.State.RuntimeProtectionUntil > 0 &&
			route.State.RuntimeProtectionUntil <= now
		if runtimeProtectionActive {
			reason := fmt.Sprintf(
				"运行时稳定性保护中，保留上一轮优先级和权重，保护至 %s",
				time.Unix(route.State.RuntimeProtectionUntil, 0).Format("2006-01-02 15:04:05"),
			)
			result.Skipped++
			channelSmartScheduleSetAdjustmentReason(scoreDetailsByRoute[key], reason)
			result.recordAdjustment(channelSmartScheduleTaskAdjustment{
				ChannelId: route.ChannelId, ChannelName: route.ChannelName,
				Group: route.Group, Model: route.Model,
				Action:      channelSmartScheduleAdjustmentSkipped,
				OldPriority: route.Priority, NewPriority: route.Priority,
				OldWeight: route.Weight, NewWeight: route.Weight,
				ScoreDetails:                       scoreDetailsByRoute[key],
				Reason:                             reason,
				ManualPrimary:                      manualPrimary,
				ManualPrimaryUntil:                 route.State.ManualPrimaryUntil,
				ManualPrimaryAllowStabilityDegrade: route.State.ManualPrimaryAllowStabilityDegrade,
			})
			continue
		}
		if runtimeProtectionExpired &&
			route.State.StabilityState == model.ChannelSmartScheduleStabilityDegraded &&
			!policy.StabilityEnabled {
			targetPriority, targetWeight := channelSmartScheduleRouteRestoreTarget(route.State)
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				targetPriority: targetPriority, targetWeight: targetWeight,
				status:  model.ChannelSmartScheduleStatusSucceeded,
				message: "运行时保护已结束，已恢复基础路由",
				stability: &model.ChannelSmartScheduleStabilityUpdate{
					Since: now,
				},
				runtimeProtectionClear: true,
				routingSnapshot:        channelSmartScheduleClearTemporaryTraffic(route.State),
				reapplyManualPrimary:   manualPrimary,
			})
			continue
		}
		if route.State.StabilityState != "" && (!policy.StabilityEnabled || !routeStabilityAvailable) &&
			!(runtimeProtectionExpired && route.State.StabilityState == model.ChannelSmartScheduleStabilityDegraded) {
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				targetPriority: currentPriority, targetWeight: currentWeight,
				status:          model.ChannelSmartScheduleStatusSkipped,
				message:         "稳定性保护未启用或统计不可用，保持当前安全状态",
				routingSnapshot: channelSmartScheduleClearTemporaryTraffic(route.State),
			})
			continue
		}

		switch route.State.StabilityState {
		case model.ChannelSmartScheduleStabilityDegraded:
			if route.State.StabilityUntil > now {
				directActions = append(directActions, channelSmartScheduleRouteDirectAction{
					key: key, currentPriority: currentPriority, currentWeight: currentWeight,
					targetPriority: channelMonitorSmartScheduleDegradedPriority,
					targetWeight:   channelMonitorSmartScheduleDegradedWeight,
					status:         model.ChannelSmartScheduleStatusSkipped,
					message: fmt.Sprintf("稳定性降级中，将于 %s 后试放",
						time.Unix(route.State.StabilityUntil, 0).Format("2006-01-02 15:04:05")),
					routingSnapshot: channelSmartScheduleClearTemporaryTraffic(route.State),
				})
				continue
			}
			targetPriority, targetWeight := channelSmartScheduleRouteProbeTarget(route.State)
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				targetPriority: targetPriority, targetWeight: targetWeight,
				status:  model.ChannelSmartScheduleStatusSucceeded,
				message: "降级时间已结束，已按小流量权重开始稳定性试放",
				stability: &model.ChannelSmartScheduleStabilityUpdate{
					State: model.ChannelSmartScheduleStabilityProbing, Since: now,
					SavedPriority: route.State.StabilitySavedPriority,
					SavedWeight:   route.State.StabilitySavedWeight,
				},
				runtimeProtectionClear: true,
				routingSnapshot:        channelSmartScheduleClearTemporaryTraffic(route.State),
			})
			continue
		case model.ChannelSmartScheduleStabilityProbing, "":
		default:
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				targetPriority: currentPriority, targetWeight: currentWeight,
				status:          model.ChannelSmartScheduleStatusSkipped,
				message:         "稳定性调度状态无效，保持当前安全状态",
				routingSnapshot: channelSmartScheduleClearTemporaryTraffic(route.State),
			})
			continue
		}

		businessPerformance := channelSmartScheduleClonePerformance(performanceByModel[modelKey])
		businessPerformance = channelSmartScheduleMergeSharedSamplePerformance(
			businessPerformance, route.SharedSamples, performanceStart,
		)
		stabilityPerformance := channelSmartScheduleClonePerformance(stabilityPerformanceByModel[modelKey])
		probeWindowStart := stabilityStart
		if policy.StabilityEnabled && route.State.StabilitySince > stabilityStart {
			probeWindowStart = route.State.StabilitySince
		}
		if policy.StabilityEnabled && logStabilityAvailable && route.State.StabilitySince > stabilityStart {
			metric, metricErr := model.GetChannelMonitorRouteStabilityMetric(
				ctx, route.State.StabilitySince, route.ChannelId, route.Model,
			)
			if metricErr != nil {
				return result, metricErr
			}
			stabilityPerformance = channelSmartScheduleSetStabilityMetric(stabilityPerformance, metric)
		}
		if policy.StabilityEnabled && policy.JitterEnabled && route.State.StabilitySince > stabilityStart {
			metric, metricErr := model.GetChannelMonitorRoutePerformanceMetric(
				ctx, route.State.StabilitySince, route.ChannelId, route.Model,
			)
			if metricErr != nil {
				return result, metricErr
			}
			stabilityPerformance = channelSmartScheduleSetPerformanceMetric(stabilityPerformance, metric)
		}
		stabilityPerformance = channelSmartScheduleMergeSharedSamplePerformance(
			stabilityPerformance, route.SharedSamples, probeWindowStart,
		)
		performance := channelSmartScheduleCombineWindowPerformance(businessPerformance, stabilityPerformance)
		if performance != nil {
			performance.Stability, performance.StabilitySampleCount = channelSmartScheduleStabilityScore(
				performance.StabilitySuccessCount,
				performance.StabilityFailureCount,
				performance.StabilityFinalFailureCount,
				performance.StabilityFailureDurationBuckets,
				policy,
			)
			channelSmartScheduleApplyJitterMeasurement(performance, route.State, policy)
			if update := channelSmartScheduleJitterBaselineUpdate(performance, route.State, policy, now); update != nil {
				jitterUpdates[key] = update
			}
		}
		candidate := channelSmartScheduleCandidate{
			ChannelId: route.ChannelId, CurrentPriority: currentPriority, CurrentWeight: currentWeight,
			StabilityAvailable: routeStabilityAvailable, ManualPrimary: manualPrimary,
		}
		if performance != nil {
			candidate.SampleGroupCount = performance.SampleGroupCount
			candidate.FirstTokenMs = performance.AverageFirstTokenMs
			if policy.JitterEnabled && performance.WinsorizedAverageFirstTokenMs != nil {
				candidate.FirstTokenMs = performance.WinsorizedAverageFirstTokenMs
			}
			candidate.TPS = performance.AverageTPS
			candidate.FirstTokenSampleCount = performance.FirstTokenSampleCount
			candidate.TPSSampleCount = performance.TPSSampleCount
			candidate.Stability = performance.Stability
			candidate.StabilitySampleCount = performance.StabilitySampleCount
		}
		scoreDetailsByRoute[key] = channelSmartScheduleNewScoreDetails(
			candidate, policy.Strategy, policy.StabilityEnabled, policy.ApplyMode,
			policy.MinSamples, forceReset, policy.Scoring,
		)

		if route.State.StabilityState == model.ChannelSmartScheduleStabilityProbing {
			runtimeHealth := getChannelSmartScheduleRuntimeHealth(
				route.ChannelId,
				route.Model,
				now,
				policy.BurstFailureWindowSeconds,
				settings.SmartScheduleControlRevision,
			)
			runtimeRecoveryReady := runtimeHealth.RecoverySuccesses >= policy.RecoverySuccessThreshold
			legacyRecoveryReady := performance != nil && performance.Stability != nil &&
				performance.StabilitySampleCount >= int64(policy.MinSamples) &&
				*performance.Stability >= recoveryStabilityScore
			if !runtimeRecoveryReady && (performance == nil || performance.Stability == nil ||
				performance.StabilitySampleCount < int64(policy.MinSamples)) {
				targetPriority, targetWeight := channelSmartScheduleRouteProbeTarget(route.State)
				directActions = append(directActions, channelSmartScheduleRouteDirectAction{
					key: key, currentPriority: currentPriority, currentWeight: currentWeight,
					targetPriority: targetPriority, targetWeight: targetWeight,
					status: model.ChannelSmartScheduleStatusSkipped,
					message: fmt.Sprintf(
						"稳定性试放成功次数不足（%d/%d）",
						runtimeHealth.RecoverySuccesses,
						policy.RecoverySuccessThreshold,
					),
					routingSnapshot: channelSmartScheduleClearTemporaryTraffic(route.State),
				})
				continue
			}
			stabilityDescription := channelSmartScheduleStabilityDescription(performance)
			if !runtimeRecoveryReady && performance != nil && performance.Stability != nil &&
				*performance.Stability < degradeStabilityScore {
				directActions = append(directActions, channelSmartScheduleRouteDirectAction{
					key: key, currentPriority: currentPriority, currentWeight: currentWeight,
					targetPriority: channelMonitorSmartScheduleDegradedPriority,
					targetWeight:   channelMonitorSmartScheduleDegradedWeight,
					status:         model.ChannelSmartScheduleStatusSucceeded,
					message: fmt.Sprintf("%s，低于降级阈值 %.1f%%，再次降级",
						stabilityDescription, policy.DegradeStabilityScore),
					stability: &model.ChannelSmartScheduleStabilityUpdate{
						State:         model.ChannelSmartScheduleStabilityDegraded,
						Until:         now + int64(policy.CooldownMinutes*60),
						SavedPriority: route.State.StabilitySavedPriority,
						SavedWeight:   route.State.StabilitySavedWeight,
					},
					routingSnapshot: channelSmartScheduleClearTemporaryTraffic(route.State),
				})
				continue
			}
			if !runtimeRecoveryReady && !legacyRecoveryReady {
				targetPriority, targetWeight := channelSmartScheduleRouteProbeTarget(route.State)
				directActions = append(directActions, channelSmartScheduleRouteDirectAction{
					key: key, currentPriority: currentPriority, currentWeight: currentWeight,
					targetPriority: targetPriority, targetWeight: targetWeight,
					status: model.ChannelSmartScheduleStatusSkipped,
					message: fmt.Sprintf("%s，尚未达到恢复阈值 %.1f%%，继续小流量试放",
						stabilityDescription, policy.RecoveryStabilityScore),
					routingSnapshot: channelSmartScheduleClearTemporaryTraffic(route.State),
				})
				continue
			}
			targetPriority, targetWeight := channelSmartScheduleRouteRestoreTarget(route.State)
			recoveryMessage := fmt.Sprintf("%s，已达到恢复阈值 %.1f%%，解除保护并恢复原优先级和权重",
				stabilityDescription, policy.RecoveryStabilityScore)
			if runtimeRecoveryReady {
				recoveryMessage = fmt.Sprintf(
					"稳定性试放已连续成功 %d 次，达到恢复要求，解除保护并恢复原优先级和权重",
					runtimeHealth.RecoverySuccesses,
				)
			}
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				targetPriority: targetPriority, targetWeight: targetWeight,
				status:               model.ChannelSmartScheduleStatusSucceeded,
				message:              recoveryMessage,
				stability:            &model.ChannelSmartScheduleStabilityUpdate{Since: route.State.StabilitySince},
				routingSnapshot:      channelSmartScheduleClearTemporaryTraffic(route.State),
				reapplyManualPrimary: manualPrimary,
			})
			continue
		} else if route.State.StabilityState == "" && route.State.StabilitySince > 0 &&
			route.State.StabilitySince <= stabilityStart {
			stabilityUpdates[key] = &model.ChannelSmartScheduleStabilityUpdate{}
		}

		if (!manualPrimary || route.State.ManualPrimaryAllowStabilityDegrade) &&
			policy.StabilityEnabled && route.State.StabilityState == "" &&
			performance != nil && performance.Stability != nil &&
			performance.StabilitySampleCount >= int64(policy.MinSamples) &&
			*performance.Stability < degradeStabilityScore {
			savedPriority, savedWeight := channelSmartScheduleSavedTarget(currentPriority, currentWeight)
			if route.State.TemporaryTrafficKind != "" {
				savedPriority = route.State.BasePriority
				savedWeight = route.State.BaseWeight
			}
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				targetPriority: channelMonitorSmartScheduleDegradedPriority,
				targetWeight:   channelMonitorSmartScheduleDegradedWeight,
				status:         model.ChannelSmartScheduleStatusSucceeded,
				message: fmt.Sprintf("%s，低于降级阈值 %.1f%%，已在当前分组和模型降级至优先级 0、权重 0",
					channelSmartScheduleStabilityDescription(performance), policy.DegradeStabilityScore),
				stability: &model.ChannelSmartScheduleStabilityUpdate{
					State:         model.ChannelSmartScheduleStabilityDegraded,
					Until:         now + int64(policy.CooldownMinutes*60),
					SavedPriority: savedPriority, SavedWeight: savedWeight,
				},
				routingSnapshot: channelSmartScheduleClearTemporaryTraffic(route.State),
			})
			continue
		}

		monitor := monitorByChannel[route.ChannelId]
		var ratio *float64
		if monitor.UpdatedTime > 0 && validateChannelMonitorRatio(&monitor.Ratio) {
			value, _, conversionErr := channelMonitorCostRatioFromModel(monitor, monitor.Ratio)
			if conversionErr == nil {
				ratio = &value
			}
		}
		candidate.Ratio = ratio
		scoreDetailsByRoute[key] = channelSmartScheduleNewScoreDetails(
			candidate, policy.Strategy, policy.StabilityEnabled, policy.ApplyMode,
			policy.MinSamples, forceReset, policy.Scoring,
		)
		candidate.PreviousBaseRank = route.State.BaseRank
		poolCandidates[poolKey] = append(poolCandidates[poolKey], candidate)
		if routeKeyByPoolChannel[poolKey] == nil {
			routeKeyByPoolChannel[poolKey] = make(map[int]channelSmartScheduleRouteKey)
		}
		routeKeyByPoolChannel[poolKey][route.ChannelId] = key
	}

	fixedChannelByPool := make(map[channelSmartScheduleRoutePoolKey]int)
	fixedPriorityByPool := make(map[channelSmartScheduleRoutePoolKey]int64)
	fixedPriorityBlockedByPool := make(map[channelSmartScheduleRoutePoolKey]bool)
	for poolKey, routes := range poolRoutes {
		fixedChannelId := 0
		fixedTargetPriority := int64(1)
		for _, route := range routes {
			if route.State.ManualPrimaryUntil > now &&
				route.State.Participates() && route.ChannelStatus == common.ChannelStatusEnabled && route.Enabled {
				fixedChannelId = route.ChannelId
				fixedTargetPriority = max(
					fixedTargetPriority,
					route.Priority,
					route.State.LastSchedulePriority,
					route.State.StabilitySavedPriority,
				)
				break
			}
		}
		if fixedChannelId == 0 {
			continue
		}
		for _, route := range routes {
			if route.ChannelId == fixedChannelId || route.ChannelStatus != common.ChannelStatusEnabled || !route.Enabled {
				continue
			}
			if route.Priority == math.MaxInt64 {
				fixedPriorityBlockedByPool[poolKey] = true
				break
			}
			fixedTargetPriority = max(fixedTargetPriority, route.Priority+1)
		}
		fixedChannelByPool[poolKey] = fixedChannelId
		fixedPriorityByPool[poolKey] = fixedTargetPriority
	}
	for index := range directActions {
		action := &directActions[index]
		if !action.reapplyManualPrimary {
			continue
		}
		poolKey := channelSmartScheduleRoutePoolKey{group: action.key.group, model: action.key.model}
		if fixedChannelByPool[poolKey] != action.key.channelId {
			continue
		}
		if fixedPriorityBlockedByPool[poolKey] {
			return result, errors.New("池内存在最大优先级路由，无法把恢复后的固定主渠道提升到更高优先级")
		}
		action.targetPriority = fixedPriorityByPool[poolKey]
		action.targetWeight = 1000
	}

	for poolKey, candidates := range poolCandidates {
		policy := policyByGroup[poolKey.group]
		fixedChannelId := fixedChannelByPool[poolKey]
		fixedTargetPriority := fixedPriorityByPool[poolKey]
		for index := range candidates {
			if candidates[index].ChannelId != fixedChannelId {
				continue
			}
			candidates[index].ManualTargetPriority = fixedTargetPriority
			if policy.ApplyMode == channelMonitorSmartScheduleApplyWeight {
				candidates[index].CurrentPriority = fixedTargetPriority
			}
			break
		}
		if fixedChannelId > 0 && fixedPriorityBlockedByPool[poolKey] {
			failure := fmt.Errorf("池内存在最大优先级路由，无法把固定主渠道提升到更高优先级")
			for _, candidate := range candidates {
				key := routeKeyByPoolChannel[poolKey][candidate.ChannelId]
				route := routeByKey[key]
				message := result.recordFailure(
					candidate.ChannelId, route.ChannelName, poolKey.group, poolKey.model, "plan", failure,
				)
				result.recordAdjustment(channelSmartScheduleTaskAdjustment{
					ChannelId: candidate.ChannelId, ChannelName: route.ChannelName,
					Group: poolKey.group, Model: poolKey.model,
					Action: channelSmartScheduleAdjustmentFailed, Reason: message, FailureStage: "plan",
					OldPriority: route.Priority, NewPriority: route.Priority,
					OldWeight: route.Weight, NewWeight: route.Weight,
					PreviousEffectiveTime:     route.State.LastScheduleTime,
					PreviousEffectivePriority: route.State.LastSchedulePriority,
					PreviousEffectiveWeight:   route.State.LastScheduleWeight,
				})
			}
			continue
		}
		plan := planChannelSmartScheduleWithScoring(
			candidates, policy.Strategy, policy.StabilityEnabled,
			policy.ApplyMode, policy.MinSamples, forceReset, policy.Scoring,
		)
		result.Planned += len(plan.Items)
		candidateByChannel := make(map[int]channelSmartScheduleCandidate, len(candidates))
		for _, candidate := range candidates {
			candidateByChannel[candidate.ChannelId] = candidate
		}
		for _, candidate := range candidates {
			reason, skipped := plan.Skipped[candidate.ChannelId]
			if !skipped {
				continue
			}
			key := routeKeyByPoolChannel[poolKey][candidate.ChannelId]
			scoreDetailsByRoute[key] = plan.Details[candidate.ChannelId]
			update := channelSmartScheduleRouteStatusUpdate(
				key, model.ChannelSmartScheduleStatusSkipped, reason, nil,
				candidate.CurrentPriority, candidate.CurrentWeight, now, stabilityUpdates[key],
			)
			statusUpdates = append(statusUpdates, update)
		}

		if policy.ApplyMode == channelMonitorSmartScheduleApplyPriorityWeight {
			temporaryChannelId := 0
			temporaryKind := ""
			temporaryPercent := 0.0
			explorationIndexes := make([]int, 0)
			for index, item := range plan.Items {
				if item.ChannelId == plan.ActualPrimaryId || item.SkipReason == "" {
					continue
				}
				candidate := candidateByChannel[item.ChannelId]
				if channelSmartScheduleCandidateNeedsExplorationWithScoring(
					candidate, policy.Strategy, policy.StabilityEnabled, policy.MinSamples, policy.Scoring,
				) {
					explorationIndexes = append(explorationIndexes, index)
				}
			}
			if policy.SampleMode == channelMonitorSmartScheduleSampleTraffic && len(explorationIndexes) > 0 {
				sort.SliceStable(explorationIndexes, func(i int, j int) bool {
					left := plan.Items[explorationIndexes[i]]
					right := plan.Items[explorationIndexes[j]]
					leftState := routeByKey[routeKeyByPoolChannel[poolKey][left.ChannelId]].State
					rightState := routeByKey[routeKeyByPoolChannel[poolKey][right.ChannelId]].State
					leftActive := leftState.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficExploration
					rightActive := rightState.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficExploration
					if leftActive != rightActive {
						return leftActive
					}
					if leftActive && leftState.TemporaryTrafficSince != rightState.TemporaryTrafficSince {
						return leftState.TemporaryTrafficSince < rightState.TemporaryTrafficSince
					}
					if left.BaseRank != right.BaseRank {
						return left.BaseRank < right.BaseRank
					}
					return left.ChannelId < right.ChannelId
				})
				temporaryChannelId = plan.Items[explorationIndexes[0]].ChannelId
				temporaryKind = model.ChannelSmartScheduleTemporaryTrafficExploration
				temporaryPercent = policy.ExplorationTrafficPercent
			} else if policy.PrioritySamplingEnabled {
				samplingIndexes := make([]int, 0, len(plan.Items)-1)
				activeIndex := -1
				for index, item := range plan.Items {
					if !item.Scored || item.BaseRank <= 1 || item.ChannelId == plan.ActualPrimaryId ||
						item.ChannelId == fixedChannelId {
						continue
					}
					samplingIndexes = append(samplingIndexes, index)
					state := routeByKey[routeKeyByPoolChannel[poolKey][item.ChannelId]].State
					if state.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficPrioritySampling &&
						now-state.TemporaryTrafficSince < int64(policy.PrioritySamplingIntervalMinutes*60) {
						activeIndex = index
					}
				}
				if activeIndex >= 0 {
					temporaryChannelId = plan.Items[activeIndex].ChannelId
				} else if len(samplingIndexes) > 0 {
					sort.SliceStable(samplingIndexes, func(i int, j int) bool {
						left := plan.Items[samplingIndexes[i]]
						right := plan.Items[samplingIndexes[j]]
						leftState := routeByKey[routeKeyByPoolChannel[poolKey][left.ChannelId]].State
						rightState := routeByKey[routeKeyByPoolChannel[poolKey][right.ChannelId]].State
						if leftState.LastPrioritySampleTime != rightState.LastPrioritySampleTime {
							return leftState.LastPrioritySampleTime < rightState.LastPrioritySampleTime
						}
						if left.BaseRank != right.BaseRank {
							return left.BaseRank < right.BaseRank
						}
						return left.ChannelId < right.ChannelId
					})
					temporaryChannelId = plan.Items[samplingIndexes[0]].ChannelId
				}
				if temporaryChannelId > 0 {
					temporaryKind = model.ChannelSmartScheduleTemporaryTrafficPrioritySampling
					for _, item := range plan.Items {
						if item.ChannelId == temporaryChannelId {
							temporaryPercent = max(
								policy.PrioritySamplingMinPercent,
								policy.PrioritySamplingBasePercent*math.Pow(
									policy.PrioritySamplingDecayPercent/100,
									float64(item.BaseRank-2),
								),
							)
							break
						}
					}
				}
			}

			type poolRouting struct {
				priority int64
				weight   uint
				managed  bool
			}
			routingByChannel := make(map[int]poolRouting, len(poolRoutes[poolKey]))
			for _, route := range poolRoutes[poolKey] {
				if route.ChannelStatus != common.ChannelStatusEnabled || !route.Enabled {
					continue
				}
				routingByChannel[route.ChannelId] = poolRouting{priority: route.Priority, weight: route.Weight}
			}
			for _, item := range plan.Items {
				routingByChannel[item.ChannelId] = poolRouting{
					priority: item.TargetPriority, weight: item.TargetWeight, managed: true,
				}
			}
			for _, action := range directActions {
				if action.key.group == poolKey.group && action.key.model == poolKey.model {
					routingByChannel[action.key.channelId] = poolRouting{
						priority: action.targetPriority, weight: action.targetWeight, managed: true,
					}
				}
			}
			highestPriority := int64(math.MinInt64)
			topLayerChannelIds := make([]int, 0)
			var topLayerWeight uint64
			for channelId, routing := range routingByChannel {
				if channelId == temporaryChannelId {
					continue
				}
				if routing.priority > highestPriority {
					highestPriority = routing.priority
					topLayerChannelIds = topLayerChannelIds[:0]
					topLayerWeight = 0
				}
				if routing.priority == highestPriority {
					topLayerChannelIds = append(topLayerChannelIds, channelId)
					if math.MaxUint64-topLayerWeight >= uint64(routing.weight) {
						topLayerWeight += uint64(routing.weight)
					}
				}
			}
			sort.Ints(topLayerChannelIds)

			if temporaryChannelId > 0 && highestPriority != int64(math.MinInt64) && topLayerWeight > 0 {
				temporaryWeight := uint(0)
				managedPrimaryOnly := len(topLayerChannelIds) == 1 &&
					topLayerChannelIds[0] == plan.ActualPrimaryId &&
					routingByChannel[plan.ActualPrimaryId].managed
				if managedPrimaryOnly {
					temporaryWeight = uint(math.Round(
						channelMonitorSmartScheduleTemporaryWeightTotal * temporaryPercent /
							channelMonitorScorePercentageTotal,
					))
					temporaryWeight = min(max(temporaryWeight, 1), uint(channelMonitorSmartScheduleTemporaryWeightTotal-1))
					for index := range plan.Items {
						if plan.Items[index].ChannelId == plan.ActualPrimaryId {
							plan.Items[index].TargetWeight = uint(channelMonitorSmartScheduleTemporaryWeightTotal) - temporaryWeight
							routing := routingByChannel[plan.ActualPrimaryId]
							routing.weight = plan.Items[index].TargetWeight
							routingByChannel[plan.ActualPrimaryId] = routing
							break
						}
					}
				} else {
					exactWeight := float64(topLayerWeight) * temporaryPercent /
						(channelMonitorScorePercentageTotal - temporaryPercent)
					if !math.IsNaN(exactWeight) && !math.IsInf(exactWeight, 0) && exactWeight >= 1 &&
						exactWeight <= float64(^uint(0)) {
						temporaryWeight = uint(math.Round(exactWeight))
					}
				}
				if temporaryWeight > 0 {
					for index := range plan.Items {
						if plan.Items[index].ChannelId == temporaryChannelId {
							plan.Items[index].TargetPriority = highestPriority
							plan.Items[index].TargetWeight = temporaryWeight
							break
						}
					}
				} else {
					temporaryChannelId = 0
					temporaryKind = ""
					temporaryPercent = 0
				}
			}

			if temporaryChannelId > 0 {
				topLayerChannelIds = append(topLayerChannelIds, temporaryChannelId)
				sort.Ints(topLayerChannelIds)
			}
			for index := range plan.Items {
				item := &plan.Items[index]
				itemTemporaryKind := ""
				itemTemporaryPercent := 0.0
				if item.ChannelId == temporaryChannelId {
					itemTemporaryKind = temporaryKind
					itemTemporaryPercent = temporaryPercent
				}
				item.ScoreDetails.Decision.AppliedPriority = item.TargetPriority
				item.ScoreDetails.Decision.AppliedWeight = item.TargetWeight
				item.ScoreDetails.Decision.ActualHighestPriority = highestPriority
				item.ScoreDetails.Decision.ActualTopLayerChannelIds = append([]int(nil), topLayerChannelIds...)
				item.ScoreDetails.Decision.TemporaryTrafficKind = itemTemporaryKind
				item.ScoreDetails.Decision.TemporaryTrafficTargetPercent = itemTemporaryPercent
				item.ScoreDetails.Decision.ActualPrimaryChannelId = plan.ActualPrimaryId
			}
		}
		for _, item := range plan.Items {
			key := routeKeyByPoolChannel[poolKey][item.ChannelId]
			scoreDetailsByRoute[key] = item.ScoreDetails
			var score *float64
			if item.Scored {
				value := item.Score
				score = &value
			}
			message := ""
			if route := routeByKey[key]; route.State.ManualPrimaryUntil > now {
				message = fmt.Sprintf("管理员已固定为主渠道，固定至 %s",
					time.Unix(route.State.ManualPrimaryUntil, 0).Format("2006-01-02 15:04:05"))
			}
			update := channelSmartScheduleRouteStatusUpdate(
				key, model.ChannelSmartScheduleStatusSucceeded, message, score,
				item.TargetPriority, item.TargetWeight, now, stabilityUpdates[key],
			)
			if policy.ApplyMode == channelMonitorSmartScheduleApplyPriorityWeight {
				state := routeByKey[key].State
				temporaryKind := item.ScoreDetails.Decision.TemporaryTrafficKind
				temporarySince := int64(0)
				lastPrioritySampleTime := state.LastPrioritySampleTime
				if temporaryKind != "" {
					temporarySince = state.TemporaryTrafficSince
					if state.TemporaryTrafficKind != temporaryKind || temporarySince <= 0 {
						temporarySince = now
					}
					if temporaryKind == model.ChannelSmartScheduleTemporaryTrafficPrioritySampling &&
						state.TemporaryTrafficKind != temporaryKind {
						lastPrioritySampleTime = now
					}
					temporaryDescription := "样本不足探索"
					if temporaryKind == model.ChannelSmartScheduleTemporaryTrafficPrioritySampling {
						temporaryDescription = "低优先级轮转"
					}
					message = fmt.Sprintf("%s，临时提升到最高优先级并分配 %.2f%% 目标流量",
						temporaryDescription, item.ScoreDetails.Decision.TemporaryTrafficTargetPercent)
					update.Error = message
				}
				update.RoutingSnapshot = &model.ChannelSmartScheduleRoutingSnapshotUpdate{
					BaseRank: item.BaseRank, BasePriority: item.BasePriority, BaseWeight: item.BaseWeight,
					TemporaryTrafficKind:          temporaryKind,
					TemporaryTrafficSince:         temporarySince,
					TemporaryTrafficTargetPercent: item.ScoreDetails.Decision.TemporaryTrafficTargetPercent,
					ExplorationMaxPromptTokens:    0,
					LastPrioritySampleTime:        lastPrioritySampleTime,
				}
				if temporaryKind == model.ChannelSmartScheduleTemporaryTrafficExploration {
					update.RoutingSnapshot.ExplorationMaxPromptTokens = policy.ExplorationMaxPromptTokens
				}
			}
			statusUpdates = append(statusUpdates, update)
		}
	}
	for _, action := range directActions {
		update := channelSmartScheduleRouteStatusUpdate(
			action.key, action.status, action.message, nil, action.targetPriority,
			action.targetWeight, now, action.stability,
		)
		update.RoutingSnapshot = action.routingSnapshot
		if action.runtimeProtectionClear {
			protectionUntil := int64(0)
			update.RuntimeProtectionUntil = &protectionUntil
		}
		statusUpdates = append(statusUpdates, update)
	}
	for index := range statusUpdates {
		key := channelSmartScheduleRouteKey{
			channelId: statusUpdates[index].ChannelId,
			group:     statusUpdates[index].Group,
			model:     statusUpdates[index].Model,
		}
		statusUpdates[index].Jitter = jitterUpdates[key]
		statusUpdates[index].ScoreDetails = scoreDetailsByRoute[key]
		if statusUpdates[index].ScoreDetails != nil && statusUpdates[index].Error != "" {
			channelSmartScheduleSetAdjustmentReason(statusUpdates[index].ScoreDetails, statusUpdates[index].Error)
		}
	}

	processed := result.Skipped
	updatesByPool := make(map[channelSmartScheduleRoutePoolKey][]model.ChannelSmartScheduleRouteResultUpdate)
	updatesByKey := make(map[channelSmartScheduleRouteKey]struct{}, len(statusUpdates))
	for _, update := range statusUpdates {
		key := channelSmartScheduleRouteKey{channelId: update.ChannelId, group: update.Group, model: update.Model}
		route := routeByKey[key]
		update.PoolGuard = true
		update.ExpectedRevision = route.State.Revision
		update.ExpectedControlRevision = controlRevision
		update.ExpectedParticipationSet = route.State.ParticipationSet
		update.ExpectedExcluded = route.State.Excluded
		update.ExpectedAbilityEnabled = route.Enabled
		update.ExpectedChannelStatus = route.ChannelStatus
		update.ExpectedPriority = route.Priority
		update.ExpectedWeight = route.Weight
		update.ApplyPriorityWeight = update.Priority != route.Priority || update.Weight != route.Weight
		if update.ScoreDetails != nil && update.ScoreDetails.Decision.AdjustmentReason == "" {
			channelSmartScheduleSetAdjustmentReason(update.ScoreDetails, channelSmartScheduleScoredAdjustmentReason(
				update.Score, update.Priority != route.Priority, update.Weight != route.Weight,
			))
		}
		poolKey := channelSmartScheduleRoutePoolKey{group: update.Group, model: update.Model}
		updatesByPool[poolKey] = append(updatesByPool[poolKey], update)
		updatesByKey[key] = struct{}{}
	}
	for poolKey := range updatesByPool {
		for _, route := range poolRoutes[poolKey] {
			key := channelSmartScheduleRouteKey{channelId: route.ChannelId, group: route.Group, model: route.Model}
			if _, exists := updatesByKey[key]; exists {
				continue
			}
			updatesByPool[poolKey] = append(updatesByPool[poolKey], model.ChannelSmartScheduleRouteResultUpdate{
				ChannelId: route.ChannelId, Group: route.Group, Model: route.Model,
				Priority: route.Priority, Weight: route.Weight,
				PoolGuard: true, ObservationOnly: true,
				ExpectedRevision: route.State.Revision, ExpectedControlRevision: controlRevision,
				ExpectedParticipationSet: route.State.ParticipationSet,
				ExpectedExcluded:         route.State.Excluded,
				ExpectedAbilityEnabled:   route.Enabled,
				ExpectedChannelStatus:    route.ChannelStatus,
				ExpectedPriority:         route.Priority, ExpectedWeight: route.Weight,
			})
		}
	}
	poolOrder := make([]channelSmartScheduleRoutePoolKey, 0, len(updatesByPool))
	for poolKey := range updatesByPool {
		poolOrder = append(poolOrder, poolKey)
	}
	sort.Slice(poolOrder, func(i int, j int) bool {
		if poolOrder[i].group != poolOrder[j].group {
			return poolOrder[i].group < poolOrder[j].group
		}
		return poolOrder[i].model < poolOrder[j].model
	})
	for _, poolKey := range poolOrder {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		updates := updatesByPool[poolKey]
		sort.Slice(updates, func(i int, j int) bool { return updates[i].ChannelId < updates[j].ChannelId })
		outcomes, applyErr := model.ApplyChannelSmartScheduleRouteResults(updates)
		poolConflict := false
		if applyErr == nil {
			poolConflict = len(outcomes) != len(updates)
			for _, outcome := range outcomes {
				poolConflict = poolConflict || !outcome.Applied
			}
		}
		for index, update := range updates {
			if update.ObservationOnly {
				continue
			}
			key := channelSmartScheduleRouteKey{channelId: update.ChannelId, group: update.Group, model: update.Model}
			route := routeByKey[key]
			adjustment := channelSmartScheduleTaskAdjustment{
				ChannelId: update.ChannelId, ChannelName: route.ChannelName,
				Group: update.Group, Model: update.Model,
				OldPriority: route.Priority, NewPriority: update.Priority,
				OldWeight: route.Weight, NewWeight: update.Weight,
				Score: update.Score, ScoreDetails: update.ScoreDetails, Reason: update.Error,
				ManualPrimary:                      route.State.ManualPrimaryUntil > now,
				ManualPrimaryUntil:                 route.State.ManualPrimaryUntil,
				ManualPrimaryAllowStabilityDegrade: route.State.ManualPrimaryAllowStabilityDegrade,
				PreviousEffectiveTime:              route.State.LastScheduleTime,
				PreviousEffectivePriority:          route.State.LastSchedulePriority,
				PreviousEffectiveWeight:            route.State.LastScheduleWeight,
			}
			if applyErr != nil {
				adjustment.Action = channelSmartScheduleAdjustmentFailed
				adjustment.FailureStage = "write"
				adjustment.Reason = result.recordFailure(
					update.ChannelId, route.ChannelName, update.Group, update.Model, adjustment.FailureStage, applyErr,
				)
			} else if poolConflict {
				adjustment.Action = channelSmartScheduleAdjustmentFailed
				adjustment.FailureStage = "configuration_conflict"
				adjustment.Reason = result.recordFailure(
					update.ChannelId, route.ChannelName, update.Group, update.Model, adjustment.FailureStage,
					fmt.Errorf("调度执行期间渠道或配置已变化，整池保留上一轮结果"),
				)
			} else if outcomes[index].RoutingChanged ||
				channelSmartScheduleRouteResultChangesTrafficState(route.State, update) {
				result.Updated++
				cacheDirty = cacheDirty || outcomes[index].RoutingChanged
				adjustment.Action = channelSmartScheduleAdjustmentUpdated
			} else if update.Status == model.ChannelSmartScheduleStatusSkipped {
				result.Skipped++
				adjustment.Action = channelSmartScheduleAdjustmentSkipped
			} else {
				result.Unchanged++
				adjustment.Action = channelSmartScheduleAdjustmentUnchanged
			}
			if adjustment.Reason == "" {
				adjustment.Reason = channelSmartScheduleScoredAdjustmentReason(
					adjustment.Score,
					adjustment.OldPriority != adjustment.NewPriority,
					adjustment.OldWeight != adjustment.NewWeight,
				)
			}
			if adjustment.ScoreDetails != nil {
				channelSmartScheduleSetAdjustmentReason(adjustment.ScoreDetails, adjustment.Reason)
			}
			result.recordAdjustment(adjustment)
			processed++
			reportProgress(processed, result.Total)
		}
	}
	reportProgress(result.Total, result.Total)
	if result.Failed > 0 {
		return result, fmt.Errorf("%d 条智能调度路由未能应用，失败池已保留上一轮结果", result.Failed)
	}
	return result, nil
}

func channelSmartScheduleRouteResultChangesTrafficState(
	state model.ChannelSmartScheduleRouteState,
	update model.ChannelSmartScheduleRouteResultUpdate,
) bool {
	if stability := update.Stability; stability != nil &&
		(stability.State != state.StabilityState ||
			(stability.State != "" && stability.Until != state.StabilityUntil)) {
		return true
	}
	if update.RuntimeProtectionUntil != nil &&
		*update.RuntimeProtectionUntil != state.RuntimeProtectionUntil {
		return true
	}
	if snapshot := update.RoutingSnapshot; snapshot != nil &&
		(snapshot.TemporaryTrafficKind != state.TemporaryTrafficKind ||
			snapshot.TemporaryTrafficSince != state.TemporaryTrafficSince ||
			snapshot.TemporaryTrafficTargetPercent != state.TemporaryTrafficTargetPercent ||
			snapshot.ExplorationMaxPromptTokens != state.ExplorationMaxPromptTokens) {
		return true
	}
	return false
}

func channelSmartScheduleScoredAdjustmentReason(score *float64, priorityChanged bool, weightChanged bool) string {
	scoreDescription := "根据智能调度评分"
	if score != nil {
		scoreDescription = fmt.Sprintf("根据智能调度评分 %.4f", *score)
	}
	switch {
	case priorityChanged && weightChanged:
		return scoreDescription + "，在同一分组和模型调度池中调整优先级和权重"
	case priorityChanged:
		return scoreDescription + "，在同一分组和模型调度池中调整优先级"
	case weightChanged:
		return scoreDescription + "，在同一分组和模型调度池中调整权重"
	default:
		return scoreDescription + "，计算后优先级和权重已是目标值，无需调整"
	}
}

func channelSmartScheduleRouteStatusUpdate(
	key channelSmartScheduleRouteKey,
	status string,
	message string,
	score *float64,
	priority int64,
	weight uint,
	updatedTime int64,
	stability *model.ChannelSmartScheduleStabilityUpdate,
) model.ChannelSmartScheduleRouteResultUpdate {
	return model.ChannelSmartScheduleRouteResultUpdate{
		ChannelId: key.channelId, Group: key.group, Model: key.model,
		Status: status, Error: message, Score: score, Priority: priority,
		Weight: weight, Time: updatedTime, Stability: stability,
	}
}

func channelSmartScheduleRouteRestoreTarget(state model.ChannelSmartScheduleRouteState) (int64, uint) {
	return channelSmartScheduleSavedTarget(state.StabilitySavedPriority, state.StabilitySavedWeight)
}

func channelSmartScheduleRouteProbeTarget(state model.ChannelSmartScheduleRouteState) (int64, uint) {
	return channelMonitorSmartScheduleDegradedPriority, channelMonitorSmartScheduleMinWeight
}

func channelSmartScheduleClearTemporaryTraffic(
	state model.ChannelSmartScheduleRouteState,
) *model.ChannelSmartScheduleRoutingSnapshotUpdate {
	if state.TemporaryTrafficKind == "" {
		return nil
	}
	return &model.ChannelSmartScheduleRoutingSnapshotUpdate{
		BaseRank:               state.BaseRank,
		BasePriority:           state.BasePriority,
		BaseWeight:             state.BaseWeight,
		LastPrioritySampleTime: state.LastPrioritySampleTime,
	}
}
