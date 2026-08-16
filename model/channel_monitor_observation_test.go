package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorObservationBoundaryResetsCurrentMetricsButPreservesHistory(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	resetChannelMonitorMetricsCache()
	t.Cleanup(resetChannelMonitorMetricsCache)

	oldFirstToken := 1000.0
	oldTPS := 10.0
	newFirstToken := 200.0
	newTPS := 40.0
	modelKey := channelMonitorMinuteDimensionKey("model-a")
	groupKey := channelMonitorMinuteDimensionKey("vip")
	require.NoError(t, db.Create(&[]ChannelMonitorMinuteRouteMetric{
		{
			MinuteStart: 120, ChannelId: 71, ModelKey: modelKey, GroupKey: groupKey, APIKeyKey: "id:11",
			ModelName: "model-a", GroupName: "vip", APIKeyId: 11, APIKeyName: "主 Key",
			ActualSuccessCount: 1, ActualFailureCount: 1, FinalSuccessCount: 1, FinalFailureCount: 1,
			CacheHitCount: 1, CacheSampleCount: 1,
			SampleCount: 2, FirstTokenSampleCount: 1, FirstTokenTotalMs: oldFirstToken,
			LatestFirstTokenMs: &oldFirstToken, LatestFirstTokenAt: 121,
			TPSSampleCount: 1, TPSTotal: oldTPS, LatestTPS: &oldTPS, LatestTPSAt: 121,
			LastUsedTime: 121,
		},
		{
			MinuteStart: 240, ChannelId: 71, ModelKey: modelKey, GroupKey: groupKey, APIKeyKey: "id:11",
			ModelName: "model-a", GroupName: "vip", APIKeyId: 11, APIKeyName: "主 Key",
			ActualSuccessCount: 2, FinalSuccessCount: 2,
			CacheHitCount: 1, CacheSampleCount: 2,
			SampleCount: 2, FirstTokenSampleCount: 2, FirstTokenTotalMs: 400,
			LatestFirstTokenMs: &newFirstToken, LatestFirstTokenAt: 241,
			TPSSampleCount: 2, TPSTotal: 80, LatestTPS: &newTPS, LatestTPSAt: 241,
			LastUsedTime: 241,
		},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelMonitorMinuteDurationBucket{
		{
			MinuteStart: 120, ChannelId: 71, ModelKey: modelKey, GroupKey: groupKey,
			ModelName: "model-a", GroupName: "vip", BucketIndex: channelMonitorDurationBucketIndex(1000),
			Count: 1, TotalMs: 1000,
		},
		{
			MinuteStart: 240, ChannelId: 71, ModelKey: modelKey, GroupKey: groupKey,
			ModelName: "model-a", GroupName: "vip", BucketIndex: channelMonitorDurationBucketIndex(200),
			Count: 2, TotalMs: 400,
		},
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleModelSampleState{
		ChannelId: 71, ModelName: "model-a", ObservationSince: 180,
	}).Error)

	performance, err := GetChannelMonitorPerformanceMetricsCached(context.Background(), 300, 3)
	require.NoError(t, err)
	require.Len(t, performance, 1)
	assert.Equal(t, 2, performance[0].SampleCount)
	assert.Equal(t, 2, performance[0].FirstTokenSampleCount)
	require.NotNil(t, performance[0].AverageFirstTokenMs)
	assert.InDelta(t, 200, *performance[0].AverageFirstTokenMs, 1e-9)
	require.NotNil(t, performance[0].LatestFirstTokenMs)
	assert.InDelta(t, 200, *performance[0].LatestFirstTokenMs, 1e-9)

	success, groups, err := GetChannelMonitorSuccessMetricsCached(context.Background(), 300, 3)
	require.NoError(t, err)
	require.Len(t, success, 1)
	assert.Equal(t, int64(2), success[0].ActualSuccessCount)
	assert.Zero(t, success[0].ActualFailureCount)
	assert.Equal(t, int64(1), success[0].CacheHitCount)
	assert.Equal(t, int64(2), success[0].CacheSampleCount)
	require.Len(t, groups, 1)
	assert.Equal(t, int64(2), groups[0].ActualSampleCount)

	routePerformance, err := GetChannelMonitorRoutePerformanceMetrics(context.Background(), 120, 300)
	require.NoError(t, err)
	require.Len(t, routePerformance, 1)
	assert.Equal(t, 2, routePerformance[0].SampleCount)
	assert.Equal(t, int64(2), routePerformance[0].FirstTokenDurationSampleCount)
	require.Len(t, routePerformance[0].FirstTokenDurationBuckets, 1)
	assert.Equal(t, int64(2), routePerformance[0].FirstTokenDurationBuckets[0].Count)
	assert.InDelta(t, 400, routePerformance[0].FirstTokenDurationBuckets[0].TotalMs, 1e-9)

	stability, err := GetChannelMonitorRouteStabilityMetrics(context.Background(), 120, 300)
	require.NoError(t, err)
	require.Len(t, stability, 1)
	assert.Equal(t, int64(2), stability[0].SuccessCount)
	assert.Zero(t, stability[0].FailureCount)
	assert.InDelta(t, 1, stability[0].SuccessRate, 1e-9)

	history, err := GetChannelMonitorSuccessDetail(context.Background(), 120, ChannelMonitorSuccessFilter{
		ChannelId: 71,
		ModelName: "model-a",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(3), history.Summary.ActualSuccessCount)
	assert.Equal(t, int64(1), history.Summary.ActualFailureCount)
	assert.Equal(t, int64(2), history.Summary.CacheHitCount)
	assert.Equal(t, int64(3), history.Summary.CacheSampleCount)
}
