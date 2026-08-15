package service

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

const (
	channelMonitorRealtimeEventsPerRoute = 1000
	channelMonitorRealtimeMaxRoutes      = 512
	channelMonitorRealtimeDedupCapacity  = 131072
	channelMonitorRealtimeDailyCostDays  = 8
)

type ChannelMonitorRealtimeScope string

const (
	ChannelMonitorRealtimeScopeGlobal  ChannelMonitorRealtimeScope = "global"
	ChannelMonitorRealtimeScopeChannel ChannelMonitorRealtimeScope = "channel"
	ChannelMonitorRealtimeScopeModel   ChannelMonitorRealtimeScope = "model"
	ChannelMonitorRealtimeScopeRoute   ChannelMonitorRealtimeScope = "route"
)

type ChannelMonitorRealtimeDailyCost struct {
	DayStart                         int64 `json:"day_start"`
	SettledCostNanoCNY               int64 `json:"settled_cost_nano_cny"`
	UnresolvedCostNanoCNY            int64 `json:"unresolved_cost_nano_cny"`
	ProbeSettledCostNanoCNY          int64 `json:"probe_settled_cost_nano_cny"`
	ModelDetectionSettledCostNanoCNY int64 `json:"model_detection_settled_cost_nano_cny"`
	SettledRequestCount              int64 `json:"settled_request_count"`
	UnresolvedRequestCount           int64 `json:"unresolved_request_count"`
}

type ChannelMonitorRealtimeSnapshot struct {
	Scope     ChannelMonitorRealtimeScope `json:"scope"`
	ChannelId int                         `json:"channel_id,omitempty"`
	ModelName string                      `json:"model,omitempty"`

	EventCount           int64   `json:"event_count"`
	BusinessRequestCount int64   `json:"business_request_count"`
	ActualSuccessCount   int64   `json:"actual_success_count"`
	ActualFailureCount   int64   `json:"actual_failure_count"`
	ActualSampleCount    int64   `json:"actual_sample_count"`
	ActualSuccessRate    float64 `json:"actual_success_rate"`
	FinalSuccessCount    int64   `json:"final_success_count"`
	FinalFailureCount    int64   `json:"final_failure_count"`
	FinalSampleCount     int64   `json:"final_sample_count"`
	FinalSuccessRate     float64 `json:"final_success_rate"`

	FirstTokenSampleCount int64    `json:"first_token_sample_count"`
	FirstTokenTotalMs     float64  `json:"first_token_total_ms"`
	AverageFirstTokenMs   *float64 `json:"average_first_token_ms"`
	TPSSampleCount        int64    `json:"tps_sample_count"`
	TPSTotal              float64  `json:"tps_total"`
	AverageTPS            *float64 `json:"average_tps"`

	CacheSampleCount       int64   `json:"cache_sample_count"`
	CacheHitCount          int64   `json:"cache_hit_count"`
	CacheHitRate           float64 `json:"cache_hit_rate"`
	CacheReadRequestCount  int64   `json:"cache_read_request_count"`
	CacheWriteRequestCount int64   `json:"cache_write_request_count"`
	CacheReadTokens        int64   `json:"cache_read_tokens"`
	CacheWriteTokens       int64   `json:"cache_write_tokens"`
	InputTokens            int64   `json:"input_tokens"`
	CacheUtilizationRate   float64 `json:"cache_utilization_rate"`

	SettledCostNanoCNY          int64                             `json:"settled_cost_nano_cny"`
	UnresolvedCostNanoCNY       int64                             `json:"unresolved_cost_nano_cny"`
	UnresolvedRequestCount      int64                             `json:"unresolved_request_count"`
	TodayDayStart               int64                             `json:"today_day_start"`
	TodaySettledCostNanoCNY     int64                             `json:"today_settled_cost_nano_cny"`
	TodayUnresolvedCostNanoCNY  int64                             `json:"today_unresolved_cost_nano_cny"`
	TodaySettledRequestCount    int64                             `json:"today_settled_request_count"`
	TodayUnresolvedRequestCount int64                             `json:"today_unresolved_request_count"`
	DailyCosts                  []ChannelMonitorRealtimeDailyCost `json:"daily_costs"`

	SourceCounts   map[model.ChannelMonitorEventSource]int64 `json:"source_counts"`
	WindowStart    int64                                     `json:"window_start"`
	WindowEnd      int64                                     `json:"window_end"`
	CoverageStart  int64                                     `json:"coverage_start"`
	DataCutoffAt   int64                                     `json:"data_cutoff_at"`
	ProcessedAt    int64                                     `json:"processed_at"`
	EventWatermark uint64                                    `json:"event_watermark"`
}

type ChannelMonitorRealtimeWindow struct {
	Snapshot ChannelMonitorRealtimeSnapshot `json:"snapshot"`
	Events   []model.ChannelMonitorEvent    `json:"events"`
}

type channelMonitorRealtimeRouteKey struct {
	channelId int
	modelName string
}

type channelMonitorRealtimeMetrics struct {
	eventCount             int64
	businessRequestCount   int64
	actualSuccessCount     int64
	actualFailureCount     int64
	finalSuccessCount      int64
	finalFailureCount      int64
	firstTokenSampleCount  int64
	firstTokenTotalMs      float64
	tPSSampleCount         int64
	tPSTotal               float64
	cacheSampleCount       int64
	cacheHitCount          int64
	cacheReadRequestCount  int64
	cacheWriteRequestCount int64
	cacheReadTokens        int64
	cacheWriteTokens       int64
	inputTokens            int64
	settledCostNanoCNY     int64
	unresolvedCostNanoCNY  int64
	unresolvedRequestCount int64
	sourceCounts           map[model.ChannelMonitorEventSource]int64
}

type channelMonitorRealtimeRoute struct {
	events           []model.ChannelMonitorEvent
	metrics          channelMonitorRealtimeMetrics
	dailyCosts       map[int64]ChannelMonitorRealtimeDailyCost
	costEvents       map[string]model.ChannelMonitorEvent
	dataCutoffAt     int64
	processedAt      int64
	eventWatermark   uint64
	truncatedThrough int64
	touchedAt        uint64
	snapshot         ChannelMonitorRealtimeSnapshot
}

type channelMonitorRealtimeProjectionConfig struct {
	eventsPerRoute int
	maxRoutes      int
	dedupCapacity  int
	dailyCostDays  int
	now            func() time.Time
}

type channelMonitorRealtimeProjection struct {
	mu sync.RWMutex

	config         channelMonitorRealtimeProjectionConfig
	startedAt      int64
	routes         map[channelMonitorRealtimeRouteKey]*channelMonitorRealtimeRoute
	seen           map[string]struct{}
	seenOrder      []string
	seenNext       int
	touchSequence  uint64
	evictedThrough int64

	globalSnapshot   ChannelMonitorRealtimeSnapshot
	channelSnapshots map[int]ChannelMonitorRealtimeSnapshot
	modelSnapshots   map[string]ChannelMonitorRealtimeSnapshot
}

func defaultChannelMonitorRealtimeProjectionConfig() channelMonitorRealtimeProjectionConfig {
	return channelMonitorRealtimeProjectionConfig{
		eventsPerRoute: channelMonitorRealtimeEventsPerRoute,
		maxRoutes:      channelMonitorRealtimeMaxRoutes,
		dedupCapacity:  channelMonitorRealtimeDedupCapacity,
		dailyCostDays:  channelMonitorRealtimeDailyCostDays,
		now:            time.Now,
	}
}

func newChannelMonitorRealtimeProjection(config channelMonitorRealtimeProjectionConfig) *channelMonitorRealtimeProjection {
	if config.eventsPerRoute <= 0 {
		config.eventsPerRoute = channelMonitorRealtimeEventsPerRoute
	}
	if config.maxRoutes <= 0 {
		config.maxRoutes = channelMonitorRealtimeMaxRoutes
	}
	if config.dedupCapacity <= 0 {
		config.dedupCapacity = channelMonitorRealtimeDedupCapacity
	}
	if config.dailyCostDays <= 0 {
		config.dailyCostDays = channelMonitorRealtimeDailyCostDays
	}
	if config.now == nil {
		config.now = time.Now
	}
	projection := &channelMonitorRealtimeProjection{
		config:           config,
		startedAt:        config.now().Unix(),
		routes:           make(map[channelMonitorRealtimeRouteKey]*channelMonitorRealtimeRoute),
		seen:             make(map[string]struct{}, config.dedupCapacity),
		seenOrder:        make([]string, 0, config.dedupCapacity),
		channelSnapshots: make(map[int]ChannelMonitorRealtimeSnapshot),
		modelSnapshots:   make(map[string]ChannelMonitorRealtimeSnapshot),
	}
	projection.globalSnapshot = ChannelMonitorRealtimeSnapshot{
		Scope:         ChannelMonitorRealtimeScopeGlobal,
		CoverageStart: projection.startedAt,
		SourceCounts:  map[model.ChannelMonitorEventSource]int64{},
		DailyCosts:    []ChannelMonitorRealtimeDailyCost{},
	}
	return projection
}

func (projection *channelMonitorRealtimeProjection) consume(ctx context.Context, events []model.ChannelMonitorEvent) error {
	normalized := make([]model.ChannelMonitorEvent, len(events))
	for index, event := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := event.Validate(); err != nil {
			return err
		}
		event.ModelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(event.ModelName))
		normalized[index] = cloneChannelMonitorEvent(event)
	}
	if len(normalized) == 0 {
		return nil
	}

	processedAt := projection.config.now().Unix()
	todayStart := model.ChannelDailyCostDayStart(processedAt)
	projection.mu.Lock()
	defer projection.mu.Unlock()

	affectedRoutes := make(map[channelMonitorRealtimeRouteKey]struct{})
	for _, event := range normalized {
		if _, duplicate := projection.seen[event.EventId]; duplicate {
			continue
		}
		projection.rememberEventId(event.EventId)
		event.ProcessedAt = processedAt

		key := channelMonitorRealtimeRouteKey{channelId: event.ChannelId, modelName: event.ModelName}
		route := projection.routes[key]
		if route == nil {
			projection.makeRouteCapacity()
			route = &channelMonitorRealtimeRoute{
				events:     make([]model.ChannelMonitorEvent, 0, projection.config.eventsPerRoute),
				dailyCosts: make(map[int64]ChannelMonitorRealtimeDailyCost, projection.config.dailyCostDays),
				costEvents: make(map[string]model.ChannelMonitorEvent),
			}
			projection.routes[key] = route
		}
		projection.touchSequence++
		route.touchedAt = projection.touchSequence
		route.processedAt = max(route.processedAt, processedAt)
		route.dataCutoffAt = max(route.dataCutoffAt, event.OccurredAt)
		route.eventWatermark = max(route.eventWatermark, event.EventSequence)
		costEventId := channelMonitorRealtimeCostEventId(event)
		replacedCostEvent := false
		if costEventId != "" && !event.RequestDispatched {
			replacedCostEvent = true
		}
		if costEventId != "" {
			if previous, exists := route.costEvents[costEventId]; exists {
				if !channelMonitorRealtimeCostEventNewer(previous, event) {
					continue
				}
				projection.removeDailyCost(route, previous)
				if !event.RequestDispatched {
					replacedCostEvent = true
				} else {
					for index := range route.events {
						if route.events[index].EventId == previous.EventId {
							route.events[index] = event
							replacedCostEvent = true
							break
						}
					}
				}
			}
			route.costEvents[costEventId] = event
		}
		if !replacedCostEvent {
			route.events = append(route.events, event)
		}
		projection.addDailyCost(route, event)
		affectedRoutes[key] = struct{}{}
	}
	for key := range affectedRoutes {
		route := projection.routes[key]
		if route == nil {
			continue
		}
		sort.SliceStable(route.events, func(i int, j int) bool {
			return channelMonitorRealtimeEventLess(route.events[i], route.events[j])
		})
		if overflow := len(route.events) - projection.config.eventsPerRoute; overflow > 0 {
			for _, removed := range route.events[:overflow] {
				route.truncatedThrough = max(route.truncatedThrough, removed.OccurredAt)
			}
			copy(route.events, route.events[overflow:])
			route.events = route.events[:len(route.events)-overflow]
		}
		route.metrics = aggregateChannelMonitorRealtimeEvents(route.events)
		projection.trimDailyCosts(route, todayStart)
		windowStart := int64(0)
		windowEnd := int64(0)
		if len(route.events) > 0 {
			windowStart = route.events[0].OccurredAt
			windowEnd = route.events[len(route.events)-1].OccurredAt
		}
		route.snapshot = buildChannelMonitorRealtimeSnapshot(
			ChannelMonitorRealtimeScopeRoute,
			key.channelId,
			key.modelName,
			route.metrics,
			route.dailyCosts,
			windowStart,
			windowEnd,
			route.dataCutoffAt,
			route.processedAt,
			route.eventWatermark,
			projection.routeCoverageStart(route),
			todayStart,
		)
	}
	projection.rebuildScopeSnapshots(todayStart)
	return nil
}

func (projection *channelMonitorRealtimeProjection) makeRouteCapacity() {
	if len(projection.routes) < projection.config.maxRoutes {
		return
	}
	var evictKey channelMonitorRealtimeRouteKey
	var oldestTouch uint64
	first := true
	for key, route := range projection.routes {
		if first || route.touchedAt < oldestTouch {
			first = false
			evictKey = key
			oldestTouch = route.touchedAt
		}
	}
	evictedRoute := projection.routes[evictKey]
	projection.evictedThrough = max(projection.evictedThrough, evictedRoute.snapshot.WindowEnd)
	delete(projection.routes, evictKey)
}

func (projection *channelMonitorRealtimeProjection) routeCoverageStart(route *channelMonitorRealtimeRoute) int64 {
	coverageStart := projection.startedAt
	if projection.evictedThrough > 0 {
		coverageStart = max(coverageStart, channelMonitorRealtimeNextTimestamp(projection.evictedThrough))
	}
	if route != nil && route.truncatedThrough > 0 {
		coverageStart = max(coverageStart, channelMonitorRealtimeNextTimestamp(route.truncatedThrough))
	}
	return coverageStart
}

func channelMonitorRealtimeNextTimestamp(timestamp int64) int64 {
	if timestamp >= math.MaxInt64 {
		return math.MaxInt64
	}
	return timestamp + 1
}

func (projection *channelMonitorRealtimeProjection) rememberEventId(eventId string) {
	projection.seen[eventId] = struct{}{}
	if len(projection.seenOrder) < projection.config.dedupCapacity {
		projection.seenOrder = append(projection.seenOrder, eventId)
		return
	}
	evicted := projection.seenOrder[projection.seenNext]
	delete(projection.seen, evicted)
	projection.seenOrder[projection.seenNext] = eventId
	projection.seenNext = (projection.seenNext + 1) % projection.config.dedupCapacity
}

func (projection *channelMonitorRealtimeProjection) addDailyCost(route *channelMonitorRealtimeRoute, event model.ChannelMonitorEvent) {
	if event.FinalRetrySummary || event.CostStatus == model.ChannelMonitorEventCostNone {
		return
	}
	dayStart := model.ChannelDailyCostDayStart(event.OccurredAt)
	cost := route.dailyCosts[dayStart]
	cost.DayStart = dayStart
	if event.CostStatus == model.ChannelMonitorEventCostSettled {
		cost.SettledCostNanoCNY = channelMonitorRealtimeAddInt64(cost.SettledCostNanoCNY, event.SettledCostNanoCNY)
		switch event.Source {
		case model.ChannelMonitorEventSourceStatusProbe, model.ChannelMonitorEventSourceSmartProbe:
			cost.ProbeSettledCostNanoCNY = channelMonitorRealtimeAddInt64(cost.ProbeSettledCostNanoCNY, event.SettledCostNanoCNY)
		case model.ChannelMonitorEventSourceModelDetection:
			cost.ModelDetectionSettledCostNanoCNY = channelMonitorRealtimeAddInt64(cost.ModelDetectionSettledCostNanoCNY, event.SettledCostNanoCNY)
		}
		cost.SettledRequestCount = channelMonitorRealtimeAddInt64(cost.SettledRequestCount, 1)
	}
	if event.CostStatus == model.ChannelMonitorEventCostUnresolved {
		cost.UnresolvedCostNanoCNY = channelMonitorRealtimeAddInt64(cost.UnresolvedCostNanoCNY, event.UnresolvedCostNanoCNY)
		cost.UnresolvedRequestCount = channelMonitorRealtimeAddInt64(cost.UnresolvedRequestCount, 1)
	}
	route.dailyCosts[dayStart] = cost
}

func (projection *channelMonitorRealtimeProjection) removeDailyCost(route *channelMonitorRealtimeRoute, event model.ChannelMonitorEvent) {
	if event.FinalRetrySummary || event.CostStatus == model.ChannelMonitorEventCostNone {
		return
	}
	dayStart := model.ChannelDailyCostDayStart(event.OccurredAt)
	cost, exists := route.dailyCosts[dayStart]
	if !exists {
		return
	}
	if event.CostStatus == model.ChannelMonitorEventCostSettled {
		cost.SettledCostNanoCNY = channelMonitorRealtimeSubtractInt64(cost.SettledCostNanoCNY, event.SettledCostNanoCNY)
		switch event.Source {
		case model.ChannelMonitorEventSourceStatusProbe, model.ChannelMonitorEventSourceSmartProbe:
			cost.ProbeSettledCostNanoCNY = channelMonitorRealtimeSubtractInt64(cost.ProbeSettledCostNanoCNY, event.SettledCostNanoCNY)
		case model.ChannelMonitorEventSourceModelDetection:
			cost.ModelDetectionSettledCostNanoCNY = channelMonitorRealtimeSubtractInt64(cost.ModelDetectionSettledCostNanoCNY, event.SettledCostNanoCNY)
		}
		cost.SettledRequestCount = channelMonitorRealtimeSubtractInt64(cost.SettledRequestCount, 1)
	}
	if event.CostStatus == model.ChannelMonitorEventCostUnresolved {
		cost.UnresolvedCostNanoCNY = channelMonitorRealtimeSubtractInt64(cost.UnresolvedCostNanoCNY, event.UnresolvedCostNanoCNY)
		cost.UnresolvedRequestCount = channelMonitorRealtimeSubtractInt64(cost.UnresolvedRequestCount, 1)
	}
	if cost.SettledCostNanoCNY == 0 && cost.UnresolvedCostNanoCNY == 0 &&
		cost.SettledRequestCount == 0 && cost.UnresolvedRequestCount == 0 {
		delete(route.dailyCosts, dayStart)
		return
	}
	route.dailyCosts[dayStart] = cost
}

func channelMonitorRealtimeCostEventId(event model.ChannelMonitorEvent) string {
	if strings.TrimSpace(event.OtherJson) == "" {
		return ""
	}
	var other struct {
		CostEventId string `json:"cost_event_id"`
	}
	if err := common.UnmarshalJsonStr(event.OtherJson, &other); err != nil || strings.TrimSpace(other.CostEventId) == "" {
		return ""
	}
	return string(event.Source) + ":" + strings.TrimSpace(other.CostEventId)
}

func channelMonitorRealtimeCostEventNewer(previous model.ChannelMonitorEvent, current model.ChannelMonitorEvent) bool {
	previousRank := channelMonitorRealtimeCostStatusRank(previous.CostStatus)
	currentRank := channelMonitorRealtimeCostStatusRank(current.CostStatus)
	if currentRank != previousRank {
		return currentRank > previousRank
	}
	if current.EventSequence != previous.EventSequence {
		return current.EventSequence > previous.EventSequence
	}
	return current.CreatedAt >= previous.CreatedAt
}

func channelMonitorRealtimeCostStatusRank(status model.ChannelMonitorEventCostStatus) int {
	switch status {
	case model.ChannelMonitorEventCostSettled:
		return 2
	case model.ChannelMonitorEventCostUnresolved:
		return 1
	default:
		return 0
	}
}

func (projection *channelMonitorRealtimeProjection) trimDailyCosts(route *channelMonitorRealtimeRoute, todayStart int64) {
	if len(route.dailyCosts) <= projection.config.dailyCostDays {
		return
	}
	days := make([]int64, 0, len(route.dailyCosts))
	for dayStart := range route.dailyCosts {
		days = append(days, dayStart)
	}
	sort.Slice(days, func(i int, j int) bool { return days[i] > days[j] })
	kept := make(map[int64]struct{}, projection.config.dailyCostDays)
	if _, ok := route.dailyCosts[todayStart]; ok {
		kept[todayStart] = struct{}{}
	}
	for _, dayStart := range days {
		if len(kept) >= projection.config.dailyCostDays {
			break
		}
		kept[dayStart] = struct{}{}
	}
	for dayStart := range route.dailyCosts {
		if _, ok := kept[dayStart]; !ok {
			delete(route.dailyCosts, dayStart)
		}
	}
}

func (projection *channelMonitorRealtimeProjection) rebuildScopeSnapshots(todayStart int64) {
	channelMetrics := make(map[int]channelMonitorRealtimeMetrics)
	channelCosts := make(map[int]map[int64]ChannelMonitorRealtimeDailyCost)
	channelWindowStarts := make(map[int]int64)
	channelWindowEnds := make(map[int]int64)
	channelCutoffs := make(map[int]int64)
	channelProcessed := make(map[int]int64)
	channelWatermarks := make(map[int]uint64)
	channelCoverageStarts := make(map[int]int64)
	modelMetrics := make(map[string]channelMonitorRealtimeMetrics)
	modelCosts := make(map[string]map[int64]ChannelMonitorRealtimeDailyCost)
	modelWindowStarts := make(map[string]int64)
	modelWindowEnds := make(map[string]int64)
	modelCutoffs := make(map[string]int64)
	modelProcessed := make(map[string]int64)
	modelWatermarks := make(map[string]uint64)
	modelCoverageStarts := make(map[string]int64)
	globalMetrics := newChannelMonitorRealtimeMetrics()
	globalCosts := make(map[int64]ChannelMonitorRealtimeDailyCost)
	var globalWindowStart int64
	var globalWindowEnd int64
	var globalCutoff int64
	var globalProcessed int64
	var globalWatermark uint64
	globalCoverageStart := projection.routeCoverageStart(nil)

	for key, route := range projection.routes {
		metrics := channelMetrics[key.channelId]
		mergeChannelMonitorRealtimeMetrics(&metrics, route.metrics)
		channelMetrics[key.channelId] = metrics
		if channelCosts[key.channelId] == nil {
			channelCosts[key.channelId] = make(map[int64]ChannelMonitorRealtimeDailyCost)
		}
		mergeChannelMonitorRealtimeDailyCosts(channelCosts[key.channelId], route.dailyCosts)
		channelWindowStarts[key.channelId] = channelMonitorRealtimeEarlierTimestamp(channelWindowStarts[key.channelId], route.snapshot.WindowStart)
		channelWindowEnds[key.channelId] = max(channelWindowEnds[key.channelId], route.snapshot.WindowEnd)
		channelCutoffs[key.channelId] = max(channelCutoffs[key.channelId], route.dataCutoffAt)
		channelProcessed[key.channelId] = max(channelProcessed[key.channelId], route.processedAt)
		channelWatermarks[key.channelId] = max(channelWatermarks[key.channelId], route.eventWatermark)
		channelCoverageStarts[key.channelId] = max(channelCoverageStarts[key.channelId], route.snapshot.CoverageStart)

		metrics = modelMetrics[key.modelName]
		mergeChannelMonitorRealtimeMetrics(&metrics, route.metrics)
		modelMetrics[key.modelName] = metrics
		if modelCosts[key.modelName] == nil {
			modelCosts[key.modelName] = make(map[int64]ChannelMonitorRealtimeDailyCost)
		}
		mergeChannelMonitorRealtimeDailyCosts(modelCosts[key.modelName], route.dailyCosts)
		modelWindowStarts[key.modelName] = channelMonitorRealtimeEarlierTimestamp(modelWindowStarts[key.modelName], route.snapshot.WindowStart)
		modelWindowEnds[key.modelName] = max(modelWindowEnds[key.modelName], route.snapshot.WindowEnd)
		modelCutoffs[key.modelName] = max(modelCutoffs[key.modelName], route.dataCutoffAt)
		modelProcessed[key.modelName] = max(modelProcessed[key.modelName], route.processedAt)
		modelWatermarks[key.modelName] = max(modelWatermarks[key.modelName], route.eventWatermark)
		modelCoverageStarts[key.modelName] = max(modelCoverageStarts[key.modelName], route.snapshot.CoverageStart)

		mergeChannelMonitorRealtimeMetrics(&globalMetrics, route.metrics)
		mergeChannelMonitorRealtimeDailyCosts(globalCosts, route.dailyCosts)
		globalWindowStart = channelMonitorRealtimeEarlierTimestamp(globalWindowStart, route.snapshot.WindowStart)
		globalWindowEnd = max(globalWindowEnd, route.snapshot.WindowEnd)
		globalCutoff = max(globalCutoff, route.dataCutoffAt)
		globalProcessed = max(globalProcessed, route.processedAt)
		globalWatermark = max(globalWatermark, route.eventWatermark)
		globalCoverageStart = max(globalCoverageStart, route.snapshot.CoverageStart)
	}

	projection.channelSnapshots = make(map[int]ChannelMonitorRealtimeSnapshot, len(channelMetrics))
	for channelId, metrics := range channelMetrics {
		projection.channelSnapshots[channelId] = buildChannelMonitorRealtimeSnapshot(
			ChannelMonitorRealtimeScopeChannel, channelId, "", metrics,
			channelCosts[channelId], channelWindowStarts[channelId], channelWindowEnds[channelId], channelCutoffs[channelId],
			channelProcessed[channelId], channelWatermarks[channelId],
			max(channelCoverageStarts[channelId], projection.routeCoverageStart(nil)), todayStart,
		)
	}
	projection.modelSnapshots = make(map[string]ChannelMonitorRealtimeSnapshot, len(modelMetrics))
	for modelName, metrics := range modelMetrics {
		projection.modelSnapshots[modelName] = buildChannelMonitorRealtimeSnapshot(
			ChannelMonitorRealtimeScopeModel, 0, modelName, metrics,
			modelCosts[modelName], modelWindowStarts[modelName], modelWindowEnds[modelName], modelCutoffs[modelName],
			modelProcessed[modelName], modelWatermarks[modelName],
			max(modelCoverageStarts[modelName], projection.routeCoverageStart(nil)), todayStart,
		)
	}
	projection.globalSnapshot = buildChannelMonitorRealtimeSnapshot(
		ChannelMonitorRealtimeScopeGlobal, 0, "", globalMetrics, globalCosts, globalWindowStart, globalWindowEnd,
		globalCutoff, globalProcessed, globalWatermark, globalCoverageStart, todayStart,
	)
}

func channelMonitorRealtimeEventLess(left model.ChannelMonitorEvent, right model.ChannelMonitorEvent) bool {
	if left.OccurredAt != right.OccurredAt {
		return left.OccurredAt < right.OccurredAt
	}
	if left.EventSequence != right.EventSequence {
		return left.EventSequence < right.EventSequence
	}
	return left.EventId < right.EventId
}

func channelMonitorRealtimeEarlierTimestamp(current int64, candidate int64) int64 {
	if current == 0 || candidate > 0 && candidate < current {
		return candidate
	}
	return current
}

func newChannelMonitorRealtimeMetrics() channelMonitorRealtimeMetrics {
	return channelMonitorRealtimeMetrics{sourceCounts: make(map[model.ChannelMonitorEventSource]int64)}
}

func aggregateChannelMonitorRealtimeEvents(events []model.ChannelMonitorEvent) channelMonitorRealtimeMetrics {
	metrics := newChannelMonitorRealtimeMetrics()
	for _, event := range events {
		metrics.eventCount = channelMonitorRealtimeAddInt64(metrics.eventCount, 1)
		metrics.sourceCounts[event.Source] = channelMonitorRealtimeAddInt64(metrics.sourceCounts[event.Source], 1)
		if event.Source == model.ChannelMonitorEventSourceBusiness && event.FinalRetrySummary {
			if event.Outcome == model.ChannelMonitorEventOutcomeFailure {
				metrics.finalFailureCount = channelMonitorRealtimeAddInt64(metrics.finalFailureCount, 1)
			}
			continue
		}
		metrics.settledCostNanoCNY = channelMonitorRealtimeAddInt64(metrics.settledCostNanoCNY, event.SettledCostNanoCNY)
		metrics.unresolvedCostNanoCNY = channelMonitorRealtimeAddInt64(metrics.unresolvedCostNanoCNY, event.UnresolvedCostNanoCNY)
		if event.CostStatus == model.ChannelMonitorEventCostUnresolved {
			metrics.unresolvedRequestCount = channelMonitorRealtimeAddInt64(metrics.unresolvedRequestCount, 1)
		}
		if event.Source != model.ChannelMonitorEventSourceBusiness {
			continue
		}
		if !event.RequestDispatched {
			continue
		}
		switch event.Outcome {
		case model.ChannelMonitorEventOutcomeSuccess:
			metrics.actualSuccessCount = channelMonitorRealtimeAddInt64(metrics.actualSuccessCount, 1)
			if event.IsFinalAttempt {
				metrics.finalSuccessCount = channelMonitorRealtimeAddInt64(metrics.finalSuccessCount, 1)
			}
		case model.ChannelMonitorEventOutcomeFailure:
			metrics.actualFailureCount = channelMonitorRealtimeAddInt64(metrics.actualFailureCount, 1)
			if event.IsFinalAttempt {
				metrics.finalFailureCount = channelMonitorRealtimeAddInt64(metrics.finalFailureCount, 1)
			}
		default:
			continue
		}
		metrics.businessRequestCount = channelMonitorRealtimeAddInt64(metrics.businessRequestCount, 1)
		if event.FirstTokenMs != nil {
			metrics.firstTokenSampleCount = channelMonitorRealtimeAddInt64(metrics.firstTokenSampleCount, 1)
			metrics.firstTokenTotalMs = channelMonitorRealtimeAddFloat64(metrics.firstTokenTotalMs, *event.FirstTokenMs)
		}
		if event.TPS != nil {
			metrics.tPSSampleCount = channelMonitorRealtimeAddInt64(metrics.tPSSampleCount, 1)
			metrics.tPSTotal = channelMonitorRealtimeAddFloat64(metrics.tPSTotal, *event.TPS)
		}
		inputTokens := int64(0)
		if event.InputTokens != nil {
			inputTokens = *event.InputTokens
		} else if event.PromptTokens != nil {
			inputTokens = *event.PromptTokens
		}
		if inputTokens > 0 {
			metrics.cacheSampleCount = channelMonitorRealtimeAddInt64(metrics.cacheSampleCount, 1)
			metrics.inputTokens = channelMonitorRealtimeAddInt64(metrics.inputTokens, inputTokens)
		}
		if event.CacheReadTokens != nil && *event.CacheReadTokens > 0 {
			metrics.cacheHitCount = channelMonitorRealtimeAddInt64(metrics.cacheHitCount, 1)
			metrics.cacheReadRequestCount = channelMonitorRealtimeAddInt64(metrics.cacheReadRequestCount, 1)
			metrics.cacheReadTokens = channelMonitorRealtimeAddInt64(metrics.cacheReadTokens, *event.CacheReadTokens)
		}
		if event.CacheWriteTokens != nil && *event.CacheWriteTokens > 0 {
			metrics.cacheWriteRequestCount = channelMonitorRealtimeAddInt64(metrics.cacheWriteRequestCount, 1)
			metrics.cacheWriteTokens = channelMonitorRealtimeAddInt64(metrics.cacheWriteTokens, *event.CacheWriteTokens)
		}
	}
	return metrics
}

func mergeChannelMonitorRealtimeMetrics(target *channelMonitorRealtimeMetrics, source channelMonitorRealtimeMetrics) {
	if target.sourceCounts == nil {
		target.sourceCounts = make(map[model.ChannelMonitorEventSource]int64)
	}
	target.eventCount = channelMonitorRealtimeAddInt64(target.eventCount, source.eventCount)
	target.businessRequestCount = channelMonitorRealtimeAddInt64(target.businessRequestCount, source.businessRequestCount)
	target.actualSuccessCount = channelMonitorRealtimeAddInt64(target.actualSuccessCount, source.actualSuccessCount)
	target.actualFailureCount = channelMonitorRealtimeAddInt64(target.actualFailureCount, source.actualFailureCount)
	target.finalSuccessCount = channelMonitorRealtimeAddInt64(target.finalSuccessCount, source.finalSuccessCount)
	target.finalFailureCount = channelMonitorRealtimeAddInt64(target.finalFailureCount, source.finalFailureCount)
	target.firstTokenSampleCount = channelMonitorRealtimeAddInt64(target.firstTokenSampleCount, source.firstTokenSampleCount)
	target.firstTokenTotalMs = channelMonitorRealtimeAddFloat64(target.firstTokenTotalMs, source.firstTokenTotalMs)
	target.tPSSampleCount = channelMonitorRealtimeAddInt64(target.tPSSampleCount, source.tPSSampleCount)
	target.tPSTotal = channelMonitorRealtimeAddFloat64(target.tPSTotal, source.tPSTotal)
	target.cacheSampleCount = channelMonitorRealtimeAddInt64(target.cacheSampleCount, source.cacheSampleCount)
	target.cacheHitCount = channelMonitorRealtimeAddInt64(target.cacheHitCount, source.cacheHitCount)
	target.cacheReadRequestCount = channelMonitorRealtimeAddInt64(target.cacheReadRequestCount, source.cacheReadRequestCount)
	target.cacheWriteRequestCount = channelMonitorRealtimeAddInt64(target.cacheWriteRequestCount, source.cacheWriteRequestCount)
	target.cacheReadTokens = channelMonitorRealtimeAddInt64(target.cacheReadTokens, source.cacheReadTokens)
	target.cacheWriteTokens = channelMonitorRealtimeAddInt64(target.cacheWriteTokens, source.cacheWriteTokens)
	target.inputTokens = channelMonitorRealtimeAddInt64(target.inputTokens, source.inputTokens)
	target.settledCostNanoCNY = channelMonitorRealtimeAddInt64(target.settledCostNanoCNY, source.settledCostNanoCNY)
	target.unresolvedCostNanoCNY = channelMonitorRealtimeAddInt64(target.unresolvedCostNanoCNY, source.unresolvedCostNanoCNY)
	target.unresolvedRequestCount = channelMonitorRealtimeAddInt64(target.unresolvedRequestCount, source.unresolvedRequestCount)
	for sourceName, count := range source.sourceCounts {
		target.sourceCounts[sourceName] = channelMonitorRealtimeAddInt64(target.sourceCounts[sourceName], count)
	}
}

func mergeChannelMonitorRealtimeDailyCosts(target map[int64]ChannelMonitorRealtimeDailyCost, source map[int64]ChannelMonitorRealtimeDailyCost) {
	for dayStart, sourceCost := range source {
		cost := target[dayStart]
		cost.DayStart = dayStart
		cost.SettledCostNanoCNY = channelMonitorRealtimeAddInt64(cost.SettledCostNanoCNY, sourceCost.SettledCostNanoCNY)
		cost.UnresolvedCostNanoCNY = channelMonitorRealtimeAddInt64(cost.UnresolvedCostNanoCNY, sourceCost.UnresolvedCostNanoCNY)
		cost.ProbeSettledCostNanoCNY = channelMonitorRealtimeAddInt64(cost.ProbeSettledCostNanoCNY, sourceCost.ProbeSettledCostNanoCNY)
		cost.ModelDetectionSettledCostNanoCNY = channelMonitorRealtimeAddInt64(cost.ModelDetectionSettledCostNanoCNY, sourceCost.ModelDetectionSettledCostNanoCNY)
		cost.SettledRequestCount = channelMonitorRealtimeAddInt64(cost.SettledRequestCount, sourceCost.SettledRequestCount)
		cost.UnresolvedRequestCount = channelMonitorRealtimeAddInt64(cost.UnresolvedRequestCount, sourceCost.UnresolvedRequestCount)
		target[dayStart] = cost
	}
}

func buildChannelMonitorRealtimeSnapshot(
	scope ChannelMonitorRealtimeScope,
	channelId int,
	modelName string,
	metrics channelMonitorRealtimeMetrics,
	dailyCosts map[int64]ChannelMonitorRealtimeDailyCost,
	windowStart int64,
	windowEnd int64,
	dataCutoffAt int64,
	processedAt int64,
	eventWatermark uint64,
	coverageStart int64,
	todayStart int64,
) ChannelMonitorRealtimeSnapshot {
	snapshot := ChannelMonitorRealtimeSnapshot{
		Scope:                  scope,
		ChannelId:              channelId,
		ModelName:              modelName,
		EventCount:             metrics.eventCount,
		BusinessRequestCount:   metrics.businessRequestCount,
		ActualSuccessCount:     metrics.actualSuccessCount,
		ActualFailureCount:     metrics.actualFailureCount,
		FinalSuccessCount:      metrics.finalSuccessCount,
		FinalFailureCount:      metrics.finalFailureCount,
		FirstTokenSampleCount:  metrics.firstTokenSampleCount,
		FirstTokenTotalMs:      metrics.firstTokenTotalMs,
		TPSSampleCount:         metrics.tPSSampleCount,
		TPSTotal:               metrics.tPSTotal,
		CacheSampleCount:       metrics.cacheSampleCount,
		CacheHitCount:          metrics.cacheHitCount,
		CacheReadRequestCount:  metrics.cacheReadRequestCount,
		CacheWriteRequestCount: metrics.cacheWriteRequestCount,
		CacheReadTokens:        metrics.cacheReadTokens,
		CacheWriteTokens:       metrics.cacheWriteTokens,
		InputTokens:            metrics.inputTokens,
		SettledCostNanoCNY:     metrics.settledCostNanoCNY,
		UnresolvedCostNanoCNY:  metrics.unresolvedCostNanoCNY,
		UnresolvedRequestCount: metrics.unresolvedRequestCount,
		TodayDayStart:          todayStart,
		SourceCounts:           cloneChannelMonitorRealtimeSourceCounts(metrics.sourceCounts),
		DailyCosts:             channelMonitorRealtimeDailyCostSlice(dailyCosts),
		WindowStart:            windowStart,
		WindowEnd:              windowEnd,
		CoverageStart:          coverageStart,
		DataCutoffAt:           dataCutoffAt,
		ProcessedAt:            processedAt,
		EventWatermark:         eventWatermark,
	}
	snapshot.ActualSampleCount = channelMonitorRealtimeAddInt64(snapshot.ActualSuccessCount, snapshot.ActualFailureCount)
	if snapshot.ActualSampleCount > 0 {
		snapshot.ActualSuccessRate = float64(snapshot.ActualSuccessCount) / float64(snapshot.ActualSampleCount)
	}
	snapshot.FinalSampleCount = channelMonitorRealtimeAddInt64(snapshot.FinalSuccessCount, snapshot.FinalFailureCount)
	if snapshot.FinalSampleCount > 0 {
		snapshot.FinalSuccessRate = float64(snapshot.FinalSuccessCount) / float64(snapshot.FinalSampleCount)
	}
	if snapshot.FirstTokenSampleCount > 0 {
		average := snapshot.FirstTokenTotalMs / float64(snapshot.FirstTokenSampleCount)
		snapshot.AverageFirstTokenMs = &average
	}
	if snapshot.TPSSampleCount > 0 {
		average := snapshot.TPSTotal / float64(snapshot.TPSSampleCount)
		snapshot.AverageTPS = &average
	}
	if snapshot.CacheSampleCount > 0 {
		snapshot.CacheHitRate = float64(snapshot.CacheHitCount) / float64(snapshot.CacheSampleCount)
	}
	if snapshot.InputTokens > 0 {
		snapshot.CacheUtilizationRate = float64(snapshot.CacheReadTokens) / float64(snapshot.InputTokens)
	}
	for _, dailyCost := range snapshot.DailyCosts {
		if dailyCost.DayStart != todayStart {
			continue
		}
		snapshot.TodaySettledCostNanoCNY = dailyCost.SettledCostNanoCNY
		snapshot.TodayUnresolvedCostNanoCNY = dailyCost.UnresolvedCostNanoCNY
		snapshot.TodaySettledRequestCount = dailyCost.SettledRequestCount
		snapshot.TodayUnresolvedRequestCount = dailyCost.UnresolvedRequestCount
		break
	}
	return snapshot
}

func channelMonitorRealtimeDailyCostSlice(costs map[int64]ChannelMonitorRealtimeDailyCost) []ChannelMonitorRealtimeDailyCost {
	result := make([]ChannelMonitorRealtimeDailyCost, 0, len(costs))
	for _, cost := range costs {
		result = append(result, cost)
	}
	sort.Slice(result, func(i int, j int) bool { return result[i].DayStart < result[j].DayStart })
	return result
}

func channelMonitorRealtimeAddInt64(left int64, right int64) int64 {
	if right <= 0 {
		return left
	}
	if left > math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}

func channelMonitorRealtimeSubtractInt64(left int64, right int64) int64 {
	if right <= 0 {
		return left
	}
	if right >= left {
		return 0
	}
	return left - right
}

func channelMonitorRealtimeAddFloat64(left float64, right float64) float64 {
	if right <= 0 {
		return left
	}
	if left > math.MaxFloat64-right {
		return math.MaxFloat64
	}
	return left + right
}

func cloneChannelMonitorEvent(event model.ChannelMonitorEvent) model.ChannelMonitorEvent {
	event.StatusCode = cloneChannelMonitorRealtimePointer(event.StatusCode)
	event.FirstTokenMs = cloneChannelMonitorRealtimePointer(event.FirstTokenMs)
	event.TPS = cloneChannelMonitorRealtimePointer(event.TPS)
	event.PromptTokens = cloneChannelMonitorRealtimePointer(event.PromptTokens)
	event.CompletionTokens = cloneChannelMonitorRealtimePointer(event.CompletionTokens)
	event.CacheReadTokens = cloneChannelMonitorRealtimePointer(event.CacheReadTokens)
	event.CacheWriteTokens = cloneChannelMonitorRealtimePointer(event.CacheWriteTokens)
	event.InputTokens = cloneChannelMonitorRealtimePointer(event.InputTokens)
	event.AttemptDurationMs = cloneChannelMonitorRealtimePointer(event.AttemptDurationMs)
	return event
}

func cloneChannelMonitorRealtimePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneChannelMonitorRealtimeSourceCounts(source map[model.ChannelMonitorEventSource]int64) map[model.ChannelMonitorEventSource]int64 {
	cloned := make(map[model.ChannelMonitorEventSource]int64, len(source))
	for sourceName, count := range source {
		cloned[sourceName] = count
	}
	return cloned
}

func cloneChannelMonitorRealtimeSnapshot(snapshot ChannelMonitorRealtimeSnapshot) ChannelMonitorRealtimeSnapshot {
	snapshot.AverageFirstTokenMs = cloneChannelMonitorRealtimePointer(snapshot.AverageFirstTokenMs)
	snapshot.AverageTPS = cloneChannelMonitorRealtimePointer(snapshot.AverageTPS)
	snapshot.SourceCounts = cloneChannelMonitorRealtimeSourceCounts(snapshot.SourceCounts)
	snapshot.DailyCosts = append([]ChannelMonitorRealtimeDailyCost(nil), snapshot.DailyCosts...)
	return snapshot
}

func applyChannelMonitorRealtimeTodayCost(snapshot *ChannelMonitorRealtimeSnapshot, timestamp int64) {
	snapshot.TodayDayStart = model.ChannelDailyCostDayStart(timestamp)
	snapshot.TodaySettledCostNanoCNY = 0
	snapshot.TodayUnresolvedCostNanoCNY = 0
	snapshot.TodaySettledRequestCount = 0
	snapshot.TodayUnresolvedRequestCount = 0
	for _, dailyCost := range snapshot.DailyCosts {
		if dailyCost.DayStart != snapshot.TodayDayStart {
			continue
		}
		snapshot.TodaySettledCostNanoCNY = dailyCost.SettledCostNanoCNY
		snapshot.TodayUnresolvedCostNanoCNY = dailyCost.UnresolvedCostNanoCNY
		snapshot.TodaySettledRequestCount = dailyCost.SettledRequestCount
		snapshot.TodayUnresolvedRequestCount = dailyCost.UnresolvedRequestCount
		return
	}
}

func (projection *channelMonitorRealtimeProjection) global() ChannelMonitorRealtimeSnapshot {
	projection.mu.RLock()
	snapshot := cloneChannelMonitorRealtimeSnapshot(projection.globalSnapshot)
	projection.mu.RUnlock()
	applyChannelMonitorRealtimeTodayCost(&snapshot, projection.config.now().Unix())
	return snapshot
}

func (projection *channelMonitorRealtimeProjection) channel(channelId int) (ChannelMonitorRealtimeSnapshot, bool) {
	projection.mu.RLock()
	snapshot, ok := projection.channelSnapshots[channelId]
	if ok {
		snapshot = cloneChannelMonitorRealtimeSnapshot(snapshot)
	}
	projection.mu.RUnlock()
	if ok {
		applyChannelMonitorRealtimeTodayCost(&snapshot, projection.config.now().Unix())
	}
	return snapshot, ok
}

func (projection *channelMonitorRealtimeProjection) model(modelName string) (ChannelMonitorRealtimeSnapshot, bool) {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	projection.mu.RLock()
	snapshot, ok := projection.modelSnapshots[modelName]
	if ok {
		snapshot = cloneChannelMonitorRealtimeSnapshot(snapshot)
	}
	projection.mu.RUnlock()
	if ok {
		applyChannelMonitorRealtimeTodayCost(&snapshot, projection.config.now().Unix())
	}
	return snapshot, ok
}

func (projection *channelMonitorRealtimeProjection) route(channelId int, modelName string) (ChannelMonitorRealtimeSnapshot, bool) {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	key := channelMonitorRealtimeRouteKey{channelId: channelId, modelName: modelName}
	projection.mu.RLock()
	route, ok := projection.routes[key]
	var snapshot ChannelMonitorRealtimeSnapshot
	if ok {
		snapshot = cloneChannelMonitorRealtimeSnapshot(route.snapshot)
		snapshot.CoverageStart = max(snapshot.CoverageStart, projection.routeCoverageStart(route))
	}
	projection.mu.RUnlock()
	if ok {
		applyChannelMonitorRealtimeTodayCost(&snapshot, projection.config.now().Unix())
	}
	return snapshot, ok
}

func (projection *channelMonitorRealtimeProjection) window(channelId int, modelName string) (ChannelMonitorRealtimeWindow, bool) {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	key := channelMonitorRealtimeRouteKey{channelId: channelId, modelName: modelName}
	projection.mu.RLock()
	route, ok := projection.routes[key]
	var window ChannelMonitorRealtimeWindow
	if ok {
		window.Snapshot = cloneChannelMonitorRealtimeSnapshot(route.snapshot)
		window.Snapshot.CoverageStart = max(
			window.Snapshot.CoverageStart,
			projection.routeCoverageStart(route),
		)
		window.Events = make([]model.ChannelMonitorEvent, len(route.events))
		for index, event := range route.events {
			window.Events[index] = cloneChannelMonitorEvent(event)
		}
	}
	projection.mu.RUnlock()
	if ok {
		applyChannelMonitorRealtimeTodayCost(&window.Snapshot, projection.config.now().Unix())
	}
	return window, ok
}

func (projection *channelMonitorRealtimeProjection) routeSnapshots() []ChannelMonitorRealtimeSnapshot {
	projection.mu.RLock()
	snapshots := make([]ChannelMonitorRealtimeSnapshot, 0, len(projection.routes))
	for _, route := range projection.routes {
		snapshot := cloneChannelMonitorRealtimeSnapshot(route.snapshot)
		snapshot.CoverageStart = max(snapshot.CoverageStart, projection.routeCoverageStart(route))
		snapshots = append(snapshots, snapshot)
	}
	projection.mu.RUnlock()
	timestamp := projection.config.now().Unix()
	for index := range snapshots {
		applyChannelMonitorRealtimeTodayCost(&snapshots[index], timestamp)
	}
	sort.Slice(snapshots, func(i int, j int) bool {
		if snapshots[i].ModelName != snapshots[j].ModelName {
			return snapshots[i].ModelName < snapshots[j].ModelName
		}
		return snapshots[i].ChannelId < snapshots[j].ChannelId
	})
	return snapshots
}

func (projection *channelMonitorRealtimeProjection) routeWindows() []ChannelMonitorRealtimeWindow {
	projection.mu.RLock()
	windows := make([]ChannelMonitorRealtimeWindow, 0, len(projection.routes))
	for _, route := range projection.routes {
		window := ChannelMonitorRealtimeWindow{
			Snapshot: cloneChannelMonitorRealtimeSnapshot(route.snapshot),
			Events:   make([]model.ChannelMonitorEvent, len(route.events)),
		}
		window.Snapshot.CoverageStart = max(
			window.Snapshot.CoverageStart,
			projection.routeCoverageStart(route),
		)
		for index, event := range route.events {
			window.Events[index] = cloneChannelMonitorEvent(event)
		}
		windows = append(windows, window)
	}
	projection.mu.RUnlock()
	timestamp := projection.config.now().Unix()
	for index := range windows {
		applyChannelMonitorRealtimeTodayCost(&windows[index].Snapshot, timestamp)
	}
	sort.Slice(windows, func(i int, j int) bool {
		if windows[i].Snapshot.ModelName != windows[j].Snapshot.ModelName {
			return windows[i].Snapshot.ModelName < windows[j].Snapshot.ModelName
		}
		return windows[i].Snapshot.ChannelId < windows[j].Snapshot.ChannelId
	})
	return windows
}

var channelMonitorRealtime = newChannelMonitorRealtimeProjection(defaultChannelMonitorRealtimeProjectionConfig())

func consumeChannelMonitorEventBatch(ctx context.Context, events []model.ChannelMonitorEvent) error {
	if err := channelMonitorRealtime.consume(ctx, events); err != nil {
		return err
	}
	channelMonitorRealtimePage.consume(events, channelMonitorRealtime.global().ProcessedAt)
	return nil
}

func GetChannelMonitorRealtimeGlobalSnapshot() ChannelMonitorRealtimeSnapshot {
	return channelMonitorRealtime.global()
}

func GetChannelMonitorRealtimeProjectionStartedAt() int64 {
	channelMonitorRealtime.mu.RLock()
	startedAt := channelMonitorRealtime.startedAt
	channelMonitorRealtime.mu.RUnlock()
	return startedAt
}

func GetChannelMonitorRealtimeProjectionCoverageStart() int64 {
	channelMonitorRealtime.mu.RLock()
	coverageStart := channelMonitorRealtime.routeCoverageStart(nil)
	channelMonitorRealtime.mu.RUnlock()
	return coverageStart
}

func GetChannelMonitorRealtimeChannelSnapshot(channelId int) (ChannelMonitorRealtimeSnapshot, bool) {
	return channelMonitorRealtime.channel(channelId)
}

func GetChannelMonitorRealtimeModelSnapshot(modelName string) (ChannelMonitorRealtimeSnapshot, bool) {
	return channelMonitorRealtime.model(modelName)
}

func GetChannelMonitorRealtimeRouteSnapshot(channelId int, modelName string) (ChannelMonitorRealtimeSnapshot, bool) {
	return channelMonitorRealtime.route(channelId, modelName)
}

func GetChannelMonitorRealtimeWindow(channelId int, modelName string) (ChannelMonitorRealtimeWindow, bool) {
	return channelMonitorRealtime.window(channelId, modelName)
}

func ListChannelMonitorRealtimeRouteSnapshots() []ChannelMonitorRealtimeSnapshot {
	return channelMonitorRealtime.routeSnapshots()
}

func ListChannelMonitorRealtimeWindows() []ChannelMonitorRealtimeWindow {
	return channelMonitorRealtime.routeWindows()
}

// ResetChannelMonitorRealtimeProjectionsForTest clears the in-memory realtime
// projections between tests. Production code should let the projections live
// for the process lifetime so page reads and scheduling share one event view.
func ResetChannelMonitorRealtimeProjectionsForTest() {
	channelMonitorRealtime.mu.Lock()
	reset := newChannelMonitorRealtimeProjection(channelMonitorRealtime.config)
	channelMonitorRealtime.config = reset.config
	channelMonitorRealtime.startedAt = reset.startedAt
	channelMonitorRealtime.routes = reset.routes
	channelMonitorRealtime.seen = reset.seen
	channelMonitorRealtime.seenOrder = reset.seenOrder
	channelMonitorRealtime.seenNext = reset.seenNext
	channelMonitorRealtime.touchSequence = reset.touchSequence
	channelMonitorRealtime.evictedThrough = reset.evictedThrough
	channelMonitorRealtime.globalSnapshot = reset.globalSnapshot
	channelMonitorRealtime.channelSnapshots = reset.channelSnapshots
	channelMonitorRealtime.modelSnapshots = reset.modelSnapshots
	channelMonitorRealtime.mu.Unlock()
	channelMonitorRealtimePage.mu.Lock()
	pageReset := newChannelMonitorRealtimePageProjection()
	channelMonitorRealtimePage.buckets = pageReset.buckets
	channelMonitorRealtimePage.seen = pageReset.seen
	channelMonitorRealtimePage.seenOrder = pageReset.seenOrder
	channelMonitorRealtimePage.seenNext = pageReset.seenNext
	channelMonitorRealtimePage.costEvents = pageReset.costEvents
	channelMonitorRealtimePage.mu.Unlock()
}

// ProjectChannelMonitorEventsForTest applies events directly to the in-memory
// projections without invoking asynchronous projected-event handlers.
func ProjectChannelMonitorEventsForTest(events ...model.ChannelMonitorEvent) error {
	for _, event := range events {
		if err := event.Validate(); err != nil {
			return err
		}
	}
	return consumeChannelMonitorEventBatch(context.Background(), events)
}
