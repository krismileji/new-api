package openai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newResponsesTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c, w
}

func TestOaiResponsesHandlerNormalizesCapacityErrorStatus(t *testing.T) {
	c, w := newResponsesTestContext()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"status":"failed","error":{"type":"server_error","code":"server_is_overloaded","message":"Selected model is at capacity. Please try a different model."}}`)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}

	usage, apiErr := OaiResponsesHandler(c, &relaycommon.RelayInfo{}, resp)
	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	assert.True(t, types.IsModelCapacityError(apiErr))
	assert.Empty(t, w.Body.String())
}

func TestOaiResponsesStreamHandlerSuppressesCapacityFailure(t *testing.T) {
	c, w := newResponsesTestContext()
	info := &relaycommon.RelayInfo{DisablePing: true}
	body := strings.Join([]string{
		`data: {"type":"response.failed","response":{"status":"failed","error":{"type":"server_error","code":"server_is_overloaded","message":"Selected model is at capacity. Please try a different model."}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)
	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	assert.True(t, types.IsModelCapacityError(apiErr))
	assert.NotContains(t, w.Body.String(), "response.failed")
}

func TestOaiResponsesStreamHandlerForwardsOrdinaryFailure(t *testing.T) {
	c, w := newResponsesTestContext()
	info := &relaycommon.RelayInfo{DisablePing: true}
	body := strings.Join([]string{
		`data: {"type":"response.failed","response":{"status":"failed","error":{"type":"server_error","code":"invalid_prompt","message":"invalid prompt"}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)
	require.NotNil(t, usage)
	assert.Nil(t, apiErr)
	assert.Contains(t, w.Body.String(), "response.failed")
}
