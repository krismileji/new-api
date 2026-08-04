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
	ExplorationMaxPromptTokens    int     `json:"exploration_max_prompt_tokens"`
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

var ErrChannelSmartScheduleRouteStabilityProtected = errors.New("该分组和模型路由处于稳定性保护状态，需要管理员确认后才能固定为主渠道")

type ChannelSmartScheduleRoutePrimaryResult struct {
	State                      ChannelSmartScheduleRouteState
	RoutingChanged             bool
	StabilityProtectionCleared bool
}

type ChannelSmartScheduleRoutePrimaryOptions struct {
	DurationMinutes           int
	AllowStabilityDegrade     bool
	ConfirmStabilityOverride  bool
	StabilityFallbackPriority int64
	StabilityFallbackWeight   uint
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
	ChannelId                int
	Group                    string
	Model                    string
	Status                   string
	Error                    string
	Score                    *float64
	ScoreDetails             *ChannelSmartScheduleScoreDetails
	Priority                 int64
	Weight                   uint
	Time                     int64
	Stability                *ChannelSmartScheduleStabilityUpdate
	Jitter                   *ChannelSmartScheduleJitterUpdate
	RuntimeProtectionUntil   *int64
	RoutingSnapshot          *ChannelSmartScheduleRoutingSnapshotUpdate
	GuardCurrent             bool
	PoolGuard                bool
	ObservationOnly          bool
	ExpectedRevision         int64
	ExpectedControlRevision  string
	ExpectedParticipationSet bool
	ExpectedExcluded         bool
	ExpectedAbilityEnabled   bool
	ExpectedChannelStatus    int
	ExpectedPriority         int64
	ExpectedWeight           uint
	ApplyPriorityWeight      bool
}

type ChannelSmartScheduleRouteApplyOutcome struct {
	Key             ChannelSmartScheduleRouteKey
	Applied         bool
	RoutingChanged  bool
	ObservationOnly bool
}

type ChannelSmartScheduleStabilityClearResult struct {
	PreviousState  string
	Cleared        bool
	RoutingChanged bool
	Priority       int64
	Weight         uint
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
	ExplorationMaxPromptTokens    int
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

func channelSmartScheduleManualPrimaryPriority(
	abilities []Ability,
	channelStatusById map[int]int,
	channelId int,
	minimumPriority int64,
) (int64, error) {
	highestOtherPriority := int64(0)
	for _, ability := range abilities {
		if ability.ChannelId == channelId || !ability.Enabled {
			continue
		}
		if status, exists := channelStatusById[ability.ChannelId]; !exists || status != common.ChannelStatusEnabled {
			continue
		}
		highestOtherPriority = max(highestOtherPriority, abilityPriority(ability))
	}
	if highestOtherPriority == math.MaxInt64 {
		return 0, errors.New("当前路由优先级已达上限，不能固定主渠道")
	}
	return max(highestOtherPriority+1, minimumPriority), nil
}

func channelSmartScheduleRouteKey(channelId int, group string, modelName string) ChannelSmartScheduleRouteKey {
	return ChannelSmartScheduleRouteKey{
		ChannelId: channelId,
		Group:     group,
		Model:     modelName,
	}
}

func InitializeChannelSmartScheduleRouteStates() error {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	return DB.Transaction(func(tx *gorm.DB) error {
		var channels []Channel
		if err := lockForUpdate(tx).
			Select("id").
			Order("id ASC").
			Find(&channels).Error; err != nil {
			return err
		}
		if len(channels) == 0 {
			return nil
		}
		channelIds := make([]int, len(channels))
		for index := range channels {
			channelIds[index] = channels[index].Id
		}

		var states []ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).
			Order("channel_id ASC, group_name ASC, model_name ASC").
			Find(&states).Error; err != nil {
			return err
		}
		var abilities []Ability
		if err := lockForUpdate(tx).
			Where("channel_id IN ?", channelIds).
			Order("channel_id ASC").
			Order(clause.OrderByColumn{Column: clause.Column{Name: "group"}}).
			Order(clause.OrderByColumn{Column: clause.Column{Name: "model"}}).
			Find(&abilities).Error; err != nil {
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
		modelName := channelSmartScheduleModelName(state.ModelName)
		state.ModelName = modelName
		sharedSamplesByModel[channelSmartScheduleModelKey{channelId: state.ChannelId, modelName: modelName}] = state
	}
	routes := make([]ChannelSmartScheduleRoute, 0, len(abilities))
	for _, ability := range abilities {
		channel, exists := channelById[ability.ChannelId]
		if !exists {
			continue
		}
		key := channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)
		modelKey := channelSmartScheduleModelKey{
			channelId: ability.ChannelId,
			modelName: channelSmartScheduleModelName(ability.Model),
		}
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
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	err = DB.Transaction(func(tx *gorm.DB) error {
		pools := []channelSmartScheduleRoutePool{{group: group, model: modelName}}
		if _, err := lockChannelSmartScheduleRoutePoolChannelsTx(tx, group, modelName, channelId); err != nil {
			return err
		}
		states, err := lockChannelSmartScheduleRoutePoolStatesTx(tx, pools)
		if err != nil {
			return err
		}
		abilities, err := lockChannelSmartScheduleRoutePoolAbilitiesTx(tx, pools)
		if err != nil {
			return err
		}

		var targetState *ChannelSmartScheduleRouteState
		for index := range states {
			if states[index].ChannelId == channelId {
				targetState = &states[index]
				break
			}
		}
		created := targetState == nil
		if created {
			targetState = &ChannelSmartScheduleRouteState{
				ChannelId: channelId,
				GroupName: group,
				ModelName: modelName,
			}
		}
		var targetAbility *Ability
		for index := range abilities {
			if abilities[index].ChannelId == channelId {
				targetAbility = &abilities[index]
				break
			}
		}
		if targetAbility == nil {
			return gorm.ErrRecordNotFound
		}

		wasParticipating := targetState.Participates()
		if targetState.ParticipationSet && targetState.Excluded == excluded {
			state = *targetState
			return nil
		}
		if targetState.Revision == math.MaxInt64 {
			return errors.New("智能调度路由修订号已达上限")
		}
		targetState.ParticipationSet = true
		targetState.Excluded = excluded
		routingChanged = true
		if excluded {
			if targetState.ManualPrimaryUntil > 0 || targetState.ManualPrimarySaved {
				changed, restoreErr := restoreChannelSmartScheduleRoutePrimaryTx(tx, targetState, targetAbility)
				if restoreErr != nil {
					return restoreErr
				}
				routingChanged = routingChanged || changed
				changed, clearErr := clearChannelSmartScheduleRoutePoolTemporaryTrafficTx(
					tx, states, abilities, group, modelName, common.GetTimestamp(),
				)
				if clearErr != nil {
					return clearErr
				}
				routingChanged = routingChanged || changed
			}
			if targetState.TemporaryTrafficKind != "" {
				priority := targetState.BasePriority
				weight := targetState.BaseWeight
				routingChanged = routingChanged || abilityPriority(*targetAbility) != priority || targetAbility.Weight != weight
				if err := updateAbilitySmartSchedulePriorityWeightTx(
					tx, channelSmartScheduleRouteKey(channelId, group, modelName), &priority, &weight,
				); err != nil {
					return err
				}
				targetAbility.Priority = &priority
				targetAbility.Weight = weight
			}
			targetState.TemporaryTrafficKind = ""
			targetState.TemporaryTrafficSince = 0
			targetState.TemporaryTrafficTargetPercent = 0
			targetState.ExplorationMaxPromptTokens = 0
			targetState.RuntimeProtectionUntil = 0
		}
		if !wasParticipating && !excluded && targetState.StabilityState == ChannelSmartScheduleStabilityProbing {
			targetState.StabilitySince = common.GetTimestamp()
		}
		if targetState.Revision == math.MaxInt64 {
			return errors.New("智能调度路由修订号已达上限")
		}
		targetState.Revision++
		if created {
			if err := tx.Create(targetState).Error; err != nil {
				return err
			}
		} else if err := saveChannelSmartScheduleRouteStateTx(tx, targetState); err != nil {
			return err
		}
		state = *targetState
		return nil
	})
	return state, routingChanged, err
}

func SaveChannelSmartScheduleChannelConfig(channelId int, excluded bool) (result ChannelSmartScheduleChannelConfigResult, err error) {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	err = DB.Transaction(func(tx *gorm.DB) error {
		var channels []Channel
		if err := lockForUpdate(tx).
			Select("id").
			Order("id ASC").
			Find(&channels).Error; err != nil {
			return err
		}
		channelExists := false
		for index := range channels {
			if channels[index].Id == channelId {
				channelExists = true
				break
			}
		}
		if !channelExists {
			return gorm.ErrRecordNotFound
		}

		var configuredAbilities []Ability
		if err := tx.Select("group", "model").
			Where("channel_id = ?", channelId).
			Find(&configuredAbilities).Error; err != nil {
			return err
		}
		pools := channelSmartScheduleRoutePoolsFromAbilities(configuredAbilities)
		if len(pools) == 0 {
			return nil
		}
		states, err := lockChannelSmartScheduleRoutePoolStatesTx(tx, pools)
		if err != nil {
			return err
		}
		abilities, err := lockChannelSmartScheduleRoutePoolAbilitiesTx(tx, pools)
		if err != nil {
			return err
		}

		stateByKey := make(map[ChannelSmartScheduleRouteKey]*ChannelSmartScheduleRouteState, len(states))
		for index := range states {
			state := &states[index]
			stateByKey[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)] = state
		}

		now := common.GetTimestamp()
		for index := range abilities {
			ability := &abilities[index]
			if ability.ChannelId != channelId {
				continue
			}
			result.Total++
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
			result.RoutingChanged = true
			if excluded {
				if state.ManualPrimaryUntil > 0 || state.ManualPrimarySaved {
					changed, restoreErr := restoreChannelSmartScheduleRoutePrimaryTx(tx, state, ability)
					if restoreErr != nil {
						return restoreErr
					}
					result.RoutingChanged = result.RoutingChanged || changed
					changed, clearErr := clearChannelSmartScheduleRoutePoolTemporaryTrafficTx(
						tx, states, abilities, ability.Group, ability.Model, now,
					)
					if clearErr != nil {
						return clearErr
					}
					result.RoutingChanged = result.RoutingChanged || changed
				}
				if state.TemporaryTrafficKind != "" {
					priority := state.BasePriority
					weight := state.BaseWeight
					if abilityPriority(*ability) != priority || ability.Weight != weight {
						result.RoutingChanged = true
					}
					if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &priority, &weight); err != nil {
						return err
					}
					ability.Priority = &priority
					ability.Weight = weight
				}
				state.TemporaryTrafficKind = ""
				state.TemporaryTrafficSince = 0
				state.TemporaryTrafficTargetPercent = 0
				state.ExplorationMaxPromptTokens = 0
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
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	now := common.GetTimestamp()
	err = DB.Transaction(func(tx *gorm.DB) error {
		channels, err := lockChannelSmartScheduleRoutePoolChannelsTx(tx, group, modelName, channelId)
		if err != nil {
			return err
		}
		var targetChannel *Channel
		channelStatusById := make(map[int]int, len(channels))
		for index := range channels {
			channelStatusById[channels[index].Id] = channels[index].Status
			if channels[index].Id == channelId {
				targetChannel = &channels[index]
			}
		}

		var states []ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).
			Where("group_name = ? AND model_name = ?", group, modelName).
			Order("channel_id ASC").
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
		abilities, err := lockChannelSmartScheduleRoutePoolAbilitiesTx(
			tx,
			[]channelSmartScheduleRoutePool{{group: group, model: modelName}},
		)
		if err != nil {
			return err
		}
		abilityByChannel := make(map[int]*Ability, len(abilities))
		for index := range abilities {
			abilityByChannel[abilities[index].ChannelId] = &abilities[index]
		}
		targetAbility := abilityByChannel[channelId]

		if durationMinutes == 0 {
			if targetState.ManualPrimaryUntil <= 0 && !targetState.ManualPrimarySaved {
				result.State = *targetState
				return nil
			}
			changed, restoreErr := restoreChannelSmartScheduleRoutePrimaryTx(tx, targetState, targetAbility)
			if restoreErr != nil {
				return restoreErr
			}
			result.RoutingChanged = result.RoutingChanged || changed
			changed, clearErr := clearChannelSmartScheduleRoutePoolTemporaryTrafficTx(
				tx, states, abilities, group, modelName, now,
			)
			if clearErr != nil {
				return clearErr
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
			changed, restoreErr := restoreChannelSmartScheduleRoutePrimaryTx(
				tx, state, abilityByChannel[state.ChannelId],
			)
			if restoreErr != nil {
				return restoreErr
			}
			result.RoutingChanged = result.RoutingChanged || changed
		}

		if targetAbility == nil {
			return gorm.ErrRecordNotFound
		}

		if targetChannel == nil {
			return gorm.ErrRecordNotFound
		}
		if targetChannel.Status != common.ChannelStatusEnabled {
			return errors.New("渠道已禁用，不能固定为主渠道")
		}
		if !targetAbility.Enabled {
			return errors.New("该分组和模型路由已禁用，不能固定为主渠道")
		}
		if !targetState.Participates() {
			return errors.New("该分组和模型路由未参与智能调度，不能固定为主渠道")
		}
		if targetState.ManualPrimaryUntil > now && targetState.ManualPrimarySaved &&
			targetState.StabilityState != "" && options.AllowStabilityDegrade {
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
			changed, restoreErr := restoreChannelSmartScheduleRoutePrimaryTx(tx, targetState, targetAbility)
			if restoreErr != nil {
				return restoreErr
			}
			result.RoutingChanged = result.RoutingChanged || changed
		}
		if targetState.StabilityState != "" {
			if !options.ConfirmStabilityOverride {
				return ErrChannelSmartScheduleRouteStabilityProtected
			}
			clearResult, clearErr := clearChannelSmartScheduleRouteStabilityTx(
				tx,
				targetState,
				targetAbility,
				options.StabilityFallbackPriority,
				options.StabilityFallbackWeight,
				"管理员确认固定主渠道，已解除稳定性保护",
			)
			if clearErr != nil {
				return clearErr
			}
			result.StabilityProtectionCleared = clearResult.Cleared
			result.RoutingChanged = result.RoutingChanged || clearResult.RoutingChanged
		}

		if targetState.TemporaryTrafficKind != "" {
			restoredPriority := targetState.BasePriority
			restoredWeight := targetState.BaseWeight
			if abilityPriority(*targetAbility) != restoredPriority || targetAbility.Weight != restoredWeight {
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
			targetState.ExplorationMaxPromptTokens = 0
		}

		if targetState.ManualPrimaryUntil <= now || !targetState.ManualPrimarySaved {
			targetState.ManualPrimarySaved = true
			targetState.ManualPrimarySavedPriority = abilityPriority(*targetAbility)
			targetState.ManualPrimarySavedWeight = targetAbility.Weight
		}
		minimumPriority := abilityPriority(*targetAbility)
		if targetState.ManualPrimaryUntil > now && targetState.ManualPrimarySaved {
			minimumPriority = max(minimumPriority, targetState.LastSchedulePriority)
		}
		manualPriority, err := channelSmartScheduleManualPrimaryPriority(
			abilities,
			channelStatusById,
			channelId,
			minimumPriority,
		)
		if err != nil {
			return err
		}
		manualWeight := uint(1000)
		if abilityPriority(*targetAbility) != manualPriority || targetAbility.Weight != manualWeight {
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
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	err = DB.Transaction(func(tx *gorm.DB) error {
		var expiredCount int64
		if err := tx.Model(&ChannelSmartScheduleRouteState{}).
			Where("manual_primary_until > ? AND manual_primary_until <= ?", 0, now).
			Count(&expiredCount).Error; err != nil {
			return err
		}
		if expiredCount == 0 {
			return nil
		}

		var expiredStates []ChannelSmartScheduleRouteState
		if err := tx.Select("channel_id", "group_name", "model_name").
			Where("manual_primary_until > ? AND manual_primary_until <= ?", 0, now).
			Find(&expiredStates).Error; err != nil {
			return err
		}
		expiredPools := make([]channelSmartScheduleRoutePool, 0, len(expiredStates))
		for _, state := range expiredStates {
			expiredPools = append(expiredPools, channelSmartScheduleRoutePool{
				group: state.GroupName,
				model: state.ModelName,
			})
		}
		expiredPools = channelSmartScheduleRoutePoolsFromAbilities(nil, expiredPools...)
		if len(expiredPools) == 0 {
			return nil
		}
		if tx.Migrator().HasTable(&Channel{}) {
			channelIds := make([]int, 0, len(expiredStates))
			for _, state := range expiredStates {
				channelIds = append(channelIds, state.ChannelId)
			}
			for _, pool := range expiredPools {
				var poolChannelIds []int
				if err := tx.Model(&Ability{}).
					Where(&Ability{Group: pool.group, Model: pool.model}).
					Pluck("channel_id", &poolChannelIds).Error; err != nil {
					return err
				}
				channelIds = append(channelIds, poolChannelIds...)
			}
			if _, err := lockChannelsForDependentWriteTx(tx, channelIds); err != nil {
				return err
			}
		}
		states, err := lockChannelSmartScheduleRoutePoolStatesTx(tx, expiredPools)
		if err != nil {
			return err
		}
		abilities, err := lockChannelSmartScheduleRoutePoolAbilitiesTx(tx, expiredPools)
		if err != nil {
			return err
		}
		abilityByKey := make(map[ChannelSmartScheduleRouteKey]*Ability, len(abilities))
		for index := range abilities {
			ability := &abilities[index]
			abilityByKey[channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)] = ability
		}

		for index := range states {
			state := &states[index]
			if state.ManualPrimaryUntil <= 0 || state.ManualPrimaryUntil > now {
				continue
			}
			changed, restoreErr := restoreChannelSmartScheduleRoutePrimaryTx(
				tx,
				state,
				abilityByKey[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)],
			)
			if restoreErr != nil {
				return restoreErr
			}
			routingChanged = routingChanged || changed
		}
		for _, pool := range expiredPools {
			changed, clearErr := clearChannelSmartScheduleRoutePoolTemporaryTrafficTx(
				tx, states, abilities, pool.group, pool.model, now,
			)
			if clearErr != nil {
				return clearErr
			}
			routingChanged = routingChanged || changed
		}
		return nil
	})
	if err != nil {
		routingChanged = false
	}
	return routingChanged, err
}

func restoreChannelSmartScheduleRoutePrimaryTx(
	tx *gorm.DB,
	state *ChannelSmartScheduleRouteState,
	ability *Ability,
) (routingChanged bool, err error) {
	if state.ManualPrimaryUntil <= 0 && !state.ManualPrimarySaved {
		return false, nil
	}
	if state.Revision == math.MaxInt64 {
		return false, errors.New("智能调度路由修订号已达上限")
	}
	if state.ManualPrimarySaved && state.StabilityState == "" && ability != nil {
		if abilityPriority(*ability) != state.ManualPrimarySavedPriority ||
			ability.Weight != state.ManualPrimarySavedWeight {
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
			ability.Priority = &priority
			ability.Weight = weight
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

func clearChannelSmartScheduleRoutePoolTemporaryTrafficTx(
	tx *gorm.DB,
	states []ChannelSmartScheduleRouteState,
	abilities []Ability,
	group string,
	modelName string,
	now int64,
) (routingChanged bool, err error) {
	if now <= 0 {
		now = common.GetTimestamp()
	}
	abilityByKey := make(map[ChannelSmartScheduleRouteKey]*Ability, len(abilities))
	for index := range abilities {
		ability := &abilities[index]
		abilityByKey[channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)] = ability
	}
	for index := range states {
		state := &states[index]
		ability := abilityByKey[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)]
		if state.GroupName == group && state.ModelName == modelName &&
			state.ManualPrimaryUntil > now && state.Participates() && state.StabilityState == "" &&
			ability != nil && ability.Enabled {
			return false, nil
		}
	}

	for index := range states {
		state := &states[index]
		if state.GroupName != group || state.ModelName != modelName || state.TemporaryTrafficKind == "" {
			continue
		}
		if state.Revision == math.MaxInt64 {
			return false, errors.New("智能调度路由修订号已达上限")
		}
		key := channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)
		ability := abilityByKey[key]
		if ability != nil && (abilityPriority(*ability) != state.BasePriority || ability.Weight != state.BaseWeight) {
			priority := state.BasePriority
			weight := state.BaseWeight
			if err := updateAbilitySmartSchedulePriorityWeightTx(
				tx,
				key,
				&priority,
				&weight,
			); err != nil {
				return false, err
			}
			ability.Priority = &priority
			ability.Weight = weight
			routingChanged = true
		}
		state.TemporaryTrafficKind = ""
		state.TemporaryTrafficSince = 0
		state.TemporaryTrafficTargetPercent = 0
		state.ExplorationMaxPromptTokens = 0
		state.Revision++
		if err := saveChannelSmartScheduleRouteStateTx(tx, state); err != nil {
			return false, err
		}
		routingChanged = true
	}
	return routingChanged, nil
}

func ClearChannelSmartScheduleTemporaryTraffic() (routingChanged bool, err error) {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	err = DB.Transaction(func(tx *gorm.DB) error {
		var clearErr error
		routingChanged, clearErr = clearChannelSmartScheduleTemporaryTrafficTx(tx)
		return clearErr
	})
	if err != nil {
		routingChanged = false
	}
	return routingChanged, err
}

func clearChannelSmartScheduleTemporaryTrafficTx(tx *gorm.DB) (routingChanged bool, err error) {
	var affectedStates []ChannelSmartScheduleRouteState
	if err := tx.
		Where("temporary_traffic_kind <> ? OR stability_state = ?", "", ChannelSmartScheduleStabilityProbing).
		Order("channel_id ASC, group_name ASC, model_name ASC").
		Find(&affectedStates).Error; err != nil {
		return false, err
	}
	if len(affectedStates) == 0 {
		return false, nil
	}
	pools := make([]channelSmartScheduleRoutePool, 0, len(affectedStates))
	for _, state := range affectedStates {
		pools = append(pools, channelSmartScheduleRoutePool{group: state.GroupName, model: state.ModelName})
	}
	pools = channelSmartScheduleRoutePoolsFromAbilities(nil, pools...)
	if tx.Migrator().HasTable(&Channel{}) {
		channelIds := make([]int, 0, len(affectedStates))
		for _, state := range affectedStates {
			channelIds = append(channelIds, state.ChannelId)
		}
		for _, pool := range pools {
			var poolChannelIds []int
			if err := tx.Model(&Ability{}).
				Where(&Ability{Group: pool.group, Model: pool.model}).
				Pluck("channel_id", &poolChannelIds).Error; err != nil {
				return false, err
			}
			channelIds = append(channelIds, poolChannelIds...)
		}
		if _, err := lockChannelsForDependentWriteTx(tx, channelIds); err != nil {
			return false, err
		}
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
	for index := range abilities {
		ability := &abilities[index]
		abilityByKey[channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)] = ability
	}

	for index := range states {
		state := &states[index]
		if state.TemporaryTrafficKind == "" && state.StabilityState != ChannelSmartScheduleStabilityProbing {
			continue
		}
		if state.Revision == math.MaxInt64 {
			return false, errors.New("智能调度路由修订号已达上限")
		}
		key := channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)
		ability := abilityByKey[key]
		targetPriority := state.BasePriority
		targetWeight := state.BaseWeight
		if state.StabilityState == ChannelSmartScheduleStabilityProbing {
			targetPriority = 0
			targetWeight = 0
			state.StabilityState = ChannelSmartScheduleStabilityDegraded
			state.StabilitySince = 0
		}
		if ability != nil && (abilityPriority(*ability) != targetPriority ||
			ability.Weight != targetWeight) {
			priority := targetPriority
			weight := targetWeight
			if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &priority, &weight); err != nil {
				return false, err
			}
			ability.Priority = &priority
			ability.Weight = weight
			routingChanged = true
		}
		state.TemporaryTrafficKind = ""
		state.TemporaryTrafficSince = 0
		state.TemporaryTrafficTargetPercent = 0
		state.ExplorationMaxPromptTokens = 0
		state.Revision++
		if err := saveChannelSmartScheduleRouteStateTx(tx, state); err != nil {
			return false, err
		}
	}
	if err := reapplyChannelSmartScheduleRoutePrimariesTx(tx, pools); err != nil {
		return false, err
	}
	if len(states) > 0 {
		routingChanged = true
	}
	return routingChanged, nil
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
	expectedControlRevision string,
) (result ChannelSmartScheduleRuntimeFailureResult, err error) {
	return protectChannelSmartScheduleRouteOnRuntimeFailure(
		channelId, group, modelName, protectionUntil, reason, expectedControlRevision, false,
	)
}

// ProtectChannelSmartScheduleRouteOnShortTermFailure also protects a normal
// participating route after its shared channel/model runtime failure threshold
// is reached.
func ProtectChannelSmartScheduleRouteOnShortTermFailure(
	channelId int,
	group string,
	modelName string,
	protectionUntil int64,
	reason string,
	expectedControlRevision string,
) (result ChannelSmartScheduleRuntimeFailureResult, err error) {
	return protectChannelSmartScheduleRouteOnRuntimeFailure(
		channelId, group, modelName, protectionUntil, reason, expectedControlRevision, true,
	)
}

func protectChannelSmartScheduleRouteOnRuntimeFailure(
	channelId int,
	group string,
	modelName string,
	protectionUntil int64,
	reason string,
	expectedControlRevision string,
	allowNormalRoute bool,
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
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	err = DB.Transaction(func(tx *gorm.DB) error {
		controlRevision, err := lockChannelSmartScheduleControlRevisionTx(tx)
		if err != nil {
			return err
		}
		if controlRevision != expectedControlRevision {
			return nil
		}
		pool := channelSmartScheduleRoutePool{group: group, model: modelName}
		if _, err := lockChannelSmartScheduleRoutePoolChannelsTx(tx, group, modelName, channelId); err != nil {
			return err
		}
		states, err := lockChannelSmartScheduleRoutePoolStatesTx(tx, []channelSmartScheduleRoutePool{pool})
		if err != nil {
			return err
		}
		abilities, err := lockChannelSmartScheduleRoutePoolAbilitiesTx(tx, []channelSmartScheduleRoutePool{pool})
		if err != nil {
			return err
		}
		var state *ChannelSmartScheduleRouteState
		for index := range states {
			if states[index].ChannelId == channelId {
				state = &states[index]
				break
			}
		}
		var ability *Ability
		for index := range abilities {
			if abilities[index].ChannelId == channelId {
				ability = &abilities[index]
				break
			}
		}
		if state == nil || ability == nil || !state.Participates() || !ability.Enabled {
			return nil
		}
		activeTemporaryTraffic := state.TemporaryTrafficKind != "" ||
			state.StabilityState == ChannelSmartScheduleStabilityProbing
		activeFixedPrimary := state.ManualPrimaryUntil > now &&
			state.ManualPrimaryAllowStabilityDegrade && state.StabilityState == ""
		normalRouteEligible := allowNormalRoute &&
			(state.StabilityState == "" || state.StabilityState == ChannelSmartScheduleStabilityDegraded)
		manualPrimaryBlocksDegrade := state.ManualPrimaryUntil > now &&
			!state.ManualPrimaryAllowStabilityDegrade && state.StabilityState == ""
		if manualPrimaryBlocksDegrade || (!activeTemporaryTraffic && !activeFixedPrimary && !normalRouteEligible) {
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
			state.ExplorationMaxPromptTokens = 0
		}
		if savedPriority <= 0 {
			savedPriority = abilityPriority(*ability)
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
		state.StabilityState = ChannelSmartScheduleStabilityDegraded
		state.StabilityUntil = max(state.StabilityUntil, protectionUntil)
		state.StabilitySince = 0
		state.StabilitySavedPriority = savedPriority
		state.StabilitySavedWeight = savedWeight
		state.RuntimeProtectionUntil = max(state.RuntimeProtectionUntil, protectionUntil)

		if state.ManualPrimaryUntil > now {
			changed, clearErr := clearChannelSmartScheduleRoutePoolTemporaryTrafficTx(
				tx, states, abilities, group, modelName, now,
			)
			if clearErr != nil {
				return clearErr
			}
			result.RoutingChanged = result.RoutingChanged || changed
		}
		if abilityPriority(*ability) != degradedPriority || ability.Weight != degradedWeight {
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
		state.LastScheduleStatus = ChannelSmartScheduleStatusFailed
		state.LastScheduleError = reason
		state.LastScheduleScore = nil
		state.LastScheduleScoreDetails = ""
		state.LastSchedulePriority = degradedPriority
		state.LastScheduleWeight = degradedWeight
		state.LastScheduleTime = now
		state.Revision++
		return saveChannelSmartScheduleRouteStateTx(tx, state)
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
	poolGuarded := false
	for index, result := range results {
		if result.Group != group || result.Model != modelName {
			return nil, errors.New("智能调度路由结果必须属于同一分组和模型池")
		}
		if _, exists := seenChannels[result.ChannelId]; exists {
			return nil, errors.New("智能调度池包含重复渠道")
		}
		seenChannels[result.ChannelId] = struct{}{}
		outcomes[index].Key = channelSmartScheduleRouteKey(result.ChannelId, result.Group, result.Model)
		outcomes[index].ObservationOnly = result.ObservationOnly
		poolGuarded = poolGuarded || result.PoolGuard
	}
	if poolGuarded {
		for _, result := range results {
			if !result.PoolGuard {
				return nil, errors.New("智能调度整池保护要求包含池内全部路由")
			}
		}
	}
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	err := DB.Transaction(func(tx *gorm.DB) error {
		controlRevision, err := lockChannelSmartScheduleControlRevisionTx(tx)
		if err != nil {
			return err
		}
		resultChannelIds := make([]int, 0, len(results))
		for _, result := range results {
			resultChannelIds = append(resultChannelIds, result.ChannelId)
		}
		channels, err := lockChannelSmartScheduleRoutePoolChannelsTx(
			tx, group, modelName, resultChannelIds...,
		)
		if err != nil {
			return err
		}
		channelStatusById := make(map[int]int, len(channels))
		for _, channel := range channels {
			channelStatusById[channel.Id] = channel.Status
		}

		var poolStates []ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).
			Where("group_name = ? AND model_name = ?", group, modelName).
			Order("channel_id ASC").
			Find(&poolStates).Error; err != nil {
			return err
		}
		stateByChannel := make(map[int]ChannelSmartScheduleRouteState, len(poolStates))
		for _, state := range poolStates {
			stateByChannel[state.ChannelId] = state
		}

		var poolAbilities []Ability
		if err := lockForUpdate(tx).
			Where(&Ability{Group: group, Model: modelName}).
			Order("channel_id ASC").
			Find(&poolAbilities).Error; err != nil {
			return err
		}
		abilityByChannel := make(map[int]Ability, len(poolAbilities))
		for _, ability := range poolAbilities {
			abilityByChannel[ability.ChannelId] = ability
		}
		if poolGuarded {
			if len(poolAbilities) != len(results) {
				return nil
			}
			for channelId := range abilityByChannel {
				if _, exists := seenChannels[channelId]; !exists {
					return nil
				}
			}
		}

		states := make([]ChannelSmartScheduleRouteState, len(results))
		abilities := make([]Ability, len(results))
		for index, result := range results {
			state, stateExists := stateByChannel[result.ChannelId]
			if !stateExists {
				if result.PoolGuard {
					return nil
				}
				state = ChannelSmartScheduleRouteState{
					ChannelId: result.ChannelId, GroupName: result.Group, ModelName: result.Model,
					ParticipationSet: true, Excluded: true,
				}
			}
			ability, abilityExists := abilityByChannel[result.ChannelId]
			if !abilityExists {
				if result.PoolGuard {
					return nil
				}
				return gorm.ErrRecordNotFound
			}
			if result.PoolGuard {
				channelStatus, channelExists := channelStatusById[result.ChannelId]
				if !channelExists || controlRevision != result.ExpectedControlRevision ||
					state.Revision != result.ExpectedRevision ||
					state.ParticipationSet != result.ExpectedParticipationSet ||
					state.Excluded != result.ExpectedExcluded ||
					ability.Enabled != result.ExpectedAbilityEnabled ||
					abilityPriority(ability) != result.ExpectedPriority ||
					ability.Weight != result.ExpectedWeight ||
					channelStatus != result.ExpectedChannelStatus {
					return nil
				}
			} else if result.GuardCurrent {
				if controlRevision != result.ExpectedControlRevision || state.Revision != result.ExpectedRevision ||
					!state.Participates() || !ability.Enabled || abilityPriority(ability) != result.ExpectedPriority ||
					ability.Weight != result.ExpectedWeight ||
					channelStatusById[result.ChannelId] != common.ChannelStatusEnabled {
					return nil
				}
			}
			states[index] = state
			abilities[index] = ability
		}

		for index, result := range results {
			if result.ObservationOnly {
				outcomes[index].Applied = true
				continue
			}
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
				if state.ManualPrimarySaved {
					state.ManualPrimarySavedPriority = result.RoutingSnapshot.BasePriority
					state.ManualPrimarySavedWeight = result.RoutingSnapshot.BaseWeight
				}
				state.TemporaryTrafficKind = result.RoutingSnapshot.TemporaryTrafficKind
				state.TemporaryTrafficSince = result.RoutingSnapshot.TemporaryTrafficSince
				state.TemporaryTrafficTargetPercent = result.RoutingSnapshot.TemporaryTrafficTargetPercent
				state.ExplorationMaxPromptTokens = result.RoutingSnapshot.ExplorationMaxPromptTokens
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
		var state ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).
			Where(&ChannelSmartScheduleRouteState{ChannelId: channelId, GroupName: group, ModelName: modelName}).
			First(&state).Error; err != nil {
			return err
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
			}
		}
		if ability == nil {
			return gorm.ErrRecordNotFound
		}
		var clearErr error
		result, clearErr = clearChannelSmartScheduleRouteStabilityTx(
			tx,
			&state,
			ability,
			fallbackPriority,
			fallbackWeight,
			"管理员已手动解除稳定性保护",
		)
		if clearErr != nil || !result.Cleared {
			return clearErr
		}
		now := common.GetTimestamp()
		if state.ManualPrimaryUntil <= now || !ability.Enabled ||
			channelStatusById[channelId] != common.ChannelStatusEnabled {
			return nil
		}
		manualPriority, priorityErr := channelSmartScheduleManualPrimaryPriority(
			abilities,
			channelStatusById,
			channelId,
			max(abilityPriority(*ability), state.LastSchedulePriority),
		)
		if priorityErr != nil {
			return priorityErr
		}
		manualWeight := uint(1000)
		if abilityPriority(*ability) != manualPriority || ability.Weight != manualWeight {
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
		if state.Revision == math.MaxInt64 {
			return errors.New("智能调度路由修订号已达上限")
		}
		state.LastScheduleStatus = ChannelSmartScheduleStatusSucceeded
		state.LastScheduleError = "管理员已手动解除稳定性保护，固定意图仍有效，已重新置顶"
		state.LastSchedulePriority = manualPriority
		state.LastScheduleWeight = manualWeight
		state.LastScheduleTime = now
		state.Revision++
		if err := saveChannelSmartScheduleRouteStateTx(tx, &state); err != nil {
			return err
		}
		result.Priority = manualPriority
		result.Weight = manualWeight
		return nil
	})
	return result, err
}

func clearChannelSmartScheduleRouteStabilityTx(
	tx *gorm.DB,
	state *ChannelSmartScheduleRouteState,
	ability *Ability,
	fallbackPriority int64,
	fallbackWeight uint,
	reason string,
) (result ChannelSmartScheduleStabilityClearResult, err error) {
	result.PreviousState = state.StabilityState
	result.Priority = abilityPriority(*ability)
	result.Weight = ability.Weight
	if result.PreviousState == "" {
		return result, nil
	}
	if state.Revision == math.MaxInt64 {
		return result, errors.New("智能调度路由修订号已达上限")
	}

	result.Priority = state.StabilitySavedPriority
	if result.Priority <= 0 {
		result.Priority = fallbackPriority
	}
	result.Weight = state.StabilitySavedWeight
	if result.Weight == 0 {
		result.Weight = fallbackWeight
	}
	if abilityPriority(*ability) != result.Priority || ability.Weight != result.Weight {
		key := channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)
		if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &result.Priority, &result.Weight); err != nil {
			return result, err
		}
		result.RoutingChanged = true
	}
	restoredPriority := result.Priority
	ability.Priority = &restoredPriority
	ability.Weight = result.Weight

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
	state.LastScheduleError = reason
	state.LastScheduleScore = nil
	state.LastScheduleScoreDetails = ""
	state.LastSchedulePriority = result.Priority
	state.LastScheduleWeight = result.Weight
	state.LastScheduleTime = now
	state.Revision++
	result.Cleared = true
	return result, saveChannelSmartScheduleRouteStateTx(tx, state)
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
