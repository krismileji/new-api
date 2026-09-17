package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSmallInputResponseCompleteContextAndUnknownInputs(t *testing.T) {
	previous := constant.CountToken
	constant.CountToken = false
	t.Cleanup(func() { constant.CountToken = previous })
	for _, tc := range []struct {
		name, body string
		request    dto.Request
		valid      bool
	}{
		{"chat", `{"model":"gpt-4o","messages":[{"role":"system","content":"完整系统提示"},{"role":"user","content":"hello"}]}`, &dto.GeneralOpenAIRequest{}, true},
		{"responses", `{"model":"gpt-4o","input":"hello","instructions":"完整系统提示"}`, &dto.OpenAIResponsesRequest{}, true},
		{"claude", `{"model":"claude-sonnet-4","system":"完整系统提示","messages":[{"role":"user","content":"hello"}],"max_tokens":10}`, &dto.ClaudeRequest{}, true},
		{"gemini", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"systemInstruction":{"parts":[{"text":"完整系统提示"}]}}`, &dto.GeminiChatRequest{}, true},
		{"referenced context", `{"input":"hello","previous_response_id":"resp_previous"}`, &dto.OpenAIResponsesRequest{}, false},
		{"image", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://invalid.example/image.png"}}]}]}`, &dto.GeneralOpenAIRequest{}, false},
		{"required tool", `{"messages":[{"role":"user","content":"hello"}],"tool_choice":"required"}`, &dto.GeneralOpenAIRequest{}, false},
		{"json schema", `{"messages":[{"role":"user","content":"hello"}],"response_format":{"type":"json_schema"}}`, &dto.GeneralOpenAIRequest{}, false},
		{"gemini cached", `{"contents":[{"parts":[{"text":"hello"}]}],"cachedContent":"cachedContents/1"}`, &dto.GeminiChatRequest{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, common.UnmarshalJsonStr(tc.body, tc.request))
			info := &relaycommon.RelayInfo{Request: tc.request, OriginModelName: "gpt-4o", RelayMode: relayconstant.RelayModeChatCompletions}
			tokens, _, valid := EstimateChannelSmallInput(info)
			assert.Equal(t, tc.valid, valid)
			if valid {
				assert.Positive(t, tokens)
			}
		})
	}
	var short, full dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"messages":[{"role":"user","content":"hello"}]}`, &short))
	full = short
	full.Messages = append([]dto.Message{{Role: "system", Content: strings.Repeat("system context ", 50)}}, full.Messages...)
	info := &relaycommon.RelayInfo{Request: &short, OriginModelName: "gpt-4o", RelayMode: relayconstant.RelayModeChatCompletions}
	shortTokens, _, valid := EstimateChannelSmallInput(info)
	require.True(t, valid)
	info.Request = &full
	fullTokens, _, valid := EstimateChannelSmallInput(info)
	require.True(t, valid)
	assert.Greater(t, fullTokens, shortTokens)
	info.IsChannelTest = true
	_, _, valid = EstimateChannelSmallInput(info)
	assert.False(t, valid)
}

func TestChannelSmallInputResponseHonorsOutputLimit(t *testing.T) {
	text := "第一行中文\nSecond line with extra output."
	for _, limit := range []int{0, 1, 5, 1000} {
		result := BuildChannelSmallInputText(text, "gpt-4o", 42, &limit)
		assert.LessOrEqual(t, result.OutputTokens, limit)
		assert.Equal(t, 42, result.InputTokens)
		assert.Equal(t, CountTextToken(result.Text, "gpt-4o"), result.OutputTokens)
		assert.Equal(t, limit < CountTextToken(text, "gpt-4o"), result.Truncated)
	}
}
