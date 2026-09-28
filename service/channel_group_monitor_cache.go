package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/model"
)

// GetChannelGroupMonitorCacheRates weights eligible streaming business requests
// by input tokens, over the configured display window. Multi-day windows
// combine completed calendar days with today's realtime minute buckets.
// Eligibility is frozen on each event; reading never reapplies current settings.
func GetChannelGroupMonitorCacheRates(ctx context.Context, groupNames []string, windowStart, windowEnd int64) (map[string]float64, error) {
	rates := make(map[string]float64, len(groupNames))
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
		patterns: []string{channelMonitorRedisSharedScopeMetadata + ":*", channelMonitorRedisSharedScopeGroup + ":*"},
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
	if realtimeStart > windowStart {
		historical, err := model.GetChannelGroupMonitorHistoricalCacheCounts(ctx, groupNames, windowStart, realtimeStart)
		if err != nil {
			return nil, err
		}
		for _, group := range historical {
			if wanted[group.GroupName] {
				counts[group.GroupName] = group
			}
		}
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
			rates[groupName] = float64(count.CacheReadTokens) / float64(count.InputTokens) * 100
		}
	}
	return rates, nil
}
