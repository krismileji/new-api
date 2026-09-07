package model

import "time"

// ChannelRoutingDecision contains routing metadata only, never keys or affinity values.
type ChannelRoutingDecision struct {
	Source              string `json:"source"`
	Group               string `json:"group"`
	Model               string `json:"model"`
	CandidateChannelID  int    `json:"candidate_channel_id"`
	ChannelID           int    `json:"channel_id"`
	LogicalChannelID    int64  `json:"logical_channel_id,omitempty"`
	LogicalRevision     int64  `json:"logical_revision,omitempty"`
	SnapshotRevision    int64  `json:"snapshot_revision,omitempty"`
	SnapshotGeneratedAt int64  `json:"snapshot_generated_at,omitempty"`
	AttemptNumber       int    `json:"attempt_number,omitempty"`
	Retry               bool   `json:"retry,omitempty"`
	RateLimitFallback   bool   `json:"rate_limit_fallback,omitempty"`
}

// Cached callers hold channelSyncLock so the recorded version belongs to the selection.
func recordChannelRoutingDecision(options ChannelSelectionOptions, route channelSmartScheduleCachedRoute,
	channelID int, group, modelName, source string, cached bool,
) {
	if options.ObserveRouting == nil {
		return
	}
	decision := ChannelRoutingDecision{
		Source: source, Group: group, Model: modelName, ChannelID: channelID,
		CandidateChannelID: route.channelId, LogicalChannelID: route.logicalChannelID,
		LogicalRevision: route.logicalRevision, RateLimitFallback: options.RateLimitFallback,
	}
	if cached && channelSmartScheduleLocalSnapshotMetadataCache != nil {
		metadata := channelSmartScheduleLocalSnapshotMetadataCache
		decision.SnapshotRevision = metadata.Revision
		decision.SnapshotGeneratedAt = time.UnixMilli(metadata.GeneratedAt).Unix()
	}
	options.ObserveRouting(decision)
}
