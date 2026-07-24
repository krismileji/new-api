package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useChannelConcurrencyTestState(t *testing.T, limits map[int]int) {
	t.Helper()
	channelConcurrency.Lock()
	originalLoaded := channelConcurrency.loaded
	originalSourceDB := channelConcurrency.sourceDB
	originalGeneration := channelConcurrency.generation
	originalLoadedAt := channelConcurrency.loadedAt
	originalConfigs := channelConcurrency.configs
	originalActive := channelConcurrency.active
	configs := make(map[int]model.ChannelConcurrencyConfig, len(limits))
	for channelID, limit := range limits {
		configs[channelID] = model.ChannelConcurrencyConfig{Limit: limit, Revision: 1}
	}
	channelConcurrency.loaded = true
	channelConcurrency.sourceDB = model.DB
	channelConcurrency.generation++
	channelConcurrency.loadedAt = time.Now()
	channelConcurrency.configs = configs
	channelConcurrency.active = make(map[int]int)
	channelConcurrency.Unlock()
	t.Cleanup(func() {
		channelConcurrency.Lock()
		channelConcurrency.loaded = originalLoaded
		channelConcurrency.sourceDB = originalSourceDB
		channelConcurrency.generation = originalGeneration
		channelConcurrency.loadedAt = originalLoadedAt
		channelConcurrency.configs = originalConfigs
		channelConcurrency.active = originalActive
		channelConcurrency.Unlock()
	})
}

func useChannelConcurrencyRedis(t *testing.T) *redis.Client {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	originalEnabled := common.RedisEnabled
	originalClient := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		common.RedisEnabled = originalEnabled
		common.RDB = originalClient
		require.NoError(t, client.Close())
	})
	return client
}

func TestAcquireChannelConcurrencyLocalHonorsLimitAndIdempotentRelease(t *testing.T) {
	useChannelConcurrencyTestState(t, map[int]int{7: 2})
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
	})

	first, acquired, status, err := AcquireChannelConcurrency(t.Context(), 7)
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 1, Limit: 2}, status)

	second, acquired, status, err := AcquireChannelConcurrency(t.Context(), 7)
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 2, Limit: 2}, status)

	blocked, acquired, status, err := AcquireChannelConcurrency(t.Context(), 7)
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Nil(t, blocked)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 2, Limit: 2}, status)

	first.Release()
	first.Release()
	replacement, acquired, status, err := AcquireChannelConcurrency(t.Context(), 7)
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 2, Limit: 2}, status)

	replacement.Release()
	second.Release()
	snapshot, err := GetChannelConcurrencySnapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 0, Limit: 2}, snapshot[7])
}

func TestAcquireChannelConcurrencyRedisSharesLimitsAndActiveLeases(t *testing.T) {
	useChannelConcurrencyTestState(t, map[int]int{9: 1})
	client := useChannelConcurrencyRedis(t)
	require.NoError(t, ensureChannelConcurrencyRedisConfig(t.Context(), client, map[int]model.ChannelConcurrencyConfig{
		9: {Limit: 1, Revision: 1},
	}))

	first, acquired, status, err := AcquireChannelConcurrency(t.Context(), 9)
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 1, Limit: 1}, status)

	blocked, acquired, status, err := AcquireChannelConcurrency(t.Context(), 9)
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Nil(t, blocked)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 1, Limit: 1}, status)

	require.NoError(t, updateChannelConcurrencyRedisLimit(t.Context(), client, 9, 2, 2))
	second, acquired, status, err := AcquireChannelConcurrency(t.Context(), 9)
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 2, Limit: 2}, status)

	snapshot, err := GetChannelConcurrencySnapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 2, Limit: 2}, snapshot[9])

	first.Release()
	first.Release()
	second.Release()
	snapshot, err = GetChannelConcurrencySnapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 0, Limit: 2}, snapshot[9])
}

func TestAcquireChannelConcurrencyRedisReclaimsExpiredLease(t *testing.T) {
	useChannelConcurrencyTestState(t, map[int]int{11: 1})
	client := useChannelConcurrencyRedis(t)
	require.NoError(t, ensureChannelConcurrencyRedisConfig(t.Context(), client, map[int]model.ChannelConcurrencyConfig{
		11: {Limit: 1, Revision: 1},
	}))
	activeKey := channelConcurrencyRedisActivePrefix + "11"
	require.NoError(t, client.ZAdd(t.Context(), activeKey, &redis.Z{
		Score:  float64(time.Now().Add(-channelConcurrencyLeaseTTL - time.Second).UnixMilli()),
		Member: "expired",
	}).Err())

	lease, acquired, status, err := AcquireChannelConcurrency(context.Background(), 11)
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 1, Limit: 1}, status)
	lease.Release()
}

func TestChannelConcurrencyRedisRejectsStaleConfigUpdate(t *testing.T) {
	useChannelConcurrencyTestState(t, map[int]int{12: 1})
	client := useChannelConcurrencyRedis(t)
	require.NoError(t, ensureChannelConcurrencyRedisConfig(t.Context(), client, map[int]model.ChannelConcurrencyConfig{
		12: {Limit: 1, Revision: 1},
	}))
	require.NoError(t, updateChannelConcurrencyRedisLimit(t.Context(), client, 12, 3, 3))
	require.NoError(t, updateChannelConcurrencyRedisLimit(t.Context(), client, 12, 2, 2))

	snapshot, err := GetChannelConcurrencySnapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 0, Limit: 3}, snapshot[12])
}

func TestChannelConcurrencyRedisConfigSyncKeepsNewestRevision(t *testing.T) {
	useChannelConcurrencyTestState(t, map[int]int{13: 1})
	client := useChannelConcurrencyRedis(t)
	require.NoError(t, ensureChannelConcurrencyRedisConfig(t.Context(), client, map[int]model.ChannelConcurrencyConfig{
		13: {Limit: 1, Revision: 1},
	}))
	require.NoError(t, ensureChannelConcurrencyRedisConfig(t.Context(), client, map[int]model.ChannelConcurrencyConfig{
		13: {Limit: 3, Revision: 3},
	}))
	require.NoError(t, ensureChannelConcurrencyRedisConfig(t.Context(), client, map[int]model.ChannelConcurrencyConfig{
		13: {Limit: 2, Revision: 2},
	}))

	snapshot, err := GetChannelConcurrencySnapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 0, Limit: 3}, snapshot[13])
}
