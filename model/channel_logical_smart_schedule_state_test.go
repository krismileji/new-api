package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupLogicalSmartScheduleStateTest(t *testing.T) (*ChannelLogicalGroup, LogicalChannelIdentity) {
	t.Helper()
	db := setupChannelSmartScheduleRouteTestDB(t)
	t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "true")
	require.NoError(t, db.AutoMigrate(
		&ChannelLogicalGroup{}, &ChannelLogicalGroupMember{},
		&ChannelLogicalSmartScheduleRouteState{}, &ChannelLogicalSmartScheduleSampleState{},
	))
	group := &ChannelLogicalGroup{Name: "共享调度", Revision: 5}
	require.NoError(t, db.Create(group).Error)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 9501, Name: "key-a", Status: common.ChannelStatusEnabled, LogicalChannelID: &group.Id},
		{Id: 9502, Name: "key-b", Status: common.ChannelStatusEnabled, LogicalChannelID: &group.Id},
	}).Error)
	fingerprint := strings.Repeat("c", 64)
	require.NoError(t, db.Create(&[]ChannelLogicalGroupMember{
		{LogicalGroupID: group.Id, ChannelID: 9501, Weight: 1, AddressFingerprint: fingerprint},
		{LogicalGroupID: group.Id, ChannelID: 9502, Weight: 1, AddressFingerprint: fingerprint},
	}).Error)
	return group, LogicalChannelIdentity{ChannelID: 9501, LogicalChannelID: group.Id, Revision: group.Revision}
}

func TestSaveLogicalChannelSmartScheduleModelSampleSharesMembersAndFreezesRevision(t *testing.T) {
	group, identity := setupLogicalSmartScheduleStateTest(t)
	now := common.GetTimestamp()
	firstTokenA := 100.0
	firstTokenB := 300.0
	_, err := SaveLogicalChannelSmartScheduleModelSample(identity, "vip", ChannelSmartScheduleModelSampleResult{
		ChannelId: 9501, Model: "model-a", Source: ChannelSmartScheduleSampleSourceStatusProbe,
		SampleId: "a", Time: now, WindowStart: now - 1, Success: true, FirstTokenMs: &firstTokenA,
	})
	require.NoError(t, err)
	identity.ChannelID = 9502
	view, err := SaveLogicalChannelSmartScheduleModelSample(identity, "vip", ChannelSmartScheduleModelSampleResult{
		ChannelId: 9502, Model: "model-a", Source: ChannelSmartScheduleSampleSourceStatusProbe,
		SampleId: "b", Time: now + 1, WindowStart: now - 1, Success: true, FirstTokenMs: &firstTokenB,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), view.SampleCount)
	require.NotNil(t, view.AverageFirstTokenMs)
	assert.InDelta(t, 200, *view.AverageFirstTokenMs, 1e-9)

	var logicalCount int64
	require.NoError(t, DB.Model(&ChannelLogicalSmartScheduleSampleState{}).Count(&logicalCount).Error)
	assert.Equal(t, int64(1), logicalCount)
	var physicalCount int64
	require.NoError(t, DB.Model(&ChannelSmartScheduleModelSampleState{}).Count(&physicalCount).Error)
	assert.Zero(t, physicalCount, "逻辑调度样本不得写入物理普通样本状态")

	require.NoError(t, DB.Model(&ChannelLogicalGroup{}).Where("id = ?", group.Id).Update("revision", group.Revision+1).Error)
	_, err = SaveLogicalChannelSmartScheduleModelSample(identity, "vip", ChannelSmartScheduleModelSampleResult{
		ChannelId: 9502, Model: "model-a", SampleId: "stale", Time: now + 2, Success: true,
	})
	assert.ErrorIs(t, err, ErrChannelLogicalGroupRevisionConflict)
}

func TestLogicalSmartScheduleRouteStateEncodingRoundTrip(t *testing.T) {
	score := 0.75
	state := ChannelSmartScheduleRouteState{
		LastScheduleStatus: "succeeded", LastScheduleScore: &score,
		LastSchedulePriority: 90, LastScheduleWeight: 70,
		ManualPrimarySaved: true, ManualPrimarySavedPriority: 80, ManualPrimarySavedWeight: 60,
	}
	raw, err := encodeLogicalSmartScheduleRouteState(state)
	require.NoError(t, err)
	decoded, err := decodeLogicalSmartScheduleRouteState(raw)
	require.NoError(t, err)
	require.NotNil(t, decoded.LastScheduleScore)
	assert.InDelta(t, score, *decoded.LastScheduleScore, 1e-9)
	assert.Equal(t, state.LastSchedulePriority, decoded.LastSchedulePriority)
	assert.Equal(t, state.LastScheduleWeight, decoded.LastScheduleWeight)
	assert.Equal(t, state.ManualPrimarySaved, decoded.ManualPrimarySaved)
	assert.Equal(t, state.ManualPrimarySavedPriority, decoded.ManualPrimarySavedPriority)
	assert.Equal(t, state.ManualPrimarySavedWeight, decoded.ManualPrimarySavedWeight)
}

func TestCoalesceChannelSmartScheduleSchedulingRoutesCreatesOneDecisionState(t *testing.T) {
	group, _ := setupLogicalSmartScheduleStateTest(t)
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, DB.Create(&[]Ability{
		{ChannelId: 9501, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 9502, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, DB.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 9501, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 2},
		{ChannelId: 9502, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 4},
	}).Error)
	var abilitiesBefore []Ability
	require.NoError(t, DB.Where(&Ability{Group: "vip", Model: "model-a"}).
		Order("channel_id ASC").Find(&abilitiesBefore).Error)
	var physicalStatesBefore []ChannelSmartScheduleRouteState
	require.NoError(t, DB.Where("group_name = ? AND model_name = ?", "vip", "model-a").
		Order("channel_id ASC").Find(&physicalStatesBefore).Error)
	routes, err := GetChannelSmartScheduleRoutes()
	require.NoError(t, err)
	logicalRoutes, err := CoalesceChannelSmartScheduleSchedulingRoutes(routes)
	require.NoError(t, err)
	require.Len(t, logicalRoutes, 1)
	route := logicalRoutes[0]
	assert.Equal(t, group.Id, route.LogicalChannelId)
	assert.Equal(t, group.Revision, route.LogicalRevision)
	assert.Equal(t, []int{9501, 9502}, route.LogicalMemberIds)

	score := 0.75
	updates := []ChannelSmartScheduleRouteResultUpdate{
		{
			ChannelId: 9501, LogicalChannelId: group.Id, LogicalRevision: group.Revision,
			ExpectedLogicalStateRevision: route.State.Revision, Group: "vip", Model: "model-a",
			Status: ChannelSmartScheduleStatusSucceeded, Score: &score, Priority: 90, Weight: 70,
			ApplyPriorityWeight: true,
		},
		{
			ChannelId: 9502, LogicalChannelId: group.Id, LogicalRevision: group.Revision,
			ExpectedLogicalStateRevision: route.State.Revision, LogicalProjectionOnly: true,
			Group: "vip", Model: "model-a", Status: ChannelSmartScheduleStatusSucceeded,
			Score: &score, Priority: 90, Weight: 70, ApplyPriorityWeight: true,
		},
	}
	outcomes, err := ApplyChannelSmartScheduleRouteResults(updates)
	require.NoError(t, err)
	require.Len(t, outcomes, 2)
	assert.True(t, outcomes[0].Applied)
	assert.True(t, outcomes[1].Applied)

	var stored ChannelLogicalSmartScheduleRouteState
	require.NoError(t, DB.Where(
		"logical_group_id = ? AND logical_revision = ? AND group_name = ? AND model_name = ?",
		group.Id, group.Revision, "vip", "model-a",
	).First(&stored).Error)
	assert.Equal(t, route.State.Revision+1, stored.StateRevision)
	state, err := decodeLogicalSmartScheduleRouteState(stored.StateJSON)
	require.NoError(t, err)
	require.NotNil(t, state.LastScheduleScore)
	assert.InDelta(t, score, *state.LastScheduleScore, 1e-9)
	var abilitiesAfter []Ability
	require.NoError(t, DB.Where(&Ability{Group: "vip", Model: "model-a"}).
		Order("channel_id ASC").Find(&abilitiesAfter).Error)
	assert.Equal(t, abilitiesBefore, abilitiesAfter)
	var physicalStatesAfter []ChannelSmartScheduleRouteState
	require.NoError(t, DB.Where("group_name = ? AND model_name = ?", "vip", "model-a").
		Order("channel_id ASC").Find(&physicalStatesAfter).Error)
	assert.Equal(t, physicalStatesBefore, physicalStatesAfter)

	require.NoError(t, DB.Model(&ChannelLogicalGroup{}).Where("id = ?", group.Id).Update("revision", group.Revision+1).Error)
	_, err = ApplyChannelSmartScheduleRouteResults(updates)
	assert.ErrorIs(t, err, ErrChannelLogicalGroupRevisionConflict)
}

func TestLogicalSmartScheduleRuntimeProtectionAndProbeRecoveryProjectMembers(t *testing.T) {
	group, identity := setupLogicalSmartScheduleStateTest(t)
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, DB.Create(&[]Ability{
		{ChannelId: 9501, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 9502, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, DB.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: 9501, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1,
			BaseRank: 1, BasePriority: priority, BaseWeight: weight,
		},
		{
			ChannelId: 9502, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1,
			BaseRank: 1, BasePriority: priority, BaseWeight: weight,
		},
	}).Error)
	routes, err := GetChannelSmartScheduleRoutes()
	require.NoError(t, err)
	_, err = CoalesceChannelSmartScheduleSchedulingRoutes(routes)
	require.NoError(t, err)
	var abilitiesBefore []Ability
	require.NoError(t, DB.Where(&Ability{Group: "vip", Model: "model-a"}).
		Order("channel_id ASC").Find(&abilitiesBefore).Error)
	var physicalStatesBefore []ChannelSmartScheduleRouteState
	require.NoError(t, DB.Where("group_name = ? AND model_name = ?", "vip", "model-a").
		Order("channel_id ASC").Find(&physicalStatesBefore).Error)
	controlRevision, err := GetChannelSmartScheduleControlRevision()
	require.NoError(t, err)
	now := common.GetTimestamp()
	protected, err := ProtectChannelSmartScheduleRouteOnShortTermFailure(
		9501, "vip", "model-a", now+60, "逻辑组运行时失败", controlRevision,
	)
	require.NoError(t, err)
	assert.True(t, protected.Handled)

	var abilities []Ability
	require.NoError(t, DB.Where(&Ability{Group: "vip", Model: "model-a"}).Order("channel_id ASC").Find(&abilities).Error)
	assert.Equal(t, abilitiesBefore, abilities)
	var physicalStates []ChannelSmartScheduleRouteState
	require.NoError(t, DB.Where("group_name = ? AND model_name = ?", "vip", "model-a").
		Order("channel_id ASC").Find(&physicalStates).Error)
	assert.Equal(t, physicalStatesBefore, physicalStates)
	var logicalState ChannelLogicalSmartScheduleRouteState
	require.NoError(t, DB.Where(&ChannelLogicalSmartScheduleRouteState{
		LogicalGroupID: group.Id, LogicalRevision: group.Revision, GroupName: "vip", ModelName: "model-a",
	}).First(&logicalState).Error)
	decoded, err := decodeLogicalSmartScheduleRouteState(logicalState.StateJSON)
	require.NoError(t, err)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, decoded.StabilityState)

	recovery := &ChannelSmartScheduleProbeRecoveryRequest{
		ExpectedControlRevision: controlRevision,
		Routes: []ChannelSmartScheduleProbeRecoveryRoute{{
			Group: "vip", Model: "model-a", RecoverySuccessThreshold: 1, CooldownUntil: now + 120,
		}},
	}
	_, err = SaveLogicalChannelSmartScheduleModelSample(identity, "vip", ChannelSmartScheduleModelSampleResult{
		ChannelId: 9501, Model: "model-a", Source: ChannelSmartScheduleSampleSourceStatusProbe,
		SampleId: "recover", Time: now + 1, WindowStart: now, Success: true, ProbeRecovery: recovery,
	})
	require.NoError(t, err)
	assert.True(t, recovery.Result.Applied)
	require.Len(t, recovery.Result.Recovered, 1)
	require.NoError(t, DB.Where(&Ability{Group: "vip", Model: "model-a"}).Order("channel_id ASC").Find(&abilities).Error)
	assert.Equal(t, abilitiesBefore, abilities)
	require.NoError(t, DB.Where("group_name = ? AND model_name = ?", "vip", "model-a").
		Order("channel_id ASC").Find(&physicalStates).Error)
	assert.Equal(t, physicalStatesBefore, physicalStates)
	require.NoError(t, DB.Where(&ChannelLogicalSmartScheduleRouteState{
		LogicalGroupID: group.Id, LogicalRevision: group.Revision, GroupName: "vip", ModelName: "model-a",
	}).First(&logicalState).Error)
	decoded, err = decodeLogicalSmartScheduleRouteState(logicalState.StateJSON)
	require.NoError(t, err)
	assert.Empty(t, decoded.StabilityState)
}
