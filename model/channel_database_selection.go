package model

import (
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func getRandomSatisfiedChannelWithoutCache(
	group string,
	modelName string,
	retry int,
	requestPath string,
	options ChannelSelectionOptions,
) (*Channel, error) {
	channel, err := getChannelFromDatabasePool(group, modelName, modelName, retry, requestPath, options)
	if err != nil || channel != nil {
		return channel, err
	}

	matchingModelName := ratio_setting.FormatMatchingModelName(modelName)
	if matchingModelName == "" || matchingModelName == modelName {
		return nil, nil
	}
	return getChannelFromDatabasePool(group, matchingModelName, modelName, retry, requestPath, options)
}

func getChannelFromDatabasePool(
	group string,
	poolModelName string,
	requestModelName string,
	retry int,
	requestPath string,
	options ChannelSelectionOptions,
) (*Channel, error) {
	query := DB.Model(&Ability{}).
		Where(commonGroupCol+" = ? AND model = ? AND enabled = ?", group, poolModelName, true)
	query = applyChannelSelectionOptions(query, options)

	var abilities []Ability
	if err := query.
		Order("priority DESC").
		Order("channel_id ASC").
		Find(&abilities).Error; err != nil {
		return nil, err
	}
	if len(abilities) == 0 {
		return nil, nil
	}

	channelIds := make([]int, len(abilities))
	for index := range abilities {
		channelIds[index] = abilities[index].ChannelId
	}
	pausedChannelIds, err := loadActiveChannelSmartSchedulePausedChannelIds(
		DB, group, poolModelName, channelIds, common.GetTimestamp(),
	)
	if err != nil {
		return nil, err
	}
	var channels []Channel
	if err := DB.
		Where("id IN ? AND status = ?", channelIds, common.ChannelStatusEnabled).
		Find(&channels).Error; err != nil {
		return nil, err
	}
	channelById := make(map[int]*Channel, len(channels))
	for index := range channels {
		channelById[channels[index].Id] = &channels[index]
	}

	available := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		if _, paused := pausedChannelIds[ability.ChannelId]; paused {
			continue
		}
		channel := channelById[ability.ChannelId]
		if channel == nil {
			continue
		}
		if requestPath != "" && channel.Type == constant.ChannelTypeAdvancedCustom {
			config := channel.GetOtherSettings().AdvancedCustom
			if config == nil || !config.SupportsPathForModel(requestPath, requestModelName) {
				continue
			}
		}
		available = append(available, ability)
	}
	if len(available) == 0 {
		return nil, nil
	}
	if options.HasRequestSize() {
		availableChannelIDs := make([]int, 0, len(available))
		for _, ability := range available {
			availableChannelIDs = append(availableChannelIDs, ability.ChannelId)
		}
		requestLimitStates, err := loadChannelSmartScheduleRequestLimitStates(
			group, poolModelName, availableChannelIDs,
		)
		if err != nil {
			return nil, err
		}
		available = filterAbilitiesBySmartScheduleRequestLimits(available, requestLimitStates, options)
	}
	if len(available) == 0 {
		return nil, nil
	}
	priorities := make([]int64, 0, len(available))
	seenPriorities := make(map[int64]struct{}, len(available))
	for _, ability := range available {
		priority, _ := channelSmartScheduleAbilityRouting(
			ability,
			channelById[ability.ChannelId],
		)
		if _, exists := seenPriorities[priority]; exists {
			continue
		}
		seenPriorities[priority] = struct{}{}
		priorities = append(priorities, priority)
	}
	sort.Slice(priorities, func(i int, j int) bool { return priorities[i] > priorities[j] })
	if retry < 0 {
		retry = 0
	}
	if retry >= len(priorities) {
		retry = len(priorities) - 1
	}
	targetPriority := priorities[retry]
	channelIds = channelIds[:0]
	weights := make([]uint, 0, len(available))
	for _, ability := range available {
		priority, weight := channelSmartScheduleAbilityRouting(
			ability,
			channelById[ability.ChannelId],
		)
		if priority != targetPriority {
			continue
		}
		channelIds = append(channelIds, ability.ChannelId)
		weights = append(weights, weight)
	}
	channelId, err := chooseChannelByWeights(channelIds, weights)
	if err != nil {
		return nil, err
	}
	return channelById[channelId], nil
}
