package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLogicalSelectionSnapshot(status int, members ...LogicalChannelMemberSnapshot) LogicalChannelSelectionSnapshot {
	return LogicalChannelSelectionSnapshot{
		LogicalChannelID: 41,
		Revision:         7,
		Status:           status,
		Members:          members,
	}
}

func fixedLogicalSelectionRandom(value uint64) LogicalChannelRandomSource {
	return LogicalChannelRandomFunc(func(uint64) uint64 { return value })
}

func TestSelectLogicalChannelMemberUsesConfiguredWeight(t *testing.T) {
	snapshot := newLogicalSelectionSnapshot(
		ChannelLogicalGroupStatusEnabled,
		LogicalChannelMemberSnapshot{ChannelID: 20, Weight: 3},
		LogicalChannelMemberSnapshot{ChannelID: 10, Weight: 1},
	)

	selected, err := SelectLogicalChannelMember(snapshot, nil, fixedLogicalSelectionRandom(0))
	require.NoError(t, err)
	assert.Equal(t, 10, selected)

	selected, err = SelectLogicalChannelMember(snapshot, nil, fixedLogicalSelectionRandom(1))
	require.NoError(t, err)
	assert.Equal(t, 20, selected)

	selected, err = SelectLogicalChannelMember(snapshot, nil, fixedLogicalSelectionRandom(3))
	require.NoError(t, err)
	assert.Equal(t, 20, selected)
}

func TestSelectLogicalChannelMemberExcludesZeroWeightWhenPositiveWeightExists(t *testing.T) {
	snapshot := newLogicalSelectionSnapshot(
		ChannelLogicalGroupStatusEnabled,
		LogicalChannelMemberSnapshot{ChannelID: 10, Weight: 0},
		LogicalChannelMemberSnapshot{ChannelID: 20, Weight: 2},
	)

	selected, err := SelectLogicalChannelMember(snapshot, nil, fixedLogicalSelectionRandom(0))
	require.NoError(t, err)
	assert.Equal(t, 20, selected)
}

func TestSelectLogicalChannelMemberAllZeroWeightsAreEqual(t *testing.T) {
	snapshot := newLogicalSelectionSnapshot(
		ChannelLogicalGroupStatusEnabled,
		LogicalChannelMemberSnapshot{ChannelID: 20, Weight: 0},
		LogicalChannelMemberSnapshot{ChannelID: 10, Weight: 0},
	)

	selected, err := SelectLogicalChannelMember(snapshot, nil, fixedLogicalSelectionRandom(0))
	require.NoError(t, err)
	assert.Equal(t, 10, selected)
	selected, err = SelectLogicalChannelMember(snapshot, nil, fixedLogicalSelectionRandom(1))
	require.NoError(t, err)
	assert.Equal(t, 20, selected)
}

func TestSelectLogicalChannelMemberFiltersUnavailableMembers(t *testing.T) {
	snapshot := newLogicalSelectionSnapshot(
		ChannelLogicalGroupStatusEnabled,
		LogicalChannelMemberSnapshot{ChannelID: 10, Weight: 1},
		LogicalChannelMemberSnapshot{ChannelID: 20, Weight: 1},
	)
	availability := []LogicalChannelMemberAvailability{
		{ChannelID: 10, Weight: 1, Available: false, Reason: "余额不足"},
		{ChannelID: 20, Weight: 1, Available: true},
	}

	selected, err := SelectLogicalChannelMember(snapshot, availability, fixedLogicalSelectionRandom(0))
	require.NoError(t, err)
	assert.Equal(t, 20, selected)

	availability[1].Available = false
	_, err = SelectLogicalChannelMember(snapshot, availability, fixedLogicalSelectionRandom(0))
	require.ErrorIs(t, err, ErrLogicalChannelSelectionNoAvailableMembers)
}

func TestSelectLogicalChannelMemberRejectsStaleAvailabilityAndDisabledGroup(t *testing.T) {
	snapshot := newLogicalSelectionSnapshot(
		ChannelLogicalGroupStatusEnabled,
		LogicalChannelMemberSnapshot{ChannelID: 10, Weight: 1},
	)
	_, err := SelectLogicalChannelMember(snapshot, []LogicalChannelMemberAvailability{{ChannelID: 10, Weight: 2, Available: true}}, fixedLogicalSelectionRandom(0))
	require.ErrorIs(t, err, ErrLogicalChannelSelectionInvalidAvailability)

	disabled := snapshot
	disabled.Status = ChannelLogicalGroupStatusDisabled
	_, err = SelectLogicalChannelMember(disabled, nil, fixedLogicalSelectionRandom(0))
	require.ErrorIs(t, err, ErrLogicalChannelSelectionGroupDisabled)
}

func TestSelectLogicalChannelMemberRejectsInvalidRandomResult(t *testing.T) {
	snapshot := newLogicalSelectionSnapshot(
		ChannelLogicalGroupStatusEnabled,
		LogicalChannelMemberSnapshot{ChannelID: 10, Weight: 1},
	)
	_, err := SelectLogicalChannelMember(snapshot, nil, fixedLogicalSelectionRandom(1))
	require.ErrorIs(t, err, ErrLogicalChannelSelectionRandomOutOfRange)
}
