package service

import (
	"math"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const (
	channelDailyCostEstimateSafetyMargin     = 1.05
	channelDailyCostEstimateDefaultMaxTokens = 8192
)

func channelDailyCostEstimateQuotaBeforeGroup(info *relaycommon.RelayInfo, maxTokens int, quotaPerUnit float64) float64 {
	if info == nil {
		return 0
	}
	toolQuota := channelDailyCostEstimateToolQuota(info, quotaPerUnit)
	if snapshot := info.TieredBillingSnapshot; snapshot != nil {
		return validChannelDailyCostEstimate(snapshot.EstimatedQuotaBeforeGroup + toolQuota)
	}

	priceData := info.PriceData
	if priceData.UsePrice {
		quotaPerUnit = channelDailyCostEstimateQuotaPerUnitOrDefault(quotaPerUnit)
		if quotaPerUnit == 0 {
			return 0
		}
		quota := priceData.ApplyOtherRatiosToFloat(priceData.ModelPrice * quotaPerUnit)
		return validChannelDailyCostEstimate(quota + toolQuota)
	}
	if priceData.ModelRatio <= 0 {
		return 0
	}

	promptTokens := max(info.GetEstimatePromptTokens(), common.PreConsumedQuota)
	completionTokens := 0
	if channelDailyCostMayGenerateOutput(info) {
		completionTokens = channelDailyCostEstimateDefaultMaxTokens
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
	return validChannelDailyCostEstimate(quota)
}

func channelDailyCostEstimateToolQuota(info *relaycommon.RelayInfo, quotaPerUnit float64) float64 {
	if info == nil {
		return 0
	}
	quotaPerUnit = channelDailyCostEstimateQuotaPerUnitOrDefault(quotaPerUnit)
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
	return validChannelDailyCostEstimate(quota)
}

func channelDailyCostEstimateQuotaPerUnitOrDefault(quotaPerUnit float64) float64 {
	if quotaPerUnit > 0 && !math.IsNaN(quotaPerUnit) && !math.IsInf(quotaPerUnit, 0) {
		return quotaPerUnit
	}
	if common.QuotaPerUnit > 0 && !math.IsNaN(common.QuotaPerUnit) && !math.IsInf(common.QuotaPerUnit, 0) {
		return common.QuotaPerUnit
	}
	return 0
}

func channelDailyCostMayGenerateOutput(info *relaycommon.RelayInfo) bool {
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

func validChannelDailyCostEstimate(quota float64) float64 {
	if quota <= 0 || math.IsNaN(quota) || math.IsInf(quota, 0) {
		return 0
	}
	if quota > float64(common.MaxQuota) {
		return float64(common.MaxQuota)
	}
	return quota
}
