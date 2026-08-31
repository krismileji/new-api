package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogicalSmartScheduleOverlayControlsSharedRouteWithoutPhysicalProjection(t *testing.T) {
	tests := []struct {
		name    string
		state   ChannelSmartScheduleRouteState
		options ChannelSelectionOptions
	}{
		{
			name: "逻辑稳定性降级",
			state: ChannelSmartScheduleRouteState{
				ParticipationSet: true, StabilityState: ChannelSmartScheduleStabilityDegraded,
			},
		},
		{
			name: "逻辑探索请求上限",
			state: ChannelSmartScheduleRouteState{
				ParticipationSet:           true,
				TemporaryTrafficKind:       ChannelSmartScheduleTemporaryTrafficExploration,
				ExplorationMaxPromptTokens: 100,
			},
			options: ChannelSelectionOptions{EstimatedPromptTokens: 101},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupDirtyLogicalSelectionTest(t)
			seedDirtyLogicalSelectionTest(t, db)
			payload, err := encodeLogicalSmartScheduleRouteStateWithRouting(test.state, 100, 100)
			require.NoError(t, err)
			require.NoError(t, db.Create(&ChannelLogicalSmartScheduleRouteState{
				LogicalGroupID: dirtySelectionLogicalID, LogicalRevision: 1,
				GroupName: "vip", ModelName: "model-a", StateRevision: 1,
				StateJSON: payload, UpdatedAt: common.GetTimestamp(),
			}).Error)
			InitChannelCache()

			selected, err := GetRandomSatisfiedChannel(
				"vip", "model-a", 0, nil,
				test.options)

			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, dirtySelectionNewMember, selected.Id)

			common.MemoryCacheEnabled = false
			selected, err = GetRandomSatisfiedChannel(
				"vip", "model-a", 0, nil,
				test.options)

			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, dirtySelectionNewMember, selected.Id)
		})
	}
}

func TestLogicalSmartScheduleOverlayAppliesWithOneAvailableMember(t *testing.T) {
	db := setupDirtyLogicalSelectionTest(t)
	seedDirtyLogicalSelectionTest(t, db)
	state := ChannelSmartScheduleRouteState{ParticipationSet: true}
	payload, err := encodeLogicalSmartScheduleRouteStateWithRouting(state, 100, 100)
	require.NoError(t, err)
	require.NoError(t, db.Create(&ChannelLogicalSmartScheduleRouteState{
		LogicalGroupID: dirtySelectionLogicalID, LogicalRevision: 1,
		GroupName: "vip", ModelName: "model-a", StateRevision: 1,
		StateJSON: payload, UpdatedAt: common.GetTimestamp(),
	}).Error)
	InitChannelCache()

	selected, err := GetRandomSatisfiedChannel(
		"vip", "model-a", 0, nil,

		ChannelSelectionOptions{ExcludedChannelIds: []int{dirtySelectionOldMember}})

	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, dirtySelectionRetained, selected.Id)
}
