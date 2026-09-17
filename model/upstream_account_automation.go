package model

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func MergeUpstreamAccountAutomations(ctx context.Context, accountID int, revision int64, update func(ChannelMonitorUpstreamAccount, []SystemTask) ([]SystemTask, error)) error {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	return DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		var account ChannelMonitorUpstreamAccount
		if err := lockForUpdate(tx).First(&account, accountID).Error; err != nil {
			return err
		}
		if account.Revision != revision {
			return ErrUpstreamAccountChanged
		}
		if account.LeaseUntil > common.GetTimestamp() {
			return errors.New("账户正在执行，请稍后重试")
		}
		var rows []SystemTask
		if err := lockForUpdate(tx).Where("type = ?", UpstreamAutomationConfigType).Order("id ASC").Find(&rows).Error; err != nil {
			return err
		}
		changed, err := update(account, rows)
		if err != nil {
			return err
		}
		for _, row := range changed {
			if err := tx.Model(&SystemTask{}).Where("id = ?", row.ID).Updates(map[string]any{"payload": row.Payload, "state": row.State, "updated_at": common.GetTimestamp()}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
