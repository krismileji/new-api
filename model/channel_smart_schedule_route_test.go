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

func updateProbeFieldsDuringRouteStateSave(
	t *testing.T,
	db *gorm.DB,
	callbackName string,
	channelId int,
	group string,
	modelName string,
) *int {
	t.Helper()
	updates := 0
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "channel_smart_schedule_route_states" {
			return
		}
		updates++
		require.NoError(t, tx.Exec(
			"UPDATE channel_smart_schedule_route_states SET probe_last_error = ?, probe_sample_count = ?, probe_samples = ? WHERE channel_id = ? AND group_name = ? AND model_name = ?",
			"最新探测结果", 91, `[{"time":91,"success":false}]`, channelId, group, modelName,
		).Error)
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Update().Remove(callbackName))
	})
	return &updates
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
		ExplorationActive: true, ExplorationSavedPriority: 95, ExplorationSavedWeight: 70,
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
	assert.False(t, recreatedState.ExplorationActive)
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

func TestApplyChannelSmartScheduleRouteResultPersistsJitterBaselineWithoutOverwritingProbeState(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(80)
	weight := uint(50)
	oldBaseline := 300.0
	probeAverage := 250.0
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1010, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1010, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		JitterBaselineFirstTokenMs: &oldBaseline, JitterBaselineUpdatedAt: 100,
		ProbeWindowStart: 90, ProbeLastTime: 120, ProbeLastSuccess: true,
		ProbeSampleCount: 3, ProbeSuccessCount: 2,
		ProbeFirstTokenSampleCount: 2, ProbeAverageFirstTokenMs: &probeAverage,
		ProbeSamples: `[{"time":120,"success":true,"first_token_ms":250}]`,
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
	assert.Equal(t, int64(90), state.ProbeWindowStart)
	assert.Equal(t, int64(120), state.ProbeLastTime)
	assert.Equal(t, int64(3), state.ProbeSampleCount)
	require.NotNil(t, state.ProbeAverageFirstTokenMs)
	assert.InDelta(t, probeAverage, *state.ProbeAverageFirstTokenMs, 1e-9)
	assert.NotEmpty(t, state.ProbeSamples)
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
		ProbeLastError: "旧探测结果", ProbeSampleCount: 2,
	}).Error)
	probeWrites := updateProbeFieldsDuringRouteStateSave(
		t, db, "test:clear_route_stability_keeps_probe", 1003, "vip", "model-a",
	)

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
	assert.Equal(t, "最新探测结果", state.ProbeLastError)
	assert.Equal(t, int64(91), state.ProbeSampleCount)
	assert.Equal(t, `[{"time":91,"success":false}]`, state.ProbeSamples)
	assert.Equal(t, 1, *probeWrites)
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
		ExplorationActive: true, ExplorationSince: 123,
		ExplorationSavedPriority: 20, ExplorationSavedWeight: 40,
		ProbeLastError: "旧探测结果", ProbeSampleCount: 2,
	}).Error)
	probeWrites := updateProbeFieldsDuringRouteStateSave(
		t, db, "test:clear_exploration_keeps_probe", 1005, "vip", "model-a",
	)

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
	assert.False(t, state.ExplorationActive)
	assert.Zero(t, state.ExplorationSince)
	assert.Zero(t, state.ExplorationSavedPriority)
	assert.Zero(t, state.ExplorationSavedWeight)
	assert.Equal(t, "最新探测结果", state.ProbeLastError)
	assert.Equal(t, int64(91), state.ProbeSampleCount)
	assert.Equal(t, `[{"time":91,"success":false}]`, state.ProbeSamples)
	assert.Equal(t, 1, *probeWrites)
}

func TestSaveChannelSmartScheduleRouteConfigDoesNotOverwriteProbeFields(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(80)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1008, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 50,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1008, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		ProbeLastError: "旧探测结果", ProbeSampleCount: 2,
	}).Error)
	probeWrites := updateProbeFieldsDuringRouteStateSave(
		t, db, "test:route_config_keeps_probe", 1008, "vip", "model-a",
	)

	state, _, err := SaveChannelSmartScheduleRouteConfig(1008, "vip", "model-a", true)
	require.NoError(t, err)
	assert.True(t, state.Excluded)
	var stored ChannelSmartScheduleRouteState
	require.NoError(t, db.First(&stored, state.Id).Error)
	assert.True(t, stored.Excluded)
	assert.Equal(t, "最新探测结果", stored.ProbeLastError)
	assert.Equal(t, int64(91), stored.ProbeSampleCount)
	assert.Equal(t, `[{"time":91,"success":false}]`, stored.ProbeSamples)
	assert.Equal(t, 1, *probeWrites)
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
		ExplorationActive: true, ExplorationSavedPriority: 80, ExplorationSavedWeight: 50,
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

func TestSaveChannelSmartScheduleChannelConfigDoesNotOverwriteProbeFields(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(80)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1009, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 50,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1009, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		ProbeLastError: "旧探测结果", ProbeSampleCount: 2,
	}).Error)
	probeWrites := updateProbeFieldsDuringRouteStateSave(
		t, db, "test:channel_config_keeps_probe", 1009, "vip", "model-a",
	)

	result, err := SaveChannelSmartScheduleChannelConfig(1009, true)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Updated)
	var stored ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1009, "vip", "model-a",
	).First(&stored).Error)
	assert.True(t, stored.Excluded)
	assert.Equal(t, "最新探测结果", stored.ProbeLastError)
	assert.Equal(t, int64(91), stored.ProbeSampleCount)
	assert.Equal(t, `[{"time":91,"success":false}]`, stored.ProbeSamples)
	assert.Equal(t, 1, *probeWrites)
}

func TestSaveChannelSmartScheduleProbeResultAggregatesWindowWithoutOverwritingScheduleState(t *testing.T) {
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
	state, err := SaveChannelSmartScheduleProbeResult(ChannelSmartScheduleProbeResult{
		ChannelId: 1006, Group: "vip", Model: "model-a",
		WindowStart: 100, Time: 200, Success: true,
		FirstTokenMs: &firstTokenFast, TPS: &tpsSlow,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), state.ProbeSampleCount)
	assert.Equal(t, int64(1), state.ProbeSuccessCount)

	firstTokenSlow := 300.0
	tpsFast := 40.0
	state, err = SaveChannelSmartScheduleProbeResult(ChannelSmartScheduleProbeResult{
		ChannelId: 1006, Group: "vip", Model: "model-a",
		WindowStart: 100, Time: 210, Success: true,
		FirstTokenMs: &firstTokenSlow, TPS: &tpsFast,
	})
	require.NoError(t, err)
	state, err = SaveChannelSmartScheduleProbeResult(ChannelSmartScheduleProbeResult{
		ChannelId: 1006, Group: "vip", Model: "model-a",
		WindowStart: 100, Time: 220, Success: false, Error: "上游暂不可用",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(3), state.ProbeSampleCount)
	assert.Equal(t, int64(2), state.ProbeSuccessCount)
	assert.Equal(t, int64(2), state.ProbeFirstTokenSampleCount)
	require.NotNil(t, state.ProbeAverageFirstTokenMs)
	assert.InDelta(t, 200, *state.ProbeAverageFirstTokenMs, 1e-9)
	assert.Equal(t, int64(2), state.ProbeTPSSampleCount)
	require.NotNil(t, state.ProbeAverageTPS)
	assert.InDelta(t, 30, *state.ProbeAverageTPS, 1e-9)
	assert.False(t, state.ProbeLastSuccess)
	assert.Equal(t, "上游暂不可用", state.ProbeLastError)

	var stored ChannelSmartScheduleRouteState
	require.NoError(t, db.First(&stored, state.Id).Error)
	assert.Equal(t, ChannelSmartScheduleStatusSucceeded, stored.LastScheduleStatus)
	assert.Equal(t, "保留调度结果", stored.LastScheduleError)
	require.NotNil(t, stored.LastScheduleScore)
	assert.InDelta(t, score, *stored.LastScheduleScore, 1e-9)
	assert.Equal(t, ChannelSmartScheduleStabilityProbing, stored.StabilityState)
	assert.Equal(t, int64(7), stored.Revision)
	assert.NotEmpty(t, stored.ProbeSamples)

	state, err = SaveChannelSmartScheduleProbeResult(ChannelSmartScheduleProbeResult{
		ChannelId: 1006, Group: "vip", Model: "model-a",
		WindowStart: 205, Time: 300, Success: false, Error: "新窗口失败",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(210), state.ProbeWindowStart)
	assert.Equal(t, int64(3), state.ProbeSampleCount)
	assert.Equal(t, int64(1), state.ProbeSuccessCount)
	assert.Equal(t, int64(1), state.ProbeFirstTokenSampleCount)
	require.NotNil(t, state.ProbeAverageFirstTokenMs)
	assert.InDelta(t, 300, *state.ProbeAverageFirstTokenMs, 1e-9)
	assert.Equal(t, int64(1), state.ProbeTPSSampleCount)
	require.NotNil(t, state.ProbeAverageTPS)
	assert.InDelta(t, 40, *state.ProbeAverageTPS, 1e-9)
	assert.Equal(t, int64(7), state.Revision)

	latest := state.ProbeMetricsSince(221)
	assert.Equal(t, int64(300), latest.WindowStart)
	assert.Equal(t, int64(1), latest.SampleCount)
	assert.Zero(t, latest.SuccessCount)
	assert.Nil(t, latest.AverageFirstTokenMs)
}

func TestSaveChannelSmartScheduleProbeResultKeepsNewestBoundedSamples(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	samples := make([]channelSmartScheduleProbeSample, 0, channelSmartScheduleMaxProbeSamples)
	for index := 1; index <= channelSmartScheduleMaxProbeSamples; index++ {
		samples = append(samples, channelSmartScheduleProbeSample{Time: int64(index), Success: true})
	}
	rawSamples, err := common.Marshal(samples)
	require.NoError(t, err)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1007, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 4, ProbeSamples: string(rawSamples),
	}).Error)

	state, err := SaveChannelSmartScheduleProbeResult(ChannelSmartScheduleProbeResult{
		ChannelId: 1007, Group: "vip", Model: "model-a",
		WindowStart: 1, Time: channelSmartScheduleMaxProbeSamples + 1, Success: false,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(channelSmartScheduleMaxProbeSamples), state.ProbeSampleCount)
	assert.Equal(t, int64(2), state.ProbeWindowStart)
	assert.Equal(t, int64(channelSmartScheduleMaxProbeSamples-1), state.ProbeSuccessCount)
	assert.Equal(t, int64(4), state.Revision)

	metrics := state.ProbeMetricsSince(channelSmartScheduleMaxProbeSamples)
	assert.Equal(t, int64(2), metrics.SampleCount)
	assert.Equal(t, int64(1), metrics.SuccessCount)
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
