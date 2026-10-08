package channelprobe

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

const (
	OptionKey                 = "ChannelMonitorProbeResponseEnabled"
	AllowedIPsOptionKey       = "ChannelMonitorProbeResponseAllowedIPs"
	MatchInputOptionKey       = "ChannelMonitorProbeResponseMatchInput"
	ResponseTextOptionKey     = "ChannelMonitorProbeResponseText"
	RulesOptionKey            = "ChannelMonitorProbeResponseRules"
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
	MaxResponseTextLength = constant.ChannelProbeMaxResponseTextLength
	MaxAllowedIPsLength   = 4_096
	MaxAllowedIPCount     = 64
	MaxDelayMs            = 600_000
	MaxTokenCount         = constant.ChannelProbeMaxInputTokens
	MaxResponseRules      = 32
)

type ResponseRule struct {
	MatchInput   string `json:"match_input"`
	ResponseText string `json:"response_text"`
}

type ResponseConfig struct {
	Enabled          bool
	AllowedIPs       string
	MatchInput       string
	ResponseText     string
	Rules            []ResponseRule
	MinDelayMs       int
	MaxDelayMs       int
	InputTokens      int
	CacheWriteTokens int
	CachedTokens     int
	OutputTokens     int
	Truncated        bool
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
		AllowedIPsOptionKey:       common.OptionMap[AllowedIPsOptionKey],
		MatchInputOptionKey:       common.OptionMap[MatchInputOptionKey],
		ResponseTextOptionKey:     common.OptionMap[ResponseTextOptionKey],
		RulesOptionKey:            common.OptionMap[RulesOptionKey],
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
	config.AllowedIPs = strings.TrimSpace(options[AllowedIPsOptionKey])
	normalizedAllowedIPs, err := normalizeResponseAllowedIPs(config.AllowedIPs)
	if err != nil {
		config.Enabled = false
	} else {
		config.AllowedIPs = normalizedAllowedIPs
	}
	config.MatchInput = parseResponseTextOption(options[MatchInputOptionKey], config.MatchInput, MaxMatchInputLength)
	config.ResponseText = parseResponseTextOption(options[ResponseTextOptionKey], config.ResponseText, MaxResponseTextLength)
	if raw := strings.TrimSpace(options[RulesOptionKey]); raw != "" {
		var rules []ResponseRule
		if common.UnmarshalJsonStr(raw, &rules) != nil {
			config.Enabled = false
		} else if normalized, err := NormalizeResponseRules(rules); err != nil {
			config.Enabled = false
		} else {
			config.Rules = normalized
			config.MatchInput = normalized[0].MatchInput
			config.ResponseText = normalized[0].ResponseText
		}
	}
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
	normalizedAllowedIPs, err := normalizeResponseAllowedIPs(config.AllowedIPs)
	if err != nil {
		return ResponseConfig{}, err
	}
	config.AllowedIPs = normalizedAllowedIPs

	rules, err := NormalizeResponseRules(config.ResponseRules())
	if err != nil {
		return ResponseConfig{}, err
	}
	config.MatchInput = rules[0].MatchInput
	config.ResponseText = rules[0].ResponseText
	if config.Rules != nil {
		config.Rules = rules
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

// ResponseRules preserves the single-pair contract for existing deployments.
func (config ResponseConfig) ResponseRules() []ResponseRule {
	if config.Rules != nil {
		return config.Rules
	}
	return []ResponseRule{{MatchInput: config.MatchInput, ResponseText: config.ResponseText}}
}

func NormalizeResponseRules(rules []ResponseRule) ([]ResponseRule, error) {
	if len(rules) == 0 || len(rules) > MaxResponseRules {
		return nil, fmt.Errorf("探针输入输出必须配置 1 到 %d 组", MaxResponseRules)
	}
	normalized := make([]ResponseRule, len(rules))
	for i, rule := range rules {
		rule.MatchInput = strings.TrimSpace(rule.MatchInput)
		rule.ResponseText = strings.TrimSpace(rule.ResponseText)
		if rule.MatchInput == "" {
			return nil, fmt.Errorf("第 %d 组探针匹配输入不能为空", i+1)
		}
		if utf8.RuneCountInString(rule.MatchInput) > MaxMatchInputLength {
			return nil, fmt.Errorf("第 %d 组探针匹配输入不能超过 %d 个字符", i+1, MaxMatchInputLength)
		}
		if rule.ResponseText == "" {
			return nil, fmt.Errorf("第 %d 组探针响应文本不能为空", i+1)
		}
		if utf8.RuneCountInString(rule.ResponseText) > MaxResponseTextLength {
			return nil, fmt.Errorf("第 %d 组探针响应文本不能超过 %d 个字符", i+1, MaxResponseTextLength)
		}
		for _, previous := range normalized[:i] {
			if strings.EqualFold(previous.MatchInput, rule.MatchInput) {
				return nil, fmt.Errorf("第 %d 组探针匹配输入与已有配置重复", i+1)
			}
		}
		normalized[i] = rule
	}
	return normalized, nil
}

func (config ResponseConfig) AllowsClientIP(rawClientIP string) bool {
	if config.AllowedIPs == "" {
		return true
	}
	clientIP, err := netip.ParseAddr(strings.TrimSpace(rawClientIP))
	if err != nil || clientIP.Zone() != "" {
		return false
	}
	clientIP = clientIP.Unmap()
	for _, rawAllowedIP := range splitResponseAllowedIPs(config.AllowedIPs) {
		allowedIP, err := netip.ParseAddr(rawAllowedIP)
		if err != nil || allowedIP.Zone() != "" {
			return false
		}
		if allowedIP.Unmap() == clientIP {
			return true
		}
	}
	return false
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

func normalizeResponseAllowedIPs(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if utf8.RuneCountInString(value) > MaxAllowedIPsLength {
		return "", fmt.Errorf("探针生效 IP 配置不能超过 %d 个字符", MaxAllowedIPsLength)
	}
	parts := splitResponseAllowedIPs(value)
	if len(parts) > MaxAllowedIPCount {
		return "", fmt.Errorf("探针生效 IP 不能超过 %d 个", MaxAllowedIPCount)
	}

	seen := make(map[netip.Addr]struct{}, len(parts))
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		address, err := netip.ParseAddr(part)
		if err != nil || address.Zone() != "" {
			return "", fmt.Errorf("探针生效 IP %q 无效", part)
		}
		address = address.Unmap()
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		normalized = append(normalized, address.String())
	}
	return strings.Join(normalized, "\n"), nil
}

func splitResponseAllowedIPs(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
}
