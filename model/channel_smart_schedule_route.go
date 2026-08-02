package model

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ChannelSmartScheduleRouteState struct {
	Id               int64  `json:"id"`
	ChannelId        int    `json:"channel_id" gorm:"not null;uniqueIndex:idx_channel_smart_schedule_route"`
	GroupName        string `json:"group" gorm:"type:varchar(64);not null;uniqueIndex:idx_channel_smart_schedule_route"`
	ModelName        string `json:"model" gorm:"type:varchar(255);not null;uniqueIndex:idx_channel_smart_schedule_route"`
	ParticipationSet bool   `json:"participation_set"`
	Excluded         bool   `json:"excluded"`
	Revision         int64  `json:"-" gorm:"bigint"`

	LastScheduleStatus       string                               `json:"last_schedule_status" gorm:"type:varchar(16);index"`
	LastScheduleError        string                               `json:"last_schedule_error" gorm:"type:varchar(255)"`
	LastScheduleScore        *float64                             `json:"last_schedule_score"`
	LastSchedulePriority     int64                                `json:"last_schedule_priority" gorm:"bigint"`
	LastScheduleWeight       uint                                 `json:"last_schedule_weight"`
	LastScheduleTime         int64                                `json:"last_schedule_time" gorm:"bigint;index"`
	LastScheduleScoreDetails ChannelSmartScheduleScoreDetailsJSON `json:"last_schedule_score_details,omitempty" gorm:"type:text"`

	StabilityState             string   `json:"stability_state" gorm:"type:varchar(16);index"`
	StabilityUntil             int64    `json:"stability_until" gorm:"bigint;index"`
	StabilitySince             int64    `json:"stability_since" gorm:"bigint"`
	StabilitySavedPriority     int64    `json:"stability_saved_priority" gorm:"bigint"`
	StabilitySavedWeight       uint     `json:"stability_saved_weight"`
	RuntimeProtectionUntil     int64    `json:"runtime_protection_until" gorm:"bigint;index"`
	JitterBaselineFirstTokenMs *float64 `json:"jitter_baseline_first_token_ms"`
	JitterBaselineUpdatedAt    int64    `json:"jitter_baseline_updated_at" gorm:"bigint"`

	BaseRank     int   `json:"base_rank"`
	BasePriority int64 `json:"base_priority" gorm:"bigint"`
	BaseWeight   uint  `json:"base_weight"`

	TemporaryTrafficKind          string  `json:"temporary_traffic_kind" gorm:"type:varchar(32);index"`
	TemporaryTrafficSince         int64   `json:"temporary_traffic_since" gorm:"bigint"`
	TemporaryTrafficTargetPercent float64 `json:"temporary_traffic_target_percent"`
	LastPrioritySampleTime        int64   `json:"last_priority_sample_time" gorm:"bigint;index"`

	// ManualPrimaryUntil keeps an administrator-selected primary route in
	// force until the unix timestamp. The saved routing values are internal
	// restore markers and are intentionally not exposed to API consumers.
	ManualPrimaryUntil                 int64 `json:"manual_primary_until" gorm:"bigint;index"`
	ManualPrimaryAllowStabilityDegrade bool  `json:"manual_primary_allow_stability_degrade"`
	ManualPrimarySaved                 bool  `json:"-"`
	ManualPrimarySavedPriority         int64 `json:"-" gorm:"bigint"`
	ManualPrimarySavedWeight           uint  `json:"-"`
}

const ChannelSmartScheduleManualPrimaryMaxMinutes = 525600

type ChannelSmartScheduleRoutePrimaryResult struct {
	State          ChannelSmartScheduleRouteState
	RoutingChanged bool
}

type ChannelSmartScheduleRoutePrimaryOptions struct {
	DurationMinutes       int
	AllowStabilityDegrade bool
}

func (state ChannelSmartScheduleRouteState) Participates() bool {
	return state.ParticipationSet && !state.Excluded
}

func saveChannelSmartScheduleRouteStateTx(tx *gorm.DB, state *ChannelSmartScheduleRouteState) error {
	return tx.Save(state).Error
}

type ChannelSmartScheduleRouteKey struct {
	ChannelId int
	Group     string
	Model     string
}

type ChannelSmartScheduleRoute struct {
	ChannelId       int                                  `json:"channel_id"`
	ChannelName     string                               `json:"channel_name"`
	ChannelStatus   int                                  `json:"channel_status"`
	ChannelPriority int64                                `json:"channel_priority"`
	ChannelWeight   uint                                 `json:"channel_weight"`
	Group           string                               `json:"group"`
	Model           string                               `json:"model"`
	Enabled         bool                                 `json:"enabled"`
	Priority        int64                                `json:"priority"`
	Weight          uint                                 `json:"weight"`
	State           ChannelSmartScheduleRouteState       `json:"state"`
	SharedSamples   ChannelSmartScheduleModelSampleState `json:"shared_samples"`
}

type ChannelSmartScheduleRouteResultUpdate struct {
	ChannelId               int
	Group                   string
	Model                   string
	Status                  string
	Error                   string
	Score                   *float64
	ScoreDetails            *ChannelSmartScheduleScoreDetails
	Priority                int64
	Weight                  uint
	Time                    int64
	Stability               *ChannelSmartScheduleStabilityUpdate
	Jitter                  *ChannelSmartScheduleJitterUpdate
	RuntimeProtectionUntil  *int64
	RoutingSnapshot         *ChannelSmartScheduleRoutingSnapshotUpdate
	GuardCurrent            bool
	ExpectedRevision        int64
	ExpectedControlRevision string
	ExpectedPriority        int64
	ExpectedWeight          uint
	ApplyPriorityWeight     bool
}

type ChannelSmartScheduleRouteApplyOutcome struct {
	Key            ChannelSmartScheduleRouteKey
	Applied        bool
	RoutingChanged bool
}

type ChannelSmartScheduleStabilityClearResult struct {
	PreviousState string
	Cleared       bool
	Priority      int64
	Weight        uint
}

type ChannelSmartScheduleStabilityUpdate struct {
	State         string
	Until         int64
	Since         int64
	SavedPriority int64
	SavedWeight   uint
}

type ChannelSmartScheduleJitterUpdate struct {
	BaselineFirstTokenMs *float64
	BaselineUpdatedAt    int64
}

type ChannelSmartScheduleRoutingSnapshotUpdate struct {
	BaseRank                      int
	BasePriority                  int64
	BaseWeight                    uint
	TemporaryTrafficKind          string
	TemporaryTrafficSince         int64
	TemporaryTrafficTargetPercent float64
	LastPrioritySampleTime        int64
}

const (
	ChannelSmartScheduleTemporaryTrafficExploration      = "insufficient_samples"
	ChannelSmartScheduleTemporaryTrafficPrioritySampling = "priority_sampling"
)

type ChannelSmartScheduleChannelConfigResult struct {
	Total          int  `json:"total"`
	Updated        int  `json:"updated"`
	RoutingChanged bool `json:"-"`
}

func abilityPriority(ability Ability) int64 {
	if ability.Priority == nil {
		return 0
	}
	return *ability.Priority
}

func channelSmartScheduleRouteKey(channelId int, group string, modelName string) ChannelSmartScheduleRouteKey {
	return ChannelSmartScheduleRouteKey{
		ChannelId: channelId,
		Group:     group,
		Model:     modelName,
	}
}

func InitializeChannelSmartScheduleRouteStates() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var abilities []Ability
		if err := tx.Find(&abilities).Error; err != nil {
			return err
		}
		var states []ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).Find(&states).Error; err != nil {
			return err
		}
		stateByKey := make(map[ChannelSmartScheduleRouteKey]struct{}, len(states))
		for _, state := range states {
			stateByKey[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)] = struct{}{}
		}

		newStates := make([]ChannelSmartScheduleRouteState, 0)
		for _, ability := range abilities {
			key := channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)
			if _, exists := stateByKey[key]; exists {
				continue
			}
			state := ChannelSmartScheduleRouteState{
				ChannelId:        ability.ChannelId,
				GroupName:        ability.Group,
				ModelName:        ability.Model,
				ParticipationSet: true,
				Excluded:         true,
				Revision:         1,
			}
			newStates = append(newStates, state)
		}
		if len(newStates) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(newStates, 500).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func GetChannelSmartScheduleRoutes() ([]ChannelSmartScheduleRoute, error) {
	var abilities []Ability
	if err := DB.Find(&abilities).Error; err != nil {
		return nil, err
	}
	var channels []Channel
	if err := DB.Select("id", "name", "status", "priority", "weight").Find(&channels).Error; err != nil {
		return nil, err
	}
	var states []ChannelSmartScheduleRouteState
	if err := DB.Find(&states).Error; err != nil {
		return nil, err
	}
	sharedSampleStates, err := GetChannelSmartScheduleModelSampleStates()
	if err != nil {
		return nil, err
	}
	channelById := make(map[int]Channel, len(channels))
	for _, channel := range channels {
		channelById[channel.Id] = channel
	}
	stateByKey := make(map[ChannelSmartScheduleRouteKey]ChannelSmartScheduleRouteState, len(states))
	for _, state := range states {
		stateByKey[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)] = state
	}
	sharedSamplesByModel := make(map[channelSmartScheduleModelKey]ChannelSmartScheduleModelSampleState, len(sharedSampleStates))
	for _, state := range sharedSampleStates {
		sharedSamplesByModel[channelSmartScheduleModelKey{channelId: state.ChannelId, modelName: state.ModelName}] = state
	}
	routes := make([]ChannelSmartScheduleRoute, 0, len(abilities))
	for _, ability := range abilities {
		channel, exists := channelById[ability.ChannelId]
		if !exists {
			continue
		}
		key := channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)
		modelKey := channelSmartScheduleModelKey{channelId: ability.ChannelId, modelName: ability.Model}
		sharedSamples := sharedSamplesByModel[modelKey]
		if sharedSamples.ChannelId == 0 {
			sharedSamples.ChannelId = ability.ChannelId
			sharedSamples.ModelName = ability.Model
		}
		routes = append(routes, ChannelSmartScheduleRoute{
			ChannelId:       ability.ChannelId,
			ChannelName:     channel.Name,
			ChannelStatus:   channel.Status,
			ChannelPriority: channel.GetPriority(),
			ChannelWeight:   uint(channel.GetWeight()),
			Group:           ability.Group,
			Model:           ability.Model,
			Enabled:         ability.Enabled,
			Priority:        abilityPriority(ability),
			Weight:          ability.Weight,
			State:           stateByKey[key],
			SharedSamples:   sharedSamples,
		})
	}
	sort.Slice(routes, func(i int, j int) bool {
		if routes[i].Group != routes[j].Group {
			return routes[i].Group < routes[j].Group
		}
		if routes[i].Model != routes[j].Model {
			return routes[i].Model < routes[j].Model
		}
		return routes[i].ChannelId < routes[j].ChannelId
	})
	return routes, nil
}

func SaveChannelSmartScheduleRouteConfig(channelId int, group string, modelName string, excluded bool) (state ChannelSmartScheduleRouteState, routingChanged bool, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		var ability Ability
		if err := lockForUpdate(tx).Where(&Ability{ChannelId: channelId, Group: group, Model: modelName}).First(&ability).Error; err != nil {
			return err
		}
		findErr := lockForUpdate(tx).
			Where(&ChannelSmartScheduleRouteState{ChannelId: channelId, GroupName: group, ModelName: modelName}).
			First(&state).Error
		created := errors.Is(findErr, gorm.ErrRecordNotFound)
		if created {
			state = ChannelSmartScheduleRouteState{
				ChannelId: channelId,
				GroupName: group,
				ModelName: modelName,
			}
		} else if findErr != nil {
			return findErr
		}
		wasParticipating := state.Participates()
		if state.ParticipationSet && state.Excluded == excluded {
			return nil
		}
		if state.Revision == math.MaxInt64 {
			return errors.New("智能调度路由修订号已达上限")
		}
		state.ParticipationSet = true
		state.Excluded = excluded
		if excluded {
			if state.ManualPrimaryUntil > 0 || state.ManualPrimarySaved {
				changed, restoreErr := restoreChannelSmartScheduleRoutePrimaryTx(tx, &state)
				if restoreErr != nil {
					return restoreErr
				}
				routingChanged = routingChanged || changed
			}
			if state.TemporaryTrafficKind != "" {
				priority := state.BasePriority
				weight := state.BaseWeight
				routingChanged = abilityPriority(ability) != priority || ability.Weight != weight
				if err := updateAbilitySmartSchedulePriorityWeightTx(
					tx, channelSmartScheduleRouteKey(channelId, group, modelName), &priority, &weight,
				); err != nil {
					return err
				}
			}
			state.TemporaryTrafficKind = ""
			state.TemporaryTrafficSince = 0
			state.TemporaryTrafficTargetPercent = 0
			state.RuntimeProtectionUntil = 0
		}
		if !wasParticipating && !excluded && state.StabilityState == ChannelSmartScheduleStabilityProbing {
			state.StabilitySince = common.GetTimestamp()
		}
		if state.Revision == math.MaxInt64 {
			return errors.New("智能调度路由修订号已达上限")
		}
		state.Revision++
		if created {
			return tx.Create(&state).Error
		}
		return saveChannelSmartScheduleRouteStateTx(tx, &state)
	})
	return state, routingChanged, err
}

func SaveChannelSmartScheduleChannelConfig(channelId int, excluded bool) (result ChannelSmartScheduleChannelConfigResult, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		var abilities []Ability
		if err := lockForUpdate(tx).Where("channel_id = ?", channelId).Find(&abilities).Error; err != nil {
			return err
		}
		result.Total = len(abilities)
		if result.Total == 0 {
			return nil
		}

		var states []ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).Where("channel_id = ?", channelId).Find(&states).Error; err != nil {
			return err
		}
		stateByKey := make(map[ChannelSmartScheduleRouteKey]*ChannelSmartScheduleRouteState, len(states))
		for index := range states {
			state := &states[index]
			stateByKey[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)] = state
		}

		now := common.GetTimestamp()
		for _, ability := range abilities {
			key := channelSmartScheduleRouteKey(channelId, ability.Group, ability.Model)
			state := stateByKey[key]
			created := state == nil
			if created {
				state = &ChannelSmartScheduleRouteState{
					ChannelId: channelId,
					GroupName: ability.Group,
					ModelName: ability.Model,
				}
			}
			wasParticipating := state.Participates()
			if state.ParticipationSet && state.Excluded == excluded {
				continue
			}
			if state.Revision == math.MaxInt64 {
				return errors.New("智能调度路由修订号已达上限")
			}
			state.ParticipationSet = true
			state.Excluded = excluded
			if excluded {
				if state.ManualPrimaryUntil > 0 || state.ManualPrimarySaved {
					changed, restoreErr := restoreChannelSmartScheduleRoutePrimaryTx(tx, state)
					if restoreErr != nil {
						return restoreErr
					}
					result.RoutingChanged = result.RoutingChanged || changed
				}
				if state.TemporaryTrafficKind != "" {
					priority := state.BasePriority
					weight := state.BaseWeight
					if abilityPriority(ability) != priority || ability.Weight != weight {
						result.RoutingChanged = true
					}
					if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &priority, &weight); err != nil {
						return err
					}
				}
				state.TemporaryTrafficKind = ""
				state.TemporaryTrafficSince = 0
				state.TemporaryTrafficTargetPercent = 0
				state.RuntimeProtectionUntil = 0
			}
			if !wasParticipating && !excluded && state.StabilityState == ChannelSmartScheduleStabilityProbing {
				state.StabilitySince = now
			}
			if state.Revision == math.MaxInt64 {
				return errors.New("智能调度路由修订号已达上限")
			}
			state.Revision++
			if created {
				if err := tx.Create(state).Error; err != nil {
					return err
				}
			} else if err := saveChannelSmartScheduleRouteStateTx(tx, state); err != nil {
				return err
			}
			result.Updated++
		}
		return nil
	})
	return result, err
}

func SaveChannelSmartScheduleRoutePrimary(
	channelId int,
	group string,
	modelName string,
	options ChannelSmartScheduleRoutePrimaryOptions,
) (result ChannelSmartScheduleRoutePrimaryResult, err error) {
	durationMinutes := options.DurationMinutes
	if durationMinutes < 0 || durationMinutes > ChannelSmartScheduleManualPrimaryMaxMinutes {
		return result, fmt.Errorf("主渠道固定时间必须在 0 到 %d 分钟之间", ChannelSmartScheduleManualPrimaryMaxMinutes)
	}
	now := common.GetTimestamp()
	err = DB.Transaction(func(tx *gorm.DB) error {
		var states []ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).
			Where("group_name = ? AND model_name = ?", group, modelName).
			Find(&states).Error; err != nil {
			return err
		}
		stateByChannel := make(map[int]*ChannelSmartScheduleRouteState, len(states))
		for index := range states {
			stateByChannel[states[index].ChannelId] = &states[index]
		}
		targetState := stateByChannel[channelId]
		if targetState == nil {
			return gorm.ErrRecordNotFound
		}

		if durationMinutes == 0 {
			changed, restoreErr := restoreChannelSmartScheduleRoutePrimaryTx(tx, targetState)
			if restoreErr != nil {
				return restoreErr
			}
			result.RoutingChanged = result.RoutingChanged || changed
			result.State = *targetState
			return nil
		}
		for index := range states {
			state := &states[index]
			if state.ChannelId == channelId || state.ManualPrimaryUntil <= 0 {
				continue
			}
			changed, restoreErr := restoreChannelSmartScheduleRoutePrimaryTx(tx, state)
			if restoreErr != nil {
				return restoreErr
			}
			result.RoutingChanged = result.RoutingChanged || changed
		}

		var targetAbility Ability
		if err := lockForUpdate(tx).Where(&Ability{
			ChannelId: channelId, Group: group, Model: modelName,
		}).First(&targetAbility).Error; err != nil {
			return err
		}
		var channel Channel
		if err := lockForUpdate(tx).Select("id", "status").Where("id = ?", channelId).First(&channel).Error; err != nil {
			return err
		}
		if channel.Status != common.ChannelStatusEnabled {
			return errors.New("渠道已禁用，不能固定为主渠道")
		}
		if !targetAbility.Enabled {
			return errors.New("该分组和模型路由已禁用，不能固定为主渠道")
		}
		if !targetState.Participates() {
			return errors.New("该分组和模型路由未参与智能调度，不能固定为主渠道")
		}
		if targetState.ManualPrimaryUntil > now && targetState.ManualPrimarySaved {
			if targetState.Revision == math.MaxInt64 {
				return errors.New("智能调度路由修订号已达上限")
			}
			targetState.ManualPrimaryUntil = now + int64(durationMinutes)*60
			targetState.ManualPrimaryAllowStabilityDegrade = options.AllowStabilityDegrade
			targetState.Revision++
			if err := saveChannelSmartScheduleRouteStateTx(tx, targetState); err != nil {
				return err
			}
			result.State = *targetState
			return nil
		}

		if targetState.ManualPrimaryUntil > 0 && targetState.ManualPrimaryUntil <= now {
			changed, restoreErr := restoreChannelSmartScheduleRoutePrimaryTx(tx, targetState)
			if restoreErr != nil {
				return restoreErr
			}
			result.RoutingChanged = result.RoutingChanged || changed
			if err := lockForUpdate(tx).Where(&Ability{
				ChannelId: channelId, Group: group, Model: modelName,
			}).First(&targetAbility).Error; err != nil {
				return err
			}
		}
		if targetState.StabilityState != "" {
			return errors.New("该分组和模型路由处于稳定性保护状态，不能固定为主渠道")
		}

		if targetState.TemporaryTrafficKind != "" {
			restoredPriority := targetState.BasePriority
			restoredWeight := targetState.BaseWeight
			if abilityPriority(targetAbility) != restoredPriority || targetAbility.Weight != restoredWeight {
				if err := updateAbilitySmartSchedulePriorityWeightTx(
					tx,
					channelSmartScheduleRouteKey(channelId, group, modelName),
					&restoredPriority,
					&restoredWeight,
				); err != nil {
					return err
				}
				result.RoutingChanged = true
				targetAbility.Priority = &restoredPriority
				targetAbility.Weight = restoredWeight
			}
			targetState.TemporaryTrafficKind = ""
			targetState.TemporaryTrafficSince = 0
			targetState.TemporaryTrafficTargetPercent = 0
		}

		if targetState.ManualPrimaryUntil <= now || !targetState.ManualPrimarySaved {
			targetState.ManualPrimarySaved = true
			targetState.ManualPrimarySavedPriority = abilityPriority(targetAbility)
			targetState.ManualPrimarySavedWeight = targetAbility.Weight
		}

		var abilities []Ability
		if err := lockForUpdate(tx).
			Where(&Ability{Group: group, Model: modelName}).
			Find(&abilities).Error; err != nil {
			return err
		}
		manualPriority := int64(0)
		for _, ability := range abilities {
			if priority := abilityPriority(ability); priority > manualPriority {
				manualPriority = priority
			}
		}
		if manualPriority == math.MaxInt64 {
			return errors.New("当前路由优先级已达上限，不能固定主渠道")
		}
		manualPriority++
		manualWeight := uint(1000)
		if abilityPriority(targetAbility) != manualPriority || targetAbility.Weight != manualWeight {
			if err := updateAbilitySmartSchedulePriorityWeightTx(
				tx,
				channelSmartScheduleRouteKey(channelId, group, modelName),
				&manualPriority,
				&manualWeight,
			); err != nil {
				return err
			}
			result.RoutingChanged = true
		}

		if targetState.Revision == math.MaxInt64 {
			return errors.New("智能调度路由修订号已达上限")
		}
		targetState.ManualPrimaryUntil = now + int64(durationMinutes)*60
		targetState.ManualPrimaryAllowStabilityDegrade = options.AllowStabilityDegrade
		targetState.LastScheduleStatus = ChannelSmartScheduleStatusSucceeded
		targetState.LastScheduleError = "管理员已固定该路由为主渠道"
		targetState.LastScheduleScore = nil
		targetState.LastScheduleScoreDetails = ""
		targetState.LastSchedulePriority = manualPriority
		targetState.LastScheduleWeight = manualWeight
		targetState.LastScheduleTime = now
		targetState.Revision++
		if err := saveChannelSmartScheduleRouteStateTx(tx, targetState); err != nil {
			return err
		}
		result.State = *targetState
		return nil
	})
	return result, err
}

func ClearExpiredChannelSmartScheduleRoutePrimaries(now int64) (routingChanged bool, err error) {
	if now <= 0 {
		now = common.GetTimestamp()
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		var states []ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).
			Where("manual_primary_until > ? AND manual_primary_until <= ?", 0, now).
			Find(&states).Error; err != nil {
			return err
		}
		for index := range states {
			changed, restoreErr := restoreChannelSmartScheduleRoutePrimaryTx(tx, &states[index])
			if restoreErr != nil {
				return restoreErr
			}
			routingChanged = routingChanged || changed
		}
		return nil
	})
	return routingChanged, err
}

func restoreChannelSmartScheduleRoutePrimaryTx(
	tx *gorm.DB,
	state *ChannelSmartScheduleRouteState,
) (routingChanged bool, err error) {
	if state.ManualPrimaryUntil <= 0 && !state.ManualPrimarySaved {
		return false, nil
	}
	if state.Revision == math.MaxInt64 {
		return false, errors.New("智能调度路由修订号已达上限")
	}
	if state.ManualPrimarySaved && state.StabilityState == "" {
		var ability Ability
		findErr := lockForUpdate(tx).Where(&Ability{
			ChannelId: state.ChannelId, Group: state.GroupName, Model: state.ModelName,
		}).First(&ability).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return false, findErr
		}
		if findErr == nil && (abilityPriority(ability) != state.ManualPrimarySavedPriority ||
			ability.Weight != state.ManualPrimarySavedWeight) {
			priority := state.ManualPrimarySavedPriority
			weight := state.ManualPrimarySavedWeight
			if err := updateAbilitySmartSchedulePriorityWeightTx(
				tx,
				channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName),
				&priority,
				&weight,
			); err != nil {
				return false, err
			}
			routingChanged = true
		}
	}
	if state.ManualPrimarySaved && state.StabilityState != "" {
		state.StabilitySavedPriority = state.ManualPrimarySavedPriority
		state.StabilitySavedWeight = state.ManualPrimarySavedWeight
	}
	state.ManualPrimaryUntil = 0
	state.ManualPrimaryAllowStabilityDegrade = false
	state.ManualPrimarySaved = false
	state.ManualPrimarySavedPriority = 0
	state.ManualPrimarySavedWeight = 0
	state.Revision++
	if err := saveChannelSmartScheduleRouteStateTx(tx, state); err != nil {
		return false, err
	}
	return routingChanged, nil
}

func ClearChannelSmartScheduleExplorations() (routingChanged bool, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		var states []ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).Where("temporary_traffic_kind <> ?", "").Find(&states).Error; err != nil {
			return err
		}
		for index := range states {
			state := &states[index]
			if state.Revision == math.MaxInt64 {
				return errors.New("智能调度路由修订号已达上限")
			}
			key := channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)
			var ability Ability
			findErr := lockForUpdate(tx).Where(&Ability{
				ChannelId: state.ChannelId, Group: state.GroupName, Model: state.ModelName,
			}).First(&ability).Error
			if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
				return findErr
			}
			if findErr == nil && (abilityPriority(ability) != state.BasePriority ||
				ability.Weight != state.BaseWeight) {
				priority := state.BasePriority
				weight := state.BaseWeight
				if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &priority, &weight); err != nil {
					return err
				}
				routingChanged = true
			}
			state.TemporaryTrafficKind = ""
			state.TemporaryTrafficSince = 0
			state.TemporaryTrafficTargetPercent = 0
			state.Revision++
			if err := saveChannelSmartScheduleRouteStateTx(tx, state); err != nil {
				return err
			}
		}
		return nil
	})
	return routingChanged, err
}

type ChannelSmartScheduleRuntimeFailureResult struct {
	Handled        bool
	RoutingChanged bool
	PreviousState  string
}

const (
	channelSmartScheduleRuntimeFallbackPriority int64 = 80
	channelSmartScheduleRuntimeFallbackWeight   uint  = 10
)

// ProtectChannelSmartScheduleRouteOnRuntimeFailure immediately removes
// temporary traffic from a route that failed while it was being explored or
// stability-tested. The normal scheduler remains responsible for recovery.
func ProtectChannelSmartScheduleRouteOnRuntimeFailure(
	channelId int,
	group string,
	modelName string,
	protectionUntil int64,
	reason string,
) (result ChannelSmartScheduleRuntimeFailureResult, err error) {
	group = strings.TrimSpace(group)
	modelName = strings.TrimSpace(modelName)
	if channelId <= 0 || group == "" || modelName == "" {
		return result, nil
	}
	now := common.GetTimestamp()
	if protectionUntil <= now {
		protectionUntil = now + 60
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "渠道运行时错误，已立即停止临时流量并进入稳定性保护"
	}
	if messageRunes := []rune(reason); len(messageRunes) > 255 {
		reason = string(messageRunes[:255])
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		var state ChannelSmartScheduleRouteState
		findErr := lockForUpdate(tx).
			Where(&ChannelSmartScheduleRouteState{
				ChannelId: channelId, GroupName: group, ModelName: modelName,
			}).First(&state).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			return nil
		}
		if findErr != nil {
			return findErr
		}
		var ability Ability
		findErr = lockForUpdate(tx).Where(&Ability{
			ChannelId: channelId, Group: group, Model: modelName,
		}).First(&ability).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			return nil
		}
		if findErr != nil {
			return findErr
		}
		if !state.Participates() || !ability.Enabled ||
			(state.TemporaryTrafficKind == "" && state.StabilityState != ChannelSmartScheduleStabilityProbing) {
			return nil
		}
		if state.ManualPrimaryUntil > now && !state.ManualPrimaryAllowStabilityDegrade {
			return nil
		}
		if state.Revision == math.MaxInt64 {
			return errors.New("智能调度路由修订号已达上限")
		}

		result.Handled = true
		result.PreviousState = state.StabilityState
		savedPriority := state.StabilitySavedPriority
		savedWeight := state.StabilitySavedWeight
		if state.TemporaryTrafficKind != "" {
			if state.BasePriority > 0 {
				savedPriority = state.BasePriority
			}
			if state.BaseWeight > 0 {
				savedWeight = state.BaseWeight
			}
			state.TemporaryTrafficKind = ""
			state.TemporaryTrafficSince = 0
			state.TemporaryTrafficTargetPercent = 0
		}
		if savedPriority <= 0 {
			savedPriority = abilityPriority(ability)
		}
		if savedPriority <= 0 {
			savedPriority = channelSmartScheduleRuntimeFallbackPriority
		}
		if savedWeight == 0 {
			savedWeight = ability.Weight
		}
		if savedWeight == 0 {
			savedWeight = channelSmartScheduleRuntimeFallbackWeight
		}

		degradedPriority := int64(0)
		degradedWeight := uint(0)
		if abilityPriority(ability) != degradedPriority || ability.Weight != degradedWeight {
			if err := updateAbilitySmartSchedulePriorityWeightTx(
				tx,
				channelSmartScheduleRouteKey(channelId, group, modelName),
				&degradedPriority,
				&degradedWeight,
			); err != nil {
				return err
			}
			result.RoutingChanged = true
		}
		state.StabilityState = ChannelSmartScheduleStabilityDegraded
		state.StabilityUntil = protectionUntil
		state.StabilitySince = 0
		state.StabilitySavedPriority = savedPriority
		state.StabilitySavedWeight = savedWeight
		state.RuntimeProtectionUntil = protectionUntil
		state.LastScheduleStatus = ChannelSmartScheduleStatusFailed
		state.LastScheduleError = reason
		state.LastScheduleScore = nil
		state.LastScheduleScoreDetails = ""
		state.LastSchedulePriority = degradedPriority
		state.LastScheduleWeight = degradedWeight
		state.LastScheduleTime = now
		state.Revision++
		return saveChannelSmartScheduleRouteStateTx(tx, &state)
	})
	if err != nil {
		result.Handled = false
		result.RoutingChanged = false
	}
	return result, err
}

func ApplyChannelSmartScheduleRouteResults(results []ChannelSmartScheduleRouteResultUpdate) ([]ChannelSmartScheduleRouteApplyOutcome, error) {
	if len(results) == 0 {
		return nil, nil
	}
	group := results[0].Group
	modelName := results[0].Model
	seenChannels := make(map[int]struct{}, len(results))
	outcomes := make([]ChannelSmartScheduleRouteApplyOutcome, len(results))
	for index, result := range results {
		if result.Group != group || result.Model != modelName {
			return nil, errors.New("智能调度路由结果必须属于同一分组和模型池")
		}
		if _, exists := seenChannels[result.ChannelId]; exists {
			return nil, errors.New("智能调度池包含重复渠道")
		}
		seenChannels[result.ChannelId] = struct{}{}
		outcomes[index].Key = channelSmartScheduleRouteKey(result.ChannelId, result.Group, result.Model)
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		guarded := false
		for _, result := range results {
			guarded = guarded || result.GuardCurrent
		}
		controlRevision := ""
		if guarded {
			var controlOption Option
			optionErr := lockForUpdate(tx).Where(&Option{Key: ChannelSmartScheduleControlRevisionOption}).First(&controlOption).Error
			if optionErr != nil && !errors.Is(optionErr, gorm.ErrRecordNotFound) {
				return optionErr
			}
			if optionErr == nil {
				controlRevision = controlOption.Value
			}
		}

		states := make([]ChannelSmartScheduleRouteState, len(results))
		abilities := make([]Ability, len(results))
		for index, result := range results {
			var state ChannelSmartScheduleRouteState
			findErr := lockForUpdate(tx).
				Where(&ChannelSmartScheduleRouteState{ChannelId: result.ChannelId, GroupName: result.Group, ModelName: result.Model}).
				First(&state).Error
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				state = ChannelSmartScheduleRouteState{
					ChannelId: result.ChannelId, GroupName: result.Group, ModelName: result.Model,
					ParticipationSet: true, Excluded: true,
				}
			} else if findErr != nil {
				return findErr
			}
			var ability Ability
			if err := lockForUpdate(tx).
				Where(&Ability{ChannelId: result.ChannelId, Group: result.Group, Model: result.Model}).
				First(&ability).Error; err != nil {
				return err
			}
			if result.GuardCurrent {
				if controlRevision != result.ExpectedControlRevision || state.Revision != result.ExpectedRevision ||
					!state.Participates() || !ability.Enabled || abilityPriority(ability) != result.ExpectedPriority ||
					ability.Weight != result.ExpectedWeight {
					return nil
				}
				var channel Channel
				if err := lockForUpdate(tx).Select("id", "status").Where("id = ?", result.ChannelId).First(&channel).Error; err != nil {
					return err
				}
				if channel.Status != common.ChannelStatusEnabled {
					return nil
				}
			}
			states[index] = state
			abilities[index] = ability
		}

		for index, result := range results {
			key := outcomes[index].Key
			state := states[index]
			ability := abilities[index]
			message := strings.TrimSpace(result.Error)
			messageRunes := []rune(message)
			if len(messageRunes) > 255 {
				message = string(messageRunes[:255])
			}
			updatedTime := result.Time
			if updatedTime <= 0 {
				updatedTime = common.GetTimestamp()
			}
			scoreDetails, err := EncodeChannelSmartScheduleScoreDetails(result.ScoreDetails)
			if err != nil {
				return fmt.Errorf("保存智能调度评分明细失败: %w", err)
			}
			state.LastScheduleStatus = result.Status
			state.LastScheduleError = message
			state.LastScheduleScore = result.Score
			state.LastScheduleScoreDetails = scoreDetails
			state.LastSchedulePriority = result.Priority
			state.LastScheduleWeight = result.Weight
			state.LastScheduleTime = updatedTime
			if result.Stability != nil {
				state.StabilityState = result.Stability.State
				state.StabilityUntil = result.Stability.Until
				state.StabilitySince = result.Stability.Since
				state.StabilitySavedPriority = result.Stability.SavedPriority
				state.StabilitySavedWeight = result.Stability.SavedWeight
			}
			if result.Jitter != nil {
				state.JitterBaselineFirstTokenMs = result.Jitter.BaselineFirstTokenMs
				state.JitterBaselineUpdatedAt = result.Jitter.BaselineUpdatedAt
			}
			if result.RuntimeProtectionUntil != nil {
				state.RuntimeProtectionUntil = *result.RuntimeProtectionUntil
			}
			if result.RoutingSnapshot != nil {
				state.BaseRank = result.RoutingSnapshot.BaseRank
				state.BasePriority = result.RoutingSnapshot.BasePriority
				state.BaseWeight = result.RoutingSnapshot.BaseWeight
				state.TemporaryTrafficKind = result.RoutingSnapshot.TemporaryTrafficKind
				state.TemporaryTrafficSince = result.RoutingSnapshot.TemporaryTrafficSince
				state.TemporaryTrafficTargetPercent = result.RoutingSnapshot.TemporaryTrafficTargetPercent
				state.LastPrioritySampleTime = result.RoutingSnapshot.LastPrioritySampleTime
			}
			if state.Revision == math.MaxInt64 {
				return errors.New("智能调度路由修订号已达上限")
			}
			state.Revision++
			if result.ApplyPriorityWeight {
				priority := result.Priority
				weight := result.Weight
				if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &priority, &weight); err != nil {
					return err
				}
				outcomes[index].RoutingChanged = priority != abilityPriority(ability) || weight != ability.Weight
			}
			if state.Id == 0 {
				if err := tx.Create(&state).Error; err != nil {
					return err
				}
			} else if err := saveChannelSmartScheduleRouteStateTx(tx, &state); err != nil {
				return err
			}
			outcomes[index].Applied = true
		}
		return nil
	})
	if err != nil {
		for index := range outcomes {
			outcomes[index].Applied = false
			outcomes[index].RoutingChanged = false
		}
	}
	return outcomes, err
}

func ClearChannelSmartScheduleRouteStability(channelId int, group string, modelName string, fallbackPriority int64, fallbackWeight uint) (result ChannelSmartScheduleStabilityClearResult, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		var state ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).
			Where(&ChannelSmartScheduleRouteState{ChannelId: channelId, GroupName: group, ModelName: modelName}).
			First(&state).Error; err != nil {
			return err
		}
		var ability Ability
		if err := lockForUpdate(tx).Where(&Ability{ChannelId: channelId, Group: group, Model: modelName}).First(&ability).Error; err != nil {
			return err
		}
		result.PreviousState = state.StabilityState
		result.Priority = abilityPriority(ability)
		result.Weight = ability.Weight
		if result.PreviousState == "" {
			return nil
		}
		if state.Revision == math.MaxInt64 {
			return errors.New("智能调度路由修订号已达上限")
		}
		result.Priority = state.StabilitySavedPriority
		if result.Priority <= 0 {
			result.Priority = fallbackPriority
		}
		result.Weight = state.StabilitySavedWeight
		if result.Weight == 0 {
			result.Weight = fallbackWeight
		}
		key := channelSmartScheduleRouteKey(channelId, group, modelName)
		if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &result.Priority, &result.Weight); err != nil {
			return err
		}
		now := common.GetTimestamp()
		state.StabilityState = ""
		state.StabilityUntil = 0
		state.StabilitySince = now
		state.StabilitySavedPriority = 0
		state.StabilitySavedWeight = 0
		state.RuntimeProtectionUntil = 0
		state.JitterBaselineFirstTokenMs = nil
		state.JitterBaselineUpdatedAt = 0
		state.LastScheduleStatus = ChannelSmartScheduleStatusSucceeded
		state.LastScheduleError = "管理员已手动解除稳定性保护"
		state.LastScheduleScore = nil
		state.LastScheduleScoreDetails = ""
		state.LastSchedulePriority = result.Priority
		state.LastScheduleWeight = result.Weight
		state.LastScheduleTime = now
		state.Revision++
		result.Cleared = true
		return saveChannelSmartScheduleRouteStateTx(tx, &state)
	})
	return result, err
}

func updateAbilitySmartSchedulePriorityWeightTx(tx *gorm.DB, key ChannelSmartScheduleRouteKey, priority *int64, weight *uint) error {
	updates := make(map[string]any, 2)
	if priority != nil {
		updates["priority"] = *priority
	}
	if weight != nil {
		updates["weight"] = *weight
	}
	if len(updates) == 0 {
		return nil
	}
	conditions := &Ability{ChannelId: key.ChannelId, Group: key.Group, Model: key.Model}
	result := tx.Model(&Ability{}).Where(conditions).Updates(updates)
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
