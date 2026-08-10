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

// ChannelSmartScheduleAffinityEligibility applies the same strict pool and
// participation gate as normal routing while smart scheduling is enabled.
func ChannelSmartScheduleAffinityEligibility(
	group string,
	modelName string,
	channelId int,
	requestPath string,
	options ...ChannelSelectionOptions,
) ChannelSmartScheduleAffinityStatus {
	group = strings.TrimSpace(group)
	requestModelName := strings.TrimSpace(modelName)
	modelNames := channelSmartScheduleRouteModelNames(requestModelName)
	if group == "" || len(modelNames) == 0 || channelId <= 0 {
		return ChannelSmartScheduleAffinityInvalid
	}

	trafficPolicy := currentChannelSmartScheduleTrafficPolicy()
	if trafficPolicy == nil || !trafficPolicy.enabled {
		paused, err := channelSmartScheduleAffinityRoutePaused(group, modelNames, channelId)
		if err != nil {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		if paused {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		channel, err := CacheGetChannel(channelId)
		if err != nil || channel == nil || channel.Status != common.ChannelStatusEnabled {
			return ChannelSmartScheduleAffinityInvalid
		}
		if requestPath != "" && channel.Type == constant.ChannelTypeAdvancedCustom {
			config := channel.GetOtherSettings().AdvancedCustom
			if config == nil || !config.SupportsPathForModel(requestPath, requestModelName) {
				return ChannelSmartScheduleAffinityInvalid
			}
		}
		if !IsChannelEnabledForGroupModel(group, requestModelName, channelId) {
			return ChannelSmartScheduleAffinityInvalid
		}
		return ChannelSmartScheduleAffinityEligible
	}

	selectionOptions := channelSelectionOptions(options)
	if !common.MemoryCacheEnabled {
		return channelSmartScheduleAffinityEligibilityFromDatabase(
			group, requestModelName, modelNames, channelId, requestPath, selectionOptions, trafficPolicy,
		)
	}
	return channelSmartScheduleAffinityEligibilityFromCache(
		group, requestModelName, modelNames, channelId, requestPath, selectionOptions, trafficPolicy,
	)
}

func channelSmartScheduleAffinityRoutePaused(
	group string,
	modelNames []string,
	channelId int,
) (bool, error) {
	now := common.GetTimestamp()
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		if channelSmartScheduleRouteCache == nil {
			return false, nil
		}
		for _, modelName := range modelNames {
			for _, route := range channelSmartScheduleRouteCache[group][modelName] {
				if route.channelId == channelId && route.trafficPausedUntil > now {
					return true, nil
				}
			}
		}
		return false, nil
	}
	if DB == nil || !DB.Migrator().HasTable(&ChannelSmartScheduleGroupPause{}) {
		return false, nil
	}
	for _, modelName := range modelNames {
		pausedChannelIDs, err := loadActiveChannelSmartSchedulePausedChannelIds(
			DB, group, modelName, []int{channelId}, now,
		)
		if err != nil {
			return false, err
		}
		if _, paused := pausedChannelIDs[channelId]; paused {
			return true, nil
		}
	}
	return false, nil
}

func channelSmartScheduleAffinityEligibilityFromDatabase(
	group string,
	requestModelName string,
	modelNames []string,
	channelId int,
	requestPath string,
	selectionOptions ChannelSelectionOptions,
	trafficPolicy *channelSmartScheduleTrafficPolicy,
) ChannelSmartScheduleAffinityStatus {
	preferredPaused := false
	preferredFilteredByRequestLimit := false
	for _, candidateModel := range modelNames {
		if !trafficPolicy.allowsPool(group, candidateModel) {
			continue
		}
		var abilities []Ability
		if err := DB.Select("channel_id", "priority", "weight").
			Where(&Ability{Group: group, Model: candidateModel, Enabled: true}).
			Find(&abilities).Error; err != nil {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		var err error
		abilities, err = filterChannelSmartScheduleTrafficAbilities(
			abilities, group, candidateModel, trafficPolicy,
		)
		if err != nil {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		if len(abilities) == 0 {
			continue
		}

		channelIDs := make([]int, 0, len(abilities))
		for _, ability := range abilities {
			channelIDs = append(channelIDs, ability.ChannelId)
		}
		pausedChannelIDs, err := loadActiveChannelSmartSchedulePausedChannelIds(
			DB, group, candidateModel, channelIDs, common.GetTimestamp(),
		)
		if err != nil {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		if _, paused := pausedChannelIDs[channelId]; paused {
			preferredPaused = true
		}

		var channels []Channel
		if err := DB.Where("id IN ? AND status = ?", channelIDs, common.ChannelStatusEnabled).
			Find(&channels).Error; err != nil {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		channelByID := make(map[int]*Channel, len(channels))
		for index := range channels {
			channel := &channels[index]
			if requestPath != "" && channel.Type == constant.ChannelTypeAdvancedCustom {
				config := channel.GetOtherSettings().AdvancedCustom
				if config == nil || !config.SupportsPathForModel(requestPath, requestModelName) {
					continue
				}
			}
			channelByID[channel.Id] = channel
		}

		available := make([]Ability, 0, len(abilities))
		for _, ability := range abilities {
			if _, paused := pausedChannelIDs[ability.ChannelId]; paused {
				continue
			}
			if channelByID[ability.ChannelId] != nil {
				available = append(available, ability)
			}
		}
		if len(available) == 0 {
			continue
		}

		preferredAvailable := containsAbilityChannel(available, channelId)
		if selectionOptions.HasRequestSize() {
			requestLimitStates, err := loadChannelSmartScheduleRequestLimitStates(
				group, candidateModel, channelIDs,
			)
			if err != nil {
				return ChannelSmartScheduleAffinityTemporarilyUnavailable
			}
			available = filterAbilitiesBySmartScheduleRequestLimits(
				available, requestLimitStates, selectionOptions,
			)
			if preferredAvailable && !containsAbilityChannel(available, channelId) {
				preferredFilteredByRequestLimit = true
			}
		}
		if len(available) == 0 {
			continue
		}

		highestPriority := int64(0)
		highestPrioritySet := false
		preferredPriority := int64(0)
		preferredFound := false
		for _, ability := range available {
			priority, _ := channelSmartScheduleAbilityRouting(ability, channelByID[ability.ChannelId])
			if !highestPrioritySet || priority > highestPriority {
				highestPriority = priority
				highestPrioritySet = true
			}
			if ability.ChannelId == channelId {
				preferredPriority = priority
				preferredFound = true
			}
		}
		if preferredFound && preferredPriority == highestPriority {
			return ChannelSmartScheduleAffinityEligible
		}
		if preferredFound || preferredPaused || preferredFilteredByRequestLimit {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		return ChannelSmartScheduleAffinityInvalid
	}
	if preferredPaused || preferredFilteredByRequestLimit {
		return ChannelSmartScheduleAffinityTemporarilyUnavailable
	}
	return ChannelSmartScheduleAffinityInvalid
}

func channelSmartScheduleAffinityEligibilityFromCache(
	group string,
	requestModelName string,
	modelNames []string,
	channelId int,
	requestPath string,
	selectionOptions ChannelSelectionOptions,
	trafficPolicy *channelSmartScheduleTrafficPolicy,
) ChannelSmartScheduleAffinityStatus {
	baseOptions := selectionOptions
	baseOptions.EstimatedPromptTokens = 0
	baseOptions.RequestBodyBytes = 0
	preferredPaused := false
	preferredFilteredByRequestLimit := false
	now := common.GetTimestamp()

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	if channelSmartScheduleRouteCache == nil {
		return ChannelSmartScheduleAffinityInvalid
	}
	for _, candidateModel := range modelNames {
		routes := filterChannelSmartScheduleTrafficCachedRoutes(
			channelSmartScheduleRouteCache[group][candidateModel],
			group,
			candidateModel,
			trafficPolicy,
		)
		for _, route := range routes {
			if route.channelId == channelId && route.trafficPausedUntil > now {
				preferredPaused = true
			}
		}
		allRoutes := filterChannelSmartScheduleCachedRoutes(
			routes, requestPath, requestModelName, baseOptions,
		)
		availableRoutes := filterChannelSmartScheduleCachedRoutes(
			routes, requestPath, requestModelName, selectionOptions,
		)
		preferredInAll := containsChannelSmartScheduleCachedRoute(allRoutes, channelId)
		if preferredInAll && !containsChannelSmartScheduleCachedRoute(availableRoutes, channelId) {
			preferredFilteredByRequestLimit = true
		}
		if len(availableRoutes) == 0 {
			continue
		}

		highestPriority := availableRoutes[0].priority
		preferredPriority := int64(0)
		preferredFound := false
		for _, route := range availableRoutes {
			if route.priority > highestPriority {
				highestPriority = route.priority
			}
			if route.channelId == channelId {
				preferredPriority = route.priority
				preferredFound = true
			}
		}
		if preferredFound && preferredPriority == highestPriority {
			return ChannelSmartScheduleAffinityEligible
		}
		if preferredFound || preferredPaused || preferredFilteredByRequestLimit {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		return ChannelSmartScheduleAffinityInvalid
	}
	if preferredPaused || preferredFilteredByRequestLimit {
		return ChannelSmartScheduleAffinityTemporarilyUnavailable
	}
	return ChannelSmartScheduleAffinityInvalid
}

func containsAbilityChannel(abilities []Ability, channelId int) bool {
	for _, ability := range abilities {
		if ability.ChannelId == channelId {
			return true
		}
	}
	return false
}

func containsChannelSmartScheduleCachedRoute(routes []channelSmartScheduleCachedRoute, channelId int) bool {
	for _, route := range routes {
		if route.channelId == channelId {
			return true
		}
	}
	return false
}
