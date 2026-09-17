package model

// ChannelPassiveTarget is captured by the producer before the event is queued.
// Replays must use this snapshot, never the consumer's current configuration.
type ChannelPassiveTarget struct {
	ID                 string `json:"id"`
	Scope              string `json:"scope"`
	ChannelID          int    `json:"channel_id,omitempty"`
	GroupName          string `json:"group_name,omitempty"`
	ModelName          string `json:"model_name"`
	IntervalSeconds    int    `json:"interval_seconds"`
	ConfigRevision     int64  `json:"config_revision"`
	PolicyRevision     int64  `json:"policy_revision,omitempty"`
	LogicalRevision    int64  `json:"logical_revision,omitempty"`
	MembershipRevision string `json:"membership_revision,omitempty"`
	EffectiveAt        int64  `json:"effective_at"`
}
