package controller

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSmartScheduleRouteResponsesExposeActiveRateLimitCooldown(t *testing.T) {
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	now := common.GetTimestamp()
	service.StartChannelRateLimitCooldown(11, "model-a", 60)

	responses := channelSmartScheduleRouteResponses([]model.ChannelSmartScheduleRoute{
		{ChannelId: 11, Group: "vip", Model: "model-*"},
	})

	require.Len(t, responses, 1)
	assert.Greater(t, responses[0].RateLimitCooldownUntil, now)
}

func TestChannelSmartScheduleApplyCurrentWindowScoresKeepsHistoryAndCandidateBoundaries(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	historicalScore := 0.42
	fastFirstToken := 100.0
	slowFirstToken := 500.0
	insufficientFirstToken := 300.0
	now := common.GetTimestamp()
	routes := []model.ChannelSmartScheduleRoute{
		{
			ChannelId: 1, ChannelStatus: common.ChannelStatusEnabled, Group: "vip", Model: "model-a",
			Enabled: true, Priority: 80, Weight: 50,
			State: model.ChannelSmartScheduleRouteState{
				ParticipationSet: true, LastScheduleScore: &historicalScore, ManualPrimaryUntil: now + 60,
				AdaptiveHealthState:                           channelSmartScheduleHealthPressure,
				AdaptiveHealthPressure:                        0.5,
				AdaptiveHealthFirstTokenWarningRequestPercent: 12.5,
				AdaptiveHealthSampleCount:                     8,
				AdaptiveHealthLastSampleAt:                    now - 1,
			},
		},
		{
			ChannelId: 2, ChannelStatus: common.ChannelStatusEnabled, Group: "vip", Model: "model-a",
			Enabled: true, Priority: 80, Weight: 50,
			State: model.ChannelSmartScheduleRouteState{ParticipationSet: true},
		},
		{
			ChannelId: 3, ChannelStatus: common.ChannelStatusEnabled, Group: "vip", Model: "model-a",
			Enabled: true, Priority: 80, Weight: 50,
			EconomicRole: channelSmartScheduleEconomicRoleBreakEvenFallback,
			State:        model.ChannelSmartScheduleRouteState{ParticipationSet: true},
		},
		{
			ChannelId: 4, ChannelStatus: common.ChannelStatusManuallyDisabled, Group: "vip", Model: "model-a",
			Enabled: true, Priority: 80, Weight: 50,
			State: model.ChannelSmartScheduleRouteState{ParticipationSet: true},
		},
		{
			ChannelId: 5, ChannelStatus: common.ChannelStatusEnabled, Group: "vip", Model: "model-a",
			Enabled: true, Priority: 80, Weight: 50,
			State: model.ChannelSmartScheduleRouteState{ParticipationSet: true, Excluded: true},
		},
		{
			ChannelId: 6, ChannelStatus: common.ChannelStatusEnabled, Group: "vip", Model: "model-a",
			Enabled: true, Priority: 80, Weight: 50,
			State: model.ChannelSmartScheduleRouteState{
				ParticipationSet: true, RuntimeProtectionUntil: now + 60,
			},
		},
		{
			ChannelId: 7, ChannelStatus: common.ChannelStatusEnabled, Group: "vip", Model: "model-a",
			Enabled: true, Priority: 80, Weight: 50,
			State: model.ChannelSmartScheduleRouteState{
				ParticipationSet: true, StabilityState: model.ChannelSmartScheduleStabilityProbing,
			},
		},
	}
	responses := channelSmartScheduleRouteResponses(routes)
	policyByGroup := map[string]channelSmartSchedulePolicy{
		"vip": {
			Strategy:   channelMonitorSmartScheduleStrategyFirstToken,
			ApplyMode:  channelMonitorSmartScheduleApplyPriorityWeight,
			MinSamples: 5,
			Scoring:    defaultChannelSmartScheduleScoring(),
		},
	}
	for channelId, firstTokenMs := range map[int]float64{
		1: fastFirstToken, 2: slowFirstToken, 3: insufficientFirstToken,
		4: fastFirstToken, 5: fastFirstToken,
	} {
		sampleCount := 5
		if channelId == 3 {
			sampleCount = 1
		}
		for index := range sampleCount {
			require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
				channelId, "vip", "model-a", now-int64(sampleCount-index), true,
				&firstTokenMs, nil, nil, false,
			))
		}
	}
	channelSmartScheduleApplyCurrentWindowScores(
		context.Background(),
		responses,
		routes,
		policyByGroup,
		now,
	)

	byChannel := make(map[int]channelSmartScheduleRouteResponse, len(responses))
	for _, response := range responses {
		byChannel[response.ChannelId] = response
	}
	fast := byChannel[1]
	require.NotNil(t, fast.CurrentWindowScore)
	assert.InDelta(t, 1, *fast.CurrentWindowScore, 1e-9)
	require.NotNil(t, fast.CurrentWindowScoreDetails)
	assert.Equal(t, 1, fast.CurrentWindowScoreDetails.Decision.ManualPrimaryChannelId)
	assert.Equal(t, channelSmartScheduleHealthUnknown, fast.CurrentWindowScoreDetails.Health.State)
	assert.False(t, fast.CurrentWindowScoreDetails.Health.Evidence)
	assert.Zero(t, fast.CurrentWindowScoreDetails.Health.FirstTokenWarningRequestPercent)
	require.NotNil(t, fast.State.LastScheduleScore)
	assert.InDelta(t, historicalScore, *fast.State.LastScheduleScore, 1e-9)
	assert.InDelta(t, historicalScore, *routes[0].State.LastScheduleScore, 1e-9)

	slow := byChannel[2]
	require.NotNil(t, slow.CurrentWindowScore)
	assert.InDelta(t, 0, *slow.CurrentWindowScore, 1e-9)

	insufficient := byChannel[3]
	assert.Nil(t, insufficient.CurrentWindowScore)
	require.NotNil(t, insufficient.CurrentWindowScoreDetails)
	require.NotNil(t, insufficient.CurrentWindowScoreDetails.Economics)
	assert.Equal(
		t,
		channelSmartScheduleEconomicRoleBreakEvenFallback,
		insufficient.CurrentWindowScoreDetails.Economics.EconomicRole,
	)
	assert.Contains(t, insufficient.CurrentWindowScoreDetails.Decision.AdjustmentReason, "首字样本不足")

	assert.Nil(t, byChannel[4].CurrentWindowScore)
	assert.Nil(t, byChannel[4].CurrentWindowScoreDetails)
	assert.Nil(t, byChannel[5].CurrentWindowScore)
	assert.Nil(t, byChannel[5].CurrentWindowScoreDetails)
	assert.Nil(t, byChannel[6].CurrentWindowScore)
	assert.Nil(t, byChannel[6].CurrentWindowScoreDetails)
	assert.Nil(t, byChannel[7].CurrentWindowScore)
	assert.Nil(t, byChannel[7].CurrentWindowScoreDetails)
}

func TestChannelSmartScheduleApplyCurrentWindowScoresUsesSwitchConfirmation(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	now := common.GetTimestamp()
	currentRatio := 2.0
	challengerRatio := 1.0
	groupRatio := 3.0
	currentMargin := groupRatio - currentRatio
	challengerMargin := groupRatio - challengerRatio
	routes := []model.ChannelSmartScheduleRoute{
		{
			ChannelId: 21, ChannelStatus: common.ChannelStatusEnabled, Group: "vip", Model: "model-a",
			Enabled: true, Priority: 2, Weight: 1000,
			CostRatio: &currentRatio, GroupRatio: &groupRatio, GrossMargin: &currentMargin,
			State: model.ChannelSmartScheduleRouteState{
				ParticipationSet: true, BaseRank: 1, BasePriority: 2, BaseWeight: 1000,
			},
		},
		{
			ChannelId: 22, ChannelStatus: common.ChannelStatusEnabled, Group: "vip", Model: "model-a",
			Enabled: true, Priority: 1, Weight: 1000,
			CostRatio: &challengerRatio, GroupRatio: &groupRatio, GrossMargin: &challengerMargin,
			State: model.ChannelSmartScheduleRouteState{
				ParticipationSet: true, BaseRank: 2, BasePriority: 1, BaseWeight: 1000,
			},
		},
	}
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 1, 80, 30,
	).policy()
	responses := channelSmartScheduleRouteResponses(routes)
	firstTokenMs := 100.0
	for _, channelId := range []int{21, 22} {
		require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
			channelId, "vip", "model-a", now, true, &firstTokenMs, nil, nil, false,
		))
	}

	require.NoError(t, channelSmartScheduleApplyCurrentWindowScores(
		context.Background(), responses, routes, map[string]channelSmartSchedulePolicy{"vip": policy}, now,
	))

	byChannel := make(map[int]channelSmartScheduleRouteResponse, len(responses))
	for _, response := range responses {
		byChannel[response.ChannelId] = response
	}
	details := byChannel[22].CurrentWindowScoreDetails
	require.NotNil(t, details)
	assert.Equal(t, 22, details.Decision.RawWinnerChannelId)
	assert.Equal(t, 21, details.Decision.CurrentPrimaryChannelId)
	assert.Equal(t, 21, details.Decision.SelectedPrimaryChannelId)
	assert.Equal(t, 21, details.Decision.ActualPrimaryChannelId)
	assert.Contains(t, details.Decision.SelectionReason, "仅允许自适应采样")
	assert.True(t, details.Health.Evidence)
	assert.Equal(t, channelSmartScheduleHealthHealthy, details.Health.State)
	currentDetails := byChannel[21].CurrentWindowScoreDetails
	require.NotNil(t, currentDetails)
	assert.Equal(t, 1, currentDetails.Decision.BaseRank)
	assert.Equal(t, 2, byChannel[22].CurrentWindowScoreDetails.Decision.BaseRank)
}

func TestChannelSmartScheduleApplyCurrentWindowScoresUsesMinimumComparableChannels(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	now := common.GetTimestamp()
	ratioOne := 1.0
	ratioTwo := 2.0
	groupRatio := 3.0
	marginOne := groupRatio - ratioOne
	marginTwo := groupRatio - ratioTwo
	routes := []model.ChannelSmartScheduleRoute{
		{
			ChannelId: 23, ChannelStatus: common.ChannelStatusEnabled, Group: "vip", Model: "model-a",
			Enabled: true, Priority: 2, Weight: 1000,
			CostRatio: &ratioOne, GroupRatio: &groupRatio, GrossMargin: &marginOne,
			State: model.ChannelSmartScheduleRouteState{
				ParticipationSet: true, BaseRank: 1, BasePriority: 2, BaseWeight: 1000,
			},
		},
		{
			ChannelId: 24, ChannelStatus: common.ChannelStatusEnabled, Group: "vip", Model: "model-a",
			Enabled: true, Priority: 1, Weight: 1000,
			CostRatio: &ratioTwo, GroupRatio: &groupRatio, GrossMargin: &marginTwo,
			State: model.ChannelSmartScheduleRouteState{
				ParticipationSet: true, BaseRank: 2, BasePriority: 1, BaseWeight: 1000,
			},
		},
	}
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 1, 80, 30,
	).policy()
	policy.AdaptiveSamplingMinComparableChannels = 3
	policy.AdaptiveSamplingEnabled = false
	responses := channelSmartScheduleRouteResponses(routes)

	require.NoError(t, channelSmartScheduleApplyCurrentWindowScores(
		context.Background(), responses, routes, map[string]channelSmartSchedulePolicy{"vip": policy}, now,
	))

	for _, response := range responses {
		assert.Nil(t, response.CurrentWindowScore)
		require.NotNil(t, response.CurrentWindowScoreDetails)
		assert.Equal(t, model.ChannelSmartScheduleComparisonInsufficient, response.CurrentWindowScoreDetails.ComparisonState)
		assert.Equal(t, 3, response.CurrentWindowScoreDetails.MinComparableChannels)
	}
}

func TestChannelSmartScheduleApplyCurrentWindowScoresUsesWinsorizedFirstToken(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	now := common.GetTimestamp()
	routes := []model.ChannelSmartScheduleRoute{
		{
			ChannelId: 25, ChannelStatus: common.ChannelStatusEnabled, Group: "vip", Model: "model-a",
			Enabled: true, Priority: 2, Weight: 1000,
			State: model.ChannelSmartScheduleRouteState{
				ParticipationSet: true, BaseRank: 1, BasePriority: 2, BaseWeight: 1000,
			},
		},
		{
			ChannelId: 26, ChannelStatus: common.ChannelStatusEnabled, Group: "vip", Model: "model-a",
			Enabled: true, Priority: 1, Weight: 1000,
			State: model.ChannelSmartScheduleRouteState{
				ParticipationSet: true, BaseRank: 2, BasePriority: 1, BaseWeight: 1000,
			},
		},
	}
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 20, 80, 30,
	).policy()
	policy.AdaptiveSamplingEnabled = false
	responses := channelSmartScheduleRouteResponses(routes)
	baselineFirstTokenMs := 300.0
	for index := range 20 {
		require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
			25, "vip", "model-a", now-int64(index), true, &baselineFirstTokenMs, nil, nil, false,
		))
	}
	fastFirstTokenMs := 100.0
	for index := range 19 {
		require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
			26, "vip", "model-a", now-int64(index), true, &fastFirstTokenMs, nil, nil, false,
		))
	}
	outlierFirstTokenMs := 10_000.0
	require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
		26, "vip", "model-a", now-19, true, &outlierFirstTokenMs, nil, nil, false,
	))

	require.NoError(t, channelSmartScheduleApplyCurrentWindowScores(
		context.Background(), responses, routes, map[string]channelSmartSchedulePolicy{"vip": policy}, now,
	))

	byChannel := make(map[int]channelSmartScheduleRouteResponse, len(responses))
	for _, response := range responses {
		byChannel[response.ChannelId] = response
	}
	details := byChannel[26].CurrentWindowScoreDetails
	require.NotNil(t, details)
	require.NotNil(t, details.Inputs.FirstTokenMs.Value)
	assert.InDelta(t, 101.25, *details.Inputs.FirstTokenMs.Value, 1e-9)
}

func TestChannelSmartScheduleRealtimeRouteMetricViewUsesPolicyStabilityWindow(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	now := common.GetTimestamp()
	require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
		31, "vip", "model-a", now-10*60, true, nil, nil, nil, false,
	))
	route := model.ChannelSmartScheduleRoute{
		ChannelId: 31,
		Group:     "vip",
		Model:     "model-a",
		State:     model.ChannelSmartScheduleRouteState{ParticipationSet: true},
	}
	configured := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 1, 80, 30,
	)
	policy := configured.policy()
	policy.StabilityWindowMinutes = 5

	shortView, err := channelSmartScheduleRealtimeRouteMetricView(
		context.Background(), route, policy, now-60*60, now,
	)
	require.NoError(t, err)
	assert.Nil(t, shortView.stability)

	policy.StabilityWindowMinutes = 15
	longView, err := channelSmartScheduleRealtimeRouteMetricView(
		context.Background(), route, policy, now-60*60, now,
	)
	require.NoError(t, err)
	require.NotNil(t, longView.stability)
	assert.Equal(t, int64(1), longView.stability.SuccessCount)
}
