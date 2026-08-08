package volcengine

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleTTSWebSocketResponseRejectsInvalidURLBeforeDial(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "app-id|token"},
	}

	_, err := handleTTSWebSocketResponse(c, "", VolcengineTTSRequest{}, info, "mp3")
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeChannelInvalidBaseURL, err.GetErrorCode())
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
}

func TestHandleTTSWebSocketResponseTreatsCanceledDialAsClientGone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil).WithContext(requestContext)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "app-id|token"},
	}

	_, err := handleTTSWebSocketResponse(c, "ws://127.0.0.1:1", VolcengineTTSRequest{}, info, "mp3")
	require.Error(t, err)
	assert.Equal(t, types.ErrorCodeClientGone, err.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(err))
}

func TestHandleTTSWebSocketResponsePreservesHandshakeStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "app-id|token"},
	}
	requestURL := "ws" + strings.TrimPrefix(server.URL, "http")

	_, apiErr := handleTTSWebSocketResponse(c, requestURL, VolcengineTTSRequest{}, info, "mp3")
	require.Error(t, apiErr)
	assert.Equal(t, types.ErrorCodeBadResponseStatusCode, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
}

func TestHandleTTSResponsePreservesUpstreamStatusCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(strings.NewReader("upstream unavailable")),
	}

	_, err := handleTTSResponse(c, resp, &relaycommon.RelayInfo{}, "mp3")
	require.Error(t, err)
	assert.Equal(t, types.ErrorCodeBadResponseStatusCode, err.GetErrorCode())
	assert.Equal(t, http.StatusBadGateway, err.StatusCode)
}

func TestHandleTTSResponseAcceptsSuccessfulPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	audio := base64.StdEncoding.EncodeToString([]byte("audio"))
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"code":3000,"message":"","data":"` + audio + `"}`)),
	}

	usage, err := handleTTSResponse(c, resp, &relaycommon.RelayInfo{}, "mp3")
	assert.Nil(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, "audio", recorder.Body.String())
}
