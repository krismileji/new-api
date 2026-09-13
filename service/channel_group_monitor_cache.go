package service

import (
	"context"
	"errors"
)

// GetChannelGroupMonitorCacheRates uses the same business-request cache counters
// as channel monitoring, over the last 24 hours of minute buckets.
func GetChannelGroupMonitorCacheRates(ctx context.Context, groupNames []string, now int64) (map[string]float64, error) {
	rates := make(map[string]float64, len(groupNames))
	if len(groupNames) == 0 {
		return rates, nil
	}
	windowStart := now - now%60 + 60 - 24*60*60
	view, err := queryChannelMonitorRealtimePageFromRedis(ctx, windowStart, now+1, channelMonitorRedisSharedQuerySelection{
		patterns: []string{channelMonitorRedisSharedScopeMetadata + ":*", channelMonitorRedisSharedScopeGroup + ":*"},
	})
	if err != nil {
		return nil, err
	}
	if view.WindowStart > windowStart {
		return nil, errors.New("缓存率统计窗口不足 24 小时")
	}
	wanted := make(map[string]bool, len(groupNames))
	for _, groupName := range groupNames {
		wanted[groupName] = true
	}
	for _, group := range view.Groups {
		if wanted[group.GroupName] && group.Summary.CacheSampleCount > 0 {
			rates[group.GroupName] = group.Summary.CacheHitRate * 100
		}
	}
	return rates, nil
}
