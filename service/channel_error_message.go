package service

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	ErrorMessageKeywordsOptionKey = "ChannelMonitorErrorMessageKeywords"
	MaxErrorMessageKeywords      = 32
	MaxErrorMessageKeywordLength = 128
)

// GetConfiguredErrorMessageKeywords returns the global channel-monitor keyword
// list. Each non-empty line is treated as one case-insensitive substring.
func GetConfiguredErrorMessageKeywords() string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[ErrorMessageKeywordsOptionKey]
}

// ValidateErrorMessageKeywords validates the global keyword list.
func ValidateErrorMessageKeywords(raw string) error {
	_, err := parseErrorMessageKeywords(raw)
	return err
}

func parseErrorMessageKeywords(raw string) ([]string, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if raw == "" {
		return nil, nil
	}
	lines := strings.Split(raw, "\n")
	keywords := make([]string, 0, len(lines))
	for _, line := range lines {
		keyword := strings.TrimSpace(line)
		if keyword == "" {
			continue
		}
		if len(keywords) >= MaxErrorMessageKeywords {
			return nil, fmt.Errorf("错误屏蔽关键字最多支持 %d 个", MaxErrorMessageKeywords)
		}
		if len([]rune(keyword)) > MaxErrorMessageKeywordLength {
			return nil, fmt.Errorf("错误屏蔽关键字长度不能超过 %d 个字符", MaxErrorMessageKeywordLength)
		}
		keywords = append(keywords, keyword)
	}
	return keywords, nil
}

// ResolveConfiguredErrorMessage applies the global keyword list to raw.
// Invalid stored configuration is ignored so an admin typo cannot break a
// relay response.
func ResolveConfiguredErrorMessage(raw string) (string, bool) {
	keywords, err := parseErrorMessageKeywords(GetConfiguredErrorMessageKeywords())
	if err != nil {
		return "", false
	}
	return ResolveMaskedErrorMessage(keywords, raw)
}

// ResolveMaskedErrorMessage removes configured keywords from raw,
// using case-insensitive substring matching. The original error is retained
// by callers for administrator logs.
func ResolveMaskedErrorMessage(keywords []string, raw string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", false
	}
	masked := raw
	matched := false
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		pattern := regexp.MustCompile("(?i)" + regexp.QuoteMeta(keyword))
		if pattern.MatchString(masked) {
			masked = pattern.ReplaceAllString(masked, "")
			matched = true
		}
	}
	return masked, matched
}
