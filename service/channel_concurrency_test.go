package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useChannelConcurrencyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := model.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-concurrency.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.ChannelRatioMonitor{}))
	t.Cleanup(func() {
		model.DB = originalDB
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func useChannelConcurrencyTestState(t *testing.T, limits map[int]int) {
	t.Helper()
	channelConcurrency.Lock()
	originalLoaded := channelConcurrency.loaded
	originalSourceDB := channelConcurrency.sourceDB
	originalGeneration := channelConcurrency.generation
	originalLoadedAt := channelConcurrency.loadedAt
	originalConfigs := channelConcurrency.configs
	originalActive := channelConcurrency.active
	originalRPM := channelConcurrency.rpm
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
	channelConcurrency.rpm = make(map[int][]int64)
	channelConcurrency.Unlock()
	t.Cleanup(func() {
		channelConcurrency.Lock()
		channelConcurrency.loaded = originalLoaded
		channelConcurrency.sourceDB = originalSourceDB
		channelConcurrency.generation = originalGeneration
		channelConcurrency.loadedAt = originalLoadedAt
		channelConcurrency.configs = originalConfigs
		channelConcurrency.active = originalActive
		channelConcurrency.rpm = originalRPM
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

func useUnavailableChannelConcurrencyRedis(t *testing.T) {
	t.Helper()
	originalEnabled := common.RedisEnabled
	originalClient := common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = originalEnabled
		common.RDB = originalClient
	})
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

func TestAcquireChannelConcurrencyLocalHonorsRPMLimit(t *testing.T) {
	useChannelConcurrencyTestState(t, nil)
	channelConcurrency.Lock()
	channelConcurrency.configs[18] = model.ChannelConcurrencyConfig{RPMLimit: 1, Revision: 1}
	channelConcurrency.Unlock()

	first, acquired, status, err := AcquireChannelConcurrency(t.Context(), 18)
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 1, Limit: 0, CurrentRPM: 1, RPMLimit: 1}, status)
	defer first.Release()

	second, acquired, status, err := AcquireChannelConcurrency(t.Context(), 18)
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Nil(t, second)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 1, Limit: 0, CurrentRPM: 1, RPMLimit: 1}, status)
}

func TestAcquireChannelConcurrencyUsesEitherConcurrencyOrRPMLimit(t *testing.T) {
	useChannelConcurrencyTestState(t, nil)
	channelConcurrency.Lock()
	channelConcurrency.configs[19] = model.ChannelConcurrencyConfig{Limit: 1, RPMLimit: 10, Revision: 1}
	channelConcurrency.Unlock()

	first, acquired, _, err := AcquireChannelConcurrency(t.Context(), 19)
	require.NoError(t, err)
	require.True(t, acquired)
	defer first.Release()

	second, acquired, status, err := AcquireChannelConcurrency(t.Context(), 19)
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Nil(t, second)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 1, Limit: 1, CurrentRPM: 1, RPMLimit: 10}, status)
}

func TestAcquireChannelConcurrencyRedisCountsUnlimitedChannelWhenRedisUnavailable(t *testing.T) {
	useChannelConcurrencyTestState(t, nil)
	useUnavailableChannelConcurrencyRedis(t)

	lease, acquired, status, err := AcquireChannelConcurrency(t.Context(), 8)
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 1, Limit: 0}, status)
	require.NotNil(t, lease)
	lease.Release()
}

func TestAcquireChannelConcurrencyRedisFailsClosedForConfiguredChannel(t *testing.T) {
	useChannelConcurrencyTestState(t, map[int]int{8: 1})
	useUnavailableChannelConcurrencyRedis(t)

	lease, acquired, status, err := AcquireChannelConcurrency(t.Context(), 8)
	require.ErrorContains(t, err, "Redis 客户端未初始化")
	assert.False(t, acquired)
	assert.Nil(t, lease)
	assert.Equal(t, ChannelConcurrencyStatus{}, status)
}

func TestAcquireChannelConcurrencyRefreshesBeforeUnlimitedFastPath(t *testing.T) {
	for _, test := range []struct {
		name   string
		loaded bool
	}{
		{
			name: "first load",
		},
		{
			name:   "minute refresh",
			loaded: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := useChannelConcurrencyTestDB(t)
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{
				ChannelId:           14,
				ConcurrencyLimit:    1,
				ConcurrencyRevision: 1,
			}).Error)
			useChannelConcurrencyTestState(t, nil)
			channelConcurrency.Lock()
			channelConcurrency.loaded = test.loaded
			if test.loaded {
				channelConcurrency.loadedAt = time.Now().Add(-channelConcurrencyConfigRefresh)
			}
			channelConcurrency.Unlock()
			useUnavailableChannelConcurrencyRedis(t)

			lease, acquired, status, err := AcquireChannelConcurrency(t.Context(), 14)
			require.ErrorContains(t, err, "Redis 客户端未初始化")
			assert.False(t, acquired)
			assert.Nil(t, lease)
			assert.Equal(t, ChannelConcurrencyStatus{}, status)
		})
	}
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

func TestAcquireChannelConcurrencyRedisHonorsRPMLimit(t *testing.T) {
	useChannelConcurrencyTestState(t, nil)
	client := useChannelConcurrencyRedis(t)
	require.NoError(t, ensureChannelConcurrencyRedisConfig(t.Context(), client, map[int]model.ChannelConcurrencyConfig{
		20: {RPMLimit: 2, Revision: 1},
	}))

	first, acquired, status, err := AcquireChannelConcurrency(t.Context(), 20)
	require.NoError(t, err)
	require.True(t, acquired)
	defer first.Release()
	assert.Equal(t, ChannelConcurrencyStatus{Active: 1, Limit: 0, CurrentRPM: 1, RPMLimit: 2}, status)

	second, acquired, status, err := AcquireChannelConcurrency(t.Context(), 20)
	require.NoError(t, err)
	require.True(t, acquired)
	defer second.Release()
	assert.Equal(t, ChannelConcurrencyStatus{Active: 2, Limit: 0, CurrentRPM: 2, RPMLimit: 2}, status)

	third, acquired, status, err := AcquireChannelConcurrency(t.Context(), 20)
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Nil(t, third)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 2, Limit: 0, CurrentRPM: 2, RPMLimit: 2}, status)
}

func TestAcquireChannelConcurrencyRedisHonorsLimitWhenLocalCacheIsStaleUnlimited(t *testing.T) {
	useChannelConcurrencyTestState(t, nil)
	client := useChannelConcurrencyRedis(t)
	require.NoError(t, ensureChannelConcurrencyRedisConfig(t.Context(), client, map[int]model.ChannelConcurrencyConfig{
		17: {Limit: 1, Revision: 1},
	}))

	first, acquired, status, err := AcquireChannelConcurrency(t.Context(), 17)
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 1, Limit: 1}, status)
	t.Cleanup(first.Release)

	second, acquired, status, err := AcquireChannelConcurrency(t.Context(), 17)
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Nil(t, second)
	assert.Equal(t, ChannelConcurrencyStatus{Active: 1, Limit: 1}, status)
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
