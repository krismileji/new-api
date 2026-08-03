package model

import (
	"errors"
	"math"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const ChannelSmartScheduleManualRoutingMaxValue = math.MaxInt32

type ChannelSmartScheduleManualRoutingResult struct {
	Priority       int64 `json:"priority"`
	Weight         uint  `json:"weight"`
	RoutingChanged bool  `json:"routing_changed"`
}

func SaveChannelSmartScheduleManualRouting(
	channelId int,
	group string,
	modelName string,
	priority int64,
	weight uint,
) (result ChannelSmartScheduleManualRoutingResult, err error) {
	if priority < 0 || priority > ChannelSmartScheduleManualRoutingMaxValue {
		return result, errors.New("人工优先级必须在 0 到 2147483647 之间")
	}
	if uint64(weight) > ChannelSmartScheduleManualRoutingMaxValue {
		return result, errors.New("人工权重必须在 0 到 2147483647 之间")
	}
	result.Priority = priority
	result.Weight = weight
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	err = DB.Transaction(func(tx *gorm.DB) error {
		channels, err := lockChannelSmartScheduleRoutePoolChannelsTx(tx, group, modelName, channelId)
		if err != nil {
			return err
		}
		channelStatusById := make(map[int]int, len(channels))
		for _, channel := range channels {
			channelStatusById[channel.Id] = channel.Status
		}
		var states []ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).
			Where("group_name = ? AND model_name = ?", group, modelName).
			Order("channel_id ASC").
			Find(&states).Error; err != nil {
			return err
		}
		var state *ChannelSmartScheduleRouteState
		for index := range states {
			if states[index].ChannelId == channelId {
				state = &states[index]
				break
			}
		}
		if state == nil {
			return gorm.ErrRecordNotFound
		}
		if state.Participates() {
			return errors.New("该路由仍在参与智能调度，请先取消参与后再手动设置优先级和权重")
		}
		if state.Revision == math.MaxInt64 {
			return errors.New("智能调度路由修订号已达上限")
		}
		var abilities []Ability
		if err := lockForUpdate(tx).
			Where(&Ability{Group: group, Model: modelName}).
			Order("channel_id ASC").
			Find(&abilities).Error; err != nil {
			return err
		}
		var ability *Ability
		for index := range abilities {
			if abilities[index].ChannelId == channelId {
				ability = &abilities[index]
				break
			}
		}
		if ability == nil {
			return gorm.ErrRecordNotFound
		}
		result.RoutingChanged = abilityPriority(*ability) != priority || ability.Weight != weight
		if result.RoutingChanged {
			key := channelSmartScheduleRouteKey(channelId, group, modelName)
			if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &priority, &weight); err != nil {
				return err
			}
			ability.Priority = &priority
			ability.Weight = weight
		}

		now := common.GetTimestamp()
		state.LastScheduleStatus = ChannelSmartScheduleStatusSucceeded
		state.LastScheduleError = "管理员手动设置未参与路由的优先级和权重"
		state.LastScheduleScore = nil
		state.LastScheduleScoreDetails = ""
		state.LastSchedulePriority = priority
		state.LastScheduleWeight = weight
		state.LastScheduleTime = now
		if state.StabilityState != "" {
			state.StabilitySavedPriority = priority
			state.StabilitySavedWeight = weight
		}
		state.Revision++
		if err := saveChannelSmartScheduleRouteStateTx(tx, state); err != nil {
			return err
		}

		for index := range states {
			primaryState := &states[index]
			if primaryState.ChannelId == channelId || primaryState.ManualPrimaryUntil <= now ||
				!primaryState.ManualPrimarySaved || primaryState.StabilityState != "" {
				continue
			}
			var primaryAbility *Ability
			for abilityIndex := range abilities {
				if abilities[abilityIndex].ChannelId == primaryState.ChannelId {
					primaryAbility = &abilities[abilityIndex]
					break
				}
			}
			if primaryAbility == nil || !primaryAbility.Enabled {
				continue
			}
			if channelStatusById[primaryState.ChannelId] != common.ChannelStatusEnabled {
				continue
			}
			primaryPriority, err := channelSmartScheduleManualPrimaryPriority(
				abilities,
				channelStatusById,
				primaryState.ChannelId,
				max(abilityPriority(*primaryAbility), primaryState.LastSchedulePriority),
			)
			if err != nil {
				return err
			}
			primaryWeight := uint(1000)
			if abilityPriority(*primaryAbility) == primaryPriority && primaryAbility.Weight == primaryWeight {
				break
			}
			if primaryState.Revision == math.MaxInt64 {
				return errors.New("智能调度路由修订号已达上限")
			}
			if err := updateAbilitySmartSchedulePriorityWeightTx(
				tx,
				channelSmartScheduleRouteKey(primaryState.ChannelId, group, modelName),
				&primaryPriority,
				&primaryWeight,
			); err != nil {
				return err
			}
			primaryState.LastScheduleStatus = ChannelSmartScheduleStatusSucceeded
			primaryState.LastScheduleError = "池内人工路由已变更，固定主渠道已重新置顶"
			primaryState.LastScheduleScore = nil
			primaryState.LastScheduleScoreDetails = ""
			primaryState.LastSchedulePriority = primaryPriority
			primaryState.LastScheduleWeight = primaryWeight
			primaryState.LastScheduleTime = now
			primaryState.Revision++
			if err := saveChannelSmartScheduleRouteStateTx(tx, primaryState); err != nil {
				return err
			}
			result.RoutingChanged = true
			break
		}
		return nil
	})
	return result, err
}
