package controller

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

func channelProbePolicySkippedOutcome(err error) channelStatusProbeOutcome {
	code := "probe_policy_unavailable"
	if errors.Is(err, service.ErrChannelAutoProbeDisabled) {
		code = "auto_probe_disabled"
	}
	now := common.GetTimestamp()
	return channelStatusProbeOutcome{
		Result: model.ChannelStatusProbeResultSkipped, StartedAt: now, FinishedAt: now,
		ErrorCode: code, ErrorMessage: common.MaskSensitiveInfo(err.Error()),
	}
}

func channelProbePolicyOutcomeSkipped(outcome channelStatusProbeOutcome) bool {
	return outcome.Result == model.ChannelStatusProbeResultSkipped &&
		(outcome.ErrorCode == "auto_probe_disabled" || outcome.ErrorCode == "probe_policy_unavailable")
}

func channelHealthTestTrigger(ctx context.Context) context.Context {
	if service.ChannelProbeTrigger(ctx) == "" {
		return service.WithChannelProbeTrigger(ctx, model.ChannelStatusProbeTriggerScheduled)
	}
	return ctx
}
