package model

import (
	"context"
	"errors"
)

type ChannelMonitorCostRetentionResult struct {
	ChannelRowsDeleted        int64 `json:"channel_rows_deleted"`
	APIKeyRowsDeleted         int64 `json:"api_key_rows_deleted"`
	MinuteRowsDeleted         int64 `json:"minute_rows_deleted"`
	DurationBucketRowsDeleted int64 `json:"duration_bucket_rows_deleted"`
}

func DeleteChannelMonitorCostsBefore(ctx context.Context, cutoff int64, batchSize int) (ChannelMonitorCostRetentionResult, error) {
	result := ChannelMonitorCostRetentionResult{}
	if cutoff <= 0 {
		return result, errors.New("channel monitor cost cutoff must be positive")
	}
	if batchSize <= 0 {
		return result, errors.New("channel monitor cost cleanup batch size must be positive")
	}

	if DB.Migrator().HasTable(&ChannelMonitorMinuteDurationBucket{}) {
		for {
			var ids []int64
			if err := DB.WithContext(ctx).
				Model(&ChannelMonitorMinuteDurationBucket{}).
				Where("minute_start < ?", cutoff).
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

	for {
		var ids []int64
		if err := DB.WithContext(ctx).
			Model(&ChannelMonitorMinuteMetric{}).
			Where("minute_start < ?", cutoff).
			Order("minute_start ASC, id ASC").
			Limit(batchSize).
			Pluck("id", &ids).Error; err != nil {
			return result, err
		}
		if len(ids) == 0 {
			break
		}
		deleted := DB.WithContext(ctx).Where("id IN ?", ids).Delete(&ChannelMonitorMinuteMetric{})
		if deleted.Error != nil {
			return result, deleted.Error
		}
		result.MinuteRowsDeleted += deleted.RowsAffected
	}

	for {
		var ids []int64
		if err := DB.WithContext(ctx).
			Model(&ChannelDailyAPIKeyCost{}).
			Where("day_start < ?", cutoff).
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

	for {
		var ids []int64
		if err := DB.WithContext(ctx).
			Model(&ChannelDailyCost{}).
			Where("day_start < ?", cutoff).
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
