package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
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

func TestInputContextLimitFormatsAreOpenAIOnly(t *testing.T) {
	for _, format := range []types.RelayFormat{
		types.RelayFormatOpenAI,
		types.RelayFormatOpenAIResponses,
		types.RelayFormatOpenAIResponsesCompaction,
	} {
		assert.True(t, IsInputContextLimitFormat(format), format)
	}
	for _, format := range []types.RelayFormat{
		types.RelayFormatClaude,
		types.RelayFormatGemini,
		types.RelayFormatOpenAIRealtime,
		types.RelayFormatEmbedding,
		types.RelayFormatRerank,
	} {
		assert.False(t, IsInputContextLimitFormat(format), format)
	}
}

func TestInputContextLimitCanBeDisabled(t *testing.T) {
	setInputContextLimitForTest(t, false)

	tokens, apiErr := EnforceInputContextLimit([]byte(strings.Repeat("x ", MaxInputContextTokens+1024)))
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
