package model

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupLogicalChannelRuntimeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	previousMemoryCache := common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "logical-runtime.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}, &ChannelLogicalGroup{}, &ChannelLogicalGroupMember{}))
	DB = db
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	previousSnapshot := logicalChannelRuntimeCache
	previousDirty := logicalChannelRuntimeDirty
	logicalChannelRuntimeCache = nil
	logicalChannelRuntimeDirty = false
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCache
		channelSyncLock.Lock()
		logicalChannelRuntimeCache = previousSnapshot
		logicalChannelRuntimeDirty = previousDirty
		channelSyncLock.Unlock()
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			assert.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func TestLogicalChannelRuntimeUngroupedAndGroupedRevision(t *testing.T) {
	db := setupLogicalChannelRuntimeTestDB(t)
	require.NoError(t, db.Create(&Channel{Id: 501, Key: "key-a", Name: "a"}).Error)
	require.NoError(t, db.Create(&Channel{Id: 502, Key: "key-b", Name: "b"}).Error)
	require.NoError(t, db.Create(&Channel{Id: 503, Key: "key-c", Name: "c"}).Error)

	identity, err := ResolveChannelLogicalIdentity(501)
	require.NoError(t, err)
	assert.Equal(t, 501, identity.ChannelID)
	assert.EqualValues(t, 501, identity.LogicalChannelID)
	assert.Zero(t, identity.Revision)

	group := &ChannelLogicalGroup{Name: "runtime-group"}
	require.NoError(t, db.Create(group).Error)
	fingerprint := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	require.NoError(t, db.Create(&ChannelLogicalGroupMember{LogicalGroupID: group.Id, ChannelID: 501, Weight: 3, AddressFingerprint: fingerprint}).Error)
	require.NoError(t, db.Create(&ChannelLogicalGroupMember{LogicalGroupID: group.Id, ChannelID: 502, Weight: 1, AddressFingerprint: fingerprint}).Error)
	require.NoError(t, db.Model(&Channel{}).Where("id IN ?", []int{501, 502}).Update("logical_channel_id", group.Id).Error)
	require.NoError(t, RefreshLogicalChannelRuntimeCache())

	identity, err = ResolveChannelLogicalIdentity(501)
	require.NoError(t, err)
	assert.EqualValues(t, group.Id, identity.LogicalChannelID)
	assert.EqualValues(t, group.Revision, identity.Revision)
	snapshot, err := GetLogicalChannelGroupSnapshot(group.Id)
	require.NoError(t, err)
	require.Len(t, snapshot.Members, 2)
	assert.EqualValues(t, 3, snapshot.Members[0].Weight)
	assert.EqualValues(t, 1, snapshot.Members[1].Weight)
	groupedIdentity := identity

	// A relation update marks the cache dirty. The next new-task resolution
	// observes the incremented revision and the replacement member set.
	oldRevision := group.Revision
	require.NoError(t, db.Model(group).Updates(map[string]interface{}{"revision": oldRevision + 1}).Error)
	require.NoError(t, db.Where("logical_group_id = ?", group.Id).Delete(&ChannelLogicalGroupMember{}).Error)
	require.NoError(t, db.Create(&ChannelLogicalGroupMember{LogicalGroupID: group.Id, ChannelID: 503, Weight: 7, AddressFingerprint: fingerprint}).Error)
	require.NoError(t, db.Model(&Channel{}).Where("id IN ?", []int{501, 502}).Update("logical_channel_id", nil).Error)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 503).Update("logical_channel_id", group.Id).Error)
	InvalidateLogicalChannelRuntimeCache()
	var selectedChannelColumns []string
	callbackName := "test:record_logical_runtime_channel_columns"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" {
			selectedChannelColumns = append(selectedChannelColumns, tx.Statement.Selects...)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	identity, err = ResolveChannelLogicalIdentity(503)
	require.NoError(t, err)
	assert.EqualValues(t, group.Id, identity.LogicalChannelID)
	assert.EqualValues(t, oldRevision+1, identity.Revision)
	assert.NotContains(t, selectedChannelColumns, "key", "runtime cache must not select complete credentials")
	_, err = GetLogicalChannelSelectionSnapshot(groupedIdentity)
	assert.ErrorIs(t, err, ErrChannelLogicalGroupRevisionConflict, "a stale task identity must not read a new member set")
	identity, err = ResolveChannelLogicalIdentity(501)
	require.NoError(t, err)
	assert.EqualValues(t, 501, identity.LogicalChannelID, "removed member falls back to its physical channel identity")
	assert.Zero(t, identity.Revision)
}

func TestLogicalChannelRuntimeFallsBackWhenOptionalSchemaIsMissing(t *testing.T) {
	previousDB := DB
	previousMemoryCache := common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "logical-runtime-legacy.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}))
	DB = db
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCache
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			assert.NoError(t, sqlDB.Close())
		}
	})
	require.NoError(t, db.Create(&Channel{Id: 509, Key: "legacy-key", Name: "legacy"}).Error)

	identity, err := ResolveChannelLogicalIdentity(509)
	require.NoError(t, err)
	assert.Equal(t, 509, identity.ChannelID)
	assert.EqualValues(t, 509, identity.LogicalChannelID)
	assert.Zero(t, identity.Revision)
	runtime, err := GetLogicalChannelRuntimeSnapshot()
	require.NoError(t, err)
	assert.Empty(t, runtime.Groups)
}

func TestLogicalChannelRuntimeRefreshFailureRetainsDiagnosticSnapshotButRejectsDirtySelection(t *testing.T) {
	db := setupLogicalChannelRuntimeTestDB(t)
	require.NoError(t, db.Create(&Channel{Id: 601, Key: "key-a", Name: "a"}).Error)
	group := &ChannelLogicalGroup{Name: "stable-group"}
	require.NoError(t, db.Create(group).Error)
	fingerprint := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	require.NoError(t, db.Create(&ChannelLogicalGroupMember{LogicalGroupID: group.Id, ChannelID: 601, Weight: 1, AddressFingerprint: fingerprint}).Error)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 601).Update("logical_channel_id", group.Id).Error)
	require.NoError(t, RefreshLogicalChannelRuntimeCache())
	before, err := GetLogicalChannelGroupSnapshot(group.Id)
	require.NoError(t, err)

	require.NoError(t, db.Model(group).Update("revision", group.Revision+1).Error)
	require.NoError(t, db.Where("logical_group_id = ?", group.Id).Delete(&ChannelLogicalGroupMember{}).Error)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 601).Update("logical_channel_id", nil).Error)
	InvalidateLogicalChannelRuntimeCache()

	wantErr := errors.New("member snapshot failed")
	callbackName := "test:fail_logical_runtime_member_snapshot"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "ChannelLogicalGroupMember" {
			tx.AddError(wantErr)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	identity, err := ResolveChannelLogicalIdentity(601)
	require.NoError(t, err)
	assert.EqualValues(t, 601, identity.LogicalChannelID, "dirty cache must not route a new request to the removed member")
	assert.Zero(t, identity.Revision)
	_, err = GetLogicalChannelGroupSnapshot(group.Id)
	assert.ErrorIs(t, err, wantErr, "dirty member selection must fail closed when current membership cannot be loaded")
	diagnostic, err := GetLogicalChannelRuntimeSnapshot()
	require.NoError(t, err)
	assert.Equal(t, before, diagnostic.Groups[group.Id], "the previous complete snapshot remains available for diagnostics")
}

func TestLogicalChannelRuntimeInvalidRelationDoesNotPublishPartialSnapshot(t *testing.T) {
	db := setupLogicalChannelRuntimeTestDB(t)
	require.NoError(t, db.Create(&Channel{Id: 701, Key: "key-a", Name: "a"}).Error)
	group := &ChannelLogicalGroup{Name: "valid-group"}
	require.NoError(t, db.Create(group).Error)
	fingerprint := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	require.NoError(t, db.Create(&ChannelLogicalGroupMember{LogicalGroupID: group.Id, ChannelID: 701, Weight: 1, AddressFingerprint: fingerprint}).Error)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 701).Update("logical_channel_id", group.Id).Error)
	require.NoError(t, RefreshLogicalChannelRuntimeCache())

	invalidGroup := &ChannelLogicalGroup{Name: "invalid-group"}
	require.NoError(t, db.Create(invalidGroup).Error)
	// The model validates member shape but deliberately does not add a foreign
	// key, so a missing physical channel is caught while building the snapshot.
	require.NoError(t, db.Create(&ChannelLogicalGroupMember{LogicalGroupID: invalidGroup.Id, ChannelID: 799, Weight: 1, AddressFingerprint: fingerprint}).Error)
	assert.Error(t, RefreshLogicalChannelRuntimeCache())

	snapshot, err := GetLogicalChannelRuntimeSnapshot()
	require.NoError(t, err)
	assert.Contains(t, snapshot.Groups, group.Id)
	assert.NotContains(t, snapshot.Groups, invalidGroup.Id)
}

func TestLogicalChannelRuntimeVirtualIdentityAvoidsGroupIdCollision(t *testing.T) {
	db := setupLogicalChannelRuntimeTestDB(t)
	// Deliberately use the same numeric value for a physical channel id and a
	// persisted logical group id; the two identity spaces are otherwise allowed
	// to evolve independently.
	require.NoError(t, db.Create(&Channel{Id: 801, Key: "key-unGrouped", Name: "ungrouped"}).Error)
	require.NoError(t, db.Create(&Channel{Id: 802, Key: "key-grouped", Name: "grouped"}).Error)
	group := &ChannelLogicalGroup{Id: 801, Name: "group-id-collision"}
	require.NoError(t, db.Create(group).Error)
	fingerprint := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	require.NoError(t, db.Create(&ChannelLogicalGroupMember{LogicalGroupID: group.Id, ChannelID: 802, Weight: 1, AddressFingerprint: fingerprint}).Error)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 802).Update("logical_channel_id", group.Id).Error)
	require.NoError(t, RefreshLogicalChannelRuntimeCache())

	ungrouped, err := ResolveChannelLogicalIdentity(801)
	require.NoError(t, err)
	self, err := GetLogicalChannelSelectionSnapshot(ungrouped)
	require.NoError(t, err)
	require.Len(t, self.Members, 1)
	assert.Equal(t, 801, self.Members[0].ChannelID)

	grouped, err := ResolveChannelLogicalIdentity(802)
	require.NoError(t, err)
	shared, err := GetLogicalChannelSelectionSnapshot(grouped)
	require.NoError(t, err)
	require.Len(t, shared.Members, 1)
	assert.Equal(t, 802, shared.Members[0].ChannelID)
}
