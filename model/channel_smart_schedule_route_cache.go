package model

import (
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type channelSmartScheduleCachedRoute struct {
	channelId                       int
	priority                        int64
	weight                          uint
	participates                    bool
	trafficPausedUntil              int64
	temporaryTrafficKind            string
	temporaryTrafficSince           int64
	stabilityState                  string
	stabilitySince                  int64
	explorationMaxPromptTokens      int
	stabilityReleaseMaxPromptTokens int
}

var channelSmartScheduleRouteCache map[string]map[string][]channelSmartScheduleCachedRoute

// RefreshChannelSmartScheduleRoutePoolCache reloads one route pool after a
// runtime overlay changes Ability routing or request-limit state.
func RefreshChannelSmartScheduleRoutePoolCache(group string, modelName string) error {
	if !common.MemoryCacheEnabled {
		return nil
	}
	group = strings.TrimSpace(group)
	modelName = strings.TrimSpace(modelName)
	if group == "" || modelName == "" {
		return nil
	}
	channelCacheRefreshLock.Lock()
	defer channelCacheRefreshLock.Unlock()

	var abilities []*Ability
	if err := DB.Where(&Ability{Group: group, Model: modelName}).
		Order("channel_id ASC").Find(&abilities).Error; err != nil {
		return err
	}
	channelIds := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		channelIds = append(channelIds, ability.ChannelId)
	}
	channels := make(map[int]*Channel, len(channelIds))
	if len(channelIds) > 0 {
		var poolChannels []*Channel
		if err := DB.Select("id", "status", "priority", "weight").
			Where("id IN ?", channelIds).Find(&poolChannels).Error; err != nil {
			return err
		}
		for _, channel := range poolChannels {
			channels[channel.Id] = channel
		}
	}
	var states []ChannelSmartScheduleRouteState
	if err := DB.Where("group_name = ? AND model_name = ?", group, modelName).
		Find(&states).Error; err != nil {
		return err
	}
	var pauses []ChannelSmartScheduleGroupPause
	if len(channelIds) > 0 {
		if err := DB.Where(
			"group_name = ? AND model_name = ? AND channel_id IN ? AND paused_until > ?",
			group, modelName, channelIds, common.GetTimestamp(),
		).Find(&pauses).Error; err != nil {
			return err
		}
	}
	refreshed := buildChannelSmartScheduleRouteCacheFromStates(abilities, channels, states, pauses)

	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channelSmartScheduleRouteCache == nil {
		return nil
	}
	modelRoutes := channelSmartScheduleRouteCache[group]
	previousRoutes := append([]channelSmartScheduleCachedRoute(nil), modelRoutes[modelName]...)
	poolRoutes := refreshed[group][modelName]
	if len(poolRoutes) == 0 {
		if modelRoutes != nil {
			delete(modelRoutes, modelName)
			if len(modelRoutes) == 0 {
				delete(channelSmartScheduleRouteCache, group)
			}
		}
		refreshChannelSmartScheduleRuntimeRouteIndex(group, modelName, previousRoutes, nil)
		return nil
	}
	if modelRoutes == nil {
		modelRoutes = make(map[string][]channelSmartScheduleCachedRoute)
		channelSmartScheduleRouteCache[group] = modelRoutes
	}
	modelRoutes[modelName] = poolRoutes
	refreshChannelSmartScheduleRuntimeRouteIndex(group, modelName, previousRoutes, poolRoutes)
	return nil
}

func buildChannelSmartScheduleRouteCache(abilities []*Ability, channels map[int]*Channel) map[string]map[string][]channelSmartScheduleCachedRoute {
	var states []ChannelSmartScheduleRouteState
	if DB != nil && DB.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
		if err := DB.
			Select(
				"channel_id", "group_name", "model_name", "participation_set", "excluded",
				"temporary_traffic_kind", "temporary_traffic_since", "stability_state", "stability_since",
				"exploration_max_prompt_tokens",
				"stability_release_max_prompt_tokens",
			).
			Where("participation_set = ? AND excluded = ?", true, false).
			Find(&states).Error; err != nil {
			common.SysError("load smart schedule managed pools failed: " + err.Error())
			return buildChannelSmartScheduleRouteCacheFromStates(abilities, channels, nil)
		}
	}
	groupPauses, err := loadActiveChannelSmartScheduleGroupPauses(DB, common.GetTimestamp())
	if err != nil {
		common.SysError("load smart schedule route pauses failed: " + err.Error())
	}
	return buildChannelSmartScheduleRouteCacheFromStates(abilities, channels, states, groupPauses)
}

func buildChannelSmartScheduleRouteCacheFromStates(
	abilities []*Ability,
	channels map[int]*Channel,
	states []ChannelSmartScheduleRouteState,
	groupPauses ...[]ChannelSmartScheduleGroupPause,
) map[string]map[string][]channelSmartScheduleCachedRoute {
	statesByPool := make(map[channelSmartScheduleRoutePool]map[int]ChannelSmartScheduleRouteState)
	for _, state := range states {
		if !state.Participates() {
			continue
		}
		pool := channelSmartScheduleRoutePool{group: state.GroupName, model: state.ModelName}
		if statesByPool[pool] == nil {
			statesByPool[pool] = make(map[int]ChannelSmartScheduleRouteState)
		}
		statesByPool[pool][state.ChannelId] = state
	}
	var pauses []ChannelSmartScheduleGroupPause
	if len(groupPauses) > 0 {
		pauses = groupPauses[0]
	}
	return buildChannelSmartScheduleRouteCacheWithStates(
		abilities,
		channels,
		statesByPool,
		channelSmartScheduleGroupPauseUntilByKey(pauses),
	)
}

func buildChannelSmartScheduleRouteCacheWithStates(
	abilities []*Ability,
	channels map[int]*Channel,
	statesByPool map[channelSmartScheduleRoutePool]map[int]ChannelSmartScheduleRouteState,
	pausedUntilByKey ...map[ChannelSmartScheduleRouteKey]int64,
) map[string]map[string][]channelSmartScheduleCachedRoute {
	groupPauseUntilByKey := map[ChannelSmartScheduleRouteKey]int64{}
	if len(pausedUntilByKey) > 0 && pausedUntilByKey[0] != nil {
		groupPauseUntilByKey = pausedUntilByKey[0]
	}
	cache := make(map[string]map[string][]channelSmartScheduleCachedRoute)
	for _, ability := range abilities {
		channel := channels[ability.ChannelId]
		if channel == nil || channel.Status != common.ChannelStatusEnabled || !ability.Enabled {
			continue
		}
		modelRoutes := cache[ability.Group]
		if modelRoutes == nil {
			modelRoutes = make(map[string][]channelSmartScheduleCachedRoute)
			cache[ability.Group] = modelRoutes
		}
		pool := channelSmartScheduleRoutePool{group: ability.Group, model: ability.Model}
		state := statesByPool[pool][ability.ChannelId]
		priority, weight := channelSmartScheduleAbilityRouting(
			*ability,
			channel,
		)
		modelRoutes[ability.Model] = append(modelRoutes[ability.Model], channelSmartScheduleCachedRoute{
			channelId:             ability.ChannelId,
			priority:              priority,
			weight:                weight,
			participates:          state.Participates(),
			temporaryTrafficSince: state.TemporaryTrafficSince,
			stabilitySince:        state.StabilitySince,
			trafficPausedUntil: groupPauseUntilByKey[channelSmartScheduleRouteKey(
				ability.ChannelId, ability.Group, ability.Model,
			)],
			temporaryTrafficKind:            state.TemporaryTrafficKind,
			stabilityState:                  state.StabilityState,
			explorationMaxPromptTokens:      state.ExplorationMaxPromptTokens,
			stabilityReleaseMaxPromptTokens: state.StabilityReleaseMaxPromptTokens,
		})
	}
	for _, modelRoutes := range cache {
		for modelName := range modelRoutes {
			sort.Slice(modelRoutes[modelName], func(i int, j int) bool {
				if modelRoutes[modelName][i].priority != modelRoutes[modelName][j].priority {
					return modelRoutes[modelName][i].priority > modelRoutes[modelName][j].priority
				}
				return modelRoutes[modelName][i].channelId < modelRoutes[modelName][j].channelId
			})
		}
	}
	return cache
}

// getRandomSatisfiedChannelByAbility uses the route-specific priority and
// weight stored on Ability. Caller must hold channelSyncLock for reading.
func getRandomSatisfiedChannelByAbility(group string, modelName string, retry int, requestPath string, options ChannelSelectionOptions) (*Channel, bool, error) {
	return getRandomSatisfiedChannelByAbilityWithTrafficPolicy(
		group, modelName, retry, requestPath, options, currentChannelSmartScheduleTrafficPolicy(),
	)
}

func getRandomSatisfiedChannelByAbilityWithTrafficPolicy(
	group string,
	modelName string,
	retry int,
	requestPath string,
	options ChannelSelectionOptions,
	trafficPolicy *channelSmartScheduleTrafficPolicy,
) (*Channel, bool, error) {
	if channelSmartScheduleRouteCache == nil {
		if trafficPolicy != nil && trafficPolicy.managesAnyPool(
			group, channelSmartScheduleRouteModelNames(modelName),
		) {
			return nil, true, nil
		}
		return nil, false, nil
	}
	routes := channelSmartScheduleRouteCache[group][modelName]
	routes = filterChannelSmartScheduleTrafficCachedRoutes(routes, group, modelName, trafficPolicy)
	routes = filterChannelSmartScheduleCachedRoutes(routes, requestPath, modelName, options)
	if len(routes) == 0 {
		normalizedModel := ratio_setting.FormatMatchingModelName(modelName)
		routes = channelSmartScheduleRouteCache[group][normalizedModel]
		routes = filterChannelSmartScheduleTrafficCachedRoutes(routes, group, normalizedModel, trafficPolicy)
		routes = filterChannelSmartScheduleCachedRoutes(routes, requestPath, modelName, options)
	}
	if len(routes) == 0 {
		return nil, true, nil
	}

	priorities := make([]int64, 0)
	seenPriorities := make(map[int64]struct{})
	for _, route := range routes {
		if _, exists := seenPriorities[route.priority]; exists {
			continue
		}
		seenPriorities[route.priority] = struct{}{}
		priorities = append(priorities, route.priority)
	}
	sort.Slice(priorities, func(i int, j int) bool { return priorities[i] > priorities[j] })
	if retry >= len(priorities) {
		retry = len(priorities) - 1
	}
	targetPriority := priorities[retry]
	targetRoutes := make([]channelSmartScheduleCachedRoute, 0, len(routes))
	for _, route := range routes {
		if route.priority != targetPriority {
			continue
		}
		targetRoutes = append(targetRoutes, route)
	}
	if len(targetRoutes) == 0 {
		return nil, true, fmt.Errorf("no channel found, group: %s, model: %s, priority: %d", group, modelName, targetPriority)
	}
	channelIDs := make([]int, len(targetRoutes))
	weights := make([]uint, len(targetRoutes))
	for index, route := range targetRoutes {
		channelIDs[index] = route.channelId
		weights[index] = route.weight
	}
	channelId, selectionErr := chooseChannelByWeights(channelIDs, weights)
	if selectionErr != nil {
		return nil, true, selectionErr
	}
	channel := channelsIDM[channelId]
	if channel == nil {
		return nil, true, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId)
	}
	return channel, true, nil
}

func filterChannelSmartScheduleCachedRoutes(routes []channelSmartScheduleCachedRoute, requestPath string, requestModel string, options ChannelSelectionOptions) []channelSmartScheduleCachedRoute {
	if len(routes) == 0 {
		return nil
	}
	channelIds := make([]int, 0, len(routes))
	now := common.GetTimestamp()
	for _, route := range routes {
		channel := channelsIDM[route.channelId]
		if channel == nil || channel.Status != common.ChannelStatusEnabled || route.trafficPausedUntil > now {
			continue
		}
		channelIds = append(channelIds, route.channelId)
	}
	channelIds = filterChannelsByRequestPathAndModel(channelIds, requestPath, requestModel)
	channelIds = filterChannelIDsBySelectionOptions(channelIds, options)
	allowed := make(map[int]struct{}, len(channelIds))
	for _, channelId := range channelIds {
		allowed[channelId] = struct{}{}
	}
	filtered := make([]channelSmartScheduleCachedRoute, 0, len(channelIds))
	for _, route := range routes {
		if _, exists := allowed[route.channelId]; exists {
			filtered = append(filtered, route)
		}
	}
	return filterChannelSmartScheduleRequestLimits(filtered, options)
}
