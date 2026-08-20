package model

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupChannelSmartScheduleRouteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := DB
	originalLogDB := LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	require.NoError(t, db.AutoMigrate(
		&Option{},
		&Channel{},
		&Ability{},
		&ChannelRatioMonitor{},
		&ChannelSmartScheduleRouteState{},
		&ChannelSmartScheduleGroupPause{},
		&ChannelSmartScheduleModelSampleState{},
	))
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func TestChannelSmartScheduleSamplesJSONUsesCrossDatabaseTextType(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
		want      string
	}{
		{name: "sqlite", dialector: sqlite.Open(":memory:"), want: "TEXT"},
		{name: "mysql", dialector: mysql.Open(""), want: "LONGTEXT"},
		{name: "postgres", dialector: postgres.Open(""), want: "TEXT"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := &gorm.DB{Config: &gorm.Config{Dialector: test.dialector}}
			assert.Equal(t, test.want, ChannelSmartScheduleSamplesJSON("").GormDBDataType(db, nil))
		})
	}
}

func TestUpdateAbilitiesPreservesExistingSmartScheduleRouteRouting(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	defaultPriority := int64(80)
	defaultWeight := uint(50)
	degradedPriority := int64(0)
	stalePriority := int64(95)
	channel := Channel{
		Id: 1001, Name: "scheduled", Status: common.ChannelStatusEnabled,
		Group: "vip,standard,new", Models: "model-a", Priority: &defaultPriority, Weight: &defaultWeight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true, Priority: &degradedPriority, Weight: 0},
		{ChannelId: channel.Id, Group: "standard", Model: "model-a", Enabled: true, Priority: &stalePriority, Weight: 70},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: channel.Id, GroupName: "standard", ModelName: "model-a", ParticipationSet: true, Excluded: true},
		{ChannelId: channel.Id, GroupName: "new", ModelName: "model-a", ParticipationSet: true},
	}).Error)

	require.NoError(t, channel.UpdateAbilities(nil))
	var abilities []Ability
	require.NoError(t, db.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	require.Len(t, abilities, 3)
	byGroup := make(map[string]Ability, len(abilities))
	for _, ability := range abilities {
		byGroup[ability.Group] = ability
	}
	require.NotNil(t, byGroup["vip"].Priority)
	assert.Equal(t, int64(0), *byGroup["vip"].Priority)
	assert.Zero(t, byGroup["vip"].Weight)
	assert.Nil(t, byGroup["standard"].Priority)
	assert.Zero(t, byGroup["standard"].Weight)
	assert.Nil(t, byGroup["new"].Priority)
	assert.Zero(t, byGroup["new"].Weight)
}

func TestUpdateAbilitiesNewGroupParticipatesInSmartSchedule(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(80)
	weight := uint(50)
	channel := Channel{
		Id: 1004, Name: "new group", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "model-a,model-b", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: channel.Id, Group: "default", Model: "model-a", Enabled: true},
		{ChannelId: channel.Id, Group: "default", Model: "model-b", Enabled: true},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: channel.Id, GroupName: "default", ModelName: "model-a", ParticipationSet: true, Excluded: true},
		{ChannelId: channel.Id, GroupName: "default", ModelName: "model-b", ParticipationSet: true, Excluded: true},
	}).Error)

	channel.Group = "default,vip"
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Update("group", channel.Group).Error)
	require.NoError(t, channel.UpdateAbilities(nil))

	var states []ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("channel_id = ?", channel.Id).
		Order("group_name ASC, model_name ASC").Find(&states).Error)
	require.Len(t, states, 4)
	for _, state := range states {
		if state.GroupName == "default" {
			assert.True(t, state.Excluded)
			continue
		}
		assert.Equal(t, "vip", state.GroupName)
		assert.True(t, state.ParticipationSet)
		assert.False(t, state.Excluded)
		assert.Equal(t, int64(1), state.Revision)
	}

	var abilities []Ability
	require.NoError(t, db.Where("channel_id = ? AND "+commonGroupCol+" = ?", channel.Id, "vip").
		Order("model ASC").Find(&abilities).Error)
	require.Len(t, abilities, 2)
	for _, ability := range abilities {
		assert.Nil(t, ability.Priority)
		assert.Zero(t, ability.Weight)
	}
}

func TestUpdateAbilitiesDoesNotCreateAbilitiesForDeletedChannel(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(80)
	weight := uint(50)
	staleChannel := Channel{
		Id: 1003, Name: "deleted channel", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&staleChannel).Error)
	require.NoError(t, db.Delete(&Channel{}, staleChannel.Id).Error)

	err := staleChannel.UpdateAbilities(nil)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	var abilityCount int64
	require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", staleChannel.Id).Count(&abilityCount).Error)
	assert.Zero(t, abilityCount)
}

func TestSaveChannelSmartScheduleRoutePrimarySwitchesAndRestoresRouting(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(80)
	for _, channelID := range []int{3101, 3102} {
		channelWeight := uint(100)
		channel := Channel{Id: channelID, Name: fmt.Sprintf("primary-%d", channelID),
			Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a",
			Priority: &priority, Weight: &channelWeight}
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, db.Create(&ChannelRatioMonitor{
			ChannelId: channelID, UpstreamRevision: 1,
		}).Error)
		abilityPriority := int64(80)
		require.NoError(t, db.Create(&Ability{ChannelId: channelID, Group: "vip", Model: "model-a",
			Enabled: true, Priority: &abilityPriority, Weight: 100}).Error)
		require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{ChannelId: channelID,
			GroupName: "vip", ModelName: "model-a", ParticipationSet: true}).Error)
	}
	first, err := SaveChannelSmartScheduleRoutePrimary(3101, "vip", "model-a", ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 10})
	require.NoError(t, err)
	assert.Greater(t, first.State.ManualPrimaryUntil, common.GetTimestamp())
	assert.True(t, first.State.ManualPrimarySaved)
	var firstAbility Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 3101, Group: "vip", Model: "model-a"}).First(&firstAbility).Error)
	assert.Equal(t, int64(81), abilityPriority(firstAbility))
	assert.Equal(t, uint(1000), firstAbility.Weight)
	rescheduledPriority := int64(100)
	require.NoError(t, db.Model(&Ability{}).
		Where(&Ability{ChannelId: 3101, Group: "vip", Model: "model-a"}).
		Updates(map[string]any{"priority": rescheduledPriority, "weight": uint(900)}).Error)
	extended, err := SaveChannelSmartScheduleRoutePrimary(3101, "vip", "model-a", ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 20})
	require.NoError(t, err)
	assert.True(t, extended.RoutingChanged)
	assert.GreaterOrEqual(t, extended.State.ManualPrimaryUntil, first.State.ManualPrimaryUntil)
	assert.Equal(t, int64(80), extended.State.ManualPrimarySavedPriority)
	assert.Equal(t, uint(100), extended.State.ManualPrimarySavedWeight)
	require.NoError(t, db.Where(&Ability{ChannelId: 3101, Group: "vip", Model: "model-a"}).First(&firstAbility).Error)
	assert.Equal(t, rescheduledPriority, abilityPriority(firstAbility))
	assert.Equal(t, uint(1000), firstAbility.Weight)

	changed, revisionCurrent, err := UpdateChannelMonitorStatusIfCurrentRevision(
		3102,
		1,
		common.ChannelStatusEnabled,
		"",
		common.ChannelStatusAutoDisabled,
		"自动监控判定渠道不可用",
	)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.True(t, revisionCurrent)
	_, err = SaveChannelSmartScheduleRoutePrimary(3102, "vip", "model-a", ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 10})
	require.ErrorContains(t, err, "渠道已禁用")
	var fixedAfterRejectedSwitch ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{ChannelId: 3101,
		GroupName: "vip", ModelName: "model-a"}).First(&fixedAfterRejectedSwitch).Error)
	assert.Greater(t, fixedAfterRejectedSwitch.ManualPrimaryUntil, common.GetTimestamp())
	var abilityAfterRejectedSwitch Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 3101, Group: "vip", Model: "model-a"}).
		First(&abilityAfterRejectedSwitch).Error)
	assert.Equal(t, rescheduledPriority, abilityPriority(abilityAfterRejectedSwitch))
	assert.Equal(t, uint(1000), abilityAfterRejectedSwitch.Weight)
	changed, revisionCurrent, err = UpdateChannelMonitorStatusIfCurrentRevision(
		3102,
		1,
		common.ChannelStatusAutoDisabled,
		"自动监控判定渠道不可用",
		common.ChannelStatusEnabled,
		"自动监控判定渠道已恢复",
	)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.True(t, revisionCurrent)

	second, err := SaveChannelSmartScheduleRoutePrimary(3102, "vip", "model-a", ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 10})
	require.NoError(t, err)
	assert.Greater(t, second.State.ManualPrimaryUntil, common.GetTimestamp())
	var restoredFirst ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{ChannelId: 3101,
		GroupName: "vip", ModelName: "model-a"}).First(&restoredFirst).Error)
	assert.Zero(t, restoredFirst.ManualPrimaryUntil)
	assert.False(t, restoredFirst.ManualPrimarySaved)
	var firstAfterSwitch Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 3101, Group: "vip", Model: "model-a"}).First(&firstAfterSwitch).Error)
	assert.Equal(t, int64(80), abilityPriority(firstAfterSwitch))
	assert.Equal(t, uint(100), firstAfterSwitch.Weight)

	cleared, err := SaveChannelSmartScheduleRoutePrimary(3102, "vip", "model-a", ChannelSmartScheduleRoutePrimaryOptions{})
	require.NoError(t, err)
	assert.Zero(t, cleared.State.ManualPrimaryUntil)
	var secondAfterClear Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 3102, Group: "vip", Model: "model-a"}).First(&secondAfterClear).Error)
	assert.Equal(t, int64(80), abilityPriority(secondAfterClear))
	assert.Equal(t, uint(100), secondAfterClear.Weight)

	expiring, err := SaveChannelSmartScheduleRoutePrimary(3101, "vip", "model-a", ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 1})
	require.NoError(t, err)
	routingChanged, err := ClearExpiredChannelSmartScheduleRoutePrimaries(expiring.State.ManualPrimaryUntil)
	require.NoError(t, err)
	assert.True(t, routingChanged)
	var expiredState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{ChannelId: 3101,
		GroupName: "vip", ModelName: "model-a"}).First(&expiredState).Error)
	assert.Zero(t, expiredState.ManualPrimaryUntil)
}

func TestSaveChannelSmartScheduleRoutePrimaryIgnoresDisabledChannelPriority(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	targetPriority := int64(10)
	disabledPriority := int64(math.MaxInt64)
	reachablePriority := int64(20)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 3111, Name: "固定目标", Status: common.ChannelStatusEnabled},
		{Id: 3112, Name: "已禁用最高层", Status: common.ChannelStatusManuallyDisabled},
		{Id: 3113, Name: "可用人工路由", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 3111, Group: "vip", Model: "model-a", Enabled: true, Priority: &targetPriority, Weight: 10},
		{ChannelId: 3112, Group: "vip", Model: "model-a", Enabled: true, Priority: &disabledPriority, Weight: 1000},
		{ChannelId: 3113, Group: "vip", Model: "model-a", Enabled: true, Priority: &reachablePriority, Weight: 100},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 3111, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{ChannelId: 3112, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{ChannelId: 3113, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
	}).Error)

	result, err := SaveChannelSmartScheduleRoutePrimary(
		3111,
		"vip",
		"model-a",
		ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 10},
	)
	require.NoError(t, err)
	assert.True(t, result.RoutingChanged)

	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 3111, Group: "vip", Model: "model-a"}).First(&ability).Error)
	assert.Equal(t, int64(21), abilityPriority(ability))
	assert.Equal(t, uint(1000), ability.Weight)
}

func TestSaveChannelSmartScheduleRoutePrimaryRequiresConfirmationToClearStabilityProtection(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	channelPriority := int64(80)
	channelWeight := uint(50)
	degradedPriority := int64(0)
	restoredPriority := int64(90)
	restoredWeight := uint(60)
	require.NoError(t, db.Create(&Channel{
		Id: 3103, Name: "protected primary", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 3103, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &degradedPriority, Weight: 0,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 3103, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		StabilityState:         ChannelSmartScheduleStabilityDegraded,
		StabilityUntil:         common.GetTimestamp() + 300,
		StabilitySavedPriority: restoredPriority,
		StabilitySavedWeight:   restoredWeight,
		RuntimeProtectionUntil: common.GetTimestamp() + 300,
	}).Error)

	_, err := SaveChannelSmartScheduleRoutePrimary(
		3103,
		"vip",
		"model-a",
		ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 10},
	)
	require.ErrorIs(t, err, ErrChannelSmartScheduleRouteStabilityProtected)

	var protectedState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 3103, GroupName: "vip", ModelName: "model-a",
	}).First(&protectedState).Error)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, protectedState.StabilityState)
	assert.Zero(t, protectedState.ManualPrimaryUntil)

	fixed, err := SaveChannelSmartScheduleRoutePrimary(
		3103,
		"vip",
		"model-a",
		ChannelSmartScheduleRoutePrimaryOptions{
			DurationMinutes:           10,
			AllowStabilityDegrade:     true,
			ConfirmStabilityOverride:  true,
			StabilityFallbackPriority: channelPriority,
			StabilityFallbackWeight:   10,
		},
	)
	require.NoError(t, err)
	assert.True(t, fixed.StabilityProtectionCleared)
	assert.Empty(t, fixed.State.StabilityState)
	assert.True(t, fixed.State.ManualPrimaryAllowStabilityDegrade)
	assert.Greater(t, fixed.State.ManualPrimaryUntil, common.GetTimestamp())
	assert.Equal(t, restoredPriority, fixed.State.ManualPrimarySavedPriority)
	assert.Equal(t, restoredWeight, fixed.State.ManualPrimarySavedWeight)

	var ability Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 3103, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	assert.Equal(t, restoredPriority, abilityPriority(ability))
	assert.Equal(t, uint(1000), ability.Weight)
}

func TestClearAndExpireChannelSmartScheduleRoutePrimaryRebaseActiveStabilityRestoreTarget(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	degradedPriority := int64(0)
	channelPriority := int64(80)
	channelWeight := uint(100)
	restoredPriority := int64(80)
	restoredWeight := uint(100)
	require.NoError(t, db.Create(&Channel{
		Id: 3111, Name: "degraded fixed primary", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 3111, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &degradedPriority, Weight: 0,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 3111, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		StabilityState:         ChannelSmartScheduleStabilityDegraded,
		StabilityUntil:         common.GetTimestamp() + 300,
		StabilitySavedPriority: 81, StabilitySavedWeight: 1000,
		ManualPrimaryUntil:                 common.GetTimestamp() + 600,
		ManualPrimaryAllowStabilityDegrade: true,
		ManualPrimarySaved:                 true,
		ManualPrimarySavedPriority:         restoredPriority,
		ManualPrimarySavedWeight:           restoredWeight,
	}).Error)

	extended, err := SaveChannelSmartScheduleRoutePrimary(
		3111, "vip", "model-a", ChannelSmartScheduleRoutePrimaryOptions{
			DurationMinutes: 20, AllowStabilityDegrade: true,
		},
	)
	require.NoError(t, err)
	assert.False(t, extended.RoutingChanged)
	assert.Greater(t, extended.State.ManualPrimaryUntil, common.GetTimestamp()+600)
	assert.True(t, extended.State.ManualPrimaryAllowStabilityDegrade)
	assert.Equal(t, restoredPriority, extended.State.ManualPrimarySavedPriority)
	assert.Equal(t, restoredWeight, extended.State.ManualPrimarySavedWeight)
	assert.Equal(t, int64(81), extended.State.StabilitySavedPriority)
	assert.Equal(t, uint(1000), extended.State.StabilitySavedWeight)

	cleared, err := SaveChannelSmartScheduleRoutePrimary(
		3111, "vip", "model-a", ChannelSmartScheduleRoutePrimaryOptions{},
	)
	require.NoError(t, err)
	assert.False(t, cleared.RoutingChanged)
	assert.Zero(t, cleared.State.ManualPrimaryUntil)
	assert.False(t, cleared.State.ManualPrimaryAllowStabilityDegrade)
	assert.Equal(t, restoredPriority, cleared.State.StabilitySavedPriority)
	assert.Equal(t, restoredWeight, cleared.State.StabilitySavedWeight)

	stabilityResult, err := ClearChannelSmartScheduleRouteStability(3111, "vip", "model-a", 80, 10)
	require.NoError(t, err)
	assert.True(t, stabilityResult.Cleared)
	assert.Equal(t, restoredPriority, stabilityResult.Priority)
	assert.Equal(t, restoredWeight, stabilityResult.Weight)
	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 3111, Group: "vip", Model: "model-a"}).First(&ability).Error)
	assert.Equal(t, restoredPriority, abilityPriority(ability))
	assert.Equal(t, restoredWeight, ability.Weight)

	expiredAt := common.GetTimestamp() + 60
	require.NoError(t, db.Model(&Ability{}).
		Where(&Ability{ChannelId: 3111, Group: "vip", Model: "model-a"}).
		Updates(map[string]any{"priority": degradedPriority, "weight": uint(0)}).Error)
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).
		Where(&ChannelSmartScheduleRouteState{ChannelId: 3111, GroupName: "vip", ModelName: "model-a"}).
		Updates(map[string]any{
			"stability_state":                        ChannelSmartScheduleStabilityDegraded,
			"stability_until":                        expiredAt + 300,
			"stability_saved_priority":               int64(81),
			"stability_saved_weight":                 uint(1000),
			"manual_primary_until":                   expiredAt,
			"manual_primary_allow_stability_degrade": true,
			"manual_primary_saved":                   true,
			"manual_primary_saved_priority":          restoredPriority,
			"manual_primary_saved_weight":            restoredWeight,
		}).Error)

	routingChanged, err := ClearExpiredChannelSmartScheduleRoutePrimaries(expiredAt)
	require.NoError(t, err)
	assert.False(t, routingChanged)
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 3111, GroupName: "vip", ModelName: "model-a",
	}).First(&cleared.State).Error)
	assert.Zero(t, cleared.State.ManualPrimaryUntil)
	assert.False(t, cleared.State.ManualPrimaryAllowStabilityDegrade)
	assert.Equal(t, restoredPriority, cleared.State.StabilitySavedPriority)
	assert.Equal(t, restoredWeight, cleared.State.StabilitySavedWeight)
}

func TestUpdateAbilitiesRemovesDeletedRouteScheduleState(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	defaultPriority := int64(80)
	defaultWeight := uint(50)
	degradedPriority := int64(0)
	channel := Channel{
		Id: 1011, Name: "route-lifecycle", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &defaultPriority, Weight: &defaultWeight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &degradedPriority, Weight: 0,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, StabilityState: ChannelSmartScheduleStabilityDegraded,
		StabilitySavedPriority: 95, StabilitySavedWeight: 70,
		BaseRank: 2, BasePriority: 95, BaseWeight: 70,
		TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficExploration,
	}).Error)

	channel.Group = "standard"
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Update("group", channel.Group).Error)
	require.NoError(t, channel.UpdateAbilities(nil))
	var stateCount int64
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", channel.Id, "vip", "model-a",
	).Count(&stateCount).Error)
	assert.Zero(t, stateCount)

	channel.Group = "vip"
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Update("group", channel.Group).Error)
	require.NoError(t, channel.UpdateAbilities(nil))
	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: channel.Id, Group: "vip", Model: "model-a"}).First(&ability).Error)
	assert.Nil(t, ability.Priority)
	assert.Zero(t, ability.Weight)

	var recreatedState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a",
	}).First(&recreatedState).Error)
	assert.True(t, recreatedState.Participates())
	assert.Empty(t, recreatedState.StabilityState)
	assert.Empty(t, recreatedState.TemporaryTrafficKind)
}

func TestFixAbilityClearsNonparticipatingRouteRouting(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	defaultPriority := int64(80)
	defaultWeight := uint(50)
	degradedPriority := int64(0)
	stalePriority := int64(95)
	channel := Channel{
		Id: 1012, Name: "fix-route-lifecycle", Status: common.ChannelStatusEnabled,
		Group: "vip,standard", Models: "model-a", Priority: &defaultPriority, Weight: &defaultWeight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true, Priority: &degradedPriority, Weight: 0},
		{ChannelId: channel.Id, Group: "standard", Model: "model-a", Enabled: true, Priority: &stalePriority, Weight: 70},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
			LastScheduleTime: 123, LastSchedulePriority: 0, LastScheduleWeight: 0,
			StabilityState: ChannelSmartScheduleStabilityDegraded, StabilitySavedPriority: 95, StabilitySavedWeight: 70,
		},
		{ChannelId: channel.Id, GroupName: "standard", ModelName: "model-a", ParticipationSet: true, Excluded: true},
		{ChannelId: channel.Id, GroupName: "removed", ModelName: "model-a", ParticipationSet: true},
	}).Error)

	success, failed, err := FixAbility()
	require.NoError(t, err)
	assert.Equal(t, 1, success)
	assert.Zero(t, failed)
	var abilities []Ability
	require.NoError(t, db.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	require.Len(t, abilities, 2)
	byGroup := make(map[string]Ability, len(abilities))
	for _, ability := range abilities {
		byGroup[ability.Group] = ability
	}
	require.NotNil(t, byGroup["vip"].Priority)
	assert.Zero(t, *byGroup["vip"].Priority)
	assert.Zero(t, byGroup["vip"].Weight)
	assert.Nil(t, byGroup["standard"].Priority)
	assert.Zero(t, byGroup["standard"].Weight)

	var routeState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a",
	}).First(&routeState).Error)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, routeState.StabilityState)
	var staleStateCount int64
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", channel.Id, "removed", "model-a",
	).Count(&staleStateCount).Error)
	assert.Zero(t, staleStateCount)
}

func TestEditChannelByTagClearsNonparticipatingRouteRouting(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	defaultPriority := int64(80)
	defaultWeight := uint(50)
	degradedPriority := int64(0)
	stalePriority := int64(95)
	tag := "bulk-routing"
	channel := Channel{
		Id: 1013, Name: "bulk-routing", Status: common.ChannelStatusEnabled,
		Group: "vip,standard", Models: "model-a", Priority: &defaultPriority, Weight: &defaultWeight, Tag: &tag,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true, Priority: &degradedPriority, Weight: 0, Tag: &tag},
		{ChannelId: channel.Id, Group: "standard", Model: "model-a", Enabled: true, Priority: &stalePriority, Weight: 70, Tag: &tag},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
			StabilityState: ChannelSmartScheduleStabilityDegraded, StabilitySavedPriority: 95, StabilitySavedWeight: 70,
		},
		{ChannelId: channel.Id, GroupName: "standard", ModelName: "model-a", ParticipationSet: true, Excluded: true},
	}).Error)
	updatedPriority := int64(65)
	updatedWeight := uint(25)

	require.NoError(t, EditChannelByTag(tag, nil, nil, nil, nil, &updatedPriority, &updatedWeight, nil, nil))
	var storedChannel Channel
	require.NoError(t, db.First(&storedChannel, channel.Id).Error)
	assert.Equal(t, updatedPriority, storedChannel.GetPriority())
	assert.Equal(t, int(updatedWeight), storedChannel.GetWeight())
	var abilities []Ability
	require.NoError(t, db.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	byGroup := make(map[string]Ability, len(abilities))
	for _, ability := range abilities {
		byGroup[ability.Group] = ability
	}
	require.NotNil(t, byGroup["vip"].Priority)
	assert.Zero(t, *byGroup["vip"].Priority)
	assert.Zero(t, byGroup["vip"].Weight)
	assert.Nil(t, byGroup["standard"].Priority)
	assert.Zero(t, byGroup["standard"].Weight)
	var routeState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a",
	}).First(&routeState).Error)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, routeState.StabilityState)
}

func TestBatchSetChannelTagKeepsParticipatingRouteRouting(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	defaultPriority := int64(80)
	defaultWeight := uint(50)
	degradedPriority := int64(0)
	oldTag := "old-tag"
	newTag := "new-tag"
	channel := Channel{
		Id: 1014, Name: "batch-tag-route", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &defaultPriority, Weight: &defaultWeight, Tag: &oldTag,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &degradedPriority, Weight: 0, Tag: &oldTag,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		StabilityState: ChannelSmartScheduleStabilityDegraded, StabilitySavedPriority: 95, StabilitySavedWeight: 70,
	}).Error)

	require.NoError(t, BatchSetChannelTag([]int{channel.Id}, &newTag))
	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: channel.Id, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Tag)
	assert.Equal(t, newTag, *ability.Tag)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
	var routeState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a",
	}).First(&routeState).Error)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, routeState.StabilityState)
}

func TestApplyChannelSmartScheduleRouteResultOnlyChangesTargetAbility(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	channelPriority := int64(70)
	channelWeight := uint(30)
	routePriority := int64(80)
	routeWeight := uint(50)
	require.NoError(t, db.Create(&Channel{
		Id: 1002, Name: "multi-group", Status: common.ChannelStatusEnabled,
		Group: "vip,standard", Models: "model-a",
		Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1002, Group: "vip", Model: "model-a", Enabled: true, Priority: &routePriority, Weight: routeWeight},
		{ChannelId: 1002, Group: "standard", Model: "model-a", Enabled: true, Priority: &routePriority, Weight: routeWeight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 1002, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{ChannelId: 1002, GroupName: "standard", ModelName: "model-a", ParticipationSet: true, Revision: 1},
	}).Error)

	outcomes, err := ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{{
		ChannelId: 1002, Group: "vip", Model: "model-a",
		Status: ChannelSmartScheduleStatusSucceeded, Priority: 100, Weight: 90,
		ExpectedPriority: routePriority, ExpectedWeight: routeWeight,
		ApplyPriorityWeight: true,
	}})
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.True(t, outcomes[0].Applied)
	assert.True(t, outcomes[0].RoutingChanged)

	var vip Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1002, Group: "vip", Model: "model-a"}).First(&vip).Error)
	require.NotNil(t, vip.Priority)
	assert.Equal(t, int64(100), *vip.Priority)
	assert.Equal(t, uint(90), vip.Weight)
	var standard Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1002, Group: "standard", Model: "model-a"}).First(&standard).Error)
	require.NotNil(t, standard.Priority)
	assert.Equal(t, routePriority, *standard.Priority)
	assert.Equal(t, routeWeight, standard.Weight)
	var channel Channel
	require.NoError(t, db.First(&channel, 1002).Error)
	assert.Equal(t, channelPriority, channel.GetPriority())
	assert.Equal(t, int(channelWeight), channel.GetWeight())
}

func TestApplyChannelSmartScheduleRouteResultsFullScheduleSnapshotPreservesRuntimeOverlay(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	channelPriority := int64(80)
	channelWeight := uint(1000)
	overlayPriority := int64(80)
	overlayWeight := uint(9700)
	recoveryScore := 0.95
	recoveryAt := common.GetTimestamp() - 5
	require.NoError(t, db.Create(&Channel{
		Id: 1004, Name: "runtime overlay", Status: common.ChannelStatusEnabled,
		Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1004, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &overlayPriority, Weight: overlayWeight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1004, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		BaseRank: 1, BasePriority: 80, BaseWeight: 1000,
		TemporaryTrafficKind:                          ChannelSmartScheduleTemporaryTrafficExploration,
		TemporaryTrafficSince:                         recoveryAt,
		TemporaryTrafficTargetPercent:                 3,
		ExplorationMaxPromptTokens:                    16384,
		StabilityReleaseMaxPromptTokens:               2048,
		AdaptiveHealthState:                           "healthy",
		AdaptiveHealthPressure:                        0.15,
		AdaptiveHealthFirstTokenWarningRequestPercent: 12.5,
		RollingStabilityScore:                         &recoveryScore,
		RollingStabilityUpdatedAt:                     recoveryAt,
		SamplingDebt:                                  2,
		SamplingCandidate:                             true,
		SamplingOrder:                                 "priority_weight",
		LastSamplingAt:                                recoveryAt,
	}).Error)

	outcomes, err := ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{{
		ChannelId: 1004, Group: "vip", Model: "model-a",
		Status: ChannelSmartScheduleStatusSucceeded, Priority: 2, Weight: 1000,
		RoutingSnapshot: &ChannelSmartScheduleRoutingSnapshotUpdate{
			BaseRank: 2, BasePriority: 2, BaseWeight: 1000,
		},
	}})
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.True(t, outcomes[0].Applied)
	assert.False(t, outcomes[0].RoutingChanged)

	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1004, Group: "vip", Model: "model-a"}).First(&ability).Error)
	assert.Equal(t, overlayPriority, abilityPriority(ability))
	assert.Equal(t, overlayWeight, ability.Weight)

	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 1004, GroupName: "vip", ModelName: "model-a",
	}).First(&state).Error)
	assert.Equal(t, 2, state.BaseRank)
	assert.Equal(t, int64(2), state.BasePriority)
	assert.Equal(t, uint(1000), state.BaseWeight)
	assert.Equal(t, ChannelSmartScheduleTemporaryTrafficExploration, state.TemporaryTrafficKind)
	assert.Equal(t, recoveryAt, state.TemporaryTrafficSince)
	assert.Equal(t, 3.0, state.TemporaryTrafficTargetPercent)
	assert.Equal(t, 16384, state.ExplorationMaxPromptTokens)
	assert.Equal(t, 2048, state.StabilityReleaseMaxPromptTokens)
	assert.Equal(t, "healthy", state.AdaptiveHealthState)
	assert.Equal(t, 0.15, state.AdaptiveHealthPressure)
	assert.Equal(t, 12.5, state.AdaptiveHealthFirstTokenWarningRequestPercent)
	require.NotNil(t, state.RollingStabilityScore)
	assert.Equal(t, recoveryScore, *state.RollingStabilityScore)
	assert.Equal(t, 2, state.SamplingDebt)
	assert.True(t, state.SamplingCandidate)
	assert.Equal(t, "priority_weight", state.SamplingOrder)
}

func TestApplyChannelSmartScheduleRouteResultsRejectsWholePoolOnGuardConflict(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(&Option{}))
	require.NoError(t, db.Create(&Option{
		Key: ChannelSmartScheduleControlRevisionOption, Value: "revision-a",
	}).Error)
	priority := int64(10)
	weight := uint(50)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1101, Name: "first", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 1102, Name: "second", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1101, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1102, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 1101, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{ChannelId: 1102, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
	}).Error)

	outcomes, err := ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{
		{
			ChannelId: 1101, Group: "vip", Model: "model-a",
			Status: ChannelSmartScheduleStatusSucceeded, Priority: 2, Weight: 1000,
			GuardCurrent: true, ExpectedRevision: 1, ExpectedControlRevision: "revision-a",
			ExpectedPriority: priority, ExpectedWeight: weight, ApplyPriorityWeight: true,
		},
		{
			ChannelId: 1102, Group: "vip", Model: "model-a",
			Status: ChannelSmartScheduleStatusSucceeded, Priority: 1, Weight: 1000,
			GuardCurrent: true, ExpectedRevision: 2, ExpectedControlRevision: "revision-a",
			ExpectedPriority: priority, ExpectedWeight: weight, ApplyPriorityWeight: true,
		},
	})
	require.NoError(t, err)
	require.Len(t, outcomes, 2)
	assert.False(t, outcomes[0].Applied)
	assert.False(t, outcomes[1].Applied)

	for _, channelId := range []int{1101, 1102} {
		var ability Ability
		require.NoError(t, db.Where(&Ability{ChannelId: channelId, Group: "vip", Model: "model-a"}).First(&ability).Error)
		require.NotNil(t, ability.Priority)
		assert.Equal(t, priority, *ability.Priority)
		assert.Equal(t, weight, ability.Weight)
		var state ChannelSmartScheduleRouteState
		require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
			ChannelId: channelId, GroupName: "vip", ModelName: "model-a",
		}).First(&state).Error)
		assert.Equal(t, int64(1), state.Revision)
		assert.Zero(t, state.LastScheduleTime)
	}
}

func TestApplyChannelSmartScheduleRouteResultsDoesNotOverwritePrimaryChanges(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(10)
	weight := uint(100)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1111, Name: "fixed", Status: common.ChannelStatusEnabled},
		{Id: 1112, Name: "peer", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1111, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1112, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 1111, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{ChannelId: 1112, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
	}).Error)
	controlRevision, err := GetChannelSmartScheduleControlRevision()
	require.NoError(t, err)

	fixed, err := SaveChannelSmartScheduleRoutePrimary(
		1111,
		"vip",
		"model-a",
		ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 10},
	)
	require.NoError(t, err)
	assert.True(t, fixed.RoutingChanged)

	staleBeforeFix := []ChannelSmartScheduleRouteResultUpdate{
		{
			ChannelId: 1111, Group: "vip", Model: "model-a",
			Status: ChannelSmartScheduleStatusSucceeded, Priority: 2, Weight: 1000,
			PoolGuard: true, ExpectedRevision: 1, ExpectedControlRevision: controlRevision,
			ExpectedParticipationSet: true, ExpectedAbilityEnabled: true,
			ExpectedChannelStatus: common.ChannelStatusEnabled,
			ExpectedPriority:      priority, ExpectedWeight: weight, ApplyPriorityWeight: true,
		},
		{
			ChannelId: 1112, Group: "vip", Model: "model-a",
			Status: ChannelSmartScheduleStatusSucceeded, Priority: 1, Weight: 1000,
			PoolGuard: true, ExpectedRevision: 1, ExpectedControlRevision: controlRevision,
			ExpectedParticipationSet: true, ExpectedAbilityEnabled: true,
			ExpectedChannelStatus: common.ChannelStatusEnabled,
			ExpectedPriority:      priority, ExpectedWeight: weight, ApplyPriorityWeight: true,
		},
	}
	outcomes, err := ApplyChannelSmartScheduleRouteResults(staleBeforeFix)
	require.NoError(t, err)
	require.Len(t, outcomes, 2)
	assert.False(t, outcomes[0].Applied)
	assert.False(t, outcomes[1].Applied)

	var fixedAbility Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1111, Group: "vip", Model: "model-a"}).First(&fixedAbility).Error)
	assert.Equal(t, int64(11), abilityPriority(fixedAbility))
	assert.Equal(t, uint(1000), fixedAbility.Weight)
	var fixedState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 1111, GroupName: "vip", ModelName: "model-a",
	}).First(&fixedState).Error)

	routingChanged, err := ClearExpiredChannelSmartScheduleRoutePrimaries(fixed.State.ManualPrimaryUntil)
	require.NoError(t, err)
	assert.True(t, routingChanged)

	staleBeforeExpiry := []ChannelSmartScheduleRouteResultUpdate{
		{
			ChannelId: 1111, Group: "vip", Model: "model-a",
			Status: ChannelSmartScheduleStatusSucceeded, Priority: 2, Weight: 1000,
			PoolGuard: true, ExpectedRevision: fixedState.Revision, ExpectedControlRevision: controlRevision,
			ExpectedParticipationSet: true, ExpectedAbilityEnabled: true,
			ExpectedChannelStatus: common.ChannelStatusEnabled,
			ExpectedPriority:      11, ExpectedWeight: 1000, ApplyPriorityWeight: true,
		},
		{
			ChannelId: 1112, Group: "vip", Model: "model-a",
			Status: ChannelSmartScheduleStatusSucceeded, Priority: 1, Weight: 1000,
			PoolGuard: true, ExpectedRevision: 1, ExpectedControlRevision: controlRevision,
			ExpectedParticipationSet: true, ExpectedAbilityEnabled: true,
			ExpectedChannelStatus: common.ChannelStatusEnabled,
			ExpectedPriority:      priority, ExpectedWeight: weight, ApplyPriorityWeight: true,
		},
	}
	outcomes, err = ApplyChannelSmartScheduleRouteResults(staleBeforeExpiry)
	require.NoError(t, err)
	require.Len(t, outcomes, 2)
	assert.False(t, outcomes[0].Applied)
	assert.False(t, outcomes[1].Applied)

	require.NoError(t, db.Where(&Ability{ChannelId: 1111, Group: "vip", Model: "model-a"}).First(&fixedAbility).Error)
	assert.Equal(t, priority, abilityPriority(fixedAbility))
	assert.Equal(t, weight, fixedAbility.Weight)
}

func TestApplyChannelSmartScheduleRouteResultsGuardsUnmanagedPoolMembers(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	managedPriority := int64(10)
	unmanagedPriority := int64(20)
	managedWeight := uint(50)
	unmanagedWeight := uint(30)
	revisedUnmanagedWeight := uint(99)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1151, Name: "managed", Status: common.ChannelStatusEnabled},
		{Id: 1152, Name: "unmanaged", Status: common.ChannelStatusManuallyDisabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1151, Group: "vip", Model: "model-a", Enabled: true, Priority: &managedPriority, Weight: managedWeight},
		{ChannelId: 1152, Group: "vip", Model: "model-a", Enabled: false, Priority: &unmanagedPriority, Weight: unmanagedWeight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 1151, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{ChannelId: 1152, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Excluded: true, Revision: 1},
	}).Error)
	require.NoError(t, db.Model(&Ability{}).
		Where(&Ability{ChannelId: 1152, Group: "vip", Model: "model-a"}).
		Update("weight", revisedUnmanagedWeight).Error)

	outcomes, err := ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{
		{
			ChannelId: 1151, Group: "vip", Model: "model-a",
			Status: ChannelSmartScheduleStatusSucceeded, Priority: 2, Weight: 1000,
			PoolGuard: true, ExpectedRevision: 1, ExpectedParticipationSet: true,
			ExpectedAbilityEnabled: true, ExpectedChannelStatus: common.ChannelStatusEnabled,
			ExpectedPriority: managedPriority, ExpectedWeight: managedWeight, ApplyPriorityWeight: true,
		},
		{
			ChannelId: 1152, Group: "vip", Model: "model-a",
			Priority: unmanagedPriority, Weight: unmanagedWeight,
			PoolGuard: true, ObservationOnly: true, ExpectedRevision: 1,
			ExpectedParticipationSet: true, ExpectedExcluded: true,
			ExpectedAbilityEnabled: false, ExpectedChannelStatus: common.ChannelStatusManuallyDisabled,
			ExpectedPriority: unmanagedPriority, ExpectedWeight: unmanagedWeight,
		},
	})
	require.NoError(t, err)
	require.Len(t, outcomes, 2)
	assert.False(t, outcomes[0].Applied)
	assert.False(t, outcomes[1].Applied)

	var managed Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1151, Group: "vip", Model: "model-a"}).First(&managed).Error)
	assert.Equal(t, managedPriority, abilityPriority(managed))
	assert.Equal(t, managedWeight, managed.Weight)
}

func TestApplyChannelSmartScheduleRouteResultsGuardsPoolMembership(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(10)
	weight := uint(50)
	require.NoError(t, db.Create(&Channel{Id: 1161, Name: "existing", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1161, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1161, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1,
	}).Error)

	newPriority := int64(20)
	require.NoError(t, db.Create(&Channel{Id: 1162, Name: "new pool member", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1162, Group: "vip", Model: "model-a", Enabled: true, Priority: &newPriority, Weight: weight,
	}).Error)
	outcomes, err := ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{{
		ChannelId: 1161, Group: "vip", Model: "model-a",
		Status: ChannelSmartScheduleStatusSucceeded, Priority: 1, Weight: 1000,
		PoolGuard: true, ExpectedRevision: 1, ExpectedParticipationSet: true,
		ExpectedAbilityEnabled: true, ExpectedChannelStatus: common.ChannelStatusEnabled,
		ExpectedPriority: priority, ExpectedWeight: weight, ApplyPriorityWeight: true,
	}})
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.False(t, outcomes[0].Applied)

	var existing Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1161, Group: "vip", Model: "model-a"}).First(&existing).Error)
	assert.Equal(t, priority, abilityPriority(existing))
	assert.Equal(t, weight, existing.Weight)
}

func TestApplyChannelSmartScheduleRouteResultsRollsBackWholePoolOnWriteFailure(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(10)
	weight := uint(50)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1201, Name: "first", Status: common.ChannelStatusEnabled},
		{Id: 1202, Name: "second", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1201, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1202, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 1201, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{ChannelId: 1202, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
	}).Error)

	callbackName := "test:fail_second_smart_schedule_ability_update"
	abilityUpdates := 0
	forcedErr := fmt.Errorf("forced second ability update failure")
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "abilities" {
			return
		}
		abilityUpdates++
		if abilityUpdates == 2 {
			tx.AddError(forcedErr)
		}
	}))
	callbackRegistered := true
	t.Cleanup(func() {
		if callbackRegistered {
			_ = db.Callback().Update().Remove(callbackName)
		}
	})

	outcomes, err := ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{
		{
			ChannelId: 1201, Group: "vip", Model: "model-a",
			Status: ChannelSmartScheduleStatusSucceeded, Priority: 2, Weight: 1000,
			ApplyPriorityWeight: true,
		},
		{
			ChannelId: 1202, Group: "vip", Model: "model-a",
			Status: ChannelSmartScheduleStatusSucceeded, Priority: 1, Weight: 1000,
			ApplyPriorityWeight: true,
		},
	})
	require.ErrorIs(t, err, forcedErr)
	require.Len(t, outcomes, 2)
	assert.False(t, outcomes[0].Applied)
	assert.False(t, outcomes[0].RoutingChanged)
	assert.False(t, outcomes[1].Applied)
	assert.False(t, outcomes[1].RoutingChanged)
	require.NoError(t, db.Callback().Update().Remove(callbackName))
	callbackRegistered = false

	for _, channelId := range []int{1201, 1202} {
		var ability Ability
		require.NoError(t, db.Where(&Ability{ChannelId: channelId, Group: "vip", Model: "model-a"}).First(&ability).Error)
		require.NotNil(t, ability.Priority)
		assert.Equal(t, priority, *ability.Priority)
		assert.Equal(t, weight, ability.Weight)
		var state ChannelSmartScheduleRouteState
		require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
			ChannelId: channelId, GroupName: "vip", ModelName: "model-a",
		}).First(&state).Error)
		assert.Equal(t, int64(1), state.Revision)
		assert.Zero(t, state.LastScheduleTime)
	}
}

func TestClearChannelSmartScheduleRouteStabilityRestoresOnlyTargetRoute(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	channelPriority := int64(80)
	channelWeight := uint(50)
	degradedPriority := int64(0)
	otherPriority := int64(70)
	require.NoError(t, db.Create(&Channel{
		Id: 1003, Name: "protected", Status: common.ChannelStatusEnabled,
		Group: "vip,standard", Models: "model-a",
		Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1003, Group: "vip", Model: "model-a", Enabled: true, Priority: &degradedPriority, Weight: 0},
		{ChannelId: 1003, Group: "standard", Model: "model-a", Enabled: true, Priority: &otherPriority, Weight: 25},
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1003, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		StabilityState:         ChannelSmartScheduleStabilityDegraded,
		StabilitySavedPriority: 95, StabilitySavedWeight: 45,
	}).Error)
	sampleWindowStart := common.GetTimestamp() - 120
	firstTokenMs := 500.0
	for index := range 2 {
		_, err := SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
			ChannelId: 1003, Model: "model-a", WindowStart: sampleWindowStart,
			Time: sampleWindowStart + int64(index), Success: true, FirstTokenMs: &firstTokenMs,
		})
		require.NoError(t, err)
	}
	var samplesBefore ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ?", 1003, "model-a",
	).First(&samplesBefore).Error)
	require.NotEmpty(t, samplesBefore.SamplesJSON)

	result, err := ClearChannelSmartScheduleRouteStability(1003, "vip", "model-a", 80, 10)
	require.NoError(t, err)
	assert.True(t, result.Cleared)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, result.PreviousState)
	assert.Equal(t, int64(95), result.Priority)
	assert.Equal(t, uint(45), result.Weight)
	assert.Positive(t, result.ObservationSince)

	var routeState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1003, "vip", "model-a",
	).First(&routeState).Error)
	assert.Zero(t, routeState.StabilitySince)
	var samplesAfter ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ?", 1003, "model-a",
	).First(&samplesAfter).Error)
	assert.Equal(t, result.ObservationSince, samplesAfter.ObservationSince)
	assert.Equal(t, samplesBefore.SamplesJSON, samplesAfter.SamplesJSON)
	assert.Zero(t, samplesAfter.SampleCount)
	assert.Zero(t, samplesAfter.SuccessCount)
	assert.Zero(t, samplesAfter.FirstTokenSampleCount)
	assert.Nil(t, samplesAfter.AverageFirstTokenMs)
	assert.Zero(t, samplesAfter.MetricsSince(0).SampleCount)

	newFirstTokenMs := 200.0
	samplesAfter, err = SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: 1003, Model: "model-a", WindowStart: sampleWindowStart,
		Time: result.ObservationSince + 1, Success: true, FirstTokenMs: &newFirstTokenMs,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), samplesAfter.SampleCount)
	assert.Equal(t, int64(1), samplesAfter.SuccessCount)
	assert.Equal(t, int64(1), samplesAfter.FirstTokenSampleCount)
	require.NotNil(t, samplesAfter.AverageFirstTokenMs)
	assert.InDelta(t, 200, *samplesAfter.AverageFirstTokenMs, 1e-9)
	assert.Equal(t, int64(1), samplesAfter.MetricsSince(0).SampleCount)

	var vip Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1003, Group: "vip", Model: "model-a"}).First(&vip).Error)
	require.NotNil(t, vip.Priority)
	assert.Equal(t, int64(95), *vip.Priority)
	assert.Equal(t, uint(45), vip.Weight)
	var standard Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1003, Group: "standard", Model: "model-a"}).First(&standard).Error)
	require.NotNil(t, standard.Priority)
	assert.Equal(t, otherPriority, *standard.Priority)
	assert.Equal(t, uint(25), standard.Weight)
}

func TestClearChannelSmartScheduleRouteExplorationRestoresOnlyTargetRoute(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	defaultPriority := int64(80)
	defaultWeight := uint(50)
	targetTemporaryPriority := int64(100)
	otherTemporaryPriority := int64(101)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1013, Name: "target", Status: common.ChannelStatusEnabled, Priority: &defaultPriority, Weight: &defaultWeight},
		{Id: 1014, Name: "other", Status: common.ChannelStatusEnabled, Priority: &defaultPriority, Weight: &defaultWeight},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1013, Group: "vip", Model: "model-a", Enabled: true, Priority: &targetTemporaryPriority, Weight: 2},
		{ChannelId: 1014, Group: "vip", Model: "model-a", Enabled: true, Priority: &otherTemporaryPriority, Weight: 3},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: 1013, GroupName: "vip", ModelName: "model-a",
			ParticipationSet: true, Revision: 1,
			BaseRank: 2, BasePriority: 20, BaseWeight: 40,
			TemporaryTrafficKind:  ChannelSmartScheduleTemporaryTrafficExploration,
			TemporaryTrafficSince: 123, TemporaryTrafficTargetPercent: 3,
			ExplorationMaxPromptTokens: 4096,
		},
		{
			ChannelId: 1014, GroupName: "vip", ModelName: "model-a",
			ParticipationSet: true, Revision: 4,
			BaseRank: 3, BasePriority: 10, BaseWeight: 30,
			TemporaryTrafficKind:  ChannelSmartScheduleTemporaryTrafficExploration,
			TemporaryTrafficSince: 456, TemporaryTrafficTargetPercent: 4,
			ExplorationMaxPromptTokens: 2048,
		},
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleModelSampleState{
		ChannelId: 1013, ModelName: "model-a", ObservationSince: 777,
	}).Error)

	result, err := ClearChannelSmartScheduleRouteExploration(1013, "vip", "model-a")
	require.NoError(t, err)
	assert.True(t, result.Cleared)
	assert.True(t, result.RoutingChanged)
	assert.Equal(t, ChannelSmartScheduleTemporaryTrafficExploration, result.PreviousKind)
	assert.Equal(t, int64(20), result.Priority)
	assert.Equal(t, uint(40), result.Weight)

	var targetAbility Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1013, Group: "vip", Model: "model-a"}).First(&targetAbility).Error)
	require.NotNil(t, targetAbility.Priority)
	assert.Equal(t, int64(20), *targetAbility.Priority)
	assert.Equal(t, uint(40), targetAbility.Weight)
	var targetState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 1013, GroupName: "vip", ModelName: "model-a",
	}).First(&targetState).Error)
	assert.Empty(t, targetState.TemporaryTrafficKind)
	assert.Zero(t, targetState.TemporaryTrafficSince)
	assert.Zero(t, targetState.TemporaryTrafficTargetPercent)
	assert.Zero(t, targetState.ExplorationMaxPromptTokens)
	assert.Equal(t, int64(2), targetState.Revision)

	var otherAbility Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1014, Group: "vip", Model: "model-a"}).First(&otherAbility).Error)
	require.NotNil(t, otherAbility.Priority)
	assert.Equal(t, otherTemporaryPriority, *otherAbility.Priority)
	assert.Equal(t, uint(3), otherAbility.Weight)
	var otherState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 1014, GroupName: "vip", ModelName: "model-a",
	}).First(&otherState).Error)
	assert.Equal(t, ChannelSmartScheduleTemporaryTrafficExploration, otherState.TemporaryTrafficKind)
	assert.Equal(t, int64(4), otherState.Revision)

	var sampleState ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ?", 1013, "model-a",
	).First(&sampleState).Error)
	assert.Equal(t, int64(777), sampleState.ObservationSince)
}

func TestClearChannelSmartScheduleTemporaryTrafficRestoresSavedRouteValues(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	channelPriority := int64(80)
	channelWeight := uint(50)
	explorationPriority := int64(100)
	require.NoError(t, db.Create(&Channel{
		Id: 1005, Name: "exploring", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a",
		Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1005, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &explorationPriority, Weight: 2,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1005, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		BaseRank: 2, BasePriority: 20, BaseWeight: 40,
		TemporaryTrafficKind:  ChannelSmartScheduleTemporaryTrafficExploration,
		TemporaryTrafficSince: 123, TemporaryTrafficTargetPercent: 3,
	}).Error)

	changed, err := ClearChannelSmartScheduleTemporaryTraffic()
	require.NoError(t, err)
	assert.True(t, changed)
	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1005, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(20), *ability.Priority)
	assert.Equal(t, uint(40), ability.Weight)
	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1005, "vip", "model-a",
	).First(&state).Error)
	assert.Empty(t, state.TemporaryTrafficKind)
	assert.Zero(t, state.TemporaryTrafficSince)
	assert.Zero(t, state.TemporaryTrafficTargetPercent)
	assert.Equal(t, int64(20), state.BasePriority)
	assert.Equal(t, uint(40), state.BaseWeight)
}

func TestClearChannelSmartScheduleTemporaryTrafficReportsReappliedPrimary(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	fixedPriority := int64(80)
	temporaryBasePriority := int64(90)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1006, Name: "fixed", Status: common.ChannelStatusEnabled},
		{Id: 1007, Name: "temporary", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1006, Group: "vip", Model: "model-a", Enabled: true, Priority: &fixedPriority, Weight: 40},
		{ChannelId: 1007, Group: "vip", Model: "model-a", Enabled: true, Priority: &temporaryBasePriority, Weight: 30},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: 1006, GroupName: "vip", ModelName: "model-a",
			ParticipationSet: true, Revision: 1,
			BasePriority: 80, BaseWeight: 40,
			ManualPrimaryUntil: common.GetTimestamp() + 600,
			ManualPrimarySaved: true, ManualPrimarySavedPriority: 80, ManualPrimarySavedWeight: 40,
		},
		{
			ChannelId: 1007, GroupName: "vip", ModelName: "model-a",
			ParticipationSet: true, Revision: 1,
			BasePriority: 90, BaseWeight: 30,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficExploration,
		},
	}).Error)

	changed, err := ClearChannelSmartScheduleTemporaryTraffic()
	require.NoError(t, err)
	assert.True(t, changed)

	var fixed Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1006, Group: "vip", Model: "model-a"}).First(&fixed).Error)
	assert.Equal(t, int64(91), abilityPriority(fixed))
	assert.Equal(t, uint(1000), fixed.Weight)
}

func TestProtectChannelSmartScheduleRouteOnRuntimeFailureStopsTemporaryTraffic(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(100)
	weight := uint(2)
	require.NoError(t, db.Create(&Channel{
		Id: 1011, Name: "runtime-protection", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1011, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1011, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		BaseRank: 2, BasePriority: 20, BaseWeight: 40,
		TemporaryTrafficKind:  ChannelSmartScheduleTemporaryTrafficExploration,
		TemporaryTrafficSince: 123, TemporaryTrafficTargetPercent: 3,
	}).Error)

	protectionUntil := common.GetTimestamp() + 600
	result, err := ProtectChannelSmartScheduleRouteOnRuntimeFailure(
		1011, "vip", "model-a", protectionUntil, "上游返回 503", "",
	)
	require.NoError(t, err)
	assert.True(t, result.Handled)
	assert.True(t, result.RoutingChanged)
	assert.Empty(t, result.PreviousState)

	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1011, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 1011, GroupName: "vip", ModelName: "model-a",
	}).First(&state).Error)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, state.StabilityState)
	assert.Equal(t, int64(20), state.StabilitySavedPriority)
	assert.Equal(t, uint(40), state.StabilitySavedWeight)
	assert.Empty(t, state.TemporaryTrafficKind)
	assert.Equal(t, protectionUntil, state.RuntimeProtectionUntil)
	assert.Equal(t, ChannelSmartScheduleStatusFailed, state.LastScheduleStatus)
	assert.Nil(t, state.LastScheduleScore)
	assert.Equal(t, "上游返回 503", state.LastScheduleError)

	revision := state.Revision
	result, err = ProtectChannelSmartScheduleRouteOnRuntimeFailure(
		1011, "vip", "model-a", protectionUntil, "重复错误", "",
	)
	require.NoError(t, err)
	assert.False(t, result.Handled)
	var unchanged ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 1011, GroupName: "vip", ModelName: "model-a",
	}).First(&unchanged).Error)
	assert.Equal(t, revision, unchanged.Revision)
}

func TestProtectFixedPrimaryOnRuntimeFailureWithdrawsPoolTemporaryTraffic(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	fixedUntil := createChannelSmartSchedulePrimaryCleanupFixture(t, db)
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).
		Where(&ChannelSmartScheduleRouteState{
			ChannelId: 4101, GroupName: "vip", ModelName: "model-a",
		}).Update("manual_primary_allow_stability_degrade", true).Error)

	protectionUntil := common.GetTimestamp() + 300
	result, err := ProtectChannelSmartScheduleRouteOnRuntimeFailure(
		4101, "vip", "model-a", protectionUntil, "固定主渠道运行时错误", "",
	)
	require.NoError(t, err)
	assert.True(t, result.Handled)
	assert.True(t, result.RoutingChanged)

	var fixedState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4101, GroupName: "vip", ModelName: "model-a",
	}).First(&fixedState).Error)
	assert.Equal(t, fixedUntil, fixedState.ManualPrimaryUntil)
	assert.True(t, fixedState.ManualPrimaryAllowStabilityDegrade)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, fixedState.StabilityState)
	var fixedAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4101, Group: "vip", Model: "model-a",
	}).First(&fixedAbility).Error)
	assert.Zero(t, abilityPriority(fixedAbility))
	assert.Zero(t, fixedAbility.Weight)

	var temporaryState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4102, GroupName: "vip", ModelName: "model-a",
	}).First(&temporaryState).Error)
	assert.Empty(t, temporaryState.TemporaryTrafficKind)
	var temporaryAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4102, Group: "vip", Model: "model-a",
	}).First(&temporaryAbility).Error)
	assert.Equal(t, int64(90), abilityPriority(temporaryAbility))
	assert.Equal(t, uint(30), temporaryAbility.Weight)
}

func TestProtectFixedPrimaryOnRuntimeFailureHonorsNoDegradeChoice(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	createChannelSmartSchedulePrimaryCleanupFixture(t, db)

	result, err := ProtectChannelSmartScheduleRouteOnRuntimeFailure(
		4101, "vip", "model-a", common.GetTimestamp()+300, "固定主渠道运行时错误", "",
	)
	require.NoError(t, err)
	assert.False(t, result.Handled)

	var fixedState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4101, GroupName: "vip", ModelName: "model-a",
	}).First(&fixedState).Error)
	assert.Empty(t, fixedState.StabilityState)
	var fixedAbility Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 4101, Group: "vip", Model: "model-a",
	}).First(&fixedAbility).Error)
	assert.Equal(t, int64(101), abilityPriority(fixedAbility))
	assert.Equal(t, uint(1000), fixedAbility.Weight)
	var temporaryState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 4102, GroupName: "vip", ModelName: "model-a",
	}).First(&temporaryState).Error)
	assert.Equal(t, ChannelSmartScheduleTemporaryTrafficExploration, temporaryState.TemporaryTrafficKind)
}

func TestProtectChannelSmartScheduleRouteOnRuntimeFailureRejectsStaleControlRevision(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.Create(&Option{
		Key: ChannelSmartScheduleControlRevisionOption, Value: "revision-current",
	}).Error)
	priority := int64(101)
	weight := uint(5)
	require.NoError(t, db.Create(&Channel{
		Id: 1013, Name: "stale runtime protection", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1013, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1013, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		BasePriority: 80, BaseWeight: 40,
		TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficExploration,
	}).Error)

	result, err := ProtectChannelSmartScheduleRouteOnRuntimeFailure(
		1013, "vip", "model-a", common.GetTimestamp()+600, "旧配置运行时错误", "revision-stale",
	)
	require.NoError(t, err)
	assert.False(t, result.Handled)

	var ability Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 1013, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	assert.Equal(t, priority, abilityPriority(ability))
	assert.Equal(t, weight, ability.Weight)
	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 1013, GroupName: "vip", ModelName: "model-a",
	}).First(&state).Error)
	assert.Equal(t, ChannelSmartScheduleTemporaryTrafficExploration, state.TemporaryTrafficKind)
}

func TestProtectChannelSmartScheduleRouteOnRuntimeFailureKeepsProbeRestoreValues(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(20)
	weight := uint(10)
	require.NoError(t, db.Create(&Channel{
		Id: 1012, Name: "runtime-probe-protection", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1012, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1012, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		StabilityState: ChannelSmartScheduleStabilityProbing,
		StabilitySince: 100, StabilitySavedPriority: 90, StabilitySavedWeight: 50,
	}).Error)

	protectionUntil := common.GetTimestamp() + 600
	result, err := ProtectChannelSmartScheduleRouteOnRuntimeFailure(
		1012, "vip", "model-a", protectionUntil, "试放失败", "",
	)
	require.NoError(t, err)
	assert.True(t, result.Handled)
	assert.Equal(t, ChannelSmartScheduleStabilityProbing, result.PreviousState)

	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1012, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 1012, GroupName: "vip", ModelName: "model-a",
	}).First(&state).Error)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, state.StabilityState)
	assert.Equal(t, int64(90), state.StabilitySavedPriority)
	assert.Equal(t, uint(50), state.StabilitySavedWeight)
	assert.Equal(t, protectionUntil, state.RuntimeProtectionUntil)
}

func TestProtectChannelSmartScheduleRouteOnRecoveryProbeFailureDoesNotRedegradeRecoveredRoute(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(90)
	weight := uint(50)
	require.NoError(t, db.Create(&Channel{
		Id: 1014, Name: "recovered probe route", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1014, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1014, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
	}).Error)

	result, err := ProtectChannelSmartScheduleRouteOnRecoveryProbeFailure(
		1014, "vip", "model-a", common.GetTimestamp()+600, "过期的降级探测失败", "",
	)
	require.NoError(t, err)
	assert.False(t, result.Handled)

	var ability Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: 1014, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	assert.Equal(t, priority, abilityPriority(ability))
	assert.Equal(t, weight, ability.Weight)
	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 1014, GroupName: "vip", ModelName: "model-a",
	}).First(&state).Error)
	assert.Empty(t, state.StabilityState)
	assert.Zero(t, state.StabilityUntil)
}

func TestSaveChannelSmartScheduleRouteConfigReportsRestoredExplorationRouting(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(100)
	weight := uint(5)
	require.NoError(t, db.Create(&Channel{Id: 1009, Name: "exploration", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1009, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1009, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		BaseRank: 2, BasePriority: 80, BaseWeight: 50,
		TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficExploration,
	}).Error)

	state, routingChanged, err := SaveChannelSmartScheduleRouteConfig(1009, "vip", "model-a", true)
	require.NoError(t, err)
	assert.True(t, routingChanged)
	assert.True(t, state.Excluded)
	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1009, Group: "vip", Model: "model-a"}).First(&ability).Error)
	assert.Nil(t, ability.Priority)
	assert.Zero(t, ability.Weight)
}

func TestSaveChannelSmartScheduleModelSampleAggregatesWindowWithoutOverwritingRouteState(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	score := 0.75
	require.NoError(t, db.Create(&Channel{Id: 1006, Name: "sample channel"}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1006, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 7,
		LastScheduleStatus: ChannelSmartScheduleStatusSucceeded,
		LastScheduleError:  "保留调度结果", LastScheduleScore: &score,
		LastSchedulePriority: 90, LastScheduleWeight: 60, LastScheduleTime: 123,
		StabilityState: ChannelSmartScheduleStabilityProbing, StabilitySince: 120,
	}).Error)
	firstTokenFast := 100.0
	tpsSlow := 20.0
	state, err := SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: 1006, Model: "model-a",
		WindowStart: 100, Time: 200, Success: true,
		FirstTokenMs: &firstTokenFast, TPS: &tpsSlow,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), state.SampleCount)
	assert.Equal(t, int64(1), state.SuccessCount)

	firstTokenSlow := 300.0
	tpsFast := 40.0
	state, err = SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: 1006, Model: "model-a",
		WindowStart: 100, Time: 210, Success: true,
		FirstTokenMs: &firstTokenSlow, TPS: &tpsFast,
	})
	require.NoError(t, err)
	state, err = SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: 1006, Model: "model-a",
		WindowStart: 100, Time: 220, Success: false, Error: "上游暂不可用",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(3), state.SampleCount)
	assert.Equal(t, int64(2), state.SuccessCount)
	assert.Equal(t, int64(2), state.FirstTokenSampleCount)
	require.NotNil(t, state.AverageFirstTokenMs)
	assert.InDelta(t, 200, *state.AverageFirstTokenMs, 1e-9)
	assert.Equal(t, int64(2), state.TPSSampleCount)
	require.NotNil(t, state.AverageTPS)
	assert.InDelta(t, 30, *state.AverageTPS, 1e-9)
	assert.False(t, state.LastSuccess)
	assert.Equal(t, "上游暂不可用", state.LastError)

	var stored ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1006, "vip", "model-a",
	).First(&stored).Error)
	assert.Equal(t, ChannelSmartScheduleStatusSucceeded, stored.LastScheduleStatus)
	assert.Equal(t, "保留调度结果", stored.LastScheduleError)
	require.NotNil(t, stored.LastScheduleScore)
	assert.InDelta(t, score, *stored.LastScheduleScore, 1e-9)
	assert.Equal(t, ChannelSmartScheduleStabilityProbing, stored.StabilityState)
	assert.Equal(t, int64(7), stored.Revision)
	assert.NotEmpty(t, state.SamplesJSON)

	state, err = SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: 1006, Model: "model-a",
		WindowStart: 205, Time: 300, Success: false, Error: "新窗口失败",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(210), state.WindowStart)
	assert.Equal(t, int64(3), state.SampleCount)
	assert.Equal(t, int64(1), state.SuccessCount)
	assert.Equal(t, int64(1), state.FirstTokenSampleCount)
	require.NotNil(t, state.AverageFirstTokenMs)
	assert.InDelta(t, 300, *state.AverageFirstTokenMs, 1e-9)
	assert.Equal(t, int64(1), state.TPSSampleCount)
	require.NotNil(t, state.AverageTPS)
	assert.InDelta(t, 40, *state.AverageTPS, 1e-9)

	latest := state.MetricsSince(221)
	assert.Equal(t, int64(300), latest.WindowStart)
	assert.Equal(t, int64(1), latest.SampleCount)
	assert.Zero(t, latest.SuccessCount)
	assert.Nil(t, latest.AverageFirstTokenMs)
}

func TestChannelSmartScheduleSharedSamplesMatchFormattedModelName(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(10)
	require.NoError(t, db.Create(&Channel{
		Id: 1021, Name: "thinking model", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1021, Group: "vip", Model: "gemini-2.5-pro-thinking-*", Enabled: true,
		Priority: &priority, Weight: 10,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1021, GroupName: "vip", ModelName: "gemini-2.5-pro-thinking-*",
		ParticipationSet: true, Revision: 1,
	}).Error)
	_, err := SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: 1021,
		Model:     "gemini-2.5-pro-thinking-2048",
		Source:    ChannelSmartScheduleSampleSourceManualTest,
		SampleId:  "formatted-model-sample",
		Time:      common.GetTimestamp(),
		Success:   true,
	})
	require.NoError(t, err)

	routes, err := GetChannelSmartScheduleRoutes()
	require.NoError(t, err)
	require.Len(t, routes, 1)
	assert.Equal(t, "gemini-2.5-pro-thinking-*", routes[0].SharedSamples.ModelName)
	assert.Equal(t, int64(1), routes[0].SharedSamples.SampleCount)
}

func TestSaveChannelSmartScheduleModelSampleKeepsNewestBoundedSamples(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.Create(&Channel{Id: 1007, Name: "bounded sample channel"}).Error)
	samples := make([]channelSmartScheduleSample, 0, channelSmartScheduleMaxSamples)
	for index := 1; index <= channelSmartScheduleMaxSamples; index++ {
		samples = append(samples, channelSmartScheduleSample{Time: int64(index), Success: true})
	}
	rawSamples, err := common.Marshal(samples)
	require.NoError(t, err)
	require.NoError(t, db.Create(&ChannelSmartScheduleModelSampleState{
		ChannelId: 1007, ModelName: "model-a", SamplesJSON: ChannelSmartScheduleSamplesJSON(rawSamples),
	}).Error)

	state, err := SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: 1007, Model: "model-a",
		WindowStart: 1, Time: channelSmartScheduleMaxSamples + 1, Success: false,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(channelSmartScheduleMaxSamples), state.SampleCount)
	assert.Equal(t, int64(2), state.WindowStart)
	assert.Equal(t, int64(channelSmartScheduleMaxSamples-1), state.SuccessCount)

	metrics := state.MetricsSince(channelSmartScheduleMaxSamples)
	assert.Equal(t, int64(2), metrics.SampleCount)
	assert.Equal(t, int64(1), metrics.SuccessCount)
}

func TestSaveChannelSmartScheduleModelSampleSeparatesAndDeduplicatesManualSamples(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.Create(&Channel{Id: 1008, Name: "deduplicated sample channel"}).Error)
	state, err := SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: 1008, Model: "model-a",
		Source:   ChannelSmartScheduleSampleSourceScheduledProbe,
		SampleId: "scheduled-1", WindowStart: 100, Time: 100, Success: true,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), state.SampleCount)

	failureDurationMs := 750.0
	manualResult := ChannelSmartScheduleModelSampleResult{
		ChannelId: 1008, Model: "model-a",
		Source:   ChannelSmartScheduleSampleSourceManualTest,
		SampleId: "manual-1", WindowStart: 100, Time: 110, Success: false,
		DurationMs: &failureDurationMs,
	}
	state, err = SaveChannelSmartScheduleModelSample(manualResult)
	require.NoError(t, err)
	assert.Equal(t, int64(2), state.SampleCount)

	manualResult.Success = true
	state, err = SaveChannelSmartScheduleModelSample(manualResult)
	require.NoError(t, err)
	assert.Equal(t, int64(2), state.SampleCount)
	assert.Equal(t, int64(1), state.SuccessCount)

	firstTokenMs := 125.0
	tps := 40.0
	state, err = SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: 1008, Model: "model-a",
		Source:   ChannelSmartScheduleSampleSourceManualTest,
		SampleId: "manual-2", WindowStart: 100, Time: 120, Success: true,
		FirstTokenMs: &firstTokenMs, TPS: &tps,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(3), state.SampleCount)

	manualMetrics := state.ManualTestMetricsSince(100)
	assert.Equal(t, int64(2), manualMetrics.SampleCount)
	assert.Equal(t, int64(1), manualMetrics.SuccessCount)
	assert.Equal(t, int64(1), manualMetrics.FailureCount)
	assert.Equal(t, int64(1), manualMetrics.FirstTokenSampleCount)
	assert.Equal(t, int64(1), manualMetrics.TPSSampleCount)

	_, err = SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: 1008, Model: "model-a",
		Source: "unknown", WindowStart: 100, Time: 130, Success: true,
	})
	assert.ErrorContains(t, err, "样本来源无效")
}

func TestSaveChannelSmartScheduleModelSampleKeepsNewerStateWhenOlderResultArrivesLate(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.Create(&Channel{Id: 1012, Name: "out of order sample channel"}).Error)
	newerFirstTokenMs := 200.0
	_, err := SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: 1012, Model: "model-a", Source: ChannelSmartScheduleSampleSourceManualTest,
		SampleId: "newer", WindowStart: 100, Time: 200, Success: true,
		FirstTokenMs: &newerFirstTokenMs,
	})
	require.NoError(t, err)

	olderDurationMs := 3_000.0
	state, err := SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: 1012, Model: "model-a", Source: ChannelSmartScheduleSampleSourceScheduledProbe,
		SampleId: "older", WindowStart: 100, Time: 150, Success: false,
		Error: "older failure", DurationMs: &olderDurationMs,
	})
	require.NoError(t, err)

	assert.Equal(t, int64(2), state.SampleCount)
	assert.Equal(t, int64(1), state.SuccessCount)
	assert.Equal(t, int64(200), state.LastTime)
	assert.True(t, state.LastSuccess)
	assert.Empty(t, state.LastError)

	var stored ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ?", 1012, "model-a",
	).First(&stored).Error)
	assert.Equal(t, int64(2), stored.SampleCount)
	assert.Equal(t, int64(200), stored.LastTime)
}

func TestGetChannelSmartScheduleRoutesSharesSamplesAcrossGroupsButKeepsRouteStateIndependent(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(80)
	require.NoError(t, db.Create(&Channel{
		Id: 1011, Name: "shared-samples", Status: common.ChannelStatusEnabled,
		Group: "vip,standard", Models: "model-a", Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1011, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 60},
		{ChannelId: 1011, Group: "standard", Model: "model-a", Enabled: true, Priority: &priority, Weight: 40},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: 1011, GroupName: "vip", ModelName: "model-a",
			ParticipationSet: true, Revision: 3, LastScheduleStatus: ChannelSmartScheduleStatusSucceeded,
		},
		{
			ChannelId: 1011, GroupName: "standard", ModelName: "model-a",
			ParticipationSet: true, Revision: 7, StabilityState: ChannelSmartScheduleStabilityProbing,
		},
	}).Error)
	firstTokenMs := 240.0
	_, err := SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: 1011, Model: "model-a", Source: ChannelSmartScheduleSampleSourceManualTest,
		SampleId: "shared-manual-1", WindowStart: 100, Time: 120, Success: true,
		FirstTokenMs: &firstTokenMs,
	})
	require.NoError(t, err)

	_, _, err = SaveChannelSmartScheduleRouteConfig(1011, "vip", "model-a", true)
	require.NoError(t, err)
	routes, err := GetChannelSmartScheduleRoutes()
	require.NoError(t, err)
	require.Len(t, routes, 2)
	routeByGroup := make(map[string]ChannelSmartScheduleRoute, len(routes))
	for _, route := range routes {
		routeByGroup[route.Group] = route
	}
	vip := routeByGroup["vip"]
	standard := routeByGroup["standard"]
	assert.True(t, vip.State.Excluded)
	assert.Equal(t, int64(4), vip.State.Revision)
	assert.False(t, standard.State.Excluded)
	assert.Equal(t, int64(7), standard.State.Revision)
	assert.Equal(t, ChannelSmartScheduleStabilityProbing, standard.State.StabilityState)
	assert.Equal(t, vip.SharedSamples.Id, standard.SharedSamples.Id)
	assert.Equal(t, int64(1), vip.SharedSamples.SampleCount)
	assert.Equal(t, int64(1), standard.SharedSamples.SampleCount)
	require.NotNil(t, vip.SharedSamples.AverageFirstTokenMs)
	require.NotNil(t, standard.SharedSamples.AverageFirstTokenMs)
	assert.InDelta(t, firstTokenMs, *vip.SharedSamples.AverageFirstTokenMs, 1e-9)
	assert.InDelta(t, firstTokenMs, *standard.SharedSamples.AverageFirstTokenMs, 1e-9)
}

func TestRouteCacheSelectsAbilityPriorityWithinGroupAndModel(t *testing.T) {
	originalChannels := channelsIDM
	originalAdvancedConfigs := channel2advancedCustomConfig
	originalRouteCache := channelSmartScheduleRouteCache
	t.Cleanup(func() {
		channelsIDM = originalChannels
		channel2advancedCustomConfig = originalAdvancedConfigs
		channelSmartScheduleRouteCache = originalRouteCache
	})
	channelPriorityHigh := int64(100)
	channelPriorityLow := int64(10)
	weight := uint(50)
	channelsIDM = map[int]*Channel{
		1: {Id: 1, Status: common.ChannelStatusEnabled, Priority: &channelPriorityHigh, Weight: &weight},
		2: {Id: 2, Status: common.ChannelStatusEnabled, Priority: &channelPriorityLow, Weight: &weight},
	}
	channel2advancedCustomConfig = map[int]*dto.AdvancedCustomConfig{}
	routePriorityLow := int64(80)
	routePriorityHigh := int64(100)
	abilities := []*Ability{
		{ChannelId: 1, Group: "vip", Model: "model-a", Enabled: true, Priority: &routePriorityLow, Weight: 50},
		{ChannelId: 2, Group: "vip", Model: "model-a", Enabled: true, Priority: &routePriorityHigh, Weight: 50},
	}
	channelSmartScheduleRouteCache = buildChannelSmartScheduleRouteCache(abilities, channelsIDM)

	channel, handled, err := getRandomSatisfiedChannelByAbility(
		"vip", "model-a", 0, "", ChannelSelectionOptions{},
	)
	require.NoError(t, err)
	assert.True(t, handled)
	require.NotNil(t, channel)
	assert.Equal(t, 2, channel.Id)
}
