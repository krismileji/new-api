package model

import (
	"errors"
	"math/rand"
	"sort"
)

// LogicalChannelSelectionSnapshot is the immutable relation snapshot used by
// the shared smart-scheduling, status-probe, and model-detection paths. It is
// an alias so callers can make the selection boundary explicit without
// copying the runtime snapshot returned by GetLogicalChannelSelectionSnapshot.
type LogicalChannelSelectionSnapshot = LogicalChannelGroupSnapshot

// LogicalChannelMemberAvailability describes the current member-level
// eligibility observed by a caller. Weight must be copied from the matching
// snapshot member; keeping it in this value lets callers retain one auditable
// decision input while the selector still rejects stale/mismatched snapshots.
// Reason is diagnostic only and is never returned to an end user.
type LogicalChannelMemberAvailability struct {
	ChannelID int
	Weight    uint
	Available bool
	Reason    string
}

// LogicalChannelRandomSource is injectable to make weighted choices
// deterministic in tests. Uint64n must return a value in [0, max).
type LogicalChannelRandomSource interface {
	Uint64n(max uint64) uint64
}

// LogicalChannelRandomFunc adapts a function to LogicalChannelRandomSource.
type LogicalChannelRandomFunc func(max uint64) uint64

func (f LogicalChannelRandomFunc) Uint64n(max uint64) uint64 {
	if f == nil {
		return 0
	}
	return f(max)
}

var (
	ErrLogicalChannelSelectionInvalidSnapshot     = errors.New("逻辑渠道组成员选择快照无效")
	ErrLogicalChannelSelectionGroupDisabled       = errors.New("逻辑渠道组已禁用")
	ErrLogicalChannelSelectionNoAvailableMembers  = errors.New("逻辑渠道组没有可用成员")
	ErrLogicalChannelSelectionInvalidAvailability = errors.New("逻辑渠道组成员可用性列表无效")
	ErrLogicalChannelSelectionRandomOutOfRange    = errors.New("逻辑渠道组随机选择源返回值无效")
)

type defaultLogicalChannelRandomSource struct{}

func (defaultLogicalChannelRandomSource) Uint64n(max uint64) uint64 {
	if max <= 1 {
		return 0
	}
	// Rejection sampling avoids modulo bias while supporting a total weight
	// larger than math.MaxInt64.
	maxUint := ^uint64(0)
	limit := maxUint - (maxUint % max)
	for {
		n := rand.Uint64()
		if n < limit {
			return n % max
		}
	}
}

// SelectLogicalChannelMember chooses one physical channel from a frozen
// logical-group snapshot. A nil availability slice means every snapshot
// member is eligible. When availability is provided, only listed members with
// Available=true are eligible; this is where callers exclude disabled keys,
// insufficient balances, member cooling, and other physical-channel health
// conditions. The selector does not inspect credentials and does not acquire
// or account for channel concurrency.
//
// If any eligible member has a positive weight, zero-weight members are
// excluded. If all eligible weights are zero, all eligible members receive an
// equal share. Members are sorted by channel ID before sampling so a seeded
// random source produces stable results regardless of database row order.
func SelectLogicalChannelMember(snapshot LogicalChannelSelectionSnapshot, availability []LogicalChannelMemberAvailability, rng LogicalChannelRandomSource) (int, error) {
	if snapshot.LogicalChannelID <= 0 || snapshot.Revision < 0 || len(snapshot.Members) == 0 {
		return 0, ErrLogicalChannelSelectionInvalidSnapshot
	}
	if snapshot.Status != ChannelLogicalGroupStatusEnabled {
		return 0, ErrLogicalChannelSelectionGroupDisabled
	}

	weights := make(map[int]uint, len(snapshot.Members))
	for _, member := range snapshot.Members {
		if member.ChannelID <= 0 || ValidateChannelLogicalGroupMemberWeight(member.Weight) != nil {
			return 0, ErrLogicalChannelSelectionInvalidSnapshot
		}
		if _, exists := weights[member.ChannelID]; exists {
			return 0, ErrLogicalChannelSelectionInvalidSnapshot
		}
		weights[member.ChannelID] = member.Weight
	}

	eligible := make([]LogicalChannelMemberAvailability, 0, len(snapshot.Members))
	if availability == nil {
		for channelID, weight := range weights {
			eligible = append(eligible, LogicalChannelMemberAvailability{ChannelID: channelID, Weight: weight, Available: true})
		}
	} else {
		seen := make(map[int]struct{}, len(availability))
		for _, candidate := range availability {
			if candidate.ChannelID <= 0 {
				return 0, ErrLogicalChannelSelectionInvalidAvailability
			}
			if _, exists := seen[candidate.ChannelID]; exists {
				return 0, ErrLogicalChannelSelectionInvalidAvailability
			}
			seen[candidate.ChannelID] = struct{}{}
			weight, exists := weights[candidate.ChannelID]
			if !exists || candidate.Weight != weight {
				return 0, ErrLogicalChannelSelectionInvalidAvailability
			}
			if candidate.Available {
				eligible = append(eligible, candidate)
			}
		}
	}
	if len(eligible) == 0 {
		return 0, ErrLogicalChannelSelectionNoAvailableMembers
	}

	sort.Slice(eligible, func(i, j int) bool { return eligible[i].ChannelID < eligible[j].ChannelID })
	totalWeight := uint64(0)
	for _, member := range eligible {
		if totalWeight > ^uint64(0)-uint64(member.Weight) {
			return 0, ErrLogicalChannelSelectionInvalidSnapshot
		}
		totalWeight += uint64(member.Weight)
	}
	if totalWeight == 0 {
		totalWeight = uint64(len(eligible))
	}

	if rng == nil {
		rng = defaultLogicalChannelRandomSource{}
	}
	selected := rng.Uint64n(totalWeight)
	if selected >= totalWeight {
		return 0, ErrLogicalChannelSelectionRandomOutOfRange
	}
	for _, member := range eligible {
		weight := uint64(member.Weight)
		if totalWeight == uint64(len(eligible)) && allEligibleWeightsZero(eligible) {
			weight = 1
		}
		if weight == 0 {
			continue
		}
		if selected < weight {
			return member.ChannelID, nil
		}
		selected -= weight
	}
	return 0, ErrLogicalChannelSelectionNoAvailableMembers
}

func allEligibleWeightsZero(members []LogicalChannelMemberAvailability) bool {
	for _, member := range members {
		if member.Weight > 0 {
			return false
		}
	}
	return true
}
