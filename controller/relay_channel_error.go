package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

func newRelayChannelError(c *gin.Context, channel *model.Channel) *types.ChannelError {
	if channel == nil {
		return types.NewChannelError(0, 0, "", false, "", false)
	}
	isMultiKey := channel.ChannelInfo.IsMultiKey
	if _, exists := common.GetContextKey(c, constant.ContextKeyChannelIsMultiKey); exists {
		isMultiKey = common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey)
	}
	return types.NewChannelError(
		channel.Id,
		channel.Type,
		channel.Name,
		isMultiKey,
		common.GetContextKeyString(c, constant.ContextKeyChannelKey),
		channel.GetAutoBan(),
	)
}
