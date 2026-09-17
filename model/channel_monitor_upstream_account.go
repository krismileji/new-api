package model

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Shared settings are authoritative here. Channel rows contain a transactional
// projection for existing pricing and policy readers; only account writes may
// change that projection. Ratios, groups and request-time cost snapshots remain
// channel-owned.
type ChannelMonitorUpstreamAccount struct {
	SourceChannelID        int      `json:"-" gorm:"-"`
	ExpectedSourceSettings string   `json:"-" gorm:"-"`
	ID                     int      `json:"id" gorm:"primaryKey"`
	Name                   string   `json:"name" gorm:"size:80;not null"`
	Revision               int64    `json:"revision" gorm:"not null"`
	Settings               string   `json:"-" gorm:"type:text;not null"`
	Proxy                  string   `json:"proxy" gorm:"type:text"`
	BalanceKey             string   `json:"-" gorm:"type:text"`
	Balance                *float64 `json:"balance"`
	LastBalanceTime        int64    `json:"last_balance_time"`
	LastBalanceError       string   `json:"last_balance_error" gorm:"size:255"`
	LeaseID                string   `json:"-" gorm:"size:64"`
	LeaseUntil             int64    `json:"-"`
	RefreshIntervalMinutes int      `json:"refresh_interval_minutes"`
	LastBalanceCheck       int64    `json:"last_balance_check"`
}

// This storage-only structure intentionally includes credentials. API responses
// must use the existing sanitized monitor view, never serialize Settings.
type ChannelMonitorAccountSettings struct {
	UpstreamType                string
	UpstreamBaseURL             string
	UpstreamAuthType            string
	UpstreamUserId              int
	UpstreamAccessToken         string
	UpstreamRefreshToken        string
	UpstreamAccount             string
	UpstreamPassword            string
	CostConversion              string
	CustomUpstreamConfig        string
	UpstreamBalanceSyncDisabled bool
	BalanceWarningThreshold     *float64
	BalanceAutoDisableThreshold *float64
}

var ErrUpstreamAccountChanged = errors.New("上游账户配置已变化，请刷新后重试")

func ChannelMonitorAccountSettingsFromMonitor(m ChannelRatioMonitor) ChannelMonitorAccountSettings {
	return ChannelMonitorAccountSettings{
		UpstreamType: m.UpstreamType, UpstreamBaseURL: m.UpstreamBaseURL,
		UpstreamAuthType: m.UpstreamAuthType, UpstreamUserId: m.UpstreamUserId,
		UpstreamAccessToken: m.UpstreamAccessToken, UpstreamRefreshToken: m.UpstreamRefreshToken,
		UpstreamAccount: m.UpstreamAccount, UpstreamPassword: m.UpstreamPassword,
		CostConversion: m.CostConversion, CustomUpstreamConfig: m.CustomUpstreamConfig,
		UpstreamBalanceSyncDisabled: m.UpstreamBalanceSyncDisabled,
		BalanceWarningThreshold:     m.BalanceWarningThreshold, BalanceAutoDisableThreshold: m.BalanceAutoDisableThreshold,
	}
}

func (account ChannelMonitorUpstreamAccount) MonitorSettings() (ChannelMonitorAccountSettings, error) {
	var settings ChannelMonitorAccountSettings
	err := common.UnmarshalJsonStr(account.Settings, &settings)
	return settings, err
}

func (settings ChannelMonitorAccountSettings) Apply(m *ChannelRatioMonitor) error {
	if m.UsesIndependentUpstreamConfig() {
		m.CostConversion = settings.CostConversion
		return nil
	}
	custom := settings.CustomUpstreamConfig
	if settings.UpstreamType == "custom" {
		var shared, local map[string]json.RawMessage
		if err := common.UnmarshalJsonStr(custom, &shared); err != nil {
			return err
		}
		if m.CustomUpstreamConfig != "" {
			if err := common.UnmarshalJsonStr(m.CustomUpstreamConfig, &local); err != nil {
				return err
			}
		} else if m.ChannelId > 0 {
			ratio, err := common.Marshal(map[string]any{"source": "fixed", "fixed_value": m.Ratio})
			if err != nil {
				return err
			}
			local = map[string]json.RawMessage{"ratio": ratio}
		}
		if ratio, ok := local["ratio"]; ok {
			shared["ratio"] = ratio
		}
		// Conditional actions have an independent durable owner. Binding must
		// never introduce a second executor on a channel.
		delete(shared, "actions")
		raw, err := common.Marshal(shared)
		if err != nil {
			return err
		}
		custom = string(raw)
	}
	m.UpstreamType, m.UpstreamBaseURL = settings.UpstreamType, settings.UpstreamBaseURL
	m.UpstreamAuthType, m.UpstreamUserId = settings.UpstreamAuthType, settings.UpstreamUserId
	m.UpstreamAccessToken, m.UpstreamRefreshToken = settings.UpstreamAccessToken, settings.UpstreamRefreshToken
	m.UpstreamAccount, m.UpstreamPassword = settings.UpstreamAccount, settings.UpstreamPassword
	m.CostConversion, m.CustomUpstreamConfig = settings.CostConversion, custom
	m.UpstreamBalanceSyncDisabled = settings.UpstreamBalanceSyncDisabled
	m.BalanceWarningThreshold, m.BalanceAutoDisableThreshold = settings.BalanceWarningThreshold, settings.BalanceAutoDisableThreshold
	return nil
}

func (settings ChannelMonitorAccountSettings) SharedJSON() (string, error) {
	if settings.CustomUpstreamConfig != "" {
		var custom map[string]json.RawMessage
		if err := common.UnmarshalJsonStr(settings.CustomUpstreamConfig, &custom); err != nil {
			return "", err
		}
		delete(custom, "ratio")
		delete(custom, "actions")
		raw, err := common.Marshal(custom)
		if err != nil {
			return "", err
		}
		settings.CustomUpstreamConfig = string(raw)
	}
	raw, err := common.Marshal(settings)
	return string(raw), err
}

func GetChannelMonitorUpstreamAccount(ctx context.Context, id int) (ChannelMonitorUpstreamAccount, error) {
	var account ChannelMonitorUpstreamAccount
	err := DB.WithContext(ctx).First(&account, id).Error
	return account, err
}

func ListChannelMonitorUpstreamAccounts(ctx context.Context) ([]ChannelMonitorUpstreamAccount, error) {
	accounts := make([]ChannelMonitorUpstreamAccount, 0)
	err := DB.WithContext(ctx).Order("id ASC").Find(&accounts).Error
	return accounts, err
}

func GetUpstreamAccountMonitors(ctx context.Context, id int) ([]ChannelRatioMonitor, error) {
	monitors := make([]ChannelRatioMonitor, 0)
	err := DB.WithContext(ctx).Where("upstream_account_id = ?", id).Order("channel_id ASC").Find(&monitors).Error
	return monitors, err
}

// SaveChannelMonitorUpstreamAccount compares the complete expected row, so a
// concurrent credential rotation cannot be overwritten by an editor. Membership
// and shared settings are committed together; omitted member IDs keep membership.
func SaveChannelMonitorUpstreamAccount(ctx context.Context, account *ChannelMonitorUpstreamAccount, expected *ChannelMonitorUpstreamAccount, memberIDs []int, expectedRevisions ...map[int]int64) ([]ChannelRatioMonitor, error) {
	account.Name = strings.TrimSpace(account.Name)
	if account.Name == "" || utf8.RuneCountInString(account.Name) > 80 {
		return nil, errors.New("账户名称须为 1 到 80 个字符")
	}
	settings, err := account.MonitorSettings()
	if err != nil {
		return nil, err
	}
	if settings.UpstreamType == "" {
		return nil, errors.New("请先配置上游监控")
	}
	if id, err := ChannelMonitorBalanceAccountID(settings.UpstreamType, settings.CustomUpstreamConfig); err != nil || id > 0 {
		return nil, errors.New("上游账户必须配置自身余额来源，不能关联另一账户")
	}
	if len(memberIDs) > 100 {
		return nil, errors.New("一个账户最多关联 100 个渠道")
	}
	if account.RefreshIntervalMinutes < 0 || account.RefreshIntervalMinutes > 10080 {
		return nil, errors.New("余额刷新间隔须为 0 到 10080 分钟，0 表示关闭自动刷新")
	}
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	var changed []ChannelRatioMonitor
	err = DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		if err := lockChannelMonitorVariableGroupReference(tx, settings.UpstreamType, settings.CustomUpstreamConfig, 0); err != nil {
			return err
		}
		if account.ID != 0 {
			var current ChannelMonitorUpstreamAccount
			if err := lockForUpdate(tx).First(&current, account.ID).Error; err != nil {
				return err
			}
			if expected == nil || current.Revision != expected.Revision || current.Settings != expected.Settings || current.Revision == math.MaxInt64 {
				return ErrUpstreamAccountChanged
			}
			if current.LeaseUntil > common.GetTimestamp() {
				return errors.New("上游账户正在执行，请稍后重试")
			}
			account.Revision = current.Revision + 1
		} else {
			if account.SourceChannelID > 0 {
				var source ChannelRatioMonitor
				if err := lockForUpdate(tx).Where("channel_id = ?", account.SourceChannelID).First(&source).Error; err != nil {
					return err
				}
				raw, err := ChannelMonitorAccountSettingsFromMonitor(source).SharedJSON()
				if err != nil {
					return err
				}
				if raw != account.ExpectedSourceSettings {
					return ErrChannelRatioMonitorConfigChanged
				}
			}
			account.Revision = 1
		}
		economic, err := lockChannelMonitorEconomicRevisionTx(tx)
		if err != nil {
			return err
		}
		account.Balance, account.LastBalanceTime, account.LastBalanceError = nil, 0, "等待账户余额同步"
		account.LastBalanceCheck, account.LeaseUntil, account.LeaseID = 0, 0, ""
		if err := tx.Save(account).Error; err != nil {
			return err
		}
		var members []ChannelRatioMonitor
		if err := lockForUpdate(tx).Where("upstream_account_id = ?", account.ID).Order("channel_id ASC").Find(&members).Error; err != nil {
			return err
		}
		selected := make(map[int]bool)
		for _, id := range memberIDs {
			if id <= 0 || selected[id] {
				return errors.New("关联渠道无效或重复")
			}
			selected[id] = true
			if err := lockChannelForDependentWriteTx(tx, id); err != nil {
				return err
			}
			var m ChannelRatioMonitor
			if err := lockForUpdate(tx).Where("channel_id = ?", id).First(&m).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				m.ChannelId = id
			} else if err != nil {
				return err
			}
			if m.UpstreamAccountID != 0 && m.UpstreamAccountID != account.ID {
				return errors.New("渠道已属于其他账户，请先解除关联")
			}
			if len(expectedRevisions) > 0 && expectedRevisions[0][id] != m.UpstreamRevision {
				return ErrChannelRatioMonitorConfigChanged
			}
			if m.UpstreamAccountID == 0 {
				members = append(members, m)
			}
		}
		for _, m := range members {
			if len(expectedRevisions) > 0 && expectedRevisions[0][m.ChannelId] != m.UpstreamRevision {
				return ErrChannelRatioMonitorConfigChanged
			}
			if m.UpstreamRevision == math.MaxInt64 {
				return ErrChannelRatioMonitorConfigChanged
			}
			if memberIDs != nil && !selected[m.ChannelId] {
				if m.UsesIndependentUpstreamConfig() {
					return errors.New("仅关联余额的渠道请先在上游配置中切换为自定义余额来源")
				}
				m.UpstreamAccountID, m.UpstreamAccountRevision = 0, 0
			} else {
				if err := settings.Apply(&m); err != nil {
					return err
				}
				m.UpstreamAccountID, m.UpstreamAccountRevision = account.ID, account.Revision
			}
			m.UpstreamRevision++
			m.UpstreamBalance, m.LastBalanceTime = nil, 0
			m.LastBalanceError = "等待账户余额同步"
			m.BalanceAlertNotified, m.BalanceFailureAlertNotified = false, false
			if err := tx.Save(&m).Error; err != nil {
				return err
			}
			changed = append(changed, m)
		}
		if memberIDs != nil {
			var tasks []SystemTask
			if err := lockForUpdate(tx).Where("type = ?", UpstreamAutomationConfigType).Order("id ASC").Find(&tasks).Error; err != nil {
				return err
			}
			for _, task := range tasks {
				var config struct {
					AccountID      int   `json:"account_id"`
					RatioChannelID int   `json:"ratio_channel_id"`
					ChannelIDs     []int `json:"channel_ids"`
				}
				if err := common.UnmarshalJsonStr(task.Payload, &config); err != nil {
					return err
				}
				if config.AccountID == account.ID && config.RatioChannelID > 0 && !selected[config.RatioChannelID] {
					return errors.New("请先修改账户自动任务的倍率来源，再解除该渠道关联")
				}
				for _, id := range config.ChannelIDs {
					if !selected[id] {
						continue
					}
					var state UpstreamAutomationState
					if err := common.UnmarshalJsonStr(task.State, &state); err != nil {
						return err
					}
					if state.LeaseUntil > common.GetTimestamp() {
						return errors.New("关联渠道的自动任务正在执行，请稍后重试")
					}
				}
			}
		}
		return economic.bump(tx)
	})
	return changed, err
}

func DeleteChannelMonitorUpstreamAccount(ctx context.Context, id int, revision int64) error {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account ChannelMonitorUpstreamAccount
		if err := lockForUpdate(tx).First(&account, id).Error; err != nil {
			return err
		}
		if account.Revision != revision {
			return ErrUpstreamAccountChanged
		}
		if account.LeaseUntil > common.GetTimestamp() {
			return errors.New("上游账户正在执行，请稍后重试")
		}
		var count int64
		if err := tx.Model(&ChannelRatioMonitor{}).Where("upstream_account_id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("账户仍有关联渠道，请先解除关联")
		}
		var tasks []SystemTask
		if err := tx.Where("type = ?", UpstreamAutomationConfigType).Find(&tasks).Error; err != nil {
			return err
		}
		for _, task := range tasks {
			var config struct {
				AccountID    int             `json:"account_id"`
				CustomConfig json.RawMessage `json:"custom_config"`
			}
			if err := common.UnmarshalJsonStr(task.Payload, &config); err != nil {
				return err
			}
			balanceAccountID, err := ChannelMonitorBalanceAccountID("custom", string(config.CustomConfig))
			if err != nil {
				return err
			}
			if config.AccountID == id || balanceAccountID == id {
				return errors.New("账户仍有关联自动任务，请先解除任务关联")
			}
		}
		return tx.Delete(&account).Error
	})
}
