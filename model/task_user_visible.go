package model

import "gorm.io/gorm"

// userVisibleTaskQuery applies the administrator task filters and restricts
// the result to terminal success/failure outcomes.
func userVisibleTaskQuery(params SyncTaskQueryParams) *gorm.DB {
	query := DB.Model(&Task{})
	if params.ChannelID != "" {
		query = query.Where("channel_id = ?", params.ChannelID)
	}
	if params.Platform != "" {
		query = query.Where("platform = ?", params.Platform)
	}
	if params.UserID != "" {
		query = query.Where("user_id = ?", params.UserID)
	}
	if len(params.UserIDs) != 0 {
		query = query.Where("user_id IN ?", params.UserIDs)
	}
	if params.TaskID != "" {
		query = query.Where("task_id = ?", params.TaskID)
	}
	if params.Action != "" {
		query = query.Where("action = ?", params.Action)
	}
	if params.Status != "" {
		query = query.Where("status = ?", params.Status)
	}
	if params.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", params.EndTimestamp)
	}
	return query.Where("status IN ?", []TaskStatus{TaskStatusSuccess, TaskStatusFailure})
}

// TaskGetAllUserVisibleTasks returns only final async-task outcomes while
// preserving the filters and pagination used by the admin endpoint.
func TaskGetAllUserVisibleTasks(startIdx int, num int, params SyncTaskQueryParams) []*Task {
	var tasks []*Task
	if err := userVisibleTaskQuery(params).
		Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error; err != nil {
		return nil
	}
	return tasks
}

// TaskGetAllUserVisibleUserTasks returns terminal outcomes for one user while
// preserving the self endpoint's channel redaction.
func TaskGetAllUserVisibleUserTasks(userID int, startIdx int, num int, params SyncTaskQueryParams) []*Task {
	var tasks []*Task
	if err := userVisibleTaskQuery(params).
		Where("user_id = ?", userID).
		Omit("channel_id").
		Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error; err != nil {
		return nil
	}
	return tasks
}

// TaskCountAllUserVisibleTasks counts rows using the same terminal predicate
// as TaskGetAllUserVisibleTasks.
func TaskCountAllUserVisibleTasks(params SyncTaskQueryParams) int64 {
	var total int64
	_ = userVisibleTaskQuery(params).Count(&total).Error
	return total
}

// TaskCountAllUserVisibleUserTasks counts terminal outcomes for one user.
func TaskCountAllUserVisibleUserTasks(userID int, params SyncTaskQueryParams) int64 {
	var total int64
	_ = userVisibleTaskQuery(params).Where("user_id = ?", userID).Count(&total).Error
	return total
}
