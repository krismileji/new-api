package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSmartScheduleApplyCurrentWindowScoresKeepsHistoryAndCandidateBoundaries(t *testing.T) {
	historicalScore := 0.42
	fastFirstToken := 100.0
	slowFirstToken := 500.0
	insufficientFirstToken := 300.0
	now := int64(1_000)
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
	performanceItems := []model.ChannelMonitorRoutePerformanceMetric{
		{ChannelId: 1, GroupName: "vip", ModelName: "model-a", FirstTokenSampleCount: 5, AverageFirstTokenMs: &fastFirstToken},
		{ChannelId: 2, GroupName: "vip", ModelName: "model-a", FirstTokenSampleCount: 5, AverageFirstTokenMs: &slowFirstToken},
		{ChannelId: 3, GroupName: "vip", ModelName: "model-a", FirstTokenSampleCount: 1, AverageFirstTokenMs: &insufficientFirstToken},
		{ChannelId: 4, GroupName: "vip", ModelName: "model-a", FirstTokenSampleCount: 5, AverageFirstTokenMs: &fastFirstToken},
		{ChannelId: 5, GroupName: "vip", ModelName: "model-a", FirstTokenSampleCount: 5, AverageFirstTokenMs: &fastFirstToken},
	}
	sampleCache := &channelSmartScheduleSampleMetricCache{
		seriesByModel:   map[channelSmartScheduleModelKey]model.ChannelSmartScheduleSampleSeries{},
		metricsByWindow: map[channelSmartScheduleSampleMetricWindowKey]model.ChannelSmartScheduleSampleMetrics{},
	}

	channelSmartScheduleApplyCurrentWindowScores(
		responses,
		routes,
		policyByGroup,
		performanceItems,
		map[channelSmartScheduleRouteKey]model.ChannelMonitorRouteStabilityMetric{},
		sampleCache,
		0,
		now,
		false,
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
	assert.Equal(t, channelSmartScheduleHealthPressure, fast.CurrentWindowScoreDetails.Health.State)
	assert.True(t, fast.CurrentWindowScoreDetails.Health.Evidence)
	assert.InDelta(t, 12.5, fast.CurrentWindowScoreDetails.Health.FirstTokenWarningRequestPercent, 1e-9)
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
