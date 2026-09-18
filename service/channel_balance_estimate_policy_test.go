package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelBalanceDisableThresholdIgnoresWarningThreshold(t *testing.T) {
	for _, test := range []struct {
		name    string
		warning *float64
	}{
		{"未设置预警值", nil},
		{"余额高于预警值", common.GetPointer(1.0)},
		{"余额等于预警值", common.GetPointer(24.0)},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newChannelBalanceFixture(t)
			f.monitor.BalanceWarningThreshold = test.warning
			f.config = ChannelBalanceConfigForMonitor(f.monitor)
			f.sync(24)
			f.operation("finish", "completed", "model", 2_000_000, false, 0)
			estimate := f.operation("start", "active", "model", 17_000_000, false, 0)
			assert.Equal(t, 5.0, estimate.PolicyBalance)
			assert.Equal(t, "ok", estimate.Decision, "等于禁用阈值时保持启用")

			estimate = f.operation("start", "crossing", "model", 1_000_000, false, 0)
			assert.Equal(t, 2.0, estimate.CompletedConsumption)
			assert.Equal(t, 18.0, estimate.InFlightConsumption)
			assert.Equal(t, 4.0, estimate.PolicyBalance)
			assert.Equal(t, "low", estimate.Decision)

			estimate = f.operation("finish", "crossing", "model", 0, false, 0)
			assert.Equal(t, 5.0, estimate.PolicyBalance)
			assert.Equal(t, "ok", estimate.Decision, "实际消费低于预估后重新判断恢复")
		})
	}
}

func TestChannelBalanceSharedPoliciesIgnoreWarningThreshold(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.monitor.UpstreamAccountID, f.monitor.UpstreamAccountRevision = 9, 1
	f.config = ChannelBalanceConfigForMonitor(f.monitor)
	f.sync(24)
	policies, err := common.Marshal([]map[string]any{
		{"id": 41, "enabled": true, "warning": nil, "threshold": 5_000_000},
		{"id": 42, "enabled": true, "warning": 1_000_000, "threshold": 2_000_000},
		{"id": 43, "enabled": true, "warning": 30_000_000, "threshold": nil},
	})
	require.NoError(t, err)
	args, err := channelBalanceConfigArguments(f.config, common.GetUUID())
	require.NoError(t, err)
	_, err = runChannelBalanceOperation(t.Context(), f.config, "configure", "", "", append(args, string(policies))...)
	require.NoError(t, err)

	estimate := f.operation("start", "first", "model", 20_000_000, false, 0)
	assert.Equal(t, 4.0, estimate.PolicyBalance)
	assert.Equal(t, "41:low,42:ok,43:unknown", estimate.Decision)
	estimate = f.operation("start", "second", "model", 3_000_000, false, 0)
	assert.Equal(t, 1.0, estimate.PolicyBalance)
	assert.Equal(t, "41:low,42:low,43:unknown", estimate.Decision)
}

func TestChannelBalancePolicyCoalescesConcurrentTransitionsWithoutLosingRecovery(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(6)
	f.operation("start", "request", "model", 3_000_000, false, 0)
	var decisions []string
	duplicateEffects := 0
	handler := func(ctx context.Context, config ChannelBalanceConfig, estimate ChannelBalanceEstimate) (bool, error) {
		decisions = append(decisions, estimate.Decision)
		if estimate.Decision == "low" {
			// Another node arrives while this node holds the effect lease.
			err := applyChannelBalancePolicyTransitions(ctx, config,
				func(context.Context, ChannelBalanceConfig, ChannelBalanceEstimate) (bool, error) {
					duplicateEffects++
					return true, nil
				})
			require.NoError(t, err)
			// The request completes while the disable effect is still in flight.
			// No subsequent request is needed to deliver the recovery transition.
			f.operation("finish", "request", "model", 1_000_000, false, 0)
		}
		return true, nil
	}
	require.NoError(t, applyChannelBalancePolicyTransitions(t.Context(), f.config, handler))
	assert.Equal(t, []string{"low", "ok"}, decisions)
	assert.Zero(t, duplicateEffects)
	require.NoError(t, applyChannelBalancePolicyTransitions(t.Context(), f.config, handler))
	assert.Equal(t, []string{"low", "ok"}, decisions, "an unchanged threshold decision performs no effect or SQL")
	estimate, err := GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.Equal(t, "ok", estimate.AppliedDecision)
	assert.Nil(t, model.DB)
}
