package service

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

const (
	RetryExcludeKeywordsOptionKey = "ChannelMonitorRetryExcludeKeywords"
	MaxRetryExcludeKeywords       = 32
	MaxRetryExcludeKeywordLength  = 128
	RetryExcludeKeywordsReason    = "命中不重试关键字"
)

func GetConfiguredRetryExcludeKeywords() string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[RetryExcludeKeywordsOptionKey]
}

func ValidateRetryExcludeKeywords(raw string) error {
	_, err := parseRetryExcludeKeywords(raw)
	return err
}

type retryExcludeRule struct {
	keyword    string
	statusCode int
}

func parseRetryExcludeKeywords(raw string) ([]retryExcludeRule, error) {
	var rules []retryExcludeRule
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(rules) >= MaxRetryExcludeKeywords {
			return nil, fmt.Errorf("不重试关键字最多支持 %d 个", MaxRetryExcludeKeywords)
		}
		keyword, statusText, hasStatus := strings.Cut(line, "=>")
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			return nil, fmt.Errorf("不重试关键字不能为空")
		}
		if utf8.RuneCountInString(keyword) > MaxRetryExcludeKeywordLength {
			return nil, fmt.Errorf("不重试关键字长度不能超过 %d 个字符", MaxRetryExcludeKeywordLength)
		}
		rule := retryExcludeRule{keyword: strings.ToLower(keyword)}
		if hasStatus {
			statusText = strings.TrimSpace(statusText)
			statusCode, err := strconv.Atoi(statusText)
			if len(statusText) != 3 || err != nil || statusCode < 400 || statusCode > 599 {
				return nil, fmt.Errorf("不重试关键字的返回状态码必须是 400 到 599")
			}
			rule.statusCode = statusCode
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// ShouldExcludeErrorFromRetry matches the original error before user-visible
// mappings or masking. Invalid stored settings leave the retry rules in effect.
func ShouldExcludeErrorFromRetry(raw string) bool {
	matched, _ := matchRetryExcludeError(raw)
	return matched
}

// RetryExcludedErrorStatusCode returns a matched rule's optional client status.
// A zero value keeps the original status, including for legacy keyword rules.
func RetryExcludedErrorStatusCode(raw string) int {
	_, statusCode := matchRetryExcludeError(raw)
	return statusCode
}

func matchRetryExcludeError(raw string) (bool, int) {
	rules, err := parseRetryExcludeKeywords(GetConfiguredRetryExcludeKeywords())
	if err != nil || raw == "" {
		return false, 0
	}
	message := strings.ToLower(raw)
	for _, rule := range rules {
		if strings.Contains(message, rule.keyword) {
			return true, rule.statusCode
		}
	}
	return false, 0
}
