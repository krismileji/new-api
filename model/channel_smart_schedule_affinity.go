package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

type ChannelSmartScheduleAffinityStatus int

const (
	ChannelSmartScheduleAffinityInvalid ChannelSmartScheduleAffinityStatus = iota
	ChannelSmartScheduleAffinityEligible
	ChannelSmartScheduleAffinityTemporarilyUnavailable
)

// ChannelSmartScheduleAffinityEligibility keeps an affinity hit only when it
// belongs to the highest usable priority for the current request. Pools
// without an active participating route retain the official affinity behavior.
func ChannelSmartScheduleAffinityEligibility(
	group string,
	modelName string,
	channelId int,
	requestPath string,
	options ...ChannelSelectionOptions,
) ChannelSmartScheduleAffinityStatus {
	group = strings.TrimSpace(group)
	requestModelName := strings.TrimSpace(modelName)
	selectionOptions := channelSelectionOptions(options)
	modelNames := channelSmartScheduleRouteModelNames(requestModelName)
	if group == "" || len(modelNames) == 0 || channelId <= 0 {
		return ChannelSmartScheduleAffinityInvalid
	}

	if !common.MemoryCacheEnabled {
		selectedModel := ""
		var abilities []Ability
		var usableChannels map[int]*Channel
		for _, candidateModel := range modelNames {
			var candidateAbilities []Ability
			if err := DB.Select("channel_id", "priority").
				Where(&Ability{Group: group, Model: candidateModel, Enabled: true}).
				Find(&candidateAbilities).Error; err != nil {
				return ChannelSmartScheduleAffinityTemporarilyUnavailable
			}
			if len(candidateAbilities) == 0 {
				continue
			}
			channelIds := make([]int, 0, len(candidateAbilities))
			for _, ability := range candidateAbilities {
				channelIds = append(channelIds, ability.ChannelId)
			}
			var channels []Channel
			if err := DB.Where("id IN ? AND status = ?", channelIds, common.ChannelStatusEnabled).
				Find(&channels).Error; err != nil {
				return ChannelSmartScheduleAffinityTemporarilyUnavailable
			}
			candidateChannels := make(map[int]*Channel, len(channels))
			for index := range channels {
				channel := &channels[index]
				if channel.Type == constant.ChannelTypeAdvancedCustom {
					config := channel.GetOtherSettings().AdvancedCustom
					if config == nil || !config.SupportsPathForModel(requestPath, requestModelName) {
						continue
					}
				}
				candidateChannels[channel.Id] = channel
			}
			for _, ability := range candidateAbilities {
				if _, usable := candidateChannels[ability.ChannelId]; usable {
					abilities = append(abilities, ability)
				}
			}
			if len(abilities) > 0 {
				selectedModel = candidateModel
				usableChannels = candidateChannels
				break
			}
		}
		if selectedModel == "" {
			return ChannelSmartScheduleAffinityInvalid
		}
		originalAbilities := append([]Ability(nil), abilities...)
		preferredInOriginal := false
		for _, ability := range originalAbilities {
			if ability.ChannelId == channelId {
				preferredInOriginal = true
				break
			}
		}
		if selectionOptions.HasRequestSize() {
			channelIDs := make([]int, 0, len(originalAbilities))
			for _, ability := range originalAbilities {
				channelIDs = append(channelIDs, ability.ChannelId)
			}
			explorationStates, err := loadChannelSmartScheduleExplorationStates(
				group, selectedModel, channelIDs,
			)
			if err != nil {
				return ChannelSmartScheduleAffinityTemporarilyUnavailable
			}
			abilities = filterAbilitiesByExplorationRequest(abilities, explorationStates, selectionOptions)
			if preferredInOriginal && !containsAbilityChannel(abilities, channelId) {
				return ChannelSmartScheduleAffinityTemporarilyUnavailable
			}
		}

		var activeStateCount int64
		if DB.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
			if err := DB.Model(&ChannelSmartScheduleRouteState{}).
				Where(
					"group_name = ? AND model_name = ? AND participation_set = ? AND excluded = ?",
					group, selectedModel, true, false,
				).
				Count(&activeStateCount).Error; err != nil {
				return ChannelSmartScheduleAffinityTemporarilyUnavailable
			}
		}
		highestPriority := int64(0)
		highestPrioritySet := false
		preferredPriority := int64(0)
		preferredFound := false
		for _, ability := range abilities {
			priority, _ := channelSmartScheduleAbilityRouting(
				ability,
				usableChannels[ability.ChannelId],
			)
			if !highestPrioritySet || priority > highestPriority {
				highestPriority = priority
				highestPrioritySet = true
			}
			if ability.ChannelId == channelId {
				preferredPriority = priority
				preferredFound = true
			}
		}
		if activeStateCount == 0 {
			if preferredFound {
				return ChannelSmartScheduleAffinityEligible
			}
			return ChannelSmartScheduleAffinityInvalid
		}
		if preferredFound && preferredPriority == highestPriority {
			return ChannelSmartScheduleAffinityEligible
		}
		if preferredFound {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		return ChannelSmartScheduleAffinityInvalid
	}

	var routes []channelSmartScheduleCachedRoute
	var allRoutes []channelSmartScheduleCachedRoute
	baseOptions := selectionOptions
	baseOptions.EstimatedPromptTokens = 0
	baseOptions.RequestBodyBytes = 0
	channelSyncLock.RLock()
	for _, candidateModel := range modelNames {
		candidateRoutes := channelSmartScheduleRouteCache[group][candidateModel]
		candidateRoutes = filterChannelSmartScheduleCachedRoutes(
			candidateRoutes, requestPath, requestModelName, selectionOptions,
		)
		if len(candidateRoutes) > 0 {
			routes = candidateRoutes
			allRoutes = filterChannelSmartScheduleCachedRoutes(
				channelSmartScheduleRouteCache[group][candidateModel],
				requestPath, requestModelName, baseOptions,
			)
			break
		}
	}
	channelSyncLock.RUnlock()
	if len(routes) == 0 {
		return ChannelSmartScheduleAffinityInvalid
	}

	highestPriority := routes[0].priority
	managed := false
	preferredPriority := int64(0)
	preferredFound := false
	for _, route := range routes {
		managed = managed || route.managed
		if route.priority > highestPriority {
			highestPriority = route.priority
		}
		if route.channelId == channelId {
			preferredPriority = route.priority
			preferredFound = true
		}
	}
	preferredInAll := false
	preferredFilteredByExploration := false
	for _, route := range allRoutes {
		if route.channelId == channelId {
			preferredInAll = true
			preferredFilteredByExploration =
				route.temporaryTrafficKind == ChannelSmartScheduleTemporaryTrafficExploration &&
					selectionOptions.ShouldAvoidExploration(route.explorationMaxPromptTokens)
			break
		}
	}
	if !preferredFound {
		if preferredInAll && preferredFilteredByExploration {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		return ChannelSmartScheduleAffinityInvalid
	}
	if !managed || preferredPriority == highestPriority {
		return ChannelSmartScheduleAffinityEligible
	}
	return ChannelSmartScheduleAffinityTemporarilyUnavailable
}

func containsAbilityChannel(abilities []Ability, channelId int) bool {
	for _, ability := range abilities {
		if ability.ChannelId == channelId {
			return true
		}
	}
	return false
}
