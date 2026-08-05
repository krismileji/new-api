package model

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

const channelMonitorTodaySuccessCacheTTL = 10 * time.Second

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

type channelMonitorTodaySuccessCacheKey struct {
	db         *gorm.DB
	generation uint64
	dayStart   int64
}

type channelMonitorTodaySuccessCacheEntry struct {
	expiresAt time.Time
	metrics   ChannelMonitorTodaySuccessMetrics
}

type channelMonitorDailySuccessCacheKey struct {
	db             *gorm.DB
	generation     uint64
	startTimestamp int64
	endTimestamp   int64
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
var channelMonitorTodaySuccessCacheGeneration atomic.Uint64

func getChannelMonitorTodaySuccessMetrics(ctx context.Context, dayStart int64) (ChannelMonitorTodaySuccessMetrics, error) {
	return getChannelMonitorMinuteTodaySuccessMetrics(ctx, dayStart, dayStart+channelMonitorCostDaySeconds)
}

func getChannelMonitorDailySuccessMetrics(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelMonitorDailySuccessMetric, error) {
	return getChannelMonitorMinuteDailySuccessMetrics(ctx, startTimestamp, endTimestamp)
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

func loadChannelMonitorTodaySuccessCacheEntry(key channelMonitorTodaySuccessCacheKey) (channelMonitorTodaySuccessCacheEntry, bool) {
	channelMonitorTodaySuccessCache.RLock()
	entry, exists := channelMonitorTodaySuccessCache.items[key]
	channelMonitorTodaySuccessCache.RUnlock()
	return entry, exists
}

func cachedChannelMonitorTodaySuccessMetrics(key channelMonitorTodaySuccessCacheKey, now time.Time) (ChannelMonitorTodaySuccessMetrics, bool) {
	entry, exists := loadChannelMonitorTodaySuccessCacheEntry(key)
	if !exists || !now.Before(entry.expiresAt) {
		return ChannelMonitorTodaySuccessMetrics{}, false
	}
	return entry.metrics, true
}

func storeChannelMonitorTodaySuccessCacheEntry(key channelMonitorTodaySuccessCacheKey, now time.Time, entry channelMonitorTodaySuccessCacheEntry) {
	channelMonitorTodaySuccessCache.Lock()
	if key.generation != channelMonitorTodaySuccessCacheGeneration.Load() {
		channelMonitorTodaySuccessCache.Unlock()
		return
	}
	for cachedKey, cachedEntry := range channelMonitorTodaySuccessCache.items {
		if !now.Before(cachedEntry.expiresAt) {
			delete(channelMonitorTodaySuccessCache.items, cachedKey)
		}
	}
	entry.expiresAt = now.Add(channelMonitorTodaySuccessCacheTTL)
	channelMonitorTodaySuccessCache.items[key] = entry
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
	if key.generation != channelMonitorTodaySuccessCacheGeneration.Load() {
		channelMonitorDailySuccessCache.Unlock()
		return
	}
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
	for {
		key := channelMonitorDailySuccessCacheKey{
			db:             DB,
			generation:     channelMonitorTodaySuccessCacheGeneration.Load(),
			startTimestamp: startTimestamp,
			endTimestamp:   endTimestamp,
		}
		now := time.Now()
		if metrics, exists := cachedChannelMonitorDailySuccessMetrics(key, now); exists {
			if key.generation == channelMonitorTodaySuccessCacheGeneration.Load() {
				return cloneChannelMonitorDailySuccessMetrics(metrics), nil
			}
			continue
		}

		singleflightKey := fmt.Sprintf(
			"daily-success:%p:%d:%d:%d", key.db, key.generation, startTimestamp, endTimestamp,
		)
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
		if key.generation != channelMonitorTodaySuccessCacheGeneration.Load() {
			continue
		}
		return cloneChannelMonitorDailySuccessMetrics(result.([]ChannelMonitorDailySuccessMetric)), nil
	}
}

func GetChannelMonitorSuccessMetricsForDayCached(ctx context.Context, dayStart int64) (ChannelMonitorTodaySuccessMetrics, error) {
	return getChannelMonitorSuccessMetricsForDayCached(ctx, ChannelDailyCostDayStart(dayStart), time.Now().Unix())
}

// GetChannelMonitorTodaySuccessMetricsCached reads one Beijing calendar day
// from the background minute aggregates.
func GetChannelMonitorTodaySuccessMetricsCached(ctx context.Context, generatedAt int64) (ChannelMonitorTodaySuccessMetrics, error) {
	return getChannelMonitorSuccessMetricsForDayCached(ctx, ChannelDailyCostDayStart(generatedAt), generatedAt)
}

func getChannelMonitorSuccessMetricsForDayCached(ctx context.Context, dayStart int64, generatedAt int64) (ChannelMonitorTodaySuccessMetrics, error) {
	for {
		key := channelMonitorTodaySuccessCacheKey{
			db:         DB,
			generation: channelMonitorTodaySuccessCacheGeneration.Load(),
			dayStart:   dayStart,
		}
		now := time.Now()
		if metrics, exists := cachedChannelMonitorTodaySuccessMetrics(key, now); exists {
			if key.generation == channelMonitorTodaySuccessCacheGeneration.Load() {
				return cloneChannelMonitorTodaySuccessMetrics(metrics), nil
			}
			continue
		}

		singleflightKey := fmt.Sprintf("today-success:%p:%d:%d", key.db, key.generation, key.dayStart)
		result, err, _ := channelMonitorTodaySuccessSingleflight.Do(singleflightKey, func() (any, error) {
			loadTime := time.Now()
			if metrics, exists := cachedChannelMonitorTodaySuccessMetrics(key, loadTime); exists {
				return metrics, nil
			}
			metrics, queryErr := getChannelMonitorMinuteTodaySuccessMetrics(ctx, key.dayStart, generatedAt)
			if queryErr != nil {
				return nil, queryErr
			}
			entry := channelMonitorTodaySuccessCacheEntry{metrics: metrics}
			storeChannelMonitorTodaySuccessCacheEntry(key, loadTime, entry)
			return metrics, nil
		})
		if err != nil {
			return ChannelMonitorTodaySuccessMetrics{}, err
		}
		if key.generation != channelMonitorTodaySuccessCacheGeneration.Load() {
			continue
		}
		return cloneChannelMonitorTodaySuccessMetrics(result.(ChannelMonitorTodaySuccessMetrics)), nil
	}
}

func resetChannelMonitorTodaySuccessCache() {
	channelMonitorTodaySuccessCache.Lock()
	channelMonitorTodaySuccessCacheGeneration.Add(1)
	channelMonitorTodaySuccessCache.items = make(map[channelMonitorTodaySuccessCacheKey]channelMonitorTodaySuccessCacheEntry)
	channelMonitorTodaySuccessCache.Unlock()
	channelMonitorDailySuccessCache.Lock()
	channelMonitorDailySuccessCache.items = make(map[channelMonitorDailySuccessCacheKey]channelMonitorDailySuccessCacheEntry)
	channelMonitorDailySuccessCache.Unlock()
}
