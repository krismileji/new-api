package model

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

var retiredChannelMonitorRetryOptionKeys = []string{
	"ChannelMonitorRetrySkipErrorCodes",
	"ChannelMonitorRetrySkipErrorMessages",
}

// MigrateRetiredChannelMonitorRetryOptions removes channel-monitor retry
// options that are no longer supported. The settings are stored as rows in
// the shared options table, so no schema change is required.
func MigrateRetiredChannelMonitorRetryOptions() error {
	if DB == nil {
		return errors.New("database is not initialized")
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(commonKeyCol+" IN ?", retiredChannelMonitorRetryOptionKeys).Delete(&Option{}).Error; err != nil {
			return fmt.Errorf("delete retired channel monitor retry options: %w", err)
		}
		return nil
	})
}
