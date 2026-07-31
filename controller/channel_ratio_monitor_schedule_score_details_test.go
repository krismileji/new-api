package controller

import (
	"context"
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

func TestRunChannelSmartSchedulePersistsExecutionTimeScoreDetails(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
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
