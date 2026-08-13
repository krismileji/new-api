package service

import (
	"math"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

const (
	channelDailyCostEstimateSafetyMargin      = 1.05
	channelDailyCostEstimateDefaultMaxTokens  = 8192
	channelDailyCostEstimateCalibrationWindow = 32
	channelDailyCostEstimateMaxProfiles       = 4096
)

type channelDailyCostEstimateProfileKey struct {
	ChannelId int
	ModelName string
}

type channelDailyCostEstimateProfile struct {
	mu          sync.RWMutex
	ratios      [channelDailyCostEstimateCalibrationWindow]float64
	count       int
	next        int
	calibration float64
}

type channelDailyCostEstimateCalibrator struct {
	profiles sync.Map
	count    atomic.Int64
}

var dailyCostEstimateCalibrator channelDailyCostEstimateCalibrator

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

func channelDailyCostEstimatePerCallQuotaBeforeGroup(modelName string, priceData types.PriceData, quotaPerUnit float64) float64 {
	quotaPerUnit = channelDailyCostEstimateQuotaPerUnitOrDefault(quotaPerUnit)
	if quotaPerUnit == 0 {
		return 0
	}
	quota := priceData.ModelPrice * quotaPerUnit
	if !priceData.UsePrice {
		quota = priceData.ModelRatio / 2 * quotaPerUnit
	}
	if !common.StringsContains(constant.TaskPricePatches, modelName) {
		quota = priceData.ApplyOtherRatiosToFloat(quota)
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

func channelDailyCostEstimatedQuota(channelId int, modelName string, quotaBeforeGroup float64) float64 {
	quotaBeforeGroup = validChannelDailyCostEstimate(quotaBeforeGroup)
	if quotaBeforeGroup == 0 {
		return 0
	}
	calibration := dailyCostEstimateCalibrator.factor(channelId, modelName)
	return validChannelDailyCostEstimate(quotaBeforeGroup * calibration * channelDailyCostEstimateSafetyMargin)
}

func observeChannelDailyCostEstimate(channelId int, modelName string, estimatedQuotaBeforeGroup float64, actualQuotaBeforeGroup float64) {
	estimatedQuotaBeforeGroup = validChannelDailyCostEstimate(estimatedQuotaBeforeGroup)
	actualQuotaBeforeGroup = validChannelDailyCostEstimate(actualQuotaBeforeGroup)
	if channelId <= 0 || estimatedQuotaBeforeGroup == 0 || actualQuotaBeforeGroup == 0 {
		return
	}
	delta := actualQuotaBeforeGroup - estimatedQuotaBeforeGroup
	if math.IsNaN(delta) || math.IsInf(delta, 0) {
		return
	}
	dailyCostEstimateCalibrator.observe(channelDailyCostEstimateProfileKey{
		ChannelId: channelId,
		ModelName: strings.TrimSpace(modelName),
	}, estimatedQuotaBeforeGroup, delta)
}

func (c *channelDailyCostEstimateCalibrator) observe(key channelDailyCostEstimateProfileKey, estimatedQuota float64, delta float64) {
	if estimatedQuota <= 0 || math.IsNaN(estimatedQuota) || math.IsInf(estimatedQuota, 0) || delta <= 0 {
		return
	}
	ratio := 1 + delta/estimatedQuota
	if ratio <= 1 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return
	}
	value, exists := c.profiles.Load(key)
	if !exists {
		reserved := false
		for {
			count := c.count.Load()
			if count >= channelDailyCostEstimateMaxProfiles {
				value, exists = c.profiles.Load(key)
				if !exists {
					return
				}
				break
			}
			if c.count.CompareAndSwap(count, count+1) {
				reserved = true
				break
			}
		}
		if reserved {
			created := &channelDailyCostEstimateProfile{calibration: 1}
			value, exists = c.profiles.LoadOrStore(key, created)
			if !exists {
				value = created
			} else {
				c.count.Add(-1)
			}
		}
	}
	profile, ok := value.(*channelDailyCostEstimateProfile)
	if !ok || profile == nil {
		return
	}
	profile.mu.Lock()
	if profile.count < channelDailyCostEstimateCalibrationWindow {
		profile.ratios[profile.count] = ratio
		profile.count++
		if ratio > profile.calibration {
			profile.calibration = ratio
		}
	} else {
		wasMaximum := profile.ratios[profile.next] == profile.calibration
		profile.ratios[profile.next] = ratio
		profile.next = (profile.next + 1) % channelDailyCostEstimateCalibrationWindow
		if ratio > profile.calibration {
			profile.calibration = ratio
		} else if wasMaximum {
			profile.calibration = 1
			for _, observedRatio := range profile.ratios {
				if observedRatio > profile.calibration {
					profile.calibration = observedRatio
				}
			}
		}
	}
	profile.mu.Unlock()
}

func (c *channelDailyCostEstimateCalibrator) factor(channelId int, modelName string) float64 {
	key := channelDailyCostEstimateProfileKey{ChannelId: channelId, ModelName: strings.TrimSpace(modelName)}
	value, exists := c.profiles.Load(key)
	if !exists {
		return 1
	}
	profile, ok := value.(*channelDailyCostEstimateProfile)
	if !ok || profile == nil {
		return 1
	}
	profile.mu.RLock()
	if profile.count == 0 {
		profile.mu.RUnlock()
		return 1
	}
	factor := profile.calibration
	profile.mu.RUnlock()
	return max(1, factor)
}

func resetChannelDailyCostEstimateCalibratorForTest() {
	dailyCostEstimateCalibrator.profiles.Range(func(key any, _ any) bool {
		dailyCostEstimateCalibrator.profiles.Delete(key)
		return true
	})
	dailyCostEstimateCalibrator.count.Store(0)
}
