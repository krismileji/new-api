package channel

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestValidateUpstreamURLRejectsMissingProtocol(t *testing.T) {
	apiErr := ValidateUpstreamURL("", false)
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeChannelInvalidBaseURL, apiErr.GetErrorCode())
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)

	apiErr = ValidateUpstreamURL("/v1/chat/completions", false)
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeChannelInvalidBaseURL, apiErr.GetErrorCode())
}

func TestValidateUpstreamURLChecksTransportScheme(t *testing.T) {
	require.Nil(t, ValidateUpstreamURL("https://upstream.example/v1/chat", false))
	require.Nil(t, ValidateUpstreamURL("wss://upstream.example/realtime", true))
	require.NotNil(t, ValidateUpstreamURL("wss://upstream.example/realtime", false))
	require.NotNil(t, ValidateUpstreamURL("https://upstream.example/realtime", true))
}
