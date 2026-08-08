package common

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	rootcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayAttemptStateRestoresRequestBaseline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Inbound-Baseline", "kept")
	c.Writer.Header().Set("X-Request-Baseline", "kept")
	c.Set("action", constant.TaskActionTextGenerate)

	startTime := time.Now()
	maxTokens := uint(321)
	temperature := 0.25
	request := &dto.GeneralOpenAIRequest{
		Model:         "model-a",
		Messages:      []dto.Message{{Role: "user", Content: "original prompt"}},
		StreamOptions: &dto.StreamOptions{IncludeUsage: true},
		MaxTokens:     &maxTokens,
		Temperature:   &temperature,
		Reasoning:     json.RawMessage(`{"effort":"medium"}`),
	}
	info := &RelayInfo{
		StartTime:         startTime,
		FirstResponseTime: startTime.Add(-time.Second),
		isFirstResponse:   true,
		OriginModelName:   "model-a",
		RequestURLPath:    "/v1/chat/completions",
		RelayMode:         1,
		RelayFormat:       types.RelayFormatOpenAI,
		IsStream:          false,
		Request:           request,
		RequestConversionChain: []types.RelayFormat{
			types.RelayFormatOpenAI,
		},
		ResponsesUsageInfo: &ResponsesUsageInfo{BuiltInTools: map[string]*BuildInToolInfo{
			dto.BuildInToolWebSearchPreview: {
				ToolName:  dto.BuildInToolWebSearchPreview,
				CallCount: 0,
			},
		}},
		TaskRelayInfo: &TaskRelayInfo{Action: constant.TaskActionRemix},
	}
	info.PriceData.ModelRatio = 2
	info.PriceData.AddOtherRatio("request", 3)
	state, err := NewRelayAttemptState(c, info)
	require.NoError(t, err)

	info.ChannelMeta = &ChannelMeta{
		UpstreamModelName: "mapped-by-first-attempt",
		IsModelMapped:     true,
	}
	info.OriginModelName = "mutated-model"
	info.RequestURLPath = "/v1/models/mutated/predictions"
	info.RelayMode = 99
	info.RelayFormat = types.RelayFormatClaude
	info.IsStream = true
	info.IsGeminiBatchEmbedding = true
	info.ShouldIncludeUsage = true
	info.DisablePing = true
	info.ReasoningEffort = "high"
	info.SendResponseCount = 4
	info.ReceivedResponseCount = 5
	info.RequestConversionChain = append(info.RequestConversionChain, types.RelayFormatClaude)
	info.FinalRequestRelayFormat = types.RelayFormatClaude
	info.RuntimeHeadersOverride = map[string]interface{}{"X-Stale": "value"}
	info.UseRuntimeHeadersOverride = true
	info.ParamOverrideAudit = []string{"stale"}
	info.StreamStatus = NewStreamStatus()
	info.ClaudeConvertInfo = &ClaudeConvertInfo{Done: true}
	info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount = 3
	info.ResponsesUsageInfo.BuiltInTools["stale-tool"] = &BuildInToolInfo{ToolName: "stale-tool", CallCount: 2}
	info.Action = constant.TaskActionTextGenerate
	info.PriceData.ModelRatio = 9
	info.PriceData.AddOtherRatio("request", 7)
	info.PriceData.AddOtherRatio("provider-only", 2)
	request.Model = "mutated-model"
	request.Messages = []dto.Message{{Role: "system", Content: "mutated prompt"}}
	request.StreamOptions = nil
	request.MaxTokens = nil
	request.MaxCompletionTokens = rootcommon.GetPointer(uint(999))
	request.Temperature = nil
	request.Reasoning = json.RawMessage(`{"effort":"high"}`)
	info.SetFirstResponseTime()
	c.Writer.Header().Del("X-Request-Baseline")
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("X-Codex-Turn-State", "stale")
	c.Set(EventStreamHeadersSetContextKey, true)
	c.Set("claude_web_search_requests", 9)
	c.Set("gemini_google_search_call", true)
	c.Set("action", "stale-action")
	c.Request.Header.Del("X-Inbound-Baseline")
	c.Request.Header.Set("X-Attempt-Only", "stale")
	rootcommon.SetContextKey(c, constant.ContextKeyAdminRejectReason, "stale rejection")
	rootcommon.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)

	require.NoError(t, state.Reset(c, info))

	assert.Equal(t, "model-a", info.OriginModelName)
	assert.Nil(t, info.ChannelMeta)
	assert.Equal(t, "model-a", request.Model)
	assert.Equal(t, []dto.Message{{Role: "user", Content: "original prompt"}}, request.Messages)
	require.NotNil(t, request.StreamOptions)
	assert.True(t, request.StreamOptions.IncludeUsage)
	require.NotNil(t, request.MaxTokens)
	assert.Equal(t, uint(321), *request.MaxTokens)
	assert.Nil(t, request.MaxCompletionTokens)
	require.NotNil(t, request.Temperature)
	assert.Equal(t, 0.25, *request.Temperature)
	assert.JSONEq(t, `{"effort":"medium"}`, string(request.Reasoning))
	assert.Equal(t, "/v1/chat/completions", info.RequestURLPath)
	assert.Equal(t, 1, info.RelayMode)
	assert.Equal(t, types.RelayFormatOpenAI, info.RelayFormat)
	assert.False(t, info.IsStream)
	assert.False(t, info.IsGeminiBatchEmbedding)
	assert.False(t, info.ShouldIncludeUsage)
	assert.False(t, info.DisablePing)
	assert.Empty(t, info.ReasoningEffort)
	assert.Zero(t, info.SendResponseCount)
	assert.Zero(t, info.ReceivedResponseCount)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatOpenAI}, info.RequestConversionChain)
	assert.Empty(t, info.FinalRequestRelayFormat)
	assert.Nil(t, info.RuntimeHeadersOverride)
	assert.False(t, info.UseRuntimeHeadersOverride)
	assert.Nil(t, info.ParamOverrideAudit)
	assert.Nil(t, info.StreamStatus)
	assert.Nil(t, info.ClaudeConvertInfo)
	require.NotNil(t, info.ResponsesUsageInfo)
	require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, dto.BuildInToolWebSearchPreview)
	assert.Zero(t, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	assert.NotContains(t, info.ResponsesUsageInfo.BuiltInTools, "stale-tool")
	assert.Equal(t, constant.TaskActionRemix, info.Action)
	assert.Equal(t, 2.0, info.PriceData.ModelRatio)
	assert.Equal(t, map[string]float64{"request": 3}, info.PriceData.OtherRatios())
	assert.Equal(t, startTime.Add(-time.Second), info.FirstResponseTime)
	assert.True(t, info.isFirstResponse)
	assert.Equal(t, "kept", c.Request.Header.Get("X-Inbound-Baseline"))
	assert.Empty(t, c.Request.Header.Get("X-Attempt-Only"))
	assert.Equal(t, "kept", c.Writer.Header().Get("X-Request-Baseline"))
	assert.Empty(t, c.Writer.Header().Get("Content-Type"))
	assert.Empty(t, c.Writer.Header().Get("X-Codex-Turn-State"))
	assert.False(t, c.GetBool(EventStreamHeadersSetContextKey))
	assert.Zero(t, c.GetInt("claude_web_search_requests"))
	assert.False(t, c.GetBool("gemini_google_search_call"))
	assert.Equal(t, constant.TaskActionTextGenerate, c.GetString("action"))
	assert.Empty(t, rootcommon.GetContextKeyString(c, constant.ContextKeyAdminRejectReason))
	assert.False(t, rootcommon.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))

	info.PriceData.AddOtherRatio("second-attempt", 4)
	require.NoError(t, state.Reset(c, info))
	assert.Equal(t, map[string]float64{"request": 3}, info.PriceData.OtherRatios())
}

func TestRelayAttemptStatePreservesExistingModelMappingBaseline(t *testing.T) {
	info := &RelayInfo{
		OriginModelName: "client-model",
		ChannelMeta: &ChannelMeta{
			ChannelId:         7,
			ChannelBaseUrl:    "https://initial.example.com",
			UpstreamModelName: "initial-upstream-model",
			IsModelMapped:     true,
			ParamOverride: map[string]interface{}{
				"temperature": 0.2,
			},
		},
	}
	state, err := NewRelayAttemptState(nil, info)
	require.NoError(t, err)

	info.UpstreamModelName = "mutated-upstream-model"
	info.IsModelMapped = false
	info.ChannelId = 9
	info.ChannelBaseUrl = "https://mutated.example.com"
	info.ParamOverride["temperature"] = 0.9

	require.NoError(t, state.Reset(nil, info))
	assert.Equal(t, 7, info.ChannelId)
	assert.Equal(t, "https://initial.example.com", info.ChannelBaseUrl)
	assert.Equal(t, "initial-upstream-model", info.UpstreamModelName)
	assert.True(t, info.IsModelMapped)
	assert.Equal(t, 0.2, info.ParamOverride["temperature"])
}

func TestRelayAttemptStateRestoresClientModelIndependentlyFromOriginRoutingModel(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{Model: "client-model"}
	info := &RelayInfo{
		OriginModelName: "client-model-compact",
		Request:         request,
	}
	state, err := NewRelayAttemptState(nil, info)
	require.NoError(t, err)

	request.Model = "mapped-upstream-model"
	info.OriginModelName = "mutated-routing-model"

	require.NoError(t, state.Reset(nil, info))
	assert.Equal(t, "client-model", request.Model)
	assert.Equal(t, "client-model-compact", info.OriginModelName)
}

func TestRelayAttemptStateRecreatesClaudeConversionState(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:       types.RelayFormatClaude,
		ClaudeConvertInfo: &ClaudeConvertInfo{LastMessagesType: LastMessageTypeNone},
	}
	state, err := NewRelayAttemptState(nil, info)
	require.NoError(t, err)
	info.ClaudeConvertInfo.Done = true
	info.ClaudeConvertInfo.Index = 7

	require.NoError(t, state.Reset(nil, info))

	require.NotNil(t, info.ClaudeConvertInfo)
	assert.Equal(t, LastMessageTypeNone, info.ClaudeConvertInfo.LastMessagesType)
	assert.False(t, info.ClaudeConvertInfo.Done)
	assert.Zero(t, info.ClaudeConvertInfo.Index)
}

func TestRelayAttemptStateRestoresRequestFieldsExcludedFromJSON(t *testing.T) {
	t.Run("image extra", func(t *testing.T) {
		request := &dto.ImageRequest{
			Model:  "image-model",
			Prompt: "original prompt",
			Extra: map[string]json.RawMessage{
				"parameters": json.RawMessage(`{"watermark":false}`),
			},
		}
		info := &RelayInfo{OriginModelName: request.Model, Request: request}
		state, err := NewRelayAttemptState(nil, info)
		require.NoError(t, err)

		request.Prompt = "mutated prompt"
		request.Extra["parameters"][0] = '['
		request.Extra["stale"] = json.RawMessage(`true`)

		require.NoError(t, state.Reset(nil, info))
		assert.Equal(t, "original prompt", request.Prompt)
		require.Len(t, request.Extra, 1)
		assert.JSONEq(t, `{"watermark":false}`, string(request.Extra["parameters"]))
	})

	t.Run("alpha search raw body", func(t *testing.T) {
		request := &dto.AlphaSearchRequest{
			Model:   "search-model",
			RawBody: json.RawMessage(`{"model":"search-model","unknown":{"keep":true}}`),
		}
		info := &RelayInfo{OriginModelName: request.Model, Request: request}
		state, err := NewRelayAttemptState(nil, info)
		require.NoError(t, err)

		request.RawBody[0] = '['
		request.RawBody = append(request.RawBody, []byte(`,"stale":true`)...)

		require.NoError(t, state.Reset(nil, info))
		assert.JSONEq(t, `{"model":"search-model","unknown":{"keep":true}}`, string(request.RawBody))
	})
}

func TestRelayAttemptStateRestoresFieldsFilteredByMarshalJSON(t *testing.T) {
	t.Run("chat completions thinking budget on model alias", func(t *testing.T) {
		request := &dto.GeneralOpenAIRequest{
			Model:          "client-model-alias",
			ThinkingBudget: json.RawMessage(`0`),
		}
		info := &RelayInfo{OriginModelName: request.Model, Request: request}
		state, err := NewRelayAttemptState(nil, info)
		require.NoError(t, err)

		request.Model = "qwen-plus"
		request.ThinkingBudget[0] = '9'

		require.NoError(t, state.Reset(nil, info))
		assert.Equal(t, "client-model-alias", request.Model)
		assert.Equal(t, "0", string(request.ThinkingBudget))
	})

	t.Run("responses thinking budget on model alias", func(t *testing.T) {
		request := &dto.OpenAIResponsesRequest{
			Model:          "client-model-alias",
			ThinkingBudget: json.RawMessage(`128`),
		}
		info := &RelayInfo{OriginModelName: request.Model, Request: request}
		state, err := NewRelayAttemptState(nil, info)
		require.NoError(t, err)

		request.Model = "qwen-plus"
		request.ThinkingBudget[0] = '9'

		require.NoError(t, state.Reset(nil, info))
		assert.Equal(t, "client-model-alias", request.Model)
		assert.Equal(t, "128", string(request.ThinkingBudget))
	})
}
