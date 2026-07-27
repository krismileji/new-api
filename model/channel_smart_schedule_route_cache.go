package model

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type channelSmartScheduleCachedRoute struct {
	channelId int
	priority  int64
	weight    uint
}

var channelSmartScheduleRouteCache map[string]map[string][]channelSmartScheduleCachedRoute

func buildChannelSmartScheduleRouteCache(abilities []*Ability, channels map[int]*Channel) map[string]map[string][]channelSmartScheduleCachedRoute {
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
		modelRoutes[ability.Model] = append(modelRoutes[ability.Model], channelSmartScheduleCachedRoute{
			channelId: ability.ChannelId,
			priority:  abilityPriority(*ability),
			weight:    ability.Weight,
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
	var sumWeight uint64
	for _, route := range routes {
		if route.priority != targetPriority {
			continue
		}
		targetRoutes = append(targetRoutes, route)
		if uint64(route.weight) > math.MaxUint64-sumWeight {
			return nil, true, errors.New("渠道路由权重溢出")
		}
		sumWeight += uint64(route.weight)
	}
	if len(targetRoutes) == 0 {
		return nil, true, fmt.Errorf("no channel found, group: %s, model: %s, priority: %d", group, modelName, targetPriority)
	}
	if len(targetRoutes) == 1 {
		channel := channelsIDM[targetRoutes[0].channelId]
		if channel == nil {
			return nil, true, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", targetRoutes[0].channelId)
		}
		return channel, true, nil
	}

	var smoothingFactor uint64 = 1
	var smoothingAdjustment uint64
	if sumWeight == 0 {
		sumWeight = uint64(len(targetRoutes)) * 100
		smoothingAdjustment = 100
	} else if sumWeight/uint64(len(targetRoutes)) < 10 {
		smoothingFactor = 100
	}
	if sumWeight > math.MaxInt64/smoothingFactor {
		return nil, true, errors.New("渠道路由权重溢出")
	}
	totalWeight := sumWeight * smoothingFactor
	randomWeight := uint64(rand.Int63n(int64(totalWeight)))
	for _, route := range targetRoutes {
		effectiveWeight := uint64(route.weight)*smoothingFactor + smoothingAdjustment
		if randomWeight < effectiveWeight {
			channel := channelsIDM[route.channelId]
			if channel == nil {
				return nil, true, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", route.channelId)
			}
			return channel, true, nil
		}
		randomWeight -= effectiveWeight
	}
	return nil, true, errors.New("未找到可用渠道")
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
