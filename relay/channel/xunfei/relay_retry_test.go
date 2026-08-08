package xunfei

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestXunfeiMakeRequestHandlesHandshakeErrorWithoutNilResponsePanic(t *testing.T) {
	_, _, _, _, err := xunfeiMakeRequest(context.Background(), dto.GeneralOpenAIRequest{}, "general", "://invalid", "app-id")
	require.Error(t, err)
}

func TestXunfeiMakeRequestPreservesHandshakeStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	requestURL := "ws" + strings.TrimPrefix(server.URL, "http")
	_, _, _, _, err := xunfeiMakeRequest(context.Background(), dto.GeneralOpenAIRequest{}, "general", requestURL, "app-id")
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, types.ErrorCodeBadResponseStatusCode, apiErr.GetErrorCode())
	require.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
}

func TestXunfeiHandlersTreatCanceledDialAsClientGone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()

	for _, handler := range []func(*gin.Context, dto.GeneralOpenAIRequest, string, string, string) (*dto.Usage, *types.NewAPIError){
		xunfeiHandler,
		xunfeiStreamHandler,
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestContext)

		usage, err := handler(c, dto.GeneralOpenAIRequest{Model: "spark-v1.1"}, "app-id", "secret", "key")
		require.Nil(t, usage)
		require.Error(t, err)
		require.Equal(t, types.ErrorCodeClientGone, err.GetErrorCode())
		require.True(t, types.IsSkipRetryError(err))
	}
}
