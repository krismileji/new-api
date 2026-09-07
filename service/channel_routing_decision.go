package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const channelRoutingDecisionKey = "channel_routing_decision"
const channelRoutingAttemptsKey = "channel_routing_attempts"

func observeChannelRouting(c *gin.Context, retry bool) func(model.ChannelRoutingDecision) {
	return func(decision model.ChannelRoutingDecision) {
		decision.Retry = retry
		if retry {
			decision.Source = "retry"
		}
		c.Set(channelRoutingDecisionKey, decision)
	}
}

func MarkChannelRoutingConcurrencyFallback(c *gin.Context) {
	if value, exists := c.Get(channelRoutingDecisionKey); exists {
		if decision, ok := value.(model.ChannelRoutingDecision); ok {
			decision.Source = "concurrency_fallback"
			c.Set(channelRoutingDecisionKey, decision)
		}
	}
}

func channelRoutingDecisionForAttempt(c *gin.Context, channelID int) *model.ChannelRoutingDecision {
	if c == nil || channelID <= 0 {
		return nil
	}
	var decision model.ChannelRoutingDecision
	if value, exists := c.Get(channelRoutingDecisionKey); exists {
		decision, _ = value.(model.ChannelRoutingDecision)
	}
	if decision.ChannelID != channelID {
		decision = model.ChannelRoutingDecision{
			Source: "other", ChannelID: channelID, CandidateChannelID: channelID,
			Group: channelMonitorUsingGroup(c), Model: common.GetContextKeyString(c, constant.ContextKeyOriginalModel),
		}
	}
	_, specific := c.Get("specific_channel_id")
	if pin, found, _ := GetChannelConstraints(c).ResolvedPin(); specific || found && pin.ChannelId == channelID {
		decision.Source, decision.CandidateChannelID = "specific_channel", channelID
		decision.LogicalChannelID, decision.LogicalRevision = 0, 0
		decision.SnapshotRevision, decision.SnapshotGeneratedAt = 0, 0
	}
	return &decision
}

func beginChannelRoutingAttempt(c *gin.Context) {
	if c == nil {
		return
	}
	decision := channelRoutingDecisionForAttempt(c, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
	if decision == nil {
		return
	}
	var attempts []model.ChannelRoutingDecision
	if value, exists := c.Get(channelRoutingAttemptsKey); exists {
		attempts, _ = value.([]model.ChannelRoutingDecision)
	}
	if len(attempts) > 0 && decision.AttemptNumber > 0 && decision.Source != "specific_channel" {
		decision.Source = "retry"
	}
	decision.AttemptNumber = len(attempts) + 1
	decision.Retry = decision.Retry || len(attempts) > 0
	c.Set(channelRoutingDecisionKey, *decision)
	c.Set(channelRoutingAttemptsKey, append(attempts, *decision))
}

func appendChannelRoutingAdminInfo(c *gin.Context, adminInfo map[string]interface{}) {
	if c == nil || adminInfo == nil {
		return
	}
	if attempts, exists := c.Get(channelRoutingAttemptsKey); exists {
		adminInfo["channel_routing"] = attempts
		return
	}
	if decision := channelRoutingDecisionForAttempt(c, common.GetContextKeyInt(c, constant.ContextKeyChannelId)); decision != nil {
		adminInfo["channel_routing"] = []model.ChannelRoutingDecision{*decision}
	}
}
