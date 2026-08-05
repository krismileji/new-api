package controller

import (
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

type channelTestUsageMetrics struct {
	available        bool
	inputTokens      int
	outputTokens     int
	totalTokens      int
	cachedTokens     int
	cacheWriteTokens int
	reasoningTokens  int
}

func buildChannelTestUsageMetrics(usage *dto.Usage, authoritative bool) channelTestUsageMetrics {
	if usage == nil || !authoritative {
		return channelTestUsageMetrics{}
	}

	return channelTestUsageMetrics{
		available:        true,
		inputTokens:      max(usage.PromptTokens, 0),
		outputTokens:     max(usage.CompletionTokens, 0),
		totalTokens:      max(usage.TotalTokens, 0),
		cachedTokens:     max(usage.PromptTokensDetails.CachedTokens, 0),
		cacheWriteTokens: usage.PromptTokensDetails.CacheCreationTokensTotal(),
		reasoningTokens:  max(usage.CompletionTokenDetails.ReasoningTokens, 0),
	}
}

func addChannelTestUsageMetrics(data gin.H, metrics channelTestUsageMetrics) {
	data["usage_available"] = metrics.available
	data["input_tokens"] = metrics.inputTokens
	data["output_tokens"] = metrics.outputTokens
	data["total_tokens"] = metrics.totalTokens
	data["cached_tokens"] = metrics.cachedTokens
	data["cache_write_tokens"] = metrics.cacheWriteTokens
	data["reasoning_tokens"] = metrics.reasoningTokens
}
