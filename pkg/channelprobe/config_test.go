package channelprobe

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetResponseConfigKeepsLegacyDefaultsWhenOptionsAreMissing(t *testing.T) {
	useProbeResponseOptionMap(t, map[string]string{})

	assert.Equal(t, DefaultResponseConfig(), GetResponseConfig())
}

func TestGetResponseConfigReadsConfiguredResponseContract(t *testing.T) {
	useProbeResponseOptionMap(t, map[string]string{
		OptionKey:                 "true",
		MatchInputOptionKey:       " health check ",
		ResponseTextOptionKey:     " healthy ",
		MinDelayMsOptionKey:       "125",
		MaxDelayMsOptionKey:       "875",
		InputTokensOptionKey:      "7",
		CacheWriteTokensOptionKey: "1",
		CachedTokensOptionKey:     "2",
		OutputTokensOptionKey:     "11",
	})

	assert.Equal(t, ResponseConfig{
		Enabled:          true,
		MatchInput:       "health check",
		ResponseText:     "healthy",
		MinDelayMs:       125,
		MaxDelayMs:       875,
		InputTokens:      7,
		CacheWriteTokens: 1,
		CachedTokens:     2,
		OutputTokens:     11,
	}, GetResponseConfig())
}

func TestGetResponseConfigFallsBackFromInvalidStoredOptions(t *testing.T) {
	useProbeResponseOptionMap(t, map[string]string{
		OptionKey:                 "invalid",
		MatchInputOptionKey:       "   ",
		ResponseTextOptionKey:     strings.Repeat("x", MaxResponseTextLength+1),
		MinDelayMsOptionKey:       "2000",
		MaxDelayMsOptionKey:       "1000",
		InputTokensOptionKey:      "-1",
		CacheWriteTokensOptionKey: "invalid",
		CachedTokensOptionKey:     "1000001",
		OutputTokensOptionKey:     "-1",
	})

	assert.Equal(t, DefaultResponseConfig(), GetResponseConfig())
}

func TestNormalizeResponseConfigValidatesAndTrimsSettings(t *testing.T) {
	valid := DefaultResponseConfig()
	valid.MatchInput = " health check "
	valid.ResponseText = " healthy "
	normalized, err := NormalizeResponseConfig(valid)
	require.NoError(t, err)
	assert.Equal(t, "health check", normalized.MatchInput)
	assert.Equal(t, "healthy", normalized.ResponseText)

	tests := []struct {
		name   string
		change func(*ResponseConfig)
	}{
		{name: "empty input", change: func(config *ResponseConfig) { config.MatchInput = " " }},
		{name: "empty response", change: func(config *ResponseConfig) { config.ResponseText = " " }},
		{name: "negative delay", change: func(config *ResponseConfig) { config.MinDelayMs = -1 }},
		{name: "reversed delay range", change: func(config *ResponseConfig) { config.MinDelayMs = config.MaxDelayMs + 1 }},
		{name: "oversized usage", change: func(config *ResponseConfig) { config.InputTokens = MaxTokenCount + 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := DefaultResponseConfig()
			test.change(&config)
			_, err := NormalizeResponseConfig(config)
			require.Error(t, err)
		})
	}
}
