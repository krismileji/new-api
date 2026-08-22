package model

import (
	"context"
	"errors"
)

type ChannelMonitorCostRetentionResult struct {
	ChannelRowsDeleted        int64 `json:"channel_rows_deleted"`
	APIKeyRowsDeleted         int64 `json:"api_key_rows_deleted"`
	TaskCostEventRowsDeleted  int64 `json:"task_cost_event_rows_deleted"`
	RouteMetricRowsDeleted    int64 `json:"route_metric_rows_deleted"`
	APIKeyMetricRowsDeleted   int64 `json:"api_key_metric_rows_deleted"`
	MinuteRowsDeleted         int64 `json:"minute_rows_deleted"`
	DurationBucketRowsDeleted int64 `json:"duration_bucket_rows_deleted"`
	Incomplete                bool  `json:"-"`
}

// DeleteChannelMonitorCostsBefore deletes each metric table
// against their configured cutoffs. The legacy entry point keeps duration
// buckets aligned with route metrics; the extended entry point supports an
// independent duration-bucket cutoff.
func DeleteChannelMonitorCostsBefore(
	ctx context.Context,
	costCutoff int64,
	routeMetricCutoff int64,
	apiKeyMetricCutoff int64,
	batchSize int,
	budget ChannelMonitorCleanupBudget,
) (ChannelMonitorCostRetentionResult, error) {
	return DeleteChannelMonitorCostsBeforeWithDurationBucketCutoff(
		ctx, costCutoff, routeMetricCutoff, routeMetricCutoff, apiKeyMetricCutoff, batchSize, budget,
	)
}

// DeleteChannelMonitorCostsBeforeWithDurationBucketCutoff lets duration
// buckets use an independent retention cutoff from route metrics.
func DeleteChannelMonitorCostsBeforeWithDurationBucketCutoff(
	ctx context.Context,
	costCutoff int64,
	routeMetricCutoff int64,
	durationBucketCutoff int64,
	apiKeyMetricCutoff int64,
	batchSize int,
	budget ChannelMonitorCleanupBudget,
) (ChannelMonitorCostRetentionResult, error) {
	result := ChannelMonitorCostRetentionResult{}
	if costCutoff <= 0 {
		return result, errors.New("channel monitor cost cutoff must be positive")
	}
	if routeMetricCutoff <= 0 {
		return result, errors.New("channel monitor route metric cutoff must be positive")
	}
	if durationBucketCutoff <= 0 {
		return result, errors.New("channel monitor duration bucket cutoff must be positive")
	}
	if apiKeyMetricCutoff <= 0 {
		return result, errors.New("channel monitor API Key metric cutoff must be positive")
	}
	if batchSize <= 0 {
		return result, errors.New("channel monitor cost cleanup batch size must be positive")
	}

	durationBudget := budget.Slice(7)
	if DB.Migrator().HasTable(&ChannelMonitorMinuteDurationBucket{}) {
		for {
			if durationBudget.Exhausted() {
				result.Incomplete = true
				break
			}
			var ids []int64
			if err := DB.WithContext(ctx).
				Model(&ChannelMonitorMinuteDurationBucket{}).
				Where("minute_start < ?", durationBucketCutoff).
				Order("minute_start ASC, id ASC").
				Limit(batchSize).
				Pluck("id", &ids).Error; err != nil {
				return result, err
			}
			if len(ids) == 0 {
				break
			}
			deleted := DB.WithContext(ctx).Where("id IN ?", ids).Delete(&ChannelMonitorMinuteDurationBucket{})
			if deleted.Error != nil {
				return result, deleted.Error
			}
			result.DurationBucketRowsDeleted += deleted.RowsAffected
		}
	}

	deleteMetricRows := func(table any, cutoff int64, metricBudget ChannelMonitorCleanupBudget, rowsDeleted *int64) error {
		if !DB.Migrator().HasTable(table) {
			return nil
		}
		for {
			if metricBudget.Exhausted() {
				result.Incomplete = true
				break
			}
			var ids []int64
			if err := DB.WithContext(ctx).
				Model(table).
				Where("minute_start < ?", cutoff).
				Order("minute_start ASC, id ASC").
				Limit(batchSize).
				Pluck("id", &ids).Error; err != nil {
				return err
			}
			if len(ids) == 0 {
				break
			}
			deleted := DB.WithContext(ctx).Where("id IN ?", ids).Delete(table)
			if deleted.Error != nil {
				return deleted.Error
			}
			*rowsDeleted += deleted.RowsAffected
		}
		return nil
	}
	routeMetricBudget := budget.Slice(6)
	if err := deleteMetricRows(
		&ChannelMonitorMinuteRouteMetric{}, routeMetricCutoff, routeMetricBudget,
		&result.RouteMetricRowsDeleted,
	); err != nil {
		return result, err
	}
	apiKeyMetricBudget := budget.Slice(5)
	if err := deleteMetricRows(
		&ChannelMonitorMinuteAPIKeyMetric{}, apiKeyMetricCutoff, apiKeyMetricBudget,
		&result.APIKeyMetricRowsDeleted,
	); err != nil {
		return result, err
	}
	result.MinuteRowsDeleted = result.RouteMetricRowsDeleted + result.APIKeyMetricRowsDeleted
	if err := TrimChannelMonitorAggregationCoverage(ctx, routeMetricCutoff); err != nil {
		return result, err
	}
	if result.MinuteRowsDeleted > 0 || result.DurationBucketRowsDeleted > 0 {
		InvalidateChannelMonitorAggregateCaches()
	}

	taskCostEventBudget := budget.Slice(3)
	if DB.Migrator().HasTable(&ChannelTaskCostEvent{}) {
		for {
			if taskCostEventBudget.Exhausted() {
				result.Incomplete = true
				break
			}
			var ids []int64
			if err := DB.WithContext(ctx).
				Model(&ChannelTaskCostEvent{}).
				Where("day_start < ?", costCutoff).
				Order("day_start ASC, id ASC").
				Limit(batchSize).
				Pluck("id", &ids).Error; err != nil {
				return result, err
			}
			if len(ids) == 0 {
				break
			}
			deleted := DB.WithContext(ctx).Where("id IN ?", ids).Delete(&ChannelTaskCostEvent{})
			if deleted.Error != nil {
				return result, deleted.Error
			}
			result.TaskCostEventRowsDeleted += deleted.RowsAffected
		}
	}

	apiKeyBudget := budget.Slice(2)
	for {
		if apiKeyBudget.Exhausted() {
			result.Incomplete = true
			break
		}
		var ids []int64
		if err := DB.WithContext(ctx).
			Model(&ChannelDailyAPIKeyCost{}).
			Where("day_start < ?", costCutoff).
			Order("day_start ASC, id ASC").
			Limit(batchSize).
			Pluck("id", &ids).Error; err != nil {
			return result, err
		}
		if len(ids) == 0 {
			break
		}
		deleted := DB.WithContext(ctx).Where("id IN ?", ids).Delete(&ChannelDailyAPIKeyCost{})
		if deleted.Error != nil {
			return result, deleted.Error
		}
		result.APIKeyRowsDeleted += deleted.RowsAffected
	}

	channelBudget := budget.Slice(1)
	for {
		if channelBudget.Exhausted() {
			result.Incomplete = true
			break
		}
		var ids []int64
		if err := DB.WithContext(ctx).
			Model(&ChannelDailyCost{}).
			Where("day_start < ?", costCutoff).
			Order("day_start ASC, id ASC").
			Limit(batchSize).
			Pluck("id", &ids).Error; err != nil {
			return result, err
		}
		if len(ids) == 0 {
			break
		}
		deleted := DB.WithContext(ctx).Where("id IN ?", ids).Delete(&ChannelDailyCost{})
		if deleted.Error != nil {
			return result, deleted.Error
		}
		result.ChannelRowsDeleted += deleted.RowsAffected
	}
	return result, nil
}
