package service

import "github.com/QuantumNous/new-api/model"

func PreferredChannelAffinityStatus(
	group string,
	modelName string,
	channelId int,
	requestPath string,
	options ...model.ChannelSelectionOptions,
) model.ChannelSmartScheduleAffinityStatus {
	if ChannelRateLimitCooldownUntil(channelId, modelName) > 0 {
		return model.ChannelSmartScheduleAffinityTemporarilyUnavailable
	}
	return model.ChannelSmartScheduleAffinityEligibility(group, modelName, channelId, requestPath, options...)
}
