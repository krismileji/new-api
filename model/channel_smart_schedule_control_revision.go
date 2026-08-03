package model

import (
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var channelSmartScheduleOptionTableReady sync.Map

func hasChannelSmartScheduleOptionTable(tx *gorm.DB) bool {
	if _, ok := channelSmartScheduleOptionTableReady.Load(DB); ok {
		return true
	}
	if !tx.Migrator().HasTable(&Option{}) {
		return false
	}
	channelSmartScheduleOptionTableReady.Store(DB, struct{}{})
	return true
}

// lockChannelSmartScheduleControlRevisionTx serializes scheduler writes with
// route and Ability configuration changes. The caller must already be inside a
// transaction; the value is intentionally read from the database rather than
// the process option cache.
func lockChannelSmartScheduleControlRevisionTx(tx *gorm.DB) (string, error) {
	if !hasChannelSmartScheduleOptionTable(tx) {
		return "", nil
	}
	option := Option{Key: ChannelSmartScheduleControlRevisionOption}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&option).Error; err != nil {
		return "", err
	}
	err := lockForUpdate(tx).
		Where(&Option{Key: ChannelSmartScheduleControlRevisionOption}).
		First(&option).Error
	if err != nil {
		return "", err
	}
	return option.Value, nil
}

func GetChannelSmartScheduleControlRevision() (string, error) {
	if !hasChannelSmartScheduleOptionTable(DB) {
		return "", nil
	}
	option := Option{Key: ChannelSmartScheduleControlRevisionOption}
	if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&option).Error; err != nil {
		return "", err
	}
	if err := DB.Where(&Option{Key: ChannelSmartScheduleControlRevisionOption}).First(&option).Error; err != nil {
		return "", err
	}
	return option.Value, nil
}
