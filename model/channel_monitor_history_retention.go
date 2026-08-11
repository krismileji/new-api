package model

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
)

type ChannelMonitorHistoryRetentionCutoffs struct {
	ExecutionDetail int64
	Task            int64
	RatioHistory    int64
}

type ChannelMonitorHistoryRetentionResult struct {
	ExecutionDetailRowsDeleted int64 `json:"execution_detail_rows_deleted"`
	TaskRowsDeleted            int64 `json:"task_rows_deleted"`
	RatioHistoryRowsDeleted    int64 `json:"ratio_history_rows_deleted"`
	Incomplete                 bool  `json:"-"`
}

type channelMonitorRetentionTask struct {
	ID     int64
	TaskID string
}

func DeleteChannelMonitorHistoryBefore(
	ctx context.Context,
	cutoffs ChannelMonitorHistoryRetentionCutoffs,
	taskTypes []string,
	keepLatestTasks int,
	batchSize int,
	budget ChannelMonitorCleanupBudget,
) (ChannelMonitorHistoryRetentionResult, error) {
	result := ChannelMonitorHistoryRetentionResult{}
	if cutoffs.ExecutionDetail <= 0 || cutoffs.Task <= 0 || cutoffs.RatioHistory <= 0 {
		return result, errors.New("渠道监控历史数据保留截止时间必须为正数")
	}
	if cutoffs.Task > cutoffs.ExecutionDetail {
		return result, errors.New("渠道监控任务保留期不能短于调度执行明细保留期")
	}
	if keepLatestTasks <= 0 {
		return result, errors.New("渠道监控任务最少保留数量必须为正数")
	}
	if batchSize <= 0 {
		return result, errors.New("渠道监控历史数据清理批次必须为正数")
	}

	normalizedTaskTypes := make([]string, 0, len(taskTypes))
	seenTaskTypes := make(map[string]struct{}, len(taskTypes))
	for _, taskType := range taskTypes {
		taskType = strings.TrimSpace(taskType)
		if taskType == "" {
			continue
		}
		if _, exists := seenTaskTypes[taskType]; exists {
			continue
		}
		seenTaskTypes[taskType] = struct{}{}
		normalizedTaskTypes = append(normalizedTaskTypes, taskType)
	}
	if len(normalizedTaskTypes) == 0 {
		return result, errors.New("渠道监控任务类型不能为空")
	}

	db := DB.WithContext(ctx)
	detailTableExists := db.Migrator().HasTable(&ChannelSmartScheduleExecutionDetail{})
	detailBudget := budget.Slice(3)
	if detailTableExists {
		for {
			if detailBudget.Exhausted() {
				result.Incomplete = true
				break
			}
			var ids []int64
			if err := db.Model(&ChannelSmartScheduleExecutionDetail{}).
				Where("created_at < ?", cutoffs.ExecutionDetail).
				Order("created_at ASC, id ASC").
				Limit(batchSize).
				Pluck("id", &ids).Error; err != nil {
				return result, err
			}
			if len(ids) == 0 {
				break
			}
			deleted := db.Where("id IN ?", ids).Delete(&ChannelSmartScheduleExecutionDetail{})
			if deleted.Error != nil {
				return result, deleted.Error
			}
			result.ExecutionDetailRowsDeleted += deleted.RowsAffected
		}
	}

	taskBudget := budget.Slice(2)
	if db.Migrator().HasTable(&SystemTask{}) {
		if taskBudget.Exhausted() {
			result.Incomplete = true
		} else {
			keptTaskIDs := make([]int64, 0, len(normalizedTaskTypes)*keepLatestTasks)
			prepared := true
			for _, taskType := range normalizedTaskTypes {
				if taskBudget.Exhausted() {
					result.Incomplete = true
					prepared = false
					break
				}
				var ids []int64
				if err := db.Model(&SystemTask{}).
					Where("type = ?", taskType).
					Order("id DESC").
					Limit(keepLatestTasks).
					Pluck("id", &ids).Error; err != nil {
					return result, err
				}
				keptTaskIDs = append(keptTaskIDs, ids...)
			}

			if prepared {
				terminalStatuses := []SystemTaskStatus{SystemTaskStatusSucceeded, SystemTaskStatusFailed}
				for {
					if taskBudget.Exhausted() {
						result.Incomplete = true
						break
					}
					var tasks []channelMonitorRetentionTask
					query := db.Model(&SystemTask{}).
						Select("id", "task_id").
						Where("type IN ?", normalizedTaskTypes).
						Where("status IN ?", terminalStatuses).
						Where("updated_at < ?", cutoffs.Task)
					if len(keptTaskIDs) > 0 {
						query = query.Where("id NOT IN ?", keptTaskIDs)
					}
					if err := query.Order("updated_at ASC, id ASC").Limit(batchSize).Find(&tasks).Error; err != nil {
						return result, err
					}
					if len(tasks) == 0 {
						break
					}

					taskIDs := make([]string, 0, len(tasks))
					ids := make([]int64, 0, len(tasks))
					for _, task := range tasks {
						taskIDs = append(taskIDs, task.TaskID)
						ids = append(ids, task.ID)
					}
					var detailRowsDeleted int64
					var taskRowsDeleted int64
					if err := db.Transaction(func(tx *gorm.DB) error {
						if detailTableExists {
							deletedDetails := tx.Where("task_id IN ?", taskIDs).
								Delete(&ChannelSmartScheduleExecutionDetail{})
							if deletedDetails.Error != nil {
								return deletedDetails.Error
							}
							detailRowsDeleted = deletedDetails.RowsAffected
						}
						deletedTasks := tx.Where("id IN ?", ids).
							Where("type IN ?", normalizedTaskTypes).
							Where("status IN ?", terminalStatuses).
							Where("updated_at < ?", cutoffs.Task).
							Delete(&SystemTask{})
						if deletedTasks.Error != nil {
							return deletedTasks.Error
						}
						taskRowsDeleted = deletedTasks.RowsAffected
						return nil
					}); err != nil {
						return result, err
					}
					result.ExecutionDetailRowsDeleted += detailRowsDeleted
					result.TaskRowsDeleted += taskRowsDeleted
				}
			}
		}
	}

	ratioBudget := budget.Slice(1)
	if db.Migrator().HasTable(&ChannelRatioHistory{}) {
		for {
			if ratioBudget.Exhausted() {
				result.Incomplete = true
				break
			}
			var ids []int
			if err := db.Model(&ChannelRatioHistory{}).
				Where("created_time < ?", cutoffs.RatioHistory).
				Order("created_time ASC, id ASC").
				Limit(batchSize).
				Pluck("id", &ids).Error; err != nil {
				return result, err
			}
			if len(ids) == 0 {
				break
			}
			deleted := db.Where("id IN ?", ids).Delete(&ChannelRatioHistory{})
			if deleted.Error != nil {
				return result, deleted.Error
			}
			result.RatioHistoryRowsDeleted += deleted.RowsAffected
		}
	}

	return result, nil
}
