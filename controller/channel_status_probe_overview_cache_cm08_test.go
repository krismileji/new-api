package controller

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type cm08RedisFailureHook struct {
	failures *atomic.Int64
	err      error
}

func (hook cm08RedisFailureHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	hook.failures.Add(1)
	return ctx, hook.err
}

func (cm08RedisFailureHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (hook cm08RedisFailureHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, hook.err
}

func (cm08RedisFailureHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func TestCM08ChannelStatusProbeOverviewConcurrentStaleReadsSurviveRedisFailureAndCoalesceRefresh(t *testing.T) {
	t.Setenv("CHANNEL_STATUS_PROBE_OVERVIEW_CACHE_TTL_MS", "10000")
	t.Setenv("CHANNEL_STATUS_PROBE_OVERVIEW_STALE_TTL_MS", "10000")
	setupChannelStatusProbeControllerTest(t)

	initial := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	require.False(t, initial.Stale)
	require.Positive(t, initial.GeneratedAt)

	generation := channelStatusProbeOverviewCacheGeneration.Load()
	key := channelStatusProbeOverviewCacheKey{db: model.DB, generation: generation}
	channelStatusProbeOverviewCache.Lock()
	entry, exists := channelStatusProbeOverviewCache.items[key]
	if exists {
		entry.expiresAt = time.Now().Add(-time.Second)
		entry.staleUntil = time.Now().Add(time.Minute)
		channelStatusProbeOverviewCache.items[key] = entry
	}
	channelStatusProbeOverviewCache.Unlock()
	require.True(t, exists)

	previousRead := common.RDBMonitorRead
	previousWrite := common.RDBMonitorWrite
	var readFailures atomic.Int64
	var writeFailures atomic.Int64
	failingReadRedis := redis.NewClient(&redis.Options{Addr: common.RDB.Options().Addr})
	failingReadRedis.AddHook(cm08RedisFailureHook{
		failures: &readFailures, err: errors.New("cm08 redis unavailable"),
	})
	failingWriteRedis := redis.NewClient(&redis.Options{Addr: common.RDB.Options().Addr})
	failingWriteRedis.AddHook(cm08RedisFailureHook{
		failures: &writeFailures, err: errors.New("cm08 redis unavailable"),
	})
	common.RDBMonitorRead = failingReadRedis
	common.RDBMonitorWrite = failingWriteRedis
	t.Cleanup(func() {
		common.RDBMonitorRead = previousRead
		common.RDBMonitorWrite = previousWrite
		require.NoError(t, failingReadRedis.Close())
		require.NoError(t, failingWriteRedis.Close())
	})

	var rebuilds atomic.Int64
	rebuildEntered := make(chan struct{}, 1)
	releaseRebuild := make(chan struct{})
	var releaseOnce sync.Once
	callbackName := "test:cm08_block_status_probe_overview_rebuild"
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if _, isChannelOverviewQuery := tx.Statement.Dest.(*[]*model.Channel); !isChannelOverviewQuery {
			return
		}
		rebuilds.Add(1)
		select {
		case rebuildEntered <- struct{}{}:
		default:
		}
		<-releaseRebuild
	}))
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseRebuild) })
		require.NoError(t, model.DB.Callback().Query().Remove(callbackName))
	})

	const readers = 16
	type overviewHTTPResult struct {
		status int
		body   []byte
	}
	contexts := make([]func(), 0, readers)
	results := make(chan overviewHTTPResult, readers)
	start := make(chan struct{})
	for range readers {
		ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/status", nil)
		contexts = append(contexts, func() {
			<-start
			GetChannelStatusProbeOverview(ctx)
			results <- overviewHTTPResult{status: recorder.Code, body: append([]byte(nil), recorder.Body.Bytes()...)}
		})
	}
	for _, run := range contexts {
		go run()
	}
	close(start)

	for range readers {
		select {
		case result := <-results:
			require.Equal(t, http.StatusOK, result.status)
			var envelope struct {
				Success bool                               `json:"success"`
				Data    channelStatusProbeOverviewResponse `json:"data"`
			}
			require.NoError(t, common.Unmarshal(result.body, &envelope))
			require.True(t, envelope.Success)
			assert.True(t, envelope.Data.Stale)
			assert.Equal(t, initial.GeneratedAt, envelope.Data.GeneratedAt)
		case <-time.After(2 * time.Second):
			require.FailNow(t, "stale overview read blocked on Redis or database rebuild")
		}
	}

	select {
	case <-rebuildEntered:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "background overview rebuild did not start")
	}
	assert.EqualValues(t, 1, rebuilds.Load())
	assert.EqualValues(t, 1, readFailures.Load())

	releaseOnce.Do(func() { close(releaseRebuild) })
	require.Eventually(t, func() bool {
		cached, cachedExists, stale := loadChannelStatusProbeOverviewCacheEntry(key, time.Now())
		return cachedExists && !stale && cached.generatedAt >= initial.GeneratedAt
	}, 2*time.Second, 5*time.Millisecond)
	assert.EqualValues(t, 1, rebuilds.Load())
	assert.EqualValues(t, 1, writeFailures.Load())
}

func TestCM08ChannelStatusProbeOverviewInvalidatedStaleReadsDoNotBlockOrFanOut(t *testing.T) {
	t.Setenv("CHANNEL_STATUS_PROBE_OVERVIEW_CACHE_TTL_MS", "10000")
	t.Setenv("CHANNEL_STATUS_PROBE_OVERVIEW_STALE_TTL_MS", "10000")
	setupChannelStatusProbeControllerTest(t)

	initial := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	require.False(t, initial.Stale)
	oldKey := channelStatusProbeOverviewCacheKey{
		db: model.DB, generation: channelStatusProbeOverviewCacheGeneration.Load(),
	}
	channelStatusProbeOverviewCache.RLock()
	oldEntry, exists := channelStatusProbeOverviewCache.items[oldKey]
	channelStatusProbeOverviewCache.RUnlock()
	require.True(t, exists)

	invalidateChannelStatusProbeOverviewCache()
	key := channelStatusProbeOverviewCacheKey{
		db: model.DB, generation: channelStatusProbeOverviewCacheGeneration.Load(),
	}
	channelStatusProbeOverviewCache.RLock()
	invalidatedEntry, exists := channelStatusProbeOverviewCache.items[key]
	channelStatusProbeOverviewCache.RUnlock()
	require.True(t, exists)
	require.True(t, invalidatedEntry.invalidated)
	assert.Equal(t, oldEntry.staleUntil, invalidatedEntry.staleUntil)

	previousRead := common.RDBMonitorRead
	previousWrite := common.RDBMonitorWrite
	var readFailures atomic.Int64
	var writeFailures atomic.Int64
	failingReadRedis := redis.NewClient(&redis.Options{Addr: common.RDB.Options().Addr})
	failingReadRedis.AddHook(cm08RedisFailureHook{
		failures: &readFailures, err: errors.New("cm08 redis unavailable"),
	})
	failingWriteRedis := redis.NewClient(&redis.Options{Addr: common.RDB.Options().Addr})
	failingWriteRedis.AddHook(cm08RedisFailureHook{
		failures: &writeFailures, err: errors.New("cm08 redis unavailable"),
	})
	common.RDBMonitorRead = failingReadRedis
	common.RDBMonitorWrite = failingWriteRedis
	t.Cleanup(func() {
		common.RDBMonitorRead = previousRead
		common.RDBMonitorWrite = previousWrite
		require.NoError(t, failingReadRedis.Close())
		require.NoError(t, failingWriteRedis.Close())
	})

	var rebuilds atomic.Int64
	rebuildEntered := make(chan struct{}, 1)
	releaseRebuild := make(chan struct{})
	var releaseOnce sync.Once
	callbackName := "test:cm08_block_invalidated_status_probe_overview_rebuild"
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if _, isChannelOverviewQuery := tx.Statement.Dest.(*[]*model.Channel); !isChannelOverviewQuery {
			return
		}
		rebuilds.Add(1)
		select {
		case rebuildEntered <- struct{}{}:
		default:
		}
		<-releaseRebuild
	}))
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseRebuild) })
		require.NoError(t, model.DB.Callback().Query().Remove(callbackName))
	})

	const readers = 16
	type overviewHTTPResult struct {
		status int
		body   []byte
	}
	results := make(chan overviewHTTPResult, readers)
	start := make(chan struct{})
	for range readers {
		ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/status", nil)
		go func() {
			<-start
			GetChannelStatusProbeOverview(ctx)
			results <- overviewHTTPResult{
				status: recorder.Code, body: append([]byte(nil), recorder.Body.Bytes()...),
			}
		}()
	}
	close(start)

	for range readers {
		select {
		case result := <-results:
			require.Equal(t, http.StatusOK, result.status)
			var envelope struct {
				Success bool                               `json:"success"`
				Data    channelStatusProbeOverviewResponse `json:"data"`
			}
			require.NoError(t, common.Unmarshal(result.body, &envelope))
			require.True(t, envelope.Success)
			assert.True(t, envelope.Data.Stale)
			assert.Equal(t, initial.GeneratedAt, envelope.Data.GeneratedAt)
		case <-time.After(2 * time.Second):
			require.FailNow(t, "invalidated stale overview read blocked on Redis or database rebuild")
		}
	}

	select {
	case <-rebuildEntered:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "invalidated overview background rebuild did not start")
	}
	assert.EqualValues(t, 1, rebuilds.Load())
	assert.EqualValues(t, 1, readFailures.Load())
	assert.EqualValues(t, 1, writeFailures.Load())

	releaseOnce.Do(func() { close(releaseRebuild) })
	require.Eventually(t, func() bool {
		cached, cachedExists, stale := loadChannelStatusProbeOverviewCacheEntry(key, time.Now())
		return cachedExists && !stale && cached.eventWatermark > oldEntry.eventWatermark
	}, 2*time.Second, 5*time.Millisecond)
	assert.EqualValues(t, 1, rebuilds.Load())
}

func TestCM08ChannelStatusProbeOverviewCachesModelFiltersIndependently(t *testing.T) {
	t.Setenv("CHANNEL_STATUS_PROBE_OVERVIEW_CACHE_TTL_MS", "10000")
	channel := setupChannelStatusProbeControllerTest(t)
	modelsJSON, err := common.Marshal([]string{"model-a", "model-b"})
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.ChannelStatusProbeConfig{
		ChannelId: channel.Id, Enabled: true, ModelsJSON: string(modelsJSON),
		IntervalSeconds: 300, DisplayValue: 60,
		DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute, Revision: 1,
	}).Error)

	var queryCount atomic.Int64
	callbackName := "test:cm08_count_status_probe_model_filter_queries"
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(*gorm.DB) {
		queryCount.Add(1)
	}))
	t.Cleanup(func() {
		require.NoError(t, model.DB.Callback().Query().Remove(callbackName))
	})

	modelA := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status?model=model-a")
	modelAQueries := queryCount.Load()
	require.Positive(t, modelAQueries)
	require.Len(t, modelA.Channels, 1)
	require.Len(t, modelA.Channels[0].ModelStatuses, 1)
	assert.Equal(t, "model-a", modelA.Channels[0].ModelStatuses[0].ModelName)

	modelB := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status?model=model-b")
	modelBQueries := queryCount.Load()
	assert.Greater(t, modelBQueries, modelAQueries)
	require.Len(t, modelB.Channels, 1)
	require.Len(t, modelB.Channels[0].ModelStatuses, 1)
	assert.Equal(t, "model-b", modelB.Channels[0].ModelStatuses[0].ModelName)

	missing := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status?model=missing-model")
	missingQueries := queryCount.Load()
	assert.Greater(t, missingQueries, modelBQueries)
	assert.Empty(t, missing.Channels)

	unfiltered := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	unfilteredQueries := queryCount.Load()
	assert.Greater(t, unfilteredQueries, missingQueries)
	require.Len(t, unfiltered.Channels, 1)
	assert.Len(t, unfiltered.Channels[0].ModelStatuses, 2)

	modelAAgain := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status?model=model-a")
	modelBAgain := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status?model=model-b")
	assert.Equal(t, modelA.GeneratedAt, modelAAgain.GeneratedAt)
	assert.Equal(t, modelB.GeneratedAt, modelBAgain.GeneratedAt)
	assert.Equal(t, unfilteredQueries, queryCount.Load())
}

func TestCM08ChannelStatusProbeOverviewGenerationFenceRejectsOlderStore(t *testing.T) {
	setupChannelStatusProbeControllerTest(t)

	channelStatusProbeOverviewCache.Lock()
	channelStatusProbeOverviewCache.items = make(map[channelStatusProbeOverviewCacheKey]channelStatusProbeOverviewCacheEntry)
	channelStatusProbeOverviewCache.Unlock()
	oldGeneration := channelStatusProbeOverviewCacheGeneration.Load()
	oldKey := channelStatusProbeOverviewCacheKey{db: model.DB, generation: oldGeneration}
	storeChannelStatusProbeOverviewCacheEntry(oldKey, time.Now(), time.Minute, time.Minute,
		channelStatusProbeOverviewRedisSnapshot{
			SchemaVersion: channelStatusProbeOverviewSnapshotSchemaVersion,
			Revision:      2, EventWatermark: 2, GeneratedAt: 200,
			GeneratedAtUnixMillis: time.Now().UnixMilli(),
			Response:              channelStatusProbeOverviewResponse{ServerNow: 200},
		},
	)

	invalidateChannelStatusProbeOverviewCache()
	currentKey := channelStatusProbeOverviewCacheKey{
		db: model.DB, generation: channelStatusProbeOverviewCacheGeneration.Load(),
	}
	storeChannelStatusProbeOverviewCacheEntry(oldKey, time.Now(), time.Minute, time.Minute,
		channelStatusProbeOverviewRedisSnapshot{
			SchemaVersion: channelStatusProbeOverviewSnapshotSchemaVersion,
			Revision:      1, EventWatermark: 1, GeneratedAt: 100,
			GeneratedAtUnixMillis: time.Now().Add(-time.Second).UnixMilli(),
			Response:              channelStatusProbeOverviewResponse{ServerNow: 100},
		},
	)

	channelStatusProbeOverviewCache.RLock()
	current, exists := channelStatusProbeOverviewCache.items[currentKey]
	_, oldExists := channelStatusProbeOverviewCache.items[oldKey]
	channelStatusProbeOverviewCache.RUnlock()
	require.True(t, exists)
	assert.False(t, oldExists)
	assert.EqualValues(t, 200, current.generatedAt)
	assert.EqualValues(t, 200, current.response.ServerNow)
}
