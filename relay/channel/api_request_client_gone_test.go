package channel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoRequestCancelsUpstreamWithClientRequest(t *testing.T) {
	service.InitHttpClient()

	upstreamStarted := make(chan struct{})
	upstreamCanceled := make(chan struct{})
	releaseUpstream := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(upstreamStarted)
		select {
		case <-request.Context().Done():
			close(upstreamCanceled)
		case <-releaseUpstream:
		}
	}))
	t.Cleanup(func() {
		close(releaseUpstream)
		server.Close()
	})

	clientContext, cancelClient := context.WithCancel(context.Background())
	t.Cleanup(cancelClient)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(clientContext)
	upstreamRequest, err := http.NewRequest(http.MethodPost, server.URL, nil)
	require.NoError(t, err)

	requestDone := make(chan error, 1)
	go func() {
		_, requestErr := DoRequest(c, upstreamRequest, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
		requestDone <- requestErr
	}()

	testContext, cancelTest := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancelTest)
	select {
	case <-upstreamStarted:
	case <-testContext.Done():
		require.FailNow(t, "upstream request did not start")
	}

	cancelClient()
	select {
	case <-upstreamCanceled:
	case <-testContext.Done():
		require.FailNow(t, "upstream request was not canceled")
	}
	select {
	case requestErr := <-requestDone:
		require.Error(t, requestErr)
		var apiErr *types.NewAPIError
		require.ErrorAs(t, requestErr, &apiErr)
		assert.ErrorIs(t, requestErr, context.Canceled)
		assert.True(t, types.IsClientGoneError(apiErr))
		assert.Equal(t, types.StatusClientClosedRequest, apiErr.StatusCode)
		assert.True(t, types.IsSkipRetryError(apiErr))
		assert.False(t, types.IsRecordErrorLog(apiErr))
	case <-testContext.Done():
		require.FailNow(t, "relay request did not return after cancellation")
	}
}

func TestDoRequestKeepsTransportFailureRetryable(t *testing.T) {
	service.InitHttpClient()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	serverURL := server.URL
	server.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	upstreamRequest, err := http.NewRequest(http.MethodPost, serverURL, nil)
	require.NoError(t, err)

	_, requestErr := DoRequest(c, upstreamRequest, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	require.Error(t, requestErr)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, requestErr, &apiErr)
	assert.False(t, types.IsClientGoneError(apiErr))
	assert.Equal(t, types.ErrorCodeDoRequestFailed, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	assert.False(t, types.IsSkipRetryError(apiErr))
}
