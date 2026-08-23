package service

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func migrateLogicalChannelUpdateTestTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(
		&model.Ability{},
		&model.ChannelSmartScheduleRouteState{},
		&model.ChannelSmartScheduleGroupPause{},
		&model.ChannelSmartScheduleModelSampleState{},
	))
}

func TestUpdateGroupedChannelAddressValidatesMembersAndAdvancesRevision(t *testing.T) {
	db := setupLogicalChannelGroupServiceDB(t)
	migrateLogicalChannelUpdateTestTables(t, db)
	address := "https://api.example.com/v1"
	equivalent := "HTTPS://API.EXAMPLE.COM:443/v1/"
	channelA := logicalGroupTestChannel(301, address, "a")
	channelB := logicalGroupTestChannel(302, address, "b")
	require.NoError(t, db.Create(channelA).Error)
	require.NoError(t, db.Create(channelB).Error)
	group, err := CreateLogicalChannelGroup("address update", "", 0, []LogicalChannelGroupMemberInput{{ChannelID: channelA.Id}, {ChannelID: channelB.Id}})
	require.NoError(t, err)
	require.NoError(t, RefreshLogicalChannelRuntimeCache())

	proposed := *channelA
	proposed.BaseURL = &equivalent
	require.NoError(t, UpdateChannelWithLogicalGroupValidation(&proposed))
	updated, err := GetLogicalChannelGroup(group.ID)
	require.NoError(t, err)
	assert.EqualValues(t, group.Revision+1, updated.Revision)
	expectedFingerprint := LogicalChannelAddressFingerprint(address)
	for _, member := range updated.Members {
		assert.Equal(t, expectedFingerprint, member.AddressFingerprint)
	}
	identity, err := ResolveChannelLogicalIdentity(channelA.Id)
	require.NoError(t, err)
	assert.EqualValues(t, updated.Revision, identity.Revision)

	mismatch := "https://other.example.com/v1"
	proposed.BaseURL = &mismatch
	err = UpdateChannelWithLogicalGroupValidation(&proposed)
	assert.ErrorIs(t, err, ErrLogicalChannelGroupAddressMismatch)
	var stored model.Channel
	require.NoError(t, db.First(&stored, channelA.Id).Error)
	assert.Equal(t, equivalent, stored.GetBaseURL())
	unchanged, err := GetLogicalChannelGroup(group.ID)
	require.NoError(t, err)
	assert.Equal(t, updated.Revision, unchanged.Revision)
}

func TestUpdateSingleMemberAddressRollsBackRelationAndCacheOnLaterFailure(t *testing.T) {
	db := setupLogicalChannelGroupServiceDB(t)
	migrateLogicalChannelUpdateTestTables(t, db)
	address := "https://api.example.com/v1"
	channel := logicalGroupTestChannel(311, address, "single")
	require.NoError(t, db.Create(channel).Error)
	group, err := CreateLogicalChannelGroup("single address", "", 0, []LogicalChannelGroupMemberInput{{ChannelID: channel.Id}})
	require.NoError(t, err)
	require.NoError(t, RefreshLogicalChannelRuntimeCache())

	forced := errors.New("forced ability query failure")
	callbackName := "test:logical_group_address_late_failure"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Ability" {
			tx.AddError(forced)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	changedAddress := "https://new.example.com/v1"
	proposed := *channel
	proposed.BaseURL = &changedAddress
	err = UpdateChannelWithLogicalGroupValidation(&proposed)
	assert.ErrorIs(t, err, forced)

	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Equal(t, address, stored.GetBaseURL())
	storedGroup, err := GetLogicalChannelGroup(group.ID)
	require.NoError(t, err)
	assert.Equal(t, group.Revision, storedGroup.Revision)
	assert.Equal(t, group.Members[0].AddressFingerprint, storedGroup.Members[0].AddressFingerprint)
	identity, err := ResolveChannelLogicalIdentity(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, group.Revision, identity.Revision, "failed transaction must keep the published snapshot")
}

func TestLogicalGroupInvalidatesOnlyAfterMemberReplacementCommit(t *testing.T) {
	db := setupLogicalChannelGroupServiceDB(t)
	address := "https://api.example.com/v1"
	channelA := logicalGroupTestChannel(321, address, "a")
	channelB := logicalGroupTestChannel(322, address, "b")
	require.NoError(t, db.Create(channelA).Error)
	require.NoError(t, db.Create(channelB).Error)
	group, err := CreateLogicalChannelGroup("commit ordering", "", 0, []LogicalChannelGroupMemberInput{{ChannelID: channelA.Id}})
	require.NoError(t, err)
	require.NoError(t, RefreshLogicalChannelRuntimeCache())

	transactionReached := make(chan struct{})
	releaseTransaction := make(chan struct{})
	var once sync.Once
	callbackName := "test:block_logical_group_before_commit"
	require.NoError(t, db.Callback().Update().After("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "ChannelLogicalGroup" {
			once.Do(func() {
				close(transactionReached)
				<-releaseTransaction
			})
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })

	type replacementResult struct {
		group *LogicalChannelGroupView
		err   error
	}
	done := make(chan replacementResult, 1)
	go func() {
		replaced, replaceErr := ReplaceLogicalChannelGroupMembers(group.ID, group.Revision, []LogicalChannelGroupMemberInput{{ChannelID: channelB.Id}})
		done <- replacementResult{group: replaced, err: replaceErr}
	}()
	select {
	case <-transactionReached:
	case <-time.After(3 * time.Second):
		t.Fatal("replacement did not reach the pre-commit checkpoint")
	}

	identity, err := ResolveChannelLogicalIdentity(channelA.Id)
	require.NoError(t, err)
	assert.EqualValues(t, group.ID, identity.LogicalChannelID)
	assert.Equal(t, group.Revision, identity.Revision, "uncommitted relation must not dirty or rebuild the cache")
	close(releaseTransaction)
	result := <-done
	require.NoError(t, result.err)
	require.NotNil(t, result.group)
	identity, err = ResolveChannelLogicalIdentity(channelB.Id)
	require.NoError(t, err)
	assert.EqualValues(t, group.ID, identity.LogicalChannelID)
	assert.Equal(t, result.group.Revision, identity.Revision, "the first new task after commit must observe the new revision")
}

func TestCreateLogicalGroupRollsBackWhenSelectedChannelDisappearsBeforeOwnershipUpdate(t *testing.T) {
	db := setupLogicalChannelGroupServiceDB(t)
	address := "https://api.example.com/v1"
	channel := logicalGroupTestChannel(331, address, "create-missing")
	require.NoError(t, db.Create(channel).Error)
	callbackName := "test:delete_logical_group_create_member_channel"
	require.NoError(t, db.Callback().Create().After("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "ChannelLogicalGroupMember" {
			return
		}
		deleteTx := tx.Session(&gorm.Session{NewDB: true})
		if err := deleteTx.Where("id = ?", channel.Id).Delete(&model.Channel{}).Error; err != nil {
			tx.AddError(err)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })

	_, err := CreateLogicalChannelGroup("create missing", "", 0, []LogicalChannelGroupMemberInput{{ChannelID: channel.Id}})
	assert.ErrorIs(t, err, ErrLogicalChannelGroupChannelMissing)

	var channelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Count(&channelCount).Error)
	assert.EqualValues(t, 1, channelCount)
	var groupCount int64
	require.NoError(t, db.Model(&model.ChannelLogicalGroup{}).Where("name = ?", "create missing").Count(&groupCount).Error)
	assert.Zero(t, groupCount)
	var memberCount int64
	require.NoError(t, db.Model(&model.ChannelLogicalGroupMember{}).Where("channel_id = ?", channel.Id).Count(&memberCount).Error)
	assert.Zero(t, memberCount)
}

func TestReplaceLogicalGroupRollsBackWhenSelectedChannelDisappearsBeforeOwnershipUpdate(t *testing.T) {
	db := setupLogicalChannelGroupServiceDB(t)
	address := "https://api.example.com/v1"
	oldChannel := logicalGroupTestChannel(341, address, "replace-old")
	newChannel := logicalGroupTestChannel(342, address, "replace-missing")
	require.NoError(t, db.Create(oldChannel).Error)
	require.NoError(t, db.Create(newChannel).Error)
	group, err := CreateLogicalChannelGroup("replace rollback", "", 0, []LogicalChannelGroupMemberInput{{ChannelID: oldChannel.Id}})
	require.NoError(t, err)
	require.NoError(t, RefreshLogicalChannelRuntimeCache())
	before, err := ResolveChannelLogicalIdentity(oldChannel.Id)
	require.NoError(t, err)

	callbackName := "test:delete_logical_group_replacement_channel"
	require.NoError(t, db.Callback().Create().After("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "ChannelLogicalGroupMember" {
			return
		}
		deleteTx := tx.Session(&gorm.Session{NewDB: true})
		if err := deleteTx.Where("id = ?", newChannel.Id).Delete(&model.Channel{}).Error; err != nil {
			tx.AddError(err)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })

	_, err = ReplaceLogicalChannelGroupMembers(group.ID, group.Revision, []LogicalChannelGroupMemberInput{{ChannelID: newChannel.Id}})
	assert.ErrorIs(t, err, ErrLogicalChannelGroupChannelMissing)

	stored, err := GetLogicalChannelGroup(group.ID)
	require.NoError(t, err)
	assert.Equal(t, group.Revision, stored.Revision)
	require.Len(t, stored.Members, 1)
	assert.Equal(t, oldChannel.Id, stored.Members[0].ChannelID)
	var newChannelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", newChannel.Id).Count(&newChannelCount).Error)
	assert.EqualValues(t, 1, newChannelCount, "the simulated deletion must roll back with member replacement")
	after, err := ResolveChannelLogicalIdentity(oldChannel.Id)
	require.NoError(t, err)
	assert.Equal(t, before, after, "failed replacement must not invalidate the published snapshot")
}
