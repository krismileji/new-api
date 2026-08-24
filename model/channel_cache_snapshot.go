package model

import (
	"context"
	"database/sql"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type channelCacheSnapshot struct {
	channels                 []*Channel
	abilities                []*Ability
	smartScheduleStates      []ChannelSmartScheduleRouteState
	logicalScheduleStates    []ChannelLogicalSmartScheduleRouteState
	smartScheduleGroupPauses []ChannelSmartScheduleGroupPause
}

func loadChannelCacheSnapshot() (snapshot channelCacheSnapshot, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		var loadErr error
		snapshot, loadErr = loadChannelCacheSnapshotFromDB(tx)
		return loadErr
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return snapshot, err
}

func loadChannelCacheSnapshotFromDB(db *gorm.DB) (snapshot channelCacheSnapshot, err error) {
	if err := db.
		Order("id ASC").
		Find(&snapshot.channels).Error; err != nil {
		return snapshot, err
	}
	if db.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
		if err := db.
			Order("channel_id ASC, group_name ASC, model_name ASC").
			Find(&snapshot.smartScheduleStates).Error; err != nil {
			return snapshot, err
		}
	}
	if IsLogicalChannelGroupingEnabled() && db.Migrator().HasTable(&ChannelLogicalSmartScheduleRouteState{}) {
		if err := db.
			Order("logical_group_id ASC, logical_revision ASC, group_name ASC, model_name ASC").
			Find(&snapshot.logicalScheduleStates).Error; err != nil {
			return snapshot, err
		}
	}
	if db.Migrator().HasTable(&ChannelSmartScheduleGroupPause{}) {
		if err := db.
			Where("paused_until > ?", common.GetTimestamp()).
			Order("channel_id ASC, group_name ASC, model_name ASC").
			Find(&snapshot.smartScheduleGroupPauses).Error; err != nil {
			return snapshot, err
		}
	}
	err = db.
		Order("channel_id ASC").
		Order(clause.OrderByColumn{Column: clause.Column{Name: "group"}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "model"}}).
		Find(&snapshot.abilities).Error
	return snapshot, err
}

func loadChannelSmartScheduleRouteSnapshotSource(ctx context.Context) (
	snapshot channelCacheSnapshot,
	logicalRuntime *LogicalChannelRuntimeSnapshot,
	err error,
) {
	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var loadErr error
		snapshot, loadErr = loadChannelCacheSnapshotFromDB(tx)
		if loadErr != nil {
			return loadErr
		}
		logicalRuntime, loadErr = buildLogicalChannelRuntimeSnapshot(tx)
		return loadErr
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return snapshot, logicalRuntime, err
}
