package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func closeChannelModelDetectionLogicalGroupTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
}

func TestChannelModelDetectionLogicalGroupCreatesOneRunAndFreezesIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "model-detection-logical-group.db")), &gorm.Config{})
	require.NoError(t, err)
	closeChannelModelDetectionLogicalGroupTestDB(t, db)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{}, &model.ChannelLogicalGroup{}, &model.ChannelLogicalGroupMember{},
		&model.ChannelModelDetectionGlobalConfig{}, &model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionTarget{}, &model.ChannelModelDetectionLogicalConfig{},
		&model.ChannelModelDetectionLogicalTarget{}, &model.ChannelModelDetectionBatch{},
		&model.ChannelModelDetectionRun{}, &model.ChannelModelDetectionExecution{},
	))
	address := "https://api.example.com/v1"
	channels := []model.Channel{
		{Id: 901, Name: "key-a", Key: "a", BaseURL: &address, Status: common.ChannelStatusEnabled},
		{Id: 902, Name: "key-b", Key: "b", BaseURL: &address, Status: common.ChannelStatusEnabled},
		{Id: 903, Name: "key-c", Key: "c", BaseURL: &address, Status: common.ChannelStatusEnabled},
	}
	require.NoError(t, db.Create(&channels).Error)
	group := model.ChannelLogicalGroup{Name: "shared", Status: model.ChannelLogicalGroupStatusEnabled, Revision: 4}
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&[]model.ChannelLogicalGroupMember{
		{LogicalGroupID: group.Id, ChannelID: 901, Weight: 1, AddressFingerprint: LogicalChannelAddressFingerprint(address)},
		{LogicalGroupID: group.Id, ChannelID: 902, Weight: 3, AddressFingerprint: LogicalChannelAddressFingerprint(address)},
	}).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id IN ?", []int{901, 902}).Update("logical_channel_id", group.Id).Error)
	config := model.ChannelModelDetectionConfig{ChannelId: 901, ScheduleEnabled: true, Revision: 2}
	require.NoError(t, db.Create(&config).Error)
	require.NoError(t, db.Create(&model.ChannelModelDetectionTarget{ConfigId: config.Id, ChannelId: 901, TargetKey: "target-a", RequestModel: "alias", ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Enabled: true}).Error)
	// A second member may have an independent legacy config; it must not create
	// a second shared target/run when the canonical member already has targets.
	secondConfig := model.ChannelModelDetectionConfig{ChannelId: 902, ScheduleEnabled: true, Revision: 9}
	require.NoError(t, db.Create(&secondConfig).Error)
	require.NoError(t, db.Create(&model.ChannelModelDetectionTarget{ConfigId: secondConfig.Id, ChannelId: 902, TargetKey: "target-b", RequestModel: "alias", ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Enabled: true}).Error)
	global := model.ChannelModelDetectionGlobalConfig{Revision: 1, ScheduledPreset: model.ChannelModelDetectionPresetLow}
	require.NoError(t, db.Create(&global).Error)
	batch := model.ChannelModelDetectionBatch{Preset: model.ChannelModelDetectionPresetLow, ScheduledFor: 1}
	require.NoError(t, db.Create(&batch).Error)
	runIDs, err := createChannelModelDetectionScheduledRuns(db, global, &batch, 2, false)
	require.NoError(t, err)
	require.Len(t, runIDs, 1)
	var runs []model.ChannelModelDetectionRun
	require.NoError(t, db.Find(&runs).Error)
	require.Len(t, runs, 1)
	assert.EqualValues(t, group.Id, runs[0].LogicalChannelID)
	assert.EqualValues(t, group.Revision, runs[0].LogicalRevision)
	assert.Equal(t, 901, runs[0].ChannelId)
	members, err := runs[0].LogicalMemberSnapshot()
	require.NoError(t, err)
	assert.Equal(t, []model.ChannelModelDetectionMemberSnapshot{{ChannelID: 901, Weight: 1}, {ChannelID: 902, Weight: 3}}, members)
	var executions []model.ChannelModelDetectionExecution
	require.NoError(t, db.Find(&executions).Error)
	require.Len(t, executions, 1)
	assert.Equal(t, 901, executions[0].ChannelId)
	assert.EqualValues(t, group.Id, executions[0].LogicalChannelID)
	require.NotNil(t, executions[0].LogicalTargetId)
	assert.Equal(t, executions[0].TargetId, *executions[0].LogicalTargetId)

	newRevision := group.Revision + 1
	require.NoError(t, db.Where("logical_group_id = ?", group.Id).Delete(&model.ChannelLogicalGroupMember{}).Error)
	require.NoError(t, db.Create(&[]model.ChannelLogicalGroupMember{
		{LogicalGroupID: group.Id, ChannelID: 901, Weight: 2, AddressFingerprint: LogicalChannelAddressFingerprint(address)},
		{LogicalGroupID: group.Id, ChannelID: 903, Weight: 5, AddressFingerprint: LogicalChannelAddressFingerprint(address)},
	}).Error)
	require.NoError(t, db.Model(&model.ChannelLogicalGroup{}).Where("id = ?", group.Id).Update("revision", newRevision).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 902).Update("logical_channel_id", nil).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 903).Update("logical_channel_id", group.Id).Error)
	require.NoError(t, db.Where("id = ?", runs[0].Id).First(&runs[0]).Error)
	assert.EqualValues(t, group.Revision, runs[0].LogicalRevision, "active run keeps its frozen relation revision")
	members, err = runs[0].LogicalMemberSnapshot()
	require.NoError(t, err)
	assert.Equal(t, []model.ChannelModelDetectionMemberSnapshot{{ChannelID: 901, Weight: 1}, {ChannelID: 902, Weight: 3}}, members)
	_, err = CreateChannelModelDetectionManualRun(context.Background(), db, ChannelModelDetectionManualRunInput{ChannelID: 901, Preset: model.ChannelModelDetectionPresetLow}, time.Unix(3, 0))
	assert.ErrorIs(t, err, ErrChannelModelDetectionRunAlreadyActive)
	released, err := model.ReleaseChannelModelDetectionRun(db, runs[0].ChannelId, runs[0].RunId, 4)
	require.NoError(t, err)
	require.True(t, released)
	newRun, err := CreateChannelModelDetectionManualRun(context.Background(), db, ChannelModelDetectionManualRunInput{ChannelID: 903, Preset: model.ChannelModelDetectionPresetLow}, time.Unix(5, 0))
	require.NoError(t, err)
	assert.EqualValues(t, newRevision, newRun.LogicalRevision)
	newMembers, err := newRun.LogicalMemberSnapshot()
	require.NoError(t, err)
	assert.Equal(t, []model.ChannelModelDetectionMemberSnapshot{{ChannelID: 901, Weight: 2}, {ChannelID: 903, Weight: 5}}, newMembers)
}

func TestChannelModelDetectionUngroupedScheduleKeepsPhysicalIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "model-detection-physical.db")), &gorm.Config{})
	require.NoError(t, err)
	closeChannelModelDetectionLogicalGroupTestDB(t, db)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{}, &model.ChannelLogicalGroup{}, &model.ChannelLogicalGroupMember{},
		&model.ChannelModelDetectionGlobalConfig{}, &model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionTarget{}, &model.ChannelModelDetectionLogicalConfig{},
		&model.ChannelModelDetectionLogicalTarget{}, &model.ChannelModelDetectionBatch{},
		&model.ChannelModelDetectionRun{}, &model.ChannelModelDetectionExecution{},
	))
	require.NoError(t, db.Create(&model.Channel{Id: 911, Name: "physical", Status: common.ChannelStatusEnabled}).Error)
	config := model.ChannelModelDetectionConfig{ChannelId: 911, ScheduleEnabled: true, Revision: 2}
	require.NoError(t, db.Create(&config).Error)
	require.NoError(t, db.Create(&model.ChannelModelDetectionTarget{ConfigId: config.Id, ChannelId: 911, TargetKey: "physical-target", RequestModel: "alias", ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Enabled: true}).Error)
	global := model.ChannelModelDetectionGlobalConfig{Revision: 1, ScheduledPreset: model.ChannelModelDetectionPresetLow}
	require.NoError(t, db.Create(&global).Error)
	batch := model.ChannelModelDetectionBatch{Preset: model.ChannelModelDetectionPresetLow, ScheduledFor: 1}
	require.NoError(t, db.Create(&batch).Error)

	runIDs, err := createChannelModelDetectionScheduledRuns(db, global, &batch, 2, false)
	require.NoError(t, err)
	require.Len(t, runIDs, 1)
	var run model.ChannelModelDetectionRun
	require.NoError(t, db.First(&run).Error)
	assert.Equal(t, 911, run.ChannelId)
	assert.EqualValues(t, 911, run.LogicalChannelID)
	assert.Zero(t, run.LogicalRevision)
	var execution model.ChannelModelDetectionExecution
	require.NoError(t, db.First(&execution).Error)
	assert.Equal(t, 911, execution.ChannelId)
	assert.EqualValues(t, 911, execution.LogicalChannelID)
	assert.Zero(t, execution.LogicalRevision)
}

func TestChannelModelDetectionGroupedConfigUsesIndependentLogicalStorage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "model-detection-shared-config.db")), &gorm.Config{})
	require.NoError(t, err)
	closeChannelModelDetectionLogicalGroupTestDB(t, db)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{}, &model.ChannelLogicalGroup{}, &model.ChannelLogicalGroupMember{},
		&model.ChannelModelDetectionGlobalConfig{}, &model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionTarget{}, &model.ChannelModelDetectionLogicalConfig{},
		&model.ChannelModelDetectionLogicalTarget{}, &model.ChannelModelDetectionRun{}, &model.ChannelModelDetectionExecution{},
	))
	address := "https://api.example.com/v1"
	channels := []model.Channel{
		{Id: 921, Name: "key-a", Models: "alias", BaseURL: &address, Status: common.ChannelStatusEnabled},
		{Id: 922, Name: "key-b", Models: "alias", BaseURL: &address, Status: common.ChannelStatusEnabled},
	}
	require.NoError(t, db.Create(&channels).Error)
	group := model.ChannelLogicalGroup{Name: "shared-config", Status: model.ChannelLogicalGroupStatusEnabled, Revision: 2}
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&[]model.ChannelLogicalGroupMember{
		{LogicalGroupID: group.Id, ChannelID: 921, Weight: 1, AddressFingerprint: LogicalChannelAddressFingerprint(address)},
		{LogicalGroupID: group.Id, ChannelID: 922, Weight: 1, AddressFingerprint: LogicalChannelAddressFingerprint(address)},
	}).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id IN ?", []int{921, 922}).Update("logical_channel_id", group.Id).Error)

	response, err := UpdateChannelModelDetectionConfig(context.Background(), db, 922, ChannelModelDetectionConfigUpdateInput{
		ExpectedRevision: 0,
		Targets:          []ChannelModelDetectionTargetUpdateInput{{RequestModel: "alias", ClaimedModel: model.ChannelModelDetectionClaimedModelSol}},
	}, time.Unix(10, 0))
	require.NoError(t, err)
	assert.Equal(t, 922, response.ChannelID, "API remains scoped to the requested physical channel")
	assert.EqualValues(t, 1, response.Revision)
	var configs []model.ChannelModelDetectionConfig
	require.NoError(t, db.Find(&configs).Error)
	require.Empty(t, configs, "group config must not overwrite a physical member")
	var targets []model.ChannelModelDetectionTarget
	require.NoError(t, db.Find(&targets).Error)
	require.Empty(t, targets)
	var logicalConfig model.ChannelModelDetectionLogicalConfig
	require.NoError(t, db.Where("logical_channel_id = ?", group.Id).First(&logicalConfig).Error)
	assert.EqualValues(t, 1, logicalConfig.Revision)
	var logicalTargets []model.ChannelModelDetectionLogicalTarget
	require.NoError(t, db.Where("logical_channel_id = ?", group.Id).Find(&logicalTargets).Error)
	require.Len(t, logicalTargets, 1)
	assert.Equal(t, "alias", logicalTargets[0].RequestModel)
}

func TestChannelModelDetectionLogicalConfigPreservesPhysicalFallbackAndUsesMemberModelUnion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "model-detection-logical-config-lifecycle.db")), &gorm.Config{})
	require.NoError(t, err)
	closeChannelModelDetectionLogicalGroupTestDB(t, db)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{}, &model.ChannelLogicalGroup{}, &model.ChannelLogicalGroupMember{},
		&model.ChannelModelDetectionGlobalConfig{}, &model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionTarget{}, &model.ChannelModelDetectionLogicalConfig{},
		&model.ChannelModelDetectionLogicalTarget{}, &model.ChannelModelDetectionRun{},
	))
	address := "https://api.example.com/v1"
	channels := []model.Channel{
		{Id: 923, Name: "alpha-key", Models: "alpha", BaseURL: &address, Status: common.ChannelStatusEnabled},
		{Id: 924, Name: "beta-key", Models: "beta", BaseURL: &address, Status: common.ChannelStatusEnabled},
	}
	require.NoError(t, db.Create(&channels).Error)
	physicalConfigs := []model.ChannelModelDetectionConfig{
		{ChannelId: 923, ScheduleEnabled: true, Revision: 2, CreatedAt: 1, UpdatedAt: 1},
		{ChannelId: 924, ScheduleEnabled: false, Revision: 7, CreatedAt: 2, UpdatedAt: 2},
	}
	require.NoError(t, db.Create(&physicalConfigs).Error)
	physicalTargets := []model.ChannelModelDetectionTarget{
		{ConfigId: physicalConfigs[0].Id, ChannelId: 923, TargetKey: "physical-alpha", RequestModel: "alpha", ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Enabled: true},
		{ConfigId: physicalConfigs[1].Id, ChannelId: 924, TargetKey: "physical-beta", RequestModel: "beta", ClaimedModel: model.ChannelModelDetectionClaimedModelTerra, Enabled: true},
	}
	require.NoError(t, db.Create(&physicalTargets).Error)
	group := model.ChannelLogicalGroup{Name: "model-union", Status: model.ChannelLogicalGroupStatusEnabled, Revision: 3}
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&[]model.ChannelLogicalGroupMember{
		{LogicalGroupID: group.Id, ChannelID: 923, Weight: 1, AddressFingerprint: LogicalChannelAddressFingerprint(address)},
		{LogicalGroupID: group.Id, ChannelID: 924, Weight: 1, AddressFingerprint: LogicalChannelAddressFingerprint(address)},
	}).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id IN ?", []int{923, 924}).Update("logical_channel_id", group.Id).Error)
	require.NoError(t, model.EnsureChannelModelDetectionLogicalConfigTx(db, group.Id, []int{923, 924}))

	response, err := UpdateChannelModelDetectionConfig(context.Background(), db, 924, ChannelModelDetectionConfigUpdateInput{
		ExpectedRevision: 2,
		Targets: []ChannelModelDetectionTargetUpdateInput{
			{RequestModel: "alpha", ClaimedModel: model.ChannelModelDetectionClaimedModelSol},
			{RequestModel: "beta", ClaimedModel: model.ChannelModelDetectionClaimedModelTerra},
		},
	}, time.Unix(10, 0))
	require.NoError(t, err)
	assert.Equal(t, 924, response.ChannelID)
	assert.EqualValues(t, 3, response.Revision)
	_, err = UpdateChannelModelDetectionConfig(context.Background(), db, 923, ChannelModelDetectionConfigUpdateInput{
		ExpectedRevision: response.Revision,
		Targets: []ChannelModelDetectionTargetUpdateInput{
			{RequestModel: "gamma", ClaimedModel: model.ChannelModelDetectionClaimedModelSol},
		},
	}, time.Unix(11, 0))
	assert.ErrorIs(t, err, ErrChannelModelDetectionInvalidConfig)

	var storedPhysicalConfigs []model.ChannelModelDetectionConfig
	require.NoError(t, db.Order("channel_id ASC").Find(&storedPhysicalConfigs).Error)
	require.Len(t, storedPhysicalConfigs, 2)
	assert.Equal(t, physicalConfigs, storedPhysicalConfigs)
	var storedPhysicalTargets []model.ChannelModelDetectionTarget
	require.NoError(t, db.Order("channel_id ASC").Find(&storedPhysicalTargets).Error)
	require.Len(t, storedPhysicalTargets, 2)
	for index := range physicalTargets {
		assert.Equal(t, physicalTargets[index].TargetKey, storedPhysicalTargets[index].TargetKey)
		assert.Equal(t, physicalTargets[index].RequestModel, storedPhysicalTargets[index].RequestModel)
		assert.Equal(t, physicalTargets[index].ClaimedModel, storedPhysicalTargets[index].ClaimedModel)
	}

	var logicalTargets []model.ChannelModelDetectionLogicalTarget
	require.NoError(t, db.Where("logical_channel_id = ?", group.Id).Order("position ASC").Find(&logicalTargets).Error)
	require.Len(t, logicalTargets, 2)
	sharedTargetKeys := []string{logicalTargets[0].TargetKey, logicalTargets[1].TargetKey}
	require.NoError(t, db.Where("logical_group_id = ? AND channel_id = ?", group.Id, 923).Delete(&model.ChannelLogicalGroupMember{}).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 923).Update("logical_channel_id", nil).Error)
	require.NoError(t, db.Model(&model.ChannelLogicalGroup{}).Where("id = ?", group.Id).Update("revision", int64(4)).Error)
	identity, err := channelModelDetectionLogicalIdentity(db, 924, false)
	require.NoError(t, err)
	sharedConfig, sharedTargets, ownerID, err := channelModelDetectionConfigForIdentity(db, identity, 924, false)
	require.NoError(t, err)
	assert.Equal(t, 924, ownerID)
	assert.EqualValues(t, response.Revision, sharedConfig.Revision)
	assert.Equal(t, sharedTargetKeys, []string{sharedTargets[0].TargetKey, sharedTargets[1].TargetKey})

	require.NoError(t, db.Model(&model.ChannelLogicalGroup{}).Where("id = ?", group.Id).Update("status", model.ChannelLogicalGroupStatusDisabled).Error)
	identity, err = channelModelDetectionLogicalIdentity(db, 924, false)
	require.NoError(t, err)
	physicalConfig, restoredTargets, restoredOwnerID, err := channelModelDetectionConfigForIdentity(db, identity, 924, false)
	require.NoError(t, err)
	assert.Equal(t, 924, restoredOwnerID)
	assert.EqualValues(t, 7, physicalConfig.Revision)
	require.Len(t, restoredTargets, 1)
	assert.Equal(t, "physical-beta", restoredTargets[0].TargetKey)
}

func TestChannelModelDetectionOverviewSharesResultButKeepsPhysicalCost(t *testing.T) {
	logicalID := int64(17)
	now := int64(1_800_000_000)
	channels := []model.Channel{
		{Id: 941, Name: "key-a", LogicalChannelID: &logicalID, Status: common.ChannelStatusEnabled},
		{Id: 942, Name: "key-b", LogicalChannelID: &logicalID, Status: common.ChannelStatusEnabled},
	}
	config := model.ChannelModelDetectionConfig{Id: 51, ChannelId: 941, ScheduleEnabled: true, Revision: 3}
	target := model.ChannelModelDetectionTarget{Id: 61, ConfigId: config.Id, ChannelId: 941, TargetKey: "shared-target", RequestModel: "alias", ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Enabled: true}
	run := model.ChannelModelDetectionRun{
		Id: 71, RunId: "shared-run", ChannelId: 941, LogicalChannelID: logicalID, LogicalRevision: 5,
		Trigger: model.ChannelModelDetectionTriggerScheduled, Preset: model.ChannelModelDetectionPresetLow,
		PresetSource: model.ChannelModelDetectionPresetSourceScheduledDefault, Status: model.ChannelModelDetectionRunStatusCompleted,
		TargetCount: 1, CompletedTargetCount: 1, QueuedAt: now - 60, FinishedAt: now - 30, UpdatedAt: now - 30,
	}
	execution := model.ChannelModelDetectionExecution{
		Id: 81, RunId: run.RunId, TargetKey: target.TargetKey, TargetId: target.Id, ChannelId: 942,
		LogicalChannelID: logicalID, LogicalRevision: run.LogicalRevision, RequestModel: target.RequestModel,
		ClaimedModel: target.ClaimedModel, Preset: run.Preset, Status: model.ChannelModelDetectionExecutionStatusCompleted,
		OutcomeCode: "success", CreatedAt: now - 60, FinishedAt: now - 30, UpdatedAt: now - 30,
	}
	memberAQuota, memberACost := int64(10), int64(100)
	memberBQuota, memberBCost := int64(20), int64(250)
	response, err := buildChannelModelDetectionOverview(
		now, model.ChannelModelDetectionGlobalConfig{DetectorURL: "http://127.0.0.1:18080", ScheduledPreset: model.ChannelModelDetectionPresetLow},
		channels, []model.ChannelModelDetectionConfig{config}, []model.ChannelModelDetectionTarget{target}, []model.ChannelModelDetectionRun{run},
		[]channelModelDetectionExecutionOverviewRow{{ChannelModelDetectionExecution: execution, RunTrigger: run.Trigger, RunPresetSource: run.PresetSource}}, []model.ChannelModelDetectionCostEvent{
			{CostEventId: "overview-a", RunId: run.RunId, TargetId: target.Id, ExecutionId: execution.Id, ChannelId: 941, RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel, Preset: run.Preset, DetectorRequestId: "a", AttemptNo: 1, DispatchState: model.ChannelModelDetectionDispatchDispatched, SettlementStatus: model.ChannelModelDetectionSettlementSettled, UsageSource: model.ChannelModelDetectionUsageUpstreamAuthoritative, UsageAvailable: true, SettledQuota: &memberAQuota, CostBasisQuota: &memberAQuota, SettledCostNanoCNY: &memberACost, CostScope: model.ChannelModelDetectionCostScopeChannelUpstreamAPI},
			{CostEventId: "overview-b", RunId: run.RunId, TargetId: target.Id, ExecutionId: execution.Id, ChannelId: 942, RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel, Preset: run.Preset, DetectorRequestId: "b", AttemptNo: 2, DispatchState: model.ChannelModelDetectionDispatchDispatched, SettlementStatus: model.ChannelModelDetectionSettlementSettled, UsageSource: model.ChannelModelDetectionUsageUpstreamAuthoritative, UsageAvailable: true, SettledQuota: &memberBQuota, CostBasisQuota: &memberBQuota, SettledCostNanoCNY: &memberBCost, CostScope: model.ChannelModelDetectionCostScopeChannelUpstreamAPI},
		},
		[]model.ChannelDailyCost{
			{ChannelId: 941, ModelDetectionCostNanoCNY: 100},
			{ChannelId: 942, ModelDetectionCostNanoCNY: 250},
		}, nil,
	)
	require.NoError(t, err)
	require.Len(t, response.Channels, 2)
	for _, channel := range response.Channels {
		require.Len(t, channel.Targets, 1)
		assert.Equal(t, target.TargetKey, channel.Targets[0].TargetKey)
		require.NotNil(t, channel.Targets[0].Latest)
	}
	assert.InDelta(t, 100.0/float64(model.ChannelDailyCostNanoPerCNY), response.Channels[0].TodayModelDetectionCostCNY, 0.000000001)
	assert.InDelta(t, 250.0/float64(model.ChannelDailyCostNanoPerCNY), response.Channels[1].TodayModelDetectionCostCNY, 0.000000001)
	require.NotNil(t, response.Channels[0].LatestRunCost)
	require.NotNil(t, response.Channels[0].LatestRunCost.SettledCostCNY)
	assert.Equal(t, "0.000000100", *response.Channels[0].LatestRunCost.SettledCostCNY)
	require.NotNil(t, response.Channels[1].LatestRunCost)
	require.NotNil(t, response.Channels[1].LatestRunCost.SettledCostCNY)
	assert.Equal(t, "0.000000250", *response.Channels[1].LatestRunCost.SettledCostCNY)
}

func TestChannelModelDetectionPhysicalCostViewDoesNotCopyAnotherMemberCost(t *testing.T) {
	settledCost := int64(250)
	run := model.ChannelModelDetectionRun{
		LogicalChannelID: 77, LogicalRevision: 3, SettledCostNanoCNY: &settledCost, SettledRequestCount: 1,
	}
	allRunEvents := []model.ChannelModelDetectionCostEvent{{ChannelId: 941, SettledCostNanoCNY: &settledCost}}
	aggregate, err := aggregateChannelModelDetectionCostForPhysicalView(run, nil, allRunEvents, true)
	require.NoError(t, err)
	require.NotNil(t, aggregate.SettledCostNanoCNY)
	assert.Zero(t, *aggregate.SettledCostNanoCNY)
	assert.Zero(t, aggregate.SettledRequestCount)
	assert.Equal(t, ChannelModelDetectionCostStatusNotStarted, aggregate.Status)
}

func TestChannelModelDetectorGroupedRetryUsesPhysicalConcurrencyAndCost(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "model-detection-member-retry.db")), &gorm.Config{})
	require.NoError(t, err)
	closeChannelModelDetectionLogicalGroupTestDB(t, db)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{}, &model.ChannelLogicalGroup{}, &model.ChannelLogicalGroupMember{},
		&model.ChannelModelDetectionExecution{}, &model.ChannelModelDetectionCostEvent{},
	))
	previousDB := model.DB
	previousMemoryCache := common.MemoryCacheEnabled
	model.DB = db
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCache
	})
	address := "https://api.example.com/v1"
	group := model.ChannelLogicalGroup{Name: "retry", Status: model.ChannelLogicalGroupStatusEnabled, Revision: 7}
	require.NoError(t, db.Create(&group).Error)
	channels := []model.Channel{
		{Id: 931, Name: "key-a", LogicalChannelID: &group.Id, BaseURL: &address, Models: "alias", Status: common.ChannelStatusEnabled},
		{Id: 932, Name: "key-b", LogicalChannelID: &group.Id, BaseURL: &address, Models: "alias", Status: common.ChannelStatusEnabled},
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&[]model.ChannelLogicalGroupMember{
		{LogicalGroupID: group.Id, ChannelID: 931, Weight: 1, AddressFingerprint: LogicalChannelAddressFingerprint(address)},
		{LogicalGroupID: group.Id, ChannelID: 932, Weight: 0, AddressFingerprint: LogicalChannelAddressFingerprint(address)},
	}).Error)
	execution := model.ChannelModelDetectionExecution{
		RunId: "logical-run", TargetKey: "logical-target", TargetId: 41, ChannelId: 931,
		LogicalChannelID: group.Id, LogicalRevision: group.Revision, RequestModel: "alias",
		ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Preset: model.ChannelModelDetectionPresetLow,
		Status: model.ChannelModelDetectionExecutionStatusPending,
	}
	require.NoError(t, db.Create(&execution).Error)

	now := time.Unix(1_800_000_000, 0).UTC()
	store := newTestChannelModelDetectorTokenStore(t, &now)
	credential, err := store.Issue(ChannelModelDetectorTokenSpec{
		RunID: execution.RunId, TargetID: execution.TargetId, ExecutionID: execution.Id, ChannelID: 931,
		LogicalChannelID: group.Id, LogicalRevision: group.Revision, RequestModel: execution.RequestModel,
		LogicalMembers: []model.ChannelModelDetectionMemberSnapshot{{ChannelID: 931, Weight: 1}, {ChannelID: 932, Weight: 0}},
		ClaimedModel:   execution.ClaimedModel, Preset: execution.Preset,
		RelayBaseURL: "http://127.0.0.1:3000/internal/model-detector", MaxHTTPAttempts: 2, ExpiresAt: now.Add(time.Hour).Unix(),
	})
	require.NoError(t, err)
	require.NoError(t, db.Where("logical_group_id = ?", group.Id).Delete(&model.ChannelLogicalGroupMember{}).Error)
	require.NoError(t, db.Delete(&group).Error)
	executor := &channelModelDetectorRelayExecutorStub{err: errors.New("retryable transport failure")}
	acquired := make([]int, 0, 2)
	relay, err := newChannelModelDetectorRelay(store, executor, func(_ context.Context, channelID int) (channelModelDetectorConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
		acquired = append(acquired, channelID)
		return &channelModelDetectorRelayLeaseStub{}, true, ChannelConcurrencyStatus{}, nil
	})
	require.NoError(t, err)
	body := []byte(`{"model":"gpt-5.6-sol"}`)
	_, err = relay.Execute(context.Background(), ChannelModelDetectorRelayRequest{BearerToken: credential.BearerToken(), DetectorRequestID: "attempt-1", Body: body})
	assert.EqualError(t, err, "retryable transport failure")
	executor.err = nil
	executor.result = ChannelModelDetectorRelayUpstreamResult{Dispatched: true, Usage: &ChannelModelDetectorUsage{Available: true, Source: model.ChannelModelDetectionUsageUpstreamAuthoritative, InputTokens: 1, OutputTokens: 1, TotalTokens: 2}}
	_, err = relay.Execute(context.Background(), ChannelModelDetectorRelayRequest{BearerToken: credential.BearerToken(), DetectorRequestID: "attempt-2", Body: body})
	require.NoError(t, err)
	assert.Equal(t, []int{931, 932}, acquired, "existing concurrency is acquired by the selected physical member")
	require.Len(t, executor.executions, 2)
	assert.Equal(t, 931, executor.executions[0].ChannelID)
	assert.Equal(t, 932, executor.executions[1].ChannelID)
	require.NoError(t, db.First(&execution, execution.Id).Error)
	assert.Equal(t, 932, execution.ChannelId)

	cost, created, err := PrepareChannelModelDetectionCostEvent(context.Background(), db, ChannelModelDetectionCostAttemptInput{
		CostEventId: "physical-member-cost", RunId: execution.RunId, TargetId: execution.TargetId, ExecutionId: execution.Id,
		ChannelId: executor.executions[1].ChannelID, RequestModel: execution.RequestModel, ClaimedModel: execution.ClaimedModel,
		Preset: execution.Preset, DetectorRequestId: "attempt-2", AttemptNo: 2, CreatedAt: now.Unix(),
	})
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, 932, cost.ChannelId)
	var logicalCostCount int64
	require.NoError(t, db.Model(&model.ChannelModelDetectionCostEvent{}).Where("channel_id = ?", group.Id).Count(&logicalCostCount).Error)
	assert.Zero(t, logicalCostCount)
}

func TestChannelModelDetectorGroupedRelayFiltersUnsupportedModels(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "model-detection-model-filter.db")), &gorm.Config{})
	require.NoError(t, err)
	closeChannelModelDetectionLogicalGroupTestDB(t, db)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelModelDetectionExecution{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 951, Name: "unsupported", Models: "other-model", Status: common.ChannelStatusEnabled},
		{Id: 952, Name: "supported", Models: "alias", Status: common.ChannelStatusEnabled},
	}).Error)
	execution := model.ChannelModelDetectionExecution{
		RunId: "model-filter-run", TargetKey: "target", TargetId: 51, ChannelId: 951,
		LogicalChannelID: 801, LogicalRevision: 2, RequestModel: "alias",
		ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Preset: model.ChannelModelDetectionPresetLow,
		Status: model.ChannelModelDetectionExecutionStatusPending,
	}
	require.NoError(t, db.Create(&execution).Error)
	now := time.Unix(1_800_000_000, 0).UTC()
	store := newTestChannelModelDetectorTokenStore(t, &now)
	credential, err := store.Issue(ChannelModelDetectorTokenSpec{
		RunID: execution.RunId, TargetID: execution.TargetId, ExecutionID: execution.Id, ChannelID: 951,
		LogicalChannelID: execution.LogicalChannelID, LogicalRevision: execution.LogicalRevision,
		LogicalMembers: []model.ChannelModelDetectionMemberSnapshot{{ChannelID: 951, Weight: 100}, {ChannelID: 952, Weight: 0}},
		RequestModel:   execution.RequestModel, ClaimedModel: execution.ClaimedModel, Preset: execution.Preset,
		RelayBaseURL: "http://127.0.0.1:3000/internal/model-detector", MaxHTTPAttempts: 2, ExpiresAt: now.Add(time.Hour).Unix(),
	})
	require.NoError(t, err)
	executor := &channelModelDetectorRelayExecutorStub{}
	acquired := make([]int, 0, 1)
	relay, err := newChannelModelDetectorRelay(store, executor, func(_ context.Context, channelID int) (channelModelDetectorConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
		acquired = append(acquired, channelID)
		return &channelModelDetectorRelayLeaseStub{}, true, ChannelConcurrencyStatus{}, nil
	})
	require.NoError(t, err)
	_, err = relay.Execute(context.Background(), ChannelModelDetectorRelayRequest{
		BearerToken: credential.BearerToken(), DetectorRequestID: "model-filter", Body: []byte(`{"model":"gpt-5.6-sol"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, []int{952}, acquired)
	require.Len(t, executor.executions, 1)
	assert.Equal(t, 952, executor.executions[0].ChannelID)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 952).Update("status", common.ChannelStatusManuallyDisabled).Error)
	_, err = relay.Execute(context.Background(), ChannelModelDetectorRelayRequest{
		BearerToken: credential.BearerToken(), DetectorRequestID: "model-filter-retry", Body: []byte(`{"model":"gpt-5.6-sol"}`),
	})
	assert.ErrorIs(t, err, ErrLogicalChannelSelectionNoAvailableMembers)
	assert.Equal(t, []int{952}, acquired, "an ineligible previous member must not be restored as a single-member fallback")
}

func TestChannelModelDetectorGroupedRelayRetriesBusyPhysicalMembers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "model-detection-busy-members.db")), &gorm.Config{})
	require.NoError(t, err)
	closeChannelModelDetectionLogicalGroupTestDB(t, db)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelModelDetectionExecution{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 961, Name: "busy-a", Models: "alias", Status: common.ChannelStatusEnabled},
		{Id: 962, Name: "available-b", Models: "alias", Status: common.ChannelStatusEnabled},
	}).Error)
	now := time.Unix(1_800_000_000, 0).UTC()
	store := newTestChannelModelDetectorTokenStore(t, &now)
	executor := &channelModelDetectorRelayExecutorStub{}
	acquired := make([]int, 0, 4)
	secondMemberAvailable := true
	relay, err := newChannelModelDetectorRelay(store, executor, func(_ context.Context, channelID int) (channelModelDetectorConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
		acquired = append(acquired, channelID)
		if channelID == 961 || !secondMemberAvailable {
			return nil, false, ChannelConcurrencyStatus{Active: 1, Limit: 1}, nil
		}
		return &channelModelDetectorRelayLeaseStub{}, true, ChannelConcurrencyStatus{}, nil
	})
	require.NoError(t, err)

	issueCredential := func(targetID int64, executionID int64) ChannelModelDetectorCredential {
		targetKey := "target-first"
		if targetID != 61 {
			targetKey = "target-second"
		}
		execution := model.ChannelModelDetectionExecution{
			Id: executionID, RunId: "busy-run", TargetKey: targetKey, TargetId: targetID, ChannelId: 961,
			LogicalChannelID: 802, LogicalRevision: 4, RequestModel: "alias",
			ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Preset: model.ChannelModelDetectionPresetLow,
			Status: model.ChannelModelDetectionExecutionStatusPending,
		}
		require.NoError(t, db.Create(&execution).Error)
		credential, issueErr := store.Issue(ChannelModelDetectorTokenSpec{
			RunID: execution.RunId, TargetID: targetID, ExecutionID: executionID, ChannelID: 961,
			LogicalChannelID: execution.LogicalChannelID, LogicalRevision: execution.LogicalRevision,
			LogicalMembers: []model.ChannelModelDetectionMemberSnapshot{{ChannelID: 961, Weight: 1}, {ChannelID: 962, Weight: 0}},
			RequestModel:   execution.RequestModel, ClaimedModel: execution.ClaimedModel, Preset: execution.Preset,
			RelayBaseURL: "http://127.0.0.1:3000/internal/model-detector", MaxHTTPAttempts: 1, ExpiresAt: now.Add(time.Hour).Unix(),
		})
		require.NoError(t, issueErr)
		return credential
	}

	credential := issueCredential(61, 1061)
	_, err = relay.Execute(context.Background(), ChannelModelDetectorRelayRequest{
		BearerToken: credential.BearerToken(), DetectorRequestID: "busy-failover", Body: []byte(`{"model":"gpt-5.6-sol"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, []int{961, 962}, acquired)
	require.Len(t, executor.executions, 1)
	assert.Equal(t, 962, executor.executions[0].ChannelID)
	var stored model.ChannelModelDetectionExecution
	require.NoError(t, db.First(&stored, 1061).Error)
	assert.Equal(t, 962, stored.ChannelId)

	secondMemberAvailable = false
	acquired = acquired[:0]
	credential = issueCredential(62, 1062)
	_, err = relay.Execute(context.Background(), ChannelModelDetectorRelayRequest{
		BearerToken: credential.BearerToken(), DetectorRequestID: "all-busy", Body: []byte(`{"model":"gpt-5.6-sol"}`),
	})
	assert.ErrorIs(t, err, ErrChannelModelDetectorRelayBusy)
	assert.Equal(t, []int{961, 962}, acquired)
	assert.NotContains(t, acquired, 802, "logical group IDs must never reach physical concurrency acquisition")
	require.Len(t, executor.executions, 1, "all-busy selection must not reach the fixed executor")
	stored = model.ChannelModelDetectionExecution{}
	require.NoError(t, db.First(&stored, 1062).Error)
	assert.Equal(t, 961, stored.ChannelId, "busy candidates are not persisted as actual execution channels")
}

func TestChannelModelDetectionOverviewAndHistoryFallBackToPhysicalChannels(t *testing.T) {
	tests := []struct {
		name        string
		groupStatus int
		disableAll  bool
	}{
		{name: "global switch disabled", groupStatus: model.ChannelLogicalGroupStatusEnabled, disableAll: true},
		{name: "logical group disabled", groupStatus: model.ChannelLogicalGroupStatusDisabled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.disableAll {
				t.Setenv(model.ChannelLogicalGroupGlobalEnableEnv, "false")
			}
			db := setupChannelModelDetectionQueryTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.ChannelLogicalGroup{}, &model.ChannelLogicalGroupMember{}))
			group := model.ChannelLogicalGroup{Name: "fallback", Status: test.groupStatus, Revision: 3}
			require.NoError(t, db.Create(&group).Error)
			channels := []model.Channel{
				{Id: 971, Name: "owner", LogicalChannelID: &group.Id, Models: "alias", Status: common.ChannelStatusEnabled},
				{Id: 972, Name: "member", LogicalChannelID: &group.Id, Models: "alias", Status: common.ChannelStatusEnabled},
			}
			require.NoError(t, db.Create(&channels).Error)
			config := model.ChannelModelDetectionConfig{ChannelId: 971, ScheduleEnabled: true, Revision: 1}
			require.NoError(t, db.Create(&config).Error)
			target := model.ChannelModelDetectionTarget{ConfigId: config.Id, ChannelId: 971, TargetKey: "owner-target", RequestModel: "alias", ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Enabled: true}
			require.NoError(t, db.Create(&target).Error)
			run := model.ChannelModelDetectionRun{
				RunId: "fallback-shared-run", ChannelId: 971, LogicalChannelID: group.Id, LogicalRevision: group.Revision,
				Trigger: model.ChannelModelDetectionTriggerScheduled, Preset: model.ChannelModelDetectionPresetLow,
				Status: model.ChannelModelDetectionRunStatusCompleted, TargetCount: 1, CompletedTargetCount: 1,
				CreatedAt: 100, UpdatedAt: 101, FinishedAt: 101,
			}
			require.NoError(t, run.SetLogicalMemberSnapshot([]model.ChannelModelDetectionMemberSnapshot{{ChannelID: 971, Weight: 1}, {ChannelID: 972, Weight: 1}}))
			require.NoError(t, db.Create(&run).Error)
			require.NoError(t, db.Create(&model.ChannelModelDetectionExecution{
				RunId: run.RunId, TargetKey: target.TargetKey, TargetId: target.Id, ChannelId: 971,
				LogicalChannelID: group.Id, LogicalRevision: group.Revision, RequestModel: target.RequestModel,
				ClaimedModel: target.ClaimedModel, Preset: run.Preset, Status: model.ChannelModelDetectionExecutionStatusCompleted,
				OutcomeCode: "success", CreatedAt: 100, UpdatedAt: 101, FinishedAt: 101,
			}).Error)

			overview, err := GetChannelModelDetectionOverview(context.Background(), db, 1_000)
			require.NoError(t, err)
			require.Len(t, overview.Channels, 2)
			require.Len(t, overview.Channels[0].Targets, 1)
			assert.Empty(t, overview.Channels[1].Targets, "physical member must not inherit the owner's model-detection view")
			assert.Nil(t, overview.Channels[1].Config)

			history, err := ListChannelModelDetectionRuns(context.Background(), db, 972, ChannelModelDetectionRunHistoryQuery{Page: 1, PageSize: 20})
			require.NoError(t, err)
			assert.Zero(t, history.Total, "physical member must not inherit grouped history while grouping is disabled")
		})
	}
}

func TestChannelModelDetectionGroupedPublicViewsProjectRequestedPhysicalChannel(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelLogicalGroup{}, &model.ChannelLogicalGroupMember{},
		&model.ChannelModelDetectionLogicalConfig{}, &model.ChannelModelDetectionLogicalTarget{},
	))
	logicalID := int64(820)
	group := model.ChannelLogicalGroup{Id: logicalID, Name: "public-view", Status: model.ChannelLogicalGroupStatusEnabled, Revision: 6}
	require.NoError(t, db.Create(&group).Error)
	channels := []model.Channel{
		{Id: 981, Name: "key-a", LogicalChannelID: &logicalID, Models: "alpha", Status: common.ChannelStatusEnabled},
		{Id: 982, Name: "key-b", LogicalChannelID: &logicalID, Models: "beta", Status: common.ChannelStatusEnabled},
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&[]model.ChannelLogicalGroupMember{
		{LogicalGroupID: logicalID, ChannelID: 981, Weight: 1, AddressFingerprint: strings.Repeat("a", 64)},
		{LogicalGroupID: logicalID, ChannelID: 982, Weight: 1, AddressFingerprint: strings.Repeat("a", 64)},
	}).Error)
	physicalConfig := model.ChannelModelDetectionConfig{ChannelId: 981, Revision: 9}
	require.NoError(t, db.Create(&physicalConfig).Error)
	physicalTarget := model.ChannelModelDetectionTarget{
		ConfigId: physicalConfig.Id, ChannelId: 981, TargetKey: "physical-colliding-target",
		RequestModel: "legacy", ClaimedModel: model.ChannelModelDetectionClaimedModelTerra, Enabled: true,
	}
	require.NoError(t, db.Create(&physicalTarget).Error)
	config := model.ChannelModelDetectionLogicalConfig{LogicalChannelId: logicalID, Revision: 2, CreatedAt: 10, UpdatedAt: 10}
	require.NoError(t, db.Create(&config).Error)
	target := model.ChannelModelDetectionLogicalTarget{
		ConfigId: config.Id, LogicalChannelId: logicalID, TargetKey: "logical-public-target",
		RequestModel: "alpha", ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Enabled: true,
	}
	require.NoError(t, db.Create(&target).Error)
	assert.Equal(t, physicalTarget.Id, target.Id, "physical and logical target IDs intentionally collide in this regression")
	run := model.ChannelModelDetectionRun{
		RunId: "logical-public-run", ChannelId: 981, LogicalChannelID: logicalID, LogicalRevision: group.Revision,
		ConfigRevision: config.Revision, Trigger: model.ChannelModelDetectionTriggerManual,
		Preset: model.ChannelModelDetectionPresetLow, PresetSource: model.ChannelModelDetectionPresetSourceManualSelected,
		Status: model.ChannelModelDetectionRunStatusCompleted, TargetCount: 1, CompletedTargetCount: 1,
		QueuedAt: 20, FinishedAt: 21, UpdatedAt: 21, CreatedAt: 20,
	}
	require.NoError(t, run.SetLogicalMemberSnapshot([]model.ChannelModelDetectionMemberSnapshot{{ChannelID: 981, Weight: 1}, {ChannelID: 982, Weight: 1}}))
	require.NoError(t, db.Create(&run).Error)
	execution := model.ChannelModelDetectionExecution{
		RunId: run.RunId, TargetKey: target.TargetKey, TargetId: target.Id, LogicalTargetId: &target.Id, ChannelId: 982,
		LogicalChannelID: logicalID, LogicalRevision: group.Revision, RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel,
		Preset: run.Preset, Status: model.ChannelModelDetectionExecutionStatusCompleted, OutcomeCode: "success", CreatedAt: 20, UpdatedAt: 21, FinishedAt: 21,
	}
	require.NoError(t, db.Create(&execution).Error)
	memberAQuota, memberACost := int64(1), int64(100)
	memberBQuota, memberBCost := int64(2), int64(250)
	require.NoError(t, db.Create(&[]model.ChannelModelDetectionCostEvent{
		{CostEventId: "public-cost-a", RunId: run.RunId, TargetId: target.Id, ExecutionId: execution.Id, ChannelId: 981, RequestModel: "alpha", ClaimedModel: target.ClaimedModel, Preset: run.Preset, DetectorRequestId: "a", AttemptNo: 1, DispatchState: model.ChannelModelDetectionDispatchDispatched, SettlementStatus: model.ChannelModelDetectionSettlementSettled, UsageSource: model.ChannelModelDetectionUsageUpstreamAuthoritative, UsageAvailable: true, SettledQuota: &memberAQuota, CostBasisQuota: &memberAQuota, SettledCostNanoCNY: &memberACost, CostScope: model.ChannelModelDetectionCostScopeChannelUpstreamAPI, CreatedAt: 20, SettledAt: 21, UpdatedAt: 21},
		{CostEventId: "public-cost-b", RunId: run.RunId, TargetId: target.Id, ExecutionId: execution.Id, ChannelId: 982, RequestModel: "alpha", ClaimedModel: target.ClaimedModel, Preset: run.Preset, DetectorRequestId: "b", AttemptNo: 2, DispatchState: model.ChannelModelDetectionDispatchDispatched, SettlementStatus: model.ChannelModelDetectionSettlementSettled, UsageSource: model.ChannelModelDetectionUsageUpstreamAuthoritative, UsageAvailable: true, SettledQuota: &memberBQuota, CostBasisQuota: &memberBQuota, SettledCostNanoCNY: &memberBCost, CostScope: model.ChannelModelDetectionCostScopeChannelUpstreamAPI, CreatedAt: 20, SettledAt: 21, UpdatedAt: 21},
	}).Error)

	overview, err := GetChannelModelDetectionOverview(context.Background(), db, 100)
	require.NoError(t, err)
	require.Len(t, overview.Channels, 2)
	for index, channelID := range []int{981, 982} {
		require.NotNil(t, overview.Channels[index].Config)
		assert.Equal(t, channelID, overview.Channels[index].Config.ChannelID)
		assert.Equal(t, []string{"alpha", "beta"}, overview.Channels[index].SupportedModels)
		require.Len(t, overview.Channels[index].Targets, 1)
	}
	history, err := ListChannelModelDetectionRuns(context.Background(), db, 982, ChannelModelDetectionRunHistoryQuery{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, history.Items, 1)
	assert.Equal(t, 982, history.Items[0].ChannelID)
	require.NotNil(t, history.Items[0].Cost.SettledCostCNY)
	assert.Equal(t, "0.000000250", *history.Items[0].Cost.SettledCostCNY)
}
