package controller

import (
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func channelSmartScheduleApplyCurrentWindowScores(
	responses []channelSmartScheduleRouteResponse,
	routes []model.ChannelSmartScheduleRoute,
	policyByGroup map[string]channelSmartSchedulePolicy,
	performanceItems []model.ChannelMonitorRoutePerformanceMetric,
	stabilityByRoute map[channelSmartScheduleRouteKey]model.ChannelMonitorRouteStabilityMetric,
	sampleMetricCache *channelSmartScheduleSampleMetricCache,
	stabilityStart int64,
	now int64,
	logStabilityAvailable bool,
) {
	responseIndexByRoute := make(map[channelSmartScheduleRouteKey]int, len(responses))
	for index := range responses {
		response := responses[index]
		responseIndexByRoute[channelSmartScheduleRouteKey{
			channelId: response.ChannelId,
			group:     response.Group,
			model:     response.Model,
		}] = index
	}
	performanceByRoute := make(map[channelSmartScheduleRouteKey]model.ChannelMonitorRoutePerformanceMetric, len(performanceItems))
	for _, metric := range performanceItems {
		performanceByRoute[channelSmartScheduleRouteKey{
			channelId: metric.ChannelId,
			group:     metric.GroupName,
			model:     metric.ModelName,
		}] = metric
	}

	candidatesByPool := make(map[channelSmartScheduleRoutePoolKey][]channelSmartScheduleCandidate)
	routeKeyByPoolChannel := make(map[channelSmartScheduleRoutePoolKey]map[int]channelSmartScheduleRouteKey)
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
		modelKey := channelSmartScheduleModelKey{
			channelId: route.ChannelId,
			model:     ratio_setting.FormatMatchingModelName(route.Model),
		}
		currentPriority := route.Priority
		currentWeight := route.Weight
		if route.State.TemporaryTrafficKind != "" && route.State.BaseRank > 0 {
			currentPriority = route.State.BasePriority
			currentWeight = route.State.BaseWeight
		}
		candidate := channelSmartScheduleCandidate{
			ChannelId:            route.ChannelId,
			CurrentPriority:      currentPriority,
			CurrentWeight:        currentWeight,
			Ratio:                route.CostRatio,
			CostRatio:            route.CostRatio,
			GroupRatio:           route.GroupRatio,
			GrossMargin:          route.GrossMargin,
			EconomicRole:         route.EconomicRole,
			ManualPrimary:        route.State.ManualPrimaryUntil > now,
			PreviousBaseRank:     route.State.BaseRank,
			ManualTargetPriority: currentPriority,
			StabilityAvailable: logStabilityAvailable ||
				policy.SampleMode == channelMonitorSmartScheduleSampleProbe ||
				sampleMetricCache.metrics(modelKey, stabilityStart).SampleCount > 0,
		}
		if performance, exists := performanceByRoute[key]; exists {
			candidate.SampleGroupCount = performance.GroupCount
			candidate.FirstTokenMs = performance.AverageFirstTokenMs
			if policy.JitterEnabled && performance.WinsorizedAverageFirstTokenMs != nil {
				candidate.FirstTokenMs = performance.WinsorizedAverageFirstTokenMs
			}
			candidate.TPS = performance.AverageTPS
			candidate.FirstTokenSampleCount = performance.FirstTokenSampleCount
			candidate.TPSSampleCount = performance.TPSSampleCount
		}
		if stability, exists := stabilityByRoute[key]; exists {
			candidate.SampleGroupCount = max(candidate.SampleGroupCount, stability.GroupCount)
			candidate.Stability = stability.StabilityScore
			candidate.StabilitySampleCount = stability.SampleCount
		}
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
		}
	}
}
