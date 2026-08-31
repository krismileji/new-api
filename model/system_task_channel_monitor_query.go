package model

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

const SystemTaskListResultMaxCharacters = 64 * 1024

func GetSystemTaskTypeByTaskID(ctx context.Context, taskID string) (taskType string, exists bool, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var task SystemTask
	err = DB.WithContext(ctx).Select("type").Where("task_id = ?", taskID).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return task.Type, true, nil
}

// GetSystemTasksByTypePage keeps the paginated task list bounded. Large legacy
// result documents are not transferred from the database; task details belong
// to their dedicated endpoint.
func GetSystemTasksByTypePage(
	ctx context.Context,
	taskType string,
	startIdx int,
	limit int,
) (tasks []*SystemTask, total int64, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if startIdx < 0 {
		startIdx = 0
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	db := DB.WithContext(ctx)
	query := db.Model(&SystemTask{}).Where("type = ?", taskType)
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err = query.Select(
		"id", "task_id", "type", "status", "active_key", "payload", "state",
		"error", "locked_by", "created_at", "updated_at",
	).Order("created_at desc, id desc").Limit(limit).Offset(startIdx).Find(&tasks).Error; err != nil {
		return nil, 0, err
	}
	if len(tasks) == 0 {
		return tasks, total, nil
	}

	taskIDs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		taskIDs = append(taskIDs, task.TaskID)
	}
	type taskResultRow struct {
		TaskID string
		Result string
	}
	var resultRows []taskResultRow
	if err = db.Model(&SystemTask{}).
		Select("task_id", "result").
		Where("task_id IN ?", taskIDs).
		Where("result = ? OR LENGTH(result) <= ?", "", SystemTaskListResultMaxCharacters).
		Find(&resultRows).Error; err != nil {
		return nil, 0, err
	}
	resultByTaskID := make(map[string]string, len(resultRows))
	for _, row := range resultRows {
		resultByTaskID[row.TaskID] = row.Result
	}
	for _, task := range tasks {
		task.Result = resultByTaskID[task.TaskID]
	}
	return tasks, total, nil
}
