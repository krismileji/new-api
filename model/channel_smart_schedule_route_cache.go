package model

import (
	"fmt"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type channelSmartScheduleCachedRoute struct {
	channelId int
	priority  int64
	weight    uint
	managed   bool
}

var channelSmartScheduleRouteCache map[string]map[string][]channelSmartScheduleCachedRoute

func buildChannelSmartScheduleRouteCache(abilities []*Ability, channels map[int]*Channel) map[string]map[string][]channelSmartScheduleCachedRoute {
	var states []ChannelSmartScheduleRouteState
	if DB != nil && DB.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
		if err := DB.
			Select("group_name", "model_name").
			Where("participation_set = ? AND excluded = ?", true, false).
			Find(&states).Error; err != nil {
			common.SysError("load smart schedule managed pools failed: " + err.Error())
			return buildChannelSmartScheduleRouteCacheWithManagedPools(abilities, channels, nil, true)
		}
	}
	return buildChannelSmartScheduleRouteCacheFromStates(abilities, channels, states)
}

func buildChannelSmartScheduleRouteCacheFromStates(
	abilities []*Ability,
	channels map[int]*Channel,
	states []ChannelSmartScheduleRouteState,
) map[string]map[string][]channelSmartScheduleCachedRoute {
	managedPools := make(map[channelSmartScheduleRoutePool]struct{})
	for _, state := range states {
		if state.Participates() {
			managedPools[channelSmartScheduleRoutePool{group: state.GroupName, model: state.ModelName}] = struct{}{}
		}
	}
	return buildChannelSmartScheduleRouteCacheWithManagedPools(abilities, channels, managedPools, false)
}

func buildChannelSmartScheduleRouteCacheWithManagedPools(
	abilities []*Ability,
	channels map[int]*Channel,
	managedPools map[channelSmartScheduleRoutePool]struct{},
	managedLookupFailed bool,
) map[string]map[string][]channelSmartScheduleCachedRoute {
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
		_, managed := managedPools[channelSmartScheduleRoutePool{group: ability.Group, model: ability.Model}]
		modelRoutes[ability.Model] = append(modelRoutes[ability.Model], channelSmartScheduleCachedRoute{
			channelId: ability.ChannelId,
			priority:  abilityPriority(*ability),
			weight:    ability.Weight,
			managed:   managedLookupFailed || managed,
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
	if channelSmartScheduleRouteCache == nil {
		return nil, false, nil
	}
	routes := channelSmartScheduleRouteCache[group][modelName]
	routes = filterChannelSmartScheduleCachedRoutes(routes, requestPath, modelName, options)
	if len(routes) == 0 {
		normalizedModel := ratio_setting.FormatMatchingModelName(modelName)
		routes = channelSmartScheduleRouteCache[group][normalizedModel]
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
	for _, route := range routes {
		channel := channelsIDM[route.channelId]
		if channel == nil || channel.Status != common.ChannelStatusEnabled {
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
	return filtered
}
