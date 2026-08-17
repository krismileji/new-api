package controller

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelSmartScheduleRemainingAdaptiveWindowRequestsPreventsCombinedWindowOverflow(t *testing.T) {
	tests := []struct {
		name     string
		limit    int
		used     int64
		expected int
	}{
		{name: "empty production window", limit: 100, used: 0, expected: 100},
		{name: "partially filled production window", limit: 100, used: 40, expected: 60},
		{name: "production window exactly full", limit: 100, used: 100, expected: 0},
		{name: "production window over limit", limit: 100, used: 101, expected: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, channelSmartScheduleRemainingAdaptiveWindowRequests(
				test.limit, test.used,
			))
		})
	}
}

func TestChannelSmartScheduleRealtimeMetricCoverageIncludesTruncatedRouteWindow(t *testing.T) {
	generatedAt := common.GetTimestamp() + 2*60*60
	stabilityWindowMinutes := 60
	settings := channelMonitorSettings{
		SmartSchedulePerformanceWindowMinutes: 60,
		SmartScheduleGroupPolicies: smartScheduleGroupPolicies{{
			StabilityWindowMinutes: &stabilityWindowMinutes,
		}},
		SmartScheduleRealtimeRetentionMinutes: 60,
		SmartScheduleRealtimeSampleLimit:      1000,
	}
	coverageStart := generatedAt - 30*60
	coverage := channelSmartScheduleRealtimeMetricCoverage(
		generatedAt,
		settings,
		[]service.ChannelMonitorRedisRouteHealthSnapshot{{
			CoverageStart: coverageStart,
			DataCutoffAt:  generatedAt - 1,
		}},
	)

	assert.Equal(t, coverageStart, coverage.AggregatedFrom)
	assert.Equal(t, generatedAt-1, coverage.AggregatedThrough)
	assert.False(t, coverage.PerformanceWindowComplete)
	assert.False(t, coverage.StabilityWindowComplete)
	assert.Equal(t, 60, coverage.RealtimeRetentionMinutes)
	assert.Equal(t, 1000, coverage.RealtimeSampleLimit)
}

func TestRunChannelSmartScheduleRecordsRouteAdjustmentsAndReasons(t *testing.T) {
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
	priority := int64(100)
	weight := uint(90)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1101, Name: "cheap", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 1102, Name: "expensive", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 1103, Name: "excluded", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1101, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1102, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1103, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 1101, Ratio: 1, UpdatedTime: 1},
		{ChannelId: 1102, Ratio: 3, UpdatedTime: 1},
		{ChannelId: 1103, Ratio: 2, UpdatedTime: 1},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 1101, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 1102, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 1103, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Excluded: true},
	}).Error)

	firstResult, err := runChannelSmartScheduleOnce(context.Background(), nil, true)
	require.NoError(t, err)
	require.Len(t, firstResult.Adjustments, 3)
	firstByChannel := make(map[int]channelSmartScheduleTaskAdjustment, len(firstResult.Adjustments))
	for _, adjustment := range firstResult.Adjustments {
		firstByChannel[adjustment.ChannelId] = adjustment
	}

	cheap := firstByChannel[1101]
	assert.Equal(t, channelSmartScheduleAdjustmentUpdated, cheap.Action)
	assert.Equal(t, int64(100), cheap.OldPriority)
	assert.Equal(t, int64(80), cheap.NewPriority)
	assert.Equal(t, uint(90), cheap.OldWeight)
	assert.Equal(t, uint(900), cheap.NewWeight)
	require.NotNil(t, cheap.Score)
	assert.Contains(t, cheap.Reason, "根据智能调度评分")
	assert.Contains(t, cheap.Reason, "调整优先级和权重")

	excluded := firstByChannel[1103]
	assert.Equal(t, channelSmartScheduleAdjustmentSkipped, excluded.Action)
	assert.Equal(t, excluded.OldPriority, excluded.NewPriority)
	assert.Equal(t, excluded.OldWeight, excluded.NewWeight)
	assert.Nil(t, excluded.Score)
	assert.Equal(t, "该分组和模型路由未参与智能调度", excluded.Reason)

	secondResult, err := runChannelSmartScheduleOnce(context.Background(), nil, true)
	require.NoError(t, err)
	require.Len(t, secondResult.Adjustments, 3)
	secondByChannel := make(map[int]channelSmartScheduleTaskAdjustment, len(secondResult.Adjustments))
	for _, adjustment := range secondResult.Adjustments {
		secondByChannel[adjustment.ChannelId] = adjustment
	}
	assert.Equal(t, channelSmartScheduleAdjustmentUnchanged, secondByChannel[1101].Action)
	assert.Contains(t, secondByChannel[1101].Reason, "无需调整")
	assert.Equal(t, channelSmartScheduleAdjustmentUnchanged, secondByChannel[1102].Action)
}

func TestRunChannelSmartScheduleRecordsUnchangedWhenOnlyBaseSnapshotRefreshes(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 5, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})
	priority := int64(100)
	weight := uint(90)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1105, Name: "cheap", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 1106, Name: "expensive", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1105, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1106, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 1105, Ratio: 1, UpdatedTime: 1},
		{ChannelId: 1106, Ratio: 2, UpdatedTime: 1},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 1105, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 1106, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)

	firstResult, err := runChannelSmartScheduleOnce(context.Background(), nil, true)
	require.NoError(t, err)
	assert.Equal(t, 2, firstResult.Updated)

	secondResult, err := runChannelSmartScheduleOnce(context.Background(), nil, true)
	require.NoError(t, err)
	assert.Zero(t, secondResult.Updated)
	assert.Equal(t, 2, secondResult.Unchanged)
	require.Len(t, secondResult.Adjustments, 2)
	for _, adjustment := range secondResult.Adjustments {
		assert.Equal(t, channelSmartScheduleAdjustmentUnchanged, adjustment.Action)
		assert.Contains(t, adjustment.Reason, "无需调整")
	}
}

func TestChannelSmartScheduleRouteResultChangesTrafficState(t *testing.T) {
	state := model.ChannelSmartScheduleRouteState{
		StabilityState:                  model.ChannelSmartScheduleStabilityDegraded,
		StabilityUntil:                  200,
		StabilitySince:                  100,
		StabilitySavedPriority:          8,
		StabilitySavedWeight:            900,
		RuntimeProtectionUntil:          300,
		BaseRank:                        2,
		BasePriority:                    7,
		BaseWeight:                      800,
		TemporaryTrafficKind:            model.ChannelSmartScheduleTemporaryTrafficExploration,
		TemporaryTrafficSince:           150,
		TemporaryTrafficTargetPercent:   3,
		StabilityReleaseMaxPromptTokens: 1234,
	}
	matchingStability := &model.ChannelSmartScheduleStabilityUpdate{
		State: model.ChannelSmartScheduleStabilityDegraded, Until: 200, Since: 100,
		SavedPriority: 8, SavedWeight: 900,
	}
	matchingSnapshot := &model.ChannelSmartScheduleRoutingSnapshotUpdate{
		BaseRank: 2, BasePriority: 7, BaseWeight: 800,
		TemporaryTrafficKind:  model.ChannelSmartScheduleTemporaryTrafficExploration,
		TemporaryTrafficSince: 150, TemporaryTrafficTargetPercent: 3,
		StabilityReleaseMaxPromptTokens: 1234,
	}

	assert.False(t, channelSmartScheduleRouteResultChangesTrafficState(state, model.ChannelSmartScheduleRouteResultUpdate{
		Stability: matchingStability, RoutingSnapshot: matchingSnapshot,
	}))
	assert.False(t, channelSmartScheduleRouteResultChangesTrafficState(state, model.ChannelSmartScheduleRouteResultUpdate{
		Stability: &model.ChannelSmartScheduleStabilityUpdate{
			State: model.ChannelSmartScheduleStabilityDegraded, Until: 200, Since: 50,
			SavedPriority: 7, SavedWeight: 800,
		},
	}))
	assert.True(t, channelSmartScheduleRouteResultChangesTrafficState(state, model.ChannelSmartScheduleRouteResultUpdate{
		Stability: &model.ChannelSmartScheduleStabilityUpdate{
			State: model.ChannelSmartScheduleStabilityProbing, Since: 100,
			SavedPriority: 8, SavedWeight: 900,
		},
	}))
	assert.True(t, channelSmartScheduleRouteResultChangesTrafficState(state, model.ChannelSmartScheduleRouteResultUpdate{
		RoutingSnapshot: &model.ChannelSmartScheduleRoutingSnapshotUpdate{
			BaseRank: 1, BasePriority: 6, BaseWeight: 700,
		},
	}))
	runtimeProtectionUntil := int64(0)
	assert.True(t, channelSmartScheduleRouteResultChangesTrafficState(state, model.ChannelSmartScheduleRouteResultUpdate{
		RuntimeProtectionUntil: &runtimeProtectionUntil,
	}))
}

func TestRunChannelSmartScheduleRecordsActiveRuntimeProtection(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 5, 80, 30,
			),
		),
	})
	priority := int64(90)
	weight := uint(80)
	protectionUntil := common.GetTimestamp() + 120
	require.NoError(t, db.Create(&model.Channel{
		Id: 1104, Name: "protected", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1104, Ratio: 1, UpdatedTime: 1,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1104, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1104, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		RuntimeProtectionUntil: protectionUntil,
	}).Error)

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	require.Len(t, result.Adjustments, 1)
	adjustment := result.Adjustments[0]
	assert.Equal(t, channelSmartScheduleAdjustmentSkipped, adjustment.Action)
	assert.Equal(t, priority, adjustment.OldPriority)
	assert.Equal(t, priority, adjustment.NewPriority)
	assert.Equal(t, weight, adjustment.OldWeight)
	assert.Equal(t, weight, adjustment.NewWeight)
	assert.Contains(t, adjustment.Reason, "运行时稳定性保护中")
	assert.Contains(t, adjustment.Reason, "保护至")
}

func TestRunChannelSmartScheduleRecordsPerRouteApplyFailures(t *testing.T) {
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
		{Id: 1111, Name: "first", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 1112, Name: "second", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1111, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1112, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 1111, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{ChannelId: 1112, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 1111, Ratio: 1, UpdatedTime: 1},
		{ChannelId: 1112, Ratio: 3, UpdatedTime: 1},
	}).Error)

	forcedError := errors.New("模拟路由写入失败")
	const callbackName = "test:fail_smart_schedule_route_update"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "abilities" {
			tx.AddError(forcedError)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Update().Remove(callbackName))
	})

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.ErrorContains(t, err, "失败池已保留上一轮结果")
	assert.Equal(t, 2, result.Failed)
	require.Len(t, result.Adjustments, 2)
	for _, adjustment := range result.Adjustments {
		assert.Equal(t, channelSmartScheduleAdjustmentFailed, adjustment.Action)
		assert.Equal(t, forcedError.Error(), adjustment.Reason)
		assert.Equal(t, "vip", adjustment.Group)
		assert.Equal(t, "model-a", adjustment.Model)
	}
	var preservedAbilities []model.Ability
	require.NoError(t, db.Order("channel_id ASC").Find(&preservedAbilities).Error)
	require.Len(t, preservedAbilities, 2)
	for _, ability := range preservedAbilities {
		require.NotNil(t, ability.Priority)
		assert.Equal(t, priority, *ability.Priority)
		assert.Equal(t, weight, ability.Weight)
	}
}

func TestRunChannelSmartScheduleKeepsSuccessfulPoolWhenLaterPoolFails(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			channelSmartScheduleTestGroupPolicy(
				"gold", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 1, 80, 30,
			),
			channelSmartScheduleTestGroupPolicy(
				"silver", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 1, 80, 30,
			),
		),
	})
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1113, Name: "gold channel", Group: "gold", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 1114, Name: "silver channel", Group: "silver", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1113, Group: "gold", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1114, Group: "silver", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 1113, GroupName: "gold", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{ChannelId: 1114, GroupName: "silver", ModelName: "model-a", ParticipationSet: true, Revision: 1},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 1113, Ratio: 1, UpdatedTime: 1},
		{ChannelId: 1114, Ratio: 1, UpdatedTime: 1},
	}).Error)

	forcedError := errors.New("模拟第二个调度池写入失败")
	const callbackName = "test:fail_second_smart_schedule_pool"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "channel_smart_schedule_route_states" {
			return
		}
		state, ok := tx.Statement.Dest.(*model.ChannelSmartScheduleRouteState)
		if ok && state.GroupName == "silver" {
			tx.AddError(forcedError)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Update().Remove(callbackName))
	})

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.ErrorContains(t, err, "失败池已保留上一轮结果")
	assert.Equal(t, 1, result.Updated)
	assert.Equal(t, 1, result.Failed)
	require.Len(t, result.Adjustments, 2)

	adjustmentByGroup := make(map[string]channelSmartScheduleTaskAdjustment, 2)
	for _, adjustment := range result.Adjustments {
		adjustmentByGroup[adjustment.Group] = adjustment
	}
	assert.Equal(t, channelSmartScheduleAdjustmentUpdated, adjustmentByGroup["gold"].Action)
	assert.Equal(t, channelSmartScheduleAdjustmentFailed, adjustmentByGroup["silver"].Action)
	assert.Equal(t, "write", adjustmentByGroup["silver"].FailureStage)
	assert.Equal(t, forcedError.Error(), adjustmentByGroup["silver"].Reason)

	var abilities []model.Ability
	require.NoError(t, db.Order("channel_id ASC").Find(&abilities).Error)
	require.Len(t, abilities, 2)
	require.NotNil(t, abilities[0].Priority)
	assert.Equal(t, int64(1), *abilities[0].Priority)
	assert.Equal(t, uint(1000), abilities[0].Weight)
	require.NotNil(t, abilities[1].Priority)
	assert.Equal(t, priority, *abilities[1].Priority)
	assert.Equal(t, weight, abilities[1].Weight)

	var states []model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Order("channel_id ASC").Find(&states).Error)
	require.Len(t, states, 2)
	assert.Positive(t, states[0].LastScheduleTime)
	assert.Zero(t, states[1].LastScheduleTime)
}

func TestChannelSmartScheduleTaskResultKeepsAllPersistedAdjustmentDetails(t *testing.T) {
	result := channelSmartScheduleTaskResult{}
	for channelId := 1; channelId <= 500; channelId++ {
		result.recordAdjustment(channelSmartScheduleTaskAdjustment{
			ChannelId: channelId, Action: channelSmartScheduleAdjustmentUnchanged,
		})
	}
	result.recordAdjustment(channelSmartScheduleTaskAdjustment{
		ChannelId: 9999, Action: channelSmartScheduleAdjustmentFailed,
		Reason: "需要优先保留的失败原因",
	})

	result.finalizeAdjustments()

	require.Len(t, result.Adjustments, 501)
	assert.Equal(t, channelSmartScheduleAdjustmentFailed, result.Adjustments[0].Action)
	assert.Equal(t, 9999, result.Adjustments[0].ChannelId)
	assert.Equal(t, 500, result.Adjustments[len(result.Adjustments)-1].ChannelId)
}

func TestRunChannelSmartScheduleByRouteIsolatesGroupModelPools(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelSmartScheduleRouteState{}))
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			channelSmartScheduleTestGroupPolicy(
				"gold", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 5, 80, 30,
			),
			channelSmartScheduleTestGroupPolicy(
				"silver", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 5, 80, 30,
			),
		),
	})
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1201, Name: "shared", Group: "gold,silver", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 1202, Name: "gold expensive", Group: "gold", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 1203, Name: "silver cheap", Group: "silver", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1201, Group: "gold", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1201, Group: "silver", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1202, Group: "gold", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1203, Group: "silver", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 1201, Ratio: 1.5, UpdatedTime: 1},
		{ChannelId: 1202, Ratio: 3, UpdatedTime: 1},
		{ChannelId: 1203, Ratio: 1, UpdatedTime: 1},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 1201, GroupName: "gold", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 1201, GroupName: "silver", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 1202, GroupName: "gold", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 1203, GroupName: "silver", ModelName: "model-a", ParticipationSet: true},
	}).Error)

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	assert.Equal(t, 4, result.Total)
	assert.Zero(t, result.Failed)

	type routeKey struct {
		channelId int
		group     string
	}
	abilityByRoute := make(map[routeKey]model.Ability)
	var abilities []model.Ability
	require.NoError(t, db.Find(&abilities).Error)
	for _, ability := range abilities {
		abilityByRoute[routeKey{channelId: ability.ChannelId, group: ability.Group}] = ability
	}
	sharedGold := abilityByRoute[routeKey{channelId: 1201, group: "gold"}]
	goldCompetitor := abilityByRoute[routeKey{channelId: 1202, group: "gold"}]
	sharedSilver := abilityByRoute[routeKey{channelId: 1201, group: "silver"}]
	silverCompetitor := abilityByRoute[routeKey{channelId: 1203, group: "silver"}]
	assert.Greater(t, sharedGold.Weight, goldCompetitor.Weight)
	assert.Less(t, sharedSilver.Weight, silverCompetitor.Weight)
	assert.NotEqual(t, sharedGold.Weight, sharedSilver.Weight)

	var sharedChannel model.Channel
	require.NoError(t, db.First(&sharedChannel, 1201).Error)
	assert.Equal(t, int64(80), sharedChannel.GetPriority())
	assert.Equal(t, 50, sharedChannel.GetWeight())
}

func TestRunChannelSmartScheduleByRouteUsesOnlyExplicitGroupPolicies(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	goldPolicy := channelSmartScheduleTestGroupPolicy(
		"gold", channelMonitorSmartScheduleStrategyTPS, false,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-b"}, 1, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, goldPolicy),
	})
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1301, Name: "gold fast expensive", Group: "gold", Models: "model-a,model-b", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 1302, Name: "gold slow cheap", Group: "gold", Models: "model-a,model-b", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 1303, Name: "silver expensive", Group: "silver", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 1304, Name: "silver cheap", Group: "silver", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1301, Group: "gold", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1301, Group: "gold", Model: "model-b", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1302, Group: "gold", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1302, Group: "gold", Model: "model-b", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1303, Group: "silver", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1304, Group: "silver", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 1301, Ratio: 3, UpdatedTime: 1},
		{ChannelId: 1302, Ratio: 1, UpdatedTime: 1},
		{ChannelId: 1303, Ratio: 3, UpdatedTime: 1},
		{ChannelId: 1304, Ratio: 1, UpdatedTime: 1},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 1301, GroupName: "gold", ModelName: "model-b", ParticipationSet: true},
		{ChannelId: 1302, GroupName: "gold", ModelName: "model-b", ParticipationSet: true},
	}).Error)
	minuteStart := common.GetTimestamp()
	minuteStart = minuteStart - minuteStart%60 - 60
	require.NoError(t, db.Create(&[]model.ChannelMonitorMinuteRouteMetric{
		{
			MinuteStart: minuteStart, ChannelId: 1301, ModelKey: "model-b", GroupKey: "gold",
			APIKeyKey: "all", ModelName: "model-b", GroupName: "gold",
			SampleCount: 1, TPSSampleCount: 1, TPSTotal: 100, LastUsedTime: minuteStart,
		},
		{
			MinuteStart: minuteStart, ChannelId: 1302, ModelKey: "model-b", GroupKey: "gold",
			APIKeyKey: "all", ModelName: "model-b", GroupName: "gold",
			SampleCount: 1, TPSSampleCount: 1, TPSTotal: 10, LastUsedTime: minuteStart,
		},
	}).Error)

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Total)
	assert.Zero(t, result.Failed)

	type routeKey struct {
		channelId int
		model     string
	}
	abilityByRoute := make(map[routeKey]model.Ability)
	var abilities []model.Ability
	require.NoError(t, db.Find(&abilities).Error)
	for _, ability := range abilities {
		abilityByRoute[routeKey{channelId: ability.ChannelId, model: ability.Model}] = ability
	}
	goldFast := abilityByRoute[routeKey{channelId: 1301, model: "model-b"}]
	goldSlow := abilityByRoute[routeKey{channelId: 1302, model: "model-b"}]
	assert.Equal(t, int64(2), *goldFast.Priority)
	assert.Equal(t, int64(1), *goldSlow.Priority)
	assert.Equal(t, goldFast.Weight, goldSlow.Weight)
	unconfiguredFast := abilityByRoute[routeKey{channelId: 1301, model: "model-a"}]
	unconfiguredSlow := abilityByRoute[routeKey{channelId: 1302, model: "model-a"}]
	assert.Nil(t, unconfiguredFast.Priority)
	assert.Zero(t, unconfiguredFast.Weight)
	assert.Nil(t, unconfiguredSlow.Priority)
	assert.Zero(t, unconfiguredSlow.Weight)

	silverExpensive := abilityByRoute[routeKey{channelId: 1303, model: "model-a"}]
	silverCheap := abilityByRoute[routeKey{channelId: 1304, model: "model-a"}]
	assert.Nil(t, silverExpensive.Priority)
	assert.Zero(t, silverExpensive.Weight)
	assert.Nil(t, silverCheap.Priority)
	assert.Zero(t, silverCheap.Weight)
}

func TestGetChannelMonitorSmartScheduleRoutesUsesSharedStabilityWithoutLogs(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	common.LogConsumeEnabled = false
	constant.ErrorLogEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 2, 90, 30,
	)
	probeMode := channelMonitorSmartScheduleSampleProbe
	policy.SampleMode = &probeMode
	policy.Scoring.StabilityPercent = 100
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t, policy,
		),
	})
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1305, Name: "probe only", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1305, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1305, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)
	now := common.GetTimestamp()
	_, err := saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
		ChannelId: 1305, Model: "model-a",
		WindowStart: now - 3600, Time: now - 60, Success: true,
	})
	require.NoError(t, err)
	fastFailureDurationMs := 500.0
	_, err = saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
		ChannelId: 1305, Model: "model-a",
		WindowStart: now - 3600, Time: now - 30, Success: false,
		DurationMs: &fastFailureDurationMs,
	})
	require.NoError(t, err)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/schedule", nil,
	)
	expectedSnapshotMetrics := model.GetChannelSmartScheduleExecutionDetailMetrics()
	GetChannelMonitorSmartScheduleRoutes(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			StabilityMetricsAvailable bool                                              `json:"stability_metrics_available"`
			StabilityItems            []model.ChannelMonitorRouteStabilityMetric        `json:"stability_items"`
			ExecutionSnapshotMetrics  *model.ChannelSmartScheduleExecutionDetailMetrics `json:"execution_snapshot_metrics"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	require.NotNil(t, response.Data.ExecutionSnapshotMetrics)
	assert.Equal(t, expectedSnapshotMetrics, *response.Data.ExecutionSnapshotMetrics)
	assert.True(t, response.Data.StabilityMetricsAvailable)
	require.Len(t, response.Data.StabilityItems, 1)
	metric := response.Data.StabilityItems[0]
	assert.Equal(t, int64(2), metric.SampleCount)
	assert.Equal(t, int64(1), metric.SuccessCount)
	assert.Equal(t, int64(1), metric.RetryFailureCount)
	assert.Zero(t, metric.FinalFailureCount)
	assert.InDelta(t, 0.5, metric.SuccessRate, 1e-9)
	assert.InDelta(t, fastFailureDurationMs, metric.AverageRetryFailureDurationMs, 1e-9)
	require.NotNil(t, metric.StabilityScore)
	assert.InDelta(t, 0.8, *metric.StabilityScore, 1e-9)
}

func TestGetChannelMonitorSmartScheduleRoutesReturnsWindowedSharedSamples(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	common.LogConsumeEnabled = false
	constant.ErrorLogEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 1, 90, 30,
	)
	stabilityWindowMinutes := 1
	policy.StabilityWindowMinutes = &stabilityWindowMinutes
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:           "true",
		channelMonitorSmartSchedulePerformanceWindowOption: "5",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t, policy,
		),
	})
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1306, Name: "windowed samples", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1306, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1306, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)
	now := common.GetTimestamp()
	_, err := saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
		ChannelId: 1306, Model: "model-a", WindowStart: now - 3600,
		Time: now - 120, Success: false, Error: "过期失败",
	})
	require.NoError(t, err)
	_, err = saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
		ChannelId: 1306, Model: "model-a", WindowStart: now - 3600,
		Time: now - 5, Success: true,
	})
	require.NoError(t, err)

	var stored model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where("channel_id = ? AND model_name = ?", 1306, "model-a").First(&stored).Error)
	assert.Equal(t, int64(2), stored.SampleCount)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/schedule", nil,
	)
	GetChannelMonitorSmartScheduleRoutes(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Routes                   []channelSmartScheduleRouteResponse          `json:"routes"`
			SampleItems              []channelSmartScheduleSampleItem             `json:"sample_items"`
			BusinessPerformanceItems []model.ChannelMonitorRoutePerformanceMetric `json:"business_performance_items"`
			PerformanceItems         []model.ChannelMonitorRoutePerformanceMetric `json:"performance_items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	require.Len(t, response.Data.Routes, 1)
	require.Len(t, response.Data.SampleItems, 1)
	performanceSamples := response.Data.SampleItems[0].PerformanceWindow
	assert.Equal(t, int64(2), performanceSamples.SampleCount)
	assert.Equal(t, int64(1), performanceSamples.SuccessCount)
	assert.Equal(t, now-5, performanceSamples.LastTime)
	assert.Empty(t, performanceSamples.LastError)
	stabilitySamples := response.Data.SampleItems[0].StabilityWindow
	assert.Equal(t, int64(1), stabilitySamples.SampleCount)
	assert.Equal(t, now-5, stabilitySamples.LastTime)
	assert.Empty(t, response.Data.BusinessPerformanceItems)
	require.Len(t, response.Data.PerformanceItems, 1)
	assert.Equal(t, 2, response.Data.PerformanceItems[0].SampleCount)
	assert.NotContains(t, recorder.Body.String(), `"shared_samples"`)
}

func TestGetChannelMonitorSmartScheduleRouteSummarySkipsMetricAndSampleLoading(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t,
			channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyFirstToken, false,
				channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 1, 90, 30,
			),
		),
	})
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1308, Name: "summary only", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1308, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1308, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleModelSampleState{
		ChannelId: 1308, ModelName: "model-a", SamplesJSON: model.ChannelSmartScheduleSamplesJSON("invalid-json"),
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/schedule?metrics=false", nil,
	)
	expectedSnapshotMetrics := model.GetChannelSmartScheduleExecutionDetailMetrics()
	GetChannelMonitorSmartScheduleRoutes(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			MetricsIncluded          bool                                              `json:"metrics_included"`
			Routes                   []channelSmartScheduleRouteResponse               `json:"routes"`
			SampleItems              []channelSmartScheduleSampleItem                  `json:"sample_items"`
			PerformanceItems         []model.ChannelMonitorRoutePerformanceMetric      `json:"performance_items"`
			StabilityItems           []model.ChannelMonitorRouteStabilityMetric        `json:"stability_items"`
			ExecutionSnapshotMetrics *model.ChannelSmartScheduleExecutionDetailMetrics `json:"execution_snapshot_metrics"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.False(t, response.Data.MetricsIncluded)
	require.NotNil(t, response.Data.ExecutionSnapshotMetrics)
	assert.Equal(t, expectedSnapshotMetrics, *response.Data.ExecutionSnapshotMetrics)
	require.Len(t, response.Data.Routes, 1)
	assert.Equal(t, "model-a", response.Data.Routes[0].SampleModel)
	assert.Empty(t, response.Data.SampleItems)
	assert.Empty(t, response.Data.PerformanceItems)
	assert.Empty(t, response.Data.StabilityItems)
}

func TestGetChannelMonitorSmartScheduleRoutesDoesNotInitializeOrClearRouteStates(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t,
			channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 1, 90, 30,
			),
		),
	})
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1309, Name: "missing state", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 1310, Name: "expired fixed primary", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1309, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1310, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	expiredAt := common.GetTimestamp() - 60
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1310, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		ManualPrimaryUntil: expiredAt, ManualPrimarySaved: true,
		ManualPrimarySavedPriority: 70, ManualPrimarySavedWeight: 40,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/schedule", nil,
	)
	GetChannelMonitorSmartScheduleRoutes(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var stateCount int64
	require.NoError(t, db.Model(&model.ChannelSmartScheduleRouteState{}).Count(&stateCount).Error)
	assert.Equal(t, int64(1), stateCount)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1310, "vip", "model-a",
	).First(&state).Error)
	assert.Equal(t, expiredAt, state.ManualPrimaryUntil)
	assert.True(t, state.ManualPrimarySaved)
	assert.Equal(t, int64(70), state.ManualPrimarySavedPriority)
	assert.Equal(t, uint(40), state.ManualPrimarySavedWeight)
	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 1310, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priority, *ability.Priority)
	assert.Equal(t, weight, ability.Weight)
}

func TestGetChannelMonitorSmartScheduleRoutesUsesParameterizedModelMetrics(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	const exactModel = "gemini-2.5-pro-thinking-2048"
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, []string{exactModel}, 1, 90, 30,
	)
	stabilityWindowMinutes := 60
	policy.StabilityWindowMinutes = &stabilityWindowMinutes
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:           "true",
		channelMonitorSmartSchedulePerformanceWindowOption: "60",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t, policy,
		),
	})
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1307, Name: "parameterized model", Group: "vip", Models: exactModel,
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1307, Group: "vip", Model: exactModel, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1307, GroupName: "vip", ModelName: exactModel, ParticipationSet: true,
	}).Error)
	now := common.GetTimestamp()
	minuteStart := now - now%60 - 60
	firstTokenMs := 200.0
	require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
		1307, "vip", exactModel, minuteStart+1, true, &firstTokenMs, nil, nil, false,
	))
	require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
		1307, "vip", exactModel, minuteStart+2, true, &firstTokenMs, nil, nil, false,
	))

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/schedule", nil,
	)
	GetChannelMonitorSmartScheduleRoutes(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			GeneratedAt         int64                                        `json:"generated_at"`
			DataCutoffAt        int64                                        `json:"data_cutoff_at"`
			ProjectionStartedAt int64                                        `json:"projection_started_at"`
			EventWatermark      uint64                                       `json:"event_watermark"`
			RealtimeDegraded    bool                                         `json:"realtime_degraded"`
			MetricCoverage      *channelSmartScheduleMetricCoverageResponse  `json:"metric_coverage"`
			PerformanceItems    []model.ChannelMonitorRoutePerformanceMetric `json:"performance_items"`
			StabilityItems      []model.ChannelMonitorRouteStabilityMetric   `json:"stability_items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	require.NotNil(t, response.Data.MetricCoverage)
	assert.True(t, response.Data.MetricCoverage.AggregationEnabled)
	assert.Equal(t, minuteStart+2, response.Data.DataCutoffAt)
	assert.NotZero(t, response.Data.ProjectionStartedAt)
	assert.NotZero(t, response.Data.EventWatermark)
	assert.True(t, response.Data.RealtimeDegraded)
	assert.Equal(t, response.Data.DataCutoffAt, response.Data.MetricCoverage.AggregatedThrough)
	assert.Greater(t, response.Data.MetricCoverage.AggregatedFrom, int64(0))
	assert.False(t, response.Data.MetricCoverage.PerformanceWindowComplete)
	assert.False(t, response.Data.MetricCoverage.StabilityWindowComplete)
	assert.Equal(t, response.Data.GeneratedAt-60*60, response.Data.MetricCoverage.PerformanceWindowStart)
	assert.Equal(t, response.Data.GeneratedAt-60*60, response.Data.MetricCoverage.StabilityWindowStart)
	require.Len(t, response.Data.PerformanceItems, 1)
	performance := response.Data.PerformanceItems[0]
	assert.Equal(t, "vip", performance.GroupName)
	assert.Equal(t, exactModel, performance.ModelName)
	assert.Equal(t, 2, performance.SampleCount)
	require.Len(t, response.Data.StabilityItems, 1)
	metric := response.Data.StabilityItems[0]
	assert.Equal(t, exactModel, metric.ModelName)
	assert.Equal(t, int64(2), metric.SampleCount)
	assert.Equal(t, int64(2), metric.SuccessCount)
}

func TestRunChannelSmartScheduleUsesManualSamplesInEverySampleMode(t *testing.T) {
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	common.LogConsumeEnabled = false
	constant.ErrorLogEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})

	for _, sampleMode := range []string{
		channelMonitorSmartScheduleSampleOff,
		channelMonitorSmartScheduleSampleTraffic,
		channelMonitorSmartScheduleSampleProbe,
	} {
		t.Run(sampleMode, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			applyMode := channelMonitorSmartScheduleApplyWeight
			if sampleMode == channelMonitorSmartScheduleSampleTraffic {
				applyMode = channelMonitorSmartScheduleApplyPriorityWeight
			}
			policy := channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, true,
				applyMode, []string{"model-a"}, 2, 50, 30,
			)
			policy.SampleMode = &sampleMode
			policy.Scoring.StabilityPercent = 100
			useChannelMonitorOptionMap(t, map[string]string{
				channelMonitorSmartScheduleEnabledOption:       "true",
				channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
			})

			priority := int64(80)
			weight := uint(50)
			require.NoError(t, db.Create(&[]model.Channel{
				{Id: 1311, Name: "stable", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
				{Id: 1312, Name: "less stable", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
			}).Error)
			require.NoError(t, db.Create(&[]model.Ability{
				{ChannelId: 1311, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
				{ChannelId: 1312, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
			}).Error)
			require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
				{ChannelId: 1311, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
				{ChannelId: 1312, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
			}).Error)

			now := common.GetTimestamp()
			for index, sample := range []struct {
				channelId int
				success   bool
			}{
				{channelId: 1311, success: true},
				{channelId: 1311, success: true},
				{channelId: 1312, success: true},
				{channelId: 1312, success: false},
			} {
				_, err := saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
					ChannelId: sample.channelId, Model: "model-a",
					Source:      model.ChannelSmartScheduleSampleSourceManualTest,
					WindowStart: now - 60, Time: now - int64(4-index), Success: sample.success,
				})
				require.NoError(t, err)
			}

			result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
			require.NoError(t, err)
			assert.Equal(t, 2, result.Planned)

			var states []model.ChannelSmartScheduleRouteState
			require.NoError(t, db.Order("channel_id ASC").Find(&states).Error)
			require.Len(t, states, 2)
			require.NotNil(t, states[0].LastScheduleScore)
			require.NotNil(t, states[1].LastScheduleScore)
			assert.Greater(t, *states[0].LastScheduleScore, *states[1].LastScheduleScore)
		})
	}
}

func TestGetChannelMonitorSmartScheduleRoutesKeepsParticipationRoutesWhenDisabled(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelSmartScheduleGroupRatio(t, `{"vip":1}`)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "false",
		channelMonitorSmartScheduleGroupPoliciesOption: "[]",
	})
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1306, Name: "disabled schedule", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1306, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1306, Ratio: 1, UpdatedTime: 1,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/schedule", nil,
	)
	GetChannelMonitorSmartScheduleRoutes(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Enabled                   bool                                         `json:"enabled"`
			Routes                    []model.ChannelSmartScheduleRoute            `json:"routes"`
			PerformanceItems          []model.ChannelMonitorRoutePerformanceMetric `json:"performance_items"`
			StabilityMetricsAvailable bool                                         `json:"stability_metrics_available"`
			StabilityItems            []model.ChannelMonitorRouteStabilityMetric   `json:"stability_items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.False(t, response.Data.Enabled)
	require.Len(t, response.Data.Routes, 1)
	assert.False(t, response.Data.Routes[0].State.Participates())
	require.NotNil(t, response.Data.Routes[0].CostRatio)
	require.NotNil(t, response.Data.Routes[0].GroupRatio)
	require.NotNil(t, response.Data.Routes[0].GrossMargin)
	assert.InDelta(t, 1, *response.Data.Routes[0].CostRatio, 1e-9)
	assert.InDelta(t, 1, *response.Data.Routes[0].GroupRatio, 1e-9)
	assert.Zero(t, *response.Data.Routes[0].GrossMargin)
	assert.Equal(t, channelSmartScheduleEconomicRoleBreakEvenFallback, response.Data.Routes[0].EconomicRole)
	assert.Empty(t, response.Data.PerformanceItems)
	assert.False(t, response.Data.StabilityMetricsAvailable)
	assert.Empty(t, response.Data.StabilityItems)
}

func TestUpdateChannelMonitorSmartScheduleChannelConfigUpdatesAllRoutes(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1210, Name: "multi route", Status: common.ChannelStatusEnabled,
		Group: "default,vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1210, Group: "default", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1210, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 1210, GroupName: "default", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 1210, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Excluded: true},
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodPut, "/api/channel_monitor/channel/1210/schedule/routes",
		map[string]any{"excluded": true},
	)
	ctx.AddParam("id", "1210")
	UpdateChannelMonitorSmartScheduleChannelConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"total":2`)
	assert.Contains(t, recorder.Body.String(), `"updated":2`)

	var states []model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("channel_id = ?", 1210).Find(&states).Error)
	require.Len(t, states, 2)
	for _, state := range states {
		assert.True(t, state.ParticipationSet)
		assert.True(t, state.Excluded)
	}
	var abilities []model.Ability
	require.NoError(t, db.Where("channel_id = ?", 1210).Find(&abilities).Error)
	require.Len(t, abilities, 2)
	for _, ability := range abilities {
		assert.Nil(t, ability.Priority)
		assert.Zero(t, ability.Weight)
	}
}

func TestUpdateChannelMonitorSmartScheduleRoutePrimaryDefaultsAndPersistsStabilityDegradeOption(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "false",
	})
	priority := int64(80)
	weight := uint(100)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1211, Name: "fixed primary", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1211, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1211, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)

	type primaryResponse struct {
		Success bool `json:"success"`
		Data    struct {
			AllowStabilityDegrade bool  `json:"allow_stability_degrade"`
			ManualPrimaryUntil    int64 `json:"manual_primary_until"`
		} `json:"data"`
	}
	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodPut, "/api/channel_monitor/channel/1211/schedule/route/primary",
		map[string]any{"group": "vip", "model": "model-a", "duration_minutes": 10},
	)
	ctx.AddParam("id", "1211")
	UpdateChannelMonitorSmartScheduleRoutePrimary(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response primaryResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.True(t, response.Data.AllowStabilityDegrade)
	assert.Greater(t, response.Data.ManualPrimaryUntil, common.GetTimestamp())

	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1211, GroupName: "vip", ModelName: "model-a",
	}).First(&state).Error)
	assert.True(t, state.ManualPrimaryAllowStabilityDegrade)

	ctx, recorder = newChannelMonitorControllerContext(
		t, http.MethodPut, "/api/channel_monitor/channel/1211/schedule/route/primary",
		map[string]any{
			"group": "vip", "model": "model-a", "duration_minutes": 20,
			"allow_stability_degrade": false,
		},
	)
	ctx.AddParam("id", "1211")
	UpdateChannelMonitorSmartScheduleRoutePrimary(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	response = primaryResponse{}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Data.AllowStabilityDegrade)
	require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1211, GroupName: "vip", ModelName: "model-a",
	}).First(&state).Error)
	assert.False(t, state.ManualPrimaryAllowStabilityDegrade)
}

func TestUpdateChannelMonitorSmartScheduleRoutePrimaryQueuesSuccessorBehindRunningSchedule(t *testing.T) {
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
	weight := uint(100)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1213, Name: "fixed while scheduling", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1213, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1213, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)
	running, err := model.CreateSystemTask(channelMonitorSmartScheduleTaskType, nil, nil)
	require.NoError(t, err)
	claimed, ok, err := model.ClaimSystemTask(
		running.ID, channelMonitorSmartScheduleTaskType, "running-scheduler", common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, claimed)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodPut, "/api/channel_monitor/channel/1213/schedule/route/primary",
		map[string]any{"group": "vip", "model": "model-a", "duration_minutes": 10},
	)
	ctx.AddParam("id", "1213")
	UpdateChannelMonitorSmartScheduleRoutePrimary(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var tasks []model.SystemTask
	require.NoError(t, db.Where("type = ?", channelMonitorSmartScheduleTaskType).Order("id ASC").Find(&tasks).Error)
	require.Len(t, tasks, 2)
	assert.Equal(t, model.SystemTaskStatusRunning, tasks[0].Status)
	assert.Nil(t, tasks[0].ActiveKey)
	assert.Equal(t, model.SystemTaskStatusPending, tasks[1].Status)
	require.NotNil(t, tasks[1].ActiveKey)
	assert.Equal(t, channelMonitorSmartScheduleTaskType, *tasks[1].ActiveKey)
}

func TestUpdateChannelMonitorSmartScheduleRoutePrimaryRequiresStabilityConfirmation(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "false",
	})
	priority := int64(80)
	weight := uint(100)
	degradedPriority := int64(0)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1212, Name: "protected primary", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1212, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &degradedPriority, Weight: 0,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1212, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		StabilityState:         model.ChannelSmartScheduleStabilityDegraded,
		StabilitySavedPriority: priority,
		StabilitySavedWeight:   weight,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t,
		http.MethodPut,
		"/api/channel_monitor/channel/1212/schedule/route/primary",
		map[string]any{"group": "vip", "model": "model-a", "duration_minutes": 10},
	)
	ctx.AddParam("id", "1212")
	UpdateChannelMonitorSmartScheduleRoutePrimary(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var confirmationResponse struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &confirmationResponse))
	assert.False(t, confirmationResponse.Success)
	assert.Equal(t, "smart_schedule_route_stability_confirmation_required", confirmationResponse.Code)

	ctx, recorder = newChannelMonitorControllerContext(
		t,
		http.MethodPut,
		"/api/channel_monitor/channel/1212/schedule/route/primary",
		map[string]any{
			"group": "vip", "model": "model-a", "duration_minutes": 10,
			"allow_stability_degrade":    true,
			"confirm_stability_override": true,
		},
	)
	ctx.AddParam("id", "1212")
	UpdateChannelMonitorSmartScheduleRoutePrimary(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var successResponse struct {
		Success bool `json:"success"`
		Data    struct {
			StabilityProtectionCleared bool `json:"stability_protection_cleared"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &successResponse))
	assert.True(t, successResponse.Success)
	assert.True(t, successResponse.Data.StabilityProtectionCleared)
}
