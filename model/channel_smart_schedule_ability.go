package model

import "gorm.io/gorm"

type channelSmartScheduleRouteRouting struct {
	priority *int64
	weight   uint
}

func channelSmartScheduleAbilityRouting(
	ability Ability,
	channel *Channel,
) (int64, uint) {
	// A nil group priority is the inheritance marker. Weight is deliberately
	// ignored in that case because the zero written while clearing an override
	// is not an effective group weight.
	if ability.Priority != nil {
		return *ability.Priority, ability.Weight
	}
	if channel == nil {
		return 0, 0
	}
	return channel.GetPriority(), uint(channel.GetWeight())
}

func applyChannelSmartScheduleAbilityRoutingTx(
	tx *gorm.DB,
	key ChannelSmartScheduleRouteKey,
	channel *Channel,
) error {
	if channel == nil {
		return gorm.ErrRecordNotFound
	}
	priority := channel.GetPriority()
	weight := uint(channel.GetWeight())
	return updateAbilitySmartSchedulePriorityWeightTx(tx, key, &priority, &weight)
}

func clearChannelSmartScheduleAbilityRoutingTx(
	tx *gorm.DB,
	key ChannelSmartScheduleRouteKey,
) error {
	conditions := &Ability{ChannelId: key.ChannelId, Group: key.Group, Model: key.Model}
	result := tx.Model(&Ability{}).Where(conditions).Updates(map[string]any{
		"priority": nil,
		"weight":   0,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&Ability{}).Where(conditions).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// getChannelSmartScheduleRouteRouting preserves the current per-route routing
// values while a channel edit rebuilds its abilities. Participating routes
// keep their scheduler-controlled overrides; routes without an override use
// the channel defaults.
func getChannelSmartScheduleRouteRouting(
	tx *gorm.DB,
	channelId int,
	channel *Channel,
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

	participating := make(map[ChannelSmartScheduleRouteKey]struct{}, len(states))
	for _, state := range states {
		key := channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)
		if state.Participates() {
			participating[key] = struct{}{}
			priority := int64(0)
			weight := uint(0)
			if channel != nil {
				priority = channel.GetPriority()
				weight = uint(channel.GetWeight())
			}
			if state.LastScheduleTime > 0 {
				priority = state.LastSchedulePriority
				weight = state.LastScheduleWeight
			}
			routingByKey[key] = channelSmartScheduleRouteRouting{
				priority: &priority,
				weight:   weight,
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
		if _, ok := participating[key]; ok {
			// A participating ability is the authoritative current routing value.
			// When an old database row is missing its override, the default or the
			// last schedule value seeded above repairs it on rebuild.
			priority := routingByKey[key].priority
			weight := routingByKey[key].weight
			if ability.Priority != nil {
				value := *ability.Priority
				priority = &value
				weight = ability.Weight
			}
			routingByKey[key] = channelSmartScheduleRouteRouting{priority: priority, weight: weight}
			continue
		}
		// Excluded routes may be explicitly configured for manual takeover. Keep
		// that override; a normal excluded route has no entry and inherits the
		// channel defaults at selection time.
		if ability.Priority != nil {
			priority := *ability.Priority
			routingByKey[key] = channelSmartScheduleRouteRouting{
				priority: &priority,
				weight:   ability.Weight,
			}
		}
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
