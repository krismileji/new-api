package replicate

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadFileFromFormReusesResultAcrossRetryConversion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	var uploadCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/files" {
			http.NotFound(w, r)
			return
		}
		uploadCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"urls":{"get":%q}}`, serverURLForTest(r))
	}))
	defer server.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("same image bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelBaseUrl: server.URL,
		ChannelType:    constant.ChannelTypeReplicate,
		ApiKey:         "test-key",
	}}

	first, err := uploadFileFromForm(c, info, "image")
	require.NoError(t, err)
	second, err := uploadFileFromForm(c, info, "image")
	require.NoError(t, err)
	require.NotEmpty(t, first)
	assert.Equal(t, first, second)
	assert.Equal(t, int32(1), uploadCalls.Load())
}

func TestUploadFileFromFormRejectsInvalidBaseURLBeforeDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("image bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelBaseUrl: "not a url",
		ChannelType:    constant.ChannelTypeReplicate,
		ApiKey:         "test-key",
	}}

	_, err = uploadFileFromForm(c, info, "image")
	require.Error(t, err)
	require.Contains(t, err.Error(), "渠道上游地址")
}

func serverURLForTest(r *http.Request) string {
	return "https://files.example/" + r.URL.Path
}
