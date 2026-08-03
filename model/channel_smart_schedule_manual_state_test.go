package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveChannelSmartScheduleManualRoutingRebasesStabilityRestoreTarget(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	degradedPriority := int64(0)
	channelPriority := int64(80)
	channelWeight := uint(100)
	require.NoError(t, db.Create(&Channel{
		Id: 3191, Name: "人工接管保护路由", Status: common.ChannelStatusEnabled,
		Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 3191, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &degradedPriority, Weight: 0,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 3191, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Excluded: true, Revision: 1,
		StabilityState:         ChannelSmartScheduleStabilityDegraded,
		StabilitySavedPriority: channelPriority,
		StabilitySavedWeight:   channelWeight,
	}).Error)

	result, err := SaveChannelSmartScheduleManualRouting(3191, "vip", "model-a", 120, 300)
	require.NoError(t, err)
	assert.True(t, result.RoutingChanged)

	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 3191, GroupName: "vip", ModelName: "model-a",
	}).First(&state).Error)
	assert.Equal(t, int64(120), state.StabilitySavedPriority)
	assert.Equal(t, uint(300), state.StabilitySavedWeight)

	cleared, err := ClearChannelSmartScheduleRouteStability(3191, "vip", "model-a", 80, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(120), cleared.Priority)
	assert.Equal(t, uint(300), cleared.Weight)
}
