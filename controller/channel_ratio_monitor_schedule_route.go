package controller

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
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

type channelSmartScheduleRouteDirectAction struct {
	key             channelSmartScheduleRouteKey
	currentPriority int64
	currentWeight   uint
	targetPriority  int64
	targetWeight    uint
	status          string
	message         string
	stability       *model.ChannelSmartScheduleStabilityUpdate
	exploration     *model.ChannelSmartScheduleExplorationUpdate
}

type channelSmartScheduleExplorationCandidate struct {
	key             channelSmartScheduleRouteKey
	currentPriority int64
	currentWeight   uint
	reason          string
	active          bool
	since           int64
	savedPriority   int64
	savedWeight     uint
}

func channelSmartScheduleSetPerformanceMetric(
	performance *channelSmartSchedulePerformance,
	metric model.ChannelMonitorRoutePerformanceMetric,
) *channelSmartSchedulePerformance {
	if performance == nil {
		performance = &channelSmartSchedulePerformance{}
	}
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
		policy.JitterBaselineHours,
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
	cacheDirty := false
	defer func() {
		if cacheDirty {
			model.InitChannelCache()
		}
	}()

	monitors, err := model.GetChannelRatioMonitors()
	if err != nil {
		return result, err
	}
	monitorByChannel := make(map[int]model.ChannelRatioMonitor, len(monitors))
	for _, monitor := range monitors {
		monitorByChannel[monitor.ChannelId] = monitor
	}

	needsPerformance := false
	needsStability := false
	for _, route := range selectedRoutes {
		policy := policyByGroup[route.Group]
		needsPerformance = needsPerformance || policy.needsPerformance() ||
			(policy.StabilityEnabled && policy.JitterEnabled)
		needsStability = needsStability || policy.StabilityEnabled
	}
	now := common.GetTimestamp()
	performanceStart := now - int64(settings.SmartSchedulePerformanceMinutes*60)
	performanceByRoute := make(map[channelSmartScheduleRouteKey]*channelSmartSchedulePerformance)
	if needsPerformance {
		metrics, metricErr := model.GetChannelMonitorRoutePerformanceMetrics(ctx, performanceStart, now)
		if metricErr != nil {
			return result, metricErr
		}
		for _, metric := range metrics {
			key := channelSmartScheduleRouteKey{channelId: metric.ChannelId, group: metric.GroupName, model: metric.ModelName}
			performanceByRoute[key] = channelSmartScheduleSetPerformanceMetric(nil, metric)
		}
	}
	logStabilityAvailable := common.LogConsumeEnabled && constant.ErrorLogEnabled
	if needsStability && logStabilityAvailable {
		metrics, metricErr := model.GetChannelMonitorRouteStabilityMetrics(ctx, performanceStart, now)
		if metricErr != nil {
			return result, metricErr
		}
		for _, metric := range metrics {
			key := channelSmartScheduleRouteKey{channelId: metric.ChannelId, group: metric.GroupName, model: metric.ModelName}
			performanceByRoute[key] = channelSmartScheduleSetStabilityMetric(performanceByRoute[key], metric)
		}
	}

	poolCandidates := make(map[channelSmartScheduleRoutePoolKey][]channelSmartScheduleCandidate)
	poolRoutes := make(map[channelSmartScheduleRoutePoolKey][]model.ChannelSmartScheduleRoute)
	explorationCandidates := make(map[channelSmartScheduleRoutePoolKey][]channelSmartScheduleExplorationCandidate)
	routeKeyByPoolChannel := make(map[channelSmartScheduleRoutePoolKey]map[int]channelSmartScheduleRouteKey)
	directActions := make([]channelSmartScheduleRouteDirectAction, 0)
	statusUpdates := make([]model.ChannelSmartScheduleRouteResultUpdate, 0, len(selectedRoutes))
	stabilityUpdates := make(map[channelSmartScheduleRouteKey]*model.ChannelSmartScheduleStabilityUpdate)
	jitterUpdates := make(map[channelSmartScheduleRouteKey]*model.ChannelSmartScheduleJitterUpdate)
	explorationUpdates := make(map[channelSmartScheduleRouteKey]*model.ChannelSmartScheduleExplorationUpdate)
	routeByKey := make(map[channelSmartScheduleRouteKey]model.ChannelSmartScheduleRoute, len(selectedRoutes))
	for _, route := range selectedRoutes {
		policy := policyByGroup[route.Group]
		poolKey := channelSmartScheduleRoutePoolKey{group: route.Group, model: route.Model}
		poolRoutes[poolKey] = append(poolRoutes[poolKey], route)
		degradeStabilityScore := policy.DegradeStabilityScore / 100
		recoveryStabilityScore := policy.RecoveryStabilityScore / 100
		key := channelSmartScheduleRouteKey{channelId: route.ChannelId, group: route.Group, model: route.Model}
		routeByKey[key] = route
		routeStabilityAvailable := logStabilityAvailable || policy.SampleMode == channelMonitorSmartScheduleSampleProbe
		currentPriority := route.Priority
		currentWeight := route.Weight
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
			result.recordAdjustment(channelSmartScheduleTaskAdjustment{
				ChannelId: route.ChannelId, ChannelName: route.ChannelName,
				Group: route.Group, Model: route.Model,
				Action:      channelSmartScheduleAdjustmentSkipped,
				OldPriority: route.Priority, NewPriority: route.Priority,
				OldWeight: route.Weight, NewWeight: route.Weight,
				Reason: reason,
			})
			continue
		}
		if route.State.ExplorationActive && route.State.StabilityState == "" &&
			policy.SampleMode != channelMonitorSmartScheduleSampleTraffic {
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				targetPriority: route.State.ExplorationSavedPriority,
				targetWeight:   route.State.ExplorationSavedWeight,
				status:         model.ChannelSmartScheduleStatusSkipped,
				message:        "探索采样已关闭，恢复探索前的优先级和权重",
				exploration:    &model.ChannelSmartScheduleExplorationUpdate{},
			})
			continue
		}

		if route.State.StabilityState != "" && (!policy.StabilityEnabled || !routeStabilityAvailable) {
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				targetPriority: currentPriority, targetWeight: currentWeight,
				status:      model.ChannelSmartScheduleStatusSkipped,
				message:     "稳定性保护未启用或统计不可用，保持当前安全状态",
				exploration: channelSmartScheduleClearExploration(route.State),
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
					exploration: channelSmartScheduleClearExploration(route.State),
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
				exploration: channelSmartScheduleClearExploration(route.State),
			})
			continue
		case model.ChannelSmartScheduleStabilityProbing, "":
		default:
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				targetPriority: currentPriority, targetWeight: currentWeight,
				status:      model.ChannelSmartScheduleStatusSkipped,
				message:     "稳定性调度状态无效，保持当前安全状态",
				exploration: channelSmartScheduleClearExploration(route.State),
			})
			continue
		}

		performance := performanceByRoute[key]
		probeWindowStart := performanceStart
		if policy.StabilityEnabled && route.State.StabilitySince > performanceStart {
			probeWindowStart = route.State.StabilitySince
		}
		if policy.StabilityEnabled && logStabilityAvailable && route.State.StabilitySince > performanceStart {
			metric, metricErr := model.GetChannelMonitorRouteStabilityMetric(
				ctx, route.State.StabilitySince, route.ChannelId, route.Group, route.Model,
			)
			if metricErr != nil {
				return result, metricErr
			}
			performance = channelSmartScheduleSetStabilityMetric(performance, metric)
			performanceByRoute[key] = performance
		}
		if policy.StabilityEnabled && policy.JitterEnabled && route.State.StabilitySince > performanceStart {
			metric, metricErr := model.GetChannelMonitorRoutePerformanceMetric(
				ctx, route.State.StabilitySince, route.ChannelId, route.Group, route.Model,
			)
			if metricErr != nil {
				return result, metricErr
			}
			performance = channelSmartScheduleSetPerformanceMetric(performance, metric)
			performanceByRoute[key] = performance
		}
		if policy.SampleMode == channelMonitorSmartScheduleSampleProbe {
			performance = channelSmartScheduleMergeProbePerformance(performance, route.State, probeWindowStart)
			performanceByRoute[key] = performance
		}
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

		if route.State.StabilityState == model.ChannelSmartScheduleStabilityProbing {
			if performance == nil || performance.Stability == nil ||
				performance.StabilitySampleCount < int64(policy.MinSamples) {
				sampleCount := int64(0)
				if performance != nil {
					sampleCount = performance.StabilitySampleCount
				}
				targetPriority, targetWeight := channelSmartScheduleRouteProbeTarget(route.State)
				directActions = append(directActions, channelSmartScheduleRouteDirectAction{
					key: key, currentPriority: currentPriority, currentWeight: currentWeight,
					targetPriority: targetPriority, targetWeight: targetWeight,
					status:      model.ChannelSmartScheduleStatusSkipped,
					message:     fmt.Sprintf("稳定性试放样本不足（%d/%d）", sampleCount, policy.MinSamples),
					exploration: channelSmartScheduleClearExploration(route.State),
				})
				continue
			}
			stabilityDescription := channelSmartScheduleStabilityDescription(performance)
			if *performance.Stability < degradeStabilityScore {
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
					exploration: channelSmartScheduleClearExploration(route.State),
				})
				continue
			}
			if *performance.Stability < recoveryStabilityScore {
				targetPriority, targetWeight := channelSmartScheduleRouteProbeTarget(route.State)
				directActions = append(directActions, channelSmartScheduleRouteDirectAction{
					key: key, currentPriority: currentPriority, currentWeight: currentWeight,
					targetPriority: targetPriority, targetWeight: targetWeight,
					status: model.ChannelSmartScheduleStatusSkipped,
					message: fmt.Sprintf("%s，尚未达到恢复阈值 %.1f%%，继续小流量试放",
						stabilityDescription, policy.RecoveryStabilityScore),
					exploration: channelSmartScheduleClearExploration(route.State),
				})
				continue
			}
			targetPriority, targetWeight := channelSmartScheduleRouteRestoreTarget(route.State)
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				targetPriority: targetPriority, targetWeight: targetWeight,
				status: model.ChannelSmartScheduleStatusSucceeded,
				message: fmt.Sprintf("%s，已达到恢复阈值 %.1f%%，解除保护并恢复原优先级和权重",
					stabilityDescription, policy.RecoveryStabilityScore),
				stability:   &model.ChannelSmartScheduleStabilityUpdate{Since: route.State.StabilitySince},
				exploration: channelSmartScheduleClearExploration(route.State),
			})
			continue
		} else if route.State.StabilityState == "" && route.State.StabilitySince > 0 &&
			route.State.StabilitySince <= performanceStart {
			stabilityUpdates[key] = &model.ChannelSmartScheduleStabilityUpdate{}
		}

		if policy.StabilityEnabled && route.State.StabilityState == "" && performance != nil && performance.Stability != nil &&
			performance.StabilitySampleCount >= int64(policy.MinSamples) &&
			*performance.Stability < degradeStabilityScore {
			savedPriority, savedWeight := channelSmartScheduleSavedTarget(currentPriority, currentWeight)
			if route.State.ExplorationActive {
				savedPriority = route.State.ExplorationSavedPriority
				savedWeight = route.State.ExplorationSavedWeight
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
				exploration: channelSmartScheduleClearExploration(route.State),
			})
			continue
		}

		monitor := monitorByChannel[route.ChannelId]
		var ratio *float64
		if monitor.UpdatedTime > 0 && validateChannelMonitorRatio(&monitor.Ratio) {
			value, _, conversionErr := channelMonitorCostRatioFromModel(monitor, monitor.Ratio)
			if conversionErr != nil && policy.needsRatio() {
				targetPriority := currentPriority
				targetWeight := currentWeight
				var exploration *model.ChannelSmartScheduleExplorationUpdate
				if route.State.ExplorationActive {
					targetPriority = route.State.ExplorationSavedPriority
					targetWeight = route.State.ExplorationSavedWeight
					exploration = &model.ChannelSmartScheduleExplorationUpdate{}
				}
				update := channelSmartScheduleRouteStatusUpdate(
					key, model.ChannelSmartScheduleStatusSkipped, "成本倍率换算失败："+conversionErr.Error(),
					nil, targetPriority, targetWeight, now, stabilityUpdates[key],
				)
				update.Exploration = exploration
				statusUpdates = append(statusUpdates, update)
				continue
			}
			if conversionErr == nil {
				ratio = &value
			}
		}
		candidate := channelSmartScheduleCandidate{
			ChannelId: route.ChannelId, CurrentPriority: currentPriority, CurrentWeight: currentWeight,
			Ratio: ratio, StabilityAvailable: routeStabilityAvailable,
		}
		if performance != nil {
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
		reason := channelSmartScheduleCandidateSkipReasonWithScoring(
			candidate, policy.Strategy, policy.StabilityEnabled,
			policy.MinSamples, policy.Scoring,
		)
		needsExploration := reason != "" && channelSmartScheduleCandidateNeedsExplorationWithScoring(
			candidate, policy.Strategy, policy.StabilityEnabled,
			policy.MinSamples, policy.Scoring,
		)
		if needsExploration && policy.ApplyMode == channelMonitorSmartScheduleApplyPriorityWeight &&
			policy.SampleMode == channelMonitorSmartScheduleSampleTraffic {
			savedPriority := currentPriority
			savedWeight := currentWeight
			if route.State.ExplorationActive {
				savedPriority = route.State.ExplorationSavedPriority
				savedWeight = route.State.ExplorationSavedWeight
			}
			explorationCandidates[poolKey] = append(explorationCandidates[poolKey], channelSmartScheduleExplorationCandidate{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				reason: reason, active: route.State.ExplorationActive,
				since:         route.State.ExplorationSince,
				savedPriority: savedPriority, savedWeight: savedWeight,
			})
			continue
		}
		if route.State.ExplorationActive {
			candidate.CurrentPriority = route.State.ExplorationSavedPriority
			candidate.CurrentWeight = route.State.ExplorationSavedWeight
			explorationUpdates[key] = &model.ChannelSmartScheduleExplorationUpdate{}
		}
		poolCandidates[poolKey] = append(poolCandidates[poolKey], candidate)
		if routeKeyByPoolChannel[poolKey] == nil {
			routeKeyByPoolChannel[poolKey] = make(map[int]channelSmartScheduleRouteKey)
		}
		routeKeyByPoolChannel[poolKey][route.ChannelId] = key
	}

	for poolKey, candidates := range explorationCandidates {
		targetPriority, _, hasRegularCandidate := channelSmartScheduleExplorationTarget(
			poolRoutes[poolKey], 0, policyByGroup[poolKey.group].ExplorationTrafficPercent,
		)
		eligible := make([]channelSmartScheduleExplorationCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			if !candidate.active && hasRegularCandidate && candidate.currentPriority >= targetPriority && candidate.currentWeight > 0 {
				directActions = append(directActions, channelSmartScheduleRouteDirectAction{
					key: candidate.key, currentPriority: candidate.currentPriority, currentWeight: candidate.currentWeight,
					targetPriority: candidate.currentPriority, targetWeight: candidate.currentWeight,
					status:  model.ChannelSmartScheduleStatusSkipped,
					message: candidate.reason + "，当前已在最高优先级自然采样",
				})
				continue
			}
			eligible = append(eligible, candidate)
		}
		if len(eligible) == 0 {
			continue
		}
		sort.SliceStable(eligible, func(i int, j int) bool {
			if eligible[i].active != eligible[j].active {
				return eligible[i].active
			}
			if eligible[i].since != eligible[j].since {
				return eligible[i].since < eligible[j].since
			}
			return eligible[i].key.channelId < eligible[j].key.channelId
		})
		selected := eligible[0]
		policy := policyByGroup[poolKey.group]
		targetPriority, targetWeight, _ := channelSmartScheduleExplorationTarget(
			poolRoutes[poolKey], selected.key.channelId, policy.ExplorationTrafficPercent,
		)
		since := selected.since
		if since <= 0 {
			since = now
		}
		var explorationUpdate *model.ChannelSmartScheduleExplorationUpdate
		if !selected.active {
			explorationUpdate = &model.ChannelSmartScheduleExplorationUpdate{
				Active: true, Since: since,
				SavedPriority: selected.savedPriority, SavedWeight: selected.savedWeight,
			}
		}
		directActions = append(directActions, channelSmartScheduleRouteDirectAction{
			key: selected.key, currentPriority: selected.currentPriority, currentWeight: selected.currentWeight,
			targetPriority: targetPriority, targetWeight: targetWeight,
			status: model.ChannelSmartScheduleStatusSkipped,
			message: fmt.Sprintf("%s，探索采样中（临时优先级 %d、权重 %d，目标流量 %.1f%%）",
				selected.reason, targetPriority, targetWeight, policy.ExplorationTrafficPercent),
			exploration: explorationUpdate,
		})
		for _, waiting := range eligible[1:] {
			if waiting.active {
				directActions = append(directActions, channelSmartScheduleRouteDirectAction{
					key: waiting.key, currentPriority: waiting.currentPriority, currentWeight: waiting.currentWeight,
					targetPriority: waiting.savedPriority, targetWeight: waiting.savedWeight,
					status:      model.ChannelSmartScheduleStatusSkipped,
					message:     waiting.reason + "，同一分组和模型仅允许一个渠道探索，已恢复等待",
					exploration: &model.ChannelSmartScheduleExplorationUpdate{},
				})
				continue
			}
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: waiting.key, currentPriority: waiting.currentPriority, currentWeight: waiting.currentWeight,
				targetPriority: waiting.currentPriority, targetWeight: waiting.currentWeight,
				status:  model.ChannelSmartScheduleStatusSkipped,
				message: waiting.reason + "，等待同一分组和模型中的当前探索完成",
			})
		}
	}

	for poolKey, candidates := range poolCandidates {
		policy := policyByGroup[poolKey.group]
		plan := planChannelSmartScheduleWithScoring(
			candidates, policy.Strategy, policy.StabilityEnabled,
			policy.ApplyMode, policy.MinSamples, forceReset, policy.Scoring,
		)
		result.Planned += len(plan.Items)
		for _, candidate := range candidates {
			reason, skipped := plan.Skipped[candidate.ChannelId]
			if !skipped {
				continue
			}
			key := routeKeyByPoolChannel[poolKey][candidate.ChannelId]
			update := channelSmartScheduleRouteStatusUpdate(
				key, model.ChannelSmartScheduleStatusSkipped, reason, nil,
				candidate.CurrentPriority, candidate.CurrentWeight, now, stabilityUpdates[key],
			)
			update.Exploration = explorationUpdates[key]
			statusUpdates = append(statusUpdates, update)
		}
		for _, item := range plan.Items {
			key := routeKeyByPoolChannel[poolKey][item.ChannelId]
			score := item.Score
			update := channelSmartScheduleRouteStatusUpdate(
				key, model.ChannelSmartScheduleStatusSucceeded, "", &score,
				item.TargetPriority, item.TargetWeight, now, stabilityUpdates[key],
			)
			update.Exploration = explorationUpdates[key]
			statusUpdates = append(statusUpdates, update)
		}
	}
	for _, action := range directActions {
		update := channelSmartScheduleRouteStatusUpdate(
			action.key, action.status, action.message, nil, action.targetPriority,
			action.targetWeight, now, action.stability,
		)
		update.Exploration = action.exploration
		statusUpdates = append(statusUpdates, update)
	}
	for index := range statusUpdates {
		key := channelSmartScheduleRouteKey{
			channelId: statusUpdates[index].ChannelId,
			group:     statusUpdates[index].Group,
			model:     statusUpdates[index].Model,
		}
		statusUpdates[index].Jitter = jitterUpdates[key]
	}

	processed := result.Skipped
	for _, update := range statusUpdates {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		key := channelSmartScheduleRouteKey{channelId: update.ChannelId, group: update.Group, model: update.Model}
		route := routeByKey[key]
		update.GuardCurrent = true
		update.ExpectedRevision = route.State.Revision
		update.ExpectedControlRevision = settings.SmartScheduleControlRevision
		update.ExpectedPriority = route.Priority
		update.ExpectedWeight = route.Weight
		update.ApplyPriorityWeight = update.Priority != route.Priority || update.Weight != route.Weight
		outcomes, applyErr := model.ApplyChannelSmartScheduleRouteResults([]model.ChannelSmartScheduleRouteResultUpdate{update})
		adjustment := channelSmartScheduleTaskAdjustment{
			ChannelId: update.ChannelId, ChannelName: route.ChannelName,
			Group: update.Group, Model: update.Model,
			OldPriority: route.Priority, NewPriority: update.Priority,
			OldWeight: route.Weight, NewWeight: update.Weight,
			Score: update.Score, Reason: update.Error,
		}
		if applyErr != nil {
			adjustment.Action = channelSmartScheduleAdjustmentFailed
			adjustment.Reason = result.recordFailure(
				update.ChannelId, route.ChannelName+" ["+update.Group+" / "+update.Model+"]", applyErr,
			)
		} else if len(outcomes) == 0 || !outcomes[0].Applied {
			result.Skipped++
			adjustment.Action = channelSmartScheduleAdjustmentSkipped
			adjustment.Reason = "调度执行期间渠道或调度配置已变化，本次结果未应用"
		} else if outcomes[0].RoutingChanged || update.Stability != nil || update.Exploration != nil {
			result.Updated++
			cacheDirty = cacheDirty || outcomes[0].RoutingChanged
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
		result.recordAdjustment(adjustment)
		processed++
		reportProgress(processed, result.Total)
	}
	reportProgress(result.Total, result.Total)
	return result, nil
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
	priority, weight := channelSmartScheduleRouteRestoreTarget(state)
	return priority, min(weight, channelMonitorSmartScheduleMinWeight)
}

func channelSmartScheduleClearExploration(state model.ChannelSmartScheduleRouteState) *model.ChannelSmartScheduleExplorationUpdate {
	if !state.ExplorationActive {
		return nil
	}
	return &model.ChannelSmartScheduleExplorationUpdate{}
}

func channelSmartScheduleExplorationTarget(
	routes []model.ChannelSmartScheduleRoute,
	explorerChannelId int,
	trafficPercent float64,
) (priority int64, weight uint, hasRegularCandidate bool) {
	var topWeight float64
	for _, route := range routes {
		if route.ChannelId == explorerChannelId || route.ChannelStatus != common.ChannelStatusEnabled ||
			!route.Enabled || !route.State.Participates() || route.State.StabilityState != "" ||
			route.State.ExplorationActive {
			continue
		}
		if !hasRegularCandidate || route.Priority > priority {
			priority = route.Priority
			topWeight = float64(route.Weight)
			hasRegularCandidate = true
		} else if route.Priority == priority {
			topWeight += float64(route.Weight)
		}
	}
	if !hasRegularCandidate {
		for _, route := range routes {
			if route.ChannelId != explorerChannelId {
				continue
			}
			priority = route.Priority
			weight = max(route.Weight, 1)
			if route.State.ExplorationActive {
				priority = route.State.ExplorationSavedPriority
				weight = max(route.State.ExplorationSavedWeight, 1)
			}
			return priority, weight, false
		}
		return channelMonitorSmartScheduleBaselinePriority, 1, false
	}
	if trafficPercent <= 0 || trafficPercent >= channelMonitorScorePercentageTotal {
		trafficPercent = 1
	}
	targetWeight := math.Ceil(topWeight * trafficPercent / (channelMonitorScorePercentageTotal - trafficPercent))
	if targetWeight < 1 {
		targetWeight = 1
	} else if targetWeight > channelMonitorSmartScheduleMaxWeight {
		targetWeight = channelMonitorSmartScheduleMaxWeight
	}
	return priority, uint(targetWeight), true
}
