package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorPageSnapshotKeyNormalizesAndHidesScope(t *testing.T) {
	first := ChannelMonitorPageSnapshotQuery{
		Page:            "performance",
		Version:         "v1",
		PermissionScope: "role=30;user=42;group=internal",
		WindowStart:     100,
		WindowEnd:       200,
		Filters: map[string][]string{
			" Model ": {" gpt-4o ", "claude"},
			"minutes": {"15"},
		},
	}
	second := ChannelMonitorPageSnapshotQuery{
		Page:            "PERFORMANCE",
		Version:         "v1",
		PermissionScope: "role=30;user=42;group=internal",
		WindowStart:     100,
		WindowEnd:       200,
		Filters: map[string][]string{
			"MINUTES": {"15"},
			"model":   {"claude", "gpt-4o"},
		},
	}

	firstKey, err := ChannelMonitorPageSnapshotKey(first)
	require.NoError(t, err)
	secondKey, err := ChannelMonitorPageSnapshotKey(second)
	require.NoError(t, err)
	assert.Equal(t, firstKey, secondKey)
	assert.NotContains(t, firstKey, "gpt-4o")
	assert.NotContains(t, firstKey, "internal")
	assert.NotContains(t, firstKey, "user=42")

	second.PermissionScope = "role=30;user=43;group=internal"
	otherUserKey, err := ChannelMonitorPageSnapshotKey(second)
	require.NoError(t, err)
	assert.NotEqual(t, firstKey, otherUserKey)
}

func TestChannelMonitorPageSnapshotFreshHitAndPermissionIsolation(t *testing.T) {
	server, client, store := newChannelMonitorPageSnapshotTestStore(t)
	ctx := context.Background()
	query := channelMonitorPageSnapshotTestQuery("user=42")
	var builds atomic.Int32
	builder := func(context.Context) (ChannelMonitorPageSnapshot, error) {
		builds.Add(1)
		return channelMonitorPageSnapshotTestValue("first"), nil
	}

	first, err := store.refreshSnapshot(ctx, query, builder)
	require.NoError(t, err)
	second, err := store.refreshSnapshot(ctx, query, builder)
	require.NoError(t, err)
	assert.Equal(t, int32(1), builds.Load())
	assert.Equal(t, first.Payload, second.Payload)

	otherUserQuery := channelMonitorPageSnapshotTestQuery("user=43")
	_, state, err := store.load(ctx, otherUserQuery, time.Now())
	assert.ErrorIs(t, err, ErrChannelMonitorPageSnapshotMissing)
	assert.Equal(t, ChannelMonitorPageSnapshotMissing, state)

	key, err := ChannelMonitorPageSnapshotKey(query)
	require.NoError(t, err)
	raw, err := server.Get(key)
	require.NoError(t, err)
	assert.NotContains(t, raw, "user=42")
	assert.NotContains(t, raw, "secret-filter")
	assert.NotContains(t, string(first.Payload), "authorization")
	_ = client
}

func TestChannelMonitorPageSnapshotForceRefreshRebuildsFreshSnapshot(t *testing.T) {
	_, _, store := newChannelMonitorPageSnapshotTestStore(t)
	ctx := context.Background()
	query := channelMonitorPageSnapshotTestQuery("user=force-refresh")
	var builds atomic.Int32
	builder := func(context.Context) (ChannelMonitorPageSnapshot, error) {
		if builds.Add(1) == 1 {
			return channelMonitorPageSnapshotTestValue("old"), nil
		}
		return channelMonitorPageSnapshotTestValue("new"), nil
	}

	first, err := store.refreshSnapshot(ctx, query, builder)
	require.NoError(t, err)
	require.Contains(t, string(first.Payload), "old")

	refreshed, err := store.refreshSnapshotForce(ctx, query, builder)
	require.NoError(t, err)
	assert.Equal(t, int32(2), builds.Load())
	assert.Contains(t, string(refreshed.Payload), "new")

	loaded, state, err := store.load(ctx, query, time.Now())
	require.NoError(t, err)
	assert.Equal(t, ChannelMonitorPageSnapshotFresh, state)
	assert.Contains(t, string(loaded.Payload), "new")
}

func TestChannelMonitorPageSnapshotForceRefreshRejectsRollback(t *testing.T) {
	_, _, store := newChannelMonitorPageSnapshotTestStore(t)
	ctx := context.Background()
	query := channelMonitorPageSnapshotTestQuery("user=force-monotonic")
	var builds atomic.Int32
	first, err := store.refreshSnapshot(ctx, query, func(context.Context) (ChannelMonitorPageSnapshot, error) {
		snapshot := channelMonitorPageSnapshotTestValue("current")
		snapshot.Revision = 8
		snapshot.EventWatermark = 12
		snapshot.DataCutoffAt = 100
		return snapshot, nil
	})
	require.NoError(t, err)

	refreshed, err := store.refreshSnapshotForce(ctx, query, func(context.Context) (ChannelMonitorPageSnapshot, error) {
		builds.Add(1)
		snapshot := channelMonitorPageSnapshotTestValue("rollback")
		snapshot.Revision = first.Revision - 1
		snapshot.EventWatermark = first.EventWatermark - 1
		snapshot.DataCutoffAt = first.DataCutoffAt - 1
		return snapshot, nil
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), builds.Load())
	assert.Equal(t, first.Revision, refreshed.Revision)
	assert.Equal(t, first.EventWatermark, refreshed.EventWatermark)
	assert.Equal(t, first.DataCutoffAt, refreshed.DataCutoffAt)
	assert.Contains(t, string(refreshed.Payload), "current")
}

func TestChannelMonitorPageSnapshotLocalCacheEnforcesByteBudget(t *testing.T) {
	_, _, store := newChannelMonitorPageSnapshotTestStore(t)
	base := time.Now()
	payload := []byte(strings.Repeat("x", channelMonitorPageSnapshotMaxLocalBytes/8))

	for index := 0; index < 9; index++ {
		timestamp := base.Add(time.Duration(index) * time.Millisecond)
		snapshot := ChannelMonitorPageSnapshot{
			GeneratedAt:          timestamp.Unix(),
			GeneratedAtUnixMilli: timestamp.UnixMilli(),
			StatusCode:           200,
			Payload:              payload,
		}
		store.rememberLocal("byte-budget-"+strconv.Itoa(index), snapshot)
	}

	store.localMu.RLock()
	assert.LessOrEqual(t, store.localBytes, int64(channelMonitorPageSnapshotMaxLocalBytes))
	assert.Len(t, store.local, 8)
	assert.NotContains(t, store.local, "byte-budget-0")
	assert.Contains(t, store.local, "byte-budget-8")
	store.localMu.RUnlock()

	store.rememberLocal("byte-budget-8", ChannelMonitorPageSnapshot{
		GeneratedAt:          base.Add(9 * time.Millisecond).Unix(),
		GeneratedAtUnixMilli: base.Add(9 * time.Millisecond).UnixMilli(),
		StatusCode:           200,
		Payload:              []byte("updated"),
	})
	store.localMu.RLock()
	assert.Equal(t, int64(7*len(payload)+len("updated")), store.localBytes)
	store.localMu.RUnlock()
}

func TestChannelMonitorPageSnapshotBuildSemaphoreLimitsFanout(t *testing.T) {
	_, _, store := newChannelMonitorPageSnapshotTestStore(t)
	var active, maxActive, builds atomic.Int32
	release := make(chan struct{})
	updateMax := func(value int32) {
		for {
			current := maxActive.Load()
			if value <= current || maxActive.CompareAndSwap(current, value) {
				return
			}
		}
	}
	builder := func(context.Context) (ChannelMonitorPageSnapshot, error) {
		builds.Add(1)
		current := active.Add(1)
		updateMax(current)
		<-release
		active.Add(-1)
		return channelMonitorPageSnapshotTestValue("bounded"), nil
	}

	var waitGroup sync.WaitGroup
	for index := 0; index < channelMonitorPageSnapshotMaxConcurrentBuilds+2; index++ {
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			query := channelMonitorPageSnapshotTestQuery("fanout-" + strconv.Itoa(index))
			_, _ = store.refreshSnapshot(context.Background(), query, builder)
		}()
	}
	require.Eventually(t, func() bool {
		return builds.Load() == channelMonitorPageSnapshotMaxConcurrentBuilds
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, int32(channelMonitorPageSnapshotMaxConcurrentBuilds), maxActive.Load())
	close(release)
	waitGroup.Wait()
	assert.Equal(t, int32(channelMonitorPageSnapshotMaxConcurrentBuilds+2), builds.Load())
}

func TestChannelMonitorPageSnapshotStaleRefreshFallsBackToLocalOnRedisError(t *testing.T) {
	server, _, store := newChannelMonitorPageSnapshotTestStore(t)
	query := channelMonitorPageSnapshotTestQuery("redis-error-refresh")
	_, err := store.refreshSnapshot(context.Background(), query, func(context.Context) (ChannelMonitorPageSnapshot, error) {
		return channelMonitorPageSnapshotTestValue("old"), nil
	})
	require.NoError(t, err)
	server.Close()

	var builds atomic.Int32
	assert.True(t, store.requestRefresh(query, func(context.Context) (ChannelMonitorPageSnapshot, error) {
		builds.Add(1)
		return channelMonitorPageSnapshotTestValue("new"), nil
	}))
	require.Eventually(t, func() bool {
		loaded, _, loadErr := store.load(context.Background(), query, time.Now())
		return builds.Load() == 1 && loadErr == nil && strings.Contains(string(loaded.Payload), "new")
	}, time.Second, 10*time.Millisecond)
}

func TestChannelMonitorPageSnapshotLocalFallbackAllowsEqualGenerationWithoutRollback(t *testing.T) {
	_, _, store := newChannelMonitorPageSnapshotTestStore(t)
	query := channelMonitorPageSnapshotTestQuery("local-equal-generation")
	key, err := ChannelMonitorPageSnapshotKey(query)
	require.NoError(t, err)
	identityHash, err := channelMonitorPageSnapshotIdentityHash(query)
	require.NoError(t, err)

	generation := time.Now().UnixMilli()
	initial := channelMonitorPageSnapshotTestValue("old")
	initial.SchemaVersion = channelMonitorPageSnapshotSchemaVersion
	initial.IdentityHash = identityHash
	initial.Revision = 3
	initial.GeneratedAtUnixMilli = generation
	initial.GeneratedAt = generation / 1000
	store.rememberLocal(key, initial)

	replacement := initial
	replacement.Payload = channelMonitorPageSnapshotTestValue("new").Payload
	store.rememberLocalAllowEqualGeneration(key, replacement)
	loaded, _, err := store.loadLocal(query, time.Now(), ErrChannelMonitorPageSnapshotMissing)
	require.NoError(t, err)
	assert.Contains(t, string(loaded.Payload), "new")

	rollback := replacement
	rollback.Payload = channelMonitorPageSnapshotTestValue("rollback").Payload
	rollback.Revision--
	store.rememberLocalAllowEqualGeneration(key, rollback)
	loaded, _, err = store.loadLocal(query, time.Now(), ErrChannelMonitorPageSnapshotMissing)
	require.NoError(t, err)
	assert.Contains(t, string(loaded.Payload), "new")
}

func TestChannelMonitorPageSnapshotReturnsStaleWhileOneBackgroundRefreshRuns(t *testing.T) {
	_, client, store := newChannelMonitorPageSnapshotTestStore(t)
	ctx := context.Background()
	query := channelMonitorPageSnapshotTestQuery("user=42")
	initial, err := store.refreshSnapshot(ctx, query, func(context.Context) (ChannelMonitorPageSnapshot, error) {
		return channelMonitorPageSnapshotTestValue("old"), nil
	})
	require.NoError(t, err)
	initial.GeneratedAtUnixMilli = time.Now().Add(-2 * channelMonitorPageSnapshotFreshTTL).UnixMilli()
	key, err := ChannelMonitorPageSnapshotKey(query)
	require.NoError(t, err)
	store.localMu.Lock()
	delete(store.local, key)
	store.localMu.Unlock()
	seedChannelMonitorPageSnapshot(t, client, key, initial)

	stale, state, err := store.load(ctx, query, time.Now())
	require.NoError(t, err)
	assert.Equal(t, ChannelMonitorPageSnapshotStale, state)
	assert.Contains(t, string(stale.Payload), "old")

	started := make(chan struct{})
	release := make(chan struct{})
	var builds atomic.Int32
	builder := func(context.Context) (ChannelMonitorPageSnapshot, error) {
		if builds.Add(1) == 1 {
			close(started)
		}
		<-release
		return channelMonitorPageSnapshotTestValue("new"), nil
	}
	assert.True(t, store.requestRefresh(query, builder))
	assert.False(t, store.requestRefresh(query, builder))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background snapshot refresh did not start")
	}
	duringRefresh, duringState, err := store.load(ctx, query, time.Now())
	require.NoError(t, err)
	assert.Equal(t, ChannelMonitorPageSnapshotStale, duringState)
	assert.Contains(t, string(duringRefresh.Payload), "old")

	close(release)
	require.Eventually(t, func() bool {
		refreshed, refreshedState, loadErr := store.load(ctx, query, time.Now())
		return loadErr == nil &&
			refreshedState == ChannelMonitorPageSnapshotFresh &&
			strings.Contains(string(refreshed.Payload), "new")
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(1), builds.Load())
}

func TestChannelMonitorPageSnapshotRedisRollbackDoesNotReplaceNewerLocalCopy(t *testing.T) {
	_, client, store := newChannelMonitorPageSnapshotTestStore(t)
	query := channelMonitorPageSnapshotTestQuery("user=rollback")
	current, err := store.refreshSnapshot(context.Background(), query, func(context.Context) (ChannelMonitorPageSnapshot, error) {
		snapshot := channelMonitorPageSnapshotTestValue("current")
		snapshot.Revision = 2
		snapshot.EventWatermark = 2
		return snapshot, nil
	})
	require.NoError(t, err)
	key, err := ChannelMonitorPageSnapshotKey(query)
	require.NoError(t, err)
	rolledBack := current
	rolledBack.Payload = channelMonitorPageSnapshotTestValue("rolled-back").Payload
	rolledBack.Revision = 1
	rolledBack.EventWatermark = 1
	rolledBack.GeneratedAtUnixMilli++
	seedChannelMonitorPageSnapshot(t, client, key, rolledBack)

	loaded, _, err := store.load(context.Background(), query, time.Now())
	require.NoError(t, err)
	assert.Equal(t, current.Revision, loaded.Revision)
	assert.Equal(t, current.EventWatermark, loaded.EventWatermark)
	assert.Contains(t, string(loaded.Payload), "current")

	// A Redis envelope can be ahead on one dimension and behind on another.
	// It is still a rollback and must not bypass the retained local copy.
	incomparable := current
	incomparable.Payload = channelMonitorPageSnapshotTestValue("older-cutoff").Payload
	incomparable.Revision++
	incomparable.EventWatermark++
	incomparable.DataCutoffAt--
	incomparable.GeneratedAtUnixMilli++
	seedChannelMonitorPageSnapshot(t, client, key, incomparable)
	loaded, _, err = store.load(context.Background(), query, time.Now())
	require.NoError(t, err)
	assert.Equal(t, current.DataCutoffAt, loaded.DataCutoffAt)
	assert.Contains(t, string(loaded.Payload), "current")
}

func TestChannelMonitorPageSnapshotRedisLeaseCoalescesAcrossStores(t *testing.T) {
	server, client, firstStore := newChannelMonitorPageSnapshotTestStore(t)
	secondStore := &channelMonitorPageSnapshotStore{
		readClient:  func() *redis.Client { return client },
		writeClient: func() *redis.Client { return client },
	}
	query := channelMonitorPageSnapshotTestQuery("user=42")
	started := make(chan struct{})
	release := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		_, err := firstStore.refreshSnapshot(context.Background(), query, func(context.Context) (ChannelMonitorPageSnapshot, error) {
			close(started)
			<-release
			return channelMonitorPageSnapshotTestValue("shared"), nil
		})
		firstResult <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first store did not acquire the snapshot lease")
	}

	var secondBuilds atomic.Int32
	baselineCommands := server.CommandCount()
	secondResult := make(chan struct {
		snapshot ChannelMonitorPageSnapshot
		err      error
	}, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		snapshot, err := secondStore.refreshSnapshot(ctx, query, func(context.Context) (ChannelMonitorPageSnapshot, error) {
			secondBuilds.Add(1)
			return channelMonitorPageSnapshotTestValue("duplicate"), nil
		})
		secondResult <- struct {
			snapshot ChannelMonitorPageSnapshot
			err      error
		}{snapshot: snapshot, err: err}
	}()
	require.Eventually(t, func() bool {
		// The second store must first read the missing snapshot and then lose
		// the SET NX lease race before the first builder is released.
		return server.CommandCount() >= baselineCommands+2
	}, time.Second, 5*time.Millisecond)
	close(release)

	require.NoError(t, <-firstResult)
	result := <-secondResult
	require.NoError(t, result.err)
	assert.Contains(t, string(result.snapshot.Payload), "shared")
	assert.Equal(t, int32(0), secondBuilds.Load())
}

func TestChannelMonitorPageSnapshotLeaseWaitHasStrictTimeout(t *testing.T) {
	_, client, firstStore := newChannelMonitorPageSnapshotTestStore(t)
	secondStore := &channelMonitorPageSnapshotStore{
		readClient:  func() *redis.Client { return client },
		writeClient: func() *redis.Client { return client },
	}
	query := channelMonitorPageSnapshotTestQuery("user=42")
	key, err := ChannelMonitorPageSnapshotKey(query)
	require.NoError(t, err)
	token, acquired, err := firstStore.acquireLease(context.Background(), key)
	require.NoError(t, err)
	require.True(t, acquired)
	defer firstStore.releaseLease(key, token)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = secondStore.refreshSnapshot(ctx, query, func(context.Context) (ChannelMonitorPageSnapshot, error) {
		return ChannelMonitorPageSnapshot{}, errors.New("builder must not run")
	})
	assert.ErrorIs(t, err, ErrChannelMonitorPageSnapshotRefreshing)
	assert.Less(t, time.Since(started), 500*time.Millisecond)
}

func TestChannelMonitorPageSnapshotPublishIsMonotonic(t *testing.T) {
	_, _, store := newChannelMonitorPageSnapshotTestStore(t)
	ctx := context.Background()
	query := channelMonitorPageSnapshotTestQuery("user=monotonic")
	first, err := store.refreshSnapshot(ctx, query, func(context.Context) (ChannelMonitorPageSnapshot, error) {
		snapshot := channelMonitorPageSnapshotTestValue("newer-watermark")
		snapshot.Revision = 4
		snapshot.EventWatermark = 10
		return snapshot, nil
	})
	require.NoError(t, err)
	key, err := ChannelMonitorPageSnapshotKey(query)
	require.NoError(t, err)
	token, acquired, err := store.acquireLease(ctx, key)
	require.NoError(t, err)
	require.True(t, acquired)
	defer store.releaseLease(key, token)

	older := first
	older.Payload = channelMonitorPageSnapshotTestValue("older-watermark").Payload
	older.GeneratedAtUnixMilli = first.GeneratedAtUnixMilli + 1
	older.EventWatermark = first.EventWatermark - 1
	published, err := store.publish(ctx, key, token, older)
	require.NoError(t, err)
	assert.False(t, published)

	current, _, err := store.load(ctx, query, time.Now())
	require.NoError(t, err)
	assert.Equal(t, uint64(10), current.EventWatermark)
	assert.Contains(t, string(current.Payload), "newer-watermark")

	rolledBackCutoff := first
	rolledBackCutoff.Payload = channelMonitorPageSnapshotTestValue("older-cutoff").Payload
	rolledBackCutoff.Revision++
	rolledBackCutoff.EventWatermark++
	rolledBackCutoff.DataCutoffAt--
	rolledBackCutoff.GeneratedAtUnixMilli++
	published, err = store.publish(ctx, key, token, rolledBackCutoff)
	require.NoError(t, err)
	assert.False(t, published)

	rolledBackGeneration := first
	rolledBackGeneration.Payload = channelMonitorPageSnapshotTestValue("older-generation").Payload
	rolledBackGeneration.Revision++
	rolledBackGeneration.EventWatermark++
	rolledBackGeneration.DataCutoffAt++
	rolledBackGeneration.GeneratedAtUnixMilli--
	published, err = store.publish(ctx, key, token, rolledBackGeneration)
	require.NoError(t, err)
	assert.False(t, published)

	// Revisions can advance more than once within the same millisecond. A
	// newer fenced snapshot must still publish when its timestamp ties the
	// previous envelope.
	tiedTimestamp := first
	tiedTimestamp.Payload = channelMonitorPageSnapshotTestValue("same-millisecond-newer").Payload
	tiedTimestamp.Revision++
	tiedTimestamp.EventWatermark++
	tiedTimestamp.GeneratedAtUnixMilli = first.GeneratedAtUnixMilli
	published, err = store.publish(ctx, key, token, tiedTimestamp)
	require.NoError(t, err)
	assert.True(t, published)
	current, _, err = store.load(ctx, query, time.Now())
	require.NoError(t, err)
	assert.Equal(t, tiedTimestamp.Revision, current.Revision)
	assert.Contains(t, string(current.Payload), "same-millisecond-newer")
}

func TestChannelMonitorPageSnapshotForcePublishReplacesSameMetadata(t *testing.T) {
	_, _, store := newChannelMonitorPageSnapshotTestStore(t)
	ctx := context.Background()
	query := channelMonitorPageSnapshotTestQuery("user=force-same-metadata")
	key, err := ChannelMonitorPageSnapshotKey(query)
	require.NoError(t, err)
	token, acquired, err := store.acquireLease(ctx, key)
	require.NoError(t, err)
	require.True(t, acquired)
	defer store.releaseLease(key, token)

	first := channelMonitorPageSnapshotTestValue("first")
	identityHash, err := channelMonitorPageSnapshotIdentityHash(query)
	require.NoError(t, err)
	first.SchemaVersion = channelMonitorPageSnapshotSchemaVersion
	first.IdentityHash = identityHash
	first.Revision = 7
	first.GeneratedAtUnixMilli = time.Now().UnixMilli()
	first.GeneratedAt = first.GeneratedAtUnixMilli / 1000
	published, err := store.publish(ctx, key, token, first)
	require.NoError(t, err)
	require.True(t, published)

	second := first
	second.Payload = channelMonitorPageSnapshotTestValue("forced").Payload
	published, err = store.publish(ctx, key, token, second)
	require.NoError(t, err)
	assert.False(t, published)

	published, err = store.publishWithMode(ctx, key, token, second, true)
	require.NoError(t, err)
	assert.True(t, published)

	current, _, err := store.load(ctx, query, time.Now())
	require.NoError(t, err)
	assert.Contains(t, string(current.Payload), "forced")
}

func TestChannelMonitorPageSnapshotFencingRejectsExpiredBuilder(t *testing.T) {
	server, _, store := newChannelMonitorPageSnapshotTestStore(t)
	ctx := context.Background()
	query := channelMonitorPageSnapshotTestQuery("user=fencing")
	key, err := ChannelMonitorPageSnapshotKey(query)
	require.NoError(t, err)
	oldToken, acquired, err := store.acquireLease(ctx, key)
	require.NoError(t, err)
	require.True(t, acquired)
	server.FastForward(channelMonitorPageSnapshotLeaseTTL + time.Second)
	newToken, acquired, err := store.acquireLease(ctx, key)
	require.NoError(t, err)
	require.True(t, acquired)
	require.Greater(t, newToken, oldToken)
	defer store.releaseLease(key, newToken)

	identityHash, err := channelMonitorPageSnapshotIdentityHash(query)
	require.NoError(t, err)
	oldBuilder := channelMonitorPageSnapshotTestValue("expired-builder")
	oldBuilder.SchemaVersion = channelMonitorPageSnapshotSchemaVersion
	oldBuilder.IdentityHash = identityHash
	oldBuilder.GeneratedAt = time.Now().Unix()
	oldBuilder.GeneratedAtUnixMilli = time.Now().UnixMilli()
	published, err := store.publish(ctx, key, oldToken, oldBuilder)
	require.NoError(t, err)
	assert.False(t, published)

	newBuilder := channelMonitorPageSnapshotTestValue("current-builder")
	newBuilder.SchemaVersion = channelMonitorPageSnapshotSchemaVersion
	newBuilder.IdentityHash = identityHash
	newBuilder.GeneratedAt = time.Now().Unix()
	newBuilder.GeneratedAtUnixMilli = oldBuilder.GeneratedAtUnixMilli + 1
	published, err = store.publish(ctx, key, newToken, newBuilder)
	require.NoError(t, err)
	assert.True(t, published)
}

func TestChannelMonitorPageSnapshotUsesBoundedLocalCopyDuringRedisFailure(t *testing.T) {
	server, _, store := newChannelMonitorPageSnapshotTestStore(t)
	ctx := context.Background()
	query := channelMonitorPageSnapshotTestQuery("user=local-fallback")
	_, err := store.refreshSnapshot(ctx, query, func(context.Context) (ChannelMonitorPageSnapshot, error) {
		return channelMonitorPageSnapshotTestValue("last-complete"), nil
	})
	require.NoError(t, err)
	server.Close()

	fallback, state, err := store.load(ctx, query, time.Now())
	require.NoError(t, err)
	assert.Equal(t, ChannelMonitorPageSnapshotStale, state)
	assert.Contains(t, string(fallback.Payload), "last-complete")

	key, err := ChannelMonitorPageSnapshotKey(query)
	require.NoError(t, err)
	store.localMu.Lock()
	expired := store.local[key]
	expired.GeneratedAtUnixMilli = time.Now().Add(-channelMonitorPageSnapshotRetention).UnixMilli()
	store.local[key] = expired
	store.localMu.Unlock()
	_, state, err = store.load(ctx, query, time.Now())
	assert.Error(t, err)
	assert.Equal(t, ChannelMonitorPageSnapshotMissing, state)
}

func TestChannelMonitorPageSnapshotRejectsFutureGeneratedAt(t *testing.T) {
	_, client, store := newChannelMonitorPageSnapshotTestStore(t)
	query := channelMonitorPageSnapshotTestQuery("user=future")
	snapshot, err := store.refreshSnapshot(context.Background(), query, func(context.Context) (ChannelMonitorPageSnapshot, error) {
		return channelMonitorPageSnapshotTestValue("future-source"), nil
	})
	require.NoError(t, err)
	future := time.Now().Add(channelMonitorPageSnapshotMaxFutureSkew + time.Second)
	snapshot.GeneratedAt = future.Unix()
	snapshot.GeneratedAtUnixMilli = future.UnixMilli()
	key, err := ChannelMonitorPageSnapshotKey(query)
	require.NoError(t, err)
	seedChannelMonitorPageSnapshot(t, client, key, snapshot)
	store.localMu.Lock()
	delete(store.local, key)
	store.localMu.Unlock()

	loaded, state, err := store.load(context.Background(), query, time.Now())
	assert.ErrorIs(t, err, ErrChannelMonitorPageSnapshotMissing)
	assert.Equal(t, ChannelMonitorPageSnapshotMissing, state)
	assert.Empty(t, loaded.Payload)
}

func TestChannelMonitorPageSnapshotRejectsFutureDataCutoff(t *testing.T) {
	_, client, store := newChannelMonitorPageSnapshotTestStore(t)
	query := channelMonitorPageSnapshotTestQuery("user=future-cutoff")
	snapshot, err := store.refreshSnapshot(context.Background(), query, func(context.Context) (ChannelMonitorPageSnapshot, error) {
		return channelMonitorPageSnapshotTestValue("current"), nil
	})
	require.NoError(t, err)
	snapshot.DataCutoffAt = time.Now().Add(channelMonitorPageSnapshotMaxFutureSkew + time.Second).Unix()
	key, err := ChannelMonitorPageSnapshotKey(query)
	require.NoError(t, err)
	seedChannelMonitorPageSnapshot(t, client, key, snapshot)
	store.localMu.Lock()
	delete(store.local, key)
	store.localMu.Unlock()

	loaded, state, err := store.load(context.Background(), query, time.Now())
	assert.ErrorIs(t, err, ErrChannelMonitorPageSnapshotMissing)
	assert.Equal(t, ChannelMonitorPageSnapshotMissing, state)
	assert.Empty(t, loaded.Payload)
}

func TestChannelMonitorPageSnapshotWaiterTakesOverAfterLeaseExpires(t *testing.T) {
	server, client, firstStore := newChannelMonitorPageSnapshotTestStore(t)
	secondStore := &channelMonitorPageSnapshotStore{
		readClient:  func() *redis.Client { return client },
		writeClient: func() *redis.Client { return client },
	}
	query := channelMonitorPageSnapshotTestQuery("user=takeover")
	key, err := ChannelMonitorPageSnapshotKey(query)
	require.NoError(t, err)
	_, acquired, err := firstStore.acquireLease(context.Background(), key)
	require.NoError(t, err)
	require.True(t, acquired)
	var builds atomic.Int32
	result := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := secondStore.refreshSnapshot(ctx, query, func(context.Context) (ChannelMonitorPageSnapshot, error) {
			builds.Add(1)
			return channelMonitorPageSnapshotTestValue("takeover"), nil
		})
		result <- err
	}()
	require.Eventually(t, func() bool {
		return client.Exists(context.Background(), key+":lease").Val() == 1
	}, time.Second, 5*time.Millisecond)
	server.FastForward(channelMonitorPageSnapshotLeaseTTL + time.Second)

	require.NoError(t, <-result)
	assert.Equal(t, int32(1), builds.Load())
	snapshot, state, err := secondStore.load(context.Background(), query, time.Now())
	require.NoError(t, err)
	assert.Equal(t, ChannelMonitorPageSnapshotFresh, state)
	assert.Contains(t, string(snapshot.Payload), "takeover")
}

func newChannelMonitorPageSnapshotTestStore(
	t *testing.T,
) (*miniredis.Miniredis, *redis.Client, *channelMonitorPageSnapshotStore) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := &channelMonitorPageSnapshotStore{
		readClient:  func() *redis.Client { return client },
		writeClient: func() *redis.Client { return client },
	}
	return server, client, store
}

func channelMonitorPageSnapshotTestQuery(scope string) ChannelMonitorPageSnapshotQuery {
	return ChannelMonitorPageSnapshotQuery{
		Page:            "performance",
		Version:         "v1",
		PermissionScope: scope,
		WindowStart:     100,
		WindowEnd:       200,
		Filters: map[string][]string{
			"model": {"secret-filter"},
		},
	}
}

func channelMonitorPageSnapshotTestValue(label string) ChannelMonitorPageSnapshot {
	return ChannelMonitorPageSnapshot{
		DataCutoffAt:   123,
		EventWatermark: 456,
		StatusCode:     200,
		ContentType:    "application/json; charset=utf-8",
		Payload: []byte(
			`{"success":true,"message":"","data":{"label":"` + label + `"}}`,
		),
	}
}

func seedChannelMonitorPageSnapshot(
	t *testing.T,
	client *redis.Client,
	key string,
	snapshot ChannelMonitorPageSnapshot,
) {
	t.Helper()
	payload, err := common.Marshal(snapshot)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, client.Set(ctx, key, payload, channelMonitorPageSnapshotRetention).Err())
	require.NoError(t, client.HSet(
		ctx,
		key+":meta",
		"revision", snapshot.Revision,
		"event_watermark", snapshot.EventWatermark,
		"generated_at_unix_milli", snapshot.GeneratedAtUnixMilli,
		"data_cutoff_at", snapshot.DataCutoffAt,
	).Err())
	require.NoError(t, client.Expire(ctx, key+":meta", channelMonitorPageSnapshotRetention).Err())
}
