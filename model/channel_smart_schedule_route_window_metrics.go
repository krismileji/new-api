package model

import (
	"context"
	"sort"

	"github.com/QuantumNous/new-api/common"
)

// ChannelMonitorRouteMetricWindow identifies one channel/model metric window.
// It is used to batch dynamic probing windows without issuing one query per
// smart-schedule route.
type ChannelMonitorRouteMetricWindow struct {
	ChannelId      int
	ModelName      string
	StartTimestamp int64
}

type ChannelMonitorRouteWindowMetrics struct {
	Window      ChannelMonitorRouteMetricWindow
	Performance ChannelMonitorRoutePerformanceMetric
	Stability   ChannelMonitorRouteStabilityMetric
}

type channelMonitorRouteMetricWindowKey struct {
	channelId      int
	modelName      string
	startTimestamp int64
}

type channelMonitorRouteMetricPairKey struct {
	channelId int
	modelName string
}

type channelMonitorRouteMinuteWindowRow struct {
	MinuteStart                 int64
	ChannelId                   int
	ModelName                   string
	GroupName                   string
	ActualSuccessCount          int64
	ActualFailureCount          int64
	FinalFailureCount           int64
	RateLimitActualFailureCount int64
	RateLimitFinalFailureCount  int64
	RetryFailureCount           int64
	RetryFailureDurationTotalMs int64
	RetryFailureUnder1sCount    int64 `gorm:"column:retry_failure_under_1s_count"`
	RetryFailure1To3sCount      int64 `gorm:"column:retry_failure_1_to_3s_count"`
	RetryFailure3To10sCount     int64 `gorm:"column:retry_failure_3_to_10s_count"`
	RetryFailure10To30sCount    int64 `gorm:"column:retry_failure_10_to_30s_count"`
	RetryFailure30To60sCount    int64 `gorm:"column:retry_failure_30_to_60s_count"`
	RetryFailureOver60sCount    int64 `gorm:"column:retry_failure_over_60s_count"`
	SampleCount                 int64
	FirstTokenSampleCount       int64
	FirstTokenTotalMs           float64
	TPSSampleCount              int64
	TPSTotal                    float64
	LastUsedTime                int64
}

type channelMonitorRouteMinuteBucketWindowRow struct {
	MinuteStart int64
	ChannelId   int
	ModelName   string
	BucketIndex int
	Count       int64
	TotalMs     float64
}

func GetChannelMonitorRouteMetricsForWindows(
	ctx context.Context,
	windows []ChannelMonitorRouteMetricWindow,
	endTimestamp int64,
	includePerformance bool,
	includeStability bool,
) ([]ChannelMonitorRouteWindowMetrics, error) {
	if len(windows) == 0 || (!includePerformance && !includeStability) {
		return []ChannelMonitorRouteWindowMetrics{}, nil
	}
	if endTimestamp <= 0 {
		endTimestamp = common.GetTimestamp()
	}
	endTimestamp = channelMonitorMinuteStart(endTimestamp)

	windowByKey := make(map[channelMonitorRouteMetricWindowKey]ChannelMonitorRouteMetricWindow, len(windows))
	requestedPairs := make(map[channelMonitorRouteMetricPairKey]struct{}, len(windows))
	channelIds := make(map[int]struct{}, len(windows))
	modelNames := make(map[string]struct{}, len(windows))
	minimumStart := int64(0)
	for _, window := range windows {
		window.ModelName = channelSmartScheduleModelName(window.ModelName)
		window.StartTimestamp = channelMonitorMinuteStart(window.StartTimestamp)
		if window.ChannelId <= 0 || window.ModelName == "" {
			continue
		}
		key := channelMonitorRouteMetricWindowKey{
			channelId:      window.ChannelId,
			modelName:      window.ModelName,
			startTimestamp: window.StartTimestamp,
		}
		windowByKey[key] = window
		if window.StartTimestamp >= endTimestamp {
			continue
		}
		pairKey := channelMonitorRouteMetricPairKey{channelId: window.ChannelId, modelName: window.ModelName}
		requestedPairs[pairKey] = struct{}{}
		channelIds[window.ChannelId] = struct{}{}
		modelNames[window.ModelName] = struct{}{}
		if minimumStart == 0 || window.StartTimestamp < minimumStart {
			minimumStart = window.StartTimestamp
		}
	}
	if len(windowByKey) == 0 {
		return []ChannelMonitorRouteWindowMetrics{}, nil
	}

	minuteRowsByPair := make(map[channelMonitorRouteMetricPairKey][]channelMonitorRouteMinuteWindowRow)
	bucketRowsByPair := make(map[channelMonitorRouteMetricPairKey][]channelMonitorRouteMinuteBucketWindowRow)
	if minimumStart < endTimestamp && len(requestedPairs) > 0 {
		channelIdList := make([]int, 0, len(channelIds))
		for channelId := range channelIds {
			channelIdList = append(channelIdList, channelId)
		}
		sort.Ints(channelIdList)
		modelNameList := make([]string, 0, len(modelNames))
		for modelName := range modelNames {
			modelNameList = append(modelNameList, modelName)
		}
		sort.Strings(modelNameList)

		metricTable := channelMonitorMinuteMetricTable
		var minuteRows []channelMonitorRouteMinuteWindowRow
		query := DB.WithContext(ctx).
			Model(&ChannelMonitorMinuteMetric{}).
			Select(
				metricTable+".minute_start AS minute_start, "+
					metricTable+".channel_id AS channel_id, "+
					metricTable+".model_name AS model_name, "+
					metricTable+".group_name AS group_name, "+
					"SUM("+metricTable+".actual_success_count) AS actual_success_count, "+
					"SUM("+metricTable+".actual_failure_count) AS actual_failure_count, "+
					"SUM("+metricTable+".final_failure_count) AS final_failure_count, "+
					"SUM("+metricTable+".rate_limit_actual_failure_count) AS rate_limit_actual_failure_count, "+
					"SUM("+metricTable+".rate_limit_final_failure_count) AS rate_limit_final_failure_count, "+
					"SUM("+metricTable+".retry_failure_count) AS retry_failure_count, "+
					"SUM("+metricTable+".retry_failure_duration_total_ms) AS retry_failure_duration_total_ms, "+
					"SUM("+metricTable+".retry_failure_under_1s_count) AS retry_failure_under_1s_count, "+
					"SUM("+metricTable+".retry_failure_1_to_3s_count) AS retry_failure_1_to_3s_count, "+
					"SUM("+metricTable+".retry_failure_3_to_10s_count) AS retry_failure_3_to_10s_count, "+
					"SUM("+metricTable+".retry_failure_10_to_30s_count) AS retry_failure_10_to_30s_count, "+
					"SUM("+metricTable+".retry_failure_30_to_60s_count) AS retry_failure_30_to_60s_count, "+
					"SUM("+metricTable+".retry_failure_over_60s_count) AS retry_failure_over_60s_count, "+
					"SUM("+metricTable+".sample_count) AS sample_count, "+
					"SUM("+metricTable+".first_token_sample_count) AS first_token_sample_count, "+
					"SUM("+metricTable+".first_token_total_ms) AS first_token_total_ms, "+
					"SUM("+metricTable+".tps_sample_count) AS tps_sample_count, "+
					"SUM("+metricTable+".tps_total) AS tps_total, "+
					"MAX("+metricTable+".last_used_time) AS last_used_time",
			).
			Where(metricTable+".minute_start >= ? AND "+metricTable+".minute_start < ?", minimumStart, endTimestamp).
			Where(metricTable+".channel_id IN ?", channelIdList).
			Where(metricTable+".model_name IN ?", modelNameList)
		query = applyChannelMonitorObservationBoundary(query, metricTable)
		if err := query.
			Group(metricTable + ".minute_start, " + metricTable + ".channel_id, " + metricTable + ".model_name, " + metricTable + ".group_name").
			Scan(&minuteRows).Error; err != nil {
			return nil, err
		}
		for _, row := range minuteRows {
			pairKey := channelMonitorRouteMetricPairKey{channelId: row.ChannelId, modelName: row.ModelName}
			if _, requested := requestedPairs[pairKey]; !requested {
				continue
			}
			minuteRowsByPair[pairKey] = append(minuteRowsByPair[pairKey], row)
		}

		if includePerformance && DB.Migrator().HasTable(&ChannelMonitorMinuteDurationBucket{}) {
			bucketTable := channelMonitorMinuteDurationBucketTable
			var bucketRows []channelMonitorRouteMinuteBucketWindowRow
			bucketQuery := DB.WithContext(ctx).
				Model(&ChannelMonitorMinuteDurationBucket{}).
				Select(
					bucketTable+".minute_start AS minute_start, "+
						bucketTable+".channel_id AS channel_id, "+
						bucketTable+".model_name AS model_name, "+
						bucketTable+".bucket_index AS bucket_index, "+
						"SUM("+bucketTable+".count) AS count, "+
						"SUM("+bucketTable+".total_ms) AS total_ms",
				).
				Where(bucketTable+".minute_start >= ? AND "+bucketTable+".minute_start < ?", minimumStart, endTimestamp).
				Where(bucketTable+".channel_id IN ?", channelIdList).
				Where(bucketTable+".model_name IN ?", modelNameList)
			bucketQuery = applyChannelMonitorObservationBoundary(bucketQuery, bucketTable)
			if err := bucketQuery.
				Group(bucketTable + ".minute_start, " + bucketTable + ".channel_id, " + bucketTable + ".model_name, " + bucketTable + ".bucket_index").
				Scan(&bucketRows).Error; err != nil {
				return nil, err
			}
			for _, row := range bucketRows {
				pairKey := channelMonitorRouteMetricPairKey{channelId: row.ChannelId, modelName: row.ModelName}
				if _, requested := requestedPairs[pairKey]; !requested {
					continue
				}
				bucketRowsByPair[pairKey] = append(bucketRowsByPair[pairKey], row)
			}
		}
	}

	results := make([]ChannelMonitorRouteWindowMetrics, 0, len(windowByKey))
	for key, window := range windowByKey {
		pairKey := channelMonitorRouteMetricPairKey{channelId: key.channelId, modelName: key.modelName}
		result := ChannelMonitorRouteWindowMetrics{
			Window: window,
			Performance: ChannelMonitorRoutePerformanceMetric{
				ChannelId:                 window.ChannelId,
				ModelName:                 window.ModelName,
				FirstTokenDurationBuckets: []ChannelMonitorDurationBucket{},
			},
			Stability: ChannelMonitorRouteStabilityMetric{
				ChannelId:                   window.ChannelId,
				ModelName:                   window.ModelName,
				RetryFailureDurationBuckets: []ChannelMonitorFailureDurationBucket{},
			},
		}
		performanceGroups := make(map[string]struct{})
		stabilityGroups := make(map[string]struct{})
		stabilityAggregate := channelMonitorRouteStabilityAggregate{
			ChannelId: window.ChannelId,
			ModelName: window.ModelName,
		}
		var performanceSampleCount int64
		var firstTokenSampleCount int64
		var firstTokenTotalMs float64
		var tpsSampleCount int64
		var tpsTotal float64
		for _, row := range minuteRowsByPair[pairKey] {
			if row.MinuteStart < key.startTimestamp {
				continue
			}
			if includePerformance && row.SampleCount > 0 {
				performanceGroups[row.GroupName] = struct{}{}
				performanceSampleCount += row.SampleCount
				firstTokenSampleCount += row.FirstTokenSampleCount
				firstTokenTotalMs += row.FirstTokenTotalMs
				tpsSampleCount += row.TPSSampleCount
				tpsTotal += row.TPSTotal
				result.Performance.LastUsedTime = max(result.Performance.LastUsedTime, row.LastUsedTime)
			}
			if includeStability {
				stabilityGroups[row.GroupName] = struct{}{}
				stabilityAggregate.ActualSuccessCount += row.ActualSuccessCount
				stabilityAggregate.ActualFailureCount += row.ActualFailureCount
				stabilityAggregate.FinalFailureCount += row.FinalFailureCount
				stabilityAggregate.RateLimitActualFailureCount += row.RateLimitActualFailureCount
				stabilityAggregate.RateLimitFinalFailureCount += row.RateLimitFinalFailureCount
				stabilityAggregate.RetryFailureCount += row.RetryFailureCount
				stabilityAggregate.RetryFailureDurationTotalMs += row.RetryFailureDurationTotalMs
				stabilityAggregate.RetryFailureUnder1sCount += row.RetryFailureUnder1sCount
				stabilityAggregate.RetryFailure1To3sCount += row.RetryFailure1To3sCount
				stabilityAggregate.RetryFailure3To10sCount += row.RetryFailure3To10sCount
				stabilityAggregate.RetryFailure10To30sCount += row.RetryFailure10To30sCount
				stabilityAggregate.RetryFailure30To60sCount += row.RetryFailure30To60sCount
				stabilityAggregate.RetryFailureOver60sCount += row.RetryFailureOver60sCount
			}
		}
		if includePerformance {
			result.Performance.GroupCount = len(performanceGroups)
			result.Performance.SampleCount = int(performanceSampleCount)
			result.Performance.FirstTokenSampleCount = int(firstTokenSampleCount)
			result.Performance.TPSSampleCount = int(tpsSampleCount)
			if firstTokenSampleCount > 0 {
				value := firstTokenTotalMs / float64(firstTokenSampleCount)
				result.Performance.AverageFirstTokenMs = &value
			}
			if tpsSampleCount > 0 {
				value := tpsTotal / float64(tpsSampleCount)
				result.Performance.AverageTPS = &value
			}
			bucketsByIndex := make(map[int]ChannelMonitorDurationBucket)
			for _, row := range bucketRowsByPair[pairKey] {
				if row.MinuteStart < key.startTimestamp || row.Count <= 0 {
					continue
				}
				lowerBoundMs, upperBoundMs, valid := channelMonitorDurationBucketBounds(row.BucketIndex)
				if !valid {
					continue
				}
				bucket := bucketsByIndex[row.BucketIndex]
				bucket.LowerBoundMs = lowerBoundMs
				bucket.UpperBoundMs = upperBoundMs
				bucket.Count += row.Count
				bucket.TotalMs += row.TotalMs
				bucketsByIndex[row.BucketIndex] = bucket
			}
			result.Performance.FirstTokenDurationBuckets = channelMonitorDurationBucketsFromAggregates(bucketsByIndex)
			result.Performance.FirstTokenDurationSampleCount,
				result.Performance.FirstTokenP50Ms,
				result.Performance.FirstTokenP95Ms,
				result.Performance.WinsorizedAverageFirstTokenMs = SummarizeChannelMonitorDurationBuckets(
				result.Performance.FirstTokenDurationBuckets,
			)
		}
		if includeStability {
			stabilityAggregate.GroupCount = len(stabilityGroups)
			result.Stability = channelMonitorRouteStabilityMetric(stabilityAggregate)
		}
		results = append(results, result)
	}
	sort.Slice(results, func(i int, j int) bool {
		if results[i].Window.ModelName != results[j].Window.ModelName {
			return results[i].Window.ModelName < results[j].Window.ModelName
		}
		if results[i].Window.ChannelId != results[j].Window.ChannelId {
			return results[i].Window.ChannelId < results[j].Window.ChannelId
		}
		return results[i].Window.StartTimestamp < results[j].Window.StartTimestamp
	})
	return results, nil
}
