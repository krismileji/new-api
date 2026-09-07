package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useSmartScheduleRoutingConsistencyFixture(t *testing.T) []ChannelSmartScheduleRoute {
	t.Helper()
	useChannelSmartScheduleTrafficPolicy(t, true, `[{"group":"vip","models":["model-a"]}]`)
	t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "true")
	originalMemory := common.MemoryCacheEnabled
	originalChannels := channelsIDM
	originalRoutes := channelSmartScheduleRouteCache
	originalRuntime := logicalChannelRuntimeCache
	originalOverlays := channelLogicalSmartScheduleRoutingCache
	common.MemoryCacheEnabled = true
	channelsIDM = map[int]*Channel{}
	logicalChannelRuntimeCache = &LogicalChannelRuntimeSnapshot{
		Channels: map[int]LogicalChannelIdentity{},
		Groups:   map[int64]LogicalChannelGroupSnapshot{},
	}
	channelLogicalSmartScheduleRoutingCache = nil
	routes := make([]ChannelSmartScheduleRoute, 0, 3)
	for _, id := range []int{9451, 9452, 9453} {
		channelsIDM[id] = &Channel{Id: id, Status: common.ChannelStatusEnabled}
		logicalChannelRuntimeCache.Channels[id] = LogicalChannelIdentity{ChannelID: id, LogicalChannelID: int64(id)}
		routes = append(routes, ChannelSmartScheduleRoute{
			ChannelId: id, Group: "vip", Model: "model-a", Enabled: true,
			ChannelStatus: common.ChannelStatusEnabled, Priority: 100, Weight: 100,
			State: ChannelSmartScheduleRouteState{ChannelId: id, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		})
	}
	channelSmartScheduleRouteCache = map[string]map[string][]channelSmartScheduleCachedRoute{
		"vip": {"model-a": {
			{channelId: 9451, priority: 100, weight: 100, participates: true},
			{channelId: 9452, priority: 100, weight: 0, participates: true},
			{channelId: 9453, priority: 50, weight: 100, participates: true},
		}},
	}
	routes[1].Weight = 0
	routes[2].Priority = 50
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemory
		channelsIDM = originalChannels
		channelSmartScheduleRouteCache = originalRoutes
		logicalChannelRuntimeCache = originalRuntime
		channelLogicalSmartScheduleRoutingCache = originalOverlays
	})
	return routes
}

func TestSmartScheduleAffinityRespectsEffectiveLogicalPriority(t *testing.T) {
	routes := useSmartScheduleRoutingConsistencyFixture(t)
	for _, id := range []int{9451, 9452} {
		logicalChannelRuntimeCache.Channels[id] = LogicalChannelIdentity{ChannelID: id, LogicalChannelID: 9450, Revision: 2}
	}
	logicalChannelRuntimeCache.Groups[9450] = LogicalChannelGroupSnapshot{
		LogicalChannelID: 9450, Revision: 2, Status: ChannelLogicalGroupStatusEnabled,
		Members: []LogicalChannelMemberSnapshot{{ChannelID: 9451, Weight: 100}, {ChannelID: 9452, Weight: 0}},
	}
	channelLogicalSmartScheduleRoutingCache = map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay{
		{logicalID: 9450, revision: 2, group: "vip", model: "model-a"}: {
			routing: channelLogicalSmartScheduleRouting{priority: 10, weight: 100},
			state:   ChannelSmartScheduleRouteState{ParticipationSet: true},
		},
	}
	views, err := GetChannelSmartScheduleRouteRuntimeViewsWithContext(context.Background(), routes)
	require.NoError(t, err)
	assert.Equal(t, int64(10), views[channelSmartScheduleRouteKey(9451, "vip", "model-a")].Priority)
	selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 9453, selected.Id)
	assert.Equal(t, ChannelSmartScheduleAffinityTemporarilyUnavailable,
		ChannelSmartScheduleAffinityCandidateEligibilityExcluding("vip", "model-a", 9451, "", nil))
	assert.Equal(t, ChannelSmartScheduleAffinityEligible,
		ChannelSmartScheduleAffinityEligibility("vip", "model-a", 9453, ""))
	selected, err = SelectChannelSmartScheduleAffinityMemberExcluding("vip", "model-a", 9451, "", nil)
	assert.ErrorIs(t, err, ErrLogicalChannelSelectionNoAvailableMembers)
	assert.Nil(t, selected)
}

func TestSmartScheduleAffinityDoesNotReviveZeroWeightRoute(t *testing.T) {
	useSmartScheduleRoutingConsistencyFixture(t)
	selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 9451, selected.Id)
	assert.Equal(t, ChannelSmartScheduleAffinityTemporarilyUnavailable,
		ChannelSmartScheduleAffinityEligibility("vip", "model-a", 9452, ""))
}

func TestSmartScheduleMonitorUsesPublishedPhysicalProtection(t *testing.T) {
	routes := useSmartScheduleRoutingConsistencyFixture(t)
	routes[0].State.StabilityState = ChannelSmartScheduleStabilityDegraded
	routes[0].Priority, routes[0].Weight = 0, 0
	views, err := GetChannelSmartScheduleRouteRuntimeViewsWithContext(context.Background(), routes)
	require.NoError(t, err)
	view := views[channelSmartScheduleRouteKey(9451, "vip", "model-a")]
	require.NotNil(t, view.State)
	assert.Empty(t, view.State.StabilityState)
	assert.Equal(t, int64(100), view.Priority)
	selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 9451, selected.Id)
}

func TestSmartScheduleMonitorUsesCacheWithLogicalGroupingDisabled(t *testing.T) {
	routes := useSmartScheduleRoutingConsistencyFixture(t)
	t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "false")
	routes[0].Priority = 10
	routes[2].Priority = 200
	views, err := GetChannelSmartScheduleRouteRuntimeViewsWithContext(context.Background(), routes)
	require.NoError(t, err)
	assert.Equal(t, int64(100), views[channelSmartScheduleRouteKey(9451, "vip", "model-a")].Priority)
	assert.Equal(t, int64(50), views[channelSmartScheduleRouteKey(9453, "vip", "model-a")].Priority)
}

func TestSmartScheduleMonitorSnapshotIncludesRoutesAwaitingRemoval(t *testing.T) {
	routes := useSmartScheduleRoutingConsistencyFixture(t)
	originalMetadata := channelSmartScheduleLocalSnapshotMetadataCache
	channelSmartScheduleLocalSnapshotMetadataCache = &channelSmartScheduleLocalSnapshotMetadata{
		Revision: 12, GeneratedAt: time.Now().UnixMilli(), FromRedis: true,
	}
	t.Cleanup(func() { channelSmartScheduleLocalSnapshotMetadataCache = originalMetadata })
	rows, views, snapshot, err := GetChannelSmartScheduleMonitorRuntimeSnapshot(context.Background(), routes[1:])
	require.NoError(t, err)
	assert.Len(t, rows, 3)
	assert.True(t, snapshot.Available)
	var decision ChannelRoutingDecision
	selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil, ChannelSelectionOptions{
		ObserveRouting: func(value ChannelRoutingDecision) { decision = value },
	})
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, snapshot.Revision, decision.SnapshotRevision)
	view := views[channelSmartScheduleRouteKey(selected.Id, "vip", "model-a")]
	require.NotNil(t, view.Enabled)
	assert.True(t, *view.Enabled)
	assert.True(t, view.Participates)
	assert.Equal(t, view.CandidateChannelId, decision.CandidateChannelID)
}

func TestSmartScheduleMonitorCoalescesAfterPhysicalCooldownExclusion(t *testing.T) {
	routes := useSmartScheduleRoutingConsistencyFixture(t)
	for _, id := range []int{9451, 9452} {
		logicalChannelRuntimeCache.Channels[id] = LogicalChannelIdentity{ChannelID: id, LogicalChannelID: 9450, Revision: 2}
	}
	logicalChannelRuntimeCache.Groups[9450] = LogicalChannelGroupSnapshot{LogicalChannelID: 9450, Revision: 2,
		Status: ChannelLogicalGroupStatusEnabled, Members: []LogicalChannelMemberSnapshot{{ChannelID: 9451, Weight: 100}, {ChannelID: 9452, Weight: 100}},
	}
	channelSmartScheduleRouteCache["vip"]["model-a"][1].priority = 10
	channelSmartScheduleRouteCache["vip"]["model-a"][1].weight = 100
	views, err := GetChannelSmartScheduleRouteRuntimeViewsWithContext(context.Background(), routes, map[string][]int{"model-a": {9451}})
	require.NoError(t, err)
	view := views[channelSmartScheduleRouteKey(9452, "vip", "model-a")]
	assert.Equal(t, int64(10), view.Priority)
	assert.Equal(t, 9452, view.CandidateChannelId)
	selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil, ChannelSelectionOptions{ExcludedChannelIds: []int{9451}})
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 9453, selected.Id)
}
