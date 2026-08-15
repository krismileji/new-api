package model

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

const (
	channelMonitorMetricsCacheTTL     = 10 * time.Second
	channelMonitorMetricsWindowBucket = time.Minute
)

type channelMonitorMetricsCacheKey struct {
	db           *gorm.DB
	databaseType common.DatabaseType
	generation   uint64
	windowEnd    int64
	rangeMinutes int
}

type channelMonitorPerformanceCacheEntry struct {
	expiresAt time.Time
	metrics   []ChannelMonitorPerformanceMetric
}

type channelMonitorSuccessCacheEntry struct {
	expiresAt    time.Time
	metrics      []ChannelMonitorSuccessMetric
	groupMetrics []ChannelMonitorGroupSuccessMetric
}

type channelMonitorSuccessCacheResult struct {
	metrics      []ChannelMonitorSuccessMetric
	groupMetrics []ChannelMonitorGroupSuccessMetric
}

var channelMonitorMetricsCache = struct {
	sync.RWMutex
	performance map[channelMonitorMetricsCacheKey]channelMonitorPerformanceCacheEntry
	success     map[channelMonitorMetricsCacheKey]channelMonitorSuccessCacheEntry
}{
	performance: make(map[channelMonitorMetricsCacheKey]channelMonitorPerformanceCacheEntry),
	success:     make(map[channelMonitorMetricsCacheKey]channelMonitorSuccessCacheEntry),
}

var channelMonitorMetricsSingleflight singleflight.Group
var channelMonitorMetricsCacheGeneration atomic.Uint64

func newChannelMonitorMetricsCacheKey(generatedAt int64, rangeMinutes int) channelMonitorMetricsCacheKey {
	bucketSeconds := int64(channelMonitorMetricsWindowBucket / time.Second)
	windowEnd := generatedAt - generatedAt%bucketSeconds
	return channelMonitorMetricsCacheKey{
		db:           DB,
		databaseType: common.MainDatabaseType(),
		generation:   channelMonitorMetricsCacheGeneration.Load(),
		windowEnd:    windowEnd,
		rangeMinutes: rangeMinutes,
	}
}

func (key channelMonitorMetricsCacheKey) singleflightKey(metricType string) string {
	return fmt.Sprintf(
		"%s:%p:%s:%d:%d:%d",
		metricType,
		key.db,
		key.databaseType,
		key.generation,
		key.windowEnd,
		key.rangeMinutes,
	)
}

func cloneChannelMonitorPerformanceMetrics(metrics []ChannelMonitorPerformanceMetric) []ChannelMonitorPerformanceMetric {
	if metrics == nil {
		return nil
	}
	cloned := make([]ChannelMonitorPerformanceMetric, len(metrics))
	copy(cloned, metrics)
	for index := range cloned {
		if metrics[index].AverageFirstTokenMs != nil {
			value := *metrics[index].AverageFirstTokenMs
			cloned[index].AverageFirstTokenMs = &value
		}
		if metrics[index].AverageTPS != nil {
			value := *metrics[index].AverageTPS
			cloned[index].AverageTPS = &value
		}
		if metrics[index].LatestFirstTokenMs != nil {
			value := *metrics[index].LatestFirstTokenMs
			cloned[index].LatestFirstTokenMs = &value
		}
		if metrics[index].LatestTPS != nil {
			value := *metrics[index].LatestTPS
			cloned[index].LatestTPS = &value
		}
	}
	return cloned
}

func cloneChannelMonitorSuccessMetrics(metrics []ChannelMonitorSuccessMetric) []ChannelMonitorSuccessMetric {
	if metrics == nil {
		return nil
	}
	cloned := make([]ChannelMonitorSuccessMetric, len(metrics))
	copy(cloned, metrics)
	return cloned
}

func cloneChannelMonitorGroupSuccessMetrics(metrics []ChannelMonitorGroupSuccessMetric) []ChannelMonitorGroupSuccessMetric {
	if metrics == nil {
		return nil
	}
	cloned := make([]ChannelMonitorGroupSuccessMetric, len(metrics))
	copy(cloned, metrics)
	return cloned
}

func cachedChannelMonitorPerformanceMetrics(key channelMonitorMetricsCacheKey, now time.Time) ([]ChannelMonitorPerformanceMetric, bool) {
	channelMonitorMetricsCache.RLock()
	entry, exists := channelMonitorMetricsCache.performance[key]
	channelMonitorMetricsCache.RUnlock()
	if !exists || !now.Before(entry.expiresAt) {
		return nil, false
	}
	return entry.metrics, true
}

func cachedChannelMonitorSuccessMetrics(key channelMonitorMetricsCacheKey, now time.Time) (channelMonitorSuccessCacheResult, bool) {
	channelMonitorMetricsCache.RLock()
	entry, exists := channelMonitorMetricsCache.success[key]
	channelMonitorMetricsCache.RUnlock()
	if !exists || !now.Before(entry.expiresAt) {
		return channelMonitorSuccessCacheResult{}, false
	}
	return channelMonitorSuccessCacheResult{
		metrics:      entry.metrics,
		groupMetrics: entry.groupMetrics,
	}, true
}

func storeChannelMonitorPerformanceMetrics(key channelMonitorMetricsCacheKey, now time.Time, metrics []ChannelMonitorPerformanceMetric) {
	channelMonitorMetricsCache.Lock()
	if key.generation != channelMonitorMetricsCacheGeneration.Load() {
		channelMonitorMetricsCache.Unlock()
		return
	}
	for cachedKey, entry := range channelMonitorMetricsCache.performance {
		if !now.Before(entry.expiresAt) {
			delete(channelMonitorMetricsCache.performance, cachedKey)
		}
	}
	channelMonitorMetricsCache.performance[key] = channelMonitorPerformanceCacheEntry{
		expiresAt: now.Add(channelMonitorMetricsCacheTTL),
		metrics:   metrics,
	}
	channelMonitorMetricsCache.Unlock()
}

func storeChannelMonitorSuccessMetrics(
	key channelMonitorMetricsCacheKey,
	now time.Time,
	metrics []ChannelMonitorSuccessMetric,
	groupMetrics []ChannelMonitorGroupSuccessMetric,
) {
	channelMonitorMetricsCache.Lock()
	if key.generation != channelMonitorMetricsCacheGeneration.Load() {
		channelMonitorMetricsCache.Unlock()
		return
	}
	for cachedKey, entry := range channelMonitorMetricsCache.success {
		if !now.Before(entry.expiresAt) {
			delete(channelMonitorMetricsCache.success, cachedKey)
		}
	}
	channelMonitorMetricsCache.success[key] = channelMonitorSuccessCacheEntry{
		expiresAt:    now.Add(channelMonitorMetricsCacheTTL),
		metrics:      metrics,
		groupMetrics: groupMetrics,
	}
	channelMonitorMetricsCache.Unlock()
}

// GetChannelMonitorPerformanceMetricsCached reads persisted minute aggregates
// and coalesces concurrent dashboard/task reads.
func GetChannelMonitorPerformanceMetricsCached(ctx context.Context, generatedAt int64, rangeMinutes int) ([]ChannelMonitorPerformanceMetric, error) {
	for {
		key := newChannelMonitorMetricsCacheKey(generatedAt, rangeMinutes)
		now := time.Now()
		if metrics, exists := cachedChannelMonitorPerformanceMetrics(key, now); exists {
			if key.generation == channelMonitorMetricsCacheGeneration.Load() {
				return cloneChannelMonitorPerformanceMetrics(metrics), nil
			}
			continue
		}

		result, err, _ := channelMonitorMetricsSingleflight.Do(key.singleflightKey("performance"), func() (any, error) {
			loadTime := time.Now()
			if metrics, exists := cachedChannelMonitorPerformanceMetrics(key, loadTime); exists {
				return metrics, nil
			}
			startTimestamp := key.windowEnd - int64(key.rangeMinutes*60)
			metrics, queryErr := getChannelMonitorObservedMinutePerformanceMetrics(ctx, startTimestamp, key.windowEnd)
			if queryErr != nil {
				return nil, queryErr
			}
			storeChannelMonitorPerformanceMetrics(key, loadTime, metrics)
			return metrics, nil
		})
		if err != nil {
			return nil, err
		}
		if key.generation != channelMonitorMetricsCacheGeneration.Load() {
			continue
		}
		return cloneChannelMonitorPerformanceMetrics(result.([]ChannelMonitorPerformanceMetric)), nil
	}
}

// GetChannelMonitorSuccessMetricsCached shares the success aggregation used by
// the dashboard and smart scheduler without changing filtered detail queries.
func GetChannelMonitorSuccessMetricsCached(ctx context.Context, generatedAt int64, rangeMinutes int) ([]ChannelMonitorSuccessMetric, []ChannelMonitorGroupSuccessMetric, error) {
	for {
		key := newChannelMonitorMetricsCacheKey(generatedAt, rangeMinutes)
		now := time.Now()
		if result, exists := cachedChannelMonitorSuccessMetrics(key, now); exists {
			if key.generation == channelMonitorMetricsCacheGeneration.Load() {
				return cloneChannelMonitorSuccessMetrics(result.metrics),
					cloneChannelMonitorGroupSuccessMetrics(result.groupMetrics), nil
			}
			continue
		}

		result, err, _ := channelMonitorMetricsSingleflight.Do(key.singleflightKey("success"), func() (any, error) {
			loadTime := time.Now()
			if cached, exists := cachedChannelMonitorSuccessMetrics(key, loadTime); exists {
				return cached, nil
			}
			metrics, groupMetrics, queryErr := getChannelMonitorObservedSuccessMetrics(
				ctx,
				key.windowEnd-int64(key.rangeMinutes*60),
				key.windowEnd,
				true,
			)
			if queryErr != nil {
				return nil, queryErr
			}
			storeChannelMonitorSuccessMetrics(key, loadTime, metrics, groupMetrics)
			return channelMonitorSuccessCacheResult{
				metrics:      metrics,
				groupMetrics: groupMetrics,
			}, nil
		})
		if err != nil {
			return nil, nil, err
		}
		if key.generation != channelMonitorMetricsCacheGeneration.Load() {
			continue
		}
		cached := result.(channelMonitorSuccessCacheResult)
		return cloneChannelMonitorSuccessMetrics(cached.metrics),
			cloneChannelMonitorGroupSuccessMetrics(cached.groupMetrics), nil
	}
}

func GetChannelMonitorStabilityMetricsCached(ctx context.Context, generatedAt int64, rangeMinutes int) ([]ChannelMonitorStabilityMetric, error) {
	metrics, _, err := GetChannelMonitorSuccessMetricsCached(ctx, generatedAt, rangeMinutes)
	if err != nil {
		return nil, err
	}
	return channelMonitorStabilityMetricsFromSuccess(metrics), nil
}

func resetChannelMonitorMetricsCache() {
	channelMonitorMetricsCache.Lock()
	channelMonitorMetricsCacheGeneration.Add(1)
	channelMonitorMetricsCache.performance = make(map[channelMonitorMetricsCacheKey]channelMonitorPerformanceCacheEntry)
	channelMonitorMetricsCache.success = make(map[channelMonitorMetricsCacheKey]channelMonitorSuccessCacheEntry)
	channelMonitorMetricsCache.Unlock()
}
