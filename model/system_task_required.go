package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errSystemTaskStateChanged = errors.New("系统任务状态已变化")

// IsSystemTaskRunning reports whether a claimed task of the requested type is
// currently executing. Pending successor tasks are intentionally excluded.
func IsSystemTaskRunning(taskType string) (bool, error) {
	taskType = strings.TrimSpace(taskType)
	if taskType == "" {
		return false, nil
	}
	var count int64
	err := DB.Model(&SystemTask{}).
		Where("type = ? AND status = ?", taskType, SystemTaskStatusRunning).
		Count(&count).Error
	return count > 0, err
}

// EnqueueRequiredSystemTask guarantees that payload will be handled by a
// future run. A pending task is upgraded in place; a running task keeps its
// lease and gets one pending successor.
func EnqueueRequiredSystemTask(taskType string, payload any) (*SystemTask, bool, error) {
	basePayloadText, err := marshalSystemTaskJSON(payload)
	if err != nil {
		return nil, false, err
	}

	var lastCreateErr error
	for range 5 {
		var queuedTask *SystemTask
		created := false
		err = DB.Transaction(func(tx *gorm.DB) error {
			queuedTask, created, err = enqueueRequiredSystemTaskTx(tx, taskType, payload, basePayloadText)
			return err
		})
		if err == nil {
			return queuedTask, created, nil
		}
		if errors.Is(err, errSystemTaskStateChanged) {
			continue
		}
		lastCreateErr = err
	}
	if lastCreateErr != nil {
		return nil, false, lastCreateErr
	}
	return nil, false, errSystemTaskStateChanged
}

func enqueueRequiredSystemTaskTx(
	tx *gorm.DB,
	taskType string,
	payload any,
	basePayloadText string,
) (*SystemTask, bool, error) {
	payloadText := basePayloadText
	var active SystemTask
	query := lockForUpdate(tx).
		Where("type = ? AND status IN ?", taskType, activeSystemTaskStatuses()).
		Order("id desc")
	if queryErr := query.First(&active).Error; queryErr == nil {
		if active.Status == SystemTaskStatusPending {
			if merger, ok := payload.(interface {
				MergeRequiredSystemTaskPayload(existing string) (string, error)
			}); ok {
				merged, err := merger.MergeRequiredSystemTaskPayload(active.Payload)
				if err != nil {
					return nil, false, err
				}
				payloadText = merged
			}
			result := tx.Model(&SystemTask{}).
				Where("id = ? AND status = ?", active.ID, SystemTaskStatusPending).
				Updates(map[string]any{
					"payload":    payloadText,
					"updated_at": common.GetTimestamp(),
				})
			if result.Error != nil {
				return nil, false, result.Error
			}
			if result.RowsAffected == 0 {
				return nil, false, errSystemTaskStateChanged
			}
			active.Payload = payloadText
			return &active, false, nil
		}

		result := tx.Model(&SystemTask{}).
			Where("id = ? AND status = ? AND active_key = ?", active.ID, SystemTaskStatusRunning, taskType).
			Update("active_key", nil)
		if result.Error != nil {
			return nil, false, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, false, errSystemTaskStateChanged
		}
	} else if !errors.Is(queryErr, gorm.ErrRecordNotFound) {
		return nil, false, queryErr
	}

	taskID, err := GenerateSystemTaskID()
	if err != nil {
		return nil, false, err
	}
	queuedTask := &SystemTask{
		TaskID:    taskID,
		Type:      taskType,
		Status:    SystemTaskStatusPending,
		ActiveKey: &taskType,
		Payload:   payloadText,
	}
	createResult := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(queuedTask)
	if createResult.Error != nil {
		return nil, false, createResult.Error
	}
	if createResult.RowsAffected == 0 {
		return nil, false, errSystemTaskStateChanged
	}
	return queuedTask, true, nil
}
