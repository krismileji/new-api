package controller

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSmartScheduleClassifyEconomicsUsesConvertedCostRatio(t *testing.T) {
	conversion, err := service.MarshalChannelMonitorCostConversion(service.ChannelMonitorCostConversion{
		Mode:        service.ChannelMonitorCostConversionRecharge,
		PaidCNY:     80,
		CreditedUSD: 100,
	})
	require.NoError(t, err)

	economics := channelSmartScheduleClassifyEconomics(
		model.ChannelRatioMonitor{Ratio: 1, UpdatedTime: 1, CostConversion: conversion},
		true,
		1,
		true,
	)

	require.NotNil(t, economics.CostRatio)
	require.NotNil(t, economics.GroupRatio)
	require.NotNil(t, economics.GrossMargin)
	assert.InDelta(t, 0.8, *economics.CostRatio, 1e-9)
	assert.InDelta(t, 1, *economics.GroupRatio, 1e-9)
	assert.InDelta(t, 0.2, *economics.GrossMargin, 1e-9)
	assert.Equal(t, channelSmartScheduleEconomicRoleNormal, economics.EconomicRole)
}

func TestChannelSmartScheduleClassifyEconomicsUsesRatioEpsilon(t *testing.T) {
	tests := []struct {
		name       string
		costRatio  float64
		groupRatio float64
		role       string
	}{
		{name: "exactly equal", costRatio: 1, groupRatio: 1, role: channelSmartScheduleEconomicRoleBreakEvenFallback},
		{name: "within epsilon", costRatio: 1 + channelMonitorRatioEpsilon/2, groupRatio: 1, role: channelSmartScheduleEconomicRoleBreakEvenFallback},
		{name: "profitable outside epsilon", costRatio: 1 - channelMonitorRatioEpsilon*2, groupRatio: 1, role: channelSmartScheduleEconomicRoleNormal},
		{name: "existing inverted cost policy", costRatio: 1 + channelMonitorRatioEpsilon*2, groupRatio: 1, role: channelSmartScheduleEconomicRoleUnknown},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			economics := channelSmartScheduleClassifyEconomics(
				model.ChannelRatioMonitor{Ratio: test.costRatio, UpdatedTime: 1},
				true,
				test.groupRatio,
				true,
			)
			assert.Equal(t, test.role, economics.EconomicRole)
		})
	}
}

func TestPlanChannelSmartSchedulePriorityWeightKeepsBreakEvenFallbackAtP1(t *testing.T) {
	normalRatio := 0.8
	fallbackRatio := 1.0
	plan := planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{ChannelId: 1, Ratio: &normalRatio, EconomicRole: channelSmartScheduleEconomicRoleNormal},
		{ChannelId: 2, Ratio: &fallbackRatio, EconomicRole: channelSmartScheduleEconomicRoleBreakEvenFallback},
		{ChannelId: 3, Ratio: &fallbackRatio, EconomicRole: channelSmartScheduleEconomicRoleBreakEvenFallback},
	}, channelMonitorSmartScheduleStrategyRatio, false, channelMonitorSmartScheduleApplyPriorityWeight, 5, false)

	require.Len(t, plan.Items, 3)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, int64(2), items[1].TargetPriority)
	assert.Equal(t, int64(1), items[2].TargetPriority)
	assert.Equal(t, int64(1), items[3].TargetPriority)
	assert.Equal(t, uint(500), items[2].TargetWeight)
	assert.Equal(t, uint(500), items[3].TargetWeight)
	assert.Equal(t, 1, plan.RawWinnerId)
	assert.Equal(t, 1, plan.ActualPrimaryId)
	assert.Equal(
		t,
		channelSmartScheduleEconomicRoleBreakEvenFallback,
		items[2].ScoreDetails.Economics.EconomicRole,
	)
}

func TestPlanChannelSmartSchedulePriorityWeightUsesFallbackWhenNoNormalCandidateExists(t *testing.T) {
	ratio := 1.0
	fastTPS := 30.0
	slowTPS := 10.0
	plan := planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{
			ChannelId: 1, Ratio: &ratio, TPS: &fastTPS, TPSSampleCount: 5,
			EconomicRole: channelSmartScheduleEconomicRoleBreakEvenFallback,
		},
		{
			ChannelId: 2, Ratio: &ratio, TPS: &slowTPS, TPSSampleCount: 5,
			EconomicRole: channelSmartScheduleEconomicRoleBreakEvenFallback,
		},
	}, channelMonitorSmartScheduleStrategyTPS, false, channelMonitorSmartScheduleApplyPriorityWeight, 5, false)

	require.Len(t, plan.Items, 2)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
		assert.Equal(t, int64(1), item.TargetPriority)
	}
	assert.Greater(t, items[1].TargetWeight, items[2].TargetWeight)
	assert.Equal(t, 1, plan.RawWinnerId)
	assert.Equal(t, 1, plan.ActualPrimaryId)
	assert.Contains(t, items[1].ScoreDetails.Decision.AdjustmentReason, "正在接管")
}

func TestPlanChannelSmartScheduleWeightModeReservesP1ForBreakEvenFallback(t *testing.T) {
	normalRatio := 0.8
	fallbackRatio := 1.0
	plan := planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{ChannelId: 1, CurrentPriority: 0, CurrentWeight: 50, Ratio: &normalRatio, EconomicRole: channelSmartScheduleEconomicRoleNormal},
		{ChannelId: 2, CurrentPriority: 0, CurrentWeight: 50, Ratio: &normalRatio, EconomicRole: channelSmartScheduleEconomicRoleNormal},
		{ChannelId: 3, CurrentPriority: 0, CurrentWeight: 50, Ratio: &fallbackRatio, EconomicRole: channelSmartScheduleEconomicRoleBreakEvenFallback},
	}, channelMonitorSmartScheduleStrategyRatio, false, channelMonitorSmartScheduleApplyWeight, 5, false)

	require.Len(t, plan.Items, 3)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, int64(2), items[1].TargetPriority)
	assert.Equal(t, int64(2), items[2].TargetPriority)
	assert.Equal(t, int64(1), items[3].TargetPriority)
	assert.Equal(t, uint(1000), items[3].TargetWeight)
}

func TestPlanChannelSmartScheduleManualPrimaryCanOverrideBreakEvenFallbackLayer(t *testing.T) {
	normalRatio := 0.8
	fallbackRatio := 1.0
	plan := planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{ChannelId: 1, Ratio: &normalRatio, EconomicRole: channelSmartScheduleEconomicRoleNormal},
		{
			ChannelId: 2, Ratio: &fallbackRatio,
			EconomicRole:  channelSmartScheduleEconomicRoleBreakEvenFallback,
			ManualPrimary: true, ManualTargetPriority: 10,
		},
	}, channelMonitorSmartScheduleStrategyRatio, false, channelMonitorSmartScheduleApplyPriorityWeight, 5, false)

	require.Len(t, plan.Items, 2)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, int64(1), items[2].BasePriority)
	assert.Equal(t, int64(10), items[2].TargetPriority)
	assert.Equal(t, 2, plan.ActualPrimaryId)
	assert.Contains(t, items[2].ScoreDetails.Decision.AdjustmentReason, "管理员固定结果覆盖")
}

func TestRunChannelSmartScheduleWritesBreakEvenFallbackLayer(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelSmartScheduleGroupRatio(t, `{"vip":1}`)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip",
		channelMonitorSmartScheduleStrategyRatio,
		false,
		channelMonitorSmartScheduleApplyPriorityWeight,
		[]string{"model-a"},
		5,
		80,
		30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t,
			policy,
		),
	})

	priority := int64(80)
	weight := uint(100)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 2301, Name: "profitable", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 2302, Name: "break even", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 2301, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 2302, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 2301, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 2302, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 2301, Ratio: 0.8, UpdatedTime: 1},
		{ChannelId: 2302, Ratio: 1, UpdatedTime: 1},
	}).Error)

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Planned)

	var abilities []model.Ability
	require.NoError(t, db.Order("channel_id ASC").Find(&abilities).Error)
	require.Len(t, abilities, 2)
	require.NotNil(t, abilities[0].Priority)
	require.NotNil(t, abilities[1].Priority)
	assert.Equal(t, int64(2), *abilities[0].Priority)
	assert.Equal(t, int64(1), *abilities[1].Priority)

	var fallbackState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?",
		2302,
		"vip",
		"model-a",
	).First(&fallbackState).Error)
	assert.Equal(t, int64(1), fallbackState.BasePriority)
	assert.Empty(t, fallbackState.TemporaryTrafficKind)
	details, err := fallbackState.LastScheduleScoreDetails.Decode()
	require.NoError(t, err)
	require.NotNil(t, details)
	require.NotNil(t, details.Economics)
	assert.Equal(t, channelSmartScheduleEconomicRoleBreakEvenFallback, details.Economics.EconomicRole)
}

func TestUpdateChannelMonitorRatioQueuesScheduleSuccessor(t *testing.T) {
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
	require.NoError(t, db.Create(&model.Channel{
		Id: 2303, Name: "ratio reschedule", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled,
	}).Error)
	running, err := model.CreateSystemTask(channelMonitorSmartScheduleTaskType, nil, nil)
	require.NoError(t, err)
	_, claimed, err := model.ClaimSystemTask(
		running.ID,
		channelMonitorSmartScheduleTaskType,
		"economic-update-runner",
		common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	require.True(t, claimed)

	ctx, recorder := newChannelMonitorControllerContext(
		t,
		http.MethodPut,
		"/api/channel_monitor/channel/2303/ratio",
		map[string]any{"ratio": 1.0, "remark": "became break even"},
	)
	ctx.AddParam("id", "2303")
	UpdateChannelMonitorRatio(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var tasks []model.SystemTask
	require.NoError(t, db.Where("type = ?", channelMonitorSmartScheduleTaskType).Order("id ASC").Find(&tasks).Error)
	require.Len(t, tasks, 2)
	assert.Equal(t, model.SystemTaskStatusRunning, tasks[0].Status)
	assert.Equal(t, model.SystemTaskStatusPending, tasks[1].Status)
}
