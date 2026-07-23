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

func TestChannelSmartSchedulePreferredModelUsesConfiguredOrder(t *testing.T) {
	assert.Equal(t, "model-b", channelSmartSchedulePreferredModel(
		[]string{"model-a", " model-b "},
		[]string{"model-c", "model-b", "model-a"},
	))
	assert.Empty(t, channelSmartSchedulePreferredModel(
		[]string{"model-a"},
		[]string{"model-b", "model-c"},
	))
}

func TestRunChannelSmartScheduleUsesFirstSupportedModelPerChannel(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:   "true",
		channelMonitorSmartScheduleStrategyOption:  channelMonitorSmartScheduleStrategyFirstToken,
		channelMonitorSmartScheduleApplyModeOption: channelMonitorSmartScheduleApplyWeight,
		channelMonitorSmartScheduleModelsOption:    `["model-b","model-a"]`,
		channelMonitorSmartScheduleSamplesOption:   "1",
	})
	priority := int64(0)
	weight := uint(50)
	channels := []model.Channel{
		{Id: 101, Name: "supports both", Group: "vip", Models: "model-a,model-b", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 102, Name: "fallback", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 103, Name: "unsupported", Group: "vip", Models: "model-c", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}
	require.NoError(t, db.Create(&channels).Error)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&[]model.Log{
		{ChannelId: 101, ModelName: "model-a", CreatedAt: now, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":100}`},
		{ChannelId: 101, ModelName: "model-b", CreatedAt: now, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":1000}`},
		{ChannelId: 102, ModelName: "model-a", CreatedAt: now, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":100}`},
	}).Error)

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"model-b", "model-a"}, result.Models)
	assert.Equal(t, "model-b", result.Model)
	assert.Equal(t, 2, result.Updated)
	assert.Equal(t, 1, result.Skipped)

	first, err := model.GetChannelById(101, false)
	require.NoError(t, err)
	second, err := model.GetChannelById(102, false)
	require.NoError(t, err)
	assert.Equal(t, 20, first.GetWeight())
	assert.Equal(t, 80, second.GetWeight())

	unsupportedMonitor, err := model.GetChannelRatioMonitor(103)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelSmartScheduleStatusSkipped, unsupportedMonitor.LastScheduleStatus)
	assert.Equal(t, "渠道不支持已配置的基准模型", unsupportedMonitor.LastScheduleError)
}

func TestRunChannelSmartScheduleUsesConvertedCostRatioAcrossGroups(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:   "true",
		channelMonitorSmartScheduleStrategyOption:  channelMonitorSmartScheduleStrategyRatio,
		channelMonitorSmartScheduleApplyModeOption: channelMonitorSmartScheduleApplyWeight,
	})
	priority := int64(0)
	weight := uint(50)
	channels := []model.Channel{
		{Id: 1, Name: "cheap raw", Group: "vip", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 2, Name: "cheap cost", Group: "standard", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{
			ChannelId: 1, Ratio: 0.5, UpdatedTime: 1,
			CostConversion: `{"mode":"recharge","paid_cny":400,"credited_usd":100}`,
		},
		{ChannelId: 2, Ratio: 1, UpdatedTime: 1},
	}).Error)

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Updated)

	first, err := model.GetChannelById(1, false)
	require.NoError(t, err)
	second, err := model.GetChannelById(2, false)
	require.NoError(t, err)
	assert.Equal(t, 20, first.GetWeight())
	assert.Equal(t, 80, second.GetWeight())

	firstMonitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	secondMonitor, err := model.GetChannelRatioMonitor(2)
	require.NoError(t, err)
	require.NotNil(t, firstMonitor.LastScheduleScore)
	require.NotNil(t, secondMonitor.LastScheduleScore)
	assert.InDelta(t, 0, *firstMonitor.LastScheduleScore, 1e-9)
	assert.InDelta(t, 1, *secondMonitor.LastScheduleScore, 1e-9)
}

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
	assert.Equal(t, 1, result.Updated)
	assert.Equal(t, 1, result.Unchanged)
	assert.Equal(t, 2, result.Skipped)

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
		var channel model.Channel
		require.NoError(t, db.First(&channel, "id = ?", channelId).Error)
		assert.Equal(t, target.priority, channel.GetPriority())
		assert.Equal(t, int(target.weight), channel.GetWeight())

		var ability model.Ability
		require.NoError(t, db.First(&ability, "channel_id = ?", channelId).Error)
		require.NotNil(t, ability.Priority)
		assert.Equal(t, target.priority, *ability.Priority)
		assert.Equal(t, target.weight, ability.Weight)
	}

	monitor, err := model.GetChannelRatioMonitor(13)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelSmartScheduleStatusSkipped, monitor.LastScheduleStatus)
	assert.Equal(t, int64(80), monitor.LastSchedulePriority)
	assert.Equal(t, uint(10), monitor.LastScheduleWeight)
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

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, true)
	require.NoError(t, err)
	assert.Zero(t, result.Updated)
	assert.Equal(t, 2, result.Skipped)

	for channelId, expected := range map[int]struct {
		priority int64
		weight   int
	}{
		21: {priority: 80, weight: 10},
		22: {priority: 80, weight: 10},
	} {
		var channel model.Channel
		require.NoError(t, db.First(&channel, "id = ?", channelId).Error)
		assert.Equal(t, expected.priority, channel.GetPriority())
		assert.Equal(t, expected.weight, channel.GetWeight())
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
	initialLogTime := time.Now().Unix() - 10
	require.NoError(t, db.Create(&[]model.Log{
		{ChannelId: 31, ModelName: "model-a", CreatedAt: initialLogTime, Type: model.LogTypeError},
		{ChannelId: 31, ModelName: "model-a", CreatedAt: initialLogTime, Type: model.LogTypeError},
		{ChannelId: 32, ModelName: "model-a", CreatedAt: initialLogTime, Type: model.LogTypeConsume},
		{ChannelId: 32, ModelName: "model-a", CreatedAt: initialLogTime, Type: model.LogTypeConsume},
	}).Error)

	_, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	channel, err := model.GetChannelById(31, false)
	require.NoError(t, err)
	assert.Zero(t, channel.GetPriority())
	assert.Zero(t, channel.GetWeight())
	monitor, err := model.GetChannelRatioMonitor(31)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, monitor.SmartScheduleStabilityState)
	assert.Equal(t, int64(90), monitor.SmartScheduleSavedPriority)
	assert.Equal(t, uint(35), monitor.SmartScheduleSavedWeight)

	require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).
		Where("channel_id = ?", 31).
		Update("smart_schedule_stability_until", time.Now().Unix()-1).Error)
	_, err = runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	channel, err = model.GetChannelById(31, false)
	require.NoError(t, err)
	assert.Equal(t, int64(90), channel.GetPriority())
	assert.Equal(t, 35, channel.GetWeight())
	monitor, err = model.GetChannelRatioMonitor(31)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelSmartScheduleStabilityProbing, monitor.SmartScheduleStabilityState)
	require.Positive(t, monitor.SmartScheduleStabilitySince)

	oldSuccesses := make([]model.Log, 20)
	for index := range oldSuccesses {
		oldSuccesses[index] = model.Log{
			ChannelId: 31, ModelName: "model-a", CreatedAt: monitor.SmartScheduleStabilitySince - 1, Type: model.LogTypeConsume,
		}
	}
	require.NoError(t, db.Create(&oldSuccesses).Error)
	require.NoError(t, db.Create(&[]model.Log{
		{ChannelId: 31, ModelName: "model-a", CreatedAt: monitor.SmartScheduleStabilitySince, Type: model.LogTypeError},
		{ChannelId: 31, ModelName: "model-a", CreatedAt: monitor.SmartScheduleStabilitySince, Type: model.LogTypeError},
	}).Error)

	_, err = runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	channel, err = model.GetChannelById(31, false)
	require.NoError(t, err)
	assert.Zero(t, channel.GetPriority())
	assert.Zero(t, channel.GetWeight())
	monitor, err = model.GetChannelRatioMonitor(31)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, monitor.SmartScheduleStabilityState)
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
	weight := uint(30)
	probeStartedAt := time.Now().Unix() - 10
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 33, Name: "recovering", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 34, Name: "stable", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{
			ChannelId: 33, Ratio: 2, UpdatedTime: 1,
			SmartScheduleStabilityState: model.ChannelSmartScheduleStabilityProbing,
			SmartScheduleStabilitySince: probeStartedAt,
			SmartScheduleSavedPriority:  80,
			SmartScheduleSavedWeight:    30,
		},
		{ChannelId: 34, Ratio: 1, UpdatedTime: 1},
	}).Error)
	require.NoError(t, db.Create(&[]model.Log{
		{ChannelId: 33, ModelName: "model-a", CreatedAt: probeStartedAt, Type: model.LogTypeConsume},
		{ChannelId: 33, ModelName: "model-a", CreatedAt: probeStartedAt, Type: model.LogTypeConsume},
		{ChannelId: 34, ModelName: "model-a", CreatedAt: probeStartedAt, Type: model.LogTypeConsume},
		{ChannelId: 34, ModelName: "model-a", CreatedAt: probeStartedAt, Type: model.LogTypeConsume},
	}).Error)

	_, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	monitor, err := model.GetChannelRatioMonitor(33)
	require.NoError(t, err)
	assert.Empty(t, monitor.SmartScheduleStabilityState)
	assert.Equal(t, probeStartedAt, monitor.SmartScheduleStabilitySince)
	channel, err := model.GetChannelById(33, false)
	require.NoError(t, err)
	assert.NotZero(t, channel.GetPriority())
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
	assert.Equal(t, uint(20), items[2].TargetWeight)
	assert.Equal(t, int64(80), items[3].TargetPriority)
	assert.Equal(t, uint(20), items[3].TargetWeight)
}

func TestPlanChannelSmartScheduleSingleMetricBiasesBestChannel(t *testing.T) {
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
			assert.Equal(t, uint(20), items[2].TargetWeight)
			assert.Equal(t, uint(10), items[3].TargetWeight)
			assert.InDelta(t, 0.5, items[2].Score, 1e-9)
		})
	}
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

func TestPlanChannelSmartScheduleSmartIgnoresStabilityScore(t *testing.T) {
	ratioLow := 1.0
	ratioHigh := 2.0
	firstTokenFast := 300.0
	firstTokenSlow := 900.0
	tpsSlow := 10.0
	tpsFast := 30.0
	stabilityLower := 0.80
	stabilityHigher := 1.0
	plan := planChannelSmartSchedule([]channelSmartScheduleCandidate{
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
	}, channelMonitorSmartScheduleStrategySmart, false, channelMonitorSmartScheduleApplyWeight, 5, false)

	require.Len(t, plan.Items, 2)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, uint(70), items[1].TargetWeight)
	assert.Equal(t, uint(40), items[2].TargetWeight)

	plan = planChannelSmartSchedule([]channelSmartScheduleCandidate{
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
	}, channelMonitorSmartScheduleStrategySmart, true, channelMonitorSmartScheduleApplyWeight, 5, false)
	require.Len(t, plan.Items, 2)
	items = make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, uint(70), items[1].TargetWeight)
	assert.Equal(t, uint(40), items[2].TargetWeight)
}

func TestPlanChannelSmartScheduleUsesSelectedStrategyWithoutStabilityScore(t *testing.T) {
	ratio := 1.0
	stableRate := 0.99
	unstableRate := 0.80
	plan := planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{ChannelId: 1, Ratio: &ratio, Stability: &stableRate, StabilitySampleCount: 100, StabilityAvailable: true},
		{ChannelId: 2, Ratio: &ratio, Stability: &unstableRate, StabilitySampleCount: 100, StabilityAvailable: true},
	}, channelMonitorSmartScheduleStrategyRatio, true, channelMonitorSmartScheduleApplyWeight, 5, false)

	require.Len(t, plan.Items, 2)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, uint(100), items[1].TargetWeight)
	assert.Equal(t, uint(100), items[2].TargetWeight)

	plan = planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{ChannelId: 3, Ratio: &ratio},
		{ChannelId: 4, Ratio: &ratio},
	}, channelMonitorSmartScheduleStrategyRatio, true, channelMonitorSmartScheduleApplyWeight, 5, false)
	assert.Empty(t, plan.Items)
	assert.Equal(t, "稳定性统计不可用，请开启消费日志和 ERROR_LOG_ENABLED", plan.Skipped[3])
	assert.Equal(t, "稳定性统计不可用，请开启消费日志和 ERROR_LOG_ENABLED", plan.Skipped[4])

	plan = planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{ChannelId: 3, Ratio: &ratio},
		{ChannelId: 4, Ratio: &ratio},
	}, channelMonitorSmartScheduleStrategyRatio, false, channelMonitorSmartScheduleApplyWeight, 5, false)
	require.Len(t, plan.Items, 2)
	assert.Empty(t, plan.Skipped)
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
	now := time.Now().Unix()
	require.NoError(t, db.Create(&[]model.Log{
		{ChannelId: 61, ModelName: "model-a", CreatedAt: now, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":500}`},
		{ChannelId: 62, ModelName: "model-a", CreatedAt: now, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":100}`},
		{ChannelId: 62, ModelName: "model-a", CreatedAt: now, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":100}`},
	}).Error)

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Updated)

	channel, err := model.GetChannelById(61, false)
	require.NoError(t, err)
	assert.Equal(t, int64(80), channel.GetPriority())
	assert.Equal(t, 10, channel.GetWeight())
	monitor, err := model.GetChannelRatioMonitor(61)
	require.NoError(t, err)
	assert.Contains(t, monitor.LastScheduleError, "使用探索基线")
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
	assert.Equal(t, uint(20), items[2].TargetWeight)
	assert.Equal(t, int64(80), items[3].TargetPriority)
	assert.Equal(t, uint(10), items[3].TargetWeight)
}
