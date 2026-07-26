package model

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

const (
	channelMonitorTodaySuccessCacheTTL        = time.Minute
	channelMonitorCacheTokenFieldPattern      = `%"cache_tokens":%`
	channelMonitorZeroCacheTokenBeforePattern = `%"cache_tokens":0,%`
	channelMonitorZeroCacheTokenAtEndPattern  = `%"cache_tokens":0}%`
)

type ChannelMonitorTodaySuccessMetrics struct {
	Summary         ChannelMonitorSuccessSummary          `json:"summary"`
	ChannelItems    []ChannelMonitorChannelSuccessMetric  `json:"channel_items"`
	APIKeyItems     []ChannelMonitorSuccessAPIKeyMetric   `json:"api_key_items"`
	CacheWriteItems []ChannelMonitorTodayCacheWriteMetric `json:"cache_write_items"`
}

type ChannelMonitorTodayCacheWriteMetric struct {
	ChannelId    int   `json:"channel_id"`
	RequestCount int64 `json:"request_count"`
}

type ChannelMonitorDailySuccessMetric struct {
	DayStart               int64
	Summary                ChannelMonitorSuccessSummary
	CacheWriteChannelCount int
	CacheWriteRequestCount int64
}

type channelMonitorTodaySuccessRow struct {
	ChannelId        int
	TokenId          int
	TokenName        string
	Type             int
	IsRetryAttempt   *bool
	Count            int64
	CacheHitCount    int64 `gorm:"column:cache_hit_count"`
	CacheSampleCount int64 `gorm:"column:cache_sample_count"`
	CacheWriteCount  int64 `gorm:"column:cache_write_count"`
}

type channelMonitorDailySuccessRow struct {
	DayBucket        int64 `gorm:"column:day_bucket"`
	ChannelId        int
	Type             int
	IsRetryAttempt   *bool
	Count            int64
	CacheHitCount    int64 `gorm:"column:cache_hit_count"`
	CacheSampleCount int64 `gorm:"column:cache_sample_count"`
	CacheWriteCount  int64 `gorm:"column:cache_write_count"`
}

type channelMonitorTodaySuccessCacheKey struct {
	logDB           *gorm.DB
	logDatabaseType common.DatabaseType
	dayStart        int64
}

type channelMonitorTodaySuccessCacheEntry struct {
	expiresAt time.Time
	metrics   ChannelMonitorTodaySuccessMetrics
}

type channelMonitorDailySuccessCacheKey struct {
	logDB           *gorm.DB
	logDatabaseType common.DatabaseType
	startTimestamp  int64
	endTimestamp    int64
}

type channelMonitorDailySuccessCacheEntry struct {
	expiresAt time.Time
	metrics   []ChannelMonitorDailySuccessMetric
}

var channelMonitorTodaySuccessCache = struct {
	sync.RWMutex
	items map[channelMonitorTodaySuccessCacheKey]channelMonitorTodaySuccessCacheEntry
}{
	items: make(map[channelMonitorTodaySuccessCacheKey]channelMonitorTodaySuccessCacheEntry),
}

var channelMonitorTodaySuccessSingleflight singleflight.Group

var channelMonitorDailySuccessCache = struct {
	sync.RWMutex
	items map[channelMonitorDailySuccessCacheKey]channelMonitorDailySuccessCacheEntry
}{
	items: make(map[channelMonitorDailySuccessCacheKey]channelMonitorDailySuccessCacheEntry),
}

var channelMonitorDailySuccessSingleflight singleflight.Group

func channelMonitorCacheWritePredicate() (string, []any) {
	patterns := [][3]string{
		{`%"cache_write_tokens":%`, `%"cache_write_tokens":0,%`, `%"cache_write_tokens":0}%`},
		{`%"cache_creation_tokens":%`, `%"cache_creation_tokens":0,%`, `%"cache_creation_tokens":0}%`},
		{`%"cache_creation_tokens_5m":%`, `%"cache_creation_tokens_5m":0,%`, `%"cache_creation_tokens_5m":0}%`},
		{`%"cache_creation_tokens_1h":%`, `%"cache_creation_tokens_1h":0,%`, `%"cache_creation_tokens_1h":0}%`},
	}
	conditions := make([]string, 0, len(patterns))
	args := make([]any, 0, len(patterns)*3)
	for _, pattern := range patterns {
		conditions = append(conditions, "(other LIKE ? AND other NOT LIKE ? AND other NOT LIKE ?)")
		args = append(args, pattern[0], pattern[1], pattern[2])
	}
	return "(" + strings.Join(conditions, " OR ") + ")", args
}

func getChannelMonitorTodaySuccessMetrics(ctx context.Context, dayStart int64) (ChannelMonitorTodaySuccessMetrics, error) {
	const daySeconds = int64(24 * 60 * 60)
	cacheWritePredicate, cacheWriteArgs := channelMonitorCacheWritePredicate()

	selectColumns := "channel_id, token_id, token_name, type, is_retry_attempt, COUNT(*) AS count, " +
		"SUM(CASE WHEN other LIKE ? THEN 1 ELSE 0 END) AS cache_sample_count, " +
		"SUM(CASE WHEN other LIKE ? AND other NOT LIKE ? AND other NOT LIKE ? THEN 1 ELSE 0 END) AS cache_hit_count, " +
		"SUM(CASE WHEN type = ? AND " + cacheWritePredicate + " THEN 1 ELSE 0 END) AS cache_write_count"
	selectArgs := []any{
		channelMonitorCacheTokenFieldPattern,
		channelMonitorCacheTokenFieldPattern,
		channelMonitorZeroCacheTokenBeforePattern,
		channelMonitorZeroCacheTokenAtEndPattern,
		LogTypeConsume,
	}
	selectArgs = append(selectArgs, cacheWriteArgs...)
	var rows []channelMonitorTodaySuccessRow
	err := LOG_DB.WithContext(ctx).
		Model(&Log{}).
		Select(selectColumns, selectArgs...).
		Where("type IN ?", []int{LogTypeConsume, LogTypeError}).
		Where("channel_id > ?", 0).
		Where("created_at >= ? AND created_at < ?", dayStart, dayStart+daySeconds).
		Group("channel_id, token_id, token_name, type, is_retry_attempt").
		Scan(&rows).Error
	if err != nil {
		return ChannelMonitorTodaySuccessMetrics{}, err
	}

	totalCounts := channelMonitorSuccessCounts{}
	channelCounts := make(map[int]*channelMonitorSuccessCounts)
	apiKeyCounts := make(map[channelMonitorSuccessAPIKeyKey]*channelMonitorSuccessAPIKeyAggregate)
	cacheWriteCounts := make(map[int]int64)
	for _, row := range rows {
		isRetryAttempt := row.IsRetryAttempt != nil && *row.IsRetryAttempt
		totalCounts.add(row.Type, isRetryAttempt, row.Count, row.CacheHitCount, row.CacheSampleCount)
		addChannelMonitorSuccessAPIKeyCount(apiKeyCounts, channelMonitorSuccessRow{
			TokenId:          row.TokenId,
			TokenName:        row.TokenName,
			Type:             row.Type,
			IsRetryAttempt:   row.IsRetryAttempt,
			Count:            row.Count,
			CacheHitCount:    row.CacheHitCount,
			CacheSampleCount: row.CacheSampleCount,
		})
		counts := channelCounts[row.ChannelId]
		if counts == nil {
			counts = &channelMonitorSuccessCounts{}
			channelCounts[row.ChannelId] = counts
		}
		counts.add(row.Type, isRetryAttempt, row.Count, row.CacheHitCount, row.CacheSampleCount)
		cacheWriteCounts[row.ChannelId] += row.CacheWriteCount
	}

	channelItems := make([]ChannelMonitorChannelSuccessMetric, 0, len(channelCounts))
	for channelId, counts := range channelCounts {
		channelItems = append(channelItems, ChannelMonitorChannelSuccessMetric{
			ChannelId:                    channelId,
			ChannelMonitorSuccessSummary: counts.summary(),
		})
	}
	sort.Slice(channelItems, func(i int, j int) bool {
		return channelItems[i].ChannelId < channelItems[j].ChannelId
	})
	cacheWriteItems := make([]ChannelMonitorTodayCacheWriteMetric, 0, len(cacheWriteCounts))
	for channelId, requestCount := range cacheWriteCounts {
		if requestCount <= 0 {
			continue
		}
		cacheWriteItems = append(cacheWriteItems, ChannelMonitorTodayCacheWriteMetric{
			ChannelId:    channelId,
			RequestCount: requestCount,
		})
	}
	sort.Slice(cacheWriteItems, func(i int, j int) bool {
		return cacheWriteItems[i].ChannelId < cacheWriteItems[j].ChannelId
	})
	return ChannelMonitorTodaySuccessMetrics{
		Summary:         totalCounts.summary(),
		ChannelItems:    channelItems,
		APIKeyItems:     channelMonitorSuccessAPIKeyMetrics(apiKeyCounts),
		CacheWriteItems: cacheWriteItems,
	}, nil
}

func getChannelMonitorDailySuccessMetrics(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelMonitorDailySuccessMetric, error) {
	if startTimestamp >= endTimestamp {
		return []ChannelMonitorDailySuccessMetric{}, nil
	}

	cacheWritePredicate, cacheWriteArgs := channelMonitorCacheWritePredicate()
	dayBucket := channelMonitorCostDayBucketSQL()
	selectColumns := dayBucket + " AS day_bucket, channel_id, type, is_retry_attempt, COUNT(*) AS count, " +
		"SUM(CASE WHEN other LIKE ? THEN 1 ELSE 0 END) AS cache_sample_count, " +
		"SUM(CASE WHEN other LIKE ? AND other NOT LIKE ? AND other NOT LIKE ? THEN 1 ELSE 0 END) AS cache_hit_count, " +
		"SUM(CASE WHEN type = ? AND " + cacheWritePredicate + " THEN 1 ELSE 0 END) AS cache_write_count"
	selectArgs := []any{
		channelMonitorCacheTokenFieldPattern,
		channelMonitorCacheTokenFieldPattern,
		channelMonitorZeroCacheTokenBeforePattern,
		channelMonitorZeroCacheTokenAtEndPattern,
		LogTypeConsume,
	}
	selectArgs = append(selectArgs, cacheWriteArgs...)

	var rows []channelMonitorDailySuccessRow
	err := LOG_DB.WithContext(ctx).
		Model(&Log{}).
		Select(selectColumns, selectArgs...).
		Where("type IN ?", []int{LogTypeConsume, LogTypeError}).
		Where("channel_id > ?", 0).
		Where("created_at >= ? AND created_at < ?", startTimestamp, endTimestamp).
		Group(dayBucket + ", channel_id, type, is_retry_attempt").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	type dailyAggregate struct {
		counts             channelMonitorSuccessCounts
		cacheWriteChannels map[int]struct{}
		cacheWriteRequests int64
	}
	aggregates := make(map[int64]*dailyAggregate)
	for _, row := range rows {
		dayStart := row.DayBucket*channelMonitorCostDaySeconds - channelMonitorCostTimezoneOffsetSeconds
		aggregate := aggregates[dayStart]
		if aggregate == nil {
			aggregate = &dailyAggregate{cacheWriteChannels: make(map[int]struct{})}
			aggregates[dayStart] = aggregate
		}
		aggregate.counts.add(
			row.Type,
			row.IsRetryAttempt != nil && *row.IsRetryAttempt,
			row.Count,
			row.CacheHitCount,
			row.CacheSampleCount,
		)
		if row.CacheWriteCount > 0 {
			aggregate.cacheWriteRequests += row.CacheWriteCount
			aggregate.cacheWriteChannels[row.ChannelId] = struct{}{}
		}
	}

	items := make([]ChannelMonitorDailySuccessMetric, 0, len(aggregates))
	for dayStart, aggregate := range aggregates {
		items = append(items, ChannelMonitorDailySuccessMetric{
			DayStart:               dayStart,
			Summary:                aggregate.counts.summary(),
			CacheWriteChannelCount: len(aggregate.cacheWriteChannels),
			CacheWriteRequestCount: aggregate.cacheWriteRequests,
		})
	}
	sort.Slice(items, func(i int, j int) bool {
		return items[i].DayStart < items[j].DayStart
	})
	return items, nil
}

func cloneChannelMonitorTodaySuccessMetrics(metrics ChannelMonitorTodaySuccessMetrics) ChannelMonitorTodaySuccessMetrics {
	cloned := metrics
	if metrics.ChannelItems != nil {
		cloned.ChannelItems = make([]ChannelMonitorChannelSuccessMetric, len(metrics.ChannelItems))
		copy(cloned.ChannelItems, metrics.ChannelItems)
	}
	if metrics.APIKeyItems != nil {
		cloned.APIKeyItems = make([]ChannelMonitorSuccessAPIKeyMetric, len(metrics.APIKeyItems))
		copy(cloned.APIKeyItems, metrics.APIKeyItems)
	}
	if metrics.CacheWriteItems != nil {
		cloned.CacheWriteItems = make([]ChannelMonitorTodayCacheWriteMetric, len(metrics.CacheWriteItems))
		copy(cloned.CacheWriteItems, metrics.CacheWriteItems)
	}
	return cloned
}

func cachedChannelMonitorTodaySuccessMetrics(key channelMonitorTodaySuccessCacheKey, now time.Time) (ChannelMonitorTodaySuccessMetrics, bool) {
	channelMonitorTodaySuccessCache.RLock()
	entry, exists := channelMonitorTodaySuccessCache.items[key]
	channelMonitorTodaySuccessCache.RUnlock()
	if !exists || !now.Before(entry.expiresAt) {
		return ChannelMonitorTodaySuccessMetrics{}, false
	}
	return entry.metrics, true
}

func storeChannelMonitorTodaySuccessMetrics(key channelMonitorTodaySuccessCacheKey, now time.Time, metrics ChannelMonitorTodaySuccessMetrics) {
	channelMonitorTodaySuccessCache.Lock()
	for cachedKey, entry := range channelMonitorTodaySuccessCache.items {
		if !now.Before(entry.expiresAt) {
			delete(channelMonitorTodaySuccessCache.items, cachedKey)
		}
	}
	channelMonitorTodaySuccessCache.items[key] = channelMonitorTodaySuccessCacheEntry{
		expiresAt: now.Add(channelMonitorTodaySuccessCacheTTL),
		metrics:   metrics,
	}
	channelMonitorTodaySuccessCache.Unlock()
}

func cloneChannelMonitorDailySuccessMetrics(metrics []ChannelMonitorDailySuccessMetric) []ChannelMonitorDailySuccessMetric {
	if metrics == nil {
		return nil
	}
	cloned := make([]ChannelMonitorDailySuccessMetric, len(metrics))
	copy(cloned, metrics)
	return cloned
}

func cachedChannelMonitorDailySuccessMetrics(key channelMonitorDailySuccessCacheKey, now time.Time) ([]ChannelMonitorDailySuccessMetric, bool) {
	channelMonitorDailySuccessCache.RLock()
	entry, exists := channelMonitorDailySuccessCache.items[key]
	channelMonitorDailySuccessCache.RUnlock()
	if !exists || !now.Before(entry.expiresAt) {
		return nil, false
	}
	return entry.metrics, true
}

func storeChannelMonitorDailySuccessMetrics(key channelMonitorDailySuccessCacheKey, now time.Time, metrics []ChannelMonitorDailySuccessMetric) {
	channelMonitorDailySuccessCache.Lock()
	for cachedKey, entry := range channelMonitorDailySuccessCache.items {
		if !now.Before(entry.expiresAt) {
			delete(channelMonitorDailySuccessCache.items, cachedKey)
		}
	}
	channelMonitorDailySuccessCache.items[key] = channelMonitorDailySuccessCacheEntry{
		expiresAt: now.Add(channelMonitorTodaySuccessCacheTTL),
		metrics:   metrics,
	}
	channelMonitorDailySuccessCache.Unlock()
}

func GetChannelMonitorDailySuccessMetricsCached(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelMonitorDailySuccessMetric, error) {
	key := channelMonitorDailySuccessCacheKey{
		logDB:           LOG_DB,
		logDatabaseType: common.LogDatabaseType(),
		startTimestamp:  startTimestamp,
		endTimestamp:    endTimestamp,
	}
	now := time.Now()
	if metrics, exists := cachedChannelMonitorDailySuccessMetrics(key, now); exists {
		return cloneChannelMonitorDailySuccessMetrics(metrics), nil
	}

	singleflightKey := fmt.Sprintf("daily-success:%p:%s:%d:%d", key.logDB, key.logDatabaseType, startTimestamp, endTimestamp)
	result, err, _ := channelMonitorDailySuccessSingleflight.Do(singleflightKey, func() (any, error) {
		loadTime := time.Now()
		if metrics, exists := cachedChannelMonitorDailySuccessMetrics(key, loadTime); exists {
			return metrics, nil
		}
		metrics, queryErr := getChannelMonitorDailySuccessMetrics(ctx, startTimestamp, endTimestamp)
		if queryErr != nil {
			return nil, queryErr
		}
		storeChannelMonitorDailySuccessMetrics(key, loadTime, metrics)
		return metrics, nil
	})
	if err != nil {
		return nil, err
	}
	return cloneChannelMonitorDailySuccessMetrics(result.([]ChannelMonitorDailySuccessMetric)), nil
}

func GetChannelMonitorSuccessMetricsForDayCached(ctx context.Context, dayStart int64) (ChannelMonitorTodaySuccessMetrics, error) {
	return getChannelMonitorSuccessMetricsForDayCached(ctx, ChannelDailyCostDayStart(dayStart))
}

// GetChannelMonitorTodaySuccessMetricsCached aggregates one Beijing calendar
// day and bounds repeated dashboard refreshes to one log query per minute.
func GetChannelMonitorTodaySuccessMetricsCached(ctx context.Context, generatedAt int64) (ChannelMonitorTodaySuccessMetrics, error) {
	return getChannelMonitorSuccessMetricsForDayCached(ctx, ChannelDailyCostDayStart(generatedAt))
}

func getChannelMonitorSuccessMetricsForDayCached(ctx context.Context, dayStart int64) (ChannelMonitorTodaySuccessMetrics, error) {
	key := channelMonitorTodaySuccessCacheKey{
		logDB:           LOG_DB,
		logDatabaseType: common.LogDatabaseType(),
		dayStart:        dayStart,
	}
	now := time.Now()
	if metrics, exists := cachedChannelMonitorTodaySuccessMetrics(key, now); exists {
		return cloneChannelMonitorTodaySuccessMetrics(metrics), nil
	}

	singleflightKey := fmt.Sprintf("today-success:%p:%s:%d", key.logDB, key.logDatabaseType, key.dayStart)
	result, err, _ := channelMonitorTodaySuccessSingleflight.Do(singleflightKey, func() (any, error) {
		loadTime := time.Now()
		if metrics, exists := cachedChannelMonitorTodaySuccessMetrics(key, loadTime); exists {
			return metrics, nil
		}
		metrics, queryErr := getChannelMonitorTodaySuccessMetrics(ctx, key.dayStart)
		if queryErr != nil {
			return nil, queryErr
		}
		storeChannelMonitorTodaySuccessMetrics(key, loadTime, metrics)
		return metrics, nil
	})
	if err != nil {
		return ChannelMonitorTodaySuccessMetrics{}, err
	}
	return cloneChannelMonitorTodaySuccessMetrics(result.(ChannelMonitorTodaySuccessMetrics)), nil
}

func resetChannelMonitorTodaySuccessCache() {
	channelMonitorTodaySuccessCache.Lock()
	channelMonitorTodaySuccessCache.items = make(map[channelMonitorTodaySuccessCacheKey]channelMonitorTodaySuccessCacheEntry)
	channelMonitorTodaySuccessCache.Unlock()
	channelMonitorDailySuccessCache.Lock()
	channelMonitorDailySuccessCache.items = make(map[channelMonitorDailySuccessCacheKey]channelMonitorDailySuccessCacheEntry)
	channelMonitorDailySuccessCache.Unlock()
}
