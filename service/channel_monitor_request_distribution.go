package service

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

const requestStatsMetricPrefix = "request_stats_"

// The capped 1K histogram supports every configurable strict integer-K
// threshold, including zero input, without persisting request payloads.
type channelMonitorRequestDistribution map[string]int64

func requestStatsEventMetrics(event model.ChannelMonitorEvent) channelMonitorRequestDistribution {
	if event.Source != model.ChannelMonitorEventSourceBusiness || !event.RequestDispatched || event.FinalRetrySummary {
		return nil
	}
	values := channelMonitorRequestDistribution{"total": 1}
	if event.IsStream {
		values["stream"] = 1
	}
	if event.ReliableInputTokens != nil {
		values["known"] = 1
		values["bucket_"+strconv.FormatInt(min(*event.ReliableInputTokens/1000, 1000), 10)] = 1
	}
	return values
}

func cloneRequestStatsDistribution(source channelMonitorRequestDistribution) channelMonitorRequestDistribution {
	if source == nil {
		return nil
	}
	result := make(channelMonitorRequestDistribution, len(source))
	for metric, count := range source {
		result[metric] = count
	}
	return result
}

func mergeRequestStatsDistribution(target, source channelMonitorRequestDistribution) (channelMonitorRequestDistribution, error) {
	result := cloneRequestStatsDistribution(target)
	if len(source) > 0 && result == nil {
		result = make(channelMonitorRequestDistribution, len(source))
	}
	for metric, count := range source {
		value, err := channelMonitorRedisSharedCheckedAddInt64(result[metric], count)
		if err != nil {
			return nil, err
		}
		result[metric] = value
	}
	return result, nil
}

// Only upstream measurements enter the histogram. Billing estimates and
// protocol fallback counts remain available to billing but are unknown here.
func channelMonitorReliableInputTokens(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) *int64 {
	if ctx == nil || !channelDailyCostUsageIsAuthoritative(ctx, info, usage) ||
		common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens) || ctx.GetBool("channel_monitor_input_estimated") {
		return nil
	}
	actual := effectiveBillingUsage(usage)
	if actual == nil {
		return nil
	}
	input := actual.PromptTokens
	if actual.InputTokens > 0 {
		input = actual.InputTokens
	} else if actual.UsageSemantic == dto.BillingUsageSemanticAnthropic {
		input += actual.PromptTokensDetails.CachedTokens + actual.PromptTokensDetails.CacheCreationTokensTotal()
	}
	if input < 0 || input == 0 && usage.BillingUsage == nil {
		return nil
	}
	value := int64(input)
	return &value
}

func requestStatsLowCount(distribution channelMonitorRequestDistribution, thresholdK int) int64 {
	var total int64
	for metric, count := range distribution {
		if bucket, ok := strings.CutPrefix(metric, "bucket_"); ok {
			index, err := strconv.Atoi(bucket)
			if err == nil && index < thresholdK {
				total += count
			}
		}
	}
	return total
}

// Validate the full histogram before using it for a destructive decision or
// displaying a rate. Unknown requests are the difference from known samples.
func validateRequestStatsDistribution(distribution channelMonitorRequestDistribution) error {
	var samples int64
	for metric, count := range distribution {
		if count < 0 {
			return errors.New("请求统计计数不能为负数")
		}
		if suffix, ok := strings.CutPrefix(metric, "bucket_"); ok {
			bucket, err := strconv.Atoi(suffix)
			if err != nil || bucket < 0 || bucket > 1000 {
				return errors.New("请求统计 Token 分桶无效")
			}
			samples, err = channelMonitorRedisSharedCheckedAddInt64(samples, count)
			if err != nil {
				return err
			}
		} else if metric != "total" && metric != "known" && metric != "stream" {
			return errors.New("请求统计指标无效")
		}
	}
	if samples != distribution["known"] || samples > distribution["total"] || distribution["stream"] > distribution["total"] {
		return errors.New("请求统计样本与总量不一致")
	}
	return nil
}
