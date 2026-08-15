package model

import (
	"context"
	"strings"
)

type ChannelMonitorPerformanceMetric struct {
	ChannelId             int      `json:"channel_id"`
	ModelName             string   `json:"model_name"`
	SampleCount           int      `json:"sample_count"`
	FirstTokenSampleCount int      `json:"first_token_sample_count"`
	TPSSampleCount        int      `json:"tps_sample_count"`
	AverageFirstTokenMs   *float64 `json:"average_first_token_ms"`
	AverageTPS            *float64 `json:"average_tps"`
	LatestFirstTokenMs    *float64 `json:"latest_first_token_ms"`
	LatestTPS             *float64 `json:"latest_tps"`
	LastUsedTime          int64    `json:"last_used_time"`
}

type ChannelMonitorStabilityMetric struct {
	ChannelId    int     `json:"channel_id"`
	ModelName    string  `json:"model_name"`
	SuccessCount int64   `json:"success_count"`
	FailureCount int64   `json:"failure_count"`
	SampleCount  int64   `json:"sample_count"`
	SuccessRate  float64 `json:"success_rate"`
}

// GetChannelMonitorPerformanceMetrics reads the persisted minute aggregates.
func GetChannelMonitorPerformanceMetrics(ctx context.Context, startTimestamp int64) ([]ChannelMonitorPerformanceMetric, error) {
	return getChannelMonitorMinutePerformanceMetrics(ctx, startTimestamp, 0)
}

// GetChannelMonitorStabilityMetrics measures upstream attempt stability from
// the shared channel-monitor success aggregation. Retry-attempt errors are
// included so a channel failure is still counted when a later fallback channel
// succeeds.
func GetChannelMonitorStabilityMetrics(ctx context.Context, startTimestamp int64) ([]ChannelMonitorStabilityMetric, error) {
	channelMetrics, _, err := getChannelMonitorSuccessMetrics(ctx, startTimestamp, 0, false)
	if err != nil {
		return nil, err
	}
	return channelMonitorStabilityMetricsFromSuccess(channelMetrics), nil
}

func channelMonitorStabilityMetricsFromSuccess(channelMetrics []ChannelMonitorSuccessMetric) []ChannelMonitorStabilityMetric {
	metrics := make([]ChannelMonitorStabilityMetric, 0, len(channelMetrics))
	for _, metric := range channelMetrics {
		metrics = append(metrics, ChannelMonitorStabilityMetric{
			ChannelId:    metric.ChannelId,
			ModelName:    metric.ModelName,
			SuccessCount: metric.ActualSuccessCount,
			FailureCount: metric.ActualFailureCount,
			SampleCount:  metric.ActualSampleCount,
			SuccessRate:  metric.ActualSuccessRate,
		})
	}
	return metrics
}

func GetChannelMonitorStabilityMetric(ctx context.Context, startTimestamp int64, filter ChannelMonitorSuccessFilter) (ChannelMonitorStabilityMetric, error) {
	rows, err := getChannelMonitorSuccessRows(ctx, startTimestamp, filter, false, false)
	if err != nil {
		return ChannelMonitorStabilityMetric{}, err
	}

	counts := channelMonitorSuccessCounts{}
	for _, row := range rows {
		if strings.TrimSpace(row.ModelName) == "" {
			continue
		}
		counts.add(row.Type, row.IsRetryAttempt != nil && *row.IsRetryAttempt, row.Count, 0, 0, 0, 0)
	}
	summary := counts.summary()
	return ChannelMonitorStabilityMetric{
		ChannelId:    filter.ChannelId,
		ModelName:    filter.ModelName,
		SuccessCount: summary.ActualSuccessCount,
		FailureCount: summary.ActualFailureCount,
		SampleCount:  summary.ActualSampleCount,
		SuccessRate:  summary.ActualSuccessRate,
	}, nil
}
