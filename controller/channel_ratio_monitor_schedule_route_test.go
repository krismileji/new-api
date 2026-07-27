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
		channelMonitorSmartScheduleScopeOption:     channelMonitorSmartScheduleScopeGroupModel,
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
	assert.Equal(t, channelMonitorSmartScheduleScopeGroupModel, result.Scope)
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

func TestSmartScheduleMutationAPIsRejectInactiveScope(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)

	t.Run("route overview in channel mode", func(t *testing.T) {
		useChannelMonitorOptionMap(t, map[string]string{
			channelMonitorSmartScheduleScopeOption: channelMonitorSmartScheduleScopeChannel,
		})
		ctx, recorder := newChannelMonitorControllerContext(
			t, http.MethodGet, "/api/channel_monitor/schedule", nil,
		)
		GetChannelMonitorSmartScheduleRoutes(ctx)
		assert.Equal(t, http.StatusConflict, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "请使用渠道级调度操作")
	})

	t.Run("channel endpoint in route mode", func(t *testing.T) {
		useChannelMonitorOptionMap(t, map[string]string{
			channelMonitorSmartScheduleScopeOption: channelMonitorSmartScheduleScopeGroupModel,
		})
		ctx, recorder := newChannelMonitorControllerContext(
			t, http.MethodPut, "/api/channel_monitor/channel/1/schedule",
			map[string]any{"excluded": false},
		)
		UpdateChannelMonitorSmartScheduleConfig(ctx)
		assert.Equal(t, http.StatusConflict, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "请使用路由级调度操作")
	})

	t.Run("route endpoint in channel mode", func(t *testing.T) {
		useChannelMonitorOptionMap(t, map[string]string{
			channelMonitorSmartScheduleScopeOption: channelMonitorSmartScheduleScopeChannel,
		})
		ctx, recorder := newChannelMonitorControllerContext(
			t, http.MethodPut, "/api/channel_monitor/channel/1/schedule/route",
			map[string]any{"group": "vip", "model": "model-a", "excluded": false},
		)
		UpdateChannelMonitorSmartScheduleRouteConfig(ctx)
		assert.Equal(t, http.StatusConflict, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "请使用渠道级调度操作")
	})
}

func TestUpdateChannelMonitorSettingsSwitchesSmartScheduleRoutingLayer(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleScopeOption: channelMonitorSmartScheduleScopeGroupModel,
	})
	require.NoError(t, db.Create(&model.Option{
		Key: channelMonitorSmartScheduleScopeOption, Value: channelMonitorSmartScheduleScopeGroupModel,
	}).Error)
	channelPriority := int64(80)
	channelWeight := uint(50)
	routePriority := int64(95)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1210, Name: "scope switch", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1210, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &routePriority, Weight: 70,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1210, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodPut, "/api/channel_monitor/settings",
		map[string]any{"smart_schedule_scope": channelMonitorSmartScheduleScopeChannel},
	)
	UpdateChannelMonitorSettings(ctx)
	assert.Equal(t, http.StatusOK, recorder.Code)
	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1210, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, channelPriority, *ability.Priority)
	assert.Equal(t, channelWeight, ability.Weight)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1210, "vip", "model-a",
	).First(&state).Error)
	assert.True(t, state.ScopeRoutingSaved)
	assert.Equal(t, routePriority, state.ScopeSavedPriority)
	assert.Equal(t, uint(70), state.ScopeSavedWeight)

	ctx, recorder = newChannelMonitorControllerContext(
		t, http.MethodPut, "/api/channel_monitor/settings",
		map[string]any{"smart_schedule_scope": channelMonitorSmartScheduleScopeGroupModel},
	)
	UpdateChannelMonitorSettings(ctx)
	assert.Equal(t, http.StatusOK, recorder.Code)
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1210, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, routePriority, *ability.Priority)
	assert.Equal(t, uint(70), ability.Weight)
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1210, "vip", "model-a",
	).First(&state).Error)
	assert.False(t, state.ScopeRoutingSaved)
}
