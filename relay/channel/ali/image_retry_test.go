package ali

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateTaskRejectsInvalidBaseURLBeforeDispatch(t *testing.T) {
	_, err, body := updateTask(context.Background(), &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}, "task-1")
	require.NotNil(t, err)
	require.Nil(t, body)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, types.ErrorCodeChannelInvalidBaseURL, apiErr.GetErrorCode())
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
}

func TestUpdateTaskPreservesPollingStatusCode(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	_, err, body := updateTask(context.Background(), &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL},
	}, "task-1")

	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, types.ErrorCodeBadResponseStatusCode, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.NotEmpty(t, body)
}
