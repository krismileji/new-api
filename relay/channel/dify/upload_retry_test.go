package dify

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadDifyFilePreservesRetryableUpstreamStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	media := dto.MediaContent{
		Type: dto.ContentTypeImageURL,
		ImageUrl: &dto.MessageImageUrl{
			Url:      "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("image")),
			MimeType: "image/png",
		},
	}

	_, err := uploadDifyFile(c, info, "user-1", media)
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, types.ErrorCodeBadResponseStatusCode, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.False(t, types.IsSkipRetryError(apiErr))
}

func TestDifyResponseRejectsNilResponseBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	_, err := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: http.StatusOK}, &relaycommon.RelayInfo{})
	require.Error(t, err)
	assert.Equal(t, types.ErrorCodeBadResponse, err.GetErrorCode())
}

func TestDifyStreamRejectsPrematureEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 1
	t.Cleanup(func() {
		constant.StreamingTimeout = originalStreamingTimeout
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{}

	_, err := difyStreamHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
	})
	require.Error(t, err)
	assert.Equal(t, types.ErrorCodeBadResponseBody, err.GetErrorCode())
}
