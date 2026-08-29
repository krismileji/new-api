package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyChannelMonitorRedisPoolHealthExposesIsolationAndRoleStats(t *testing.T) {
	server := miniredis.RunT(t)
	clients := make([]*redis.Client, 4)
	for index := range clients {
		clients[index] = redis.NewClient(&redis.Options{Addr: server.Addr(), PoolSize: 2})
		require.NoError(t, clients[index].Ping(context.Background()).Err())
	}
	t.Cleanup(func() {
		for _, client := range clients {
			_ = client.Close()
		}
	})
	previous := struct {
		rdb, write, read, consumer *redis.Client
	}{common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer}
	t.Cleanup(func() {
		common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer = previous.rdb, previous.write, previous.read, previous.consumer
	})
	common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer = clients[0], clients[1], clients[2], clients[3]

	status := ChannelMonitorRedisRealtimeStatus{DegradedReasons: make([]string, 0)}
	applyChannelMonitorRedisPoolHealth(&status)

	assert.True(t, status.RedisPoolIsolation)
	assert.Equal(t, common.RedisClientPoolIsolationModeIsolated, status.RedisPoolIsolationMode)
	assert.False(t, status.RedisPoolShared)
	assert.Empty(t, status.RedisPoolDegradedRoles)
	require.Len(t, status.RedisPoolStats, 4)
	for _, role := range []common.RedisClientRole{
		common.RedisClientRoleUser,
		common.RedisClientRoleMonitorWrite,
		common.RedisClientRoleMonitorRead,
		common.RedisClientRoleMonitorConsumer,
	} {
		stats, ok := status.RedisPoolStats[role]
		require.True(t, ok)
		assert.Equal(t, role, stats.Role)
		assert.Equal(t, uint32(0), stats.InUse)
		assert.False(t, stats.PoolCongested)
		assert.Empty(t, stats.DegradedReason)
	}
	assert.Empty(t, status.DegradedReasons)
}

func TestChannelMonitorRedisUnavailableStatusIncludesPoolHealthShape(t *testing.T) {
	previous := struct {
		rdb, write, read, consumer *redis.Client
	}{common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer}
	common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer = nil, nil, nil, nil
	t.Cleanup(func() {
		common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer = previous.rdb, previous.write, previous.read, previous.consumer
	})

	status := channelMonitorRedisUnavailableStatus()
	assert.False(t, status.RedisPoolIsolation)
	assert.Equal(t, common.RedisClientPoolIsolationModeUnavailable, status.RedisPoolIsolationMode)
	assert.False(t, status.RedisPoolShared)
	require.Len(t, status.RedisPoolStats, 4)
	for _, stats := range status.RedisPoolStats {
		assert.True(t, stats.Unavailable)
		assert.Equal(t, common.RedisClientPoolDegradedReasonUnavailable, stats.DegradedReason)
	}
}

func TestApplyChannelMonitorRedisPoolHealthMarksSharedRoles(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), PoolSize: 1})
	require.NoError(t, client.Ping(context.Background()).Err())
	t.Cleanup(func() { _ = client.Close() })
	previous := struct {
		rdb, write, read, consumer *redis.Client
	}{common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer}
	t.Cleanup(func() {
		common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer = previous.rdb, previous.write, previous.read, previous.consumer
	})
	common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer = client, nil, nil, nil

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	blocked := make(chan error, 1)
	go func() { blocked <- client.BLPop(ctx, 0, "pool-health-shared").Err() }()
	require.Eventually(t, func() bool {
		pool := client.PoolStats()
		return pool != nil && pool.TotalConns == 1 && pool.IdleConns == 0
	}, time.Second, time.Millisecond)

	status := ChannelMonitorRedisRealtimeStatus{DegradedReasons: make([]string, 0)}
	applyChannelMonitorRedisPoolHealth(&status)

	assert.False(t, status.RedisPoolIsolation)
	assert.Equal(t, common.RedisClientPoolIsolationModeShared, status.RedisPoolIsolationMode)
	assert.True(t, status.RedisPoolShared)
	assert.Contains(t, status.DegradedReasons, ChannelMonitorRedisDegradedReasonPoolCongested)
	require.Len(t, status.RedisPoolDegradedRoles, 4)
	for _, degraded := range status.RedisPoolDegradedRoles {
		assert.Equal(t, common.RedisClientPoolDegradedReasonPoolCongested, degraded.Reason)
		if degraded.Role != common.RedisClientRoleUser {
			assert.True(t, degraded.Shared)
			assert.Equal(t, common.RedisClientRoleUser, degraded.SharedWith)
		}
	}

	cancel()
	assert.Error(t, <-blocked)
}
