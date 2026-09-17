package model

import (
	"errors"
	"math"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

func ChannelMonitorBalanceAccountID(upstreamType, raw string) (int, error) {
	if upstreamType != "custom" || raw == "" {
		return 0, nil
	}
	var config struct {
		Balance struct {
			Source    string `json:"source"`
			AccountID int    `json:"account_id"`
		} `json:"balance"`
	}
	if err := common.UnmarshalJsonStr(raw, &config); err != nil {
		return 0, err
	}
	if config.Balance.Source != "account" {
		return 0, nil
	}
	if config.Balance.AccountID <= 0 {
		return 0, errors.New("请选择有效的上游余额账户")
	}
	return config.Balance.AccountID, nil
}

func (monitor ChannelRatioMonitor) UsesIndependentUpstreamConfig() bool {
	id, err := ChannelMonitorBalanceAccountID(monitor.UpstreamType, monitor.CustomUpstreamConfig)
	return err == nil && id > 0
}

// Both sides of a reference change are locked before the monitor. This keeps
// account deletion and in-flight balance polls from racing a bind or detach.
func lockChannelMonitorBalanceSource(tx *gorm.DB, monitor ChannelRatioMonitor, upstreamType string, options ChannelRatioUpstreamOptions) (*ChannelMonitorUpstreamAccount, error) {
	id, err := ChannelMonitorBalanceAccountID(upstreamType, options.CustomUpstreamConfig)
	if err != nil {
		return nil, err
	}
	if id == 0 && !monitor.UsesIndependentUpstreamConfig() {
		return nil, nil
	}
	ids := []int{}
	if id > 0 {
		ids = append(ids, id)
	}
	if monitor.UpstreamAccountID > 0 && monitor.UpstreamAccountID != id {
		ids = append(ids, monitor.UpstreamAccountID)
	}
	sort.Ints(ids)
	var selected *ChannelMonitorUpstreamAccount
	for _, accountID := range ids {
		var account ChannelMonitorUpstreamAccount
		if err := lockForUpdate(tx).First(&account, accountID).Error; err != nil {
			return nil, err
		}
		if account.LeaseUntil > common.GetTimestamp() {
			return nil, errors.New("上游账户正在执行，请稍后重试")
		}
		if accountID == id {
			if options.BalanceAccountRevision != account.Revision {
				return nil, ErrUpstreamAccountChanged
			}
			var count int64
			if err := tx.Model(&ChannelRatioMonitor{}).Where("upstream_account_id = ? AND channel_id <> ?", id, monitor.ChannelId).Count(&count).Error; err != nil {
				return nil, err
			}
			if count >= 100 {
				return nil, errors.New("一个账户最多关联 100 个渠道")
			}
			selected = &account
		}
	}
	if monitor.UpstreamAccountID > 0 && monitor.UpstreamAccountID != id {
		var tasks []SystemTask
		if err := tx.Where("type = ?", UpstreamAutomationConfigType).Find(&tasks).Error; err != nil {
			return nil, err
		}
		for _, task := range tasks {
			var config struct {
				AccountID      int `json:"account_id"`
				RatioChannelID int `json:"ratio_channel_id"`
			}
			if err := common.UnmarshalJsonStr(task.Payload, &config); err != nil {
				return nil, err
			}
			if config.AccountID == monitor.UpstreamAccountID && config.RatioChannelID == monitor.ChannelId {
				return nil, errors.New("请先修改账户自动任务的倍率来源，再解除该渠道关联")
			}
		}
	}
	return selected, nil
}

// Membership and policy revisions fence stale Redis configurations without
// changing the wallet identity or the cost snapshots of in-flight requests.
func reviseChannelMonitorBalanceSources(tx *gorm.DB, previousID, nextID int) error {
	ids := []int{}
	if previousID > 0 {
		ids = append(ids, previousID)
	}
	if nextID > 0 && nextID != previousID {
		ids = append(ids, nextID)
	}
	sort.Ints(ids)
	for _, id := range ids {
		var account ChannelMonitorUpstreamAccount
		if err := lockForUpdate(tx).First(&account, id).Error; err != nil {
			return err
		}
		if account.Revision == math.MaxInt64 {
			return ErrUpstreamAccountChanged
		}
		account.Revision++
		if err := tx.Model(&account).Updates(map[string]any{"revision": account.Revision, "last_balance_check": 0}).Error; err != nil {
			return err
		}
		var members []ChannelRatioMonitor
		if err := lockForUpdate(tx).Where("upstream_account_id = ?", id).Find(&members).Error; err != nil {
			return err
		}
		for _, member := range members {
			if member.UpstreamRevision == math.MaxInt64 {
				return ErrChannelRatioMonitorConfigChanged
			}
			if err := tx.Model(&member).Updates(map[string]any{"upstream_account_revision": account.Revision, "upstream_revision": member.UpstreamRevision + 1}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
