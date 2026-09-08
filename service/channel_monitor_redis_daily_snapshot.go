package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/go-redis/redis/v8"
)

// readChannelMonitorRedisDailyHash returns a bounded, consistent daily
// snapshot. A concurrent increment, expiry or rebuild invalidates the entire
// scan, including when it happens between two selected dimension patterns.
func readChannelMonitorRedisDailyHash(ctx context.Context, client *redis.Client, key string, patterns []string, maxFields int) (map[string]string, error) {
	if len(patterns) == 0 {
		patterns = []string{"*"}
	}
	for attempt := 0; attempt < channelMonitorRedisSharedWriteRetries; attempt++ {
		values := make(map[string]string)
		err := client.Watch(ctx, func(tx *redis.Tx) error {
			count, err := tx.HLen(ctx, key).Result()
			if err != nil {
				return err
			}
			if count == 0 {
				return ErrChannelMonitorRedisSharedProjectionUnavailable
			}
			for _, pattern := range patterns {
				if pattern == "*" && count > int64(maxFields) {
					return &ChannelMonitorRedisSharedProjectionLimitError{Resource: "hash_fields", Limit: int64(maxFields), Actual: count}
				}
				cursor := uint64(0)
				for {
					items, next, err := tx.HScan(ctx, key, cursor, pattern, channelMonitorRedisSharedScanCount).Result()
					if err != nil {
						return err
					}
					if len(items)%2 != 0 {
						return errors.New("渠道监控 Redis 日汇总哈希扫描结果无效")
					}
					for index := 0; index < len(items); index += 2 {
						values[items[index]] = items[index+1]
					}
					if len(values) > maxFields {
						return &ChannelMonitorRedisSharedProjectionLimitError{Resource: "hash_fields", Limit: int64(maxFields), Actual: int64(len(values))}
					}
					cursor = next
					if cursor == 0 {
						break
					}
				}
			}
			// EXEC validates WATCH even though this transaction only reads.
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Exists(ctx, key)
				return nil
			})
			return err
		}, key)
		if !errors.Is(err, redis.TxFailedErr) {
			if err == nil {
				for field, raw := range values {
					metric := field[strings.LastIndexByte(field, ':')+1:]
					if _, integer := channelMonitorRedisSharedIntegerMetrics[metric]; integer {
						value, parseErr := strconv.ParseInt(raw, 10, 64)
						if parseErr != nil || value < 0 {
							return nil, fmt.Errorf("渠道监控日汇总整数指标无效: %s", metric)
						}
					}
					if _, floating := channelMonitorRedisSharedFloatMetrics[metric]; floating {
						value, parseErr := strconv.ParseFloat(raw, 64)
						if parseErr != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
							return nil, fmt.Errorf("渠道监控日汇总性能指标无效: %s", metric)
						}
					}
				}
			}
			return values, err
		}
	}
	return nil, redis.TxFailedErr
}
