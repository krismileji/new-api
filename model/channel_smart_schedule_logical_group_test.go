package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoalesceChannelSmartScheduleLogicalRoutesProducesOneCandidatePerGroup(t *testing.T) {
	oldDirty := logicalChannelRuntimeDirty
	oldChannels := logicalChannelRuntimeCache
	t.Cleanup(func() {
		logicalChannelRuntimeDirty = oldDirty
		logicalChannelRuntimeCache = oldChannels
	})
	logicalChannelRuntimeDirty = false
	runtime := &LogicalChannelRuntimeSnapshot{
		Channels: map[int]LogicalChannelIdentity{
			10: {ChannelID: 10, LogicalChannelID: 900, Revision: 4},
			20: {ChannelID: 20, LogicalChannelID: 900, Revision: 4},
			30: {ChannelID: 30, LogicalChannelID: 30},
		},
		Groups: map[int64]LogicalChannelGroupSnapshot{
			900: {
				LogicalChannelID: 900, Revision: 4, Status: ChannelLogicalGroupStatusEnabled,
				Members: []LogicalChannelMemberSnapshot{
					{ChannelID: 10, Weight: 3}, {ChannelID: 20, Weight: 1},
				},
			},
		},
	}
	routes := []channelSmartScheduleCachedRoute{
		{channelId: 10, priority: 80, weight: 20, officialPriority: 80, officialWeight: 20},
		{channelId: 20, priority: 100, weight: 10, officialPriority: 90, officialWeight: 30},
		{channelId: 30, priority: 70, weight: 5, officialPriority: 70, officialWeight: 5},
	}
	coalesced := coalesceChannelSmartScheduleLogicalRoutes(routes, runtime)
	require.Len(t, coalesced, 2)
	var grouped channelSmartScheduleCachedRoute
	for _, route := range coalesced {
		if route.logicalChannelID == 900 {
			grouped = route
		}
	}
	require.Equal(t, int64(900), grouped.logicalChannelID)
	assert.Equal(t, int64(4), grouped.logicalRevision)
	assert.Equal(t, 10, grouped.channelId, "canonical physical id is stable and cannot duplicate candidates")
	assert.Equal(t, int64(100), grouped.logicalPriority, "group keeps the best member's smart priority")
	assert.Equal(t, uint(20), grouped.logicalWeight)
	assert.Equal(t, int64(90), grouped.logicalOfficialPriority)
	assert.Equal(t, uint(30), grouped.logicalOfficialWeight)
	assert.Equal(t, []channelSmartScheduleLogicalMember{{channelID: 10, weight: 3}, {channelID: 20, weight: 1}}, grouped.logicalMembers)
}

func TestSelectLogicalSmartScheduleMemberUsesLogicalMemberWeight(t *testing.T) {
	oldChannels := channelsIDM
	oldRuntime := logicalChannelRuntimeCache
	oldDirty := logicalChannelRuntimeDirty
	t.Cleanup(func() {
		channelsIDM = oldChannels
		logicalChannelRuntimeCache = oldRuntime
		logicalChannelRuntimeDirty = oldDirty
	})
	channelsIDM = map[int]*Channel{10: {Id: 10}, 20: {Id: 20}}
	logicalChannelRuntimeDirty = false
	logicalChannelRuntimeCache = &LogicalChannelRuntimeSnapshot{
		Channels: map[int]LogicalChannelIdentity{
			10: {ChannelID: 10, LogicalChannelID: 900, Revision: 4},
			20: {ChannelID: 20, LogicalChannelID: 900, Revision: 4},
		},
		Groups: map[int64]LogicalChannelGroupSnapshot{
			900: {LogicalChannelID: 900, Revision: 4, Status: ChannelLogicalGroupStatusEnabled, Members: []LogicalChannelMemberSnapshot{{ChannelID: 10, Weight: 3}, {ChannelID: 20, Weight: 1}}},
		},
	}
	route := channelSmartScheduleCachedRoute{
		channelId: 10, logicalChannelID: 900, logicalRevision: 4,
		logicalMembers: []channelSmartScheduleLogicalMember{{channelID: 10, weight: 3}, {channelID: 20, weight: 1}},
	}
	selectedID, err := selectLogicalSmartScheduleMemberID(route, logicalChannelRuntimeCache)
	require.NoError(t, err)
	assert.Contains(t, []int{10, 20}, selectedID)
}

func TestCoalesceChannelSmartScheduleLogicalRoutesOverlaysLogicalRouting(t *testing.T) {
	runtime := &LogicalChannelRuntimeSnapshot{
		Channels: map[int]LogicalChannelIdentity{
			10: {ChannelID: 10, LogicalChannelID: 900, Revision: 4},
			20: {ChannelID: 20, LogicalChannelID: 900, Revision: 4},
		},
		Groups: map[int64]LogicalChannelGroupSnapshot{
			900: {
				LogicalChannelID: 900, Revision: 4, Status: ChannelLogicalGroupStatusEnabled,
				Members: []LogicalChannelMemberSnapshot{{ChannelID: 10, Weight: 1}, {ChannelID: 20, Weight: 1}},
			},
		},
	}
	routes := []channelSmartScheduleCachedRoute{
		{channelId: 10, priority: 100, weight: 90},
		{channelId: 20, priority: 80, weight: 70},
	}
	routings := map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay{
		{logicalID: 900, revision: 4, group: "vip", model: "model-a"}: {
			routing: channelLogicalSmartScheduleRouting{priority: 30, weight: 20},
			state:   ChannelSmartScheduleRouteState{ParticipationSet: true},
		},
	}
	coalesced := coalesceChannelSmartScheduleLogicalRoutesWithRouting(
		routes, runtime, "vip", "model-a", routings,
	)
	require.Len(t, coalesced, 1)
	assert.Equal(t, int64(30), coalesced[0].logicalPriority)
	assert.Equal(t, uint(20), coalesced[0].logicalWeight)
}
