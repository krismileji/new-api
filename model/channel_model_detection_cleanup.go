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
	activeStatuses := []string{
		ChannelModelDetectionRunStatusQueued,
		ChannelModelDetectionRunStatusWaitingDetector,
		ChannelModelDetectionRunStatusSubmitting,
		ChannelModelDetectionRunStatusRunning,
		ChannelModelDetectionRunStatusSubmissionUnknown,
		ChannelModelDetectionRunStatusCanceling,
	}
	var activeRuns []ChannelModelDetectionRun
	if err := tx.Select("run_id", "channel_id", "logical_channel_id", "logical_revision", "logical_member_snapshot_json").
		Where("status IN ?", activeStatuses).Find(&activeRuns).Error; err != nil {
		return err
	}
	deleted := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		deleted[channelID] = struct{}{}
	}
	activeRunIDs := make(map[string]struct{}, len(activeRuns))
	for _, run := range activeRuns {
		activeRunIDs[run.RunId] = struct{}{}
		if _, ownerDeleted := deleted[run.ChannelId]; ownerDeleted {
			return ErrChannelModelDetectionChannelActive
		}
		if run.LogicalRevision <= 0 {
			continue
		}
		members, err := run.LogicalMemberSnapshot()
		if err != nil {
			return err
		}
		for _, member := range members {
			if _, memberDeleted := deleted[member.ChannelID]; memberDeleted {
				return ErrChannelModelDetectionChannelActive
			}
		}
	}
	if len(activeRunIDs) > 0 {
		var actualRunIDs []string
		if err := tx.Model(&ChannelModelDetectionExecution{}).Where("channel_id IN ?", channelIDs).Distinct().Pluck("run_id", &actualRunIDs).Error; err != nil {
			return err
		}
		for _, runID := range actualRunIDs {
			if _, active := activeRunIDs[runID]; active {
				return ErrChannelModelDetectionChannelActive
			}
		}
	}

	var physicalRunIDs []string
	if err := tx.Model(&ChannelModelDetectionRun{}).
		Where("channel_id IN ? AND COALESCE(logical_revision, 0) = 0", channelIDs).
		Pluck("run_id", &physicalRunIDs).Error; err != nil {
		return err
	}
	if len(physicalRunIDs) > 0 {
		if err := tx.Where("run_id IN ?", physicalRunIDs).Delete(&ChannelModelDetectionCostEvent{}).Error; err != nil {
			return err
		}
		if err := tx.Where("run_id IN ?", physicalRunIDs).Delete(&ChannelModelDetectionExecution{}).Error; err != nil {
			return err
		}
		if err := tx.Where("run_id IN ?", physicalRunIDs).Delete(&ChannelModelDetectionRun{}).Error; err != nil {
			return err
		}
	}
	groupedRunIDs := tx.Model(&ChannelModelDetectionRun{}).
		Select("run_id").Where("COALESCE(logical_revision, 0) > 0")
	if err := tx.Where("channel_id IN ? AND run_id NOT IN (?)", channelIDs, groupedRunIDs).Delete(&ChannelModelDetectionCostEvent{}).Error; err != nil {
		return err
	}
	if err := tx.Where("channel_id IN ? AND run_id NOT IN (?)", channelIDs, groupedRunIDs).Delete(&ChannelModelDetectionExecution{}).Error; err != nil {
		return err
	}
	if err := tx.Where("channel_id IN ?", channelIDs).Delete(&ChannelModelDetectionTarget{}).Error; err != nil {
		return err
	}
	if err := tx.Where("channel_id IN ?", channelIDs).Delete(&ChannelModelDetectionConfig{}).Error; err != nil {
		return err
	}
	return nil
}
