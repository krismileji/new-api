package coze

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCozeDoesNotRetryAfterChatCreation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	var createCalls atomic.Int32
	var retrieveCalls atomic.Int32
	var detailCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/chat":
			createCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"id":"chat-1","conversation_id":"conversation-1"}}`)
		case "/v3/chat/retrieve":
			retrieveCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"status":"completed","usage":{"token_count":3,"input_count":2,"output_count":1}}}`)
		case "/v3/chat/message/list":
			detailCalls.Add(1)
			http.Error(w, "detail unavailable", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("bot_id", "bot-1")
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: server.URL,
			ApiKey:         "test-key",
		},
	}

	_, err := (&Adaptor{}).DoRequest(c, info, bytes.NewReader([]byte(`{"prompt":"hello"}`)))
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.True(t, errors.As(err, &apiErr), "detail failure should preserve structured retry metadata")
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.Equal(t, int32(1), createCalls.Load())
	assert.Equal(t, int32(1), retrieveCalls.Load())
	assert.Equal(t, int32(1), detailCalls.Load())
}

func TestCozePreservesRetrieveStatusAfterChatCreation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	var createCalls atomic.Int32
	var retrieveCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/chat":
			createCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"id":"chat-1","conversation_id":"conversation-1"}}`)
		case "/v3/chat/retrieve":
			retrieveCalls.Add(1)
			http.Error(w, "rate limited", http.StatusTooManyRequests)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("bot_id", "bot-1")
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: server.URL,
			ApiKey:         "test-key",
		},
	}

	_, err := (&Adaptor{}).DoRequest(c, info, bytes.NewReader([]byte(`{"prompt":"hello"}`)))
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, types.ErrorCodeBadResponseStatusCode, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.Equal(t, int32(1), createCalls.Load())
	assert.Equal(t, int32(1), retrieveCalls.Load())
}

func TestCozePreservesCreateChatStatusCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/chat" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: server.URL,
			ApiKey:         "test-key",
		},
	}

	_, err := (&Adaptor{}).DoRequest(c, info, bytes.NewReader([]byte(`{"prompt":"hello"}`)))
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, types.ErrorCodeBadResponseStatusCode, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.False(t, types.IsSkipRetryError(apiErr))
}

func TestCozeRejectsNilResponseBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	_, err := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: http.StatusOK}, &relaycommon.RelayInfo{})
	require.Error(t, err)
	assert.Equal(t, types.ErrorCodeBadResponse, err.GetErrorCode())
}

func TestCozeStreamRejectsPrematureEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		"event: conversation.message.delta",
		`data: {"content":"partial"}`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := cozeChatStreamHandler(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "coze-test"}}, resp)

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.Contains(t, apiErr.Error(), "ended before completion")
	assert.Contains(t, recorder.Body.String(), "partial")
	assert.NotContains(t, recorder.Body.String(), "[DONE]")
}

func TestCozeStreamReturnsErrorEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		"event: error",
		`data: {"code":5000,"message":"generation failed"}`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := cozeChatStreamHandler(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}, resp)

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.Contains(t, apiErr.Error(), "generation failed")
	assert.NotContains(t, recorder.Body.String(), "[DONE]")
}

func TestCozeStreamCompletesOnlyAfterCompletedEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		"event: conversation.message.delta",
		`data: {"content":"complete"}`,
		"",
		"event: conversation.chat.completed",
		`data: {"usage":{"token_count":3,"input_count":2,"output_count":1}}`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := cozeChatStreamHandler(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "coze-test"}}, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 3, usage.TotalTokens)
	assert.Contains(t, recorder.Body.String(), "complete")
	assert.Contains(t, recorder.Body.String(), "[DONE]")
}
