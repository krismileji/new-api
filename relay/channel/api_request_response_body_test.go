package channel_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseHandlersClassifyCancellationAfterHeaders(t *testing.T) {
	service.InitHttpClient()

	tests := []struct {
		name    string
		path    string
		handler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{"chat completions", "/v1/chat/completions", openai.OpenaiHandler},
		{"responses", "/v1/responses", openai.OaiResponsesHandler},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			releaseUpstream := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, _ = io.Copy(io.Discard, request.Body)
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(http.StatusOK)
				writer.(http.Flusher).Flush()
				select {
				case <-request.Context().Done():
				case <-releaseUpstream:
				}
			}))
			t.Cleanup(func() {
				close(releaseUpstream)
				server.Close()
			})

			clientContext, cancelClient := context.WithTimeout(t.Context(), 5*time.Second)
			t.Cleanup(cancelClient)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, nil).WithContext(clientContext)
			upstreamRequest, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("{}"))
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

			response, err := channel.DoRequest(c, upstreamRequest, info)
			require.NoError(t, err)
			require.NotNil(t, response)
			t.Cleanup(func() { _ = response.Body.Close() })
			require.Equal(t, http.StatusOK, response.StatusCode)

			cancelClient()
			usage, apiErr := tt.handler(c, info, response)
			require.NotNil(t, apiErr)
			assert.Nil(t, usage)
			assert.ErrorIs(t, apiErr, context.Canceled)
			assert.Equal(t, types.ErrorCodeClientGone, apiErr.GetErrorCode())
			assert.Equal(t, types.StatusClientClosedRequest, apiErr.StatusCode)
			assert.True(t, types.IsSkipRetryError(apiErr))
			assert.False(t, types.IsRecordErrorLog(apiErr))
		})
	}
}
