package model

import (
	"context"
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const SystemTaskTypeChannelMonitorCustomAction = "channel_monitor_custom_action"

type ChannelMonitorCustomActionState struct {
	Triggered   bool    `json:"triggered"`
	Day         string  `json:"day"`
	Attempts    int     `json:"attempts"`
	LastAttempt int64   `json:"last_attempt"`
	AttemptID   string  `json:"attempt_id"`
	LastValue   float64 `json:"last_value"`
	Status      string  `json:"status"`
	Message     string  `json:"message"`
}

// Keep one durable task per channel, using the existing task schema. It is not
// queued for automatic retry: an uncertain upstream reset must never be replayed.
func UpdateChannelMonitorCustomActionState(ctx context.Context, channelID int, revision *int64, update func(ChannelRatioMonitor, map[string]ChannelMonitorCustomActionState) (bool, error)) error {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var monitor ChannelRatioMonitor
		if err := lockForUpdate(tx).Where("channel_id = ?", channelID).First(&monitor).Error; err != nil {
			return err
		}
		if revision != nil && (monitor.UpstreamType != "custom" || monitor.UpstreamRevision != *revision) {
			return ErrChannelRatioMonitorConfigChanged
		}
		taskID := "cm_custom_action_" + strconv.Itoa(channelID)
		var task SystemTask
		err := tx.Where("task_id = ?", taskID).First(&task).Error
		created := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !created {
			return err
		}
		states := make(map[string]ChannelMonitorCustomActionState)
		if task.State != "" {
			if err := common.UnmarshalJsonStr(task.State, &states); err != nil {
				return err
			}
			if states == nil {
				return errors.New("自定义接口执行状态无效，已停止执行")
			}
		}
		changed, err := update(monitor, states)
		if err != nil || !changed {
			return err
		}
		encoded, err := common.Marshal(states)
		if err != nil {
			return err
		}
		if created {
			return tx.Create(&SystemTask{TaskID: taskID, Type: SystemTaskTypeChannelMonitorCustomAction, Status: SystemTaskStatusSucceeded, State: string(encoded)}).Error
		}
		return tx.Model(&SystemTask{}).Where("id = ?", task.ID).Updates(map[string]interface{}{"state": string(encoded), "updated_at": common.GetTimestamp()}).Error
	})
}

func GetChannelMonitorCustomActionStates(channelID int) (map[string]ChannelMonitorCustomActionState, error) {
	var task SystemTask
	err := DB.Where("task_id = ?", "cm_custom_action_"+strconv.Itoa(channelID)).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var states map[string]ChannelMonitorCustomActionState
	err = common.UnmarshalJsonStr(task.State, &states)
	return states, err
}
