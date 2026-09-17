package model

import (
	"context"
	"errors"
	"math"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Account leases serialize polling and actions across processes, including
// installations without Redis. Every result is fenced by revision and lease.
func AcquireUpstreamAccountLease(ctx context.Context, id int, revision int64, token string, until int64) (bool, error) {
	r := DB.WithContext(ctx).Model(&ChannelMonitorUpstreamAccount{}).
		Where("id = ? AND revision = ? AND (lease_until IS NULL OR lease_until <= ?)", id, revision, common.GetTimestamp()).
		Updates(map[string]any{"lease_id": token, "lease_until": until})
	return r.RowsAffected == 1, r.Error
}

func ReleaseUpstreamAccountLease(ctx context.Context, id int, token string) error {
	return DB.WithContext(ctx).Model(&ChannelMonitorUpstreamAccount{}).Where("id = ? AND lease_id = ?", id, token).
		Updates(map[string]any{"lease_id": "", "lease_until": 0}).Error
}

func RecordUpstreamAccountBalance(ctx context.Context, id int, revision int64, token string, balance *float64, message string) error {
	if balance != nil && (math.IsNaN(*balance) || math.IsInf(*balance, 0)) {
		return errors.New("上游余额不是有效数字")
	}
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account ChannelMonitorUpstreamAccount
		if err := lockForUpdate(tx).First(&account, id).Error; err != nil {
			return err
		}
		if account.Revision != revision || account.LeaseID != token || account.LeaseUntil <= common.GetTimestamp() {
			return ErrUpstreamAccountChanged
		}
		if len([]rune(message)) > 255 {
			message = string([]rune(message)[:255])
		}
		updates := map[string]any{"last_balance_error": message, "last_balance_check": common.GetTimestamp()}
		if balance != nil {
			updates["balance"], updates["last_balance_time"] = *balance, common.GetTimestamp()
		}
		if err := tx.Model(&account).Updates(updates).Error; err != nil {
			return err
		}
		delete(updates, "last_balance_check")
		if balance != nil {
			delete(updates, "balance")
			updates["upstream_balance"] = *balance
			updates["balance_consecutive_failures"], updates["balance_failure_alert_notified"] = 0, false
		} else {
			updates["balance_consecutive_failures"] = gorm.Expr("balance_consecutive_failures + ?", 1)
		}
		if err := tx.Model(&ChannelRatioMonitor{}).Where("upstream_account_id = ? AND upstream_account_revision = ?", id, revision).Updates(updates).Error; err != nil {
			return err
		}
		if balance == nil {
			return nil
		}
		return tx.Model(&ChannelRatioMonitor{}).Where("upstream_account_id = ? AND upstream_account_revision = ?", id, revision).
			Where("balance_warning_threshold IS NULL OR balance_warning_threshold <= ?", *balance).Update("balance_alert_notified", false).Error
	})
}

// Persist credential refreshes without changing the balance pool or repricing
// active requests. Compare in Go so case-insensitive MySQL collations cannot
// accept a different credential. All channel projections change atomically.
func RefreshUpstreamAccountCredentials(ctx context.Context, expected ChannelMonitorUpstreamAccount, settings ChannelMonitorAccountSettings) error {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	return DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		var current ChannelMonitorUpstreamAccount
		if err := lockForUpdate(tx).First(&current, expected.ID).Error; err != nil {
			return err
		}
		if current.Revision != expected.Revision || current.Settings != expected.Settings {
			return ErrUpstreamAccountChanged
		}
		raw, err := common.Marshal(settings)
		if err != nil {
			return err
		}
		if err := tx.Model(&current).Update("settings", string(raw)).Error; err != nil {
			return err
		}
		var members []ChannelRatioMonitor
		if err := lockForUpdate(tx).Where("upstream_account_id = ?", current.ID).Find(&members).Error; err != nil {
			return err
		}
		for _, m := range members {
			if err := settings.Apply(&m); err != nil {
				return err
			}
			if err := tx.Save(&m).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func RotateUpstreamAccountRefreshToken(ctx context.Context, id int, revision int64, separate bool, oldToken, newToken string) (bool, error) {
	account, err := GetChannelMonitorUpstreamAccount(ctx, id)
	if err != nil {
		return false, err
	}
	settings, err := account.MonitorSettings()
	if err != nil {
		return false, err
	}
	if account.Revision != revision {
		return false, ErrUpstreamAccountChanged
	}
	if separate {
		if settings.UpstreamRefreshToken != oldToken {
			return false, errors.New("账户凭据已更新")
		}
		settings.UpstreamRefreshToken = newToken
	} else {
		if settings.UpstreamAccessToken != oldToken {
			return false, errors.New("账户凭据已更新")
		}
		settings.UpstreamAccessToken = newToken
	}
	err = RefreshUpstreamAccountCredentials(ctx, account, settings)
	return err == nil, err
}
