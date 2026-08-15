package controller

import (
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

const channelSmartScheduleProjectedEventDelay = 50 * time.Millisecond

type channelSmartScheduleProjectedRouteKey struct {
	channelId int
	modelName string
}

type channelSmartScheduleProjectedRouteFlags struct {
	protect     bool
	rateLimited bool
}

var channelSmartScheduleProjectedEvents = struct {
	sync.Mutex
	pending map[channelSmartScheduleProjectedRouteKey]channelSmartScheduleProjectedRouteFlags
	running bool
}{
	pending: make(map[channelSmartScheduleProjectedRouteKey]channelSmartScheduleProjectedRouteFlags),
}

func init() {
	service.RegisterChannelMonitorEventProjectedHandler(handleChannelSmartScheduleProjectedEvents)
}

func handleChannelSmartScheduleProjectedEvents(events []model.ChannelMonitorEvent) {
	affected := make(map[channelSmartScheduleProjectedRouteKey]channelSmartScheduleProjectedRouteFlags)
	for _, event := range events {
		if !channelSmartScheduleEventAffectsScheduling(event) {
			continue
		}
		modelName := ratio_setting.FormatMatchingModelName(strings.TrimSpace(event.ModelName))
		if event.ChannelId <= 0 || modelName == "" {
			continue
		}
		key := channelSmartScheduleProjectedRouteKey{channelId: event.ChannelId, modelName: modelName}
		flags := affected[key]
		if event.Source == model.ChannelMonitorEventSourceBusiness &&
			event.RuntimeProtectionEligible && event.RequestDispatched &&
			event.Outcome == model.ChannelMonitorEventOutcomeFailure && !event.FinalRetrySummary {
			if event.StatusCode != nil && *event.StatusCode == http.StatusTooManyRequests {
				flags.rateLimited = true
			} else {
				flags.protect = true
			}
		}
		affected[key] = flags
	}
	if len(affected) == 0 {
		return
	}

	channelSmartScheduleProjectedEvents.Lock()
	for key, flags := range affected {
		current := channelSmartScheduleProjectedEvents.pending[key]
		current.protect = current.protect || flags.protect
		current.rateLimited = current.rateLimited || flags.rateLimited
		channelSmartScheduleProjectedEvents.pending[key] = current
	}
	if channelSmartScheduleProjectedEvents.running {
		channelSmartScheduleProjectedEvents.Unlock()
		return
	}
	channelSmartScheduleProjectedEvents.running = true
	channelSmartScheduleProjectedEvents.Unlock()
	go runChannelSmartScheduleProjectedEventWorker()
}

func runChannelSmartScheduleProjectedEventWorker() {
	timer := time.NewTimer(channelSmartScheduleProjectedEventDelay)
	defer timer.Stop()
	<-timer.C

	channelSmartScheduleProjectedEvents.Lock()
	pending := channelSmartScheduleProjectedEvents.pending
	channelSmartScheduleProjectedEvents.pending = make(
		map[channelSmartScheduleProjectedRouteKey]channelSmartScheduleProjectedRouteFlags,
	)
	channelSmartScheduleProjectedEvents.running = false
	channelSmartScheduleProjectedEvents.Unlock()

	keys := make([]channelSmartScheduleProjectedRouteKey, 0, len(pending))
	for key := range pending {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i int, j int) bool {
		if keys[i].modelName != keys[j].modelName {
			return keys[i].modelName < keys[j].modelName
		}
		return keys[i].channelId < keys[j].channelId
	})
	for _, key := range keys {
		flags := pending[key]
		if flags.rateLimited {
			protectChannelSmartScheduleProjectedRateLimit(key.channelId, key.modelName)
		}
		if flags.protect {
			protectChannelSmartScheduleProjectedFailure(key.channelId, key.modelName)
		}
		enqueueChannelSmartScheduleAdaptiveRefresh(key.channelId, key.modelName)
	}
}

func channelSmartScheduleEventAffectsScheduling(event model.ChannelMonitorEvent) bool {
	return event.SchedulingEligible
}

func channelSmartScheduleRealtimeEvents(
	channelId int,
	modelName string,
	windowStart int64,
	observationSince int64,
	maxRequests int,
) ([]model.ChannelMonitorEvent, service.ChannelMonitorRealtimeSnapshot) {
	window, available := service.GetChannelMonitorRealtimeWindow(channelId, modelName)
	if !available {
		return nil, service.ChannelMonitorRealtimeSnapshot{
			CoverageStart: service.GetChannelMonitorRealtimeProjectionCoverageStart(),
		}
	}
	windowStart = max(windowStart, observationSince)
	events := make([]model.ChannelMonitorEvent, 0, len(window.Events))
	for _, event := range window.Events {
		if event.OccurredAt < windowStart || !channelSmartScheduleEventAffectsScheduling(event) ||
			!event.RequestDispatched || event.FinalRetrySummary ||
			event.Outcome == model.ChannelMonitorEventOutcomeCanceled ||
			event.Outcome == model.ChannelMonitorEventOutcomeUnresolved {
			continue
		}
		if event.StatusCode != nil && *event.StatusCode == http.StatusTooManyRequests {
			continue
		}
		events = append(events, event)
	}
	if maxRequests > 0 && len(events) > maxRequests {
		events = events[len(events)-maxRequests:]
	}
	return events, window.Snapshot
}

func channelSmartScheduleRealtimeAdaptiveMetric(
	channelId int,
	modelName string,
	windowStart int64,
	observationSince int64,
	maxRequests int,
	warningSeconds float64,
	criticalSeconds float64,
) (model.ChannelSmartScheduleAdaptiveHealthMetric, service.ChannelMonitorRealtimeSnapshot) {
	events, snapshot := channelSmartScheduleRealtimeEvents(
		channelId, modelName, windowStart, observationSince, maxRequests,
	)
	return channelSmartScheduleRealtimeAdaptiveMetricFromEvents(
		events, warningSeconds, criticalSeconds,
	), snapshot
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

func resetChannelSmartScheduleProjectedEventsForTest() {
	channelSmartScheduleProjectedEvents.Lock()
	channelSmartScheduleProjectedEvents.pending = make(
		map[channelSmartScheduleProjectedRouteKey]channelSmartScheduleProjectedRouteFlags,
	)
	channelSmartScheduleProjectedEvents.running = false
	channelSmartScheduleProjectedEvents.Unlock()
	_ = common.GetTimestamp()
}
