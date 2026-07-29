package model

import (
	"context"
	"sort"
	"strings"
)

type ChannelMonitorRoutePerformanceMetric struct {
	ChannelId                     int                            `json:"channel_id"`
	GroupName                     string                         `json:"group"`
	ModelName                     string                         `json:"model"`
	SampleCount                   int                            `json:"sample_count"`
	FirstTokenSampleCount         int                            `json:"first_token_sample_count"`
	FirstTokenDurationSampleCount int64                          `json:"first_token_duration_sample_count"`
	TPSSampleCount                int                            `json:"tps_sample_count"`
	AverageFirstTokenMs           *float64                       `json:"average_first_token_ms"`
	FirstTokenP50Ms               *float64                       `json:"first_token_p50_ms"`
	FirstTokenP95Ms               *float64                       `json:"first_token_p95_ms"`
	WinsorizedAverageFirstTokenMs *float64                       `json:"winsorized_average_first_token_ms"`
	FirstTokenDurationBuckets     []ChannelMonitorDurationBucket `json:"-"`
	AverageTPS                    *float64                       `json:"average_tps"`
	LastUsedTime                  int64                          `json:"last_used_time"`
}

type ChannelMonitorRouteStabilityMetric struct {
	ChannelId                     int                                   `json:"channel_id"`
	GroupName                     string                                `json:"group"`
	ModelName                     string                                `json:"model"`
	SuccessCount                  int64                                 `json:"success_count"`
	FailureCount                  int64                                 `json:"failure_count"`
	FinalFailureCount             int64                                 `json:"final_failure_count"`
	RetryFailureCount             int64                                 `json:"retry_failure_count"`
	SampleCount                   int64                                 `json:"sample_count"`
	SuccessRate                   float64                               `json:"success_rate"`
	StabilityScore                *float64                              `json:"stability_score"`
	AverageRetryFailureDurationMs float64                               `json:"average_retry_failure_duration_ms"`
	RetryFailureDurationBuckets   []ChannelMonitorFailureDurationBucket `json:"retry_failure_duration_buckets"`
	JitterAvailable               bool                                  `json:"jitter_available"`
	FirstTokenBaselineMs          *float64                              `json:"first_token_baseline_ms"`
	FirstTokenP50Ms               *float64                              `json:"first_token_p50_ms"`
	FirstTokenP95Ms               *float64                              `json:"first_token_p95_ms"`
	JitterThresholdMs             *float64                              `json:"jitter_threshold_ms"`
	JitterSampleCount             int64                                 `json:"jitter_sample_count"`
	JitterSlowCount               int64                                 `json:"jitter_slow_count"`
	JitterAllowedCount            int64                                 `json:"jitter_allowed_count"`
	JitterPenalty                 float64                               `json:"jitter_penalty"`
}

type ChannelMonitorFailureDurationBucket struct {
	LowerBoundMs int64 `json:"lower_bound_ms"`
	UpperBoundMs int64 `json:"upper_bound_ms"`
	Count        int64 `json:"count"`
}

type channelMonitorRouteMetricKey struct {
	channelId int
	groupName string
	modelName string
}

func getChannelMonitorRoutePerformanceMetrics(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
	filter ChannelMonitorSuccessFilter,
) ([]ChannelMonitorRoutePerformanceMetric, error) {
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
	query := DB.WithContext(ctx).
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
		Where("sample_count > ?", 0)
	if filter.ChannelId > 0 {
		query = query.Where("channel_id = ?", filter.ChannelId)
	}
	if filter.Group != "" {
		query = query.Where("group_name = ?", filter.Group)
	}
	if filter.ModelName != "" {
		query = query.Where("model_name = ?", filter.ModelName)
	}
	var aggregates []performanceAggregate
	err := query.
		Group("channel_id, group_name, model_name").
		Scan(&aggregates).Error
	if err != nil {
		return nil, err
	}
	durationBucketsByRoute, err := getChannelMonitorRouteDurationBuckets(
		ctx, startTimestamp, endTimestamp, filter,
	)
	if err != nil {
		return nil, err
	}
	result := make([]ChannelMonitorRoutePerformanceMetric, 0, len(aggregates))
	for _, aggregate := range aggregates {
		metric := ChannelMonitorRoutePerformanceMetric{
			ChannelId:                 aggregate.ChannelId,
			GroupName:                 aggregate.GroupName,
			ModelName:                 aggregate.ModelName,
			SampleCount:               int(aggregate.SampleCount),
			FirstTokenSampleCount:     int(aggregate.FirstTokenSampleCount),
			TPSSampleCount:            int(aggregate.TPSSampleCount),
			LastUsedTime:              aggregate.LastUsedTime,
			FirstTokenDurationBuckets: []ChannelMonitorDurationBucket{},
		}
		if aggregate.FirstTokenSampleCount > 0 {
			value := aggregate.FirstTokenTotalMs / float64(aggregate.FirstTokenSampleCount)
			metric.AverageFirstTokenMs = &value
		}
		if aggregate.TPSSampleCount > 0 {
			value := aggregate.TPSTotal / float64(aggregate.TPSSampleCount)
			metric.AverageTPS = &value
		}
		key := channelMonitorRouteMetricKey{
			channelId: aggregate.ChannelId,
			groupName: aggregate.GroupName,
			modelName: aggregate.ModelName,
		}
		if buckets := durationBucketsByRoute[key]; len(buckets) > 0 {
			metric.FirstTokenDurationBuckets = buckets
			metric.FirstTokenDurationSampleCount, metric.FirstTokenP50Ms, metric.FirstTokenP95Ms,
				metric.WinsorizedAverageFirstTokenMs = SummarizeChannelMonitorDurationBuckets(buckets)
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

func GetChannelMonitorRoutePerformanceMetrics(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelMonitorRoutePerformanceMetric, error) {
	return getChannelMonitorRoutePerformanceMetrics(
		ctx, startTimestamp, endTimestamp, ChannelMonitorSuccessFilter{},
	)
}

func GetChannelMonitorRoutePerformanceMetric(
	ctx context.Context,
	startTimestamp int64,
	channelId int,
	groupName string,
	modelName string,
) (ChannelMonitorRoutePerformanceMetric, error) {
	metrics, err := getChannelMonitorRoutePerformanceMetrics(ctx, startTimestamp, 0, ChannelMonitorSuccessFilter{
		ChannelId: channelId,
		Group:     groupName,
		ModelName: modelName,
	})
	if err != nil {
		return ChannelMonitorRoutePerformanceMetric{}, err
	}
	if len(metrics) == 0 {
		return ChannelMonitorRoutePerformanceMetric{
			ChannelId: channelId, GroupName: groupName, ModelName: modelName,
			FirstTokenDurationBuckets: []ChannelMonitorDurationBucket{},
		}, nil
	}
	return metrics[0], nil
}

type channelMonitorRouteStabilityAggregate struct {
	ChannelId                   int
	GroupName                   string
	ModelName                   string
	ActualSuccessCount          int64
	ActualFailureCount          int64
	FinalFailureCount           int64
	RetryFailureCount           int64
	RetryFailureDurationTotalMs int64
	RetryFailureUnder1sCount    int64 `gorm:"column:retry_failure_under_1s_count"`
	RetryFailure1To3sCount      int64 `gorm:"column:retry_failure_1_to_3s_count"`
	RetryFailure3To10sCount     int64 `gorm:"column:retry_failure_3_to_10s_count"`
	RetryFailure10To30sCount    int64 `gorm:"column:retry_failure_10_to_30s_count"`
	RetryFailure30To60sCount    int64 `gorm:"column:retry_failure_30_to_60s_count"`
	RetryFailureOver60sCount    int64 `gorm:"column:retry_failure_over_60s_count"`
}

func getChannelMonitorRouteStabilityAggregates(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
	filter ChannelMonitorSuccessFilter,
) ([]channelMonitorRouteStabilityAggregate, error) {
	startTimestamp, endTimestamp = channelMonitorMinuteRange(startTimestamp, endTimestamp)
	if startTimestamp >= endTimestamp {
		return []channelMonitorRouteStabilityAggregate{}, nil
	}
	query := DB.WithContext(ctx).
		Model(&ChannelMonitorMinuteMetric{}).
		Select(
			"channel_id, group_name, model_name, "+
				"SUM(actual_success_count) AS actual_success_count, "+
				"SUM(actual_failure_count) AS actual_failure_count, "+
				"SUM(final_failure_count) AS final_failure_count, "+
				"SUM(retry_failure_count) AS retry_failure_count, "+
				"SUM(retry_failure_duration_total_ms) AS retry_failure_duration_total_ms, "+
				"SUM(retry_failure_under_1s_count) AS retry_failure_under_1s_count, "+
				"SUM(retry_failure_1_to_3s_count) AS retry_failure_1_to_3s_count, "+
				"SUM(retry_failure_3_to_10s_count) AS retry_failure_3_to_10s_count, "+
				"SUM(retry_failure_10_to_30s_count) AS retry_failure_10_to_30s_count, "+
				"SUM(retry_failure_30_to_60s_count) AS retry_failure_30_to_60s_count, "+
				"SUM(retry_failure_over_60s_count) AS retry_failure_over_60s_count",
		).
		Where("minute_start >= ? AND minute_start < ?", startTimestamp, endTimestamp)
	if filter.ChannelId > 0 {
		query = query.Where("channel_id = ?", filter.ChannelId)
	}
	if filter.Group != "" {
		query = query.Where("group_name = ?", filter.Group)
	}
	if filter.ModelName != "" {
		query = query.Where("model_name = ?", filter.ModelName)
	}
	var aggregates []channelMonitorRouteStabilityAggregate
	if err := query.Group("channel_id, group_name, model_name").Scan(&aggregates).Error; err != nil {
		return nil, err
	}
	return aggregates, nil
}

func channelMonitorRouteStabilityMetric(aggregate channelMonitorRouteStabilityAggregate) ChannelMonitorRouteStabilityMetric {
	failureCount := max(max(aggregate.ActualFailureCount, aggregate.FinalFailureCount), 0)
	finalFailureCount := min(max(aggregate.FinalFailureCount, 0), failureCount)
	retryFailureLimit := failureCount - finalFailureCount
	retryFailureCount := min(max(aggregate.RetryFailureCount, 0), retryFailureLimit)
	sampleCount := max(aggregate.ActualSuccessCount, 0) + failureCount
	metric := ChannelMonitorRouteStabilityMetric{
		ChannelId:         aggregate.ChannelId,
		GroupName:         strings.TrimSpace(aggregate.GroupName),
		ModelName:         strings.TrimSpace(aggregate.ModelName),
		SuccessCount:      max(aggregate.ActualSuccessCount, 0),
		FailureCount:      failureCount,
		FinalFailureCount: finalFailureCount,
		RetryFailureCount: retryFailureCount,
		SampleCount:       sampleCount,
		RetryFailureDurationBuckets: []ChannelMonitorFailureDurationBucket{
			{LowerBoundMs: 0, UpperBoundMs: 1000, Count: max(aggregate.RetryFailureUnder1sCount, 0)},
			{LowerBoundMs: 1000, UpperBoundMs: 3000, Count: max(aggregate.RetryFailure1To3sCount, 0)},
			{LowerBoundMs: 3000, UpperBoundMs: 10000, Count: max(aggregate.RetryFailure3To10sCount, 0)},
			{LowerBoundMs: 10000, UpperBoundMs: 30000, Count: max(aggregate.RetryFailure10To30sCount, 0)},
			{LowerBoundMs: 30000, UpperBoundMs: 60000, Count: max(aggregate.RetryFailure30To60sCount, 0)},
			{LowerBoundMs: 60000, UpperBoundMs: 0, Count: max(aggregate.RetryFailureOver60sCount, 0)},
		},
	}
	if sampleCount > 0 {
		metric.SuccessRate = float64(metric.SuccessCount) / float64(sampleCount)
	}
	if retryFailureCount > 0 && aggregate.RetryFailureDurationTotalMs > 0 {
		metric.AverageRetryFailureDurationMs = float64(aggregate.RetryFailureDurationTotalMs) / float64(retryFailureCount)
	}
	return metric
}

func GetChannelMonitorRouteStabilityMetrics(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelMonitorRouteStabilityMetric, error) {
	aggregates, err := getChannelMonitorRouteStabilityAggregates(
		ctx, startTimestamp, endTimestamp, ChannelMonitorSuccessFilter{},
	)
	if err != nil {
		return nil, err
	}
	metrics := make([]ChannelMonitorRouteStabilityMetric, 0, len(aggregates))
	for _, aggregate := range aggregates {
		metric := channelMonitorRouteStabilityMetric(aggregate)
		if metric.ChannelId <= 0 || metric.GroupName == "" || metric.ModelName == "" {
			continue
		}
		metrics = append(metrics, metric)
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
	aggregates, err := getChannelMonitorRouteStabilityAggregates(ctx, startTimestamp, 0, ChannelMonitorSuccessFilter{
		ChannelId: channelId,
		Group:     groupName,
		ModelName: modelName,
	})
	if err != nil {
		return ChannelMonitorRouteStabilityMetric{}, err
	}
	if len(aggregates) == 0 {
		return ChannelMonitorRouteStabilityMetric{
			ChannelId: channelId, GroupName: groupName, ModelName: modelName,
			RetryFailureDurationBuckets: []ChannelMonitorFailureDurationBucket{},
		}, nil
	}
	return channelMonitorRouteStabilityMetric(aggregates[0]), nil
}
