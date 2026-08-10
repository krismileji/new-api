package model

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSmartScheduleModelSampleStateWindowedExcludesExpiredSamples(t *testing.T) {
	firstTokenMs := 300.0
	tps := 20.0
	failureDurationMs := 500.0
	raw, err := common.Marshal([]channelSmartScheduleSample{
		{Time: 100, Success: false, FailureDurationMs: &failureDurationMs},
		{Time: 180, Success: true, FirstTokenMs: &firstTokenMs, TPS: &tps},
		{Time: 200, Success: false, FailureDurationMs: &failureDurationMs},
	})
	require.NoError(t, err)
	state := ChannelSmartScheduleModelSampleState{
		ChannelId: 51, ModelName: "model-a", WindowStart: 100,
		LastTime: 200, LastSuccess: false, LastError: "上游失败",
		SampleCount: 3, SuccessCount: 1, SamplesJSON: ChannelSmartScheduleSamplesJSON(raw),
	}

	windowed := state.Windowed(150)
	assert.Equal(t, int64(180), windowed.WindowStart)
	assert.Equal(t, int64(200), windowed.LastTime)
	assert.False(t, windowed.LastSuccess)
	assert.Equal(t, "上游失败", windowed.LastError)
	assert.Equal(t, int64(2), windowed.SampleCount)
	assert.Equal(t, int64(1), windowed.SuccessCount)
	assert.Equal(t, int64(1), windowed.FailureDurationSampleCount)
	require.NotNil(t, windowed.AverageFailureDurationMs)
	assert.InDelta(t, 500, *windowed.AverageFailureDurationMs, 1e-9)
	assert.Equal(t, int64(1), windowed.FirstTokenSampleCount)
	require.NotNil(t, windowed.AverageFirstTokenMs)
	assert.InDelta(t, 300, *windowed.AverageFirstTokenMs, 1e-9)
	assert.Equal(t, int64(1), windowed.TPSSampleCount)
	require.NotNil(t, windowed.AverageTPS)
	assert.InDelta(t, 20, *windowed.AverageTPS, 1e-9)

	expired := state.Windowed(250)
	assert.Zero(t, expired.WindowStart)
	assert.Zero(t, expired.LastTime)
	assert.False(t, expired.LastSuccess)
	assert.Empty(t, expired.LastError)
	assert.Zero(t, expired.SampleCount)
	assert.Zero(t, expired.SuccessCount)
	assert.Nil(t, expired.AverageFailureDurationMs)
	assert.Nil(t, expired.AverageFirstTokenMs)
	assert.Nil(t, expired.AverageTPS)
}

func TestChannelSmartScheduleModelSampleStateSampleSeriesReportsCorruptJSON(t *testing.T) {
	state := ChannelSmartScheduleModelSampleState{
		ChannelId:   52,
		ModelName:   "model-b",
		SamplesJSON: ChannelSmartScheduleSamplesJSON("invalid-json"),
	}

	_, err := state.SampleSeries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "渠道 52 模型 model-b")
}

func TestChannelSmartScheduleSampleSeriesManualMetricsRespectObservationBoundary(t *testing.T) {
	raw, err := common.Marshal([]channelSmartScheduleSample{
		{Time: 100, Success: false, Source: ChannelSmartScheduleSampleSourceManualTest},
		{Time: 160, Success: true, Source: ChannelSmartScheduleSampleSourceManualTest},
		{Time: 170, Success: true, Source: ChannelSmartScheduleSampleSourceScheduledProbe},
	})
	require.NoError(t, err)
	state := ChannelSmartScheduleModelSampleState{
		ChannelId: 53, ModelName: "model-c", ObservationSince: 150,
		SamplesJSON: ChannelSmartScheduleSamplesJSON(raw),
	}

	series, err := state.SampleSeries()
	require.NoError(t, err)
	metrics := series.ManualTestMetricsSince(0)
	assert.Equal(t, int64(1), metrics.SampleCount)
	assert.Equal(t, int64(1), metrics.SuccessCount)
	assert.Zero(t, metrics.FailureCount)
	assert.Equal(t, int64(160), metrics.WindowStart)
}

func TestChannelSmartScheduleSampleSeriesAdaptiveHealthUsesWindowRequestClasses(t *testing.T) {
	fastFirstTokenMs := 4_000.0
	warningFirstTokenMs := 5_000.0
	criticalFirstTokenMs := 10_000.0
	invalidFirstTokenMs := math.NaN()
	tpsWithoutFirstToken := 20.0
	series := ChannelSmartScheduleSampleSeries{
		observationSince: 150,
		samples: []channelSmartScheduleSample{
			{Time: 140, Success: false},
			{Time: 170, Success: false},
			{Time: 180, Success: true, TPS: &tpsWithoutFirstToken},
			{Time: 190, Success: true, FirstTokenMs: &fastFirstTokenMs},
			{Time: 200, Success: true, FirstTokenMs: &warningFirstTokenMs},
			{Time: 210, Success: true, FirstTokenMs: &criticalFirstTokenMs},
			{Time: 220, Success: true, FirstTokenMs: &invalidFirstTokenMs},
		},
	}

	metric := series.AdaptiveHealthMetricsSince(160, 5, 10)

	assert.Equal(t, int64(6), metric.RequestCount)
	assert.Equal(t, int64(1), metric.FailureCount)
	assert.Equal(t, int64(2), metric.SlowRequestCount)
	assert.Equal(t, int64(3), metric.HealthyRequestCount)
	assert.Equal(t, int64(3), metric.FirstTokenCount)
	assert.InDelta(t, 19_000, metric.FirstTokenTotalMs, 1e-9)
	assert.Equal(t, int64(1), metric.TPSSampleCount)
	assert.InDelta(t, 20, metric.TPSTotal, 1e-9)
	assert.InDelta(t, 1, metric.LatencyPressure, 1e-9)
	assert.Equal(t, int64(220), metric.LastUsedTime)
}

func TestChannelSmartScheduleSampleSeriesAdaptiveHealthCapsToNewestRequests(t *testing.T) {
	series := ChannelSmartScheduleSampleSeries{
		samples: []channelSmartScheduleSample{
			{Time: 100, Success: false},
			{Time: 200, Success: true},
			{Time: 300, Success: true},
		},
	}

	metric := series.AdaptiveHealthMetricsSinceWithMaxRequests(0, 2, 5, 10)

	assert.Equal(t, int64(2), metric.RequestCount)
	assert.Zero(t, metric.FailureCount)
	assert.Equal(t, int64(2), metric.HealthyRequestCount)
	assert.Equal(t, int64(300), metric.LastUsedTime)

	empty := series.AdaptiveHealthMetricsSinceWithMaxRequests(0, 0, 5, 10)
	assert.Zero(t, empty.RequestCount)
}
