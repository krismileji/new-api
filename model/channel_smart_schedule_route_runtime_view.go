package model

import (
	"context"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Runtime views retain configuration/history separately from the fields that
// actually control selection. Every routing field comes from one published pool.
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
	Enabled              *bool
	ChannelStatus        *int
	RateLimitCoolingDown *bool
}

func GetChannelSmartScheduleRouteRuntimeViewsWithContext(
	ctx context.Context, routes []ChannelSmartScheduleRoute,
	cooldownOptions ...map[string][]int,
) (map[ChannelSmartScheduleRouteKey]ChannelSmartScheduleRouteRuntimeView, error) {
	views := make(map[ChannelSmartScheduleRouteKey]ChannelSmartScheduleRouteRuntimeView, len(routes))
	for _, route := range routes {
		state := route.State
		views[channelSmartScheduleRouteKey(route.ChannelId, route.Group, route.Model)] = ChannelSmartScheduleRouteRuntimeView{
			Priority: route.Priority, Weight: route.Weight, CandidateChannelId: route.ChannelId,
			Participates: state.Participates(), TrafficPausedUntil: route.TrafficPausedUntil, State: &state,
		}
	}
	if len(routes) == 0 {
		return views, nil
	}
	policy := currentChannelSmartScheduleTrafficPolicy()
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		// Pool maps can be replaced by a targeted refresh. Keep the lock for the
		// entire projection instead of retaining map references after unlocking.
		applyChannelSmartScheduleCachedRuntimeViews(views, routes, channelSmartScheduleRouteCache,
			channelsIDM, logicalChannelRuntimeCache, channelLogicalSmartScheduleRoutingCache, policy, cooldownOptions...)
		return views, nil
	}
	if DB == nil {
		return views, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	db := DB.WithContext(ctx)
	runtime, err := buildLogicalChannelRuntimeSnapshot(db)
	if err != nil {
		return nil, err
	}
	logicalIDs := make([]int64, 0, len(runtime.Groups))
	for id := range runtime.Groups {
		logicalIDs = append(logicalIDs, id)
	}
	overlays, err := loadLogicalSmartScheduleRouteOverlaysWithDB(db, logicalIDs, "", "")
	if err != nil {
		return nil, err
	}
	cache := make(map[string]map[string][]channelSmartScheduleCachedRoute)
	channels := make(map[int]*Channel, len(routes))
	for _, route := range routes {
		channels[route.ChannelId] = &Channel{Id: route.ChannelId, Status: route.ChannelStatus}
		if !route.Enabled {
			continue
		}
		if cache[route.Group] == nil {
			cache[route.Group] = make(map[string][]channelSmartScheduleCachedRoute)
		}
		cache[route.Group][route.Model] = append(cache[route.Group][route.Model], channelSmartScheduleCachedRoute{
			channelId: route.ChannelId, priority: route.Priority, weight: route.Weight,
			participates: route.State.Participates(), trafficPausedUntil: route.TrafficPausedUntil,
			stabilityState: route.State.StabilityState, stabilitySince: route.State.StabilitySince,
			temporaryTrafficKind: route.State.TemporaryTrafficKind, temporaryTrafficSince: route.State.TemporaryTrafficSince,
			explorationMaxPromptTokens:      route.State.ExplorationMaxPromptTokens,
			stabilityReleaseMaxPromptTokens: route.State.StabilityReleaseMaxPromptTokens,
		})
	}
	applyChannelSmartScheduleCachedRuntimeViews(views, routes, cache, channels, runtime, overlays, policy, cooldownOptions...)
	return views, nil
}

func applyChannelSmartScheduleCachedRuntimeViews(
	views map[ChannelSmartScheduleRouteKey]ChannelSmartScheduleRouteRuntimeView,
	routes []ChannelSmartScheduleRoute,
	cachedRoutes map[string]map[string][]channelSmartScheduleCachedRoute,
	cachedChannels map[int]*Channel,
	runtime *LogicalChannelRuntimeSnapshot,
	routings map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay,
	policy *channelSmartScheduleTrafficPolicy,
	cooldownOptions ...map[string][]int,
) {
	now := common.GetTimestamp()
	var cooldowns map[string][]int
	if len(cooldownOptions) > 0 {
		cooldowns = cooldownOptions[0]
	}
	pools := make(map[channelSmartScheduleRoutePool]bool)
	for _, row := range routes {
		pools[channelSmartScheduleRoutePool{group: row.Group, model: row.Model}] = true
	}
	physical := make(map[ChannelSmartScheduleRouteKey]channelSmartScheduleCachedRoute)
	candidates := make(map[ChannelSmartScheduleRouteKey]channelSmartScheduleCachedRoute)
	for pool := range pools {
		managed := policy != nil && policy.managesPool(pool.group, pool.model)
		coolingIDs := make(map[int]bool, len(cooldowns[pool.model]))
		for _, id := range cooldowns[pool.model] {
			coolingIDs[id] = true
		}
		available := make([]channelSmartScheduleCachedRoute, 0)
		for _, cached := range cachedRoutes[pool.group][pool.model] {
			key := channelSmartScheduleRouteKey(cached.channelId, pool.group, pool.model)
			physical[key] = cached
			if cooldowns != nil {
				view := views[key]
				cooling := coolingIDs[cached.channelId]
				view.RateLimitCoolingDown = &cooling
				views[key] = view
			}
			if managed && !cached.participates || cached.trafficPausedUntil > now {
				continue
			}
			if cachedChannels != nil {
				channel := cachedChannels[cached.channelId]
				if channel == nil || channel.Status != common.ChannelStatusEnabled {
					continue
				}
			}
			available = append(available, cached)
		}
		if len(coolingIDs) > 0 {
			outsideCooldown := make([]channelSmartScheduleCachedRoute, 0, len(available))
			for _, route := range available {
				if !coolingIDs[route.channelId] {
					outsideCooldown = append(outsideCooldown, route)
				}
			}
			if managed {
				outsideCooldown = coalesceChannelSmartScheduleLogicalRoutesWithRouting(outsideCooldown, runtime, pool.group, pool.model, routings)
			}
			outsideCooldown = filterChannelSmartScheduleParticipatingCachedRoutes(outsideCooldown, pool.group, pool.model, policy)
			outsideCooldown = filterChannelSmartScheduleStableCachedRoutes(outsideCooldown, pool.group, pool.model, policy, false)
			if len(outsideCooldown) > 0 {
				available = outsideCooldown
			} else if managed {
				available = coalesceChannelSmartScheduleLogicalRoutesWithRouting(available, runtime, pool.group, pool.model, routings)
			}
		} else if managed {
			available = coalesceChannelSmartScheduleLogicalRoutesWithRouting(available, runtime, pool.group, pool.model, routings)
		}
		for _, candidate := range available {
			if len(candidate.logicalMembers) == 0 {
				candidates[channelSmartScheduleRouteKey(candidate.channelId, pool.group, pool.model)] = candidate
				continue
			}
			for _, member := range candidate.logicalMembers {
				candidates[channelSmartScheduleRouteKey(member.channelID, pool.group, pool.model)] = candidate
			}
		}
	}
	for _, row := range routes {
		key := channelSmartScheduleRouteKey(row.ChannelId, row.Group, row.Model)
		view := views[key]
		cached, exists := physical[key]
		managed := policy != nil && policy.managesPool(row.Group, row.Model)
		enabled := exists
		status := row.ChannelStatus
		if cachedChannels != nil {
			if channel := cachedChannels[row.ChannelId]; channel != nil {
				status = channel.Status
			} else {
				enabled = false
			}
		}
		view.Enabled, view.ChannelStatus = &enabled, &status
		view.TrafficPausedUntil = cached.trafficPausedUntil
		view.Priority, view.Weight = channelSmartScheduleCachedRouteRouting(cached, managed)
		view.Participates = exists && cached.participates
		if candidate, ok := candidates[key]; ok {
			view.Priority, view.Weight = channelSmartScheduleCachedRouteRouting(candidate, managed)
			view.Participates = cached.participates && candidate.participates
			view.CandidateChannelId = candidate.channelId
			view.LogicalChannelId, view.LogicalRevision = candidate.logicalChannelID, candidate.logicalRevision
			for _, member := range candidate.logicalMembers {
				view.LogicalMemberIds = append(view.LogicalMemberIds, member.channelID)
				view.LogicalMemberWeights = append(view.LogicalMemberWeights, member.weight)
			}
			cached = candidate
		}
		// Historical scores and administrator configuration retain their database
		// values. Selection-affecting state always comes from the published cache,
		// including explicit empty values after protection has been removed.
		state := row.State
		state.ParticipationSet, state.Excluded = view.Participates, !view.Participates
		state.StabilityState, state.StabilitySince = cached.stabilityState, cached.stabilitySince
		state.TemporaryTrafficKind, state.TemporaryTrafficSince = cached.temporaryTrafficKind, cached.temporaryTrafficSince
		state.ExplorationMaxPromptTokens = cached.explorationMaxPromptTokens
		state.StabilityReleaseMaxPromptTokens = cached.stabilityReleaseMaxPromptTokens
		view.State = &state
		views[key] = view
	}
}

// The row inventory, effective fields and version are projected under one lock.
// A route removed in SQL may still serve requests until the next publication.
func GetChannelSmartScheduleMonitorRuntimeSnapshot(ctx context.Context, routes []ChannelSmartScheduleRoute, cooldownOptions ...map[string][]int) (
	[]ChannelSmartScheduleRoute, map[ChannelSmartScheduleRouteKey]ChannelSmartScheduleRouteRuntimeView,
	ChannelSmartScheduleRouteSnapshotStatus, error,
) {
	if !common.MemoryCacheEnabled {
		views, err := GetChannelSmartScheduleRouteRuntimeViewsWithContext(ctx, routes, cooldownOptions...)
		return routes, views, ChannelSmartScheduleRouteSnapshotStatus{
			Available: err == nil, GeneratedAt: common.GetTimestamp(),
			MaxAgeSeconds: int64(channelSmartScheduleRouteSnapshotMaxAgeDuration() / time.Second),
		}, err
	}
	policy := currentChannelSmartScheduleTrafficPolicy()
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	rows := append([]ChannelSmartScheduleRoute(nil), routes...)
	views := make(map[ChannelSmartScheduleRouteKey]ChannelSmartScheduleRouteRuntimeView, len(rows))
	seen := make(map[ChannelSmartScheduleRouteKey]bool, len(rows))
	for _, row := range rows {
		seen[channelSmartScheduleRouteKey(row.ChannelId, row.Group, row.Model)] = true
	}
	for group, models := range channelSmartScheduleRouteCache {
		for modelName, cached := range models {
			for _, route := range cached {
				key := channelSmartScheduleRouteKey(route.channelId, group, modelName)
				channel := channelsIDM[route.channelId]
				if seen[key] || channel == nil {
					continue
				}
				seen[key] = true
				rows = append(rows, ChannelSmartScheduleRoute{
					ChannelId: channel.Id, ChannelName: channel.Name, ChannelStatus: channel.Status,
					Group: group, Model: modelName,
					State: ChannelSmartScheduleRouteState{ChannelId: channel.Id, GroupName: group, ModelName: modelName},
				})
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Group != rows[j].Group {
			return rows[i].Group < rows[j].Group
		}
		if rows[i].Model != rows[j].Model {
			return rows[i].Model < rows[j].Model
		}
		return rows[i].ChannelId < rows[j].ChannelId
	})
	for _, row := range rows {
		views[channelSmartScheduleRouteKey(row.ChannelId, row.Group, row.Model)] = ChannelSmartScheduleRouteRuntimeView{
			CandidateChannelId: row.ChannelId,
		}
	}
	applyChannelSmartScheduleCachedRuntimeViews(views, rows, channelSmartScheduleRouteCache,
		channelsIDM, logicalChannelRuntimeCache, channelLogicalSmartScheduleRoutingCache, policy, cooldownOptions...)
	return rows, views, channelSmartScheduleRouteSnapshotStatusLocked(), nil
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
