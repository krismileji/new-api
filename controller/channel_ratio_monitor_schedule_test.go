package controller

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunChannelSmartScheduleForceResetSetsBaselineBeforePlanning(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:   "true",
		channelMonitorSmartScheduleStrategyOption:  channelMonitorSmartScheduleStrategyRatio,
		channelMonitorSmartScheduleApplyModeOption: channelMonitorSmartScheduleApplyWeight,
	})
	priority := int64(100)
	weight := uint(90)
	channels := []model.Channel{
		{Id: 11, Name: "best", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 12, Name: "worst", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 13, Name: "missing ratio", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 14, Name: "excluded", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 11, Ratio: 1, UpdatedTime: 1},
		{ChannelId: 12, Ratio: 3, UpdatedTime: 1},
		{ChannelId: 13},
		{ChannelId: 14, Ratio: 2, UpdatedTime: 1, SmartScheduleExcluded: true},
	}).Error)
	abilities := make([]model.Ability, 0, len(channels))
	for _, channel := range channels {
		abilities = append(abilities, model.Ability{
			Group:     "vip",
			Model:     "model-a",
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  &priority,
			Weight:    weight,
		})
	}
	require.NoError(t, db.Create(&abilities).Error)

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, true)
	require.NoError(t, err)
	assert.Equal(t, 3, result.Updated)
	assert.Zero(t, result.Unchanged)
	assert.Equal(t, 1, result.Skipped)

	expected := map[int]struct {
		priority int64
		weight   uint
	}{
		11: {priority: 80, weight: 100},
		12: {priority: 80, weight: 10},
		13: {priority: 80, weight: 10},
		14: {priority: 100, weight: 90},
	}
	for channelId, target := range expected {
		var ability model.Ability
		require.NoError(t, db.First(&ability, "channel_id = ?", channelId).Error)
		require.NotNil(t, ability.Priority)
		assert.Equal(t, target.priority, *ability.Priority)
		assert.Equal(t, target.weight, ability.Weight)
	}

	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 13, "vip", "model-a",
	).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleStatusSkipped, state.LastScheduleStatus)
	assert.Equal(t, int64(80), state.LastSchedulePriority)
	assert.Equal(t, uint(10), state.LastScheduleWeight)
}

func TestRunChannelSmartScheduleForceResetKeepsBaselineWhenCohortIsTooSmall(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:   "true",
		channelMonitorSmartScheduleStrategyOption:  channelMonitorSmartScheduleStrategyRatio,
		channelMonitorSmartScheduleApplyModeOption: channelMonitorSmartScheduleApplyWeight,
	})
	firstPriority := int64(100)
	firstWeight := uint(80)
	secondPriority := int64(90)
	secondWeight := uint(70)
	channels := []model.Channel{
		{Id: 21, Name: "only candidate", Group: "vip", Status: common.ChannelStatusEnabled, Priority: &firstPriority, Weight: &firstWeight},
		{Id: 22, Name: "missing ratio", Group: "vip", Status: common.ChannelStatusEnabled, Priority: &secondPriority, Weight: &secondWeight},
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 21, Ratio: 1, UpdatedTime: 1},
		{ChannelId: 22},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 21, Group: "vip", Model: "model-a", Enabled: true, Priority: &firstPriority, Weight: firstWeight},
		{ChannelId: 22, Group: "vip", Model: "model-a", Enabled: true, Priority: &secondPriority, Weight: secondWeight},
	}).Error)

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, true)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Updated)
	assert.Zero(t, result.Skipped)

	for channelId, expected := range map[int]struct {
		priority int64
		weight   uint
	}{
		21: {priority: 80, weight: 10},
		22: {priority: 80, weight: 10},
	} {
		var ability model.Ability
		require.NoError(t, db.First(&ability, "channel_id = ?", channelId).Error)
		require.NotNil(t, ability.Priority)
		assert.Equal(t, expected.priority, *ability.Priority)
		assert.Equal(t, expected.weight, ability.Weight)
	}
}

func TestRunChannelSmartScheduleDegradesReleasesAndRechecksOnlyProbeSamples(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:     "true",
		channelMonitorSmartScheduleStrategyOption:    channelMonitorSmartScheduleStrategyRatio,
		channelMonitorSmartScheduleStabilityOption:   "true",
		channelMonitorSmartScheduleApplyModeOption:   channelMonitorSmartScheduleApplyPriorityWeight,
		channelMonitorSmartScheduleModelsOption:      `["model-a"]`,
		channelMonitorSmartScheduleSamplesOption:     "2",
		channelMonitorSmartScheduleSuccessRateOption: "80",
		channelMonitorSmartScheduleCooldownOption:    "30",
	})
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	common.LogConsumeEnabled = true
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})

	priority := int64(90)
	weight := uint(35)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 31, Name: "unstable", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 32, Name: "stable", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 31, Ratio: 2, UpdatedTime: 1},
		{ChannelId: 32, Ratio: 1, UpdatedTime: 1},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 31, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 32, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 31, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 32, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)
	completedMinuteEnd := time.Now().Unix()
	completedMinuteEnd -= completedMinuteEnd % 60
	initialMinute := completedMinuteEnd - 180
	initialLogTime := initialMinute + 1
	require.NoError(t, db.Create(&[]model.Log{
		{ChannelId: 31, Group: "vip", ModelName: "model-a", CreatedAt: initialLogTime, Type: model.LogTypeError},
		{ChannelId: 31, Group: "vip", ModelName: "model-a", CreatedAt: initialLogTime, Type: model.LogTypeError},
		{ChannelId: 32, Group: "vip", ModelName: "model-a", CreatedAt: initialLogTime, Type: model.LogTypeConsume},
		{ChannelId: 32, Group: "vip", ModelName: "model-a", CreatedAt: initialLogTime, Type: model.LogTypeConsume},
	}).Error)
	require.NoError(t, aggregateChannelMonitorTestLogs(initialMinute, initialMinute+60))

	_, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 31, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 31, "vip", "model-a",
	).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, state.StabilityState)
	assert.Equal(t, int64(90), state.StabilitySavedPriority)
	assert.Equal(t, uint(35), state.StabilitySavedWeight)

	require.NoError(t, db.Model(&model.ChannelSmartScheduleRouteState{}).
		Where("channel_id = ? AND group_name = ? AND model_name = ?", 31, "vip", "model-a").
		Update("stability_until", time.Now().Unix()-1).Error)
	_, err = runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	require.NoError(t, db.Where(&model.Ability{ChannelId: 31, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(90), *ability.Priority)
	assert.Equal(t, uint(channelMonitorSmartScheduleMinWeight), ability.Weight)
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 31, "vip", "model-a",
	).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityProbing, state.StabilityState)
	require.Positive(t, state.StabilitySince)
	probeMinute := completedMinuteEnd - 60
	require.NoError(t, db.Model(&model.ChannelSmartScheduleRouteState{}).
		Where("channel_id = ? AND group_name = ? AND model_name = ?", 31, "vip", "model-a").
		Update("stability_since", probeMinute).Error)
	state.StabilitySince = probeMinute

	oldSuccesses := make([]model.Log, 20)
	for index := range oldSuccesses {
		oldSuccesses[index] = model.Log{
			ChannelId: 31, Group: "vip", ModelName: "model-a", CreatedAt: state.StabilitySince - 1, Type: model.LogTypeConsume,
		}
	}
	require.NoError(t, db.Create(&oldSuccesses).Error)
	require.NoError(t, db.Create(&[]model.Log{
		{ChannelId: 31, Group: "vip", ModelName: "model-a", CreatedAt: state.StabilitySince, Type: model.LogTypeError},
		{ChannelId: 31, Group: "vip", ModelName: "model-a", CreatedAt: state.StabilitySince, Type: model.LogTypeError},
	}).Error)
	require.NoError(t, aggregateChannelMonitorTestLogs(probeMinute-60, completedMinuteEnd))

	_, err = runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	require.NoError(t, db.Where(&model.Ability{ChannelId: 31, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 31, "vip", "model-a",
	).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, state.StabilityState)
}

func TestRunChannelSmartScheduleClearsProbeStateAfterSuccessfulNewSamples(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:     "true",
		channelMonitorSmartScheduleStrategyOption:    channelMonitorSmartScheduleStrategyRatio,
		channelMonitorSmartScheduleStabilityOption:   "true",
		channelMonitorSmartScheduleApplyModeOption:   channelMonitorSmartScheduleApplyPriorityWeight,
		channelMonitorSmartScheduleModelsOption:      `["model-a"]`,
		channelMonitorSmartScheduleSamplesOption:     "2",
		channelMonitorSmartScheduleSuccessRateOption: "80",
	})
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	common.LogConsumeEnabled = true
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})

	priority := int64(80)
	probeWeight := uint(channelMonitorSmartScheduleMinWeight)
	probeStartedAt := time.Now().Unix()
	probeStartedAt = probeStartedAt - probeStartedAt%60 - 60
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 33, Name: "recovering", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &probeWeight},
		{Id: 34, Name: "stable", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &probeWeight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 33, Ratio: 2, UpdatedTime: 1},
		{ChannelId: 34, Ratio: 1, UpdatedTime: 1},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 33, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: probeWeight},
		{ChannelId: 34, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: probeWeight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{
			ChannelId: 33, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
			StabilityState:         model.ChannelSmartScheduleStabilityProbing,
			StabilitySince:         probeStartedAt,
			StabilitySavedPriority: 80,
			StabilitySavedWeight:   30,
		},
		{ChannelId: 34, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)
	require.NoError(t, db.Create(&[]model.Log{
		{ChannelId: 33, Group: "vip", ModelName: "model-a", CreatedAt: probeStartedAt, Type: model.LogTypeConsume},
		{ChannelId: 33, Group: "vip", ModelName: "model-a", CreatedAt: probeStartedAt, Type: model.LogTypeConsume},
		{ChannelId: 34, Group: "vip", ModelName: "model-a", CreatedAt: probeStartedAt, Type: model.LogTypeConsume},
		{ChannelId: 34, Group: "vip", ModelName: "model-a", CreatedAt: probeStartedAt, Type: model.LogTypeConsume},
	}).Error)
	require.NoError(t, aggregateChannelMonitorTestLogs(probeStartedAt, probeStartedAt+60))

	_, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 33, "vip", "model-a",
	).First(&state).Error)
	assert.Empty(t, state.StabilityState)
	assert.Equal(t, probeStartedAt, state.StabilitySince)
	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 33, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(80), *ability.Priority)
	assert.Equal(t, uint(30), ability.Weight)
}

func TestPlanChannelSmartScheduleWeightOnlyKeepsPriorityCohorts(t *testing.T) {
	ratioOne := 1.0
	ratioTwo := 2.0
	ratioThree := 3.0
	plan := planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{ChannelId: 1, CurrentPriority: 0, Ratio: &ratioOne},
		{ChannelId: 2, CurrentPriority: 0, Ratio: &ratioTwo},
		{ChannelId: 3, CurrentPriority: 10, Ratio: &ratioThree},
		{ChannelId: 4, CurrentPriority: 10, Ratio: &ratioOne},
	}, channelMonitorSmartScheduleStrategyRatio, false, channelMonitorSmartScheduleApplyWeight, 5, false)

	require.Len(t, plan.Items, 4)
	assert.Empty(t, plan.Skipped)

	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, int64(0), items[1].TargetPriority)
	assert.Equal(t, uint(100), items[1].TargetWeight)
	assert.Equal(t, int64(0), items[2].TargetPriority)
	assert.Equal(t, uint(10), items[2].TargetWeight)
	assert.Equal(t, int64(10), items[3].TargetPriority)
	assert.Equal(t, uint(10), items[3].TargetWeight)
	assert.Equal(t, int64(10), items[4].TargetPriority)
	assert.Equal(t, uint(100), items[4].TargetWeight)
}

func TestPlanChannelSmartSchedulePriorityWeightUsesQualityTiersAndDamping(t *testing.T) {
	ratioOne := 1.0
	ratioTwo := 2.0
	ratioThree := 3.0
	plan := planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{ChannelId: 1, CurrentPriority: 0, CurrentWeight: 50, Ratio: &ratioOne},
		{ChannelId: 2, CurrentPriority: 0, CurrentWeight: 50, Ratio: &ratioTwo},
		{ChannelId: 3, CurrentPriority: 0, CurrentWeight: 50, Ratio: &ratioThree},
	}, channelMonitorSmartScheduleStrategyRatio, false, channelMonitorSmartScheduleApplyPriorityWeight, 5, false)

	require.Len(t, plan.Items, 3)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, int64(100), items[1].TargetPriority)
	assert.Equal(t, uint(80), items[1].TargetWeight)
	assert.Equal(t, int64(90), items[2].TargetPriority)
	assert.Equal(t, uint(50), items[2].TargetWeight)
	assert.Equal(t, int64(80), items[3].TargetPriority)
	assert.Equal(t, uint(20), items[3].TargetWeight)
}

func TestPlanChannelSmartScheduleUsesLinearScoreCurveByDefault(t *testing.T) {
	ratioLow := 1.0
	ratioMiddle := 2.0
	ratioHigh := 3.0
	firstTokenFast := 100.0
	firstTokenMiddle := 200.0
	firstTokenSlow := 300.0
	tpsFast := 30.0
	tpsMiddle := 20.0
	tpsSlow := 10.0
	tests := []struct {
		name       string
		strategy   string
		candidates []channelSmartScheduleCandidate
	}{
		{
			name:     "cost ratio",
			strategy: channelMonitorSmartScheduleStrategyRatio,
			candidates: []channelSmartScheduleCandidate{
				{ChannelId: 1, CurrentPriority: 80, CurrentWeight: 10, Ratio: &ratioLow},
				{ChannelId: 2, CurrentPriority: 80, CurrentWeight: 10, Ratio: &ratioMiddle},
				{ChannelId: 3, CurrentPriority: 80, CurrentWeight: 10, Ratio: &ratioHigh},
			},
		},
		{
			name:     "first token",
			strategy: channelMonitorSmartScheduleStrategyFirstToken,
			candidates: []channelSmartScheduleCandidate{
				{ChannelId: 1, CurrentPriority: 80, CurrentWeight: 10, FirstTokenMs: &firstTokenFast, FirstTokenSampleCount: 5},
				{ChannelId: 2, CurrentPriority: 80, CurrentWeight: 10, FirstTokenMs: &firstTokenMiddle, FirstTokenSampleCount: 5},
				{ChannelId: 3, CurrentPriority: 80, CurrentWeight: 10, FirstTokenMs: &firstTokenSlow, FirstTokenSampleCount: 5},
			},
		},
		{
			name:     "tps",
			strategy: channelMonitorSmartScheduleStrategyTPS,
			candidates: []channelSmartScheduleCandidate{
				{ChannelId: 1, CurrentPriority: 80, CurrentWeight: 10, TPS: &tpsFast, TPSSampleCount: 5},
				{ChannelId: 2, CurrentPriority: 80, CurrentWeight: 10, TPS: &tpsMiddle, TPSSampleCount: 5},
				{ChannelId: 3, CurrentPriority: 80, CurrentWeight: 10, TPS: &tpsSlow, TPSSampleCount: 5},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := planChannelSmartSchedule(tt.candidates, tt.strategy, false, channelMonitorSmartScheduleApplyWeight, 5, false)

			require.Len(t, plan.Items, 3)
			items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
			for _, item := range plan.Items {
				items[item.ChannelId] = item
			}
			assert.Equal(t, uint(40), items[1].TargetWeight)
			assert.Equal(t, uint(40), items[2].TargetWeight)
			assert.Equal(t, uint(10), items[3].TargetWeight)
			assert.InDelta(t, 0.5, items[2].Score, 1e-9)
		})
	}
}

func TestPlanChannelSmartScheduleRatioBalancesPerformanceGuardrails(t *testing.T) {
	ratioCheap := 1.0
	ratioExpensive := 1.01
	firstTokenSlow := 900.0
	firstTokenFast := 100.0
	tpsSlow := 10.0
	tpsFast := 30.0
	scoring := defaultChannelSmartScheduleScoring()
	scoring.RelativeWeightEnabled = false
	plan := planChannelSmartScheduleWithScoring([]channelSmartScheduleCandidate{
		{
			ChannelId:             1,
			CurrentPriority:       80,
			CurrentWeight:         10,
			Ratio:                 &ratioCheap,
			FirstTokenMs:          &firstTokenSlow,
			FirstTokenSampleCount: 5,
			TPS:                   &tpsSlow,
			TPSSampleCount:        5,
		},
		{
			ChannelId:             2,
			CurrentPriority:       80,
			CurrentWeight:         10,
			Ratio:                 &ratioExpensive,
			FirstTokenMs:          &firstTokenFast,
			FirstTokenSampleCount: 5,
			TPS:                   &tpsFast,
			TPSSampleCount:        5,
		},
	}, channelMonitorSmartScheduleStrategyRatio, false, channelMonitorSmartScheduleApplyWeight, 5, false, scoring)

	require.Len(t, plan.Items, 2)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.InDelta(t, 0.7, items[1].Score, 1e-9)
	assert.InDelta(t, 0.3, items[2].Score, 1e-9)
	assert.Equal(t, uint(40), items[1].TargetWeight)
	assert.Equal(t, uint(35), items[2].TargetWeight)
}

func TestPlanChannelSmartScheduleUsesConfiguredStrategyPercentages(t *testing.T) {
	ratioCheap := 1.0
	ratioExpensive := 2.0
	firstTokenSlow := 900.0
	firstTokenFast := 100.0
	tpsSlow := 10.0
	tpsFast := 30.0
	candidates := []channelSmartScheduleCandidate{
		{
			ChannelId: 1, Ratio: &ratioCheap,
			FirstTokenMs: &firstTokenSlow, FirstTokenSampleCount: 5,
			TPS: &tpsSlow, TPSSampleCount: 5,
		},
		{
			ChannelId: 2, Ratio: &ratioExpensive,
			FirstTokenMs: &firstTokenFast, FirstTokenSampleCount: 5,
			TPS: &tpsFast, TPSSampleCount: 5,
		},
	}
	scoring := defaultChannelSmartScheduleScoring()
	scoring.RelativeWeightEnabled = false
	scoring.Smart = channelSmartScheduleMetricPercentages{
		CostRatioPercent: 60, FirstTokenPercent: 20, TPSPercent: 20,
	}
	scoring.Ratio = channelSmartScheduleMetricPercentages{
		CostRatioPercent: 20, FirstTokenPercent: 40, TPSPercent: 40,
	}

	plan := planChannelSmartScheduleWithScoring(
		candidates,
		channelMonitorSmartScheduleStrategySmart,
		false,
		channelMonitorSmartScheduleApplyWeight,
		5,
		false,
		scoring,
	)
	require.Len(t, plan.Items, 2)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.InDelta(t, 0.6, items[1].Score, 1e-9)
	assert.InDelta(t, 0.4, items[2].Score, 1e-9)
	assert.Equal(t, uint(65), items[1].TargetWeight)
	assert.Equal(t, uint(45), items[2].TargetWeight)

	plan = planChannelSmartScheduleWithScoring(
		candidates,
		channelMonitorSmartScheduleStrategyRatio,
		false,
		channelMonitorSmartScheduleApplyWeight,
		5,
		false,
		scoring,
	)
	require.Len(t, plan.Items, 2)
	items = make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.InDelta(t, 0.2, items[1].Score, 1e-9)
	assert.InDelta(t, 0.8, items[2].Score, 1e-9)
	assert.Equal(t, uint(30), items[1].TargetWeight)
	assert.Equal(t, uint(80), items[2].TargetWeight)
}

func TestPlanChannelSmartScheduleDoesNotRequireZeroPercentMetrics(t *testing.T) {
	ratioLow := 1.0
	ratioHigh := 2.0
	scoring := defaultChannelSmartScheduleScoring()
	scoring.Smart = channelSmartScheduleMetricPercentages{CostRatioPercent: 100}

	plan := planChannelSmartScheduleWithScoring(
		[]channelSmartScheduleCandidate{
			{ChannelId: 1, Ratio: &ratioLow},
			{ChannelId: 2, Ratio: &ratioHigh},
		},
		channelMonitorSmartScheduleStrategySmart,
		false,
		channelMonitorSmartScheduleApplyWeight,
		5,
		false,
		scoring,
	)

	require.Len(t, plan.Items, 2)
	assert.Empty(t, plan.Skipped)
}

func TestPlanChannelSmartScheduleFullStabilityDoesNotRequireBusinessMetrics(t *testing.T) {
	stabilityLow := 0.8
	stabilityHigh := 1.0
	scoring := defaultChannelSmartScheduleScoring()
	scoring.StabilityPercent = 100
	candidates := []channelSmartScheduleCandidate{
		{ChannelId: 1, Stability: &stabilityLow, StabilitySampleCount: 5, StabilityAvailable: true},
		{ChannelId: 2, Stability: &stabilityHigh, StabilitySampleCount: 5, StabilityAvailable: true},
	}

	for _, strategy := range []string{
		channelMonitorSmartScheduleStrategySmart,
		channelMonitorSmartScheduleStrategyRatio,
	} {
		plan := planChannelSmartScheduleWithScoring(
			candidates,
			strategy,
			true,
			channelMonitorSmartScheduleApplyWeight,
			5,
			false,
			scoring,
		)

		require.Len(t, plan.Items, 2)
		assert.Empty(t, plan.Skipped)
		assert.InDelta(t, stabilityLow, plan.Items[0].Score, 1e-9)
		assert.InDelta(t, stabilityHigh, plan.Items[1].Score, 1e-9)
	}
}

func TestPlanChannelSmartScheduleRatioIgnoresInsufficientPerformanceSamples(t *testing.T) {
	ratioCheap := 1.0
	ratioExpensive := 2.0
	firstTokenSlow := 900.0
	firstTokenFast := 100.0
	tpsSlow := 10.0
	tpsFast := 30.0
	plan := planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{
			ChannelId:             1,
			CurrentPriority:       80,
			CurrentWeight:         10,
			Ratio:                 &ratioCheap,
			FirstTokenMs:          &firstTokenSlow,
			FirstTokenSampleCount: 4,
			TPS:                   &tpsSlow,
			TPSSampleCount:        4,
		},
		{
			ChannelId:             2,
			CurrentPriority:       80,
			CurrentWeight:         10,
			Ratio:                 &ratioExpensive,
			FirstTokenMs:          &firstTokenFast,
			FirstTokenSampleCount: 5,
			TPS:                   &tpsFast,
			TPSSampleCount:        5,
		},
	}, channelMonitorSmartScheduleStrategyRatio, false, channelMonitorSmartScheduleApplyWeight, 5, false)

	require.Len(t, plan.Items, 2)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.InDelta(t, 1, items[1].Score, 1e-9)
	assert.InDelta(t, 0, items[2].Score, 1e-9)
	assert.Equal(t, uint(40), items[1].TargetWeight)
	assert.Equal(t, uint(10), items[2].TargetWeight)
}

func TestPlanChannelSmartScheduleRequiresConfiguredSamples(t *testing.T) {
	ratio := 1.0
	firstToken := 1000.0
	tps := 30.0
	plan := planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{
			ChannelId:             1,
			Ratio:                 &ratio,
			FirstTokenMs:          &firstToken,
			TPS:                   &tps,
			FirstTokenSampleCount: 5,
			TPSSampleCount:        5,
		},
		{
			ChannelId:             2,
			Ratio:                 &ratio,
			FirstTokenMs:          &firstToken,
			TPS:                   &tps,
			FirstTokenSampleCount: 4,
			TPSSampleCount:        5,
		},
	}, channelMonitorSmartScheduleStrategyFirstToken, false, channelMonitorSmartScheduleApplyWeight, 5, false)

	assert.Empty(t, plan.Items)
	assert.Equal(t, "同优先级可调渠道不足 2 个", plan.Skipped[1])
	assert.Equal(t, "首字样本不足（4/5）", plan.Skipped[2])
}

func TestPlanChannelSmartScheduleSmartUsesStabilityScoreWhenEnabled(t *testing.T) {
	ratioLow := 1.0
	ratioHigh := 2.0
	firstTokenFast := 300.0
	firstTokenSlow := 900.0
	tpsSlow := 10.0
	tpsFast := 30.0
	stabilityLower := 0.80
	stabilityHigher := 1.0
	scoring := defaultChannelSmartScheduleScoring()
	scoring.RelativeWeightEnabled = false
	plan := planChannelSmartScheduleWithScoring([]channelSmartScheduleCandidate{
		{
			ChannelId: 1, Ratio: &ratioLow,
			FirstTokenMs: &firstTokenFast, FirstTokenSampleCount: 5,
			TPS: &tpsSlow, TPSSampleCount: 5,
			Stability: &stabilityLower, StabilitySampleCount: 5, StabilityAvailable: true,
		},
		{
			ChannelId: 2, Ratio: &ratioHigh,
			FirstTokenMs: &firstTokenSlow, FirstTokenSampleCount: 5,
			TPS: &tpsFast, TPSSampleCount: 5,
			Stability: &stabilityHigher, StabilitySampleCount: 5, StabilityAvailable: true,
		},
	}, channelMonitorSmartScheduleStrategySmart, false, channelMonitorSmartScheduleApplyWeight, 5, false, scoring)

	require.Len(t, plan.Items, 2)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, uint(80), items[1].TargetWeight)
	assert.Equal(t, uint(30), items[2].TargetWeight)

	plan = planChannelSmartScheduleWithScoring([]channelSmartScheduleCandidate{
		{
			ChannelId: 1, Ratio: &ratioLow,
			FirstTokenMs: &firstTokenFast, FirstTokenSampleCount: 5,
			TPS: &tpsSlow, TPSSampleCount: 5,
			Stability: &stabilityLower, StabilitySampleCount: 5, StabilityAvailable: true,
		},
		{
			ChannelId: 2, Ratio: &ratioHigh,
			FirstTokenMs: &firstTokenSlow, FirstTokenSampleCount: 5,
			TPS: &tpsFast, TPSSampleCount: 5,
			Stability: &stabilityHigher, StabilitySampleCount: 5, StabilityAvailable: true,
		},
	}, channelMonitorSmartScheduleStrategySmart, true, channelMonitorSmartScheduleApplyWeight, 5, false, scoring)
	require.Len(t, plan.Items, 2)
	items = make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, uint(80), items[1].TargetWeight)
	assert.Equal(t, uint(65), items[2].TargetWeight)
}

func TestPlanChannelSmartScheduleUsesSelectedStrategyWithStabilityScore(t *testing.T) {
	ratio := 1.0
	stableRate := 0.99
	unstableRate := 0.80
	scoring := defaultChannelSmartScheduleScoring()
	scoring.RelativeWeightEnabled = false
	plan := planChannelSmartScheduleWithScoring([]channelSmartScheduleCandidate{
		{ChannelId: 1, Ratio: &ratio, Stability: &stableRate, StabilitySampleCount: 100, StabilityAvailable: true},
		{ChannelId: 2, Ratio: &ratio, Stability: &unstableRate, StabilitySampleCount: 100, StabilityAvailable: true},
	}, channelMonitorSmartScheduleStrategyRatio, true, channelMonitorSmartScheduleApplyWeight, 5, false, scoring)

	require.Len(t, plan.Items, 2)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.InDelta(t, 0.995, items[1].Score, 1e-9)
	assert.InDelta(t, 0.9, items[2].Score, 1e-9)
	assert.Equal(t, uint(100), items[1].TargetWeight)
	assert.Equal(t, uint(90), items[2].TargetWeight)

	plan = planChannelSmartScheduleWithScoring([]channelSmartScheduleCandidate{
		{ChannelId: 3, Ratio: &ratio},
		{ChannelId: 4, Ratio: &ratio},
	}, channelMonitorSmartScheduleStrategyRatio, true, channelMonitorSmartScheduleApplyWeight, 5, false, scoring)
	assert.Empty(t, plan.Items)
	assert.Equal(t, "稳定性统计不可用，请开启消费日志和 ERROR_LOG_ENABLED", plan.Skipped[3])
	assert.Equal(t, "稳定性统计不可用，请开启消费日志和 ERROR_LOG_ENABLED", plan.Skipped[4])

	plan = planChannelSmartScheduleWithScoring([]channelSmartScheduleCandidate{
		{ChannelId: 3, Ratio: &ratio},
		{ChannelId: 4, Ratio: &ratio},
	}, channelMonitorSmartScheduleStrategyRatio, false, channelMonitorSmartScheduleApplyWeight, 5, false, scoring)
	require.Len(t, plan.Items, 2)
	assert.Empty(t, plan.Skipped)
}

func TestPlanChannelSmartScheduleRelativeWeightStretching(t *testing.T) {
	tests := []struct {
		name             string
		enabled          bool
		startPercent     float64
		fullPercent      float64
		lowerScore       float64
		higherScore      float64
		wantLowerWeight  uint
		wantHigherWeight uint
	}{
		{
			name: "disabled keeps absolute score mapping", enabled: false,
			startPercent: 3, fullPercent: 10, lowerScore: 0.80, higherScore: 0.90,
			wantLowerWeight: 80, wantHigherWeight: 90,
		},
		{
			name: "spread below start keeps absolute score mapping", enabled: true,
			startPercent: 3, fullPercent: 10, lowerScore: 0.80, higherScore: 0.82,
			wantLowerWeight: 80, wantHigherWeight: 85,
		},
		{
			name: "spread between thresholds blends relative positions", enabled: true,
			startPercent: 3, fullPercent: 10, lowerScore: 0.80, higherScore: 0.85,
			wantLowerWeight: 60, wantHigherWeight: 90,
		},
		{
			name: "spread at full threshold uses the full weight range", enabled: true,
			startPercent: 3, fullPercent: 10, lowerScore: 0.80, higherScore: 0.90,
			wantLowerWeight: 10, wantHigherWeight: 100,
		},
		{
			name: "configurable thresholds can fully stretch close scores", enabled: true,
			startPercent: 0, fullPercent: 1, lowerScore: 0.80, higherScore: 0.81,
			wantLowerWeight: 10, wantHigherWeight: 100,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scoring := defaultChannelSmartScheduleScoring()
			scoring.StabilityPercent = 100
			scoring.RelativeWeightEnabled = test.enabled
			scoring.RelativeWeightStartPercent = test.startPercent
			scoring.RelativeWeightFullPercent = test.fullPercent
			plan := planChannelSmartScheduleWithScoring(
				[]channelSmartScheduleCandidate{
					{ChannelId: 1, Stability: &test.lowerScore, StabilitySampleCount: 5, StabilityAvailable: true},
					{ChannelId: 2, Stability: &test.higherScore, StabilitySampleCount: 5, StabilityAvailable: true},
				},
				channelMonitorSmartScheduleStrategyRatio,
				true,
				channelMonitorSmartScheduleApplyWeight,
				5,
				true,
				scoring,
			)

			require.Len(t, plan.Items, 2)
			items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
			for _, item := range plan.Items {
				items[item.ChannelId] = item
			}
			assert.Equal(t, test.wantLowerWeight, items[1].TargetWeight)
			assert.Equal(t, test.wantHigherWeight, items[2].TargetWeight)
		})
	}
}

func TestRunChannelSmartScheduleUsesExplorationBaselineForInsufficientSamples(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:   "true",
		channelMonitorSmartScheduleStrategyOption:  channelMonitorSmartScheduleStrategyFirstToken,
		channelMonitorSmartScheduleApplyModeOption: channelMonitorSmartScheduleApplyPriorityWeight,
		channelMonitorSmartScheduleSamplesOption:   "2",
	})
	priorityLow := int64(20)
	priorityHigh := int64(100)
	weight := uint(50)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 61, Name: "insufficient", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priorityLow, Weight: &weight},
		{Id: 62, Name: "measured", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priorityHigh, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 61, Group: "vip", Model: "model-a", Enabled: true, Priority: &priorityLow, Weight: weight},
		{ChannelId: 62, Group: "vip", Model: "model-a", Enabled: true, Priority: &priorityHigh, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 61, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 62, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)
	completedMinute := time.Now().Unix()
	completedMinute = completedMinute - completedMinute%60 - 60
	logTime := completedMinute + 1
	require.NoError(t, db.Create(&[]model.Log{
		{ChannelId: 61, Group: "vip", ModelName: "model-a", CreatedAt: logTime, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":500}`},
		{ChannelId: 62, Group: "vip", ModelName: "model-a", CreatedAt: logTime, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":100}`},
		{ChannelId: 62, Group: "vip", ModelName: "model-a", CreatedAt: logTime, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":100}`},
	}).Error)
	require.NoError(t, aggregateChannelMonitorTestLogs(completedMinute, completedMinute+60))

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Updated)

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 61, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(80), *ability.Priority)
	assert.Equal(t, uint(10), ability.Weight)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 61, "vip", "model-a",
	).First(&state).Error)
	assert.Contains(t, state.LastScheduleError, "使用探索基线")
}

func TestPlanChannelSmartScheduleForceResetRecalculatesPriorityAndWeight(t *testing.T) {
	ratioLow := 1.0
	ratioHigh := 3.0
	plan := planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{ChannelId: 1, CurrentPriority: 100, CurrentWeight: 90, Ratio: &ratioLow},
		{ChannelId: 2, CurrentPriority: 80, CurrentWeight: 90, Ratio: &ratioHigh},
	}, channelMonitorSmartScheduleStrategyRatio, false, channelMonitorSmartScheduleApplyWeight, 5, true)

	require.Len(t, plan.Items, 2)
	assert.Empty(t, plan.Skipped)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, int64(80), items[1].TargetPriority)
	assert.Equal(t, uint(100), items[1].TargetWeight)
	assert.Equal(t, int64(80), items[2].TargetPriority)
	assert.Equal(t, uint(10), items[2].TargetWeight)

	ratioMiddle := 2.0
	plan = planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{ChannelId: 1, CurrentPriority: 0, CurrentWeight: 10, Ratio: &ratioLow},
		{ChannelId: 2, CurrentPriority: 0, CurrentWeight: 10, Ratio: &ratioMiddle},
		{ChannelId: 3, CurrentPriority: 0, CurrentWeight: 100, Ratio: &ratioHigh},
	}, channelMonitorSmartScheduleStrategyRatio, false, channelMonitorSmartScheduleApplyPriorityWeight, 5, true)

	require.Len(t, plan.Items, 3)
	items = make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, int64(100), items[1].TargetPriority)
	assert.Equal(t, uint(100), items[1].TargetWeight)
	assert.Equal(t, int64(90), items[2].TargetPriority)
	assert.Equal(t, uint(55), items[2].TargetWeight)
	assert.Equal(t, int64(80), items[3].TargetPriority)
	assert.Equal(t, uint(10), items[3].TargetWeight)
}
