package model

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type ChannelSmartScheduleChannelPrimaryRule struct {
	Id                                 int64 `json:"id" gorm:"primaryKey"`
	ChannelId                          int   `json:"channel_id" gorm:"not null;uniqueIndex:uk_channel_smart_schedule_primary_rule"`
	ManualPrimaryUntil                 int64 `json:"manual_primary_until" gorm:"bigint;not null;index"`
	ManualPrimaryAllowStabilityDegrade bool  `json:"manual_primary_allow_stability_degrade"`
	UpdatedAt                          int64 `json:"updated_at" gorm:"bigint;not null;index"`
	UpdatedOrder                       int64 `json:"-" gorm:"bigint;not null;index"`
	Revision                           int64 `json:"revision" gorm:"bigint;not null"`
}

type ChannelSmartScheduleChannelPrimaryRuleOptions struct {
	DurationMinutes           int
	AllowStabilityDegrade     bool
	ConfirmStabilityOverride  bool
	StabilityFallbackPriority int64
	StabilityFallbackWeight   uint
}

type ChannelSmartScheduleChannelPrimaryRuleResult struct {
	ChannelId             int   `json:"channel_id"`
	DurationMinutes       *int  `json:"duration_minutes"`
	ManualPrimaryUntil    int64 `json:"manual_primary_until"`
	AllowStabilityDegrade bool  `json:"allow_stability_degrade"`
	Total                 int   `json:"total"`
	FixedRoutes           int   `json:"fixed_routes"`
	SkippedRoutes         int   `json:"skipped_routes"`
	RoutingChanged        bool  `json:"routing_changed"`
}

type ChannelSmartSchedulePrimaryRuleRoute struct {
	Group                     string `json:"group"`
	Model                     string `json:"model"`
	Status                    string `json:"status"`
	CurrentPrimaryChannelId   int    `json:"current_primary_channel_id"`
	CurrentPrimaryChannelName string `json:"current_primary_channel_name"`
}

type ChannelSmartScheduleChannelPrimaryRuleInfo struct {
	ChannelId             int                                    `json:"channel_id"`
	ChannelName           string                                 `json:"channel_name"`
	ManualPrimaryUntil    int64                                  `json:"manual_primary_until"`
	AllowStabilityDegrade bool                                   `json:"allow_stability_degrade"`
	UpdatedAt             int64                                  `json:"updated_at"`
	Routes                []ChannelSmartSchedulePrimaryRuleRoute `json:"routes"`
}

type ChannelSmartScheduleManualPrimaryInfo struct {
	ChannelId             int    `json:"channel_id"`
	ChannelName           string `json:"channel_name"`
	Group                 string `json:"group"`
	Model                 string `json:"model"`
	ManualPrimaryUntil    int64  `json:"manual_primary_until"`
	AllowStabilityDegrade bool   `json:"allow_stability_degrade"`
}

type ChannelSmartSchedulePrimaryManagement struct {
	ChannelRules   []ChannelSmartScheduleChannelPrimaryRuleInfo `json:"channel_rules"`
	RoutePrimaries []ChannelSmartScheduleManualPrimaryInfo      `json:"route_primaries"`
}

var ErrChannelSmartScheduleChannelPrimaryRuleStabilityProtected = errors.New("渠道固定规则涉及处于稳定性保护中的模型路由，需要管理员确认")

type channelSmartSchedulePrimaryRuleApplyOptions struct {
	confirmStabilityOverride     bool
	requireStabilityConfirmation bool
	fallbackPriority             int64
	fallbackWeight               uint
}

func normalizeChannelSmartSchedulePrimaryRuleApplyOptions(
	options channelSmartSchedulePrimaryRuleApplyOptions,
) channelSmartSchedulePrimaryRuleApplyOptions {
	if options.fallbackPriority <= 0 {
		options.fallbackPriority = channelSmartScheduleRuntimeFallbackPriority
	}
	if options.fallbackWeight == 0 {
		options.fallbackWeight = channelSmartScheduleRuntimeFallbackWeight
	}
	return options
}

func channelSmartSchedulePrimaryRuleUntil(now int64, durationMinutes int) int64 {
	if durationMinutes == ChannelSmartSchedulePermanentDurationMinutes {
		return common.ChannelMonitorSmartSchedulePermanentUntil
	}
	if durationMinutes <= 0 {
		return 0
	}
	return now + int64(durationMinutes)*60
}

func channelSmartSchedulePrimaryRuleIsNewer(
	first ChannelSmartScheduleChannelPrimaryRule,
	second ChannelSmartScheduleChannelPrimaryRule,
) bool {
	if first.UpdatedOrder != second.UpdatedOrder {
		return first.UpdatedOrder > second.UpdatedOrder
	}
	if first.UpdatedAt != second.UpdatedAt {
		return first.UpdatedAt > second.UpdatedAt
	}
	return first.ChannelId > second.ChannelId
}

func SaveChannelSmartScheduleChannelPrimaryRule(
	channelId int,
	options ChannelSmartScheduleChannelPrimaryRuleOptions,
) (result ChannelSmartScheduleChannelPrimaryRuleResult, err error) {
	if channelId <= 0 {
		return result, gorm.ErrRecordNotFound
	}
	durationMinutes := options.DurationMinutes
	if durationMinutes != ChannelSmartSchedulePermanentDurationMinutes &&
		(durationMinutes < 0 || durationMinutes > ChannelSmartScheduleManualPrimaryMaxMinutes) {
		return result, fmt.Errorf("主渠道固定时间必须在 0 到 %d 分钟之间", ChannelSmartScheduleManualPrimaryMaxMinutes)
	}

	result.ChannelId = channelId
	if durationMinutes > 0 {
		result.DurationMinutes = &durationMinutes
	}
	result.ManualPrimaryUntil = channelSmartSchedulePrimaryRuleUntil(common.GetTimestamp(), durationMinutes)
	result.AllowStabilityDegrade = options.AllowStabilityDegrade

	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	err = DB.Transaction(func(tx *gorm.DB) error {
		var channel Channel
		if err := lockForUpdate(tx).Select("id").Where("id = ?", channelId).First(&channel).Error; err != nil {
			return err
		}
		var currentAbilities []Ability
		if err := tx.Select(commonGroupCol, "model").
			Where("channel_id = ?", channelId).
			Order(commonGroupCol).
			Order("model ASC").
			Find(&currentAbilities).Error; err != nil {
			return err
		}
		pools := channelSmartScheduleRoutePoolsFromAbilities(currentAbilities)
		for _, pool := range pools {
			if _, err := lockChannelSmartScheduleRoutePoolChannelsTx(tx, pool.group, pool.model, channelId); err != nil {
				return err
			}
		}
		if _, err := lockChannelSmartScheduleRoutePoolStatesTx(tx, pools); err != nil {
			return err
		}
		abilities, err := lockChannelSmartScheduleRoutePoolAbilitiesTx(tx, pools)
		if err != nil {
			return err
		}
		for _, ability := range abilities {
			if ability.ChannelId == channelId {
				result.Total++
			}
		}

		var rule ChannelSmartScheduleChannelPrimaryRule
		findErr := lockForUpdate(tx).Where("channel_id = ?", channelId).First(&rule).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		if durationMinutes == 0 {
			if !errors.Is(findErr, gorm.ErrRecordNotFound) {
				if err := tx.Delete(&rule).Error; err != nil {
					return err
				}
			}
		} else {
			if !errors.Is(findErr, gorm.ErrRecordNotFound) && rule.Revision == math.MaxInt64 {
				return errors.New("渠道固定规则修订号已达上限")
			}
			now := common.GetTimestamp()
			rule.ChannelId = channelId
			rule.ManualPrimaryUntil = channelSmartSchedulePrimaryRuleUntil(now, durationMinutes)
			rule.ManualPrimaryAllowStabilityDegrade = options.AllowStabilityDegrade
			rule.UpdatedAt = now
			rule.UpdatedOrder = time.Now().UnixNano()
			rule.Revision++
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				rule.Revision = 1
				if err := tx.Create(&rule).Error; err != nil {
					return err
				}
			} else if err := tx.Save(&rule).Error; err != nil {
				return err
			}
		}

		applyOptions := normalizeChannelSmartSchedulePrimaryRuleApplyOptions(channelSmartSchedulePrimaryRuleApplyOptions{
			confirmStabilityOverride:     options.ConfirmStabilityOverride,
			requireStabilityConfirmation: durationMinutes != 0,
			fallbackPriority:             options.StabilityFallbackPriority,
			fallbackWeight:               options.StabilityFallbackWeight,
		})
		changedKeys, err := reapplyChannelSmartScheduleRoutePrimariesTxWithOptions(tx, pools, applyOptions)
		if err != nil {
			return err
		}
		result.RoutingChanged = len(changedKeys) > 0
		var updatedStates []ChannelSmartScheduleRouteState
		if err := tx.Where("channel_id = ?", channelId).Find(&updatedStates).Error; err != nil {
			return err
		}
		for _, state := range updatedStates {
			if state.ChannelId == channelId && state.ManualPrimaryChannelRule && state.ManualPrimaryUntil > common.GetTimestamp() {
				result.FixedRoutes++
			}
		}
		result.SkippedRoutes = max(0, result.Total-result.FixedRoutes)
		return nil
	})
	if err != nil {
		result.RoutingChanged = false
	}
	return result, err
}

func reconcileChannelSmartSchedulePrimaryRulePoolTx(
	tx *gorm.DB,
	pool channelSmartScheduleRoutePool,
	states []ChannelSmartScheduleRouteState,
	abilities []Ability,
	channelStatusById map[int]int,
	now int64,
	options channelSmartSchedulePrimaryRuleApplyOptions,
) error {
	if !tx.Migrator().HasTable(&ChannelSmartScheduleChannelPrimaryRule{}) {
		return nil
	}
	stateByChannel := make(map[int]*ChannelSmartScheduleRouteState, len(states))
	for index := range states {
		if states[index].GroupName == pool.group && states[index].ModelName == pool.model {
			stateByChannel[states[index].ChannelId] = &states[index]
		}
	}
	abilityByChannel := make(map[int]*Ability, len(abilities))
	channelIds := make([]int, 0, len(abilities))
	for index := range abilities {
		abilityByChannel[abilities[index].ChannelId] = &abilities[index]
		channelIds = append(channelIds, abilities[index].ChannelId)
	}

	var rules []ChannelSmartScheduleChannelPrimaryRule
	if len(channelIds) > 0 {
		if err := tx.Where("channel_id IN ? AND manual_primary_until > ?", channelIds, now).
			Order("updated_order DESC, id DESC").Find(&rules).Error; err != nil {
			return err
		}
	}
	var explicitPrimary *ChannelSmartScheduleRouteState
	for index := range states {
		state := &states[index]
		if state.GroupName != pool.group || state.ModelName != pool.model ||
			state.ManualPrimaryUntil <= now || state.ManualPrimaryChannelRule {
			continue
		}
		if explicitPrimary != nil {
			return errors.New("同一分组和模型存在多个有效的固定主渠道")
		}
		explicitPrimary = state
	}

	var winningRule *ChannelSmartScheduleChannelPrimaryRule
	if explicitPrimary == nil {
		for index := range rules {
			rule := &rules[index]
			state := stateByChannel[rule.ChannelId]
			ability := abilityByChannel[rule.ChannelId]
			if state == nil || ability == nil || !state.Participates() || !ability.Enabled ||
				channelStatusById[rule.ChannelId] != common.ChannelStatusEnabled {
				continue
			}
			winningRule = rule
			break
		}
	}

	for index := range states {
		state := &states[index]
		if state.GroupName != pool.group || state.ModelName != pool.model || !state.ManualPrimaryChannelRule {
			continue
		}
		if winningRule != nil && state.ChannelId == winningRule.ChannelId && state.ManualPrimaryUntil > now {
			continue
		}
		if explicitPrimary != nil && state.ChannelId == explicitPrimary.ChannelId {
			continue
		}
		if _, err := restoreChannelSmartScheduleRoutePrimaryTx(tx, state, abilityByChannel[state.ChannelId]); err != nil {
			return err
		}
	}
	if winningRule == nil || explicitPrimary != nil {
		return nil
	}

	targetState := stateByChannel[winningRule.ChannelId]
	targetAbility := abilityByChannel[winningRule.ChannelId]
	if targetState == nil || targetAbility == nil {
		return nil
	}
	if !targetState.ManualPrimaryChannelRule && targetState.ManualPrimarySaved {
		if _, err := restoreChannelSmartScheduleRoutePrimaryTx(tx, targetState, targetAbility); err != nil {
			return err
		}
	}
	if targetState.StabilityState != "" && !targetState.ManualPrimaryChannelRule {
		if options.requireStabilityConfirmation && !options.confirmStabilityOverride {
			return ErrChannelSmartScheduleChannelPrimaryRuleStabilityProtected
		}
		if !options.confirmStabilityOverride {
			return nil
		}
		if _, err := clearChannelSmartScheduleRouteStabilityTx(
			tx, targetState, targetAbility, options.fallbackPriority, options.fallbackWeight,
			"管理员确认渠道固定规则，已解除稳定性保护",
		); err != nil {
			return err
		}
	}
	if targetState.ManualPrimaryChannelRule && targetState.ManualPrimaryUntil > now {
		if targetState.ManualPrimaryUntil == winningRule.ManualPrimaryUntil &&
			targetState.ManualPrimaryAllowStabilityDegrade == winningRule.ManualPrimaryAllowStabilityDegrade {
			return nil
		}
		if targetState.Revision == math.MaxInt64 {
			return errors.New("智能调度路由修订号已达上限")
		}
		targetState.ManualPrimaryUntil = winningRule.ManualPrimaryUntil
		targetState.ManualPrimaryAllowStabilityDegrade = winningRule.ManualPrimaryAllowStabilityDegrade
		targetState.Revision++
		return saveChannelSmartScheduleRouteStateTx(tx, targetState)
	}
	if targetState.TemporaryTrafficKind != "" || targetState.StabilityReleaseMaxPromptTokens != 0 {
		if _, err := clearChannelSmartScheduleRoutePoolTemporaryTrafficTx(
			tx, states, abilities, pool.group, pool.model, now,
		); err != nil {
			return err
		}
	}
	if !targetState.ManualPrimarySaved {
		targetState.ManualPrimarySaved = true
		targetState.ManualPrimarySavedPriority = abilityPriority(*targetAbility)
		targetState.ManualPrimarySavedWeight = targetAbility.Weight
	}
	minimumPriority := abilityPriority(*targetAbility)
	priority, err := channelSmartScheduleManualPrimaryPriority(
		abilities, states, channelStatusById, winningRule.ChannelId, minimumPriority,
	)
	if err != nil {
		return err
	}
	weight := uint(1000)
	if targetAbility.Priority == nil || abilityPriority(*targetAbility) != priority || targetAbility.Weight != weight {
		if err := updateAbilitySmartSchedulePriorityWeightTx(
			tx, channelSmartScheduleRouteKey(winningRule.ChannelId, pool.group, pool.model), &priority, &weight,
		); err != nil {
			return err
		}
		targetAbility.Priority = &priority
		targetAbility.Weight = weight
	}
	if targetState.Revision == math.MaxInt64 {
		return errors.New("智能调度路由修订号已达上限")
	}
	targetState.ManualPrimaryUntil = winningRule.ManualPrimaryUntil
	targetState.ManualPrimaryAllowStabilityDegrade = winningRule.ManualPrimaryAllowStabilityDegrade
	targetState.ManualPrimaryChannelRule = true
	targetState.LastScheduleStatus = ChannelSmartScheduleStatusSucceeded
	targetState.LastScheduleError = "管理员通过渠道固定规则设为主渠道"
	targetState.LastScheduleScore = nil
	targetState.LastScheduleScoreAt = 0
	targetState.LastScheduleScoreDetails = ""
	targetState.LastSchedulePriority = priority
	targetState.LastScheduleWeight = weight
	targetState.LastScheduleTime = now
	targetState.Revision++
	return saveChannelSmartScheduleRouteStateTx(tx, targetState)
}

func GetChannelSmartSchedulePrimaryManagement() (ChannelSmartSchedulePrimaryManagement, error) {
	result := ChannelSmartSchedulePrimaryManagement{
		ChannelRules:   []ChannelSmartScheduleChannelPrimaryRuleInfo{},
		RoutePrimaries: []ChannelSmartScheduleManualPrimaryInfo{},
	}
	if DB == nil || !DB.Migrator().HasTable(&ChannelSmartScheduleChannelPrimaryRule{}) {
		return result, nil
	}
	now := common.GetTimestamp()
	var channels []Channel
	if err := DB.Select("id", "name", "status").Find(&channels).Error; err != nil {
		return result, err
	}
	var abilities []Ability
	if err := DB.Find(&abilities).Error; err != nil {
		return result, err
	}
	var states []ChannelSmartScheduleRouteState
	if err := DB.Find(&states).Error; err != nil {
		return result, err
	}
	var rules []ChannelSmartScheduleChannelPrimaryRule
	if err := DB.Where("manual_primary_until > ?", now).
		Order("updated_order DESC, id DESC").Find(&rules).Error; err != nil {
		return result, err
	}
	channelById := make(map[int]Channel, len(channels))
	for _, channel := range channels {
		channelById[channel.Id] = channel
	}
	stateByKey := make(map[ChannelSmartScheduleRouteKey]ChannelSmartScheduleRouteState, len(states))
	for _, state := range states {
		stateByKey[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)] = state
		if state.ManualPrimaryUntil > now && !state.ManualPrimaryChannelRule {
			channel := channelById[state.ChannelId]
			result.RoutePrimaries = append(result.RoutePrimaries, ChannelSmartScheduleManualPrimaryInfo{
				ChannelId: state.ChannelId, ChannelName: channel.Name,
				Group: state.GroupName, Model: state.ModelName,
				ManualPrimaryUntil:    state.ManualPrimaryUntil,
				AllowStabilityDegrade: state.ManualPrimaryAllowStabilityDegrade,
			})
		}
	}
	ruleByChannel := make(map[int]ChannelSmartScheduleChannelPrimaryRule, len(rules))
	for _, rule := range rules {
		ruleByChannel[rule.ChannelId] = rule
	}
	abilityByPool := make(map[channelSmartScheduleRoutePool][]Ability)
	explicitByPool := make(map[channelSmartScheduleRoutePool]ChannelSmartScheduleRouteState)
	winnerByPool := make(map[channelSmartScheduleRoutePool]ChannelSmartScheduleChannelPrimaryRule)
	for _, ability := range abilities {
		pool := channelSmartScheduleRoutePool{group: ability.Group, model: ability.Model}
		abilityByPool[pool] = append(abilityByPool[pool], ability)
		state := stateByKey[channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)]
		if state.ManualPrimaryUntil > now && !state.ManualPrimaryChannelRule {
			explicitByPool[pool] = state
		}
	}
	for pool, poolAbilities := range abilityByPool {
		if _, hasExplicit := explicitByPool[pool]; hasExplicit {
			continue
		}
		for _, ability := range poolAbilities {
			state := stateByKey[channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)]
			rule, hasRule := ruleByChannel[ability.ChannelId]
			channel := channelById[ability.ChannelId]
			if !hasRule || !state.Participates() || !ability.Enabled ||
				channel.Status != common.ChannelStatusEnabled {
				continue
			}
			winner, exists := winnerByPool[pool]
			if !exists || channelSmartSchedulePrimaryRuleIsNewer(rule, winner) {
				winnerByPool[pool] = rule
			}
		}
	}

	for _, rule := range rules {
		channel := channelById[rule.ChannelId]
		info := ChannelSmartScheduleChannelPrimaryRuleInfo{
			ChannelId: rule.ChannelId, ChannelName: channel.Name,
			ManualPrimaryUntil:    rule.ManualPrimaryUntil,
			AllowStabilityDegrade: rule.ManualPrimaryAllowStabilityDegrade,
			UpdatedAt:             rule.UpdatedAt,
			Routes:                []ChannelSmartSchedulePrimaryRuleRoute{},
		}
		for _, ability := range abilities {
			if ability.ChannelId != rule.ChannelId {
				continue
			}
			state := stateByKey[channelSmartScheduleRouteKey(ability.ChannelId, ability.Group, ability.Model)]
			routeInfo := ChannelSmartSchedulePrimaryRuleRoute{
				Group: ability.Group, Model: ability.Model, Status: "pending",
			}
			if !state.Participates() || !ability.Enabled || channel.Status != common.ChannelStatusEnabled {
				routeInfo.Status = "not_eligible"
				info.Routes = append(info.Routes, routeInfo)
				continue
			}
			pool := channelSmartScheduleRoutePool{group: ability.Group, model: ability.Model}
			if explicitPrimary, exists := explicitByPool[pool]; exists {
				routeInfo.Status = "single_model_override"
				routeInfo.CurrentPrimaryChannelId = explicitPrimary.ChannelId
				routeInfo.CurrentPrimaryChannelName = channelById[explicitPrimary.ChannelId].Name
				info.Routes = append(info.Routes, routeInfo)
				continue
			}
			if winner, exists := winnerByPool[pool]; exists {
				routeInfo.CurrentPrimaryChannelId = winner.ChannelId
				routeInfo.CurrentPrimaryChannelName = channelById[winner.ChannelId].Name
				switch {
				case winner.ChannelId != rule.ChannelId:
					routeInfo.Status = "channel_rule_conflict"
				case state.ManualPrimaryChannelRule && state.ManualPrimaryUntil > now:
					routeInfo.Status = "active"
				case state.StabilityState != "":
					routeInfo.Status = "stability_protected"
				default:
					routeInfo.Status = "pending"
				}
			}
			info.Routes = append(info.Routes, routeInfo)
		}
		sort.Slice(info.Routes, func(i, j int) bool {
			if info.Routes[i].Group != info.Routes[j].Group {
				return info.Routes[i].Group < info.Routes[j].Group
			}
			return info.Routes[i].Model < info.Routes[j].Model
		})
		result.ChannelRules = append(result.ChannelRules, info)
	}
	sort.Slice(result.ChannelRules, func(i, j int) bool {
		return result.ChannelRules[i].ChannelId < result.ChannelRules[j].ChannelId
	})
	sort.Slice(result.RoutePrimaries, func(i, j int) bool {
		first, second := result.RoutePrimaries[i], result.RoutePrimaries[j]
		if first.ChannelName != second.ChannelName {
			return first.ChannelName < second.ChannelName
		}
		if first.Group != second.Group {
			return first.Group < second.Group
		}
		return first.Model < second.Model
	})
	return result, nil
}
