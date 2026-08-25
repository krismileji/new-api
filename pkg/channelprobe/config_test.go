package channelprobe

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetResponseConfigUsesProbeDefaultsWhenOptionsAreMissing(t *testing.T) {
	useProbeResponseOptionMap(t, map[string]string{})

	config := GetResponseConfig()
	assert.Equal(t, DefaultMatchInput, config.MatchInput)
	assert.Empty(t, config.AllowedIPs)
	assert.Equal(t, DefaultResponseText, config.ResponseText)
	assert.Equal(t, DefaultMinDelayMs, config.MinDelayMs)
	assert.Equal(t, DefaultMaxDelayMs, config.MaxDelayMs)
	assert.Equal(t, 4_387, config.InputTokens)
	assert.Equal(t, 0, config.CacheWriteTokens)
	assert.Equal(t, 3_840, config.CachedTokens)
	assert.Equal(t, 14, config.OutputTokens)
}

func TestGetResponseConfigReadsConfiguredResponseContract(t *testing.T) {
	useProbeResponseOptionMap(t, map[string]string{
		OptionKey:                 "true",
		AllowedIPsOptionKey:       " 203.0.113.10, ::ffff:192.0.2.20 ",
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
		AllowedIPs:       "203.0.113.10\n192.0.2.20",
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
	valid.AllowedIPs = " 203.0.113.10, 2001:db8::10, 203.0.113.10 "
	valid.MatchInput = " health check "
	valid.ResponseText = " healthy "
	normalized, err := NormalizeResponseConfig(valid)
	require.NoError(t, err)
	assert.Equal(t, "health check", normalized.MatchInput)
	assert.Equal(t, "healthy", normalized.ResponseText)
	assert.Equal(t, "203.0.113.10\n2001:db8::10", normalized.AllowedIPs)

	tests := []struct {
		name   string
		change func(*ResponseConfig)
	}{
		{name: "empty input", change: func(config *ResponseConfig) { config.MatchInput = " " }},
		{name: "invalid allowed IP", change: func(config *ResponseConfig) { config.AllowedIPs = "not-an-ip" }},
		{name: "too many allowed IPs", change: func(config *ResponseConfig) {
			config.AllowedIPs = strings.Repeat("192.0.2.1,", MaxAllowedIPCount) + "192.0.2.2"
		}},
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

func TestResponseConfigAllowsOnlyConfiguredClientIPs(t *testing.T) {
	config := DefaultResponseConfig()
	config.AllowedIPs = "203.0.113.10\n2001:db8::10"

	assert.True(t, config.AllowsClientIP("203.0.113.10"))
	assert.True(t, config.AllowsClientIP("::ffff:203.0.113.10"))
	assert.True(t, config.AllowsClientIP("2001:db8::10"))
	assert.False(t, config.AllowsClientIP("203.0.113.11"))
	assert.False(t, config.AllowsClientIP("invalid"))

	config.AllowedIPs = ""
	assert.True(t, config.AllowsClientIP("203.0.113.11"))
}

func TestGetResponseConfigDisablesProbeForInvalidStoredAllowedIPs(t *testing.T) {
	useProbeResponseOptionMap(t, map[string]string{
		OptionKey:           "true",
		AllowedIPsOptionKey: "not-an-ip",
	})

	config := GetResponseConfig()
	assert.False(t, config.Enabled)
	assert.Equal(t, "not-an-ip", config.AllowedIPs)
}
