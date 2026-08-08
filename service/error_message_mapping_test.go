package service

import (
	"testing"

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
