package model

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	dirtySelectionLogicalID = int64(9600)
	dirtySelectionOldMember = 9601
	dirtySelectionRetained  = 9602
	dirtySelectionNewMember = 9603
)

func setupDirtyLogicalSelectionTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupChannelSmartScheduleRouteTestDB(t)
	t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "true")
	useChannelSmartScheduleTrafficPolicy(t, true, `[{"group":"vip","models":["model-a"]}]`)
	require.NoError(t, db.AutoMigrate(
		&ChannelLogicalGroup{},
		&ChannelLogicalGroupMember{},
		&ChannelLogicalSmartScheduleRouteState{},
	))

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalRedisClient := common.RDB
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	common.RDB = redisClient
	originalRuntimeRouteIndex := channelSmartScheduleRuntimeRouteIndexCache.Load()
	channelSyncLock.Lock()
	originalGroupCache := group2model2channels
	originalChannelCache := channelsIDM
	originalAdvancedCustomCache := channel2advancedCustomConfig
	originalRouteCache := channelSmartScheduleRouteCache
	originalRouteCacheDirty := channelSmartScheduleRouteCacheDirty
	originalLogicalRoutingCache := channelLogicalSmartScheduleRoutingCache
	originalLogicalRuntime := logicalChannelRuntimeCache
	originalLogicalDirty := logicalChannelRuntimeDirty
	originalSnapshotMetadata := channelSmartScheduleLocalSnapshotMetadataCache
	originalSnapshotDirtySince := channelSmartScheduleRouteSnapshotDirtySince
	originalSnapshotDirtyGeneration := channelSmartScheduleRouteSnapshotDirtyGeneration
	originalSnapshotDirtyWatermark := channelSmartScheduleRouteSnapshotDirtyWatermark
	channelSyncLock.Unlock()
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		_ = StopChannelSmartScheduleRefreshWorker(context.Background())
		common.RDB = originalRedisClient
		assert.NoError(t, redisClient.Close())
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		group2model2channels = originalGroupCache
		channelsIDM = originalChannelCache
		channel2advancedCustomConfig = originalAdvancedCustomCache
		channelSmartScheduleRouteCache = originalRouteCache
		channelSmartScheduleRouteCacheDirty = originalRouteCacheDirty
		channelLogicalSmartScheduleRoutingCache = originalLogicalRoutingCache
		logicalChannelRuntimeCache = originalLogicalRuntime
		logicalChannelRuntimeDirty = originalLogicalDirty
		channelSmartScheduleLocalSnapshotMetadataCache = originalSnapshotMetadata
		channelSmartScheduleRouteSnapshotDirtySince = originalSnapshotDirtySince
		channelSmartScheduleRouteSnapshotDirtyGeneration = originalSnapshotDirtyGeneration
		channelSmartScheduleRouteSnapshotDirtyWatermark = originalSnapshotDirtyWatermark
		channelSyncLock.Unlock()
		channelSmartScheduleRuntimeRouteIndexCache.Store(originalRuntimeRouteIndex)
	})
	return db
}

func seedDirtyLogicalSelectionTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	oldPriority := int64(10)
	retainedPriority := int64(100)
	newPriority := int64(50)
	defaultWeight := uint(100)
	logicalID := dirtySelectionLogicalID
	require.NoError(t, db.Create(&ChannelLogicalGroup{
		Id: logicalID, Name: "dirty-selection",
		Status: ChannelLogicalGroupStatusEnabled, Revision: 1,
	}).Error)
	require.NoError(t, db.Create(&[]Channel{
		{
			Id: dirtySelectionOldMember, Name: "old-member", Status: common.ChannelStatusEnabled,
			Group: "vip", Models: "model-a", LogicalChannelID: &logicalID,
		},
		{
			Id: dirtySelectionRetained, Name: "retained-member", Status: common.ChannelStatusEnabled,
			Group: "vip", Models: "model-a", LogicalChannelID: &logicalID,
		},
		{
			Id: dirtySelectionNewMember, Name: "new-member", Status: common.ChannelStatusEnabled,
			Group: "vip", Models: "model-a",
		},
	}).Error)
	fingerprint := strings.Repeat("a", 64)
	require.NoError(t, db.Create(&[]ChannelLogicalGroupMember{
		{LogicalGroupID: logicalID, ChannelID: dirtySelectionOldMember, Weight: 100, AddressFingerprint: fingerprint},
		{LogicalGroupID: logicalID, ChannelID: dirtySelectionRetained, Weight: 0, AddressFingerprint: fingerprint},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: dirtySelectionOldMember, Group: "vip", Model: "model-a", Enabled: true, Priority: &oldPriority, Weight: defaultWeight},
		{ChannelId: dirtySelectionRetained, Group: "vip", Model: "model-a", Enabled: true, Priority: &retainedPriority, Weight: defaultWeight},
		{ChannelId: dirtySelectionNewMember, Group: "vip", Model: "model-a", Enabled: true, Priority: &newPriority, Weight: defaultWeight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: dirtySelectionOldMember, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: dirtySelectionRetained, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: dirtySelectionNewMember, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)
	InitChannelCache()
	channelSyncLock.RLock()
	require.False(t, logicalChannelRuntimeDirty)
	require.NotNil(t, logicalChannelRuntimeCache)
	channelSyncLock.RUnlock()
	StartChannelSmartScheduleRefreshWorker()
	require.Eventually(t, func() bool {
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		return channelSmartScheduleLocalSnapshotMetadataCache != nil &&
			channelSmartScheduleLocalSnapshotMetadataCache.FromRedis
	}, 2*time.Second, 10*time.Millisecond)
}

func replaceDirtyLogicalSelectionRelation(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(
			"logical_group_id = ? AND channel_id = ?",
			dirtySelectionLogicalID, dirtySelectionOldMember,
		).Delete(&ChannelLogicalGroupMember{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&ChannelLogicalGroupMember{
			LogicalGroupID: dirtySelectionLogicalID, ChannelID: dirtySelectionNewMember, Weight: 100,
			AddressFingerprint: strings.Repeat("a", 64),
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&ChannelLogicalGroup{}).Where("id = ?", dirtySelectionLogicalID).
			Update("revision", 2).Error; err != nil {
			return err
		}
		if err := tx.Model(&Channel{}).Where("id = ?", dirtySelectionOldMember).
			Update("logical_channel_id", nil).Error; err != nil {
			return err
		}
		return tx.Model(&Channel{}).Where("id = ?", dirtySelectionNewMember).
			Update("logical_channel_id", dirtySelectionLogicalID).Error
	}))
}

func assertDirtyLogicalSelectionMatchesDatabase(t *testing.T, wantChannelID int) {
	t.Helper()
	selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, "")
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, wantChannelID, selected.Id)

	selectedFromDatabase, err := getRandomSatisfiedChannelWithoutCacheWithTrafficPolicy(
		"vip", "model-a", 0, "", ChannelSelectionOptions{}, currentChannelSmartScheduleTrafficPolicy(),
	)
	require.NoError(t, err)
	require.NotNil(t, selectedFromDatabase)
	assert.Equal(t, selectedFromDatabase.Id, selected.Id)
}

func waitForDirtyLogicalSelection(t *testing.T, wantChannelID int) {
	t.Helper()
	var selected *Channel
	var err error
	require.Eventually(t, func() bool {
		selected, err = GetRandomSatisfiedChannel("vip", "model-a", 0, "")
		return err == nil && selected != nil && selected.Id == wantChannelID
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, wantChannelID, selected.Id)
}

func TestDirtyLogicalSelectionRefreshesReplacedRelationBeforeRouting(t *testing.T) {
	db := setupDirtyLogicalSelectionTest(t)
	seedDirtyLogicalSelectionTest(t, db)
	replaceDirtyLogicalSelectionRelation(t, db)
	InvalidateLogicalChannelRuntimeCache()

	// The request path immediately serves the previous complete snapshot.
	selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, "")
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, dirtySelectionOldMember, selected.Id)
	waitForDirtyLogicalSelection(t, dirtySelectionNewMember)
}

func TestDirtyLogicalSelectionRefreshesDisabledGroupBeforeRouting(t *testing.T) {
	db := setupDirtyLogicalSelectionTest(t)
	seedDirtyLogicalSelectionTest(t, db)
	require.NoError(t, db.Model(&ChannelLogicalGroup{}).Where("id = ?", dirtySelectionLogicalID).
		Updates(map[string]any{"status": ChannelLogicalGroupStatusDisabled, "revision": 2}).Error)
	InvalidateLogicalChannelRuntimeCache()

	selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, "")
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, dirtySelectionOldMember, selected.Id)
	waitForDirtyLogicalSelection(t, dirtySelectionRetained)
}

func TestDirtyLogicalSelectionRefreshesDeletedGroupBeforeRouting(t *testing.T) {
	db := setupDirtyLogicalSelectionTest(t)
	seedDirtyLogicalSelectionTest(t, db)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("logical_group_id = ?", dirtySelectionLogicalID).
			Delete(&ChannelLogicalGroupMember{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&ChannelLogicalGroup{}, dirtySelectionLogicalID).Error; err != nil {
			return err
		}
		return tx.Model(&Channel{}).Where("id IN ?", []int{dirtySelectionOldMember, dirtySelectionRetained}).
			Update("logical_channel_id", nil).Error
	}))
	InvalidateLogicalChannelRuntimeCache()

	selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, "")
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, dirtySelectionOldMember, selected.Id)
	channelSyncLock.RLock()
	assert.True(t, logicalChannelRuntimeDirty)
	channelSyncLock.RUnlock()
}

func TestDirtyLogicalSelectionKeepsLastCompleteSnapshotWhenRefreshFails(t *testing.T) {
	db := setupDirtyLogicalSelectionTest(t)
	seedDirtyLogicalSelectionTest(t, db)
	replaceDirtyLogicalSelectionRelation(t, db)
	require.NoError(t, db.Create(&ChannelLogicalGroup{
		Id: 9699, Name: "invalid-unrelated-group", Status: ChannelLogicalGroupStatusEnabled, Revision: 1,
	}).Error)
	InvalidateLogicalChannelRuntimeCache()

	selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, "")
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, dirtySelectionOldMember, selected.Id)
	channelSyncLock.RLock()
	assert.True(t, logicalChannelRuntimeDirty)
	channelSyncLock.RUnlock()
}
