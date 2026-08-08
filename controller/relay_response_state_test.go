package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	taskdto "github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useGlobalErrorMessageMapping(t *testing.T, mapping string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	wasNil := common.OptionMap == nil
	if wasNil {
		common.OptionMap = make(map[string]string)
	}
	original, existed := common.OptionMap[service.ErrorMessageMappingOptionKey]
	common.OptionMap[service.ErrorMessageMappingOptionKey] = mapping
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if wasNil {
			common.OptionMap = nil
		} else if existed {
			common.OptionMap[service.ErrorMessageMappingOptionKey] = original
		} else {
			delete(common.OptionMap, service.ErrorMessageMappingOptionKey)
		}
	})
}

func TestWriteRelayErrorResponseDoesNotAppendAfterResponseStarted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, err := c.Writer.Write([]byte("data: partial\n\n"))
	require.NoError(t, err)

	apiErr := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	writeRelayErrorResponse(c, nil, types.RelayFormatOpenAIResponses, apiErr)

	assert.True(t, relayResponseStarted(c))
	assert.Equal(t, "data: partial\n\n", recorder.Body.String())
}

func TestWriteRelayErrorResponseWritesBeforeResponseStarts(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	apiErr := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponse, http.StatusBadGateway)

	writeRelayErrorResponse(c, nil, types.RelayFormatOpenAI, apiErr)

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "upstream failed")
}

func TestWriteRelayErrorResponseUsesGlobalMessageMappingBeforeResponseStarts(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	useGlobalErrorMessageMapping(t, `{"bad_response":"上游暂时不可用"}`)
	apiErr := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponse, http.StatusBadGateway)

	writeRelayErrorResponse(c, nil, types.RelayFormatOpenAI, apiErr)

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "上游暂时不可用")
	assert.NotContains(t, recorder.Body.String(), "upstream failed")
}

func TestWriteRelayErrorResponseReplacesPendingEventStreamHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	helper.SetEventStreamHeaders(c)
	require.False(t, relayResponseStarted(c))

	apiErr := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	writeRelayErrorResponse(c, nil, types.RelayFormatOpenAI, apiErr)

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.Empty(t, recorder.Header().Get("Transfer-Encoding"))
	assert.False(t, c.GetBool(relaycommon.EventStreamHeadersSetContextKey))
}

func TestRelayAttemptResponseStartedAllowsRealtimeRetryBeforeUpstreamConnects(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	_, err := c.Writer.Write([]byte("websocket handshake already completed"))
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{}

	assert.True(t, relayResponseStarted(c))
	assert.False(t, relayAttemptResponseStarted(c, info, types.RelayFormatOpenAIRealtime))

	info.TargetWs = &websocket.Conn{}
	assert.True(t, relayAttemptResponseStarted(c, info, types.RelayFormatOpenAIRealtime))
}

func TestRespondTaskErrorDoesNotAppendAfterResponseStarted(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	_, err := c.Writer.Write([]byte(`{"id":"task_public"}`))
	require.NoError(t, err)

	respondTaskError(c, &taskdto.TaskError{StatusCode: http.StatusInternalServerError, Message: "late error"})

	assert.Equal(t, `{"id":"task_public"}`, recorder.Body.String())
}

func TestRespondTaskErrorUsesGlobalMessageMappingBeforeResponseStarts(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	useGlobalErrorMessageMapping(t, `{"fail_to_fetch_task":"任务暂时无法提交"}`)

	respondTaskError(c, &taskdto.TaskError{
		Code:       "fail_to_fetch_task",
		Message:    "upstream failed",
		StatusCode: http.StatusBadGateway,
	})

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "任务暂时无法提交")
	assert.NotContains(t, recorder.Body.String(), "upstream failed")
}

func TestMarkAcceptedUpstreamResponseErrorDisablesRetry(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(string(service.UpstreamResponseStatusContextKey), http.StatusOK)

	apiErr := types.NewError(errors.New("response body could not be decoded"), types.ErrorCodeBadResponseBody)
	got := markAcceptedUpstreamResponseError(c, apiErr)

	assert.Same(t, apiErr, got)
	assert.True(t, types.IsSkipRetryError(got))
}

func TestMarkAcceptedUpstreamResponseErrorKeepsTransportRetryable(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(string(service.UpstreamResponseStatusContextKey), http.StatusBadGateway)

	apiErr := types.NewError(errors.New("bad gateway"), types.ErrorCodeBadResponseStatusCode)
	markAcceptedUpstreamResponseError(c, apiErr)

	assert.False(t, types.IsSkipRetryError(apiErr))
}

func TestMarkAcceptedUpstreamResponseErrorKeepsStreamFirstResponseTransportRetryable(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(string(service.UpstreamResponseStatusContextKey), http.StatusOK)
	common.SetContextKey(c, service.UpstreamErrorDiagnosticContextKey, service.UpstreamErrorDiagnostic{
		Category: service.UpstreamErrorCategoryResponseTimeout,
	})

	apiErr := types.NewError(errors.New("等待上游流式首字超时"), types.ErrorCodeDoRequestFailed)
	markAcceptedUpstreamResponseError(c, apiErr)

	assert.False(t, types.IsSkipRetryError(apiErr))
}

func TestMarkAcceptedUpstreamResponseErrorKeepsCapacityFailureRetryable(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(string(service.UpstreamResponseStatusContextKey), http.StatusOK)

	apiErr := types.WithOpenAIError(types.OpenAIError{
		Type:    "server_error",
		Code:    "server_is_overloaded",
		Message: "Selected model is at capacity. Please try a different model.",
	}, http.StatusServiceUnavailable)
	markAcceptedUpstreamResponseError(c, apiErr)

	assert.True(t, types.IsModelCapacityError(apiErr))
	assert.False(t, types.IsSkipRetryError(apiErr))
}
