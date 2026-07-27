package controller

import (
	"context"
	"fmt"
	"slices"
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
}

func runChannelSmartScheduleByRouteOnce(
	ctx context.Context,
	reportProgress func(processed, total int),
	forceReset bool,
	settings channelMonitorSettings,
	result channelSmartScheduleTaskResult,
) (channelSmartScheduleTaskResult, error) {
	if err := initializeChannelSmartScheduleParticipation(); err != nil {
		return result, err
	}
	if err := model.InitializeChannelSmartScheduleRouteStates(); err != nil {
		return result, err
	}
	routes, err := model.GetChannelSmartScheduleRoutes()
	if err != nil {
		return result, err
	}
	selectedGroups := make(map[string]struct{}, len(settings.SmartScheduleGroups))
	for _, group := range settings.SmartScheduleGroups {
		selectedGroups[group] = struct{}{}
	}
	defaultPolicy := channelSmartSchedulePolicyFromSettings(settings)
	policyByGroup := make(map[string]channelSmartSchedulePolicy, len(settings.SmartScheduleGroupPolicies))
	for _, groupPolicy := range settings.SmartScheduleGroupPolicies {
		policyByGroup[groupPolicy.Group] = groupPolicy.resolve(defaultPolicy)
	}
	selectedRoutes := make([]model.ChannelSmartScheduleRoute, 0, len(routes))
	for _, route := range routes {
		if len(selectedGroups) > 0 {
			if _, selected := selectedGroups[route.Group]; !selected {
				continue
			}
		}
		policy := defaultPolicy
		if groupPolicy, configured := policyByGroup[route.Group]; configured {
			policy = groupPolicy
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
		policy := defaultPolicy
		if groupPolicy, configured := policyByGroup[route.Group]; configured {
			policy = groupPolicy
		}
		needsPerformance = needsPerformance || policy.needsPerformance()
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
			performanceByRoute[key] = &channelSmartSchedulePerformance{
				FirstTokenSampleCount: metric.FirstTokenSampleCount,
				TPSSampleCount:        metric.TPSSampleCount,
				AverageFirstTokenMs:   metric.AverageFirstTokenMs,
				AverageTPS:            metric.AverageTPS,
			}
		}
	}
	stabilityAvailable := common.LogConsumeEnabled && constant.ErrorLogEnabled
	if needsStability && stabilityAvailable {
		metrics, metricErr := model.GetChannelMonitorRouteStabilityMetrics(ctx, performanceStart, now)
		if metricErr != nil {
			return result, metricErr
		}
		for _, metric := range metrics {
			key := channelSmartScheduleRouteKey{channelId: metric.ChannelId, group: metric.GroupName, model: metric.ModelName}
			performance := performanceByRoute[key]
			if performance == nil {
				performance = &channelSmartSchedulePerformance{}
				performanceByRoute[key] = performance
			}
			performance.StabilitySampleCount = metric.SampleCount
			if metric.SampleCount > 0 {
				value := metric.SuccessRate
				performance.Stability = &value
			}
		}
	}

	poolCandidates := make(map[channelSmartScheduleRoutePoolKey][]channelSmartScheduleCandidate)
	routeKeyByPoolChannel := make(map[channelSmartScheduleRoutePoolKey]map[int]channelSmartScheduleRouteKey)
	directActions := make([]channelSmartScheduleRouteDirectAction, 0)
	statusUpdates := make([]model.ChannelSmartScheduleRouteResultUpdate, 0, len(selectedRoutes))
	stabilityUpdates := make(map[channelSmartScheduleRouteKey]*model.ChannelSmartScheduleStabilityUpdate)
	routeByKey := make(map[channelSmartScheduleRouteKey]model.ChannelSmartScheduleRoute, len(selectedRoutes))
	for _, route := range selectedRoutes {
		policy := defaultPolicy
		if groupPolicy, configured := policyByGroup[route.Group]; configured {
			policy = groupPolicy
		}
		minimumSuccessRate := policy.MinSuccessRate / 100
		key := channelSmartScheduleRouteKey{channelId: route.ChannelId, group: route.Group, model: route.Model}
		routeByKey[key] = route
		currentPriority := route.Priority
		currentWeight := route.Weight
		if forceReset && route.ChannelStatus == common.ChannelStatusEnabled && route.Enabled && route.State.Participates() && route.State.StabilityState == "" {
			currentPriority = channelMonitorSmartScheduleBaselinePriority
			currentWeight = channelMonitorSmartScheduleMinWeight
		}
		if route.ChannelStatus != common.ChannelStatusEnabled || !route.Enabled || !route.State.Participates() {
			result.Skipped++
			continue
		}

		if route.State.StabilityState != "" && (!policy.StabilityEnabled || !stabilityAvailable) {
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				targetPriority: currentPriority, targetWeight: currentWeight,
				status:  model.ChannelSmartScheduleStatusSkipped,
				message: "稳定性保护未启用或统计不可用，保持当前安全状态",
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
					message: fmt.Sprintf("低成功率降级中，将于 %s 后试放",
						time.Unix(route.State.StabilityUntil, 0).Format("2006-01-02 15:04:05")),
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
			})
			continue
		case model.ChannelSmartScheduleStabilityProbing, "":
		default:
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				targetPriority: currentPriority, targetWeight: currentWeight,
				status:  model.ChannelSmartScheduleStatusSkipped,
				message: "稳定性调度状态无效，保持当前安全状态",
			})
			continue
		}

		performance := performanceByRoute[key]
		if policy.StabilityEnabled && stabilityAvailable && route.State.StabilitySince > performanceStart {
			metric, metricErr := model.GetChannelMonitorRouteStabilityMetric(
				ctx, route.State.StabilitySince, route.ChannelId, route.Group, route.Model,
			)
			if metricErr != nil {
				return result, metricErr
			}
			if performance == nil {
				performance = &channelSmartSchedulePerformance{}
				performanceByRoute[key] = performance
			}
			performance.StabilitySampleCount = metric.SampleCount
			performance.Stability = nil
			if metric.SampleCount > 0 {
				value := metric.SuccessRate
				performance.Stability = &value
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
					status:  model.ChannelSmartScheduleStatusSkipped,
					message: fmt.Sprintf("稳定性试放样本不足（%d/%d）", sampleCount, policy.MinSamples),
				})
				continue
			}
			if *performance.Stability < minimumSuccessRate {
				directActions = append(directActions, channelSmartScheduleRouteDirectAction{
					key: key, currentPriority: currentPriority, currentWeight: currentWeight,
					targetPriority: channelMonitorSmartScheduleDegradedPriority,
					targetWeight:   channelMonitorSmartScheduleDegradedWeight,
					status:         model.ChannelSmartScheduleStatusSucceeded,
					message: fmt.Sprintf("试放成功率 %.1f%% 低于 %.1f%%，再次降级",
						*performance.Stability*100, policy.MinSuccessRate),
					stability: &model.ChannelSmartScheduleStabilityUpdate{
						State:         model.ChannelSmartScheduleStabilityDegraded,
						Until:         now + int64(policy.CooldownMinutes*60),
						SavedPriority: route.State.StabilitySavedPriority,
						SavedWeight:   route.State.StabilitySavedWeight,
					},
				})
				continue
			}
			targetPriority, targetWeight := channelSmartScheduleRouteRestoreTarget(route.State)
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				targetPriority: targetPriority, targetWeight: targetWeight,
				status: model.ChannelSmartScheduleStatusSucceeded,
				message: fmt.Sprintf("试放成功率 %.1f%% 已达到 %.1f%%，已解除保护并恢复原优先级和权重",
					*performance.Stability*100, policy.MinSuccessRate),
				stability: &model.ChannelSmartScheduleStabilityUpdate{Since: route.State.StabilitySince},
			})
			continue
		} else if route.State.StabilityState == "" && route.State.StabilitySince > 0 &&
			route.State.StabilitySince <= performanceStart {
			stabilityUpdates[key] = &model.ChannelSmartScheduleStabilityUpdate{}
		}

		if policy.StabilityEnabled && route.State.StabilityState == "" && performance != nil && performance.Stability != nil &&
			performance.StabilitySampleCount >= int64(policy.MinSamples) &&
			*performance.Stability < minimumSuccessRate {
			savedPriority, savedWeight := channelSmartScheduleSavedTarget(currentPriority, currentWeight)
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				targetPriority: channelMonitorSmartScheduleDegradedPriority,
				targetWeight:   channelMonitorSmartScheduleDegradedWeight,
				status:         model.ChannelSmartScheduleStatusSucceeded,
				message: fmt.Sprintf("成功率 %.1f%% 低于 %.1f%%，已在当前分组和模型降级至优先级 0、权重 0",
					*performance.Stability*100, policy.MinSuccessRate),
				stability: &model.ChannelSmartScheduleStabilityUpdate{
					State:         model.ChannelSmartScheduleStabilityDegraded,
					Until:         now + int64(policy.CooldownMinutes*60),
					SavedPriority: savedPriority, SavedWeight: savedWeight,
				},
			})
			continue
		}

		monitor := monitorByChannel[route.ChannelId]
		var ratio *float64
		if monitor.UpdatedTime > 0 && validateChannelMonitorRatio(&monitor.Ratio) {
			value, _, conversionErr := channelMonitorCostRatioFromModel(monitor, monitor.Ratio)
			if conversionErr != nil && policy.needsRatio() {
				statusUpdates = append(statusUpdates, channelSmartScheduleRouteStatusUpdate(
					key, model.ChannelSmartScheduleStatusSkipped, "成本倍率换算失败："+conversionErr.Error(),
					nil, currentPriority, currentWeight, now, stabilityUpdates[key],
				))
				continue
			}
			if conversionErr == nil {
				ratio = &value
			}
		}
		candidate := channelSmartScheduleCandidate{
			ChannelId: route.ChannelId, CurrentPriority: currentPriority, CurrentWeight: currentWeight,
			Ratio: ratio, StabilityAvailable: stabilityAvailable,
		}
		if performance != nil {
			candidate.FirstTokenMs = performance.AverageFirstTokenMs
			candidate.TPS = performance.AverageTPS
			candidate.FirstTokenSampleCount = performance.FirstTokenSampleCount
			candidate.TPSSampleCount = performance.TPSSampleCount
			candidate.Stability = performance.Stability
			candidate.StabilitySampleCount = performance.StabilitySampleCount
		}
		if reason := channelSmartScheduleCandidateSkipReasonWithScoring(
			candidate, policy.Strategy, policy.StabilityEnabled,
			policy.MinSamples, policy.Scoring,
		); reason != "" && channelSmartScheduleCandidateNeedsExplorationWithScoring(
			candidate, policy.Strategy, policy.StabilityEnabled,
			policy.MinSamples, policy.Scoring,
		) {
			directActions = append(directActions, channelSmartScheduleRouteDirectAction{
				key: key, currentPriority: currentPriority, currentWeight: currentWeight,
				targetPriority: channelMonitorSmartScheduleBaselinePriority,
				targetWeight:   channelMonitorSmartScheduleMinWeight,
				status:         model.ChannelSmartScheduleStatusSkipped,
				message:        reason + "，使用探索基线（优先级 80、权重 10）",
			})
			continue
		}
		poolKey := channelSmartScheduleRoutePoolKey{group: route.Group, model: route.Model}
		poolCandidates[poolKey] = append(poolCandidates[poolKey], candidate)
		if routeKeyByPoolChannel[poolKey] == nil {
			routeKeyByPoolChannel[poolKey] = make(map[int]channelSmartScheduleRouteKey)
		}
		routeKeyByPoolChannel[poolKey][route.ChannelId] = key
	}

	for poolKey, candidates := range poolCandidates {
		policy := defaultPolicy
		if groupPolicy, configured := policyByGroup[poolKey.group]; configured {
			policy = groupPolicy
		}
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
			statusUpdates = append(statusUpdates, channelSmartScheduleRouteStatusUpdate(
				key, model.ChannelSmartScheduleStatusSkipped, reason, nil,
				candidate.CurrentPriority, candidate.CurrentWeight, now, stabilityUpdates[key],
			))
		}
		for _, item := range plan.Items {
			key := routeKeyByPoolChannel[poolKey][item.ChannelId]
			score := item.Score
			statusUpdates = append(statusUpdates, channelSmartScheduleRouteStatusUpdate(
				key, model.ChannelSmartScheduleStatusSucceeded, "", &score,
				item.TargetPriority, item.TargetWeight, now, stabilityUpdates[key],
			))
		}
	}
	for _, action := range directActions {
		statusUpdates = append(statusUpdates, channelSmartScheduleRouteStatusUpdate(
			action.key, action.status, action.message, nil, action.targetPriority,
			action.targetWeight, now, action.stability,
		))
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
		if applyErr != nil {
			result.recordFailure(update.ChannelId, route.ChannelName+" ["+update.Group+" / "+update.Model+"]", applyErr)
		} else if len(outcomes) == 0 || !outcomes[0].Applied {
			result.Skipped++
		} else if outcomes[0].RoutingChanged || update.Stability != nil {
			result.Updated++
			cacheDirty = cacheDirty || outcomes[0].RoutingChanged
		} else if update.Status == model.ChannelSmartScheduleStatusSkipped {
			result.Skipped++
		} else {
			result.Unchanged++
		}
		processed++
		reportProgress(processed, result.Total)
	}
	reportProgress(result.Total, result.Total)
	return result, nil
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
