package service

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/go-redis/redis/v8"
)

type ChannelMonitorRedisDailySuccessEntry struct {
	Scope     string
	Identity  string
	Aggregate ChannelMonitorRedisSharedAggregate
}

type ChannelMonitorRedisDailySuccessView struct {
	DayStart       int64
	DataCutoffAt   int64
	ProcessedAt    int64
	EventWatermark uint64
	Entries        []ChannelMonitorRedisDailySuccessEntry
}

// QueryChannelMonitorRedisDailySuccess reads one dedicated daily hash. It
// never scans dashboard minute keys and returns one row per selected scope.
func QueryChannelMonitorRedisDailySuccess(
	ctx context.Context,
	dayStart int64,
	patterns []string,
) (ChannelMonitorRedisDailySuccessView, error) {
	projection, err := NewChannelMonitorRedisSharedProjection()
	if err != nil {
		return ChannelMonitorRedisDailySuccessView{}, err
	}
	return queryChannelMonitorRedisDailySuccessWithClient(ctx, projection.client, dayStart, patterns)
}

func queryChannelMonitorRedisDailySuccessWithClient(
	ctx context.Context,
	client *redis.Client,
	dayStart int64,
	patterns []string,
) (ChannelMonitorRedisDailySuccessView, error) {
	if client == nil {
		return ChannelMonitorRedisDailySuccessView{}, ErrChannelMonitorRedisSharedProjectionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(patterns) == 0 {
		patterns = []string{"*"}
	}
	limits := defaultChannelMonitorRedisSharedProjectionLimits()
	fieldCount := int64(0)
	values := make(map[string]string)
	for _, pattern := range patterns {
		cursor := uint64(0)
		for {
			opCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisSharedOperationTimeout)
			items, next, err := client.HScan(opCtx, ChannelMonitorRedisSuccessDayKey(dayStart), cursor, pattern, channelMonitorRedisSharedScanCount).Result()
			cancel()
			if err != nil {
				return ChannelMonitorRedisDailySuccessView{}, err
			}
			if len(items)%2 != 0 {
				return ChannelMonitorRedisDailySuccessView{}, errors.New("渠道监控 Redis 日汇总哈希扫描结果无效")
			}
			fieldCount += int64(len(items) / 2)
			if fieldCount > int64(limits.MaxHashFields) {
				return ChannelMonitorRedisDailySuccessView{}, &ChannelMonitorRedisSharedProjectionLimitError{
					Resource: "hash_fields", Limit: int64(limits.MaxHashFields), Actual: fieldCount,
				}
			}
			for index := 0; index < len(items); index += 2 {
				values[items[index]] = items[index+1]
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}

	entries := make(map[string]*ChannelMonitorRedisDailySuccessEntry)
	view := ChannelMonitorRedisDailySuccessView{DayStart: dayStart}
	for field, raw := range values {
		parts := strings.SplitN(field, ":", 3)
		if len(parts) < 2 {
			continue
		}
		scope, metric, identity := parts[0], parts[len(parts)-1], ""
		if len(parts) == 3 {
			identity = parts[1]
		}
		if scope == channelMonitorRedisSharedScopeMetadata {
			switch metric {
			case channelMonitorRedisSharedMetricDataCutoffAt:
				view.DataCutoffAt = max(view.DataCutoffAt, parseDailySuccessInt64(raw))
			case channelMonitorRedisSharedMetricProcessedAt:
				view.ProcessedAt = max(view.ProcessedAt, parseDailySuccessInt64(raw))
			case channelMonitorRedisSharedMetricEventWatermark:
				if parsed, err := strconv.ParseUint(raw, 10, 64); err == nil {
					view.EventWatermark = max(view.EventWatermark, parsed)
				}
			}
			continue
		}
		entryKey := scope + "\x00" + identity
		entry := entries[entryKey]
		if entry == nil {
			entry = &ChannelMonitorRedisDailySuccessEntry{Scope: scope, Identity: identity}
			entries[entryKey] = entry
		}
		if err := addChannelMonitorRedisAggregateField(&entry.Aggregate, metric, raw); err != nil {
			return ChannelMonitorRedisDailySuccessView{}, err
		}
	}
	view.Entries = make([]ChannelMonitorRedisDailySuccessEntry, 0, len(entries))
	if len(entries) > limits.MaxDimensionEntries {
		return ChannelMonitorRedisDailySuccessView{}, &ChannelMonitorRedisSharedProjectionLimitError{
			Resource: "dimension_entries", Limit: int64(limits.MaxDimensionEntries), Actual: int64(len(entries)),
		}
	}
	for _, entry := range entries {
		view.Entries = append(view.Entries, *entry)
	}
	sort.Slice(view.Entries, func(i, j int) bool {
		if view.Entries[i].Scope != view.Entries[j].Scope {
			return view.Entries[i].Scope < view.Entries[j].Scope
		}
		return view.Entries[i].Identity < view.Entries[j].Identity
	})
	return view, nil
}

func parseDailySuccessInt64(value string) int64 {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}
