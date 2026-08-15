package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func emitChannelModelDetectionMonitorEvent(costEvent model.ChannelModelDetectionCostEvent) {
	if costEvent.ChannelId <= 0 || strings.TrimSpace(costEvent.CostEventId) == "" {
		return
	}
	outcome := model.ChannelMonitorEventOutcomeUnresolved
	occurredAt := costEvent.UpdatedAt
	if costEvent.SettlementStatus == model.ChannelModelDetectionSettlementSettled {
		outcome = model.ChannelMonitorEventOutcomeSuccess
		occurredAt = costEvent.SettledAt
	} else if costEvent.SettlementStatus != model.ChannelModelDetectionSettlementUnresolved {
		return
	}
	if occurredAt <= 0 {
		occurredAt = common.GetTimestamp()
	}
	event := model.NewChannelMonitorEvent(
		costEvent.ChannelId,
		model.ChannelMonitorEventSourceModelDetection,
		outcome,
		occurredAt,
	)
	event.EventId = "model-detection:" + costEvent.CostEventId + ":" + costEvent.SettlementStatus
	event.ModelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(costEvent.RequestModel))
	event.RequestId = strings.TrimSpace(costEvent.RequestId)
	event.IsFinalAttempt = true
	event.RequestDispatched = true
	if other, err := common.Marshal(map[string]string{"cost_event_id": costEvent.CostEventId}); err == nil {
		event.OtherJson = string(other)
	}
	promptTokens := costEvent.InputTokens
	completionTokens := costEvent.OutputTokens
	inputTokens := costEvent.InputTokens
	event.PromptTokens = &promptTokens
	event.CompletionTokens = &completionTokens
	event.InputTokens = &inputTokens
	if costEvent.SettlementStatus == model.ChannelModelDetectionSettlementSettled {
		event.CostStatus = model.ChannelMonitorEventCostSettled
		if costEvent.SettledCostNanoCNY != nil {
			event.SettledCostNanoCNY = *costEvent.SettledCostNanoCNY
		}
	} else {
		event.CostStatus = model.ChannelMonitorEventCostUnresolved
		if costEvent.EstimatedCostNanoCNY != nil {
			event.UnresolvedCostNanoCNY = *costEvent.EstimatedCostNanoCNY
		}
	}
	EmitChannelMonitorEvent(event)
}
