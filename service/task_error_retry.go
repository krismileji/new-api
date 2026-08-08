package service

import (
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func TaskErrorFromBillingAPIError(apiErr *types.NewAPIError) *taskdto.TaskError {
	if apiErr == nil {
		return nil
	}
	return &taskdto.TaskError{
		Code:       string(apiErr.GetErrorCode()),
		Message:    apiErr.Error(),
		StatusCode: apiErr.StatusCode,
		LocalError: true,
		Error:      apiErr,
	}
}
