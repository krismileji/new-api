package model

import (
	"errors"
	"math"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/shopspring/decimal"
)

// TaskChannelCostSnapshot freezes the upstream conversion independently of the
// user's group price. It is stored in the task's existing private-data JSON.
type TaskChannelCostSnapshot struct {
	CostRatioCNY float64 `json:"cost_ratio_cny"`
}

// ExpressionCost uses the frozen expression and quota unit, before applying a
// group ratio or rounding/saturating the user's charge. A zero user charge can
// therefore still have a nonzero upstream cost.
func (s *TaskChannelCostSnapshot) ExpressionCost(snap *billingexpr.BillingSnapshot) (int64, error) {
	if s == nil || snap == nil || !snap.TaskUsageBilling ||
		math.IsNaN(s.CostRatioCNY) || math.IsInf(s.CostRatioCNY, 0) || s.CostRatioCNY < 0 ||
		math.IsNaN(snap.QuotaPerUnit) || math.IsInf(snap.QuotaPerUnit, 0) || snap.QuotaPerUnit <= 0 {
		return 0, errors.New("任务表达式成本快照无效")
	}
	result, err := billingexpr.ComputeTieredQuotaWithRequest(snap, billingexpr.TokenParams{}, billingexpr.RequestInput{Usage: snap.UsageFacts})
	if err != nil {
		return 0, err
	}
	quota := result.ActualQuotaBeforeGroup
	if math.IsNaN(quota) || math.IsInf(quota, 0) || quota < 0 {
		return 0, errors.New("任务表达式成本无效")
	}
	cost := decimal.NewFromFloat(quota).Div(decimal.NewFromFloat(snap.QuotaPerUnit)).
		Mul(decimal.NewFromFloat(s.CostRatioCNY)).Mul(decimal.NewFromInt(ChannelDailyCostNanoPerCNY)).Round(0)
	if cost.IsNegative() || cost.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, errors.New("任务表达式成本超出范围")
	}
	return cost.IntPart(), nil
}
