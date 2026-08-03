package model

import (
	"errors"

	"gorm.io/gorm"
)

func expireStaleSystemTaskLocksSafely(now int64) error {
	var staleLocks []*SystemTaskLock
	if err := DB.Where("locked_until < ?", now).Find(&staleLocks).Error; err != nil {
		return err
	}
	for _, staleLock := range staleLocks {
		if err := DB.Transaction(func(tx *gorm.DB) error {
			var task SystemTask
			if err := lockForUpdate(tx).Where("task_id = ?", staleLock.TaskID).First(&task).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}

			var currentLock SystemTaskLock
			if err := lockForUpdate(tx).Where(
				"type = ? AND task_id = ? AND locked_by = ? AND locked_until < ?",
				staleLock.Type,
				staleLock.TaskID,
				staleLock.LockedBy,
				now,
			).First(&currentLock).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}

			if task.Status == SystemTaskStatusRunning {
				if err := tx.Model(&SystemTask{}).
					Where("id = ? AND status = ?", task.ID, SystemTaskStatusRunning).
					Updates(map[string]any{
						"status":     SystemTaskStatusFailed,
						"active_key": nil,
						"error":      "task lease expired",
						"updated_at": now,
					}).Error; err != nil {
					return err
				}
			}
			return tx.Where(
				"type = ? AND task_id = ? AND locked_by = ? AND locked_until < ?",
				currentLock.Type,
				currentLock.TaskID,
				currentLock.LockedBy,
				now,
			).Delete(&SystemTaskLock{}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}
