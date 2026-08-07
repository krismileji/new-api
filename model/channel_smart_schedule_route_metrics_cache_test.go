package model

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetChannelMonitorRouteMetricsForWindowsCachedReturnsDefensiveCopy(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	resetChannelMonitorMetricsCache()
	t.Cleanup(resetChannelMonitorMetricsCache)
	require.NoError(t, db.Create(&[]ChannelMonitorMinuteMetric{
		{
			MinuteStart: 120, ChannelId: 71, ModelKey: "model-a", GroupKey: "vip", APIKeyKey: "all",
			ModelName: "model-a", GroupName: "vip", ActualSuccessCount: 2,
			SampleCount: 2, FirstTokenSampleCount: 2, FirstTokenTotalMs: 200,
			TPSSampleCount: 2, TPSTotal: 20, LastUsedTime: 150,
		},
		{
			MinuteStart: 120, ChannelId: 73, ModelKey: "model-a", GroupKey: "vip", APIKeyKey: "all",
			ModelName: "model-a", GroupName: "vip", ActualSuccessCount: 1,
			SampleCount: 1, FirstTokenSampleCount: 1, FirstTokenTotalMs: 300,
			TPSSampleCount: 1, TPSTotal: 5, LastUsedTime: 150,
		},
	}).Error)
	require.NoError(t, db.Create(&ChannelMonitorMinuteDurationBucket{
		MinuteStart: 120, ChannelId: 71, ModelKey: "model-a", GroupKey: "vip",
		ModelName: "model-a", GroupName: "vip", BucketIndex: channelMonitorDurationBucketIndex(100),
		Count: 2, TotalMs: 200,
	}).Error)
	var aggregateQueries atomic.Int32
	const callbackName = "test:count_canonical_route_window_queries"
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == channelMonitorMinuteMetricTable {
			aggregateQueries.Add(1)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Row().Remove(callbackName))
	})

	windows := []ChannelMonitorRouteMetricWindow{
		{ChannelId: 73, ModelName: "model-a", StartTimestamp: 120},
		{ChannelId: 71, ModelName: "model-a", StartTimestamp: 120},
		{ChannelId: 71, ModelName: "model-a", StartTimestamp: 120},
	}

	first, err := GetChannelMonitorRouteMetricsForWindowsCached(
		context.Background(), windows, 180, true, true,
	)
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.NotNil(t, first[0].Performance.AverageFirstTokenMs)
	require.NotEmpty(t, first[0].Performance.FirstTokenDurationBuckets)
	require.NotEmpty(t, first[0].Stability.RetryFailureDurationBuckets)
	*first[0].Performance.AverageFirstTokenMs = 999
	first[0].Performance.FirstTokenDurationBuckets[0].Count = 999
	first[0].Stability.RetryFailureDurationBuckets[0].Count = 999

	second, err := GetChannelMonitorRouteMetricsForWindowsCached(
		context.Background(), []ChannelMonitorRouteMetricWindow{
			{ChannelId: 71, ModelName: "model-a", StartTimestamp: 120},
			{ChannelId: 73, ModelName: "model-a", StartTimestamp: 120},
		}, 180, true, true,
	)
	require.NoError(t, err)
	require.Len(t, second, 2)
	require.NotNil(t, second[0].Performance.AverageFirstTokenMs)
	assert.InDelta(t, 100, *second[0].Performance.AverageFirstTokenMs, 1e-9)
	require.NotEmpty(t, second[0].Performance.FirstTokenDurationBuckets)
	assert.Equal(t, int64(2), second[0].Performance.FirstTokenDurationBuckets[0].Count)
	require.NotEmpty(t, second[0].Stability.RetryFailureDurationBuckets)
	assert.Zero(t, second[0].Stability.RetryFailureDurationBuckets[0].Count)
	assert.Equal(t, int32(1), aggregateQueries.Load())
}

func TestGetChannelMonitorRoutePerformanceMetricsCachedCoalescesConcurrentQueries(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	resetChannelMonitorMetricsCache()
	t.Cleanup(resetChannelMonitorMetricsCache)
	require.NoError(t, db.Create(&ChannelMonitorMinuteMetric{
		MinuteStart: 120, ChannelId: 72, ModelKey: "model-a", GroupKey: "vip", APIKeyKey: "all",
		ModelName: "model-a", GroupName: "vip", SampleCount: 1,
		FirstTokenSampleCount: 1, FirstTokenTotalMs: 100, LastUsedTime: 150,
	}).Error)

	var aggregateQueries atomic.Int32
	queryStarted := make(chan struct{})
	releaseQuery := make(chan struct{})
	var blockFirstQuery sync.Once
	const callbackName = "test:count_cached_route_performance_queries"
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == channelMonitorMinuteMetricTable {
			aggregateQueries.Add(1)
			blockFirstQuery.Do(func() {
				close(queryStarted)
				<-releaseQuery
			})
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Row().Remove(callbackName))
	})

	const callers = 16
	type queryResult struct {
		metrics []ChannelMonitorRoutePerformanceMetric
		err     error
	}
	results := make(chan queryResult, callers)
	go func() {
		metrics, err := GetChannelMonitorRoutePerformanceMetricsCached(
			context.Background(), 120, 180,
		)
		results <- queryResult{metrics: metrics, err: err}
	}()
	<-queryStarted
	for range callers - 1 {
		go func() {
			metrics, err := GetChannelMonitorRoutePerformanceMetricsCached(
				context.Background(), 120, 180,
			)
			results <- queryResult{metrics: metrics, err: err}
		}()
	}
	close(releaseQuery)
	for range callers {
		result := <-results
		require.NoError(t, result.err)
		require.Len(t, result.metrics, 1)
	}
	assert.Equal(t, int32(1), aggregateQueries.Load())
}
