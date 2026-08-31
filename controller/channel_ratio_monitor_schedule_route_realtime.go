package controller

import (
	"context"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type channelSmartScheduleRealtimeRouteMetrics struct {
	performance         *model.ChannelMonitorRoutePerformanceMetric
	businessPerformance *model.ChannelMonitorRoutePerformanceMetric
	stability           *model.ChannelMonitorRouteStabilityMetric
	sampleItem          channelSmartScheduleSampleItem
	events              []model.ChannelMonitorEvent
	snapshot            service.ChannelMonitorRedisRouteHealthSnapshot
}

type channelSmartScheduleMetricCoverageResponse struct {
	AggregationEnabled            bool  `json:"aggregation_enabled"`
	AggregatedFrom                int64 `json:"aggregated_from"`
	AggregatedThrough             int64 `json:"aggregated_through"`
	PerformanceWindowStart        int64 `json:"performance_window_start"`
	StabilityWindowStart          int64 `json:"stability_window_start"`
	PerformanceWindowComplete     bool  `json:"performance_window_complete"`
	StabilityWindowComplete       bool  `json:"stability_window_complete"`
	ConfiguredRetentionDays       int   `json:"configured_retention_days"`
	RequiredRetentionMinutes      int   `json:"required_retention_minutes"`
	ConfiguredRetentionSufficient bool  `json:"configured_retention_sufficient"`
	RealtimeRetentionMinutes      int   `json:"realtime_retention_minutes"`
	RealtimeSampleLimit           int   `json:"realtime_sample_limit"`
	SampleLimitTruncated          bool  `json:"sample_limit_truncated"`
	SampleLimitCutoffAt           int64 `json:"sample_limit_cutoff_at"`
}

func channelSmartScheduleRealtimeRouteMetricView(
	ctx context.Context,
	route model.ChannelSmartScheduleRoute,
	policy channelSmartSchedulePolicy,
	performanceStart int64,
	generatedAt int64,
) (channelSmartScheduleRealtimeRouteMetrics, error) {
	routeStabilityStart := generatedAt - int64(policy.StabilityWindowMinutes*60)
	if route.State.StabilityState == model.ChannelSmartScheduleStabilityProbing &&
		route.State.StabilitySince > routeStabilityStart {
		routeStabilityStart = route.State.StabilitySince
	}
	adaptiveWindowStart := generatedAt - int64(policy.AdaptiveSamplingWindowSeconds)
	events, snapshot, err := channelSmartScheduleRealtimeEvents(
		ctx, route.ChannelId, route.Model, min(performanceStart, routeStabilityStart, adaptiveWindowStart),
		route.SharedSamples.ObservationSince, 0,
	)
	if err != nil {
		return channelSmartScheduleRealtimeRouteMetrics{}, err
	}
	return channelSmartScheduleRealtimeRouteMetricViewFromEvents(
		route, policy, performanceStart, generatedAt, events, snapshot,
	), nil
}

func channelSmartScheduleRealtimeRouteMetricViews(
	ctx context.Context,
	routes []model.ChannelSmartScheduleRoute,
	policyByGroup map[string]channelSmartSchedulePolicy,
	performanceStart int64,
	generatedAt int64,
) (map[channelSmartScheduleRouteKey]channelSmartScheduleRealtimeRouteMetrics, error) {
	views := make(map[channelSmartScheduleRouteKey]channelSmartScheduleRealtimeRouteMetrics, len(routes))
	if len(routes) == 0 {
		return views, nil
	}
	redisRoutes := make([]service.ChannelMonitorRedisRouteHealthRouteKey, 0, len(routes))
	for _, route := range routes {
		key, valid := service.NewChannelMonitorRedisRouteHealthRouteKey(route.ChannelId, route.Model)
		if valid {
			redisRoutes = append(redisRoutes, key)
		}
	}
	batch, err := service.GetChannelMonitorRedisRouteHealthWindows(ctx, redisRoutes)
	if err != nil {
		return nil, err
	}
	for _, route := range routes {
		policy := policyByGroup[route.Group]
		routeStabilityStart := generatedAt - int64(policy.StabilityWindowMinutes*60)
		if route.State.StabilityState == model.ChannelSmartScheduleStabilityProbing &&
			route.State.StabilitySince > routeStabilityStart {
			routeStabilityStart = route.State.StabilitySince
		}
		adaptiveWindowStart := generatedAt - int64(policy.AdaptiveSamplingWindowSeconds)
		windowStart := min(performanceStart, routeStabilityStart, adaptiveWindowStart)
		snapshot := service.ChannelMonitorRedisRouteHealthSnapshot{
			CoverageStart:       batch.CoverageStart,
			ProjectionStartedAt: batch.ProjectionStartedAt,
		}
		var events []model.ChannelMonitorEvent
		redisKey, valid := service.NewChannelMonitorRedisRouteHealthRouteKey(route.ChannelId, route.Model)
		if valid {
			if window, available := batch.Windows[redisKey]; available {
				snapshot = window.Snapshot
				events = channelSmartScheduleRedisWindowEvents(
					window,
					route.ChannelId,
					route.Model,
					windowStart,
					route.SharedSamples.ObservationSince,
					0,
					0,
				)
			}
		}
		key := channelSmartScheduleRouteKey{channelId: route.ChannelId, group: route.Group, model: route.Model}
		views[key] = channelSmartScheduleRealtimeRouteMetricViewFromEvents(
			route, policy, performanceStart, generatedAt, events, snapshot,
		)
	}
	return views, nil
}

func channelSmartScheduleRealtimeRouteMetricViewFromEvents(
	route model.ChannelSmartScheduleRoute,
	policy channelSmartSchedulePolicy,
	performanceStart int64,
	generatedAt int64,
	events []model.ChannelMonitorEvent,
	snapshot service.ChannelMonitorRedisRouteHealthSnapshot,
) channelSmartScheduleRealtimeRouteMetrics {
	routeStabilityStart := generatedAt - int64(policy.StabilityWindowMinutes*60)
	if route.State.StabilityState == model.ChannelSmartScheduleStabilityProbing &&
		route.State.StabilitySince > routeStabilityStart {
		routeStabilityStart = route.State.StabilitySince
	}
	performanceEvents := channelSmartScheduleEventsForWindow(events, performanceStart, 0)
	businessEvents := make([]model.ChannelMonitorEvent, 0, len(performanceEvents))
	performanceSampleEvents := make([]model.ChannelMonitorEvent, 0, len(performanceEvents))
	for _, event := range performanceEvents {
		if event.Source == model.ChannelMonitorEventSourceBusiness {
			businessEvents = append(businessEvents, event)
			continue
		}
		performanceSampleEvents = append(performanceSampleEvents, event)
	}

	view := channelSmartScheduleRealtimeRouteMetrics{events: events, snapshot: snapshot}
	if metric, available := channelSmartScheduleRealtimePerformanceMetric(
		route, performanceEvents, policy,
	); available {
		view.performance = &metric
	}
	if metric, available := channelSmartScheduleRealtimePerformanceMetric(
		route, businessEvents, policy,
	); available {
		view.businessPerformance = &metric
	}

	stabilityEvents := channelSmartScheduleEventsForWindow(events, routeStabilityStart, 0)
	stabilitySampleEvents := make([]model.ChannelMonitorEvent, 0, len(stabilityEvents))
	for _, event := range stabilityEvents {
		if event.Source != model.ChannelMonitorEventSourceBusiness {
			stabilitySampleEvents = append(stabilitySampleEvents, event)
		}
	}
	if policy.StabilityEnabled {
		if metric, available := channelSmartScheduleRealtimeStabilityMetric(
			route, stabilityEvents, policy,
		); available {
			view.stability = &metric
		}
	}
	view.sampleItem = channelSmartScheduleSampleItem{
		ChannelId: route.ChannelId,
		Model:     route.Model,
		PerformanceWindow: channelSmartScheduleRealtimeSampleState(
			route, performanceStart, performanceSampleEvents,
		),
		StabilityWindow: channelSmartScheduleRealtimeSampleState(
			route, routeStabilityStart, stabilitySampleEvents,
		),
	}
	return view
}

func channelSmartScheduleRealtimePerformanceMetric(
	route model.ChannelSmartScheduleRoute,
	events []model.ChannelMonitorEvent,
	policy channelSmartSchedulePolicy,
) (model.ChannelMonitorRoutePerformanceMetric, bool) {
	if len(events) == 0 {
		return model.ChannelMonitorRoutePerformanceMetric{}, false
	}
	adaptive := channelSmartScheduleRealtimeAdaptiveMetricFromEvents(
		events,
		policy.AdaptiveSamplingFirstTokenWarningSeconds,
		policy.AdaptiveSamplingFirstTokenCriticalSeconds,
	)
	firstTokenMs, tps := channelSmartScheduleRealtimeAverage(adaptive)
	metric := model.ChannelMonitorRoutePerformanceMetric{
		ChannelId:                     route.ChannelId,
		GroupName:                     route.Group,
		ModelName:                     route.Model,
		GroupCount:                    channelSmartScheduleRealtimeGroupCount(events),
		SampleCount:                   len(events),
		FirstTokenSampleCount:         int(adaptive.FirstTokenCount),
		FirstTokenDurationSampleCount: adaptive.FirstTokenCount,
		TPSSampleCount:                int(adaptive.TPSSampleCount),
		AverageFirstTokenMs:           firstTokenMs,
		AverageTPS:                    tps,
		LastUsedTime:                  adaptive.LastUsedTime,
		FirstTokenDurationBuckets: append(
			[]model.ChannelMonitorDurationBucket(nil),
			adaptive.FirstTokenDurationBuckets...,
		),
	}
	metric.FirstTokenDurationSampleCount, metric.FirstTokenP50Ms, metric.FirstTokenP95Ms,
		metric.WinsorizedAverageFirstTokenMs = model.SummarizeChannelMonitorDurationBuckets(
		metric.FirstTokenDurationBuckets,
	)
	for _, event := range events {
		if event.Source != model.ChannelMonitorEventSourceBusiness {
			continue
		}
		inputTokens := int64(0)
		if event.InputTokens != nil {
			inputTokens = *event.InputTokens
		} else if event.PromptTokens != nil {
			inputTokens = *event.PromptTokens
		}
		if inputTokens > 0 {
			metric.CacheSampleCount++
			// Cache utilization is token-weighted and only includes streams;
			// cache hit-rate samples remain request-based.
			if event.IsStream {
				metric.InputTokens += inputTokens
			}
		}
		if event.CacheReadTokens != nil && *event.CacheReadTokens > 0 {
			metric.CacheHitCount++
			if event.IsStream {
				metric.CacheReadTokens += *event.CacheReadTokens
			}
		}
	}
	if metric.CacheSampleCount > 0 {
		metric.CacheHitRate = float64(metric.CacheHitCount) / float64(metric.CacheSampleCount)
	}
	if metric.InputTokens > 0 {
		metric.CacheUtilizationRate = float64(metric.CacheReadTokens) / float64(metric.InputTokens)
	}
	return metric, true
}

func channelSmartScheduleRealtimeStabilityMetric(
	route model.ChannelSmartScheduleRoute,
	events []model.ChannelMonitorEvent,
	policy channelSmartSchedulePolicy,
) (model.ChannelMonitorRouteStabilityMetric, bool) {
	if len(events) == 0 {
		return model.ChannelMonitorRouteStabilityMetric{}, false
	}
	adaptive := channelSmartScheduleRealtimeAdaptiveMetricFromEvents(
		events,
		policy.AdaptiveSamplingFirstTokenWarningSeconds,
		policy.AdaptiveSamplingFirstTokenCriticalSeconds,
	)
	performance := &channelSmartSchedulePerformance{
		SampleGroupCount:                     channelSmartScheduleRealtimeGroupCount(events),
		StabilitySuccessCount:                adaptive.StabilitySuccessCount,
		StabilityFailureCount:                adaptive.StabilityFailureCount,
		StabilityFinalFailureCount:           adaptive.StabilityFinalFailureCount,
		StabilityRetryFailureCount:           adaptive.StabilityRetryFailureCount,
		StabilityFailureDurationBuckets:      append([]model.ChannelMonitorFailureDurationBucket(nil), adaptive.RetryFailureDurationBuckets...),
		StabilityRetryFailureDurationTotalMs: adaptive.RetryFailureDurationTotalMs,
		FirstTokenDurationSampleCount:        adaptive.FirstTokenCount,
		FirstTokenDurationBuckets:            append([]model.ChannelMonitorDurationBucket(nil), adaptive.FirstTokenDurationBuckets...),
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
		return model.ChannelMonitorRouteStabilityMetric{}, false
	}
	metric := model.ChannelMonitorRouteStabilityMetric{
		ChannelId:                   route.ChannelId,
		GroupName:                   route.Group,
		ModelName:                   route.Model,
		GroupCount:                  performance.SampleGroupCount,
		SuccessCount:                performance.StabilitySuccessCount,
		FailureCount:                performance.StabilityFailureCount,
		FinalFailureCount:           performance.StabilityFinalFailureCount,
		RetryFailureCount:           performance.StabilityRetryFailureCount,
		SampleCount:                 performance.StabilitySampleCount,
		RetryFailureDurationBuckets: append([]model.ChannelMonitorFailureDurationBucket(nil), performance.StabilityFailureDurationBuckets...),
		JitterAvailable:             performance.JitterAvailable,
		FirstTokenP50Ms:             performance.FirstTokenP50Ms,
		FirstTokenP95Ms:             performance.FirstTokenP95Ms,
		JitterThresholdMs:           performance.JitterThresholdMs,
		JitterSampleCount:           performance.JitterSampleCount,
		JitterSlowCount:             performance.JitterSlowCount,
		JitterAllowedCount:          performance.JitterAllowedCount,
		JitterPenalty:               performance.JitterPenalty,
		StabilityScore:              performance.Stability,
	}
	metric.SuccessRate = float64(metric.SuccessCount) / float64(metric.SampleCount)
	if metric.RetryFailureCount > 0 {
		metric.AverageRetryFailureDurationMs = performance.StabilityRetryFailureDurationTotalMs /
			float64(metric.RetryFailureCount)
	}
	return metric, true
}

func channelSmartScheduleRealtimeSampleState(
	route model.ChannelSmartScheduleRoute,
	windowStart int64,
	events []model.ChannelMonitorEvent,
) model.ChannelSmartScheduleModelSampleState {
	state := route.SharedSamples
	state.ChannelId = route.ChannelId
	state.ModelName = route.Model
	state.WindowStart = windowStart
	state.SampleCount = int64(len(events))
	state.SuccessCount = 0
	state.FailureDurationSampleCount = 0
	state.AverageFailureDurationMs = nil
	state.FirstTokenSampleCount = 0
	state.AverageFirstTokenMs = nil
	state.TPSSampleCount = 0
	state.AverageTPS = nil
	state.LastTime = 0
	state.LastSuccess = false
	state.LastError = ""
	state.SamplesJSON = ""
	failureDurationTotalMs := float64(0)
	firstTokenTotalMs := float64(0)
	tPSTotal := float64(0)
	for _, event := range events {
		if event.Outcome == model.ChannelMonitorEventOutcomeSuccess {
			state.SuccessCount++
		}
		if event.Outcome == model.ChannelMonitorEventOutcomeFailure && event.AttemptDurationMs != nil {
			state.FailureDurationSampleCount++
			failureDurationTotalMs += float64(*event.AttemptDurationMs)
		}
		if event.FirstTokenMs != nil {
			state.FirstTokenSampleCount++
			firstTokenTotalMs += *event.FirstTokenMs
		}
		if event.TPS != nil {
			state.TPSSampleCount++
			tPSTotal += *event.TPS
		}
		if event.OccurredAt >= state.LastTime {
			state.LastTime = event.OccurredAt
			state.LastSuccess = event.Outcome == model.ChannelMonitorEventOutcomeSuccess
			state.LastError = strings.TrimSpace(event.ErrorMessage)
		}
	}
	if state.FailureDurationSampleCount > 0 {
		value := failureDurationTotalMs / float64(state.FailureDurationSampleCount)
		state.AverageFailureDurationMs = &value
	}
	if state.FirstTokenSampleCount > 0 {
		value := firstTokenTotalMs / float64(state.FirstTokenSampleCount)
		state.AverageFirstTokenMs = &value
	}
	if state.TPSSampleCount > 0 {
		value := tPSTotal / float64(state.TPSSampleCount)
		state.AverageTPS = &value
	}
	return state
}

func channelSmartScheduleRealtimeGroupCount(events []model.ChannelMonitorEvent) int {
	groups := make(map[string]struct{})
	for _, event := range events {
		if group := strings.TrimSpace(event.GroupName); group != "" {
			groups[group] = struct{}{}
		}
	}
	if len(groups) == 0 && len(events) > 0 {
		return 1
	}
	return len(groups)
}

func channelSmartScheduleRealtimeMetricCoverage(
	generatedAt int64,
	settings channelMonitorSettings,
	snapshots []service.ChannelMonitorRedisRouteHealthSnapshot,
) channelSmartScheduleMetricCoverageResponse {
	performanceStart := max(generatedAt-int64(settings.SmartSchedulePerformanceWindowMinutes*60), 0)
	stabilityWindowMinutes := settings.SmartScheduleGroupPolicies.maxStabilityWindowMinutes()
	stabilityStart := max(generatedAt-int64(stabilityWindowMinutes*60), 0)
	coverageStart := service.ChannelMonitorRedisRouteHealthCoverageStart()
	for _, snapshot := range snapshots {
		coverageStart = max(coverageStart, snapshot.CoverageStart)
	}
	coverage := channelSmartScheduleMetricCoverageResponse{
		AggregationEnabled:        true,
		AggregatedFrom:            coverageStart,
		PerformanceWindowStart:    performanceStart,
		StabilityWindowStart:      stabilityStart,
		PerformanceWindowComplete: coverageStart <= performanceStart,
		StabilityWindowComplete:   coverageStart <= stabilityStart,
		ConfiguredRetentionDays:   settings.CostRetentionDays,
		RequiredRetentionMinutes:  max(settings.SmartSchedulePerformanceWindowMinutes, stabilityWindowMinutes),
		ConfiguredRetentionSufficient: settings.SmartScheduleRealtimeRetentionMinutes >=
			max(settings.SmartSchedulePerformanceWindowMinutes, stabilityWindowMinutes),
		RealtimeRetentionMinutes: settings.SmartScheduleRealtimeRetentionMinutes,
		RealtimeSampleLimit:      settings.SmartScheduleRealtimeSampleLimit,
	}
	for _, snapshot := range snapshots {
		coverage.AggregatedThrough = max(coverage.AggregatedThrough, snapshot.DataCutoffAt)
		coverage.SampleLimitTruncated = coverage.SampleLimitTruncated || snapshot.SampleLimitTruncated
		coverage.SampleLimitCutoffAt = max(coverage.SampleLimitCutoffAt, snapshot.SampleLimitCutoffAt)
	}
	return coverage
}

func channelSmartScheduleMergeRealtimeSnapshot(
	target *service.ChannelMonitorRedisRouteHealthSnapshot,
	source service.ChannelMonitorRedisRouteHealthSnapshot,
) {
	if source.WindowStart > 0 && (target.WindowStart == 0 || source.WindowStart < target.WindowStart) {
		target.WindowStart = source.WindowStart
	}
	target.CoverageStart = max(target.CoverageStart, source.CoverageStart)
	target.ProjectionStartedAt = max(target.ProjectionStartedAt, source.ProjectionStartedAt)
	target.RetentionMinutes = max(target.RetentionMinutes, source.RetentionMinutes)
	target.SampleLimit = max(target.SampleLimit, source.SampleLimit)
	target.SampleLimitTruncated = target.SampleLimitTruncated || source.SampleLimitTruncated
	target.SampleLimitCutoffAt = max(target.SampleLimitCutoffAt, source.SampleLimitCutoffAt)
	target.WindowEnd = max(target.WindowEnd, source.WindowEnd)
	target.DataCutoffAt = max(target.DataCutoffAt, source.DataCutoffAt)
	target.ProcessedAt = max(target.ProcessedAt, source.ProcessedAt)
	target.EventWatermark = max(target.EventWatermark, source.EventWatermark)
}
