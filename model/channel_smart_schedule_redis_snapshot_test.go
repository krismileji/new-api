package model

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelSmartScheduleRedisSnapshotTest(t *testing.T) (*gorm.DB, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	require.NoError(t, StopChannelSmartScheduleRefreshWorker(context.Background()))
	db := setupChannelSmartScheduleRouteTestDB(t)
	t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "true")
	useChannelSmartScheduleTrafficPolicy(t, true, `[{"group":"vip","models":["model-a"]}]`)

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalRedisClient := common.RDB
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
	channelSmartScheduleLocalSnapshotMetadataCache = nil
	channelSmartScheduleRouteSnapshotDirtySince = 0
	channelSmartScheduleRouteSnapshotDirtyGeneration = 0
	channelSmartScheduleRouteSnapshotDirtyWatermark = 0
	channelSyncLock.Unlock()
	channelSmartScheduleRouteSnapshotHealth.Lock()
	originalSnapshotRedisSuccessAt := channelSmartScheduleRouteSnapshotHealth.lastRedisSuccessAt
	originalSnapshotRedisFailureAt := channelSmartScheduleRouteSnapshotHealth.lastRedisFailureAt
	originalSnapshotRedisError := channelSmartScheduleRouteSnapshotHealth.lastRedisError
	channelSmartScheduleRouteSnapshotHealth.lastRedisSuccessAt = 0
	channelSmartScheduleRouteSnapshotHealth.lastRedisFailureAt = 0
	channelSmartScheduleRouteSnapshotHealth.lastRedisError = ""
	channelSmartScheduleRouteSnapshotHealth.Unlock()
	originalRuntimeRouteIndex := channelSmartScheduleRuntimeRouteIndexCache.Load()

	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	common.RDB = redisClient
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		_ = StopChannelSmartScheduleRefreshWorker(context.Background())
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.RDB = originalRedisClient
		assert.NoError(t, redisClient.Close())
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
		channelSmartScheduleRouteSnapshotHealth.Lock()
		channelSmartScheduleRouteSnapshotHealth.lastRedisSuccessAt = originalSnapshotRedisSuccessAt
		channelSmartScheduleRouteSnapshotHealth.lastRedisFailureAt = originalSnapshotRedisFailureAt
		channelSmartScheduleRouteSnapshotHealth.lastRedisError = originalSnapshotRedisError
		channelSmartScheduleRouteSnapshotHealth.Unlock()
	})
	return db, redisServer, redisClient
}

func seedChannelSmartScheduleRedisSnapshotTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	priority := int64(37)
	weight := uint(61)
	releaseLimit := 4096
	require.NoError(t, db.Create(&Channel{
		Id: 9701, Name: "快照渠道", Key: "sk-sensitive-route-snapshot-key",
		Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a",
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 9701, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 9701, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, StabilityState: ChannelSmartScheduleStabilityProbing,
		StabilitySince: 1234, StabilityReleaseMaxPromptTokens: releaseLimit,
	}).Error)
	InitChannelCache()
}

func TestChannelSmartScheduleRedisSnapshotPublishesVersionedCompletePayload(t *testing.T) {
	db, redisServer, redisClient := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)

	require.NoError(t, publishChannelSmartScheduleRouteSnapshot(context.Background()))
	pointer, err := redisClient.Get(context.Background(), channelSmartScheduleRouteSnapshotPointerKey).Result()
	require.NoError(t, err)
	assert.Equal(t, "1", pointer)
	versionKey := channelSmartScheduleRouteSnapshotVersionKey(1)
	raw, err := redisClient.Get(context.Background(), versionKey).Result()
	require.NoError(t, err)
	assert.NotContains(t, raw, "sk-sensitive-route-snapshot-key")
	assert.Greater(t, redisClient.TTL(context.Background(), versionKey).Val(), time.Duration(0))
	for _, key := range redisServer.Keys() {
		assert.NotContains(t, key, ":temporary:", "atomic publication must not leave a temporary payload")
	}

	snapshot, err := unmarshalChannelSmartScheduleRouteSnapshot([]byte(raw))
	require.NoError(t, err)
	assert.EqualValues(t, 1, snapshot.Revision)
	assert.Equal(t, snapshot.Revision, snapshot.SourceWatermark)
	require.Len(t, snapshot.Routes["vip"]["model-a"], 1)
	route := snapshot.Routes["vip"]["model-a"][0]
	assert.Equal(t, 9701, route.ChannelID)
	assert.EqualValues(t, 37, route.Priority)
	assert.EqualValues(t, 61, route.Weight)
	assert.True(t, route.Participates)
	assert.Equal(t, ChannelSmartScheduleStabilityProbing, route.StabilityState)
	assert.Equal(t, 4096, route.StabilityReleaseMaxPromptTokens)
}

func TestChannelSmartScheduleRedisSnapshotLoadsAcrossInstancesAndRejectsCorruption(t *testing.T) {
	db, _, redisClient := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)
	require.NoError(t, publishChannelSmartScheduleRouteSnapshot(context.Background()))

	channelSyncLock.Lock()
	channelSmartScheduleRouteCache = nil
	logicalChannelRuntimeCache = nil
	channelLogicalSmartScheduleRoutingCache = nil
	channelSmartScheduleLocalSnapshotMetadataCache = nil
	channelSyncLock.Unlock()
	require.NoError(t, loadChannelSmartScheduleRouteSnapshot(context.Background()))
	channelSyncLock.RLock()
	require.Len(t, channelSmartScheduleRouteCache["vip"]["model-a"], 1)
	assert.Equal(t, 9701, channelSmartScheduleRouteCache["vip"]["model-a"][0].channelId)
	require.NotNil(t, channelSmartScheduleLocalSnapshotMetadataCache)
	assert.EqualValues(t, 1, channelSmartScheduleLocalSnapshotMetadataCache.Revision)
	channelSyncLock.RUnlock()

	require.NoError(t, redisClient.Set(
		context.Background(), channelSmartScheduleRouteSnapshotVersionKey(2),
		`{"schema_version":1,"revision":2,"generated_at":1,"source_watermark":2,"routes":{},"checksum":"bad"}`,
		time.Hour,
	).Err())
	require.NoError(t, redisClient.Set(
		context.Background(), channelSmartScheduleRouteSnapshotPointerKey, "2", time.Hour,
	).Err())
	err := loadChannelSmartScheduleRouteSnapshot(context.Background())
	assert.ErrorIs(t, err, ErrChannelSmartScheduleRouteSnapshotInvalid)
	channelSyncLock.RLock()
	assert.EqualValues(t, 1, channelSmartScheduleLocalSnapshotMetadataCache.Revision)
	assert.Equal(t, 9701, channelSmartScheduleRouteCache["vip"]["model-a"][0].channelId)
	channelSyncLock.RUnlock()
}

func TestChannelSmartScheduleRedisSnapshotUsesMonitorRoleClients(t *testing.T) {
	db, _, redisClient := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)

	previousWrite := common.RDBMonitorWrite
	previousRead := common.RDBMonitorRead
	t.Cleanup(func() {
		common.RDBMonitorWrite = previousWrite
		common.RDBMonitorRead = previousRead
	})

	unavailableServer := miniredis.RunT(t)
	unavailableClient := redis.NewClient(&redis.Options{Addr: unavailableServer.Addr()})
	require.NoError(t, unavailableClient.Close())

	common.RDB = unavailableClient
	common.RDBMonitorWrite = redisClient
	common.RDBMonitorRead = unavailableClient
	require.NoError(t, publishChannelSmartScheduleRouteSnapshot(context.Background()))

	channelSyncLock.Lock()
	channelSmartScheduleRouteCache = nil
	logicalChannelRuntimeCache = nil
	channelLogicalSmartScheduleRoutingCache = nil
	channelSmartScheduleLocalSnapshotMetadataCache = nil
	channelSyncLock.Unlock()
	common.RDBMonitorWrite = unavailableClient
	common.RDBMonitorRead = redisClient
	require.NoError(t, loadChannelSmartScheduleRouteSnapshot(context.Background()))

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	require.NotNil(t, channelSmartScheduleLocalSnapshotMetadataCache)
	assert.EqualValues(t, 1, channelSmartScheduleLocalSnapshotMetadataCache.Revision)
}

func TestChannelSmartScheduleRedisSnapshotRevisionDoesNotRegressWhenCounterIsMissing(t *testing.T) {
	db, _, redisClient := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)
	require.NoError(t, publishChannelSmartScheduleRouteSnapshot(context.Background()))
	require.NoError(t, redisClient.Set(
		context.Background(), channelSmartScheduleRouteSnapshotPointerKey, "7", time.Hour,
	).Err())
	require.NoError(t, redisClient.Del(
		context.Background(), channelSmartScheduleRouteSnapshotRevisionKey,
	).Err())

	require.NoError(t, publishChannelSmartScheduleRouteSnapshot(context.Background()))
	pointer, err := redisClient.Get(
		context.Background(), channelSmartScheduleRouteSnapshotPointerKey,
	).Int64()
	require.NoError(t, err)
	assert.EqualValues(t, 8, pointer)
	counter, err := redisClient.Get(
		context.Background(), channelSmartScheduleRouteSnapshotRevisionKey,
	).Int64()
	require.NoError(t, err)
	assert.EqualValues(t, 8, counter)
	payload, err := redisClient.Get(
		context.Background(), channelSmartScheduleRouteSnapshotVersionKey(8),
	).Bytes()
	require.NoError(t, err)
	snapshot, err := unmarshalChannelSmartScheduleRouteSnapshot(payload)
	require.NoError(t, err)
	assert.EqualValues(t, 8, snapshot.Revision)
	assert.EqualValues(t, 1, snapshot.SourceWatermark)
}

func TestChannelSmartScheduleRedisSnapshotRejectsWriterAfterLeaseTakeover(t *testing.T) {
	db, _, redisClient := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)
	ctx := context.Background()
	const oldToken = "old-writer"
	require.NoError(t, redisClient.Set(
		ctx, channelSmartScheduleRouteSnapshotLeaseKey, oldToken,
		channelSmartScheduleRouteSnapshotLeaseTTL,
	).Err())

	sourceWatermark, err := currentChannelSmartScheduleRouteSourceWatermark(ctx, redisClient)
	require.NoError(t, err)
	snapshot, err := buildChannelSmartScheduleRouteSnapshot(context.Background())
	require.NoError(t, err)
	snapshot.Revision, err = nextChannelSmartScheduleRouteSnapshotRevision(ctx, redisClient, oldToken)
	require.NoError(t, err)
	snapshot.SourceWatermark = sourceWatermark
	snapshot.GeneratedAt, err = nextChannelSmartScheduleRouteSnapshotGeneratedAt(ctx, redisClient)
	require.NoError(t, err)
	payload, err := marshalChannelSmartScheduleRouteSnapshot(snapshot)
	require.NoError(t, err)
	temporaryKey := channelSmartScheduleRouteSnapshotTemporaryKey(snapshot.Revision, oldToken)
	require.NoError(t, redisClient.Set(
		ctx, temporaryKey, payload, channelSmartScheduleRouteSnapshotTemporaryTTL,
	).Err())

	require.NoError(t, redisClient.Set(
		ctx, channelSmartScheduleRouteSnapshotLeaseKey, "new-writer",
		channelSmartScheduleRouteSnapshotLeaseTTL,
	).Err())
	err = commitChannelSmartScheduleRouteSnapshot(
		ctx,
		redisClient,
		oldToken,
		temporaryKey,
		channelSmartScheduleRouteSnapshotVersionKey(snapshot.Revision),
		snapshot,
	)
	assert.ErrorIs(t, err, ErrChannelSmartScheduleRouteSnapshotUnavailable)
	assert.ErrorIs(t, redisClient.Get(ctx, channelSmartScheduleRouteSnapshotPointerKey).Err(), redis.Nil)
	assert.ErrorIs(t, redisClient.Get(
		ctx, channelSmartScheduleRouteSnapshotVersionKey(snapshot.Revision),
	).Err(), redis.Nil)
}

func TestChannelSmartScheduleRedisSnapshotDirtyWatermarkFencesActiveWriter(t *testing.T) {
	db, _, redisClient := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)
	ctx := context.Background()
	const token = "active-writer"
	require.NoError(t, redisClient.Set(
		ctx, channelSmartScheduleRouteSnapshotLeaseKey, token,
		channelSmartScheduleRouteSnapshotLeaseTTL,
	).Err())

	sourceWatermark, err := currentChannelSmartScheduleRouteSourceWatermark(ctx, redisClient)
	require.NoError(t, err)
	snapshot, err := buildChannelSmartScheduleRouteSnapshot(ctx)
	require.NoError(t, err)
	snapshot.Revision, err = nextChannelSmartScheduleRouteSnapshotRevision(ctx, redisClient, token)
	require.NoError(t, err)
	snapshot.SourceWatermark = sourceWatermark
	snapshot.GeneratedAt, err = nextChannelSmartScheduleRouteSnapshotGeneratedAt(ctx, redisClient)
	require.NoError(t, err)
	payload, err := marshalChannelSmartScheduleRouteSnapshot(snapshot)
	require.NoError(t, err)
	temporaryKey := channelSmartScheduleRouteSnapshotTemporaryKey(snapshot.Revision, token)
	require.NoError(t, redisClient.Set(
		ctx, temporaryKey, payload, channelSmartScheduleRouteSnapshotTemporaryTTL,
	).Err())

	watermark, err := advanceChannelSmartScheduleRouteSourceWatermark(ctx, redisClient)
	require.NoError(t, err)
	assert.Greater(t, watermark, sourceWatermark)
	err = commitChannelSmartScheduleRouteSnapshot(
		ctx,
		redisClient,
		token,
		temporaryKey,
		channelSmartScheduleRouteSnapshotVersionKey(snapshot.Revision),
		snapshot,
	)
	assert.ErrorIs(t, err, ErrChannelSmartScheduleRouteSnapshotUnavailable)
	assert.ErrorIs(t, redisClient.Get(ctx, channelSmartScheduleRouteSnapshotPointerKey).Err(), redis.Nil)
}

func TestChannelSmartScheduleRedisSnapshotLocalApplyIsMonotonic(t *testing.T) {
	db, _, _ := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)
	require.NoError(t, publishChannelSmartScheduleRouteSnapshot(context.Background()))

	channelSyncLock.RLock()
	current := *channelSmartScheduleLocalSnapshotMetadataCache
	currentChannelID := channelSmartScheduleRouteCache["vip"]["model-a"][0].channelId
	channelSyncLock.RUnlock()
	stale := newChannelSmartScheduleRouteSnapshot(
		map[string]map[string][]channelSmartScheduleCachedRoute{
			"vip": {"model-a": {{channelId: 9999, priority: 1, weight: 1, participates: true}}},
		},
		&LogicalChannelRuntimeSnapshot{
			Channels: map[int]LogicalChannelIdentity{},
			Groups:   map[int64]LogicalChannelGroupSnapshot{},
		},
		nil,
	)
	stale.Revision = current.Revision + 1
	stale.SourceWatermark = current.SourceWatermark + 1
	stale.GeneratedAt = current.GeneratedAt - 1
	stale.fromRedis = true
	assert.False(t, applyChannelSmartScheduleRouteSnapshot(stale))

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	assert.Equal(t, current, *channelSmartScheduleLocalSnapshotMetadataCache)
	assert.Equal(t, currentChannelID, channelSmartScheduleRouteCache["vip"]["model-a"][0].channelId)
}

func TestChannelSmartScheduleRedisSnapshotRejectsRegressedPointer(t *testing.T) {
	db, _, redisClient := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)
	ctx := context.Background()
	require.NoError(t, publishChannelSmartScheduleRouteSnapshot(ctx))
	require.NoError(t, publishChannelSmartScheduleRouteSnapshot(ctx))
	require.NoError(t, redisClient.Set(
		ctx, channelSmartScheduleRouteSnapshotPointerKey, "1", channelSmartScheduleRouteSnapshotTTL,
	).Err())

	err := loadChannelSmartScheduleRouteSnapshot(ctx)
	assert.ErrorIs(t, err, ErrChannelSmartScheduleRouteSnapshotInvalid)
	assert.True(t, channelSmartScheduleRouteSnapshotIsDirty())
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	assert.EqualValues(t, 2, channelSmartScheduleLocalSnapshotMetadataCache.Revision)
	assert.Equal(t, 9701, channelSmartScheduleRouteCache["vip"]["model-a"][0].channelId)
}

func TestChannelSmartScheduleRedisSnapshotRebuildsWhenSourceWatermarkAdvances(t *testing.T) {
	db, _, redisClient := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)
	ctx := context.Background()
	require.NoError(t, publishChannelSmartScheduleRouteSnapshot(ctx))
	require.NoError(t, redisClient.Incr(ctx, channelSmartScheduleRouteSnapshotWatermarkKey).Err())

	err := loadChannelSmartScheduleRouteSnapshot(ctx)
	assert.ErrorIs(t, err, ErrChannelSmartScheduleRouteSnapshotUnavailable)
	assert.True(t, channelSmartScheduleRouteSnapshotIsDirty())
	require.NoError(t, publishChannelSmartScheduleRouteSnapshot(ctx))
	status := GetChannelSmartScheduleRouteSnapshotStatus()
	assert.False(t, status.Dirty)
	assert.EqualValues(t, 2, status.Revision)
	assert.EqualValues(t, 2, status.SourceWatermark)
}

func TestChannelSmartScheduleRedisDisabledUsesBackgroundLocalSnapshots(t *testing.T) {
	db, redisServer, _ := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)
	originalMonitorWrite := common.RDBMonitorWrite
	originalMonitorRead := common.RDBMonitorRead
	originalMonitorConsumer := common.RDBMonitorConsumer
	t.Cleanup(func() {
		common.RDBMonitorWrite = originalMonitorWrite
		common.RDBMonitorRead = originalMonitorRead
		common.RDBMonitorConsumer = originalMonitorConsumer
	})
	redisServer.Close()
	common.RDB = nil
	common.RDBMonitorWrite = nil
	common.RDBMonitorRead = nil
	common.RDBMonitorConsumer = nil

	StartChannelSmartScheduleRefreshWorker()
	require.Eventually(t, func() bool {
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		return channelSmartScheduleLocalSnapshotMetadataCache != nil &&
			channelSmartScheduleLocalSnapshotMetadataCache.Revision > 0 &&
			!channelSmartScheduleLocalSnapshotMetadataCache.FromRedis
	}, 2*time.Second, 10*time.Millisecond)
	channelSyncLock.RLock()
	beforeRevision := channelSmartScheduleLocalSnapshotMetadataCache.Revision
	channelSyncLock.RUnlock()
	require.NoError(t, db.Model(&Ability{}).
		Where(&Ability{ChannelId: 9701, Group: "vip", Model: "model-a"}).
		Update("weight", 97).Error)
	require.NoError(t, RefreshChannelSmartScheduleRoutePoolCache("vip", "model-a"))
	require.Eventually(t, func() bool {
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		return channelSmartScheduleLocalSnapshotMetadataCache.Revision > beforeRevision &&
			channelSmartScheduleRouteCache["vip"]["model-a"][0].weight == 97
	}, 2*time.Second, 10*time.Millisecond)
	status := GetChannelSmartScheduleRouteSnapshotStatus()
	assert.True(t, status.Available)
	assert.False(t, status.RedisBacked)
	assert.False(t, status.ProtectionMode)
	beforeLocalRevision := status.Revision
	beforeGeneratedAt := status.GeneratedAt
	beforeWatermark := status.SourceWatermark
	InitChannelCache()
	status = GetChannelSmartScheduleRouteSnapshotStatus()
	assert.Greater(t, status.Revision, beforeLocalRevision)
	assert.Greater(t, status.SourceWatermark, beforeWatermark)
	assert.GreaterOrEqual(t, status.GeneratedAt, beforeGeneratedAt)
	assert.False(t, status.RedisBacked)
}

func TestChannelSmartScheduleRedisSnapshotDoesNotClearDirtyCreatedDuringBuild(t *testing.T) {
	db, _, _ := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)

	snapshot, err := buildChannelSmartScheduleRouteSnapshot(context.Background())
	require.NoError(t, err)
	markChannelSmartScheduleRoutePoolDirty("vip", "model-a")
	applyChannelSmartScheduleRouteSnapshot(snapshot)

	channelSyncLock.RLock()
	_, dirty := channelSmartScheduleRouteCacheDirty[channelSmartScheduleRoutePool{
		group: "vip", model: "model-a",
	}]
	dirtySince := channelSmartScheduleRouteSnapshotDirtySince
	channelSyncLock.RUnlock()
	assert.True(t, dirty)
	assert.Positive(t, dirtySince)
}

func TestChannelSmartScheduleRedisOutageUsesLastSnapshotThenProtectsWithoutDBFallback(t *testing.T) {
	db, redisServer, _ := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)
	StartChannelSmartScheduleRefreshWorker()
	require.Eventually(t, func() bool {
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		return channelSmartScheduleLocalSnapshotMetadataCache != nil &&
			channelSmartScheduleLocalSnapshotMetadataCache.FromRedis
	}, 2*time.Second, 10*time.Millisecond)
	redisServer.Close()
	assert.Error(t, loadChannelSmartScheduleRouteSnapshot(context.Background()))
	status := GetChannelSmartScheduleRouteSnapshotStatus()
	assert.True(t, status.Available)
	assert.True(t, status.RedisBacked)
	assert.True(t, status.Degraded)
	assert.False(t, status.ProtectionMode)
	assert.EqualValues(t, 1, status.Revision)
	assert.EqualValues(t, 1, status.SourceWatermark)
	assert.NotEmpty(t, status.LastRedisError)

	queryCount := 0
	callbackName := "test:count_route_snapshot_protection_queries"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(*gorm.DB) {
		queryCount++
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 9701, selected.Id)
	assert.Zero(t, queryCount, "Redis outage must not cause request-path database fallback")

	channelSyncLock.Lock()
	channelSmartScheduleLocalSnapshotMetadataCache.GeneratedAt = time.Now().Add(-2 * time.Hour).UnixMilli()
	channelSyncLock.Unlock()
	t.Setenv("CHANNEL_SMART_SCHEDULE_ROUTE_SNAPSHOT_MAX_AGE_SECONDS", "60")
	selected, err = GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
	assert.ErrorIs(t, err, ErrChannelSmartScheduleRouteSnapshotUnavailable)
	assert.Nil(t, selected)
	assert.Zero(t, queryCount, "stale protection mode must fail fast without DB queries")
	status = GetChannelSmartScheduleRouteSnapshotStatus()
	assert.True(t, status.Stale)
	assert.True(t, status.ProtectionMode)
	assert.GreaterOrEqual(t, status.SnapshotAgeSeconds, int64(60))

	channelSyncLock.Lock()
	channelSmartScheduleRouteCache = nil
	channelSmartScheduleLocalSnapshotMetadataCache = nil
	channelSyncLock.Unlock()
	selected, err = GetRandomSatisfiedChannel("vip", "model-a", 0, nil)
	assert.ErrorIs(t, err, ErrChannelSmartScheduleRouteSnapshotUnavailable)
	assert.Nil(t, selected)
	assert.Zero(t, queryCount)
}

func TestChannelSmartScheduleRedisSnapshotChecksumDetectsPayloadMutation(t *testing.T) {
	snapshot := newChannelSmartScheduleRouteSnapshot(
		map[string]map[string][]channelSmartScheduleCachedRoute{
			"vip": {"model-a": {{channelId: 7, priority: 3, weight: 10, participates: true}}},
		},
		&LogicalChannelRuntimeSnapshot{
			Channels: map[int]LogicalChannelIdentity{7: {ChannelID: 7, LogicalChannelID: 7}},
			Groups:   map[int64]LogicalChannelGroupSnapshot{},
		},
		nil,
	)
	snapshot.Revision = 1
	snapshot.SourceWatermark = 1
	payload, err := marshalChannelSmartScheduleRouteSnapshot(snapshot)
	require.NoError(t, err)
	mutated := strings.Replace(string(payload), `"priority":3`, `"priority":4`, 1)
	_, err = unmarshalChannelSmartScheduleRouteSnapshot([]byte(mutated))
	assert.True(t, errors.Is(err, ErrChannelSmartScheduleRouteSnapshotInvalid))
}

func TestChannelSmartScheduleRefreshQueueDeduplicatesSamePool(t *testing.T) {
	channelSmartScheduleRefreshWorker.mu.Lock()
	require.False(t, channelSmartScheduleRefreshWorker.started)
	channelSmartScheduleRefreshWorker.started = true
	channelSmartScheduleRefreshWorker.stopping = false
	channelSmartScheduleRefreshWorker.queue = make(chan channelSmartScheduleRefreshKey, 8)
	channelSmartScheduleRefreshWorker.stop = make(chan struct{})
	channelSmartScheduleRefreshWorker.done = make(chan struct{})
	channelSmartScheduleRefreshWorker.pending = make(map[channelSmartScheduleRefreshKey]struct{})
	channelSmartScheduleRefreshWorker.mu.Unlock()
	t.Cleanup(func() {
		channelSmartScheduleRefreshWorker.mu.Lock()
		channelSmartScheduleRefreshWorker.started = false
		channelSmartScheduleRefreshWorker.stopping = false
		channelSmartScheduleRefreshWorker.queue = nil
		channelSmartScheduleRefreshWorker.stop = nil
		channelSmartScheduleRefreshWorker.done = nil
		channelSmartScheduleRefreshWorker.pending = nil
		channelSmartScheduleRefreshWorker.mu.Unlock()
	})

	key := channelSmartScheduleRefreshKey{group: "vip", model: "model-a"}
	for index := 0; index < 100; index++ {
		assert.True(t, enqueueChannelSmartScheduleRefresh(key))
	}
	channelSmartScheduleRefreshWorker.mu.Lock()
	assert.Len(t, channelSmartScheduleRefreshWorker.pending, 1)
	assert.Len(t, channelSmartScheduleRefreshWorker.queue, 1)
	channelSmartScheduleRefreshWorker.mu.Unlock()
}

func TestChannelSmartScheduleRefreshWorkerStopIsIdempotent(t *testing.T) {
	db, _, _ := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)
	StartChannelSmartScheduleRefreshWorker()
	require.NoError(t, StopChannelSmartScheduleRefreshWorker(context.Background()))
	require.NoError(t, StopChannelSmartScheduleRefreshWorker(context.Background()))
}

func TestChannelSmartScheduleRefreshWorkerStopCancelsActiveRebuild(t *testing.T) {
	db, redisServer, _ := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)
	originalMonitorWrite := common.RDBMonitorWrite
	originalMonitorRead := common.RDBMonitorRead
	originalMonitorConsumer := common.RDBMonitorConsumer
	t.Cleanup(func() {
		common.RDBMonitorWrite = originalMonitorWrite
		common.RDBMonitorRead = originalMonitorRead
		common.RDBMonitorConsumer = originalMonitorConsumer
	})
	redisServer.Close()
	common.RDB = nil
	common.RDBMonitorWrite = nil
	common.RDBMonitorRead = nil
	common.RDBMonitorConsumer = nil

	queryStarted := make(chan struct{})
	var startOnce sync.Once
	callbackName := "test:block_route_snapshot_rebuild_until_shutdown"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		startOnce.Do(func() { close(queryStarted) })
		<-tx.Statement.Context.Done()
		tx.AddError(tx.Statement.Context.Err())
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	StartChannelSmartScheduleRefreshWorker()
	select {
	case <-queryStarted:
	case <-time.After(time.Second):
		t.Fatal("route snapshot rebuild did not start")
	}
	stopContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, StopChannelSmartScheduleRefreshWorker(stopContext))
}

func TestChannelSmartScheduleRefreshWorkerRenewsAgingSnapshot(t *testing.T) {
	db, _, redisClient := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)
	require.NoError(t, publishChannelSmartScheduleRouteSnapshot(context.Background()))
	t.Setenv("CHANNEL_SMART_SCHEDULE_ROUTE_SNAPSHOT_MAX_AGE_SECONDS", "2")
	channelSyncLock.Lock()
	channelSmartScheduleLocalSnapshotMetadataCache.GeneratedAt = time.Now().Add(-time.Second).UnixMilli()
	channelSyncLock.Unlock()

	StartChannelSmartScheduleRefreshWorker()
	require.Eventually(t, func() bool {
		revision, err := redisClient.Get(
			context.Background(), channelSmartScheduleRouteSnapshotPointerKey,
		).Int64()
		return err == nil && revision >= 2
	}, 3*time.Second, 20*time.Millisecond)
	assert.True(t, channelSmartScheduleRouteSnapshotUsable(time.Now()))
}

func TestChannelSmartScheduleRefreshWorkerRetriesDirtyWatermark(t *testing.T) {
	db, _, redisClient := setupChannelSmartScheduleRedisSnapshotTest(t)
	seedChannelSmartScheduleRedisSnapshotTest(t, db)
	require.NoError(t, publishChannelSmartScheduleRouteSnapshot(context.Background()))
	channelSyncLock.Lock()
	channelSmartScheduleRouteSnapshotDirtySince = time.Now().Add(time.Millisecond).UnixMilli()
	logicalChannelRuntimeDirty = false
	channelSmartScheduleRouteCacheDirty = make(map[channelSmartScheduleRoutePool]struct{})
	channelSyncLock.Unlock()

	StartChannelSmartScheduleRefreshWorker()
	require.Eventually(t, func() bool {
		revision, err := redisClient.Get(
			context.Background(), channelSmartScheduleRouteSnapshotPointerKey,
		).Int64()
		return err == nil && revision >= 2
	}, 3*time.Second, 20*time.Millisecond)
	assert.False(t, channelSmartScheduleRouteSnapshotIsDirty())
}
