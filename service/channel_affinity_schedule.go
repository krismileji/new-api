package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func PreferredChannelAffinityStatus(
	group string,
	modelName string,
	channelId int,
	requestPath string,
	options ...model.ChannelSelectionOptions,
) model.ChannelSmartScheduleAffinityStatus {
	return model.ChannelSmartScheduleAffinityCandidateEligibilityExcluding(
		group, modelName, channelId, requestPath,
		channelAffinityCooldownExclusions(modelName), options...,
	)
}

func SelectPreferredChannelAffinityMember(
	group string,
	modelName string,
	preferredChannelID int,
	requestPath string,
	options ...model.ChannelSelectionOptions,
) (*model.Channel, error) {
	return model.SelectChannelSmartScheduleAffinityMemberExcluding(
		group, modelName, preferredChannelID, requestPath,
		channelAffinityCooldownExclusions(modelName), options...,
	)
}

func channelAffinityCooldownExclusions(modelName string) map[int]struct{} {
	channelIDs := channelRateLimitCooldownChannelIds(modelName, common.GetTimestamp())
	excluded := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		excluded[channelID] = struct{}{}
	}
	return excluded
}
