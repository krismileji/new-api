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
	batchSize int,
	budget ChannelMonitorCleanupBudget,
) (ChannelMonitorHistoryRetentionResult, error) {
	return DeleteChannelMonitorHistoryBeforeWithTaskCutoffs(
		ctx,
		cutoffs,
		taskTypes,
		nil,
		batchSize,
		budget,
	)
}

// DeleteChannelMonitorHistoryBeforeWithTaskCutoffs applies the common history
// cutoffs while allowing selected system task types to use their own cutoff.
func DeleteChannelMonitorHistoryBeforeWithTaskCutoffs(
	ctx context.Context,
	cutoffs ChannelMonitorHistoryRetentionCutoffs,
	taskTypes []string,
	taskTypeCutoffs map[string]int64,
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
	normalizedTaskCutoffs := make(map[string]int64, len(taskTypeCutoffs))
	for taskType, cutoff := range taskTypeCutoffs {
		taskType = strings.TrimSpace(taskType)
		if taskType == "" || cutoff <= 0 {
			return result, errors.New("渠道监控任务类型保留截止时间必须为正数")
		}
		normalizedTaskCutoffs[taskType] = cutoff
	}

	taskCutoffPredicate := func() (string, []any) {
		conditions := make([]string, 0, len(normalizedTaskTypes))
		args := make([]any, 0, len(normalizedTaskTypes)*2)
		for _, taskType := range normalizedTaskTypes {
			cutoff := cutoffs.Task
			if configuredCutoff, ok := normalizedTaskCutoffs[taskType]; ok {
				cutoff = configuredCutoff
			}
			conditions = append(conditions, "(type = ? AND updated_at < ?)")
			args = append(args, taskType, cutoff)
		}
		return strings.Join(conditions, " OR "), args
	}

	db := DB.WithContext(ctx)
	detailTableExists := db.Migrator().HasTable(&ChannelSmartScheduleExecutionDetail{})
	terminalStatuses := []SystemTaskStatus{SystemTaskStatusSucceeded, SystemTaskStatusFailed}
	// Snapshots belonging to active tasks are retained so an in-flight task is
	// never deleted while it is still running. Completed task history follows
	// the configured retention cutoff without a count-based floor.
	protectedDetailTaskIDs := make(map[string]struct{})
	if detailTableExists && db.Migrator().HasTable(&SystemTask{}) {
		var activeTaskIDs []string
		if err := db.Model(&SystemTask{}).
			Where("type IN ?", normalizedTaskTypes).
			Where("status IN ?", []SystemTaskStatus{SystemTaskStatusPending, SystemTaskStatusRunning}).
			Pluck("task_id", &activeTaskIDs).Error; err != nil {
			return result, err
		}
		for _, taskID := range activeTaskIDs {
			if taskID != "" {
				protectedDetailTaskIDs[taskID] = struct{}{}
			}
		}
	}
	protectedDetailTaskIDList := make([]string, 0, len(protectedDetailTaskIDs))
	for taskID := range protectedDetailTaskIDs {
		protectedDetailTaskIDList = append(protectedDetailTaskIDList, taskID)
	}
	detailBudget := budget.Slice(3)
	if detailTableExists {
		for {
			if detailBudget.Exhausted() {
				result.Incomplete = true
				break
			}
			var ids []int64
			query := db.Model(&ChannelSmartScheduleExecutionDetail{}).
				Where("created_at < ?", cutoffs.ExecutionDetail)
			if len(protectedDetailTaskIDList) > 0 {
				query = query.Where("task_id NOT IN ?", protectedDetailTaskIDList)
			}
			if err := query.Order("created_at ASC, id ASC").
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
			for {
				if taskBudget.Exhausted() {
					result.Incomplete = true
					break
				}
				var tasks []channelMonitorRetentionTask
				query := db.Model(&SystemTask{}).
					Select("id", "task_id").
					Where("type IN ?", normalizedTaskTypes).
					Where("status IN ?", terminalStatuses)
				taskCutoffSQL, taskCutoffArgs := taskCutoffPredicate()
				query = query.Where(taskCutoffSQL, taskCutoffArgs...)
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
						Where("status IN ?", terminalStatuses)
					taskCutoffSQL, taskCutoffArgs := taskCutoffPredicate()
					deletedTasks = deletedTasks.Where(taskCutoffSQL, taskCutoffArgs...)
					deletedTasks = deletedTasks.Delete(&SystemTask{})
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
