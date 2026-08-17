package controller

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanChannelSmartScheduleCapturesExactScoreCalculation(t *testing.T) {
	ratioLow := 1.0
	ratioHigh := 2.0
	firstTokenSlow := 900.0
	firstTokenFast := 100.0
	tpsSlow := 10.0
	tpsFast := 30.0
	stabilityLow := 0.80
	stabilityHigh := 1.0
	scoring := defaultChannelSmartScheduleScoring()
	plan := planChannelSmartScheduleWithScoring([]channelSmartScheduleCandidate{
		{
			ChannelId: 1, CurrentPriority: 80, CurrentWeight: 50,
			Ratio:        &ratioLow,
			FirstTokenMs: &firstTokenSlow, FirstTokenSampleCount: 6,
			TPS: &tpsSlow, TPSSampleCount: 7,
			Stability: &stabilityLow, StabilitySampleCount: 8, StabilityAvailable: true,
		},
		{
			ChannelId: 2, CurrentPriority: 80, CurrentWeight: 50,
			Ratio:        &ratioHigh,
			FirstTokenMs: &firstTokenFast, FirstTokenSampleCount: 9,
			TPS: &tpsFast, TPSSampleCount: 10,
			Stability: &stabilityHigh, StabilitySampleCount: 11, StabilityAvailable: true,
		},
	}, channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyWeight, 5, false, scoring)

	require.Len(t, plan.Items, 2)
	details := plan.Details[1]
	require.NotNil(t, details)
	assert.Equal(t, model.ChannelSmartScheduleScoreDetailsVersion, details.Version)
	assert.Equal(t, channelMonitorSmartScheduleStrategySmart, details.Strategy)
	assert.Equal(t, 5, details.MinSamples)
	require.NotNil(t, details.Inputs.CostRatio.Value)
	assert.InDelta(t, 1, *details.Inputs.CostRatio.Value, 1e-9)
	assert.Equal(t, int64(1), details.Inputs.CostRatio.SampleCount)
	assert.Equal(t, int64(6), details.Inputs.FirstTokenMs.SampleCount)
	assert.Equal(t, int64(7), details.Inputs.TPS.SampleCount)
	assert.Equal(t, int64(8), details.Inputs.Stability.SampleCount)
	require.NotNil(t, details.Cohort.CostRatio.Minimum)
	require.NotNil(t, details.Cohort.CostRatio.Maximum)
	assert.InDelta(t, 1, *details.Cohort.CostRatio.Minimum, 1e-9)
	assert.InDelta(t, 2, *details.Cohort.CostRatio.Maximum, 1e-9)
	assert.Equal(t, 2, details.Cohort.CostRatio.AvailableCount)
	require.NotNil(t, details.Components.CostRatio.NormalizedScore)
	require.NotNil(t, details.Components.FirstTokenMs.NormalizedScore)
	require.NotNil(t, details.Components.TPS.NormalizedScore)
	assert.InDelta(t, 1, *details.Components.CostRatio.NormalizedScore, 1e-9)
	assert.InDelta(t, 0, *details.Components.FirstTokenMs.NormalizedScore, 1e-9)
	assert.InDelta(t, 0, *details.Components.TPS.NormalizedScore, 1e-9)
	assert.InDelta(t, 40, details.Components.CostRatio.ConfiguredWeightPercent, 1e-9)
	assert.InDelta(t, 40, details.Components.CostRatio.EffectiveWeightPercent, 1e-9)
	assert.InDelta(t, 40, details.Components.FirstTokenMs.EffectiveWeightPercent, 1e-9)
	assert.InDelta(t, 20, details.Components.TPS.EffectiveWeightPercent, 1e-9)
	require.NotNil(t, details.BusinessScore)
	assert.InDelta(t, 0.4, *details.BusinessScore, 1e-9)
	assert.True(t, details.Stability.Applied)
	require.NotNil(t, details.Stability.RawScore)
	assert.InDelta(t, 0.8, *details.Stability.RawScore, 1e-9)
	assert.InDelta(t, 50, details.Stability.EffectiveWeightPercent, 1e-9)
	assert.InDelta(t, 0.2, details.Stability.BusinessContribution, 1e-9)
	assert.InDelta(t, 0.4, details.Stability.Contribution, 1e-9)
	require.NotNil(t, details.FinalScore)
	assert.InDelta(t, 0.6, *details.FinalScore, 1e-9)
	assert.Equal(t, 2, details.Decision.RawWinnerChannelId)
	assert.Equal(t, 2, details.Decision.SelectedPrimaryChannelId)
	assert.False(t, details.Decision.SelectedPrimary)
	assert.Contains(t, details.Decision.SelectionReason, "当前没有唯一主渠道")
}

func TestPlanChannelSmartScheduleRecordsEffectiveWeightAfterUnavailableMetrics(t *testing.T) {
	ratioLow := 1.0
	ratioHigh := 2.0
	plan := planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{ChannelId: 1, CurrentPriority: 80, Ratio: &ratioLow},
		{ChannelId: 2, CurrentPriority: 80, Ratio: &ratioHigh},
	}, channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, 5, false)

	require.Len(t, plan.Items, 2)
	details := plan.Details[1]
	require.NotNil(t, details)
	assert.True(t, details.Components.CostRatio.Available)
	assert.False(t, details.Components.FirstTokenMs.Available)
	assert.False(t, details.Components.TPS.Available)
	assert.InDelta(t, 70, details.Components.CostRatio.ConfiguredWeightPercent, 1e-9)
	assert.InDelta(t, 100, details.Components.CostRatio.EffectiveWeightPercent, 1e-9)
	assert.Zero(t, details.Components.FirstTokenMs.EffectiveWeightPercent)
	assert.Zero(t, details.Components.TPS.EffectiveWeightPercent)
}

func TestPlanChannelSmartScheduleDoesNotScoreSingleFirstTokenSample(t *testing.T) {
	firstToken := 120.0
	ratio := 1.0
	scoring := defaultChannelSmartScheduleScoring()
	plan := planChannelSmartScheduleWithScoring(
		[]channelSmartScheduleCandidate{
			{
				ChannelId: 1, CurrentPriority: 100, CurrentWeight: 1000,
				Ratio:        &ratio,
				FirstTokenMs: &firstToken, FirstTokenSampleCount: 5,
			},
			{
				ChannelId: 2, CurrentPriority: 90, CurrentWeight: 1000,
			},
		},
		channelMonitorSmartScheduleStrategyFirstToken,
		false,
		channelMonitorSmartScheduleApplyPriorityWeight,
		5,
		false,
		scoring,
	)

	assert.Zero(t, plan.RawWinnerId)
	assert.Equal(t, 1, plan.ActualPrimaryId)
	details := plan.Details[1]
	require.NotNil(t, details)
	assert.False(t, details.Components.FirstTokenMs.Available)
	assert.Equal(t, model.ChannelSmartScheduleComparisonInsufficient, details.ComparisonState)
	assert.Equal(t, model.ChannelSmartScheduleComparisonInsufficient, details.Components.CostRatio.ComparisonState)
	assert.Equal(t, model.ChannelSmartScheduleComparisonInsufficient, details.Components.FirstTokenMs.ComparisonState)
	assert.Equal(t, model.ChannelSmartScheduleComparisonNone, details.Components.TPS.ComparisonState)
	assert.Nil(t, details.Components.FirstTokenMs.NormalizedScore)
	assert.Nil(t, details.FinalScore)

	skippedDetails := plan.Details[2]
	require.NotNil(t, skippedDetails)
	assert.Equal(t, model.ChannelSmartScheduleComparisonInsufficient, skippedDetails.ComparisonState)
	assert.Equal(t, model.ChannelSmartScheduleComparisonNone, skippedDetails.Components.CostRatio.ComparisonState)
	assert.Equal(t, model.ChannelSmartScheduleComparisonNone, skippedDetails.Components.FirstTokenMs.ComparisonState)
	assert.Equal(t, model.ChannelSmartScheduleComparisonNone, skippedDetails.Components.TPS.ComparisonState)
}

func TestPlanChannelSmartScheduleComparesFirstTokenAfterTwoSamples(t *testing.T) {
	fast := 120.0
	slow := 240.0
	plan := planChannelSmartScheduleWithScoring(
		[]channelSmartScheduleCandidate{
			{
				ChannelId: 1, CurrentPriority: 100, CurrentWeight: 1000,
				FirstTokenMs: &fast, FirstTokenSampleCount: 5,
			},
			{
				ChannelId: 2, CurrentPriority: 90, CurrentWeight: 1000,
				FirstTokenMs: &slow, FirstTokenSampleCount: 5,
			},
		},
		channelMonitorSmartScheduleStrategyFirstToken,
		false,
		channelMonitorSmartScheduleApplyPriorityWeight,
		5,
		false,
		defaultChannelSmartScheduleScoring(),
	)

	assert.Equal(t, 1, plan.RawWinnerId)
	for _, channelID := range []int{1, 2} {
		details := plan.Details[channelID]
		require.NotNil(t, details)
		assert.True(t, details.Components.FirstTokenMs.Available)
		require.NotNil(t, details.Components.FirstTokenMs.NormalizedScore)
	}
}

func TestPlanChannelSmartSchedulePriorityWeightAssignsUniqueRoutingBelowComparableThreshold(t *testing.T) {
	cheap := 1.0
	expensive := 2.0
	plan := planChannelSmartScheduleWithScoring(
		[]channelSmartScheduleCandidate{
			{
				ChannelId: 1, CurrentPriority: 100, CurrentWeight: 900,
				Ratio: &cheap, MinComparableChannels: 3,
			},
			{
				ChannelId: 2, CurrentPriority: 80, CurrentWeight: 100,
				Ratio: &expensive, MinComparableChannels: 3,
			},
		},
		channelMonitorSmartScheduleStrategyRatio,
		false,
		channelMonitorSmartScheduleApplyPriorityWeight,
		5,
		false,
		defaultChannelSmartScheduleScoring(),
	)

	require.Len(t, plan.Items, 2)
	assert.Zero(t, plan.RawWinnerId)
	assert.Equal(t, 1, plan.ActualPrimaryId)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
		assert.False(t, item.Scored)
		assert.Contains(t, item.SkipReason, "可比渠道不足（2/3）")
	}
	assert.Equal(t, int64(2), items[1].TargetPriority)
	assert.Equal(t, uint(1000), items[1].TargetWeight)
	assert.Equal(t, int64(1), items[2].TargetPriority)
	assert.Equal(t, uint(1000), items[2].TargetWeight)

	details := plan.Details[1]
	require.NotNil(t, details)
	assert.Equal(t, 3, details.MinComparableChannels)
	assert.Equal(t, model.ChannelSmartScheduleComparisonInsufficient, details.ComparisonState)
	assert.Equal(t, 2, details.Cohort.CostRatio.AvailableCount)
	assert.Equal(t, model.ChannelSmartScheduleComparisonInsufficient, details.Components.CostRatio.ComparisonState)
	assert.Nil(t, details.BusinessScore)
	assert.Nil(t, details.FinalScore)
}

func TestPlanChannelSmartScheduleWeightPreservesRoutingBelowComparableThreshold(t *testing.T) {
	cheap := 1.0
	expensive := 2.0
	plan := planChannelSmartScheduleWithScoring(
		[]channelSmartScheduleCandidate{
			{
				ChannelId: 1, CurrentPriority: 100, CurrentWeight: 900,
				Ratio: &cheap, MinComparableChannels: 3,
			},
			{
				ChannelId: 2, CurrentPriority: 100, CurrentWeight: 100,
				Ratio: &expensive, MinComparableChannels: 3,
			},
		},
		channelMonitorSmartScheduleStrategyRatio,
		false,
		channelMonitorSmartScheduleApplyWeight,
		5,
		false,
		defaultChannelSmartScheduleScoring(),
	)

	assert.Empty(t, plan.Items)
	assert.Equal(t, "可比渠道不足（2/3），暂不产生相对评分", plan.Skipped[1])
	assert.Equal(t, "可比渠道不足（2/3），暂不产生相对评分", plan.Skipped[2])
	for _, channelID := range []int{1, 2} {
		details := plan.Details[channelID]
		require.NotNil(t, details)
		assert.Equal(t, model.ChannelSmartScheduleComparisonInsufficient, details.ComparisonState)
		assert.Equal(t, 2, details.Cohort.CostRatio.AvailableCount)
		assert.Nil(t, details.FinalScore)
	}
}

func TestChannelSmartScheduleHealthUsesWindowRequestPercentages(t *testing.T) {
	policy := channelSmartSchedulePolicy{
		AdaptiveSamplingEnabled:                         true,
		MinSamples:                                      5,
		AdaptiveSamplingErrorWarningPercent:             5,
		AdaptiveSamplingErrorCriticalPercent:            15,
		AdaptiveSamplingFirstTokenWarningSeconds:        5,
		AdaptiveSamplingFirstTokenCriticalSeconds:       10,
		AdaptiveSamplingWindowMinutes:                   10,
		AdaptiveSamplingWindowRequests:                  100,
		AdaptiveSamplingFirstTokenWarningRequestPercent: 10,
		AdaptiveSamplingRecoverRequestPercent:           95,
	}
	errorOnly := model.ChannelSmartScheduleAdaptiveHealthMetric{
		RequestCount: 100, FailureCount: 6, HealthyRequestCount: 94, LastUsedTime: 100,
	}

	errorUpdate := channelSmartScheduleEvaluateHealth(model.ChannelSmartScheduleRouteState{}, errorOnly, policy)
	assert.Equal(t, channelSmartScheduleHealthObserve, errorUpdate.State)
	assert.Greater(t, errorUpdate.ErrorPressure, 0.0)
	assert.InDelta(t, 6, errorUpdate.ErrorRequestPercent, 1e-9)
	assert.InDelta(t, 6, errorUpdate.RiskRequestPercent, 1e-9)
	assert.Zero(t, errorUpdate.FirstTokenWarningRequestPercent)
	assert.Equal(t, 10, errorUpdate.WindowMinutes)
	assert.Equal(t, 100, errorUpdate.WindowRequests)

	combinedBelowBothGates := model.ChannelSmartScheduleAdaptiveHealthMetric{
		RequestCount: 100, FailureCount: 5, SlowRequestCount: 9, HealthyRequestCount: 86,
		FirstTokenCount: 9, LatencyPressure: 9, LastUsedTime: 101,
	}
	combinedUpdate := channelSmartScheduleEvaluateHealth(
		model.ChannelSmartScheduleRouteState{}, combinedBelowBothGates, policy,
	)
	assert.Equal(t, channelSmartScheduleHealthHealthy, combinedUpdate.State)
	assert.Zero(t, combinedUpdate.ErrorPressure)
	assert.InDelta(t, 14, combinedUpdate.RiskRequestPercent, 1e-9)
	assert.InDelta(t, 9, combinedUpdate.FirstTokenWarningRequestPercent, 1e-9)

	latencyAtBoundary := model.ChannelSmartScheduleAdaptiveHealthMetric{
		RequestCount: 100, SlowRequestCount: 10, HealthyRequestCount: 90,
		FirstTokenCount: 10, LastUsedTime: 102,
	}
	latencyUpdate := channelSmartScheduleEvaluateHealth(
		model.ChannelSmartScheduleRouteState{}, latencyAtBoundary, policy,
	)
	assert.Equal(t, channelSmartScheduleHealthObserve, latencyUpdate.State)
	assert.Zero(t, latencyUpdate.LatencyPressure)
	assert.InDelta(t, 10, latencyUpdate.FirstTokenWarningRequestPercent, 1e-9)

	previous := model.ChannelSmartScheduleRouteState{
		AdaptiveHealthState: channelSmartScheduleHealthHighRisk,
	}
	mostlyHealthy := model.ChannelSmartScheduleAdaptiveHealthMetric{
		RequestCount: 100, SlowRequestCount: 6, HealthyRequestCount: 94,
		FirstTokenCount: 6, LastUsedTime: 103,
	}
	notRecovered := channelSmartScheduleEvaluateHealth(previous, mostlyHealthy, policy)
	assert.Equal(t, channelSmartScheduleHealthHighRisk, notRecovered.State)
	assert.InDelta(t, 94, notRecovered.HealthyRequestPercent, 1e-9)

	recovering := model.ChannelSmartScheduleAdaptiveHealthMetric{
		RequestCount: 20, SlowRequestCount: 1, HealthyRequestCount: 19,
		FirstTokenCount: 1, LatencyPressure: 1, LastUsedTime: 104,
	}
	recovered := channelSmartScheduleEvaluateHealth(previous, recovering, policy)
	assert.Equal(t, channelSmartScheduleHealthHealthy, recovered.State)
	assert.InDelta(t, 95, recovered.HealthyRequestPercent, 1e-9)

	overlappingRecoveryPolicy := policy
	overlappingRecoveryPolicy.AdaptiveSamplingErrorWarningPercent = 1
	overlappingRecoveryPolicy.AdaptiveSamplingErrorCriticalPercent = 11
	activeError := model.ChannelSmartScheduleAdaptiveHealthMetric{
		RequestCount: 100, FailureCount: 2, HealthyRequestCount: 98, LastUsedTime: 105,
	}
	stillUnderPressure := channelSmartScheduleEvaluateHealth(
		previous, activeError, overlappingRecoveryPolicy,
	)
	assert.Equal(t, channelSmartScheduleHealthObserve, stillUnderPressure.State)
	assert.InDelta(t, 2, stillUnderPressure.ErrorRequestPercent, 1e-9)
}

func TestChannelSmartScheduleAdaptiveSamplingBudgetUsesConfiguredPressure(t *testing.T) {
	policy := channelSmartSchedulePolicy{
		AdaptiveSamplingEnabled:     true,
		AdaptiveSamplingBasePercent: 3,
		AdaptiveSamplingMaxPercent:  30,
		ExplorationTrafficPercent:   8,
		MinSamples:                  5,
		SampleMode:                  channelMonitorSmartScheduleSampleTraffic,
	}
	backup := channelSmartScheduleCandidate{ChannelId: 2, SampleDebt: 5}

	basePrimary := channelSmartScheduleCandidate{
		ChannelId:   1,
		HealthState: channelSmartScheduleHealthObserve,
	}
	assert.InDelta(t, 3, channelSmartScheduleAdaptiveSamplingBudget(
		basePrimary, []channelSmartScheduleCandidate{basePrimary, backup}, policy,
	), 1e-9)

	pressuredPrimary := basePrimary
	pressuredPrimary.HealthState = channelSmartScheduleHealthHighRisk
	pressuredPrimary.HealthPressure = 1
	assert.InDelta(t, 30, channelSmartScheduleAdaptiveSamplingBudget(
		pressuredPrimary, []channelSmartScheduleCandidate{pressuredPrimary, backup}, policy,
	), 1e-9)

	maxBudgetPolicy := policy
	maxBudgetPolicy.AdaptiveSamplingMaxPercent = 49
	assert.InDelta(t, 49, channelSmartScheduleAdaptiveSamplingBudget(
		pressuredPrimary, []channelSmartScheduleCandidate{pressuredPrimary, backup}, maxBudgetPolicy,
	), 1e-9)
}

func TestChannelSmartScheduleSwitchConfirmationKeepsUnverifiedPrimary(t *testing.T) {
	currentDetails := &model.ChannelSmartScheduleScoreDetails{}
	winnerDetails := &model.ChannelSmartScheduleScoreDetails{}
	plan := channelSmartSchedulePlan{
		Items: []channelSmartSchedulePlanItem{
			{ChannelId: 1, Score: 0.4, ScoreDetails: currentDetails, Scored: true, TargetPriority: 1},
			{ChannelId: 2, Score: 0.9, ScoreDetails: winnerDetails, Scored: true, TargetPriority: 2},
		},
		RawWinnerId:     2,
		ActualPrimaryId: 2,
	}
	currentDetails.Decision.CurrentPrimaryChannelId = 1
	winnerDetails.Decision.CurrentPrimaryChannelId = 1
	candidates := []channelSmartScheduleCandidate{
		{ChannelId: 1, HealthState: channelSmartScheduleHealthHealthy, HealthEvidence: true},
		{ChannelId: 2, HealthState: channelSmartScheduleHealthUnknown, HealthEvidence: false},
	}
	policy := channelSmartSchedulePolicy{AdaptiveSamplingSwitchConfirmRequestPercent: 95}

	channelSmartScheduleApplySwitchConfirmation(&plan, candidates, policy, false)

	assert.Equal(t, 1, plan.ActualPrimaryId)
	assert.Equal(t, int64(2), plan.Items[0].TargetPriority)
	assert.Equal(t, int64(1), plan.Items[1].TargetPriority)
	assert.Contains(t, winnerDetails.Decision.SelectionReason, "仅允许自适应采样")
}

func TestChannelSmartScheduleSwitchConfirmationUsesHealthyRequestPercent(t *testing.T) {
	newPlan := func() channelSmartSchedulePlan {
		currentDetails := &model.ChannelSmartScheduleScoreDetails{}
		winnerDetails := &model.ChannelSmartScheduleScoreDetails{}
		currentDetails.Decision.CurrentPrimaryChannelId = 1
		winnerDetails.Decision.CurrentPrimaryChannelId = 1
		return channelSmartSchedulePlan{
			Items: []channelSmartSchedulePlanItem{
				{ChannelId: 1, Score: 0.4, ScoreDetails: currentDetails, Scored: true, TargetPriority: 1},
				{ChannelId: 2, Score: 0.9, ScoreDetails: winnerDetails, Scored: true, TargetPriority: 2},
			},
			RawWinnerId:     2,
			ActualPrimaryId: 2,
		}
	}
	candidates := []channelSmartScheduleCandidate{
		{ChannelId: 1, HealthState: channelSmartScheduleHealthObserve, HealthEvidence: true},
		{ChannelId: 2, HealthState: channelSmartScheduleHealthHealthy, HealthEvidence: true, HealthHealthyRequestPercent: 90},
	}
	policy := channelSmartSchedulePolicy{AdaptiveSamplingSwitchConfirmRequestPercent: 95}

	firstPlan := newPlan()
	channelSmartScheduleApplySwitchConfirmation(&firstPlan, candidates, policy, false)

	assert.Equal(t, 1, firstPlan.ActualPrimaryId)

	candidates[1].HealthHealthyRequestPercent = 95
	secondPlan := newPlan()
	channelSmartScheduleApplySwitchConfirmation(&secondPlan, candidates, policy, false)

	assert.Equal(t, 2, secondPlan.ActualPrimaryId)
}

func TestChannelSmartScheduleSwitchConfirmationUsesNextConfirmedCandidate(t *testing.T) {
	currentDetails := &model.ChannelSmartScheduleScoreDetails{}
	winnerDetails := &model.ChannelSmartScheduleScoreDetails{}
	nextDetails := &model.ChannelSmartScheduleScoreDetails{}
	fallbackDetails := &model.ChannelSmartScheduleScoreDetails{}
	plan := channelSmartSchedulePlan{
		Items: []channelSmartSchedulePlanItem{
			{ChannelId: 1, Score: 0.4, ScoreDetails: currentDetails, Scored: true, TargetPriority: 2, TargetWeight: 10000},
			{ChannelId: 2, Score: 0.9, ScoreDetails: winnerDetails, Scored: true, TargetPriority: 4, TargetWeight: 10000},
			{ChannelId: 3, Score: 0.8, ScoreDetails: nextDetails, Scored: true, TargetPriority: 3, TargetWeight: 10000},
			{ChannelId: 4, Score: 1, ScoreDetails: fallbackDetails, Scored: true, TargetPriority: 1, TargetWeight: 10000},
		},
		RawWinnerId:     2,
		ActualPrimaryId: 2,
	}
	for _, details := range []*model.ChannelSmartScheduleScoreDetails{
		currentDetails,
		winnerDetails,
		nextDetails,
		fallbackDetails,
	} {
		details.Decision.CurrentPrimaryChannelId = 1
	}
	candidates := []channelSmartScheduleCandidate{
		{
			ChannelId: 1, HealthEvidence: true, HealthState: channelSmartScheduleHealthObserve,
			HealthHealthyRequestPercent: 40,
		},
		{
			ChannelId: 2, HealthEvidence: true, HealthState: channelSmartScheduleHealthObserve,
			HealthHealthyRequestPercent: 83.3,
		},
		{
			ChannelId: 3, HealthEvidence: true, HealthState: channelSmartScheduleHealthHealthy,
			HealthHealthyRequestPercent: 100,
		},
		{
			ChannelId: 4, EconomicRole: channelSmartScheduleEconomicRoleBreakEvenFallback,
			HealthEvidence: true, HealthState: channelSmartScheduleHealthHealthy,
			HealthHealthyRequestPercent: 100,
		},
	}
	policy := channelSmartSchedulePolicy{
		AdaptiveSamplingSwitchConfirmRequestPercent: 95,
		Scoring: channelSmartScheduleScoring{
			PrimarySwitchThresholdPercent: 3,
		},
	}

	channelSmartScheduleApplySwitchConfirmation(&plan, candidates, policy, false)

	assert.Equal(t, 3, plan.ActualPrimaryId)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, int64(4), items[3].TargetPriority)
	assert.Equal(t, int64(3), items[2].TargetPriority)
	assert.Equal(t, int64(2), items[1].TargetPriority)
	assert.Equal(t, int64(1), items[4].TargetPriority)
	assert.Equal(t, 3, nextDetails.Decision.ActualPrimaryChannelId)
	assert.True(t, nextDetails.Decision.SelectedPrimary)
	assert.Equal(t, items[3].TargetPriority, nextDetails.Decision.AppliedPriority)
	assert.Equal(t, items[4].TargetPriority, fallbackDetails.Decision.AppliedPriority)
	assert.Contains(t, nextDetails.Decision.SelectionReason, "评分第一渠道 ID 2")
	assert.Contains(t, nextDetails.Decision.SelectionReason, "渠道 ID 3")
}

func TestChannelSmartScheduleSwitchConfirmationKeepsPrimaryWhenConfirmedFallbackMissesScoreThreshold(t *testing.T) {
	currentDetails := &model.ChannelSmartScheduleScoreDetails{}
	winnerDetails := &model.ChannelSmartScheduleScoreDetails{}
	nextDetails := &model.ChannelSmartScheduleScoreDetails{}
	plan := channelSmartSchedulePlan{
		Items: []channelSmartSchedulePlanItem{
			{ChannelId: 1, Score: 0.4, ScoreDetails: currentDetails, Scored: true, TargetPriority: 1},
			{ChannelId: 2, Score: 0.9, ScoreDetails: winnerDetails, Scored: true, TargetPriority: 3},
			{ChannelId: 3, Score: 0.42, ScoreDetails: nextDetails, Scored: true, TargetPriority: 2},
		},
		RawWinnerId:     2,
		ActualPrimaryId: 2,
	}
	for _, details := range []*model.ChannelSmartScheduleScoreDetails{
		currentDetails,
		winnerDetails,
		nextDetails,
	} {
		details.Decision.CurrentPrimaryChannelId = 1
	}
	candidates := []channelSmartScheduleCandidate{
		{ChannelId: 1, HealthEvidence: true, HealthState: channelSmartScheduleHealthObserve},
		{ChannelId: 2, HealthEvidence: true, HealthState: channelSmartScheduleHealthObserve},
		{
			ChannelId: 3, HealthEvidence: true, HealthState: channelSmartScheduleHealthHealthy,
			HealthHealthyRequestPercent: 100,
		},
	}
	policy := channelSmartSchedulePolicy{
		AdaptiveSamplingSwitchConfirmRequestPercent: 95,
		Scoring: channelSmartScheduleScoring{
			PrimarySwitchThresholdPercent: 3,
		},
	}

	channelSmartScheduleApplySwitchConfirmation(&plan, candidates, policy, false)

	assert.Equal(t, 1, plan.ActualPrimaryId)
	assert.True(t, currentDetails.Decision.SelectedPrimary)
}

func TestChannelSmartScheduleSwitchConfirmationDoesNotCrossEconomicLayers(t *testing.T) {
	currentDetails := &model.ChannelSmartScheduleScoreDetails{}
	winnerDetails := &model.ChannelSmartScheduleScoreDetails{}
	fallbackDetails := &model.ChannelSmartScheduleScoreDetails{}
	plan := channelSmartSchedulePlan{
		Items: []channelSmartSchedulePlanItem{
			{ChannelId: 1, Score: 0.4, ScoreDetails: currentDetails, Scored: true, TargetPriority: 2},
			{ChannelId: 2, Score: 0.9, ScoreDetails: winnerDetails, Scored: true, TargetPriority: 3},
			{ChannelId: 3, Score: 1, ScoreDetails: fallbackDetails, Scored: true, TargetPriority: 1},
		},
		RawWinnerId:     2,
		ActualPrimaryId: 2,
	}
	for _, details := range []*model.ChannelSmartScheduleScoreDetails{
		currentDetails,
		winnerDetails,
		fallbackDetails,
	} {
		details.Decision.CurrentPrimaryChannelId = 1
	}
	candidates := []channelSmartScheduleCandidate{
		{
			ChannelId: 1, EconomicRole: channelSmartScheduleEconomicRoleNormal,
			HealthEvidence: true, HealthState: channelSmartScheduleHealthObserve,
		},
		{
			ChannelId: 2, EconomicRole: channelSmartScheduleEconomicRoleNormal,
			HealthEvidence: true, HealthState: channelSmartScheduleHealthObserve,
		},
		{
			ChannelId: 3, EconomicRole: channelSmartScheduleEconomicRoleBreakEvenFallback,
			HealthEvidence: true, HealthState: channelSmartScheduleHealthHealthy,
			HealthHealthyRequestPercent: 100,
		},
	}
	policy := channelSmartSchedulePolicy{
		AdaptiveSamplingSwitchConfirmRequestPercent: 95,
		Scoring: channelSmartScheduleScoring{
			PrimarySwitchThresholdPercent: 3,
		},
	}

	channelSmartScheduleApplySwitchConfirmation(&plan, candidates, policy, false)

	assert.Equal(t, 1, plan.ActualPrimaryId)
	assert.True(t, currentDetails.Decision.SelectedPrimary)
	assert.Equal(t, int64(1), plan.Items[2].TargetPriority)
}

func TestChannelSmartScheduleSwitchConfirmationRejectsInvalidHealthyRequestPercent(t *testing.T) {
	for _, healthyRequestPercent := range []float64{math.NaN(), math.Inf(1)} {
		t.Run(fmt.Sprintf("percent_%v", healthyRequestPercent), func(t *testing.T) {
			currentDetails := &model.ChannelSmartScheduleScoreDetails{}
			winnerDetails := &model.ChannelSmartScheduleScoreDetails{}
			currentDetails.Decision.CurrentPrimaryChannelId = 1
			winnerDetails.Decision.CurrentPrimaryChannelId = 1
			plan := channelSmartSchedulePlan{
				Items: []channelSmartSchedulePlanItem{
					{ChannelId: 1, Score: 0.4, ScoreDetails: currentDetails, Scored: true, TargetPriority: 1},
					{ChannelId: 2, Score: 0.9, ScoreDetails: winnerDetails, Scored: true, TargetPriority: 2},
				},
				RawWinnerId:     2,
				ActualPrimaryId: 2,
			}
			candidates := []channelSmartScheduleCandidate{
				{ChannelId: 1, HealthEvidence: true, HealthState: channelSmartScheduleHealthObserve},
				{
					ChannelId: 2, HealthEvidence: true, HealthState: channelSmartScheduleHealthHealthy,
					HealthHealthyRequestPercent: healthyRequestPercent,
				},
			}

			channelSmartScheduleApplySwitchConfirmation(&plan, candidates, channelSmartSchedulePolicy{
				AdaptiveSamplingSwitchConfirmRequestPercent: 95,
			}, false)

			assert.Equal(t, 1, plan.ActualPrimaryId)
			assert.True(t, currentDetails.Decision.SelectedPrimary)
			assert.Contains(t, winnerDetails.Decision.SelectionReason, "健康状态未确认")
		})
	}
}

func TestRunChannelSmartSchedulePersistsExecutionTimeScoreDetails(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelSmartScheduleGroupRatio(t, `{"vip":100}`)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 5, 80, 30,
			),
		),
	})
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 3211, Name: "snapshot-low", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 3212, Name: "snapshot-high", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 3213, Name: "snapshot-excluded", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 3211, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 3212, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 3213, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 3211, Ratio: 1, UpdatedTime: 1},
		{ChannelId: 3212, Ratio: 3, UpdatedTime: 1},
		{ChannelId: 3213, Ratio: 2, UpdatedTime: 1},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 3211, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 3212, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 3213, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Excluded: true},
	}).Error)

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, true)
	require.NoError(t, err)
	require.Len(t, result.Adjustments, 3)
	var executionDetails *model.ChannelSmartScheduleScoreDetails
	for _, adjustment := range result.Adjustments {
		require.NotNil(t, adjustment.ScoreDetails)
		assert.NotEmpty(t, adjustment.ScoreDetails.Decision.AdjustmentReason)
		if adjustment.ChannelId == 3211 {
			executionDetails = adjustment.ScoreDetails
		}
	}
	require.NotNil(t, executionDetails)
	rawResult, err := common.Marshal(result)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelSmartScheduleScoreDetailsVersion, executionDetails.Version)
	assert.Contains(t, string(rawResult), `"score_details":{"version":`)
	assert.Contains(t, string(rawResult), `"minimum_samples":5`)
	assert.Contains(t, string(rawResult), `"switch_threshold_percent":3`)
	require.NotNil(t, executionDetails.Inputs.CostRatio.Value)
	assert.InDelta(t, 1, *executionDetails.Inputs.CostRatio.Value, 1e-9)

	var stored model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 3211, "vip", "model-a",
	).First(&stored).Error)
	persistedDetails, err := stored.LastScheduleScoreDetails.Decode()
	require.NoError(t, err)
	require.NotNil(t, persistedDetails)
	require.NotNil(t, persistedDetails.Inputs.CostRatio.Value)
	assert.InDelta(t, 1, *persistedDetails.Inputs.CostRatio.Value, 1e-9)
	assert.Equal(t, executionDetails.Decision.AdjustmentReason, persistedDetails.Decision.AdjustmentReason)

	require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).
		Where("channel_id = ?", 3211).
		Updates(map[string]any{"ratio": 99.0, "updated_time": int64(999)}).Error)
	routes, err := model.GetChannelSmartScheduleRoutes()
	require.NoError(t, err)
	for _, route := range routes {
		if route.ChannelId != 3211 || route.Group != "vip" || route.Model != "model-a" {
			continue
		}
		latest, decodeErr := route.State.LastScheduleScoreDetails.Decode()
		require.NoError(t, decodeErr)
		require.NotNil(t, latest)
		require.NotNil(t, latest.Inputs.CostRatio.Value)
		assert.InDelta(t, 1, *latest.Inputs.CostRatio.Value, 1e-9)
		return
	}
	t.Fatal("expected smart-schedule route was not returned")
}
