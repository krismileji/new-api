package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	ErrorMessageMappingOptionKey        = "ChannelMonitorErrorMessageMapping"
	maxErrorMessageMappingEntries       = 100
	maxErrorMessageMappingKeyLength     = 128
	maxErrorMessageMappingMessageLength = 4096
)

// GetConfiguredErrorMessageMapping returns the global channel-monitor mapping.
func GetConfiguredErrorMessageMapping() string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[ErrorMessageMappingOptionKey]
}

// ValidateErrorMessageMapping validates the optional error message map.
// Keys may be upstream error codes or HTTP status codes; values are the
// messages exposed to the requesting user.
func ValidateErrorMessageMapping(raw string) error {
	_, err := parseErrorMessageMapping(raw)
	return err
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
