package controller

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

type channelSmartScheduleMetricCoverageResponse struct {
	AggregationEnabled            bool  `json:"aggregation_enabled"`
	AggregatedFrom                int64 `json:"aggregated_from"`
	AggregatedThrough             int64 `json:"aggregated_through"`
	PerformanceWindowStart        int64 `json:"performance_window_start"`
	StabilityWindowStart          int64 `json:"stability_window_start"`
	PerformanceWindowComplete     bool  `json:"performance_window_complete"`
	StabilityWindowComplete       bool  `json:"stability_window_complete"`
	ConfiguredRetentionDays       int   `json:"configured_retention_days"`
	RequiredRetentionMinutes      int   `json:"required_retention_minutes"`
	ConfiguredRetentionSufficient bool  `json:"configured_retention_sufficient"`
}

func channelSmartScheduleMetricCoverage(
	ctx context.Context,
	generatedAt int64,
	settings channelMonitorSettings,
) (channelSmartScheduleMetricCoverageResponse, error) {
	coverage, err := model.GetChannelMonitorAggregationCoverage(ctx)
	if err != nil {
		return channelSmartScheduleMetricCoverageResponse{}, err
	}
	windowEnd := generatedAt - generatedAt%60
	performanceStart := windowEnd - int64(settings.SmartSchedulePerformanceWindowMinutes*60)
	if performanceStart < 0 {
		performanceStart = 0
	}
	stabilityStart := windowEnd - int64(settings.SmartScheduleStabilityWindowMinutes*60)
	if stabilityStart < 0 {
		stabilityStart = 0
	}
	completeThrough := coverage.CompletedThrough >= windowEnd
	performanceComplete := coverage.CoveredFrom > 0 &&
		coverage.CoveredFrom <= performanceStart && completeThrough
	stabilityComplete := coverage.CoveredFrom > 0 &&
		coverage.CoveredFrom <= stabilityStart && completeThrough
	requiredRetentionMinutes := max(
		settings.SmartSchedulePerformanceWindowMinutes,
		settings.SmartScheduleStabilityWindowMinutes,
	)
	return channelSmartScheduleMetricCoverageResponse{
		AggregationEnabled:            common.LogConsumeEnabled || constant.ErrorLogEnabled,
		AggregatedFrom:                coverage.CoveredFrom,
		AggregatedThrough:             coverage.CompletedThrough,
		PerformanceWindowStart:        performanceStart,
		StabilityWindowStart:          stabilityStart,
		PerformanceWindowComplete:     performanceComplete,
		StabilityWindowComplete:       stabilityComplete,
		ConfiguredRetentionDays:       settings.CostRetentionDays,
		RequiredRetentionMinutes:      requiredRetentionMinutes,
		ConfiguredRetentionSufficient: settings.CostRetentionDays*24*60 >= requiredRetentionMinutes,
	}, nil
}
