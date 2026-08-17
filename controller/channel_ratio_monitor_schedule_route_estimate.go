package controller

import (
	"context"
	"fmt"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

func channelSmartScheduleApplyCurrentWindowScores(
	ctx context.Context,
	responses []channelSmartScheduleRouteResponse,
	routes []model.ChannelSmartScheduleRoute,
	policyByGroup map[string]channelSmartSchedulePolicy,
	now int64,
) error {
	settings := getChannelMonitorRuntimeSettings()
	performanceStart := now - int64(settings.SmartSchedulePerformanceWindowMinutes*60)
	responseIndexByRoute := make(map[channelSmartScheduleRouteKey]int, len(responses))
	for index := range responses {
		response := responses[index]
		responseIndexByRoute[channelSmartScheduleRouteKey{
			channelId: response.ChannelId,
			group:     response.Group,
			model:     response.Model,
		}] = index
	}

	candidatesByPool := make(map[channelSmartScheduleRoutePoolKey][]channelSmartScheduleCandidate)
	routeKeyByPoolChannel := make(map[channelSmartScheduleRoutePoolKey]map[int]channelSmartScheduleRouteKey)
	snapshotByRoute := make(map[channelSmartScheduleRouteKey]service.ChannelMonitorRedisRouteHealthSnapshot)
	for _, route := range routes {
		policy, configured := policyByGroup[route.Group]
		if !configured || (len(policy.Models) > 0 && !slices.Contains(policy.Models, route.Model)) ||
			route.ChannelStatus != common.ChannelStatusEnabled || !route.Enabled ||
			!route.State.Participates() || route.TrafficPaused(now) ||
			route.State.RuntimeProtectionUntil > now || route.State.StabilityState != "" {
			continue
		}
		key := channelSmartScheduleRouteKey{
			channelId: route.ChannelId,
			group:     route.Group,
			model:     route.Model,
		}
		poolKey := channelSmartScheduleRoutePoolKey{group: route.Group, model: route.Model}
		currentPriority := route.Priority
		currentWeight := route.Weight
		if route.State.TemporaryTrafficKind != "" && route.State.BaseRank > 0 {
			currentPriority = route.State.BasePriority
			currentWeight = route.State.BaseWeight
		}
		adaptiveWindowStart := now - int64(policy.AdaptiveSamplingWindowSeconds)
		stabilityStart := now - int64(policy.StabilityWindowMinutes*60)
		readWindowStart := min(adaptiveWindowStart, performanceStart, stabilityStart)
		routeEvents, snapshot, err := channelSmartScheduleRealtimeEvents(
			ctx, route.ChannelId, route.Model, readWindowStart,
			route.SharedSamples.ObservationSince, 0,
		)
		if err != nil {
			return fmt.Errorf("读取渠道 %d 模型 %s 的 Redis 健康窗口失败: %w", route.ChannelId, route.Model, err)
		}
		healthMetric := channelSmartScheduleRealtimeAdaptiveMetricFromEvents(
			channelSmartScheduleEventsForWindow(
				routeEvents, adaptiveWindowStart, policy.AdaptiveSamplingWindowRequests,
			),
			policy.AdaptiveSamplingFirstTokenWarningSeconds,
			policy.AdaptiveSamplingFirstTokenCriticalSeconds,
		)
		health := channelSmartScheduleEvaluateHealth(route.State, healthMetric, policy)
		performanceMetric := channelSmartScheduleRealtimeAdaptiveMetricFromEvents(
			channelSmartScheduleEventsForWindow(routeEvents, performanceStart, 0),
			policy.AdaptiveSamplingFirstTokenWarningSeconds,
			policy.AdaptiveSamplingFirstTokenCriticalSeconds,
		)
		stabilityMetric := channelSmartScheduleRealtimeAdaptiveMetricFromEvents(
			channelSmartScheduleEventsForWindow(routeEvents, stabilityStart, 0),
			policy.AdaptiveSamplingFirstTokenWarningSeconds,
			policy.AdaptiveSamplingFirstTokenCriticalSeconds,
		)
		firstTokenMs, tps := channelSmartScheduleRealtimeAverage(performanceMetric)
		performance := &channelSmartSchedulePerformance{
			FirstTokenSampleCount:                int(performanceMetric.FirstTokenCount),
			FirstTokenDurationSampleCount:        performanceMetric.FirstTokenCount,
			TPSSampleCount:                       int(performanceMetric.TPSSampleCount),
			AverageFirstTokenMs:                  firstTokenMs,
			AverageTPS:                           tps,
			LastUsedTime:                         max(performanceMetric.LastUsedTime, stabilityMetric.LastUsedTime),
			StabilitySuccessCount:                stabilityMetric.StabilitySuccessCount,
			StabilityFailureCount:                stabilityMetric.StabilityFailureCount,
			StabilityFinalFailureCount:           stabilityMetric.StabilityFinalFailureCount,
			StabilityRetryFailureCount:           stabilityMetric.StabilityRetryFailureCount,
			StabilityRetryFailureDurationTotalMs: stabilityMetric.RetryFailureDurationTotalMs,
			StabilityFailureDurationBuckets: append(
				[]model.ChannelMonitorFailureDurationBucket(nil),
				stabilityMetric.RetryFailureDurationBuckets...,
			),
			FirstTokenDurationBuckets: append(
				[]model.ChannelMonitorDurationBucket(nil),
				performanceMetric.FirstTokenDurationBuckets...,
			),
		}
		if performanceMetric.RequestCount > 0 || stabilityMetric.RequestCount > 0 {
			performance.SampleGroupCount = 1
		}
		performance.Stability, performance.StabilitySampleCount = channelSmartScheduleStabilityScore(
			performance.StabilitySuccessCount,
			performance.StabilityFailureCount,
			performance.StabilityFinalFailureCount,
			performance.StabilityFailureDurationBuckets,
			policy,
		)
		channelSmartScheduleApplyJitterMeasurement(performance, policy)
		candidate := channelSmartScheduleCandidate{
			ChannelId:                             route.ChannelId,
			CurrentPriority:                       currentPriority,
			CurrentWeight:                         currentWeight,
			Ratio:                                 route.CostRatio,
			CostRatio:                             route.CostRatio,
			GroupRatio:                            route.GroupRatio,
			GrossMargin:                           route.GrossMargin,
			EconomicRole:                          route.EconomicRole,
			ManualPrimary:                         route.State.ManualPrimaryUntil > now,
			PreviousBaseRank:                      route.State.BaseRank,
			ManualTargetPriority:                  currentPriority,
			HealthState:                           health.State,
			HealthPressure:                        health.Pressure,
			HealthErrorPressure:                   health.ErrorPressure,
			HealthLatencyPressure:                 health.LatencyPressure,
			HealthEvidence:                        health.Evidence,
			HealthSampleCount:                     health.SampleCount,
			HealthLastSampleAt:                    health.LastSampleAt,
			HealthErrorRequestPercent:             health.ErrorRequestPercent,
			HealthRiskRequestPercent:              health.RiskRequestPercent,
			HealthFirstTokenWarningRequestPercent: health.FirstTokenWarningRequestPercent,
			HealthHealthyRequestPercent:           health.HealthyRequestPercent,
			HealthWindowMinutes:                   policy.AdaptiveSamplingWindowMinutes,
			HealthWindowRequests:                  policy.AdaptiveSamplingWindowRequests,
			MinComparableChannels:                 policy.AdaptiveSamplingMinComparableChannels,
			StabilityAvailable:                    stabilityMetric.RequestCount > 0,
			FirstTokenMs:                          performance.AverageFirstTokenMs,
			TPS:                                   performance.AverageTPS,
			FirstTokenSampleCount:                 performance.FirstTokenSampleCount,
			TPSSampleCount:                        performance.TPSSampleCount,
			Stability:                             performance.Stability,
			StabilitySampleCount:                  performance.StabilitySampleCount,
		}
		candidate.SampleGroupCount = performance.SampleGroupCount
		if policy.JitterEnabled && performance.WinsorizedAverageFirstTokenMs != nil {
			candidate.FirstTokenMs = performance.WinsorizedAverageFirstTokenMs
		}
		candidate.SampleDebt = channelSmartScheduleCandidateSampleDebt(
			candidate, policy.Strategy, policy.StabilityEnabled, policy.Scoring, policy.MinSamples,
		)
		snapshotByRoute[key] = snapshot
		candidatesByPool[poolKey] = append(candidatesByPool[poolKey], candidate)
		if routeKeyByPoolChannel[poolKey] == nil {
			routeKeyByPoolChannel[poolKey] = make(map[int]channelSmartScheduleRouteKey)
		}
		routeKeyByPoolChannel[poolKey][route.ChannelId] = key
	}

	for poolKey, candidates := range candidatesByPool {
		policy := policyByGroup[poolKey.group]
		plan := planChannelSmartScheduleWithScoring(
			candidates,
			policy.Strategy,
			policy.StabilityEnabled,
			policy.ApplyMode,
			policy.MinSamples,
			false,
			policy.Scoring,
		)
		if policy.AdaptiveSamplingEnabled {
			channelSmartScheduleApplySwitchConfirmation(&plan, candidates, policy, false)
		}
		for channelId, details := range plan.Details {
			key := routeKeyByPoolChannel[poolKey][channelId]
			responseIndex, exists := responseIndexByRoute[key]
			if !exists || details == nil {
				continue
			}
			responses[responseIndex].CurrentWindowScoreDetails = details
			responses[responseIndex].CurrentWindowScore = channelSmartScheduleCopyFloat(details.FinalScore)
			snapshot := snapshotByRoute[key]
			channelSmartScheduleAttachRealtimeWindow(details, snapshot)
		}
	}
	return nil
}
