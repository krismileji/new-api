package common

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisMonitorClientsUseIndependentPoolsAndExposeStats(t *testing.T) {
	server := miniredis.RunT(t)
	user := redis.NewClient(&redis.Options{Addr: server.Addr(), PoolSize: 2})
	write := redis.NewClient(&redis.Options{Addr: server.Addr(), PoolSize: 3})
	read := redis.NewClient(&redis.Options{Addr: server.Addr(), PoolSize: 4})
	consumer := redis.NewClient(&redis.Options{Addr: server.Addr(), PoolSize: 5})
	previous := struct {
		rdb      *redis.Client
		write    *redis.Client
		read     *redis.Client
		consumer *redis.Client
		sizes    struct {
			user, write, read, consumer int
		}
	}{RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer, struct {
		user, write, read, consumer int
	}{redisClientPoolSizes.user, redisClientPoolSizes.monitorWrite, redisClientPoolSizes.monitorRead, redisClientPoolSizes.monitorConsumer}}
	t.Cleanup(func() {
		RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer = previous.rdb, previous.write, previous.read, previous.consumer
		redisClientPoolSizes.user = previous.sizes.user
		redisClientPoolSizes.monitorWrite = previous.sizes.write
		redisClientPoolSizes.monitorRead = previous.sizes.read
		redisClientPoolSizes.monitorConsumer = previous.sizes.consumer
		for _, client := range []*redis.Client{user, write, read, consumer} {
			_ = client.Close()
		}
	})
	RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer = user, write, read, consumer
	redisClientPoolSizes.user = 2
	redisClientPoolSizes.monitorWrite = 3
	redisClientPoolSizes.monitorRead = 4
	redisClientPoolSizes.monitorConsumer = 5

	assert.Same(t, write, RedisMonitorWriteClient())
	assert.Same(t, read, RedisMonitorReadClient())
	assert.Same(t, consumer, RedisMonitorConsumerClient())
	assert.NotSame(t, user, RedisMonitorWriteClient())

	ctx := t.Context()
	require.NoError(t, user.Ping(ctx).Err())
	require.NoError(t, write.Ping(ctx).Err())
	require.NoError(t, read.Ping(ctx).Err())
	require.NoError(t, consumer.Ping(ctx).Err())
	stats := GetRedisClientPoolStats()
	assert.Equal(t, 2, stats[RedisClientRoleUser].PoolSize)
	assert.Equal(t, 3, stats[RedisClientRoleMonitorWrite].PoolSize)
	assert.Equal(t, 4, stats[RedisClientRoleMonitorRead].PoolSize)
	assert.Equal(t, 5, stats[RedisClientRoleMonitorConsumer].PoolSize)
	assert.False(t, stats[RedisClientRoleMonitorRead].Unavailable)
	assert.Equal(t, RedisClientPoolIsolationModeIsolated, RedisClientPoolIsolationMode())
	for _, role := range []RedisClientRole{
		RedisClientRoleMonitorWrite,
		RedisClientRoleMonitorRead,
		RedisClientRoleMonitorConsumer,
	} {
		assert.False(t, stats[role].Shared)
		assert.Empty(t, stats[role].SharedWith)
	}
}

func TestRedisMonitorClientsFallBackToLegacyClient(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previous := struct {
		rdb      *redis.Client
		write    *redis.Client
		read     *redis.Client
		consumer *redis.Client
	}{RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer}
	t.Cleanup(func() {
		RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer = previous.rdb, previous.write, previous.read, previous.consumer
		_ = client.Close()
	})
	RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer = client, nil, nil, nil
	assert.Same(t, client, RedisMonitorWriteClient())
	assert.Same(t, client, RedisMonitorReadClient())
	assert.Same(t, client, RedisMonitorConsumerClient())
	assert.Equal(t, RedisClientPoolIsolationModeShared, RedisClientPoolIsolationMode())
	stats := GetRedisClientPoolStats()
	for _, role := range []RedisClientRole{
		RedisClientRoleMonitorWrite,
		RedisClientRoleMonitorRead,
		RedisClientRoleMonitorConsumer,
	} {
		assert.True(t, stats[role].Shared)
		assert.Equal(t, RedisClientRoleUser, stats[role].SharedWith)
	}
}

func TestCloseRedisClientsClosesEveryRoleAfterOneCloseError(t *testing.T) {
	server := miniredis.RunT(t)
	newClient := func() *redis.Client {
		client := redis.NewClient(&redis.Options{Addr: server.Addr()})
		require.NoError(t, client.Ping(t.Context()).Err())
		return client
	}
	user := newClient()
	write := newClient()
	read := newClient()
	consumer := newClient()
	previous := struct {
		rdb      *redis.Client
		write    *redis.Client
		read     *redis.Client
		consumer *redis.Client
	}{RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer}
	t.Cleanup(func() {
		RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer = previous.rdb, previous.write, previous.read, previous.consumer
		for _, client := range []*redis.Client{user, write, read, consumer} {
			_ = client.Close()
		}
	})
	RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer = user, write, read, consumer

	require.NoError(t, write.Close())
	require.NoError(t, user.Ping(t.Context()).Err())
	require.Error(t, CloseRedisClients())
	assert.Nil(t, RDB)
	assert.Nil(t, RDBMonitorWrite)
	assert.Nil(t, RDBMonitorRead)
	assert.Nil(t, RDBMonitorConsumer)
	assert.ErrorIs(t, user.Ping(t.Context()).Err(), redis.ErrClosed)
	assert.ErrorIs(t, read.Ping(t.Context()).Err(), redis.ErrClosed)
	assert.ErrorIs(t, consumer.Ping(t.Context()).Err(), redis.ErrClosed)
	assert.NoError(t, CloseRedisClients())
}

func TestRedisMonitorConsumerPoolSizeSupportsLegacyAlias(t *testing.T) {
	t.Setenv("REDIS_MONITOR_CONSUMER_POOL_SIZE", "")
	t.Setenv("REDIS_CONSUMER_POOL_SIZE", "7")
	assert.Equal(t, 7, redisRolePoolSize(
		"REDIS_MONITOR_CONSUMER_POOL_SIZE", "REDIS_CONSUMER_POOL_SIZE", defaultRedisMonitorConsumerPoolSize,
	))

	t.Setenv("REDIS_MONITOR_CONSUMER_POOL_SIZE", "6")
	assert.Equal(t, 6, redisRolePoolSize(
		"REDIS_MONITOR_CONSUMER_POOL_SIZE", "REDIS_CONSUMER_POOL_SIZE", defaultRedisMonitorConsumerPoolSize,
	))
}

func TestRedisClientPoolStatsTrackCommandLatencyAndErrors(t *testing.T) {
	server := miniredis.RunT(t)
	metrics := &redisClientCommandMetrics{}
	client := newRedisClientWithMetrics(&redis.Options{Addr: server.Addr()}, metrics)
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Ping(t.Context()).Err())
	_, _ = client.Get(t.Context(), "missing").Result()
	stats := redisClientPoolStats(RedisClientRoleMonitorRead, client, 1, metrics)
	assert.GreaterOrEqual(t, stats.CommandCount, uint64(2))
	assert.GreaterOrEqual(t, stats.CommandErrorCount, uint64(1))
	assert.GreaterOrEqual(t, stats.CommandLatencyTotalMicros, uint64(0))
}

func TestRedisClientPoolStatsClassifyContextAndPoolTimeouts(t *testing.T) {
	server := miniredis.RunT(t)
	metrics := &redisClientCommandMetrics{}
	client := newRedisClientWithMetrics(&redis.Options{Addr: server.Addr(), PoolSize: 3}, metrics)
	t.Cleanup(func() { _ = client.Close() })
	deadlineCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_ = client.BLPop(deadlineCtx, 0, "redis-pool-deadline").Err()

	poolMetrics := &redisClientCommandMetrics{}
	poolClient := newRedisClientWithMetrics(&redis.Options{
		Addr:        server.Addr(),
		PoolSize:    1,
		PoolTimeout: 20 * time.Millisecond,
	}, poolMetrics)
	t.Cleanup(func() { _ = poolClient.Close() })
	poolCtx, poolCancel := context.WithCancel(context.Background())
	defer poolCancel()
	blockDone := make(chan error, 1)
	go func() { blockDone <- poolClient.BLPop(poolCtx, 0, "redis-pool-timeout").Err() }()
	require.Eventually(t, func() bool {
		pool := poolClient.PoolStats()
		return pool != nil && pool.TotalConns == 1 && pool.IdleConns == 0
	}, time.Second, time.Millisecond)
	_ = poolClient.Ping(context.Background()).Err()
	poolCancel()
	<-blockDone

	deadlineStats := redisClientPoolStats(RedisClientRoleMonitorRead, client, 3, metrics)
	assert.GreaterOrEqual(t, deadlineStats.ContextDeadlineCount, uint64(1))
	assert.Equal(t, RedisClientPoolDegradedReasonContextDeadline, deadlineStats.DegradedReason)
	poolStats := redisClientPoolStats(RedisClientRoleMonitorRead, poolClient, 1, poolMetrics)
	assert.GreaterOrEqual(t, poolStats.PoolTimeoutCount, uint64(1))
	assert.Equal(t, RedisClientPoolDegradedReasonPoolTimeout, poolStats.DegradedReason)
}

func TestRedisClientPoolStatsExposeInUseAndCongestion(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), PoolSize: 1})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pingDone := make(chan error, 1)
	go func() { pingDone <- client.BLPop(ctx, 0, "redis-pool-stats-block").Err() }()
	require.Eventually(t, func() bool {
		pool := client.PoolStats()
		return pool != nil && pool.TotalConns == 1 && pool.IdleConns == 0
	}, time.Second, time.Millisecond)

	stats := redisClientPoolStats(RedisClientRoleMonitorRead, client, 1, nil)
	assert.Equal(t, uint32(1), stats.InUse)
	assert.True(t, stats.PoolCongested)
	assert.Equal(t, RedisClientPoolDegradedReasonPoolCongested, stats.DegradedReason)
	cancel()
	assert.Error(t, <-pingDone)
}

func TestRedisClientPoolIsolationReflectsRoleClientIdentity(t *testing.T) {
	server := miniredis.RunT(t)
	clients := make([]*redis.Client, 4)
	for index := range clients {
		clients[index] = redis.NewClient(&redis.Options{Addr: server.Addr()})
	}
	t.Cleanup(func() {
		for _, client := range clients {
			_ = client.Close()
		}
	})
	previous := struct {
		rdb, write, read, consumer *redis.Client
	}{RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer}
	t.Cleanup(func() {
		RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer = previous.rdb, previous.write, previous.read, previous.consumer
	})
	RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer = clients[0], clients[1], clients[2], clients[3]
	assert.True(t, RedisClientPoolIsolationEnabled())
	assert.Equal(t, RedisClientPoolIsolationModeIsolated, RedisClientPoolIsolationMode())

	RDBMonitorRead = RDB
	assert.False(t, RedisClientPoolIsolationEnabled())
	assert.Equal(t, RedisClientPoolIsolationModeMixed, RedisClientPoolIsolationMode())
}

func TestRedisClientPoolIsolationModeReportsUnavailableWithoutUserClient(t *testing.T) {
	previous := struct {
		rdb, write, read, consumer *redis.Client
	}{RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer}
	t.Cleanup(func() {
		RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer = previous.rdb, previous.write, previous.read, previous.consumer
	})
	RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer = nil, nil, nil, nil

	assert.False(t, RedisClientPoolIsolationEnabled())
	assert.Equal(t, RedisClientPoolIsolationModeUnavailable, RedisClientPoolIsolationMode())
}

func TestInitRedisClientCreatesIsolatedRolePools(t *testing.T) {
	server := miniredis.RunT(t)
	previous := struct {
		enabled bool
		rdb     *redis.Client
		write   *redis.Client
		read    *redis.Client
		consume *redis.Client
	}{RedisEnabled, RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer}
	t.Cleanup(func() {
		_ = CloseRedisClients()
		RedisEnabled = previous.enabled
		RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer = previous.rdb, previous.write, previous.read, previous.consume
	})
	t.Setenv("REDIS_CONN_STRING", fmt.Sprintf("redis://%s/0", server.Addr()))
	t.Setenv("SYNC_FREQUENCY", "60")
	t.Setenv("REDIS_POOL_SIZE", "2")
	t.Setenv("REDIS_MONITOR_WRITE_POOL_SIZE", "3")
	t.Setenv("REDIS_MONITOR_READ_POOL_SIZE", "4")
	t.Setenv("REDIS_MONITOR_CONSUMER_POOL_SIZE", "5")
	t.Setenv("REDIS_CLIENT_POOL_ISOLATION", "true")
	require.NoError(t, InitRedisClient())
	assert.NotNil(t, RDB)
	assert.NotNil(t, RDBMonitorWrite)
	assert.NotNil(t, RDBMonitorRead)
	assert.NotNil(t, RDBMonitorConsumer)
	assert.NotSame(t, RDB, RDBMonitorWrite)
	assert.NotSame(t, RDB, RDBMonitorRead)
	assert.NotSame(t, RDB, RDBMonitorConsumer)
	stats := GetRedisClientPoolStats()
	assert.Equal(t, 2, stats[RedisClientRoleUser].PoolSize)
	assert.Equal(t, 3, stats[RedisClientRoleMonitorWrite].PoolSize)
	assert.Equal(t, 4, stats[RedisClientRoleMonitorRead].PoolSize)
	assert.Equal(t, 5, stats[RedisClientRoleMonitorConsumer].PoolSize)
}

func TestInitRedisClientSupportsPoolIsolationRollback(t *testing.T) {
	server := miniredis.RunT(t)
	previous := struct {
		enabled bool
		rdb     *redis.Client
		write   *redis.Client
		read    *redis.Client
		consume *redis.Client
	}{RedisEnabled, RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer}
	t.Cleanup(func() {
		_ = CloseRedisClients()
		RedisEnabled = previous.enabled
		RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer = previous.rdb, previous.write, previous.read, previous.consume
	})
	t.Setenv("REDIS_CONN_STRING", fmt.Sprintf("redis://%s/0", server.Addr()))
	t.Setenv("SYNC_FREQUENCY", "60")
	t.Setenv("REDIS_POOL_SIZE", "6")
	t.Setenv("REDIS_CLIENT_POOL_ISOLATION", "false")
	require.NoError(t, InitRedisClient())

	assert.Nil(t, RDBMonitorWrite)
	assert.Nil(t, RDBMonitorRead)
	assert.Nil(t, RDBMonitorConsumer)
	assert.Same(t, RDB, RedisMonitorWriteClient())
	assert.Same(t, RDB, RedisMonitorReadClient())
	assert.Same(t, RDB, RedisMonitorConsumerClient())
	assert.Equal(t, RedisClientPoolIsolationModeShared, RedisClientPoolIsolationMode())
	stats := GetRedisClientPoolStats()
	for _, role := range []RedisClientRole{
		RedisClientRoleUser,
		RedisClientRoleMonitorWrite,
		RedisClientRoleMonitorRead,
		RedisClientRoleMonitorConsumer,
	} {
		assert.Equal(t, 6, stats[role].PoolSize)
		assert.False(t, stats[role].Unavailable)
		if role != RedisClientRoleUser {
			assert.True(t, stats[role].Shared)
			assert.Equal(t, RedisClientRoleUser, stats[role].SharedWith)
		}
	}
}
