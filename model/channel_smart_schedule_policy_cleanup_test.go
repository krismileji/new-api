package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemovedSmartSchedulePolicyRefreshRestoresCachedChannelRouting(t *testing.T) {
	db, _, _ := setupChannelSmartScheduleRedisSnapshotTest(t)
	const policies = `[{"group":"vip","models":["model-a"]}]`
	require.NoError(t, db.Create(&[]Option{
		{Key: channelMonitorSmartScheduleEnabledOption, Value: "true"},
		{Key: ChannelMonitorSmartScheduleGroupPoliciesOption, Value: policies},
		{Key: ChannelSmartScheduleControlRevisionOption, Value: "before-remove"},
	}).Error)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 9801, Name: "scheduled-primary", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(10)), Weight: common.GetPointer(uint(30))},
		{Id: 9802, Name: "default-primary", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(80)), Weight: common.GetPointer(uint(60))},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 9801, Group: "vip", Model: "model-a", Enabled: true, Priority: common.GetPointer(int64(100)), Weight: 1000},
		{ChannelId: 9802, Group: "vip", Model: "model-a", Enabled: true, Priority: common.GetPointer(int64(5)), Weight: 10},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 9801, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{ChannelId: 9802, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
	}).Error)
	InitChannelCache()
	selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 9801, selected.Id)

	changed, err := UpdateChannelMonitorSettingsOptions(map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "false",
		ChannelMonitorSmartScheduleGroupPoliciesOption: "[]",
		ChannelSmartScheduleControlRevisionOption:      "after-remove",
	}, true, common.GetPointer("before-remove"))
	require.NoError(t, err)
	require.True(t, changed)
	InitChannelCache()
	selected, err = GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 9802, selected.Id)
	channelSyncLock.RLock()
	routes := append([]channelSmartScheduleCachedRoute(nil), channelSmartScheduleRouteCache["vip"]["model-a"]...)
	channelSyncLock.RUnlock()
	for _, route := range routes {
		if route.channelId == 9802 {
			priority, weight := channelSmartScheduleCachedRouteRouting(route, false)
			assert.Equal(t, int64(80), priority)
			assert.Equal(t, uint(60), weight)
		}
	}
}

func TestRemovedSmartSchedulePolicyAllowsRepairingMalformedSettings(t *testing.T) {
	db, _, _ := setupChannelSmartScheduleRedisSnapshotTest(t)
	require.NoError(t, db.Create(&Option{Key: ChannelMonitorSmartScheduleGroupPoliciesOption, Value: "{"}).Error)
	state := ChannelSmartScheduleRouteState{ChannelId: 9801, GroupName: "unrelated", ModelName: "model-a", ParticipationSet: true}
	require.NoError(t, db.Create(&state).Error)
	changed, err := UpdateChannelMonitorSettingsOptions(map[string]string{
		ChannelMonitorSmartScheduleGroupPoliciesOption: "[]",
	}, false, nil)
	require.NoError(t, err)
	assert.False(t, changed)
	var saved Option
	require.NoError(t, db.Where(&Option{Key: ChannelMonitorSmartScheduleGroupPoliciesOption}).First(&saved).Error)
	assert.Equal(t, "[]", saved.Value)
	var retained ChannelSmartScheduleRouteState
	require.NoError(t, db.First(&retained, state.Id).Error)
	assert.Equal(t, state, retained, "malformed old settings do not justify deleting unrelated groups")
}
