package controller

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
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

type channelStatusProbeOverviewLeaseAttemptHook struct {
	leaseKey string
	attempts atomic.Int64
}

func (hook *channelStatusProbeOverviewLeaseAttemptHook) BeforeProcess(
	ctx context.Context,
	cmd redis.Cmder,
) (context.Context, error) {
	for _, arg := range cmd.Args() {
		if fmt.Sprint(arg) == hook.leaseKey {
			hook.attempts.Add(1)
			break
		}
	}
	return ctx, nil
}

func (*channelStatusProbeOverviewLeaseAttemptHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (*channelStatusProbeOverviewLeaseAttemptHook) BeforeProcessPipeline(
	ctx context.Context,
	_ []redis.Cmder,
) (context.Context, error) {
	return ctx, nil
}

func (*channelStatusProbeOverviewLeaseAttemptHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func TestChannelStatusProbeOverviewLeaseTakeoverFencesOldWriter(t *testing.T) {
	setupChannelStatusProbeControllerTest(t)
	selectedModel := "model-a"

	firstLease, acquired, err := acquireChannelStatusProbeOverviewBuildLease(selectedModel)
	require.NoError(t, err)
	require.True(t, acquired)
	require.Positive(t, firstLease.fencingToken)

	_, acquired, err = acquireChannelStatusProbeOverviewBuildLease(selectedModel)
	require.NoError(t, err)
	assert.False(t, acquired)

	require.NoError(t, firstLease.client.Del(context.Background(), firstLease.key).Err())
	secondLease, acquired, err := acquireChannelStatusProbeOverviewBuildLease(selectedModel)
	require.NoError(t, err)
	require.True(t, acquired)
	require.Greater(t, secondLease.fencingToken, firstLease.fencingToken)
	defer releaseChannelStatusProbeOverviewBuildLease(secondLease)

	oldSnapshot := newChannelStatusProbeOverviewSnapshot(
		selectedModel,
		firstLease.fencingToken,
		firstLease.eventWatermark,
		channelStatusProbeOverviewResponse{ServerNow: 100},
	)
	published, err := storeChannelStatusProbeOverviewRedisSnapshot(selectedModel, firstLease, oldSnapshot)
	require.NoError(t, err)
	assert.False(t, published)

	currentSnapshot := newChannelStatusProbeOverviewSnapshot(
		selectedModel,
		secondLease.fencingToken,
		secondLease.eventWatermark,
		channelStatusProbeOverviewResponse{ServerNow: 200},
	)
	published, err = storeChannelStatusProbeOverviewRedisSnapshot(selectedModel, secondLease, currentSnapshot)
	require.NoError(t, err)
	require.True(t, published)

	entry, exists := loadChannelStatusProbeOverviewRedisSnapshot(selectedModel, time.Minute, time.Minute)
	require.True(t, exists)
	assert.Equal(t, secondLease.fencingToken, entry.revision)
	assert.EqualValues(t, 200, entry.response.ServerNow)
}

func TestChannelStatusProbeOverviewRedisCASRejectsRevisionAndWatermarkRollback(t *testing.T) {
	setupChannelStatusProbeControllerTest(t)
	selectedModel := "model-b"
	lease, acquired, err := acquireChannelStatusProbeOverviewBuildLease(selectedModel)
	require.NoError(t, err)
	require.True(t, acquired)
	defer releaseChannelStatusProbeOverviewBuildLease(lease)

	current := newChannelStatusProbeOverviewSnapshot(
		selectedModel,
		lease.fencingToken,
		lease.eventWatermark,
		channelStatusProbeOverviewResponse{ServerNow: 300},
	)
	published, err := storeChannelStatusProbeOverviewRedisSnapshot(selectedModel, lease, current)
	require.NoError(t, err)
	require.True(t, published)
	sameRevision := current
	sameRevision.GeneratedAtUnixMillis++
	sameRevision.Response.ServerNow = 301
	published, err = storeChannelStatusProbeOverviewRedisSnapshot(selectedModel, lease, sameRevision)
	require.NoError(t, err)
	assert.False(t, published)

	redisKey := channelStatusProbeOverviewRedisKey(selectedModel)
	require.NoError(t, lease.client.Set(
		context.Background(), lease.key, strconv.FormatUint(lease.fencingToken-1, 10),
		channelStatusProbeOverviewBuildLeaseTTL,
	).Err())
	olderRevision := current
	olderRevision.Revision--
	olderRevision.GeneratedAtUnixMillis++
	olderRevision.Response.ServerNow = 200
	published, err = storeChannelStatusProbeOverviewRedisSnapshot(
		selectedModel,
		channelStatusProbeOverviewBuildLease{
			client: lease.client, key: lease.key,
			fencingToken: olderRevision.Revision, eventWatermark: olderRevision.EventWatermark,
		},
		olderRevision,
	)
	require.NoError(t, err)
	assert.False(t, published)

	require.NoError(t, lease.client.Set(
		context.Background(), lease.key, strconv.FormatUint(lease.fencingToken, 10),
		channelStatusProbeOverviewBuildLeaseTTL,
	).Err())
	newWatermark := advanceChannelStatusProbeOverviewEventWatermark()
	require.Greater(t, newWatermark, current.EventWatermark)
	watermarkRollback := current
	watermarkRollback.GeneratedAtUnixMillis += 2
	watermarkRollback.Response.ServerNow = 400
	published, err = storeChannelStatusProbeOverviewRedisSnapshot(selectedModel, lease, watermarkRollback)
	require.NoError(t, err)
	assert.False(t, published)

	payload, err := lease.client.Get(context.Background(), redisKey).Bytes()
	require.NoError(t, err)
	var stored channelStatusProbeOverviewRedisSnapshot
	require.NoError(t, common.Unmarshal(payload, &stored))
	assert.Equal(t, current.Revision, stored.Revision)
	assert.Equal(t, current.EventWatermark, stored.EventWatermark)
	assert.EqualValues(t, 300, stored.Response.ServerNow)
}

func TestChannelStatusProbeOverviewRedisCASAcceptsSameMillisecondNewRevision(t *testing.T) {
	setupChannelStatusProbeControllerTest(t)
	selectedModel := "same-millisecond-model"
	firstLease, acquired, err := acquireChannelStatusProbeOverviewBuildLease(selectedModel)
	require.NoError(t, err)
	require.True(t, acquired)

	current := newChannelStatusProbeOverviewSnapshot(
		selectedModel, firstLease.fencingToken, firstLease.eventWatermark,
		channelStatusProbeOverviewResponse{ServerNow: 700},
	)
	published, err := storeChannelStatusProbeOverviewRedisSnapshot(selectedModel, firstLease, current)
	require.NoError(t, err)
	require.True(t, published)
	releaseChannelStatusProbeOverviewBuildLease(firstLease)

	secondLease, acquired, err := acquireChannelStatusProbeOverviewBuildLease(selectedModel)
	require.NoError(t, err)
	require.True(t, acquired)
	defer releaseChannelStatusProbeOverviewBuildLease(secondLease)

	next := current
	next.Revision = secondLease.fencingToken
	next.EventWatermark = secondLease.eventWatermark
	next.GeneratedAtUnixMillis = current.GeneratedAtUnixMillis
	next.Response.ServerNow = 701
	published, err = storeChannelStatusProbeOverviewRedisSnapshot(selectedModel, secondLease, next)
	require.NoError(t, err)
	assert.True(t, published)
	entry, exists := loadChannelStatusProbeOverviewRedisSnapshot(selectedModel, time.Minute, time.Minute)
	require.True(t, exists)
	assert.Equal(t, next.Revision, entry.revision)
	assert.EqualValues(t, 701, entry.response.ServerNow)
}

func TestChannelStatusProbeOverviewRedisCASRejectsSnapshotOutsideLeaseIdentity(t *testing.T) {
	setupChannelStatusProbeControllerTest(t)
	selectedModel := "lease-identity-model"
	lease, acquired, err := acquireChannelStatusProbeOverviewBuildLease(selectedModel)
	require.NoError(t, err)
	require.True(t, acquired)
	defer releaseChannelStatusProbeOverviewBuildLease(lease)

	snapshot := newChannelStatusProbeOverviewSnapshot(
		selectedModel, lease.fencingToken+1, lease.eventWatermark,
		channelStatusProbeOverviewResponse{ServerNow: 800},
	)
	published, err := storeChannelStatusProbeOverviewRedisSnapshot(selectedModel, lease, snapshot)
	require.NoError(t, err)
	assert.False(t, published)

	snapshot.Revision = lease.fencingToken
	snapshot.EventWatermark++
	published, err = storeChannelStatusProbeOverviewRedisSnapshot(selectedModel, lease, snapshot)
	require.NoError(t, err)
	assert.False(t, published)
	assert.Zero(t, lease.client.Exists(context.Background(), channelStatusProbeOverviewRedisKey(selectedModel)).Val())
}

func TestChannelStatusProbeOverviewRedisSnapshotKeepsOriginalStaleDeadline(t *testing.T) {
	setupChannelStatusProbeControllerTest(t)
	ttl := time.Second
	staleTTL := 10 * time.Second

	staleModel := "stale-model"
	staleLease, acquired, err := acquireChannelStatusProbeOverviewBuildLease(staleModel)
	require.NoError(t, err)
	require.True(t, acquired)
	staleSnapshot := newChannelStatusProbeOverviewSnapshot(
		staleModel,
		staleLease.fencingToken,
		staleLease.eventWatermark,
		channelStatusProbeOverviewResponse{ServerNow: 500},
	)
	staleGeneratedTime := time.Now().Add(-2 * time.Second)
	staleSnapshot.GeneratedAt = staleGeneratedTime.Unix()
	staleSnapshot.GeneratedAtUnixMillis = staleGeneratedTime.UnixMilli()
	published, err := storeChannelStatusProbeOverviewRedisSnapshot(staleModel, staleLease, staleSnapshot)
	require.NoError(t, err)
	require.True(t, published)
	releaseChannelStatusProbeOverviewBuildLease(staleLease)

	entry, exists := loadChannelStatusProbeOverviewRedisSnapshot(staleModel, ttl, staleTTL)
	require.True(t, exists)
	assert.True(t, entry.expiresAt.Before(time.Now()))
	assert.True(t, time.Now().Before(entry.staleUntil))
	assert.Equal(t, staleSnapshot.GeneratedAtUnixMillis, entry.generatedAtUnixMillis)

	expiredModel := "expired-model"
	expiredLease, acquired, err := acquireChannelStatusProbeOverviewBuildLease(expiredModel)
	require.NoError(t, err)
	require.True(t, acquired)
	expiredSnapshot := newChannelStatusProbeOverviewSnapshot(
		expiredModel,
		expiredLease.fencingToken,
		expiredLease.eventWatermark,
		channelStatusProbeOverviewResponse{ServerNow: 600},
	)
	expiredGeneratedTime := time.Now().Add(-(ttl + staleTTL + time.Second))
	expiredSnapshot.GeneratedAt = expiredGeneratedTime.Unix()
	expiredSnapshot.GeneratedAtUnixMillis = expiredGeneratedTime.UnixMilli()
	published, err = storeChannelStatusProbeOverviewRedisSnapshot(expiredModel, expiredLease, expiredSnapshot)
	require.NoError(t, err)
	require.True(t, published)
	releaseChannelStatusProbeOverviewBuildLease(expiredLease)

	_, exists = loadChannelStatusProbeOverviewRedisSnapshot(expiredModel, ttl, staleTTL)
	assert.False(t, exists)
}

func TestChannelStatusProbeOverviewRejectsFutureGeneratedAt(t *testing.T) {
	setupChannelStatusProbeControllerTest(t)
	selectedModel := "future-model"
	lease, acquired, err := acquireChannelStatusProbeOverviewBuildLease(selectedModel)
	require.NoError(t, err)
	require.True(t, acquired)
	defer releaseChannelStatusProbeOverviewBuildLease(lease)

	snapshot := newChannelStatusProbeOverviewSnapshot(
		selectedModel, lease.fencingToken, lease.eventWatermark,
		channelStatusProbeOverviewResponse{ServerNow: 600},
	)
	future := time.Now().Add(channelStatusProbeOverviewMaxFutureSkew + time.Second)
	snapshot.GeneratedAt = future.Unix()
	snapshot.GeneratedAtUnixMillis = future.UnixMilli()
	payload, err := common.Marshal(snapshot)
	require.NoError(t, err)
	redisKey := channelStatusProbeOverviewRedisKey(selectedModel)
	ctx := context.Background()
	require.NoError(t, lease.client.Set(ctx, redisKey, payload, time.Minute).Err())
	require.NoError(t, lease.client.HSet(ctx, redisKey+":meta",
		"revision", snapshot.Revision,
		"event_watermark", snapshot.EventWatermark,
		"generated_at_unix_millis", snapshot.GeneratedAtUnixMillis,
	).Err())
	require.NoError(t, lease.client.Set(ctx, channelStatusProbeOverviewEventWatermarkKey, snapshot.EventWatermark, time.Minute).Err())

	_, exists := loadChannelStatusProbeOverviewRedisSnapshot(
		selectedModel, time.Minute, time.Minute,
	)
	assert.False(t, exists)
}

func TestChannelStatusProbeOverviewCrossInstanceLeasePreventsDatabaseFanout(t *testing.T) {
	setupChannelStatusProbeControllerTest(t)
	selectedModel := "model-a"
	key := channelStatusProbeOverviewCacheKey{
		db: model.DB, generation: channelStatusProbeOverviewCacheGeneration.Load(),
		selectedModel: selectedModel,
	}
	leaseHook := &channelStatusProbeOverviewLeaseAttemptHook{
		leaseKey: channelStatusProbeOverviewRedisKey(selectedModel) + ":lease",
	}
	common.RedisMonitorWriteClient().AddHook(leaseHook)

	var rebuilds atomic.Int64
	rebuildEntered := make(chan struct{}, 1)
	releaseRebuild := make(chan struct{})
	var releaseOnce sync.Once
	callbackName := "test:status_probe_overview_cross_instance_lease"
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

	type buildResult struct {
		snapshot channelStatusProbeOverviewRedisSnapshot
		err      error
	}
	results := make(chan buildResult, 2)
	go func() {
		snapshot, err := buildChannelStatusProbeOverviewSnapshot(key, time.Minute, time.Minute, 0, 0)
		results <- buildResult{snapshot: snapshot, err: err}
	}()
	select {
	case <-rebuildEntered:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "first status overview builder did not enter the database")
	}
	firstLeaseAttempts := leaseHook.attempts.Load()
	require.Positive(t, firstLeaseAttempts)

	go func() {
		snapshot, err := buildChannelStatusProbeOverviewSnapshot(key, time.Minute, time.Minute, 0, 0)
		results <- buildResult{snapshot: snapshot, err: err}
	}()
	require.Eventually(t, func() bool {
		return leaseHook.attempts.Load() > firstLeaseAttempts
	}, time.Second, 5*time.Millisecond, "second builder did not attempt the shared Redis lease")
	assert.EqualValues(t, 1, rebuilds.Load())
	releaseOnce.Do(func() { close(releaseRebuild) })

	completed := make([]buildResult, 0, 2)
	for range 2 {
		select {
		case result := <-results:
			completed = append(completed, result)
		case <-time.After(2 * time.Second):
			require.FailNow(t, "status overview builder did not finish after lease holder published")
		}
	}
	first := completed[0]
	second := completed[1]
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	assert.Equal(t, first.snapshot.Revision, second.snapshot.Revision)
	assert.Equal(t, first.snapshot.EventWatermark, second.snapshot.EventWatermark)
	assert.EqualValues(t, 1, rebuilds.Load())
}

func TestChannelStatusProbeOverviewColdLeaseContentionReturnsServiceUnavailable(t *testing.T) {
	setupChannelStatusProbeControllerTest(t)
	selectedModel := "cold-lease-model"
	lease, acquired, err := acquireChannelStatusProbeOverviewBuildLease(selectedModel)
	require.NoError(t, err)
	require.True(t, acquired)
	defer releaseChannelStatusProbeOverviewBuildLease(lease)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/status?model="+selectedModel, nil,
	)
	GetChannelStatusProbeOverview(ctx)
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Contains(t, recorder.Body.String(), errChannelStatusProbeOverviewSnapshotUnavailable.Error())
}

func TestChannelStatusProbeOverviewRejectsOldRedisEnvelope(t *testing.T) {
	setupChannelStatusProbeControllerTest(t)
	selectedModel := "old-envelope-model"
	redisKey := channelStatusProbeOverviewRedisKey(selectedModel)
	oldEnvelope := channelStatusProbeOverviewRedisSnapshot{
		SchemaVersion:         channelStatusProbeOverviewSnapshotSchemaVersion - 1,
		Revision:              1,
		EventWatermark:        1,
		SelectedModel:         selectedModel,
		GeneratedAt:           time.Now().Unix(),
		GeneratedAtUnixMillis: time.Now().UnixMilli(),
		Response:              channelStatusProbeOverviewResponse{ServerNow: 700},
	}
	payload, err := common.Marshal(oldEnvelope)
	require.NoError(t, err)
	client := common.RedisMonitorWriteClient()
	require.NoError(t, client.Set(context.Background(), redisKey, payload, time.Minute).Err())
	require.NoError(t, client.HSet(
		context.Background(), redisKey+":meta",
		"revision", oldEnvelope.Revision,
		"event_watermark", oldEnvelope.EventWatermark,
		"generated_at_unix_millis", oldEnvelope.GeneratedAtUnixMillis,
	).Err())
	require.NoError(t, client.Set(
		context.Background(), channelStatusProbeOverviewEventWatermarkKey,
		oldEnvelope.EventWatermark, 0,
	).Err())

	_, exists := loadChannelStatusProbeOverviewRedisSnapshot(selectedModel, time.Minute, time.Minute)
	assert.False(t, exists)
}

func TestChannelStatusProbeOverviewRetriesWhenGenerationChangesDuringFirstBuild(t *testing.T) {
	setupChannelStatusProbeControllerTest(t)
	var builds atomic.Int64
	callbackName := "test:status_probe_overview_generation_retry"
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if _, isChannelOverviewQuery := tx.Statement.Dest.(*[]*model.Channel); !isChannelOverviewQuery {
			return
		}
		if builds.Add(1) == 1 {
			invalidateChannelStatusProbeOverviewCache()
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, model.DB.Callback().Query().Remove(callbackName))
	})

	response := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	assert.False(t, response.Stale)
	assert.EqualValues(t, 2, builds.Load())
}
