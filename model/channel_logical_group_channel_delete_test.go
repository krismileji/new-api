package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupLogicalGroupChannelDeleteTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelLogicalGroup{}, &ChannelLogicalGroupMember{}))
	originalMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	previousSnapshot := logicalChannelRuntimeCache
	previousDirty := logicalChannelRuntimeDirty
	logicalChannelRuntimeCache = nil
	logicalChannelRuntimeDirty = false
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCache
		channelSyncLock.Lock()
		logicalChannelRuntimeCache = previousSnapshot
		logicalChannelRuntimeDirty = previousDirty
		channelSyncLock.Unlock()
	})
	return db
}

func seedLogicalGroupForChannelDelete(t *testing.T, db *gorm.DB, channelIDs ...int) *ChannelLogicalGroup {
	t.Helper()
	group := &ChannelLogicalGroup{Name: "delete relation"}
	require.NoError(t, db.Create(group).Error)
	fingerprint := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, channelID := range channelIDs {
		require.NoError(t, db.Create(&Channel{Id: channelID, Name: "member", LogicalChannelID: &group.Id}).Error)
		require.NoError(t, db.Create(&ChannelLogicalGroupMember{LogicalGroupID: group.Id, ChannelID: channelID, Weight: 1, AddressFingerprint: fingerprint}).Error)
	}
	return group
}

func TestDeleteLogicalGroupMemberAdvancesRevisionAndDeletesEmptyGroup(t *testing.T) {
	db := setupLogicalGroupChannelDeleteTest(t)
	group := seedLogicalGroupForChannelDelete(t, db, 901, 902)
	require.NoError(t, RefreshLogicalChannelRuntimeCache())

	require.NoError(t, (&Channel{Id: 901}).Delete())
	var storedGroup ChannelLogicalGroup
	require.NoError(t, db.First(&storedGroup, group.Id).Error)
	assert.Equal(t, group.Revision+1, storedGroup.Revision)
	var members []ChannelLogicalGroupMember
	require.NoError(t, db.Where("logical_group_id = ?", group.Id).Find(&members).Error)
	require.Len(t, members, 1)
	assert.Equal(t, 902, members[0].ChannelID)
	identity, err := ResolveChannelLogicalIdentity(902)
	require.NoError(t, err)
	assert.Equal(t, storedGroup.Revision, identity.Revision)

	require.NoError(t, (&Channel{Id: 902}).Delete())
	var groupCount int64
	require.NoError(t, db.Model(&ChannelLogicalGroup{}).Where("id = ?", group.Id).Count(&groupCount).Error)
	assert.Zero(t, groupCount, "deleting the last member removes the empty logical group")
	var memberCount int64
	require.NoError(t, db.Model(&ChannelLogicalGroupMember{}).Where("logical_group_id = ?", group.Id).Count(&memberCount).Error)
	assert.Zero(t, memberCount)
}

func TestBatchDeleteAllLogicalGroupMembersDeletesGroup(t *testing.T) {
	db := setupLogicalGroupChannelDeleteTest(t)
	group := seedLogicalGroupForChannelDelete(t, db, 911, 912)
	deleted, err := BatchDeleteChannels([]int{912, 911})
	require.NoError(t, err)
	assert.EqualValues(t, 2, deleted)
	var groupCount int64
	require.NoError(t, db.Model(&ChannelLogicalGroup{}).Where("id = ?", group.Id).Count(&groupCount).Error)
	assert.Zero(t, groupCount)
}

func TestDeleteLogicalGroupMemberRollbackKeepsDatabaseAndCache(t *testing.T) {
	db := setupLogicalGroupChannelDeleteTest(t)
	group := seedLogicalGroupForChannelDelete(t, db, 921, 922)
	require.NoError(t, db.Create(&Ability{ChannelId: 921, Group: "default", Model: "model-a", Enabled: true}).Error)
	require.NoError(t, RefreshLogicalChannelRuntimeCache())
	before, err := GetLogicalChannelGroupSnapshot(group.Id)
	require.NoError(t, err)

	forced := errors.New("forced ability delete failure")
	callbackName := "test:logical_group_delete_late_failure"
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Ability" {
			tx.AddError(forced)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Delete().Remove(callbackName) })
	err = (&Channel{Id: 921}).Delete()
	assert.ErrorIs(t, err, forced)

	var channelCount int64
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 921).Count(&channelCount).Error)
	assert.EqualValues(t, 1, channelCount)
	var storedGroup ChannelLogicalGroup
	require.NoError(t, db.First(&storedGroup, group.Id).Error)
	assert.Equal(t, group.Revision, storedGroup.Revision)
	var memberCount int64
	require.NoError(t, db.Model(&ChannelLogicalGroupMember{}).Where("logical_group_id = ?", group.Id).Count(&memberCount).Error)
	assert.EqualValues(t, 2, memberCount)
	after, err := GetLogicalChannelGroupSnapshot(group.Id)
	require.NoError(t, err)
	assert.Equal(t, before, after, "failed deletion must not invalidate or replace the published snapshot")
}
