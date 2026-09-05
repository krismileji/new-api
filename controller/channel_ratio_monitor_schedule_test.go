package controller

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func channelSmartScheduleTestGroupPolicy(
	group string,
	strategy string,
	stabilityEnabled bool,
	applyMode string,
	models []string,
	minSamples int,
	recoveryStabilityScore float64,
	cooldownMinutes int,
) channelSmartScheduleGroupPolicy {
	scoring := defaultChannelSmartScheduleScoring()
	stabilityWindowMinutes := 5
	sampleMode := channelMonitorSmartScheduleSampleOff
	samplingOrder := channelMonitorSmartScheduleSamplingOrderPriorityWeight
	explorationTrafficPercent := 3.0
	explorationMaxPromptTokens := model.DefaultChannelSmartScheduleExplorationMaxPromptTokens
	stabilityReleaseMaxPromptTokens := model.DefaultChannelSmartScheduleStabilityReleaseMaxPromptTokens
	probeIntervalMinutes := 10
	adaptiveSamplingEnabled := applyMode == channelMonitorSmartScheduleApplyPriorityWeight
	adaptiveSamplingBasePercent := 3.0
	adaptiveSamplingMaxPercent := 30.0
	adaptiveSamplingErrorWarningPercent := 5.0
	adaptiveSamplingErrorCriticalPercent := 15.0
	adaptiveSamplingFirstTokenWarningSeconds := 5.0
	adaptiveSamplingFirstTokenCriticalSeconds := 10.0
	adaptiveSamplingWindowSeconds := 600
	adaptiveSamplingFirstTokenWarningRequestPercent := 10.0
	adaptiveSamplingRecoverRequestPercent := 95.0
	adaptiveSamplingSwitchConfirmRequestPercent := 95.0
	adaptiveSamplingMinComparableChannels := 2
	recoveryStabilityScore = math.Min(recoveryStabilityScore+5, 100)
	fastFailurePenaltyPercent := 40.0
	fastFailureSeconds := 1.0
	fastFailureSameChannelRetryCount := defaultChannelMonitorSmartScheduleFastFailureSameChannelRetryCount
	fastFailureRetryDelayMs := defaultChannelMonitorSmartScheduleFastRetryDelayMs
	slowFailureSeconds := 10.0
	burstFailureWindowSeconds := defaultChannelMonitorSmartScheduleBurstFailureWindowSeconds
	consecutiveFailureThreshold := defaultChannelMonitorSmartScheduleConsecutiveFailureThreshold
	burstFailureThreshold := defaultChannelMonitorSmartScheduleBurstFailureThreshold
	recoverySuccessThreshold := defaultChannelMonitorSmartScheduleRecoverySuccessThreshold
	jitterEnabled := true
	jitterTolerancePercent := 5.0
	jitterSlowThresholdSeconds := 10.0
	if models == nil {
		models = []string{}
	}
	return channelSmartScheduleGroupPolicy{
		Group:                                           group,
		Strategy:                                        &strategy,
		StabilityEnabled:                                &stabilityEnabled,
		StabilityWindowMinutes:                          &stabilityWindowMinutes,
		Scoring:                                         &scoring,
		ApplyMode:                                       &applyMode,
		Models:                                          &models,
		MinSamples:                                      &minSamples,
		RecoveryStabilityScore:                          &recoveryStabilityScore,
		FastFailurePenaltyPercent:                       &fastFailurePenaltyPercent,
		FastFailureSeconds:                              &fastFailureSeconds,
		FastFailureSameChannelRetryCount:                &fastFailureSameChannelRetryCount,
		FastFailureRetryDelayMs:                         &fastFailureRetryDelayMs,
		SlowFailureSeconds:                              &slowFailureSeconds,
		BurstFailureWindowSeconds:                       &burstFailureWindowSeconds,
		ConsecutiveFailureThreshold:                     &consecutiveFailureThreshold,
		BurstFailureThreshold:                           &burstFailureThreshold,
		RecoverySuccessThreshold:                        &recoverySuccessThreshold,
		JitterEnabled:                                   &jitterEnabled,
		JitterTolerancePercent:                          &jitterTolerancePercent,
		JitterSlowThresholdSeconds:                      &jitterSlowThresholdSeconds,
		CooldownMinutes:                                 &cooldownMinutes,
		SampleMode:                                      &sampleMode,
		SamplingOrder:                                   &samplingOrder,
		ExplorationTrafficPercent:                       &explorationTrafficPercent,
		ExplorationMaxPromptTokens:                      &explorationMaxPromptTokens,
		StabilityReleaseMaxPromptTokens:                 &stabilityReleaseMaxPromptTokens,
		ProbeIntervalMinutes:                            &probeIntervalMinutes,
		AdaptiveSamplingEnabled:                         &adaptiveSamplingEnabled,
		AdaptiveSamplingBasePercent:                     &adaptiveSamplingBasePercent,
		AdaptiveSamplingMaxPercent:                      &adaptiveSamplingMaxPercent,
		AdaptiveSamplingErrorWarningPercent:             &adaptiveSamplingErrorWarningPercent,
		AdaptiveSamplingErrorCriticalPercent:            &adaptiveSamplingErrorCriticalPercent,
		AdaptiveSamplingFirstTokenWarningSeconds:        &adaptiveSamplingFirstTokenWarningSeconds,
		AdaptiveSamplingFirstTokenCriticalSeconds:       &adaptiveSamplingFirstTokenCriticalSeconds,
		AdaptiveSamplingWindowSeconds:                   &adaptiveSamplingWindowSeconds,
		AdaptiveSamplingFirstTokenWarningRequestPercent: &adaptiveSamplingFirstTokenWarningRequestPercent,
		AdaptiveSamplingRecoverRequestPercent:           &adaptiveSamplingRecoverRequestPercent,
		AdaptiveSamplingSwitchConfirmRequestPercent:     &adaptiveSamplingSwitchConfirmRequestPercent,
		AdaptiveSamplingMinComparableChannels:           &adaptiveSamplingMinComparableChannels,
	}
}

func TestNormalizeChannelSmartScheduleGroupPolicyRequiresStabilityWindow(t *testing.T) {
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
	)
	policy.StabilityWindowMinutes = nil

	_, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{policy})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "稳定性评分窗口")
}

func TestNormalizeChannelSmartScheduleGroupPoliciesKeepsIndependentStabilityWindows(t *testing.T) {
	shortWindow := 5
	longWindow := 90
	shortPolicy := channelSmartScheduleTestGroupPolicy(
		"standard", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
	)
	shortPolicy.StabilityWindowMinutes = &shortWindow
	longPolicy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
	)
	longPolicy.StabilityWindowMinutes = &longWindow

	normalized, err := normalizeChannelSmartScheduleGroupPolicies(
		[]channelSmartScheduleGroupPolicy{longPolicy, shortPolicy},
	)
	require.NoError(t, err)
	assert.Equal(t, 90, smartScheduleGroupPolicies(normalized).maxStabilityWindowMinutes())
	assert.Equal(t, 5, normalized[0].policy().StabilityWindowMinutes)
	assert.Equal(t, 90, normalized[1].policy().StabilityWindowMinutes)
}

func TestNormalizeChannelSmartScheduleGroupPolicyRejectsInvalidStabilityWindow(t *testing.T) {
	for _, windowMinutes := range []int{0, maxChannelMonitorSmartScheduleWindowMinutes + 1} {
		policy := channelSmartScheduleTestGroupPolicy(
			"vip", channelMonitorSmartScheduleStrategySmart, true,
			channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
		)
		policy.StabilityWindowMinutes = &windowMinutes

		_, err := normalizeChannelSmartScheduleGroupPolicies(
			[]channelSmartScheduleGroupPolicy{policy},
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "稳定性评分窗口")
	}
}

func TestNormalizeChannelSmartScheduleGroupPolicySamplingOrder(t *testing.T) {
	tests := []struct {
		name       string
		order      *string
		want       string
		wantErrMsg string
	}{
		{name: "priority and weight", order: common.GetPointer(" priority_weight "), want: channelMonitorSmartScheduleSamplingOrderPriorityWeight},
		{name: "cost ratio", order: common.GetPointer(" ratio "), want: channelMonitorSmartScheduleSamplingOrderRatio},
		{name: "missing", wantErrMsg: "必须完整配置"},
		{name: "unsupported", order: common.GetPointer("score"), wantErrMsg: "采样顺序无效"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategySmart, true,
				channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
			)
			policy.SamplingOrder = test.order

			normalized, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{policy})
			if test.wantErrMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.wantErrMsg)
				return
			}
			require.NoError(t, err)
			require.Len(t, normalized, 1)
			require.NotNil(t, normalized[0].SamplingOrder)
			assert.Equal(t, test.want, *normalized[0].SamplingOrder)
			assert.Equal(t, test.want, normalized[0].policy().SamplingOrder)
		})
	}
}

func TestNormalizeChannelSmartScheduleGroupPolicyRequiresAdaptiveSamplingControls(t *testing.T) {
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
	)
	policy.AdaptiveSamplingMaxPercent = nil

	_, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{policy})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "必须完整配置")
}

func TestNormalizeChannelSmartScheduleGroupPolicyValidatesAdaptiveSamplingCombinations(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*channelSmartScheduleGroupPolicy)
		message string
	}{
		{
			name: "base exceeds maximum",
			prepare: func(policy *channelSmartScheduleGroupPolicy) {
				maximum := 2.0
				base := 3.0
				policy.AdaptiveSamplingMaxPercent = &maximum
				policy.AdaptiveSamplingBasePercent = &base
			},
			message: "基础预算不能大于最大预算",
		},
		{
			name: "weight mode cannot enable adaptive sampling",
			prepare: func(policy *channelSmartScheduleGroupPolicy) {
				mode := channelMonitorSmartScheduleApplyWeight
				policy.ApplyMode = &mode
			},
			message: "仅支持按优先级和权重调整",
		},
		{
			name: "adaptive window is bounded",
			prepare: func(policy *channelSmartScheduleGroupPolicy) {
				value := maxChannelMonitorSmartScheduleAdaptiveSamplingWindowSeconds + 1
				policy.AdaptiveSamplingWindowSeconds = &value
			},
			message: "统计窗口必须在 60 到 3600 秒之间",
		},
		{
			name: "first-token warning request percentage is bounded",
			prepare: func(policy *channelSmartScheduleGroupPolicy) {
				value := 0.0
				policy.AdaptiveSamplingFirstTokenWarningRequestPercent = &value
			},
			message: "请求占比阈值必须大于 0% 且不超过 100%",
		},
		{
			name: "switch confirmation cannot be below recovery",
			prepare: func(policy *channelSmartScheduleGroupPolicy) {
				value := 90.0
				policy.AdaptiveSamplingSwitchConfirmRequestPercent = &value
			},
			message: "切换确认请求占比不能低于恢复请求占比",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategySmart, true,
				channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
			)
			test.prepare(&policy)

			_, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{policy})
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.message)
		})
	}
}

func TestNormalizeChannelSmartScheduleGroupPolicyDefaultsAndValidatesFastFailureSameChannelRetries(t *testing.T) {
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
	)
	policy.FastFailureSameChannelRetryCount = nil
	policy.FastFailureRetryDelayMs = nil

	normalized, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{policy})
	require.NoError(t, err)
	require.Len(t, normalized, 1)
	require.NotNil(t, normalized[0].FastFailureSameChannelRetryCount)
	assert.Equal(t, defaultChannelMonitorSmartScheduleFastFailureSameChannelRetryCount, *normalized[0].FastFailureSameChannelRetryCount)
	require.NotNil(t, normalized[0].FastFailureRetryDelayMs)
	assert.Equal(t, defaultChannelMonitorSmartScheduleFastRetryDelayMs, *normalized[0].FastFailureRetryDelayMs)

	for _, value := range []int{-1, maxChannelMonitorSmartScheduleFastFailureSameChannelRetryCount + 1} {
		invalidPolicy := channelSmartScheduleTestGroupPolicy(
			"vip", channelMonitorSmartScheduleStrategySmart, true,
			channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
		)
		invalidPolicy.FastFailureSameChannelRetryCount = &value
		_, err = normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{invalidPolicy})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "快速失败同渠道重试次数")
	}
	for _, value := range []int{-1, maxChannelMonitorSmartScheduleFastRetryDelayMs + 1} {
		invalidPolicy := channelSmartScheduleTestGroupPolicy(
			"vip", channelMonitorSmartScheduleStrategySmart, true,
			channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
		)
		invalidPolicy.FastFailureRetryDelayMs = &value
		_, err = normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{invalidPolicy})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "快速失败同渠道重试延迟")
	}
}

func TestNormalizeChannelSmartScheduleGroupPolicyRequiresAdaptivePercentages(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*channelSmartScheduleGroupPolicy)
	}{
		{
			name: "first-token warning request percent",
			prepare: func(policy *channelSmartScheduleGroupPolicy) {
				policy.AdaptiveSamplingFirstTokenWarningRequestPercent = nil
			},
		},
		{
			name: "recover request percent",
			prepare: func(policy *channelSmartScheduleGroupPolicy) {
				policy.AdaptiveSamplingRecoverRequestPercent = nil
			},
		},
		{
			name: "switch confirm request percent",
			prepare: func(policy *channelSmartScheduleGroupPolicy) {
				policy.AdaptiveSamplingSwitchConfirmRequestPercent = nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategySmart, true,
				channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
			)
			test.prepare(&policy)

			_, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{policy})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "必须完整配置")
		})
	}
}

func TestNormalizeChannelSmartScheduleGroupPolicyValidatesFirstTokenWarningRequestPercent(t *testing.T) {
	tests := []struct {
		name          string
		value         float64
		recover       float64
		switchConfirm float64
		wantErr       bool
	}{
		{name: "positive fraction", value: 0.1, recover: 100, switchConfirm: 100},
		{name: "upper boundary", value: 100},
		{name: "zero", value: 0, wantErr: true},
		{name: "negative", value: -0.1, wantErr: true},
		{name: "above upper boundary", value: 100.1, wantErr: true},
		{name: "nan", value: math.NaN(), wantErr: true},
		{name: "positive infinity", value: math.Inf(1), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategySmart, true,
				channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
			)
			policy.AdaptiveSamplingFirstTokenWarningRequestPercent = &test.value
			if test.recover > 0 {
				policy.AdaptiveSamplingRecoverRequestPercent = &test.recover
			}
			if test.switchConfirm > 0 {
				policy.AdaptiveSamplingSwitchConfirmRequestPercent = &test.switchConfirm
			}

			normalized, err := normalizeChannelSmartScheduleGroupPolicies(
				[]channelSmartScheduleGroupPolicy{policy},
			)
			if test.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "请求占比阈值")
				return
			}
			require.NoError(t, err)
			require.Len(t, normalized, 1)
			require.NotNil(t, normalized[0].AdaptiveSamplingFirstTokenWarningRequestPercent)
			assert.Equal(t, test.value, *normalized[0].AdaptiveSamplingFirstTokenWarningRequestPercent)
		})
	}
}

func TestNormalizeChannelSmartScheduleGroupPolicyPreservesWarningRecoveryHysteresis(t *testing.T) {
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
	)
	firstTokenWarningRequestPercent := 20.0
	recoverRequestPercent := 80.0
	policy.AdaptiveSamplingFirstTokenWarningRequestPercent = &firstTokenWarningRequestPercent
	policy.AdaptiveSamplingRecoverRequestPercent = &recoverRequestPercent

	_, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{policy})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "必须保留滞回区间")
}

func TestNormalizeChannelSmartScheduleGroupPolicyDefaultsDegradedProbeDisabled(t *testing.T) {
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
	)
	policy.DegradedProbeEnabled = nil

	normalized, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{policy})
	require.NoError(t, err)
	require.Len(t, normalized, 1)
	require.NotNil(t, normalized[0].DegradedProbeEnabled)
	assert.False(t, *normalized[0].DegradedProbeEnabled)
	assert.False(t, normalized[0].policy().DegradedProbeEnabled)
}

func channelSmartScheduleTestGroupPoliciesJSON(t *testing.T, policies ...channelSmartScheduleGroupPolicy) string {
	serialized, err := common.Marshal(policies)
	require.NoError(t, err)
	return string(serialized)
}

func useChannelSmartScheduleGroupRatio(t *testing.T, raw string) {
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(raw))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
}

func TestNormalizeChannelSmartScheduleGroupPolicyDefaultsPromptLimits(t *testing.T) {
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
	)
	policy.ExplorationMaxPromptTokens = nil
	policy.StabilityReleaseMaxPromptTokens = nil

	normalized, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{policy})
	require.NoError(t, err)
	require.Len(t, normalized, 1)
	require.NotNil(t, normalized[0].ExplorationMaxPromptTokens)
	require.NotNil(t, normalized[0].StabilityReleaseMaxPromptTokens)
	assert.Equal(t, 50_000, *normalized[0].ExplorationMaxPromptTokens)
	assert.Equal(t, model.DefaultChannelSmartScheduleStabilityReleaseMaxPromptTokens, *normalized[0].StabilityReleaseMaxPromptTokens)
}

func TestChannelSmartScheduleGroupPolicyWindowContractSerialization(t *testing.T) {
	newPolicy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
	)
	burstMinutes, burstRequests, burstPercent := 2, 80, 12.5
	adaptiveMinutes, adaptiveRequests := 15, 240
	newPolicy.BurstFailureWindowMinutes = &burstMinutes
	newPolicy.BurstFailureWindowRequests = &burstRequests
	newPolicy.BurstFailureThresholdPercent = &burstPercent
	newPolicy.BurstFailureWindowSeconds = nil
	newPolicy.BurstFailureThreshold = nil
	newPolicy.AdaptiveSamplingWindowMinutes = &adaptiveMinutes
	newPolicy.AdaptiveSamplingWindowRequests = &adaptiveRequests
	newPolicy.AdaptiveSamplingWindowSeconds = nil
	normalized, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{newPolicy})
	require.NoError(t, err)
	raw, err := common.Marshal(normalized)
	require.NoError(t, err)
	serialized := string(raw)
	assert.Contains(t, serialized, `"burst_failure_window_minutes":2`)
	assert.Contains(t, serialized, `"burst_failure_window_requests":80`)
	assert.Contains(t, serialized, `"burst_failure_threshold_percent":12.5`)
	assert.Contains(t, serialized, `"adaptive_sampling_window_minutes":15`)
	assert.Contains(t, serialized, `"adaptive_sampling_window_requests":240`)
	assert.NotContains(t, serialized, `"burst_failure_window_seconds"`)
	assert.NotContains(t, serialized, `"burst_failure_threshold"`)
	assert.NotContains(t, serialized, `"adaptive_sampling_window_seconds"`)

	legacyPolicy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
	)
	normalized, err = normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{legacyPolicy})
	require.NoError(t, err)
	raw, err = common.Marshal(normalized)
	require.NoError(t, err)
	serialized = string(raw)
	assert.Contains(t, serialized, `"burst_failure_window_seconds"`)
	assert.Contains(t, serialized, `"burst_failure_threshold"`)
	assert.Contains(t, serialized, `"adaptive_sampling_window_seconds"`)
	assert.NotContains(t, serialized, `"burst_failure_window_minutes"`)
	assert.NotContains(t, serialized, `"burst_failure_threshold_percent"`)
	assert.NotContains(t, serialized, `"adaptive_sampling_window_minutes"`)
}

func TestNormalizeChannelSmartScheduleGroupPolicyValidatesPromptLimits(t *testing.T) {
	invalidValues := []int{
		-1,
		1,
		999,
		1_001,
		16_384,
		model.MaxChannelSmartScheduleExplorationPromptTokens + 1,
	}
	for _, value := range invalidValues {
		policy := channelSmartScheduleTestGroupPolicy(
			"vip", channelMonitorSmartScheduleStrategySmart, true,
			channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
		)
		policy.ExplorationMaxPromptTokens = &value

		_, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{policy})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "探索请求上限")
	}
	for _, value := range invalidValues {
		policy := channelSmartScheduleTestGroupPolicy(
			"vip", channelMonitorSmartScheduleStrategySmart, true,
			channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
		)
		policy.StabilityReleaseMaxPromptTokens = &value

		_, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{policy})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "稳定性释放请求上限")
	}
	for _, value := range []int{
		0,
		model.ChannelSmartSchedulePromptTokensPerK,
		50 * model.ChannelSmartSchedulePromptTokensPerK,
		model.MaxChannelSmartScheduleExplorationPromptTokens,
	} {
		policy := channelSmartScheduleTestGroupPolicy(
			"vip", channelMonitorSmartScheduleStrategySmart, true,
			channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
		)
		policy.ExplorationMaxPromptTokens = &value
		policy.StabilityReleaseMaxPromptTokens = &value

		_, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{policy})
		require.NoError(t, err)
	}
}

func TestRunChannelSmartScheduleForceResetSetsBaselineBeforePlanning(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelSmartScheduleGroupRatio(t, `{"vip":100}`)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyWeight, nil, 5, 80, 30,
			),
		),
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
		{ChannelId: 14, Ratio: 2, UpdatedTime: 1},
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
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 11, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 12, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 13, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 14, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Excluded: true},
	}).Error)

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, true)
	require.NoError(t, err)
	assert.Equal(t, 3, result.Updated)
	assert.Zero(t, result.Unchanged)
	assert.Equal(t, 1, result.Skipped)

	expected := map[int]struct {
		priority int64
		weight   uint
	}{
		11: {priority: 80, weight: 900},
		12: {priority: 80, weight: 100},
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
	useChannelSmartScheduleGroupRatio(t, `{"vip":100}`)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyWeight, nil, 5, 80, 30,
			),
		),
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
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 21, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{ChannelId: 22, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
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

func TestRunChannelSmartScheduleDoesNotHardDegradeLowStabilityScore(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 2, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
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
	staleRollingScore := 1.0
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{
			ChannelId: 31, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
			RollingStabilityScore: &staleRollingScore, RollingStabilitySampleCount: 100,
			RollingStabilityUpdatedAt: common.GetTimestamp() - 60,
		},
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
	for index := range 2 {
		timestamp := initialLogTime + int64(index)
		require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
			31, "vip", "model-a", timestamp, false, nil, nil, nil, false,
		))
		require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
			32, "vip", "model-a", timestamp, true, nil, nil, nil, false,
		))
	}
	require.NoError(t, aggregateChannelMonitorTestLogs(initialMinute, initialMinute+60))

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 31, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Positive(t, *ability.Priority)
	assert.Positive(t, ability.Weight)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 31, "vip", "model-a",
	).First(&state).Error)
	assert.Empty(t, state.StabilityState)

	var unstableAdjustment *channelSmartScheduleTaskAdjustment
	for index := range result.Adjustments {
		if result.Adjustments[index].ChannelId == 31 {
			unstableAdjustment = &result.Adjustments[index]
			break
		}
	}
	require.NotNil(t, unstableAdjustment)
	require.NotNil(t, unstableAdjustment.ScoreDetails)
	require.NotNil(t, unstableAdjustment.ScoreDetails.Stability.RawScore)
	assert.Less(t, *unstableAdjustment.ScoreDetails.Stability.RawScore, 0.8)
	assert.NotContains(t, unstableAdjustment.Reason, "稳定性降级")
}

func TestRuntimeRefreshClearsProbeStateAfterSuccessfulNewSamples(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, true,
				channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 2, 80, 30,
			),
		),
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
	for index := range 2 {
		timestamp := probeStartedAt + int64(index)
		require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
			33, "vip", "model-a", timestamp, true, nil, nil, nil, false,
		))
		require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
			34, "vip", "model-a", timestamp, true, nil, nil, nil, false,
		))
	}
	require.NoError(t, aggregateChannelMonitorTestLogs(probeStartedAt, probeStartedAt+60))

	recoveryStartedAt := common.GetTimestamp()
	processChannelSmartScheduleAdaptiveRefreshEvents([]channelSmartScheduleAdaptiveRefreshEvent{{
		database: model.DB, channelId: 33, modelName: "model-a",
	}})
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 33, "vip", "model-a",
	).First(&state).Error)
	assert.Empty(t, state.StabilityState)
	assert.Zero(t, state.StabilitySince)
	var sampleState model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ?", 33, "model-a",
	).First(&sampleState).Error)
	assert.GreaterOrEqual(t, sampleState.ObservationSince, recoveryStartedAt)
	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 33, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(80), *ability.Priority)
	assert.Equal(t, uint(30), ability.Weight)
	require.Eventually(t, func() bool {
		channelSmartScheduleAdaptiveRefreshQueue.Lock()
		defer channelSmartScheduleAdaptiveRefreshQueue.Unlock()
		return !channelSmartScheduleAdaptiveRefreshQueue.running
	}, 3*time.Second, 20*time.Millisecond)
}

func TestFullScheduleDoesNotRecoverDegradedRouteFromProcessLocalSuccesses(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 100, 80, 30,
	)
	degradedProbeEnabled := true
	recoverySuccessThreshold := 2
	policy.DegradedProbeEnabled = &degradedProbeEnabled
	policy.RecoverySuccessThreshold = &recoverySuccessThreshold
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	restoredPriority := int64(80)
	restoredWeight := uint(30)
	degradedPriority := int64(0)
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&model.Channel{
		Id: 37, Name: "degraded probe recovery", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &restoredPriority, Weight: &restoredWeight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 37, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &degradedPriority, Weight: 0,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 37, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		StabilityState:         model.ChannelSmartScheduleStabilityDegraded,
		StabilityUntil:         now + 30*60,
		StabilitySavedPriority: restoredPriority,
		StabilitySavedWeight:   restoredWeight,
		RuntimeProtectionUntil: now + 30*60,
	}).Error)

	settings := getChannelMonitorSettings()
	projectChannelSmartScheduleRuntimeFailureForTest(
		37, "model-a", now-1, policy.policy().BurstFailureWindowSeconds,
		settings.SmartScheduleControlRevision,
	)
	projectChannelSmartScheduleRuntimeProbeSuccessForTest(37, "model-a")
	projectChannelSmartScheduleRuntimeProbeSuccessForTest(37, "model-a")

	_, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 37, "vip", "model-a",
	).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, state.StabilityState)
	assert.Greater(t, state.StabilityUntil, now)
	assert.Zero(t, state.StabilitySince)
	assert.Greater(t, state.RuntimeProtectionUntil, now)
	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 37, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)

	health := getChannelSmartScheduleRuntimeHealth(
		37, "model-a", common.GetTimestamp(), policy.policy().BurstFailureWindowSeconds,
		settings.SmartScheduleControlRevision,
	)
	assert.Zero(t, health.ConsecutiveFailures)
	assert.Len(t, health.FailureTimes, 1)
}

func TestFullScheduleDoesNotRecoverProbingRouteFromProcessLocalSuccesses(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 100, 80, 30,
	)
	probeMode := channelMonitorSmartScheduleSampleProbe
	recoverySuccessThreshold := 2
	policy.SampleMode = &probeMode
	policy.RecoverySuccessThreshold = &recoverySuccessThreshold
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	restoredPriority := int64(80)
	probePriority := int64(0)
	restoredWeight := uint(30)
	probeWeight := uint(channelMonitorSmartScheduleMinWeight)
	require.NoError(t, db.Create(&model.Channel{
		Id: 36, Name: "runtime probe recovery", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &restoredPriority, Weight: &restoredWeight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 36, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &probePriority, Weight: probeWeight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 36, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		StabilityState:         model.ChannelSmartScheduleStabilityProbing,
		StabilitySince:         common.GetTimestamp() - 60,
		StabilitySavedPriority: restoredPriority,
		StabilitySavedWeight:   restoredWeight,
	}).Error)

	projectChannelSmartScheduleRuntimeProbeSuccessForTest(36, "model-a")
	_, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 36, "vip", "model-a",
	).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityProbing, state.StabilityState)
	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 36, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Equal(t, probeWeight, ability.Weight)

	projectChannelSmartScheduleRuntimeProbeSuccessForTest(36, "model-a")
	_, err = runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 36, "vip", "model-a",
	).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityProbing, state.StabilityState)
	require.NoError(t, db.Where(&model.Ability{ChannelId: 36, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Equal(t, probeWeight, ability.Weight)
}

func TestFullScheduleDoesNotAdvanceRecoveryBoundary(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 2, 80, 30,
	)
	probeMode := channelMonitorSmartScheduleSampleProbe
	recoverySuccessThreshold := 1
	policy.SampleMode = &probeMode
	policy.RecoverySuccessThreshold = &recoverySuccessThreshold
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	common.LogConsumeEnabled = false
	constant.ErrorLogEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})

	restoredPriority := int64(80)
	restoredWeight := uint(30)
	probePriority := int64(0)
	probeWeight := uint(channelMonitorSmartScheduleMinWeight)
	now := common.GetTimestamp()
	recoveryWindowStart := now - 60
	require.NoError(t, db.Create(&model.Channel{
		Id: 38, Name: "recovery boundary", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &restoredPriority, Weight: &restoredWeight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 38, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &probePriority, Weight: probeWeight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 38, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		StabilityState:         model.ChannelSmartScheduleStabilityProbing,
		StabilitySince:         recoveryWindowStart,
		StabilitySavedPriority: restoredPriority,
		StabilitySavedWeight:   restoredWeight,
	}).Error)
	failureDurationMs := 500.0
	for index := range 2 {
		_, err := saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 38, Model: "model-a", WindowStart: recoveryWindowStart,
			Time: now - int64(2-index), Success: false, DurationMs: &failureDurationMs,
		})
		require.NoError(t, err)
	}

	projectChannelSmartScheduleRuntimeProbeSuccessForTest(38, "model-a")
	_, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 38, "vip", "model-a",
	).First(&state).Error)
	require.Equal(t, model.ChannelSmartScheduleStabilityProbing, state.StabilityState)
	require.Equal(t, recoveryWindowStart, state.StabilitySince)
	var sampleState model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ?", 38, "model-a",
	).First(&sampleState).Error)
	require.Zero(t, sampleState.ObservationSince)

	_, err = runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 38, "vip", "model-a",
	).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityProbing, state.StabilityState)
	assert.Equal(t, recoveryWindowStart, state.StabilitySince)
}

func TestRunChannelSmartSchedulePreservesProbingState(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 10, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t, policy,
		),
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
	probePriority := int64(0)
	probeWeight := uint(channelMonitorSmartScheduleMinWeight)
	restoredWeight := uint(35)
	probeStartedAt := time.Now().Unix()
	probeStartedAt = probeStartedAt - probeStartedAt%60 - 60
	require.NoError(t, db.Create(&model.Channel{
		Id: 35, Name: "recovering", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &restoredWeight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 35, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &probePriority, Weight: probeWeight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 35, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		StabilityState:         model.ChannelSmartScheduleStabilityProbing,
		StabilitySince:         probeStartedAt,
		StabilitySavedPriority: priority,
		StabilitySavedWeight:   restoredWeight,
	}).Error)

	logs := make([]model.Log, 0, 10)
	for index := range 7 {
		logs = append(logs, model.Log{
			ChannelId: 35, Group: "vip", ModelName: "model-a",
			CreatedAt: probeStartedAt + int64(index), Type: model.LogTypeConsume,
		})
	}
	for index, durationMs := range []int64{500, 500, 20_000} {
		other, err := common.Marshal(map[string]any{
			"channel_monitor_attempt_duration_ms": durationMs,
		})
		require.NoError(t, err)
		logs = append(logs, model.Log{
			ChannelId: 35, Group: "vip", ModelName: "model-a",
			CreatedAt: probeStartedAt + int64(10+index), Type: model.LogTypeError,
			IsRetryAttempt: true, RequestId: fmt.Sprintf("probing-%d", index), Other: string(other),
		})
	}
	require.NoError(t, db.Create(&logs).Error)
	for index := range 7 {
		require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
			35, "vip", "model-a", probeStartedAt+int64(index), true,
			nil, nil, nil, false,
		))
	}
	for index, durationMs := range []int64{500, 500, 20_000} {
		require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
			35, "vip", "model-a", probeStartedAt+int64(10+index), false,
			nil, nil, &durationMs, true,
		))
	}
	require.NoError(t, aggregateChannelMonitorTestLogs(probeStartedAt, probeStartedAt+60))

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	assert.Zero(t, result.Failed)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 35, "vip", "model-a",
	).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityProbing, state.StabilityState)
	assert.Contains(t, state.LastScheduleError, "恢复仅由实际请求、手动测试或定时探测结果触发")
	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 35, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Equal(t, probeWeight, ability.Weight)
}

func TestChannelSmartScheduleStabilityScorePenalizesSlowRetryFailuresMore(t *testing.T) {
	policy := channelSmartSchedulePolicy{
		FastFailurePenaltyPercent: 40,
		FastFailureSeconds:        1,
		SlowFailureSeconds:        10,
	}
	fastScore, fastSamples := channelSmartScheduleStabilityScore(
		8, 2, 0,
		[]model.ChannelMonitorFailureDurationBucket{{LowerBoundMs: 0, UpperBoundMs: 1000, Count: 2}},
		policy,
	)
	slowScore, slowSamples := channelSmartScheduleStabilityScore(
		8, 2, 0,
		[]model.ChannelMonitorFailureDurationBucket{{LowerBoundMs: 10000, UpperBoundMs: 30000, Count: 2}},
		policy,
	)

	require.NotNil(t, fastScore)
	require.NotNil(t, slowScore)
	assert.Equal(t, int64(10), fastSamples)
	assert.Equal(t, int64(10), slowSamples)
	assert.InDelta(t, 0.92, *fastScore, 1e-9)
	assert.InDelta(t, 0.80, *slowScore, 1e-9)
	assert.Greater(t, *fastScore, *slowScore)
}

func TestChannelSmartScheduleStabilityScoreFullyPenalizesFinalFailure(t *testing.T) {
	policy := channelSmartSchedulePolicy{
		FastFailurePenaltyPercent: 40,
		FastFailureSeconds:        1,
		SlowFailureSeconds:        10,
	}
	fastBucket := []model.ChannelMonitorFailureDurationBucket{
		{LowerBoundMs: 0, UpperBoundMs: 1000, Count: 1},
	}
	retryScore, retrySamples := channelSmartScheduleStabilityScore(9, 1, 0, fastBucket, policy)
	finalScore, finalSamples := channelSmartScheduleStabilityScore(9, 1, 1, fastBucket, policy)

	require.NotNil(t, retryScore)
	require.NotNil(t, finalScore)
	assert.Equal(t, int64(10), retrySamples)
	assert.Equal(t, int64(10), finalSamples)
	assert.InDelta(t, 0.96, *retryScore, 1e-9)
	assert.InDelta(t, 0.90, *finalScore, 1e-9)
	assert.Less(t, *finalScore, *retryScore)
}

func TestChannelSmartScheduleJitterUsesFixedSlowThreshold(t *testing.T) {
	policy := channelSmartSchedulePolicy{
		StabilityEnabled:           true,
		JitterEnabled:              true,
		JitterTolerancePercent:     5,
		JitterSlowThresholdSeconds: 1.3,
	}
	measurement := channelSmartScheduleMeasureJitter([]model.ChannelMonitorDurationBucket{
		{LowerBoundMs: 0, UpperBoundMs: 1000, Count: 99, TotalMs: 29700},
		{LowerBoundMs: 1300, UpperBoundMs: 1500, Count: 1, TotalMs: 1300},
	}, 20, policy)

	assert.True(t, measurement.Available)
	assert.InDelta(t, 1300, measurement.ThresholdMs, 1e-9)
	assert.Equal(t, int64(100), measurement.SampleCount)
	assert.Equal(t, int64(1), measurement.SlowCount)
	assert.Equal(t, int64(5), measurement.AllowedCount)
	assert.Zero(t, measurement.Penalty)
}

func TestChannelSmartScheduleJitterPenalizesOnlyExcessSlowSuccesses(t *testing.T) {
	policy := channelSmartSchedulePolicy{
		StabilityEnabled:           true,
		JitterEnabled:              true,
		JitterTolerancePercent:     5,
		JitterSlowThresholdSeconds: 1,
	}
	measurement := channelSmartScheduleMeasureJitter([]model.ChannelMonitorDurationBucket{
		{LowerBoundMs: 0, UpperBoundMs: 1000, Count: 80, TotalMs: 24000},
		{LowerBoundMs: 5000, UpperBoundMs: 10000, Count: 20, TotalMs: 120000},
	}, 20, policy)
	baseScore := 1.0
	adjusted := channelSmartScheduleApplyJitterPenalty(&baseScore, 100, measurement.Penalty)

	assert.Equal(t, int64(20), measurement.SlowCount)
	assert.Equal(t, int64(5), measurement.AllowedCount)
	assert.InDelta(t, 15, measurement.Penalty, 1e-9)
	require.NotNil(t, adjusted)
	assert.InDelta(t, 0.85, *adjusted, 1e-9)
}

func TestChannelSmartScheduleJitterRequiresDistributionSamples(t *testing.T) {
	policy := channelSmartSchedulePolicy{
		StabilityEnabled:           true,
		JitterEnabled:              true,
		JitterTolerancePercent:     0,
		JitterSlowThresholdSeconds: 1,
		MinSamples:                 5,
	}
	stability := 1.0
	performance := &channelSmartSchedulePerformance{
		FirstTokenSampleCount:         1000,
		FirstTokenDurationSampleCount: 1,
		FirstTokenDurationBuckets: []model.ChannelMonitorDurationBucket{
			{LowerBoundMs: 10_000, UpperBoundMs: 15_000, Count: 1, TotalMs: 10_000},
		},
		Stability:            &stability,
		StabilitySampleCount: 1000,
	}
	channelSmartScheduleApplyJitterMeasurement(performance, policy)

	assert.False(t, performance.JitterAvailable)
	assert.InDelta(t, 1, *performance.Stability, 1e-9)
}

func TestChannelSmartScheduleJitterZeroThresholdMarksEverySuccessSlow(t *testing.T) {
	policy := channelSmartSchedulePolicy{
		StabilityEnabled:           true,
		JitterEnabled:              true,
		JitterSlowThresholdSeconds: 0,
	}
	measurement := channelSmartScheduleMeasureJitter([]model.ChannelMonitorDurationBucket{
		{LowerBoundMs: 0, UpperBoundMs: 1000, Count: 3, TotalMs: 900},
	}, 1, policy)

	assert.True(t, measurement.Available)
	assert.Zero(t, measurement.ThresholdMs)
	assert.Equal(t, int64(3), measurement.SlowCount)
	assert.Equal(t, 3.0, measurement.Penalty)
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
	assert.Equal(t, uint(900), items[1].TargetWeight)
	assert.Equal(t, int64(0), items[2].TargetPriority)
	assert.Equal(t, uint(100), items[2].TargetWeight)
	assert.Equal(t, int64(10), items[3].TargetPriority)
	assert.Equal(t, uint(100), items[3].TargetWeight)
	assert.Equal(t, int64(10), items[4].TargetPriority)
	assert.Equal(t, uint(900), items[4].TargetWeight)
}

func TestPlanChannelSmartSchedulePriorityWeightUsesPrimaryAndFallbackRanks(t *testing.T) {
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
	assert.Equal(t, int64(3), items[1].TargetPriority)
	assert.Equal(t, uint(1000), items[1].TargetWeight)
	assert.Equal(t, int64(2), items[2].TargetPriority)
	assert.Equal(t, uint(1000), items[2].TargetWeight)
	assert.Equal(t, int64(1), items[3].TargetPriority)
	assert.Equal(t, uint(1000), items[3].TargetWeight)
}

func TestPlanChannelSmartSchedulePriorityWeightTiesRetainCurrentThenUseChannelId(t *testing.T) {
	ratio := 1.0
	plan := planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{ChannelId: 3, CurrentPriority: 100, CurrentWeight: 50, Ratio: &ratio},
		{ChannelId: 1, CurrentPriority: 80, CurrentWeight: 50, Ratio: &ratio},
		{ChannelId: 2, CurrentPriority: 80, CurrentWeight: 50, Ratio: &ratio},
	}, channelMonitorSmartScheduleStrategyRatio, false, channelMonitorSmartScheduleApplyPriorityWeight, 5, false)

	require.Len(t, plan.Items, 3)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, int64(3), items[3].TargetPriority)
	assert.Equal(t, int64(2), items[1].TargetPriority)
	assert.Equal(t, int64(1), items[2].TargetPriority)
	assert.Equal(t, uint(1000), items[1].TargetWeight)
	assert.Equal(t, uint(1000), items[2].TargetWeight)
	assert.Equal(t, uint(1000), items[3].TargetWeight)
}

func TestPlanChannelSmartSchedulePriorityWeightAssignsUniquePrioritiesToTenHealthyChannels(t *testing.T) {
	ratios := make([]float64, 10)
	candidates := make([]channelSmartScheduleCandidate, 0, len(ratios))
	for index := range ratios {
		ratios[index] = float64(index + 1)
		candidates = append(candidates, channelSmartScheduleCandidate{
			ChannelId: index + 1, CurrentPriority: 100, CurrentWeight: 50, Ratio: &ratios[index],
		})
	}

	plan := planChannelSmartSchedule(
		candidates,
		channelMonitorSmartScheduleStrategyRatio,
		false,
		channelMonitorSmartScheduleApplyPriorityWeight,
		5,
		false,
	)

	require.Len(t, plan.Items, 10)
	priorities := make(map[int64]struct{}, len(plan.Items))
	for _, item := range plan.Items {
		assert.Equal(t, int64(11-item.ChannelId), item.TargetPriority)
		assert.Equal(t, uint(1000), item.TargetWeight)
		priorities[item.TargetPriority] = struct{}{}
	}
	assert.Len(t, priorities, 10)
}

func TestPlanChannelSmartSchedulePriorityWeightRanksInsufficientSamplesAfterScoredChannels(t *testing.T) {
	ratioBest := 1.0
	ratioSecond := 2.0
	plan := planChannelSmartSchedule(
		[]channelSmartScheduleCandidate{
			{ChannelId: 1, Ratio: &ratioBest},
			{ChannelId: 2, Ratio: &ratioSecond},
			{ChannelId: 3, PreviousBaseRank: 4},
			{ChannelId: 4, PreviousBaseRank: 3},
		},
		channelMonitorSmartScheduleStrategyRatio,
		false,
		channelMonitorSmartScheduleApplyPriorityWeight,
		5,
		false,
	)

	require.Len(t, plan.Items, 4)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.True(t, items[1].Scored)
	assert.True(t, items[2].Scored)
	assert.False(t, items[3].Scored)
	assert.False(t, items[4].Scored)
	assert.Equal(t, int64(4), items[1].BasePriority)
	assert.Equal(t, int64(3), items[2].BasePriority)
	assert.Equal(t, int64(2), items[4].BasePriority)
	assert.Equal(t, int64(1), items[3].BasePriority)
}

func TestPlanChannelSmartSchedulePriorityWeightAssignsUniqueRanksWhenAllSamplesAreInsufficient(t *testing.T) {
	plan := planChannelSmartSchedule(
		[]channelSmartScheduleCandidate{
			{ChannelId: 3, CurrentPriority: 100, CurrentWeight: 50},
			{ChannelId: 1, CurrentPriority: 100, CurrentWeight: 50},
			{ChannelId: 2, CurrentPriority: 100, CurrentWeight: 50},
		},
		channelMonitorSmartScheduleStrategyRatio,
		false,
		channelMonitorSmartScheduleApplyPriorityWeight,
		5,
		false,
	)

	require.Len(t, plan.Items, 3)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
		assert.False(t, item.Scored)
		assert.Equal(t, uint(1000), item.BaseWeight)
	}
	assert.Equal(t, int64(3), items[1].BasePriority)
	assert.Equal(t, int64(2), items[2].BasePriority)
	assert.Equal(t, int64(1), items[3].BasePriority)
}

func TestPlanChannelSmartSchedulePriorityWeightKeepsCurrentPrimaryThenSampleValidRoute(t *testing.T) {
	ratio := 1.0
	plan := planChannelSmartSchedule(
		[]channelSmartScheduleCandidate{
			{ChannelId: 1, Ratio: &ratio, PreviousBaseRank: 3},
			{ChannelId: 2, PreviousBaseRank: 2},
			{ChannelId: 3, PreviousBaseRank: 1},
		},
		channelMonitorSmartScheduleStrategyRatio,
		false,
		channelMonitorSmartScheduleApplyPriorityWeight,
		5,
		false,
	)

	require.Len(t, plan.Items, 3)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
		assert.False(t, item.Scored)
		assert.Equal(t, uint(1000), item.BaseWeight)
	}
	assert.Equal(t, int64(3), items[3].BasePriority)
	assert.Equal(t, int64(2), items[1].BasePriority)
	assert.Equal(t, int64(1), items[2].BasePriority)
	assert.Contains(t, items[1].SkipReason, "可比渠道不足")
}

func TestPlanChannelSmartScheduleAssignsConfiguredPrimaryTraffic(t *testing.T) {
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
			assert.Equal(t, uint(900), items[1].TargetWeight)
			assert.Equal(t, uint(98), items[2].TargetWeight)
			assert.Equal(t, uint(2), items[3].TargetWeight)
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
	assert.Equal(t, uint(900), items[1].TargetWeight)
	assert.Equal(t, uint(100), items[2].TargetWeight)
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
	assert.Equal(t, uint(900), items[1].TargetWeight)
	assert.Equal(t, uint(100), items[2].TargetWeight)

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
	assert.Equal(t, uint(100), items[1].TargetWeight)
	assert.Equal(t, uint(900), items[2].TargetWeight)
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
	assert.Equal(t, uint(900), items[1].TargetWeight)
	assert.Equal(t, uint(100), items[2].TargetWeight)
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
	assert.Equal(t, "可比渠道不足（1/2），暂不产生相对评分", plan.Skipped[1])
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
	assert.Equal(t, uint(900), items[1].TargetWeight)
	assert.Equal(t, uint(100), items[2].TargetWeight)

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
	assert.Equal(t, uint(900), items[1].TargetWeight)
	assert.Equal(t, uint(100), items[2].TargetWeight)
}

func TestPlanChannelSmartScheduleUsesSelectedStrategyWithStabilityScore(t *testing.T) {
	ratio := 1.0
	stableRate := 0.99
	unstableRate := 0.80
	scoring := defaultChannelSmartScheduleScoring()
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
	assert.Equal(t, uint(900), items[1].TargetWeight)
	assert.Equal(t, uint(100), items[2].TargetWeight)

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

func TestPlanChannelSmartScheduleUsesHysteresisAndForceReset(t *testing.T) {
	currentScore := 0.80
	challengerScore := 0.82
	scoring := defaultChannelSmartScheduleScoring()
	scoring.StabilityPercent = 100
	candidates := []channelSmartScheduleCandidate{
		{
			ChannelId: 1, CurrentPriority: 80, CurrentWeight: 900,
			Stability: &currentScore, StabilitySampleCount: 5, StabilityAvailable: true,
		},
		{
			ChannelId: 2, CurrentPriority: 80, CurrentWeight: 100,
			Stability: &challengerScore, StabilitySampleCount: 5, StabilityAvailable: true,
		},
	}
	plan := planChannelSmartScheduleWithScoring(
		candidates, channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, 5, false, scoring,
	)
	require.Len(t, plan.Items, 2)
	assert.Equal(t, uint(900), plan.Items[0].TargetWeight)
	assert.Equal(t, uint(100), plan.Items[1].TargetWeight)

	plan = planChannelSmartScheduleWithScoring(
		candidates, channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, 5, true, scoring,
	)
	require.Len(t, plan.Items, 2)
	assert.Equal(t, uint(100), plan.Items[0].TargetWeight)
	assert.Equal(t, uint(900), plan.Items[1].TargetWeight)

	challengerScore = 0.83
	plan = planChannelSmartScheduleWithScoring(
		candidates, channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, 5, false, scoring,
	)
	require.Len(t, plan.Items, 2)
	assert.Equal(t, uint(100), plan.Items[0].TargetWeight)
	assert.Equal(t, uint(900), plan.Items[1].TargetWeight)
}

func TestPlanChannelSmartScheduleManualPrimaryOverridesScore(t *testing.T) {
	higherScore := 0.98
	lowerScore := 0.60
	scoring := defaultChannelSmartScheduleScoring()
	scoring.StabilityPercent = 100
	scoring.PrimaryTrafficPercent = 90
	candidates := []channelSmartScheduleCandidate{
		{
			ChannelId: 1, CurrentPriority: 80, CurrentWeight: 900,
			Stability: &higherScore, StabilitySampleCount: 20, StabilityAvailable: true,
		},
		{
			ChannelId: 2, CurrentPriority: 80, CurrentWeight: 100,
			Stability: &lowerScore, StabilitySampleCount: 20, StabilityAvailable: true,
			ManualPrimary: true, ManualTargetPriority: 3,
		},
	}

	plan := planChannelSmartScheduleWithScoring(
		candidates, channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, 5, false, scoring,
	)
	require.Len(t, plan.Items, 2)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, uint(100), items[1].TargetWeight)
	assert.Equal(t, uint(900), items[2].TargetWeight)

	plan = planChannelSmartScheduleWithScoring(
		candidates, channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, 5, false, scoring,
	)
	require.Len(t, plan.Items, 2)
	items = make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, 1, items[1].BaseRank)
	assert.Equal(t, int64(2), items[1].BasePriority)
	assert.Equal(t, int64(2), items[1].TargetPriority)
	assert.Equal(t, 2, items[2].BaseRank)
	assert.Equal(t, int64(1), items[2].BasePriority)
	assert.Equal(t, int64(3), items[2].TargetPriority)
	assert.Equal(t, 1, plan.RawWinnerId)
	assert.Equal(t, 2, plan.ActualPrimaryId)
	assert.Equal(t, 2, items[1].ScoreDetails.Decision.ManualPrimaryChannelId)
	assert.Equal(t, 2, items[2].ScoreDetails.Decision.ManualPrimaryChannelId)
}

func TestRunChannelSmartScheduleManualPrimaryOverridesStabilityDegrade(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 2, 80, 30,
	)
	sampleMode := channelMonitorSmartScheduleSampleProbe
	policy.SampleMode = &sampleMode
	policy.Scoring.StabilityPercent = 100
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	priority := int64(80)
	weight := uint(100)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 66, Name: "manual unstable", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 67, Name: "automatic stable", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 66, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 67, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 66, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 67, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)

	now := common.GetTimestamp()
	windowStart := now - 3600
	failureDurationMs := 500.0
	for index := 0; index < 2; index++ {
		_, err := saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 66, Model: "model-a",
			WindowStart: windowStart, Time: now - int64(2-index), Success: false,
			DurationMs: &failureDurationMs,
		})
		require.NoError(t, err)
		_, err = saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 67, Model: "model-a",
			WindowStart: windowStart, Time: now - int64(2-index), Success: true,
		})
		require.NoError(t, err)
	}
	fixed, err := model.SaveChannelSmartScheduleRoutePrimary(
		66, "vip", "model-a", model.ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 10},
	)
	require.NoError(t, err)

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	var fixedAbility model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 66, Group: "vip", Model: "model-a"}).
		First(&fixedAbility).Error)
	assert.Equal(t, int64(81), *fixedAbility.Priority)
	assert.Equal(t, uint(1000), fixedAbility.Weight)
	var fixedState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 66, "vip", "model-a",
	).First(&fixedState).Error)
	assert.Empty(t, fixedState.StabilityState)
	assert.Equal(t, fixed.State.ManualPrimaryUntil, fixedState.ManualPrimaryUntil)

	var fixedAdjustment *channelSmartScheduleTaskAdjustment
	for index := range result.Adjustments {
		if result.Adjustments[index].ChannelId == 66 {
			fixedAdjustment = &result.Adjustments[index]
			break
		}
	}
	require.NotNil(t, fixedAdjustment)
	assert.True(t, fixedAdjustment.ManualPrimary)
	assert.Equal(t, fixed.State.ManualPrimaryUntil, fixedAdjustment.ManualPrimaryUntil)
	require.NotNil(t, fixedAdjustment.Score)
	assert.Less(t, *fixedAdjustment.Score, 0.8)
	assert.Contains(t, fixedAdjustment.Reason, "管理员已固定为主渠道")
}

func TestRunChannelSmartScheduleManualPrimaryAllowsStabilityDegrade(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 2, 80, 30,
	)
	sampleMode := channelMonitorSmartScheduleSampleProbe
	policy.SampleMode = &sampleMode
	policy.Scoring.StabilityPercent = 100
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	priority := int64(80)
	weight := uint(100)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 68, Name: "degradable fixed primary", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 69, Name: "stable fallback", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 68, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 69, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 68, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 69, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)

	now := common.GetTimestamp()
	failureDurationMs := 500.0
	for index := 0; index < 2; index++ {
		_, err := saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 68, Model: "model-a", WindowStart: now - 3600,
			Time: now - int64(2-index), Success: false, DurationMs: &failureDurationMs,
		})
		require.NoError(t, err)
		_, err = saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 69, Model: "model-a", WindowStart: now - 3600,
			Time: now - int64(2-index), Success: true,
		})
		require.NoError(t, err)
	}
	fixed, err := model.SaveChannelSmartScheduleRoutePrimary(
		68, "vip", "model-a", model.ChannelSmartScheduleRoutePrimaryOptions{
			DurationMinutes: 10, AllowStabilityDegrade: true,
		},
	)
	require.NoError(t, err)

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	var fixedAbility model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 68, Group: "vip", Model: "model-a"}).
		First(&fixedAbility).Error)
	require.NotNil(t, fixedAbility.Priority)
	assert.Positive(t, *fixedAbility.Priority)
	assert.Positive(t, fixedAbility.Weight)
	var fixedState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 68, "vip", "model-a",
	).First(&fixedState).Error)
	assert.Empty(t, fixedState.StabilityState)
	assert.Equal(t, fixed.State.ManualPrimaryUntil, fixedState.ManualPrimaryUntil)
	assert.True(t, fixedState.ManualPrimaryAllowStabilityDegrade)

	var fixedAdjustment *channelSmartScheduleTaskAdjustment
	for index := range result.Adjustments {
		if result.Adjustments[index].ChannelId == 68 {
			fixedAdjustment = &result.Adjustments[index]
			break
		}
	}
	require.NotNil(t, fixedAdjustment)
	assert.True(t, fixedAdjustment.ManualPrimary)
	assert.True(t, fixedAdjustment.ManualPrimaryAllowStabilityDegrade)
	assert.Contains(t, fixedAdjustment.Reason, "管理员已固定为主渠道")
	assert.NotContains(t, fixedAdjustment.Reason, "降级阈值")

	savedPriority := *fixedAbility.Priority
	savedWeight := fixedAbility.Weight
	runtimeError := types.NewErrorWithStatusCode(
		errors.New("上游返回 503"), types.ErrorCodeGetChannelFailed, 503,
	)
	projectAndProtectChannelSmartScheduleRuntimeFailureForTest(68, "model-a", runtimeError)
	projectAndProtectChannelSmartScheduleRuntimeFailureForTest(68, "model-a", runtimeError)

	require.NoError(t, db.Where(&model.Ability{ChannelId: 68, Group: "vip", Model: "model-a"}).
		First(&fixedAbility).Error)
	require.NotNil(t, fixedAbility.Priority)
	assert.Zero(t, *fixedAbility.Priority)
	assert.Zero(t, fixedAbility.Weight)
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 68, "vip", "model-a",
	).First(&fixedState).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, fixedState.StabilityState)
	assert.Equal(t, savedPriority, fixedState.StabilitySavedPriority)
	assert.Equal(t, savedWeight, fixedState.StabilitySavedWeight)
	assert.Equal(t, fixed.State.ManualPrimaryUntil, fixedState.ManualPrimaryUntil)
	assert.True(t, fixedState.ManualPrimaryAllowStabilityDegrade)
	assert.Contains(t, fixedState.LastScheduleError, "短期失败达到保护阈值")

	probeStartedAt := common.GetTimestamp() + 1
	require.NoError(t, db.Model(&model.ChannelSmartScheduleRouteState{}).
		Where("channel_id = ? AND group_name = ? AND model_name = ?", 68, "vip", "model-a").
		Updates(map[string]any{
			"stability_state":          model.ChannelSmartScheduleStabilityProbing,
			"stability_since":          probeStartedAt,
			"stability_until":          0,
			"runtime_protection_until": 0,
		}).Error)
	require.NoError(t, db.Model(&model.Ability{}).
		Where(&model.Ability{ChannelId: 69, Group: "vip", Model: "model-a"}).
		Update("enabled", false).Error)
	for index := 0; index < 2; index++ {
		_, err = saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 68, Model: "model-a", WindowStart: now - 3600,
			Time: probeStartedAt, Success: true, SampleId: fmt.Sprintf("fixed-recovery-%d", index),
		})
		require.NoError(t, err)
	}

	processChannelSmartScheduleAdaptiveRefreshEvents([]channelSmartScheduleAdaptiveRefreshEvent{{
		database: model.DB, channelId: 68, modelName: "model-a",
	}})
	require.NoError(t, db.Where(&model.Ability{ChannelId: 68, Group: "vip", Model: "model-a"}).
		First(&fixedAbility).Error)
	assert.Equal(t, int64(81), *fixedAbility.Priority)
	assert.Equal(t, uint(1000), fixedAbility.Weight)
}

func TestRunChannelSmartScheduleKeepsFixedPrimaryAtMaximumPriority(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 1, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	fixedPriority := int64(math.MaxInt64)
	backupPriority := int64(80)
	weight := uint(100)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 70, Name: "maximum fixed primary", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &fixedPriority, Weight: &weight},
		{Id: 71, Name: "normal backup", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &backupPriority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 70, Group: "vip", Model: "model-a", Enabled: true, Priority: &fixedPriority, Weight: weight},
		{ChannelId: 71, Group: "vip", Model: "model-a", Enabled: true, Priority: &backupPriority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 70, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 71, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 70, Ratio: 1, UpdatedTime: now},
		{ChannelId: 71, Ratio: 2, UpdatedTime: now},
	}).Error)

	_, err := model.SaveChannelSmartScheduleRoutePrimary(
		70,
		"vip",
		"model-a",
		model.ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 10},
	)
	require.NoError(t, err)
	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	assert.Zero(t, result.Failed)

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 70, Group: "vip", Model: "model-a"}).
		First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, fixedPriority, *ability.Priority)
}

func TestRunChannelSmartScheduleFixedPrimaryPreservesBaseRankingAndClearsImmediately(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelSmartScheduleGroupRatio(t, `{"vip":100}`)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 1, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	primaryPriority := int64(2)
	fixedBasePriority := int64(1)
	weight := uint(1000)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 76, Name: "scored primary", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &primaryPriority, Weight: &weight},
		{Id: 77, Name: "fixed lower score", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &fixedBasePriority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 76, Group: "vip", Model: "model-a", Enabled: true, Priority: &primaryPriority, Weight: weight},
		{ChannelId: 77, Group: "vip", Model: "model-a", Enabled: true, Priority: &fixedBasePriority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 76, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, BaseRank: 1, BasePriority: primaryPriority, BaseWeight: weight},
		{ChannelId: 77, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, BaseRank: 2, BasePriority: fixedBasePriority, BaseWeight: weight},
	}).Error)
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 76, Ratio: 1, UpdatedTime: now},
		{ChannelId: 77, Ratio: 2, UpdatedTime: now},
	}).Error)

	_, err := model.SaveChannelSmartScheduleRoutePrimary(
		77,
		"vip",
		"model-a",
		model.ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 10},
	)
	require.NoError(t, err)
	_, err = runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)

	var primaryState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
		ChannelId: 76, GroupName: "vip", ModelName: "model-a",
	}).First(&primaryState).Error)
	assert.Equal(t, 1, primaryState.BaseRank)
	assert.Equal(t, primaryPriority, primaryState.BasePriority)

	var fixedState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
		ChannelId: 77, GroupName: "vip", ModelName: "model-a",
	}).First(&fixedState).Error)
	assert.Equal(t, 2, fixedState.BaseRank)
	assert.Equal(t, fixedBasePriority, fixedState.BasePriority)

	var fixedAbility model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 77, Group: "vip", Model: "model-a",
	}).First(&fixedAbility).Error)
	assert.Equal(t, int64(3), *fixedAbility.Priority)

	require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).
		Where("channel_id = ?", 76).
		Update("ratio", 3).Error)
	require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).
		Where("channel_id = ?", 77).
		Update("ratio", 1).Error)
	_, err = runChannelSmartScheduleOnce(context.Background(), nil, true)
	require.NoError(t, err)

	require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
		ChannelId: 76, GroupName: "vip", ModelName: "model-a",
	}).First(&primaryState).Error)
	assert.Equal(t, 2, primaryState.BaseRank)
	assert.Equal(t, fixedBasePriority, primaryState.BasePriority)
	require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
		ChannelId: 77, GroupName: "vip", ModelName: "model-a",
	}).First(&fixedState).Error)
	assert.Equal(t, 1, fixedState.BaseRank)
	assert.Equal(t, primaryPriority, fixedState.BasePriority)
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 77, Group: "vip", Model: "model-a",
	}).First(&fixedAbility).Error)
	assert.Equal(t, int64(3), *fixedAbility.Priority)

	cleared, err := model.SaveChannelSmartScheduleRoutePrimary(
		77,
		"vip",
		"model-a",
		model.ChannelSmartScheduleRoutePrimaryOptions{},
	)
	require.NoError(t, err)
	assert.True(t, cleared.RoutingChanged)
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 77, Group: "vip", Model: "model-a",
	}).First(&fixedAbility).Error)
	assert.Equal(t, primaryPriority, *fixedAbility.Priority)

	var primaryAbility model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 76, Group: "vip", Model: "model-a",
	}).First(&primaryAbility).Error)
	assert.Equal(t, fixedBasePriority, *primaryAbility.Priority)
}

func TestPlanChannelSmartScheduleFirstRunSelectsRawWinner(t *testing.T) {
	firstScore := 0.80
	secondScore := 0.82
	scoring := defaultChannelSmartScheduleScoring()
	scoring.StabilityPercent = 100
	plan := planChannelSmartScheduleWithScoring(
		[]channelSmartScheduleCandidate{
			{
				ChannelId: 1, CurrentPriority: 80, CurrentWeight: 50,
				Stability: &firstScore, StabilitySampleCount: 5, StabilityAvailable: true,
			},
			{
				ChannelId: 2, CurrentPriority: 80, CurrentWeight: 50,
				Stability: &secondScore, StabilitySampleCount: 5, StabilityAvailable: true,
			},
		},
		channelMonitorSmartScheduleStrategyRatio,
		true,
		channelMonitorSmartScheduleApplyWeight,
		5,
		false,
		scoring,
	)

	require.Len(t, plan.Items, 2)
	assert.Equal(t, uint(100), plan.Items[0].TargetWeight)
	assert.Equal(t, uint(900), plan.Items[1].TargetWeight)
}

func TestPlanChannelSmartScheduleUsesConfiguredPrimaryTrafficAndDeterministicRounding(t *testing.T) {
	scores := []float64{0.90, 0.60, 0.30, 0}
	scoring := defaultChannelSmartScheduleScoring()
	scoring.StabilityPercent = 100
	scoring.PrimaryTrafficPercent = 75
	plan := planChannelSmartScheduleWithScoring(
		[]channelSmartScheduleCandidate{
			{ChannelId: 1, Stability: &scores[0], StabilitySampleCount: 5, StabilityAvailable: true},
			{ChannelId: 2, Stability: &scores[1], StabilitySampleCount: 5, StabilityAvailable: true},
			{ChannelId: 3, Stability: &scores[2], StabilitySampleCount: 5, StabilityAvailable: true},
			{ChannelId: 4, Stability: &scores[3], StabilitySampleCount: 5, StabilityAvailable: true},
		},
		channelMonitorSmartScheduleStrategyRatio,
		true,
		channelMonitorSmartScheduleApplyWeight,
		5,
		true,
		scoring,
	)

	require.Len(t, plan.Items, 4)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	totalWeight := uint(0)
	for _, item := range plan.Items {
		items[item.ChannelId] = item
		totalWeight += item.TargetWeight
	}
	assert.Equal(t, uint(1000), totalWeight)
	assert.Equal(t, uint(750), items[1].TargetWeight)
	assert.Equal(t, uint(165), items[2].TargetWeight)
	assert.Equal(t, uint(82), items[3].TargetWeight)
	assert.Equal(t, uint(3), items[4].TargetWeight)
}

func TestPlanChannelSmartScheduleMinimumPrimaryTrafficStaysStrictlyHigher(t *testing.T) {
	score := 0.90
	scoring := defaultChannelSmartScheduleScoring()
	scoring.StabilityPercent = 100
	scoring.PrimaryTrafficPercent = 51
	plan := planChannelSmartScheduleWithScoring(
		[]channelSmartScheduleCandidate{
			{ChannelId: 1, Stability: &score, StabilitySampleCount: 5, StabilityAvailable: true},
			{ChannelId: 2, Stability: &score, StabilitySampleCount: 5, StabilityAvailable: true},
		},
		channelMonitorSmartScheduleStrategyRatio,
		true,
		channelMonitorSmartScheduleApplyWeight,
		5,
		false,
		scoring,
	)

	require.Len(t, plan.Items, 2)
	assert.Equal(t, uint(510), plan.Items[0].TargetWeight)
	assert.Equal(t, uint(490), plan.Items[1].TargetWeight)
	assert.Greater(t, plan.Items[0].TargetWeight, plan.Items[1].TargetWeight)
}

func TestPlanChannelSmartSchedulePriorityWeightRanksHysteresisPrimaryBeforeRawChallenger(t *testing.T) {
	scores := []float64{0.80, 0.82, 0.50, 0.25}
	scoring := defaultChannelSmartScheduleScoring()
	scoring.StabilityPercent = 100
	plan := planChannelSmartScheduleWithScoring(
		[]channelSmartScheduleCandidate{
			{
				ChannelId: 1, CurrentPriority: 100, CurrentWeight: 1000,
				Stability: &scores[0], StabilitySampleCount: 5, StabilityAvailable: true,
			},
			{
				ChannelId: 2, CurrentPriority: 90, CurrentWeight: 1000,
				Stability: &scores[1], StabilitySampleCount: 5, StabilityAvailable: true,
			},
			{
				ChannelId: 3, CurrentPriority: 80, CurrentWeight: 500,
				Stability: &scores[2], StabilitySampleCount: 5, StabilityAvailable: true,
			},
			{
				ChannelId: 4, CurrentPriority: 80, CurrentWeight: 500,
				Stability: &scores[3], StabilitySampleCount: 5, StabilityAvailable: true,
			},
		},
		channelMonitorSmartScheduleStrategyRatio,
		true,
		channelMonitorSmartScheduleApplyPriorityWeight,
		5,
		false,
		scoring,
	)

	require.Len(t, plan.Items, 4)
	assert.Equal(t, int64(4), plan.Items[0].TargetPriority)
	assert.Equal(t, int64(3), plan.Items[1].TargetPriority)
	assert.Equal(t, int64(2), plan.Items[2].TargetPriority)
	assert.Equal(t, int64(1), plan.Items[3].TargetPriority)
	for _, item := range plan.Items {
		assert.Equal(t, uint(1000), item.TargetWeight)
	}
}

func TestFullScheduleQueuesExplorationForRuntimeRefresh(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 2, 80, 30,
	)
	sampleMode := channelMonitorSmartScheduleSampleTraffic
	policy.SampleMode = &sampleMode
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			policy,
		),
	})
	priorityLow := int64(20)
	priorityHigh := int64(100)
	weight := uint(50)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 61, Name: "insufficient", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priorityLow, Weight: &weight},
		{Id: 62, Name: "measured", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priorityHigh, Weight: &weight},
		{Id: 63, Name: "waiting", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priorityLow, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 61, Group: "vip", Model: "model-a", Enabled: true, Priority: &priorityLow, Weight: weight},
		{ChannelId: 62, Group: "vip", Model: "model-a", Enabled: true, Priority: &priorityHigh, Weight: weight},
		{ChannelId: 63, Group: "vip", Model: "model-a", Enabled: true, Priority: &priorityLow, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 61, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 62, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 63, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)
	completedMinute := time.Now().Unix()
	completedMinute = completedMinute - completedMinute%60 - 60
	logTime := completedMinute + 1
	require.NoError(t, db.Create(&[]model.Log{
		{ChannelId: 61, Group: "vip", ModelName: "model-a", CreatedAt: logTime, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":500}`},
		{ChannelId: 62, Group: "vip", ModelName: "model-a", CreatedAt: logTime, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":100}`},
		{ChannelId: 63, Group: "vip", ModelName: "model-a", CreatedAt: logTime, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":600}`},
		{ChannelId: 62, Group: "vip", ModelName: "model-a", CreatedAt: logTime, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":100}`},
	}).Error)
	require.NoError(t, aggregateChannelMonitorTestLogs(completedMinute, completedMinute+60))

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	assert.Equal(t, 3, result.Updated)

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 61, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(2), *ability.Priority)
	assert.Equal(t, uint(1000), ability.Weight)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 61, "vip", "model-a",
	).First(&state).Error)
	assert.Empty(t, state.TemporaryTrafficKind)
	assert.Equal(t, int64(2), state.BasePriority)
	conflict, err := refreshChannelSmartScheduleAdaptivePool(
		context.Background(), channelSmartScheduleRoutePoolKey{group: "vip", model: "model-a"},
		policy.policy(), getChannelMonitorSettings().SmartScheduleControlRevision, model.DB,
	)
	require.NoError(t, err)
	assert.False(t, conflict)
	require.NoError(t, db.Where(&model.Ability{ChannelId: 61, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(3), *ability.Priority)
	assert.Equal(t, uint(300), ability.Weight)
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 61, "vip", "model-a",
	).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleTemporaryTrafficExploration, state.TemporaryTrafficKind)
	assert.NotZero(t, state.TemporaryTrafficSince)
	assert.Equal(t, 3.0, state.TemporaryTrafficTargetPercent)
	ability = model.Ability{}
	require.NoError(t, db.Where(&model.Ability{ChannelId: 63, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(1), *ability.Priority)
	assert.Equal(t, uint(1000), ability.Weight)
	state = model.ChannelSmartScheduleRouteState{}
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 63, "vip", "model-a",
	).First(&state).Error)
	assert.Empty(t, state.TemporaryTrafficKind)
}

func TestRunChannelSmartScheduleCompletesExplorationBeforeFormalScoring(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 2, 80, 30,
	)
	sampleMode := channelMonitorSmartScheduleSampleTraffic
	policy.SampleMode = &sampleMode
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			policy,
		),
	})
	priorityHigh := int64(100)
	priorityLow := int64(20)
	weightHigh := uint(50)
	explorationWeight := uint(2)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 64, Name: "exploring", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priorityLow, Weight: &weightHigh},
		{Id: 65, Name: "measured", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priorityHigh, Weight: &weightHigh},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 64, Group: "vip", Model: "model-a", Enabled: true, Priority: &priorityHigh, Weight: explorationWeight},
		{ChannelId: 65, Group: "vip", Model: "model-a", Enabled: true, Priority: &priorityHigh, Weight: weightHigh},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{
			ChannelId: 64, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
			BaseRank: 2, BasePriority: priorityLow, BaseWeight: weightHigh,
			TemporaryTrafficKind:          model.ChannelSmartScheduleTemporaryTrafficExploration,
			TemporaryTrafficSince:         time.Now().Unix() - 60,
			TemporaryTrafficTargetPercent: 3,
		},
		{ChannelId: 65, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)
	completedMinute := time.Now().Unix()
	completedMinute = completedMinute - completedMinute%60 - 60
	logTime := completedMinute + 1
	require.NoError(t, db.Create(&[]model.Log{
		{ChannelId: 64, Group: "vip", ModelName: "model-a", CreatedAt: logTime, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":500}`},
		{ChannelId: 64, Group: "vip", ModelName: "model-a", CreatedAt: logTime, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":500}`},
		{ChannelId: 65, Group: "vip", ModelName: "model-a", CreatedAt: logTime, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":100}`},
		{ChannelId: 65, Group: "vip", ModelName: "model-a", CreatedAt: logTime, Type: model.LogTypeConsume, IsStream: true, Other: `{"frt":100}`},
	}).Error)
	fastFirstTokenMs := 100.0
	slowFirstTokenMs := 500.0
	for index := range 2 {
		require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
			64, "vip", "model-a", logTime+int64(index), true,
			&slowFirstTokenMs, nil, nil, false,
		))
		require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
			65, "vip", "model-a", logTime+int64(index), true,
			&fastFirstTokenMs, nil, nil, false,
		))
	}
	require.NoError(t, aggregateChannelMonitorTestLogs(completedMinute, completedMinute+60))

	_, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 64, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priorityHigh, *ability.Priority)
	assert.Equal(t, explorationWeight, ability.Weight)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 64, "vip", "model-a",
	).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleTemporaryTrafficExploration, state.TemporaryTrafficKind)
	assert.NotNil(t, state.LastScheduleScore)
	conflict, err := refreshChannelSmartScheduleAdaptivePool(
		context.Background(), channelSmartScheduleRoutePoolKey{group: "vip", model: "model-a"},
		policy.policy(), getChannelMonitorSettings().SmartScheduleControlRevision, model.DB,
	)
	require.NoError(t, err)
	assert.False(t, conflict)
	require.NoError(t, db.Where(&model.Ability{ChannelId: 64, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(1), *ability.Priority)
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 64, "vip", "model-a",
	).First(&state).Error)
	assert.Empty(t, state.TemporaryTrafficKind)
	assert.Zero(t, state.TemporaryTrafficSince)
}

func TestRuntimeRefreshUsesPressureBudgetToSampleUnknownBackup(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
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
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
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
	weight := uint(100)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 66, Name: "degrading-primary", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &primaryPriority, Weight: &weight},
		{Id: 67, Name: "unknown-backup", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &backupPriority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 66, Group: "vip", Model: "model-a", Enabled: true, Priority: &primaryPriority, Weight: weight},
		{ChannelId: 67, Group: "vip", Model: "model-a", Enabled: true, Priority: &backupPriority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 66, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 67, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)

	completedMinute := time.Now().Unix()
	completedMinute = completedMinute - completedMinute%60 - 60
	logs := make([]model.Log, 5)
	for index := range logs {
		logs[index] = model.Log{
			ChannelId: 66, Group: "vip", ModelName: "model-a",
			CreatedAt: completedMinute + int64(index+1), Type: model.LogTypeConsume,
			IsStream: true, Other: `{"frt":10000}`,
		}
	}
	require.NoError(t, db.Create(&logs).Error)
	firstTokenMs := 10_000.0
	for index := range logs {
		require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
			66, "vip", "model-a", completedMinute+int64(index+1), true,
			&firstTokenMs, nil, nil, false,
		))
	}
	require.NoError(t, aggregateChannelMonitorTestLogs(completedMinute, completedMinute+60))

	_, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	conflict, err := refreshChannelSmartScheduleAdaptivePool(
		context.Background(), channelSmartScheduleRoutePoolKey{group: "vip", model: "model-a"},
		policy.policy(), getChannelMonitorSettings().SmartScheduleControlRevision, model.DB,
	)
	require.NoError(t, err)
	assert.False(t, conflict)

	var primaryAbility model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 66, Group: "vip", Model: "model-a",
	}).First(&primaryAbility).Error)
	require.NotNil(t, primaryAbility.Priority)
	assert.Equal(t, int64(2), *primaryAbility.Priority)
	assert.Equal(t, uint(7000), primaryAbility.Weight)

	var backupAbility model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 67, Group: "vip", Model: "model-a",
	}).First(&backupAbility).Error)
	require.NotNil(t, backupAbility.Priority)
	assert.Equal(t, int64(2), *backupAbility.Priority)
	assert.Equal(t, uint(3000), backupAbility.Weight)

	var primaryState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 66, "vip", "model-a",
	).First(&primaryState).Error)
	assert.Equal(t, channelSmartScheduleHealthHighRisk, primaryState.AdaptiveHealthState)
	assert.InDelta(t, 1, primaryState.AdaptiveHealthPressure, 1e-9)
	assert.InDelta(t, 100, primaryState.AdaptiveHealthFirstTokenWarningRequestPercent, 1e-9)

	var backupState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 67, "vip", "model-a",
	).First(&backupState).Error)
	assert.Equal(t, model.ChannelSmartScheduleTemporaryTrafficAdaptive, backupState.TemporaryTrafficKind)
	assert.InDelta(t, 30, backupState.TemporaryTrafficTargetPercent, 1e-9)

	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	service.StartChannelRateLimitCooldown(67, "model-a", 60)
	conflict, err = refreshChannelSmartScheduleAdaptivePool(
		context.Background(), channelSmartScheduleRoutePoolKey{group: "vip", model: "model-a"},
		policy.policy(), getChannelMonitorSettings().SmartScheduleControlRevision, model.DB,
	)
	require.NoError(t, err)
	assert.False(t, conflict)
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: 67, Group: "vip", Model: "model-a",
	}).First(&backupAbility).Error)
	require.NotNil(t, backupAbility.Priority)
	assert.Equal(t, backupState.BasePriority, *backupAbility.Priority)
	assert.Equal(t, backupState.BaseWeight, backupAbility.Weight)
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 67, "vip", "model-a",
	).First(&backupState).Error)
	assert.Empty(t, backupState.TemporaryTrafficKind)
}

func TestRunChannelSmartScheduleFixedPrimaryIgnoresUnmanagedManualRoute(t *testing.T) {
	for _, test := range []struct {
		name           string
		applyMode      string
		fixedPriority  int64
		manualPriority int64
	}{
		{name: "priority weight with higher manual route", applyMode: channelMonitorSmartScheduleApplyPriorityWeight, fixedPriority: 10, manualPriority: 500},
		{name: "priority weight with same layer manual route", applyMode: channelMonitorSmartScheduleApplyPriorityWeight, fixedPriority: 500, manualPriority: 500},
		{name: "weight with higher manual route", applyMode: channelMonitorSmartScheduleApplyWeight, fixedPriority: 10, manualPriority: 500},
		{name: "weight with same layer manual route", applyMode: channelMonitorSmartScheduleApplyWeight, fixedPriority: 500, manualPriority: 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			policy := channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, false,
				test.applyMode, nil, 2, 80, 30,
			)
			useChannelMonitorOptionMap(t, map[string]string{
				channelMonitorSmartScheduleEnabledOption:       "true",
				channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
			})
			managedPriority := int64(20)
			weight := uint(50)
			require.NoError(t, db.Create(&[]model.Channel{
				{Id: 81, Name: "fixed", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &test.fixedPriority, Weight: &weight},
				{Id: 82, Name: "managed", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &managedPriority, Weight: &weight},
				{Id: 83, Name: "manual", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &test.manualPriority, Weight: &weight},
			}).Error)
			require.NoError(t, db.Create(&[]model.Ability{
				{ChannelId: 81, Group: "vip", Model: "model-a", Enabled: true, Priority: &test.fixedPriority, Weight: weight},
				{ChannelId: 82, Group: "vip", Model: "model-a", Enabled: true, Priority: &managedPriority, Weight: weight},
				{ChannelId: 83, Group: "vip", Model: "model-a", Enabled: true, Priority: &test.manualPriority, Weight: weight},
			}).Error)
			require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
				{ChannelId: 81, Ratio: 3, UpdatedTime: 1},
				{ChannelId: 82, Ratio: 1, UpdatedTime: 1},
				{ChannelId: 83, Ratio: 2, UpdatedTime: 1},
			}).Error)
			require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
				{
					ChannelId: 81, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
				},
				{ChannelId: 82, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
				{ChannelId: 83, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Excluded: true},
			}).Error)
			_, err := model.SaveChannelSmartScheduleRoutePrimary(
				81, "vip", "model-a", model.ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 30},
			)
			require.NoError(t, err)

			_, err = runChannelSmartScheduleOnce(context.Background(), nil, false)
			require.NoError(t, err)
			var fixedAbility model.Ability
			require.NoError(t, db.Where(&model.Ability{ChannelId: 81, Group: "vip", Model: "model-a"}).First(&fixedAbility).Error)
			require.NotNil(t, fixedAbility.Priority)
			expectedFixedPriority := max(test.fixedPriority, managedPriority+1)
			assert.Equal(t, expectedFixedPriority, *fixedAbility.Priority)
			var manualAbility model.Ability
			require.NoError(t, db.Where(&model.Ability{ChannelId: 83, Group: "vip", Model: "model-a"}).First(&manualAbility).Error)
			require.NotNil(t, manualAbility.Priority)
			assert.Equal(t, test.manualPriority, *manualAbility.Priority)
			assert.Equal(t, weight, manualAbility.Weight)
		})
	}
}

func TestPlanChannelSmartScheduleForceResetSelectsRawWinnerAndPreservesWeightCohort(t *testing.T) {
	ratioLow := 1.0
	ratioHigh := 3.0
	plan := planChannelSmartSchedule([]channelSmartScheduleCandidate{
		{ChannelId: 1, CurrentPriority: 100, CurrentWeight: 100, Ratio: &ratioLow},
		{ChannelId: 2, CurrentPriority: 100, CurrentWeight: 900, Ratio: &ratioHigh},
	}, channelMonitorSmartScheduleStrategyRatio, false, channelMonitorSmartScheduleApplyWeight, 5, true)

	require.Len(t, plan.Items, 2)
	assert.Empty(t, plan.Skipped)
	items := make(map[int]channelSmartSchedulePlanItem, len(plan.Items))
	for _, item := range plan.Items {
		items[item.ChannelId] = item
	}
	assert.Equal(t, int64(100), items[1].TargetPriority)
	assert.Equal(t, uint(900), items[1].TargetWeight)
	assert.Equal(t, int64(100), items[2].TargetPriority)
	assert.Equal(t, uint(100), items[2].TargetWeight)

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
	assert.Equal(t, int64(3), items[1].TargetPriority)
	assert.Equal(t, uint(1000), items[1].TargetWeight)
	assert.Equal(t, int64(2), items[2].TargetPriority)
	assert.Equal(t, uint(1000), items[2].TargetWeight)
	assert.Equal(t, int64(1), items[3].TargetPriority)
	assert.Equal(t, uint(1000), items[3].TargetWeight)
}
