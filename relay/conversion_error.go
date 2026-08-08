package relay

import (
	"errors"

	"github.com/QuantumNous/new-api/relaykit/types"
)

// newAPIErrorFromConversion preserves provider-generated API errors returned
// while converting a request. Auxiliary upstream calls can fail transiently
// during conversion and must retain their status and retry policy.
func newAPIErrorFromConversion(err error) *types.NewAPIError {
	var apiErr *types.NewAPIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
}
