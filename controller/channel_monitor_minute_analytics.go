package controller

import (
	"context"
	"errors"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func queryChannelMonitorMinuteAnalytics(ctx context.Context, query channelMonitorAnalyticsQuery) (channelMonitorAnalyticsResponse, error) {
	now := common.GetTimestamp()
	query.To = now - now%60 + 60
	query.From = query.To - int64(query.Minutes)*60
	query.Model = ratio_setting.FormatMatchingModelName(query.Model)
	var err error
	query, err = prepareChannelMonitorAnalyticsSearch(ctx, query)
	if err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	shared, err := service.QueryChannelMonitorRedisMinuteAnalytics(ctx, query.From, now+1, model.ChannelMonitorSuccessFilter{ChannelId: query.Channel, Group: query.Group})
	if err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	tokenIDs := make([]int, 0, len(shared.APIKeyScopes))
	seen := make(map[int]bool)
	for _, scope := range shared.APIKeyScopes {
		if scope.APIKeyID > 0 && !seen[scope.APIKeyID] {
			seen[scope.APIKeyID] = true
			tokenIDs = append(tokenIDs, scope.APIKeyID)
		}
	}
	owners, err := getChannelMonitorAPIKeyOwners(ctx, tokenIDs)
	if err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	facts, err := channelMonitorMinuteAnalyticsFacts(shared, query.Group, owners)
	if err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	response, err := queryChannelMonitorCurrentSuccessFacts(ctx, query, service.ChannelMonitorRedisDailySuccessAnalyticsView{
		DayStart: query.From, Facts: facts, DataCutoffAt: shared.DataCutoffAt, ProcessedAt: shared.ProcessedAt,
	})
	if err != nil {
		return response, err
	}
	response.Source = "redis_minutes"
	response.RangeMinutes, response.WindowStart, response.WindowEnd = query.Minutes, shared.WindowStart, shared.WindowEnd
	// Failure categories are channel/model totals; they have no user or key
	// attribution and must not be shown as a filtered user's error counts.
	if query.Channel > 0 && !query.hasUserFilter() && !query.hasAPIKeyFilter() && query.APIKeyKey == nil && query.Search == "" {
		type failureIdentity struct {
			channel, status int
			errorType, code string
		}
		categories := make(map[failureIdentity]model.ChannelMonitorFailureCategory)
		for _, failure := range shared.Failures {
			identity := model.ChannelMonitorDailyMetricIdentity{Model: failure.ModelName}
			if !channelMonitorAnalyticsCurrentMatch(query, failure.ChannelID, 0, 0, "", "", failure.ModelName, identity.LedgerRow(query.From).ModelKey) {
				continue
			}
			key := failureIdentity{failure.ChannelID, failure.StatusCode, failure.ErrorType, failure.ErrorCode}
			category := categories[key]
			category.ChannelId, category.StatusCode, category.ErrorType, category.ErrorCode = failure.ChannelID, failure.StatusCode, failure.ErrorType, failure.ErrorCode
			if err := channelMonitorAddNonNegativeInt64(&category.ActualCount, failure.ActualCount); err != nil {
				return channelMonitorAnalyticsResponse{}, err
			}
			if err := channelMonitorAddNonNegativeInt64(&category.FinalCount, failure.FinalCount); err != nil {
				return channelMonitorAnalyticsResponse{}, err
			}
			if failure.LastOccurred >= category.LastOccurred {
				category.LastOccurred, category.SampleContent = failure.LastOccurred, failure.SampleContent
			}
			categories[key] = category
		}
		for _, category := range categories {
			response.FailureCategories = append(response.FailureCategories, category)
		}
		sort.Slice(response.FailureCategories, func(i, j int) bool {
			left, right := response.FailureCategories[i], response.FailureCategories[j]
			if left.ActualCount != right.ActualCount {
				return left.ActualCount > right.ActualCount
			}
			if left.StatusCode != right.StatusCode {
				return left.StatusCode < right.StatusCode
			}
			if left.ErrorType != right.ErrorType {
				return left.ErrorType < right.ErrorType
			}
			return left.ErrorCode < right.ErrorCode
		})
		if len(response.FailureCategories) > channelMonitorCostAPIKeyMaxRows {
			response.FailureCategories = response.FailureCategories[:channelMonitorCostAPIKeyMaxRows]
			response.FailureCategoriesTruncated = true
		}
	}
	return response, nil
}

func channelMonitorMinuteAnalyticsFacts(shared service.ChannelMonitorRedisSharedProjectionView, group string, owners map[int]channelMonitorCostAPIKeyOwner) ([]service.ChannelMonitorRedisDailySuccessAnalyticsRow, error) {
	type route struct {
		channel int
		model   string
	}
	remaining := make(map[route]service.ChannelMonitorRedisSharedAggregate)
	if group == "" {
		for _, item := range shared.Routes {
			remaining[route{item.ChannelID, item.ModelName}] = item.ChannelMonitorRedisSharedAggregate
		}
	} else {
		for _, item := range shared.GroupChannels {
			remaining[route{channel: item.ChannelID}] = item.ChannelMonitorRedisSharedAggregate
		}
	}
	facts := make([]service.ChannelMonitorRedisDailySuccessAnalyticsRow, 0, len(shared.APIKeyScopes)+len(remaining))
	for _, scope := range shared.APIKeyScopes {
		key := route{scope.ChannelID, scope.ModelName}
		if group != "" {
			key.model = ""
		}
		total := remaining[key]
		if err := subtractChannelMonitorMinuteAnalyticsCounts(&total, scope.ChannelMonitorRedisSharedAggregate); err != nil {
			return nil, err
		}
		remaining[key] = total
		identity := model.ChannelMonitorDailyMetricIdentityFromEvent(model.ChannelMonitorEvent{APIKeyId: scope.APIKeyID, APIKeyName: scope.APIKeyName, ModelName: scope.ModelName})
		owner := owners[scope.APIKeyID]
		attribution := string(model.ChannelMonitorEventUserAttributionUnknown)
		if owner.UserId > 0 {
			attribution = string(model.ChannelMonitorEventUserAttributionInferred)
		}
		facts = append(facts, service.ChannelMonitorRedisDailySuccessAnalyticsRow{
			ChannelID: scope.ChannelID, UserID: owner.UserId, UserAttribution: attribution,
			APIKeyID: scope.APIKeyID, APIKeyKey: identity.APIKeyKey, APIKeyName: scope.APIKeyName,
			ModelName: scope.ModelName, ModelKey: identity.LedgerRow(shared.WindowStart).ModelKey,
			Aggregate: scope.ChannelMonitorRedisSharedAggregate,
		})
	}
	for key, total := range remaining {
		if total.ActualSuccessCount == 0 && total.ActualFailureCount == 0 && total.FinalSuccessCount == 0 && total.FinalFailureCount == 0 {
			continue
		}
		identity := model.ChannelMonitorDailyMetricIdentity{Model: key.model}
		facts = append(facts, service.ChannelMonitorRedisDailySuccessAnalyticsRow{
			ChannelID: key.channel, ModelName: key.model, ModelKey: identity.LedgerRow(shared.WindowStart).ModelKey,
			UserAttribution: string(model.ChannelMonitorEventUserAttributionUnknown), Aggregate: total,
		})
	}
	return facts, nil
}

func subtractChannelMonitorMinuteAnalyticsCounts(total *service.ChannelMonitorRedisSharedAggregate, part service.ChannelMonitorRedisSharedAggregate) error {
	for _, counter := range []struct {
		total *int64
		part  int64
	}{
		{&total.ActualSuccessCount, part.ActualSuccessCount}, {&total.ActualFailureCount, part.ActualFailureCount},
		{&total.FinalSuccessCount, part.FinalSuccessCount}, {&total.FinalFailureCount, part.FinalFailureCount},
		{&total.CacheHitCount, part.CacheHitCount}, {&total.CacheSampleCount, part.CacheSampleCount},
		{&total.CacheReadTokens, part.CacheReadTokens}, {&total.InputTokens, part.InputTokens},
		{&total.CacheWriteRequestCount, part.CacheWriteRequestCount},
	} {
		if counter.part < 0 || *counter.total < counter.part {
			return errors.New("分钟统计明细与渠道总数不一致，请刷新后重试")
		}
		*counter.total -= counter.part
	}
	return nil
}
