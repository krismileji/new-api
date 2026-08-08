package service

import (
	"errors"
	"fmt"
	"net/http"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// ReserveBilling raises an existing pre-consume reservation before another
// upstream attempt and preserves structured quota errors from the funding
// source.
func ReserveBilling(relayInfo *relaycommon.RelayInfo, targetQuota int) *types.NewAPIError {
	if relayInfo == nil || relayInfo.Billing == nil {
		return types.NewError(
			errors.New("billing session is nil"),
			types.ErrorCodeUpdateDataError,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if relayInfo.QuotaClamp != nil {
		return types.NewErrorWithStatusCode(
			relayInfo.QuotaClamp,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if targetQuota < 0 {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("reserve quota cannot be negative: %d", targetQuota),
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	if err := relayInfo.Billing.Reserve(targetQuota); err != nil {
		var apiErr *types.NewAPIError
		if errors.As(err, &apiErr) {
			return apiErr
		}
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	relayInfo.FinalPreConsumedQuota = relayInfo.Billing.GetPreConsumedQuota()
	return nil
}
