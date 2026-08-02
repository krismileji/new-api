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
	err = DB.Transaction(func(tx *gorm.DB) error {
		var state ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).
			Where(&ChannelSmartScheduleRouteState{
				ChannelId: channelId,
				GroupName: group,
				ModelName: modelName,
			}).First(&state).Error; err != nil {
			return err
		}
		if state.Participates() {
			return errors.New("该路由仍在参与智能调度，请先取消参与后再手动设置优先级和权重")
		}
		if state.Revision == math.MaxInt64 {
			return errors.New("智能调度路由修订号已达上限")
		}

		var ability Ability
		if err := lockForUpdate(tx).
			Where(&Ability{ChannelId: channelId, Group: group, Model: modelName}).
			First(&ability).Error; err != nil {
			return err
		}
		result.RoutingChanged = abilityPriority(ability) != priority || ability.Weight != weight
		if result.RoutingChanged {
			key := channelSmartScheduleRouteKey(channelId, group, modelName)
			if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &priority, &weight); err != nil {
				return err
			}
		}

		now := common.GetTimestamp()
		state.LastScheduleStatus = ChannelSmartScheduleStatusSucceeded
		state.LastScheduleError = "管理员手动设置未参与路由的优先级和权重"
		state.LastScheduleScore = nil
		state.LastScheduleScoreDetails = ""
		state.LastSchedulePriority = priority
		state.LastScheduleWeight = weight
		state.LastScheduleTime = now
		state.Revision++
		return saveChannelSmartScheduleRouteStateTx(tx, &state)
	})
	return result, err
}
