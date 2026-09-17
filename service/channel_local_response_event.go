package service

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const ChannelLocalResponseContextKey = "channel_local_response"

func IsChannelBusinessRequest(c *gin.Context, info *relaycommon.RelayInfo) bool {
	return info != nil && !info.IsChannelTest && channelMonitorEventSource(c) == model.ChannelMonitorEventSourceBusiness
}

func EmitChannelLocalResponseEvent(c *gin.Context, info *relaycommon.RelayInfo, channelID int, policy model.ChannelProbePolicy, inputTokens int) {
	event := model.NewChannelMonitorEvent(channelID, model.ChannelMonitorEventSourceLocalResponse, model.ChannelMonitorEventOutcomeSuccess, time.Now().Unix())
	event.RequestId = channelMonitorRequestId(c, info.RequestId)
	event.ModelName = info.OriginModelName
	event.GroupName = channelMonitorUsingGroup(c)
	event.UserId = info.UserId
	event.APIKeyId = info.TokenId
	if info.UserId > 0 {
		event.UserAttribution = model.ChannelMonitorEventUserAttributionRequest
	}
	event.IsFinalAttempt = true
	event.InputTokens = common.GetPointer(int64(inputTokens))
	other, _ := common.Marshal(map[string]any{"probe_policy_revision": policy.ProbePolicyRevision, "small_input_threshold_tokens": policy.SmallInputThresholdTokens})
	event.OtherJson = string(other)
	status, err := EnqueueChannelMonitorEvent(event)
	ObserveChannelMonitorEventPublishStatus(c, status, err)
}
