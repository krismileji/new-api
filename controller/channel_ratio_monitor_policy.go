package controller

import (
	"context"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

const channelMonitorRatioEpsilon = 1e-9

type channelMonitorPolicyPlan struct {
	GroupRatioUpdates       map[string]float64
	GroupRatioRevisions     model.ChannelMonitorGroupRatioRevisionGuard
	GroupRatioMemberships   model.ChannelMonitorGroupRatioMembershipGuard
	GroupRatioStatuses      model.ChannelMonitorGroupRatioStatusGuard
	GroupRatioValues        model.ChannelMonitorGroupRatioValueGuard
	GroupMembershipRemovals []model.ChannelMonitorGroupMembershipRemoval
	DisableChannelIds       []int
	DisableChannelRevisions map[int]int64
	DisableChannelStatuses  map[int]model.ChannelMonitorStatusSnapshot
	SkippedGroupCount       int
}

type channelMonitorPolicyInput struct {
	UpstreamRevision                 int64
	CostRatio                        float64
	BalanceBelowAutoDisableThreshold bool
	SingleChannelAction              string
	MultipleChannelsAction           string
}

type channelMonitorPolicyMember struct {
	ChannelId              int
	Target                 float64
	SingleChannelAction    string
	MultipleChannelsAction string
}

type channelMonitorPolicyGroup struct {
	Name          string
	CurrentRatio  float64
	Coefficient   float64
	ChannelIds    []int
	AllChannelIds []int
}

type channelMonitorPolicyMembership struct {
	ChannelId int
	Group     string
}

func collectChannelMonitorPolicyMembers(
	group channelMonitorPolicyGroup,
	policyInputs map[int]channelMonitorPolicyInput,
	disabledChannelIds map[int]struct{},
	removedMemberships map[channelMonitorPolicyMembership]struct{},
) ([]channelMonitorPolicyMember, bool) {
	members := make([]channelMonitorPolicyMember, 0, len(group.ChannelIds))
	for _, channelId := range group.ChannelIds {
		if _, disabled := disabledChannelIds[channelId]; disabled {
			continue
		}
		if _, removed := removedMemberships[channelMonitorPolicyMembership{ChannelId: channelId, Group: group.Name}]; removed {
			continue
		}
		input, exists := policyInputs[channelId]
		if !exists {
			return nil, false
		}
		target := input.CostRatio * group.Coefficient
		if !validateChannelMonitorRatio(&target) {
			return nil, false
		}
		members = append(members, channelMonitorPolicyMember{
			ChannelId:              channelId,
			Target:                 target,
			SingleChannelAction:    normalizeChannelMonitorPolicyAction(input.SingleChannelAction),
			MultipleChannelsAction: normalizeChannelMonitorPolicyAction(input.MultipleChannelsAction),
		})
	}
	return members, true
}

func planChannelMonitorPolicyActions(
	channels []*model.Channel,
	policyInputs map[int]channelMonitorPolicyInput,
	groupRatios map[string]float64,
	coefficients map[string]float64,
) channelMonitorPolicyPlan {
	plan := channelMonitorPolicyPlan{
		GroupRatioUpdates:       make(map[string]float64),
		GroupRatioRevisions:     make(model.ChannelMonitorGroupRatioRevisionGuard),
		GroupRatioMemberships:   make(model.ChannelMonitorGroupRatioMembershipGuard),
		GroupRatioStatuses:      make(model.ChannelMonitorGroupRatioStatusGuard),
		GroupRatioValues:        make(model.ChannelMonitorGroupRatioValueGuard),
		DisableChannelRevisions: make(map[int]int64),
		DisableChannelStatuses:  make(map[int]model.ChannelMonitorStatusSnapshot),
	}
	hasPolicy := false
	for _, input := range policyInputs {
		if normalizeChannelMonitorPolicyAction(input.SingleChannelAction) != channelMonitorPolicyActionNone ||
			normalizeChannelMonitorPolicyAction(input.MultipleChannelsAction) != channelMonitorPolicyActionNone {
			hasPolicy = true
			break
		}
	}
	if !hasPolicy {
		return plan
	}

	channelIdsByGroup := make(map[string][]int)
	allChannelIdsByGroup := make(map[string][]int)
	channelGroupCounts := make(map[int]int, len(channels))
	channelGroupsById := make(map[int]string, len(channels))
	channelStatusById := make(map[int]model.ChannelMonitorStatusSnapshot, len(channels))
	for _, channel := range channels {
		channelGroupsById[channel.Id] = channel.Group
		channelStatusById[channel.Id] = model.CaptureChannelMonitorStatus(channel)
		seenGroups := make(map[string]struct{})
		for _, group := range channel.GetGroups() {
			if group == "" {
				continue
			}
			if _, exists := seenGroups[group]; exists {
				continue
			}
			seenGroups[group] = struct{}{}
			channelGroupCounts[channel.Id]++
			allChannelIdsByGroup[group] = append(allChannelIdsByGroup[group], channel.Id)
			if channel.Status == common.ChannelStatusEnabled {
				channelIdsByGroup[group] = append(channelIdsByGroup[group], channel.Id)
			}
		}
	}
	groupNames := make([]string, 0, len(channelIdsByGroup))
	for group := range channelIdsByGroup {
		groupNames = append(groupNames, group)
	}
	sort.Strings(groupNames)

	groups := make([]channelMonitorPolicyGroup, 0, len(groupNames))
	for _, group := range groupNames {
		currentRatio, exists := groupRatios[group]
		if !exists {
			currentRatio = 1
		}
		if !validateChannelMonitorRatio(&currentRatio) {
			plan.SkippedGroupCount++
			continue
		}
		sort.Ints(channelIdsByGroup[group])
		sort.Ints(allChannelIdsByGroup[group])
		groups = append(groups, channelMonitorPolicyGroup{
			Name:          group,
			CurrentRatio:  currentRatio,
			Coefficient:   getChannelMonitorGroupCoefficient(coefficients, group),
			ChannelIds:    channelIdsByGroup[group],
			AllChannelIds: allChannelIdsByGroup[group],
		})
	}

	disableChannelIds := make(map[int]struct{})
	removedMemberships := make(map[channelMonitorPolicyMembership]struct{})
	for {
		nextDisableChannelIds := make(map[int]struct{})
		for _, group := range groups {
			members, complete := collectChannelMonitorPolicyMembers(group, policyInputs, disableChannelIds, removedMemberships)
			if !complete || len(members) == 0 {
				continue
			}
			if len(members) == 1 {
				member := members[0]
				if member.Target-group.CurrentRatio > channelMonitorRatioEpsilon &&
					member.SingleChannelAction == channelMonitorPolicyActionDisableChannel {
					nextDisableChannelIds[member.ChannelId] = struct{}{}
				}
				continue
			}
			for _, member := range members {
				if member.Target-group.CurrentRatio > channelMonitorRatioEpsilon &&
					member.MultipleChannelsAction == channelMonitorPolicyActionDisableChannel {
					nextDisableChannelIds[member.ChannelId] = struct{}{}
				}
			}
		}
		if len(nextDisableChannelIds) > 0 {
			for channelId := range nextDisableChannelIds {
				disableChannelIds[channelId] = struct{}{}
			}
			continue
		}

		removedOne := false
		for _, group := range groups {
			members, complete := collectChannelMonitorPolicyMembers(group, policyInputs, disableChannelIds, removedMemberships)
			if !complete || len(members) <= 1 {
				continue
			}
			for _, member := range members {
				if member.Target-group.CurrentRatio <= channelMonitorRatioEpsilon ||
					member.MultipleChannelsAction != channelMonitorPolicyActionRemoveFromGroup ||
					channelGroupCounts[member.ChannelId] <= 1 {
					continue
				}
				membership := channelMonitorPolicyMembership{ChannelId: member.ChannelId, Group: group.Name}
				removedMemberships[membership] = struct{}{}
				channelGroupCounts[member.ChannelId]--
				removedOne = true
				break
			}
			if removedOne {
				break
			}
		}
		if !removedOne {
			break
		}
	}

	for _, group := range groups {
		members, complete := collectChannelMonitorPolicyMembers(group, policyInputs, disableChannelIds, removedMemberships)
		if !complete {
			plan.SkippedGroupCount++
			continue
		}
		switch len(members) {
		case 0:
		case 1:
			member := members[0]
			if member.Target-group.CurrentRatio > channelMonitorRatioEpsilon &&
				member.SingleChannelAction == channelMonitorPolicyActionUpdateGroupRatio {
				plan.GroupRatioUpdates[group.Name] = member.Target
			}
		default:
			for _, member := range members {
				if member.Target-group.CurrentRatio <= channelMonitorRatioEpsilon ||
					member.MultipleChannelsAction != channelMonitorPolicyActionUpdateGroupRatio {
					continue
				}
				if currentTarget, exists := plan.GroupRatioUpdates[group.Name]; !exists || member.Target > currentTarget {
					plan.GroupRatioUpdates[group.Name] = member.Target
				}
			}
		}
		if _, updatesRatio := plan.GroupRatioUpdates[group.Name]; updatesRatio {
			revisions := make(map[int]int64, len(group.ChannelIds))
			memberships := make(map[int]string, len(group.AllChannelIds))
			statuses := make(map[int]int, len(group.AllChannelIds))
			for _, channelId := range group.ChannelIds {
				revisions[channelId] = policyInputs[channelId].UpstreamRevision
			}
			for _, channelId := range group.AllChannelIds {
				memberships[channelId] = channelGroupsById[channelId]
				statuses[channelId] = channelStatusById[channelId].Status
			}
			plan.GroupRatioRevisions[group.Name] = revisions
			plan.GroupRatioMemberships[group.Name] = memberships
			plan.GroupRatioStatuses[group.Name] = statuses
			plan.GroupRatioValues[group.Name] = model.ChannelMonitorGroupRatioValueSnapshot{
				Ratio: group.CurrentRatio, Coefficient: group.Coefficient,
			}
		}
	}

	plan.GroupMembershipRemovals = make([]model.ChannelMonitorGroupMembershipRemoval, 0, len(removedMemberships))
	for membership := range removedMemberships {
		if _, disabled := disableChannelIds[membership.ChannelId]; disabled {
			continue
		}
		plan.GroupMembershipRemovals = append(plan.GroupMembershipRemovals, model.ChannelMonitorGroupMembershipRemoval{
			ChannelId:                membership.ChannelId,
			Group:                    membership.Group,
			ExpectedGroups:           channelGroupsById[membership.ChannelId],
			ExpectedUpstreamRevision: policyInputs[membership.ChannelId].UpstreamRevision,
			GuardUpstreamRevision:    true,
		})
	}
	sort.Slice(plan.GroupMembershipRemovals, func(i, j int) bool {
		if plan.GroupMembershipRemovals[i].ChannelId != plan.GroupMembershipRemovals[j].ChannelId {
			return plan.GroupMembershipRemovals[i].ChannelId < plan.GroupMembershipRemovals[j].ChannelId
		}
		return plan.GroupMembershipRemovals[i].Group < plan.GroupMembershipRemovals[j].Group
	})
	plan.DisableChannelIds = make([]int, 0, len(disableChannelIds))
	for channelId := range disableChannelIds {
		plan.DisableChannelIds = append(plan.DisableChannelIds, channelId)
		plan.DisableChannelRevisions[channelId] = policyInputs[channelId].UpstreamRevision
		plan.DisableChannelStatuses[channelId] = channelStatusById[channelId]
	}
	sort.Ints(plan.DisableChannelIds)
	return plan
}

func applyChannelMonitorPolicyPlan(ctx context.Context, plan channelMonitorPolicyPlan) (groupsUpdated int, removedMemberships []model.ChannelMonitorGroupMembershipRemoval, disabledChannelIds []int, groupUpdateFailed bool, err error) {
	if len(plan.GroupMembershipRemovals) > 0 {
		if ctx != nil && ctx.Err() != nil {
			return groupsUpdated, nil, nil, false, ctx.Err()
		}
		removedMemberships, err = model.RemoveChannelMonitorGroupMemberships(plan.GroupMembershipRemovals)
		if err != nil {
			return groupsUpdated, nil, nil, false, err
		}
	}

	disabledChannelIds = make([]int, 0, len(plan.DisableChannelIds))
	disableApplied := make(map[int]struct{}, len(plan.DisableChannelIds))
	for _, channelId := range plan.DisableChannelIds {
		if ctx != nil && ctx.Err() != nil {
			return groupsUpdated, removedMemberships, disabledChannelIds, false, ctx.Err()
		}
		changed, revisionCurrent, _, updateErr := model.UpdateChannelMonitorStatusIfSnapshotRevision(
			channelId,
			plan.DisableChannelRevisions[channelId],
			plan.DisableChannelStatuses[channelId],
			common.ChannelStatusAutoDisabled,
			channelMonitorCostRatioPolicyDisableReason,
		)
		if updateErr != nil {
			return groupsUpdated, removedMemberships, disabledChannelIds, false, updateErr
		}
		if !revisionCurrent || !changed {
			continue
		}
		if changed {
			disabledChannelIds = append(disabledChannelIds, channelId)
			disableApplied[channelId] = struct{}{}
		}
	}

	// A ratio target is calculated from the post-action membership/status set.
	// If an action was stale or skipped, discard only the affected group rather
	// than writing a ratio based on an assumption that did not take effect.
	ratioUpdates := make(map[string]float64, len(plan.GroupRatioUpdates))
	for group, ratio := range plan.GroupRatioUpdates {
		ratioUpdates[group] = ratio
	}
	revisionGuards := plan.GroupRatioRevisions
	membershipGuards := make(model.ChannelMonitorGroupRatioMembershipGuard, len(plan.GroupRatioMemberships))
	for group, expectedByChannel := range plan.GroupRatioMemberships {
		membershipGuards[group] = make(map[int]string, len(expectedByChannel))
		for channelId, expectedGroups := range expectedByChannel {
			membershipGuards[group][channelId] = expectedGroups
		}
	}
	statusGuards := make(model.ChannelMonitorGroupRatioStatusGuard, len(plan.GroupRatioStatuses))
	for group, expectedByChannel := range plan.GroupRatioStatuses {
		statusGuards[group] = make(map[int]int, len(expectedByChannel))
		for channelId, expectedStatus := range expectedByChannel {
			statusGuards[group][channelId] = expectedStatus
		}
	}

	appliedRemovalByChannel := make(map[int]map[string]struct{})
	for _, removal := range removedMemberships {
		if appliedRemovalByChannel[removal.ChannelId] == nil {
			appliedRemovalByChannel[removal.ChannelId] = make(map[string]struct{})
		}
		appliedRemovalByChannel[removal.ChannelId][removal.Group] = struct{}{}
	}
	plannedRemovalByChannel := make(map[int]map[string]struct{})
	originalGroupsByChannel := make(map[int]string)
	for _, removal := range plan.GroupMembershipRemovals {
		if plannedRemovalByChannel[removal.ChannelId] == nil {
			plannedRemovalByChannel[removal.ChannelId] = make(map[string]struct{})
		}
		plannedRemovalByChannel[removal.ChannelId][removal.Group] = struct{}{}
		if _, exists := originalGroupsByChannel[removal.ChannelId]; !exists {
			originalGroupsByChannel[removal.ChannelId] = removal.ExpectedGroups
		}
		if _, applied := appliedRemovalByChannel[removal.ChannelId][removal.Group]; !applied {
			delete(ratioUpdates, removal.Group)
		}
	}
	for channelId, plannedGroups := range plannedRemovalByChannel {
		appliedGroups := appliedRemovalByChannel[channelId]
		finalGroups := make([]string, 0)
		for _, group := range strings.Split(originalGroupsByChannel[channelId], ",") {
			group = strings.TrimSpace(group)
			if group == "" {
				continue
			}
			if _, removed := appliedGroups[group]; removed {
				continue
			}
			seen := false
			for _, existing := range finalGroups {
				if existing == group {
					seen = true
					break
				}
			}
			if !seen {
				finalGroups = append(finalGroups, group)
			}
		}
		finalGroupString := strings.Join(finalGroups, ",")
		for group, expectedByChannel := range membershipGuards {
			if _, tracked := expectedByChannel[channelId]; !tracked {
				continue
			}
			if _, planned := plannedGroups[group]; planned {
				if _, applied := appliedGroups[group]; applied {
					delete(expectedByChannel, channelId)
				}
				continue
			}
			expectedByChannel[channelId] = finalGroupString
		}
	}
	for group, expectedByChannel := range statusGuards {
		for channelId := range expectedByChannel {
			if _, planned := plan.DisableChannelRevisions[channelId]; !planned {
				continue
			}
			if _, applied := disableApplied[channelId]; applied {
				expectedByChannel[channelId] = common.ChannelStatusAutoDisabled
			} else {
				delete(ratioUpdates, group)
			}
		}
	}

	if len(removedMemberships) > 0 || len(disabledChannelIds) > 0 {
		model.InitChannelCache()
		service.ResetProxyClientCache()
	}
	if len(ratioUpdates) > 0 {
		groupsUpdated, err = model.MergeChannelMonitorGroupOptionsIfCurrent(
			ratioUpdates,
			nil,
			true,
			revisionGuards,
			membershipGuards,
			statusGuards,
			plan.GroupRatioValues,
		)
		if err != nil {
			return groupsUpdated, removedMemberships, disabledChannelIds, true, err
		}
	}
	return groupsUpdated, removedMemberships, disabledChannelIds, false, nil
}
