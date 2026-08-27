package service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withChannelMonitorRetrySkipOptions(t *testing.T, codes, messages string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	originalCodes, hadCodes := common.OptionMap[RetrySkipErrorCodesOptionKey]
	originalMessages, hadMessages := common.OptionMap[RetrySkipErrorMessagesOptionKey]
	common.OptionMap[RetrySkipErrorCodesOptionKey] = codes
	common.OptionMap[RetrySkipErrorMessagesOptionKey] = messages
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		if hadCodes {
			common.OptionMap[RetrySkipErrorCodesOptionKey] = originalCodes
		} else {
			delete(common.OptionMap, RetrySkipErrorCodesOptionKey)
		}
		if hadMessages {
			common.OptionMap[RetrySkipErrorMessagesOptionKey] = originalMessages
		} else {
			delete(common.OptionMap, RetrySkipErrorMessagesOptionKey)
		}
		common.OptionMapRWMutex.Unlock()
	})
}

func TestShouldSkipRetryForConfiguredErrorMatchesErrorCode(t *testing.T) {
	withChannelMonitorRetrySkipOptions(t, " insufficient_quota\n429 ", "")

	assert.True(t, ShouldSkipRetryForConfiguredError("INSUFFICIENT_QUOTA", 500, "temporary failure"))
	assert.True(t, ShouldSkipRetryForConfiguredError("upstream_error", http.StatusTooManyRequests, "temporary failure"))
	assert.False(t, ShouldSkipRetryForConfiguredError("upstream_error", http.StatusBadGateway, "temporary failure"))
}

func TestShouldSkipRetryForConfiguredErrorMatchesMessageSubstring(t *testing.T) {
	withChannelMonitorRetrySkipOptions(t, "", "invalid api key, quota exceeded")

	assert.True(t, ShouldSkipRetryForConfiguredError("upstream_error", 0, "Provider returned INVALID API KEY"))
	assert.False(t, ShouldSkipRetryForConfiguredError("upstream_error", 0, "temporary failure"))
}

func TestShouldSkipRetryForErrorUsesNewAPIErrorFields(t *testing.T) {
	withChannelMonitorRetrySkipOptions(t, "provider_limit", "")
	err := types.NewErrorWithStatusCode(errors.New("provider rejected request"), types.ErrorCode("provider_limit"), http.StatusBadRequest)

	assert.True(t, ShouldSkipRetryForError(err))
}

func TestShouldSkipRetryForErrorOnlyExtractsExplicitStatusCodes(t *testing.T) {
	withChannelMonitorRetrySkipOptions(t, "429", "")

	assert.True(t, ShouldSkipRetryForError(errors.New("upstream status code: 429")))
	assert.True(t, ShouldSkipRetryForError(errors.New("HTTP 429 from provider")))
	assert.True(t, ShouldSkipRetryForError(errors.New("上游返回 429 Too Many Requests")))
	assert.True(t, ShouldSkipRetryForError(errors.New("接口返回 429 Too Many Requests")))
	assert.False(t, ShouldSkipRetryForError(errors.New("request 429938 failed")))
	assert.False(t, ShouldSkipRetryForError(errors.New("request id 429 failed")))
}

func TestValidateRetrySkipConfiguration(t *testing.T) {
	require.NoError(t, ValidateRetrySkipErrorCodes("429\ninsufficient_quota"))
	require.NoError(t, ValidateRetrySkipErrorMessages("quota exceeded, invalid api key"))
	tooManyCodes := make([]string, MaxRetrySkipErrorCodes+1)
	for index := range tooManyCodes {
		tooManyCodes[index] = fmt.Sprintf("code-%d", index)
	}
	assert.ErrorContains(t, ValidateRetrySkipErrorCodes(strings.Join(tooManyCodes, "\n")), "最多支持")
	assert.ErrorContains(t, ValidateRetrySkipErrorMessages(strings.Repeat("x", MaxRetrySkipErrorMessageLength+1)), "不能超过")
}

func TestShouldSkipRetryForConfiguredErrorIgnoresInvalidConfiguration(t *testing.T) {
	tooManyCodes := make([]string, MaxRetrySkipErrorCodes+1)
	tooManyMessages := make([]string, MaxRetrySkipErrorMessages+1)
	for index := range tooManyCodes {
		tooManyCodes[index] = fmt.Sprintf("code-%d", index)
	}
	for index := range tooManyMessages {
		tooManyMessages[index] = fmt.Sprintf("message-%d", index)
	}
	withChannelMonitorRetrySkipOptions(t, strings.Join(tooManyCodes, "\n"), strings.Join(tooManyMessages, "\n"))

	assert.False(t, ShouldSkipRetryForConfiguredError("x", 0, "y"))
	assert.False(t, ShouldSkipRetryForError(nil))
}
