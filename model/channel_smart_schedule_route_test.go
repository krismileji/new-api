package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

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
		&Channel{},
		&Ability{},
		&ChannelRatioMonitor{},
		&ChannelSmartScheduleRouteState{},
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

func TestUpdateAbilitiesPreservesParticipatingSmartScheduleRouteRouting(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	defaultPriority := int64(80)
	defaultWeight := uint(50)
	degradedPriority := int64(0)
	manualPriority := int64(95)
	channel := Channel{
		Id: 1001, Name: "scheduled", Status: common.ChannelStatusEnabled,
		Group: "vip,standard,new", Models: "model-a", Priority: &defaultPriority, Weight: &defaultWeight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true, Priority: &degradedPriority, Weight: 0},
		{ChannelId: channel.Id, Group: "standard", Model: "model-a", Enabled: true, Priority: &manualPriority, Weight: 70},
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
	require.NotNil(t, byGroup["standard"].Priority)
	assert.Equal(t, defaultPriority, *byGroup["standard"].Priority)
	assert.Equal(t, defaultWeight, byGroup["standard"].Weight)
	require.NotNil(t, byGroup["new"].Priority)
	assert.Equal(t, defaultPriority, *byGroup["new"].Priority)
	assert.Equal(t, defaultWeight, byGroup["new"].Weight)
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
	assert.False(t, extended.RoutingChanged)
	assert.GreaterOrEqual(t, extended.State.ManualPrimaryUntil, first.State.ManualPrimaryUntil)
	assert.Equal(t, int64(80), extended.State.ManualPrimarySavedPriority)
	assert.Equal(t, uint(100), extended.State.ManualPrimarySavedWeight)

	require.NoError(t, db.Model(&Ability{}).
		Where(&Ability{ChannelId: 3102, Group: "vip", Model: "model-a"}).
		Update("enabled", false).Error)
	_, err = SaveChannelSmartScheduleRoutePrimary(3102, "vip", "model-a", ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 10})
	require.ErrorContains(t, err, "路由已禁用")
	var fixedAfterRejectedSwitch ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{ChannelId: 3101,
		GroupName: "vip", ModelName: "model-a"}).First(&fixedAfterRejectedSwitch).Error)
	assert.Greater(t, fixedAfterRejectedSwitch.ManualPrimaryUntil, common.GetTimestamp())
	var abilityAfterRejectedSwitch Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 3101, Group: "vip", Model: "model-a"}).
		First(&abilityAfterRejectedSwitch).Error)
	assert.Equal(t, int64(100), abilityPriority(abilityAfterRejectedSwitch))
	assert.Equal(t, uint(900), abilityAfterRejectedSwitch.Weight)
	require.NoError(t, db.Model(&Ability{}).
		Where(&Ability{ChannelId: 3102, Group: "vip", Model: "model-a"}).
		Update("enabled", true).Error)

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
			DurationMinutes: 20, AllowStabilityDegrade: false,
		},
	)
	require.NoError(t, err)
	assert.False(t, extended.RoutingChanged)
	assert.Greater(t, extended.State.ManualPrimaryUntil, common.GetTimestamp()+600)
	assert.False(t, extended.State.ManualPrimaryAllowStabilityDegrade)
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
	require.NoError(t, channel.UpdateAbilities(nil))
	var stateCount int64
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", channel.Id, "vip", "model-a",
	).Count(&stateCount).Error)
	assert.Zero(t, stateCount)

	channel.Group = "vip"
	require.NoError(t, channel.UpdateAbilities(nil))
	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: channel.Id, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, defaultPriority, *ability.Priority)
	assert.Equal(t, defaultWeight, ability.Weight)

	require.NoError(t, InitializeChannelSmartScheduleRouteStates())
	var recreatedState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a",
	}).First(&recreatedState).Error)
	assert.False(t, recreatedState.Participates())
	assert.Empty(t, recreatedState.StabilityState)
	assert.Empty(t, recreatedState.TemporaryTrafficKind)
}

func TestFixAbilityPreservesParticipatingRouteRouting(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	defaultPriority := int64(80)
	defaultWeight := uint(50)
	degradedPriority := int64(0)
	manualPriority := int64(95)
	channel := Channel{
		Id: 1012, Name: "fix-route-lifecycle", Status: common.ChannelStatusEnabled,
		Group: "vip,standard", Models: "model-a", Priority: &defaultPriority, Weight: &defaultWeight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true, Priority: &degradedPriority, Weight: 0},
		{ChannelId: channel.Id, Group: "standard", Model: "model-a", Enabled: true, Priority: &manualPriority, Weight: 70},
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
	require.NotNil(t, byGroup["standard"].Priority)
	assert.Equal(t, defaultPriority, *byGroup["standard"].Priority)
	assert.Equal(t, defaultWeight, byGroup["standard"].Weight)

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

func TestEditChannelByTagKeepsParticipatingRouteRouting(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	defaultPriority := int64(80)
	defaultWeight := uint(50)
	degradedPriority := int64(0)
	manualPriority := int64(95)
	tag := "bulk-routing"
	channel := Channel{
		Id: 1013, Name: "bulk-routing", Status: common.ChannelStatusEnabled,
		Group: "vip,standard", Models: "model-a", Priority: &defaultPriority, Weight: &defaultWeight, Tag: &tag,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true, Priority: &degradedPriority, Weight: 0, Tag: &tag},
		{ChannelId: channel.Id, Group: "standard", Model: "model-a", Enabled: true, Priority: &manualPriority, Weight: 70, Tag: &tag},
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
	require.NotNil(t, byGroup["standard"].Priority)
	assert.Equal(t, updatedPriority, *byGroup["standard"].Priority)
	assert.Equal(t, updatedWeight, byGroup["standard"].Weight)
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

func TestApplyChannelSmartScheduleRouteResultsRollsBackWholePoolOnWriteFailure(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(10)
	weight := uint(50)
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

func TestApplyChannelSmartScheduleRouteResultPersistsJitterBaselineWithoutChangingSharedSamples(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(80)
	weight := uint(50)
	oldBaseline := 300.0
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1010, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1010, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		JitterBaselineFirstTokenMs: &oldBaseline, JitterBaselineUpdatedAt: 100,
	}).Error)
	probeAverage := 250.0
	require.NoError(t, db.Create(&ChannelSmartScheduleModelSampleState{
		ChannelId: 1010, ModelName: "model-a", LastTime: 120, LastSuccess: true,
		SampleCount: 3, SuccessCount: 2, FirstTokenSampleCount: 2,
		AverageFirstTokenMs: &probeAverage,
		SamplesJSON:         `[{"time":120,"success":true,"first_token_ms":250}]`,
	}).Error)

	newBaseline := 320.0
	outcomes, err := ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{{
		ChannelId: 1010, Group: "vip", Model: "model-a",
		Status: ChannelSmartScheduleStatusSucceeded, Priority: priority, Weight: weight,
		Jitter: &ChannelSmartScheduleJitterUpdate{
			BaselineFirstTokenMs: &newBaseline,
			BaselineUpdatedAt:    200,
		},
	}})
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.True(t, outcomes[0].Applied)
	assert.False(t, outcomes[0].RoutingChanged)

	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1010, "vip", "model-a",
	).First(&state).Error)
	require.NotNil(t, state.JitterBaselineFirstTokenMs)
	assert.InDelta(t, newBaseline, *state.JitterBaselineFirstTokenMs, 1e-9)
	assert.Equal(t, int64(200), state.JitterBaselineUpdatedAt)
	var samples ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where(&ChannelSmartScheduleModelSampleState{
		ChannelId: 1010, ModelName: "model-a",
	}).First(&samples).Error)
	assert.Equal(t, int64(120), samples.LastTime)
	assert.Equal(t, int64(3), samples.SampleCount)
	require.NotNil(t, samples.AverageFirstTokenMs)
	assert.InDelta(t, probeAverage, *samples.AverageFirstTokenMs, 1e-9)
	assert.NotEmpty(t, samples.SamplesJSON)
}

func TestClearChannelSmartScheduleRouteStabilityRestoresOnlyTargetRoute(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	channelPriority := int64(80)
	channelWeight := uint(50)
	degradedPriority := int64(0)
	otherPriority := int64(70)
	jitterBaseline := 300.0
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
		JitterBaselineFirstTokenMs: &jitterBaseline, JitterBaselineUpdatedAt: 123,
	}).Error)

	result, err := ClearChannelSmartScheduleRouteStability(1003, "vip", "model-a", 80, 10)
	require.NoError(t, err)
	assert.True(t, result.Cleared)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, result.PreviousState)
	assert.Equal(t, int64(95), result.Priority)
	assert.Equal(t, uint(45), result.Weight)

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
	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1003, "vip", "model-a",
	).First(&state).Error)
	assert.Nil(t, state.JitterBaselineFirstTokenMs)
	assert.Zero(t, state.JitterBaselineUpdatedAt)
}

func TestClearChannelSmartScheduleExplorationsRestoresSavedRouteValues(t *testing.T) {
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

	changed, err := ClearChannelSmartScheduleExplorations()
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
		1011, "vip", "model-a", protectionUntil, "上游返回 503",
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
		1011, "vip", "model-a", protectionUntil, "重复错误",
	)
	require.NoError(t, err)
	assert.False(t, result.Handled)
	var unchanged ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 1011, GroupName: "vip", ModelName: "model-a",
	}).First(&unchanged).Error)
	assert.Equal(t, revision, unchanged.Revision)
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
		1012, "vip", "model-a", protectionUntil, "试放失败",
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

func TestSaveChannelSmartScheduleRouteConfigReportsRestoredExplorationRouting(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(100)
	weight := uint(5)
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
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(80), *ability.Priority)
	assert.Equal(t, uint(50), ability.Weight)
}

func TestSaveChannelSmartScheduleModelSampleAggregatesWindowWithoutOverwritingRouteState(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	score := 0.75
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

func TestSaveChannelSmartScheduleModelSampleKeepsNewestBoundedSamples(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
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
	setupChannelSmartScheduleRouteTestDB(t)
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
