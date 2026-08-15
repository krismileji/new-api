package service

import (
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

const channelMonitorRealtimePageMinutes = 1441

type ChannelMonitorRealtimePageAggregate struct {
	ChannelId  int    `json:"channel_id,omitempty"`
	ModelName  string `json:"model_name,omitempty"`
	GroupName  string `json:"group,omitempty"`
	APIKeyId   int    `json:"api_key_id,omitempty"`
	APIKeyName string `json:"api_key_name,omitempty"`

	Summary                model.ChannelMonitorSuccessSummary `json:"summary"`
	SampleCount            int                                `json:"sample_count"`
	FirstTokenSampleCount  int                                `json:"first_token_sample_count"`
	TPSSampleCount         int                                `json:"tps_sample_count"`
	AverageFirstTokenMs    *float64                           `json:"average_first_token_ms"`
	AverageTPS             *float64                           `json:"average_tps"`
	LatestFirstTokenMs     *float64                           `json:"latest_first_token_ms"`
	LatestTPS              *float64                           `json:"latest_tps"`
	LastUsedTime           int64                              `json:"last_used_time"`
	CacheWriteRequestCount int64                              `json:"cache_write_request_count"`
	SettledCostNanoCNY     int64                              `json:"settled_cost_nano_cny"`
	SettledRequestCount    int64                              `json:"settled_request_count"`
	UnresolvedCostNanoCNY  int64                              `json:"unresolved_cost_nano_cny"`
	UnresolvedRequestCount int64                              `json:"unresolved_request_count"`
}

type ChannelMonitorRealtimePageView struct {
	Summary        ChannelMonitorRealtimePageAggregate   `json:"summary"`
	Routes         []ChannelMonitorRealtimePageAggregate `json:"routes"`
	Channels       []ChannelMonitorRealtimePageAggregate `json:"channels"`
	Groups         []ChannelMonitorRealtimePageAggregate `json:"groups"`
	APIKeys        []ChannelMonitorRealtimePageAggregate `json:"api_keys"`
	Failures       []model.ChannelMonitorFailureCategory `json:"failures"`
	WindowStart    int64                                 `json:"window_start"`
	WindowEnd      int64                                 `json:"window_end"`
	DataCutoffAt   int64                                 `json:"data_cutoff_at"`
	ProcessedAt    int64                                 `json:"processed_at"`
	EventWatermark uint64                                `json:"event_watermark"`
}

type ChannelMonitorRealtimeSuccessDetailView struct {
	Detail         model.ChannelMonitorSuccessDetail `json:"detail"`
	WindowStart    int64                             `json:"window_start"`
	WindowEnd      int64                             `json:"window_end"`
	DataCutoffAt   int64                             `json:"data_cutoff_at"`
	ProcessedAt    int64                             `json:"processed_at"`
	EventWatermark uint64                            `json:"event_watermark"`
}

type channelMonitorRealtimePageRouteKey struct {
	channelId int
	modelName string
}

type channelMonitorRealtimePageFailureKey struct {
	channelId  int
	modelName  string
	groupName  string
	statusCode int
	errorType  string
	errorCode  string
}

type channelMonitorRealtimePageGroupChannelKey struct {
	groupName string
	channelId int
}

type channelMonitorRealtimePageAPIKeyScopeKey struct {
	apiKeyId  int
	channelId int
	modelName string
	groupName string
}

type channelMonitorRealtimePageMetrics struct {
	actualSuccess          int64
	actualFailure          int64
	finalSuccess           int64
	finalFailure           int64
	cacheHit               int64
	cacheSample            int64
	cacheReadTokens        int64
	inputTokens            int64
	sampleCount            int
	firstTokenSampleCount  int
	firstTokenTotalMs      float64
	tPSSampleCount         int
	tPSTotal               float64
	latestFirstTokenMs     *float64
	latestFirstTokenAt     int64
	latestTPS              *float64
	latestTPSAt            int64
	lastUsedTime           int64
	cacheWriteRequestCount int64
	settledCostNanoCNY     int64
	settledRequestCount    int64
	unresolvedCostNanoCNY  int64
	unresolvedRequestCount int64
}

type channelMonitorRealtimePageBucket struct {
	global         channelMonitorRealtimePageMetrics
	routes         map[channelMonitorRealtimePageRouteKey]channelMonitorRealtimePageMetrics
	channels       map[int]channelMonitorRealtimePageMetrics
	groups         map[string]channelMonitorRealtimePageMetrics
	apiKeys        map[int]channelMonitorRealtimePageMetrics
	apiKeyNames    map[int]string
	groupChannels  map[channelMonitorRealtimePageGroupChannelKey]channelMonitorRealtimePageMetrics
	apiKeyScopes   map[channelMonitorRealtimePageAPIKeyScopeKey]channelMonitorRealtimePageMetrics
	failures       map[channelMonitorRealtimePageFailureKey]model.ChannelMonitorFailureCategory
	dataCutoffAt   int64
	processedAt    int64
	eventWatermark uint64
}

type channelMonitorRealtimePageProjection struct {
	mu         sync.RWMutex
	buckets    map[int64]*channelMonitorRealtimePageBucket
	costEvents map[string]model.ChannelMonitorEvent
	seen       map[string]struct{}
	seenOrder  []string
	seenNext   int
}

func newChannelMonitorRealtimePageProjection() *channelMonitorRealtimePageProjection {
	return &channelMonitorRealtimePageProjection{
		buckets:    make(map[int64]*channelMonitorRealtimePageBucket),
		costEvents: make(map[string]model.ChannelMonitorEvent),
		seen:       make(map[string]struct{}, channelMonitorRealtimeDedupCapacity),
		seenOrder:  make([]string, 0, channelMonitorRealtimeDedupCapacity),
	}
}

func (projection *channelMonitorRealtimePageProjection) consume(events []model.ChannelMonitorEvent, now int64) {
	projection.mu.Lock()
	defer projection.mu.Unlock()
	for _, event := range events {
		if _, duplicate := projection.seen[event.EventId]; duplicate {
			continue
		}
		projection.rememberEventId(event.EventId)
		if event.Source != model.ChannelMonitorEventSourceBusiness {
			continue
		}
		costEventId := channelMonitorRealtimeCostEventId(event)
		if costEventId != "" {
			if previous, exists := projection.costEvents[costEventId]; exists {
				if !channelMonitorRealtimeCostEventNewer(previous, event) {
					continue
				}
				projection.removeCostEvent(previous)
			}
			projection.costEvents[costEventId] = event
		}
		minuteStart := event.OccurredAt - event.OccurredAt%60
		bucket := projection.buckets[minuteStart]
		if bucket == nil {
			bucket = &channelMonitorRealtimePageBucket{
				routes:        make(map[channelMonitorRealtimePageRouteKey]channelMonitorRealtimePageMetrics),
				channels:      make(map[int]channelMonitorRealtimePageMetrics),
				groups:        make(map[string]channelMonitorRealtimePageMetrics),
				apiKeys:       make(map[int]channelMonitorRealtimePageMetrics),
				apiKeyNames:   make(map[int]string),
				groupChannels: make(map[channelMonitorRealtimePageGroupChannelKey]channelMonitorRealtimePageMetrics),
				apiKeyScopes:  make(map[channelMonitorRealtimePageAPIKeyScopeKey]channelMonitorRealtimePageMetrics),
				failures:      make(map[channelMonitorRealtimePageFailureKey]model.ChannelMonitorFailureCategory),
			}
			projection.buckets[minuteStart] = bucket
		}
		event.ModelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(event.ModelName))
		bucket.dataCutoffAt = max(bucket.dataCutoffAt, event.OccurredAt)
		bucket.processedAt = max(bucket.processedAt, now)
		bucket.eventWatermark = max(bucket.eventWatermark, event.EventSequence)
		channelMonitorRealtimePageAddEvent(&bucket.global, event)
		routeKey := channelMonitorRealtimePageRouteKey{channelId: event.ChannelId, modelName: event.ModelName}
		route := bucket.routes[routeKey]
		channelMonitorRealtimePageAddEvent(&route, event)
		bucket.routes[routeKey] = route
		channel := bucket.channels[event.ChannelId]
		channelMonitorRealtimePageAddEvent(&channel, event)
		bucket.channels[event.ChannelId] = channel
		groupName := strings.TrimSpace(event.GroupName)
		group := bucket.groups[groupName]
		channelMonitorRealtimePageAddEvent(&group, event)
		bucket.groups[groupName] = group
		groupChannelKey := channelMonitorRealtimePageGroupChannelKey{groupName: groupName, channelId: event.ChannelId}
		groupChannel := bucket.groupChannels[groupChannelKey]
		channelMonitorRealtimePageAddEvent(&groupChannel, event)
		bucket.groupChannels[groupChannelKey] = groupChannel
		if event.APIKeyId > 0 {
			apiKey := bucket.apiKeys[event.APIKeyId]
			channelMonitorRealtimePageAddEvent(&apiKey, event)
			bucket.apiKeys[event.APIKeyId] = apiKey
			if name := strings.TrimSpace(event.APIKeyName); name != "" {
				bucket.apiKeyNames[event.APIKeyId] = name
			}
			apiKeyScopeKey := channelMonitorRealtimePageAPIKeyScopeKey{
				apiKeyId: event.APIKeyId, channelId: event.ChannelId,
				modelName: event.ModelName, groupName: groupName,
			}
			apiKeyScope := bucket.apiKeyScopes[apiKeyScopeKey]
			channelMonitorRealtimePageAddEvent(&apiKeyScope, event)
			bucket.apiKeyScopes[apiKeyScopeKey] = apiKeyScope
		}
		channelMonitorRealtimePageAddFailure(bucket.failures, event)
	}
	cutoffMinute := now - now%60 - int64(channelMonitorRealtimePageMinutes-1)*60
	for minuteStart := range projection.buckets {
		if minuteStart < cutoffMinute {
			delete(projection.buckets, minuteStart)
		}
	}
}

func (projection *channelMonitorRealtimePageProjection) removeCostEvent(event model.ChannelMonitorEvent) {
	minuteStart := event.OccurredAt - event.OccurredAt%60
	bucket := projection.buckets[minuteStart]
	if bucket == nil {
		return
	}
	channelMonitorRealtimePageRemoveCost(&bucket.global, event)
	routeKey := channelMonitorRealtimePageRouteKey{channelId: event.ChannelId, modelName: event.ModelName}
	route := bucket.routes[routeKey]
	channelMonitorRealtimePageRemoveCost(&route, event)
	bucket.routes[routeKey] = route
	channel := bucket.channels[event.ChannelId]
	channelMonitorRealtimePageRemoveCost(&channel, event)
	bucket.channels[event.ChannelId] = channel
	groupName := strings.TrimSpace(event.GroupName)
	group := bucket.groups[groupName]
	channelMonitorRealtimePageRemoveCost(&group, event)
	bucket.groups[groupName] = group
	groupChannelKey := channelMonitorRealtimePageGroupChannelKey{groupName: groupName, channelId: event.ChannelId}
	groupChannel := bucket.groupChannels[groupChannelKey]
	channelMonitorRealtimePageRemoveCost(&groupChannel, event)
	bucket.groupChannels[groupChannelKey] = groupChannel
	if event.APIKeyId > 0 {
		apiKey := bucket.apiKeys[event.APIKeyId]
		channelMonitorRealtimePageRemoveCost(&apiKey, event)
		bucket.apiKeys[event.APIKeyId] = apiKey
		apiKeyScopeKey := channelMonitorRealtimePageAPIKeyScopeKey{
			apiKeyId: event.APIKeyId, channelId: event.ChannelId,
			modelName: event.ModelName, groupName: groupName,
		}
		apiKeyScope := bucket.apiKeyScopes[apiKeyScopeKey]
		channelMonitorRealtimePageRemoveCost(&apiKeyScope, event)
		bucket.apiKeyScopes[apiKeyScopeKey] = apiKeyScope
	}
}

func channelMonitorRealtimePageRemoveCost(
	metrics *channelMonitorRealtimePageMetrics,
	event model.ChannelMonitorEvent,
) {
	if event.CostStatus == model.ChannelMonitorEventCostSettled {
		metrics.settledCostNanoCNY = channelMonitorRealtimeSubtractInt64(metrics.settledCostNanoCNY, event.SettledCostNanoCNY)
		metrics.settledRequestCount = channelMonitorRealtimeSubtractInt64(metrics.settledRequestCount, 1)
	}
	if event.CostStatus == model.ChannelMonitorEventCostUnresolved {
		metrics.unresolvedCostNanoCNY = channelMonitorRealtimeSubtractInt64(metrics.unresolvedCostNanoCNY, event.UnresolvedCostNanoCNY)
		metrics.unresolvedRequestCount = channelMonitorRealtimeSubtractInt64(metrics.unresolvedRequestCount, 1)
	}
}

func (projection *channelMonitorRealtimePageProjection) rememberEventId(eventId string) {
	projection.seen[eventId] = struct{}{}
	if len(projection.seenOrder) < channelMonitorRealtimeDedupCapacity {
		projection.seenOrder = append(projection.seenOrder, eventId)
		return
	}
	evicted := projection.seenOrder[projection.seenNext]
	delete(projection.seen, evicted)
	projection.seenOrder[projection.seenNext] = eventId
	projection.seenNext = (projection.seenNext + 1) % channelMonitorRealtimeDedupCapacity
}

func channelMonitorRealtimePageAddEvent(metrics *channelMonitorRealtimePageMetrics, event model.ChannelMonitorEvent) {
	if event.FinalRetrySummary {
		if event.Outcome == model.ChannelMonitorEventOutcomeFailure {
			metrics.finalFailure = channelMonitorRealtimeAddInt64(metrics.finalFailure, 1)
		}
		return
	}
	if event.CostStatus == model.ChannelMonitorEventCostSettled {
		metrics.settledCostNanoCNY = channelMonitorRealtimeAddInt64(metrics.settledCostNanoCNY, event.SettledCostNanoCNY)
		metrics.settledRequestCount = channelMonitorRealtimeAddInt64(metrics.settledRequestCount, 1)
	}
	if event.CostStatus == model.ChannelMonitorEventCostUnresolved {
		metrics.unresolvedCostNanoCNY = channelMonitorRealtimeAddInt64(metrics.unresolvedCostNanoCNY, event.UnresolvedCostNanoCNY)
		metrics.unresolvedRequestCount = channelMonitorRealtimeAddInt64(metrics.unresolvedRequestCount, 1)
	}
	if !event.RequestDispatched {
		return
	}
	switch event.Outcome {
	case model.ChannelMonitorEventOutcomeSuccess:
		metrics.actualSuccess = channelMonitorRealtimeAddInt64(metrics.actualSuccess, 1)
		if event.IsFinalAttempt {
			metrics.finalSuccess = channelMonitorRealtimeAddInt64(metrics.finalSuccess, 1)
		}
	case model.ChannelMonitorEventOutcomeFailure:
		metrics.actualFailure = channelMonitorRealtimeAddInt64(metrics.actualFailure, 1)
		if event.IsFinalAttempt {
			metrics.finalFailure = channelMonitorRealtimeAddInt64(metrics.finalFailure, 1)
		}
	default:
		return
	}
	if metrics.sampleCount < math.MaxInt {
		metrics.sampleCount++
	}
	metrics.lastUsedTime = max(metrics.lastUsedTime, event.OccurredAt)
	if event.FirstTokenMs != nil {
		if metrics.firstTokenSampleCount < math.MaxInt {
			metrics.firstTokenSampleCount++
		}
		metrics.firstTokenTotalMs = channelMonitorRealtimeAddFloat64(metrics.firstTokenTotalMs, *event.FirstTokenMs)
		if event.OccurredAt >= metrics.latestFirstTokenAt {
			metrics.latestFirstTokenAt = event.OccurredAt
			metrics.latestFirstTokenMs = cloneChannelMonitorRealtimePointer(event.FirstTokenMs)
		}
	}
	if event.TPS != nil {
		if metrics.tPSSampleCount < math.MaxInt {
			metrics.tPSSampleCount++
		}
		metrics.tPSTotal = channelMonitorRealtimeAddFloat64(metrics.tPSTotal, *event.TPS)
		if event.OccurredAt >= metrics.latestTPSAt {
			metrics.latestTPSAt = event.OccurredAt
			metrics.latestTPS = cloneChannelMonitorRealtimePointer(event.TPS)
		}
	}
	inputTokens := int64(0)
	if event.InputTokens != nil {
		inputTokens = *event.InputTokens
	} else if event.PromptTokens != nil {
		inputTokens = *event.PromptTokens
	}
	if inputTokens > 0 {
		metrics.cacheSample = channelMonitorRealtimeAddInt64(metrics.cacheSample, 1)
		metrics.inputTokens = channelMonitorRealtimeAddInt64(metrics.inputTokens, inputTokens)
	}
	if event.CacheReadTokens != nil && *event.CacheReadTokens > 0 {
		metrics.cacheHit = channelMonitorRealtimeAddInt64(metrics.cacheHit, 1)
		metrics.cacheReadTokens = channelMonitorRealtimeAddInt64(metrics.cacheReadTokens, *event.CacheReadTokens)
	}
	if event.CacheWriteTokens != nil && *event.CacheWriteTokens > 0 {
		metrics.cacheWriteRequestCount = channelMonitorRealtimeAddInt64(metrics.cacheWriteRequestCount, 1)
	}
}

func channelMonitorRealtimePageAddFailure(
	failures map[channelMonitorRealtimePageFailureKey]model.ChannelMonitorFailureCategory,
	event model.ChannelMonitorEvent,
) {
	if event.Outcome != model.ChannelMonitorEventOutcomeFailure || !event.RequestDispatched && !event.FinalRetrySummary {
		return
	}
	statusCode := 0
	if event.StatusCode != nil {
		statusCode = *event.StatusCode
	}
	key := channelMonitorRealtimePageFailureKey{
		channelId:  event.ChannelId,
		modelName:  event.ModelName,
		groupName:  strings.TrimSpace(event.GroupName),
		statusCode: statusCode,
		errorType:  strings.TrimSpace(event.ErrorType),
		errorCode:  strings.TrimSpace(event.ErrorCode),
	}
	category := failures[key]
	category.ChannelId = event.ChannelId
	category.StatusCode = statusCode
	category.ErrorType = key.errorType
	category.ErrorCode = key.errorCode
	if !event.FinalRetrySummary {
		category.ActualCount = channelMonitorRealtimeAddInt64(category.ActualCount, 1)
	}
	if event.IsFinalAttempt || event.FinalRetrySummary {
		category.FinalCount = channelMonitorRealtimeAddInt64(category.FinalCount, 1)
	}
	if event.OccurredAt >= category.LastOccurred {
		category.LastOccurred = event.OccurredAt
		category.SampleContent = strings.TrimSpace(event.ErrorMessage)
	}
	failures[key] = category
}

func mergeChannelMonitorRealtimePageMetrics(target *channelMonitorRealtimePageMetrics, source channelMonitorRealtimePageMetrics) {
	target.actualSuccess = channelMonitorRealtimeAddInt64(target.actualSuccess, source.actualSuccess)
	target.actualFailure = channelMonitorRealtimeAddInt64(target.actualFailure, source.actualFailure)
	target.finalSuccess = channelMonitorRealtimeAddInt64(target.finalSuccess, source.finalSuccess)
	target.finalFailure = channelMonitorRealtimeAddInt64(target.finalFailure, source.finalFailure)
	target.cacheHit = channelMonitorRealtimeAddInt64(target.cacheHit, source.cacheHit)
	target.cacheSample = channelMonitorRealtimeAddInt64(target.cacheSample, source.cacheSample)
	target.cacheReadTokens = channelMonitorRealtimeAddInt64(target.cacheReadTokens, source.cacheReadTokens)
	target.inputTokens = channelMonitorRealtimeAddInt64(target.inputTokens, source.inputTokens)
	if source.sampleCount > math.MaxInt-target.sampleCount {
		target.sampleCount = math.MaxInt
	} else {
		target.sampleCount += source.sampleCount
	}
	if source.firstTokenSampleCount > math.MaxInt-target.firstTokenSampleCount {
		target.firstTokenSampleCount = math.MaxInt
	} else {
		target.firstTokenSampleCount += source.firstTokenSampleCount
	}
	target.firstTokenTotalMs = channelMonitorRealtimeAddFloat64(target.firstTokenTotalMs, source.firstTokenTotalMs)
	if source.tPSSampleCount > math.MaxInt-target.tPSSampleCount {
		target.tPSSampleCount = math.MaxInt
	} else {
		target.tPSSampleCount += source.tPSSampleCount
	}
	target.tPSTotal = channelMonitorRealtimeAddFloat64(target.tPSTotal, source.tPSTotal)
	target.cacheWriteRequestCount = channelMonitorRealtimeAddInt64(target.cacheWriteRequestCount, source.cacheWriteRequestCount)
	target.settledCostNanoCNY = channelMonitorRealtimeAddInt64(target.settledCostNanoCNY, source.settledCostNanoCNY)
	target.settledRequestCount = channelMonitorRealtimeAddInt64(target.settledRequestCount, source.settledRequestCount)
	target.unresolvedCostNanoCNY = channelMonitorRealtimeAddInt64(target.unresolvedCostNanoCNY, source.unresolvedCostNanoCNY)
	target.unresolvedRequestCount = channelMonitorRealtimeAddInt64(target.unresolvedRequestCount, source.unresolvedRequestCount)
	target.lastUsedTime = max(target.lastUsedTime, source.lastUsedTime)
	if source.latestFirstTokenMs != nil && source.latestFirstTokenAt >= target.latestFirstTokenAt {
		target.latestFirstTokenAt = source.latestFirstTokenAt
		target.latestFirstTokenMs = cloneChannelMonitorRealtimePointer(source.latestFirstTokenMs)
	}
	if source.latestTPS != nil && source.latestTPSAt >= target.latestTPSAt {
		target.latestTPSAt = source.latestTPSAt
		target.latestTPS = cloneChannelMonitorRealtimePointer(source.latestTPS)
	}
}

func channelMonitorRealtimePageAggregate(metrics channelMonitorRealtimePageMetrics) ChannelMonitorRealtimePageAggregate {
	result := ChannelMonitorRealtimePageAggregate{
		SampleCount:            metrics.sampleCount,
		FirstTokenSampleCount:  metrics.firstTokenSampleCount,
		TPSSampleCount:         metrics.tPSSampleCount,
		LatestFirstTokenMs:     cloneChannelMonitorRealtimePointer(metrics.latestFirstTokenMs),
		LatestTPS:              cloneChannelMonitorRealtimePointer(metrics.latestTPS),
		LastUsedTime:           metrics.lastUsedTime,
		CacheWriteRequestCount: metrics.cacheWriteRequestCount,
		SettledCostNanoCNY:     metrics.settledCostNanoCNY,
		SettledRequestCount:    metrics.settledRequestCount,
		UnresolvedCostNanoCNY:  metrics.unresolvedCostNanoCNY,
		UnresolvedRequestCount: metrics.unresolvedRequestCount,
	}
	result.Summary.ActualSuccessCount = metrics.actualSuccess
	result.Summary.ActualFailureCount = metrics.actualFailure
	result.Summary.ActualSampleCount = channelMonitorRealtimeAddInt64(metrics.actualSuccess, metrics.actualFailure)
	if result.Summary.ActualSampleCount > 0 {
		result.Summary.ActualSuccessRate = float64(metrics.actualSuccess) / float64(result.Summary.ActualSampleCount)
	}
	result.Summary.FinalSuccessCount = metrics.finalSuccess
	result.Summary.FinalFailureCount = metrics.finalFailure
	result.Summary.FinalSampleCount = channelMonitorRealtimeAddInt64(metrics.finalSuccess, metrics.finalFailure)
	if result.Summary.FinalSampleCount > 0 {
		result.Summary.FinalSuccessRate = float64(metrics.finalSuccess) / float64(result.Summary.FinalSampleCount)
	}
	result.Summary.CacheHitCount = metrics.cacheHit
	result.Summary.CacheSampleCount = metrics.cacheSample
	if metrics.cacheSample > 0 {
		result.Summary.CacheHitRate = float64(metrics.cacheHit) / float64(metrics.cacheSample)
	}
	result.Summary.CacheReadTokens = metrics.cacheReadTokens
	result.Summary.InputTokens = metrics.inputTokens
	if metrics.inputTokens > 0 {
		result.Summary.CacheUtilization = float64(metrics.cacheReadTokens) / float64(metrics.inputTokens)
	}
	if metrics.firstTokenSampleCount > 0 {
		value := metrics.firstTokenTotalMs / float64(metrics.firstTokenSampleCount)
		result.AverageFirstTokenMs = &value
	}
	if metrics.tPSSampleCount > 0 {
		value := metrics.tPSTotal / float64(metrics.tPSSampleCount)
		result.AverageTPS = &value
	}
	return result
}

func (projection *channelMonitorRealtimePageProjection) query(startAt int64, endAt int64) ChannelMonitorRealtimePageView {
	startMinute := startAt - startAt%60
	projection.mu.RLock()
	global := channelMonitorRealtimePageMetrics{}
	routes := make(map[channelMonitorRealtimePageRouteKey]channelMonitorRealtimePageMetrics)
	channels := make(map[int]channelMonitorRealtimePageMetrics)
	groups := make(map[string]channelMonitorRealtimePageMetrics)
	apiKeys := make(map[int]channelMonitorRealtimePageMetrics)
	apiKeyNames := make(map[int]string)
	failures := make(map[channelMonitorRealtimePageFailureKey]model.ChannelMonitorFailureCategory)
	dataCutoffAt := int64(0)
	processedAt := int64(0)
	eventWatermark := uint64(0)
	for minuteStart, bucket := range projection.buckets {
		if minuteStart < startMinute || endAt > 0 && minuteStart >= endAt {
			continue
		}
		mergeChannelMonitorRealtimePageMetrics(&global, bucket.global)
		for key, metrics := range bucket.routes {
			merged := routes[key]
			mergeChannelMonitorRealtimePageMetrics(&merged, metrics)
			routes[key] = merged
		}
		for channelId, metrics := range bucket.channels {
			merged := channels[channelId]
			mergeChannelMonitorRealtimePageMetrics(&merged, metrics)
			channels[channelId] = merged
		}
		for groupName, metrics := range bucket.groups {
			merged := groups[groupName]
			mergeChannelMonitorRealtimePageMetrics(&merged, metrics)
			groups[groupName] = merged
		}
		for apiKeyId, metrics := range bucket.apiKeys {
			merged := apiKeys[apiKeyId]
			mergeChannelMonitorRealtimePageMetrics(&merged, metrics)
			apiKeys[apiKeyId] = merged
			if name := bucket.apiKeyNames[apiKeyId]; name != "" {
				apiKeyNames[apiKeyId] = name
			}
		}
		for key, category := range bucket.failures {
			merged := failures[key]
			merged.ChannelId = category.ChannelId
			merged.StatusCode = category.StatusCode
			merged.ErrorType = category.ErrorType
			merged.ErrorCode = category.ErrorCode
			merged.ActualCount = channelMonitorRealtimeAddInt64(merged.ActualCount, category.ActualCount)
			merged.FinalCount = channelMonitorRealtimeAddInt64(merged.FinalCount, category.FinalCount)
			if category.LastOccurred >= merged.LastOccurred {
				merged.LastOccurred = category.LastOccurred
				merged.SampleContent = category.SampleContent
			}
			failures[key] = merged
		}
		dataCutoffAt = max(dataCutoffAt, bucket.dataCutoffAt)
		processedAt = max(processedAt, bucket.processedAt)
		eventWatermark = max(eventWatermark, bucket.eventWatermark)
	}
	projection.mu.RUnlock()

	view := ChannelMonitorRealtimePageView{
		Summary:        channelMonitorRealtimePageAggregate(global),
		Routes:         make([]ChannelMonitorRealtimePageAggregate, 0, len(routes)),
		Channels:       make([]ChannelMonitorRealtimePageAggregate, 0, len(channels)),
		Groups:         make([]ChannelMonitorRealtimePageAggregate, 0, len(groups)),
		APIKeys:        make([]ChannelMonitorRealtimePageAggregate, 0, len(apiKeys)),
		Failures:       make([]model.ChannelMonitorFailureCategory, 0, len(failures)),
		WindowStart:    startMinute,
		WindowEnd:      endAt,
		DataCutoffAt:   dataCutoffAt,
		ProcessedAt:    processedAt,
		EventWatermark: eventWatermark,
	}
	for key, metrics := range routes {
		item := channelMonitorRealtimePageAggregate(metrics)
		item.ChannelId = key.channelId
		item.ModelName = key.modelName
		view.Routes = append(view.Routes, item)
	}
	for channelId, metrics := range channels {
		item := channelMonitorRealtimePageAggregate(metrics)
		item.ChannelId = channelId
		view.Channels = append(view.Channels, item)
	}
	for groupName, metrics := range groups {
		item := channelMonitorRealtimePageAggregate(metrics)
		item.GroupName = groupName
		view.Groups = append(view.Groups, item)
	}
	for apiKeyId, metrics := range apiKeys {
		item := channelMonitorRealtimePageAggregate(metrics)
		item.APIKeyId = apiKeyId
		item.APIKeyName = apiKeyNames[apiKeyId]
		view.APIKeys = append(view.APIKeys, item)
	}
	for _, category := range failures {
		view.Failures = append(view.Failures, category)
	}
	sort.Slice(view.Routes, func(i int, j int) bool {
		if view.Routes[i].ModelName != view.Routes[j].ModelName {
			return view.Routes[i].ModelName < view.Routes[j].ModelName
		}
		return view.Routes[i].ChannelId < view.Routes[j].ChannelId
	})
	sort.Slice(view.Channels, func(i int, j int) bool { return view.Channels[i].ChannelId < view.Channels[j].ChannelId })
	sort.Slice(view.Groups, func(i int, j int) bool { return view.Groups[i].GroupName < view.Groups[j].GroupName })
	sort.Slice(view.APIKeys, func(i int, j int) bool { return view.APIKeys[i].APIKeyId < view.APIKeys[j].APIKeyId })
	sort.Slice(view.Failures, func(i int, j int) bool {
		if view.Failures[i].ChannelId != view.Failures[j].ChannelId {
			return view.Failures[i].ChannelId < view.Failures[j].ChannelId
		}
		if view.Failures[i].StatusCode != view.Failures[j].StatusCode {
			return view.Failures[i].StatusCode < view.Failures[j].StatusCode
		}
		if view.Failures[i].ErrorType != view.Failures[j].ErrorType {
			return view.Failures[i].ErrorType < view.Failures[j].ErrorType
		}
		return view.Failures[i].ErrorCode < view.Failures[j].ErrorCode
	})
	return view
}

func (projection *channelMonitorRealtimePageProjection) successDetail(
	startAt int64,
	endAt int64,
	filter model.ChannelMonitorSuccessFilter,
) ChannelMonitorRealtimeSuccessDetailView {
	startMinute := startAt - startAt%60
	filter.ModelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(filter.ModelName))
	filter.Group = strings.TrimSpace(filter.Group)
	projection.mu.RLock()
	total := channelMonitorRealtimePageMetrics{}
	channels := make(map[int]channelMonitorRealtimePageMetrics)
	apiKeys := make(map[int]channelMonitorRealtimePageMetrics)
	apiKeyNames := make(map[int]string)
	failures := make(map[channelMonitorRealtimePageFailureKey]model.ChannelMonitorFailureCategory)
	dataCutoffAt := int64(0)
	processedAt := int64(0)
	eventWatermark := uint64(0)
	for minuteStart, bucket := range projection.buckets {
		if minuteStart < startMinute || endAt > 0 && minuteStart >= endAt {
			continue
		}
		if filter.ChannelId > 0 {
			if filter.ModelName == "" {
				for key, metrics := range bucket.routes {
					if key.channelId != filter.ChannelId {
						continue
					}
					mergeChannelMonitorRealtimePageMetrics(&total, metrics)
					mergeChannelMonitorRealtimePageMetricsForMap(channels, filter.ChannelId, metrics)
				}
			} else {
				metrics := bucket.routes[channelMonitorRealtimePageRouteKey{channelId: filter.ChannelId, modelName: filter.ModelName}]
				mergeChannelMonitorRealtimePageMetrics(&total, metrics)
				mergeChannelMonitorRealtimePageMetricsForMap(channels, filter.ChannelId, metrics)
			}
		} else {
			metrics := bucket.groups[filter.Group]
			mergeChannelMonitorRealtimePageMetrics(&total, metrics)
			for key, channelMetrics := range bucket.groupChannels {
				if key.groupName == filter.Group {
					mergeChannelMonitorRealtimePageMetricsForMap(channels, key.channelId, channelMetrics)
				}
			}
		}
		for key, metrics := range bucket.apiKeyScopes {
			matches := filter.ChannelId > 0 && key.channelId == filter.ChannelId && (filter.ModelName == "" || key.modelName == filter.ModelName) ||
				filter.ChannelId == 0 && key.groupName == filter.Group
			if !matches {
				continue
			}
			mergeChannelMonitorRealtimePageMetricsForMap(apiKeys, key.apiKeyId, metrics)
			if name := bucket.apiKeyNames[key.apiKeyId]; name != "" {
				apiKeyNames[key.apiKeyId] = name
			}
		}
		for key, category := range bucket.failures {
			matches := filter.ChannelId > 0 && key.channelId == filter.ChannelId && (filter.ModelName == "" || key.modelName == filter.ModelName) ||
				filter.ChannelId == 0 && key.groupName == filter.Group
			if !matches {
				continue
			}
			merged := failures[key]
			merged.ChannelId = category.ChannelId
			merged.StatusCode = category.StatusCode
			merged.ErrorType = category.ErrorType
			merged.ErrorCode = category.ErrorCode
			merged.ActualCount = channelMonitorRealtimeAddInt64(merged.ActualCount, category.ActualCount)
			merged.FinalCount = channelMonitorRealtimeAddInt64(merged.FinalCount, category.FinalCount)
			if category.LastOccurred >= merged.LastOccurred {
				merged.LastOccurred = category.LastOccurred
				merged.SampleContent = category.SampleContent
			}
			failures[key] = merged
		}
		dataCutoffAt = max(dataCutoffAt, bucket.dataCutoffAt)
		processedAt = max(processedAt, bucket.processedAt)
		eventWatermark = max(eventWatermark, bucket.eventWatermark)
	}
	projection.mu.RUnlock()

	detail := model.ChannelMonitorSuccessDetail{
		Summary:           channelMonitorRealtimePageAggregate(total).Summary,
		ChannelItems:      make([]model.ChannelMonitorChannelSuccessMetric, 0, len(channels)),
		APIKeyItems:       make([]model.ChannelMonitorSuccessAPIKeyMetric, 0, len(apiKeys)),
		FailureCategories: make([]model.ChannelMonitorFailureCategory, 0, len(failures)),
	}
	for channelId, metrics := range channels {
		detail.ChannelItems = append(detail.ChannelItems, model.ChannelMonitorChannelSuccessMetric{
			ChannelId:                    channelId,
			ChannelMonitorSuccessSummary: channelMonitorRealtimePageAggregate(metrics).Summary,
		})
	}
	for apiKeyId, metrics := range apiKeys {
		detail.APIKeyItems = append(detail.APIKeyItems, model.ChannelMonitorSuccessAPIKeyMetric{
			APIKeyId:                     apiKeyId,
			APIKeyName:                   apiKeyNames[apiKeyId],
			ChannelMonitorSuccessSummary: channelMonitorRealtimePageAggregate(metrics).Summary,
		})
	}
	for _, category := range failures {
		detail.FailureCategories = append(detail.FailureCategories, category)
	}
	sort.Slice(detail.ChannelItems, func(i int, j int) bool { return detail.ChannelItems[i].ChannelId < detail.ChannelItems[j].ChannelId })
	sort.Slice(detail.APIKeyItems, func(i int, j int) bool { return detail.APIKeyItems[i].APIKeyId < detail.APIKeyItems[j].APIKeyId })
	sort.Slice(detail.FailureCategories, func(i int, j int) bool {
		if detail.FailureCategories[i].ChannelId != detail.FailureCategories[j].ChannelId {
			return detail.FailureCategories[i].ChannelId < detail.FailureCategories[j].ChannelId
		}
		if detail.FailureCategories[i].StatusCode != detail.FailureCategories[j].StatusCode {
			return detail.FailureCategories[i].StatusCode < detail.FailureCategories[j].StatusCode
		}
		if detail.FailureCategories[i].ErrorType != detail.FailureCategories[j].ErrorType {
			return detail.FailureCategories[i].ErrorType < detail.FailureCategories[j].ErrorType
		}
		return detail.FailureCategories[i].ErrorCode < detail.FailureCategories[j].ErrorCode
	})
	return ChannelMonitorRealtimeSuccessDetailView{
		Detail:         detail,
		WindowStart:    startMinute,
		WindowEnd:      endAt,
		DataCutoffAt:   dataCutoffAt,
		ProcessedAt:    processedAt,
		EventWatermark: eventWatermark,
	}
}

func mergeChannelMonitorRealtimePageMetricsForMap[K comparable](
	items map[K]channelMonitorRealtimePageMetrics,
	key K,
	metrics channelMonitorRealtimePageMetrics,
) {
	merged := items[key]
	mergeChannelMonitorRealtimePageMetrics(&merged, metrics)
	items[key] = merged
}

var channelMonitorRealtimePage = newChannelMonitorRealtimePageProjection()

func QueryChannelMonitorRealtimePage(startAt int64, endAt int64) ChannelMonitorRealtimePageView {
	return channelMonitorRealtimePage.query(startAt, endAt)
}

func QueryChannelMonitorRealtimeSuccessDetail(
	startAt int64,
	endAt int64,
	filter model.ChannelMonitorSuccessFilter,
) ChannelMonitorRealtimeSuccessDetailView {
	return channelMonitorRealtimePage.successDetail(startAt, endAt, filter)
}
