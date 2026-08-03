package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"

	"github.com/stretchr/testify/assert"
)

func TestBuildChannelTestUsageMetricsPreservesCompleteUsage(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     120,
		CompletionTokens: 36,
		TotalTokens:      156,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         80,
			CachedCreationTokens: 12,
			CacheWriteTokens:     9,
		},
		CompletionTokenDetails: dto.OutputTokenDetails{
			ReasoningTokens: 8,
		},
	}

	metrics := buildChannelTestUsageMetrics(usage, true)

	assert.True(t, metrics.available)
	assert.Equal(t, 120, metrics.inputTokens)
	assert.Equal(t, 36, metrics.outputTokens)
	assert.Equal(t, 156, metrics.totalTokens)
	assert.Equal(t, 80, metrics.cachedTokens)
	assert.Equal(t, 12, metrics.cacheWriteTokens)
	assert.Equal(t, 8, metrics.reasoningTokens)
}

func TestBuildChannelTestUsageMetricsUsesNativeCacheWriteAndKeepsRealZeroes(t *testing.T) {
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedCreationTokens: 3,
			CacheWriteTokens:     15,
		},
	}

	metrics := buildChannelTestUsageMetrics(usage, true)

	assert.True(t, metrics.available)
	assert.Zero(t, metrics.inputTokens)
	assert.Zero(t, metrics.outputTokens)
	assert.Zero(t, metrics.totalTokens)
	assert.Zero(t, metrics.cachedTokens)
	assert.Equal(t, 15, metrics.cacheWriteTokens)
	assert.Zero(t, metrics.reasoningTokens)
}

func TestBuildChannelTestUsageMetricsMarksEstimatedUsageUnavailable(t *testing.T) {
	metrics := buildChannelTestUsageMetrics(&dto.Usage{PromptTokens: 12}, false)

	assert.False(t, metrics.available)
	assert.Zero(t, metrics.inputTokens)
	assert.Zero(t, metrics.outputTokens)
	assert.Zero(t, metrics.totalTokens)
	assert.Zero(t, metrics.cachedTokens)
	assert.Zero(t, metrics.cacheWriteTokens)
	assert.Zero(t, metrics.reasoningTokens)
}
