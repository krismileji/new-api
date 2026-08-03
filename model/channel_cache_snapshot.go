package model

import (
	"database/sql"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type channelCacheSnapshot struct {
	channels            []*Channel
	abilities           []*Ability
	smartScheduleStates []ChannelSmartScheduleRouteState
}

func loadChannelCacheSnapshot() (snapshot channelCacheSnapshot, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Order("id ASC").
			Find(&snapshot.channels).Error; err != nil {
			return err
		}
		if tx.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
			if err := tx.
				Order("channel_id ASC, group_name ASC, model_name ASC").
				Find(&snapshot.smartScheduleStates).Error; err != nil {
				return err
			}
		}
		return tx.
			Order("channel_id ASC").
			Order(clause.OrderByColumn{Column: clause.Column{Name: "group"}}).
			Order(clause.OrderByColumn{Column: clause.Column{Name: "model"}}).
			Find(&snapshot.abilities).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return snapshot, err
}
