package model

import (
	"context"
	"math"
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
		{
			CreatedAt: now, Type: LogTypeConsume, ChannelId: 2401,
			ModelName: "gemini-2.5-pro-thinking-1024", RequestId: "status-probe",
			Other: `{"channel_monitor_status_probe":true}`,
		},
	}).Error)

	sampleCount, err := GetChannelSmartScheduleRouteSampleCount(
		context.Background(), now-60, 2401, "gemini-2.5-pro-thinking-2048",
	)
	require.NoError(t, err)
	assert.Equal(t, int64(2), sampleCount)
}

func TestChannelSmartScheduleAdaptiveHealthMetricsSaturatesRetryDurationConversion(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&Log{
		CreatedAt: now, Type: LogTypeError, ChannelId: 2405,
		ModelName: "model-a", UseTime: int(^uint(0) >> 1), IsRetryAttempt: true,
		Other: `{"status_code":503}`,
	}).Error)

	results, err := GetChannelSmartScheduleAdaptiveHealthMetrics(
		context.Background(),
		[]ChannelSmartScheduleAdaptiveHealthMetricWindow{{
			ChannelId: 2405, ModelName: "model-a", StartTimestamp: now - 1,
			WarningSeconds: 5, CriticalSeconds: 10,
		}},
		now+1,
	)
	require.NoError(t, err)
	require.Len(t, results, 1)
	metric := results[0].Metric
	assert.Equal(t, int64(1), metric.StabilityRetryFailureCount)
	assert.Equal(t, float64(math.MaxInt64), metric.RetryFailureDurationTotalMs)
	require.Len(t, metric.RetryFailureDurationBuckets, 6)
	assert.Equal(t, int64(1), metric.RetryFailureDurationBuckets[5].Count)
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
		{
			CreatedAt: now, Type: LogTypeConsume, ChannelId: 2404,
			ModelName: "model-a", IsStream: true, CompletionTokens: 100, UseTime: 1,
			Other: `{"frt":100,"channel_monitor_status_probe":true}`,
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
	assert.InDelta(t, 10_000, metric.FirstTokenTotalMs, 1e-9)
	assert.Equal(t, int64(2), metric.TPSSampleCount)
	assert.InDelta(t, 20, metric.TPSTotal, 1e-9)
	assert.InDelta(t, 1, metric.LatencyPressure, 1e-9)
	assert.Equal(t, now-2, metric.LastUsedTime)
}

func TestChannelSmartScheduleAdaptiveHealthMetricsUsesNewestRequestCap(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&[]Log{
		{
			CreatedAt: now - 4, Type: LogTypeError, ChannelId: 2406,
			ModelName: "model-a", Other: `{"status_code":503}`,
		},
		{
			CreatedAt: now - 3, Type: LogTypeConsume, ChannelId: 2406,
			ModelName: "model-a",
		},
		{
			CreatedAt: now - 2, Type: LogTypeError, ChannelId: 2406,
			ModelName: "model-a", Other: `{"status_code":503}`,
		},
		{
			CreatedAt: now - 1, Type: LogTypeConsume, ChannelId: 2406,
			ModelName: "model-a",
		},
	}).Error)

	results, err := GetChannelSmartScheduleAdaptiveHealthMetrics(
		context.Background(),
		[]ChannelSmartScheduleAdaptiveHealthMetricWindow{{
			ChannelId: 2406, ModelName: "model-a", StartTimestamp: now - 10,
			MaxRequests: 2, WarningSeconds: 5, CriticalSeconds: 10,
		}},
		now+1,
	)
	require.NoError(t, err)
	require.Len(t, results, 1)
	metric := results[0].Metric
	assert.Equal(t, int64(2), metric.RequestCount)
	assert.Equal(t, int64(1), metric.FailureCount)
	assert.Equal(t, now-1, metric.LastUsedTime)
}

func TestChannelSmartScheduleAdaptiveHealthMetricsPreservesExactWindowAcrossMinutes(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.Create(&[]Log{
		{
			CreatedAt: 109, Type: LogTypeConsume, ChannelId: 2407,
			ModelName: "model-a", IsStream: true, CompletionTokens: 100, UseTime: 1,
			Other: `{"frt":9000}`,
		},
		{
			CreatedAt: 115, Type: LogTypeConsume, ChannelId: 2407,
			ModelName: "model-a", IsStream: true, CompletionTokens: 20, UseTime: 2,
			Other: `{"frt":125}`,
		},
		{
			CreatedAt: 119, Type: LogTypeError, ChannelId: 2407,
			ModelName: "model-a", RequestId: "cross-minute-retry", IsRetryAttempt: true,
			Other: `{"status_code":503,"channel_monitor_attempt_duration_ms":500}`,
		},
		{
			CreatedAt: 121, Type: LogTypeError, ChannelId: 2407,
			ModelName: "model-a", RequestId: "cross-minute-retry",
			Other: `{"status_code":503,"channel_monitor_final_retry_summary":true}`,
		},
	}).Error)

	results, err := GetChannelSmartScheduleAdaptiveHealthMetrics(
		context.Background(),
		[]ChannelSmartScheduleAdaptiveHealthMetricWindow{{
			ChannelId: 2407, ModelName: "model-a", StartTimestamp: 100, ObservationSince: 110,
			WarningSeconds: 5, CriticalSeconds: 10,
		}},
		122,
	)
	require.NoError(t, err)
	require.Len(t, results, 1)
	metric := results[0].Metric
	assert.Equal(t, int64(2), metric.RequestCount)
	assert.Equal(t, int64(1), metric.StabilitySuccessCount)
	assert.Equal(t, int64(1), metric.StabilityFailureCount)
	assert.Equal(t, int64(1), metric.StabilityFinalFailureCount)
	assert.Zero(t, metric.StabilityRetryFailureCount)
	assert.Equal(t, int64(1), metric.FirstTokenCount)
	assert.InDelta(t, 125, metric.FirstTokenTotalMs, 1e-9)
	assert.Equal(t, int64(1), metric.TPSSampleCount)
	assert.InDelta(t, 10, metric.TPSTotal, 1e-9)
	require.Len(t, metric.FirstTokenDurationBuckets, 1)
	assert.Equal(t, int64(1), metric.FirstTokenDurationBuckets[0].Count)
	assert.InDelta(t, 125, metric.FirstTokenDurationBuckets[0].TotalMs, 1e-9)
}
