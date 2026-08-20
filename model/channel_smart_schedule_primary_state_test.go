package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveChannelSmartScheduleRoutePrimaryIgnoresAbilityWithoutRouteState(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	targetPriority := int64(10)
	unmanagedPriority := int64(500)
	weight := uint(100)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 3181, Name: "固定目标", Status: common.ChannelStatusEnabled},
		{Id: 3182, Name: "新人工路由", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 3181, Group: "vip", Model: "model-a", Enabled: true, Priority: &targetPriority, Weight: weight},
		{ChannelId: 3182, Group: "vip", Model: "model-a", Enabled: true, Priority: &unmanagedPriority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 3181, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
	}).Error)

	result, err := SaveChannelSmartScheduleRoutePrimary(
		3181,
		"vip",
		"model-a",
		ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 10},
	)
	require.NoError(t, err)
	assert.True(t, result.RoutingChanged)

	var ability Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 3181, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	assert.Equal(t, targetPriority, abilityPriority(ability))
	assert.Equal(t, uint(1000), ability.Weight)
}

func TestClearChannelSmartScheduleRouteStabilityReappliesActivePrimary(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	degradedPriority := int64(0)
	backupPriority := int64(90)
	basePriority := int64(500)
	baseWeight := uint(100)
	fixedUntil := time.Now().Add(10 * time.Minute).Unix()
	require.NoError(t, db.Create(&[]Channel{
		{Id: 3183, Name: "恢复中的固定渠道", Status: common.ChannelStatusEnabled},
		{Id: 3184, Name: "临时主渠道", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 3183, Group: "vip", Model: "model-a", Enabled: true, Priority: &degradedPriority, Weight: 0},
		{ChannelId: 3184, Group: "vip", Model: "model-a", Enabled: true, Priority: &backupPriority, Weight: baseWeight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: 3183, GroupName: "vip", ModelName: "model-a",
			ParticipationSet: true, Revision: 1,
			StabilityState:                     ChannelSmartScheduleStabilityDegraded,
			StabilitySavedPriority:             basePriority,
			StabilitySavedWeight:               baseWeight,
			ManualPrimaryUntil:                 fixedUntil,
			ManualPrimaryAllowStabilityDegrade: true,
			ManualPrimarySaved:                 true,
			ManualPrimarySavedPriority:         basePriority,
			ManualPrimarySavedWeight:           baseWeight,
		},
		{ChannelId: 3184, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
	}).Error)

	result, err := ClearChannelSmartScheduleRouteStability(3183, "vip", "model-a", 80, 10)
	require.NoError(t, err)
	assert.True(t, result.Cleared)
	assert.True(t, result.RoutingChanged)
	assert.Equal(t, basePriority, result.Priority)
	assert.Equal(t, uint(1000), result.Weight)

	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 3183, GroupName: "vip", ModelName: "model-a",
	}).First(&state).Error)
	assert.Empty(t, state.StabilityState)
	assert.Equal(t, fixedUntil, state.ManualPrimaryUntil)
	assert.Equal(t, basePriority, state.ManualPrimarySavedPriority)
	assert.Equal(t, baseWeight, state.ManualPrimarySavedWeight)

	routingChanged, err := ClearExpiredChannelSmartScheduleRoutePrimaries(fixedUntil)
	require.NoError(t, err)
	assert.True(t, routingChanged)
	var ability Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 3183, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	assert.Equal(t, basePriority, abilityPriority(ability))
	assert.Equal(t, baseWeight, ability.Weight)
}

func TestUpdateAbilitiesKeepsActivePrimaryAboveParticipatingRoutes(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	fixedPriority := int64(10)
	newRoutePriority := int64(500)
	weight := uint(100)
	fixedChannel := Channel{
		Id: 3185, Name: "已固定渠道", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &fixedPriority, Weight: &weight,
	}
	newRouteChannel := Channel{
		Id: 3186, Name: "新增高优先级路由", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-b", Priority: &newRoutePriority, Weight: &weight,
	}
	require.NoError(t, db.Create(&[]Channel{fixedChannel, newRouteChannel}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 3185, Group: "vip", Model: "model-a", Enabled: true, Priority: &fixedPriority, Weight: weight},
		{ChannelId: 3186, Group: "vip", Model: "model-b", Enabled: true, Priority: &newRoutePriority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 3185, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{ChannelId: 3186, GroupName: "vip", ModelName: "model-b", ParticipationSet: true, Excluded: true, Revision: 1},
	}).Error)
	_, err := SaveChannelSmartScheduleRoutePrimary(
		3185,
		"vip",
		"model-a",
		ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 10},
	)
	require.NoError(t, err)

	newRouteChannel.Models = "model-a"
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", newRouteChannel.Id).
		Update("models", newRouteChannel.Models).Error)
	require.NoError(t, newRouteChannel.UpdateAbilities(nil))

	var abilities []Ability
	require.NoError(t, db.Where(&Ability{Group: "vip", Model: "model-a"}).
		Order("channel_id ASC").Find(&abilities).Error)
	require.Len(t, abilities, 2)
	assert.Equal(t, fixedPriority, abilityPriority(abilities[0]))
	assert.Equal(t, uint(1000), abilities[0].Weight)
	assert.Nil(t, abilities[1].Priority)
	assert.Zero(t, abilities[1].Weight)
	priority, effectiveWeight := channelSmartScheduleAbilityRouting(abilities[1])
	assert.Zero(t, priority)
	assert.Zero(t, effectiveWeight)
}

func TestUpdateAbilityStatusKeepsActivePrimaryAboveEnabledParticipatingRoute(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	fixedPriority := int64(10)
	disabledPriority := int64(500)
	weight := uint(100)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 3187, Name: "已固定渠道", Status: common.ChannelStatusEnabled},
		{Id: 3188, Name: "待启用高优先级路由", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 3187, Group: "vip", Model: "model-a", Enabled: true, Priority: &fixedPriority, Weight: weight},
		{ChannelId: 3188, Group: "vip", Model: "model-a", Enabled: false, Priority: &disabledPriority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 3187, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{ChannelId: 3188, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Excluded: true, Revision: 1},
	}).Error)
	_, err := SaveChannelSmartScheduleRoutePrimary(
		3187,
		"vip",
		"model-a",
		ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 10},
	)
	require.NoError(t, err)

	require.NoError(t, UpdateAbilityStatus(3188, true))

	var abilities []Ability
	require.NoError(t, db.Where(&Ability{Group: "vip", Model: "model-a"}).
		Order("channel_id ASC").Find(&abilities).Error)
	require.Len(t, abilities, 2)
	assert.Equal(t, fixedPriority, abilityPriority(abilities[0]))
	assert.Equal(t, uint(1000), abilities[0].Weight)
	assert.True(t, abilities[1].Enabled)
	assert.Equal(t, disabledPriority, abilityPriority(abilities[1]))
}
