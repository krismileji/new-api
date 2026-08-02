package controller

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtectChannelSmartScheduleRuntimeFailureWaitsForMinimumSamples(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 2, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	priority := int64(100)
	weight := uint(2)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1501, Name: "runtime sample gate", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1501, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1501, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		BaseRank: 2, BasePriority: 20, BaseWeight: 40,
		TemporaryTrafficKind:  model.ChannelSmartScheduleTemporaryTrafficExploration,
		TemporaryTrafficSince: common.GetTimestamp() - 30,
	}).Error)

	now := common.GetTimestamp()
	minuteStart := now - now%60 - 60
	require.NoError(t, db.Create(&model.ChannelMonitorMinuteMetric{
		MinuteStart: minuteStart, ChannelId: 1501,
		ModelKey: "model-a", GroupKey: "vip", APIKeyKey: "all",
		ModelName: "model-a", GroupName: "vip",
		ActualSuccessCount: 1, FinalSuccessCount: 1, SampleCount: 1,
	}).Error)

	runtimeError := types.NewErrorWithStatusCode(errors.New("上游返回 503"), types.ErrorCodeGetChannelFailed, 503)
	protectChannelSmartScheduleRuntimeFailure(1501, "model-a", runtimeError)

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1501, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priority, *ability.Priority)
	assert.Equal(t, weight, ability.Weight)

	_, err := model.SaveChannelSmartScheduleModelSample(model.ChannelSmartScheduleModelSampleResult{
		ChannelId:   1501,
		Model:       "model-a",
		Source:      model.ChannelSmartScheduleSampleSourceManualTest,
		SampleId:    "runtime-gate-sample",
		WindowStart: now - 3600,
		Time:        now - 30,
		Success:     true,
	})
	require.NoError(t, err)

	protectChannelSmartScheduleRuntimeFailure(1501, "model-a", runtimeError)
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1501, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
}

func TestProtectChannelSmartScheduleRuntimeFailureUses429CooldownWithoutStabilityDegradation(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 1, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:           "true",
		channelMonitorSmartScheduleGroupPoliciesOption:     channelSmartScheduleTestGroupPoliciesJSON(t, policy),
		channelMonitorSmartScheduleRateLimitCooldownOption: "30",
	})

	priority := int64(100)
	weight := uint(20)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1502, Name: "rate limit cooldown", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1502, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1502, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1, BasePriority: 100, BaseWeight: 20,
	}).Error)
	now := common.GetTimestamp()
	minuteStart := now - now%60 - 60
	require.NoError(t, db.Create(&model.ChannelMonitorMinuteMetric{
		MinuteStart: minuteStart, ChannelId: 1502,
		ModelKey: "model-a", GroupKey: "vip", APIKeyKey: "all",
		ModelName: "model-a", GroupName: "vip",
		ActualSuccessCount: 1, FinalSuccessCount: 1, SampleCount: 1,
	}).Error)

	rateLimitError := types.NewErrorWithStatusCode(errors.New("上游并发已满"), types.ErrorCodeGetChannelFailed, 429)
	assert.False(t, isChannelSmartScheduleRuntimeFailure(rateLimitError))
	protectChannelSmartScheduleRuntimeFailure(1502, "model-a", rateLimitError)
	assert.Greater(t, service.ChannelRateLimitCooldownUntil(1502, "model-a"), now)

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1502, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priority, *ability.Priority)
	assert.Equal(t, weight, ability.Weight)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1502, GroupName: "vip", ModelName: "model-a",
	}).First(&state).Error)
	assert.Empty(t, state.StabilityState)
}

func TestProtectChannelSmartScheduleRuntimeFailureCanDisable429Cooldown(t *testing.T) {
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 1, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:           "true",
		channelMonitorSmartScheduleGroupPoliciesOption:     channelSmartScheduleTestGroupPoliciesJSON(t, policy),
		channelMonitorSmartScheduleRateLimitCooldownOption: "0",
	})

	rateLimitError := types.NewErrorWithStatusCode(errors.New("上游并发已满"), types.ErrorCodeGetChannelFailed, 429)
	protectChannelSmartScheduleRuntimeFailure(1503, "model-a", rateLimitError)
	assert.Zero(t, service.ChannelRateLimitCooldownUntil(1503, "model-a"))
}

func TestProtectChannelSmartScheduleRuntimeFailureIgnoresLocalSkipRetry429(t *testing.T) {
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 1, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:           "true",
		channelMonitorSmartScheduleGroupPoliciesOption:     channelSmartScheduleTestGroupPoliciesJSON(t, policy),
		channelMonitorSmartScheduleRateLimitCooldownOption: "30",
	})

	localLimitError := types.NewErrorWithStatusCode(
		errors.New("本地渠道并发已满"),
		types.ErrorCodeGetChannelFailed,
		429,
		types.ErrOptionWithSkipRetry(),
	)
	protectChannelSmartScheduleRuntimeFailure(1504, "model-a", localLimitError)
	assert.Zero(t, service.ChannelRateLimitCooldownUntil(1504, "model-a"))
}
