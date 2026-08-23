package channelprobe

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

const (
	OptionKey                 = "ChannelMonitorProbeResponseEnabled"
	MatchInputOptionKey       = "ChannelMonitorProbeResponseMatchInput"
	ResponseTextOptionKey     = "ChannelMonitorProbeResponseText"
	MinDelayMsOptionKey       = "ChannelMonitorProbeResponseMinDelayMilliseconds"
	MaxDelayMsOptionKey       = "ChannelMonitorProbeResponseMaxDelayMilliseconds"
	InputTokensOptionKey      = "ChannelMonitorProbeResponseInputTokens"
	CacheWriteTokensOptionKey = "ChannelMonitorProbeResponseCacheWriteTokens"
	CachedTokensOptionKey     = "ChannelMonitorProbeResponseCachedTokens"
	OutputTokensOptionKey     = "ChannelMonitorProbeResponseOutputTokens"

	DefaultMatchInput       = "hi"
	DefaultResponseText     = "Hi. What are you working on?"
	DefaultMinDelayMs       = 500
	DefaultMaxDelayMs       = 2_000
	DefaultInputTokens      = 4_387
	DefaultCacheWriteTokens = 0
	DefaultCachedTokens     = 3_840
	DefaultOutputTokens     = 14

	MaxMatchInputLength   = 4_096
	MaxResponseTextLength = 16_384
	MaxDelayMs            = 600_000
	MaxTokenCount         = 1_000_000
)

type ResponseConfig struct {
	Enabled          bool
	MatchInput       string
	ResponseText     string
	MinDelayMs       int
	MaxDelayMs       int
	InputTokens      int
	CacheWriteTokens int
	CachedTokens     int
	OutputTokens     int
}

func DefaultResponseConfig() ResponseConfig {
	return ResponseConfig{
		MatchInput:       DefaultMatchInput,
		ResponseText:     DefaultResponseText,
		MinDelayMs:       DefaultMinDelayMs,
		MaxDelayMs:       DefaultMaxDelayMs,
		InputTokens:      DefaultInputTokens,
		CacheWriteTokens: DefaultCacheWriteTokens,
		CachedTokens:     DefaultCachedTokens,
		OutputTokens:     DefaultOutputTokens,
	}
}

func GetResponseConfig() ResponseConfig {
	common.OptionMapRWMutex.RLock()
	options := map[string]string{
		OptionKey:                 common.OptionMap[OptionKey],
		MatchInputOptionKey:       common.OptionMap[MatchInputOptionKey],
		ResponseTextOptionKey:     common.OptionMap[ResponseTextOptionKey],
		MinDelayMsOptionKey:       common.OptionMap[MinDelayMsOptionKey],
		MaxDelayMsOptionKey:       common.OptionMap[MaxDelayMsOptionKey],
		InputTokensOptionKey:      common.OptionMap[InputTokensOptionKey],
		CacheWriteTokensOptionKey: common.OptionMap[CacheWriteTokensOptionKey],
		CachedTokensOptionKey:     common.OptionMap[CachedTokensOptionKey],
		OutputTokensOptionKey:     common.OptionMap[OutputTokensOptionKey],
	}
	common.OptionMapRWMutex.RUnlock()
	return ResponseConfigFromOptions(options)
}

func ResponseConfigFromOptions(options map[string]string) ResponseConfig {
	config := DefaultResponseConfig()
	config.Enabled, _ = strconv.ParseBool(options[OptionKey])
	config.MatchInput = parseResponseTextOption(options[MatchInputOptionKey], config.MatchInput, MaxMatchInputLength)
	config.ResponseText = parseResponseTextOption(options[ResponseTextOptionKey], config.ResponseText, MaxResponseTextLength)
	config.MinDelayMs = parseResponseIntOption(options[MinDelayMsOptionKey], config.MinDelayMs, 0, MaxDelayMs)
	config.MaxDelayMs = parseResponseIntOption(options[MaxDelayMsOptionKey], config.MaxDelayMs, 0, MaxDelayMs)
	if config.MinDelayMs > config.MaxDelayMs {
		config.MinDelayMs = DefaultMinDelayMs
		config.MaxDelayMs = DefaultMaxDelayMs
	}
	config.InputTokens = parseResponseIntOption(options[InputTokensOptionKey], config.InputTokens, 0, MaxTokenCount)
	config.CacheWriteTokens = parseResponseIntOption(options[CacheWriteTokensOptionKey], config.CacheWriteTokens, 0, MaxTokenCount)
	config.CachedTokens = parseResponseIntOption(options[CachedTokensOptionKey], config.CachedTokens, 0, MaxTokenCount)
	config.OutputTokens = parseResponseIntOption(options[OutputTokensOptionKey], config.OutputTokens, 0, MaxTokenCount)
	return config
}

func NormalizeResponseConfig(config ResponseConfig) (ResponseConfig, error) {
	config.MatchInput = strings.TrimSpace(config.MatchInput)
	if config.MatchInput == "" {
		return ResponseConfig{}, fmt.Errorf("探针匹配输入不能为空")
	}
	if utf8.RuneCountInString(config.MatchInput) > MaxMatchInputLength {
		return ResponseConfig{}, fmt.Errorf("探针匹配输入不能超过 %d 个字符", MaxMatchInputLength)
	}

	config.ResponseText = strings.TrimSpace(config.ResponseText)
	if config.ResponseText == "" {
		return ResponseConfig{}, fmt.Errorf("探针响应文本不能为空")
	}
	if utf8.RuneCountInString(config.ResponseText) > MaxResponseTextLength {
		return ResponseConfig{}, fmt.Errorf("探针响应文本不能超过 %d 个字符", MaxResponseTextLength)
	}

	if config.MinDelayMs < 0 || config.MinDelayMs > MaxDelayMs {
		return ResponseConfig{}, fmt.Errorf("探针最小延迟必须在 0 到 %d 毫秒之间", MaxDelayMs)
	}
	if config.MaxDelayMs < 0 || config.MaxDelayMs > MaxDelayMs {
		return ResponseConfig{}, fmt.Errorf("探针最大延迟必须在 0 到 %d 毫秒之间", MaxDelayMs)
	}
	if config.MinDelayMs > config.MaxDelayMs {
		return ResponseConfig{}, fmt.Errorf("探针最小延迟不能大于最大延迟")
	}

	for _, tokenCount := range []struct {
		label string
		value int
	}{
		{label: "输入 Token", value: config.InputTokens},
		{label: "缓存写 Token", value: config.CacheWriteTokens},
		{label: "缓存命中 Token", value: config.CachedTokens},
		{label: "输出 Token", value: config.OutputTokens},
	} {
		if tokenCount.value < 0 || tokenCount.value > MaxTokenCount {
			return ResponseConfig{}, fmt.Errorf("探针%s必须在 0 到 %d 之间", tokenCount.label, MaxTokenCount)
		}
	}
	return config, nil
}

func (config ResponseConfig) TotalTokens() int {
	return config.InputTokens + config.OutputTokens
}

func parseResponseTextOption(raw string, fallback string, maxLength int) string {
	value := strings.TrimSpace(raw)
	if value == "" || utf8.RuneCountInString(value) > maxLength {
		return fallback
	}
	return value
}

func parseResponseIntOption(raw string, fallback int, minimum int, maximum int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return fallback
	}
	return value
}
