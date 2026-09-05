package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

type ChannelSmartScheduleRuntimeTemporaryRoute struct {
	ModelName   string
	SampleSince int64
}

type ChannelSmartScheduleRuntimeRoute struct {
	ModelName            string
	SampleSince          int64
	StabilityState       string
	TemporaryTrafficKind string
}

func getChannelSmartScheduleRuntimeAbilityRoutes(channelId int, modelName string) (map[string]string, []string, error) {
	modelNames := channelSmartScheduleRouteModelNames(modelName)
	if channelId <= 0 || len(modelNames) == 0 {
		return map[string]string{}, nil, nil
	}

	var channel Channel
	err := DB.Select("id", "status").Where("id = ?", channelId).First(&channel).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && channel.Status != common.ChannelStatusEnabled) {
		return map[string]string{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	var abilities []Ability
	if err := DB.Select(commonGroupCol, "model").
		Where("channel_id = ? AND model IN ? AND enabled = ?", channelId, modelNames, true).
		Find(&abilities).Error; err != nil {
		return nil, nil, err
	}
	selectedModelByGroup := make(map[string]string)
	abilityModelsByGroup := make(map[string]map[string]struct{})
	for _, ability := range abilities {
		models := abilityModelsByGroup[ability.Group]
		if models == nil {
			models = make(map[string]struct{})
			abilityModelsByGroup[ability.Group] = models
		}
		models[ability.Model] = struct{}{}
	}
	for group, models := range abilityModelsByGroup {
		for _, candidateModel := range modelNames {
			if _, exists := models[candidateModel]; exists {
				selectedModelByGroup[group] = candidateModel
				break
			}
		}
	}
	if len(selectedModelByGroup) == 0 {
		return map[string]string{}, modelNames, nil
	}
	return selectedModelByGroup, modelNames, nil
}

// GetChannelSmartScheduleRuntimeParticipatingRoutes returns the effective
// smart-schedule routes for one channel and requested model. Exact abilities
// take precedence over matching wildcard abilities in the same group.
func GetChannelSmartScheduleRuntimeParticipatingRoutes(channelId int, modelName string) (map[string]string, error) {
	if routes, cacheEnabled := GetCachedChannelSmartScheduleRuntimeParticipatingRoutes(channelId, modelName); cacheEnabled {
		return routes, nil
	}
	selectedModelByGroup, modelNames, err := getChannelSmartScheduleRuntimeAbilityRoutes(channelId, modelName)
	if err != nil || len(selectedModelByGroup) == 0 {
		return selectedModelByGroup, err
	}

	groups := make([]string, 0, len(selectedModelByGroup))
	for group := range selectedModelByGroup {
		groups = append(groups, group)
	}
	var states []ChannelSmartScheduleRouteState
	if err := DB.Select("group_name", "model_name").
		Where("channel_id = ? AND group_name IN ? AND model_name IN ?", channelId, groups, modelNames).
		Where("participation_set = ? AND excluded = ?", true, false).
		Find(&states).Error; err != nil {
		return nil, err
	}
	participating := make(map[string]string, len(states))
	for _, state := range states {
		if selectedModelByGroup[state.GroupName] == state.ModelName {
			participating[state.GroupName] = state.ModelName
		}
	}
	return participating, nil
}

// GetChannelSmartScheduleRuntimeRoutes returns every participating route for
// one channel/model request, including normal routes that may need short-term
// runtime protection before the scheduled stability score catches up.
func GetChannelSmartScheduleRuntimeRoutes(channelId int, modelName string) (map[string]ChannelSmartScheduleRuntimeRoute, error) {
	if routes, cacheEnabled := GetCachedChannelSmartScheduleRuntimeRoutes(channelId, modelName); cacheEnabled {
		return routes, nil
	}
	selectedModelByGroup, modelNames, err := getChannelSmartScheduleRuntimeAbilityRoutes(channelId, modelName)
	if err != nil || len(selectedModelByGroup) == 0 {
		return map[string]ChannelSmartScheduleRuntimeRoute{}, err
	}

	groups := make([]string, 0, len(selectedModelByGroup))
	for group := range selectedModelByGroup {
		groups = append(groups, group)
	}
	var states []ChannelSmartScheduleRouteState
	if err := DB.Select(
		"group_name", "model_name", "temporary_traffic_kind", "temporary_traffic_since", "stability_state", "stability_since",
	).
		Where("channel_id = ? AND group_name IN ? AND model_name IN ?", channelId, groups, modelNames).
		Where("participation_set = ? AND excluded = ?", true, false).
		Find(&states).Error; err != nil {
		return nil, err
	}
	routes := make(map[string]ChannelSmartScheduleRuntimeRoute, len(states))
	for _, state := range states {
		if selectedModelByGroup[state.GroupName] != state.ModelName {
			continue
		}
		sampleSince := state.TemporaryTrafficSince
		if state.StabilityState == ChannelSmartScheduleStabilityProbing && state.StabilitySince > sampleSince {
			sampleSince = state.StabilitySince
		}
		routes[state.GroupName] = ChannelSmartScheduleRuntimeRoute{
			ModelName:            state.ModelName,
			SampleSince:          sampleSince,
			StabilityState:       state.StabilityState,
			TemporaryTrafficKind: state.TemporaryTrafficKind,
		}
	}
	return routes, nil
}

func GetChannelSmartScheduleRuntimeTemporaryRoutes(channelId int, modelName string) (map[string]ChannelSmartScheduleRuntimeTemporaryRoute, error) {
	selectedModelByGroup, modelNames, err := getChannelSmartScheduleRuntimeAbilityRoutes(channelId, modelName)
	if err != nil {
		return nil, err
	}
	if len(selectedModelByGroup) == 0 {
		return map[string]ChannelSmartScheduleRuntimeTemporaryRoute{}, nil
	}

	groups := make([]string, 0, len(selectedModelByGroup))
	for group := range selectedModelByGroup {
		groups = append(groups, group)
	}
	var states []ChannelSmartScheduleRouteState
	if err := DB.Select(
		"group_name", "model_name", "temporary_traffic_kind", "temporary_traffic_since", "stability_state", "stability_since",
		"manual_primary_until", "manual_primary_allow_stability_degrade",
	).
		Where("channel_id = ? AND group_name IN ? AND model_name IN ?", channelId, groups, modelNames).
		Where("participation_set = ? AND excluded = ?", true, false).
		Where(
			"temporary_traffic_kind <> ? OR stability_state = ? OR (manual_primary_until > ? AND manual_primary_allow_stability_degrade = ? AND stability_state = ?)",
			"", ChannelSmartScheduleStabilityProbing, common.GetTimestamp(), true, "",
		).
		Find(&states).Error; err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	routes := make(map[string]ChannelSmartScheduleRuntimeTemporaryRoute, len(states))
	for _, state := range states {
		if selectedModelByGroup[state.GroupName] != state.ModelName {
			continue
		}
		activeFixedPrimary := state.ManualPrimaryUntil > now &&
			state.ManualPrimaryAllowStabilityDegrade && state.StabilityState == ""
		activeTemporaryTraffic := state.TemporaryTrafficKind != "" ||
			state.StabilityState == ChannelSmartScheduleStabilityProbing
		if !activeFixedPrimary && !activeTemporaryTraffic {
			continue
		}
		sampleSince := state.TemporaryTrafficSince
		if state.StabilityState == ChannelSmartScheduleStabilityProbing && state.StabilitySince > sampleSince {
			sampleSince = state.StabilitySince
		}
		if sampleSince <= 0 && !activeFixedPrimary {
			sampleSince = now
		}
		routes[state.GroupName] = ChannelSmartScheduleRuntimeTemporaryRoute{
			ModelName: state.ModelName, SampleSince: sampleSince,
		}
	}
	return routes, nil
}
