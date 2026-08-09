package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdvanceExpiredChannelSmartScheduleDegradedRoutesIsGuardedAndRestoresPoolOverlay(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	const (
		now      = int64(1_700_000_000)
		revision = "release-revision"
	)
	revisionOption := Option{Key: ChannelSmartScheduleControlRevisionOption, Value: revision}
	require.NoError(t, db.Create(&revisionOption).Error)
	channelPriority := int64(80)
	channelWeight := uint(1000)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 2101, Name: "primary", Status: common.ChannelStatusEnabled, Priority: &channelPriority, Weight: &channelWeight},
		{Id: 2102, Name: "sampled", Status: common.ChannelStatusEnabled, Priority: &channelPriority, Weight: &channelWeight},
		{Id: 2103, Name: "degraded", Status: common.ChannelStatusEnabled, Priority: &channelPriority, Weight: &channelWeight},
	}).Error)
	primaryAppliedPriority := int64(100)
	sampledAppliedPriority := int64(100)
	degradedAppliedPriority := int64(0)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 2101, Group: "vip", Model: "model-a", Enabled: true, Priority: &primaryAppliedPriority, Weight: 9700},
		{ChannelId: 2102, Group: "vip", Model: "model-a", Enabled: true, Priority: &sampledAppliedPriority, Weight: 300},
		{ChannelId: 2103, Group: "vip", Model: "model-a", Enabled: true, Priority: &degradedAppliedPriority, Weight: 0},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: 2101, GroupName: "vip", ModelName: "model-a",
			ParticipationSet: true, Revision: 3,
			BaseRank: 1, BasePriority: 100, BaseWeight: 1000,
		},
		{
			ChannelId: 2102, GroupName: "vip", ModelName: "model-a",
			ParticipationSet: true, Revision: 4,
			BaseRank: 2, BasePriority: 90, BaseWeight: 1000,
			TemporaryTrafficKind:  ChannelSmartScheduleTemporaryTrafficExploration,
			TemporaryTrafficSince: now - 30, TemporaryTrafficTargetPercent: 3,
			ExplorationMaxPromptTokens: 16384, SamplingCandidate: true,
		},
		{
			ChannelId: 2103, GroupName: "vip", ModelName: "model-a",
			ParticipationSet: true, Revision: 5,
			BaseRank: 3, BasePriority: 80, BaseWeight: 1000,
			StabilityState: ChannelSmartScheduleStabilityDegraded,
			StabilityUntil: now, RuntimeProtectionUntil: now,
			StabilitySavedPriority: 80, StabilitySavedWeight: 1000,
		},
	}).Error)

	expired, err := GetExpiredChannelSmartScheduleDegradedRoutes(now)
	require.NoError(t, err)
	require.Equal(t, []ChannelSmartScheduleRouteKey{{
		ChannelId: 2103, Group: "vip", Model: "model-a",
	}}, expired)

	pool := []ChannelSmartScheduleStabilityReleasePool{{
		Group: "vip", Model: "model-a", StabilityReleaseMaxPromptTokens: 4096,
	}}
	staleResult, err := AdvanceExpiredChannelSmartScheduleDegradedRoutes(now, "stale-revision", pool)
	require.NoError(t, err)
	assert.False(t, staleResult.Applied)
	assert.Empty(t, staleResult.Released)

	result, err := AdvanceExpiredChannelSmartScheduleDegradedRoutes(now, revision, pool)
	require.NoError(t, err)
	assert.True(t, result.Applied)
	assert.True(t, result.RoutingChanged)
	require.Equal(t, []ChannelSmartScheduleRouteKey{{
		ChannelId: 2103, Group: "vip", Model: "model-a",
	}}, result.Released)

	var states []ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("group_name = ? AND model_name = ?", "vip", "model-a").
		Order("channel_id ASC").Find(&states).Error)
	require.Len(t, states, 3)
	assert.Empty(t, states[1].TemporaryTrafficKind)
	assert.Zero(t, states[1].TemporaryTrafficSince)
	assert.Zero(t, states[1].TemporaryTrafficTargetPercent)
	assert.Zero(t, states[1].ExplorationMaxPromptTokens)
	assert.False(t, states[1].SamplingCandidate)
	assert.Equal(t, ChannelSmartScheduleStabilityProbing, states[2].StabilityState)
	assert.Zero(t, states[2].StabilityUntil)
	assert.Equal(t, now, states[2].StabilitySince)
	assert.Zero(t, states[2].RuntimeProtectionUntil)
	assert.Equal(t, int64(80), states[2].StabilitySavedPriority)
	assert.Equal(t, uint(1000), states[2].StabilitySavedWeight)
	assert.Equal(t, 3, states[2].BaseRank)
	assert.Equal(t, int64(80), states[2].BasePriority)
	assert.Equal(t, uint(1000), states[2].BaseWeight)
	assert.Equal(t, 4096, states[2].StabilityReleaseMaxPromptTokens)

	var abilities []Ability
	require.NoError(t, db.Where(&Ability{Group: "vip", Model: "model-a"}).
		Order("channel_id ASC").Find(&abilities).Error)
	require.Len(t, abilities, 3)
	require.NotNil(t, abilities[0].Priority)
	require.NotNil(t, abilities[1].Priority)
	require.NotNil(t, abilities[2].Priority)
	assert.Equal(t, int64(100), *abilities[0].Priority)
	assert.Equal(t, uint(1000), abilities[0].Weight)
	assert.Equal(t, int64(90), *abilities[1].Priority)
	assert.Equal(t, uint(1000), abilities[1].Weight)
	assert.Equal(t, int64(0), *abilities[2].Priority)
	assert.Equal(t, uint(10), abilities[2].Weight)

	secondResult, err := AdvanceExpiredChannelSmartScheduleDegradedRoutes(now+1, revision, pool)
	require.NoError(t, err)
	assert.True(t, secondResult.Applied)
	assert.Empty(t, secondResult.Released)
	assert.False(t, secondResult.RoutingChanged)
}
