package model

import (
	"context"
	"sort"
	"strings"
)

type ChannelMonitorRoutePerformanceMetric struct {
	ChannelId             int      `json:"channel_id"`
	GroupName             string   `json:"group"`
	ModelName             string   `json:"model"`
	SampleCount           int      `json:"sample_count"`
	FirstTokenSampleCount int      `json:"first_token_sample_count"`
	TPSSampleCount        int      `json:"tps_sample_count"`
	AverageFirstTokenMs   *float64 `json:"average_first_token_ms"`
	AverageTPS            *float64 `json:"average_tps"`
	LastUsedTime          int64    `json:"last_used_time"`
}

type ChannelMonitorRouteStabilityMetric struct {
	ChannelId    int     `json:"channel_id"`
	GroupName    string  `json:"group"`
	ModelName    string  `json:"model"`
	SuccessCount int64   `json:"success_count"`
	FailureCount int64   `json:"failure_count"`
	SampleCount  int64   `json:"sample_count"`
	SuccessRate  float64 `json:"success_rate"`
}

type channelMonitorRouteMetricKey struct {
	channelId int
	groupName string
	modelName string
}

func GetChannelMonitorRoutePerformanceMetrics(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelMonitorRoutePerformanceMetric, error) {
	startTimestamp, endTimestamp = channelMonitorMinuteRange(startTimestamp, endTimestamp)
	if startTimestamp >= endTimestamp {
		return []ChannelMonitorRoutePerformanceMetric{}, nil
	}
	type performanceAggregate struct {
		ChannelId             int
		GroupName             string
		ModelName             string
		SampleCount           int64
		FirstTokenSampleCount int64
		TPSSampleCount        int64
		FirstTokenTotalMs     float64
		TPSTotal              float64
		LastUsedTime          int64
	}
	var aggregates []performanceAggregate
	err := DB.WithContext(ctx).
		Model(&ChannelMonitorMinuteMetric{}).
		Select(
			"channel_id, group_name, model_name, "+
				"SUM(sample_count) AS sample_count, "+
				"SUM(first_token_sample_count) AS first_token_sample_count, "+
				"SUM(tps_sample_count) AS tps_sample_count, "+
				"SUM(first_token_total_ms) AS first_token_total_ms, "+
				"SUM(tps_total) AS tps_total, "+
				"MAX(last_used_time) AS last_used_time",
		).
		Where("minute_start >= ? AND minute_start < ?", startTimestamp, endTimestamp).
		Where("sample_count > ?", 0).
		Group("channel_id, group_name, model_name").
		Scan(&aggregates).Error
	if err != nil {
		return nil, err
	}
	result := make([]ChannelMonitorRoutePerformanceMetric, 0, len(aggregates))
	for _, aggregate := range aggregates {
		metric := ChannelMonitorRoutePerformanceMetric{
			ChannelId:             aggregate.ChannelId,
			GroupName:             aggregate.GroupName,
			ModelName:             aggregate.ModelName,
			SampleCount:           int(aggregate.SampleCount),
			FirstTokenSampleCount: int(aggregate.FirstTokenSampleCount),
			TPSSampleCount:        int(aggregate.TPSSampleCount),
			LastUsedTime:          aggregate.LastUsedTime,
		}
		if aggregate.FirstTokenSampleCount > 0 {
			value := aggregate.FirstTokenTotalMs / float64(aggregate.FirstTokenSampleCount)
			metric.AverageFirstTokenMs = &value
		}
		if aggregate.TPSSampleCount > 0 {
			value := aggregate.TPSTotal / float64(aggregate.TPSSampleCount)
			metric.AverageTPS = &value
		}
		result = append(result, metric)
	}
	sort.Slice(result, func(i int, j int) bool {
		if result[i].GroupName != result[j].GroupName {
			return result[i].GroupName < result[j].GroupName
		}
		if result[i].ModelName != result[j].ModelName {
			return result[i].ModelName < result[j].ModelName
		}
		return result[i].ChannelId < result[j].ChannelId
	})
	return result, nil
}

func GetChannelMonitorRouteStabilityMetrics(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelMonitorRouteStabilityMetric, error) {
	rows, err := getChannelMonitorMinuteSuccessRows(
		ctx,
		startTimestamp,
		endTimestamp,
		ChannelMonitorSuccessFilter{},
		false,
		false,
	)
	if err != nil {
		return nil, err
	}
	countsByRoute := make(map[channelMonitorRouteMetricKey]*channelMonitorSuccessCounts)
	for _, row := range rows {
		groupName := strings.TrimSpace(row.GroupName)
		modelName := strings.TrimSpace(row.ModelName)
		if row.ChannelId <= 0 || groupName == "" || modelName == "" {
			continue
		}
		key := channelMonitorRouteMetricKey{channelId: row.ChannelId, groupName: groupName, modelName: modelName}
		counts := countsByRoute[key]
		if counts == nil {
			counts = &channelMonitorSuccessCounts{}
			countsByRoute[key] = counts
		}
		counts.add(row.Type, row.IsRetryAttempt != nil && *row.IsRetryAttempt, row.Count, 0, 0)
	}
	metrics := make([]ChannelMonitorRouteStabilityMetric, 0, len(countsByRoute))
	for key, counts := range countsByRoute {
		summary := counts.summary()
		metrics = append(metrics, ChannelMonitorRouteStabilityMetric{
			ChannelId:    key.channelId,
			GroupName:    key.groupName,
			ModelName:    key.modelName,
			SuccessCount: summary.ActualSuccessCount,
			FailureCount: summary.ActualFailureCount,
			SampleCount:  summary.ActualSampleCount,
			SuccessRate:  summary.ActualSuccessRate,
		})
	}
	sort.Slice(metrics, func(i int, j int) bool {
		if metrics[i].GroupName != metrics[j].GroupName {
			return metrics[i].GroupName < metrics[j].GroupName
		}
		if metrics[i].ModelName != metrics[j].ModelName {
			return metrics[i].ModelName < metrics[j].ModelName
		}
		return metrics[i].ChannelId < metrics[j].ChannelId
	})
	return metrics, nil
}

func GetChannelMonitorRouteStabilityMetric(ctx context.Context, startTimestamp int64, channelId int, groupName string, modelName string) (ChannelMonitorRouteStabilityMetric, error) {
	rows, err := getChannelMonitorSuccessRows(ctx, startTimestamp, ChannelMonitorSuccessFilter{
		ChannelId: channelId,
		Group:     groupName,
		ModelName: modelName,
	}, false, false)
	if err != nil {
		return ChannelMonitorRouteStabilityMetric{}, err
	}
	counts := channelMonitorSuccessCounts{}
	for _, row := range rows {
		counts.add(row.Type, row.IsRetryAttempt != nil && *row.IsRetryAttempt, row.Count, 0, 0)
	}
	summary := counts.summary()
	return ChannelMonitorRouteStabilityMetric{
		ChannelId: channelId, GroupName: groupName, ModelName: modelName,
		SuccessCount: summary.ActualSuccessCount, FailureCount: summary.ActualFailureCount,
		SampleCount: summary.ActualSampleCount, SuccessRate: summary.ActualSuccessRate,
	}, nil
}
