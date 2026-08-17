package service

import (
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

const (
	channelModelDetectionEstimateSafetyMargin     = 1.05
	channelModelDetectionEstimateDefaultMaxTokens = 8192
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

// EstimateChannelModelDetectionQuota calculates a conservative pre-group
// estimate for model detection only. It never writes channel daily costs.
func EstimateChannelModelDetectionQuota(info *relaycommon.RelayInfo, maxTokens int, snapshot ChannelModelDetectionCostSnapshot) (int64, bool) {
	return calculateChannelModelDetectionRequestQuota(info, maxTokens, snapshot, channelModelDetectionEstimateSafetyMargin)
}

// CalculateChannelModelDetectionRequestQuota prices one request exactly as it
// was sent, without the preview-only safety margin. It is used only after the
// request crosses the upstream transport boundary.
func CalculateChannelModelDetectionRequestQuota(info *relaycommon.RelayInfo, maxTokens int, snapshot ChannelModelDetectionCostSnapshot) (int64, bool) {
	return calculateChannelModelDetectionRequestQuota(info, maxTokens, snapshot, 1)
}

func calculateChannelModelDetectionRequestQuota(info *relaycommon.RelayInfo, maxTokens int, snapshot ChannelModelDetectionCostSnapshot, multiplier float64) (int64, bool) {
	if info == nil {
		return 0, false
	}
	quotaPerUnit := common.QuotaPerUnit
	if snapshot.QuotaPerUnit != nil {
		quotaPerUnit = float64(*snapshot.QuotaPerUnit)
	}
	estimate := channelModelDetectionEstimateQuotaBeforeGroup(info, maxTokens, quotaPerUnit)
	estimate = validChannelModelDetectionEstimate(estimate * multiplier)
	if estimate == 0 {
		return 0, true
	}
	quota, clamp := common.QuotaFromDecimalChecked(decimal.NewFromFloat(estimate).Ceil())
	if clamp != nil || quota < 0 {
		return 0, false
	}
	return int64(quota), true
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

// The following helpers price a request before group markup. The preview adds
// a safety margin; runtime request settlement does not.
func channelModelDetectionEstimateQuotaBeforeGroup(info *relaycommon.RelayInfo, maxTokens int, quotaPerUnit float64) float64 {
	if info == nil {
		return 0
	}
	toolQuota := channelModelDetectionEstimateToolQuota(info, quotaPerUnit)
	if snapshot := info.TieredBillingSnapshot; snapshot != nil {
		return validChannelModelDetectionEstimate(snapshot.EstimatedQuotaBeforeGroup + toolQuota)
	}

	priceData := info.PriceData
	if priceData.UsePrice {
		quotaPerUnit = channelModelDetectionEstimateQuotaPerUnitOrDefault(quotaPerUnit)
		if quotaPerUnit == 0 {
			return 0
		}
		quota := priceData.ApplyOtherRatiosToFloat(priceData.ModelPrice * quotaPerUnit)
		return validChannelModelDetectionEstimate(quota + toolQuota)
	}
	if priceData.ModelRatio <= 0 {
		return 0
	}

	promptTokens := max(info.GetEstimatePromptTokens(), common.PreConsumedQuota)
	completionTokens := 0
	if channelModelDetectionMayGenerateOutput(info) {
		completionTokens = channelModelDetectionEstimateDefaultMaxTokens
		if maxTokens > 0 {
			completionTokens = maxTokens
		}
	}
	inputRatio := max(1,
		priceData.CacheRatio,
		priceData.CacheCreationRatio,
		priceData.CacheCreation5mRatio,
		priceData.CacheCreation1hRatio,
		priceData.ImageRatio,
		priceData.AudioRatio,
	)
	completionRatio := max(priceData.CompletionRatio, priceData.AudioRatio*priceData.AudioCompletionRatio)
	if math.IsNaN(inputRatio) || math.IsInf(inputRatio, 0) || math.IsNaN(completionRatio) || math.IsInf(completionRatio, 0) {
		return 0
	}
	quota := (float64(promptTokens)*inputRatio + float64(completionTokens)*completionRatio) * priceData.ModelRatio
	quota = priceData.ApplyOtherRatiosToFloat(quota)
	quota += toolQuota
	return validChannelModelDetectionEstimate(quota)
}

func channelModelDetectionEstimateToolQuota(info *relaycommon.RelayInfo, quotaPerUnit float64) float64 {
	if info == nil {
		return 0
	}
	quotaPerUnit = channelModelDetectionEstimateQuotaPerUnitOrDefault(quotaPerUnit)
	if quotaPerUnit == 0 {
		return 0
	}

	var quota float64
	if info.ResponsesUsageInfo != nil {
		for name, tool := range info.ResponsesUsageInfo.BuiltInTools {
			if tool == nil {
				continue
			}
			price := operation_setting.GetToolPriceForModel(name, info.OriginModelName)
			if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
				continue
			}
			callCount := tool.CallCount
			if callCount <= 0 {
				callCount = 1
			}
			quota += price * float64(callCount) / 1000 * quotaPerUnit
		}
	}
	if info.RelayMode == relayconstant.RelayModeAlphaSearch {
		price := operation_setting.GetToolPriceForModel(dto.BuildInToolWebSearchPreview, info.OriginModelName)
		if price > 0 && !math.IsNaN(price) && !math.IsInf(price, 0) {
			quota = max(quota, price/1000*quotaPerUnit)
		}
	}
	return validChannelModelDetectionEstimate(quota)
}

func channelModelDetectionEstimateQuotaPerUnitOrDefault(quotaPerUnit float64) float64 {
	if quotaPerUnit > 0 && !math.IsNaN(quotaPerUnit) && !math.IsInf(quotaPerUnit, 0) {
		return quotaPerUnit
	}
	if common.QuotaPerUnit > 0 && !math.IsNaN(common.QuotaPerUnit) && !math.IsInf(common.QuotaPerUnit, 0) {
		return common.QuotaPerUnit
	}
	return 0
}

func channelModelDetectionMayGenerateOutput(info *relaycommon.RelayInfo) bool {
	if info == nil || info.IsGeminiBatchEmbedding {
		return false
	}
	switch info.RelayMode {
	case relayconstant.RelayModeChatCompletions,
		relayconstant.RelayModeCompletions,
		relayconstant.RelayModeResponses,
		relayconstant.RelayModeResponsesCompact,
		relayconstant.RelayModeRealtime,
		relayconstant.RelayModeGemini,
		relayconstant.RelayModeAlphaSearch:
		return true
	default:
		return false
	}
}

func validChannelModelDetectionEstimate(quota float64) float64 {
	if quota <= 0 || math.IsNaN(quota) || math.IsInf(quota, 0) {
		return 0
	}
	if quota > float64(common.MaxQuota) {
		return float64(common.MaxQuota)
	}
	return quota
}
