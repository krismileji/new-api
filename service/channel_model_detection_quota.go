package service

import (
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// ChannelModelDetectionQuotaResult is a calculation-only settlement. It does
// not touch user quota, tokens, subscriptions, logs, or ChannelDailyCost.
type ChannelModelDetectionQuotaResult struct {
	SettledQuota   int64
	CostBasisQuota int64
	Usage          ChannelModelDetectorUsage
	Reliable       bool
}

// AlignChannelModelDetectionCostSnapshot makes the event use the same quota
// unit already frozen by tiered billing. This prevents a concurrent settings
// update from mixing two quota units within one attempt.
func AlignChannelModelDetectionCostSnapshot(info *relaycommon.RelayInfo, snapshot ChannelModelDetectionCostSnapshot) (ChannelModelDetectionCostSnapshot, error) {
	if info == nil || info.TieredBillingSnapshot == nil {
		return snapshot, nil
	}
	quotaPerUnit := info.TieredBillingSnapshot.QuotaPerUnit
	if quotaPerUnit <= 0 || math.IsNaN(quotaPerUnit) || math.IsInf(quotaPerUnit, 0) {
		return ChannelModelDetectionCostSnapshot{}, model.ErrChannelModelDetectionInvalidCost
	}
	quotaPerUnitDecimal := decimal.NewFromFloat(quotaPerUnit)
	if !quotaPerUnitDecimal.Equal(quotaPerUnitDecimal.Truncate(0)) || quotaPerUnitDecimal.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return ChannelModelDetectionCostSnapshot{}, model.ErrChannelModelDetectionInvalidCost
	}
	value := quotaPerUnitDecimal.IntPart()
	snapshot.QuotaPerUnit = &value
	return snapshot, nil
}

// CalculateChannelModelDetectionQuota reuses the normal text settlement math
// while deliberately omitting all account mutation and aggregate cost writes.
func CalculateChannelModelDetectionQuota(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) ChannelModelDetectionQuotaResult {
	return calculateChannelModelDetectionQuota(ctx, info, usage, common.QuotaPerUnit)
}

func CalculateChannelModelDetectionQuotaWithSnapshot(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage, snapshot ChannelModelDetectionCostSnapshot) ChannelModelDetectionQuotaResult {
	if snapshot.QuotaPerUnit == nil || *snapshot.QuotaPerUnit <= 0 {
		return ChannelModelDetectionQuotaResult{}
	}
	return calculateChannelModelDetectionQuota(ctx, info, usage, float64(*snapshot.QuotaPerUnit))
}

func calculateChannelModelDetectionQuota(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage, quotaPerUnit float64) ChannelModelDetectionQuotaResult {
	if ctx == nil || info == nil || usage == nil || !channelDailyCostUsageIsAuthoritative(ctx, usage) {
		return ChannelModelDetectionQuotaResult{}
	}
	if quotaPerUnit <= 0 || math.IsNaN(quotaPerUnit) || math.IsInf(quotaPerUnit, 0) {
		return ChannelModelDetectionQuotaResult{}
	}
	billingUsage := effectiveBillingUsage(usage)
	normalizedUsage, ok := normalizeChannelModelDetectionAuthoritativeUsage(billingUsage)
	if !ok {
		return ChannelModelDetectionQuotaResult{}
	}

	summary := calculateTextQuotaSummaryWithQuotaPerUnit(ctx, info, billingUsage, quotaPerUnit)
	settledQuota := summary.Quota
	costBasisQuota := int64(0)

	if info.TieredBillingSnapshot != nil {
		usedVars := billingexpr.UsedVars(info.TieredBillingSnapshot.ExprString)
		tieredOK, tieredQuota, tieredResult := TryTieredSettle(info, BuildTieredTokenParams(billingUsage, summary.IsClaudeUsageSemantic, usedVars))
		if !tieredOK || tieredResult == nil {
			return ChannelModelDetectionQuotaResult{Usage: normalizedUsage}
		}
		settledQuota = composeTieredTextQuota(info, summary, tieredQuota, tieredResult)

		copiedInfo := *info
		copiedInfo.PriceData = info.PriceData
		copiedInfo.PriceData.GroupRatioInfo.GroupRatio = 1
		copiedInfo.QuotaClamp = nil
		baseSummary := calculateTextQuotaSummaryWithQuotaPerUnit(ctx, &copiedInfo, billingUsage, quotaPerUnit)
		basis, clamp := common.QuotaFromDecimalChecked(
			decimal.NewFromFloat(tieredResult.ActualQuotaBeforeGroup).Add(baseSummary.ToolCallSurchargeQuota),
		)
		if clamp != nil || copiedInfo.QuotaClamp != nil || basis < 0 {
			return ChannelModelDetectionQuotaResult{Usage: normalizedUsage}
		}
		costBasisQuota = int64(basis)
	} else {
		copiedInfo := *info
		copiedInfo.PriceData = info.PriceData
		copiedInfo.PriceData.GroupRatioInfo.GroupRatio = 1
		copiedInfo.QuotaClamp = nil
		baseSummary := calculateTextQuotaSummaryWithQuotaPerUnit(ctx, &copiedInfo, billingUsage, quotaPerUnit)
		if copiedInfo.QuotaClamp != nil || baseSummary.Quota < 0 {
			return ChannelModelDetectionQuotaResult{Usage: normalizedUsage}
		}
		costBasisQuota = int64(baseSummary.Quota)
	}

	if info.QuotaClamp != nil || settledQuota < 0 {
		return ChannelModelDetectionQuotaResult{Usage: normalizedUsage}
	}
	return ChannelModelDetectionQuotaResult{
		SettledQuota:   int64(settledQuota),
		CostBasisQuota: costBasisQuota,
		Usage:          normalizedUsage,
		Reliable:       true,
	}
}

func normalizeChannelModelDetectionAuthoritativeUsage(usage *dto.Usage) (ChannelModelDetectorUsage, bool) {
	if usage == nil {
		return ChannelModelDetectorUsage{}, false
	}
	inputTokens := usage.InputTokens
	if inputTokens == 0 {
		inputTokens = usage.PromptTokens
	}
	outputTokens := usage.OutputTokens
	if outputTokens == 0 {
		outputTokens = usage.CompletionTokens
	}
	if inputTokens < 0 || outputTokens < 0 || int64(inputTokens) > math.MaxInt64-int64(outputTokens) {
		return ChannelModelDetectorUsage{}, false
	}
	totalTokens := int64(inputTokens) + int64(outputTokens)
	return ChannelModelDetectorUsage{
		Available:    true,
		Source:       model.ChannelModelDetectionUsageUpstreamAuthoritative,
		InputTokens:  int64(inputTokens),
		OutputTokens: int64(outputTokens),
		TotalTokens:  totalTokens,
	}, true
}
