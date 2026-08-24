package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChannelMonitorRedisRealtimeStatusReusesShortLivedSnapshot(t *testing.T) {
	_, client := setupChannelMonitorRedisRealtimeStatusCacheTest(t)
	ctx := context.Background()
	require.NoError(t, client.XGroupCreateMkStream(
		ctx,
		ChannelMonitorRedisEventStream,
		ChannelMonitorRedisConsumerGroup,
		"0",
	).Err())
	require.NoError(t, client.Set(
		ctx,
		ChannelMonitorRedisConsumerHeartbeatKey,
		"status-cache-consumer",
		time.Minute,
	).Err())

	initial := GetChannelMonitorRedisRealtimeStatus(ctx)
	assert.Equal(t, ChannelMonitorRedisStatusAvailable, initial.RedisStatus)
	require.NotNil(t, initial.DegradedReasons)
	assert.Empty(t, initial.DegradedReasons)
	assert.Zero(t, initial.PendingCount)

	_, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: ChannelMonitorRedisEventStream,
		Values: map[string]interface{}{"event": "status-cache"},
	}).Result()
	require.NoError(t, err)
	read, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    ChannelMonitorRedisConsumerGroup,
		Consumer: "status-cache-consumer",
		Streams:  []string{ChannelMonitorRedisEventStream, ">"},
		Count:    1,
		Block:    -1,
	}).Result()
	require.NoError(t, err)
	require.Len(t, read, 1)
	require.Len(t, read[0].Messages, 1)

	withinTTL := GetChannelMonitorRedisRealtimeStatus(ctx)
	require.NotNil(t, withinTTL.DegradedReasons)
	assert.Empty(t, withinTTL.DegradedReasons)
	assert.Zero(t, withinTTL.PendingCount, "a page fan-out should reuse the current status snapshot")

	var refreshed ChannelMonitorRedisRealtimeStatus
	require.Eventually(t, func() bool {
		refreshed = GetChannelMonitorRedisRealtimeStatus(ctx)
		return refreshed.PendingCount == 1
	}, channelMonitorRedisRealtimeStatusCacheTTL+500*time.Millisecond, 10*time.Millisecond)
	assert.Equal(t, ChannelMonitorRedisStatusAvailable, refreshed.RedisStatus)
}

func TestGetChannelMonitorRedisRealtimeStatusCoalescesConcurrentRefreshes(t *testing.T) {
	server, client := setupChannelMonitorRedisRealtimeStatusCacheTest(t)
	ctx := context.Background()
	require.NoError(t, client.XGroupCreateMkStream(
		ctx,
		ChannelMonitorRedisEventStream,
		ChannelMonitorRedisConsumerGroup,
		"0",
	).Err())
	require.NoError(t, client.Set(
		ctx,
		ChannelMonitorRedisConsumerHeartbeatKey,
		"status-cache-consumer",
		time.Minute,
	).Err())

	queryStarted := make(chan struct{})
	releaseQuery := make(chan struct{})
	client.AddHook(&blockFirstRedisPingHook{
		started: queryStarted,
		release: releaseQuery,
	})
	baseline := server.CommandCount()
	const callers = 16
	results := make([]ChannelMonitorRedisRealtimeStatus, callers)
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(callers)
	done.Add(callers)
	start := make(chan struct{})
	for index := 0; index < callers; index++ {
		go func(index int) {
			defer done.Done()
			ready.Done()
			<-start
			results[index] = GetChannelMonitorRedisRealtimeStatus(ctx)
		}(index)
	}
	ready.Wait()
	close(start)
	select {
	case <-queryStarted:
	case <-time.After(time.Second):
		t.Fatal("status refresh did not reach Redis")
	}
	close(releaseQuery)
	done.Wait()

	for _, result := range results {
		assert.Equal(t, ChannelMonitorRedisStatusAvailable, result.RedisStatus)
		assert.False(t, result.RealtimeDegraded)
	}
	// The empty status reads the monitor Stream plus the reliable cost Stream
	// and its consumer group. A concurrent page fan-out must execute that
	// nine-command Redis read set once, not once per caller.
	assert.Equal(t, 9, server.CommandCount()-baseline)
}

func TestCloneChannelMonitorRedisRealtimeStatusPreservesIndependentPoolStats(t *testing.T) {
	status := ChannelMonitorRedisRealtimeStatus{
		RedisPoolStats: map[common.RedisClientRole]common.RedisClientPoolStats{
			common.RedisClientRoleMonitorRead: {
				Role: common.RedisClientRoleMonitorRead, PoolSize: 7,
			},
		},
	}

	cloned := cloneChannelMonitorRedisRealtimeStatus(status)
	require.Contains(t, cloned.RedisPoolStats, common.RedisClientRoleMonitorRead)
	assert.Equal(t, 7, cloned.RedisPoolStats[common.RedisClientRoleMonitorRead].PoolSize)
	cloned.RedisPoolStats[common.RedisClientRoleMonitorRead] = common.RedisClientPoolStats{PoolSize: 9}
	assert.Equal(t, 7, status.RedisPoolStats[common.RedisClientRoleMonitorRead].PoolSize)
}

type blockFirstRedisPingHook struct {
	once    sync.Once
	started chan struct{}
	release <-chan struct{}
}

func (h *blockFirstRedisPingHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	if strings.EqualFold(cmd.Name(), "ping") {
		h.once.Do(func() {
			close(h.started)
			<-h.release
		})
	}
	return ctx, nil
}

func (h *blockFirstRedisPingHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (h *blockFirstRedisPingHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (h *blockFirstRedisPingHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func setupChannelMonitorRedisRealtimeStatusCacheTest(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousEnabled := common.RedisEnabled
	previousClient := common.RDB
	previousMonitorRead := common.RDBMonitorRead
	common.RedisEnabled = true
	common.RDB = client
	common.RDBMonitorRead = nil
	resetChannelMonitorRedisRealtimeStatusCache()
	t.Cleanup(func() {
		resetChannelMonitorRedisRealtimeStatusCache()
		common.RedisEnabled = previousEnabled
		common.RDB = previousClient
		common.RDBMonitorRead = previousMonitorRead
		_ = client.Close()
	})
	return server, client
}

func resetChannelMonitorRedisRealtimeStatusCache() {
	invalidateChannelMonitorRedisRealtimeStatusCache()
}
