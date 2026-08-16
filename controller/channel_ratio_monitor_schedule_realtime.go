package controller

import (
	"context"
	"math"
	"sort"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

func channelSmartScheduleRealtimeEvents(
	ctx context.Context,
	channelId int,
	modelName string,
	windowStart int64,
	observationSince int64,
	maxRequests int,
) ([]model.ChannelMonitorEvent, service.ChannelMonitorRedisRouteHealthSnapshot, error) {
	window, available, err := service.GetChannelMonitorRedisRouteHealthWindow(ctx, channelId, modelName)
	if err != nil {
		return nil, service.ChannelMonitorRedisRouteHealthSnapshot{}, err
	}
	if !available {
		return nil, service.ChannelMonitorRedisRouteHealthSnapshot{
			CoverageStart: service.ChannelMonitorRedisRouteHealthCoverageStart(),
		}, nil
	}
	windowStart = max(windowStart, observationSince)
	events := channelSmartScheduleRedisWindowEvents(
		window, channelId, modelName, windowStart, 0, maxRequests, 0,
	)
	return events, window.Snapshot, nil
}

func channelSmartScheduleEventsForWindow(
	events []model.ChannelMonitorEvent,
	windowStart int64,
	maxRequests int,
) []model.ChannelMonitorEvent {
	first := sort.Search(len(events), func(index int) bool {
		return events[index].OccurredAt >= windowStart
	})
	events = events[first:]
	if maxRequests > 0 && len(events) > maxRequests {
		events = events[len(events)-maxRequests:]
	}
	return events
}

func channelSmartScheduleRealtimeAdaptiveMetric(
	ctx context.Context,
	channelId int,
	modelName string,
	windowStart int64,
	observationSince int64,
	maxRequests int,
	warningSeconds float64,
	criticalSeconds float64,
) (model.ChannelSmartScheduleAdaptiveHealthMetric, service.ChannelMonitorRedisRouteHealthSnapshot, error) {
	events, snapshot, err := channelSmartScheduleRealtimeEvents(
		ctx, channelId, modelName, windowStart, observationSince, maxRequests,
	)
	if err != nil {
		return model.ChannelSmartScheduleAdaptiveHealthMetric{}, snapshot, err
	}
	return channelSmartScheduleRealtimeAdaptiveMetricFromEvents(
		events, warningSeconds, criticalSeconds,
	), snapshot, nil
}

func channelSmartScheduleRealtimeAdaptiveMetricFromEvents(
	events []model.ChannelMonitorEvent,
	warningSeconds float64,
	criticalSeconds float64,
) model.ChannelSmartScheduleAdaptiveHealthMetric {
	metric := model.ChannelSmartScheduleAdaptiveHealthMetric{}
	failureBuckets := [6]int64{}
	firstTokenBuckets := make(map[int64]model.ChannelMonitorDurationBucket)
	for _, event := range events {
		metric.RequestCount++
		metric.LastUsedTime = max(metric.LastUsedTime, event.OccurredAt)
		if event.Outcome == model.ChannelMonitorEventOutcomeFailure {
			metric.FailureCount++
			metric.StabilityFailureCount++
			if event.Source == model.ChannelMonitorEventSourceBusiness && event.IsFinalAttempt {
				metric.StabilityFinalFailureCount++
			} else {
				metric.StabilityRetryFailureCount++
			}
			if event.AttemptDurationMs != nil {
				durationMs := *event.AttemptDurationMs
				metric.RetryFailureDurationTotalMs += float64(durationMs)
				switch {
				case durationMs < 1000:
					failureBuckets[0]++
				case durationMs < 3000:
					failureBuckets[1]++
				case durationMs < 10000:
					failureBuckets[2]++
				case durationMs < 30000:
					failureBuckets[3]++
				case durationMs < 60000:
					failureBuckets[4]++
				default:
					failureBuckets[5]++
				}
			}
			continue
		}
		metric.StabilitySuccessCount++
		metric.HealthyRequestCount++
		if event.TPS != nil && *event.TPS >= 0 && !math.IsNaN(*event.TPS) && !math.IsInf(*event.TPS, 0) {
			metric.TPSSampleCount++
			metric.TPSTotal += *event.TPS
		}
		if event.FirstTokenMs == nil || *event.FirstTokenMs < 0 ||
			math.IsNaN(*event.FirstTokenMs) || math.IsInf(*event.FirstTokenMs, 0) {
			continue
		}
		metric.FirstTokenCount++
		metric.FirstTokenTotalMs += *event.FirstTokenMs
		lower, upper := channelSmartScheduleRealtimeDurationBucket(*event.FirstTokenMs)
		bucket := firstTokenBuckets[lower]
		bucket.LowerBoundMs = lower
		bucket.UpperBoundMs = upper
		bucket.Count++
		bucket.TotalMs += *event.FirstTokenMs
		firstTokenBuckets[lower] = bucket
		metric.LatencyPressure += channelSmartScheduleLinearPressure(
			*event.FirstTokenMs,
			warningSeconds*1000,
			criticalSeconds*1000,
		)
		if *event.FirstTokenMs >= warningSeconds*1000 {
			metric.SlowRequestCount++
			metric.HealthyRequestCount--
		}
	}
	metric.RetryFailureDurationBuckets = []model.ChannelMonitorFailureDurationBucket{
		{LowerBoundMs: 0, UpperBoundMs: 1000, Count: failureBuckets[0]},
		{LowerBoundMs: 1000, UpperBoundMs: 3000, Count: failureBuckets[1]},
		{LowerBoundMs: 3000, UpperBoundMs: 10000, Count: failureBuckets[2]},
		{LowerBoundMs: 10000, UpperBoundMs: 30000, Count: failureBuckets[3]},
		{LowerBoundMs: 30000, UpperBoundMs: 60000, Count: failureBuckets[4]},
		{LowerBoundMs: 60000, UpperBoundMs: 0, Count: failureBuckets[5]},
	}
	metric.FirstTokenDurationBuckets = make([]model.ChannelMonitorDurationBucket, 0, len(firstTokenBuckets))
	for _, bucket := range firstTokenBuckets {
		metric.FirstTokenDurationBuckets = append(metric.FirstTokenDurationBuckets, bucket)
	}
	sort.Slice(metric.FirstTokenDurationBuckets, func(i int, j int) bool {
		return metric.FirstTokenDurationBuckets[i].LowerBoundMs <
			metric.FirstTokenDurationBuckets[j].LowerBoundMs
	})
	return metric
}

func channelSmartScheduleRealtimeDurationBucket(valueMs float64) (int64, int64) {
	upperBounds := [...]int64{
		25, 50, 75, 100, 125, 150, 175, 200, 250, 300, 350, 400, 500, 600, 750,
		1000, 1250, 1500, 2000, 2500, 3000, 4000, 5000, 7500, 10000, 15000,
		20000, 30000, 45000, 60000, 90000, 120000, 180000, 300000, 600000,
		900000, 1800000, 3600000,
	}
	index := sort.Search(len(upperBounds), func(index int) bool {
		return valueMs < float64(upperBounds[index])
	})
	lower := int64(0)
	if index > 0 {
		lower = upperBounds[index-1]
	}
	if index == len(upperBounds) {
		return lower, 0
	}
	return lower, upperBounds[index]
}

func channelSmartScheduleRealtimeAverage(metric model.ChannelSmartScheduleAdaptiveHealthMetric) (*float64, *float64) {
	var firstTokenMs *float64
	if metric.FirstTokenCount > 0 {
		value := metric.FirstTokenTotalMs / float64(metric.FirstTokenCount)
		firstTokenMs = &value
	}
	var tps *float64
	if metric.TPSSampleCount > 0 {
		value := metric.TPSTotal / float64(metric.TPSSampleCount)
		tps = &value
	}
	return firstTokenMs, tps
}
