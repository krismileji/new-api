package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
