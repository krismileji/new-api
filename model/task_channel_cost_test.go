package model

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskExpressionChannelCostIgnoresUserChargeLimits(t *testing.T) {
	for _, group := range []float64{0, 0.00001, 1, 1e10} {
		snap := &billingexpr.BillingSnapshot{ExprString: `u("units") * 0.01`, TaskUsageBilling: true,
			UsageFacts: map[string]any{"units": 2}, QuotaPerUnit: 500000, GroupRatio: group}
		cost, err := (&TaskChannelCostSnapshot{CostRatioCNY: 4}).ExpressionCost(snap)
		require.NoError(t, err)
		assert.Equal(t, int64(80000000), cost, "cost must not depend on zero, rounded or saturated user quota")
	}
}

func TestTaskExpressionChannelCostRejectsUnusablePricing(t *testing.T) {
	for _, tc := range []struct {
		name, expression string
		ratio, unit      float64
	}{
		{"negative result", "-1", 4, 500000},
		{"overflow", "1e100", 4, 500000},
		{"missing usage", `u("missing")`, 4, 500000},
		{"invalid conversion", "0.01", math.Inf(1), 500000},
		{"invalid unit", "0.01", 4, math.NaN()},
		{"zero unit", "0.01", 4, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := &billingexpr.BillingSnapshot{ExprString: tc.expression, TaskUsageBilling: true,
				QuotaPerUnit: tc.unit, GroupRatio: 1}
			_, err := (&TaskChannelCostSnapshot{CostRatioCNY: tc.ratio}).ExpressionCost(snap)
			require.Error(t, err, "invalid pricing must not become a resolved free task")
		})
	}
}
