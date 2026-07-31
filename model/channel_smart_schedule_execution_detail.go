package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

type ChannelSmartScheduleExecutionDetail struct {
	Id              int64  `gorm:"primaryKey"`
	TaskId          string `gorm:"type:varchar(64);not null;uniqueIndex:idx_schedule_execution_detail_task_item;index"`
	AdjustmentIndex int    `gorm:"not null;uniqueIndex:idx_schedule_execution_detail_task_item"`
	Payload         string `gorm:"type:text;not null"`
	CreatedAt       int64  `gorm:"bigint;not null;index"`
}

type ChannelSmartScheduleExecutionDetailInput struct {
	AdjustmentIndex int
	Payload         any
}

type ChannelSmartScheduleExecutionDetailPayload struct {
	AdjustmentIndex int
	Payload         string
}

func SaveChannelSmartScheduleExecutionDetails(
	taskId string,
	inputs []ChannelSmartScheduleExecutionDetailInput,
) error {
	taskId = strings.TrimSpace(taskId)
	if taskId == "" {
		return errors.New("智能调度任务 ID 不能为空")
	}
	now := common.GetTimestamp()
	rows := make([]ChannelSmartScheduleExecutionDetail, 0, len(inputs))
	for _, input := range inputs {
		if input.AdjustmentIndex < 0 || input.Payload == nil {
			continue
		}
		encoded, err := common.Marshal(input.Payload)
		if err != nil {
			return fmt.Errorf("编码智能调度执行明细失败: %w", err)
		}
		rows = append(rows, ChannelSmartScheduleExecutionDetail{
			TaskId:          taskId,
			AdjustmentIndex: input.AdjustmentIndex,
			Payload:         string(encoded),
			CreatedAt:       now,
		})
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ?", taskId).
			Delete(&ChannelSmartScheduleExecutionDetail{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.CreateInBatches(&rows, 100).Error
	})
}

func GetChannelSmartScheduleExecutionDetails(
	taskIds []string,
) (map[string][]ChannelSmartScheduleExecutionDetailPayload, error) {
	result := make(map[string][]ChannelSmartScheduleExecutionDetailPayload)
	if len(taskIds) == 0 {
		return result, nil
	}
	var rows []ChannelSmartScheduleExecutionDetail
	if err := DB.Where("task_id IN ?", taskIds).
		Order("task_id asc, adjustment_index asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.TaskId] = append(result[row.TaskId], ChannelSmartScheduleExecutionDetailPayload{
			AdjustmentIndex: row.AdjustmentIndex,
			Payload:         row.Payload,
		})
	}
	return result, nil
}
