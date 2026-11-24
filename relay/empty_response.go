package relay

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func deferEmptyResponseSettlement(c *gin.Context, info *relaycommon.RelayInfo, usage any, settle func(*gin.Context, *relaycommon.RelayInfo, *dto.Usage)) {
	complete := info.CompleteEmptyResponse
	if complete == nil {
		return
	}
	info.CompleteEmptyResponse = func() *types.NewAPIError {
		if err := complete(); err != nil {
			service.ResetStatusCode(err, c.GetString("status_code_mapping"))
			return err
		}
		settle(c, info, usage.(*dto.Usage))
		return nil
	}
}
