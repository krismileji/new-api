package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveMaskedErrorMessageMatchesCaseInsensitiveSubstring(t *testing.T) {
	message, matched := ResolveMaskedErrorMessage(
		[]string{"invalid api key"},
		"provider: INVALID API KEY; retry",
	)

	require.True(t, matched)
	assert.Equal(t, "provider: ; retry", message)
}

func TestResolveMaskedErrorMessageIgnoresBlankKeywords(t *testing.T) {
	message, matched := ResolveMaskedErrorMessage([]string{"", "  "}, "upstream failed")

	assert.False(t, matched)
	assert.Equal(t, "upstream failed", message)
}

func TestResolveMaskedErrorMessageRemovesAllOccurrences(t *testing.T) {
	message, matched := ResolveMaskedErrorMessage(
		[]string{"secret"},
		"secret upstream secret",
	)

	require.True(t, matched)
	assert.Equal(t, " upstream ", message)
}

func TestValidateErrorMessageKeywordsUsesOneTrimmedKeywordPerLine(t *testing.T) {
	require.NoError(t, ValidateErrorMessageKeywords(" invalid api key \n\nprovider"))
	require.ErrorContains(t, ValidateErrorMessageKeywords(strings.Repeat("x\n", MaxErrorMessageKeywords+1)), "最多支持")
	require.ErrorContains(t, ValidateErrorMessageKeywords(strings.Repeat("x", MaxErrorMessageKeywordLength+1)), "不能超过")
}

func TestResolveConfiguredErrorMessageReadsGlobalOption(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	original := common.OptionMap[ErrorMessageKeywordsOptionKey]
	common.OptionMap[ErrorMessageKeywordsOptionKey] = "secret"
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap[ErrorMessageKeywordsOptionKey] = original
		common.OptionMapRWMutex.Unlock()
	})

	message, matched := ResolveConfiguredErrorMessage("secret upstream")
	require.True(t, matched)
	assert.Equal(t, " upstream", message)
}
