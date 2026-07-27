package model

import (
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const ChannelSmartScheduleRouteInitializedOption = "ChannelMonitorSmartScheduleRouteInitialized"

type ChannelSmartScheduleRouteState struct {
	Id               int64  `json:"id"`
	ChannelId        int    `json:"channel_id" gorm:"not null;uniqueIndex:idx_channel_smart_schedule_route"`
	GroupName        string `json:"group" gorm:"type:varchar(64);not null;uniqueIndex:idx_channel_smart_schedule_route"`
	ModelName        string `json:"model" gorm:"type:varchar(255);not null;uniqueIndex:idx_channel_smart_schedule_route"`
	ParticipationSet bool   `json:"participation_set"`
	Excluded         bool   `json:"excluded"`
	Revision         int64  `json:"-" gorm:"bigint"`

	LastScheduleStatus   string   `json:"last_schedule_status" gorm:"type:varchar(16);index"`
	LastScheduleError    string   `json:"last_schedule_error" gorm:"type:varchar(255)"`
	LastScheduleScore    *float64 `json:"last_schedule_score"`
	LastSchedulePriority int64    `json:"last_schedule_priority" gorm:"bigint"`
	LastScheduleWeight   uint     `json:"last_schedule_weight"`
	LastScheduleTime     int64    `json:"last_schedule_time" gorm:"bigint;index"`

	StabilityState         string `json:"stability_state" gorm:"type:varchar(16);index"`
	StabilityUntil         int64  `json:"stability_until" gorm:"bigint;index"`
	StabilitySince         int64  `json:"stability_since" gorm:"bigint"`
	StabilitySavedPriority int64  `json:"stability_saved_priority" gorm:"bigint"`
	StabilitySavedWeight   uint   `json:"stability_saved_weight"`

	ScopeRoutingSaved  bool  `json:"-"`
	ScopeSavedPriority int64 `json:"-" gorm:"bigint"`
	ScopeSavedWeight   uint  `json:"-"`
}

func (state ChannelSmartScheduleRouteState) Participates() bool {
	return state.ParticipationSet && !state.Excluded
}

type ChannelSmartScheduleRouteKey struct {
	ChannelId int
	Group     string
	Model     string
}

type ChannelSmartScheduleRoute struct {
	ChannelId       int                            `json:"channel_id"`
	ChannelName     string                         `json:"channel_name"`
	ChannelStatus   int                            `json:"channel_status"`
	ChannelPriority int64                          `json:"channel_priority"`
	ChannelWeight   uint                           `json:"channel_weight"`
	Group           string                         `json:"group"`
	Model           string                         `json:"model"`
	Enabled         bool                           `json:"enabled"`
	Priority        int64                          `json:"priority"`
	Weight          uint                           `json:"weight"`
	State           ChannelSmartScheduleRouteState `json:"state"`
}

type ChannelSmartScheduleRouteResultUpdate struct {
	ChannelId               int
	Group                   string
	Model                   string
	Status                  string
	Error                   string
	Score                   *float64
	Priority                int64
	Weight                  uint
	Time                    int64
	Stability               *ChannelSmartScheduleStabilityUpdate
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
	err := DB.Transaction(func(tx *gorm.DB) error {
		var option Option
		optionErr := lockForUpdate(tx).
			Where(&Option{Key: ChannelSmartScheduleRouteInitializedOption}).
			First(&option).Error
		initialized := optionErr == nil && option.Value == "true"
		if optionErr != nil && !errors.Is(optionErr, gorm.ErrRecordNotFound) {
			return optionErr
		}

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

		monitorByChannel := make(map[int]ChannelRatioMonitor)
		if !initialized {
			var monitors []ChannelRatioMonitor
			if err := tx.Find(&monitors).Error; err != nil {
				return err
			}
			monitorByChannel = make(map[int]ChannelRatioMonitor, len(monitors))
			for _, monitor := range monitors {
				monitorByChannel[monitor.ChannelId] = monitor
			}
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
			if !initialized {
				if monitor, exists := monitorByChannel[ability.ChannelId]; exists {
					state.Excluded = !monitor.ParticipatesInSmartSchedule()
					state.LastScheduleStatus = monitor.LastScheduleStatus
					state.LastScheduleError = monitor.LastScheduleError
					state.LastScheduleScore = monitor.LastScheduleScore
					state.LastSchedulePriority = monitor.LastSchedulePriority
					state.LastScheduleWeight = monitor.LastScheduleWeight
					state.LastScheduleTime = monitor.LastScheduleTime
					state.StabilityState = monitor.SmartScheduleStabilityState
					state.StabilityUntil = monitor.SmartScheduleStabilityUntil
					state.StabilitySince = monitor.SmartScheduleStabilitySince
					state.StabilitySavedPriority = monitor.SmartScheduleSavedPriority
					state.StabilitySavedWeight = monitor.SmartScheduleSavedWeight
				}
			}
			newStates = append(newStates, state)
		}
		if len(newStates) > 0 {
			if err := tx.CreateInBatches(newStates, 500).Error; err != nil {
				return err
			}
		}
		if initialized {
			return nil
		}
		option.Key = ChannelSmartScheduleRouteInitializedOption
		option.Value = "true"
		if errors.Is(optionErr, gorm.ErrRecordNotFound) {
			return tx.Create(&option).Error
		}
		return tx.Save(&option).Error
	})
	if err != nil {
		return err
	}
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMap[ChannelSmartScheduleRouteInitializedOption] = "true"
	common.OptionMapRWMutex.Unlock()
	return nil
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
	channelById := make(map[int]Channel, len(channels))
	for _, channel := range channels {
		channelById[channel.Id] = channel
	}
	stateByKey := make(map[ChannelSmartScheduleRouteKey]ChannelSmartScheduleRouteState, len(states))
	for _, state := range states {
		stateByKey[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)] = state
	}
	routes := make([]ChannelSmartScheduleRoute, 0, len(abilities))
	for _, ability := range abilities {
		channel, exists := channelById[ability.ChannelId]
		if !exists {
			continue
		}
		key := channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)
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

func SaveChannelSmartScheduleRouteConfig(channelId int, group string, modelName string, excluded bool) (state ChannelSmartScheduleRouteState, err error) {
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
		if !wasParticipating && !excluded && state.StabilityState == ChannelSmartScheduleStabilityProbing {
			state.StabilitySince = common.GetTimestamp()
		}
		state.Revision++
		if created {
			return tx.Create(&state).Error
		}
		return tx.Save(&state).Error
	})
	return state, err
}

// SuspendChannelSmartScheduleRouteRouting restores Ability routing to the
// channel defaults while preserving each isolated route for a later resume.
func SuspendChannelSmartScheduleRouteRouting() (routingChanged bool, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		var controlOption Option
		controlErr := lockForUpdate(tx).
			Where(&Option{Key: ChannelSmartScheduleControlRevisionOption}).
			First(&controlOption).Error
		if controlErr != nil && !errors.Is(controlErr, gorm.ErrRecordNotFound) {
			return controlErr
		}
		var switchErr error
		routingChanged, switchErr = suspendChannelSmartScheduleRouteRoutingTx(tx)
		return switchErr
	})
	return routingChanged, err
}

func suspendChannelSmartScheduleRouteRoutingTx(tx *gorm.DB) (routingChanged bool, err error) {
	var channels []Channel
	if err := lockForUpdate(tx).Select("id", "priority", "weight").Find(&channels).Error; err != nil {
		return false, err
	}
	channelById := make(map[int]Channel, len(channels))
	for _, channel := range channels {
		channelById[channel.Id] = channel
	}

	var states []ChannelSmartScheduleRouteState
	if err := lockForUpdate(tx).Find(&states).Error; err != nil {
		return false, err
	}
	stateByKey := make(map[ChannelSmartScheduleRouteKey]*ChannelSmartScheduleRouteState, len(states))
	for index := range states {
		state := &states[index]
		stateByKey[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)] = state
	}
	var abilities []Ability
	if err := lockForUpdate(tx).Find(&abilities).Error; err != nil {
		return false, err
	}

	for _, ability := range abilities {
		channel, exists := channelById[ability.ChannelId]
		if !exists {
			continue
		}
		key := channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)
		state := stateByKey[key]
		created := state == nil
		if created {
			state = &ChannelSmartScheduleRouteState{
				ChannelId: ability.ChannelId, GroupName: ability.Group, ModelName: ability.Model,
				ParticipationSet: true, Excluded: true,
			}
			stateByKey[key] = state
		}
		if !state.ScopeRoutingSaved {
			if state.Revision == math.MaxInt64 {
				return false, errors.New("智能调度路由修订号已达上限")
			}
			state.ScopeRoutingSaved = true
			state.ScopeSavedPriority = abilityPriority(ability)
			state.ScopeSavedWeight = ability.Weight
			state.Revision++
			if created {
				if err := tx.Create(state).Error; err != nil {
					return false, err
				}
			} else if err := tx.Save(state).Error; err != nil {
				return false, err
			}
		}

		priority := channel.GetPriority()
		weight := uint(channel.GetWeight())
		if abilityPriority(ability) == priority && ability.Weight == weight {
			continue
		}
		if err := updateAbilitySmartSchedulePriorityWeightTx(tx, key, &priority, &weight); err != nil {
			return false, err
		}
		routingChanged = true
	}
	return routingChanged, nil
}

// ResumeChannelSmartScheduleRouteRouting restores the isolated Ability values
// that were suspended when channel-compatible routing became active.
func ResumeChannelSmartScheduleRouteRouting() (routingChanged bool, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		var controlOption Option
		controlErr := lockForUpdate(tx).
			Where(&Option{Key: ChannelSmartScheduleControlRevisionOption}).
			First(&controlOption).Error
		if controlErr != nil && !errors.Is(controlErr, gorm.ErrRecordNotFound) {
			return controlErr
		}
		var switchErr error
		routingChanged, switchErr = resumeChannelSmartScheduleRouteRoutingTx(tx)
		return switchErr
	})
	return routingChanged, err
}

func resumeChannelSmartScheduleRouteRoutingTx(tx *gorm.DB) (routingChanged bool, err error) {
	var states []ChannelSmartScheduleRouteState
	if err := lockForUpdate(tx).Where("scope_routing_saved = ?", true).Find(&states).Error; err != nil {
		return false, err
	}
	for index := range states {
		state := &states[index]
		if state.Revision == math.MaxInt64 {
			return false, errors.New("智能调度路由修订号已达上限")
		}
		key := channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)
		var ability Ability
		findErr := lockForUpdate(tx).
			Where(&Ability{ChannelId: state.ChannelId, Group: state.GroupName, Model: state.ModelName}).
			First(&ability).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return false, findErr
		}
		if findErr == nil && (abilityPriority(ability) != state.ScopeSavedPriority || ability.Weight != state.ScopeSavedWeight) {
			if err := updateAbilitySmartSchedulePriorityWeightTx(
				tx, key, &state.ScopeSavedPriority, &state.ScopeSavedWeight,
			); err != nil {
				return false, err
			}
			routingChanged = true
		}
		state.ScopeRoutingSaved = false
		state.ScopeSavedPriority = 0
		state.ScopeSavedWeight = 0
		state.Revision++
		if err := tx.Save(state).Error; err != nil {
			return false, err
		}
	}
	return routingChanged, nil
}

// UpdateOptionsBulkWithChannelSmartScheduleScope commits the settings and
// routing-layer switch together so readers never observe a half-switched scope.
func UpdateOptionsBulkWithChannelSmartScheduleScope(values map[string]string, targetScope string) (scopeChanged bool, err error) {
	if targetScope != channelSmartScheduleScopeChannel && targetScope != channelSmartScheduleScopeGroupModel {
		return false, errors.New("智能调度范围无效")
	}
	if values[channelSmartScheduleScopeOption] != targetScope {
		return false, errors.New("智能调度范围设置不一致")
	}
	if _, exists := values[ChannelSmartScheduleControlRevisionOption]; !exists {
		return false, errors.New("智能调度控制修订号不能为空")
	}
	for key, value := range values {
		if err := validateOptionValue(key, value); err != nil {
			return false, err
		}
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		currentScope := channelSmartScheduleScopeChannel
		var scopeOption Option
		scopeErr := lockForUpdate(tx).
			Where(&Option{Key: channelSmartScheduleScopeOption}).
			First(&scopeOption).Error
		if scopeErr == nil && scopeOption.Value == channelSmartScheduleScopeGroupModel {
			currentScope = channelSmartScheduleScopeGroupModel
		} else if scopeErr != nil && !errors.Is(scopeErr, gorm.ErrRecordNotFound) {
			return scopeErr
		}

		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			option := Option{Key: key}
			if err := tx.FirstOrCreate(&option, Option{Key: key}).Error; err != nil {
				return err
			}
			option.Value = values[key]
			if err := tx.Save(&option).Error; err != nil {
				return err
			}
		}

		if currentScope == targetScope {
			return nil
		}
		scopeChanged = true
		if targetScope == channelSmartScheduleScopeGroupModel {
			_, err = resumeChannelSmartScheduleRouteRoutingTx(tx)
		} else {
			_, err = suspendChannelSmartScheduleRouteRoutingTx(tx)
		}
		return err
	})
	if err != nil {
		return false, err
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		if key != channelSmartScheduleScopeOption {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	keys = append(keys, channelSmartScheduleScopeOption)
	for _, key := range keys {
		if err := updateOptionMap(key, values[key]); err != nil {
			return scopeChanged, err
		}
	}
	return scopeChanged, nil
}

func ApplyChannelSmartScheduleRouteResults(results []ChannelSmartScheduleRouteResultUpdate) ([]ChannelSmartScheduleRouteApplyOutcome, error) {
	if len(results) == 0 {
		return nil, nil
	}
	outcomes := make([]ChannelSmartScheduleRouteApplyOutcome, 0, len(results))
	err := DB.Transaction(func(tx *gorm.DB) error {
		for _, result := range results {
			key := channelSmartScheduleRouteKey(result.ChannelId, result.Group, result.Model)
			outcome := ChannelSmartScheduleRouteApplyOutcome{Key: key}
			var controlOption Option
			if result.GuardCurrent {
				optionErr := lockForUpdate(tx).Where(&Option{Key: ChannelSmartScheduleControlRevisionOption}).First(&controlOption).Error
				if errors.Is(optionErr, gorm.ErrRecordNotFound) {
					controlOption.Value = ""
				} else if optionErr != nil {
					return optionErr
				}
			}
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
				if controlOption.Value != result.ExpectedControlRevision || state.Revision != result.ExpectedRevision ||
					!state.Participates() || !ability.Enabled || abilityPriority(ability) != result.ExpectedPriority ||
					ability.Weight != result.ExpectedWeight {
					outcomes = append(outcomes, outcome)
					continue
				}
				var channel Channel
				if err := lockForUpdate(tx).Select("id", "status").Where("id = ?", result.ChannelId).First(&channel).Error; err != nil {
					return err
				}
				if channel.Status != common.ChannelStatusEnabled {
					outcomes = append(outcomes, outcome)
					continue
				}
			}

			message := strings.TrimSpace(result.Error)
			messageRunes := []rune(message)
			if len(messageRunes) > 255 {
				message = string(messageRunes[:255])
			}
			updatedTime := result.Time
			if updatedTime <= 0 {
				updatedTime = common.GetTimestamp()
			}
			state.LastScheduleStatus = result.Status
			state.LastScheduleError = message
			state.LastScheduleScore = result.Score
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
				outcome.RoutingChanged = priority != result.ExpectedPriority || weight != result.ExpectedWeight
			}
			if state.Id == 0 {
				if err := tx.Create(&state).Error; err != nil {
					return err
				}
			} else if err := tx.Save(&state).Error; err != nil {
				return err
			}
			outcome.Applied = true
			outcomes = append(outcomes, outcome)
		}
		return nil
	})
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
		state.LastScheduleStatus = ChannelSmartScheduleStatusSucceeded
		state.LastScheduleError = "管理员已手动解除稳定性保护"
		state.LastScheduleScore = nil
		state.LastSchedulePriority = result.Priority
		state.LastScheduleWeight = result.Weight
		state.LastScheduleTime = now
		state.Revision++
		result.Cleared = true
		return tx.Save(&state).Error
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
