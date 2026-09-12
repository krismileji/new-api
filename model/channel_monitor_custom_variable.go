package model

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// UpdateChannelMonitorCustomVariableConfig persists refreshed variables without
// changing the user's configuration revision or overwriting a concurrent edit.
func UpdateChannelMonitorCustomVariableConfig(ctx context.Context, channelID int, revision int64, expected, updated string) error {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	// This write contains credentials; never include its SQL parameters in logs.
	return DB.WithContext(ctx).Session(&gorm.Session{Logger: DB.Logger.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		var monitor ChannelRatioMonitor
		err := lockForUpdate(tx).Where("channel_id = ?", channelID).First(&monitor).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrChannelRatioMonitorConfigChanged
		}
		if err != nil {
			return err
		}
		// Compare bytes in Go to avoid case-insensitive TEXT collation matches on MySQL.
		if monitor.UpstreamType != "custom" || monitor.UpstreamRevision != revision || monitor.CustomUpstreamConfig != expected {
			return ErrChannelRatioMonitorConfigChanged
		}
		return tx.Model(&ChannelRatioMonitor{}).Where("channel_id = ?", channelID).Update("custom_upstream_config", updated).Error
	})
}
