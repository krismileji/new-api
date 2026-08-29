package service

import (
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveUserErrorMessagePrefersErrorCode(t *testing.T) {
	mapping := `{"insufficient_quota":"额度不足","429":"请求过于频繁"}`

	message, ok := ResolveUserErrorMessage(mapping, "insufficient_quota", 429)

	require.True(t, ok)
	assert.Equal(t, "额度不足", message)
}

func TestResolveUserErrorMessageFallsBackToStatusCode(t *testing.T) {
	mapping := `{"429":"请求过于频繁"}`

	message, ok := ResolveUserErrorMessage(mapping, "upstream_rate_limit", 429)

	require.True(t, ok)
	assert.Equal(t, "请求过于频繁", message)
}

func TestResolveUserErrorMessageIgnoresInvalidConfiguration(t *testing.T) {
	require.NoError(t, ValidateErrorMessageMapping(`{"429":"请求过于频繁"}`))

	message, ok := ResolveUserErrorMessage(`{"429":429}`, "upstream_rate_limit", 429)

	assert.False(t, ok)
	assert.Empty(t, message)
}

func TestValidateErrorMessageMappingRejectsEmptyMessages(t *testing.T) {
	err := ValidateErrorMessageMapping(`{"429":" "}`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "返回信息不能为空")
}

func TestShouldBypassErrorMessageHandlingMatchesCodeAndStatus(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	original, existed := common.OptionMap[ErrorMessageWhitelistOptionKey]
	common.OptionMap[ErrorMessageWhitelistOptionKey] = " provider_specific_error\n503 "
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if existed {
			common.OptionMap[ErrorMessageWhitelistOptionKey] = original
		} else {
			delete(common.OptionMap, ErrorMessageWhitelistOptionKey)
		}
	})

	assert.True(t, ShouldBypassErrorMessageHandling("PROVIDER_SPECIFIC_ERROR", 500))
	assert.True(t, ShouldBypassErrorMessageHandling("upstream_error", 503))
	assert.False(t, ShouldBypassErrorMessageHandling("upstream_error", 502))
}

func TestValidateErrorMessageWhitelist(t *testing.T) {
	require.NoError(t, ValidateErrorMessageWhitelist("provider_specific_error\n503"))
	tooManyValues := make([]string, MaxErrorMessageWhitelistCodes+1)
	for index := range tooManyValues {
		tooManyValues[index] = "code-" + strconv.Itoa(index)
	}
	tooMany := strings.Join(tooManyValues, "\n")
	assert.ErrorContains(t, ValidateErrorMessageWhitelist(tooMany), "最多支持")
	assert.ErrorContains(t, ValidateErrorMessageWhitelist(strings.Repeat("x", MaxErrorMessageWhitelistCodeLength+1)), "不能超过")
}

func TestShouldBypassErrorMessageHandlingIgnoresInvalidConfiguration(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	original, existed := common.OptionMap[ErrorMessageWhitelistOptionKey]
	tooManyValues := make([]string, MaxErrorMessageWhitelistCodes+1)
	for index := range tooManyValues {
		tooManyValues[index] = "code-" + strconv.Itoa(index)
	}
	common.OptionMap[ErrorMessageWhitelistOptionKey] = strings.Join(tooManyValues, "\n")
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if existed {
			common.OptionMap[ErrorMessageWhitelistOptionKey] = original
		} else {
			delete(common.OptionMap, ErrorMessageWhitelistOptionKey)
		}
	})

	assert.False(t, ShouldBypassErrorMessageHandling("code", 503))
}
