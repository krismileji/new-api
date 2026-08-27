package service

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
)

const (
	RetrySkipErrorCodesOptionKey    = "ChannelMonitorRetrySkipErrorCodes"
	RetrySkipErrorMessagesOptionKey = "ChannelMonitorRetrySkipErrorMessages"
	MaxRetrySkipErrorCodes          = 32
	MaxRetrySkipErrorCodeLength     = 128
	MaxRetrySkipErrorMessages       = 32
	MaxRetrySkipErrorMessageLength  = 256
)

var retrySkipStatusCodePattern = regexp.MustCompile(`(?i)(?:\b(?:status(?:_code|\s+code)?|http)\s*[:=]?\s*|(?:接口|上游)?返回\s+)([1-5][0-9]{2})\b`)

func GetConfiguredRetrySkipErrorCodes() string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[RetrySkipErrorCodesOptionKey]
}

func GetConfiguredRetrySkipErrorMessages() string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[RetrySkipErrorMessagesOptionKey]
}

func ValidateRetrySkipErrorCodes(raw string) error {
	_, err := parseRetrySkipLines(raw, "不重试错误码", MaxRetrySkipErrorCodes, MaxRetrySkipErrorCodeLength)
	return err
}

func ValidateRetrySkipErrorMessages(raw string) error {
	_, err := parseRetrySkipLines(raw, "不重试错误信息", MaxRetrySkipErrorMessages, MaxRetrySkipErrorMessageLength)
	return err
}

func parseRetrySkipLines(raw, label string, maxCount, maxLength int) ([]string, error) {
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
		if len(values) >= maxCount {
			return nil, fmt.Errorf("%s最多支持 %d 个", label, maxCount)
		}
		if len([]rune(value)) > maxLength {
			return nil, fmt.Errorf("%s长度不能超过 %d 个字符", label, maxLength)
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

// ShouldSkipRetryForConfiguredError reports whether a configured channel-monitor
// rule matches. Error codes are exact, case-insensitive matches. Status codes
// are accepted as error codes too, and error messages use substring matching.
func ShouldSkipRetryForConfiguredError(errorCode string, statusCode int, errorMessage string) bool {
	codes, codeErr := parseRetrySkipLines(
		GetConfiguredRetrySkipErrorCodes(),
		"不重试错误码",
		MaxRetrySkipErrorCodes,
		MaxRetrySkipErrorCodeLength,
	)
	messages, messageErr := parseRetrySkipLines(
		GetConfiguredRetrySkipErrorMessages(),
		"不重试错误信息",
		MaxRetrySkipErrorMessages,
		MaxRetrySkipErrorMessageLength,
	)
	if codeErr != nil {
		codes = nil
	}
	if messageErr != nil {
		messages = nil
	}

	normalizedCode := strings.ToLower(strings.TrimSpace(errorCode))
	codeCandidates := []string{normalizedCode}
	if statusCode >= 100 && statusCode <= 599 {
		codeCandidates = append(codeCandidates, strconv.Itoa(statusCode))
	}
	for _, candidate := range codeCandidates {
		if candidate == "" {
			continue
		}
		for _, configured := range codes {
			if strings.EqualFold(candidate, configured) {
				return true
			}
		}
	}

	message := strings.ToLower(strings.TrimSpace(errorMessage))
	if message == "" {
		return false
	}
	for _, configured := range messages {
		if strings.Contains(message, strings.ToLower(configured)) {
			return true
		}
	}
	return false
}

// ShouldSkipRetryForError is the error-shaped convenience wrapper used by
// channel-monitor retry loops. It also extracts an HTTP status from plain
// upstream errors, which are common in ratio and balance synchronization.
func ShouldSkipRetryForError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *types.NewAPIError
	if errors.As(err, &apiErr) && apiErr != nil {
		return ShouldSkipRetryForConfiguredError(
			string(apiErr.GetErrorCode()),
			apiErr.StatusCode,
			apiErr.Error(),
		)
	}
	message := err.Error()
	statusCode := 0
	if match := retrySkipStatusCodePattern.FindStringSubmatch(message); len(match) == 2 {
		statusCode, _ = strconv.Atoi(match[1])
	}
	return ShouldSkipRetryForConfiguredError("", statusCode, message)
}
