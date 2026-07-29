package model

import "gorm.io/gorm"

type channelSmartScheduleRouteRouting struct {
	priority *int64
	weight   uint
}

// getChannelSmartScheduleRouteRouting preserves the current per-route routing
// values while a channel edit rebuilds its abilities. Only explicitly
// participating routes keep their scheduler-controlled values.
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
		Where("channel_id = ? AND participation_set = ? AND excluded = ?", channelId, true, false).
		Find(&states).Error; err != nil {
		return nil, err
	}
	if len(states) == 0 {
		return routingByKey, nil
	}

	participating := make(map[ChannelSmartScheduleRouteKey]struct{}, len(states))
	for _, state := range states {
		key := channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)
		participating[key] = struct{}{}
		if state.LastScheduleTime > 0 {
			priority := state.LastSchedulePriority
			routingByKey[key] = channelSmartScheduleRouteRouting{
				priority: &priority,
				weight:   state.LastScheduleWeight,
			}
		}
	}

	var abilities []Ability
	if err := lockForUpdate(tx).Where("channel_id = ?", channelId).Find(&abilities).Error; err != nil {
		return nil, err
	}
	for _, ability := range abilities {
		key := channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)
		if _, ok := participating[key]; !ok {
			continue
		}
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
	if err := lockForUpdate(tx).Where("channel_id = ?", channelId).Find(&states).Error; err != nil {
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

func deleteChannelSmartScheduleRouteStatesForMissingChannels(tx *gorm.DB, channelIDs []int) error {
	if !tx.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
		return nil
	}
	if len(channelIDs) == 0 {
		return tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&ChannelSmartScheduleRouteState{}).Error
	}
	return tx.Where("channel_id NOT IN ?", channelIDs).Delete(&ChannelSmartScheduleRouteState{}).Error
}
