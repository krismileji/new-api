package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIContextLimitRejectsCompleteJSON(t *testing.T) {
	setInputContextLimitForTest(t, true)
	payload := strings.Repeat("x ", MaxInputContextTokens+1024)

	for _, testCase := range []struct {
		name string
		item map[string]any
	}{
		{
			name: "function call output",
			item: map[string]any{
				"type":    "function_call_output",
				"call_id": "call_123",
				"output":  payload,
			},
		},
		{
			name: "unknown future item",
			item: map[string]any{
				"type":    "future_context_item",
				"payload": payload,
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			body, err := common.Marshal(map[string]any{
				"model": "gpt-5.5",
				"input": []any{testCase.item},
			})
			require.NoError(t, err)

			tokens, apiErr := EnforceInputContextLimit(body)
			require.NotNil(t, apiErr)
			assert.Greater(t, tokens, MaxInputContextTokens)
			assert.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode())
			assert.Equal(t, 200, apiErr.StatusCode)
			assert.True(t, types.IsSkipRetryError(apiErr))
		})
	}
}

func TestOpenAIContextLimitAllowsUncountableState(t *testing.T) {
	setInputContextLimitForTest(t, true)

	for _, body := range []string{
		`{"model":"gpt-5.5","input":"hello","previous_response_id":"resp_123"}`,
		`{"model":"gpt-5.5","input":"hello","conversation":{"id":"conv_123"}}`,
		`{"model":"gpt-5.5","input":"hello","prompt":{"id":"pmpt_123"}}`,
		`{"model":"gpt-5.5","input":[{"type":"item_reference","id":"item_123"}]}`,
		`{"model":"gpt-5.5","input":[{"type":"reasoning","encrypted_content":"opaque"}]}`,
		`{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":[{"type":"input_file","file_url":"https://example.com/a.pdf"}]}]}`,
		`{"model":"gpt-5.5","tools":[{"type":"web_search_preview"}],"input":"hello"}`,
	} {
		tokens, apiErr := EnforceInputContextLimit([]byte(body))
		require.Nil(t, apiErr)
		assert.Positive(t, tokens)
	}
}

func TestShouldEnforceInputContextLimitUsesOpenAIChannelAndTextEndpoint(t *testing.T) {
	setInputContextLimitForTest(t, true)

	for _, path := range []string{
		"/v1/chat/completions",
		"/v1/completions",
		"/v1/responses",
		"/v1/responses/compact",
		"/proxy/openai/v1/responses",
		"/v1/responses/",
		"/v1/responses?store=false",
	} {
		assert.True(t, ShouldEnforceInputContextLimit(constant.ChannelTypeOpenAI, path), path)
	}
	for _, path := range []string{
		"/v1/moderations",
		"/v1/realtime",
		"/v1/images/generations",
		"/v1/audio/speech",
		"/v1/embeddings",
		"/v1/messages",
		"/v1beta/models/gemini-test:generateContent",
	} {
		assert.False(t, ShouldEnforceInputContextLimit(constant.ChannelTypeOpenAI, path), path)
	}
	for _, channelType := range []int{
		constant.ChannelTypeAnthropic,
		constant.ChannelTypeGemini,
		constant.ChannelTypeAws,
	} {
		assert.False(t, ShouldEnforceInputContextLimit(channelType, "/v1/responses"), channelType)
	}
}

func TestInputContextLimitCanBeDisabled(t *testing.T) {
	setInputContextLimitForTest(t, false)

	tokens, apiErr := EnforceInputContextLimit([]byte(strings.Repeat("x ", MaxInputContextTokens+1024)))
	assert.Zero(t, tokens)
	assert.Nil(t, apiErr)
	assert.Nil(t, EnforceInputContextTokenLimit(MaxInputContextTokens+1))
	assert.False(t, ShouldEnforceInputContextLimit(constant.ChannelTypeOpenAI, "/v1/responses"))
}

func TestInputContextTokenLimitBoundary(t *testing.T) {
	setInputContextLimitForTest(t, true)
	assert.Nil(t, EnforceInputContextTokenLimit(MaxInputContextTokens))

	apiErr := EnforceInputContextTokenLimit(MaxInputContextTokens + 1)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(apiErr))
}

func setInputContextLimitForTest(t *testing.T, enabled bool) {
	t.Helper()
	previous := enableInputContextLimit
	enableInputContextLimit = enabled
	t.Cleanup(func() {
		enableInputContextLimit = previous
	})
}
