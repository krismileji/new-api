package controller

import (
	"errors"
	"net/http"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// prepareRelayBillingForSelectedGroup refreshes group-dependent pricing and
// raises the reservation before the selected channel receives the request.
func prepareRelayBillingForSelectedGroup(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta) *types.NewAPIError {
	if info == nil {
		return types.NewError(
			errors.New("relay info is nil"),
			types.ErrorCodeInvalidRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if info.TieredBillingSnapshot != nil {
		return service.PrepareTieredBillingForSelectedGroup(c, info)
	}

	priceData, err := helper.ModelPriceHelper(c, info, promptTokens, meta)
	if err != nil {
		return types.NewErrorWithStatusCode(
			err,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if priceData.FreeModel {
		return nil
	}
	if info.Billing == nil {
		return service.PreConsumeBilling(c, priceData.QuotaToPreConsume, info)
	}
	return service.ReserveBilling(info, priceData.QuotaToPreConsume)
}
