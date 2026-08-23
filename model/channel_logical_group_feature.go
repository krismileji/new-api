package model

import "github.com/QuantumNous/new-api/common"

// ChannelLogicalGroupGlobalEnableEnv is the process-wide kill switch for the
// logical-channel grouping rollout. It defaults to enabled so existing
// installations keep the configured behavior; setting it to false makes new
// scheduling, status-probe, and model-detection tasks use physical identities.
const ChannelLogicalGroupGlobalEnableEnv = "CHANNEL_LOGICAL_GROUP_ENABLED"

// IsLogicalChannelGroupingEnabled reports whether the logical-group rollout is
// enabled for this process. The environment value is intentionally read at
// decision time so operators can disable the feature during an incident and
// restart workers without changing persisted physical-channel data.
func IsLogicalChannelGroupingEnabled() bool {
	return common.GetEnvOrDefaultBool(ChannelLogicalGroupGlobalEnableEnv, true)
}

// IsLogicalChannelGroupActive combines the process-wide rollout switch with
// the per-group administrative status. A disabled group is treated as
// ungrouped by new task identity resolution, which restores the pre-feature
// physical-channel behavior without deleting relation rows or history.
func IsLogicalChannelGroupActive(status int) bool {
	return IsLogicalChannelGroupingEnabled() && status == ChannelLogicalGroupStatusEnabled
}
