package controller

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError, info *relaycommon.RelayInfo) {
	processChannelErrorWithTiming(c, channelError, err, false, false, nil, false, info)
}
