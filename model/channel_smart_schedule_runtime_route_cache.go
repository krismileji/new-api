package model

import (
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
)

type channelSmartScheduleRuntimeRouteIndex struct {
	routesByChannel map[int]map[string]map[string]channelSmartScheduleCachedRoute
}

type channelSmartScheduleRuntimeSelectedRoute struct {
	modelName string
	route     channelSmartScheduleCachedRoute
}

var channelSmartScheduleRuntimeRouteIndexCache atomic.Pointer[channelSmartScheduleRuntimeRouteIndex]

func buildChannelSmartScheduleRuntimeRouteIndex(
	routeCache map[string]map[string][]channelSmartScheduleCachedRoute,
) *channelSmartScheduleRuntimeRouteIndex {
	index := &channelSmartScheduleRuntimeRouteIndex{
		routesByChannel: make(map[int]map[string]map[string]channelSmartScheduleCachedRoute),
	}
	for group, modelRoutes := range routeCache {
		if group == "" {
			continue
		}
		for modelName, routes := range modelRoutes {
			if modelName == "" {
				continue
			}
			for _, route := range routes {
				if route.channelId <= 0 {
					continue
				}
				models := index.routesByChannel[route.channelId]
				if models == nil {
					models = make(map[string]map[string]channelSmartScheduleCachedRoute)
					index.routesByChannel[route.channelId] = models
				}
				groups := models[modelName]
				if groups == nil {
					groups = make(map[string]channelSmartScheduleCachedRoute)
					models[modelName] = groups
				}
				groups[group] = route
			}
		}
	}
	return index
}

func publishChannelSmartScheduleRuntimeRouteIndex(index *channelSmartScheduleRuntimeRouteIndex) {
	if index == nil {
		index = buildChannelSmartScheduleRuntimeRouteIndex(nil)
	}
	channelSmartScheduleRuntimeRouteIndexCache.Store(index)
}

// refreshChannelSmartScheduleRuntimeRouteIndex publishes an immutable copy
// with one group/model pool replaced. The caller holds channelSyncLock.
func refreshChannelSmartScheduleRuntimeRouteIndex(
	group string,
	modelName string,
	previousRoutes []channelSmartScheduleCachedRoute,
	refreshedRoutes []channelSmartScheduleCachedRoute,
) {
	current := channelSmartScheduleRuntimeRouteIndexCache.Load()
	if current == nil {
		publishChannelSmartScheduleRuntimeRouteIndex(
			buildChannelSmartScheduleRuntimeRouteIndex(channelSmartScheduleRouteCache),
		)
		return
	}

	affectedChannelIds := make(map[int]struct{}, len(previousRoutes)+len(refreshedRoutes))
	refreshedByChannel := make(map[int]channelSmartScheduleCachedRoute, len(refreshedRoutes))
	for _, route := range previousRoutes {
		affectedChannelIds[route.channelId] = struct{}{}
	}
	for _, route := range refreshedRoutes {
		affectedChannelIds[route.channelId] = struct{}{}
		refreshedByChannel[route.channelId] = route
	}

	nextRoutesByChannel := make(
		map[int]map[string]map[string]channelSmartScheduleCachedRoute,
		len(current.routesByChannel)+len(refreshedRoutes),
	)
	for channelId, models := range current.routesByChannel {
		nextRoutesByChannel[channelId] = models
	}
	for channelId := range affectedChannelIds {
		currentModels := current.routesByChannel[channelId]
		nextModels := make(map[string]map[string]channelSmartScheduleCachedRoute, len(currentModels)+1)
		for candidateModel, groups := range currentModels {
			nextModels[candidateModel] = groups
		}

		currentGroups := currentModels[modelName]
		nextGroups := make(map[string]channelSmartScheduleCachedRoute, len(currentGroups)+1)
		for candidateGroup, route := range currentGroups {
			nextGroups[candidateGroup] = route
		}
		delete(nextGroups, group)
		if route, exists := refreshedByChannel[channelId]; exists {
			nextGroups[group] = route
		}
		if len(nextGroups) == 0 {
			delete(nextModels, modelName)
		} else {
			nextModels[modelName] = nextGroups
		}
		if len(nextModels) == 0 {
			delete(nextRoutesByChannel, channelId)
		} else {
			nextRoutesByChannel[channelId] = nextModels
		}
	}
	publishChannelSmartScheduleRuntimeRouteIndex(&channelSmartScheduleRuntimeRouteIndex{
		routesByChannel: nextRoutesByChannel,
	})
}

func channelSmartScheduleRuntimeRoutesFromCache(
	channelId int,
	requestedModelName string,
) map[string]channelSmartScheduleRuntimeSelectedRoute {
	if channelId <= 0 {
		return map[string]channelSmartScheduleRuntimeSelectedRoute{}
	}
	modelNames := channelSmartScheduleRouteModelNames(requestedModelName)
	if len(modelNames) == 0 {
		return map[string]channelSmartScheduleRuntimeSelectedRoute{}
	}
	index := channelSmartScheduleRuntimeRouteIndexCache.Load()
	if index == nil {
		return map[string]channelSmartScheduleRuntimeSelectedRoute{}
	}
	models := index.routesByChannel[channelId]
	selected := make(map[string]channelSmartScheduleRuntimeSelectedRoute)
	for _, modelName := range modelNames {
		for group, route := range models[modelName] {
			if _, exists := selected[group]; exists {
				continue
			}
			selected[group] = channelSmartScheduleRuntimeSelectedRoute{
				modelName: modelName,
				route:     route,
			}
		}
	}
	return selected
}

// CachedChannelSmartScheduleRuntimeParticipates reports whether the cached
// channel/model route participates in any managed pool without allocating a
// per-request route map. The second result is false only when channel caching
// is disabled and callers must preserve the database fallback behavior.
func CachedChannelSmartScheduleRuntimeParticipates(channelId int, modelName string) (bool, bool) {
	if !common.MemoryCacheEnabled {
		return false, false
	}
	modelName = strings.TrimSpace(modelName)
	if channelId <= 0 || modelName == "" {
		return false, true
	}
	index := channelSmartScheduleRuntimeRouteIndexCache.Load()
	if index == nil {
		return false, true
	}
	models := index.routesByChannel[channelId]
	exactGroups := models[modelName]
	for _, route := range exactGroups {
		if route.participates {
			return true, true
		}
	}
	normalizedModelName := channelSmartScheduleModelName(modelName)
	if normalizedModelName == "" || normalizedModelName == modelName {
		return false, true
	}
	for group, route := range models[normalizedModelName] {
		if _, exact := exactGroups[group]; exact {
			continue
		}
		if route.participates {
			return true, true
		}
	}
	return false, true
}

// GetCachedChannelSmartScheduleRuntimeParticipatingRoutes returns the cached
// effective routes for one channel and requested model. The boolean is false
// only when the process-level channel cache is disabled.
func GetCachedChannelSmartScheduleRuntimeParticipatingRoutes(
	channelId int,
	modelName string,
) (map[string]string, bool) {
	if !common.MemoryCacheEnabled {
		return nil, false
	}
	selected := channelSmartScheduleRuntimeRoutesFromCache(channelId, modelName)
	participating := make(map[string]string, len(selected))
	for group, route := range selected {
		if route.route.participates {
			participating[group] = route.modelName
		}
	}
	return participating, true
}

// GetCachedChannelSmartScheduleRuntimeRoutes returns cached participating
// routes together with the runtime stability and temporary-traffic state.
func GetCachedChannelSmartScheduleRuntimeRoutes(
	channelId int,
	modelName string,
) (map[string]ChannelSmartScheduleRuntimeRoute, bool) {
	if !common.MemoryCacheEnabled {
		return nil, false
	}
	selected := channelSmartScheduleRuntimeRoutesFromCache(channelId, modelName)
	routes := make(map[string]ChannelSmartScheduleRuntimeRoute, len(selected))
	for group, selectedRoute := range selected {
		route := selectedRoute.route
		if !route.participates {
			continue
		}
		sampleSince := route.temporaryTrafficSince
		if route.stabilityState == ChannelSmartScheduleStabilityProbing && route.stabilitySince > sampleSince {
			sampleSince = route.stabilitySince
		}
		routes[group] = ChannelSmartScheduleRuntimeRoute{
			ModelName:            selectedRoute.modelName,
			SampleSince:          sampleSince,
			StabilityState:       route.stabilityState,
			TemporaryTrafficKind: route.temporaryTrafficKind,
		}
	}
	return routes, true
}
