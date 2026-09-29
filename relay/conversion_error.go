package relay

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/relaykit/types"
)

// newAPIErrorFromConversion preserves provider-generated API errors returned
// while converting a request. Auxiliary upstream calls can fail transiently
// during conversion and must retain their status and retry policy. Local
// conversion rejections keep the Responses bridge's bad-request response.
func newAPIErrorFromConversion(err error) *types.NewAPIError {
	var apiErr *types.NewAPIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}
