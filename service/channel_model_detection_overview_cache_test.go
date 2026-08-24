package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withChannelModelDetectionOverviewCacheDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	previous := model.DB
	model.DB = db
	resetChannelModelDetectionOverviewCacheForTest()
	t.Cleanup(func() {
		resetChannelModelDetectionOverviewCacheForTest()
		model.DB = previous
	})
}

func resetChannelModelDetectionOverviewCacheForTest() {
	channelModelDetectionOverviewRefreshSchedule.Lock()
	if channelModelDetectionOverviewRefreshSchedule.timer != nil {
		channelModelDetectionOverviewRefreshSchedule.timer.Stop()
	}
	channelModelDetectionOverviewRefreshSchedule.timer = nil
	channelModelDetectionOverviewRefreshSchedule.db = nil
	channelModelDetectionOverviewRefreshSchedule.Unlock()
	channelModelDetectionOverviewCache.Lock()
	channelModelDetectionOverviewCacheGeneration.Add(1)
	channelModelDetectionOverviewCache.items = make(map[*gorm.DB]channelModelDetectionOverviewCacheEntry)
	channelModelDetectionOverviewCache.buildBackoff = make(map[*gorm.DB]time.Time)
	channelModelDetectionOverviewCache.Unlock()
}

func useChannelModelDetectionOverviewRedisTestClient(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousEnabled := common.RedisEnabled
	previousClient := common.RDB
	previousReadClient := common.RDBMonitorRead
	previousWriteClient := common.RDBMonitorWrite
	common.RedisEnabled = true
	common.RDB = client
	common.RDBMonitorRead = client
	common.RDBMonitorWrite = client
	t.Cleanup(func() {
		common.RedisEnabled = previousEnabled
		common.RDB = previousClient
		common.RDBMonitorRead = previousReadClient
		common.RDBMonitorWrite = previousWriteClient
		_ = client.Close()
	})
	return server, client
}

func seedCachedChannelModelDetectionOverview(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Create(&model.ChannelModelDetectionGlobalConfig{
		DetectorURL:     "http://127.0.0.1:18080/private",
		ScheduledPreset: model.ChannelModelDetectionPresetMedium,
		IntervalHours:   24, ScheduleTime: "02:30", Timezone: "Asia/Shanghai", Revision: 1,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1801, Name: "cached-overview", Key: "cached-overview-secret",
		Models: "gpt-5.6-sol", Status: common.ChannelStatusEnabled,
	}).Error)
}

func TestCachedChannelModelDetectionOverviewReturnsGeneratedMetadataAndReusesFreshSnapshot(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	seedCachedChannelModelDetectionOverview(t, db)
	t.Setenv("CHANNEL_MODEL_DETECTION_OVERVIEW_CACHE_TTL_MS", "1000")

	first, err := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
	require.NoError(t, err)
	second, err := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
	require.NoError(t, err)

	assert.NotZero(t, first.GeneratedAt)
	assert.Equal(t, channelModelDetectionOverviewSnapshotVersion, first.SnapshotVersion)
	assert.Positive(t, first.SnapshotRevision)
	assert.Positive(t, first.DataCutoffAt)
	assert.GreaterOrEqual(t, first.ServerNow, first.DataCutoffAt)
	assert.Zero(t, first.SnapshotAgeSeconds)
	assert.False(t, first.Stale)
	assert.Equal(t, first.GeneratedAt, second.GeneratedAt)
	assert.Equal(t, first.EventWatermark, second.EventWatermark)
	assert.False(t, second.Stale)
	encoded, err := common.Marshal(second)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "cached-overview-secret")
}

func TestCachedChannelModelDetectionOverviewStablePollingDoesNotQueryDatabase(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	seedCachedChannelModelDetectionOverview(t, db)
	t.Setenv("CHANNEL_MODEL_DETECTION_OVERVIEW_CACHE_TTL_MS", "10")
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })

	_, err := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
	require.NoError(t, err)
	var queryCount atomic.Int64
	callbackName := "test:model_detection_overview_stable_poll"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(*gorm.DB) {
		queryCount.Add(1)
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	time.Sleep(20 * time.Millisecond)

	for range 100 {
		response, queryErr := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
		require.NoError(t, queryErr)
		assert.False(t, response.Stale)
	}
	assert.Zero(t, queryCount.Load(), "stable one-second polling must only read the complete local snapshot")
}

func TestCachedChannelModelDetectionOverviewServesStaleWhileRefreshRunsInBackground(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	seedCachedChannelModelDetectionOverview(t, db)
	t.Setenv("CHANNEL_MODEL_DETECTION_OVERVIEW_CACHE_TTL_MS", "1")

	first, err := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
	require.NoError(t, err)
	channelModelDetectionOverviewCache.Lock()
	entry := channelModelDetectionOverviewCache.items[db]
	entry.expiresAt = time.Now().Add(-time.Second)
	channelModelDetectionOverviewCache.items[db] = entry
	channelModelDetectionOverviewCache.Unlock()
	InvalidateChannelModelDetectionOverviewCache()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	callbackName := "test:block_model_detection_overview_refresh"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(*gorm.DB) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
	}))
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		_ = db.Callback().Query().Remove(callbackName)
	})

	startedAt := time.Now()
	stale, err := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
	elapsed := time.Since(startedAt)
	require.NoError(t, err)
	assert.True(t, stale.Stale)
	assert.Equal(t, first.GeneratedAt, stale.GeneratedAt)
	assert.Less(t, elapsed, 100*time.Millisecond)

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("model detection overview refresh did not start asynchronously")
	}
	releaseOnce.Do(func() { close(release) })
	require.Eventually(t, func() bool {
		entry, exists, _ := loadChannelModelDetectionOverviewCacheEntry(db, time.Now())
		return exists && entry.snapshot.Revision > first.SnapshotRevision
	}, 2*time.Second, 10*time.Millisecond)
}

func TestCachedChannelModelDetectionOverviewUsesSharedRedisSnapshotAfterLocalLoss(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	seedCachedChannelModelDetectionOverview(t, db)
	server, _ := useChannelModelDetectionOverviewRedisTestClient(t)
	t.Setenv("CHANNEL_MODEL_DETECTION_OVERVIEW_CACHE_TTL_MS", "1000")

	first, err := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
	require.NoError(t, err)
	assert.True(t, server.Exists(channelModelDetectionOverviewRedisKey))

	resetChannelModelDetectionOverviewCacheForTest()
	var queryCount atomic.Int64
	callbackName := "test:model_detection_overview_redis_hit"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(*gorm.DB) {
		queryCount.Add(1)
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	second, err := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
	require.NoError(t, err)
	assert.Equal(t, first.SnapshotRevision, second.SnapshotRevision)
	assert.Equal(t, first.GeneratedAt, second.GeneratedAt)
	assert.Zero(t, queryCount.Load(), "Redis snapshot hit must not query the database")
}

func TestWarmChannelModelDetectionOverviewUsesSharedRedisSnapshotWithoutDatabaseBuild(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	seedCachedChannelModelDetectionOverview(t, db)
	_, _ = useChannelModelDetectionOverviewRedisTestClient(t)
	t.Setenv("CHANNEL_MODEL_DETECTION_OVERVIEW_CACHE_TTL_MS", "1000")

	first, err := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
	require.NoError(t, err)
	resetChannelModelDetectionOverviewCacheForTest()
	var queryCount atomic.Int64
	callbackName := "test:model_detection_overview_warm_redis_hit"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(*gorm.DB) {
		queryCount.Add(1)
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	WarmChannelModelDetectionOverviewSnapshot()
	entry, exists, stale := loadChannelModelDetectionOverviewCacheEntry(db, time.Now())
	require.True(t, exists)
	assert.False(t, stale)
	assert.Equal(t, first.SnapshotRevision, entry.snapshot.Revision)
	assert.Zero(t, queryCount.Load(), "startup warm must reuse an existing shared snapshot")
}

func TestRefreshChannelModelDetectionOverviewRebuildsAfterLocalLoss(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	seedCachedChannelModelDetectionOverview(t, db)
	_, _ = useChannelModelDetectionOverviewRedisTestClient(t)
	t.Setenv("CHANNEL_MODEL_DETECTION_OVERVIEW_CACHE_TTL_MS", "1000")

	first, err := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
	require.NoError(t, err)
	resetChannelModelDetectionOverviewCacheForTest()
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1801).Update("name", "refreshed-overview").Error)

	InvalidateChannelModelDetectionOverviewCache()
	RefreshChannelModelDetectionOverviewSnapshot()

	var refreshed channelModelDetectionOverviewSnapshot
	require.Eventually(t, func() bool {
		snapshot, exists := loadChannelModelDetectionOverviewRedisSnapshot()
		if !exists || snapshot.Revision <= first.SnapshotRevision {
			return false
		}
		for _, channel := range snapshot.Response.Channels {
			if channel.ID == 1801 && channel.Name == "refreshed-overview" {
				refreshed = snapshot
				return true
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond)
	assert.Greater(t, refreshed.Revision, first.SnapshotRevision)
}

func TestRefreshChannelModelDetectionOverviewWaitsForLeaseAndBuildsCurrentGeneration(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	seedCachedChannelModelDetectionOverview(t, db)
	_, _ = useChannelModelDetectionOverviewRedisTestClient(t)
	t.Setenv("CHANNEL_MODEL_DETECTION_OVERVIEW_CACHE_TTL_MS", "1000")

	first, err := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
	require.NoError(t, err)
	oldGeneration := channelModelDetectionOverviewCacheGeneration.Load()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var blocked atomic.Bool
	callbackName := "test:model_detection_overview_force_waits_for_lease"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(*gorm.DB) {
		if blocked.CompareAndSwap(false, true) {
			entered <- struct{}{}
			<-release
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	oldBuild := make(chan error, 1)
	go func() {
		_, buildErr := buildChannelModelDetectionOverviewSnapshot(
			db, oldGeneration, first.SnapshotRevision, first.EventWatermark, false,
		)
		oldBuild <- buildErr
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("old generation build did not acquire the shared lease")
	}
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1801).Update("name", "current-generation").Error)
	InvalidateChannelModelDetectionOverviewCache()
	RefreshChannelModelDetectionOverviewSnapshot()
	close(release)
	require.ErrorIs(t, <-oldBuild, ErrChannelModelDetectionOverviewSnapshotUnavailable)

	require.Eventually(t, func() bool {
		snapshot, exists := loadChannelModelDetectionOverviewRedisSnapshot()
		if !exists || snapshot.Revision <= first.SnapshotRevision {
			return false
		}
		for _, channel := range snapshot.Response.Channels {
			if channel.ID == 1801 && channel.Name == "current-generation" {
				return true
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond)
}

func TestCachedChannelModelDetectionOverviewDoesNotFallBackToDatabaseWhenRedisIsUnavailable(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	seedCachedChannelModelDetectionOverview(t, db)
	server, _ := useChannelModelDetectionOverviewRedisTestClient(t)
	t.Setenv("CHANNEL_MODEL_DETECTION_OVERVIEW_CACHE_TTL_MS", "1000")

	first, err := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
	require.NoError(t, err)
	channelModelDetectionOverviewCache.Lock()
	entry := channelModelDetectionOverviewCache.items[db]
	entry.expiresAt = time.Now().Add(-time.Second)
	channelModelDetectionOverviewCache.items[db] = entry
	channelModelDetectionOverviewCache.Unlock()
	server.Close()
	var queryCount atomic.Int64
	callbackName := "test:model_detection_overview_no_database_fallback"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(*gorm.DB) {
		queryCount.Add(1)
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	response, err := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
	require.NoError(t, err)
	assert.True(t, response.Stale)
	assert.Equal(t, first.SnapshotRevision, response.SnapshotRevision)
	assert.Equal(t, first.GeneratedAt, response.GeneratedAt)
	assert.Zero(t, queryCount.Load(), "an overview poll must not rebuild from DB while Redis is unavailable")
}

func TestChannelModelDetectionOverviewRedisLeaseAllowsOnlyOneCrossInstanceBuild(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	seedCachedChannelModelDetectionOverview(t, db)
	server, _ := useChannelModelDetectionOverviewRedisTestClient(t)
	generation := channelModelDetectionOverviewCacheGeneration.Load()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var blocked atomic.Bool
	callbackName := "test:model_detection_overview_cross_instance_lease"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(*gorm.DB) {
		if blocked.CompareAndSwap(false, true) {
			entered <- struct{}{}
			<-release
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	type result struct {
		snapshot channelModelDetectionOverviewSnapshot
		err      error
	}
	results := make(chan result, 2)
	go func() {
		snapshot, err := buildChannelModelDetectionOverviewSnapshot(db, generation, 0, 0, false)
		results <- result{snapshot: snapshot, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first snapshot build did not reach the database")
	}
	commandCount := server.CommandCount()
	go func() {
		snapshot, err := buildChannelModelDetectionOverviewSnapshot(db, generation, 0, 0, false)
		results <- result{snapshot: snapshot, err: err}
	}()
	require.Eventually(t, func() bool {
		return server.CommandCount() > commandCount
	}, time.Second, 5*time.Millisecond, "second instance did not attempt the shared Redis lease")
	close(release)

	first := <-results
	second := <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	assert.Positive(t, first.snapshot.Revision)
	assert.Equal(t, first.snapshot.Revision, second.snapshot.Revision)
}

func TestChannelModelDetectionOverviewRedisFencingRejectsExpiredLeaseWriter(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	server, _ := useChannelModelDetectionOverviewRedisTestClient(t)

	oldLease, acquired, err := acquireChannelModelDetectionOverviewBuildLease()
	require.NoError(t, err)
	require.True(t, acquired)
	server.FastForward(channelModelDetectionOverviewBuildLeaseTTL + time.Second)
	newLease, acquired, err := acquireChannelModelDetectionOverviewBuildLease()
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Greater(t, newLease.fencingToken, oldLease.fencingToken)

	cutoff := time.Now().Add(-time.Second)
	generated := cutoff.Add(100 * time.Millisecond)
	newSnapshot := channelModelDetectionOverviewSnapshot{
		Version: channelModelDetectionOverviewSnapshotVersion, Revision: newLease.fencingToken,
		EventWatermark: newLease.eventWatermark, GeneratedAt: generated.Unix(), GeneratedAtUnixMillis: generated.UnixMilli(),
		DataCutoffAt: cutoff.Unix(), DataCutoffAtUnixMillis: cutoff.UnixMilli(), Response: ChannelModelDetectionOverviewResponse{},
	}
	published, err := storeChannelModelDetectionOverviewRedisSnapshot(newLease, newSnapshot)
	require.NoError(t, err)
	require.True(t, published)

	oldSnapshot := newSnapshot
	oldSnapshot.Revision = oldLease.fencingToken
	oldSnapshot.EventWatermark = oldLease.eventWatermark
	oldSnapshot.GeneratedAtUnixMillis--
	oldSnapshot.DataCutoffAtUnixMillis--
	published, err = storeChannelModelDetectionOverviewRedisSnapshot(oldLease, oldSnapshot)
	require.NoError(t, err)
	assert.False(t, published)
	loaded, exists := loadChannelModelDetectionOverviewRedisSnapshot()
	require.True(t, exists)
	assert.Equal(t, newLease.fencingToken, loaded.Revision)
}

func TestChannelModelDetectionOverviewRedisCASRejectsGeneratedTimeRollback(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	_, _ = useChannelModelDetectionOverviewRedisTestClient(t)

	firstLease, acquired, err := acquireChannelModelDetectionOverviewBuildLease()
	require.NoError(t, err)
	require.True(t, acquired)
	cutoff := time.Now().Add(-time.Second)
	generated := cutoff.Add(200 * time.Millisecond)
	current := channelModelDetectionOverviewSnapshot{
		Version: channelModelDetectionOverviewSnapshotVersion, Revision: firstLease.fencingToken,
		EventWatermark: firstLease.eventWatermark, GeneratedAt: generated.Unix(), GeneratedAtUnixMillis: generated.UnixMilli(),
		DataCutoffAt: cutoff.Unix(), DataCutoffAtUnixMillis: cutoff.UnixMilli(), Response: ChannelModelDetectionOverviewResponse{},
	}
	published, err := storeChannelModelDetectionOverviewRedisSnapshot(firstLease, current)
	require.NoError(t, err)
	require.True(t, published)
	releaseChannelModelDetectionOverviewBuildLease(firstLease)

	secondLease, acquired, err := acquireChannelModelDetectionOverviewBuildLease()
	require.NoError(t, err)
	require.True(t, acquired)
	defer releaseChannelModelDetectionOverviewBuildLease(secondLease)
	rollback := current
	rollback.Revision = secondLease.fencingToken
	rollback.EventWatermark = secondLease.eventWatermark
	rollback.GeneratedAtUnixMillis--
	published, err = storeChannelModelDetectionOverviewRedisSnapshot(secondLease, rollback)
	require.NoError(t, err)
	assert.False(t, published)

	loaded, exists := loadChannelModelDetectionOverviewRedisSnapshot()
	require.True(t, exists)
	assert.Equal(t, current.Revision, loaded.Revision)
	assert.Equal(t, current.GeneratedAtUnixMillis, loaded.GeneratedAtUnixMillis)
}

func TestChannelModelDetectionOverviewRedisFencingRejectsSnapshotOutsideLeaseIdentity(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	_, client := useChannelModelDetectionOverviewRedisTestClient(t)

	lease, acquired, err := acquireChannelModelDetectionOverviewBuildLease()
	require.NoError(t, err)
	require.True(t, acquired)
	defer releaseChannelModelDetectionOverviewBuildLease(lease)
	cutoff := time.Now().Add(-time.Second)
	generated := cutoff.Add(100 * time.Millisecond)
	snapshot := channelModelDetectionOverviewSnapshot{
		Version: channelModelDetectionOverviewSnapshotVersion, Revision: lease.fencingToken + 1,
		EventWatermark: lease.eventWatermark, GeneratedAt: generated.Unix(), GeneratedAtUnixMillis: generated.UnixMilli(),
		DataCutoffAt: cutoff.Unix(), DataCutoffAtUnixMillis: cutoff.UnixMilli(), Response: ChannelModelDetectionOverviewResponse{},
	}
	published, err := storeChannelModelDetectionOverviewRedisSnapshot(lease, snapshot)
	require.NoError(t, err)
	assert.False(t, published)

	snapshot.Revision = lease.fencingToken
	snapshot.EventWatermark++
	published, err = storeChannelModelDetectionOverviewRedisSnapshot(lease, snapshot)
	require.NoError(t, err)
	assert.False(t, published)
	assert.Zero(t, client.Exists(context.Background(), channelModelDetectionOverviewRedisKey).Val())
}

func TestChannelModelDetectionOverviewRedisWatermarkRejectsPreMutationBuild(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	_, _ = useChannelModelDetectionOverviewRedisTestClient(t)

	oldLease, acquired, err := acquireChannelModelDetectionOverviewBuildLease()
	require.NoError(t, err)
	require.True(t, acquired)
	watermark := advanceChannelModelDetectionOverviewEventWatermark()
	assert.Greater(t, watermark, oldLease.eventWatermark)
	cutoff := time.Now().Add(-time.Second)
	generated := cutoff.Add(100 * time.Millisecond)
	oldSnapshot := channelModelDetectionOverviewSnapshot{
		Version: channelModelDetectionOverviewSnapshotVersion, Revision: oldLease.fencingToken,
		EventWatermark: oldLease.eventWatermark, GeneratedAt: generated.Unix(), GeneratedAtUnixMillis: generated.UnixMilli(),
		DataCutoffAt: cutoff.Unix(), DataCutoffAtUnixMillis: cutoff.UnixMilli(), Response: ChannelModelDetectionOverviewResponse{},
	}
	published, err := storeChannelModelDetectionOverviewRedisSnapshot(oldLease, oldSnapshot)
	require.NoError(t, err)
	assert.False(t, published)
	releaseChannelModelDetectionOverviewBuildLease(oldLease)

	newLease, acquired, err := acquireChannelModelDetectionOverviewBuildLease()
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, watermark, newLease.eventWatermark)
}

func TestLoadChannelModelDetectionOverviewRedisSnapshotRejectsStaleWatermark(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	_, client := useChannelModelDetectionOverviewRedisTestClient(t)

	lease, acquired, err := acquireChannelModelDetectionOverviewBuildLease()
	require.NoError(t, err)
	require.True(t, acquired)
	cutoff := time.Now().Add(-time.Second)
	generated := cutoff.Add(100 * time.Millisecond)
	snapshot := channelModelDetectionOverviewSnapshot{
		Version: channelModelDetectionOverviewSnapshotVersion, Revision: lease.fencingToken,
		EventWatermark: lease.eventWatermark, GeneratedAt: generated.Unix(), GeneratedAtUnixMillis: generated.UnixMilli(),
		DataCutoffAt: cutoff.Unix(), DataCutoffAtUnixMillis: cutoff.UnixMilli(), Response: ChannelModelDetectionOverviewResponse{},
	}
	ctx := context.Background()
	published, err := storeChannelModelDetectionOverviewRedisSnapshot(lease, snapshot)
	require.NoError(t, err)
	require.True(t, published)
	require.NoError(t, client.Set(ctx, channelModelDetectionOverviewWatermarkKey, snapshot.EventWatermark+1, 0).Err())
	_, exists := loadChannelModelDetectionOverviewRedisSnapshot()
	assert.False(t, exists)
}

func TestInvalidateChannelModelDetectionOverviewDoesNotExtendMaximumStaleDeadline(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	seedCachedChannelModelDetectionOverview(t, db)
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })
	t.Setenv("CHANNEL_MODEL_DETECTION_OVERVIEW_STALE_TTL_MS", "1000")

	_, err := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
	require.NoError(t, err)
	channelModelDetectionOverviewCache.RLock()
	originalDeadline := channelModelDetectionOverviewCache.items[db].staleUntil
	channelModelDetectionOverviewCache.RUnlock()

	InvalidateChannelModelDetectionOverviewCache()
	InvalidateChannelModelDetectionOverviewCache()
	channelModelDetectionOverviewCache.RLock()
	invalidated := channelModelDetectionOverviewCache.items[db]
	channelModelDetectionOverviewCache.RUnlock()
	assert.Equal(t, originalDeadline, invalidated.staleUntil)
	_, exists, _ := loadChannelModelDetectionOverviewCacheEntry(db, originalDeadline.Add(time.Nanosecond))
	assert.False(t, exists)
}

func TestNotifyChannelModelDetectionOverviewChangedCoalescesRebuilds(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	seedCachedChannelModelDetectionOverview(t, db)
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })

	first, err := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1801).Update("name", "coalesced-overview").Error)
	for range 10 {
		NotifyChannelModelDetectionOverviewChanged()
	}

	require.Eventually(t, func() bool {
		entry, exists, _ := loadChannelModelDetectionOverviewCacheEntry(db, time.Now())
		return exists && entry.snapshot.Revision > first.SnapshotRevision
	}, 3*time.Second, 10*time.Millisecond)
	channelModelDetectionOverviewCache.RLock()
	refreshed := channelModelDetectionOverviewCache.items[db].snapshot
	channelModelDetectionOverviewCache.RUnlock()
	assert.Equal(t, first.SnapshotRevision+1, refreshed.Revision)
	assert.Equal(t, first.EventWatermark+10, refreshed.EventWatermark)
	assert.Equal(t, "coalesced-overview", refreshed.Response.Channels[0].Name)
}

func TestStoreChannelModelDetectionOverviewCacheRejectsMonotonicRegression(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	generated := time.Now().Add(-time.Second)
	cutoff := generated.Add(-time.Second)
	base := channelModelDetectionOverviewSnapshot{
		Version: channelModelDetectionOverviewSnapshotVersion, Revision: 10, EventWatermark: 20,
		GeneratedAt: generated.Unix(), GeneratedAtUnixMillis: generated.UnixMilli(),
		DataCutoffAt: cutoff.Unix(), DataCutoffAtUnixMillis: cutoff.UnixMilli(),
	}
	generation := channelModelDetectionOverviewCacheGeneration.Load()
	require.True(t, storeChannelModelDetectionOverviewCacheEntry(db, generation, base, time.Second, time.Minute))

	tests := []struct {
		name   string
		mutate func(*channelModelDetectionOverviewSnapshot)
	}{
		{name: "revision", mutate: func(snapshot *channelModelDetectionOverviewSnapshot) {
			snapshot.Revision--
		}},
		{name: "event watermark", mutate: func(snapshot *channelModelDetectionOverviewSnapshot) {
			snapshot.Revision++
			snapshot.EventWatermark--
		}},
		{name: "data cutoff", mutate: func(snapshot *channelModelDetectionOverviewSnapshot) {
			snapshot.Revision++
			snapshot.DataCutoffAtUnixMillis--
		}},
		{name: "generated time", mutate: func(snapshot *channelModelDetectionOverviewSnapshot) {
			snapshot.Revision++
			snapshot.GeneratedAtUnixMillis--
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := base
			test.mutate(&candidate)
			assert.False(t, storeChannelModelDetectionOverviewCacheEntry(db, generation, candidate, time.Second, time.Minute))
		})
	}
}

func TestChannelModelDetectionOverviewRejectsFutureGeneratedAt(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	_, client := useChannelModelDetectionOverviewRedisTestClient(t)
	lease, acquired, err := acquireChannelModelDetectionOverviewBuildLease()
	require.NoError(t, err)
	require.True(t, acquired)
	defer releaseChannelModelDetectionOverviewBuildLease(lease)

	future := time.Now().Add(6 * time.Second)
	cutoff := time.Now()
	snapshot := channelModelDetectionOverviewSnapshot{
		Version: channelModelDetectionOverviewSnapshotVersion, Revision: lease.fencingToken,
		EventWatermark: lease.eventWatermark, GeneratedAt: future.Unix(), GeneratedAtUnixMillis: future.UnixMilli(),
		DataCutoffAt: cutoff.Unix(), DataCutoffAtUnixMillis: cutoff.UnixMilli(),
		Response: ChannelModelDetectionOverviewResponse{},
	}
	published, err := storeChannelModelDetectionOverviewRedisSnapshot(lease, snapshot)
	require.NoError(t, err)
	assert.False(t, published)

	_, exists := loadChannelModelDetectionOverviewRedisSnapshot()
	assert.False(t, exists)
	assert.False(t, storeChannelModelDetectionOverviewCacheEntry(
		db, channelModelDetectionOverviewCacheGeneration.Load(), snapshot, time.Second, time.Minute,
	))
	assert.Zero(t, client.Exists(context.Background(), channelModelDetectionOverviewRedisKey).Val())
}

func TestCachedChannelModelDetectionOverviewConcurrentMissUsesOneLocalBuild(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	withChannelModelDetectionOverviewCacheDB(t, db)
	seedCachedChannelModelDetectionOverview(t, db)
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var queryCount atomic.Int64
	callbackName := "test:model_detection_overview_local_singleflight"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(*gorm.DB) {
		if queryCount.Add(1) == 1 {
			entered <- struct{}{}
			<-release
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	const callers = 20
	responses := make(chan ChannelModelDetectionOverviewResponse, callers)
	errorsFound := make(chan error, callers)
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(callers)
	for range callers {
		go func() {
			ready.Done()
			<-start
			response, err := GetCachedChannelModelDetectionOverview(context.Background(), common.GetTimestamp())
			responses <- response
			errorsFound <- err
		}()
	}
	ready.Wait()
	close(start)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("snapshot build did not reach the database")
	}
	assert.Equal(t, int64(1), queryCount.Load(), "concurrent misses must join the first database build")
	close(release)

	var revision uint64
	for range callers {
		require.NoError(t, <-errorsFound)
		response := <-responses
		if revision == 0 {
			revision = response.SnapshotRevision
		}
		assert.Equal(t, revision, response.SnapshotRevision)
	}
}
