package model

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createChannelSmartSchedulePrimaryCleanupFixture(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	fixedPriority := int64(101)
	temporaryPriority := int64(101)
	fixedUntil := common.GetTimestamp() + 600
	require.NoError(t, db.Create(&[]Channel{
		{Id: 4101, Name: "fixed", Status: common.ChannelStatusEnabled},
		{Id: 4102, Name: "temporary", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 4101, Group: "vip", Model: "model-a", Enabled: true, Priority: &fixedPriority, Weight: 1000},
		{ChannelId: 4102, Group: "vip", Model: "model-a", Enabled: true, Priority: &temporaryPriority, Weight: 5},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: 4101, GroupName: "vip", ModelName: "model-a",
			ParticipationSet: true, Revision: 1,
			BasePriority: 80, BaseWeight: 40,
			ManualPrimaryUntil: fixedUntil, ManualPrimarySaved: true,
			ManualPrimarySavedPriority: 80, ManualPrimarySavedWeight: 40,
		},
		{
			ChannelId: 4102, GroupName: "vip", ModelName: "model-a",
			ParticipationSet: true, Revision: 1,
			BasePriority: 90, BaseWeight: 30,
			TemporaryTrafficKind:  ChannelSmartScheduleTemporaryTrafficExploration,
			TemporaryTrafficSince: 100, TemporaryTrafficTargetPercent: 5,
		},
	}).Error)
	return fixedUntil
}

func assertChannelSmartSchedulePrimaryCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	var fixedAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4101, Group: "vip", Model: "model-a",
	}).First(&fixedAbility).Error)
	assert.Equal(t, int64(80), abilityPriority(fixedAbility))
	assert.Equal(t, uint(40), fixedAbility.Weight)

	var temporaryAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4102, Group: "vip", Model: "model-a",
	}).First(&temporaryAbility).Error)
	assert.Equal(t, int64(90), abilityPriority(temporaryAbility))
	assert.Equal(t, uint(30), temporaryAbility.Weight)

	var temporaryState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4102, GroupName: "vip", ModelName: "model-a",
	}).First(&temporaryState).Error)
	assert.Empty(t, temporaryState.TemporaryTrafficKind)
	assert.Zero(t, temporaryState.TemporaryTrafficSince)
	assert.Zero(t, temporaryState.TemporaryTrafficTargetPercent)
}

func TestSaveChannelSmartScheduleRouteConfigReportsParticipationChange(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(80)
	channelPriority := int64(65)
	channelWeight := uint(25)
	require.NoError(t, db.Create(&Channel{
		Id: 4201, Name: "route participation", Status: common.ChannelStatusEnabled,
		Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 4201, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: 40,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 4201, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Excluded: true, Revision: 1,
	}).Error)

	state, routingChanged, err := SaveChannelSmartScheduleRouteConfig(4201, "vip", "model-a", false)
	require.NoError(t, err)
	assert.True(t, routingChanged)
	assert.True(t, state.Participates())

	var ability Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4201, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priority, *ability.Priority)
	assert.Equal(t, uint(40), ability.Weight)
}

func TestSaveChannelSmartScheduleChannelConfigReportsParticipationChange(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(80)
	require.NoError(t, db.Create(&Channel{
		Id: 4202, Name: "channel participation", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 4202, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: 40,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 4202, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Excluded: true, Revision: 1,
	}).Error)

	result, err := SaveChannelSmartScheduleChannelConfig(4202, false)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	assert.Equal(t, 1, result.Updated)
	assert.True(t, result.RoutingChanged)
}

func TestClearChannelSmartScheduleRoutePrimaryRestoresPoolTemporaryTraffic(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	createChannelSmartSchedulePrimaryCleanupFixture(t, db)

	result, err := SaveChannelSmartScheduleRoutePrimary(
		4101, "vip", "model-a", ChannelSmartScheduleRoutePrimaryOptions{},
	)
	require.NoError(t, err)
	assert.True(t, result.RoutingChanged)
	assert.Zero(t, result.State.ManualPrimaryUntil)
	assertChannelSmartSchedulePrimaryCleanup(t, db)
}

func TestClearChannelSmartScheduleRoutePrimaryWithoutFixedIntentKeepsTemporaryTraffic(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	createChannelSmartSchedulePrimaryCleanupFixture(t, db)
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).
		Where(&ChannelSmartScheduleRouteState{
			ChannelId: 4101, GroupName: "vip", ModelName: "model-a",
		}).Updates(map[string]any{
		"manual_primary_until": 0,
		"manual_primary_saved": false,
	}).Error)

	result, err := SaveChannelSmartScheduleRoutePrimary(
		4101, "vip", "model-a", ChannelSmartScheduleRoutePrimaryOptions{},
	)
	require.NoError(t, err)
	assert.False(t, result.RoutingChanged)

	var temporaryAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4102, Group: "vip", Model: "model-a",
	}).First(&temporaryAbility).Error)
	assert.Equal(t, int64(101), abilityPriority(temporaryAbility))
	assert.Equal(t, uint(5), temporaryAbility.Weight)
	var temporaryState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4102, GroupName: "vip", ModelName: "model-a",
	}).First(&temporaryState).Error)
	assert.Equal(t, ChannelSmartScheduleTemporaryTrafficExploration, temporaryState.TemporaryTrafficKind)
}

func TestExcludeChannelSmartScheduleRoutePrimaryRestoresPoolTemporaryTraffic(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	createChannelSmartSchedulePrimaryCleanupFixture(t, db)

	state, routingChanged, err := SaveChannelSmartScheduleRouteConfig(4101, "vip", "model-a", true)
	require.NoError(t, err)
	assert.True(t, routingChanged)
	assert.True(t, state.Excluded)
	assert.Zero(t, state.ManualPrimaryUntil)

	var fixedAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4101, Group: "vip", Model: "model-a",
	}).First(&fixedAbility).Error)
	assert.Nil(t, fixedAbility.Priority)
	assert.Zero(t, fixedAbility.Weight)

	var temporaryAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4102, Group: "vip", Model: "model-a",
	}).First(&temporaryAbility).Error)
	assert.Equal(t, int64(90), abilityPriority(temporaryAbility))
	assert.Equal(t, uint(30), temporaryAbility.Weight)
	var temporaryState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4102, GroupName: "vip", ModelName: "model-a",
	}).First(&temporaryState).Error)
	assert.Empty(t, temporaryState.TemporaryTrafficKind)

}

func TestExpireChannelSmartScheduleRoutePrimaryRestoresPoolTemporaryTraffic(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	fixedUntil := createChannelSmartSchedulePrimaryCleanupFixture(t, db)

	routingChanged, err := ClearExpiredChannelSmartScheduleRoutePrimaries(fixedUntil)
	require.NoError(t, err)
	assert.True(t, routingChanged)
	assertChannelSmartSchedulePrimaryCleanup(t, db)
}

func TestDisableFixedPrimaryWithdrawsTemporaryTrafficAndKeepsIntent(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	fixedUntil := createChannelSmartSchedulePrimaryCleanupFixture(t, db)

	assert.True(t, UpdateChannelStatus(
		4101, "", common.ChannelStatusManuallyDisabled, "管理员禁用固定主渠道",
	))

	var fixedState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4101, GroupName: "vip", ModelName: "model-a",
	}).First(&fixedState).Error)
	assert.Equal(t, fixedUntil, fixedState.ManualPrimaryUntil)
	assert.True(t, fixedState.ManualPrimarySaved)

	var temporaryAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4102, Group: "vip", Model: "model-a",
	}).First(&temporaryAbility).Error)
	assert.Equal(t, int64(90), abilityPriority(temporaryAbility))
	assert.Equal(t, uint(30), temporaryAbility.Weight)
	var temporaryState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4102, GroupName: "vip", ModelName: "model-a",
	}).First(&temporaryState).Error)
	assert.Empty(t, temporaryState.TemporaryTrafficKind)

	assert.True(t, UpdateChannelStatus(4101, "", common.ChannelStatusEnabled, "管理员重新启用固定主渠道"))
	var fixedAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4101, Group: "vip", Model: "model-a",
	}).First(&fixedAbility).Error)
	assert.True(t, fixedAbility.Enabled)
	assert.Greater(t, abilityPriority(fixedAbility), abilityPriority(temporaryAbility))
	assert.Equal(t, uint(1000), fixedAbility.Weight)
}

func TestAutoDisableFixedPrimaryCancelsIntentAndRestoresRouting(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	createChannelSmartSchedulePrimaryCleanupFixture(t, db)

	assert.True(t, UpdateChannelStatus(
		4101, "", common.ChannelStatusAutoDisabled, "自动禁用固定主渠道",
	))

	var fixedState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4101, GroupName: "vip", ModelName: "model-a",
	}).First(&fixedState).Error)
	assert.Zero(t, fixedState.ManualPrimaryUntil)
	assert.False(t, fixedState.ManualPrimarySaved)

	var fixedAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4101, Group: "vip", Model: "model-a",
	}).First(&fixedAbility).Error)
	assert.False(t, fixedAbility.Enabled)
	assert.Equal(t, int64(80), abilityPriority(fixedAbility))
	assert.Equal(t, uint(40), fixedAbility.Weight)

	var temporaryState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4102, GroupName: "vip", ModelName: "model-a",
	}).First(&temporaryState).Error)
	assert.Empty(t, temporaryState.TemporaryTrafficKind)

	assert.True(t, UpdateChannelStatus(
		4101, "", common.ChannelStatusEnabled, "自动恢复固定主渠道",
	))
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4101, GroupName: "vip", ModelName: "model-a",
	}).First(&fixedState).Error)
	assert.Zero(t, fixedState.ManualPrimaryUntil)
	assert.False(t, fixedState.ManualPrimarySaved)
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4101, Group: "vip", Model: "model-a",
	}).First(&fixedAbility).Error)
	assert.True(t, fixedAbility.Enabled)
	assert.Equal(t, int64(80), abilityPriority(fixedAbility))
	assert.Equal(t, uint(40), fixedAbility.Weight)
}

func TestDisableFixedPrimaryAbilityWithdrawsTemporaryTrafficAndKeepsIntent(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	fixedUntil := createChannelSmartSchedulePrimaryCleanupFixture(t, db)

	require.NoError(t, UpdateAbilityStatus(4101, false))

	var fixedState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4101, GroupName: "vip", ModelName: "model-a",
	}).First(&fixedState).Error)
	assert.Equal(t, fixedUntil, fixedState.ManualPrimaryUntil)
	var temporaryState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4102, GroupName: "vip", ModelName: "model-a",
	}).First(&temporaryState).Error)
	assert.Empty(t, temporaryState.TemporaryTrafficKind)
}

func TestDisableUnrelatedChannelWithoutFixedIntentKeepsTemporaryTraffic(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	temporaryPriority := int64(101)
	otherPriority := int64(80)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 4301, Name: "temporary", Status: common.ChannelStatusEnabled},
		{Id: 4302, Name: "unrelated", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 4301, Group: "vip", Model: "model-a", Enabled: true, Priority: &temporaryPriority, Weight: 5},
		{ChannelId: 4302, Group: "vip", Model: "model-a", Enabled: true, Priority: &otherPriority, Weight: 20},
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 4301, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		BasePriority: 90, BaseWeight: 30,
		TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficExploration,
	}).Error)

	assert.True(t, UpdateChannelStatus(
		4302, "", common.ChannelStatusManuallyDisabled, "管理员禁用无关渠道",
	))

	var temporaryAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4301, Group: "vip", Model: "model-a",
	}).First(&temporaryAbility).Error)
	assert.Equal(t, temporaryPriority, abilityPriority(temporaryAbility))
	assert.Equal(t, uint(5), temporaryAbility.Weight)
	var temporaryState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4301, GroupName: "vip", ModelName: "model-a",
	}).First(&temporaryState).Error)
	assert.Equal(t, ChannelSmartScheduleTemporaryTrafficExploration, temporaryState.TemporaryTrafficKind)
}

func TestDeleteFixedPrimaryWithdrawsTemporaryTraffic(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	createChannelSmartSchedulePrimaryCleanupFixture(t, db)

	deleted, err := BatchDeleteChannels([]int{4101})
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	var fixedStateCount int64
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).
		Where("channel_id = ?", 4101).
		Count(&fixedStateCount).Error)
	assert.Zero(t, fixedStateCount)
	var temporaryAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4102, Group: "vip", Model: "model-a",
	}).First(&temporaryAbility).Error)
	assert.Equal(t, int64(90), abilityPriority(temporaryAbility))
	assert.Equal(t, uint(30), temporaryAbility.Weight)
	var temporaryState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4102, GroupName: "vip", ModelName: "model-a",
	}).First(&temporaryState).Error)
	assert.Empty(t, temporaryState.TemporaryTrafficKind)
}

func TestMoveFixedPrimaryWithdrawsTemporaryTrafficFromOldPool(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	createChannelSmartSchedulePrimaryCleanupFixture(t, db)

	var fixedChannel Channel
	require.NoError(t, db.First(&fixedChannel, 4101).Error)
	fixedChannel.Group = "other"
	fixedChannel.Models = "model-a"
	priority := int64(80)
	weight := uint(40)
	fixedChannel.Priority = &priority
	fixedChannel.Weight = &weight
	require.NoError(t, fixedChannel.Update())

	var oldFixedStateCount int64
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).
		Where(&ChannelSmartScheduleRouteState{
			ChannelId: 4101, GroupName: "vip", ModelName: "model-a",
		}).Count(&oldFixedStateCount).Error)
	assert.Zero(t, oldFixedStateCount)
	var temporaryAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4102, Group: "vip", Model: "model-a",
	}).First(&temporaryAbility).Error)
	assert.Equal(t, int64(90), abilityPriority(temporaryAbility))
	assert.Equal(t, uint(30), temporaryAbility.Weight)
	var temporaryState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4102, GroupName: "vip", ModelName: "model-a",
	}).First(&temporaryState).Error)
	assert.Empty(t, temporaryState.TemporaryTrafficKind)
}

func TestClearChannelSmartScheduleRoutePrimaryKeepsTemporaryTrafficForAnotherActivePrimary(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	fixedUntil := createChannelSmartSchedulePrimaryCleanupFixture(t, db)
	otherPriority := int64(102)
	require.NoError(t, db.Create(&Channel{
		Id: 4103, Name: "other fixed", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 4103, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &otherPriority, Weight: 1000,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 4103, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		ManualPrimaryUntil: fixedUntil, ManualPrimarySaved: true,
		ManualPrimarySavedPriority: 70, ManualPrimarySavedWeight: 20,
	}).Error)

	_, err := SaveChannelSmartScheduleRoutePrimary(
		4101, "vip", "model-a", ChannelSmartScheduleRoutePrimaryOptions{},
	)
	require.NoError(t, err)

	var temporaryAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4102, Group: "vip", Model: "model-a",
	}).First(&temporaryAbility).Error)
	assert.Equal(t, int64(101), abilityPriority(temporaryAbility))
	assert.Equal(t, uint(5), temporaryAbility.Weight)
	var temporaryState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4102, GroupName: "vip", ModelName: "model-a",
	}).First(&temporaryState).Error)
	assert.Equal(t, ChannelSmartScheduleTemporaryTrafficExploration, temporaryState.TemporaryTrafficKind)
}

func TestClearChannelSmartScheduleRoutePrimaryRollsBackPoolCleanupFailure(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	fixedUntil := createChannelSmartSchedulePrimaryCleanupFixture(t, db)
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).
		Where(&ChannelSmartScheduleRouteState{
			ChannelId: 4102, GroupName: "vip", ModelName: "model-a",
		}).Update("revision", int64(math.MaxInt64)).Error)

	_, err := SaveChannelSmartScheduleRoutePrimary(
		4101, "vip", "model-a", ChannelSmartScheduleRoutePrimaryOptions{},
	)
	require.ErrorContains(t, err, "修订号已达上限")

	var fixedAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4101, Group: "vip", Model: "model-a",
	}).First(&fixedAbility).Error)
	assert.Equal(t, int64(101), abilityPriority(fixedAbility))
	assert.Equal(t, uint(1000), fixedAbility.Weight)
	var fixedState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4101, GroupName: "vip", ModelName: "model-a",
	}).First(&fixedState).Error)
	assert.Equal(t, fixedUntil, fixedState.ManualPrimaryUntil)
	assert.True(t, fixedState.ManualPrimarySaved)

	var temporaryState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4102, GroupName: "vip", ModelName: "model-a",
	}).First(&temporaryState).Error)
	assert.Equal(t, ChannelSmartScheduleTemporaryTrafficExploration, temporaryState.TemporaryTrafficKind)
	assert.Equal(t, int64(math.MaxInt64), temporaryState.Revision)
}
