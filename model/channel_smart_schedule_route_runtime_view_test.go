package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChannelSmartScheduleRouteRuntimeViewsUsePublishedLogicalCandidate(t *testing.T) {
	t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "true")
	useChannelSmartScheduleTrafficPolicy(t, true, `[{"group":"vip","models":["model-a"]}]`)

	cachedRoutes := map[string]map[string][]channelSmartScheduleCachedRoute{
		"vip": {
			"model-a": {
				{channelId: 9451, priority: 100, weight: 100, participates: true},
				{channelId: 9452, priority: 100, weight: 100, participates: true},
				{channelId: 9453, priority: 50, weight: 100, participates: true},
			},
		},
	}
	logicalID := int64(9450)
	runtime := &LogicalChannelRuntimeSnapshot{
		Channels: map[int]LogicalChannelIdentity{
			9451: {ChannelID: 9451, LogicalChannelID: logicalID, Revision: 2},
			9452: {ChannelID: 9452, LogicalChannelID: logicalID, Revision: 2},
			9453: {ChannelID: 9453, LogicalChannelID: 9453},
		},
		Groups: map[int64]LogicalChannelGroupSnapshot{
			logicalID: {
				LogicalChannelID: logicalID,
				Revision:         2,
				Status:           ChannelLogicalGroupStatusEnabled,
				Members: []LogicalChannelMemberSnapshot{
					{ChannelID: 9451, Weight: 1},
					{ChannelID: 9452, Weight: 3},
				},
			},
		},
	}
	logicalState := ChannelSmartScheduleRouteState{ParticipationSet: true}
	routings := map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay{
		{logicalID: logicalID, revision: 2, group: "vip", model: "model-a"}: {
			routing: channelLogicalSmartScheduleRouting{priority: 10, weight: 100},
			state:   logicalState,
		},
	}

	state := func(channelID int) ChannelSmartScheduleRouteState {
		return ChannelSmartScheduleRouteState{
			ChannelId:        channelID,
			GroupName:        "vip",
			ModelName:        "model-a",
			ParticipationSet: true,
		}
	}
	routes := []ChannelSmartScheduleRoute{
		{ChannelId: 9451, Group: "vip", Model: "model-a", Priority: 100, Weight: 100, State: state(9451)},
		{ChannelId: 9452, Group: "vip", Model: "model-a", Priority: 100, Weight: 100, State: state(9452)},
		{ChannelId: 9453, Group: "vip", Model: "model-a", Priority: 50, Weight: 100, State: state(9453)},
	}

	views := make(map[ChannelSmartScheduleRouteKey]ChannelSmartScheduleRouteRuntimeView, len(routes))
	for _, route := range routes {
		state := route.State
		views[channelSmartScheduleRouteKey(route.ChannelId, route.Group, route.Model)] = ChannelSmartScheduleRouteRuntimeView{
			Priority: route.Priority, Weight: route.Weight, CandidateChannelId: route.ChannelId,
			Participates: state.Participates(), State: &state,
		}
	}
	applyChannelSmartScheduleCachedRuntimeViews(views, routes, cachedRoutes, nil, runtime, routings)
	logicalView := views[ChannelSmartScheduleRouteKey{ChannelId: 9451, Group: "vip", Model: "model-a"}]
	assert.Equal(t, int64(10), logicalView.Priority)
	assert.Equal(t, uint(100), logicalView.Weight)
	assert.Equal(t, 9451, logicalView.CandidateChannelId)
	assert.Equal(t, []int{9451, 9452}, logicalView.LogicalMemberIds)
	assert.Equal(t, []uint{1, 3}, logicalView.LogicalMemberWeights)
	assert.Equal(t, int64(50), views[ChannelSmartScheduleRouteKey{ChannelId: 9453, Group: "vip", Model: "model-a"}].Priority)
}
