package channelprobe

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChannelSmallInputResponseProtocolContract(t *testing.T) {
	for _, tc := range []struct {
		name, body, path, finishPath, finish, streamEnd string
		request                                         dto.Request
	}{
		{"chat", `{"model":"gpt-4o","stream_options":{"include_usage":true}}`, "/v1/chat/completions", "choices.0.finish_reason", "length", "data: [DONE]", &dto.GeneralOpenAIRequest{}},
		{"responses", `{"model":"gpt-4o"}`, "/v1/responses", "status", "incomplete", "event: response.incomplete", &dto.OpenAIResponsesRequest{}},
		{"claude", `{"model":"claude-sonnet-4","max_tokens":2}`, "/v1/messages", "stop_reason", "max_tokens", "event: message_stop", &dto.ClaudeRequest{}},
		{"gemini", `{}`, "/v1beta/models/gemini:generateContent", "candidates.0.finishReason", "MAX_TOKENS", "data:", &dto.GeminiChatRequest{}},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: " JSON", true: " SSE"}[stream], func(t *testing.T) {
				body := tc.body
				path := tc.path
				if stream {
					body = strings.TrimSuffix(body, "}") + `,"stream":true}`
					if tc.name == "gemini" {
						path = "/v1beta/models/gemini:streamGenerateContent?alt=sse"
						body = `{}`
					}
				}
				require.NoError(t, common.UnmarshalJsonStr(body, tc.request))
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest("POST", path, strings.NewReader(body))
				WriteLocalTextResponse(c, tc.request, "client-model", ResponseConfig{ResponseText: "中文\nhello", InputTokens: 25, OutputTokens: 2, Truncated: true})
				assert.Equal(t, 200, recorder.Code)
				assert.Equal(t, "local_response", recorder.Header().Get("X-New-Api-Response-Source"))
				if stream {
					assert.Contains(t, recorder.Body.String(), tc.streamEnd)
					assert.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
				} else {
					require.True(t, gjson.Valid(recorder.Body.String()))
					assert.Equal(t, tc.finish, gjson.Get(recorder.Body.String(), tc.finishPath).String())
				}
			})
		}
	}
}
