package model

import "gorm.io/gorm"

// userVisibleMidjourneyQuery applies the same filters as the administrator
// query, then limits the result to completed outcomes. A submission failure
// can have an empty or stale status in legacy/provider-specific rows; a
// non-empty failure reason makes that row a terminal result as well.
func userVisibleMidjourneyQuery(params TaskQueryParams) *gorm.DB {
	query := DB.Model(&Midjourney{})
	if params.ChannelID != "" {
		query = query.Where("channel_id = ?", params.ChannelID)
	}
	if params.MjID != "" {
		query = query.Where("mj_id = ?", params.MjID)
	}
	if params.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", params.EndTimestamp)
	}
	return query.Where(
		"(status IN ? OR (fail_reason IS NOT NULL AND fail_reason <> ?))",
		[]string{"SUCCESS", "FAILURE"},
		"",
	)
}

// GetAllUserVisibleMidjourneyTasks returns only final Midjourney outcomes
// while preserving the filters and pagination used by the admin endpoint.
func GetAllUserVisibleMidjourneyTasks(startIdx int, num int, params TaskQueryParams) []*Midjourney {
	var tasks []*Midjourney
	if err := userVisibleMidjourneyQuery(params).
		Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error; err != nil {
		return nil
	}
	return tasks
}

// GetAllUserVisibleMidjourneyUserTasks returns terminal outcomes for one user
// while preserving the self endpoint's response fields.
func GetAllUserVisibleMidjourneyUserTasks(userID int, startIdx int, num int, params TaskQueryParams) []*Midjourney {
	var tasks []*Midjourney
	if err := userVisibleMidjourneyQuery(params).
		Where("user_id = ?", userID).
		Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error; err != nil {
		return nil
	}
	return tasks
}

// CountAllUserVisibleMidjourneyTasks counts rows using the same terminal
// outcome predicate as GetAllUserVisibleMidjourneyTasks.
func CountAllUserVisibleMidjourneyTasks(params TaskQueryParams) int64 {
	var total int64
	_ = userVisibleMidjourneyQuery(params).Count(&total).Error
	return total
}

// CountAllUserVisibleMidjourneyUserTasks counts terminal outcomes for one user.
func CountAllUserVisibleMidjourneyUserTasks(userID int, params TaskQueryParams) int64 {
	var total int64
	_ = userVisibleMidjourneyQuery(params).Where("user_id = ?", userID).Count(&total).Error
	return total
}
