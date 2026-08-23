package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createLogicalGroupFeatureFixture(t *testing.T, status int) (int, int64) {
	t.Helper()
	t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "true")
	db := setupLogicalChannelRuntimeTestDB(t)
	require.NoError(t, db.Create(&Channel{Id: 1201, Key: "feature-key-a", Name: "feature-a"}).Error)
	require.NoError(t, db.Create(&Channel{Id: 1202, Key: "feature-key-b", Name: "feature-b"}).Error)
	group := &ChannelLogicalGroup{Name: "feature-group", Status: status}
	require.NoError(t, db.Create(group).Error)
	fingerprint := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	for _, channelID := range []int{1201, 1202} {
		require.NoError(t, db.Create(&ChannelLogicalGroupMember{
			LogicalGroupID: group.Id, ChannelID: channelID, Weight: 1, AddressFingerprint: fingerprint,
		}).Error)
	}
	require.NoError(t, db.Model(&Channel{}).Where("id IN ?", []int{1201, 1202}).Update("logical_channel_id", group.Id).Error)
	require.NoError(t, RefreshLogicalChannelRuntimeCache())
	return 1201, group.Id
}

func TestLogicalChannelGroupDisabledFallsBackToPhysicalIdentity(t *testing.T) {
	channelID, groupID := createLogicalGroupFeatureFixture(t, ChannelLogicalGroupStatusDisabled)
	identity, err := ResolveChannelLogicalIdentity(channelID)
	require.NoError(t, err)
	assert.Equal(t, channelID, identity.ChannelID)
	assert.EqualValues(t, channelID, identity.LogicalChannelID)
	assert.Zero(t, identity.Revision)

	selection, err := GetLogicalChannelSelectionSnapshot(identity)
	require.NoError(t, err)
	require.Len(t, selection.Members, 1)
	assert.Equal(t, channelID, selection.Members[0].ChannelID)

	// Persisted group data remains available for diagnostics and a later
	// re-enable; disabling must not delete history or relation rows.
	group, err := GetLogicalChannelGroupSnapshot(groupID)
	require.NoError(t, err)
	assert.Equal(t, ChannelLogicalGroupStatusDisabled, group.Status)
	assert.Len(t, group.Members, 2)
}

func TestLogicalChannelGlobalKillSwitchFallsBackWithoutChangingRelations(t *testing.T) {
	channelID, groupID := createLogicalGroupFeatureFixture(t, ChannelLogicalGroupStatusEnabled)
	t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "false")

	identity, err := ResolveChannelLogicalIdentity(channelID)
	require.NoError(t, err)
	assert.EqualValues(t, channelID, identity.LogicalChannelID)
	assert.Zero(t, identity.Revision)

	var memberCount int64
	require.NoError(t, DB.Model(&ChannelLogicalGroupMember{}).Where("logical_group_id = ?", groupID).Count(&memberCount).Error)
	assert.EqualValues(t, 2, memberCount)

	t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "true")
	identity, err = ResolveChannelLogicalIdentity(channelID)
	require.NoError(t, err)
	assert.EqualValues(t, groupID, identity.LogicalChannelID)
	assert.NotZero(t, identity.Revision)
}

func TestLogicalChannelGlobalKillSwitchKeepsSmartScheduleCandidatesPhysical(t *testing.T) {
	t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "false")
	runtime := &LogicalChannelRuntimeSnapshot{
		Channels: map[int]LogicalChannelIdentity{
			1501: {ChannelID: 1501, LogicalChannelID: 9500, Revision: 3},
			1502: {ChannelID: 1502, LogicalChannelID: 9500, Revision: 3},
		},
		Groups: map[int64]LogicalChannelGroupSnapshot{
			9500: {
				LogicalChannelID: 9500, Revision: 3, Status: ChannelLogicalGroupStatusEnabled,
				Members: []LogicalChannelMemberSnapshot{{ChannelID: 1501, Weight: 1}, {ChannelID: 1502, Weight: 1}},
			},
		},
	}
	routes := []channelSmartScheduleCachedRoute{{channelId: 1501}, {channelId: 1502}}

	coalesced := coalesceChannelSmartScheduleLogicalRoutes(routes, runtime)
	require.Len(t, coalesced, 2)
	assert.Zero(t, coalesced[0].logicalChannelID)
	assert.Zero(t, coalesced[1].logicalChannelID)
}
