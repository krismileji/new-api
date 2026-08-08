package relay

import (
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func prepareTaskBilling(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info == nil || info.PriceData.FreeModel {
		return nil
	}
	info.ForcePreConsume = true
	if info.Billing == nil {
		return service.TaskErrorFromBillingAPIError(service.PreConsumeBilling(c, info.PriceData.Quota, info))
	}
	return service.TaskErrorFromBillingAPIError(service.ReserveBilling(info, info.PriceData.Quota))
}
