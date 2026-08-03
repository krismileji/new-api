package model

import "gorm.io/gorm"

type channelSmartScheduleRouteRouting struct {
	priority *int64
	weight   uint
}

// getChannelSmartScheduleRouteRouting preserves the current per-route routing
// values while a channel edit rebuilds its abilities. Existing routes keep
// their scheduler- or administrator-controlled values; only newly exposed
// routes inherit the channel defaults.
func getChannelSmartScheduleRouteRouting(
	tx *gorm.DB,
	channelId int,
) (map[ChannelSmartScheduleRouteKey]channelSmartScheduleRouteRouting, error) {
	routingByKey := make(map[ChannelSmartScheduleRouteKey]channelSmartScheduleRouteRouting)
	if !tx.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
		return routingByKey, nil
	}

	var states []ChannelSmartScheduleRouteState
	if err := lockForUpdate(tx).
		Where("channel_id = ?", channelId).
		Order("group_name ASC, model_name ASC").
		Find(&states).Error; err != nil {
		return nil, err
	}

	for _, state := range states {
		key := channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)
		if state.Participates() && state.LastScheduleTime > 0 {
			priority := state.LastSchedulePriority
			routingByKey[key] = channelSmartScheduleRouteRouting{
				priority: &priority,
				weight:   state.LastScheduleWeight,
			}
		}
	}

	var abilities []Ability
	if err := lockForUpdate(tx).
		Where("channel_id = ?", channelId).
		Order("model ASC").
		Find(&abilities).Error; err != nil {
		return nil, err
	}
	for _, ability := range abilities {
		key := channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)
		var priority *int64
		if ability.Priority != nil {
			value := *ability.Priority
			priority = &value
		}
		routingByKey[key] = channelSmartScheduleRouteRouting{priority: priority, weight: ability.Weight}
	}
	return routingByKey, nil
}

// deleteObsoleteChannelSmartScheduleRouteStates removes state belonging to
// routes no longer exposed by the channel. A later re-added route must begin
// as a new, explicitly configured scheduling participant.
func deleteObsoleteChannelSmartScheduleRouteStates(
	tx *gorm.DB,
	channelId int,
	activeRoutes map[ChannelSmartScheduleRouteKey]struct{},
) error {
	if !tx.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
		return nil
	}

	var states []ChannelSmartScheduleRouteState
	if err := lockForUpdate(tx).
		Where("channel_id = ?", channelId).
		Order("group_name ASC, model_name ASC").
		Find(&states).Error; err != nil {
		return err
	}
	staleIDs := make([]int64, 0)
	for _, state := range states {
		if _, active := activeRoutes[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)]; !active {
			staleIDs = append(staleIDs, state.Id)
		}
	}
	if len(staleIDs) == 0 {
		return nil
	}
	return tx.Where("id IN ?", staleIDs).Delete(&ChannelSmartScheduleRouteState{}).Error
}

func deleteChannelSmartScheduleRouteStatesForMissingChannels(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
		return nil
	}
	return tx.Where(
		"NOT EXISTS (?)",
		tx.Model(&Channel{}).Select("1").Where("channels.id = channel_smart_schedule_route_states.channel_id"),
	).Delete(&ChannelSmartScheduleRouteState{}).Error
}
