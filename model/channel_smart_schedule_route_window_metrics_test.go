package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChannelMonitorRouteMetricsForWindowsBatchesDifferentProbingStarts(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	modelAKey := channelMonitorMinuteDimensionKey("model-a")
	modelBKey := channelMonitorMinuteDimensionKey("model-b")
	groupKey := channelMonitorMinuteDimensionKey("vip")
	require.NoError(t, db.Create(&[]ChannelMonitorMinuteRouteMetric{
		{
			MinuteStart: 120, ChannelId: 61, ModelKey: modelAKey, GroupKey: groupKey, APIKeyKey: "key-1",
			ModelName: "model-a", GroupName: "vip", ActualSuccessCount: 2, ActualFailureCount: 1,
			FinalFailureCount: 1, SampleCount: 2, FirstTokenSampleCount: 2, FirstTokenTotalMs: 200,
			TPSSampleCount: 2, TPSTotal: 20, LastUsedTime: 140,
		},
		{
			MinuteStart: 180, ChannelId: 61, ModelKey: modelAKey, GroupKey: groupKey, APIKeyKey: "key-1",
			ModelName: "model-a", GroupName: "vip", ActualSuccessCount: 3, ActualFailureCount: 1,
			RetryFailureCount: 1, RetryFailureDurationTotalMs: 500, RetryFailureUnder1sCount: 1,
			SampleCount: 1, FirstTokenSampleCount: 1, FirstTokenTotalMs: 300,
			TPSSampleCount: 1, TPSTotal: 10, LastUsedTime: 190,
		},
		{
			MinuteStart: 180, ChannelId: 62, ModelKey: modelBKey, GroupKey: groupKey, APIKeyKey: "key-1",
			ModelName: "model-b", GroupName: "vip", ActualSuccessCount: 1,
			SampleCount: 1, FirstTokenSampleCount: 1, FirstTokenTotalMs: 400,
			TPSSampleCount: 1, TPSTotal: 5, LastUsedTime: 200,
		},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelMonitorMinuteDurationBucket{
		{
			MinuteStart: 120, ChannelId: 61, ModelKey: modelAKey, GroupKey: groupKey,
			ModelName: "model-a", GroupName: "vip", BucketIndex: channelMonitorDurationBucketIndex(100),
			Count: 2, TotalMs: 200,
		},
		{
			MinuteStart: 180, ChannelId: 61, ModelKey: modelAKey, GroupKey: groupKey,
			ModelName: "model-a", GroupName: "vip", BucketIndex: channelMonitorDurationBucketIndex(300),
			Count: 1, TotalMs: 300,
		},
	}).Error)

	metrics, err := GetChannelMonitorRouteMetricsForWindows(
		context.Background(),
		[]ChannelMonitorRouteMetricWindow{
			{ChannelId: 61, ModelName: "model-a", StartTimestamp: 120},
			{ChannelId: 61, ModelName: "model-a", StartTimestamp: 180},
			{ChannelId: 62, ModelName: "model-b", StartTimestamp: 180},
		},
		240,
		true,
		true,
	)
	require.NoError(t, err)
	require.Len(t, metrics, 3)

	byWindow := make(map[ChannelMonitorRouteMetricWindow]ChannelMonitorRouteWindowMetrics, len(metrics))
	for _, metric := range metrics {
		byWindow[metric.Window] = metric
	}
	full := byWindow[ChannelMonitorRouteMetricWindow{ChannelId: 61, ModelName: "model-a", StartTimestamp: 120}]
	assert.Equal(t, 3, full.Performance.SampleCount)
	assert.Equal(t, 3, full.Performance.FirstTokenSampleCount)
	assert.InDelta(t, 500.0/3.0, *full.Performance.AverageFirstTokenMs, 1e-9)
	assert.Equal(t, int64(3), full.Performance.FirstTokenDurationSampleCount)
	assert.Equal(t, int64(7), full.Stability.SampleCount)
	assert.Equal(t, int64(5), full.Stability.SuccessCount)
	assert.Equal(t, int64(2), full.Stability.FailureCount)
	assert.Equal(t, int64(1), full.Stability.FinalFailureCount)
	assert.Equal(t, int64(1), full.Stability.RetryFailureCount)

	recent := byWindow[ChannelMonitorRouteMetricWindow{ChannelId: 61, ModelName: "model-a", StartTimestamp: 180}]
	assert.Equal(t, 1, recent.Performance.SampleCount)
	assert.InDelta(t, 300, *recent.Performance.AverageFirstTokenMs, 1e-9)
	assert.Equal(t, int64(4), recent.Stability.SampleCount)
	assert.Equal(t, int64(3), recent.Stability.SuccessCount)
	assert.Equal(t, int64(1), recent.Stability.FailureCount)
	assert.Equal(t, int64(1), recent.Stability.RetryFailureCount)

	other := byWindow[ChannelMonitorRouteMetricWindow{ChannelId: 62, ModelName: "model-b", StartTimestamp: 180}]
	assert.Equal(t, 1, other.Performance.SampleCount)
	assert.Equal(t, int64(1), other.Stability.SampleCount)
}
