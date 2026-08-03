package model

import (
	"context"
	"errors"
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
// available for one channel/model route in the requested window. Production
// request samples come from minute metrics plus the not-yet-aggregated log
// tail; manual tests and scheduled probes are stored in the shared sample
// buffer because their logs are excluded from the production aggregation.
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
	err := DB.WithContext(ctx).
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
	// database and keep the larger count for each minute. This lets a
	// just-persisted failure satisfy the configured sample gate without being
	// double-counted after aggregation catches up. A partial first minute is
	// also read from logs when it falls before the live tail, because its
	// aggregate includes samples from before the temporary state began.
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
			if parsed && (other.SmartScheduleProbe || other.ChannelTest) {
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

	var state ChannelSmartScheduleModelSampleState
	err = DB.WithContext(ctx).
		Where(&ChannelSmartScheduleModelSampleState{ChannelId: channelId, ModelName: modelName}).
		First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return productionSampleCount, nil
	}
	if err != nil {
		return 0, err
	}
	return productionSampleCount + state.MetricsSince(startTimestamp).SampleCount, nil
}
