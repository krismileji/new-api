package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSmartScheduleMetricsShareBusinessSamplesAcrossGroups(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.Create(&[]ChannelMonitorMinuteMetric{
		{
			MinuteStart: 120, ChannelId: 21, ModelKey: "model-a", GroupKey: "vip", APIKeyKey: "all",
			ModelName: "model-a", GroupName: "vip", SampleCount: 2,
			FirstTokenSampleCount: 2, FirstTokenTotalMs: 400,
			TPSSampleCount: 2, TPSTotal: 20, ActualSuccessCount: 2, LastUsedTime: 130,
		},
		{
			MinuteStart: 120, ChannelId: 21, ModelKey: "model-a", GroupKey: "standard", APIKeyKey: "all",
			ModelName: "model-a", GroupName: "standard", SampleCount: 1,
			FirstTokenSampleCount: 1, FirstTokenTotalMs: 400,
			TPSSampleCount: 1, TPSTotal: 40, ActualFailureCount: 1, FinalFailureCount: 1,
			LastUsedTime: 140,
		},
	}).Error)

	performance, err := GetChannelMonitorRoutePerformanceMetrics(context.Background(), 120, 180)
	require.NoError(t, err)
	require.Len(t, performance, 1)
	assert.Equal(t, 21, performance[0].ChannelId)
	assert.Equal(t, "model-a", performance[0].ModelName)
	assert.Equal(t, 2, performance[0].GroupCount)
	assert.Equal(t, 3, performance[0].SampleCount)
	assert.Equal(t, 3, performance[0].FirstTokenSampleCount)
	require.NotNil(t, performance[0].AverageFirstTokenMs)
	assert.InDelta(t, 800.0/3.0, *performance[0].AverageFirstTokenMs, 1e-9)
	assert.Equal(t, 3, performance[0].TPSSampleCount)
	require.NotNil(t, performance[0].AverageTPS)
	assert.InDelta(t, 20, *performance[0].AverageTPS, 1e-9)
	assert.Equal(t, int64(140), performance[0].LastUsedTime)

	stability, err := GetChannelMonitorRouteStabilityMetrics(context.Background(), 120, 180)
	require.NoError(t, err)
	require.Len(t, stability, 1)
	assert.Equal(t, 2, stability[0].GroupCount)
	assert.Equal(t, int64(2), stability[0].SuccessCount)
	assert.Equal(t, int64(1), stability[0].FailureCount)
	assert.Equal(t, int64(1), stability[0].FinalFailureCount)
	assert.Equal(t, int64(3), stability[0].SampleCount)
}

func TestChannelSmartScheduleMetricLookupNormalizesParameterizedModel(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	const normalizedModel = "gemini-2.5-pro-thinking-*"
	require.NoError(t, db.Create(&ChannelMonitorMinuteMetric{
		MinuteStart: 120, ChannelId: 22, ModelKey: normalizedModel, GroupKey: "vip", APIKeyKey: "all",
		ModelName: normalizedModel, GroupName: "vip", SampleCount: 2,
		FirstTokenSampleCount: 2, FirstTokenTotalMs: 500,
		ActualSuccessCount: 1, ActualFailureCount: 1, FinalFailureCount: 1,
	}).Error)

	performance, err := GetChannelMonitorRoutePerformanceMetric(
		context.Background(), 120, 22, "gemini-2.5-pro-thinking-2048",
	)
	require.NoError(t, err)
	assert.Equal(t, normalizedModel, performance.ModelName)
	assert.Equal(t, 2, performance.SampleCount)
	require.NotNil(t, performance.AverageFirstTokenMs)
	assert.InDelta(t, 250, *performance.AverageFirstTokenMs, 1e-9)

	stability, err := GetChannelMonitorRouteStabilityMetric(
		context.Background(), 120, 22, "gemini-2.5-pro-thinking-2048",
	)
	require.NoError(t, err)
	assert.Equal(t, normalizedModel, stability.ModelName)
	assert.Equal(t, int64(2), stability.SampleCount)
	assert.Equal(t, int64(1), stability.SuccessCount)
	assert.Equal(t, int64(1), stability.FailureCount)
}
