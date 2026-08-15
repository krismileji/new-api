package controller

import (
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

func channelSmartScheduleApplyCurrentWindowScores(
	responses []channelSmartScheduleRouteResponse,
	routes []model.ChannelSmartScheduleRoute,
	policyByGroup map[string]channelSmartSchedulePolicy,
	stabilityStart int64,
	now int64,
) {
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
	snapshotByRoute := make(map[channelSmartScheduleRouteKey]service.ChannelMonitorRealtimeSnapshot)
	for _, route := range routes {
		policy, configured := policyByGroup[route.Group]
		if !configured || (len(policy.Models) > 0 && !slices.Contains(policy.Models, route.Model)) ||
			route.ChannelStatus != common.ChannelStatusEnabled || !route.Enabled ||
			!route.State.Participates() || route.TrafficPaused(now) {
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
		healthMetric, healthSnapshot := channelSmartScheduleRealtimeAdaptiveMetric(
			route.ChannelId,
			route.Model,
			now-int64(policy.AdaptiveSamplingWindowSeconds),
			route.SharedSamples.ObservationSince,
			policy.AdaptiveSamplingWindowRequests,
			policy.AdaptiveSamplingFirstTokenWarningSeconds,
			policy.AdaptiveSamplingFirstTokenCriticalSeconds,
		)
		health := channelSmartScheduleEvaluateHealth(route.State, healthMetric, policy)
		performanceMetric, performanceSnapshot := channelSmartScheduleRealtimeAdaptiveMetric(
			route.ChannelId,
			route.Model,
			performanceStart,
			route.SharedSamples.ObservationSince,
			0,
			policy.AdaptiveSamplingFirstTokenWarningSeconds,
			policy.AdaptiveSamplingFirstTokenCriticalSeconds,
		)
		stabilityMetric, _ := channelSmartScheduleRealtimeAdaptiveMetric(
			route.ChannelId,
			route.Model,
			stabilityStart,
			route.SharedSamples.ObservationSince,
			0,
			policy.AdaptiveSamplingFirstTokenWarningSeconds,
			policy.AdaptiveSamplingFirstTokenCriticalSeconds,
		)
		firstTokenMs, tps := channelSmartScheduleRealtimeAverage(performanceMetric)
		stability, stabilitySampleCount := channelSmartScheduleStabilityScore(
			stabilityMetric.StabilitySuccessCount,
			stabilityMetric.StabilityFailureCount,
			stabilityMetric.StabilityFinalFailureCount,
			stabilityMetric.RetryFailureDurationBuckets,
			policy,
		)
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
			StabilityAvailable:                    stabilitySampleCount > 0,
			FirstTokenMs:                          firstTokenMs,
			TPS:                                   tps,
			FirstTokenSampleCount:                 int(performanceMetric.FirstTokenCount),
			TPSSampleCount:                        int(performanceMetric.TPSSampleCount),
			Stability:                             stability,
			StabilitySampleCount:                  stabilitySampleCount,
		}
		if performanceMetric.RequestCount > 0 {
			candidate.SampleGroupCount = 1
		}
		snapshot := performanceSnapshot
		if healthSnapshot.EventWatermark > snapshot.EventWatermark {
			snapshot = healthSnapshot
		}
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
}
