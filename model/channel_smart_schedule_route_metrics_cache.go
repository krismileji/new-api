package model

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

type channelMonitorRouteMetricsCacheKey struct {
	db           *gorm.DB
	databaseType common.DatabaseType
	generation   uint64
	windowStart  int64
	windowEnd    int64
}

type channelMonitorRouteWindowMetricsCacheKey struct {
	channelMonitorRouteMetricsCacheKey
	windowsHash        [sha256.Size]byte
	windowCount        int
	includePerformance bool
	includeStability   bool
}

type channelMonitorRoutePerformanceCacheEntry struct {
	expiresAt time.Time
	metrics   []ChannelMonitorRoutePerformanceMetric
}

type channelMonitorRouteStabilityCacheEntry struct {
	expiresAt time.Time
	metrics   []ChannelMonitorRouteStabilityMetric
}

type channelMonitorRouteWindowCacheEntry struct {
	expiresAt time.Time
	metrics   []ChannelMonitorRouteWindowMetrics
}

var channelMonitorRouteMetricsCache = struct {
	sync.RWMutex
	performance map[channelMonitorRouteMetricsCacheKey]channelMonitorRoutePerformanceCacheEntry
	stability   map[channelMonitorRouteMetricsCacheKey]channelMonitorRouteStabilityCacheEntry
	windows     map[channelMonitorRouteWindowMetricsCacheKey]channelMonitorRouteWindowCacheEntry
}{
	performance: make(map[channelMonitorRouteMetricsCacheKey]channelMonitorRoutePerformanceCacheEntry),
	stability:   make(map[channelMonitorRouteMetricsCacheKey]channelMonitorRouteStabilityCacheEntry),
	windows:     make(map[channelMonitorRouteWindowMetricsCacheKey]channelMonitorRouteWindowCacheEntry),
}

var channelMonitorRouteMetricsSingleflight singleflight.Group

func newChannelMonitorRouteMetricsCacheKey(
	startTimestamp int64,
	endTimestamp int64,
) channelMonitorRouteMetricsCacheKey {
	startTimestamp, endTimestamp = channelMonitorMinuteRange(startTimestamp, endTimestamp)
	return channelMonitorRouteMetricsCacheKey{
		db:           DB,
		databaseType: common.MainDatabaseType(),
		generation:   channelMonitorMetricsCacheGeneration.Load(),
		windowStart:  startTimestamp,
		windowEnd:    endTimestamp,
	}
}

func (key channelMonitorRouteMetricsCacheKey) singleflightKey(metricType string) string {
	return fmt.Sprintf(
		"smart-schedule:%s:%p:%s:%d:%d:%d",
		metricType,
		key.db,
		key.databaseType,
		key.generation,
		key.windowStart,
		key.windowEnd,
	)
}

func cloneChannelMonitorRoutePerformanceMetrics(
	metrics []ChannelMonitorRoutePerformanceMetric,
) []ChannelMonitorRoutePerformanceMetric {
	if metrics == nil {
		return nil
	}
	cloned := make([]ChannelMonitorRoutePerformanceMetric, len(metrics))
	copy(cloned, metrics)
	for index := range cloned {
		cloned[index].AverageFirstTokenMs = cloneChannelMonitorMetricFloat(metrics[index].AverageFirstTokenMs)
		cloned[index].FirstTokenP50Ms = cloneChannelMonitorMetricFloat(metrics[index].FirstTokenP50Ms)
		cloned[index].FirstTokenP95Ms = cloneChannelMonitorMetricFloat(metrics[index].FirstTokenP95Ms)
		cloned[index].WinsorizedAverageFirstTokenMs = cloneChannelMonitorMetricFloat(
			metrics[index].WinsorizedAverageFirstTokenMs,
		)
		cloned[index].AverageTPS = cloneChannelMonitorMetricFloat(metrics[index].AverageTPS)
		cloned[index].FirstTokenDurationBuckets = append(
			[]ChannelMonitorDurationBucket(nil),
			metrics[index].FirstTokenDurationBuckets...,
		)
	}
	return cloned
}

func cloneChannelMonitorRouteStabilityMetrics(
	metrics []ChannelMonitorRouteStabilityMetric,
) []ChannelMonitorRouteStabilityMetric {
	if metrics == nil {
		return nil
	}
	cloned := make([]ChannelMonitorRouteStabilityMetric, len(metrics))
	copy(cloned, metrics)
	for index := range cloned {
		cloned[index].StabilityScore = cloneChannelMonitorMetricFloat(metrics[index].StabilityScore)
		cloned[index].FirstTokenP50Ms = cloneChannelMonitorMetricFloat(metrics[index].FirstTokenP50Ms)
		cloned[index].FirstTokenP95Ms = cloneChannelMonitorMetricFloat(metrics[index].FirstTokenP95Ms)
		cloned[index].JitterThresholdMs = cloneChannelMonitorMetricFloat(metrics[index].JitterThresholdMs)
		cloned[index].RetryFailureDurationBuckets = append(
			[]ChannelMonitorFailureDurationBucket(nil),
			metrics[index].RetryFailureDurationBuckets...,
		)
	}
	return cloned
}

func cloneChannelMonitorRouteWindowMetrics(
	metrics []ChannelMonitorRouteWindowMetrics,
) []ChannelMonitorRouteWindowMetrics {
	if metrics == nil {
		return nil
	}
	cloned := make([]ChannelMonitorRouteWindowMetrics, len(metrics))
	for index := range metrics {
		cloned[index].Window = metrics[index].Window
		cloned[index].Performance = cloneChannelMonitorRoutePerformanceMetrics(
			[]ChannelMonitorRoutePerformanceMetric{metrics[index].Performance},
		)[0]
		cloned[index].Stability = cloneChannelMonitorRouteStabilityMetrics(
			[]ChannelMonitorRouteStabilityMetric{metrics[index].Stability},
		)[0]
	}
	return cloned
}

func cloneChannelMonitorMetricFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cachedChannelMonitorRoutePerformanceMetrics(
	key channelMonitorRouteMetricsCacheKey,
	now time.Time,
) ([]ChannelMonitorRoutePerformanceMetric, bool) {
	channelMonitorRouteMetricsCache.RLock()
	entry, exists := channelMonitorRouteMetricsCache.performance[key]
	channelMonitorRouteMetricsCache.RUnlock()
	if !exists || !now.Before(entry.expiresAt) {
		return nil, false
	}
	return entry.metrics, true
}

func cachedChannelMonitorRouteStabilityMetrics(
	key channelMonitorRouteMetricsCacheKey,
	now time.Time,
) ([]ChannelMonitorRouteStabilityMetric, bool) {
	channelMonitorRouteMetricsCache.RLock()
	entry, exists := channelMonitorRouteMetricsCache.stability[key]
	channelMonitorRouteMetricsCache.RUnlock()
	if !exists || !now.Before(entry.expiresAt) {
		return nil, false
	}
	return entry.metrics, true
}

func storeChannelMonitorRoutePerformanceMetrics(
	key channelMonitorRouteMetricsCacheKey,
	now time.Time,
	metrics []ChannelMonitorRoutePerformanceMetric,
) {
	channelMonitorRouteMetricsCache.Lock()
	defer channelMonitorRouteMetricsCache.Unlock()
	if key.generation != channelMonitorMetricsCacheGeneration.Load() {
		return
	}
	for cachedKey, entry := range channelMonitorRouteMetricsCache.performance {
		if !now.Before(entry.expiresAt) {
			delete(channelMonitorRouteMetricsCache.performance, cachedKey)
		}
	}
	channelMonitorRouteMetricsCache.performance[key] = channelMonitorRoutePerformanceCacheEntry{
		expiresAt: now.Add(channelMonitorMetricsCacheTTL),
		metrics:   metrics,
	}
}

func storeChannelMonitorRouteStabilityMetrics(
	key channelMonitorRouteMetricsCacheKey,
	now time.Time,
	metrics []ChannelMonitorRouteStabilityMetric,
) {
	channelMonitorRouteMetricsCache.Lock()
	defer channelMonitorRouteMetricsCache.Unlock()
	if key.generation != channelMonitorMetricsCacheGeneration.Load() {
		return
	}
	for cachedKey, entry := range channelMonitorRouteMetricsCache.stability {
		if !now.Before(entry.expiresAt) {
			delete(channelMonitorRouteMetricsCache.stability, cachedKey)
		}
	}
	channelMonitorRouteMetricsCache.stability[key] = channelMonitorRouteStabilityCacheEntry{
		expiresAt: now.Add(channelMonitorMetricsCacheTTL),
		metrics:   metrics,
	}
}

// GetChannelMonitorRoutePerformanceMetricsCached reuses one completed-minute
// route window across dashboard requests and smart-schedule executions.
func GetChannelMonitorRoutePerformanceMetricsCached(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
) ([]ChannelMonitorRoutePerformanceMetric, error) {
	for {
		key := newChannelMonitorRouteMetricsCacheKey(startTimestamp, endTimestamp)
		now := time.Now()
		if metrics, exists := cachedChannelMonitorRoutePerformanceMetrics(key, now); exists {
			if key.generation == channelMonitorMetricsCacheGeneration.Load() {
				return cloneChannelMonitorRoutePerformanceMetrics(metrics), nil
			}
			continue
		}

		result, err, _ := channelMonitorRouteMetricsSingleflight.Do(
			key.singleflightKey("performance"),
			func() (any, error) {
				loadTime := time.Now()
				if metrics, exists := cachedChannelMonitorRoutePerformanceMetrics(key, loadTime); exists {
					return metrics, nil
				}
				metrics, queryErr := GetChannelMonitorRoutePerformanceMetrics(
					ctx, key.windowStart, key.windowEnd,
				)
				if queryErr != nil {
					return nil, queryErr
				}
				storeChannelMonitorRoutePerformanceMetrics(key, loadTime, metrics)
				return metrics, nil
			},
		)
		if err != nil {
			return nil, err
		}
		if key.generation != channelMonitorMetricsCacheGeneration.Load() {
			continue
		}
		return cloneChannelMonitorRoutePerformanceMetrics(
			result.([]ChannelMonitorRoutePerformanceMetric),
		), nil
	}
}

// GetChannelMonitorRouteStabilityMetricsCached reuses one completed-minute
// stability window across dashboard requests and smart-schedule executions.
func GetChannelMonitorRouteStabilityMetricsCached(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
) ([]ChannelMonitorRouteStabilityMetric, error) {
	for {
		key := newChannelMonitorRouteMetricsCacheKey(startTimestamp, endTimestamp)
		now := time.Now()
		if metrics, exists := cachedChannelMonitorRouteStabilityMetrics(key, now); exists {
			if key.generation == channelMonitorMetricsCacheGeneration.Load() {
				return cloneChannelMonitorRouteStabilityMetrics(metrics), nil
			}
			continue
		}

		result, err, _ := channelMonitorRouteMetricsSingleflight.Do(
			key.singleflightKey("stability"),
			func() (any, error) {
				loadTime := time.Now()
				if metrics, exists := cachedChannelMonitorRouteStabilityMetrics(key, loadTime); exists {
					return metrics, nil
				}
				metrics, queryErr := GetChannelMonitorRouteStabilityMetrics(
					ctx, key.windowStart, key.windowEnd,
				)
				if queryErr != nil {
					return nil, queryErr
				}
				storeChannelMonitorRouteStabilityMetrics(key, loadTime, metrics)
				return metrics, nil
			},
		)
		if err != nil {
			return nil, err
		}
		if key.generation != channelMonitorMetricsCacheGeneration.Load() {
			continue
		}
		return cloneChannelMonitorRouteStabilityMetrics(
			result.([]ChannelMonitorRouteStabilityMetric),
		), nil
	}
}

func normalizeChannelMonitorRouteMetricWindows(
	windows []ChannelMonitorRouteMetricWindow,
) []ChannelMonitorRouteMetricWindow {
	unique := make(map[ChannelMonitorRouteMetricWindow]struct{}, len(windows))
	for _, window := range windows {
		window.ModelName = channelSmartScheduleModelName(window.ModelName)
		window.StartTimestamp = channelMonitorMinuteStart(window.StartTimestamp)
		if window.ChannelId <= 0 || window.ModelName == "" {
			continue
		}
		unique[window] = struct{}{}
	}
	normalized := make([]ChannelMonitorRouteMetricWindow, 0, len(unique))
	for window := range unique {
		normalized = append(normalized, window)
	}
	sort.Slice(normalized, func(i int, j int) bool {
		if normalized[i].ModelName != normalized[j].ModelName {
			return normalized[i].ModelName < normalized[j].ModelName
		}
		if normalized[i].ChannelId != normalized[j].ChannelId {
			return normalized[i].ChannelId < normalized[j].ChannelId
		}
		return normalized[i].StartTimestamp < normalized[j].StartTimestamp
	})
	return normalized
}

func hashChannelMonitorRouteMetricWindows(
	windows []ChannelMonitorRouteMetricWindow,
) [sha256.Size]byte {
	hasher := sha256.New()
	buffer := make([]byte, 8)
	for _, window := range windows {
		binary.LittleEndian.PutUint64(buffer, uint64(window.ChannelId))
		_, _ = hasher.Write(buffer)
		binary.LittleEndian.PutUint64(buffer, uint64(len(window.ModelName)))
		_, _ = hasher.Write(buffer)
		_, _ = hasher.Write([]byte(window.ModelName))
		binary.LittleEndian.PutUint64(buffer, uint64(window.StartTimestamp))
		_, _ = hasher.Write(buffer)
	}
	var result [sha256.Size]byte
	copy(result[:], hasher.Sum(nil))
	return result
}

func cachedChannelMonitorRouteWindowMetrics(
	key channelMonitorRouteWindowMetricsCacheKey,
	now time.Time,
) ([]ChannelMonitorRouteWindowMetrics, bool) {
	channelMonitorRouteMetricsCache.RLock()
	entry, exists := channelMonitorRouteMetricsCache.windows[key]
	channelMonitorRouteMetricsCache.RUnlock()
	if !exists || !now.Before(entry.expiresAt) {
		return nil, false
	}
	return entry.metrics, true
}

func storeChannelMonitorRouteWindowMetrics(
	key channelMonitorRouteWindowMetricsCacheKey,
	now time.Time,
	metrics []ChannelMonitorRouteWindowMetrics,
) {
	channelMonitorRouteMetricsCache.Lock()
	defer channelMonitorRouteMetricsCache.Unlock()
	if key.generation != channelMonitorMetricsCacheGeneration.Load() {
		return
	}
	for cachedKey, entry := range channelMonitorRouteMetricsCache.windows {
		if !now.Before(entry.expiresAt) {
			delete(channelMonitorRouteMetricsCache.windows, cachedKey)
		}
	}
	channelMonitorRouteMetricsCache.windows[key] = channelMonitorRouteWindowCacheEntry{
		expiresAt: now.Add(channelMonitorMetricsCacheTTL),
		metrics:   metrics,
	}
}

// GetChannelMonitorRouteMetricsForWindowsCached caches the batched probing
// windows after canonicalizing their order, so concurrent callers share work.
func GetChannelMonitorRouteMetricsForWindowsCached(
	ctx context.Context,
	windows []ChannelMonitorRouteMetricWindow,
	endTimestamp int64,
	includePerformance bool,
	includeStability bool,
) ([]ChannelMonitorRouteWindowMetrics, error) {
	normalizedWindows := normalizeChannelMonitorRouteMetricWindows(windows)
	if len(normalizedWindows) == 0 || (!includePerformance && !includeStability) {
		return []ChannelMonitorRouteWindowMetrics{}, nil
	}
	windowsHash := hashChannelMonitorRouteMetricWindows(normalizedWindows)
	for {
		baseKey := newChannelMonitorRouteMetricsCacheKey(0, endTimestamp)
		key := channelMonitorRouteWindowMetricsCacheKey{
			channelMonitorRouteMetricsCacheKey: baseKey,
			windowsHash:                        windowsHash,
			windowCount:                        len(normalizedWindows),
			includePerformance:                 includePerformance,
			includeStability:                   includeStability,
		}
		now := time.Now()
		if metrics, exists := cachedChannelMonitorRouteWindowMetrics(key, now); exists {
			if key.generation == channelMonitorMetricsCacheGeneration.Load() {
				return cloneChannelMonitorRouteWindowMetrics(metrics), nil
			}
			continue
		}

		flightKey := fmt.Sprintf(
			"%s:%x:%d:%t:%t",
			baseKey.singleflightKey("probing-windows"),
			key.windowsHash,
			key.windowCount,
			key.includePerformance,
			key.includeStability,
		)
		result, err, _ := channelMonitorRouteMetricsSingleflight.Do(flightKey, func() (any, error) {
			loadTime := time.Now()
			if metrics, exists := cachedChannelMonitorRouteWindowMetrics(key, loadTime); exists {
				return metrics, nil
			}
			metrics, queryErr := GetChannelMonitorRouteMetricsForWindows(
				ctx,
				normalizedWindows,
				key.windowEnd,
				includePerformance,
				includeStability,
			)
			if queryErr != nil {
				return nil, queryErr
			}
			storeChannelMonitorRouteWindowMetrics(key, loadTime, metrics)
			return metrics, nil
		})
		if err != nil {
			return nil, err
		}
		if key.generation != channelMonitorMetricsCacheGeneration.Load() {
			continue
		}
		return cloneChannelMonitorRouteWindowMetrics(result.([]ChannelMonitorRouteWindowMetrics)), nil
	}
}

func resetChannelMonitorRouteMetricsCache() {
	channelMonitorRouteMetricsCache.Lock()
	channelMonitorRouteMetricsCache.performance = make(
		map[channelMonitorRouteMetricsCacheKey]channelMonitorRoutePerformanceCacheEntry,
	)
	channelMonitorRouteMetricsCache.stability = make(
		map[channelMonitorRouteMetricsCacheKey]channelMonitorRouteStabilityCacheEntry,
	)
	channelMonitorRouteMetricsCache.windows = make(
		map[channelMonitorRouteWindowMetricsCacheKey]channelMonitorRouteWindowCacheEntry,
	)
	channelMonitorRouteMetricsCache.Unlock()
}
