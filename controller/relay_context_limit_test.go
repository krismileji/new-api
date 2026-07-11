package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayRejectsOversizedResponsesInputBeforeChannelSelection(t *testing.T) {
	if !service.InputContextLimitEnabled() {
		t.Skip("ENABLE_272K_CONTEXT_LIMIT is disabled")
	}
	gin.SetMode(gin.TestMode)
	body, err := common.Marshal(map[string]any{
		"model": "gpt-5.5",
		"input": []any{
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_123",
				"output":  strings.Repeat("x ", service.MaxInputContextTokens+1024),
			},
		},
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "gpt-5.5")
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	defer common.CleanupBodyStorage(ctx)

	Relay(ctx, types.RelayFormatOpenAIResponses)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Empty(t, ctx.GetStringSlice("use_channel"))
	var response struct {
		Error types.OpenAIError `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, string(types.ErrorCodeInvalidRequest), response.Error.Code)
	assert.Contains(t, response.Error.Message, "exceeds the maximum allowed context length")
}
