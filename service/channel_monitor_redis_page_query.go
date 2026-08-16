package service

import (
	"context"
	"sort"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

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

// QueryChannelMonitorRealtimePageFromRedis adapts the Redis shared projection
// to the response model used by the channel-monitor controllers.
func QueryChannelMonitorRealtimePageFromRedis(
	ctx context.Context,
	startAt int64,
	endAt int64,
) (ChannelMonitorRealtimePageView, error) {
	projection, err := NewChannelMonitorRedisSharedProjection()
	if err != nil {
		return ChannelMonitorRealtimePageView{}, err
	}
	shared, err := projection.Query(ctx, startAt, endAt)
	if err != nil {
		return ChannelMonitorRealtimePageView{}, err
	}
	return channelMonitorRedisSharedPageView(shared), nil
}

// QueryChannelMonitorRealtimeSuccessDetailFromRedis returns filtered success
// and failure data from the same Redis projection used by the dashboard.
func QueryChannelMonitorRealtimeSuccessDetailFromRedis(
	ctx context.Context,
	startAt int64,
	endAt int64,
	filter model.ChannelMonitorSuccessFilter,
) (ChannelMonitorRealtimeSuccessDetailView, error) {
	projection, err := NewChannelMonitorRedisSharedProjection()
	if err != nil {
		return ChannelMonitorRealtimeSuccessDetailView{}, err
	}
	shared, err := projection.Query(ctx, startAt, endAt)
	if err != nil {
		return ChannelMonitorRealtimeSuccessDetailView{}, err
	}
	filter.ModelName = ratio_setting.FormatMatchingModelName(filter.ModelName)
	return channelMonitorRedisSharedSuccessDetailView(shared, filter), nil
}

func GetChannelMonitorRedisSharedProjectionMetadata(
	ctx context.Context,
	startAt int64,
	endAt int64,
) (dataCutoffAt, processedAt int64, eventWatermark uint64, err error) {
	projection, err := NewChannelMonitorRedisSharedProjection()
	if err != nil {
		return 0, 0, 0, err
	}
	view, err := projection.Query(ctx, startAt, endAt)
	if err != nil {
		return 0, 0, 0, err
	}
	return view.DataCutoffAt, view.ProcessedAt, view.EventWatermark, nil
}

func QueryChannelMonitorRedisSharedProjectionForCosts(
	ctx context.Context,
	startAt int64,
	endAt int64,
) (map[int]ChannelMonitorRedisSharedAggregate, error) {
	projection, err := NewChannelMonitorRedisSharedProjection()
	if err != nil {
		return nil, err
	}
	view, err := projection.Query(ctx, startAt, endAt)
	if err != nil {
		return nil, err
	}
	day := view.DailyCosts[model.ChannelDailyCostDayStart(startAt)]
	return day.Channels, nil
}

func channelMonitorRedisSharedPageView(shared ChannelMonitorRedisSharedProjectionView) ChannelMonitorRealtimePageView {
	view := ChannelMonitorRealtimePageView{
		Summary:        channelMonitorRedisSharedPageAggregate(shared.Summary),
		Routes:         make([]ChannelMonitorRealtimePageAggregate, 0, len(shared.Routes)),
		Channels:       make([]ChannelMonitorRealtimePageAggregate, 0, len(shared.Channels)),
		Groups:         make([]ChannelMonitorRealtimePageAggregate, 0, len(shared.Groups)),
		APIKeys:        make([]ChannelMonitorRealtimePageAggregate, 0, len(shared.APIKeys)),
		Failures:       make([]model.ChannelMonitorFailureCategory, 0, len(shared.Failures)),
		WindowStart:    shared.WindowStart,
		WindowEnd:      shared.WindowEnd,
		DataCutoffAt:   shared.DataCutoffAt,
		ProcessedAt:    shared.ProcessedAt,
		EventWatermark: shared.EventWatermark,
	}
	for _, route := range shared.Routes {
		item := channelMonitorRedisSharedPageAggregate(route.ChannelMonitorRedisSharedAggregate)
		item.ChannelId = route.ChannelID
		item.ModelName = route.ModelName
		view.Routes = append(view.Routes, item)
	}
	for channelID, aggregate := range shared.Channels {
		item := channelMonitorRedisSharedPageAggregate(aggregate)
		item.ChannelId = channelID
		view.Channels = append(view.Channels, item)
	}
	for groupName, aggregate := range shared.Groups {
		item := channelMonitorRedisSharedPageAggregate(aggregate)
		item.GroupName = groupName
		view.Groups = append(view.Groups, item)
	}
	for apiKeyID, aggregate := range shared.APIKeys {
		item := channelMonitorRedisSharedPageAggregate(aggregate)
		item.APIKeyId = apiKeyID
		item.APIKeyName = aggregate.APIKeyName
		view.APIKeys = append(view.APIKeys, item)
	}
	for _, category := range shared.Failures {
		view.Failures = append(view.Failures, model.ChannelMonitorFailureCategory{
			ChannelId:     category.ChannelID,
			StatusCode:    category.StatusCode,
			ErrorType:     category.ErrorType,
			ErrorCode:     category.ErrorCode,
			SampleContent: category.SampleContent,
			ActualCount:   category.ActualCount,
			FinalCount:    category.FinalCount,
			LastOccurred:  category.LastOccurred,
		})
	}
	sort.Slice(view.Routes, func(i, j int) bool {
		if view.Routes[i].ModelName != view.Routes[j].ModelName {
			return view.Routes[i].ModelName < view.Routes[j].ModelName
		}
		return view.Routes[i].ChannelId < view.Routes[j].ChannelId
	})
	sort.Slice(view.Channels, func(i, j int) bool { return view.Channels[i].ChannelId < view.Channels[j].ChannelId })
	sort.Slice(view.Groups, func(i, j int) bool { return view.Groups[i].GroupName < view.Groups[j].GroupName })
	sort.Slice(view.APIKeys, func(i, j int) bool { return view.APIKeys[i].APIKeyId < view.APIKeys[j].APIKeyId })
	return view
}

func channelMonitorRedisSharedSuccessDetailView(
	shared ChannelMonitorRedisSharedProjectionView,
	filter model.ChannelMonitorSuccessFilter,
) ChannelMonitorRealtimeSuccessDetailView {
	detail := model.ChannelMonitorSuccessDetail{
		ChannelItems:      make([]model.ChannelMonitorChannelSuccessMetric, 0),
		APIKeyItems:       make([]model.ChannelMonitorSuccessAPIKeyMetric, 0),
		FailureCategories: make([]model.ChannelMonitorFailureCategory, 0),
	}
	var total ChannelMonitorRedisSharedAggregate
	channelAggregates := make(map[int]ChannelMonitorRedisSharedAggregate)
	apiKeyAggregates := make(map[int]ChannelMonitorRedisSharedAggregate)
	for _, route := range shared.Routes {
		matches := filter.ChannelId > 0 && route.ChannelID == filter.ChannelId &&
			(filter.ModelName == "" || route.ModelName == filter.ModelName)
		if !matches {
			continue
		}
		mergeChannelMonitorRedisSharedAggregate(&total, route.ChannelMonitorRedisSharedAggregate)
		mergeChannelMonitorRedisSharedAggregateMap(channelAggregates, route.ChannelID, route.ChannelMonitorRedisSharedAggregate)
	}
	for _, groupChannel := range shared.GroupChannels {
		if filter.ChannelId > 0 || groupChannel.GroupName != filter.Group {
			continue
		}
		mergeChannelMonitorRedisSharedAggregate(&total, groupChannel.ChannelMonitorRedisSharedAggregate)
		mergeChannelMonitorRedisSharedAggregateMap(channelAggregates, groupChannel.ChannelID, groupChannel.ChannelMonitorRedisSharedAggregate)
	}
	for _, scope := range shared.APIKeyScopes {
		matches := filter.ChannelId > 0 && scope.ChannelID == filter.ChannelId &&
			(filter.ModelName == "" || scope.ModelName == filter.ModelName)
		if filter.ChannelId == 0 {
			matches = scope.GroupName == filter.Group
		}
		if matches {
			mergeChannelMonitorRedisSharedAggregateMap(apiKeyAggregates, scope.APIKeyID, scope.ChannelMonitorRedisSharedAggregate)
		}
	}
	for _, category := range shared.Failures {
		matches := filter.ChannelId > 0 && category.ChannelID == filter.ChannelId &&
			(filter.ModelName == "" || category.ModelName == filter.ModelName)
		if filter.ChannelId == 0 {
			matches = category.GroupName == filter.Group
		}
		if !matches {
			continue
		}
		detail.FailureCategories = append(detail.FailureCategories, model.ChannelMonitorFailureCategory{
			ChannelId: category.ChannelID, StatusCode: category.StatusCode,
			ErrorType: category.ErrorType, ErrorCode: category.ErrorCode,
			SampleContent: category.SampleContent, ActualCount: category.ActualCount,
			FinalCount: category.FinalCount, LastOccurred: category.LastOccurred,
		})
	}
	detail.Summary = channelMonitorRedisSharedSuccessSummary(total)
	for channelID, aggregate := range channelAggregates {
		detail.ChannelItems = append(detail.ChannelItems, model.ChannelMonitorChannelSuccessMetric{
			ChannelId: channelID, ChannelMonitorSuccessSummary: channelMonitorRedisSharedSuccessSummary(aggregate),
		})
	}
	for apiKeyID, aggregate := range apiKeyAggregates {
		detail.APIKeyItems = append(detail.APIKeyItems, model.ChannelMonitorSuccessAPIKeyMetric{
			APIKeyId: apiKeyID, APIKeyName: aggregate.APIKeyName,
			ChannelMonitorSuccessSummary: channelMonitorRedisSharedSuccessSummary(aggregate),
		})
	}
	sort.Slice(detail.ChannelItems, func(i, j int) bool { return detail.ChannelItems[i].ChannelId < detail.ChannelItems[j].ChannelId })
	sort.Slice(detail.APIKeyItems, func(i, j int) bool { return detail.APIKeyItems[i].APIKeyId < detail.APIKeyItems[j].APIKeyId })
	sort.Slice(detail.FailureCategories, func(i, j int) bool {
		if detail.FailureCategories[i].ChannelId != detail.FailureCategories[j].ChannelId {
			return detail.FailureCategories[i].ChannelId < detail.FailureCategories[j].ChannelId
		}
		if detail.FailureCategories[i].StatusCode != detail.FailureCategories[j].StatusCode {
			return detail.FailureCategories[i].StatusCode < detail.FailureCategories[j].StatusCode
		}
		return detail.FailureCategories[i].ErrorCode < detail.FailureCategories[j].ErrorCode
	})
	return ChannelMonitorRealtimeSuccessDetailView{
		Detail: detail, WindowStart: shared.WindowStart, WindowEnd: shared.WindowEnd,
		DataCutoffAt: shared.DataCutoffAt, ProcessedAt: shared.ProcessedAt,
		EventWatermark: shared.EventWatermark,
	}
}

func channelMonitorRedisSharedPageAggregate(aggregate ChannelMonitorRedisSharedAggregate) ChannelMonitorRealtimePageAggregate {
	result := ChannelMonitorRealtimePageAggregate{
		Summary:                channelMonitorRedisSharedSuccessSummary(aggregate),
		SampleCount:            int(aggregate.EventCount),
		FirstTokenSampleCount:  int(aggregate.FirstTokenSampleCount),
		TPSSampleCount:         int(aggregate.TPSSampleCount),
		LatestFirstTokenMs:     aggregate.LatestFirstTokenMs,
		LatestTPS:              aggregate.LatestTPS,
		LastUsedTime:           aggregate.LastUsedTime,
		CacheWriteRequestCount: aggregate.CacheWriteRequestCount,
		SettledCostNanoCNY:     aggregate.SettledCostNanoCNY,
		SettledRequestCount:    aggregate.SettledRequestCount,
		UnresolvedCostNanoCNY:  aggregate.UnresolvedCostNanoCNY,
		UnresolvedRequestCount: aggregate.UnresolvedRequestCount,
	}
	if aggregate.FirstTokenSampleCount > 0 {
		value := aggregate.FirstTokenTotalMs / float64(aggregate.FirstTokenSampleCount)
		result.AverageFirstTokenMs = &value
	}
	if aggregate.TPSSampleCount > 0 {
		value := aggregate.TPSTotal / float64(aggregate.TPSSampleCount)
		result.AverageTPS = &value
	}
	return result
}

func channelMonitorRedisSharedSuccessSummary(aggregate ChannelMonitorRedisSharedAggregate) model.ChannelMonitorSuccessSummary {
	result := model.ChannelMonitorSuccessSummary{
		ActualSuccessCount: aggregate.ActualSuccessCount,
		ActualFailureCount: aggregate.ActualFailureCount,
		FinalSuccessCount:  aggregate.FinalSuccessCount,
		FinalFailureCount:  aggregate.FinalFailureCount,
		CacheHitCount:      aggregate.CacheHitCount,
		CacheSampleCount:   aggregate.CacheSampleCount,
		CacheReadTokens:    aggregate.CacheReadTokens,
		InputTokens:        aggregate.InputTokens,
	}
	result.ActualSampleCount = result.ActualSuccessCount + result.ActualFailureCount
	if result.ActualSampleCount > 0 {
		result.ActualSuccessRate = float64(result.ActualSuccessCount) / float64(result.ActualSampleCount)
	}
	result.FinalSampleCount = result.FinalSuccessCount + result.FinalFailureCount
	if result.FinalSampleCount > 0 {
		result.FinalSuccessRate = float64(result.FinalSuccessCount) / float64(result.FinalSampleCount)
	}
	if result.CacheSampleCount > 0 {
		result.CacheHitRate = float64(result.CacheHitCount) / float64(result.CacheSampleCount)
	}
	if result.InputTokens > 0 {
		result.CacheUtilization = float64(result.CacheReadTokens) / float64(result.InputTokens)
	}
	return result
}
