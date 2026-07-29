package model

import (
	"context"
	"math"
	"sort"
)

// ChannelMonitorMinuteDurationBucket keeps a compact first-token latency
// distribution for one channel/group/model route and minute. Only non-empty
// buckets are persisted.
type ChannelMonitorMinuteDurationBucket struct {
	Id          int64   `gorm:"primaryKey"`
	MinuteStart int64   `gorm:"not null;uniqueIndex:idx_channel_monitor_minute_duration_dimensions;index:idx_channel_monitor_minute_duration_start"`
	ChannelId   int     `gorm:"not null;uniqueIndex:idx_channel_monitor_minute_duration_dimensions;index:idx_channel_monitor_minute_duration_channel"`
	ModelKey    string  `gorm:"size:32;not null;uniqueIndex:idx_channel_monitor_minute_duration_dimensions"`
	GroupKey    string  `gorm:"size:32;not null;uniqueIndex:idx_channel_monitor_minute_duration_dimensions"`
	BucketIndex int     `gorm:"not null;uniqueIndex:idx_channel_monitor_minute_duration_dimensions"`
	ModelName   string  `gorm:"size:255;not null"`
	GroupName   string  `gorm:"size:255;not null"`
	Count       int64   `gorm:"not null"`
	TotalMs     float64 `gorm:"not null"`
}

type ChannelMonitorDurationBucket struct {
	LowerBoundMs int64   `json:"lower_bound_ms"`
	UpperBoundMs int64   `json:"upper_bound_ms"`
	Count        int64   `json:"count"`
	TotalMs      float64 `json:"total_ms"`
}

type channelMonitorMinuteDurationBucketKey struct {
	MinuteStart int64
	ChannelId   int
	ModelKey    string
	GroupKey    string
	BucketIndex int
}

// Fine-grained buckets cover ordinary first-token latency, then widen for
// long-running requests. The final bucket is open-ended.
var channelMonitorDurationUpperBoundsMs = [...]int64{
	25, 50, 75, 100, 125, 150, 175, 200,
	250, 300, 350, 400, 500, 600, 750, 1_000,
	1_250, 1_500, 2_000, 2_500, 3_000, 4_000, 5_000, 7_500, 10_000,
	15_000, 20_000, 30_000, 45_000, 60_000,
	90_000, 120_000, 180_000, 300_000, 600_000,
	900_000, 1_800_000, 3_600_000,
}

func channelMonitorDurationBucketIndex(durationMs float64) int {
	return sort.Search(len(channelMonitorDurationUpperBoundsMs), func(index int) bool {
		return durationMs < float64(channelMonitorDurationUpperBoundsMs[index])
	})
}

func channelMonitorDurationBucketBounds(index int) (int64, int64, bool) {
	if index < 0 || index > len(channelMonitorDurationUpperBoundsMs) {
		return 0, 0, false
	}
	lowerBoundMs := int64(0)
	if index > 0 {
		lowerBoundMs = channelMonitorDurationUpperBoundsMs[index-1]
	}
	if index == len(channelMonitorDurationUpperBoundsMs) {
		return lowerBoundMs, 0, true
	}
	return lowerBoundMs, channelMonitorDurationUpperBoundsMs[index], true
}

func channelMonitorDurationBucketsFromAggregates(
	aggregates map[int]ChannelMonitorDurationBucket,
) []ChannelMonitorDurationBucket {
	buckets := make([]ChannelMonitorDurationBucket, 0, len(aggregates))
	for index, aggregate := range aggregates {
		lowerBoundMs, upperBoundMs, valid := channelMonitorDurationBucketBounds(index)
		if !valid || aggregate.Count <= 0 {
			continue
		}
		buckets = append(buckets, ChannelMonitorDurationBucket{
			LowerBoundMs: lowerBoundMs,
			UpperBoundMs: upperBoundMs,
			Count:        aggregate.Count,
			TotalMs:      aggregate.TotalMs,
		})
	}
	sort.Slice(buckets, func(i int, j int) bool {
		return buckets[i].LowerBoundMs < buckets[j].LowerBoundMs
	})
	return buckets
}

func channelMonitorDurationPercentileRank(sampleCount int64, numerator int64, denominator int64) int64 {
	quotient := sampleCount / denominator
	remainder := sampleCount % denominator
	return quotient*numerator + (remainder*numerator+denominator-1)/denominator
}

// SummarizeChannelMonitorDurationBuckets merges equal buckets, uses the upper
// edge as a conservative percentile estimate, and caps only samples above the
// P95 bucket when calculating the winsorized average.
func SummarizeChannelMonitorDurationBuckets(
	buckets []ChannelMonitorDurationBucket,
) (sampleCount int64, p50 *float64, p95 *float64, winsorizedAverage *float64) {
	type bucketKey struct {
		lowerBoundMs int64
		upperBoundMs int64
	}
	merged := make(map[bucketKey]ChannelMonitorDurationBucket, len(buckets))
	for _, bucket := range buckets {
		if bucket.Count <= 0 || bucket.LowerBoundMs < 0 ||
			(bucket.UpperBoundMs > 0 && bucket.UpperBoundMs <= bucket.LowerBoundMs) ||
			bucket.TotalMs < 0 || math.IsNaN(bucket.TotalMs) || math.IsInf(bucket.TotalMs, 0) {
			continue
		}
		key := bucketKey{lowerBoundMs: bucket.LowerBoundMs, upperBoundMs: bucket.UpperBoundMs}
		item := merged[key]
		if math.MaxInt64-item.Count < bucket.Count {
			item.Count = math.MaxInt64
		} else {
			item.Count += bucket.Count
		}
		item.LowerBoundMs = bucket.LowerBoundMs
		item.UpperBoundMs = bucket.UpperBoundMs
		item.TotalMs += bucket.TotalMs
		merged[key] = item
	}
	ordered := make([]ChannelMonitorDurationBucket, 0, len(merged))
	for _, bucket := range merged {
		ordered = append(ordered, bucket)
		if math.MaxInt64-sampleCount < bucket.Count {
			sampleCount = math.MaxInt64
		} else {
			sampleCount += bucket.Count
		}
	}
	if sampleCount <= 0 {
		return 0, nil, nil, nil
	}
	sort.Slice(ordered, func(i int, j int) bool {
		if ordered[i].LowerBoundMs != ordered[j].LowerBoundMs {
			return ordered[i].LowerBoundMs < ordered[j].LowerBoundMs
		}
		if ordered[i].UpperBoundMs == 0 {
			return false
		}
		if ordered[j].UpperBoundMs == 0 {
			return true
		}
		return ordered[i].UpperBoundMs < ordered[j].UpperBoundMs
	})

	p50Rank := channelMonitorDurationPercentileRank(sampleCount, 1, 2)
	p95Rank := channelMonitorDurationPercentileRank(sampleCount, 19, 20)
	cumulativeCount := int64(0)
	p95BucketIndex := -1
	for index, bucket := range ordered {
		if math.MaxInt64-cumulativeCount < bucket.Count {
			cumulativeCount = math.MaxInt64
		} else {
			cumulativeCount += bucket.Count
		}
		conservativeUpperBound := float64(bucket.UpperBoundMs)
		if bucket.UpperBoundMs == 0 {
			// With non-negative samples, their sum is an upper bound for every
			// individual value in the open-ended bucket.
			conservativeUpperBound = math.Max(float64(bucket.LowerBoundMs), bucket.TotalMs)
		}
		if p50 == nil && cumulativeCount >= p50Rank {
			value := conservativeUpperBound
			p50 = &value
		}
		if cumulativeCount >= p95Rank {
			value := conservativeUpperBound
			p95 = &value
			p95BucketIndex = index
			break
		}
	}
	if p95 == nil || p95BucketIndex < 0 {
		return sampleCount, p50, nil, nil
	}

	cappedTotalMs := float64(0)
	for index, bucket := range ordered {
		if index <= p95BucketIndex {
			cappedTotalMs += bucket.TotalMs
			continue
		}
		cappedTotalMs += float64(bucket.Count) * *p95
	}
	value := cappedTotalMs / float64(sampleCount)
	winsorizedAverage = &value
	return sampleCount, p50, p95, winsorizedAverage
}

type channelMonitorDurationAggregateRow struct {
	ChannelId   int
	GroupName   string
	ModelName   string
	BucketIndex int
	Count       int64
	TotalMs     float64
}

func getChannelMonitorRouteDurationBuckets(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
	filter ChannelMonitorSuccessFilter,
) (map[channelMonitorRouteMetricKey][]ChannelMonitorDurationBucket, error) {
	if !DB.Migrator().HasTable(&ChannelMonitorMinuteDurationBucket{}) {
		return map[channelMonitorRouteMetricKey][]ChannelMonitorDurationBucket{}, nil
	}
	query := DB.WithContext(ctx).
		Model(&ChannelMonitorMinuteDurationBucket{}).
		Select(
			"channel_id, group_name, model_name, bucket_index, "+
				"SUM(count) AS count, SUM(total_ms) AS total_ms",
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
	var rows []channelMonitorDurationAggregateRow
	err := query.
		Group("channel_id, group_name, model_name, bucket_index").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	aggregates := make(map[channelMonitorRouteMetricKey]map[int]ChannelMonitorDurationBucket)
	for _, row := range rows {
		lowerBoundMs, upperBoundMs, valid := channelMonitorDurationBucketBounds(row.BucketIndex)
		if !valid || row.Count <= 0 {
			continue
		}
		key := channelMonitorRouteMetricKey{
			channelId: row.ChannelId,
			groupName: row.GroupName,
			modelName: row.ModelName,
		}
		byIndex := aggregates[key]
		if byIndex == nil {
			byIndex = make(map[int]ChannelMonitorDurationBucket)
			aggregates[key] = byIndex
		}
		bucket := byIndex[row.BucketIndex]
		bucket.LowerBoundMs = lowerBoundMs
		bucket.UpperBoundMs = upperBoundMs
		bucket.Count += row.Count
		bucket.TotalMs += row.TotalMs
		byIndex[row.BucketIndex] = bucket
	}
	result := make(map[channelMonitorRouteMetricKey][]ChannelMonitorDurationBucket, len(aggregates))
	for key, byIndex := range aggregates {
		result[key] = channelMonitorDurationBucketsFromAggregates(byIndex)
	}
	return result, nil
}
