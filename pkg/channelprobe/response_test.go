package channelprobe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useProbeResponseOptionMap(t *testing.T, values map[string]string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	original := common.OptionMap
	common.OptionMap = make(map[string]string, len(values))
	for key, value := range values {
		common.OptionMap[key] = value
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = original
		common.OptionMapRWMutex.Unlock()
	})
}

func testProbeResponseConfig() ResponseConfig {
	config := DefaultResponseConfig()
	config.ResponseText = "Configured probe response"
	config.InputTokens = 7
	config.CacheWriteTokens = 1
	config.CachedTokens = 2
	config.OutputTokens = 11
	return config
}

func TestMatchesChannelProbeRequest(t *testing.T) {
	tests := []struct {
		name      string
		relayMode int
		request   dto.Request
		want      bool
	}{
		{
			name:      "responses string input",
			relayMode: relayconstant.RelayModeResponses,
			request: &dto.OpenAIResponsesRequest{
				Model: "gpt-5.6-sol",
				Input: json.RawMessage(`" HI "`),
			},
			want: true,
		},
		{
			name:      "responses allows instructions and one user input",
			relayMode: relayconstant.RelayModeResponses,
			request: &dto.OpenAIResponsesRequest{
				Model:        "gpt-5.6-sol",
				Instructions: json.RawMessage(`"probe instructions"`),
				Input: json.RawMessage(`[
					{"type":"message","role":"developer","content":"system rules"},
					{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}
				]`),
			},
			want: true,
		},
		{
			name:      "responses rejects ordinary text",
			relayMode: relayconstant.RelayModeResponses,
			request: &dto.OpenAIResponsesRequest{
				Model: "gpt-5.6-sol",
				Input: json.RawMessage(`"say hi to me"`),
			},
		},
		{
			name:      "responses rejects multiple user turns",
			relayMode: relayconstant.RelayModeResponses,
			request: &dto.OpenAIResponsesRequest{
				Model: "gpt-5.6-sol",
				Input: json.RawMessage(`[
					{"role":"user","content":"hi"},
					{"role":"user","content":"hi"}
				]`),
			},
		},
		{
			name:      "responses rejects media input",
			relayMode: relayconstant.RelayModeResponses,
			request: &dto.OpenAIResponsesRequest{
				Model: "gpt-5.6-sol",
				Input: json.RawMessage(`[
					{"role":"user","content":[
						{"type":"input_text","text":"hi"},
						{"type":"input_image","image_url":"https://example.com/probe.png"}
					]}
				]`),
			},
		},
		{
			name:      "responses rejects continued conversation",
			relayMode: relayconstant.RelayModeResponses,
			request: &dto.OpenAIResponsesRequest{
				Model:              "gpt-5.6-sol",
				Input:              json.RawMessage(`"hi"`),
				PreviousResponseID: "resp_previous",
			},
		},
		{
			name:      "chat allows system prompt and one user message",
			relayMode: relayconstant.RelayModeChatCompletions,
			request: &dto.GeneralOpenAIRequest{
				Model: "gpt-5.6-sol",
				Messages: []dto.Message{
					{Role: "system", Content: "probe instructions"},
					{Role: "user", Content: " hi "},
				},
			},
			want: true,
		},
		{
			name:      "chat accepts one text content part",
			relayMode: relayconstant.RelayModeChatCompletions,
			request: &dto.GeneralOpenAIRequest{
				Model: "gpt-5.6-sol",
				Messages: []dto.Message{{
					Role: "user",
					Content: []any{
						map[string]any{"type": "text", "text": "HI"},
					},
				}},
			},
			want: true,
		},
		{
			name:      "chat rejects assistant history",
			relayMode: relayconstant.RelayModeChatCompletions,
			request: &dto.GeneralOpenAIRequest{
				Model: "gpt-5.6-sol",
				Messages: []dto.Message{
					{Role: "assistant", Content: "earlier response"},
					{Role: "user", Content: "hi"},
				},
			},
		},
		{
			name:      "chat rejects multiple content parts",
			relayMode: relayconstant.RelayModeChatCompletions,
			request: &dto.GeneralOpenAIRequest{
				Model: "gpt-5.6-sol",
				Messages: []dto.Message{{
					Role: "user",
					Content: []any{
						map[string]any{"type": "text", "text": "hi"},
						map[string]any{"type": "text", "text": "again"},
					},
				}},
			},
		},
		{
			name:      "other relay modes are not probes",
			relayMode: relayconstant.RelayModeCompletions,
			request: &dto.GeneralOpenAIRequest{
				Model:  "gpt-5.6-sol",
				Prompt: "hi",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, matchesChannelProbeRequest(test.relayMode, test.request, DefaultMatchInput))
		})
	}
}

func TestMatchesChannelProbeRequestUsesConfiguredInput(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`" health check "`),
	}

	assert.True(t, matchesChannelProbeRequest(relayconstant.RelayModeResponses, request, "health check"))
	assert.False(t, matchesChannelProbeRequest(relayconstant.RelayModeResponses, request, DefaultMatchInput))
}

func TestMiddlewareRoutesProbeRequestsBeforeDownstream(t *testing.T) {
	useProbeResponseOptionMap(t, map[string]string{
		OptionKey:             "true",
		MatchInputOptionKey:   "health check",
		ResponseTextOptionKey: "healthy",
		MinDelayMsOptionKey:   "0",
		MaxDelayMsOptionKey:   "0",
		InputTokensOptionKey:  "7",
		OutputTokensOptionKey: "11",
	})

	t.Run("intercepts a matching request without calling downstream", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		downstreamCalled := false
		router := gin.New()
		router.Use(Middleware())
		router.POST("/v1/responses", func(c *gin.Context) {
			downstreamCalled = true
			c.Status(http.StatusNoContent)
		})

		request := httptest.NewRequest(
			http.MethodPost,
			"/v1/responses",
			strings.NewReader(`{"model":"gpt-5.6-sol","input":"health check"}`),
		)
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		router.ServeHTTP(recorder, request)

		assert.False(t, downstreamCalled)
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Contains(t, recorder.Body.String(), `"text":"healthy"`)
		assert.Contains(t, recorder.Body.String(), `"input_tokens":7`)
		assert.Contains(t, recorder.Body.String(), `"output_tokens":11`)
		assert.Contains(t, recorder.Body.String(), `"total_tokens":18`)
	})

	t.Run("passes a non-matching request with its body reusable", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		var downstreamRequest dto.OpenAIResponsesRequest
		router := gin.New()
		router.Use(Middleware())
		router.POST("/v1/responses", func(c *gin.Context) {
			cachedRequest, cached := ValidatedRequest(c, types.RelayFormatOpenAIResponses)
			require.True(t, cached)
			require.IsType(t, &dto.OpenAIResponsesRequest{}, cachedRequest)
			require.NoError(t, common.DecodeJson(c.Request.Body, &downstreamRequest))
			c.Status(http.StatusNoContent)
		})

		request := httptest.NewRequest(
			http.MethodPost,
			"/v1/responses",
			strings.NewReader(`{"model":"gpt-5.6-sol","input":"hello"}`),
		)
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		router.ServeHTTP(recorder, request)

		assert.Equal(t, http.StatusNoContent, recorder.Code)
		assert.Equal(t, "gpt-5.6-sol", downstreamRequest.Model)
		assert.JSONEq(t, `"hello"`, string(downstreamRequest.Input))
	})
}

func TestServeChannelProbeResponsesJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	temperature := 0.7
	topP := 0.98
	request := &dto.OpenAIResponsesRequest{
		Model:             "gpt-5.6-sol",
		Input:             json.RawMessage(`"hi"`),
		Instructions:      json.RawMessage(`"probe instructions"`),
		Moderation:        json.RawMessage(`{"type":"omni_moderation"}`),
		ParallelToolCalls: json.RawMessage("false"),
		Temperature:       &temperature,
		TopP:              &topP,
	}
	config := testProbeResponseConfig()

	serveChannelProbeResponse(c, relayconstant.RelayModeResponses, request, config, 0)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
	var response channelProbeResponsesResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, strings.HasPrefix(response.ID, "resp_"))
	assert.Len(t, response.ID, len("resp_")+64)
	assert.Equal(t, "response", response.Object)
	assert.Equal(t, "completed", response.Status)
	assert.Equal(t, "gpt-5.6-sol", response.Model)
	require.Len(t, response.Output, 1)
	assert.Equal(t, "assistant", response.Output[0].Role)
	require.Len(t, response.Output[0].Content, 1)
	assert.Equal(t, config.ResponseText, response.Output[0].Content[0].Text)
	require.NotNil(t, response.Usage)
	assert.Equal(t, 18, response.Usage.TotalTokens)
	assert.Equal(t, 1, response.Usage.InputTokensDetails.CacheWriteTokens)
	assert.Equal(t, 2, response.Usage.InputTokensDetails.CachedTokens)
	assert.Zero(t, response.Usage.OutputTokensDetails.ReasoningTokens)
	assert.Zero(t, response.ToolUsage.ImageGen.TotalTokens)
	assert.Zero(t, response.ToolUsage.WebSearch.NumRequests)
	assert.JSONEq(t, `"probe instructions"`, string(response.Instructions))
	assert.JSONEq(t, `{"type":"omni_moderation"}`, string(response.Moderation))
	assert.JSONEq(t, "false", string(response.ParallelToolCalls))
	assert.Equal(t, temperature, response.Temperature)
	assert.Equal(t, topP, response.TopP)
	assert.Zero(t, response.FrequencyPenalty)
	assert.Zero(t, response.PresencePenalty)

	var wire struct {
		Output    json.RawMessage `json:"output"`
		ToolUsage json.RawMessage `json:"tool_usage"`
		Usage     json.RawMessage `json:"usage"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &wire))
	assert.JSONEq(t, `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Configured probe response"}]}]`, string(wire.Output))
	assert.JSONEq(t, `{
		"image_gen": {
			"input_tokens": 0,
			"input_tokens_details": {"image_tokens": 0, "text_tokens": 0},
			"output_tokens": 0,
			"output_tokens_details": {"image_tokens": 0, "text_tokens": 0},
			"total_tokens": 0
		},
		"web_search": {"num_requests": 0}
	}`, string(wire.ToolUsage))
	assert.JSONEq(t, `{
		"input_tokens": 7,
		"input_tokens_details": {"cache_write_tokens": 1, "cached_tokens": 2},
		"output_tokens": 11,
		"output_tokens_details": {"reasoning_tokens": 0},
		"total_tokens": 18,
		"prompt_tokens_details": {"cache_write_tokens": 1, "cached_tokens": 2}
	}`, string(wire.Usage))
}

func TestServeChannelProbeResponsesStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	stream := true
	request := &dto.OpenAIResponsesRequest{
		Model:  "gpt-5.6-sol",
		Input:  json.RawMessage(`"hi"`),
		Stream: &stream,
	}
	config := testProbeResponseConfig()

	serveChannelProbeResponse(c, relayconstant.RelayModeResponses, request, config, 0)

	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	body := recorder.Body.String()
	expectedTypes := []string{
		"response.created",
		"response.in_progress",
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.delta",
		"response.output_text.done",
		"response.content_part.done",
		"response.output_item.done",
		"response.completed",
	}
	eventTypes := make([]string, 0, len(expectedTypes))
	events := make([]channelProbeResponsesEvent, 0, len(expectedTypes))
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if eventType, ok := strings.CutPrefix(line, "event: "); ok {
			eventTypes = append(eventTypes, eventType)
			continue
		}
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			var event channelProbeResponsesEvent
			require.NoError(t, common.Unmarshal([]byte(data), &event))
			events = append(events, event)
		}
	}

	require.Equal(t, expectedTypes, eventTypes)
	require.Len(t, events, len(expectedTypes))
	for index, event := range events {
		assert.Equal(t, expectedTypes[index], event.Type)
		assert.Equal(t, index, event.SequenceNumber)
	}
	require.NotNil(t, events[0].Response)
	assert.Equal(t, "in_progress", events[0].Response.Status)
	assert.Empty(t, events[0].Response.Output)
	assert.Nil(t, events[0].Response.Usage)
	require.NotNil(t, events[2].Item)
	assert.Equal(t, "in_progress", events[2].Item.Status)
	assert.Empty(t, events[2].Item.Content)
	require.NotNil(t, events[3].Part)
	assert.Equal(t, "output_text", events[3].Part.Type)
	assert.Empty(t, events[3].Part.Text)
	assert.Empty(t, events[3].Part.Annotations)
	assert.Empty(t, events[3].Part.Logprobs)
	assert.Equal(t, config.ResponseText, events[4].Delta)
	require.NotNil(t, events[4].Logprobs)
	assert.Empty(t, *events[4].Logprobs)
	assert.Equal(t, config.ResponseText, events[5].Text)
	require.NotNil(t, events[6].Part)
	assert.Equal(t, config.ResponseText, events[6].Part.Text)
	require.NotNil(t, events[7].Item)
	assert.Equal(t, "completed", events[7].Item.Status)
	require.NotNil(t, events[8].Response)
	assert.Equal(t, "completed", events[8].Response.Status)
	require.Len(t, events[8].Response.Output, 1)
	assert.Equal(t, config.ResponseText, events[8].Response.Output[0].Content[0].Text)
	require.NotNil(t, events[8].Response.Usage)
	assert.Equal(t, 7, events[8].Response.Usage.InputTokens)
	assert.Equal(t, 11, events[8].Response.Usage.OutputTokens)
	assert.Equal(t, 18, events[8].Response.Usage.TotalTokens)
	assert.Equal(t, 2, events[8].Response.Usage.InputTokensDetails.CachedTokens)
	assert.Equal(t, 1, events[8].Response.Usage.InputTokensDetails.CacheWriteTokens)
	assert.NotContains(t, body, "data: [DONE]")
}

func TestServeChannelProbeChatJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request := &dto.GeneralOpenAIRequest{
		Model:    "gpt-5.6-sol",
		Messages: []dto.Message{{Role: "user", Content: "hi"}},
	}
	config := testProbeResponseConfig()

	serveChannelProbeResponse(c, relayconstant.RelayModeChatCompletions, request, config, 0)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response channelProbeChatResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, strings.HasPrefix(response.ID, "chatcmpl-"))
	assert.Equal(t, "chat.completion", response.Object)
	assert.Equal(t, "gpt-5.6-sol", response.Model)
	require.Len(t, response.Choices, 1)
	assert.Equal(t, "assistant", response.Choices[0].Message.Role)
	assert.Equal(t, config.ResponseText, response.Choices[0].Message.Content)
	assert.Equal(t, "stop", response.Choices[0].FinishReason)
	assert.Equal(t, 7, response.Usage.PromptTokens)
	assert.Equal(t, 11, response.Usage.CompletionTokens)
	assert.Equal(t, 18, response.Usage.TotalTokens)
	assert.Equal(t, 2, response.Usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 1, response.Usage.PromptTokensDetails.CacheWriteTokens)
}

func TestServeChannelProbeChatStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	stream := true
	request := &dto.GeneralOpenAIRequest{
		Model:         "gpt-5.6-sol",
		Messages:      []dto.Message{{Role: "user", Content: "hi"}},
		Stream:        &stream,
		StreamOptions: &dto.StreamOptions{IncludeUsage: true},
	}
	config := testProbeResponseConfig()

	serveChannelProbeResponse(c, relayconstant.RelayModeChatCompletions, request, config, 0)

	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	body := recorder.Body.String()
	assert.Contains(t, body, `"object":"chat.completion.chunk"`)
	assert.Contains(t, body, config.ResponseText)
	assert.Contains(t, body, `"finish_reason":"stop"`)
	assert.Contains(t, body, `"usage":{`)
	assert.Contains(t, body, `"prompt_tokens":7`)
	assert.Contains(t, body, `"completion_tokens":11`)
	assert.Contains(t, body, `"total_tokens":18`)
	assert.Contains(t, body, `"cached_tokens":2`)
	assert.Contains(t, body, `"cache_write_tokens":1`)
	assert.Contains(t, body, "data: [DONE]")
}

func TestServeChannelProbeResponseStopsWhenClientCancels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestContext)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`"hi"`),
	}

	serveChannelProbeResponse(c, relayconstant.RelayModeResponses, request, DefaultResponseConfig(), time.Hour)

	assert.Empty(t, recorder.Body.String())
}
