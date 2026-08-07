package controller

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSmartScheduleMetricCoverageReportsEachWindow(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	common.LogConsumeEnabled = true
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})

	_, err := model.AggregateChannelMonitorMinuteRangeWithState(
		context.Background(), 360, 600, true,
	)
	require.NoError(t, err)

	coverage, err := channelSmartScheduleMetricCoverage(
		context.Background(),
		600,
		channelMonitorSettings{
			CostRetentionDays:                     1,
			SmartSchedulePerformanceWindowMinutes: 5,
			SmartScheduleStabilityWindowMinutes:   2,
		},
	)
	require.NoError(t, err)
	assert.True(t, coverage.AggregationEnabled)
	assert.Equal(t, int64(360), coverage.AggregatedFrom)
	assert.Equal(t, int64(600), coverage.AggregatedThrough)
	assert.Equal(t, int64(300), coverage.PerformanceWindowStart)
	assert.Equal(t, int64(480), coverage.StabilityWindowStart)
	assert.False(t, coverage.PerformanceWindowComplete)
	assert.True(t, coverage.StabilityWindowComplete)
	assert.Equal(t, 5, coverage.RequiredRetentionMinutes)
	assert.True(t, coverage.ConfiguredRetentionSufficient)
}

func TestChannelSmartScheduleMetricCoverageFlagsShortConfiguredRetention(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)

	coverage, err := channelSmartScheduleMetricCoverage(
		context.Background(),
		200_000,
		channelMonitorSettings{
			CostRetentionDays:                     1,
			SmartSchedulePerformanceWindowMinutes: 2 * 24 * 60,
			SmartScheduleStabilityWindowMinutes:   60,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, 2*24*60, coverage.RequiredRetentionMinutes)
	assert.False(t, coverage.ConfiguredRetentionSufficient)
}
