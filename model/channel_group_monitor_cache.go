package model

import (
	"context"
	"errors"
)

type ChannelGroupMonitorCacheCounts struct {
	GroupName        string
	CacheHitCount    int64
	CacheSampleCount int64
}

// GetChannelGroupMonitorHistoricalCacheCounts reads complete business-request
// days before the current day, which is supplied by the realtime projection.
func GetChannelGroupMonitorHistoricalCacheCounts(ctx context.Context, groupNames []string, startAt, endAt int64) ([]ChannelGroupMonitorCacheCounts, error) {
	if len(groupNames) == 0 || startAt >= endAt {
		return nil, nil
	}
	if DB == nil {
		return nil, errors.New("缓存率历史统计数据库不可用")
	}
	var counts []ChannelGroupMonitorCacheCounts
	err := DB.WithContext(ctx).Model(&ChannelMonitorDailySuccessLedger{}).
		Select("group_name, SUM(cache_hit_count) AS cache_hit_count, SUM(cache_sample_count) AS cache_sample_count").
		Where("day_start >= ? AND day_start < ?", startAt, endAt).
		Where("group_name IN ?", groupNames).
		Group("group_name").Scan(&counts).Error
	return counts, err
}
