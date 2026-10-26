package service

import (
	"fmt"
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

func parseRetryExcludeKeywords(raw string) ([]string, error) {
	var keywords []string
	for line := range strings.SplitSeq(raw, "\n") {
		keyword := strings.TrimSpace(line)
		if keyword == "" {
			continue
		}
		if len(keywords) >= MaxRetryExcludeKeywords {
			return nil, fmt.Errorf("不重试关键字最多支持 %d 个", MaxRetryExcludeKeywords)
		}
		if utf8.RuneCountInString(keyword) > MaxRetryExcludeKeywordLength {
			return nil, fmt.Errorf("不重试关键字长度不能超过 %d 个字符", MaxRetryExcludeKeywordLength)
		}
		keywords = append(keywords, strings.ToLower(keyword))
	}
	return keywords, nil
}

// ShouldExcludeErrorFromRetry matches the original error before user-visible
// mappings or masking. Invalid stored settings leave the retry rules in effect.
func ShouldExcludeErrorFromRetry(raw string) bool {
	keywords, err := parseRetryExcludeKeywords(GetConfiguredRetryExcludeKeywords())
	if err != nil || len(keywords) == 0 || raw == "" {
		return false
	}
	message := strings.ToLower(raw)
	for _, keyword := range keywords {
		if strings.Contains(message, keyword) {
			return true
		}
	}
	return false
}
