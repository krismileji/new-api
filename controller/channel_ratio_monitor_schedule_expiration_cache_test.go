package controller

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunChannelSmartScheduleRefreshesCacheWhenOnlyExpiredPrimaryChanges(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = false })

	fixedPriority := int64(101)
	fixedWeight := uint(1000)
	restoredPriority := int64(80)
	restoredWeight := uint(100)
	backupPriority := int64(90)
	backupWeight := uint(100)
	require.NoError(t, db.Create(&[]model.Channel{
		{
			Id: 1701, Name: "expired fixed", Status: common.ChannelStatusEnabled,
			Group: "vip", Models: "model-a", Priority: &restoredPriority, Weight: &restoredWeight,
		},
		{
			Id: 1702, Name: "backup", Status: common.ChannelStatusEnabled,
			Group: "vip", Models: "model-a", Priority: &backupPriority, Weight: &backupWeight,
		},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{
			ChannelId: 1701, Group: "vip", Model: "model-a", Enabled: true,
			Priority: &fixedPriority, Weight: fixedWeight,
		},
		{
			ChannelId: 1702, Group: "vip", Model: "model-a", Enabled: true,
			Priority: &backupPriority, Weight: backupWeight,
		},
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1701, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		ManualPrimaryUntil:         common.GetTimestamp() - 1,
		ManualPrimarySaved:         true,
		ManualPrimarySavedPriority: restoredPriority,
		ManualPrimarySavedWeight:   restoredWeight,
	}).Error)

	model.InitChannelCache()
	selected, err := model.GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 1701, selected.Id)

	models := []string{"model-b"}
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyPriorityWeight, models, 1, 80, 30,
	)
	result, err := runChannelSmartScheduleByRouteOnce(
		context.Background(),
		func(int, int) {},
		false,
		channelMonitorSettings{SmartScheduleGroupPolicies: smartScheduleGroupPolicies{policy}},
		channelSmartScheduleTaskResult{},
	)
	require.NoError(t, err)
	assert.Zero(t, result.Total)

	selected, err = model.GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 1702, selected.Id)

	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1701, GroupName: "vip", ModelName: "model-a",
	}).First(&state).Error)
	assert.Zero(t, state.ManualPrimaryUntil)
}
