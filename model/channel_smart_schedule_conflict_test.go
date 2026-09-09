package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelSmartScheduleRecoveryConflictRollsBackWholePool(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db, _, _ := setupChannelSmartScheduleRedisSnapshotTest(t)
			db = setupChannelSmartScheduleMonitorSnapshotMatrixDB(t, engine, db)
			runChannelSmartScheduleRecoveryConflict(t, db)
		})
	}
}

func runChannelSmartScheduleRecoveryConflict(t *testing.T, db *gorm.DB) {
	t.Helper()
	priority := int64(10)
	for _, channelID := range []int{1101, 1102} {
		require.NoError(t, db.Create(&Channel{Id: channelID, Name: "route", Status: common.ChannelStatusEnabled}).Error)
		require.NoError(t, db.Create(&Ability{ChannelId: channelID, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 50}).Error)
		require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
			ChannelId: channelID, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1,
		}).Error)
	}
	zero := int64(0)
	outcomes, err := ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{
		{ChannelId: 1101, Group: "vip", Model: "model-a", AdaptiveOverlayOnly: true, PoolGuard: true,
			ApplyPriorityWeight: true, Priority: 20, Weight: 100, ExpectedRevision: 1,
			ExpectedParticipationSet: true, ExpectedAbilityEnabled: true, ExpectedChannelStatus: common.ChannelStatusEnabled,
			ExpectedPriority: 10, ExpectedWeight: 50},
		{ChannelId: 1102, Group: "vip", Model: "model-a", AdaptiveOverlayOnly: true, PoolGuard: true,
			RuntimeStabilityRecovery: true, Stability: &ChannelSmartScheduleStabilityUpdate{}, RuntimeProtectionUntil: &zero,
			ExpectedRevision: 1, ExpectedParticipationSet: true, ExpectedAbilityEnabled: true,
			ExpectedChannelStatus: common.ChannelStatusEnabled, ExpectedPriority: 10, ExpectedWeight: 50},
	})
	require.NoError(t, err)
	require.Len(t, outcomes, 2)
	for _, outcome := range outcomes {
		assert.False(t, outcome.Applied)
		assert.False(t, outcome.RoutingChanged)
	}
	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1101, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priority, *ability.Priority)
	assert.EqualValues(t, 50, ability.Weight)
	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("channel_id = ?", 1101).First(&state).Error)
	assert.EqualValues(t, 1, state.Revision)
}
