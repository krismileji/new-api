package channel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type websocketStatusAdaptor struct {
	Adaptor
	requestURL string
}

func (a *websocketStatusAdaptor) GetRequestURL(*relaycommon.RelayInfo) (string, error) {
	return a.requestURL, nil
}

func (a *websocketStatusAdaptor) SetupRequestHeader(*gin.Context, *http.Header, *relaycommon.RelayInfo) error {
	return nil
}

func TestDoWssRequestPreservesHandshakeStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	requestURL := "ws" + strings.TrimPrefix(server.URL, "http")

	conn, err := DoWssRequest(
		&websocketStatusAdaptor{requestURL: requestURL},
		c,
		&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}},
		nil,
	)
	require.Nil(t, conn)
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, types.ErrorCodeBadResponseStatusCode, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
	assert.Equal(t, http.StatusTooManyRequests, c.GetInt(string(service.UpstreamResponseStatusContextKey)))
}
