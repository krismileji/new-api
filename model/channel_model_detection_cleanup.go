package model

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

var ErrChannelModelDetectionChannelActive = errors.New("渠道仍有活动模型检测轮次")

type ChannelModelDetectionRetentionResult struct {
	CostEventRowsDeleted int64 `json:"cost_event_rows_deleted"`
	ExecutionRowsDeleted int64 `json:"execution_rows_deleted"`
	RunRowsDeleted       int64 `json:"run_rows_deleted"`
	Incomplete           bool  `json:"-"`
}

func DeleteChannelModelDetectionHistoryBefore(
	ctx context.Context,
	cutoff int64,
	batchSize int,
	budget ChannelMonitorCleanupBudget,
) (ChannelModelDetectionRetentionResult, error) {
	result := ChannelModelDetectionRetentionResult{}
	if cutoff <= 0 {
		return result, errors.New("模型检测历史保留截止时间必须为正数")
	}
	if batchSize <= 0 {
		return result, errors.New("模型检测历史清理批次必须为正数")
	}
	if DB == nil {
		return result, errors.New("模型检测历史清理数据库不可用")
	}

	db := DB.WithContext(ctx)
	terminalStatuses := []string{
		ChannelModelDetectionRunStatusCompleted,
		ChannelModelDetectionRunStatusPartial,
		ChannelModelDetectionRunStatusFailed,
		ChannelModelDetectionRunStatusExternalSessionConflict,
		ChannelModelDetectionRunStatusCanceled,
	}
	for {
		if budget.Exhausted() {
			result.Incomplete = true
			break
		}
		var runIDs []string
		protectedRunIDs := db.Model(&ChannelModelDetectionCostEvent{}).
			Select("run_id").
			Where("dispatch_state = ? OR settlement_status = ?", ChannelModelDetectionDispatchPrepared, ChannelModelDetectionSettlementPending)
		if err := db.Model(&ChannelModelDetectionRun{}).
			Where("status IN ? AND finished_at > 0 AND finished_at < ?", terminalStatuses, cutoff).
			Where("run_id NOT IN (?)", protectedRunIDs).
			Order("finished_at ASC, id ASC").
			Limit(batchSize).
			Pluck("run_id", &runIDs).Error; err != nil {
			return result, err
		}
		if len(runIDs) == 0 {
			break
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			deletedCosts := tx.Where("run_id IN ?", runIDs).Delete(&ChannelModelDetectionCostEvent{})
			if deletedCosts.Error != nil {
				return deletedCosts.Error
			}
			deletedExecutions := tx.Where("run_id IN ?", runIDs).Delete(&ChannelModelDetectionExecution{})
			if deletedExecutions.Error != nil {
				return deletedExecutions.Error
			}
			deletedRuns := tx.Where("run_id IN ?", runIDs).Delete(&ChannelModelDetectionRun{})
			if deletedRuns.Error != nil {
				return deletedRuns.Error
			}
			result.CostEventRowsDeleted += deletedCosts.RowsAffected
			result.ExecutionRowsDeleted += deletedExecutions.RowsAffected
			result.RunRowsDeleted += deletedRuns.RowsAffected
			return nil
		}); err != nil {
			return result, err
		}
	}
	return result, nil
}

func deleteChannelModelDetectionDataTx(tx *gorm.DB, channelIDs []int) error {
	if tx == nil || len(channelIDs) == 0 {
		return nil
	}
	tables := []any{
		&ChannelModelDetectionConfig{},
		&ChannelModelDetectionTarget{},
		&ChannelModelDetectionRun{},
		&ChannelModelDetectionExecution{},
		&ChannelModelDetectionCostEvent{},
	}
	for _, table := range tables {
		if !tx.Migrator().HasTable(table) {
			return nil
		}
	}
	var active int64
	if err := tx.Model(&ChannelModelDetectionRun{}).
		Where("channel_id IN ? AND status IN ?", channelIDs, []string{
			ChannelModelDetectionRunStatusQueued,
			ChannelModelDetectionRunStatusWaitingDetector,
			ChannelModelDetectionRunStatusSubmitting,
			ChannelModelDetectionRunStatusRunning,
			ChannelModelDetectionRunStatusSubmissionUnknown,
			ChannelModelDetectionRunStatusCanceling,
		}).Count(&active).Error; err != nil {
		return err
	}
	if active > 0 {
		return ErrChannelModelDetectionChannelActive
	}
	for _, table := range []any{
		&ChannelModelDetectionCostEvent{},
		&ChannelModelDetectionExecution{},
		&ChannelModelDetectionRun{},
		&ChannelModelDetectionTarget{},
		&ChannelModelDetectionConfig{},
	} {
		if err := tx.Where("channel_id IN ?", channelIDs).Delete(table).Error; err != nil {
			return err
		}
	}
	return nil
}
