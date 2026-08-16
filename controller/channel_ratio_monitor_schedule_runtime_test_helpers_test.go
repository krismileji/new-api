package controller

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func projectChannelSmartScheduleRuntimeEventForTest(
	channelId int,
	modelName string,
	timestamp int64,
	failure bool,
) {
	outcome := model.ChannelMonitorEventOutcomeSuccess
	if failure {
		outcome = model.ChannelMonitorEventOutcomeFailure
	}
	event := model.NewChannelMonitorEvent(channelId, model.ChannelMonitorEventSourceBusiness, outcome, timestamp)
	event.ModelName = modelName
	event.RequestDispatched = true
	event.IsFinalAttempt = true
	event.SchedulingEligible = true
	event.RuntimeProtectionEligible = failure
	if failure {
		statusCode := http.StatusInternalServerError
		event.StatusCode = &statusCode
	}
	_ = projectChannelSmartScheduleTestEvent(event)
}

func projectAndProtectChannelSmartScheduleRuntimeFailureForTest(
	channelId int,
	modelName string,
	err *types.NewAPIError,
) {
	if err == nil || types.IsSkipRetryError(err) {
		return
	}
	now := time.Now().Unix()
	if err.StatusCode == http.StatusTooManyRequests {
		event := model.NewChannelMonitorEvent(
			channelId, model.ChannelMonitorEventSourceBusiness,
			model.ChannelMonitorEventOutcomeFailure, now,
		)
		event.ModelName = modelName
		event.RequestDispatched = true
		event.IsFinalAttempt = true
		event.RuntimeProtectionEligible = true
		statusCode := err.StatusCode
		event.StatusCode = &statusCode
		_ = projectChannelSmartScheduleTestEvent(event)
		_ = applyChannelSmartScheduleRuntimeFailureWithSource(
			channelId, modelName, err, false, false, false, nil, true, 0, 0,
		)
		return
	}
	if !isChannelSmartScheduleRuntimeFailure(err) {
		return
	}
	projectChannelSmartScheduleRuntimeEventForTest(channelId, modelName, now, true)
	_ = applyChannelSmartScheduleRuntimeFailureWithSource(
		channelId, modelName, err, false, false, false, nil, true, 0, 0,
	)
}

func projectChannelSmartScheduleRuntimeRequestSuccessForTest(channelId int, modelName string) {
	projectChannelSmartScheduleRuntimeEventForTest(channelId, modelName, time.Now().Unix(), false)
}

func projectChannelSmartScheduleRuntimeProbeSuccessForTest(channelId int, modelName string) {
	projectChannelSmartScheduleRuntimeRequestSuccessForTest(channelId, modelName)
}

func projectChannelSmartScheduleRuntimeFailureForTest(
	channelId int,
	modelName string,
	now int64,
	retentionSeconds int,
	revision string,
) channelSmartScheduleRuntimeHealthSnapshot {
	projectChannelSmartScheduleRuntimeEventForTest(channelId, modelName, now, true)
	return getChannelSmartScheduleRuntimeHealth(channelId, modelName, now, retentionSeconds, revision)
}

func projectChannelSmartScheduleRuntimeSuccessForTest(
	channelId int,
	modelName string,
	now int64,
	revision string,
) channelSmartScheduleRuntimeHealthSnapshot {
	projectChannelSmartScheduleRuntimeEventForTest(channelId, modelName, now, false)
	return getChannelSmartScheduleRuntimeHealth(
		channelId, modelName, now, maxChannelSmartScheduleRuntimeRetentionSeconds(), revision,
	)
}
