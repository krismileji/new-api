package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtectChannelSmartScheduleRuntimeFailureWaitsForMinimumSamples(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
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
	_, err := model.SaveChannelSmartScheduleModelSample(model.ChannelSmartScheduleModelSampleResult{
		ChannelId: 1501, Model: "model-a",
		Source:   model.ChannelSmartScheduleSampleSourceManualTest,
		SampleId: "runtime-gate-initial-sample", WindowStart: now - 3600,
		Time: now - 20, Success: true,
	})
	require.NoError(t, err)

	runtimeError := types.NewErrorWithStatusCode(errors.New("上游返回 503"), types.ErrorCodeGetChannelFailed, 503)
	protectChannelSmartScheduleRuntimeFailure(1501, "model-a", runtimeError)
	assert.Zero(t, service.ChannelRateLimitCooldownUntil(1501, "model-a"))

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1501, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priority, *ability.Priority)
	assert.Equal(t, weight, ability.Weight)

	_, err = model.SaveChannelSmartScheduleModelSample(model.ChannelSmartScheduleModelSampleResult{
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
	assert.Greater(t, service.ChannelRateLimitCooldownUntil(1501, "model-a"), now)
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1501, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
}

func TestProtectChannelSmartScheduleRuntimeFailureUsesConsecutiveThresholdAfterSuccessReset(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 5, 80, 30,
	)
	consecutiveFailureThreshold := 2
	burstFailureThreshold := 100
	policy.ConsecutiveFailureThreshold = &consecutiveFailureThreshold
	policy.BurstFailureThreshold = &burstFailureThreshold
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1511, Name: "runtime consecutive failures", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1511, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1511, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)

	runtimeError := types.NewErrorWithStatusCode(errors.New("上游返回 503"), types.ErrorCodeGetChannelFailed, 503)
	protectChannelSmartScheduleRuntimeFailure(1511, "model-a", runtimeError)
	observeChannelSmartScheduleRuntimeRequestSuccess(1511, "model-a")
	protectChannelSmartScheduleRuntimeFailure(1511, "model-a", runtimeError)

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1511, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priority, *ability.Priority)
	assert.Equal(t, weight, ability.Weight)

	protectChannelSmartScheduleRuntimeFailure(1511, "model-a", runtimeError)
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1511, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1511, "vip", "model-a",
	).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, state.StabilityState)
	assert.Greater(t, state.RuntimeProtectionUntil, common.GetTimestamp())
}

func TestChannelSmartScheduleRuntimeRequestSuccessDoesNotCountAsProbeRecovery(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	const (
		channelID = 1591
		revision  = "runtime-health-success"
	)

	failure := observeChannelSmartScheduleRuntimeFailure(channelID, "model-a", 100, 30, revision)
	assert.Equal(t, 1, failure.ConsecutiveFailures)

	requestSuccess := observeChannelSmartScheduleRuntimeSuccess(
		channelID, "model-a", 101, revision, channelSmartScheduleRuntimeRequestSuccess,
	)
	assert.Zero(t, requestSuccess.ConsecutiveFailures)
	assert.Zero(t, requestSuccess.RecoverySuccesses)
	assert.Len(t, requestSuccess.FailureTimes, 1)

	probeSuccess := observeChannelSmartScheduleRuntimeSuccess(
		channelID, "model-a", 102, revision, channelSmartScheduleRuntimeRecoveryProbeSuccess,
	)
	assert.Equal(t, 1, probeSuccess.RecoverySuccesses)
}

func TestChannelSmartScheduleRuntimeHealthKeepsFailuresForLongerConfiguredWindows(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	const (
		channelID = 1592
		revision  = "runtime-health-window"
	)

	observeChannelSmartScheduleRuntimeFailure(channelID, "model-a", 100, 300, revision)
	snapshot := getChannelSmartScheduleRuntimeHealth(channelID, "model-a", 200, 30, revision)

	assert.Equal(t, 1, channelSmartScheduleRuntimeFailureCount(snapshot, 200, 300))
	assert.Zero(t, channelSmartScheduleRuntimeFailureCount(snapshot, 200, 30))
}

func TestChannelSmartScheduleRuntimeRegularProbeDoesNotAccumulateRecovery(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 5, 80, 30,
	)
	probeMode := channelMonitorSmartScheduleSampleProbe
	policy.SampleMode = &probeMode
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	priority := int64(80)
	weight := uint(30)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1593, Name: "regular probe recovery", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1593, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1593, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)

	observeChannelSmartScheduleRuntimeProbeSuccess(1593, "model-a")
	snapshot := getChannelSmartScheduleRuntimeHealth(1593, "model-a", common.GetTimestamp(), 30, "")
	assert.Zero(t, snapshot.RecoverySuccesses)

	require.NoError(t, db.Model(&model.ChannelSmartScheduleRouteState{}).Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1593, "vip", "model-a",
	).Updates(map[string]interface{}{
		"stability_state": model.ChannelSmartScheduleStabilityProbing,
	}).Error)
	observeChannelSmartScheduleRuntimeProbeSuccess(1593, "model-a")
	snapshot = getChannelSmartScheduleRuntimeHealth(1593, "model-a", common.GetTimestamp(), 30, "")
	assert.Equal(t, 1, snapshot.RecoverySuccesses)
}

func TestProtectChannelSmartScheduleRuntimeFailureUsesBurstThresholdAcrossSuccesses(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 5, 80, 30,
	)
	consecutiveFailureThreshold := 100
	burstFailureThreshold := 3
	policy.ConsecutiveFailureThreshold = &consecutiveFailureThreshold
	policy.BurstFailureThreshold = &burstFailureThreshold
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1512, Name: "runtime burst failures", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1512, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1512, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)

	runtimeError := types.NewErrorWithStatusCode(errors.New("上游返回 503"), types.ErrorCodeGetChannelFailed, 503)
	for range 2 {
		protectChannelSmartScheduleRuntimeFailure(1512, "model-a", runtimeError)
		observeChannelSmartScheduleRuntimeRequestSuccess(1512, "model-a")
	}
	protectChannelSmartScheduleRuntimeFailure(1512, "model-a", runtimeError)

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1512, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
}

func TestProtectChannelSmartScheduleRuntimeFailureReDegradesProbeImmediately(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 3, 80, 30,
	)
	policy.StabilityReleaseMaxPromptTokens = common.GetPointer(1234)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	now := common.GetTimestamp()
	probePriority := int64(0)
	restorePriority := int64(100)
	restoreWeight := uint(40)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1510, Name: "runtime probe sample gate", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &restorePriority, Weight: &restoreWeight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1510, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &probePriority, Weight: 10,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1510, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		StabilityState: model.ChannelSmartScheduleStabilityProbing,
		StabilitySince: now - 30, StabilitySavedPriority: restorePriority,
		StabilitySavedWeight:            restoreWeight,
		StabilityReleaseMaxPromptTokens: 1234,
	}).Error)

	oldMinute := now - now%60 - 60
	require.NoError(t, db.Create(&model.ChannelMonitorMinuteMetric{
		MinuteStart: oldMinute, ChannelId: 1510,
		ModelKey: "model-a", GroupKey: "vip", APIKeyKey: "all",
		ModelName: "model-a", GroupName: "vip",
		ActualSuccessCount: 10, FinalSuccessCount: 10, SampleCount: 10,
	}).Error)
	require.NoError(t, db.Create(&model.Log{
		CreatedAt: now, Type: model.LogTypeError, ChannelId: 1510,
		ModelName: "model-a", RequestId: "runtime-probe-error", Other: `{"status_code":503}`,
	}).Error)

	runtimeError := types.NewErrorWithStatusCode(errors.New("上游返回 503"), types.ErrorCodeGetChannelFailed, 503)
	protectChannelSmartScheduleRuntimeFailure(1510, "model-a", runtimeError)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1510, GroupName: "vip", ModelName: "model-a",
	}).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, state.StabilityState)
	assert.Zero(t, state.StabilityReleaseMaxPromptTokens)

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1510, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)

	for index, sampleTime := range []int64{now - 20, now - 10} {
		_, err := model.SaveChannelSmartScheduleModelSample(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 1510, Model: "model-a",
			Source:      model.ChannelSmartScheduleSampleSourceManualTest,
			SampleId:    fmt.Sprintf("runtime-probe-sample-%d", index),
			WindowStart: now - 3600, Time: sampleTime, Success: true,
		})
		require.NoError(t, err)
	}

	protectChannelSmartScheduleRuntimeFailure(1510, "model-a", runtimeError)
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1510, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
}

func TestProtectChannelSmartScheduleRuntimeFailureCountsEachPersistedLiveErrorOnce(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	originalErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 3, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	priority := int64(100)
	weight := uint(2)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1505, Name: "runtime live sample gate", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1505, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1505, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		BaseRank: 2, BasePriority: 20, BaseWeight: 40,
		TemporaryTrafficKind:  model.ChannelSmartScheduleTemporaryTrafficExploration,
		TemporaryTrafficSince: common.GetTimestamp() - 30,
	}).Error)

	now := common.GetTimestamp()
	require.NoError(t, db.Create(&[]model.Log{
		{
			CreatedAt: now, Type: model.LogTypeConsume, ChannelId: 1505, ModelName: "model-a",
			RequestId: "runtime-live-success",
		},
		{
			CreatedAt: now, Type: model.LogTypeError, ChannelId: 1505, ModelName: "model-a",
			RequestId: "runtime-live-error-1", Other: `{"status_code":503}`,
		},
		{
			CreatedAt: now, Type: model.LogTypeError, ChannelId: 1505, ModelName: "model-a",
			RequestId: "runtime-live-rate-limit", Other: `{"status_code":429}`,
		},
		{
			CreatedAt: now, Type: model.LogTypeError, ChannelId: 1505, ModelName: "model-a",
			RequestId: "runtime-live-channel-test", Other: `{"status_code":503,"channel_monitor_channel_test":true}`,
		},
		{
			CreatedAt: now, Type: model.LogTypeError, ChannelId: 1505, ModelName: "model-a",
			RequestId: "runtime-live-final-summary", Other: `{"status_code":503,"channel_monitor_final_retry_summary":true}`,
		},
	}).Error)

	runtimeError := types.NewErrorWithStatusCode(errors.New("上游返回 503"), types.ErrorCodeGetChannelFailed, 503)
	protectChannelSmartScheduleRuntimeFailure(1505, "model-a", runtimeError)
	protectChannelSmartScheduleRuntimeFailure(1505, "model-a", runtimeError)

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1505, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priority, *ability.Priority)
	assert.Equal(t, weight, ability.Weight)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("original_model", "model-a")
	c.Set("group", "vip")
	c.Set(common.RequestIdKey, "runtime-live-error-2")
	processChannelErrorWithTiming(
		c,
		*types.NewChannelError(1505, 1, "runtime live sample gate", false, "", false),
		runtimeError,
		false,
		nil,
		false,
	)

	require.NoError(t, db.Where(&model.Ability{ChannelId: 1505, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
}

func TestProtectChannelSmartScheduleRuntimeFailureMatchesParameterizedModelRouteAndLiveLog(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	const routeModel = "gemini-2.5-pro-thinking-*"
	const requestModel = "gemini-2.5-pro-thinking-2048"
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{routeModel}, 1, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	priority := int64(100)
	weight := uint(2)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1507, Name: "parameterized runtime route", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: routeModel, Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1507, Group: "vip", Model: routeModel, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1507, GroupName: "vip", ModelName: routeModel,
		ParticipationSet: true, Revision: 1,
		BaseRank: 2, BasePriority: 20, BaseWeight: 40,
		TemporaryTrafficKind: model.ChannelSmartScheduleTemporaryTrafficExploration,
	}).Error)
	require.NoError(t, db.Create(&model.Log{
		CreatedAt: common.GetTimestamp(), Type: model.LogTypeError,
		ChannelId: 1507, ModelName: requestModel, RequestId: "runtime-parameterized-error",
		Other: `{"status_code":503}`,
	}).Error)

	runtimeError := types.NewErrorWithStatusCode(errors.New("上游返回 503"), types.ErrorCodeGetChannelFailed, 503)
	protectChannelSmartScheduleRuntimeFailure(1507, requestModel, runtimeError)

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 1507, Group: "vip", Model: routeModel,
	}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1507, GroupName: "vip", ModelName: routeModel,
	}).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, state.StabilityState)
}

func TestProtectChannelSmartScheduleRuntimeFailurePrefersExactParameterizedRoute(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	const exactModel = "gemini-2.5-pro-thinking-2048"
	const wildcardModel = "gemini-2.5-pro-thinking-*"
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{exactModel}, 1, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	priority := int64(100)
	weight := uint(2)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1509, Name: "exact parameterized runtime route", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1509, Group: "vip", Model: exactModel, Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1509, Group: "vip", Model: wildcardModel, Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{
			ChannelId: 1509, GroupName: "vip", ModelName: exactModel, ParticipationSet: true,
			BasePriority: priority, BaseWeight: weight,
			TemporaryTrafficKind: model.ChannelSmartScheduleTemporaryTrafficExploration,
		},
		{
			ChannelId: 1509, GroupName: "vip", ModelName: wildcardModel, ParticipationSet: true,
			BasePriority: priority, BaseWeight: weight,
			TemporaryTrafficKind: model.ChannelSmartScheduleTemporaryTrafficExploration,
		},
	}).Error)
	require.NoError(t, db.Create(&model.Log{
		CreatedAt: common.GetTimestamp(), Type: model.LogTypeError,
		ChannelId: 1509, ModelName: exactModel, RequestId: "runtime-exact-parameterized-error",
		Other: `{"status_code":503}`,
	}).Error)

	runtimeError := types.NewErrorWithStatusCode(errors.New("上游返回 503"), types.ErrorCodeGetChannelFailed, 503)
	protectChannelSmartScheduleRuntimeFailure(1509, exactModel, runtimeError)

	var exactAbility model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 1509, Group: "vip", Model: exactModel,
	}).First(&exactAbility).Error)
	require.NotNil(t, exactAbility.Priority)
	assert.Zero(t, *exactAbility.Priority)
	assert.Zero(t, exactAbility.Weight)
	var wildcardAbility model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 1509, Group: "vip", Model: wildcardModel,
	}).First(&wildcardAbility).Error)
	require.NotNil(t, wildcardAbility.Priority)
	assert.Equal(t, priority, *wildcardAbility.Priority)
	assert.Equal(t, weight, wildcardAbility.Weight)

	var exactState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1509, GroupName: "vip", ModelName: exactModel,
	}).First(&exactState).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, exactState.StabilityState)
	var wildcardState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1509, GroupName: "vip", ModelName: wildcardModel,
	}).First(&wildcardState).Error)
	assert.Empty(t, wildcardState.StabilityState)
	assert.Equal(t, model.ChannelSmartScheduleTemporaryTrafficExploration, wildcardState.TemporaryTrafficKind)
}

func TestProtectChannelSmartScheduleRuntimeFailureIgnoresNormalScheduledRoute(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 1, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})
	priority := int64(10)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1506, Name: "normal route", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1506, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: 100,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1506, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
	}).Error)
	_, err := model.SaveChannelSmartScheduleModelSample(model.ChannelSmartScheduleModelSampleResult{
		ChannelId: 1506, Model: "model-a",
		Source:   model.ChannelSmartScheduleSampleSourceManualTest,
		SampleId: "normal-route-sample", Time: common.GetTimestamp(), Success: true,
	})
	require.NoError(t, err)

	runtimeError := types.NewErrorWithStatusCode(errors.New("上游返回 503"), types.ErrorCodeGetChannelFailed, 503)
	protectChannelSmartScheduleRuntimeFailure(1506, "model-a", runtimeError)

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1506, Group: "vip", Model: "model-a"}).First(&ability).Error)
	assert.Equal(t, int64(10), *ability.Priority)
	assert.Equal(t, uint(100), ability.Weight)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1506, GroupName: "vip", ModelName: "model-a",
	}).First(&state).Error)
	assert.Empty(t, state.StabilityState)
}

func TestProtectChannelSmartScheduleRuntimeFailureIgnoresModelRemovedFromPolicy(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-b"}, 1, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})
	priority := int64(100)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1508, Name: "removed policy model", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1508, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: 2,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1508, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, TemporaryTrafficKind: model.ChannelSmartScheduleTemporaryTrafficExploration,
	}).Error)

	runtimeError := types.NewErrorWithStatusCode(errors.New("上游返回 503"), types.ErrorCodeGetChannelFailed, 503)
	protectChannelSmartScheduleRuntimeFailure(1508, "model-a", runtimeError)

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1508, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priority, *ability.Priority)
	assert.Equal(t, uint(2), ability.Weight)
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

func TestProtectChannelSmartScheduleRuntimeFailureDoesNotCoolDownExcludedRoute(t *testing.T) {
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
	require.NoError(t, db.Create(&model.Channel{
		Id: 1510, Name: "excluded rate limit", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1510, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 20,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1510, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Excluded: true,
	}).Error)

	rateLimitError := types.NewErrorWithStatusCode(errors.New("上游并发已满"), types.ErrorCodeGetChannelFailed, 429)
	protectChannelSmartScheduleRuntimeFailure(1510, "model-a", rateLimitError)

	assert.Zero(t, service.ChannelRateLimitCooldownUntil(1510, "model-a"))
}

func TestProtectChannelSmartScheduleRuntimeFailureDoesNotCoolDownRouteOutsidePolicyModels(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-b"}, 1, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:           "true",
		channelMonitorSmartScheduleGroupPoliciesOption:     channelSmartScheduleTestGroupPoliciesJSON(t, policy),
		channelMonitorSmartScheduleRateLimitCooldownOption: "30",
	})
	priority := int64(100)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1511, Name: "unconfigured model rate limit", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1511, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 20,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1511, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)

	rateLimitError := types.NewErrorWithStatusCode(errors.New("上游并发已满"), types.ErrorCodeGetChannelFailed, 429)
	protectChannelSmartScheduleRuntimeFailure(1511, "model-a", rateLimitError)

	assert.Zero(t, service.ChannelRateLimitCooldownUntil(1511, "model-a"))
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

func TestProtectChannelSmartScheduleRuntimeFailureRejects429FromStaleConfiguration(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	require.NoError(t, db.Create(&model.Option{
		Key: model.ChannelSmartScheduleControlRevisionOption, Value: "revision-current",
	}).Error)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 1, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:           "true",
		channelMonitorSmartScheduleGroupPoliciesOption:     channelSmartScheduleTestGroupPoliciesJSON(t, policy),
		channelMonitorSmartScheduleRateLimitCooldownOption: "30",
		channelMonitorSmartScheduleControlRevisionOption:   "revision-stale",
	})
	priority := int64(100)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1506, Name: "stale configuration rate limit", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1506, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 20,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1506, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)

	rateLimitError := types.NewErrorWithStatusCode(errors.New("上游并发已满"), types.ErrorCodeGetChannelFailed, 429)
	protectChannelSmartScheduleRuntimeFailure(1506, "model-a", rateLimitError)

	assert.Zero(t, service.ChannelRateLimitCooldownUntil(1506, "model-a"))
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
