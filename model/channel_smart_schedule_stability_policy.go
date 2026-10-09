package model

import (
	"errors"
	"math"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// clearDisabledChannelSmartScheduleStabilityTx reconciles protection with the
// policies being committed under the settings/control-revision lock. It also
// repairs protection created by older versions while the switch was off.
func clearDisabledChannelSmartScheduleStabilityTx(tx *gorm.DB, rawPolicies string) (bool, error) {
	var policies []struct {
		Group            string   `json:"group"`
		Models           []string `json:"models"`
		StabilityEnabled *bool    `json:"stability_enabled"`
	}
	if err := common.UnmarshalJsonStr(rawPolicies, &policies); err != nil {
		return false, err
	}
	disabled := make(map[string][]string)
	groups := make([]string, 0)
	for _, policy := range policies {
		if policy.StabilityEnabled != nil && !*policy.StabilityEnabled {
			disabled[policy.Group] = policy.Models
			groups = append(groups, policy.Group)
		}
	}
	if len(groups) == 0 {
		return false, nil
	}
	var affected []ChannelSmartScheduleRouteState
	if err := tx.Where("group_name IN ? AND stability_state <> ?", groups, "").
		Order("channel_id ASC, group_name ASC, model_name ASC").Find(&affected).Error; err != nil {
		return false, err
	}
	pools := make([]channelSmartScheduleRoutePool, 0, len(affected))
	for _, state := range affected {
		models := disabled[state.GroupName]
		if len(models) == 0 || slices.Contains(models, state.ModelName) {
			pools = append(pools, channelSmartScheduleRoutePool{group: state.GroupName, model: state.ModelName})
		}
	}
	pools = channelSmartScheduleRoutePoolsFromAbilities(nil, pools...)
	channelIDs := make([]int, 0, len(affected))
	for _, state := range affected {
		channelIDs = append(channelIDs, state.ChannelId)
	}
	for _, pool := range pools {
		var poolChannelIDs []int
		if err := tx.Model(&Ability{}).Where(&Ability{Group: pool.group, Model: pool.model}).
			Pluck("channel_id", &poolChannelIDs).Error; err != nil {
			return false, err
		}
		channelIDs = append(channelIDs, poolChannelIDs...)
	}
	if _, err := lockChannelsForDependentWriteTx(tx, channelIDs); err != nil {
		return false, err
	}
	states, err := lockChannelSmartScheduleRoutePoolStatesTx(tx, pools)
	if err != nil {
		return false, err
	}
	abilities, err := lockChannelSmartScheduleRoutePoolAbilitiesTx(tx, pools)
	if err != nil {
		return false, err
	}
	abilityByKey := make(map[ChannelSmartScheduleRouteKey]*Ability, len(abilities))
	for i := range abilities {
		ability := &abilities[i]
		abilityByKey[channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)] = ability
	}
	changed := false
	now := common.GetTimestamp()
	for i := range states {
		state := &states[i]
		if state.StabilityState == "" {
			continue
		}
		priority, weight, err := restoreChannelSmartScheduleDisabledStability(state, now)
		if err != nil {
			return false, err
		}
		key := channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)
		if ability := abilityByKey[key]; ability != nil {
			if state.Participates() {
				if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &priority, &weight); err != nil {
					return false, err
				}
			} else if err := clearChannelSmartScheduleAbilityRoutingTx(tx, key); err != nil {
				return false, err
			}
		}
		if err := saveChannelSmartScheduleRouteStateTx(tx, state); err != nil {
			return false, err
		}
		changed = true
	}
	if len(pools) > 0 {
		if err := reapplyChannelSmartScheduleRoutePrimariesTx(tx, pools); err != nil {
			return false, err
		}
	}

	// Logical protection must not keep overriding the restored physical routes.
	// Preserve its routing intent and sample history.
	if !tx.Migrator().HasTable(&ChannelLogicalSmartScheduleRouteState{}) {
		return changed, nil
	}
	var logicalStates []ChannelLogicalSmartScheduleRouteState
	if err := lockForUpdate(tx).Where("group_name IN ?", groups).
		Order("logical_group_id ASC, logical_revision ASC, group_name ASC, model_name ASC").
		Find(&logicalStates).Error; err != nil {
		return false, err
	}
	for _, stored := range logicalStates {
		models := disabled[stored.GroupName]
		if len(models) > 0 && !slices.Contains(models, stored.ModelName) {
			continue
		}
		payload, err := decodeLogicalSmartScheduleRoutePayload(stored.StateJSON)
		if err != nil {
			return false, err
		}
		state := payload.State
		if state.StabilityState == "" {
			continue
		}
		state.Revision = stored.StateRevision
		priority, weight, err := restoreChannelSmartScheduleDisabledStability(&state, now)
		if err != nil {
			return false, err
		}
		raw, err := encodeLogicalSmartScheduleRouteStateWithRouting(state, priority, weight)
		if err != nil {
			return false, err
		}
		if err := tx.Model(&ChannelLogicalSmartScheduleRouteState{}).Where("id = ?", stored.Id).
			Updates(map[string]any{"state_revision": state.Revision, "state_json": raw, "updated_at": now}).Error; err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

func restoreChannelSmartScheduleDisabledStability(state *ChannelSmartScheduleRouteState, now int64) (int64, uint, error) {
	if state.Revision == math.MaxInt64 {
		return 0, 0, errors.New("智能调度路由修订号已达上限")
	}
	priority, weight := state.StabilitySavedPriority, state.StabilitySavedWeight
	if priority <= 0 {
		priority = state.BasePriority
	}
	if weight == 0 {
		weight = state.BaseWeight
	}
	if priority <= 0 {
		priority = channelSmartScheduleRuntimeFallbackPriority
	}
	if weight == 0 {
		weight = channelSmartScheduleRuntimeFallbackWeight
	}
	state.StabilityState = ""
	state.StabilityUntil = 0
	state.StabilitySince = 0
	state.StabilitySavedPriority = 0
	state.StabilitySavedWeight = 0
	state.RuntimeProtectionUntil = 0
	state.StabilityReleaseMaxPromptTokens = 0
	state.LastScheduleStatus = ChannelSmartScheduleStatusSucceeded
	state.LastScheduleError = "分组策略已关闭稳定性保护，已解除降级并恢复调度"
	state.LastScheduleScore = nil
	state.LastScheduleScoreAt = 0
	state.LastScheduleScoreDetails = ""
	state.LastSchedulePriority = priority
	state.LastScheduleWeight = weight
	state.LastScheduleTime = now
	state.Revision++
	return priority, weight, nil
}
