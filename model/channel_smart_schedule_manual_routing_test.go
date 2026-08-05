package model

import (
	"math"
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

func TestSaveChannelSmartScheduleManualRoutingCreatesZeroOverride(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.Create(&Channel{
		Id: 1405, Name: "零值人工路由", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a",
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1405, Group: "vip", Model: "model-a", Enabled: true,
	}).Error)
	require.NoError(t, clearChannelSmartScheduleAbilityRoutingTx(
		db, channelSmartScheduleRouteKey(1405, "vip", "model-a"),
	))
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1405, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Excluded: true, Revision: 1,
	}).Error)

	result, err := SaveChannelSmartScheduleManualRouting(1405, "vip", "model-a", 0, 0)
	require.NoError(t, err)
	assert.True(t, result.RoutingChanged)

	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1405, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
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

func TestSaveChannelSmartScheduleManualRoutingKeepsActiveFixedPrimaryOnTop(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	fixedPriority := int64(100)
	manualPriority := int64(10)
	weight := uint(20)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1403, Name: "固定主渠道", Status: common.ChannelStatusEnabled},
		{Id: 1404, Name: "人工路由", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1403, Group: "vip", Model: "model-a", Enabled: true, Priority: &fixedPriority, Weight: 1000},
		{ChannelId: 1404, Group: "vip", Model: "model-a", Enabled: true, Priority: &manualPriority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: 1403, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1,
			ManualPrimaryUntil: common.GetTimestamp() + 600, ManualPrimarySaved: true,
			ManualPrimarySavedPriority: 10, ManualPrimarySavedWeight: weight,
		},
		{
			ChannelId: 1404, GroupName: "vip", ModelName: "model-a",
			ParticipationSet: true, Excluded: true, Revision: 1,
		},
	}).Error)

	result, err := SaveChannelSmartScheduleManualRouting(1404, "vip", "model-a", 50, 700)
	require.NoError(t, err)
	assert.True(t, result.RoutingChanged)

	var fixed Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1403, Group: "vip", Model: "model-a"}).First(&fixed).Error)
	assert.Equal(t, fixedPriority, abilityPriority(fixed))
	assert.Equal(t, uint(1000), fixed.Weight)
	var fixedState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 1403, GroupName: "vip", ModelName: "model-a",
	}).First(&fixedState).Error)
	assert.Equal(t, int64(1), fixedState.Revision)
	assert.Empty(t, fixedState.LastScheduleError)
}

func TestSaveChannelSmartScheduleManualRoutingIgnoresDisabledChannelWhenReapplyingPrimary(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	fixedPriority := int64(11)
	manualPriority := int64(10)
	disabledPriority := int64(math.MaxInt64)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1411, Name: "固定主渠道", Status: common.ChannelStatusEnabled},
		{Id: 1412, Name: "人工路由", Status: common.ChannelStatusEnabled},
		{Id: 1413, Name: "已禁用最高层", Status: common.ChannelStatusManuallyDisabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1411, Group: "vip", Model: "model-a", Enabled: true, Priority: &fixedPriority, Weight: 1000},
		{ChannelId: 1412, Group: "vip", Model: "model-a", Enabled: true, Priority: &manualPriority, Weight: 20},
		{ChannelId: 1413, Group: "vip", Model: "model-a", Enabled: true, Priority: &disabledPriority, Weight: 1000},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: 1411, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1,
			ManualPrimaryUntil: common.GetTimestamp() + 600, ManualPrimarySaved: true,
			ManualPrimarySavedPriority: 10, ManualPrimarySavedWeight: 20,
		},
		{ChannelId: 1412, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Excluded: true, Revision: 1},
		{ChannelId: 1413, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
	}).Error)

	result, err := SaveChannelSmartScheduleManualRouting(1412, "vip", "model-a", 50, 700)
	require.NoError(t, err)
	assert.True(t, result.RoutingChanged)

	var fixed Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1411, Group: "vip", Model: "model-a"}).First(&fixed).Error)
	assert.Equal(t, int64(51), abilityPriority(fixed))
	assert.Equal(t, uint(1000), fixed.Weight)
}
