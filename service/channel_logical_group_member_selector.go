package service

import "github.com/QuantumNous/new-api/model"

// LogicalChannelSelectionSnapshot names the frozen member relation consumed
// by shared scheduling, probing, and model-detection callers. It aliases the
// model type so no second snapshot representation can drift from the runtime
// cache contract.
type LogicalChannelSelectionSnapshot = model.LogicalChannelSelectionSnapshot

// LogicalChannelMemberAvailability is the caller-owned physical health view.
// Weight must match the corresponding member in the snapshot; Available is
// false for disabled, unhealthy, insufficient-balance, or cooling members.
type LogicalChannelMemberAvailability = model.LogicalChannelMemberAvailability

// LogicalChannelRandomSource allows deterministic tests and callers that need
// request-scoped randomness. A nil source uses the model package default.
type LogicalChannelRandomSource = model.LogicalChannelRandomSource

// LogicalChannelRandomFunc adapts a function to LogicalChannelRandomSource.
type LogicalChannelRandomFunc = model.LogicalChannelRandomFunc

var (
	ErrLogicalChannelSelectionInvalidSnapshot     = model.ErrLogicalChannelSelectionInvalidSnapshot
	ErrLogicalChannelSelectionGroupDisabled       = model.ErrLogicalChannelSelectionGroupDisabled
	ErrLogicalChannelSelectionNoAvailableMembers  = model.ErrLogicalChannelSelectionNoAvailableMembers
	ErrLogicalChannelSelectionInvalidAvailability = model.ErrLogicalChannelSelectionInvalidAvailability
	ErrLogicalChannelSelectionRandomOutOfRange    = model.ErrLogicalChannelSelectionRandomOutOfRange
)

// SelectLogicalChannelMember chooses one eligible physical channel by the
// logical-group member weight. It returns only channel_id. The selected caller
// must invoke the existing AcquireChannelConcurrency(channelID) flow; this
// selector intentionally creates no counters, leases, or Redis state.
func SelectLogicalChannelMember(snapshot LogicalChannelSelectionSnapshot, availability []LogicalChannelMemberAvailability, rng LogicalChannelRandomSource) (int, error) {
	return model.SelectLogicalChannelMember(snapshot, availability, rng)
}
