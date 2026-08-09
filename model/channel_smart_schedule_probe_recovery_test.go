package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSmartScheduleProbeRecoveryPersistsAcrossThresholdsAndRenewsOnFailure(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	revision := "probe-recovery-revision"
	require.NoError(t, db.Create(&Option{
		Key: ChannelSmartScheduleControlRevisionOption, Value: revision,
	}).Error)
	channelPriority := int64(80)
	channelWeight := uint(100)
	channel := Channel{
		Id: 2201, Name: "shared probe", Status: common.ChannelStatusEnabled,
		Priority: &channelPriority, Weight: &channelWeight,
	}
	require.NoError(t, db.Create(&channel).Error)
	degradedPriorityA := int64(0)
	degradedPriorityB := int64(0)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: channel.Id, Group: "group-a", Model: "model-a", Enabled: true, Priority: &degradedPriorityA, Weight: 0},
		{ChannelId: channel.Id, Group: "group-b", Model: "model-a", Enabled: true, Priority: &degradedPriorityB, Weight: 0},
	}).Error)
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: channel.Id, GroupName: "group-a", ModelName: "model-a",
			ParticipationSet: true, Revision: 1,
			BaseRank: 1, BasePriority: 90, BaseWeight: 70,
			StabilityState: ChannelSmartScheduleStabilityDegraded,
			StabilityUntil: now + 300, RuntimeProtectionUntil: now + 300,
			StabilitySavedPriority: 90, StabilitySavedWeight: 70,
		},
		{
			ChannelId: channel.Id, GroupName: "group-b", ModelName: "model-a",
			ParticipationSet: true, Revision: 1,
			BaseRank: 2, BasePriority: 80, BaseWeight: 60,
			StabilityState: ChannelSmartScheduleStabilityDegraded,
			StabilityUntil: now + 300, RuntimeProtectionUntil: now + 300,
			StabilitySavedPriority: 80, StabilitySavedWeight: 60,
		},
	}).Error)

	routes := []ChannelSmartScheduleProbeRecoveryRoute{
		{Group: "group-a", Model: "model-a", RecoverySuccessThreshold: 1, CooldownUntil: now + 600},
		{Group: "group-b", Model: "model-a", RecoverySuccessThreshold: 2, CooldownUntil: now + 600},
	}
	staleRecovery := &ChannelSmartScheduleProbeRecoveryRequest{
		ExpectedControlRevision: "stale-revision", Routes: routes,
	}
	staleState, err := SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: channel.Id, Model: "model-a", Source: ChannelSmartScheduleSampleSourceScheduledProbe,
		SampleId: "stale-success", WindowStart: now - 60, Time: now - 3, Success: true,
		ProbeRecovery: staleRecovery,
	})
	require.NoError(t, err)
	assert.False(t, staleRecovery.Result.Applied)
	assert.Zero(t, staleState.RecoverySuccessCount)
	var staleRoutes []ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("channel_id = ?", channel.Id).Order("group_name ASC").Find(&staleRoutes).Error)
	require.Len(t, staleRoutes, 2)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, staleRoutes[0].StabilityState)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, staleRoutes[1].StabilityState)

	firstRecovery := &ChannelSmartScheduleProbeRecoveryRequest{
		ExpectedControlRevision: revision, Routes: routes,
	}
	firstState, err := SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: channel.Id, Model: "model-a", Source: ChannelSmartScheduleSampleSourceScheduledProbe,
		SampleId: "first-success", WindowStart: now - 60, Time: now - 2, Success: true,
		ProbeRecovery: firstRecovery,
	})
	require.NoError(t, err)
	assert.True(t, firstRecovery.Result.Applied)
	assert.Equal(t, 1, firstRecovery.Result.RecoverySuccessCount)
	assert.Equal(t, []ChannelSmartScheduleRouteKey{{
		ChannelId: channel.Id, Group: "group-a", Model: "model-a",
	}}, firstRecovery.Result.Recovered)
	assert.Equal(t, 1, firstState.RecoverySuccessCount)
	assert.Equal(t, now-2, firstState.RecoverySuccessAt)
	assert.Positive(t, firstState.ObservationSince)

	var firstRoutes []ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("channel_id = ?", channel.Id).Order("group_name ASC").Find(&firstRoutes).Error)
	require.Len(t, firstRoutes, 2)
	assert.Empty(t, firstRoutes[0].StabilityState)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, firstRoutes[1].StabilityState)
	var firstAbilities []Ability
	require.NoError(t, db.Where("channel_id = ?", channel.Id).Find(&firstAbilities).Error)
	require.Len(t, firstAbilities, 2)
	firstAbilityByGroup := make(map[string]Ability, len(firstAbilities))
	for _, ability := range firstAbilities {
		firstAbilityByGroup[ability.Group] = ability
	}
	require.NotNil(t, firstAbilityByGroup["group-a"].Priority)
	assert.Equal(t, int64(90), *firstAbilityByGroup["group-a"].Priority)
	assert.Equal(t, uint(70), firstAbilityByGroup["group-a"].Weight)

	// A second request object represents another process reading the persisted
	// shared count rather than relying on process-local recovery state.
	secondRecovery := &ChannelSmartScheduleProbeRecoveryRequest{
		ExpectedControlRevision: revision, Routes: routes,
	}
	secondState, err := SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: channel.Id, Model: "model-a", Source: ChannelSmartScheduleSampleSourceScheduledProbe,
		SampleId: "second-success", WindowStart: now - 60, Time: now - 1, Success: true,
		ProbeRecovery: secondRecovery,
	})
	require.NoError(t, err)
	assert.True(t, secondRecovery.Result.Applied)
	assert.Equal(t, []ChannelSmartScheduleRouteKey{{
		ChannelId: channel.Id, Group: "group-b", Model: "model-a",
	}}, secondRecovery.Result.Recovered)
	assert.Zero(t, secondRecovery.Result.RecoverySuccessCount)
	assert.Zero(t, secondState.RecoverySuccessCount)
	assert.Zero(t, secondState.RecoverySuccessAt)
	assert.GreaterOrEqual(t, secondState.ObservationSince, firstState.ObservationSince)

	var recoveredRoutes []ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("channel_id = ?", channel.Id).Order("group_name ASC").Find(&recoveredRoutes).Error)
	require.Len(t, recoveredRoutes, 2)
	assert.Empty(t, recoveredRoutes[0].StabilityState)
	assert.Empty(t, recoveredRoutes[1].StabilityState)
	var recoveredAbilities []Ability
	require.NoError(t, db.Where("channel_id = ?", channel.Id).Find(&recoveredAbilities).Error)
	require.Len(t, recoveredAbilities, 2)
	recoveredAbilityByGroup := make(map[string]Ability, len(recoveredAbilities))
	for _, ability := range recoveredAbilities {
		recoveredAbilityByGroup[ability.Group] = ability
	}
	require.NotNil(t, recoveredAbilityByGroup["group-b"].Priority)
	assert.Equal(t, int64(80), *recoveredAbilityByGroup["group-b"].Priority)
	assert.Equal(t, uint(60), recoveredAbilityByGroup["group-b"].Weight)

	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).
		Where("channel_id = ? AND group_name = ?", channel.Id, "group-a").
		Updates(map[string]any{
			"stability_state":          ChannelSmartScheduleStabilityDegraded,
			"stability_until":          now + 100,
			"runtime_protection_until": now + 100,
			"stability_saved_priority": int64(90),
			"stability_saved_weight":   uint(70),
		}).Error)
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).
		Where("channel_id = ? AND group_name = ?", channel.Id, "group-b").
		Updates(map[string]any{
			"stability_state":                     ChannelSmartScheduleStabilityProbing,
			"stability_until":                     0,
			"runtime_protection_until":            0,
			"stability_saved_priority":            int64(80),
			"stability_saved_weight":              uint(60),
			"stability_release_max_prompt_tokens": 4096,
		}).Error)
	require.NoError(t, db.Model(&Ability{}).
		Where(&Ability{ChannelId: channel.Id, Group: "group-a", Model: "model-a"}).
		Updates(map[string]any{"priority": int64(0), "weight": uint(0)}).Error)
	require.NoError(t, db.Model(&Ability{}).
		Where(&Ability{ChannelId: channel.Id, Group: "group-b", Model: "model-a"}).
		Updates(map[string]any{"priority": int64(0), "weight": uint(10)}).Error)
	require.NoError(t, db.Model(&ChannelSmartScheduleModelSampleState{}).
		Where("channel_id = ? AND model_name = ?", channel.Id, "model-a").
		Updates(map[string]any{"recovery_success_count": 1, "recovery_success_at": now}).Error)

	failureRecovery := &ChannelSmartScheduleProbeRecoveryRequest{
		ExpectedControlRevision: revision,
		FailureReason:           "probe failed",
		Routes:                  routes,
	}
	failureState, err := SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: channel.Id, Model: "model-a", Source: ChannelSmartScheduleSampleSourceScheduledProbe,
		SampleId: "probe-failure", WindowStart: now - 60, Time: now, Success: false,
		Error: "probe failed", ProbeRecovery: failureRecovery,
	})
	require.NoError(t, err)
	assert.True(t, failureRecovery.Result.Applied)
	assert.Zero(t, failureRecovery.Result.RecoverySuccessCount)
	assert.Len(t, failureRecovery.Result.Renewed, 2)
	assert.Zero(t, failureState.RecoverySuccessCount)
	assert.Zero(t, failureState.RecoverySuccessAt)

	var renewedRoutes []ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("channel_id = ?", channel.Id).Order("group_name ASC").Find(&renewedRoutes).Error)
	require.Len(t, renewedRoutes, 2)
	for _, route := range renewedRoutes {
		assert.Equal(t, ChannelSmartScheduleStabilityDegraded, route.StabilityState)
		assert.Equal(t, now+600, route.StabilityUntil)
		assert.Equal(t, now+600, route.RuntimeProtectionUntil)
		assert.Zero(t, route.StabilityReleaseMaxPromptTokens)
	}
	var renewedAbilities []Ability
	require.NoError(t, db.Where("channel_id = ?", channel.Id).Find(&renewedAbilities).Error)
	require.Len(t, renewedAbilities, 2)
	for _, ability := range renewedAbilities {
		require.NotNil(t, ability.Priority)
		assert.Zero(t, *ability.Priority)
		assert.Zero(t, ability.Weight)
	}
}
