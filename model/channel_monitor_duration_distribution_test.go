package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummarizeChannelMonitorDurationBucketsCapsSingleSlowOutlierAtP95(t *testing.T) {
	fastIndex := channelMonitorDurationBucketIndex(300)
	slowIndex := channelMonitorDurationBucketIndex(10_000)
	buckets := channelMonitorDurationBucketsFromAggregates(map[int]ChannelMonitorDurationBucket{
		fastIndex: {Count: 99, TotalMs: 99 * 300},
		slowIndex: {Count: 1, TotalMs: 10_000},
	})

	sampleCount, p50, p95, winsorizedAverage := SummarizeChannelMonitorDurationBuckets(buckets)

	assert.Equal(t, int64(100), sampleCount)
	require.NotNil(t, p50)
	assert.InDelta(t, 350, *p50, 1e-9)
	require.NotNil(t, p95)
	assert.InDelta(t, 350, *p95, 1e-9)
	require.NotNil(t, winsorizedAverage)
	assert.InDelta(t, 300.5, *winsorizedAverage, 1e-9)
}

func TestAggregateChannelMonitorMinuteFirstTokenBucketsShareChannelModelAcrossGroups(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.Create(&[]Log{
		{ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 121, Type: LogTypeConsume, IsStream: true, Other: `{"frt":300}`},
		{ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 122, Type: LogTypeConsume, IsStream: true, Other: `{"frt":320}`},
		{ChannelId: 1, Group: "standard", ModelName: "model-a", CreatedAt: 123, Type: LogTypeConsume, IsStream: true, Other: `{"frt":2000}`},
		{ChannelId: 1, Group: "vip", ModelName: "model-b", CreatedAt: 124, Type: LogTypeConsume, IsStream: true, Other: `{"frt":5000}`},
		{ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 125, Type: LogTypeConsume, IsStream: false, Other: `{"frt":50}`},
		{ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 126, Type: LogTypeError, IsStream: true, Other: `{"frt":50}`},
		{ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 127, Type: LogTypeConsume, IsStream: true, Other: `{"frt":0}`},
		{ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 128, Type: LogTypeConsume, IsStream: true, Other: `{"frt":50,"channel_monitor_smart_schedule_probe":true}`},
	}).Error)

	aggregateChannelMonitorMinuteTestRange(t, 120, 180)
	metrics, err := GetChannelMonitorRoutePerformanceMetrics(context.Background(), 120, 180)
	require.NoError(t, err)
	require.Len(t, metrics, 2)

	byModel := make(map[string]ChannelMonitorRoutePerformanceMetric, len(metrics))
	for _, metric := range metrics {
		byModel[metric.ModelName] = metric
	}
	modelA := byModel["model-a"]
	assert.Equal(t, 2, modelA.GroupCount)
	assert.Equal(t, 3, modelA.FirstTokenSampleCount)
	assert.Equal(t, int64(3), modelA.FirstTokenDurationSampleCount)
	require.Len(t, modelA.FirstTokenDurationBuckets, 2)
	assert.Equal(t, int64(2), modelA.FirstTokenDurationBuckets[0].Count)
	assert.InDelta(t, 620, modelA.FirstTokenDurationBuckets[0].TotalMs, 1e-9)
	assert.Equal(t, int64(1), modelA.FirstTokenDurationBuckets[1].Count)
	assert.InDelta(t, 2000, modelA.FirstTokenDurationBuckets[1].TotalMs, 1e-9)
	require.NotNil(t, modelA.FirstTokenP50Ms)
	assert.InDelta(t, 350, *modelA.FirstTokenP50Ms, 1e-9)

	vipModelB := byModel["model-b"]
	assert.Equal(t, 1, vipModelB.GroupCount)
	assert.Equal(t, 1, vipModelB.FirstTokenSampleCount)
	assert.Equal(t, int64(1), vipModelB.FirstTokenDurationSampleCount)
	require.Len(t, vipModelB.FirstTokenDurationBuckets, 1)
	assert.InDelta(t, 5000, vipModelB.FirstTokenDurationBuckets[0].TotalMs, 1e-9)
}

func TestChannelMonitorRoutePerformanceMetricSeparatesHistoricalAndDistributionSamples(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	minuteStart := int64(120)
	require.NoError(t, db.Create(&ChannelMonitorMinuteMetric{
		MinuteStart: minuteStart, ChannelId: 9, ModelKey: "model-a", GroupKey: "vip", APIKeyKey: "all",
		ModelName: "model-a", GroupName: "vip", SampleCount: 1000,
		FirstTokenSampleCount: 1000, FirstTokenTotalMs: 300_000, LastUsedTime: minuteStart,
	}).Error)
	require.NoError(t, db.Create(&ChannelMonitorMinuteDurationBucket{
		MinuteStart: minuteStart, ChannelId: 9, ModelKey: "model-a", GroupKey: "vip",
		ModelName: "model-a", GroupName: "vip", BucketIndex: channelMonitorDurationBucketIndex(300),
		Count: 1, TotalMs: 300,
	}).Error)

	metrics, err := GetChannelMonitorRoutePerformanceMetrics(context.Background(), minuteStart, minuteStart+60)
	require.NoError(t, err)
	require.Len(t, metrics, 1)
	assert.Equal(t, 1000, metrics[0].FirstTokenSampleCount)
	assert.Equal(t, int64(1), metrics[0].FirstTokenDurationSampleCount)
}

func TestAggregateChannelMonitorMinuteRangeReplacesFirstTokenBuckets(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	logs := []Log{
		{ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 121, Type: LogTypeConsume, IsStream: true, Other: `{"frt":300}`},
		{ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 122, Type: LogTypeConsume, IsStream: true, Other: `{"frt":320}`},
	}
	require.NoError(t, db.Create(&logs).Error)
	aggregateChannelMonitorMinuteTestRange(t, 120, 180)

	var buckets []ChannelMonitorMinuteDurationBucket
	require.NoError(t, db.Find(&buckets).Error)
	require.Len(t, buckets, 1)
	assert.Equal(t, int64(2), buckets[0].Count)
	assert.InDelta(t, 620, buckets[0].TotalMs, 1e-9)

	require.NoError(t, db.Delete(&logs[1]).Error)
	require.NoError(t, db.Model(&logs[0]).Update("other", `{"frt":2000}`).Error)
	aggregateChannelMonitorMinuteTestRange(t, 120, 180)

	buckets = nil
	require.NoError(t, db.Find(&buckets).Error)
	require.Len(t, buckets, 1)
	assert.Equal(t, channelMonitorDurationBucketIndex(2000), buckets[0].BucketIndex)
	assert.Equal(t, int64(1), buckets[0].Count)
	assert.InDelta(t, 2000, buckets[0].TotalMs, 1e-9)
}

func TestChannelSmartScheduleSampleMetricsExposeFirstTokenDistribution(t *testing.T) {
	samples := make([]channelSmartScheduleSample, 0, 20)
	fast := 300.0
	for index := 0; index < 19; index++ {
		samples = append(samples, channelSmartScheduleSample{
			Time: int64(index + 1), Success: true, FirstTokenMs: &fast,
		})
	}
	slow := 10_000.0
	samples = append(samples, channelSmartScheduleSample{
		Time: 20, Success: true, FirstTokenMs: &slow,
	})

	metrics := channelSmartScheduleCalculateSampleMetrics(samples, 1)

	assert.Equal(t, int64(20), metrics.FirstTokenSampleCount)
	require.NotNil(t, metrics.FirstTokenP50Ms)
	assert.InDelta(t, 350, *metrics.FirstTokenP50Ms, 1e-9)
	require.NotNil(t, metrics.FirstTokenP95Ms)
	assert.InDelta(t, 350, *metrics.FirstTokenP95Ms, 1e-9)
	require.NotNil(t, metrics.WinsorizedAverageFirstTokenMs)
	assert.InDelta(t, 302.5, *metrics.WinsorizedAverageFirstTokenMs, 1e-9)
	require.Len(t, metrics.FirstTokenDurationBuckets, 2)
}
