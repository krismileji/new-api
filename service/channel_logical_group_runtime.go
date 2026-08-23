package service

import "github.com/QuantumNous/new-api/model"

// Runtime snapshot aliases keep scheduling, status-probe and model-detection
// callers on a service-level contract while the cache remains model-owned.
type LogicalChannelIdentity = model.LogicalChannelIdentity
type LogicalChannelMemberSnapshot = model.LogicalChannelMemberSnapshot
type LogicalChannelGroupSnapshot = model.LogicalChannelGroupSnapshot
type LogicalChannelRuntimeSnapshot = model.LogicalChannelRuntimeSnapshot

var (
	ErrLogicalChannelRuntimeChannelNotFound = model.ErrLogicalChannelRuntimeChannelNotFound
	ErrLogicalChannelRuntimeGroupNotFound   = model.ErrLogicalChannelRuntimeGroupNotFound
	ErrLogicalChannelRuntimeUnavailable     = model.ErrLogicalChannelRuntimeUnavailable
)

func ResolveChannelLogicalIdentity(channelID int) (LogicalChannelIdentity, error) {
	return model.ResolveChannelLogicalIdentity(channelID)
}

func GetLogicalChannelGroupSnapshot(logicalID int64) (LogicalChannelGroupSnapshot, error) {
	return model.GetLogicalChannelGroupSnapshot(logicalID)
}

func GetLogicalChannelSelectionSnapshot(identity LogicalChannelIdentity) (LogicalChannelGroupSnapshot, error) {
	return model.GetLogicalChannelSelectionSnapshot(identity)
}

func GetLogicalChannelMembers(channelID int) (LogicalChannelIdentity, LogicalChannelGroupSnapshot, error) {
	return model.GetLogicalChannelMembers(channelID)
}

func GetLogicalChannelRuntimeSnapshot() (*LogicalChannelRuntimeSnapshot, error) {
	return model.GetLogicalChannelRuntimeSnapshot()
}

func RefreshLogicalChannelRuntimeCache() error {
	return model.RefreshLogicalChannelRuntimeCache()
}

func InvalidateLogicalChannelRuntimeCache() {
	model.InvalidateLogicalChannelRuntimeCache()
}
