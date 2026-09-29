package relay

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	openaichannel "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextRequestViaResponsesReasoningClassification(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	tests := []struct {
		name       string
		request    dto.Request
		format     types.RelayFormat
		wantError  bool
		wantPlain  bool
		wantLoss   bool
		wantEffort string
		wantCode   string
	}{
		{
			name: "invalid chat reasoning is a client error",
			request: &dto.GeneralOpenAIRequest{Model: "gpt-5", ReasoningEffort: "ultra",
				Messages: []dto.Message{{Role: "user", Content: "hello"}}},
			format: types.RelayFormatOpenAI, wantError: true,
		},
		{
			name: "unsupported multiple choices is a client error",
			request: &dto.GeneralOpenAIRequest{Model: "gpt-5", N: common.GetPointer(2),
				Messages: []dto.Message{{Role: "user", Content: "hello"}}},
			format: types.RelayFormatOpenAI, wantError: true, wantPlain: true,
		},
		{
			name: "invalid Claude reasoning is a client error",
			request: &dto.ClaudeRequest{Model: "gpt-5", OutputConfig: []byte(`{"effort":"ultra"}`),
				Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}}},
			format: types.RelayFormatClaude, wantError: true,
		},
		{
			name: "safe tool loss is a client error with admin diagnostics",
			request: &dto.ClaudeRequest{Model: "gpt-5",
				Tools:    []any{map[string]any{"type": "web_fetch_20250910", "name": "web_fetch"}},
				Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}}},
			format: types.RelayFormatClaude, wantError: true, wantLoss: true, wantCode: "unsupported_hosted_tool",
		},
		{
			name: "chat conflict proceeds with the selected effort and diagnostics",
			request: &dto.GeneralOpenAIRequest{Model: "gpt-5", ReasoningEffort: "low", Reasoning: []byte(`{"effort":"high"}`),
				Messages: []dto.Message{{Role: "user", Content: "hello"}}},
			format: types.RelayFormatOpenAI, wantEffort: "low", wantCode: "explicit_fields_conflict",
		},
		{
			name: "Claude thinking coercion proceeds with diagnostics",
			request: &dto.ClaudeRequest{Model: "gpt-5", Thinking: &dto.Thinking{Type: "auto"},
				Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}}},
			format: types.RelayFormatClaude, wantEffort: "high", wantCode: "claude_thinking_type_coerced",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			upstreamRequests := make(chan *dto.OpenAIResponsesRequest, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var request dto.OpenAIResponsesRequest
				if err := common.DecodeJson(r.Body, &request); err != nil {
					t.Errorf("decode upstream request: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				assert.Equal(t, "/v1/responses", r.URL.Path)
				upstreamRequests <- &request
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"resp_audit","object":"response","status":"completed","model":"gpt-5","output":[{"type":"message","id":"msg_audit","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`))
			}))
			defer upstream.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeChatCompletions, RelayFormat: tt.format,
				OriginModelName: "gpt-5", Request: tt.request, RequestConversionChain: []types.RelayFormat{tt.format},
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType: constant.ChannelTypeOpenAI, ChannelBaseUrl: upstream.URL,
					ApiKey: "test-key", UpstreamModelName: "gpt-5",
					ChannelOtherSettings: dto.ChannelOtherSettings{ToolLossPolicy: string(types.ConversionLossPolicySafe)},
				},
			}
			adaptor := &openaichannel.Adaptor{}
			adaptor.Init(info)
			usage, apiErr := textRequestViaResponses(c, info, adaptor, tt.request)
			if tt.wantError {
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
				assert.True(t, types.IsSkipRetryError(apiErr))
				assert.Nil(t, usage)
				assert.Zero(t, calls.Load())
				if tt.wantLoss {
					var loss *types.ConversionLossError
					require.ErrorAs(t, apiErr, &loss)
				} else if !tt.wantPlain {
					assert.True(t, reasoning.IsClientError(apiErr))
				}
			} else {
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				assert.Equal(t, 5, usage.TotalTokens)
				require.Equal(t, int32(1), calls.Load())
				require.Len(t, upstreamRequests, 1)
				request := <-upstreamRequests
				require.NotNil(t, request.Reasoning)
				assert.Equal(t, tt.wantEffort, request.Reasoning.Effort)
			}
			if tt.wantCode != "" {
				diagnostics := info.ConversionDiagnostics()
				assert.True(t, hasHostDiagnosticCode(diagnostics, tt.wantCode))
				other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 0, 0, 1).Snapshot()
				adminInfo, ok := other["admin_info"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, diagnostics, adminInfo["conversion_diagnostics"])
				assert.NotContains(t, other, "conversion_diagnostics")
			}
		})
	}
}
