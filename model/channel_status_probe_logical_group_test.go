package model

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelStatusProbeLogicalGroupTest(t *testing.T) (*gorm.DB, ChannelLogicalGroup, []int) {
	t.Helper()
	db := setupChannelStatusProbeModelTestDB(t)
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCache })
	require.NoError(t, db.AutoMigrate(&Channel{}, &ChannelLogicalGroup{}, &ChannelLogicalGroupMember{}))

	group := ChannelLogicalGroup{Name: "status-probe-group"}
	require.NoError(t, db.Create(&group).Error)
	channelIDs := []int{8101, 8102}
	for _, channelID := range channelIDs {
		logicalID := group.Id
		require.NoError(t, db.Create(&Channel{
			Id: channelID, Name: "probe-member", Key: "test-key", Status: common.ChannelStatusEnabled,
			Models: "gpt-a", LogicalChannelID: &logicalID,
		}).Error)
		require.NoError(t, db.Create(&ChannelLogicalGroupMember{
			LogicalGroupID: group.Id, ChannelID: channelID, Weight: 1,
			AddressFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}).Error)
	}
	return db, group, channelIDs
}

func TestSaveChannelStatusProbeExecutionRetainsLogicalAndActualMemberSnapshot(t *testing.T) {
	db, group, channelIDs := setupChannelStatusProbeLogicalGroupTest(t)
	config, err := SaveChannelStatusProbeConfig(channelIDs[0], ChannelStatusProbeConfigInput{
		Enabled: true, Models: []string{"model-a"}, IntervalSeconds: 60,
	}, 90)
	require.NoError(t, err)
	actualMember := channelIDs[1]
	execution := &ChannelStatusProbeExecution{
		RunId: "logical-run-1", ChannelId: channelIDs[0], LogicalChannelId: group.Id, LogicalRevision: group.Revision, ActualChannelId: actualMember,
		ModelName: "model-a", ConfigRevision: config.Revision, Trigger: ChannelStatusProbeTriggerScheduled,
		Result: ChannelStatusProbeResultSuccess, StartedAt: 100, FinishedAt: 101,
		RequestDispatched: true, CreatedAt: 101,
	}
	created, err := SaveChannelStatusProbeExecution(execution)
	require.NoError(t, err)
	assert.True(t, created)

	var stored ChannelStatusProbeExecution
	require.NoError(t, db.Where("run_id = ?", "logical-run-1").First(&stored).Error)
	assert.Equal(t, group.Id, stored.LogicalChannelId)
	assert.Equal(t, actualMember, stored.ActualChannelId)

}

func TestChannelStatusProbeLogicalGroupUsesOwnerScheduleAcrossStaggeredRows(t *testing.T) {
	db, group, channelIDs := setupChannelStatusProbeLogicalGroupTest(t)
	require.NoError(t, db.Create(&ChannelStatusProbeConfig{
		ChannelId: channelIDs[0], LogicalChannelId: group.Id, LogicalRevision: group.Revision,
		Enabled: true, ModelsJSON: `["gpt-a"]`, IntervalSeconds: 300, NextRunAt: 1_200, Revision: 1,
	}).Error)
	require.NoError(t, db.Create(&ChannelStatusProbeConfig{
		ChannelId: channelIDs[1], LogicalChannelId: group.Id, LogicalRevision: group.Revision,
		Enabled: true, ModelsJSON: `["gpt-a"]`, IntervalSeconds: 30, NextRunAt: 1_050, Revision: 1,
	}).Error)

	claims, err := ClaimDueChannelStatusProbes(1_050, 10)
	require.NoError(t, err)
	assert.Empty(t, claims, "non-owner staggered rows must not create a second logical period")

	claims, err = ClaimDueChannelStatusProbes(1_200, 10)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	assert.Equal(t, channelIDs[0], claims[0].Config.ChannelId)
	assert.Equal(t, group.Id, claims[0].Identity.LogicalChannelID)
	assert.Equal(t, group.Revision, claims[0].Identity.Revision)
	require.Len(t, claims[0].Snapshot.Members, 2)
	var physicalCount int64
	require.NoError(t, db.Model(&ChannelStatusProbeConfig{}).Count(&physicalCount).Error)
	assert.EqualValues(t, 2, physicalCount, "sharing must not delete physical configs")
	var logicalCount int64
	require.NoError(t, db.Model(&ChannelStatusProbeLogicalConfig{}).Count(&logicalCount).Error)
	assert.EqualValues(t, 1, logicalCount)
}

func TestChannelStatusProbeLogicalGroupManualRunIsShared(t *testing.T) {
	db, group, channelIDs := setupChannelStatusProbeLogicalGroupTest(t)
	created, err := SaveChannelStatusProbeConfig(channelIDs[1], ChannelStatusProbeConfigInput{
		Enabled: true, Models: []string{"gpt-a"}, IntervalSeconds: 60,
	}, 2_000)
	require.NoError(t, err)
	assert.Equal(t, channelIDs[1], created.ChannelId, "API projection keeps the requested physical channel")

	var stored ChannelStatusProbeLogicalConfig
	require.NoError(t, db.Where("logical_channel_id = ?", group.Id).First(&stored).Error)
	assert.Equal(t, channelIDs[0], stored.OwnerChannelId, "the smallest member is the stable owner")
	assert.Equal(t, group.Id, stored.LogicalChannelId)
	var physicalCount int64
	require.NoError(t, db.Model(&ChannelStatusProbeConfig{}).Count(&physicalCount).Error)
	assert.Zero(t, physicalCount, "creating shared config must not synthesize physical configs")

	requestID, err := RequestChannelStatusProbeManualRun(channelIDs[1], 2_010)
	require.NoError(t, err)
	_, err = RequestChannelStatusProbeManualRun(channelIDs[0], 2_011)
	assert.ErrorIs(t, err, ErrChannelStatusProbeManualPending)

	claims, err := ClaimDueChannelStatusProbes(2_012, 10)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	assert.Equal(t, requestID, claims[0].RunId)
	assert.Equal(t, ChannelStatusProbeTriggerManual, claims[0].Trigger)
	assert.Equal(t, channelIDs[0], claims[0].Config.ChannelId)
	assert.True(t, claims[0].LogicalConfig)
	require.NoError(t, CompleteChannelStatusProbeClaim(claims[0], 2_020))
	require.NoError(t, db.Where("logical_channel_id = ?", group.Id).First(&stored).Error)
	assert.Empty(t, stored.LeaseToken)
	assert.Empty(t, stored.ManualRequestId)
}

func TestChannelStatusProbeLogicalGroupProjectsOneSharedStateToMembers(t *testing.T) {
	db, group, channelIDs := setupChannelStatusProbeLogicalGroupTest(t)
	created, err := SaveChannelStatusProbeConfig(channelIDs[0], ChannelStatusProbeConfigInput{
		Enabled: true, Models: []string{"gpt-a"}, IntervalSeconds: 60,
	}, 3_000)
	require.NoError(t, err)

	execution := ChannelStatusProbeExecution{
		RunId: "shared-state-run", ChannelId: channelIDs[0], LogicalChannelId: group.Id, LogicalRevision: group.Revision,
		ActualChannelId: channelIDs[1], ModelName: "gpt-a", ConfigRevision: created.Revision,
		Trigger: ChannelStatusProbeTriggerScheduled, Result: ChannelStatusProbeResultSuccess,
		StartedAt: 3_001, FinishedAt: 3_002, RequestDispatched: true, SampleStatus: ChannelStatusProbeSampleSkipped,
	}
	inserted, err := SaveChannelStatusProbeExecution(&execution)
	require.NoError(t, err)
	assert.True(t, inserted)

	var physicalStateCount int64
	require.NoError(t, db.Model(&ChannelStatusProbeState{}).Count(&physicalStateCount).Error)
	assert.Zero(t, physicalStateCount)
	var persisted ChannelStatusProbeLogicalState
	require.NoError(t, db.Where("logical_channel_id = ?", group.Id).First(&persisted).Error)
	assert.Equal(t, group.Id, persisted.LogicalChannelId)

	states, err := GetChannelStatusProbeStates()
	require.NoError(t, err)
	require.Len(t, states, 2)
	assert.Equal(t, channelIDs, []int{states[0].ChannelId, states[1].ChannelId})
	assert.Equal(t, states[0].ExecutionId, states[1].ExecutionId)
	assert.Equal(t, ChannelStatusProbeResultSuccess, states[0].Result)
	assert.Equal(t, ChannelStatusProbeResultSuccess, states[1].Result)

	history, total, err := ListChannelStatusProbeExecutions(channelIDs[1], 1, 20, "gpt-a", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, history, 1)
	assert.Equal(t, channelIDs[1], history[0].ChannelId)
	assert.Equal(t, channelIDs[1], history[0].ActualChannelId)

	relations, err := LoadChannelStatusProbeOverviewRelations(t.Context(), db)
	require.NoError(t, err)
	overviewExecutions, err := GetChannelStatusProbeExecutionsSinceForOverview(
		t.Context(), db, 3_000, 3_003, nil, "gpt-a", relations,
	)
	require.NoError(t, err)
	require.Len(t, overviewExecutions, 2)
	assert.Equal(t, channelIDs, []int{overviewExecutions[0].ChannelId, overviewExecutions[1].ChannelId})
	filteredOverviewExecutions, err := GetChannelStatusProbeExecutionsSinceForOverview(
		t.Context(), db, 3_000, 3_003, []int{channelIDs[1]}, "gpt-a", relations,
	)
	require.NoError(t, err)
	require.Len(t, filteredOverviewExecutions, 1)
	assert.Equal(t, channelIDs[1], filteredOverviewExecutions[0].ChannelId)

	require.NoError(t, db.Where("logical_group_id = ? AND channel_id = ?", group.Id, channelIDs[1]).
		Delete(&ChannelLogicalGroupMember{}).Error)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channelIDs[1]).Update("logical_channel_id", nil).Error)
	require.NoError(t, db.Model(&ChannelLogicalGroup{}).Where("id = ?", group.Id).
		Update("revision", group.Revision+1).Error)
	InvalidateLogicalChannelRuntimeCache()

	history, total, err = ListChannelStatusProbeExecutions(channelIDs[1], 1, 20, "gpt-a", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, history, 1)
	assert.Equal(t, channelIDs[1], history[0].ChannelId, "legacy response projects the requested physical channel")
	assert.Equal(t, channelIDs[1], history[0].ActualChannelId, "actual execution member remains auditable after removal")
}

func TestChannelStatusProbeDisableRestoresEveryPhysicalConfigAndState(t *testing.T) {
	db, group, channelIDs := setupChannelStatusProbeLogicalGroupTest(t)
	physicalConfigs := []ChannelStatusProbeConfig{
		{ChannelId: channelIDs[0], Enabled: true, ModelsJSON: `["model-a"]`, IntervalSeconds: 300, NextRunAt: 1_200, Revision: 4},
		{ChannelId: channelIDs[1], Enabled: true, ModelsJSON: `["model-b"]`, IntervalSeconds: 30, NextRunAt: 1_050, Revision: 7},
	}
	require.NoError(t, db.Create(&physicalConfigs).Error)
	require.NoError(t, db.Create(&[]ChannelStatusProbeState{
		{ChannelId: channelIDs[0], ModelName: "model-a", Result: ChannelStatusProbeResultSuccess, FinishedAt: 900},
		{ChannelId: channelIDs[1], ModelName: "model-b", Result: ChannelStatusProbeResultUpstreamFailure, FinishedAt: 901},
	}).Error)

	effective, err := GetChannelStatusProbeConfig(channelIDs[1])
	require.NoError(t, err)
	assert.Equal(t, 300, effective.IntervalSeconds)
	models, err := effective.Models()
	require.NoError(t, err)
	assert.Equal(t, []string{"model-a"}, models)

	shared, err := SaveChannelStatusProbeConfig(channelIDs[1], ChannelStatusProbeConfigInput{
		Enabled: true, Models: []string{"shared-model"}, IntervalSeconds: 60, Revision: effective.Revision,
	}, 2_000)
	require.NoError(t, err)
	assert.Equal(t, int64(5), shared.Revision)

	require.NoError(t, db.Model(&ChannelLogicalGroup{}).Where("id = ?", group.Id).
		Updates(map[string]any{"status": ChannelLogicalGroupStatusDisabled, "revision": group.Revision + 1}).Error)
	InvalidateLogicalChannelRuntimeCache()

	first, err := GetChannelStatusProbeConfig(channelIDs[0])
	require.NoError(t, err)
	firstModels, err := first.Models()
	require.NoError(t, err)
	assert.Equal(t, []string{"model-a"}, firstModels)
	assert.Equal(t, 300, first.IntervalSeconds)
	assert.Equal(t, int64(4), first.Revision)

	second, err := GetChannelStatusProbeConfig(channelIDs[1])
	require.NoError(t, err)
	secondModels, err := second.Models()
	require.NoError(t, err)
	assert.Equal(t, []string{"model-b"}, secondModels)
	assert.Equal(t, 30, second.IntervalSeconds)
	assert.Equal(t, int64(7), second.Revision)

	states, err := GetChannelStatusProbeStates()
	require.NoError(t, err)
	require.Len(t, states, 2)
	assert.Equal(t, []int{channelIDs[0], channelIDs[1]}, []int{states[0].ChannelId, states[1].ChannelId})
	assert.Equal(t, []string{"model-a", "model-b"}, []string{states[0].ModelName, states[1].ModelName})
}

func TestChannelStatusProbeOldRevisionExecutionDoesNotOverwriteLogicalState(t *testing.T) {
	db, group, channelIDs := setupChannelStatusProbeLogicalGroupTest(t)
	config, err := SaveChannelStatusProbeConfig(channelIDs[0], ChannelStatusProbeConfigInput{
		Enabled: true, Models: []string{"gpt-a"}, IntervalSeconds: 60,
	}, 4_000)
	require.NoError(t, err)

	first := ChannelStatusProbeExecution{
		RunId: "revision-current", ChannelId: channelIDs[0], LogicalChannelId: group.Id, LogicalRevision: group.Revision,
		ActualChannelId: channelIDs[0], ModelName: "gpt-a", ConfigRevision: config.Revision,
		Trigger: ChannelStatusProbeTriggerScheduled, Result: ChannelStatusProbeResultSuccess,
		StartedAt: 4_001, FinishedAt: 4_002, RequestDispatched: true,
	}
	created, err := SaveChannelStatusProbeExecution(&first)
	require.NoError(t, err)
	assert.True(t, created)

	require.NoError(t, db.Model(&ChannelLogicalGroup{}).Where("id = ?", group.Id).
		Update("revision", group.Revision+1).Error)
	InvalidateLogicalChannelRuntimeCache()

	stale := ChannelStatusProbeExecution{
		RunId: "revision-stale", ChannelId: channelIDs[0], LogicalChannelId: group.Id, LogicalRevision: group.Revision,
		ActualChannelId: channelIDs[1], ModelName: "gpt-a", ConfigRevision: config.Revision,
		Trigger: ChannelStatusProbeTriggerScheduled, Result: ChannelStatusProbeResultUpstreamFailure,
		StartedAt: 4_010, FinishedAt: 4_011, RequestDispatched: true,
	}
	created, err = SaveChannelStatusProbeExecution(&stale)
	require.NoError(t, err)
	assert.True(t, created, "stale execution history remains auditable")

	var executionCount int64
	require.NoError(t, db.Model(&ChannelStatusProbeExecution{}).Count(&executionCount).Error)
	assert.EqualValues(t, 2, executionCount)
	var row ChannelStatusProbeLogicalState
	require.NoError(t, db.Where("logical_channel_id = ? AND model_name = ?", group.Id, "gpt-a").First(&row).Error)
	state, err := row.State(channelIDs[0])
	require.NoError(t, err)
	assert.Equal(t, "revision-current", state.RunId)
	assert.Equal(t, ChannelStatusProbeResultSuccess, state.Result)
	states, err := GetChannelStatusProbeStates()
	require.NoError(t, err)
	assert.Empty(t, states, "an aggregate from the previous relation revision must not project as current")

	current := ChannelStatusProbeExecution{
		RunId: "revision-new-current", ChannelId: channelIDs[0], LogicalChannelId: group.Id, LogicalRevision: group.Revision + 1,
		ActualChannelId: channelIDs[1], ModelName: "gpt-a", ConfigRevision: config.Revision,
		Trigger: ChannelStatusProbeTriggerScheduled, Result: ChannelStatusProbeResultUpstreamFailure,
		StartedAt: 4_020, FinishedAt: 4_021, RequestDispatched: true,
	}
	created, err = SaveChannelStatusProbeExecution(&current)
	require.NoError(t, err)
	assert.True(t, created)
	require.NoError(t, db.Where("logical_channel_id = ? AND model_name = ?", group.Id, "gpt-a").First(&row).Error)
	state, err = row.State(channelIDs[0])
	require.NoError(t, err)
	assert.Equal(t, "revision-new-current", state.RunId)
	assert.Equal(t, group.Revision+1, state.LogicalRevision)
	assert.Zero(t, state.ConsecutiveSuccesses, "new relation revision must not inherit old counters")
	assert.Equal(t, 1, state.ConsecutiveFailures)
}

func TestChannelStatusProbeGlobalDisableRestoresPhysicalConfigs(t *testing.T) {
	db, _, channelIDs := setupChannelStatusProbeLogicalGroupTest(t)
	t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "true")
	require.NoError(t, db.Create(&[]ChannelStatusProbeConfig{
		{ChannelId: channelIDs[0], Enabled: true, ModelsJSON: `["first"]`, IntervalSeconds: 300, Revision: 2},
		{ChannelId: channelIDs[1], Enabled: true, ModelsJSON: `["second"]`, IntervalSeconds: 30, Revision: 6},
	}).Error)

	effective, err := GetChannelStatusProbeConfig(channelIDs[1])
	require.NoError(t, err)
	_, err = SaveChannelStatusProbeConfig(channelIDs[1], ChannelStatusProbeConfigInput{
		Enabled: true, Models: []string{"shared"}, IntervalSeconds: 60, Revision: effective.Revision,
	}, 5_000)
	require.NoError(t, err)

	t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "false")
	for index, expected := range []struct {
		model    string
		interval int
		revision int64
	}{{"first", 300, 2}, {"second", 30, 6}} {
		config, err := GetChannelStatusProbeConfig(channelIDs[index])
		require.NoError(t, err)
		models, err := config.Models()
		require.NoError(t, err)
		assert.Equal(t, []string{expected.model}, models)
		assert.Equal(t, expected.interval, config.IntervalSeconds)
		assert.Equal(t, expected.revision, config.Revision)
	}
}

func TestChannelStatusProbeLogicalStateStorageRetainsHiddenBuckets(t *testing.T) {
	state := ChannelStatusProbeState{
		ChannelId: 41, LogicalChannelId: 99, LogicalRevision: 3, ModelName: "gpt-a", ExecutionId: 7,
		MinuteBucketsJSON: `[{"started_at":60,"success":1}]`,
		HourBucketsJSON:   `[{"started_at":3600,"upstream_failure":1}]`,
		DayBucketsJSON:    `[{"started_at":86400,"rate_limited":1}]`,
	}
	row, err := newChannelStatusProbeLogicalStateRow(state)
	require.NoError(t, err)
	restored, err := row.State(41)
	require.NoError(t, err)
	assert.Equal(t, state.MinuteBucketsJSON, restored.MinuteBucketsJSON)
	assert.Equal(t, state.HourBucketsJSON, restored.HourBucketsJSON)
	assert.Equal(t, state.DayBucketsJSON, restored.DayBucketsJSON)
}

func TestChannelStatusProbeLogicalMaterializationRejectsCapturedOldRevision(t *testing.T) {
	db, group, channelIDs := setupChannelStatusProbeLogicalGroupTest(t)
	require.NoError(t, db.Create(&ChannelStatusProbeConfig{
		ChannelId: channelIDs[0], Enabled: true, ModelsJSON: `["gpt-a"]`, IntervalSeconds: 60, Revision: 3,
	}).Error)
	scope, err := resolveChannelStatusProbeScope(channelIDs[0])
	require.NoError(t, err)
	require.Equal(t, group.Revision, scope.Identity.Revision)

	require.NoError(t, db.Model(&ChannelLogicalGroup{}).Where("id = ?", group.Id).
		Update("revision", group.Revision+1).Error)
	var materializeErr error
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, materializeErr = materializeChannelStatusProbeLogicalConfig(tx, scope, 6_000)
		return nil
	}))
	assert.ErrorIs(t, materializeErr, ErrChannelLogicalGroupRevisionConflict)

	var logicalCount int64
	require.NoError(t, db.Model(&ChannelStatusProbeLogicalConfig{}).Count(&logicalCount).Error)
	assert.Zero(t, logicalCount, "a stale captured scope must not materialize shared configuration")
}

func TestChannelStatusProbeOverviewRelationsReadCurrentDatabaseWithFixedQueries(t *testing.T) {
	db, group, channelIDs := setupChannelStatusProbeLogicalGroupTest(t)
	require.NoError(t, db.Create(&[]ChannelStatusProbeConfig{
		{ChannelId: channelIDs[0], Enabled: true, ModelsJSON: `["model-a"]`, IntervalSeconds: 60, Revision: 1},
		{ChannelId: channelIDs[1], Enabled: true, ModelsJSON: `["model-b"]`, IntervalSeconds: 120, Revision: 1},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelStatusProbeState{
		{ChannelId: channelIDs[0], ModelName: "model-a", Result: ChannelStatusProbeResultSuccess, FinishedAt: 100},
		{ChannelId: channelIDs[1], ModelName: "model-b", Result: ChannelStatusProbeResultUpstreamFailure, FinishedAt: 101},
	}).Error)
	for channelID := 8200; channelID < 8220; channelID++ {
		require.NoError(t, db.Create(&Channel{
			Id: channelID, Name: "physical-probe-channel", Key: "test-key", Status: common.ChannelStatusEnabled, Models: "model-c",
		}).Error)
		require.NoError(t, db.Create(&ChannelStatusProbeConfig{
			ChannelId: channelID, Enabled: true, ModelsJSON: `["model-c"]`, IntervalSeconds: 300, Revision: 1,
		}).Error)
		require.NoError(t, db.Create(&ChannelStatusProbeState{
			ChannelId: channelID, ModelName: "model-c", Result: ChannelStatusProbeResultSuccess, FinishedAt: int64(channelID),
		}).Error)
	}

	staleRelations, err := buildLogicalChannelRuntimeSnapshot(db)
	require.NoError(t, err)
	channelSyncLock.Lock()
	previousRelations := logicalChannelRuntimeCache
	previousDirty := logicalChannelRuntimeDirty
	logicalChannelRuntimeCache = staleRelations
	logicalChannelRuntimeDirty = true
	channelSyncLock.Unlock()
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCache
		channelSyncLock.Lock()
		logicalChannelRuntimeCache = previousRelations
		logicalChannelRuntimeDirty = previousDirty
		channelSyncLock.Unlock()
	})

	require.NoError(t, db.Where("logical_group_id = ? AND channel_id = ?", group.Id, channelIDs[1]).
		Delete(&ChannelLogicalGroupMember{}).Error)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channelIDs[1]).Update("logical_channel_id", nil).Error)
	require.NoError(t, db.Model(&ChannelLogicalGroup{}).Where("id = ?", group.Id).
		Update("revision", group.Revision+1).Error)

	var queryCount atomic.Int64
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:count_status_probe_overview_relations", func(*gorm.DB) {
		queryCount.Add(1)
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Query().Remove("test:count_status_probe_overview_relations"))
	})

	relations, err := LoadChannelStatusProbeOverviewRelations(context.Background(), db)
	require.NoError(t, err)
	configs, err := GetChannelStatusProbeConfigsForOverview(context.Background(), db, relations)
	require.NoError(t, err)
	states, err := GetChannelStatusProbeStatesForOverview(context.Background(), db, nil, nil, "", relations)
	require.NoError(t, err)
	assert.LessOrEqual(t, queryCount.Load(), int64(7), "overview relationship projection must use fixed batch queries")
	require.Len(t, configs, 22)
	require.Len(t, states, 22)

	configByChannel := make(map[int]ChannelStatusProbeConfig, len(configs))
	for _, config := range configs {
		configByChannel[config.ChannelId] = config
	}
	firstModels, err := configByChannel[channelIDs[0]].Models()
	require.NoError(t, err)
	secondModels, err := configByChannel[channelIDs[1]].Models()
	require.NoError(t, err)
	assert.Equal(t, []string{"model-a"}, firstModels)
	assert.Equal(t, []string{"model-b"}, secondModels, "overview must not project the stale cached group onto the removed member")
	assert.Equal(t, group.Revision+1, configByChannel[channelIDs[0]].LogicalRevision)
	assert.Zero(t, configByChannel[channelIDs[1]].LogicalRevision)

	stateByChannel := make(map[int]ChannelStatusProbeState, len(states))
	for _, state := range states {
		stateByChannel[state.ChannelId] = state
	}
	assert.Equal(t, ChannelStatusProbeResultSuccess, stateByChannel[channelIDs[0]].Result)
	assert.Equal(t, ChannelStatusProbeResultUpstreamFailure, stateByChannel[channelIDs[1]].Result)
}
