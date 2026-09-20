package model

import (
	"context"
	"errors"
	"math"

	"github.com/QuantumNous/new-api/common"
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
	rows, err := DB.WithContext(ctx).Model(&ChannelMonitorDailySuccessLedger{}).
		Select("group_name, cache_hit_count, cache_sample_count, aggregate_json").
		Where("day_start >= ? AND day_start < ?", startAt, endAt).
		Where("group_name IN ?", groupNames).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byGroup := make(map[string]ChannelGroupMonitorCacheCounts)
	for rows.Next() {
		var row struct {
			ChannelGroupMonitorCacheCounts
			AggregateJSON string
		}
		if err := DB.ScanRows(rows, &row); err != nil {
			return nil, err
		}
		var excluded struct {
			Hits    int64 `json:"group_cache_excluded_hits"`
			Samples int64 `json:"group_cache_excluded_samples"`
		}
		// Legacy snapshots have no exclusions and retain their original counts.
		if row.AggregateJSON != "" {
			if err := common.UnmarshalJsonStr(row.AggregateJSON, &excluded); err != nil {
				return nil, err
			}
		}
		if excluded.Hits < 0 || excluded.Hits > row.CacheHitCount || excluded.Samples < 0 || excluded.Samples > row.CacheSampleCount {
			return nil, errors.New("分组缓存率历史统计无效")
		}
		hits, samples := row.CacheHitCount-excluded.Hits, row.CacheSampleCount-excluded.Samples
		count := byGroup[row.GroupName]
		if hits < 0 || samples < 0 || count.CacheHitCount > math.MaxInt64-hits || count.CacheSampleCount > math.MaxInt64-samples {
			return nil, errors.New("分组缓存率历史统计溢出")
		}
		count.GroupName = row.GroupName
		count.CacheHitCount += hits
		count.CacheSampleCount += samples
		byGroup[row.GroupName] = count
	}
	counts := make([]ChannelGroupMonitorCacheCounts, 0, len(byGroup))
	for _, count := range byGroup {
		counts = append(counts, count)
	}
	return counts, rows.Err()
}
