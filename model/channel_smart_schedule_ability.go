package model

import (
	"strings"

	"gorm.io/gorm"
)

type channelSmartScheduleRouteRouting struct {
	priority *int64
	weight   uint
}

func channelAbilityRouting(
	ability Ability,
	channel *Channel,
) (int64, uint) {
	if ability.Priority != nil {
		return *ability.Priority, ability.Weight
	}
	if channel == nil {
		return 0, 0
	}
	return channel.GetPriority(), uint(channel.GetWeight())
}

func channelSmartScheduleAbilityRouting(ability Ability) (int64, uint) {
	if ability.Priority == nil {
		return 0, 0
	}
	return *ability.Priority, ability.Weight
}

func channelRoutingForTrafficPolicy(
	ability Ability,
	channel *Channel,
	group string,
	modelName string,
	trafficPolicy *channelSmartScheduleTrafficPolicy,
) (int64, uint) {
	if trafficPolicy != nil && trafficPolicy.managesPool(group, modelName) {
		return channelSmartScheduleAbilityRouting(ability)
	}
	return channelAbilityRouting(ability, channel)
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

// getChannelSmartScheduleRouteRouting preserves scheduler-controlled routing
// while a channel edit rebuilds its participating abilities.
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

	participating := make(map[ChannelSmartScheduleRouteKey]struct{}, len(states))
	for _, state := range states {
		key := channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)
		if state.Participates() {
			participating[key] = struct{}{}
			if state.LastScheduleTime > 0 {
				priority := state.LastSchedulePriority
				routingByKey[key] = channelSmartScheduleRouteRouting{
					priority: &priority,
					weight:   state.LastScheduleWeight,
				}
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
			// A scheduled value is preserved; an unscheduled route keeps its
			// empty smart-scheduling routing until the next scheduler run.
			if ability.Priority != nil {
				value := *ability.Priority
				routingByKey[key] = channelSmartScheduleRouteRouting{
					priority: &value,
					weight:   ability.Weight,
				}
			}
			continue
		}
	}
	return routingByKey, nil
}

// admitNewChannelSmartScheduleGroupsTx makes routes from newly associated
// groups immediately eligible for scheduling without changing existing route
// participation choices.
func admitNewChannelSmartScheduleGroupsTx(
	tx *gorm.DB,
	channel *Channel,
	previousAbilities []Ability,
) error {
	if !tx.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
		return nil
	}

	previousGroups := make(map[string]struct{}, len(previousAbilities))
	for _, ability := range previousAbilities {
		previousGroups[ability.Group] = struct{}{}
	}

	var states []ChannelSmartScheduleRouteState
	if err := lockForUpdate(tx).
		Where("channel_id = ?", channel.Id).
		Order("group_name ASC, model_name ASC").
		Find(&states).Error; err != nil {
		return err
	}
	existingRoutes := make(map[ChannelSmartScheduleRouteKey]struct{}, len(states))
	for _, state := range states {
		existingRoutes[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)] = struct{}{}
	}

	newStates := make([]ChannelSmartScheduleRouteState, 0)
	for _, group := range strings.Split(channel.Group, ",") {
		if _, existed := previousGroups[group]; existed {
			continue
		}
		for _, modelName := range strings.Split(channel.Models, ",") {
			key := channelSmartScheduleRouteKey(channel.Id, group, modelName)
			if _, exists := existingRoutes[key]; exists {
				continue
			}
			existingRoutes[key] = struct{}{}
			newStates = append(newStates, ChannelSmartScheduleRouteState{
				ChannelId:        channel.Id,
				GroupName:        group,
				ModelName:        modelName,
				ParticipationSet: true,
				Revision:         1,
			})
		}
	}
	if len(newStates) == 0 {
		return nil
	}
	return tx.CreateInBatches(newStates, 50).Error
}

// deleteObsoleteChannelSmartScheduleRouteStates removes state belonging to
// routes no longer exposed by the channel. A later re-added group starts with
// fresh scheduling state through admitNewChannelSmartScheduleGroupsTx.
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
