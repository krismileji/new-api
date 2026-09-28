package model

import (
	"context"
	"errors"
	"math"

	"github.com/QuantumNous/new-api/common"
)

type ChannelGroupMonitorCacheCounts struct {
	GroupName       string
	CacheReadTokens int64
	InputTokens     int64
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
		Select("group_name, aggregate_json").
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
		var tokens struct {
			Read  int64 `json:"group_cache_read_tokens"`
			Input int64 `json:"group_cache_input_tokens"`
		}
		// Old snapshots have no eligible token totals. Leave them out rather
		// than mixing request counts or unfiltered tokens into this rate.
		if row.AggregateJSON != "" {
			if err := common.UnmarshalJsonStr(row.AggregateJSON, &tokens); err != nil {
				return nil, err
			}
		}
		if tokens.Read < 0 || tokens.Input < 0 {
			return nil, errors.New("分组缓存率历史统计无效")
		}
		count := byGroup[row.GroupName]
		if count.CacheReadTokens > math.MaxInt64-tokens.Read || count.InputTokens > math.MaxInt64-tokens.Input {
			return nil, errors.New("分组缓存率历史统计溢出")
		}
		count.GroupName = row.GroupName
		count.CacheReadTokens += tokens.Read
		count.InputTokens += tokens.Input
		byGroup[row.GroupName] = count
	}
	counts := make([]ChannelGroupMonitorCacheCounts, 0, len(byGroup))
	for _, count := range byGroup {
		counts = append(counts, count)
	}
	return counts, rows.Err()
}
