package model

import (
	"context"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ChannelSmartScheduleRouteRuntimeView contains the routing values used by
// request selection for one physical monitor row. Logical groups expose one
// candidate to the selector, so all of their physical members share the
// candidate priority/weight while retaining their member weights for display.
type ChannelSmartScheduleRouteRuntimeView struct {
	Priority             int64
	Weight               uint
	CandidateChannelId   int
	LogicalChannelId     int64
	LogicalRevision      int64
	LogicalMemberIds     []int
	LogicalMemberWeights []uint
	Participates         bool
	TrafficPausedUntil   int64
	State                *ChannelSmartScheduleRouteState
}

// GetChannelSmartScheduleRouteRuntimeViewsWithContext returns a read-only
// projection of the routing snapshot used by smart-schedule selection. It
// intentionally does not initialize or mutate route state; the monitor API is
// a diagnostic read and must not change scheduling data while rendering it.
func GetChannelSmartScheduleRouteRuntimeViewsWithContext(
	ctx context.Context,
	routes []ChannelSmartScheduleRoute,
) (map[ChannelSmartScheduleRouteKey]ChannelSmartScheduleRouteRuntimeView, error) {
	views := make(map[ChannelSmartScheduleRouteKey]ChannelSmartScheduleRouteRuntimeView, len(routes))
	for _, route := range routes {
		state := route.State
		views[channelSmartScheduleRouteKey(route.ChannelId, route.Group, route.Model)] = ChannelSmartScheduleRouteRuntimeView{
			Priority:           route.Priority,
			Weight:             route.Weight,
			CandidateChannelId: route.ChannelId,
			Participates:       state.Participates(),
			TrafficPausedUntil: route.TrafficPausedUntil,
			State:              &state,
		}
	}
	if len(routes) == 0 || !IsLogicalChannelGroupingEnabled() {
		return views, nil
	}
	// The selector reads these atomically published snapshots under the same
	// lock. Prefer them so the monitor cannot describe a different candidate
	// set during the asynchronous database-to-cache refresh.
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		cachedRoutes := channelSmartScheduleRouteCache
		cachedChannels := channelsIDM
		runtime := cloneLogicalChannelRuntimeSnapshot(logicalChannelRuntimeCache)
		routings := channelLogicalSmartScheduleRoutingCache
		channelSyncLock.RUnlock()
		if cachedRoutes != nil && runtime != nil {
			applyChannelSmartScheduleCachedRuntimeViews(views, routes, cachedRoutes, cachedChannels, runtime, routings)
			return views, nil
		}
	}
	if DB == nil {
		return views, nil
	}
	policy := currentChannelSmartScheduleTrafficPolicy()
	managedModel := func(group string, model string) bool {
		normalized := channelSmartScheduleModelName(model)
		return policy != nil &&
			(policy.managesPool(group, model) || policy.managesPool(group, normalized))
	}
	if ctx == nil {
		ctx = context.Background()
	}

	db := DB.WithContext(ctx)
	runtime, err := buildLogicalChannelRuntimeSnapshot(db)
	if err != nil || runtime == nil {
		return nil, err
	}
	logicalIDs := make([]int64, 0)
	seenLogicalIDs := make(map[int64]struct{})
	type poolKey struct {
		logicalID int64
		revision  int64
		group     string
		model     string
	}
	indexesByPool := make(map[poolKey][]int)
	for index, route := range routes {
		if !managedModel(route.Group, route.Model) {
			continue
		}
		identity, exists := runtime.Channels[route.ChannelId]
		if !exists || identity.Revision <= 0 || identity.LogicalChannelID == int64(route.ChannelId) {
			continue
		}
		group, exists := runtime.Groups[identity.LogicalChannelID]
		if !exists || !IsLogicalChannelGroupActive(group.Status) || len(group.Members) < 2 {
			continue
		}
		if _, seen := seenLogicalIDs[identity.LogicalChannelID]; !seen {
			seenLogicalIDs[identity.LogicalChannelID] = struct{}{}
			logicalIDs = append(logicalIDs, identity.LogicalChannelID)
		}
		key := poolKey{
			logicalID: identity.LogicalChannelID,
			revision:  identity.Revision,
			group:     route.Group,
			model:     channelSmartScheduleModelName(route.Model),
		}
		indexesByPool[key] = append(indexesByPool[key], index)
	}
	if len(indexesByPool) == 0 {
		return views, nil
	}

	overlays, err := loadLogicalSmartScheduleRouteOverlaysWithDB(
		db, logicalIDs, "", "",
	)
	if err != nil {
		return nil, err
	}
	for key, indexes := range indexesByPool {
		sort.Slice(indexes, func(i, j int) bool {
			return routes[indexes[i]].ChannelId < routes[indexes[j]].ChannelId
		})
		eligibleIndexes := indexes[:0]
		for _, index := range indexes {
			if routes[index].State.Participates() {
				eligibleIndexes = append(eligibleIndexes, index)
			}
		}
		indexes = eligibleIndexes
		if len(indexes) == 0 {
			continue
		}
		canonical := routes[indexes[0]]
		priority := canonical.Priority
		weight := canonical.Weight
		for _, index := range indexes[1:] {
			priority = max(priority, routes[index].Priority)
			weight = max(weight, routes[index].Weight)
		}
		state := canonical.State
		overlay, hasOverlay := overlays[channelLogicalSmartScheduleRouteKey{
			logicalID: key.logicalID, revision: key.revision,
			group: key.group, model: key.model,
		}]
		if hasOverlay {
			priority = overlay.routing.priority
			weight = overlay.routing.weight
			state = overlay.state
		}
		memberIDs := make([]int, 0, len(indexes))
		memberWeights := make([]uint, 0, len(indexes))
		for _, index := range indexes {
			memberID := routes[index].ChannelId
			memberIDs = append(memberIDs, memberID)
			memberWeight := uint(0)
			for _, member := range runtime.Groups[key.logicalID].Members {
				if member.ChannelID == memberID {
					memberWeight = member.Weight
					break
				}
			}
			memberWeights = append(memberWeights, memberWeight)
		}

		for _, index := range indexes {
			route := routes[index]
			routeKey := channelSmartScheduleRouteKey(route.ChannelId, route.Group, route.Model)
			view := views[routeKey]
			view.Priority = priority
			view.Weight = weight
			view.CandidateChannelId = canonical.ChannelId
			view.LogicalChannelId = key.logicalID
			view.LogicalRevision = key.revision
			view.LogicalMemberIds = append([]int(nil), memberIDs...)
			view.LogicalMemberWeights = append([]uint(nil), memberWeights...)
			if hasOverlay {
				effectiveState := state
				effectiveState.ChannelId = route.ChannelId
				effectiveState.GroupName = route.Group
				effectiveState.ModelName = route.Model
				view.State = &effectiveState
			}
			view.Participates = route.State.Participates() && state.Participates()
			views[routeKey] = view
		}
	}
	return views, nil
}

func applyChannelSmartScheduleCachedRuntimeViews(
	views map[ChannelSmartScheduleRouteKey]ChannelSmartScheduleRouteRuntimeView,
	routes []ChannelSmartScheduleRoute,
	cachedRoutes map[string]map[string][]channelSmartScheduleCachedRoute,
	cachedChannels map[int]*Channel,
	runtime *LogicalChannelRuntimeSnapshot,
	routings map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay,
) {
	policy := currentChannelSmartScheduleTrafficPolicy()
	now := common.GetTimestamp()
	managedModel := func(group string, model string) bool {
		normalized := channelSmartScheduleModelName(model)
		return policy != nil &&
			(policy.managesPool(group, model) || policy.managesPool(group, normalized))
	}
	type logicalKey struct {
		id, revision int64
		group, model string
	}
	logicalCandidates := make(map[logicalKey]channelSmartScheduleCachedRoute)
	seenMembers := make(map[logicalKey]map[int]struct{})
	for _, route := range routes {
		if !managedModel(route.Group, route.Model) {
			continue
		}
		pool := cachedRoutes[route.Group][route.Model]
		for _, cached := range pool {
			if cached.channelId != route.ChannelId {
				continue
			}
			identity, ok := runtime.Channels[cached.channelId]
			group, groupOK := runtime.Groups[identity.LogicalChannelID]
			if !ok || identity.Revision <= 0 || identity.LogicalChannelID == int64(cached.channelId) ||
				!groupOK || !IsLogicalChannelGroupActive(group.Status) || len(group.Members) < 2 {
				continue
			}
			key := logicalKey{identity.LogicalChannelID, identity.Revision, route.Group, channelSmartScheduleModelName(route.Model)}
			if !cached.participates || cached.trafficPausedUntil > now {
				continue
			}
			if cachedChannels != nil {
				if channel := cachedChannels[cached.channelId]; channel == nil || channel.Status != common.ChannelStatusEnabled {
					continue
				}
			}
			candidate, exists := logicalCandidates[key]
			if !exists {
				candidate = cached
				candidate.logicalChannelID = identity.LogicalChannelID
				candidate.logicalRevision = identity.Revision
				candidate.logicalMembers = nil
			}
			if seenMembers[key] == nil {
				seenMembers[key] = make(map[int]struct{})
			}
			if _, seen := seenMembers[key][cached.channelId]; !seen {
				seenMembers[key][cached.channelId] = struct{}{}
				candidate.logicalMembers = append(candidate.logicalMembers, channelSmartScheduleLogicalMember{
					channelID: cached.channelId, weight: logicalGroupMemberWeight(group, cached.channelId),
				})
			}
			candidate.logicalPriority = max(candidate.logicalPriority, cached.priority)
			candidate.logicalWeight = max(candidate.logicalWeight, cached.weight)
			if cached.channelId < candidate.channelId {
				candidate.channelId = cached.channelId
			}
			if overlay, hasOverlay := routings[channelLogicalSmartScheduleRouteKey{
				logicalID: identity.LogicalChannelID, revision: identity.Revision,
				group: route.Group, model: channelSmartScheduleModelName(route.Model),
			}]; hasOverlay {
				candidate.logicalPriority = overlay.routing.priority
				candidate.logicalWeight = overlay.routing.weight
				candidate.participates = overlay.state.Participates()
				candidate.stabilityState = overlay.state.StabilityState
				candidate.temporaryTrafficKind = overlay.state.TemporaryTrafficKind
				candidate.temporaryTrafficSince = overlay.state.TemporaryTrafficSince
				candidate.explorationMaxPromptTokens = overlay.state.ExplorationMaxPromptTokens
				candidate.stabilityReleaseMaxPromptTokens = overlay.state.StabilityReleaseMaxPromptTokens
			}
			logicalCandidates[key] = candidate
		}
	}
	for _, route := range routes {
		key := channelSmartScheduleRouteKey(route.ChannelId, route.Group, route.Model)
		view := views[key]
		pool := cachedRoutes[route.Group][route.Model]
		for _, cached := range pool {
			if cached.channelId != route.ChannelId {
				continue
			}
			identity, ok := runtime.Channels[cached.channelId]
			if !ok {
				break
			}
			logical, isLogical := logicalCandidates[logicalKey{identity.LogicalChannelID, identity.Revision, route.Group, channelSmartScheduleModelName(route.Model)}]
			normalizedModel := channelSmartScheduleModelName(route.Model)
			managed := managedModel(route.Group, normalizedModel)
			if managed && isLogical && identity.Revision > 0 {
				view.Priority, view.Weight = logical.logicalPriority, logical.logicalWeight
				view.CandidateChannelId = logical.channelId
				view.LogicalChannelId, view.LogicalRevision = identity.LogicalChannelID, identity.Revision
				memberIDs := make([]int, 0, len(logical.logicalMembers))
				memberWeights := make([]uint, 0, len(logical.logicalMembers))
				for _, member := range logical.logicalMembers {
					memberIDs = append(memberIDs, member.channelID)
					memberWeights = append(memberWeights, member.weight)
				}
				view.LogicalMemberIds, view.LogicalMemberWeights = memberIDs, memberWeights
				view.Participates = cached.participates && logical.participates
				state := route.State
				state.ParticipationSet = cached.participates && logical.participates
				state.Excluded = !state.ParticipationSet
				state.StabilityState = logical.stabilityState
				state.TemporaryTrafficKind, state.TemporaryTrafficSince = logical.temporaryTrafficKind, logical.temporaryTrafficSince
				state.ExplorationMaxPromptTokens, state.StabilityReleaseMaxPromptTokens = logical.explorationMaxPromptTokens, logical.stabilityReleaseMaxPromptTokens
				view.State = &state
			} else {
				view.Priority, view.Weight = channelSmartScheduleCachedRouteRouting(cached, managed)
				view.Participates = cached.participates
				view.TrafficPausedUntil = cached.trafficPausedUntil
			}
			views[key] = view
			break
		}
	}
}

// loadLogicalSmartScheduleRouteOverlaysWithDB is the context-aware read path
// used by monitoring. The scheduler's existing helper remains unchanged for
// callers that use the process-global database handle.
func loadLogicalSmartScheduleRouteOverlaysWithDB(
	db *gorm.DB,
	logicalIDs []int64,
	groupName string,
	modelName string,
) (map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay, error) {
	result := make(map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay)
	if db == nil || len(logicalIDs) == 0 || !IsLogicalChannelGroupingEnabled() ||
		!db.Migrator().HasTable(&ChannelLogicalSmartScheduleRouteState{}) {
		return result, nil
	}
	var rows []ChannelLogicalSmartScheduleRouteState
	query := db.Where("logical_group_id IN ?", logicalIDs)
	if groupName != "" {
		query = query.Where("group_name = ?", groupName)
	}
	if modelName != "" {
		query = query.Where("model_name = ?", channelSmartScheduleModelName(modelName))
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return logicalSmartScheduleRouteOverlaysFromStates(rows)
}
