package model

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const channelSmartScheduleRuntimeLiveTailSeconds = 5 * channelMonitorMinuteSeconds

type channelSmartScheduleRuntimeLog struct {
	Type      int
	ModelName string
	Other     string
	CreatedAt int64
}

type ChannelSmartScheduleRuntimeTemporaryRoute struct {
	ModelName   string
	SampleSince int64
}

type ChannelSmartScheduleRuntimeRoute struct {
	ModelName            string
	SampleSince          int64
	StabilityState       string
	TemporaryTrafficKind string
}

// ChannelSmartScheduleAdaptiveHealthMetricWindow describes one exact-second
// adaptive health window. The threshold values are part of the window because
// different group policies may use different first-token boundaries for the
// same channel and model.
type ChannelSmartScheduleAdaptiveHealthMetricWindow struct {
	ChannelId        int
	ModelName        string
	StartTimestamp   int64
	ObservationSince int64
	MaxRequests      int
	WarningSeconds   float64
	CriticalSeconds  float64
}

type ChannelSmartScheduleAdaptiveHealthMetricResult struct {
	Window ChannelSmartScheduleAdaptiveHealthMetricWindow
	Metric ChannelSmartScheduleAdaptiveHealthMetric
}

type channelSmartScheduleAdaptiveHealthLog struct {
	ChannelId        int
	ModelName        string
	Type             int
	IsStream         bool
	CompletionTokens int
	UseTime          int
	IsRetryAttempt   bool
	Other            string
	CreatedAt        int64
}

func channelSmartScheduleAdaptiveModelMatches(logModelName, requestedModelName string) bool {
	logModelName = channelSmartScheduleModelName(logModelName)
	requestedModelName = channelSmartScheduleModelName(requestedModelName)
	if logModelName == requestedModelName {
		return true
	}
	if strings.HasSuffix(requestedModelName, "*") {
		return strings.HasPrefix(logModelName, strings.TrimSuffix(requestedModelName, "*"))
	}
	return false
}

// GetChannelSmartScheduleAdaptiveHealthMetrics reads only the requested
// channel/model pairs and classifies production requests at request precision.
// Minute aggregates cannot answer arbitrary first-token thresholds, so this
// path intentionally reads the bounded adaptive window from the log database.
func GetChannelSmartScheduleAdaptiveHealthMetrics(
	ctx context.Context,
	windows []ChannelSmartScheduleAdaptiveHealthMetricWindow,
	endTimestamp int64,
) ([]ChannelSmartScheduleAdaptiveHealthMetricResult, error) {
	if len(windows) == 0 {
		return []ChannelSmartScheduleAdaptiveHealthMetricResult{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if endTimestamp <= 0 {
		endTimestamp = common.GetTimestamp()
	}
	results := make([]ChannelSmartScheduleAdaptiveHealthMetricResult, len(windows))
	minimumStart := endTimestamp
	channelIds := make(map[int]struct{}, len(windows))
	windowIndexesByChannel := make(map[int][]int, len(windows))
	for index, window := range windows {
		window.ModelName = channelSmartScheduleModelName(window.ModelName)
		if window.ObservationSince > window.StartTimestamp {
			window.StartTimestamp = window.ObservationSince
		}
		results[index].Window = window
		if window.ChannelId <= 0 || window.ModelName == "" || window.StartTimestamp >= endTimestamp {
			continue
		}
		channelIds[window.ChannelId] = struct{}{}
		windowIndexesByChannel[window.ChannelId] = append(windowIndexesByChannel[window.ChannelId], index)
		if window.StartTimestamp < minimumStart {
			minimumStart = window.StartTimestamp
		}
	}
	if LOG_DB == nil || len(channelIds) == 0 || minimumStart >= endTimestamp {
		return results, nil
	}
	channelIdList := make([]int, 0, len(channelIds))
	for channelId := range channelIds {
		channelIdList = append(channelIdList, channelId)
	}
	query := LOG_DB.WithContext(ctx).
		Model(&Log{}).
		Select("channel_id, model_name, type, is_stream, completion_tokens, use_time, is_retry_attempt, other, created_at").
		Where("type IN ?", []int{LogTypeConsume, LogTypeError}).
		Where("channel_id IN ?", channelIdList).
		Where("created_at >= ? AND created_at < ?", minimumStart, endTimestamp).
		Order("created_at DESC, id DESC")
	rows, err := query.Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	firstTokenBuckets := make([]map[int]ChannelMonitorDurationBucket, len(results))
	retryFailureBuckets := make([][6]int64, len(results))
	actualFailureCounts := make([]int64, len(results))
	configuredFinalFailureCounts := make([]int64, len(results))
	retryFailureCounts := make([]int64, len(results))
	requestCounts := make([]int, len(results))
	for rows.Next() {
		var log channelSmartScheduleAdaptiveHealthLog
		if err := rows.Scan(
			&log.ChannelId, &log.ModelName, &log.Type, &log.IsStream, &log.CompletionTokens,
			&log.UseTime, &log.IsRetryAttempt, &log.Other, &log.CreatedAt,
		); err != nil {
			return nil, err
		}
		if log.CreatedAt >= endTimestamp {
			continue
		}
		parsedOther, parsed := channelMonitorMinuteOther(log.Other)
		if parsed && (parsedOther.SmartScheduleProbe || parsedOther.ChannelTest || parsedOther.StatusProbe) {
			continue
		}
		for _, index := range windowIndexesByChannel[log.ChannelId] {
			window := results[index]
			if log.CreatedAt < window.Window.StartTimestamp ||
				!channelSmartScheduleAdaptiveModelMatches(log.ModelName, window.Window.ModelName) {
				continue
			}
			metric := &results[index].Metric
			if log.Type == LogTypeError {
				if parsed && channelMonitorMinuteRateLimited(parsedOther.StatusCode) {
					continue
				}
				if parsed && parsedOther.FinalRetrySummary {
					configuredFinalFailureCounts[index]++
					continue
				}
				if window.Window.MaxRequests > 0 && requestCounts[index] >= window.Window.MaxRequests {
					continue
				}
				requestCounts[index]++
				metric.RequestCount++
				metric.FailureCount++
				metric.LastUsedTime = max(metric.LastUsedTime, log.CreatedAt)
				actualFailureCounts[index]++
				if !log.IsRetryAttempt {
					configuredFinalFailureCounts[index]++
					continue
				}
				retryFailureCounts[index]++
				durationMs := int64(log.UseTime)
				if durationMs < 0 {
					durationMs = 0
				} else if durationMs > math.MaxInt64/1000 {
					durationMs = math.MaxInt64
				} else {
					durationMs *= 1000
				}
				if parsed && parsedOther.AttemptDurationMs != nil && *parsedOther.AttemptDurationMs >= 0 {
					durationMs = *parsedOther.AttemptDurationMs
				}
				metric.RetryFailureDurationTotalMs += float64(durationMs)
				switch {
				case durationMs < 1000:
					retryFailureBuckets[index][0]++
				case durationMs < 3000:
					retryFailureBuckets[index][1]++
				case durationMs < 10000:
					retryFailureBuckets[index][2]++
				case durationMs < 30000:
					retryFailureBuckets[index][3]++
				case durationMs < 60000:
					retryFailureBuckets[index][4]++
				default:
					retryFailureBuckets[index][5]++
				}
				continue
			}
			if window.Window.MaxRequests > 0 && requestCounts[index] >= window.Window.MaxRequests {
				continue
			}
			requestCounts[index]++
			metric.RequestCount++
			metric.StabilitySuccessCount++
			metric.HealthyRequestCount++
			metric.LastUsedTime = max(metric.LastUsedTime, log.CreatedAt)
			if log.CompletionTokens > 0 && log.UseTime > 0 {
				tps := float64(log.CompletionTokens) / float64(log.UseTime)
				if !math.IsNaN(tps) && !math.IsInf(tps, 0) {
					metric.TPSSampleCount++
					metric.TPSTotal += tps
				}
			}
			if !log.IsStream || !parsed || parsedOther.FirstResponseTime == nil ||
				*parsedOther.FirstResponseTime <= 0 ||
				math.IsNaN(*parsedOther.FirstResponseTime) ||
				math.IsInf(*parsedOther.FirstResponseTime, 0) {
				continue
			}
			firstTokenMs := *parsedOther.FirstResponseTime
			metric.FirstTokenCount++
			metric.FirstTokenTotalMs += firstTokenMs
			if firstTokenBuckets[index] == nil {
				firstTokenBuckets[index] = make(map[int]ChannelMonitorDurationBucket)
			}
			bucketIndex := channelMonitorDurationBucketIndex(firstTokenMs)
			bucket := firstTokenBuckets[index][bucketIndex]
			bucket.Count++
			bucket.TotalMs += firstTokenMs
			firstTokenBuckets[index][bucketIndex] = bucket
			metric.LatencyPressure += channelSmartScheduleAdaptiveLatencyPressure(
				firstTokenMs, window.Window.WarningSeconds, window.Window.CriticalSeconds,
			)
			if firstTokenMs >= window.Window.WarningSeconds*1000 {
				metric.SlowRequestCount++
				metric.HealthyRequestCount--
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range results {
		metric := &results[index].Metric
		failureCount := max(actualFailureCounts[index], configuredFinalFailureCounts[index])
		finalFailureCount := min(configuredFinalFailureCounts[index], failureCount)
		retryFailureCount := min(max(retryFailureCounts[index], int64(0)), failureCount-finalFailureCount)
		metric.StabilityFailureCount = failureCount
		metric.StabilityFinalFailureCount = finalFailureCount
		metric.StabilityRetryFailureCount = retryFailureCount
		metric.RetryFailureDurationBuckets = []ChannelMonitorFailureDurationBucket{
			{LowerBoundMs: 0, UpperBoundMs: 1000, Count: retryFailureBuckets[index][0]},
			{LowerBoundMs: 1000, UpperBoundMs: 3000, Count: retryFailureBuckets[index][1]},
			{LowerBoundMs: 3000, UpperBoundMs: 10000, Count: retryFailureBuckets[index][2]},
			{LowerBoundMs: 10000, UpperBoundMs: 30000, Count: retryFailureBuckets[index][3]},
			{LowerBoundMs: 30000, UpperBoundMs: 60000, Count: retryFailureBuckets[index][4]},
			{LowerBoundMs: 60000, UpperBoundMs: 0, Count: retryFailureBuckets[index][5]},
		}
		metric.FirstTokenDurationBuckets = channelMonitorDurationBucketsFromAggregates(firstTokenBuckets[index])
	}
	return results, nil
}

func getChannelSmartScheduleRuntimeAbilityRoutes(channelId int, modelName string) (map[string]string, []string, error) {
	modelNames := channelSmartScheduleRouteModelNames(modelName)
	if channelId <= 0 || len(modelNames) == 0 {
		return map[string]string{}, nil, nil
	}

	var channel Channel
	err := DB.Select("id", "status").Where("id = ?", channelId).First(&channel).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && channel.Status != common.ChannelStatusEnabled) {
		return map[string]string{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	var abilities []Ability
	if err := DB.Select("group", "model").
		Where("channel_id = ? AND model IN ? AND enabled = ?", channelId, modelNames, true).
		Find(&abilities).Error; err != nil {
		return nil, nil, err
	}
	selectedModelByGroup := make(map[string]string)
	abilityModelsByGroup := make(map[string]map[string]struct{})
	for _, ability := range abilities {
		models := abilityModelsByGroup[ability.Group]
		if models == nil {
			models = make(map[string]struct{})
			abilityModelsByGroup[ability.Group] = models
		}
		models[ability.Model] = struct{}{}
	}
	for group, models := range abilityModelsByGroup {
		for _, candidateModel := range modelNames {
			if _, exists := models[candidateModel]; exists {
				selectedModelByGroup[group] = candidateModel
				break
			}
		}
	}
	if len(selectedModelByGroup) == 0 {
		return map[string]string{}, modelNames, nil
	}
	return selectedModelByGroup, modelNames, nil
}

// GetChannelSmartScheduleRuntimeParticipatingRoutes returns the effective
// smart-schedule routes for one channel and requested model. Exact abilities
// take precedence over matching wildcard abilities in the same group.
func GetChannelSmartScheduleRuntimeParticipatingRoutes(channelId int, modelName string) (map[string]string, error) {
	if routes, cacheEnabled := GetCachedChannelSmartScheduleRuntimeParticipatingRoutes(channelId, modelName); cacheEnabled {
		return routes, nil
	}
	selectedModelByGroup, modelNames, err := getChannelSmartScheduleRuntimeAbilityRoutes(channelId, modelName)
	if err != nil || len(selectedModelByGroup) == 0 {
		return selectedModelByGroup, err
	}

	groups := make([]string, 0, len(selectedModelByGroup))
	for group := range selectedModelByGroup {
		groups = append(groups, group)
	}
	var states []ChannelSmartScheduleRouteState
	if err := DB.Select("group_name", "model_name").
		Where("channel_id = ? AND group_name IN ? AND model_name IN ?", channelId, groups, modelNames).
		Where("participation_set = ? AND excluded = ?", true, false).
		Find(&states).Error; err != nil {
		return nil, err
	}
	participating := make(map[string]string, len(states))
	for _, state := range states {
		if selectedModelByGroup[state.GroupName] == state.ModelName {
			participating[state.GroupName] = state.ModelName
		}
	}
	return participating, nil
}

// GetChannelSmartScheduleRuntimeRoutes returns every participating route for
// one channel/model request, including normal routes that may need short-term
// runtime protection before the scheduled stability score catches up.
func GetChannelSmartScheduleRuntimeRoutes(channelId int, modelName string) (map[string]ChannelSmartScheduleRuntimeRoute, error) {
	if routes, cacheEnabled := GetCachedChannelSmartScheduleRuntimeRoutes(channelId, modelName); cacheEnabled {
		return routes, nil
	}
	selectedModelByGroup, modelNames, err := getChannelSmartScheduleRuntimeAbilityRoutes(channelId, modelName)
	if err != nil || len(selectedModelByGroup) == 0 {
		return map[string]ChannelSmartScheduleRuntimeRoute{}, err
	}

	groups := make([]string, 0, len(selectedModelByGroup))
	for group := range selectedModelByGroup {
		groups = append(groups, group)
	}
	var states []ChannelSmartScheduleRouteState
	if err := DB.Select(
		"group_name", "model_name", "temporary_traffic_kind", "temporary_traffic_since", "stability_state", "stability_since",
	).
		Where("channel_id = ? AND group_name IN ? AND model_name IN ?", channelId, groups, modelNames).
		Where("participation_set = ? AND excluded = ?", true, false).
		Find(&states).Error; err != nil {
		return nil, err
	}
	routes := make(map[string]ChannelSmartScheduleRuntimeRoute, len(states))
	for _, state := range states {
		if selectedModelByGroup[state.GroupName] != state.ModelName {
			continue
		}
		sampleSince := state.TemporaryTrafficSince
		if state.StabilityState == ChannelSmartScheduleStabilityProbing && state.StabilitySince > sampleSince {
			sampleSince = state.StabilitySince
		}
		routes[state.GroupName] = ChannelSmartScheduleRuntimeRoute{
			ModelName:            state.ModelName,
			SampleSince:          sampleSince,
			StabilityState:       state.StabilityState,
			TemporaryTrafficKind: state.TemporaryTrafficKind,
		}
	}
	return routes, nil
}

func GetChannelSmartScheduleRuntimeTemporaryRoutes(channelId int, modelName string) (map[string]ChannelSmartScheduleRuntimeTemporaryRoute, error) {
	selectedModelByGroup, modelNames, err := getChannelSmartScheduleRuntimeAbilityRoutes(channelId, modelName)
	if err != nil {
		return nil, err
	}
	if len(selectedModelByGroup) == 0 {
		return map[string]ChannelSmartScheduleRuntimeTemporaryRoute{}, nil
	}

	groups := make([]string, 0, len(selectedModelByGroup))
	for group := range selectedModelByGroup {
		groups = append(groups, group)
	}
	var states []ChannelSmartScheduleRouteState
	if err := DB.Select(
		"group_name", "model_name", "temporary_traffic_kind", "temporary_traffic_since", "stability_state", "stability_since",
		"manual_primary_until", "manual_primary_allow_stability_degrade",
	).
		Where("channel_id = ? AND group_name IN ? AND model_name IN ?", channelId, groups, modelNames).
		Where("participation_set = ? AND excluded = ?", true, false).
		Where(
			"temporary_traffic_kind <> ? OR stability_state = ? OR (manual_primary_until > ? AND manual_primary_allow_stability_degrade = ? AND stability_state = ?)",
			"", ChannelSmartScheduleStabilityProbing, common.GetTimestamp(), true, "",
		).
		Find(&states).Error; err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	routes := make(map[string]ChannelSmartScheduleRuntimeTemporaryRoute, len(states))
	for _, state := range states {
		if selectedModelByGroup[state.GroupName] != state.ModelName {
			continue
		}
		activeFixedPrimary := state.ManualPrimaryUntil > now &&
			state.ManualPrimaryAllowStabilityDegrade && state.StabilityState == ""
		activeTemporaryTraffic := state.TemporaryTrafficKind != "" ||
			state.StabilityState == ChannelSmartScheduleStabilityProbing
		if !activeFixedPrimary && !activeTemporaryTraffic {
			continue
		}
		sampleSince := state.TemporaryTrafficSince
		if state.StabilityState == ChannelSmartScheduleStabilityProbing && state.StabilitySince > sampleSince {
			sampleSince = state.StabilitySince
		}
		if sampleSince <= 0 && !activeFixedPrimary {
			sampleSince = now
		}
		routes[state.GroupName] = ChannelSmartScheduleRuntimeTemporaryRoute{
			ModelName: state.ModelName, SampleSince: sampleSince,
		}
	}
	return routes, nil
}

// GetChannelSmartScheduleRouteSampleCount returns the total stability samples
// available for one channel/model route in the requested window. It is kept
// for sample reporting; runtime hard protection uses its own failure window.
// Production requests come from minute metrics plus the not-yet-aggregated
// log tail, while tests and probes use the shared sample buffer.
func GetChannelSmartScheduleRouteSampleCount(
	ctx context.Context,
	startTimestamp int64,
	channelId int,
	modelName string,
) (int64, error) {
	requestModelName := strings.TrimSpace(modelName)
	modelName = channelSmartScheduleModelName(requestModelName)
	logModelNames := []string{requestModelName}
	if modelName != requestModelName {
		logModelNames = append(logModelNames, modelName)
	}
	if channelId <= 0 || modelName == "" {
		return 0, nil
	}
	var state ChannelSmartScheduleModelSampleState
	err := DB.WithContext(ctx).
		Where(&ChannelSmartScheduleModelSampleState{ChannelId: channelId, ModelName: modelName}).
		First(&state).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	if state.ObservationSince > startTimestamp {
		startTimestamp = state.ObservationSince
	}

	now := common.GetTimestamp()
	if startTimestamp > now {
		return 0, nil
	}
	windowStart := channelMonitorMinuteStart(startTimestamp)
	aggregateStart := windowStart
	if startTimestamp > windowStart {
		aggregateStart += channelMonitorMinuteSeconds
	}
	liveStart := channelMonitorMinuteStart(now) - channelSmartScheduleRuntimeLiveTailSeconds
	if liveStart < windowStart {
		liveStart = windowStart
	}
	productionSampleCount := int64(0)
	if liveStart > aggregateStart {
		aggregates, err := getChannelMonitorRouteStabilityAggregates(
			ctx,
			aggregateStart,
			liveStart,
			ChannelMonitorSuccessFilter{ChannelId: channelId, ModelName: modelName},
		)
		if err != nil {
			return 0, err
		}
		if len(aggregates) > 0 {
			productionSampleCount = channelMonitorRouteStabilityMetric(aggregates[0]).SampleCount
		}
	}
	tailAggregates := make(map[int64]channelMonitorRouteStabilityAggregate)
	metricStart := max(liveStart, aggregateStart)
	var minuteMetrics []ChannelMonitorMinuteMetric
	err = DB.WithContext(ctx).
		Select(
			"minute_start", "actual_success_count", "actual_failure_count", "final_failure_count",
			"rate_limit_actual_failure_count", "rate_limit_final_failure_count",
		).
		Where("channel_id = ? AND model_name IN ?", channelId, logModelNames).
		Where("minute_start >= ? AND minute_start <= ?", metricStart, channelMonitorMinuteStart(now)).
		Find(&minuteMetrics).Error
	if err != nil {
		return 0, err
	}
	for _, metric := range minuteMetrics {
		aggregate := tailAggregates[metric.MinuteStart]
		aggregate.ChannelId = channelId
		aggregate.ModelName = modelName
		aggregate.ActualSuccessCount += metric.ActualSuccessCount
		aggregate.ActualFailureCount += metric.ActualFailureCount
		aggregate.FinalFailureCount += metric.FinalFailureCount
		aggregate.RateLimitActualFailureCount += metric.RateLimitActualFailureCount
		aggregate.RateLimitFinalFailureCount += metric.RateLimitFinalFailureCount
		tailAggregates[metric.MinuteStart] = aggregate
	}
	tailSampleCounts := make(map[int64]int64, len(tailAggregates)+1)
	for minuteStart, aggregate := range tailAggregates {
		tailSampleCounts[minuteStart] = channelMonitorRouteStabilityMetric(aggregate).SampleCount
	}
	liveSampleCounts := make(map[int64]int64, len(tailSampleCounts)+2)

	// The aggregation worker intentionally excludes the open minute and may
	// still be replacing its short retry tail. Read that same tail from the log
	// database and keep the larger count for each minute. This includes a
	// just-persisted sample without double-counting it after aggregation catches
	// up. A partial first minute is also read from logs when it falls before the
	// live tail, because its aggregate includes samples from before the requested
	// window began.
	type logRange struct {
		start int64
		end   int64
	}
	logRanges := make([]logRange, 0, 2)
	if startTimestamp > windowStart && startTimestamp < liveStart {
		partialMinuteEnd := min(windowStart+channelMonitorMinuteSeconds, liveStart)
		if startTimestamp < partialMinuteEnd {
			logRanges = append(logRanges, logRange{start: startTimestamp, end: partialMinuteEnd})
		}
	}
	tailStart := max(liveStart, startTimestamp)
	if tailStart <= now {
		logRanges = append(logRanges, logRange{start: tailStart, end: now + 1})
	}
	for _, currentRange := range logRanges {
		logQuery := LOG_DB.WithContext(ctx).
			Model(&Log{}).
			Select("type, model_name, other, created_at").
			Where("type IN ?", []int{LogTypeConsume, LogTypeError}).
			Where("channel_id = ?", channelId).
			Where("created_at >= ? AND created_at < ?", currentRange.start, currentRange.end)
		if strings.Contains(modelName, "*") {
			logQuery = logQuery.Where(
				"(model_name IN ? OR model_name LIKE ?)",
				logModelNames,
				strings.ReplaceAll(modelName, "*", "%"),
			)
		} else {
			logQuery = logQuery.Where("model_name IN ?", logModelNames)
		}
		rows, err := logQuery.Rows()
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var log channelSmartScheduleRuntimeLog
			if err := rows.Scan(&log.Type, &log.ModelName, &log.Other, &log.CreatedAt); err != nil {
				rows.Close()
				return 0, err
			}
			if channelSmartScheduleModelName(log.ModelName) != modelName {
				continue
			}
			other, parsed := channelMonitorMinuteOther(log.Other)
			if parsed && (other.SmartScheduleProbe || other.ChannelTest || other.StatusProbe) {
				continue
			}
			switch log.Type {
			case LogTypeConsume:
				liveSampleCounts[channelMonitorMinuteStart(log.CreatedAt)]++
			case LogTypeError:
				if parsed && (other.FinalRetrySummary || channelMonitorMinuteRateLimited(other.StatusCode)) {
					continue
				}
				liveSampleCounts[channelMonitorMinuteStart(log.CreatedAt)]++
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return 0, err
		}
		if err := rows.Close(); err != nil {
			return 0, err
		}
	}
	for minuteStart, sampleCount := range liveSampleCounts {
		if sampleCount > tailSampleCounts[minuteStart] {
			tailSampleCounts[minuteStart] = sampleCount
		}
	}
	for _, sampleCount := range tailSampleCounts {
		productionSampleCount += sampleCount
	}

	return productionSampleCount + state.MetricsSince(startTimestamp).SampleCount, nil
}
