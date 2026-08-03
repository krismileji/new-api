package model

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func lockChannelSmartScheduleRoutePoolChannelsTx(
	tx *gorm.DB,
	group string,
	modelName string,
	additionalChannelIds ...int,
) ([]Channel, error) {
	channelIds := append([]int(nil), additionalChannelIds...)
	var poolChannelIds []int
	if err := tx.Model(&Ability{}).
		Where(&Ability{Group: group, Model: modelName}).
		Order("channel_id ASC").
		Pluck("channel_id", &poolChannelIds).Error; err != nil {
		return nil, err
	}
	channelIds = append(channelIds, poolChannelIds...)
	if tx.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
		var stateChannelIds []int
		if err := tx.Model(&ChannelSmartScheduleRouteState{}).
			Where("group_name = ? AND model_name = ?", group, modelName).
			Pluck("channel_id", &stateChannelIds).Error; err != nil {
			return nil, err
		}
		channelIds = append(channelIds, stateChannelIds...)
	}
	return lockChannelsForDependentWriteTx(tx, channelIds)
}

func lockChannelSmartScheduleRoutePoolStatesTx(
	tx *gorm.DB,
	pools []channelSmartScheduleRoutePool,
) ([]ChannelSmartScheduleRouteState, error) {
	pools = channelSmartScheduleRoutePoolsFromAbilities(nil, pools...)
	if len(pools) == 0 || !tx.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
		return nil, nil
	}
	query := tx.Model(&ChannelSmartScheduleRouteState{})
	for index, pool := range pools {
		if index == 0 {
			query = query.Where("group_name = ? AND model_name = ?", pool.group, pool.model)
			continue
		}
		query = query.Or("group_name = ? AND model_name = ?", pool.group, pool.model)
	}
	var states []ChannelSmartScheduleRouteState
	err := lockForUpdate(query).
		Order("channel_id ASC, group_name ASC, model_name ASC").
		Find(&states).Error
	return states, err
}

func lockChannelSmartScheduleRoutePoolAbilitiesTx(
	tx *gorm.DB,
	pools []channelSmartScheduleRoutePool,
) ([]Ability, error) {
	pools = channelSmartScheduleRoutePoolsFromAbilities(nil, pools...)
	if len(pools) == 0 {
		return nil, nil
	}
	query := tx.Model(&Ability{})
	for index, pool := range pools {
		if index == 0 {
			query = query.Where(&Ability{Group: pool.group, Model: pool.model})
			continue
		}
		query = query.Or(&Ability{Group: pool.group, Model: pool.model})
	}
	var abilities []Ability
	err := lockForUpdate(query).
		Order("channel_id ASC").
		Order(clause.OrderByColumn{Column: clause.Column{Name: "group"}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "model"}}).
		Find(&abilities).Error
	return abilities, err
}
