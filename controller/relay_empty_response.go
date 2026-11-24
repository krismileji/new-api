package controller

import (
	"maps"
	"net/http"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/requestplugin"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// Keep the completed attempt available until another upstream attempt starts.
// Channel selection can exhaust its candidates before it dispatches a retry.
type relayEmptyResponse struct {
	info      relaycommon.RelayInfo
	context   *gin.Context
	headers   http.Header
	channelID int
}

func (response *relayEmptyResponse) finalize() {
	if response != nil {
		service.FinalizeChannelDailyCostAttempt(response.context, response.channelID, false)
	}
}

func (response *relayEmptyResponse) restore(c *gin.Context, info *relaycommon.RelayInfo) {
	*info = response.info
	c.Keys = response.context.Keys
	clear(c.Writer.Header())
	maps.Copy(c.Writer.Header(), response.headers)
}

func completeRelayEmptyResponse(c *gin.Context, info *relaycommon.RelayInfo, format types.RelayFormat, channelID int) *types.NewAPIError {
	complete := info.CompleteEmptyResponse
	info.CompleteEmptyResponse = nil
	finishReply := requestplugin.Reply(c, format, info)
	defer finishReply()
	apiErr := complete()
	service.FinalizeChannelDailyCostAttempt(c, channelID, false)
	if apiErr == nil {
		service.MarkRequestPolicySuccess(c, info.StreamStatus)
		return nil
	}
	return markAcceptedUpstreamResponseError(c, apiErr)
}
