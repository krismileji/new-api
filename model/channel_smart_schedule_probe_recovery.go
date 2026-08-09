package model

import (
	"errors"
	"math"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// ChannelSmartScheduleProbeRecoveryRoute describes one protected route that
// shares a channel/model probe result. Policy values are captured by the
// controller so the transaction can reject a stale control revision.
type ChannelSmartScheduleProbeRecoveryRoute struct {
	Group                    string
	Model                    string
	RecoverySuccessThreshold int
	CooldownUntil            int64
}

type ChannelSmartScheduleProbeRecoveryResult struct {
	Applied              bool
	RecoverySuccessCount int
	Recovered            []ChannelSmartScheduleRouteKey
	Renewed              []ChannelSmartScheduleRouteKey
	RoutingChanged       bool
	ObservationSince     int64
}

type ChannelSmartScheduleProbeRecoveryRequest struct {
	ExpectedControlRevision string
	FailureReason           string
	Routes                  []ChannelSmartScheduleProbeRecoveryRoute
	Result                  ChannelSmartScheduleProbeRecoveryResult
}

type channelSmartScheduleProbeRecoveryRouteKey struct {
	group string
	model string
}

type channelSmartScheduleProbeRecoveryTx struct {
	revisionMatched bool
	request         ChannelSmartScheduleProbeRecoveryRequest
	specByPool      map[channelSmartScheduleProbeRecoveryRouteKey]ChannelSmartScheduleProbeRecoveryRoute
	pools           []channelSmartScheduleRoutePool
	states          []ChannelSmartScheduleRouteState
}

func prepareChannelSmartScheduleProbeRecoveryTx(
	tx *gorm.DB,
	channelId int,
	request ChannelSmartScheduleProbeRecoveryRequest,
) (*channelSmartScheduleProbeRecoveryTx, error) {
	prepared := &channelSmartScheduleProbeRecoveryTx{request: request}
	controlRevision, err := lockChannelSmartScheduleControlRevisionTx(tx)
	if err != nil {
		return nil, err
	}
	if controlRevision != request.ExpectedControlRevision {
		return prepared, nil
	}
	prepared.revisionMatched = true
	prepared.specByPool = make(
		map[channelSmartScheduleProbeRecoveryRouteKey]ChannelSmartScheduleProbeRecoveryRoute,
		len(request.Routes),
	)
	for _, route := range request.Routes {
		group := strings.TrimSpace(route.Group)
		modelName := strings.TrimSpace(route.Model)
		if group == "" || modelName == "" {
			continue
		}
		if route.RecoverySuccessThreshold <= 0 {
			route.RecoverySuccessThreshold = 1
		}
		route.Group = group
		route.Model = modelName
		key := channelSmartScheduleProbeRecoveryRouteKey{group: group, model: modelName}
		if existing, exists := prepared.specByPool[key]; !exists ||
			route.RecoverySuccessThreshold < existing.RecoverySuccessThreshold ||
			route.CooldownUntil > existing.CooldownUntil {
			prepared.specByPool[key] = route
		}
	}
	for key := range prepared.specByPool {
		prepared.pools = append(
			prepared.pools,
			channelSmartScheduleRoutePool{group: key.group, model: key.model},
		)
	}
	sort.Slice(prepared.pools, func(i int, j int) bool {
		if prepared.pools[i].group != prepared.pools[j].group {
			return prepared.pools[i].group < prepared.pools[j].group
		}
		return prepared.pools[i].model < prepared.pools[j].model
	})

	channelIds := []int{channelId}
	for _, pool := range prepared.pools {
		var abilityChannelIds []int
		if err := tx.Model(&Ability{}).
			Where(&Ability{Group: pool.group, Model: pool.model}).
			Pluck("channel_id", &abilityChannelIds).Error; err != nil {
			return nil, err
		}
		channelIds = append(channelIds, abilityChannelIds...)
		var stateChannelIds []int
		if err := tx.Model(&ChannelSmartScheduleRouteState{}).
			Where("group_name = ? AND model_name = ?", pool.group, pool.model).
			Pluck("channel_id", &stateChannelIds).Error; err != nil {
			return nil, err
		}
		channelIds = append(channelIds, stateChannelIds...)
	}
	if _, err := lockChannelsForDependentWriteTx(tx, channelIds); err != nil {
		return nil, err
	}
	prepared.states, err = lockChannelSmartScheduleRoutePoolStatesTx(tx, prepared.pools)
	if err != nil {
		return nil, err
	}
	return prepared, nil
}

func applyChannelSmartScheduleProbeRecoveryTx(
	tx *gorm.DB,
	sampleState *ChannelSmartScheduleModelSampleState,
	succeeded bool,
	resultTime int64,
	prepared *channelSmartScheduleProbeRecoveryTx,
) (result ChannelSmartScheduleProbeRecoveryResult, err error) {
	if tx == nil || sampleState == nil {
		return result, errors.New("智能调度探测恢复缺少事务或共享样本")
	}
	if prepared == nil || !prepared.revisionMatched {
		return result, nil
	}
	if len(prepared.specByPool) == 0 {
		if sampleState.RecoverySuccessCount != 0 || sampleState.RecoverySuccessAt != 0 {
			sampleState.RecoverySuccessCount = 0
			sampleState.RecoverySuccessAt = 0
			if err := tx.Save(sampleState).Error; err != nil {
				return result, err
			}
		}
		result.Applied = true
		return result, nil
	}

	abilities, err := lockChannelSmartScheduleRoutePoolAbilitiesTx(tx, prepared.pools)
	if err != nil {
		return result, err
	}

	type stateAbility struct {
		state   *ChannelSmartScheduleRouteState
		ability *Ability
		spec    ChannelSmartScheduleProbeRecoveryRoute
	}
	eligible := make([]stateAbility, 0, len(prepared.pools))
	for stateIndex := range prepared.states {
		state := &prepared.states[stateIndex]
		if state.ChannelId != sampleState.ChannelId || !state.Participates() ||
			(state.StabilityState != ChannelSmartScheduleStabilityDegraded &&
				state.StabilityState != ChannelSmartScheduleStabilityProbing) {
			continue
		}
		spec, configured := prepared.specByPool[channelSmartScheduleProbeRecoveryRouteKey{
			group: state.GroupName, model: state.ModelName,
		}]
		if !configured {
			continue
		}
		for abilityIndex := range abilities {
			ability := &abilities[abilityIndex]
			if ability.ChannelId == state.ChannelId && ability.Group == state.GroupName &&
				ability.Model == state.ModelName && ability.Enabled {
				eligible = append(eligible, stateAbility{state: state, ability: ability, spec: spec})
				break
			}
		}
	}

	result.Applied = true
	if len(eligible) == 0 {
		sampleState.RecoverySuccessCount = 0
		sampleState.RecoverySuccessAt = 0
		if err := tx.Save(sampleState).Error; err != nil {
			return result, err
		}
		return result, nil
	}

	if succeeded {
		if sampleState.RecoverySuccessCount < math.MaxInt {
			sampleState.RecoverySuccessCount++
		}
		sampleState.RecoverySuccessAt = resultTime
		result.RecoverySuccessCount = sampleState.RecoverySuccessCount
		if err := tx.Save(sampleState).Error; err != nil {
			return result, err
		}
		recoveredCount := 0
		for _, item := range eligible {
			if sampleState.RecoverySuccessCount < item.spec.RecoverySuccessThreshold {
				continue
			}
			clearResult, clearErr := clearChannelSmartScheduleRouteStabilityTx(
				tx,
				item.state,
				item.ability,
				item.state.BasePriority,
				item.state.BaseWeight,
				"恢复探测连续成功达到阈值，已立即解除稳定性保护",
			)
			if clearErr != nil {
				return result, clearErr
			}
			if !clearResult.Cleared {
				continue
			}
			result.Recovered = append(result.Recovered, channelSmartScheduleRouteKey(
				item.state.ChannelId, item.state.GroupName, item.state.ModelName,
			))
			result.RoutingChanged = result.RoutingChanged || clearResult.RoutingChanged
			result.ObservationSince = max(result.ObservationSince, clearResult.ObservationSince)
			recoveredCount++
		}
		if len(result.Recovered) == 0 {
			return result, nil
		}
		remainingProtectedRoutes := len(eligible) - recoveredCount
		nextCount := 0
		nextSuccessAt := int64(0)
		if remainingProtectedRoutes > 0 {
			nextCount = sampleState.RecoverySuccessCount
			nextSuccessAt = resultTime
		}
		if err := tx.Model(&ChannelSmartScheduleModelSampleState{}).
			Where("id = ?", sampleState.Id).
			Updates(map[string]any{
				"recovery_success_count": nextCount,
				"recovery_success_at":    nextSuccessAt,
			}).Error; err != nil {
			return result, err
		}
		result.RecoverySuccessCount = nextCount
		return result, nil
	}

	sampleState.RecoverySuccessCount = 0
	sampleState.RecoverySuccessAt = 0
	if err := tx.Save(sampleState).Error; err != nil {
		return result, err
	}
	reason := strings.TrimSpace(prepared.request.FailureReason)
	if reason == "" {
		reason = "恢复探测失败，已清零连续成功次数并续满稳定性保护"
	}
	if runes := []rune(reason); len(runes) > 255 {
		reason = string(runes[:255])
	}
	for _, item := range eligible {
		state := item.state
		ability := item.ability
		if state.Revision == math.MaxInt64 {
			return result, errors.New("智能调度路由修订号已达上限")
		}
		savedPriority := state.StabilitySavedPriority
		savedWeight := state.StabilitySavedWeight
		if savedPriority <= 0 {
			savedPriority = state.BasePriority
		}
		if savedWeight == 0 {
			savedWeight = state.BaseWeight
		}
		if savedPriority <= 0 {
			savedPriority = abilityPriority(*ability)
		}
		if savedWeight == 0 {
			savedWeight = ability.Weight
		}
		if savedPriority <= 0 {
			savedPriority = channelSmartScheduleRuntimeFallbackPriority
		}
		if savedWeight == 0 {
			savedWeight = channelSmartScheduleRuntimeFallbackWeight
		}

		state.StabilityState = ChannelSmartScheduleStabilityDegraded
		state.StabilityUntil = max(state.StabilityUntil, item.spec.CooldownUntil)
		state.StabilitySince = 0
		state.StabilitySavedPriority = savedPriority
		state.StabilitySavedWeight = savedWeight
		state.RuntimeProtectionUntil = max(state.RuntimeProtectionUntil, item.spec.CooldownUntil)
		state.TemporaryTrafficKind = ""
		state.TemporaryTrafficSince = 0
		state.TemporaryTrafficTargetPercent = 0
		state.ExplorationMaxPromptTokens = 0
		state.StabilityReleaseMaxPromptTokens = 0
		state.LastScheduleStatus = ChannelSmartScheduleStatusFailed
		state.LastScheduleError = reason
		state.LastScheduleScore = nil
		state.LastScheduleScoreDetails = ""
		state.LastSchedulePriority = 0
		state.LastScheduleWeight = 0
		state.LastScheduleTime = resultTime
		state.Revision++

		priority := int64(0)
		weight := uint(0)
		if ability.Priority == nil || abilityPriority(*ability) != priority || ability.Weight != weight {
			if err := updateAbilitySmartSchedulePriorityWeightTx(
				tx,
				channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName),
				&priority,
				&weight,
			); err != nil {
				return result, err
			}
			result.RoutingChanged = true
		}
		if err := saveChannelSmartScheduleRouteStateTx(tx, state); err != nil {
			return result, err
		}
		result.Renewed = append(result.Renewed, channelSmartScheduleRouteKey(
			state.ChannelId, state.GroupName, state.ModelName,
		))
	}
	return result, nil
}
