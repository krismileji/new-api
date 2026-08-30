package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	ErrorMessageMappingOptionKey        = "ChannelMonitorErrorMessageMapping"
	ErrorMessageWhitelistOptionKey      = "ChannelMonitorErrorMessageWhitelist"
	maxErrorMessageMappingEntries       = 100
	maxErrorMessageMappingKeyLength     = 128
	maxErrorMessageMappingMessageLength = 4096
	MaxErrorMessageWhitelistCodes       = 32
	MaxErrorMessageWhitelistCodeLength  = 128
)

// GetConfiguredErrorMessageMapping returns the global channel-monitor mapping.
func GetConfiguredErrorMessageMapping() string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[ErrorMessageMappingOptionKey]
}

// GetConfiguredErrorMessageWhitelist returns the error codes that bypass all
// user-visible error-message processing.
func GetConfiguredErrorMessageWhitelist() string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[ErrorMessageWhitelistOptionKey]
}

// ValidateErrorMessageMapping validates the optional error message map.
// Keys may be upstream error codes or HTTP status codes; values are the
// messages exposed to the requesting user.
func ValidateErrorMessageMapping(raw string) error {
	_, err := parseErrorMessageMapping(raw)
	return err
}

// ValidateErrorMessageWhitelist validates the optional error-code whitelist.
func ValidateErrorMessageWhitelist(raw string) error {
	_, err := parseErrorMessageWhitelist(raw)
	return err
}

// ShouldBypassErrorMessageHandling reports whether an error code or final HTTP
// status code is configured to bypass user-visible error processing.
func ShouldBypassErrorMessageHandling(errorCode string, statusCode int) bool {
	codes, err := parseErrorMessageWhitelist(GetConfiguredErrorMessageWhitelist())
	if err != nil {
		return false
	}

	candidates := []string{strings.ToLower(strings.TrimSpace(errorCode))}
	if statusCode >= 100 && statusCode <= 599 {
		candidates = append(candidates, strconv.Itoa(statusCode))
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		for _, configured := range codes {
			if strings.EqualFold(candidate, configured) {
				return true
			}
		}
	}
	return false
}

// ResolveUserErrorMessage returns the configured message for an upstream error
// code, falling back to the final HTTP status code. Invalid stored configuration
// is ignored at request time so it cannot turn an upstream failure into a new
// gateway failure.
func ResolveUserErrorMessage(raw string, errorCode string, statusCode int) (string, bool) {
	mapping, err := parseErrorMessageMapping(raw)
	if err != nil {
		return "", false
	}

	errorCode = strings.TrimSpace(errorCode)
	if errorCode != "" {
		if message, ok := mapping[errorCode]; ok {
			return message, true
		}
	}
	if statusCode != 0 {
		if message, ok := mapping[strconv.Itoa(statusCode)]; ok {
			return message, true
		}
	}
	return "", false
}

func parseErrorMessageMapping(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, nil
	}

	mapping := make(map[string]string)
	if err := common.Unmarshal([]byte(raw), &mapping); err != nil {
		return nil, fmt.Errorf("错误信息映射必须是 JSON 对象，且值必须是字符串: %w", err)
	}
	if len(mapping) > maxErrorMessageMappingEntries {
		return nil, fmt.Errorf("错误信息映射最多支持 %d 条规则", maxErrorMessageMappingEntries)
	}

	normalized := make(map[string]string, len(mapping))
	for key, message := range mapping {
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey == "" {
			return nil, fmt.Errorf("错误信息映射的错误码不能为空")
		}
		if len(normalizedKey) > maxErrorMessageMappingKeyLength {
			return nil, fmt.Errorf("错误信息映射的错误码不能超过 %d 个字符", maxErrorMessageMappingKeyLength)
		}
		if _, exists := normalized[normalizedKey]; exists {
			return nil, fmt.Errorf("错误信息映射包含重复错误码: %s", normalizedKey)
		}
		if strings.TrimSpace(message) == "" {
			return nil, fmt.Errorf("错误信息映射的返回信息不能为空")
		}
		if len(message) > maxErrorMessageMappingMessageLength {
			return nil, fmt.Errorf("错误信息映射的返回信息不能超过 %d 个字符", maxErrorMessageMappingMessageLength)
		}
		normalized[normalizedKey] = message
	}
	return normalized, nil
}

func parseErrorMessageWhitelist(raw string) ([]string, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if raw == "" {
		return nil, nil
	}
	lines := strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == ',' })
	values := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		value := strings.TrimSpace(line)
		if value == "" {
			continue
		}
		if len(values) >= MaxErrorMessageWhitelistCodes {
			return nil, fmt.Errorf("错误码白名单最多支持 %d 个", MaxErrorMessageWhitelistCodes)
		}
		if len([]rune(value)) > MaxErrorMessageWhitelistCodeLength {
			return nil, fmt.Errorf("错误码白名单长度不能超过 %d 个字符", MaxErrorMessageWhitelistCodeLength)
		}
		normalized := strings.ToLower(value)
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		values = append(values, value)
	}
	return values, nil
}
