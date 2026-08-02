package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveChannelSmartScheduleManualRoutingOnlyUpdatesExcludedRoute(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(8)
	weight := uint(20)
	require.NoError(t, db.Create(&Channel{
		Id: 1401, Name: "人工路由", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1401, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1401, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Excluded: true, Revision: 1,
	}).Error)

	result, err := SaveChannelSmartScheduleManualRouting(1401, "vip", "model-a", 25, 700)
	require.NoError(t, err)
	assert.True(t, result.RoutingChanged)
	assert.Equal(t, int64(25), result.Priority)
	assert.Equal(t, uint(700), result.Weight)

	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1401, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(25), *ability.Priority)
	assert.Equal(t, uint(700), ability.Weight)

	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 1401, GroupName: "vip", ModelName: "model-a",
	}).First(&state).Error)
	assert.Equal(t, int64(2), state.Revision)
	assert.Equal(t, "管理员手动设置未参与路由的优先级和权重", state.LastScheduleError)
}

func TestSaveChannelSmartScheduleManualRoutingRejectsParticipatingRoute(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(8)
	weight := uint(20)
	require.NoError(t, db.Create(&Channel{
		Id: 1402, Name: "自动路由", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1402, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1402, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
	}).Error)

	_, err := SaveChannelSmartScheduleManualRouting(1402, "vip", "model-a", 25, 700)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "请先取消参与")

	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1402, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priority, *ability.Priority)
	assert.Equal(t, weight, ability.Weight)
}
