package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSmartScheduleMetricsMatchChannelViewCompletedMinuteWindow(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.Create(&[]Log{
		{
			ChannelId: 41, ModelName: "model-a", Group: "vip", CreatedAt: 61,
			Type: LogTypeConsume, IsStream: true, CompletionTokens: 20, UseTime: 2,
			Other: `{"frt":100}`,
		},
		{
			ChannelId: 41, ModelName: "model-a", Group: "vip", CreatedAt: 62,
			Type: LogTypeError,
		},
	}).Error)
	aggregateChannelMonitorMinuteTestRange(t, 60, 120)
	require.NoError(t, db.Create(&[]Log{
		{
			ChannelId: 41, ModelName: "model-a", Group: "vip", CreatedAt: 305,
			Type: LogTypeConsume, IsStream: true, CompletionTokens: 30, UseTime: 3,
			Other: `{"frt":300}`,
		},
		{
			ChannelId: 41, ModelName: "model-a", Group: "vip", CreatedAt: 306,
			Type: LogTypeError,
		},
	}).Error)

	ctx := context.Background()
	channelPerformance, err := GetChannelMonitorObservedPerformanceMetrics(ctx, 310, 4)
	require.NoError(t, err)
	require.Len(t, channelPerformance, 1)
	routePerformance, err := GetChannelMonitorRoutePerformanceMetrics(ctx, 60, 310)
	require.NoError(t, err)
	require.Len(t, routePerformance, 1)
	assert.Equal(t, channelPerformance[0].ChannelId, routePerformance[0].ChannelId)
	assert.Equal(t, channelPerformance[0].ModelName, routePerformance[0].ModelName)
	assert.Equal(t, channelPerformance[0].SampleCount, routePerformance[0].SampleCount)
	assert.Equal(t, channelPerformance[0].FirstTokenSampleCount, routePerformance[0].FirstTokenSampleCount)
	assert.Equal(t, channelPerformance[0].TPSSampleCount, routePerformance[0].TPSSampleCount)
	assert.Equal(t, channelPerformance[0].AverageFirstTokenMs, routePerformance[0].AverageFirstTokenMs)
	assert.Equal(t, channelPerformance[0].AverageTPS, routePerformance[0].AverageTPS)
	assert.Equal(t, channelPerformance[0].LastUsedTime, routePerformance[0].LastUsedTime)

	channelSuccess, _, err := GetChannelMonitorObservedSuccessMetrics(ctx, 310, 4)
	require.NoError(t, err)
	require.Len(t, channelSuccess, 1)
	routeStability, err := GetChannelMonitorRouteStabilityMetrics(ctx, 60, 310)
	require.NoError(t, err)
	require.Len(t, routeStability, 1)
	assert.Equal(t, channelSuccess[0].ActualSuccessCount, routeStability[0].SuccessCount)
	assert.Equal(t, channelSuccess[0].ActualFailureCount, routeStability[0].FailureCount)
	assert.Equal(t, channelSuccess[0].ActualSampleCount, routeStability[0].SampleCount)
	assert.Equal(t, channelSuccess[0].ActualSuccessRate, routeStability[0].SuccessRate)
}
