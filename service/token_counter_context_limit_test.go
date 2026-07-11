package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEstimateRequestTokenForContextLimitUsesSemanticText(t *testing.T) {
	context, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{}

	for _, testCase := range []struct {
		name string
		want int
	}{
		{name: "at limit", want: MaxInputContextTokens},
		{name: "over limit", want: MaxInputContextTokens + 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			meta := &types.TokenCountMeta{
				CombineText: strings.Repeat("a", testCase.want),
				TokenType:   types.TokenTypeTextNumber,
			}

			got, err := EstimateRequestTokenForContextLimit(context, meta, info)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestResponsesContextLimitRejectsPreviouslyDroppedItems(t *testing.T) {
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
			input, err := common.Marshal([]any{testCase.item})
			require.NoError(t, err)
			request := &dto.OpenAIResponsesRequest{Model: "gpt-5.5", Input: input}

			meta := request.GetTokenCountMeta()
			assert.Contains(t, meta.CombineText, payload)

			body, err := common.Marshal(request)
			require.NoError(t, err)
			tokens, apiErr := EnforceInputContextLimit(body, types.RelayFormatOpenAIResponses)
			require.NotNil(t, apiErr)
			assert.Greater(t, tokens, MaxInputContextTokens)
			assert.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode())
			assert.Equal(t, 200, apiErr.StatusCode)
			assert.True(t, types.IsSkipRetryError(apiErr))
		})
	}
}

func TestInputContextLimitRejectsUncountableState(t *testing.T) {
	setInputContextLimitForTest(t, true)
	for _, testCase := range []struct {
		name       string
		format     types.RelayFormat
		body       string
		wantInPath string
	}{
		{name: "responses previous response", format: types.RelayFormatOpenAIResponses, body: `{"model":"gpt-5.5","input":"hello","previous_response_id":"resp_123"}`, wantInPath: "previous_response_id"},
		{name: "responses conversation", format: types.RelayFormatOpenAIResponses, body: `{"model":"gpt-5.5","input":"hello","conversation":{"id":"conv_123"}}`, wantInPath: "conversation"},
		{name: "responses reusable prompt", format: types.RelayFormatOpenAIResponses, body: `{"model":"gpt-5.5","input":"hello","prompt":{"id":"pmpt_123"}}`, wantInPath: "prompt"},
		{name: "responses item reference", format: types.RelayFormatOpenAIResponses, body: `{"model":"gpt-5.5","input":[{"type":"item_reference","id":"item_123"}]}`, wantInPath: "item_reference"},
		{name: "responses file id", format: types.RelayFormatOpenAIResponses, body: `{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":[{"type":"input_file","file_id":"file_123"}]}]}`, wantInPath: "file_id"},
		{name: "responses remote file", format: types.RelayFormatOpenAIResponses, body: `{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":[{"type":"input_file","file_url":"https://example.com/a.pdf"}]}]}`, wantInPath: "file_url"},
		{name: "responses encrypted reasoning", format: types.RelayFormatOpenAIResponses, body: `{"model":"gpt-5.5","input":[{"type":"reasoning","encrypted_content":"opaque"}]}`, wantInPath: "encrypted_content"},
		{name: "responses compact previous response", format: types.RelayFormatOpenAIResponsesCompaction, body: `{"model":"gpt-5.5","previous_response_id":"resp_123"}`, wantInPath: "previous_response_id"},
		{name: "responses compact reasoning context", format: types.RelayFormatOpenAIResponsesCompaction, body: `{"model":"gpt-5.5","reasoning":{"context":["opaque"]}}`, wantInPath: "reasoning.context"},
		{name: "responses nested audio url", format: types.RelayFormatOpenAIResponses, body: `{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":[{"type":"input_audio","input_audio":{"url":"https://example.com/a.wav"}}]}]}`, wantInPath: "input_audio.url"},
		{name: "responses file string url", format: types.RelayFormatOpenAIResponses, body: `{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":[{"type":"input_file","file":"https://example.com/a.pdf"}]}]}`, wantInPath: ".file"},
		{name: "chat file id", format: types.RelayFormatOpenAI, body: `{"model":"gpt-5.5","messages":[{"role":"user","content":[{"type":"file","file":{"file_id":"file_123"}}]}]}`, wantInPath: "file_id"},
		{name: "chat remote image", format: types.RelayFormatOpenAI, body: `{"model":"gpt-5.5","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`, wantInPath: "image_url"},
		{name: "chat empty web search options", format: types.RelayFormatOpenAI, body: `{"model":"gpt-5.5","messages":[{"role":"user","content":"hello"}],"web_search_options":{}}`, wantInPath: "server-side search"},
		{name: "claude container", format: types.RelayFormatClaude, body: `{"model":"claude-sonnet","messages":[{"role":"user","content":"hello"}],"container":"container_123"}`, wantInPath: "container"},
		{name: "claude redacted thinking", format: types.RelayFormatClaude, body: `{"model":"claude-sonnet","messages":[{"role":"assistant","content":[{"type":"redacted_thinking","data":"opaque"}]}]}`, wantInPath: "messages[0].content[0]"},
		{name: "gemini cached content", format: types.RelayFormatGemini, body: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"cachedContent":"cachedContents/123"}`, wantInPath: "cachedContent"},
		{name: "gemini file uri", format: types.RelayFormatGemini, body: `{"contents":[{"role":"user","parts":[{"fileData":{"fileUri":"gs://bucket/file"}}]}]}`, wantInPath: "fileData.fileUri"},
		{name: "gemini thought signature", format: types.RelayFormatGemini, body: `{"contents":[{"role":"model","parts":[{"text":"thought","thoughtSignature":"opaque"}]}]}`, wantInPath: "thoughtSignature"},
		{name: "gemini empty google search tool", format: types.RelayFormatGemini, body: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"tools":[{"googleSearch":{}}]}`, wantInPath: "googleSearch"},
		{name: "gemini single code execution tool", format: types.RelayFormatGemini, body: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"tools":{"codeExecution":{}}}`, wantInPath: "codeExecution"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tokens, apiErr := EnforceInputContextLimit([]byte(testCase.body), testCase.format)
			require.NotNil(t, apiErr)
			assert.LessOrEqual(t, tokens, MaxInputContextTokens)
			assert.Contains(t, apiErr.Error(), testCase.wantInPath)
			assert.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode())
			assert.Equal(t, 200, apiErr.StatusCode)
			assert.True(t, types.IsSkipRetryError(apiErr))
		})
	}
}

func TestInputContextLimitStateChecksArePathAware(t *testing.T) {
	setInputContextLimitForTest(t, true)
	for _, testCase := range []struct {
		name   string
		format types.RelayFormat
		body   string
	}{
		{
			name:   "file id text inside tool output",
			format: types.RelayFormatOpenAIResponses,
			body:   `{"model":"gpt-5.5","input":[{"type":"function_call_output","call_id":"call_123","output":"{\"file_id\":\"file_123\"}"}]}`,
		},
		{
			name:   "gemini internal thought signature bypass",
			format: types.RelayFormatGemini,
			body:   `{"contents":[{"role":"model","parts":[{"text":"hello","thoughtSignature":"context_engineering_is_the_way_to_go"}]}]}`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tokens, apiErr := EnforceInputContextLimit([]byte(testCase.body), testCase.format)
			require.Nil(t, apiErr)
			assert.Positive(t, tokens)
		})
	}
}

func TestInputContextLimitCanBeDisabled(t *testing.T) {
	setInputContextLimitForTest(t, false)

	tokens, apiErr := EnforceInputContextLimit(
		[]byte(`{"model":"gpt-5.5","input":"hello","previous_response_id":"resp_123"}`),
		types.RelayFormatOpenAIResponses,
	)
	assert.Zero(t, tokens)
	assert.Nil(t, apiErr)
	assert.Nil(t, EnforceInputContextTokenLimit(MaxInputContextTokens+1))
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
