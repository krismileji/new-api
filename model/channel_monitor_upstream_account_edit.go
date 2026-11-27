package model

import (
	"context"
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const UpstreamAccountEditConfigType = "upstream_account_edit_config"

var errUpstreamAccountEditBusy = errors.New("上游账户正在执行，配置等待应用")

// Credentials stay in storage and must never be exposed by generic task APIs.
type upstreamAccountEdit struct {
	Account           ChannelMonitorUpstreamAccount
	Settings          string
	BalanceKey        string
	ExpectedSettings  string
	MemberIDs         []int
	ExpectedRevisions []map[int]int64
}

func upstreamAccountEditTaskID(id int) string { return "cm_account_edit_" + strconv.Itoa(id) }

// Keep rotated credentials when an edit only changes other account settings.
// Custom request edits conflict if that same request was refreshed meanwhile.
func rebaseUpstreamAccountEditCredentials(account, expected *ChannelMonitorUpstreamAccount, current ChannelMonitorUpstreamAccount) error {
	if account.Settings == expected.Settings {
		account.Settings, expected.Settings = current.Settings, current.Settings
		return nil
	}
	previous, err := expected.MonitorSettings()
	if err != nil {
		return err
	}
	desired, err := account.MonitorSettings()
	if err != nil {
		return err
	}
	latest, err := current.MonitorSettings()
	if err != nil {
		return err
	}
	if desired.UpstreamType != previous.UpstreamType || desired.UpstreamBaseURL != previous.UpstreamBaseURL || desired.UpstreamAuthType != previous.UpstreamAuthType || desired.UpstreamAccount != previous.UpstreamAccount {
		return ErrUpstreamAccountChanged
	}
	if desired.UpstreamAccessToken == previous.UpstreamAccessToken {
		desired.UpstreamAccessToken = latest.UpstreamAccessToken
	}
	if desired.UpstreamRefreshToken == previous.UpstreamRefreshToken {
		desired.UpstreamRefreshToken = latest.UpstreamRefreshToken
	}
	if desired.UpstreamPassword == previous.UpstreamPassword {
		desired.UpstreamPassword = latest.UpstreamPassword
	}
	if desired.CustomUpstreamConfig == previous.CustomUpstreamConfig {
		desired.CustomUpstreamConfig = latest.CustomUpstreamConfig
	} else if latest.CustomUpstreamConfig != previous.CustomUpstreamConfig {
		return ErrUpstreamAccountChanged
	}
	raw, err := common.Marshal(desired)
	if err != nil {
		return err
	}
	account.Settings, expected.Settings = string(raw), current.Settings
	return nil
}

func lockPendingUpstreamAccountEdit(tx *gorm.DB, id int, expected *SystemTask) error {
	var row SystemTask
	err := lockForUpdate(tx).Where("task_id = ? AND type = ?", upstreamAccountEditTaskID(id), UpstreamAccountEditConfigType).First(&row).Error
	if expected == nil && errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if expected != nil && (row.Payload != expected.Payload || row.UpdatedAt != expected.UpdatedAt || row.Status != SystemTaskStatusPending) {
		return ErrUpstreamAccountChanged
	}
	return nil
}

func stageUpstreamAccountEdit(tx *gorm.DB, account, expected ChannelMonitorUpstreamAccount, members []int, revisions []map[int]int64) error {
	payload, err := common.Marshal(upstreamAccountEdit{Account: account, Settings: account.Settings, BalanceKey: account.BalanceKey,
		ExpectedSettings: expected.Settings, MemberIDs: members, ExpectedRevisions: revisions})
	if err != nil {
		return err
	}
	var row SystemTask
	err = lockForUpdate(tx).Where("task_id = ?", upstreamAccountEditTaskID(account.ID)).First(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = SystemTask{TaskID: upstreamAccountEditTaskID(account.ID), Type: UpstreamAccountEditConfigType}
	}
	row.Payload, row.Status, row.Error, row.UpdatedAt = string(payload), SystemTaskStatusPending, "", common.GetTimestamp()
	return tx.Save(&row).Error
}

func UpstreamAccountEditStatus(ctx context.Context, id int) (SystemTaskStatus, string, error) {
	var rows []SystemTask
	err := DB.WithContext(ctx).Select("status", "error").Where("task_id = ? AND type = ?", upstreamAccountEditTaskID(id), UpstreamAccountEditConfigType).Limit(1).Find(&rows).Error
	if err != nil || len(rows) == 0 {
		return "", "", err
	}
	return rows[0].Status, rows[0].Error, nil
}

// A scheduled pass also recovers staged edits after restart or lease expiry.
func ApplyPendingUpstreamAccountEdits(ctx context.Context) ([]ChannelRatioMonitor, error) {
	var rows []SystemTask
	if err := DB.WithContext(ctx).Where("type = ? AND status = ?", UpstreamAccountEditConfigType, SystemTaskStatusPending).Order("id ASC").Limit(100).Find(&rows).Error; err != nil {
		return nil, err
	}
	var changed []ChannelRatioMonitor
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return changed, err
		}
		var edit upstreamAccountEdit
		err := common.UnmarshalJsonStr(row.Payload, &edit)
		if err == nil {
			edit.Account.Settings, edit.Account.BalanceKey = edit.Settings, edit.BalanceKey
			expected := edit.Account
			expected.Settings = edit.ExpectedSettings
			var members []ChannelRatioMonitor
			members, err = saveChannelMonitorUpstreamAccount(ctx, &edit.Account, &expected, edit.MemberIDs, &row, edit.ExpectedRevisions...)
			if err == nil {
				changed = append(changed, members...)
				continue
			}
		}
		if errors.Is(err, errUpstreamAccountEditBusy) {
			continue
		}
		// Compare the full payload in Go, including on case-insensitive MySQL.
		channelStatusLock.Lock()
		finishErr := DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
			var current SystemTask
			if err := lockForUpdate(tx).First(&current, row.ID).Error; err != nil {
				return err
			}
			if current.Payload != row.Payload || current.Status != SystemTaskStatusPending {
				return nil
			}
			return tx.Model(&current).Updates(map[string]any{"status": SystemTaskStatusFailed, "payload": "", "error": "暂存配置应用失败，配置或凭据可能已变化，请重新打开配置后保存", "updated_at": common.GetTimestamp()}).Error
		})
		channelStatusLock.Unlock()
		if finishErr != nil && !errors.Is(finishErr, gorm.ErrRecordNotFound) {
			return changed, finishErr
		}
	}
	return changed, nil
}
