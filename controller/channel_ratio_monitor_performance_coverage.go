package controller

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

type channelMonitorPerformanceMetricCoverageResponse struct {
	AggregationEnabled bool  `json:"aggregation_enabled"`
	AggregatedFrom     int64 `json:"aggregated_from"`
	AggregatedThrough  int64 `json:"aggregated_through"`
	WindowStart        int64 `json:"window_start"`
	WindowComplete     bool  `json:"window_complete"`
}

func channelMonitorPerformanceMetricCoverage(
	ctx context.Context,
	generatedAt int64,
	windowMinutes int,
) (channelMonitorPerformanceMetricCoverageResponse, error) {
	coverage, err := model.GetChannelMonitorAggregationCoverage(ctx)
	if err != nil {
		return channelMonitorPerformanceMetricCoverageResponse{}, err
	}
	windowEnd := generatedAt - generatedAt%60
	windowStart := windowEnd - int64(windowMinutes*60)
	if windowStart < 0 {
		windowStart = 0
	}
	return channelMonitorPerformanceMetricCoverageResponse{
		AggregationEnabled: common.LogConsumeEnabled || constant.ErrorLogEnabled,
		AggregatedFrom:     coverage.CoveredFrom,
		AggregatedThrough:  coverage.CompletedThrough,
		WindowStart:        windowStart,
		WindowComplete: coverage.CoveredFrom > 0 &&
			coverage.CoveredFrom <= windowStart && coverage.CompletedThrough >= windowEnd,
	}, nil
}
