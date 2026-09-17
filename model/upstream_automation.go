package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const UpstreamAutomationConfigType = "upstream_automation_config"

type UpstreamAutomationEvent struct {
	ID      string `json:"id"`
	Time    int64  `json:"time"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func hasMigratedUpstreamAutomation(tx *gorm.DB, channelID int) (bool, error) {
	var row SystemTask
	err := tx.Where("task_id = ?", "cm_custom_action_"+strconv.Itoa(channelID)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil || row.Payload == "" {
		return false, err
	}
	var marker struct {
		AutomationID string `json:"upstream_automation_id"`
	}
	err = common.UnmarshalJsonStr(row.Payload, &marker)
	return marker.AutomationID != "", err
}

func validateChannelUpstreamAutomationOwnership(tx *gorm.DB, channelID int, upstreamType, raw string) error {
	if upstreamType != "custom" || raw == "" {
		return nil
	}
	var config struct {
		Actions []any `json:"actions"`
	}
	if err := common.UnmarshalJsonStr(raw, &config); err != nil {
		return err
	}
	if len(config.Actions) == 0 {
		return nil
	}
	migrated, err := hasMigratedUpstreamAutomation(tx, channelID)
	if err != nil {
		return err
	}
	if migrated {
		return errors.New("条件触发规则已迁移至上游自动任务，请刷新页面后编辑")
	}
	return nil
}

// Configuration and durable execution state reuse system_tasks; these rows are
// never queued, exposed through generic task APIs, or removed by task retention.
type UpstreamAutomationState struct {
	Revision   int64                                      `json:"revision"`
	LastCheck  int64                                      `json:"last_check"`
	NextCheck  int64                                      `json:"next_check"`
	LeaseID    string                                     `json:"lease_id,omitempty"`
	LeaseUntil int64                                      `json:"lease_until,omitempty"`
	Status     string                                     `json:"status"`
	Message    string                                     `json:"message"`
	Failures   int                                        `json:"failures"`
	Ratio      *float64                                   `json:"ratio,omitempty"`
	Balance    *float64                                   `json:"balance,omitempty"`
	Actions    map[string]ChannelMonitorCustomActionState `json:"actions"`
	History    []UpstreamAutomationEvent                  `json:"history"`
}

func ListUpstreamAutomations(ctx context.Context) ([]SystemTask, error) {
	var rows []SystemTask
	err := DB.WithContext(ctx).Where("type = ?", UpstreamAutomationConfigType).Order("id asc").Find(&rows).Error
	return rows, err
}

func GetUpstreamAutomation(ctx context.Context, id string) (SystemTask, error) {
	var row SystemTask
	err := DB.WithContext(ctx).Where("type = ? AND task_id = ?", UpstreamAutomationConfigType, id).First(&row).Error
	return row, err
}

func upstreamAutomationVariableGroupReferences(tx *gorm.DB, id int, lock bool) ([]SystemTask, error) {
	if !tx.Migrator().HasTable(&SystemTask{}) {
		return nil, nil
	}
	query := tx.Where("type = ?", UpstreamAutomationConfigType).Order("id asc")
	if lock {
		query = lockForUpdate(query)
	}
	var tasks []SystemTask
	if err := query.Find(&tasks).Error; err != nil {
		return nil, err
	}
	references := make([]SystemTask, 0)
	for _, task := range tasks {
		var config struct {
			AccountID    int `json:"account_id"`
			CustomConfig struct {
				VariableGroupID int `json:"variable_group_id"`
			} `json:"custom_config"`
		}
		if err := common.UnmarshalJsonStr(task.Payload, &config); err != nil {
			return nil, err
		}
		if config.AccountID > 0 {
			var account ChannelMonitorUpstreamAccount
			if err := tx.First(&account, config.AccountID).Error; err != nil {
				return nil, err
			}
			settings, err := account.MonitorSettings()
			if err != nil {
				return nil, err
			}
			config.CustomConfig.VariableGroupID, err = channelMonitorVariableGroupID(settings.CustomUpstreamConfig)
			if err != nil {
				return nil, err
			}
			if config.CustomConfig.VariableGroupID == id {
				var payload, custom, shared map[string]any
				if err := common.UnmarshalJsonStr(task.Payload, &payload); err != nil {
					return nil, err
				}
				custom, _ = payload["custom_config"].(map[string]any)
				if err := common.UnmarshalJsonStr(settings.CustomUpstreamConfig, &shared); err != nil {
					return nil, err
				}
				shared["actions"] = custom["actions"]
				payload["custom_config"], payload["base_url"], payload["proxy"] = shared, settings.UpstreamBaseURL, account.Proxy
				raw, err := common.Marshal(payload)
				if err != nil {
					return nil, err
				}
				task.Payload = string(raw)
			}
		}
		if config.CustomConfig.VariableGroupID == id {
			references = append(references, task)
		}
	}
	return references, nil
}

// The same row lock covers edits, reservations and completion. The process lock
// provides SQLite's equivalent serialization; cross-process writes still use a
// transaction and can safely fail busy without sending an upstream action.
func MutateUpstreamAutomation(ctx context.Context, id string, create bool, groupID int, groupRevision int64, update func(*SystemTask) error, accountIDs ...int) (SystemTask, error) {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	var row SystemTask
	err := DB.WithContext(ctx).Session(&gorm.Session{Logger: DB.Logger.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		if len(accountIDs) > 0 && accountIDs[0] > 0 {
			var account ChannelMonitorUpstreamAccount
			if err := lockForUpdate(tx).First(&account, accountIDs[0]).Error; err != nil {
				return err
			}
			if account.LeaseUntil > common.GetTimestamp() {
				return errors.New("上游账户正在执行，请稍后重试")
			}
			var tasks []SystemTask
			if err := tx.Where("type = ?", UpstreamAutomationConfigType).Find(&tasks).Error; err != nil {
				return err
			}
			var members []int
			if err := tx.Model(&ChannelRatioMonitor{}).Where("upstream_account_id = ?", account.ID).Pluck("channel_id", &members).Error; err != nil {
				return err
			}
			memberSet := make(map[int]bool, len(members))
			for _, id := range members {
				memberSet[id] = true
			}
			for _, task := range tasks {
				var config struct {
					AccountID    int    `json:"account_id"`
					ChannelIDs   []int  `json:"channel_ids"`
					MergedInto   string `json:"merged_into"`
					CustomConfig struct {
						Balance struct {
							Source string `json:"source"`
						} `json:"balance"`
					} `json:"custom_config"`
				}
				if err := common.UnmarshalJsonStr(task.Payload, &config); err != nil {
					return err
				}
				if config.AccountID == account.ID && task.TaskID != id {
					return errors.New("该账户已有自动任务，请在同一任务中管理规则")
				}
				if config.AccountID == 0 && config.MergedInto == "" && task.TaskID != id && config.CustomConfig.Balance.Source != "account" {
					for _, channelID := range config.ChannelIDs {
						if memberSet[channelID] {
							return errors.New("账户渠道仍有旧自动任务，请先合并以保留全部执行次数和冷却状态")
						}
					}
				}
			}
		}
		// Group edits take the same group -> task lock order. Runtime state
		// updates and local credential refreshes do not acquire a group lock.
		if groupID > 0 {
			var group ChannelMonitorVariableGroup
			if err := lockForUpdate(tx).First(&group, groupID).Error; err != nil {
				return err
			}
			if group.Revision != groupRevision {
				return ErrChannelMonitorVariableGroupChanged
			}
		}
		if len(accountIDs) > 1 && accountIDs[1] > 0 {
			var account ChannelMonitorUpstreamAccount
			if err := lockForUpdate(tx).First(&account, accountIDs[1]).Error; err != nil {
				return errors.New("关联的上游账户不存在，请重新选择余额来源")
			}
		}
		err := lockForUpdate(tx).Where("type = ? AND task_id = ?", UpstreamAutomationConfigType, id).First(&row).Error
		missing := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && (!missing || !create) {
			return err
		}
		if create && !missing {
			return errors.New("任务已存在，请刷新后重试")
		}
		if missing {
			row = SystemTask{TaskID: id, Type: UpstreamAutomationConfigType, Status: SystemTaskStatusSucceeded}
		}
		previousPayload := row.Payload
		if err := update(&row); err != nil {
			return err
		}
		if row.Payload != previousPayload {
			var input struct {
				CustomConfig struct {
					VariableGroupID int `json:"variable_group_id"`
				} `json:"custom_config"`
			}
			if err := common.UnmarshalJsonStr(row.Payload, &input); err != nil {
				return err
			}
			if input.CustomConfig.VariableGroupID != groupID {
				return ErrChannelMonitorVariableGroupChanged
			}
		}
		if missing {
			return tx.Create(&row).Error
		}
		return tx.Model(&SystemTask{}).Where("id = ?", row.ID).Updates(map[string]any{
			"payload": row.Payload, "state": row.State, "updated_at": common.GetTimestamp(),
		}).Error
	})
	return row, err
}

func DeleteUpstreamAutomation(ctx context.Context, id string, revision int64) error {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row SystemTask
		if err := lockForUpdate(tx).Where("type = ? AND task_id = ?", UpstreamAutomationConfigType, id).First(&row).Error; err != nil {
			return err
		}
		var state UpstreamAutomationState
		if err := common.UnmarshalJsonStr(row.State, &state); err != nil {
			return err
		}
		if state.Revision != revision || state.LeaseUntil > common.GetTimestamp() {
			return errors.New("任务正在执行或配置已变化，请刷新后重试")
		}
		return tx.Delete(&row).Error
	})
}

// Migration is atomic with disabling the channel-owned rules. An in-flight
// legacy attempt keeps its counters and requires confirmation before replay.
func ImportChannelUpstreamAutomation(ctx context.Context, expected ChannelRatioMonitor, groupRevision int64, payload, remainingConfig string) error {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	return DB.WithContext(ctx).Session(&gorm.Session{Logger: DB.Logger.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		if err := lockChannelMonitorVariableGroupReference(tx, expected.UpstreamType, expected.CustomUpstreamConfig, groupRevision); err != nil {
			return err
		}
		var monitor ChannelRatioMonitor
		if err := lockForUpdate(tx).Where("channel_id = ?", expected.ChannelId).First(&monitor).Error; err != nil {
			return err
		}
		if monitor.UpstreamRevision != expected.UpstreamRevision || monitor.CustomUpstreamConfig != expected.CustomUpstreamConfig {
			return ErrChannelRatioMonitorConfigChanged
		}
		if err := validateChannelUpstreamAutomationOwnership(tx, monitor.ChannelId, monitor.UpstreamType, monitor.CustomUpstreamConfig); err != nil {
			return err
		}
		var previous SystemTask
		err := tx.Where("task_id = ?", "cm_custom_action_"+strconv.Itoa(monitor.ChannelId)).First(&previous).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		state := UpstreamAutomationState{Revision: 1, Status: "waiting", Message: "已从渠道迁移，等待独立检查", Actions: map[string]ChannelMonitorCustomActionState{}}
		if previous.State != "" {
			if err := common.UnmarshalJsonStr(previous.State, &state.Actions); err != nil {
				return err
			}
			for id, action := range state.Actions {
				if action.Status == "running" {
					// Preserve pending confirmation instead of replaying on migration.
					action.NeedsConfirmation = true
				}
				if action.Status == "failed" {
					action.NeedsConfirmation = true
				}
				state.Actions[id] = action
			}
		}
		encoded, err := common.Marshal(state)
		if err != nil {
			return err
		}
		row := SystemTask{TaskID: fmt.Sprintf("ua_channel_%d_%d", monitor.ChannelId, monitor.UpstreamRevision), Type: UpstreamAutomationConfigType, Status: SystemTaskStatusSucceeded, Payload: payload, State: string(encoded)}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		marker, err := common.Marshal(map[string]string{"upstream_automation_id": row.TaskID})
		if err != nil {
			return err
		}
		if previous.ID == 0 {
			previous = SystemTask{TaskID: "cm_custom_action_" + strconv.Itoa(monitor.ChannelId), Type: SystemTaskTypeChannelMonitorCustomAction, Status: SystemTaskStatusSucceeded, Payload: string(marker), State: "{}"}
			if err := tx.Create(&previous).Error; err != nil {
				return err
			}
		} else if err := tx.Model(&previous).Update("payload", string(marker)).Error; err != nil {
			return err
		}
		return tx.Model(&ChannelRatioMonitor{}).Where("channel_id = ?", monitor.ChannelId).Updates(map[string]any{
			"custom_upstream_config": remainingConfig, "upstream_revision": monitor.UpstreamRevision + 1,
		}).Error
	})
}
