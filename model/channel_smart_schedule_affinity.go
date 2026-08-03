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
func ChannelSmartScheduleAffinityEligibility(group string, modelName string, channelId int, requestPath string) ChannelSmartScheduleAffinityStatus {
	group = strings.TrimSpace(group)
	requestModelName := strings.TrimSpace(modelName)
	modelNames := channelSmartScheduleRouteModelNames(requestModelName)
	if group == "" || len(modelNames) == 0 || channelId <= 0 {
		return ChannelSmartScheduleAffinityInvalid
	}

	if !common.MemoryCacheEnabled {
		selectedModel := ""
		var abilities []Ability
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
			usableChannels := make(map[int]struct{}, len(channels))
			for _, channel := range channels {
				if channel.Type == constant.ChannelTypeAdvancedCustom {
					config := channel.GetOtherSettings().AdvancedCustom
					if config == nil || !config.SupportsPathForModel(requestPath, requestModelName) {
						continue
					}
				}
				usableChannels[channel.Id] = struct{}{}
			}
			for _, ability := range candidateAbilities {
				if _, usable := usableChannels[ability.ChannelId]; usable {
					abilities = append(abilities, ability)
				}
			}
			if len(abilities) > 0 {
				selectedModel = candidateModel
				break
			}
		}
		if selectedModel == "" {
			return ChannelSmartScheduleAffinityInvalid
		}

		var activeStateCount int64
		if err := DB.Model(&ChannelSmartScheduleRouteState{}).
			Where(
				"group_name = ? AND model_name = ? AND participation_set = ? AND excluded = ?",
				group, selectedModel, true, false,
			).
			Count(&activeStateCount).Error; err != nil {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		highestPriority := int64(0)
		highestPrioritySet := false
		preferredPriority := int64(0)
		preferredFound := false
		for _, ability := range abilities {
			priority := abilityPriority(ability)
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
	channelSyncLock.RLock()
	for _, candidateModel := range modelNames {
		candidateRoutes := channelSmartScheduleRouteCache[group][candidateModel]
		candidateRoutes = filterChannelSmartScheduleCachedRoutes(
			candidateRoutes, requestPath, requestModelName, ChannelSelectionOptions{},
		)
		if len(candidateRoutes) > 0 {
			routes = candidateRoutes
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
	if !preferredFound {
		return ChannelSmartScheduleAffinityInvalid
	}
	if !managed || preferredPriority == highestPriority {
		return ChannelSmartScheduleAffinityEligible
	}
	return ChannelSmartScheduleAffinityTemporarilyUnavailable
}
