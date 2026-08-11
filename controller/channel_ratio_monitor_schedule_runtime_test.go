package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetChannelSmartScheduleRuntimeHealthForTest() {
	for index := range channelSmartScheduleRuntimeHealth {
		shard := &channelSmartScheduleRuntimeHealth[index]
		shard.Lock()
		shard.database = model.DB
		shard.states = make(
			map[channelSmartScheduleRuntimeHealthKey]channelSmartScheduleRuntimeHealthState,
		)
		shard.Unlock()
	}
}

func channelSmartScheduleRuntimeHealthStateCountForTest() int {
	count := 0
	for index := range channelSmartScheduleRuntimeHealth {
		shard := &channelSmartScheduleRuntimeHealth[index]
		shard.Lock()
		count += len(shard.states)
		shard.Unlock()
	}
	return count
}

func TestProtectChannelSmartScheduleRuntimeFailureIgnoresMinimumSamples(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 100, 80, 30,
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

	runtimeError := types.NewErrorWithStatusCode(errors.New("上游返回 503"), types.ErrorCodeGetChannelFailed, 503)
	protectChannelSmartScheduleRuntimeFailure(1501, "model-a", runtimeError)
	assert.Zero(t, service.ChannelRateLimitCooldownUntil(1501, "model-a"))

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1501, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priority, *ability.Priority)
	assert.Equal(t, weight, ability.Weight)

	now := common.GetTimestamp()
	protectChannelSmartScheduleRuntimeFailure(1501, "model-a", runtimeError)
	assert.Greater(t, service.ChannelRateLimitCooldownUntil(1501, "model-a"), now)
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1501, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
}

func TestAdaptiveRefreshQueueCoalescesDuplicatesAndRetainsInProcessSuccessor(t *testing.T) {
	require.Eventually(t, func() bool {
		channelSmartScheduleAdaptiveRefreshQueue.Lock()
		defer channelSmartScheduleAdaptiveRefreshQueue.Unlock()
		return !channelSmartScheduleAdaptiveRefreshQueue.running
	}, 3*time.Second, 10*time.Millisecond)

	event := channelSmartScheduleAdaptiveRefreshEvent{
		database: model.DB, channelId: 1001, modelName: "model-a",
	}
	channelSmartScheduleAdaptiveRefreshQueue.Lock()
	channelSmartScheduleAdaptiveRefreshQueue.running = true
	channelSmartScheduleAdaptiveRefreshQueue.pending = make(map[channelSmartScheduleAdaptiveRefreshEvent]struct{})
	channelSmartScheduleAdaptiveRefreshQueue.Unlock()
	t.Cleanup(func() {
		channelSmartScheduleAdaptiveRefreshQueue.Lock()
		channelSmartScheduleAdaptiveRefreshQueue.running = false
		channelSmartScheduleAdaptiveRefreshQueue.pending = make(map[channelSmartScheduleAdaptiveRefreshEvent]struct{})
		channelSmartScheduleAdaptiveRefreshQueue.Unlock()
	})

	enqueueChannelSmartScheduleAdaptiveRefresh(event.channelId, event.modelName)
	enqueueChannelSmartScheduleAdaptiveRefresh(event.channelId, event.modelName)
	channelSmartScheduleAdaptiveRefreshQueue.Lock()
	assert.Equal(t, map[channelSmartScheduleAdaptiveRefreshEvent]struct{}{event: {}}, channelSmartScheduleAdaptiveRefreshQueue.pending)
	channelSmartScheduleAdaptiveRefreshQueue.pending = make(map[channelSmartScheduleAdaptiveRefreshEvent]struct{})
	channelSmartScheduleAdaptiveRefreshQueue.Unlock()

	// The first batch has been drained and is now processing. A matching event
	// must remain pending so the worker performs one successor refresh.
	enqueueChannelSmartScheduleAdaptiveRefresh(event.channelId, event.modelName)
	channelSmartScheduleAdaptiveRefreshQueue.Lock()
	assert.Equal(t, map[channelSmartScheduleAdaptiveRefreshEvent]struct{}{event: {}}, channelSmartScheduleAdaptiveRefreshQueue.pending)
	channelSmartScheduleAdaptiveRefreshQueue.Unlock()
}

func TestFullSchedulePreservesRuntimeOverlayAndQueuesOnePoolReplay(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.Eventually(t, func() bool {
		channelSmartScheduleAdaptiveRefreshQueue.Lock()
		defer channelSmartScheduleAdaptiveRefreshQueue.Unlock()
		return !channelSmartScheduleAdaptiveRefreshQueue.running
	}, 3*time.Second, 10*time.Millisecond)
	channelSmartScheduleAdaptiveRefreshQueue.Lock()
	channelSmartScheduleAdaptiveRefreshQueue.running = true
	channelSmartScheduleAdaptiveRefreshQueue.pending = make(map[channelSmartScheduleAdaptiveRefreshEvent]struct{})
	channelSmartScheduleAdaptiveRefreshQueue.Unlock()
	t.Cleanup(func() {
		channelSmartScheduleAdaptiveRefreshQueue.Lock()
		channelSmartScheduleAdaptiveRefreshQueue.running = false
		channelSmartScheduleAdaptiveRefreshQueue.pending = make(map[channelSmartScheduleAdaptiveRefreshEvent]struct{})
		channelSmartScheduleAdaptiveRefreshQueue.Unlock()
	})

	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 2, 80, 30,
	)
	sampleMode := channelMonitorSmartScheduleSampleTraffic
	adaptiveSamplingEnabled := false
	policy.SampleMode = &sampleMode
	policy.AdaptiveSamplingEnabled = &adaptiveSamplingEnabled
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	primaryBasePriority := int64(80)
	sampledBasePriority := int64(20)
	protectedBasePriority := int64(10)
	baseWeight := uint(1000)
	overlayPriority := int64(80)
	degradedPriority := int64(0)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1631, Name: "overlay primary", Status: common.ChannelStatusEnabled, Priority: &primaryBasePriority, Weight: &baseWeight},
		{Id: 1632, Name: "overlay sample", Status: common.ChannelStatusEnabled, Priority: &sampledBasePriority, Weight: &baseWeight},
		{Id: 1633, Name: "hard protected", Status: common.ChannelStatusEnabled, Priority: &protectedBasePriority, Weight: &baseWeight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1631, Group: "vip", Model: "model-a", Enabled: true, Priority: &overlayPriority, Weight: 9700},
		{ChannelId: 1632, Group: "vip", Model: "model-a", Enabled: true, Priority: &overlayPriority, Weight: 300},
		{ChannelId: 1633, Group: "vip", Model: "model-a", Enabled: true, Priority: &degradedPriority, Weight: 0},
	}).Error)
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{
			ChannelId: 1631, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
			Revision: 1, BaseRank: 1, BasePriority: primaryBasePriority, BaseWeight: baseWeight,
		},
		{
			ChannelId: 1632, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
			Revision: 1, BaseRank: 2, BasePriority: sampledBasePriority, BaseWeight: baseWeight,
			TemporaryTrafficKind:  model.ChannelSmartScheduleTemporaryTrafficExploration,
			TemporaryTrafficSince: now - 10, TemporaryTrafficTargetPercent: 3,
			ExplorationMaxPromptTokens: model.DefaultChannelSmartScheduleExplorationMaxPromptTokens,
			SamplingDebt:               2, SamplingCandidate: true,
		},
		{
			ChannelId: 1633, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
			Revision: 1, BaseRank: 3, BasePriority: protectedBasePriority, BaseWeight: baseWeight,
			StabilityState: model.ChannelSmartScheduleStabilityDegraded,
			StabilityUntil: now + 1800, RuntimeProtectionUntil: now + 1800,
			StabilitySavedPriority: protectedBasePriority, StabilitySavedWeight: baseWeight,
		},
	}).Error)

	_, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	var abilities []model.Ability
	require.NoError(t, db.Where(&model.Ability{Group: "vip", Model: "model-a"}).
		Order("channel_id ASC").Find(&abilities).Error)
	require.Len(t, abilities, 3)
	assert.Equal(t, overlayPriority, modelAbilityPriority(t, abilities[0]))
	assert.Equal(t, uint(9700), abilities[0].Weight)
	assert.Equal(t, overlayPriority, modelAbilityPriority(t, abilities[1]))
	assert.Equal(t, uint(300), abilities[1].Weight)
	assert.Zero(t, modelAbilityPriority(t, abilities[2]))
	assert.Zero(t, abilities[2].Weight)

	var states []model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("group_name = ? AND model_name = ?", "vip", "model-a").
		Order("channel_id ASC").Find(&states).Error)
	require.Len(t, states, 3)
	assert.Equal(t, int64(2), states[0].BasePriority)
	assert.Equal(t, int64(1), states[1].BasePriority)
	assert.Equal(t, model.ChannelSmartScheduleTemporaryTrafficExploration, states[1].TemporaryTrafficKind)
	assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, states[2].StabilityState)

	forcedEvent := channelSmartScheduleAdaptiveRefreshEvent{
		database: model.DB, group: "vip", modelName: "model-a",
	}
	channelSmartScheduleAdaptiveRefreshQueue.Lock()
	assert.Equal(t, map[channelSmartScheduleAdaptiveRefreshEvent]struct{}{forcedEvent: {}}, channelSmartScheduleAdaptiveRefreshQueue.pending)
	events := make([]channelSmartScheduleAdaptiveRefreshEvent, 0, len(channelSmartScheduleAdaptiveRefreshQueue.pending))
	for event := range channelSmartScheduleAdaptiveRefreshQueue.pending {
		events = append(events, event)
	}
	channelSmartScheduleAdaptiveRefreshQueue.pending = make(map[channelSmartScheduleAdaptiveRefreshEvent]struct{})
	channelSmartScheduleAdaptiveRefreshQueue.Unlock()
	processChannelSmartScheduleAdaptiveRefreshEvents(events)

	require.NoError(t, db.Where(&model.Ability{Group: "vip", Model: "model-a"}).
		Order("channel_id ASC").Find(&abilities).Error)
	require.Len(t, abilities, 3)
	assert.Equal(t, int64(2), modelAbilityPriority(t, abilities[0]))
	assert.Equal(t, uint(9700), abilities[0].Weight)
	assert.Equal(t, int64(2), modelAbilityPriority(t, abilities[1]))
	assert.Equal(t, uint(300), abilities[1].Weight)
	assert.Zero(t, modelAbilityPriority(t, abilities[2]))
	assert.Zero(t, abilities[2].Weight)
}

func modelAbilityPriority(t *testing.T, ability model.Ability) int64 {
	t.Helper()
	require.NotNil(t, ability.Priority)
	return *ability.Priority
}

func TestStabilityReleaseTimerAdvancesExpiredDegradedRouteWithoutDegradedProbe(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	const revision = "stability-release-v1"
	require.NoError(t, db.Create(&model.Option{
		Key: model.ChannelSmartScheduleControlRevisionOption, Value: revision,
	}).Error)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 2, 80, 30,
	)
	degradedProbeEnabled := false
	releaseMaxPromptTokens := 2_000
	policy.DegradedProbeEnabled = &degradedProbeEnabled
	policy.StabilityReleaseMaxPromptTokens = &releaseMaxPromptTokens
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:         "true",
		channelMonitorSmartScheduleControlRevisionOption: revision,
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			policy,
		),
	})
	channelPriority := int64(80)
	channelWeight := uint(100)
	degradedPriority := int64(0)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1621, Name: "expired degraded", Status: common.ChannelStatusEnabled,
		Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1621, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &degradedPriority, Weight: 0,
	}).Error)
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1621, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		Revision: 1, BaseRank: 1, BasePriority: 80, BaseWeight: 100,
		StabilityState: model.ChannelSmartScheduleStabilityDegraded,
		StabilityUntil: now, RuntimeProtectionUntil: now,
		StabilitySavedPriority: 80, StabilitySavedWeight: 100,
	}).Error)

	require.NoError(t, runChannelSmartScheduleStabilityReleaseOnce(now))
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1621, "vip", "model-a",
	).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityProbing, state.StabilityState)
	assert.Zero(t, state.StabilityUntil)
	assert.Equal(t, now, state.StabilitySince)
	assert.Zero(t, state.RuntimeProtectionUntil)
	assert.Equal(t, releaseMaxPromptTokens, state.StabilityReleaseMaxPromptTokens)
	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 1621, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Equal(t, uint(10), ability.Weight)
	require.Eventually(t, func() bool {
		channelSmartScheduleAdaptiveRefreshQueue.Lock()
		defer channelSmartScheduleAdaptiveRefreshQueue.Unlock()
		return !channelSmartScheduleAdaptiveRefreshQueue.running
	}, 3*time.Second, 10*time.Millisecond)
}

func TestRuntimeRefreshRecoversProbingRouteWithStabilityOnlyPolicy(t *testing.T) {
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
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 2, 80, 30,
	)
	jitterEnabled := false
	adaptiveSamplingEnabled := false
	recoveryStabilityScore := 90.0
	policy.JitterEnabled = &jitterEnabled
	policy.AdaptiveSamplingEnabled = &adaptiveSamplingEnabled
	policy.RecoveryStabilityScore = &recoveryStabilityScore
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	now := common.GetTimestamp()
	channelPriority := int64(80)
	channelWeight := uint(1000)
	probePriority := int64(0)
	restorePriority := int64(90)
	restoreWeight := uint(70)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1622, Name: "stability-only recovery", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1622, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &probePriority, Weight: 10,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1622, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		BaseRank: 1, BasePriority: channelPriority, BaseWeight: channelWeight,
		StabilityState: model.ChannelSmartScheduleStabilityProbing,
		StabilitySince: now - 30, StabilitySavedPriority: restorePriority,
		StabilitySavedWeight: restoreWeight, StabilityReleaseMaxPromptTokens: 2048,
	}).Error)
	for index, sampleTime := range []int64{now - 20, now - 10} {
		_, err := model.SaveChannelSmartScheduleModelSample(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 1622, Model: "model-a", Source: model.ChannelSmartScheduleSampleSourceManualTest,
			SampleId:    fmt.Sprintf("stability-only-%d", index),
			WindowStart: now - 60, Time: sampleTime, Success: true,
		})
		require.NoError(t, err)
	}

	conflict, err := refreshChannelSmartScheduleAdaptivePool(
		context.Background(),
		channelSmartScheduleRoutePoolKey{group: "vip", model: "model-a"},
		policy.policy(), "stale-control-revision", model.DB,
	)
	require.NoError(t, err)
	assert.True(t, conflict)
	var protectedState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1622, GroupName: "vip", ModelName: "model-a",
	}).First(&protectedState).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityProbing, protectedState.StabilityState)

	processChannelSmartScheduleAdaptiveRefreshEvents([]channelSmartScheduleAdaptiveRefreshEvent{{
		database: model.DB, channelId: 1622, modelName: "model-a",
	}})
	var recoveredState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1622, GroupName: "vip", ModelName: "model-a",
	}).First(&recoveredState).Error)
	assert.Empty(t, recoveredState.StabilityState)
	assert.Zero(t, recoveredState.StabilityReleaseMaxPromptTokens)
	assert.Equal(t, model.ChannelSmartScheduleStatusSucceeded, recoveredState.LastScheduleStatus)
	assert.Contains(t, recoveredState.LastScheduleError, "滚动稳定性得分达到恢复阈值")

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 1622, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, restorePriority, *ability.Priority)
	assert.Equal(t, restoreWeight, ability.Weight)
	var sampleState model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ?", 1622, "model-a",
	).First(&sampleState).Error)
	assert.GreaterOrEqual(t, sampleState.ObservationSince, now)

	require.Eventually(t, func() bool {
		channelSmartScheduleAdaptiveRefreshQueue.Lock()
		defer channelSmartScheduleAdaptiveRefreshQueue.Unlock()
		return !channelSmartScheduleAdaptiveRefreshQueue.running
	}, 3*time.Second, 20*time.Millisecond)
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

func TestChannelSmartScheduleRuntimeSuccessResetsOnlyConsecutiveFailures(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	const (
		channelID = 1591
		revision  = "runtime-health-success"
	)

	failure := observeChannelSmartScheduleRuntimeFailure(channelID, "model-a", 100, 30, revision)
	assert.Equal(t, 1, failure.ConsecutiveFailures)

	requestSuccess := observeChannelSmartScheduleRuntimeSuccess(
		channelID, "model-a", 101, revision,
	)
	assert.Zero(t, requestSuccess.ConsecutiveFailures)
	assert.Len(t, requestSuccess.FailureTimes, 1)
}

func TestChannelSmartScheduleRuntimeWindowFailureThresholdUsesRecentRequestPercentage(t *testing.T) {
	snapshot := channelSmartScheduleRuntimeHealthSnapshot{
		RequestEvents: []channelSmartScheduleRuntimeRequestEvent{
			{Timestamp: 100, Failure: true},
			{Timestamp: 101},
			{Timestamp: 102},
			{Timestamp: 103},
		},
	}

	reached, failures, requests := channelSmartScheduleRuntimeWindowFailureThresholdReached(
		snapshot, 104, 60, 4, 25,
	)
	assert.True(t, reached)
	assert.Equal(t, 1, failures)
	assert.Equal(t, 4, requests)

	reached, failures, requests = channelSmartScheduleRuntimeWindowFailureThresholdReached(
		snapshot, 104, 60, 4, 25.1,
	)
	assert.False(t, reached)
	assert.Equal(t, 1, failures)
	assert.Equal(t, 4, requests)

	// The cap keeps only the newest requests, so the old failure no longer
	// contributes to the denominator or numerator.
	reached, failures, requests = channelSmartScheduleRuntimeWindowFailureThresholdReached(
		snapshot, 104, 60, 3, 1,
	)
	assert.False(t, reached)
	assert.Zero(t, failures)
	assert.Equal(t, 3, requests)
}

func TestChannelSmartScheduleRuntimeSuccessEntersFailureRateDenominator(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	const revision = "runtime-health-percentage"

	observeChannelSmartScheduleRuntimeFailure(1602, "model-a", 100, 3600, revision)
	requestSuccess := observeChannelSmartScheduleRuntimeSuccess(1602, "model-a", 101, revision)

	reached, failures, requests := channelSmartScheduleRuntimeWindowFailureThresholdReached(
		requestSuccess, 101, 3600, 100, 50,
	)
	assert.True(t, reached)
	assert.Equal(t, 1, failures)
	assert.Equal(t, 2, requests)
}

func TestChannelSmartScheduleRuntimeRequestWindowSharesSuccessAndFailureThroughRedis(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
		require.NoError(t, client.Close())
	})

	const revision = "runtime-health-redis-percentage"
	first := observeChannelSmartScheduleRuntimeFailure(1603, "model-a", 100, 3600, revision)
	assert.Len(t, first.RequestEvents, 1)

	// Drop the process-local state to model a second instance. The success
	// must still be visible through the shared Redis request stream.
	resetChannelSmartScheduleRuntimeHealthForTest()
	shared := observeChannelSmartScheduleRuntimeSuccess(1603, "model-a", 101, revision)
	assert.Len(t, shared.RequestEvents, 2)
	assert.Zero(t, shared.ConsecutiveFailures)

	resetChannelSmartScheduleRuntimeHealthForTest()
	shared = observeChannelSmartScheduleRuntimeFailure(1603, "model-a", 102, 3600, revision)
	reached, failures, requests := channelSmartScheduleRuntimeWindowFailureThresholdReached(
		shared, 102, 3600, 100, 50,
	)
	assert.True(t, reached)
	assert.Equal(t, 2, failures)
	assert.Equal(t, 3, requests)
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

func TestClearChannelSmartScheduleRuntimeHealthDropsRecoveredFailuresAndKeepsLaterFailures(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	const (
		channelID = 1594
		revision  = "runtime-health-recovery"
	)

	observeChannelSmartScheduleRuntimeFailure(channelID, "model-a", 100, 300, revision)
	observeChannelSmartScheduleRuntimeFailure(channelID, "model-a", 110, 300, revision)
	clearChannelSmartScheduleRuntimeHealth(channelID, "model-a", 105)

	snapshot := getChannelSmartScheduleRuntimeHealth(channelID, "model-a", 111, 300, revision)
	assert.Equal(t, []int64{110}, snapshot.FailureTimes)
	assert.Equal(t, 1, snapshot.ConsecutiveFailures)
}

func TestChannelSmartScheduleRuntimeSuccessDoesNotMutatePersistedRecoveryCount(t *testing.T) {
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
	require.NoError(t, db.Create(&model.ChannelSmartScheduleModelSampleState{
		ChannelId: 1593, ModelName: "model-a", RecoverySuccessCount: 2,
	}).Error)

	observeChannelSmartScheduleRuntimeRequestSuccess(1593, "model-a")
	observeChannelSmartScheduleRuntimeProbeSuccess(1593, "model-a")
	var sampleState model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ?", 1593, "model-a",
	).First(&sampleState).Error)
	assert.Equal(t, 2, sampleState.RecoverySuccessCount)
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

func TestProtectChannelSmartScheduleRuntimeFailureSharesBurstWindowThroughRedis(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
		require.NoError(t, client.Close())
	})

	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 100, 80, 30,
	)
	consecutiveFailureThreshold := 100
	burstFailureThreshold := 2
	policy.ConsecutiveFailureThreshold = &consecutiveFailureThreshold
	policy.BurstFailureThreshold = &burstFailureThreshold
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1513, Name: "redis burst failures", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1513, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1513, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)

	runtimeError := types.NewErrorWithStatusCode(errors.New("上游返回 503"), types.ErrorCodeGetChannelFailed, 503)
	protectChannelSmartScheduleRuntimeFailure(1513, "model-a", runtimeError)
	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 1513, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priority, *ability.Priority)
	assert.Equal(t, weight, ability.Weight)

	resetChannelSmartScheduleRuntimeHealthForTest()

	protectChannelSmartScheduleRuntimeFailure(1513, "model-a", runtimeError)

	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 1513, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
}

func TestProtectChannelSmartScheduleRuntimeFailureSkipsIncompleteSharedWindow(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		resetChannelSmartScheduleRuntimeHealthForTest()
		resetChannelSmartScheduleRuntimeRedisSuccessQueueForTest()
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
		require.NoError(t, client.Close())
	})
	resetChannelSmartScheduleRuntimeHealthForTest()
	resetChannelSmartScheduleRuntimeRedisSuccessQueueForTest()

	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 1, 80, 30,
	)
	consecutiveFailureThreshold := 1
	policy.ConsecutiveFailureThreshold = &consecutiveFailureThreshold
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1514, Name: "incomplete redis window", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1514, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1514, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)

	settings := getChannelMonitorRuntimeSettings()
	redisKey := channelSmartScheduleRuntimeFailureRedisKey(
		1514, "model-a", settings.SmartScheduleControlRevision,
	)
	require.NoError(t, client.Set(
		context.Background(), channelSmartScheduleRuntimeRedisIncompleteKey(redisKey), "1", time.Hour,
	).Err())
	runtimeError := types.NewErrorWithStatusCode(
		errors.New("上游返回 503"), types.ErrorCodeGetChannelFailed, 503,
	)

	protectChannelSmartScheduleRuntimeFailure(1514, "model-a", runtimeError)

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 1514, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priority, *ability.Priority)
	assert.Equal(t, weight, ability.Weight)

	require.NoError(t, client.Del(
		context.Background(), channelSmartScheduleRuntimeRedisIncompleteKey(redisKey),
	).Err())
	protectChannelSmartScheduleRuntimeFailure(1514, "model-a", runtimeError)

	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 1514, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
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
	policy.StabilityReleaseMaxPromptTokens = common.GetPointer(1_000)
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
		StabilityReleaseMaxPromptTokens: 1_000,
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

func TestProtectChannelSmartScheduleRuntimeFailureDoesNotRecountPersistedErrors(t *testing.T) {
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
	failureThreshold := 1
	policy.ConsecutiveFailureThreshold = &failureThreshold
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
	failureThreshold := 1
	policy.ConsecutiveFailureThreshold = &failureThreshold
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

func TestProtectChannelSmartScheduleRuntimeFailureWaitsForThresholdOnNormalRoute(t *testing.T) {
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

func TestAdaptiveSamplingRefreshesOnRequestEventAndPreservesBaseSchedule(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	originalRefreshInterval := channelSmartScheduleAdaptiveRefreshMinInterval
	channelSmartScheduleAdaptiveRefreshMinInterval = 50 * time.Millisecond
	channelSmartScheduleAdaptiveRefreshThrottle.Lock()
	channelSmartScheduleAdaptiveRefreshThrottle.database = nil
	channelSmartScheduleAdaptiveRefreshThrottle.lastRun = make(
		map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time,
	)
	channelSmartScheduleAdaptiveRefreshThrottle.scheduled = make(
		map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time,
	)
	channelSmartScheduleAdaptiveRefreshThrottle.Unlock()
	t.Cleanup(func() {
		channelSmartScheduleAdaptiveRefreshMinInterval = originalRefreshInterval
		channelSmartScheduleAdaptiveRefreshThrottle.Lock()
		channelSmartScheduleAdaptiveRefreshThrottle.database = nil
		channelSmartScheduleAdaptiveRefreshThrottle.lastRun = make(
			map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time,
		)
		channelSmartScheduleAdaptiveRefreshThrottle.scheduled = make(
			map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time,
		)
		channelSmartScheduleAdaptiveRefreshThrottle.Unlock()
	})
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	common.LogConsumeEnabled = true
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 5, 80, 30,
	)
	sampleMode := channelMonitorSmartScheduleSampleOff
	policy.SampleMode = &sampleMode
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			policy,
		),
	})

	primaryPriority := int64(100)
	backupPriority := int64(20)
	secondBackupPriority := int64(10)
	primaryWeight := uint(9000)
	backupWeight := uint(100)
	revisionBefore := int64(7)
	lastScheduleTime := int64(12345)
	reason := "完整调度结果"
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1601, Name: "primary", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &primaryPriority, Weight: &primaryWeight},
		{Id: 1602, Name: "backup", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &backupPriority, Weight: &backupWeight},
		{Id: 1603, Name: "second backup", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &secondBackupPriority, Weight: &backupWeight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1601, Group: "vip", Model: "model-a", Enabled: true, Priority: &primaryPriority, Weight: primaryWeight},
		{ChannelId: 1602, Group: "vip", Model: "model-a", Enabled: true, Priority: &backupPriority, Weight: backupWeight},
		{ChannelId: 1603, Group: "vip", Model: "model-a", Enabled: true, Priority: &secondBackupPriority, Weight: backupWeight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{
			ChannelId: 1601, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
			Revision: revisionBefore, BaseRank: 1, BasePriority: primaryPriority, BaseWeight: primaryWeight,
			LastScheduleStatus: model.ChannelSmartScheduleStatusSucceeded,
			LastScheduleError:  reason, LastSchedulePriority: primaryPriority,
			LastScheduleWeight: primaryWeight, LastScheduleTime: lastScheduleTime,
		},
		{
			ChannelId: 1602, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
			Revision: revisionBefore, BaseRank: 2, BasePriority: backupPriority, BaseWeight: backupWeight,
			LastScheduleStatus: model.ChannelSmartScheduleStatusSucceeded,
			LastScheduleError:  reason, LastSchedulePriority: backupPriority,
			LastScheduleWeight: backupWeight, LastScheduleTime: lastScheduleTime,
		},
		{
			ChannelId: 1603, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
			Revision: revisionBefore, BaseRank: 3, BasePriority: secondBackupPriority, BaseWeight: backupWeight,
			LastScheduleStatus: model.ChannelSmartScheduleStatusSucceeded,
			LastScheduleError:  reason, LastSchedulePriority: secondBackupPriority,
			LastScheduleWeight: backupWeight, LastScheduleTime: lastScheduleTime,
		},
	}).Error)
	now := common.GetTimestamp()
	logs := make([]model.Log, 5)
	for index := range logs {
		logs[index] = model.Log{
			ChannelId: 1601, Group: "vip", ModelName: "model-a", Type: model.LogTypeConsume,
			IsStream: true, Other: `{"frt":10000}`, CreatedAt: now - int64(index),
		}
	}
	require.NoError(t, db.Create(&logs).Error)

	activeKey := channelMonitorSmartScheduleTaskType
	runningTask := model.SystemTask{
		TaskID: "adaptive-refresh-running-schedule", Type: channelMonitorSmartScheduleTaskType,
		Status: model.SystemTaskStatusRunning, ActiveKey: &activeKey, LockedBy: "test-runner",
	}
	require.NoError(t, db.Create(&runningTask).Error)
	processChannelSmartScheduleAdaptiveRefreshEvents([]channelSmartScheduleAdaptiveRefreshEvent{{
		database: model.DB, channelId: 1601, modelName: "model-a",
	}})
	var deferredState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1602, "vip", "model-a",
	).First(&deferredState).Error)
	assert.Empty(t, deferredState.TemporaryTrafficKind)
	require.NoError(t, db.Model(&runningTask).Update("status", model.SystemTaskStatusSucceeded).Error)
	require.Eventually(t, func() bool {
		var state model.ChannelSmartScheduleRouteState
		if err := db.Where(
			"channel_id = ? AND group_name = ? AND model_name = ?", 1602, "vip", "model-a",
		).First(&state).Error; err != nil {
			return false
		}
		return state.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficAdaptive
	}, 3*time.Second, 20*time.Millisecond)

	var primaryAbility model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 1601, Group: "vip", Model: "model-a",
	}).First(&primaryAbility).Error)
	require.NotNil(t, primaryAbility.Priority)
	assert.Equal(t, primaryPriority, *primaryAbility.Priority)
	assert.Equal(t, uint(7000), primaryAbility.Weight)
	var backupAbility model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 1602, Group: "vip", Model: "model-a",
	}).First(&backupAbility).Error)
	require.NotNil(t, backupAbility.Priority)
	assert.Equal(t, primaryPriority, *backupAbility.Priority)
	assert.Equal(t, uint(3000), backupAbility.Weight)
	processChannelSmartScheduleAdaptiveRefreshEvents([]channelSmartScheduleAdaptiveRefreshEvent{{
		database: model.DB, channelId: 1601, modelName: "model-a",
	}})
	var secondBackupState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1603, "vip", "model-a",
	).First(&secondBackupState).Error)
	assert.Empty(t, secondBackupState.TemporaryTrafficKind)
	var selectedBackupState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1602, "vip", "model-a",
	).First(&selectedBackupState).Error)
	assert.Equal(t, model.ChannelSmartScheduleTemporaryTrafficAdaptive, selectedBackupState.TemporaryTrafficKind)
	assert.Equal(t, lastScheduleTime, selectedBackupState.LastScheduleTime)
	assert.Equal(t, reason, selectedBackupState.LastScheduleError)
	service.StartChannelRateLimitCooldown(1602, "model-a", 60)
	processChannelSmartScheduleAdaptiveRefreshEvents([]channelSmartScheduleAdaptiveRefreshEvent{{
		database: model.DB, channelId: 1601, modelName: "model-a",
	}})
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1602, "vip", "model-a",
	).First(&selectedBackupState).Error)
	assert.Empty(t, selectedBackupState.TemporaryTrafficKind)
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1603, "vip", "model-a",
	).First(&secondBackupState).Error)
	assert.Equal(t, model.ChannelSmartScheduleTemporaryTrafficAdaptive, secondBackupState.TemporaryTrafficKind)
	service.ClearChannelRateLimitCooldowns()
	protection, err := model.ProtectChannelSmartScheduleRouteOnShortTermFailure(
		1602, "vip", "model-a", common.GetTimestamp()+1800, "测试硬保护",
		getChannelMonitorSettings().SmartScheduleControlRevision,
	)
	require.NoError(t, err)
	assert.True(t, protection.Handled)
	processChannelSmartScheduleAdaptiveRefreshEvents([]channelSmartScheduleAdaptiveRefreshEvent{{
		database: model.DB, channelId: 1601, modelName: "model-a",
	}})
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 1602, Group: "vip", Model: "model-a",
	}).First(&backupAbility).Error)
	require.NotNil(t, backupAbility.Priority)
	assert.Zero(t, *backupAbility.Priority)
	assert.Zero(t, backupAbility.Weight)
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1602, "vip", "model-a",
	).First(&selectedBackupState).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, selectedBackupState.StabilityState)
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1603, "vip", "model-a",
	).First(&secondBackupState).Error)
	assert.Equal(t, model.ChannelSmartScheduleTemporaryTrafficAdaptive, secondBackupState.TemporaryTrafficKind)

	var primaryState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1601, "vip", "model-a",
	).First(&primaryState).Error)
	assert.Equal(t, 1, primaryState.BaseRank)
	assert.Equal(t, primaryPriority, primaryState.BasePriority)
	assert.Equal(t, primaryWeight, primaryState.BaseWeight)
	assert.Equal(t, lastScheduleTime, primaryState.LastScheduleTime)
	assert.Equal(t, reason, primaryState.LastScheduleError)
	assert.Greater(t, primaryState.Revision, revisionBefore)

	oldTimestamp := now - int64(*policy.AdaptiveSamplingWindowSeconds) - 1
	require.NoError(t, db.Model(&model.Log{}).
		Where("channel_id IN ?", []int{1601, 1602, 1603}).
		Update("created_at", oldTimestamp).Error)
	require.NoError(t, db.Create(&model.Log{
		ChannelId: 1601, Group: "vip", ModelName: "model-a", Type: model.LogTypeConsume,
		IsStream: true, Other: `{"frt":100}`, CreatedAt: common.GetTimestamp(),
	}).Error)
	observeChannelSmartScheduleRuntimeRequestSuccess(1601, "model-a")
	require.Eventually(t, func() bool {
		if err := db.Where(&model.Ability{
			ChannelId: 1601, Group: "vip", Model: "model-a",
		}).First(&primaryAbility).Error; err != nil || primaryAbility.Priority == nil {
			return false
		}
		return *primaryAbility.Priority == primaryPriority && primaryAbility.Weight == primaryWeight
	}, 3*time.Second, 20*time.Millisecond)
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 1601, Group: "vip", Model: "model-a",
	}).First(&primaryAbility).Error)
	require.NotNil(t, primaryAbility.Priority)
	assert.Equal(t, primaryPriority, *primaryAbility.Priority)
	assert.Equal(t, primaryWeight, primaryAbility.Weight)
	var backupState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1602, "vip", "model-a",
	).First(&backupState).Error)
	assert.Empty(t, backupState.TemporaryTrafficKind)
	assert.Equal(t, 2, backupState.BaseRank)
	assert.Equal(t, backupPriority, backupState.BasePriority)
	assert.Equal(t, backupWeight, backupState.BaseWeight)
	assert.Greater(t, backupState.LastScheduleTime, lastScheduleTime)
	assert.Equal(t, "测试硬保护", backupState.LastScheduleError)
}

func TestAdaptiveRefreshMaturesCandidateSwitchesPrimaryAndContinuesSamplingInSameEvent(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	common.LogConsumeEnabled = true
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 2, 80, 30,
	)
	sampleMode := channelMonitorSmartScheduleSampleTraffic
	explorationPercent := 3.0
	minimumComparable := 3
	switchConfirmPercent := 95.0
	adaptiveSamplingEnabled := false
	policy.SampleMode = &sampleMode
	policy.ExplorationTrafficPercent = &explorationPercent
	policy.AdaptiveSamplingMinComparableChannels = &minimumComparable
	policy.AdaptiveSamplingSwitchConfirmRequestPercent = &switchConfirmPercent
	policy.AdaptiveSamplingEnabled = &adaptiveSamplingEnabled
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			policy,
		),
	})

	primaryPriority := int64(3)
	matureBasePriority := int64(2)
	nextBasePriority := int64(1)
	baseWeight := uint(1000)
	sampledPriority := int64(3)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1611, Name: "pressured primary", Status: common.ChannelStatusEnabled, Priority: &primaryPriority, Weight: &baseWeight},
		{Id: 1612, Name: "mature backup", Status: common.ChannelStatusEnabled, Priority: &matureBasePriority, Weight: &baseWeight},
		{Id: 1613, Name: "next debt", Status: common.ChannelStatusEnabled, Priority: &nextBasePriority, Weight: &baseWeight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1611, Group: "vip", Model: "model-a", Enabled: true, Priority: &sampledPriority, Weight: 9700},
		{ChannelId: 1612, Group: "vip", Model: "model-a", Enabled: true, Priority: &sampledPriority, Weight: 300},
		{ChannelId: 1613, Group: "vip", Model: "model-a", Enabled: true, Priority: &nextBasePriority, Weight: baseWeight},
	}).Error)
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{
			ChannelId: 1611, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
			Revision: 1, BaseRank: 1, BasePriority: primaryPriority, BaseWeight: baseWeight,
		},
		{
			ChannelId: 1612, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
			Revision: 1, BaseRank: 2, BasePriority: matureBasePriority, BaseWeight: baseWeight,
			TemporaryTrafficKind:  model.ChannelSmartScheduleTemporaryTrafficExploration,
			TemporaryTrafficSince: now - 10, TemporaryTrafficTargetPercent: explorationPercent,
			ExplorationMaxPromptTokens: 16384,
			SamplingDebt:               1, SamplingCandidate: true,
		},
		{
			ChannelId: 1613, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
			Revision: 1, BaseRank: 3, BasePriority: nextBasePriority, BaseWeight: baseWeight,
			SamplingDebt: 2,
		},
	}).Error)
	logs := make([]model.Log, 0, 7)
	for index := 0; index < 5; index++ {
		logs = append(logs, model.Log{
			ChannelId: 1611, Group: "vip", ModelName: "model-a", Type: model.LogTypeConsume,
			IsStream: true, Other: `{"frt":1000}`, CreatedAt: now - int64(index),
		})
	}
	for index := 0; index < 2; index++ {
		logs = append(logs, model.Log{
			ChannelId: 1612, Group: "vip", ModelName: "model-a", Type: model.LogTypeConsume,
			IsStream: true, Other: `{"frt":100}`, CreatedAt: now - int64(index),
		})
	}
	require.NoError(t, db.Create(&logs).Error)

	settings := getChannelMonitorSettings()
	conflict, err := refreshChannelSmartScheduleAdaptivePool(
		context.Background(),
		channelSmartScheduleRoutePoolKey{group: "vip", model: "model-a"},
		policy.policy(),
		settings.SmartScheduleControlRevision,
		model.DB,
	)
	require.NoError(t, err)
	assert.False(t, conflict)
	var insufficientAbilities []model.Ability
	require.NoError(t, db.Where(&model.Ability{Group: "vip", Model: "model-a"}).
		Order("channel_id ASC").Find(&insufficientAbilities).Error)
	require.Len(t, insufficientAbilities, 3)
	require.NotNil(t, insufficientAbilities[0].Priority)
	require.NotNil(t, insufficientAbilities[1].Priority)
	require.NotNil(t, insufficientAbilities[2].Priority)
	assert.Equal(t, int64(3), *insufficientAbilities[0].Priority)
	assert.Equal(t, uint(9700), insufficientAbilities[0].Weight)
	assert.Equal(t, int64(2), *insufficientAbilities[1].Priority)
	assert.Equal(t, uint(1000), insufficientAbilities[1].Weight)
	assert.Equal(t, int64(3), *insufficientAbilities[2].Priority)
	assert.Equal(t, uint(300), insufficientAbilities[2].Weight)

	minimumComparable = 2
	conflict, err = refreshChannelSmartScheduleAdaptivePool(
		context.Background(),
		channelSmartScheduleRoutePoolKey{group: "vip", model: "model-a"},
		policy.policy(),
		settings.SmartScheduleControlRevision,
		model.DB,
	)
	require.NoError(t, err)
	assert.False(t, conflict)

	var abilities []model.Ability
	require.NoError(t, db.Where(&model.Ability{Group: "vip", Model: "model-a"}).
		Order("channel_id ASC").Find(&abilities).Error)
	require.Len(t, abilities, 3)
	require.NotNil(t, abilities[0].Priority)
	require.NotNil(t, abilities[1].Priority)
	require.NotNil(t, abilities[2].Priority)
	assert.Equal(t, int64(2), *abilities[0].Priority)
	assert.Equal(t, uint(1000), abilities[0].Weight)
	assert.Equal(t, int64(3), *abilities[1].Priority)
	assert.Equal(t, uint(9700), abilities[1].Weight)
	assert.Equal(t, int64(3), *abilities[2].Priority)
	assert.Equal(t, uint(300), abilities[2].Weight)

	var states []model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("group_name = ? AND model_name = ?", "vip", "model-a").
		Order("channel_id ASC").Find(&states).Error)
	require.Len(t, states, 3)
	assert.Zero(t, states[1].SamplingDebt)
	assert.False(t, states[1].SamplingCandidate)
	assert.Empty(t, states[1].TemporaryTrafficKind)
	assert.Equal(t, 2, states[2].SamplingDebt)
	assert.True(t, states[2].SamplingCandidate)
	assert.Equal(t, model.ChannelSmartScheduleTemporaryTrafficExploration, states[2].TemporaryTrafficKind)
	assert.Equal(t, explorationPercent, states[2].TemporaryTrafficTargetPercent)
	assert.Equal(t, 1, states[0].BaseRank)
	assert.Equal(t, primaryPriority, states[0].BasePriority)
	assert.Equal(t, 2, states[1].BaseRank)
	assert.Equal(t, matureBasePriority, states[1].BasePriority)
}
