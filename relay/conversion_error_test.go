package relay

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAPIErrorFromConversionPreservesRetryableProviderError(t *testing.T) {
	providerErr := types.NewErrorWithStatusCode(
		errors.New("dify file upload failed with status 502"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadGateway,
	)

	got := newAPIErrorFromConversion(providerErr)

	require.Same(t, providerErr, got)
	assert.Equal(t, types.ErrorCodeBadResponseStatusCode, got.GetErrorCode())
	assert.Equal(t, http.StatusBadGateway, got.StatusCode)
	assert.False(t, types.IsSkipRetryError(got))
}

func TestNewAPIErrorFromConversionMarksLocalConversionFailureNonRetryable(t *testing.T) {
	got := newAPIErrorFromConversion(errors.New("invalid image payload"))

	require.NotNil(t, got)
	assert.Equal(t, types.ErrorCodeConvertRequestFailed, got.GetErrorCode())
	assert.Equal(t, http.StatusInternalServerError, got.StatusCode)
	assert.True(t, types.IsSkipRetryError(got))
}
