package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSmartScheduleRuntimeSampleCountSharesParameterizedLiveLogs(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelSmartScheduleModelSampleState{}))
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&[]Log{
		{
			CreatedAt: now, Type: LogTypeConsume, ChannelId: 2401,
			ModelName: "gemini-2.5-pro-thinking-128", RequestId: "parameterized-success",
		},
		{
			CreatedAt: now, Type: LogTypeError, ChannelId: 2401,
			ModelName: "gemini-2.5-pro-thinking-512", RequestId: "parameterized-failure",
			Other: `{"status_code":503}`,
		},
		{
			CreatedAt: now, Type: LogTypeError, ChannelId: 2401,
			ModelName: "gemini-2.5-flash-thinking-512", RequestId: "different-model",
			Other: `{"status_code":503}`,
		},
	}).Error)

	sampleCount, err := GetChannelSmartScheduleRouteSampleCount(
		context.Background(), now-60, 2401, "gemini-2.5-pro-thinking-2048",
	)
	require.NoError(t, err)
	assert.Equal(t, int64(2), sampleCount)
}

func TestChannelSmartScheduleRuntimeSampleCountExcludesPartialMinuteHistory(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelSmartScheduleModelSampleState{}))
	now := common.GetTimestamp()
	minuteStart := channelMonitorMinuteStart(now) - channelMonitorMinuteSeconds
	sampleStart := minuteStart + 30
	require.NoError(t, db.Create(&ChannelMonitorMinuteMetric{
		MinuteStart: minuteStart, ChannelId: 2402,
		ModelKey: "model-a", GroupKey: "vip", APIKeyKey: "all",
		ModelName: "model-a", GroupName: "vip",
		ActualSuccessCount: 2, FinalSuccessCount: 2, SampleCount: 2,
	}).Error)
	require.NoError(t, db.Create(&[]Log{
		{
			CreatedAt: minuteStart + 20, Type: LogTypeConsume, ChannelId: 2402,
			ModelName: "model-a", RequestId: "before-runtime-state",
		},
		{
			CreatedAt: minuteStart + 40, Type: LogTypeConsume, ChannelId: 2402,
			ModelName: "model-a", RequestId: "after-runtime-state",
		},
	}).Error)

	sampleCount, err := GetChannelSmartScheduleRouteSampleCount(
		context.Background(), sampleStart, 2402, "model-a",
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), sampleCount)
}

func TestChannelSmartScheduleRuntimeSampleCountReadsOldPartialMinuteFromLogs(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelSmartScheduleModelSampleState{}))
	now := common.GetTimestamp()
	minuteStart := channelMonitorMinuteStart(now) - 6*channelMonitorMinuteSeconds
	sampleStart := minuteStart + 30
	require.NoError(t, db.Create(&ChannelMonitorMinuteMetric{
		MinuteStart: minuteStart, ChannelId: 2403,
		ModelKey: "model-a", GroupKey: "vip", APIKeyKey: "all",
		ModelName: "model-a", GroupName: "vip",
		ActualSuccessCount: 2, FinalSuccessCount: 2, SampleCount: 2,
	}).Error)
	require.NoError(t, db.Create(&[]Log{
		{
			CreatedAt: minuteStart + 20, Type: LogTypeConsume, ChannelId: 2403,
			ModelName: "model-a", RequestId: "old-before-runtime-state",
		},
		{
			CreatedAt: minuteStart + 40, Type: LogTypeConsume, ChannelId: 2403,
			ModelName: "model-a", RequestId: "old-after-runtime-state",
		},
	}).Error)

	sampleCount, err := GetChannelSmartScheduleRouteSampleCount(
		context.Background(), sampleStart, 2403, "model-a",
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), sampleCount)
}

func TestChannelSmartScheduleAdaptiveHealthMetricsCountsIndependentTPSAndRequestClasses(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&[]Log{
		{
			CreatedAt: now - 4, Type: LogTypeConsume, ChannelId: 2404,
			ModelName: "model-a", CompletionTokens: 10, UseTime: 1,
		},
		{
			CreatedAt: now - 3, Type: LogTypeConsume, ChannelId: 2404,
			ModelName: "model-a", IsStream: true, CompletionTokens: 10, UseTime: 1,
			Other: `{"frt":10000}`,
		},
		{
			CreatedAt: now - 2, Type: LogTypeError, ChannelId: 2404,
			ModelName: "model-a", Other: `{"status_code":503}`,
		},
		{
			CreatedAt: now - 1, Type: LogTypeError, ChannelId: 2404,
			ModelName: "model-a", Other: `{"status_code":429}`,
		},
		{
			CreatedAt: now, Type: LogTypeConsume, ChannelId: 2404,
			ModelName: "model-a", IsStream: true, CompletionTokens: 10, UseTime: 1,
			Other: `{"frt":100,"channel_monitor_smart_schedule_probe":true}`,
		},
	}).Error)

	results, err := GetChannelSmartScheduleAdaptiveHealthMetrics(
		context.Background(),
		[]ChannelSmartScheduleAdaptiveHealthMetricWindow{{
			ChannelId: 2404, ModelName: "model-a", StartTimestamp: now - 10,
			WarningSeconds: 5, CriticalSeconds: 10,
		}},
		now+1,
	)
	require.NoError(t, err)
	require.Len(t, results, 1)
	metric := results[0].Metric
	assert.Equal(t, int64(3), metric.RequestCount)
	assert.Equal(t, int64(1), metric.FailureCount)
	assert.Equal(t, int64(1), metric.SlowRequestCount)
	assert.Equal(t, int64(1), metric.HealthyRequestCount)
	assert.Equal(t, int64(1), metric.FirstTokenCount)
	assert.Equal(t, int64(2), metric.TPSSampleCount)
	assert.InDelta(t, 1, metric.LatencyPressure, 1e-9)
	assert.Equal(t, now-2, metric.LastUsedTime)
}
