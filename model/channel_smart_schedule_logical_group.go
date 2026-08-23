package model

import "sort"

// channelSmartScheduleLogicalMember is the transient member projection kept
// on a coalesced route. It deliberately contains only channel IDs and the
// configured logical-group weight; no credentials or physical billing data
// are copied into the shared scheduling candidate.
type channelSmartScheduleLogicalMember struct {
	channelID int
	weight    uint
}

// coalesceChannelSmartScheduleLogicalRoutes collapses physical route entries
// belonging to one enabled logical group into one scheduling candidate. The
// persisted ChannelSmartScheduleRouteState rows remain physical-channel
// rows; this projection exists only for candidate selection and therefore
// cannot create duplicate group candidates or rewrite ordinary monitoring.
//
// The caller normally holds channelSyncLock.RLock while passing the published
// runtime snapshot. A nil/dirty snapshot is intentionally treated as the
// compatibility path (physical routes), so a cache refresh failure never
// sends a request through a partial relation.
func coalesceChannelSmartScheduleLogicalRoutes(
	routes []channelSmartScheduleCachedRoute,
	runtime *LogicalChannelRuntimeSnapshot,
) []channelSmartScheduleCachedRoute {
	return coalesceChannelSmartScheduleLogicalRoutesWithRouting(routes, runtime, "", "", nil)
}

func coalesceChannelSmartScheduleLogicalRoutesWithRouting(
	routes []channelSmartScheduleCachedRoute,
	runtime *LogicalChannelRuntimeSnapshot,
	groupName string,
	modelName string,
	routingByLogicalKey map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay,
) []channelSmartScheduleCachedRoute {
	if len(routes) == 0 || runtime == nil || !IsLogicalChannelGroupingEnabled() {
		return routes
	}

	result := make([]channelSmartScheduleCachedRoute, 0, len(routes))
	groupIndex := make(map[int64]int)
	for _, route := range routes {
		identity, ok := runtime.Channels[route.channelId]
		if !ok || identity.Revision <= 0 || identity.LogicalChannelID == int64(route.channelId) {
			result = append(result, route)
			continue
		}
		group, ok := runtime.Groups[identity.LogicalChannelID]
		if !ok || group.Status != ChannelLogicalGroupStatusEnabled || len(group.Members) < 2 {
			result = append(result, route)
			continue
		}

		index, exists := groupIndex[identity.LogicalChannelID]
		if !exists {
			index = len(result)
			groupIndex[identity.LogicalChannelID] = index
			logicalRoute := route
			logicalRoute.logicalChannelID = identity.LogicalChannelID
			logicalRoute.logicalRevision = identity.Revision
			logicalRoute.logicalPriority = route.priority
			logicalRoute.logicalWeight = route.weight
			logicalRoute.logicalOfficialPriority = route.officialPriority
			logicalRoute.logicalOfficialWeight = route.officialWeight
			logicalRoute.logicalMembers = []channelSmartScheduleLogicalMember{{
				channelID: route.channelId,
				weight:    logicalGroupMemberWeight(group, route.channelId),
			}}
			result = append(result, logicalRoute)
			continue
		}

		logicalRoute := &result[index]
		logicalRoute.logicalMembers = append(logicalRoute.logicalMembers, channelSmartScheduleLogicalMember{
			channelID: route.channelId,
			weight:    logicalGroupMemberWeight(group, route.channelId),
		})
		// A logical group must occupy one priority/weight slot in the
		// candidate pool. Taking the best observed route preserves the
		// existing priority semantics without multiplying traffic by the
		// number of keys in the group.
		if route.priority > logicalRoute.logicalPriority ||
			(route.priority == logicalRoute.logicalPriority && route.channelId < logicalRoute.channelId) {
			logicalRoute.logicalPriority = route.priority
		}
		if route.weight > logicalRoute.logicalWeight {
			logicalRoute.logicalWeight = route.weight
		}
		if route.officialPriority > logicalRoute.logicalOfficialPriority ||
			(route.officialPriority == logicalRoute.logicalOfficialPriority && route.channelId < logicalRoute.channelId) {
			logicalRoute.logicalOfficialPriority = route.officialPriority
		}
		if route.officialWeight > logicalRoute.logicalOfficialWeight {
			logicalRoute.logicalOfficialWeight = route.officialWeight
		}
		if route.channelId < logicalRoute.channelId {
			logicalRoute.channelId = route.channelId
		}
	}

	for index := range result {
		if result[index].logicalChannelID == 0 {
			continue
		}
		sort.Slice(result[index].logicalMembers, func(i, j int) bool {
			return result[index].logicalMembers[i].channelID < result[index].logicalMembers[j].channelID
		})
		if overlay, exists := routingByLogicalKey[channelLogicalSmartScheduleRouteKey{
			logicalID: result[index].logicalChannelID, revision: result[index].logicalRevision,
			group: groupName, model: channelSmartScheduleModelName(modelName),
		}]; exists {
			result[index].logicalPriority = overlay.routing.priority
			result[index].logicalWeight = overlay.routing.weight
			result[index].participates = overlay.state.Participates()
			result[index].temporaryTrafficKind = overlay.state.TemporaryTrafficKind
			result[index].temporaryTrafficSince = overlay.state.TemporaryTrafficSince
			result[index].stabilityState = overlay.state.StabilityState
			result[index].stabilitySince = overlay.state.StabilitySince
			result[index].explorationMaxPromptTokens = overlay.state.ExplorationMaxPromptTokens
			result[index].stabilityReleaseMaxPromptTokens = overlay.state.StabilityReleaseMaxPromptTokens
		}
	}
	return result
}

func logicalGroupMemberWeight(group LogicalChannelGroupSnapshot, channelID int) uint {
	for _, member := range group.Members {
		if member.ChannelID == channelID {
			return member.Weight
		}
	}
	return 0
}

// channelSmartScheduleDatabaseRoutes adapts the already-filtered database
// abilities to the same transient logical candidate representation used by
// the memory-cache selector. It intentionally runs after physical status,
// path/model, exclusion, pause, and request-limit filters.
func channelSmartScheduleDatabaseRoutes(
	abilities []Ability,
	channelByID map[int]*Channel,
	group string,
	modelName string,
	trafficPolicy *channelSmartScheduleTrafficPolicy,
) ([]channelSmartScheduleCachedRoute, *LogicalChannelRuntimeSnapshot, error) {
	routes := make([]channelSmartScheduleCachedRoute, 0, len(abilities))
	channelIDs := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		channelIDs = append(channelIDs, ability.ChannelId)
	}
	statesByChannelID := make(map[int]ChannelSmartScheduleRouteState, len(channelIDs))
	if DB != nil && DB.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
		var states []ChannelSmartScheduleRouteState
		if err := DB.Where(
			"group_name = ? AND model_name = ? AND channel_id IN ?", group, modelName, channelIDs,
		).Find(&states).Error; err != nil {
			return nil, nil, err
		}
		for _, state := range states {
			statesByChannelID[state.ChannelId] = state
		}
	}
	for _, ability := range abilities {
		priority, weight := channelRoutingForTrafficPolicy(
			ability, channelByID[ability.ChannelId], group, modelName, trafficPolicy,
		)
		state := statesByChannelID[ability.ChannelId]
		routes = append(routes, channelSmartScheduleCachedRoute{
			channelId: ability.ChannelId, priority: priority, weight: weight,
			participates:                    state.Participates(),
			temporaryTrafficKind:            state.TemporaryTrafficKind,
			temporaryTrafficSince:           state.TemporaryTrafficSince,
			stabilityState:                  state.StabilityState,
			stabilitySince:                  state.StabilitySince,
			explorationMaxPromptTokens:      state.ExplorationMaxPromptTokens,
			stabilityReleaseMaxPromptTokens: state.StabilityReleaseMaxPromptTokens,
		})
	}
	if trafficPolicy == nil || !trafficPolicy.managesPool(group, modelName) ||
		!IsLogicalChannelGroupingEnabled() {
		return routes, nil, nil
	}
	runtime, err := loadChannelSmartScheduleLogicalRuntime(channelIDs)
	if err != nil {
		return nil, nil, err
	}
	if runtime == nil {
		return routes, nil, nil
	}
	if !IsLogicalChannelGroupingEnabled() {
		return routes, runtime, nil
	}
	logicalIDs := make([]int64, 0, len(runtime.Groups))
	for logicalID, logicalGroup := range runtime.Groups {
		if IsLogicalChannelGroupActive(logicalGroup.Status) {
			logicalIDs = append(logicalIDs, logicalID)
		}
	}
	routings, err := loadLogicalSmartScheduleRouteOverlays(logicalIDs, group, modelName)
	if err != nil {
		return nil, nil, err
	}
	return coalesceChannelSmartScheduleLogicalRoutesWithRouting(
		routes, runtime, group, modelName, routings,
	), runtime, nil
}

// loadChannelSmartScheduleLogicalRuntime loads only the logical groups touched
// by one database-selection pool. Memory-cache-disabled deployments therefore
// do not rebuild the global channel relation on every request.
func loadChannelSmartScheduleLogicalRuntime(channelIDs []int) (*LogicalChannelRuntimeSnapshot, error) {
	if DB == nil || len(channelIDs) == 0 ||
		!DB.Migrator().HasTable(&ChannelLogicalGroup{}) ||
		!DB.Migrator().HasTable(&ChannelLogicalGroupMember{}) ||
		!DB.Migrator().HasColumn(&Channel{}, "logical_channel_id") {
		return nil, nil
	}
	type channelIdentityRow struct {
		ID               int
		LogicalChannelID *int64
	}
	var rows []channelIdentityRow
	if err := DB.Model(&Channel{}).Select("id", "logical_channel_id").
		Where("id IN ?", channelIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	runtime := &LogicalChannelRuntimeSnapshot{
		Channels: make(map[int]LogicalChannelIdentity, len(rows)),
		Groups:   make(map[int64]LogicalChannelGroupSnapshot),
	}
	logicalIDs := make([]int64, 0)
	seenLogicalIDs := make(map[int64]struct{})
	for _, row := range rows {
		identity := LogicalChannelIdentity{ChannelID: row.ID, LogicalChannelID: int64(row.ID)}
		if row.LogicalChannelID != nil && *row.LogicalChannelID > 0 {
			identity.LogicalChannelID = *row.LogicalChannelID
			if _, exists := seenLogicalIDs[*row.LogicalChannelID]; !exists {
				seenLogicalIDs[*row.LogicalChannelID] = struct{}{}
				logicalIDs = append(logicalIDs, *row.LogicalChannelID)
			}
		}
		runtime.Channels[row.ID] = identity
	}
	if len(logicalIDs) == 0 {
		return runtime, nil
	}
	var groups []ChannelLogicalGroup
	if err := DB.Select("id", "status", "revision").
		Where("id IN ?", logicalIDs).Find(&groups).Error; err != nil {
		return nil, err
	}
	for _, group := range groups {
		runtime.Groups[group.Id] = LogicalChannelGroupSnapshot{
			LogicalChannelID: group.Id, Revision: group.Revision, Status: group.Status,
		}
	}
	var members []ChannelLogicalGroupMember
	if err := DB.Select("logical_group_id", "channel_id", "weight", "address_fingerprint").
		Where("logical_group_id IN ?", logicalIDs).Order("logical_group_id ASC, channel_id ASC").
		Find(&members).Error; err != nil {
		return nil, err
	}
	for _, member := range members {
		group, exists := runtime.Groups[member.LogicalGroupID]
		if !exists {
			continue
		}
		group.Members = append(group.Members, LogicalChannelMemberSnapshot{
			ChannelID: member.ChannelID, Weight: member.Weight,
			AddressFingerprint: member.AddressFingerprint,
		})
		runtime.Groups[member.LogicalGroupID] = group
	}
	for channelID, identity := range runtime.Channels {
		if identity.LogicalChannelID == int64(channelID) {
			continue
		}
		if group, exists := runtime.Groups[identity.LogicalChannelID]; exists {
			identity.Revision = group.Revision
			runtime.Channels[channelID] = identity
		}
	}
	return runtime, nil
}

// selectLogicalSmartScheduleMember applies the shared member selector after
// a logical route has won the normal priority/Ability.Weight competition.
// Availability is reconstructed from the already-filtered physical routes,
// so request-path/model/exclusion filters remain per-Key while the scoring
// decision remains one logical candidate.
func selectLogicalSmartScheduleMemberID(
	route channelSmartScheduleCachedRoute,
	runtime *LogicalChannelRuntimeSnapshot,
) (int, error) {
	if route.logicalChannelID <= 0 || len(route.logicalMembers) == 0 {
		return route.channelId, nil
	}
	identity := LogicalChannelIdentity{
		ChannelID:        route.channelId,
		LogicalChannelID: route.logicalChannelID,
		Revision:         route.logicalRevision,
	}
	var snapshot LogicalChannelSelectionSnapshot
	if runtime != nil {
		group, ok := runtime.Groups[route.logicalChannelID]
		if !ok {
			return 0, ErrLogicalChannelRuntimeGroupNotFound
		}
		snapshot = group
		snapshot.Members = append([]LogicalChannelMemberSnapshot(nil), group.Members...)
	} else {
		var err error
		snapshot, err = GetLogicalChannelSelectionSnapshot(identity)
		if err != nil {
			return 0, err
		}
	}
	available := make(map[int]struct{}, len(route.logicalMembers))
	for _, member := range route.logicalMembers {
		available[member.channelID] = struct{}{}
	}
	availability := make([]LogicalChannelMemberAvailability, 0, len(snapshot.Members))
	for _, member := range snapshot.Members {
		_, ok := available[member.ChannelID]
		availability = append(availability, LogicalChannelMemberAvailability{
			ChannelID: member.ChannelID,
			Weight:    member.Weight,
			Available: ok,
		})
	}
	channelID, err := SelectLogicalChannelMember(snapshot, availability, nil)
	if err != nil {
		return 0, err
	}
	return channelID, nil
}
