package model

import (
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

type channelSmartScheduleRoutePool struct {
	group string
	model string
}

func channelSmartScheduleRoutePools(groupList string, modelList string) []channelSmartScheduleRoutePool {
	seen := make(map[channelSmartScheduleRoutePool]struct{})
	for _, group := range strings.Split(groupList, ",") {
		for _, modelName := range strings.Split(modelList, ",") {
			seen[channelSmartScheduleRoutePool{group: group, model: modelName}] = struct{}{}
		}
	}
	pools := make([]channelSmartScheduleRoutePool, 0, len(seen))
	for pool := range seen {
		pools = append(pools, pool)
	}
	sort.Slice(pools, func(i int, j int) bool {
		if pools[i].group != pools[j].group {
			return pools[i].group < pools[j].group
		}
		return pools[i].model < pools[j].model
	})
	return pools
}

func channelSmartScheduleRoutePoolsFromAbilities(
	abilities []Ability,
	additional ...channelSmartScheduleRoutePool,
) []channelSmartScheduleRoutePool {
	seen := make(map[channelSmartScheduleRoutePool]struct{}, len(abilities)+len(additional))
	for _, pool := range additional {
		seen[pool] = struct{}{}
	}
	for _, ability := range abilities {
		seen[channelSmartScheduleRoutePool{group: ability.Group, model: ability.Model}] = struct{}{}
	}
	pools := make([]channelSmartScheduleRoutePool, 0, len(seen))
	for pool := range seen {
		pools = append(pools, pool)
	}
	sort.Slice(pools, func(i int, j int) bool {
		if pools[i].group != pools[j].group {
			return pools[i].group < pools[j].group
		}
		return pools[i].model < pools[j].model
	})
	return pools
}

func lockChannelSmartScheduleRoutePoolsTx(tx *gorm.DB, pools []channelSmartScheduleRoutePool) error {
	_, err := lockChannelSmartScheduleRoutePoolStatesTx(tx, pools)
	return err
}

func reapplyChannelSmartScheduleRoutePrimariesTx(
	tx *gorm.DB,
	pools []channelSmartScheduleRoutePool,
) error {
	if len(pools) == 0 || !tx.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
		return nil
	}
	if err := lockChannelSmartScheduleRoutePoolsTx(tx, pools); err != nil {
		return err
	}
	now := common.GetTimestamp()
	for _, pool := range pools {
		var states []ChannelSmartScheduleRouteState
		if err := tx.
			Where("group_name = ? AND model_name = ?", pool.group, pool.model).
			Order("channel_id ASC").
			Find(&states).Error; err != nil {
			return err
		}
		var abilities []Ability
		if err := lockForUpdate(tx).
			Where(&Ability{Group: pool.group, Model: pool.model}).
			Order("channel_id ASC").
			Find(&abilities).Error; err != nil {
			return err
		}
		abilityByKey := make(map[ChannelSmartScheduleRouteKey]*Ability, len(abilities))
		channelIds := make([]int, 0, len(abilities))
		for index := range abilities {
			ab := &abilities[index]
			abilityByKey[channelSmartScheduleRouteKey(ab.ChannelId, ab.Group, ab.Model)] = ab
			channelIds = append(channelIds, ab.ChannelId)
		}
		var channels []Channel
		if err := tx.
			Select("id", "status", "priority", "weight").
			Where("id IN ?", channelIds).
			Order("id ASC").
			Find(&channels).Error; err != nil {
			return err
		}
		channelStatusById := make(map[int]int, len(channels))
		channelById := make(map[int]Channel, len(channels))
		for _, channel := range channels {
			channelStatusById[channel.Id] = channel.Status
			channelById[channel.Id] = channel
		}
		var primaryState *ChannelSmartScheduleRouteState
		autoDisabledPrimaryCleared := false
		for index := range states {
			state := &states[index]
			if state.ManualPrimaryUntil > now &&
				channelStatusById[state.ChannelId] == common.ChannelStatusAutoDisabled {
				if _, err := restoreChannelSmartScheduleRoutePrimaryTx(
					tx,
					state,
					abilityByKey[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)],
				); err != nil {
					return err
				}
				autoDisabledPrimaryCleared = true
			}
			if states[index].ManualPrimaryUntil <= now {
				continue
			}
			if primaryState != nil {
				return errors.New("同一分组和模型存在多个有效的固定主渠道")
			}
			primaryState = &states[index]
		}
		if primaryState == nil {
			if autoDisabledPrimaryCleared {
				if _, err := clearChannelSmartScheduleRoutePoolTemporaryTrafficTx(
					tx, states, abilities, pool.group, pool.model, now,
				); err != nil {
					return err
				}
			}
			continue
		}

		var primaryAbility *Ability
		for index := range abilities {
			if abilities[index].ChannelId == primaryState.ChannelId {
				primaryAbility = &abilities[index]
			}
		}
		primaryAvailable := primaryState.Participates() && primaryState.StabilityState == "" &&
			primaryAbility != nil && primaryAbility.Enabled &&
			channelStatusById[primaryState.ChannelId] == common.ChannelStatusEnabled
		if !primaryAvailable {
			if _, err := clearChannelSmartScheduleRoutePoolTemporaryTrafficTx(
				tx, states, abilities, pool.group, pool.model, now,
			); err != nil {
				return err
			}
			continue
		}
		if _, primaryChannelExists := channelById[primaryState.ChannelId]; !primaryChannelExists {
			return gorm.ErrRecordNotFound
		}
		primaryPriority, _ := channelSmartScheduleAbilityRouting(*primaryAbility)
		priority, err := channelSmartScheduleManualPrimaryPriority(
			abilities,
			states,
			channelStatusById,
			primaryState.ChannelId,
			max(primaryPriority, primaryState.LastSchedulePriority),
		)
		if err != nil {
			return err
		}
		weight := uint(1000)
		if primaryAbility.Priority != nil && primaryPriority == priority && primaryAbility.Weight == weight {
			continue
		}
		if primaryState.Revision == math.MaxInt64 {
			return errors.New("智能调度路由修订号已达上限")
		}
		if err := updateAbilitySmartSchedulePriorityWeightTx(
			tx,
			channelSmartScheduleRouteKey(primaryState.ChannelId, pool.group, pool.model),
			&priority,
			&weight,
		); err != nil {
			return err
		}
		primaryState.LastScheduleStatus = ChannelSmartScheduleStatusSucceeded
		primaryState.LastScheduleError = "池内渠道能力已变化，固定主渠道已重新置顶"
		primaryState.LastScheduleScore = nil
		primaryState.LastScheduleScoreDetails = ""
		primaryState.LastSchedulePriority = priority
		primaryState.LastScheduleWeight = weight
		primaryState.LastScheduleTime = now
		primaryState.Revision++
		if err := saveChannelSmartScheduleRouteStateTx(tx, primaryState); err != nil {
			return err
		}
	}
	return nil
}
