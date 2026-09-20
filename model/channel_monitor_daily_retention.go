package model

import (
	"context"
	"errors"
	"strconv"
)

const ChannelMonitorDailyMetricRetentionDaysOption = "ChannelMonitorDailyMetricRetentionDays"
const ChannelMonitorDailyMetricDefaultRetentionDays = 30
const ChannelMonitorDailyMetricMinRetentionDays = 2

type ChannelMonitorDailyRetentionResult struct {
	DailyMetricRowsDeleted     int64 `json:"daily_metric_rows_deleted"`
	DailyMinuteRowsDeleted     int64 `json:"daily_minute_rows_deleted"`
	DailyCheckpointRowsDeleted int64 `json:"daily_checkpoint_rows_deleted"`
	Incomplete                 bool  `json:"-"`
}

// GetChannelMonitorDailyRetentionCutoff also bounds the persistence worker so
// late events cannot continually recreate data that cleanup has expired.
func GetChannelMonitorDailyRetentionCutoff(ctx context.Context, now int64) (int64, error) {
	var options []Option
	if err := DB.WithContext(ctx).Where(QuotedMainKeyColumn()+" IN ?", []string{
		ChannelMonitorDailyMetricRetentionDaysOption, "ChannelMonitorCleanupEnabled",
	}).Find(&options).Error; err != nil {
		return 0, err
	}
	days := ChannelMonitorDailyMetricDefaultRetentionDays
	enabled := true
	for _, option := range options {
		var err error
		if option.Key == "ChannelMonitorCleanupEnabled" {
			enabled, err = strconv.ParseBool(option.Value)
		} else {
			days, err = strconv.Atoi(option.Value)
			if days < ChannelMonitorDailyMetricMinRetentionDays || days > 3650 {
				err = errors.New("业务日统计保留天数必须在 2 到 3650 天之间")
			}
		}
		if err != nil {
			return 0, err
		}
	}
	if !enabled {
		return 0, nil
	}
	return ChannelDailyCostDayStart(now) - int64(days-1)*24*60*60, nil
}

// Daily aggregates and their legacy minute contributions share one cutoff.
// Checkpoints are removed last. Every table gets a bounded share of the budget.
func DeleteChannelMonitorDailyMetricsBefore(ctx context.Context, cutoff int64, batchSize int, budget ChannelMonitorCleanupBudget) (ChannelMonitorDailyRetentionResult, error) {
	result := ChannelMonitorDailyRetentionResult{}
	if cutoff <= 0 || batchSize <= 0 {
		return result, errors.New("业务日统计清理边界或批量大小无效")
	}
	tables := []struct {
		model      any
		timeColumn string
		deleted    *int64
	}{
		{&ChannelMonitorDailySuccessMinute{}, "minute_start", &result.DailyMinuteRowsDeleted},
		{&ChannelMonitorDailySuccessLedger{}, "day_start", &result.DailyMetricRowsDeleted},
		{&ChannelMonitorDailyCheckpoint{}, "day_start", &result.DailyCheckpointRowsDeleted},
	}
	for index, table := range tables {
		if !DB.Migrator().HasTable(table.model) {
			continue
		}
		tableBudget := budget.Slice(len(tables) - index)
		for {
			if tableBudget.Exhausted() {
				result.Incomplete = true
				break
			}
			var ids []int64
			if err := DB.WithContext(ctx).Model(table.model).Where(table.timeColumn+" < ?", cutoff).
				Order(table.timeColumn+" ASC, id ASC").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
				return result, err
			}
			if len(ids) == 0 {
				break
			}
			deleted := DB.WithContext(ctx).Where("id IN ? AND "+table.timeColumn+" < ?", ids, cutoff).Delete(table.model)
			if deleted.Error != nil {
				return result, deleted.Error
			}
			*table.deleted += deleted.RowsAffected
		}
	}
	return result, nil
}
