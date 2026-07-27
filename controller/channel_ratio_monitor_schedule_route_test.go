package controller

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunChannelSmartScheduleByRouteIsolatesGroupModelPools(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelSmartScheduleRouteState{}))
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:   "true",
		channelMonitorSmartScheduleStrategyOption:  channelMonitorSmartScheduleStrategyRatio,
		channelMonitorSmartScheduleApplyModeOption: channelMonitorSmartScheduleApplyWeight,
		channelMonitorSmartScheduleModelsOption:    `["model-a"]`,
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
		{ChannelId: 1201, Ratio: 1.5, UpdatedTime: 1, SmartScheduleParticipationSet: true},
		{ChannelId: 1202, Ratio: 3, UpdatedTime: 1, SmartScheduleParticipationSet: true},
		{ChannelId: 1203, Ratio: 1, UpdatedTime: 1, SmartScheduleParticipationSet: true},
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

func TestRunChannelSmartScheduleByRouteUsesGroupPolicyOverrides(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	strategy := channelMonitorSmartScheduleStrategyTPS
	applyMode := channelMonitorSmartScheduleApplyPriorityWeight
	models := []string{"model-b"}
	minSamples := 1
	groupPolicies, err := common.Marshal([]channelSmartScheduleGroupPolicy{{
		Group: "gold", Strategy: &strategy, ApplyMode: &applyMode, Models: &models, MinSamples: &minSamples,
	}})
	require.NoError(t, err)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleStrategyOption:      channelMonitorSmartScheduleStrategyRatio,
		channelMonitorSmartScheduleApplyModeOption:     channelMonitorSmartScheduleApplyWeight,
		channelMonitorSmartScheduleModelsOption:        `["model-a"]`,
		channelMonitorSmartScheduleGroupPoliciesOption: string(groupPolicies),
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
		{ChannelId: 1301, Ratio: 3, UpdatedTime: 1, SmartScheduleParticipationSet: true},
		{ChannelId: 1302, Ratio: 1, UpdatedTime: 1, SmartScheduleParticipationSet: true},
		{ChannelId: 1303, Ratio: 3, UpdatedTime: 1, SmartScheduleParticipationSet: true},
		{ChannelId: 1304, Ratio: 1, UpdatedTime: 1, SmartScheduleParticipationSet: true},
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
	assert.Equal(t, 4, result.Total)
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
	assert.Less(t, silverExpensive.Weight, silverCheap.Weight)
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
