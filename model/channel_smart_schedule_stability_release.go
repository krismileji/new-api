package model

import (
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const channelSmartScheduleStabilityReleaseWeight uint = 10

type ChannelSmartScheduleStabilityReleasePool struct {
	Group                           string
	Model                           string
	StabilityReleaseMaxPromptTokens int
}

type ChannelSmartScheduleStabilityReleaseResult struct {
	Applied        bool
	Released       []ChannelSmartScheduleRouteKey
	RoutingChanged bool
}

// GetExpiredChannelSmartScheduleDegradedRoutes returns possible cooldown
// transitions. AdvanceExpiredChannelSmartScheduleDegradedRoutes rechecks every
// row under the control-revision and pool locks before applying the change.
func GetExpiredChannelSmartScheduleDegradedRoutes(now int64) ([]ChannelSmartScheduleRouteKey, error) {
	if now <= 0 {
		now = common.GetTimestamp()
	}
	var states []ChannelSmartScheduleRouteState
	if err := DB.Select("channel_id", "group_name", "model_name").
		Where(
			"stability_state = ? AND stability_until > ? AND stability_until <= ?",
			ChannelSmartScheduleStabilityDegraded,
			0,
			now,
		).
		Order("channel_id ASC, group_name ASC, model_name ASC").
		Find(&states).Error; err != nil {
		return nil, err
	}
	routes := make([]ChannelSmartScheduleRouteKey, 0, len(states))
	for _, state := range states {
		routes = append(routes, channelSmartScheduleRouteKey(
			state.ChannelId,
			state.GroupName,
			state.ModelName,
		))
	}
	return routes, nil
}

// AdvanceExpiredChannelSmartScheduleDegradedRoutes moves eligible degraded
// routes to the P0/W10 stability-release state without waiting for a full
// schedule. The operation is idempotent and guarded by the persisted control
// revision so multiple instances may run the expiry timer concurrently.
func AdvanceExpiredChannelSmartScheduleDegradedRoutes(
	now int64,
	expectedControlRevision string,
	configuredPools []ChannelSmartScheduleStabilityReleasePool,
) (result ChannelSmartScheduleStabilityReleaseResult, err error) {
	if now <= 0 {
		now = common.GetTimestamp()
	}
	specByPool := make(map[channelSmartScheduleRoutePool]ChannelSmartScheduleStabilityReleasePool)
	for _, configured := range configuredPools {
		configured.Group = strings.TrimSpace(configured.Group)
		configured.Model = strings.TrimSpace(configured.Model)
		if configured.Group == "" || configured.Model == "" {
			continue
		}
		if configured.StabilityReleaseMaxPromptTokens < 0 {
			configured.StabilityReleaseMaxPromptTokens = 0
		}
		pool := channelSmartScheduleRoutePool{group: configured.Group, model: configured.Model}
		specByPool[pool] = configured
	}
	if len(specByPool) == 0 {
		return result, nil
	}
	pools := make([]channelSmartScheduleRoutePool, 0, len(specByPool))
	for pool := range specByPool {
		pools = append(pools, pool)
	}
	sort.Slice(pools, func(i int, j int) bool {
		if pools[i].group != pools[j].group {
			return pools[i].group < pools[j].group
		}
		return pools[i].model < pools[j].model
	})

	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	err = DB.Transaction(func(tx *gorm.DB) error {
		controlRevision, revisionErr := lockChannelSmartScheduleControlRevisionTx(tx)
		if revisionErr != nil {
			return revisionErr
		}
		if controlRevision != expectedControlRevision {
			return nil
		}
		result.Applied = true

		channelIds := make([]int, 0)
		for _, pool := range pools {
			var abilityChannelIds []int
			if err := tx.Model(&Ability{}).
				Where(&Ability{Group: pool.group, Model: pool.model}).
				Pluck("channel_id", &abilityChannelIds).Error; err != nil {
				return err
			}
			channelIds = append(channelIds, abilityChannelIds...)
			var stateChannelIds []int
			if err := tx.Model(&ChannelSmartScheduleRouteState{}).
				Where("group_name = ? AND model_name = ?", pool.group, pool.model).
				Pluck("channel_id", &stateChannelIds).Error; err != nil {
				return err
			}
			channelIds = append(channelIds, stateChannelIds...)
		}
		channels, lockErr := lockChannelsForDependentWriteTx(tx, channelIds)
		if lockErr != nil {
			return lockErr
		}
		channelStatusById := make(map[int]int, len(channels))
		for _, channel := range channels {
			channelStatusById[channel.Id] = channel.Status
		}
		states, lockErr := lockChannelSmartScheduleRoutePoolStatesTx(tx, pools)
		if lockErr != nil {
			return lockErr
		}
		abilities, lockErr := lockChannelSmartScheduleRoutePoolAbilitiesTx(tx, pools)
		if lockErr != nil {
			return lockErr
		}
		abilityByKey := make(map[ChannelSmartScheduleRouteKey]*Ability, len(abilities))
		for index := range abilities {
			ability := &abilities[index]
			abilityByKey[channelSmartScheduleRouteKey(
				ability.ChannelId,
				ability.Group,
				ability.Model,
			)] = ability
		}

		affectedPools := make(map[channelSmartScheduleRoutePool]struct{})
		for index := range states {
			state := &states[index]
			pool := channelSmartScheduleRoutePool{group: state.GroupName, model: state.ModelName}
			if _, configured := specByPool[pool]; !configured ||
				state.StabilityState != ChannelSmartScheduleStabilityDegraded ||
				state.StabilityUntil <= 0 || state.StabilityUntil > now ||
				!state.Participates() || channelStatusById[state.ChannelId] != common.ChannelStatusEnabled {
				continue
			}
			ability := abilityByKey[channelSmartScheduleRouteKey(
				state.ChannelId,
				state.GroupName,
				state.ModelName,
			)]
			if ability == nil || !ability.Enabled {
				continue
			}
			affectedPools[pool] = struct{}{}
		}
		if len(affectedPools) == 0 {
			return nil
		}

		// Withdraw the pool's sampling overlay first. The marker is stored on
		// the sampled route, while the applied weight change can also affect the
		// primary, so every normal route in that pool must return to exact base.
		temporaryPools := make(map[channelSmartScheduleRoutePool]struct{})
		for index := range states {
			state := &states[index]
			pool := channelSmartScheduleRoutePool{group: state.GroupName, model: state.ModelName}
			if _, affected := affectedPools[pool]; affected &&
				(state.TemporaryTrafficKind == ChannelSmartScheduleTemporaryTrafficExploration ||
					state.TemporaryTrafficKind == ChannelSmartScheduleTemporaryTrafficAdaptive) {
				temporaryPools[pool] = struct{}{}
			}
		}
		for index := range states {
			state := &states[index]
			pool := channelSmartScheduleRoutePool{group: state.GroupName, model: state.ModelName}
			if _, temporary := temporaryPools[pool]; !temporary {
				continue
			}
			ability := abilityByKey[channelSmartScheduleRouteKey(
				state.ChannelId,
				state.GroupName,
				state.ModelName,
			)]
			if ability != nil && state.Participates() && ability.Enabled &&
				state.StabilityState == "" && state.ManualPrimaryUntil <= now {
				priority := state.BasePriority
				weight := state.BaseWeight
				if ability.Priority == nil || abilityPriority(*ability) != priority || ability.Weight != weight {
					if err := updateAbilitySmartSchedulePriorityWeightTx(
						tx,
						channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName),
						&priority,
						&weight,
					); err != nil {
						return err
					}
					ability.Priority = &priority
					ability.Weight = weight
					result.RoutingChanged = true
				}
			}
			if state.TemporaryTrafficKind != ChannelSmartScheduleTemporaryTrafficExploration &&
				state.TemporaryTrafficKind != ChannelSmartScheduleTemporaryTrafficAdaptive {
				continue
			}
			if state.Revision == math.MaxInt64 {
				return errors.New("智能调度路由修订号已达上限")
			}
			state.TemporaryTrafficKind = ""
			state.TemporaryTrafficSince = 0
			state.TemporaryTrafficTargetPercent = 0
			state.ExplorationMaxPromptTokens = 0
			state.SamplingCandidate = false
			state.Revision++
			if err := saveChannelSmartScheduleRouteStateTx(tx, state); err != nil {
				return err
			}
		}

		for index := range states {
			state := &states[index]
			pool := channelSmartScheduleRoutePool{group: state.GroupName, model: state.ModelName}
			spec, configured := specByPool[pool]
			if !configured || state.StabilityState != ChannelSmartScheduleStabilityDegraded ||
				state.StabilityUntil <= 0 || state.StabilityUntil > now ||
				!state.Participates() || channelStatusById[state.ChannelId] != common.ChannelStatusEnabled {
				continue
			}
			key := channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)
			ability := abilityByKey[key]
			if ability == nil || !ability.Enabled {
				continue
			}
			if state.Revision == math.MaxInt64 {
				return errors.New("智能调度路由修订号已达上限")
			}
			state.StabilityState = ChannelSmartScheduleStabilityProbing
			state.StabilityUntil = 0
			state.StabilitySince = now
			state.RuntimeProtectionUntil = 0
			state.TemporaryTrafficKind = ""
			state.TemporaryTrafficSince = 0
			state.TemporaryTrafficTargetPercent = 0
			state.ExplorationMaxPromptTokens = 0
			state.StabilityReleaseMaxPromptTokens = spec.StabilityReleaseMaxPromptTokens
			state.SamplingCandidate = false
			state.LastScheduleStatus = ChannelSmartScheduleStatusSucceeded
			state.LastScheduleError = "稳定性冷却已到期，已立即进入小流量试放"
			state.LastScheduleScore = nil
			state.LastScheduleScoreDetails = ""
			state.LastSchedulePriority = 0
			state.LastScheduleWeight = channelSmartScheduleStabilityReleaseWeight
			state.LastScheduleTime = now
			state.Revision++

			priority := int64(0)
			weight := channelSmartScheduleStabilityReleaseWeight
			if ability.Priority == nil || abilityPriority(*ability) != priority || ability.Weight != weight {
				if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &priority, &weight); err != nil {
					return err
				}
				result.RoutingChanged = true
			}
			if err := saveChannelSmartScheduleRouteStateTx(tx, state); err != nil {
				return err
			}
			result.Released = append(result.Released, key)
		}
		return nil
	})
	if err != nil {
		result = ChannelSmartScheduleStabilityReleaseResult{}
	}
	return result, err
}
