package controller

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRunChannelSmartScheduleRecordsRouteAdjustmentsAndReasons(t *testing.T) {
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
	assert.Equal(t, uint(100), cheap.NewWeight)
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
	require.NoError(t, err)
	assert.Equal(t, 2, result.Failed)
	require.Len(t, result.Adjustments, 2)
	for _, adjustment := range result.Adjustments {
		assert.Equal(t, channelSmartScheduleAdjustmentFailed, adjustment.Action)
		assert.Equal(t, forcedError.Error(), adjustment.Reason)
		assert.Equal(t, "vip", adjustment.Group)
		assert.Equal(t, "model-a", adjustment.Model)
	}
}

func TestChannelSmartScheduleTaskResultLimitsPersistedAdjustmentDetails(t *testing.T) {
	result := channelSmartScheduleTaskResult{}
	for channelId := 1; channelId <= maxChannelSmartScheduleTaskAdjustmentDetails; channelId++ {
		result.recordAdjustment(channelSmartScheduleTaskAdjustment{
			ChannelId: channelId, Action: channelSmartScheduleAdjustmentUnchanged,
		})
	}
	result.recordAdjustment(channelSmartScheduleTaskAdjustment{
		ChannelId: 9999, Action: channelSmartScheduleAdjustmentFailed,
		Reason: "需要优先保留的失败原因",
	})

	result.finalizeAdjustments()

	assert.True(t, result.AdjustmentDetailsTruncated)
	require.Len(t, result.Adjustments, maxChannelSmartScheduleTaskAdjustmentDetails)
	assert.Equal(t, channelSmartScheduleAdjustmentFailed, result.Adjustments[0].Action)
	assert.Equal(t, 9999, result.Adjustments[0].ChannelId)
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
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			channelSmartScheduleTestGroupPolicy(
				"gold", channelMonitorSmartScheduleStrategyTPS, false,
				channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-b"}, 1, 80, 30,
			),
		),
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
	require.NoError(t, db.Create(&[]model.ChannelMonitorMinuteMetric{
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
	assert.Equal(t, int64(100), *goldFast.Priority)
	assert.Equal(t, int64(90), *goldSlow.Priority)
	assert.Greater(t, goldFast.Weight, goldSlow.Weight)
	assert.Equal(t, int64(80), *abilityByRoute[routeKey{channelId: 1301, model: "model-a"}].Priority)
	assert.Equal(t, weight, abilityByRoute[routeKey{channelId: 1301, model: "model-a"}].Weight)

	silverExpensive := abilityByRoute[routeKey{channelId: 1303, model: "model-a"}]
	silverCheap := abilityByRoute[routeKey{channelId: 1304, model: "model-a"}]
	assert.Equal(t, int64(80), *silverExpensive.Priority)
	assert.Equal(t, int64(80), *silverCheap.Priority)
	assert.Equal(t, weight, silverExpensive.Weight)
	assert.Equal(t, weight, silverCheap.Weight)
}

func TestGetChannelMonitorSmartScheduleRoutesUsesProbeStabilityWithoutLogs(t *testing.T) {
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
	_, err := model.SaveChannelSmartScheduleProbeResult(model.ChannelSmartScheduleProbeResult{
		ChannelId: 1305, Group: "vip", Model: "model-a",
		WindowStart: now - 3600, Time: now - 60, Success: true,
	})
	require.NoError(t, err)
	fastFailureDurationMs := 500.0
	_, err = model.SaveChannelSmartScheduleProbeResult(model.ChannelSmartScheduleProbeResult{
		ChannelId: 1305, Group: "vip", Model: "model-a",
		WindowStart: now - 3600, Time: now - 30, Success: false,
		DurationMs: &fastFailureDurationMs,
	})
	require.NoError(t, err)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/schedule", nil,
	)
	GetChannelMonitorSmartScheduleRoutes(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			StabilityMetricsAvailable bool                                       `json:"stability_metrics_available"`
			StabilityItems            []model.ChannelMonitorRouteStabilityMetric `json:"stability_items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
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

func TestGetChannelMonitorSmartScheduleRoutesKeepsParticipationRoutesWhenDisabled(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
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
	assert.Contains(t, recorder.Body.String(), `"updated":1`)

	var states []model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("channel_id = ?", 1210).Find(&states).Error)
	require.Len(t, states, 2)
	for _, state := range states {
		assert.True(t, state.ParticipationSet)
		assert.True(t, state.Excluded)
	}
}
