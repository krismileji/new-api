package service

import (
	"github.com/QuantumNous/new-api/model"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
)

// MergeChannelModelDetectorAuthoritativeUsage replaces any adaptor estimate
// with the Usage parsed from the detector response while retaining provider
// billing details that the adaptor already extracted.
func MergeChannelModelDetectorAuthoritativeUsage(usage *relaydto.Usage, authoritative ChannelModelDetectorUsage) (*relaydto.Usage, error) {
	if !authoritative.Available || authoritative.Source != model.ChannelModelDetectionUsageUpstreamAuthoritative {
		return nil, ErrChannelModelDetectorUsageUnavailable
	}
	if _, err := validateChannelModelDetectorUsage(authoritative); err != nil {
		return nil, err
	}

	if usage == nil {
		usage = &relaydto.Usage{}
	}
	inputTokens, err := channelModelDetectorUsageInt(authoritative.InputTokens)
	if err != nil {
		return nil, err
	}
	outputTokens, err := channelModelDetectorUsageInt(authoritative.OutputTokens)
	if err != nil {
		return nil, err
	}
	totalTokens, err := channelModelDetectorUsageInt(authoritative.TotalTokens)
	if err != nil {
		return nil, err
	}

	usage.PromptTokens = inputTokens
	usage.CompletionTokens = outputTokens
	usage.TotalTokens = totalTokens
	usage.InputTokens = inputTokens
	usage.OutputTokens = outputTokens
	usage.UsageSource = authoritative.Source
	if authoritative.InputTokenDetailsAvailable {
		cachedTokens, err := channelModelDetectorUsageInt(authoritative.CachedTokens)
		if err != nil {
			return nil, err
		}
		cachedCreationTokens, err := channelModelDetectorUsageInt(authoritative.CachedCreationTokens)
		if err != nil {
			return nil, err
		}
		cacheWriteTokens, err := channelModelDetectorUsageInt(authoritative.CacheWriteTokens)
		if err != nil {
			return nil, err
		}
		usage.PromptTokensDetails.CachedTokens = cachedTokens
		usage.PromptTokensDetails.CachedCreationTokens = cachedCreationTokens
		usage.PromptTokensDetails.CacheWriteTokens = cacheWriteTokens
		if usage.InputTokensDetails != nil {
			usage.InputTokensDetails.CachedTokens = cachedTokens
			usage.InputTokensDetails.CachedCreationTokens = cachedCreationTokens
			usage.InputTokensDetails.CacheWriteTokens = cacheWriteTokens
		}
	}
	mergeChannelModelDetectorNestedBillingUsage(usage, authoritative, inputTokens, outputTokens, totalTokens)
	return usage, nil
}

func channelModelDetectorUsageInt(value int64) (int, error) {
	converted := int(value)
	if int64(converted) != value {
		return 0, ErrChannelModelDetectorUsageInvalid
	}
	return converted, nil
}

func mergeChannelModelDetectorNestedBillingUsage(usage *relaydto.Usage, authoritative ChannelModelDetectorUsage, inputTokens, outputTokens, totalTokens int) {
	if usage == nil || usage.BillingUsage == nil {
		return
	}
	usage.BillingUsage.Estimated = false
	switch {
	case usage.BillingUsage.OpenAIUsage != nil:
		mergeChannelModelDetectorUsageFields(usage.BillingUsage.OpenAIUsage, authoritative, inputTokens, outputTokens, totalTokens)
	case usage.BillingUsage.ClaudeUsage != nil:
		usage.BillingUsage.ClaudeUsage.InputTokens = inputTokens
		usage.BillingUsage.ClaudeUsage.OutputTokens = outputTokens
	case usage.BillingUsage.GeminiUsageMetadata != nil:
		usage.BillingUsage.GeminiUsageMetadata.PromptTokenCount = inputTokens
		usage.BillingUsage.GeminiUsageMetadata.CandidatesTokenCount = outputTokens
		usage.BillingUsage.GeminiUsageMetadata.TotalTokenCount = totalTokens
	}
}

func mergeChannelModelDetectorUsageFields(usage *relaydto.Usage, authoritative ChannelModelDetectorUsage, inputTokens, outputTokens, totalTokens int) {
	if usage == nil {
		return
	}
	usage.PromptTokens = inputTokens
	usage.CompletionTokens = outputTokens
	usage.TotalTokens = totalTokens
	usage.InputTokens = inputTokens
	usage.OutputTokens = outputTokens
	if authoritative.InputTokenDetailsAvailable {
		usage.PromptTokensDetails.CachedTokens = int(authoritative.CachedTokens)
		usage.PromptTokensDetails.CachedCreationTokens = int(authoritative.CachedCreationTokens)
		usage.PromptTokensDetails.CacheWriteTokens = int(authoritative.CacheWriteTokens)
	}
}
