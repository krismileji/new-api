package controller

import (
	"context"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

type relayRetryDecision uint8

const (
	relayRetryNone relayRetryDecision = iota
	relayRetryFastFailureSameChannel
	relayRetryOrdinary
)

type relayFastFailureRetryBudget struct {
	loaded    bool
	enabled   bool
	policies  smartScheduleGroupPolicies
	channelID int
	used      int
}

func (budget *relayFastFailureRetryBudget) decide(
	group string,
	modelName string,
	channelID int,
	attemptDuration time.Duration,
	fastFailureRetryable bool,
	ordinaryRetryable bool,
) (relayRetryDecision, time.Duration) {
	if fastFailureRetryable {
		if retryDelay, ok := budget.take(group, modelName, channelID, attemptDuration); ok {
			return relayRetryFastFailureSameChannel, retryDelay
		}
	}
	if ordinaryRetryable {
		budget.resetChannelVisit()
		return relayRetryOrdinary, 0
	}
	return relayRetryNone, 0
}

func (budget *relayFastFailureRetryBudget) take(
	group string,
	modelName string,
	channelID int,
	attemptDuration time.Duration,
) (time.Duration, bool) {
	if channelID <= 0 || attemptDuration < 0 {
		return 0, false
	}
	if budget.channelID != channelID {
		budget.channelID = channelID
		budget.used = 0
	}
	if !budget.loaded {
		settings := getChannelMonitorSettings()
		budget.loaded = true
		budget.enabled = settings.SmartScheduleEnabled
		budget.policies = settings.SmartScheduleGroupPolicies
	}
	if !budget.enabled {
		return 0, false
	}

	for _, configured := range budget.policies {
		if configured.Group != group {
			continue
		}
		policy := configured.policy()
		if len(policy.Models) > 0 && !slices.Contains(policy.Models, modelName) {
			return 0, false
		}
		if policy.FastFailureSameChannelRetryCount <= 0 ||
			budget.used >= policy.FastFailureSameChannelRetryCount {
			return 0, false
		}
		fastFailureThreshold := time.Duration(policy.FastFailureSeconds * float64(time.Second))
		if attemptDuration > fastFailureThreshold {
			return 0, false
		}
		budget.used++
		return time.Duration(policy.FastFailureRetryDelayMs) * time.Millisecond, true
	}
	return 0, false
}

func waitForRelayFastFailureRetry(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (budget *relayFastFailureRetryBudget) resetChannelVisit() {
	budget.channelID = 0
	budget.used = 0
}

func relayRetryGroup(c *gin.Context, tokenGroup string) string {
	if tokenGroup == "auto" {
		return common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	}
	return tokenGroup
}

func isFastFailureSameChannelRetryable(c *gin.Context, err *types.NewAPIError) bool {
	if err == nil || types.IsSkipRetryError(err) || types.IsClientGoneError(err) {
		return false
	}
	switch err.GetErrorCode() {
	case types.ErrorCodeChannelInvalidBaseURL,
		types.ErrorCodeChannelParamOverrideInvalid,
		types.ErrorCodeChannelHeaderOverrideInvalid,
		types.ErrorCodeChannelModelMappedError:
		// These failures are deterministic for the selected channel. Retrying the
		// same channel only delays the ordinary retry that can select another one.
		return false
	}
	if _, specificChannel := c.Get("specific_channel_id"); specificChannel {
		return false
	}
	return shouldRetry(c, err, 1)
}
