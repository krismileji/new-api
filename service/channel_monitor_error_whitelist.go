package service

import relaytypes "github.com/QuantumNous/new-api/relaykit/types"

// ShouldExcludeErrorFromSmartScheduling uses the error-message whitelist to
// exclude failures from scheduling samples and protection without changing
// their outcome in monitoring statistics.
func ShouldExcludeErrorFromSmartScheduling(err *relaytypes.NewAPIError) bool {
	return err != nil && ShouldBypassErrorMessageHandling(string(err.GetErrorCode()), err.StatusCode)
}
