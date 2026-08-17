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
