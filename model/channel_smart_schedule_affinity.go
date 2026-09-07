package model

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
)

type ChannelSmartScheduleAffinityStatus int

const (
	ChannelSmartScheduleAffinityInvalid ChannelSmartScheduleAffinityStatus = iota
	ChannelSmartScheduleAffinityEligible
	ChannelSmartScheduleAffinityTemporarilyUnavailable
)

// ChannelSmartScheduleAffinityEligibility applies the strict participation
// gate to managed pools and preserves official affinity for other pools.
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
	if trafficPolicy == nil || !trafficPolicy.managesAnyPool(group, modelNames) {
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
	if common.MemoryCacheEnabled && channelSmartScheduleRefreshWorkerIsStarted() &&
		!channelSmartScheduleRouteSnapshotUsable(time.Now()) {
		return ChannelSmartScheduleAffinityTemporarilyUnavailable
	}
	if !common.MemoryCacheEnabled {
		return channelSmartScheduleAffinityEligibilityFromDatabase(
			group, requestModelName, modelNames, channelId, requestPath, selectionOptions, trafficPolicy,
		)
	}
	return channelSmartScheduleAffinityEligibilityFromCache(
		group, requestModelName, modelNames, channelId, requestPath, selectionOptions, trafficPolicy,
	)
}

// ChannelSmartScheduleAffinityCandidateEligibilityExcluding checks the
// logical candidate represented by a cached physical member. Exclusions stay
// physical so one Key's cooldown does not invalidate its sibling Keys.
func ChannelSmartScheduleAffinityCandidateEligibilityExcluding(
	group string,
	modelName string,
	preferredChannelID int,
	requestPath string,
	excludedChannelIDs map[int]struct{},
	options ...ChannelSelectionOptions,
) ChannelSmartScheduleAffinityStatus {
	selectionOptions := channelSelectionOptions(options)
	selectionOptions.ExcludedChannelIds = append([]int(nil), selectionOptions.ExcludedChannelIds...)
	for id := range excludedChannelIDs {
		selectionOptions.ExcludedChannelIds = append(selectionOptions.ExcludedChannelIds, id)
	}
	options = []ChannelSelectionOptions{selectionOptions}
	identity, err := ResolveChannelLogicalIdentity(preferredChannelID)
	if err != nil {
		return ChannelSmartScheduleAffinityInvalid
	}
	trafficPolicy := currentChannelSmartScheduleTrafficPolicy()
	if identity.Revision == 0 || identity.LogicalChannelID == int64(preferredChannelID) ||
		trafficPolicy == nil || !trafficPolicy.managesAnyPool(group, channelSmartScheduleRouteModelNames(modelName)) {
		if _, excluded := excludedChannelIDs[preferredChannelID]; excluded {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		return ChannelSmartScheduleAffinityEligibility(
			group, modelName, preferredChannelID, requestPath, options...,
		)
	}
	snapshot, err := GetLogicalChannelSelectionSnapshot(identity)
	if err != nil {
		return ChannelSmartScheduleAffinityTemporarilyUnavailable
	}
	temporarilyUnavailable := false
	for _, member := range snapshot.Members {
		if _, excluded := excludedChannelIDs[member.ChannelID]; excluded {
			temporarilyUnavailable = true
			continue
		}
		status := ChannelSmartScheduleAffinityEligibility(
			group, modelName, member.ChannelID, requestPath, options...,
		)
		if status == ChannelSmartScheduleAffinityEligible {
			return ChannelSmartScheduleAffinityEligible
		}
		if status == ChannelSmartScheduleAffinityTemporarilyUnavailable {
			temporarilyUnavailable = true
		}
	}
	if temporarilyUnavailable {
		return ChannelSmartScheduleAffinityTemporarilyUnavailable
	}
	return ChannelSmartScheduleAffinityInvalid
}

// SelectChannelSmartScheduleAffinityMember keeps affinity at the logical
// candidate boundary. A cached physical channel identifies the logical group,
// but the actual Key is selected again by current member availability and the
// configured logical member weight. Explicit specific_channel_id routing does
// not call this function and therefore remains physically pinned.
func SelectChannelSmartScheduleAffinityMember(
	group string,
	modelName string,
	preferredChannelID int,
	requestPath string,
	options ...ChannelSelectionOptions,
) (*Channel, error) {
	return SelectChannelSmartScheduleAffinityMemberExcluding(
		group, modelName, preferredChannelID, requestPath, nil, options...,
	)
}

// SelectChannelSmartScheduleAffinityMemberExcluding reselects the physical
// Key inside one affinity-pinned logical candidate while preserving member
// weights. Exclusions are physical-channel conditions such as 429 cooldowns.
func SelectChannelSmartScheduleAffinityMemberExcluding(
	group string,
	modelName string,
	preferredChannelID int,
	requestPath string,
	excludedChannelIDs map[int]struct{},
	options ...ChannelSelectionOptions,
) (*Channel, error) {
	trafficPolicy := currentChannelSmartScheduleTrafficPolicy()
	managed := trafficPolicy != nil && trafficPolicy.managesAnyPool(group, channelSmartScheduleRouteModelNames(modelName))
	if common.MemoryCacheEnabled && managed {
		if channelSmartScheduleRefreshWorkerIsStarted() && !channelSmartScheduleRouteSnapshotUsable(time.Now()) {
			return nil, ErrChannelSmartScheduleRouteSnapshotUnavailable
		}
		selectionOptions := channelSelectionOptions(options)
		selectionOptions.ExcludedChannelIds = append([]int(nil), selectionOptions.ExcludedChannelIds...)
		for id := range excludedChannelIDs {
			selectionOptions.ExcludedChannelIds = append(selectionOptions.ExcludedChannelIds, id)
		}
		selectionOptions.Filters = append([]dto.ChannelFilter(nil), selectionOptions.Filters...)
		if requestPath != "" {
			selectionOptions.Filters = append(selectionOptions.Filters, dto.ChannelFilter{Kind: dto.FilterRequestPath, RequestPath: requestPath})
		}
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		if IsLogicalChannelGroupingEnabled() && logicalChannelRuntimeCache == nil &&
			channelSmartScheduleRouteNeedsRuntime(group, channelSmartScheduleRouteModelNames(modelName)) {
			return nil, ErrLogicalChannelRuntimeUnavailable
		}
		preferredLogicalID := int64(0)
		if IsLogicalChannelGroupingEnabled() && logicalChannelRuntimeCache != nil {
			identity := logicalChannelRuntimeCache.Channels[preferredChannelID]
			if identity.Revision > 0 {
				preferredLogicalID = identity.LogicalChannelID
			}
		}
		for _, poolModel := range channelSmartScheduleRouteModelNames(modelName) {
			poolManaged := trafficPolicy.managesPool(group, poolModel)
			routes := prepareChannelSmartScheduleCachedRoutes(channelSmartScheduleRouteCache[group][poolModel],
				group, poolModel, modelName, requestPath, selectionOptions, trafficPolicy, false, poolManaged)
			if len(routes) == 0 {
				continue
			}
			for _, route := range routes {
				matches := route.logicalChannelID == 0 && route.channelId == preferredChannelID ||
					preferredLogicalID > 0 && route.logicalChannelID == preferredLogicalID
				if !matches {
					continue
				}
				memberID, err := selectLogicalSmartScheduleMemberID(route, logicalChannelRuntimeCache)
				if err != nil {
					return nil, err
				}
				if channelSmartScheduleCandidateAffinityStatus(routes, memberID, poolManaged) != ChannelSmartScheduleAffinityEligible {
					return nil, ErrLogicalChannelSelectionNoAvailableMembers
				}
				recordChannelRoutingDecision(selectionOptions, route, memberID, group, poolModel, "affinity", true)
				return channelsIDM[memberID], nil
			}
			break
		}
		return nil, ErrLogicalChannelSelectionNoAvailableMembers
	}
	if ChannelSmartScheduleAffinityCandidateEligibilityExcluding(
		group, modelName, preferredChannelID, requestPath, excludedChannelIDs, options...,
	) != ChannelSmartScheduleAffinityEligible {
		return nil, ErrLogicalChannelSelectionNoAvailableMembers
	}
	identity, err := ResolveChannelLogicalIdentity(preferredChannelID)
	if err != nil {
		return nil, err
	}
	if identity.Revision == 0 || identity.LogicalChannelID == int64(preferredChannelID) ||
		trafficPolicy == nil || !trafficPolicy.managesAnyPool(group, channelSmartScheduleRouteModelNames(modelName)) {
		if _, excluded := excludedChannelIDs[preferredChannelID]; excluded {
			return nil, ErrLogicalChannelSelectionNoAvailableMembers
		}
		recordChannelRoutingDecision(channelSelectionOptions(options), channelSmartScheduleCachedRoute{channelId: preferredChannelID},
			preferredChannelID, group, modelName, "affinity", false)
		return CacheGetChannel(preferredChannelID)
	}
	snapshot, err := GetLogicalChannelSelectionSnapshot(identity)
	if err != nil {
		return nil, err
	}
	selectionOptions := channelSelectionOptions(options)
	selectionOptions.ExcludedChannelIds = append([]int(nil), selectionOptions.ExcludedChannelIds...)
	for id := range excludedChannelIDs {
		selectionOptions.ExcludedChannelIds = append(selectionOptions.ExcludedChannelIds, id)
	}
	availability := make([]LogicalChannelMemberAvailability, 0, len(snapshot.Members))
	for _, member := range snapshot.Members {
		status := ChannelSmartScheduleAffinityEligibility(
			group, modelName, member.ChannelID, requestPath, selectionOptions,
		)
		_, excluded := excludedChannelIDs[member.ChannelID]
		availability = append(availability, LogicalChannelMemberAvailability{
			ChannelID: member.ChannelID, Weight: member.Weight,
			Available: status == ChannelSmartScheduleAffinityEligible && !excluded,
		})
	}
	channelID, err := SelectLogicalChannelMember(snapshot, availability, nil)
	if err != nil {
		return nil, err
	}
	candidateID := channelID
	for _, member := range availability {
		if member.Available && member.ChannelID < candidateID {
			candidateID = member.ChannelID
		}
	}
	recordChannelRoutingDecision(selectionOptions, channelSmartScheduleCachedRoute{channelId: candidateID,
		logicalChannelID: identity.LogicalChannelID, logicalRevision: identity.Revision},
		channelID, group, modelName, "affinity", false)
	return CacheGetChannel(channelID)
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
	knownRoute := false
	selectionOptions.Filters = append([]dto.ChannelFilter(nil), selectionOptions.Filters...)
	if requestPath != "" {
		selectionOptions.Filters = append(selectionOptions.Filters, dto.ChannelFilter{Kind: dto.FilterRequestPath, RequestPath: requestPath})
	}
	for _, candidateModel := range modelNames {
		managedPool := trafficPolicy.managesPool(group, candidateModel)
		var abilities []Ability
		if err := DB.Where(&Ability{Group: group, Model: candidateModel, Enabled: true}).Find(&abilities).Error; err != nil {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		var err error
		abilities, err = filterChannelSmartScheduleParticipatingAbilities(abilities, group, candidateModel, trafficPolicy)
		if err != nil {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		if len(abilities) == 0 {
			continue
		}
		knownRoute = knownRoute || containsAbilityChannel(abilities, channelId)
		channelIDs := make([]int, 0, len(abilities))
		for _, ability := range abilities {
			channelIDs = append(channelIDs, ability.ChannelId)
		}
		paused, err := loadActiveChannelSmartSchedulePausedChannelIds(DB, group, candidateModel, channelIDs, common.GetTimestamp())
		if err != nil {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		var channels []Channel
		if err := DB.Where("id IN ? AND status = ?", channelIDs, common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		channelByID := make(map[int]*Channel, len(channels))
		for index := range channels {
			channel := &channels[index]
			if matches, _ := ChannelSatisfiesFilters(channel, requestModelName, selectionOptions.Filters); matches {
				channelByID[channel.Id] = channel
			}
		}
		allowedIDs := filterChannelIDsBySelectionOptions(channelIDs, selectionOptions)
		allowed := make(map[int]bool, len(allowedIDs))
		for _, id := range allowedIDs {
			allowed[id] = true
		}
		available := make([]Ability, 0, len(abilities))
		for _, ability := range abilities {
			_, isPaused := paused[ability.ChannelId]
			if allowed[ability.ChannelId] && !isPaused && channelByID[ability.ChannelId] != nil {
				available = append(available, ability)
			}
		}
		routes, _, err := channelSmartScheduleDatabaseRoutes(available, channelByID, group, candidateModel, trafficPolicy)
		if err != nil {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		routes = filterChannelSmartScheduleParticipatingCachedRoutes(routes, group, candidateModel, trafficPolicy)
		routes = filterChannelSmartScheduleStableCachedRoutes(routes, group, candidateModel, trafficPolicy, false)
		routes = filterChannelSmartScheduleRequestLimits(routes, selectionOptions)
		if len(routes) == 0 {
			continue
		}
		status := channelSmartScheduleCandidateAffinityStatus(routes, channelId, managedPool)
		if status == ChannelSmartScheduleAffinityInvalid && knownRoute {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		return status
	}
	if knownRoute {
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
	selectionOptions.Filters = append([]dto.ChannelFilter(nil), selectionOptions.Filters...)
	if requestPath != "" {
		selectionOptions.Filters = append(selectionOptions.Filters, dto.ChannelFilter{Kind: dto.FilterRequestPath, RequestPath: requestPath})
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	if channelSmartScheduleRouteCache == nil {
		return ChannelSmartScheduleAffinityTemporarilyUnavailable
	}
	knownRoute := false
	for _, candidateModel := range modelNames {
		managedPool := trafficPolicy.managesPool(group, candidateModel)
		pool := channelSmartScheduleRouteCache[group][candidateModel]
		for _, route := range pool {
			if route.channelId == channelId && (!managedPool || route.participates) {
				knownRoute = true
			}
		}
		routes := prepareChannelSmartScheduleCachedRoutes(pool, group, candidateModel,
			requestModelName, requestPath, selectionOptions, trafficPolicy, false, managedPool)
		if len(routes) == 0 {
			continue
		}
		status := channelSmartScheduleCandidateAffinityStatus(routes, channelId, managedPool)
		if status == ChannelSmartScheduleAffinityInvalid && knownRoute {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
		return status
	}
	if knownRoute {
		return ChannelSmartScheduleAffinityTemporarilyUnavailable
	}
	return ChannelSmartScheduleAffinityInvalid
}

// Affinity may retain a member only while its effective candidate is in the
// same first-attempt layer used by normal selection. Zero weights follow the
// selector's all-zero fallback semantics.
func channelSmartScheduleCandidateAffinityStatus(
	routes []channelSmartScheduleCachedRoute, channelID int, managed bool,
) ChannelSmartScheduleAffinityStatus {
	var preferred *channelSmartScheduleCachedRoute
	highestPriority := int64(0)
	highestSet := false
	positiveWeight := false
	for index := range routes {
		route := &routes[index]
		priority, weight := channelSmartScheduleCachedRouteRouting(*route, managed)
		if !highestSet || priority > highestPriority {
			highestPriority, highestSet, positiveWeight = priority, true, weight > 0
		} else if priority == highestPriority && weight > 0 {
			positiveWeight = true
		}
		if route.logicalChannelID == 0 && route.channelId == channelID {
			preferred = route
		}
		for _, member := range route.logicalMembers {
			if member.channelID == channelID {
				preferred = route
			}
		}
	}
	if preferred == nil {
		return ChannelSmartScheduleAffinityInvalid
	}
	if !managed {
		return ChannelSmartScheduleAffinityEligible
	}
	priority, weight := channelSmartScheduleCachedRouteRouting(*preferred, true)
	if priority != highestPriority || (weight == 0 && positiveWeight) {
		return ChannelSmartScheduleAffinityTemporarilyUnavailable
	}
	if len(preferred.logicalMembers) > 0 {
		memberWeight := uint(0)
		positiveMember := false
		for _, member := range preferred.logicalMembers {
			positiveMember = positiveMember || member.weight > 0
			if member.channelID == channelID {
				memberWeight = member.weight
			}
		}
		if memberWeight == 0 && positiveMember {
			return ChannelSmartScheduleAffinityTemporarilyUnavailable
		}
	}
	return ChannelSmartScheduleAffinityEligible
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
