package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRelayFastFailureRetryBudgetDoesNotConsumeOrdinaryRetry(t *testing.T) {
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 5, 80, 30,
	)
	count := 2
	delayMs := 750
	policy.FastFailureSameChannelRetryCount = &count
	policy.FastFailureRetryDelayMs = &delayMs
	budget := &relayFastFailureRetryBudget{
		loaded:   true,
		enabled:  true,
		policies: smartScheduleGroupPolicies{policy},
	}

	decision, delay := budget.decide(
		"vip", "model-a", 7, time.Second, true, false,
	)
	assert.Equal(t, relayRetryFastFailureSameChannel, decision)
	assert.Equal(t, 750*time.Millisecond, delay)
	decision, delay = budget.decide(
		"vip", "model-a", 7, 500*time.Millisecond, true, false,
	)
	assert.Equal(t, relayRetryFastFailureSameChannel, decision)
	assert.Equal(t, 750*time.Millisecond, delay)
	decision, delay = budget.decide(
		"vip", "model-a", 7, 500*time.Millisecond, true, false,
	)
	assert.Equal(t, relayRetryNone, decision)
	assert.Zero(t, delay)

	decision, delay = budget.decide(
		"vip", "model-a", 7, 500*time.Millisecond, true, true,
	)
	assert.Equal(t, relayRetryOrdinary, decision)
	assert.Zero(t, delay)
	decision, delay = budget.decide(
		"vip", "model-a", 7, 500*time.Millisecond, true, false,
	)
	assert.Equal(t, relayRetryFastFailureSameChannel, decision)
	assert.Equal(t, 750*time.Millisecond, delay)
}

func TestRelayFastFailureRetryBudgetRequiresMatchingFastFailure(t *testing.T) {
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 5, 80, 30,
	)
	count := 1
	policy.FastFailureSameChannelRetryCount = &count

	for _, test := range []struct {
		name      string
		enabled   bool
		group     string
		modelName string
		duration  time.Duration
	}{
		{name: "disabled schedule", enabled: false, group: "vip", modelName: "model-a", duration: time.Millisecond},
		{name: "different group", enabled: true, group: "default", modelName: "model-a", duration: time.Millisecond},
		{name: "different model", enabled: true, group: "vip", modelName: "model-b", duration: time.Millisecond},
		{name: "slow failure", enabled: true, group: "vip", modelName: "model-a", duration: time.Second + time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			budget := &relayFastFailureRetryBudget{
				loaded:   true,
				enabled:  test.enabled,
				policies: smartScheduleGroupPolicies{policy},
			}
			decision, delay := budget.decide(
				test.group, test.modelName, 7, test.duration, true, true,
			)
			assert.Equal(t, relayRetryOrdinary, decision)
			assert.Zero(t, delay)
		})
	}
}

func TestRelayFastFailureRetryBudgetLoadsPolicyForSelectedAutoGroup(t *testing.T) {
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 5, 80, 30,
	)
	count := 1
	policy.FastFailureSameChannelRetryCount = &count
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyAutoGroup, "vip")

	budget := &relayFastFailureRetryBudget{}
	decision, delay := budget.decide(
		relayRetryGroup(ctx, "auto"), "model-a", 7, time.Millisecond, true, false,
	)
	assert.Equal(t, relayRetryFastFailureSameChannel, decision)
	assert.Equal(t, time.Second, delay)
}

func TestWaitForRelayFastFailureRetryHonorsCancellationAndZeroDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.False(t, waitForRelayFastFailureRetry(ctx, time.Hour))
	assert.True(t, waitForRelayFastFailureRetry(ctx, 0))
}

func TestFastFailureSameChannelRetrySkipsDeterministicChannelConfigurationErrors(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	for _, errorCode := range []types.ErrorCode{
		types.ErrorCodeChannelInvalidBaseURL,
		types.ErrorCodeChannelParamOverrideInvalid,
		types.ErrorCodeChannelHeaderOverrideInvalid,
		types.ErrorCodeChannelModelMappedError,
	} {
		apiErr := types.NewErrorWithStatusCode(errors.New("invalid channel configuration"), errorCode, http.StatusBadGateway)
		assert.False(t, isFastFailureSameChannelRetryable(ctx, apiErr), string(errorCode))
	}
}
