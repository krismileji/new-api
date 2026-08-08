package dify

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIRequestReturnsDifyUploadFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/files/upload" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "upload unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelBaseUrl: server.URL,
		ApiKey:         "test-key",
	}}
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{dto.MediaContent{
				Type: dto.ContentTypeImageURL,
				ImageUrl: &dto.MessageImageUrl{
					Url:      "data:image/png;base64,aGVsbG8=",
					MimeType: "image/png",
				},
			}},
		}},
	}

	_, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, request)
	require.Error(t, err)
	require.Contains(t, fmt.Sprint(err), "dify upload failed with status 502")
}
