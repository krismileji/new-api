package service

import (
	"context"
	"errors"
	"slices"

	"github.com/QuantumNous/new-api/model"
)

// GetChannelGroupMonitorCacheRates weights eligible streaming business requests
// by input tokens, over the configured display window. Multi-day windows
// combine completed calendar days with today's realtime minute buckets.
// Eligibility is frozen on each event; reading never reapplies current settings.
func GetChannelGroupMonitorCacheRates(ctx context.Context, groupNames []string, windowStart, windowEnd int64) (map[string]float64, error) {
	statistics, err := GetChannelGroupMonitorCacheStatistics(ctx, groupNames, windowStart, windowEnd)
	if err != nil {
		return nil, err
	}
	rates := make(map[string]float64, len(groupNames))
	for group, statistic := range statistics {
		if statistic.Weighted != nil {
			rates[group] = *statistic.Weighted
		}
	}
	return rates, nil
}

type ChannelGroupMonitorCacheStatistics struct {
	Weighted      *float64
	APIKeyMax     *float64
	APIKeyAverage *float64
}

// Combine each key's token totals across routes and days before calculating
// the maximum and equal-weight mean. Unknown keys never become a synthetic key.
func GetChannelGroupMonitorCacheStatistics(ctx context.Context, groupNames []string, windowStart, windowEnd int64) (map[string]ChannelGroupMonitorCacheStatistics, error) {
	rates := make(map[string]ChannelGroupMonitorCacheStatistics, len(groupNames))
	if len(groupNames) == 0 || windowStart >= windowEnd {
		return rates, nil
	}
	realtimeStart := windowStart
	if windowEnd-windowStart > 24*60*60 {
		realtimeStart = model.ChannelDailyCostDayStart(windowEnd - 1)
	}
	projection, err := NewChannelMonitorRedisSharedProjection()
	if err != nil {
		return nil, err
	}
	view, err := projection.querySelected(ctx, realtimeStart, windowEnd, channelMonitorRedisSharedQuerySelection{
		patterns: []string{
			channelMonitorRedisSharedScopeMetadata + ":*",
			channelMonitorRedisSharedScopeGroup + ":*:group_cache_*",
			channelMonitorRedisSharedScopeAPIKeyRoute + ":*:group_cache_*",
		},
	})
	if err != nil {
		return nil, err
	}
	if view.WindowStart > realtimeStart {
		return nil, errors.New("缓存率统计窗口不足当前配置的展示范围")
	}
	wanted := make(map[string]bool, len(groupNames))
	for _, groupName := range groupNames {
		wanted[groupName] = true
	}
	counts := make(map[string]model.ChannelGroupMonitorCacheCounts, len(groupNames))
	type groupKey struct {
		group string
		id    int
	}
	keyCounts := make(map[groupKey]model.ChannelGroupMonitorCacheCounts)
	var samples []model.ChannelGroupMonitorCacheCounts
	if realtimeStart > windowStart {
		historical, err := model.GetChannelGroupMonitorHistoricalCacheCounts(ctx, groupNames, windowStart, realtimeStart)
		if err != nil {
			return nil, err
		}
		samples = append(samples, historical...)
	}
	for _, sample := range samples {
		count := counts[sample.GroupName]
		count.CacheReadTokens, err = channelMonitorRedisSharedCheckedAddInt64(count.CacheReadTokens, sample.CacheReadTokens)
		if err != nil {
			return nil, err
		}
		count.InputTokens, err = channelMonitorRedisSharedCheckedAddInt64(count.InputTokens, sample.InputTokens)
		if err != nil {
			return nil, err
		}
		counts[sample.GroupName] = count
	}
	for _, scope := range view.APIKeyScopes {
		if wanted[scope.GroupName] && scope.APIKeyID > 0 {
			samples = append(samples, model.ChannelGroupMonitorCacheCounts{
				GroupName: scope.GroupName, APIKeyId: scope.APIKeyID,
				CacheReadTokens: scope.GroupCacheReadTokens, InputTokens: scope.GroupCacheInputTokens,
			})
		}
	}
	for _, sample := range samples {
		if sample.APIKeyId <= 0 || !wanted[sample.GroupName] {
			continue
		}
		if sample.CacheReadTokens < 0 || sample.InputTokens < 0 {
			return nil, errors.New("分组缓存率统计无效")
		}
		key := groupKey{sample.GroupName, sample.APIKeyId}
		count := keyCounts[key]
		count.CacheReadTokens, err = channelMonitorRedisSharedCheckedAddInt64(count.CacheReadTokens, sample.CacheReadTokens)
		if err != nil {
			return nil, err
		}
		count.InputTokens, err = channelMonitorRedisSharedCheckedAddInt64(count.InputTokens, sample.InputTokens)
		if err != nil {
			return nil, err
		}
		keyCounts[key] = count
	}
	for groupName, group := range view.Groups {
		if !wanted[groupName] {
			continue
		}
		count := counts[groupName]
		readTokens, inputTokens := group.GroupCacheReadTokens, group.GroupCacheInputTokens
		if readTokens < 0 || inputTokens < 0 {
			return nil, errors.New("分组缓存率统计无效")
		}
		count.CacheReadTokens, err = channelMonitorRedisSharedCheckedAddInt64(count.CacheReadTokens, readTokens)
		if err != nil {
			return nil, err
		}
		count.InputTokens, err = channelMonitorRedisSharedCheckedAddInt64(count.InputTokens, inputTokens)
		if err != nil {
			return nil, err
		}
		counts[groupName] = count
	}
	for groupName, count := range counts {
		if count.InputTokens > 0 {
			rate := float64(count.CacheReadTokens) / float64(count.InputTokens) * 100
			rates[groupName] = ChannelGroupMonitorCacheStatistics{Weighted: &rate}
		}
	}
	byGroup := make(map[string][]float64)
	for key, count := range keyCounts {
		if count.InputTokens > 0 {
			byGroup[key.group] = append(byGroup[key.group], float64(count.CacheReadTokens)/float64(count.InputTokens)*100)
		}
	}
	for group, values := range byGroup {
		// Stable summation avoids response jitter caused by map iteration order.
		slices.Sort(values)
		var sum float64
		for _, value := range values {
			sum += value
		}
		average, maximum := sum/float64(len(values)), values[len(values)-1]
		statistic := rates[group]
		statistic.APIKeyMax, statistic.APIKeyAverage = &maximum, &average
		rates[group] = statistic
	}
	return rates, nil
}
