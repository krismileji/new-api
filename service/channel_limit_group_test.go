package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useSharedLimitFixture(t *testing.T, backend string) *model.ChannelLimitGroup {
	t.Helper()
	originalDB, originalRedis, originalClient := model.DB, common.RedisEnabled, common.RDB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "limits.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelRatioMonitor{}, &model.ChannelLimitGroup{}, &model.ChannelLimitGroupTier{}, &model.ChannelLimitGroupMember{}, &model.ChannelLimitGroupRevision{}))
	model.DB = db
	common.RedisEnabled = false
	channelLimitConfig.Lock()
	channelLimitConfig.db = nil
	channelLimitConfig.registry = channelLimitRegistry{}
	channelLimitConfig.local = make(map[int64]*channelLimitLocalPool)
	channelLimitConfig.Unlock()
	useChannelConcurrencyTestState(t, nil)
	t.Cleanup(func() {
		model.DB = originalDB
		common.RedisEnabled = originalRedis
		common.RDB = originalClient
		sqlDB, closeErr := db.DB()
		require.NoError(t, closeErr)
		require.NoError(t, sqlDB.Close())
		channelLimitConfig.Lock()
		channelLimitConfig.db = nil
		channelLimitConfig.registry = channelLimitRegistry{}
		channelLimitConfig.local = make(map[int64]*channelLimitLocalPool)
		channelLimitConfig.Unlock()
	})
	if backend == "redis" {
		useChannelConcurrencyRedis(t)
	}
	if backend == "real" {
		address := os.Getenv("TEST_SHARED_LIMIT_REDIS_ADDR")
		if address == "" {
			t.Skip("TEST_SHARED_LIMIT_REDIS_ADDR 未配置")
		}
		client := redis.NewClient(&redis.Options{Addr: address, DB: 14})
		require.NoError(t, client.Ping(t.Context()).Err())
		// DB 14 on the explicitly supplied disposable Redis is reserved for this test.
		require.NoError(t, client.FlushDB(t.Context()).Err())
		common.RedisEnabled = true
		common.RDB = client
		t.Cleanup(func() {
			require.NoError(t, client.FlushDB(context.Background()).Err())
			require.NoError(t, client.Close())
		})
	}
	for id := 1; id <= 4; id++ {
		require.NoError(t, db.Create(&model.Channel{Id: id, Name: fmt.Sprintf("渠道%d", id), Key: "test", Status: 1}).Error)
	}
	group := &model.ChannelLimitGroup{Name: "共享上游", Enabled: true, ConcurrencyLimit: 10, RPMLimit: 300,
		Tiers:   []model.ChannelLimitGroupTier{{Priority: 100, ReservedConcurrency: 3, ReservedRPM: 90}, {Priority: 0}},
		Members: []model.ChannelLimitGroupMember{{ChannelID: 1, Priority: 100}, {ChannelID: 2, Priority: 100}, {ChannelID: 3, Priority: 0}, {ChannelID: 4, Priority: 0}}}
	require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
	return group
}

func TestSharedLimitReservationAcrossChannels(t *testing.T) {
	for _, backend := range []string{"local", "redis", "real"} {
		t.Run(backend, func(t *testing.T) {
			useSharedLimitFixture(t, backend)
			for i := 0; i < 7; i++ {
				lease, ok, _, err := AcquireChannelConcurrency(t.Context(), 3+i%2)
				require.NoError(t, err)
				require.True(t, ok)
				t.Cleanup(lease.Release)
			}
			denied, ok, status, err := AcquireChannelConcurrency(t.Context(), 4)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Nil(t, denied)
			require.NotNil(t, status.Shared)
			assert.Equal(t, "reserved_concurrency", status.Shared.Reason)
			for i := 0; i < 3; i++ {
				lease, ok, _, err := AcquireChannelConcurrency(t.Context(), 1+i%2)
				require.NoError(t, err)
				require.True(t, ok)
				t.Cleanup(lease.Release)
			}
			_, ok, status, err = AcquireChannelConcurrency(t.Context(), 1)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "group_concurrency", status.Shared.Reason)
		})
	}
}

func TestSharedLimitRPMIsNotReturnedOnRelease(t *testing.T) {
	for _, backend := range []string{"local", "redis", "real"} {
		t.Run(backend, func(t *testing.T) {
			group := useSharedLimitFixture(t, backend)
			group.RPMLimit = 3
			group.Tiers[0].ReservedRPM = 1
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			for _, id := range []int{3, 4} {
				lease, ok, _, err := AcquireChannelConcurrency(t.Context(), id)
				require.NoError(t, err)
				require.True(t, ok)
				lease.Release()
				lease.Release()
			}
			_, ok, status, err := AcquireChannelConcurrency(t.Context(), 3)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "reserved_rpm", status.Shared.Reason)
			lease, ok, _, err := AcquireChannelConcurrency(t.Context(), 1)
			require.NoError(t, err)
			require.True(t, ok)
			lease.Release()
			_, ok, status, err = AcquireChannelConcurrency(t.Context(), 2)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "group_rpm", status.Shared.Reason)
		})
	}
}

func TestSharedLimitPriorityAndFIFO(t *testing.T) {
	for _, backend := range []string{"local", "redis", "real"} {
		t.Run(backend, func(t *testing.T) {
			group := useSharedLimitFixture(t, backend)
			group.ConcurrencyLimit = 4
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			held := make([]*ChannelConcurrencyLease, 0, 4)
			for i := 0; i < 4; i++ {
				lease, ok, _, err := AcquireChannelConcurrency(t.Context(), 1)
				require.NoError(t, err)
				require.True(t, ok)
				held = append(held, lease)
				t.Cleanup(lease.Release)
			}
			lowCtx, low := NewChannelAdmission(t.Context(), time.Now().Add(time.Minute))
			low.Waiting = true
			t.Cleanup(low.Close)
			highCtx, high := NewChannelAdmission(t.Context(), time.Now().Add(time.Minute))
			high.Waiting = true
			t.Cleanup(high.Close)
			nextCtx, next := NewChannelAdmission(t.Context(), time.Now().Add(time.Minute))
			next.Waiting = true
			t.Cleanup(next.Close)
			for _, input := range []struct {
				ctx context.Context
				id  int
			}{{lowCtx, 3}, {highCtx, 1}, {nextCtx, 2}} {
				_, ok, _, err := AcquireChannelConcurrency(input.ctx, input.id)
				require.NoError(t, err)
				assert.False(t, ok)
			}
			held[0].Release()
			_, ok, status, err := AcquireChannelConcurrency(lowCtx, 3)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "priority_wait", status.Shared.Reason)
			_, ok, status, err = AcquireChannelConcurrency(nextCtx, 2)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "priority_wait", status.Shared.Reason)
			lease, ok, _, err := AcquireChannelConcurrency(highCtx, 1)
			require.NoError(t, err)
			require.True(t, ok)
			lease.Release()
			lease, ok, _, err = AcquireChannelConcurrency(nextCtx, 2)
			require.NoError(t, err)
			require.True(t, ok)
			lease.Release()
		})
	}
}

func TestSharedLimitAtomicContention(t *testing.T) {
	useSharedLimitFixture(t, "real")
	require.NoError(t, ensureChannelLimitRegistry(t.Context()))
	_, err := loadChannelConcurrencyLimits(t.Context(), true)
	require.NoError(t, err)
	require.NoError(t, ensureChannelConcurrencyRedisConfig(t.Context(), common.RDB, getChannelConcurrencyConfigsSnapshot()))
	clients := []*redis.Client{redis.NewClient(common.RDB.Options()), redis.NewClient(common.RDB.Options())}
	for _, client := range clients {
		t.Cleanup(func() { require.NoError(t, client.Close()) })
	}
	type result struct {
		lease *ChannelConcurrencyLease
		ok    bool
		err   error
	}
	results := make(chan result, 12)
	start := make(chan struct{})
	var workers sync.WaitGroup
	// Twelve simultaneous low-priority requests compete for exactly seven slots.
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func(id int, client *redis.Client) {
			defer workers.Done()
			<-start
			lease, ok, _, err := acquireChannelLimitRedis(t.Context(), client, id)
			results <- result{lease, ok, err}
		}(3+i%2, clients[i%2])
	}
	close(start)
	workers.Wait()
	close(results)
	admitted := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.ok {
			admitted++
			t.Cleanup(result.lease.Release)
		}
	}
	assert.Equal(t, 7, admitted)
}

func TestSharedLimitExplicitPauseAndChannelDeletionProtection(t *testing.T) {
	group := useSharedLimitFixture(t, "redis")
	lease, ok, _, err := AcquireChannelConcurrency(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, ok)
	t.Cleanup(lease.Release)
	group.Enabled = false
	require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
	_, ok, status, err := AcquireChannelConcurrency(t.Context(), 2)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, "paused", status.Shared.Reason)
	group.Members = group.Members[:3]
	require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
	_, err = model.BatchDeleteChannels([]int{1})
	require.ErrorContains(t, err, "共享限流组")
}

func TestSharedLimitThreeTiersCheckCumulativeUse(t *testing.T) {
	for _, backend := range []string{"local", "redis", "real"} {
		t.Run(backend, func(t *testing.T) {
			group := useSharedLimitFixture(t, backend)
			group.Tiers = []model.ChannelLimitGroupTier{{Priority: 100, ReservedConcurrency: 3}, {Priority: 50, ReservedConcurrency: 2}, {Priority: 0}}
			group.Members[1].Priority = 50
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			for i := 0; i < 7; i++ {
				lease, ok, _, err := AcquireChannelConcurrency(t.Context(), 2)
				require.NoError(t, err)
				require.True(t, ok)
				t.Cleanup(lease.Release)
			}
			_, ok, status, err := AcquireChannelConcurrency(t.Context(), 3)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "reserved_concurrency", status.Shared.Reason)
			lease, ok, _, err := AcquireChannelConcurrency(t.Context(), 1)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(lease.Release)
		})
	}
}

func TestSharedLimitSingleChannelAndProbeRespectReservations(t *testing.T) {
	for _, backend := range []string{"local", "redis", "real"} {
		t.Run(backend, func(t *testing.T) {
			useSharedLimitFixture(t, backend)
			_, err := SaveChannelConcurrencyLimit(t.Context(), 1, 1)
			require.NoError(t, err)
			held, ok, _, err := AcquireChannelConcurrency(t.Context(), 1)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(held.Release)
			_, ok, status, err := AcquireChannelConcurrency(t.Context(), 1)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "channel_concurrency", status.Shared.Reason)
			for i := 0; i < 7; i++ {
				lease, ok, _, err := AcquireChannelConcurrency(t.Context(), 3)
				require.NoError(t, err)
				require.True(t, ok)
				t.Cleanup(lease.Release)
			}
			_, err = AcquireChannelLimitProbeLease(t.Context(), 2)
			assert.ErrorIs(t, err, ErrChannelLimitProbeBusy)
			lease, ok, _, err := AcquireChannelConcurrency(t.Context(), 2)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(lease.Release)
			duplicate, err := AcquireChannelLimitProbeLease(WithChannelConcurrencyLease(t.Context(), 2, lease), 2)
			require.NoError(t, err)
			assert.Nil(t, duplicate)
		})
	}
}

func TestSharedLimitAdmissionRetryIsIdempotent(t *testing.T) {
	for _, backend := range []string{"local", "redis", "real"} {
		t.Run(backend, func(t *testing.T) {
			useSharedLimitFixture(t, backend)
			ctx, admission := NewChannelAdmission(t.Context(), time.Now().Add(time.Minute))
			t.Cleanup(admission.Close)
			first, ok, status, err := AcquireChannelConcurrency(ctx, 1)
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, 1, status.Shared.RPM)
			t.Cleanup(first.Release)
			repeated, ok, status, err := AcquireChannelConcurrency(ctx, 1)
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, 1, status.Shared.RPM)
			t.Cleanup(repeated.Release)
			first.Release()
			repeated.Release()
			lease, ok, status, err := AcquireChannelConcurrency(t.Context(), 2)
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, 1, status.Shared.Active)
			assert.Equal(t, 2, status.Shared.RPM)
			t.Cleanup(lease.Release)
		})
	}
}

func TestSharedLimitWaitingCancellationAndDeadlineDoNotConsumeQuota(t *testing.T) {
	for _, backend := range []string{"local", "redis", "real"} {
		t.Run(backend, func(t *testing.T) {
			group := useSharedLimitFixture(t, backend)
			group.ConcurrencyLimit = 3
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			for i := 0; i < 3; i++ {
				lease, ok, _, err := AcquireChannelConcurrency(t.Context(), 1)
				require.NoError(t, err)
				require.True(t, ok)
				t.Cleanup(lease.Release)
			}
			ctx, cancel := context.WithCancel(t.Context())
			waitingCtx, waiting := NewChannelAdmission(ctx, time.Now().Add(time.Minute))
			waiting.Waiting = true
			_, ok, _, err := AcquireChannelConcurrency(waitingCtx, 2)
			require.NoError(t, err)
			assert.False(t, ok)
			cancel()
			_, _, _, err = AcquireChannelConcurrency(waitingCtx, 2)
			require.ErrorIs(t, err, context.Canceled)
			expiredCtx, expired := NewChannelAdmission(t.Context(), time.Now().Add(-time.Second))
			expired.Waiting = true
			_, ok, status, err := AcquireChannelConcurrency(expiredCtx, 2)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "wait_timeout", status.Shared.Reason)
			views, err := ListChannelLimitGroupViews(t.Context())
			require.NoError(t, err)
			assert.Equal(t, 0, views[0].Runtime.Waiting)
			assert.Equal(t, 3, views[0].Runtime.RPM)
			assert.Contains(t, views[0].Runtime.Tiers, ChannelLimitTierUsage{Priority: 100, Active: 3, RPM: 3})
		})
	}
}

func TestSharedLimitQueueIsBoundedAndCancelledSlotCanBeReused(t *testing.T) {
	for _, backend := range []string{"local", "real"} {
		t.Run(backend, func(t *testing.T) {
			group := useSharedLimitFixture(t, backend)
			group.ConcurrencyLimit = 3
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			for i := 0; i < 3; i++ {
				lease, ok, _, err := AcquireChannelConcurrency(t.Context(), 1)
				require.NoError(t, err)
				require.True(t, ok)
				t.Cleanup(lease.Release)
			}
			var first *ChannelAdmission
			for i := 0; i < 256; i++ {
				ctx, admission := NewChannelAdmission(t.Context(), time.Now().Add(time.Minute))
				admission.Waiting = true
				t.Cleanup(admission.Close)
				if i == 0 {
					first = admission
				}
				_, ok, status, err := AcquireChannelConcurrency(ctx, 2)
				require.NoError(t, err)
				require.False(t, ok)
				require.Equal(t, "group_concurrency", status.Shared.Reason)
			}
			ctx, extra := NewChannelAdmission(t.Context(), time.Now().Add(time.Minute))
			extra.Waiting = true
			t.Cleanup(extra.Close)
			_, ok, status, err := AcquireChannelConcurrency(ctx, 2)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "queue_full", status.Shared.Reason)
			first.Close()
			_, ok, status, err = AcquireChannelConcurrency(ctx, 2)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "group_concurrency", status.Shared.Reason)
			assert.Equal(t, 256, status.Shared.Waiting)
			assert.Equal(t, 3, status.Shared.RPM)
		})
	}
}

func TestSharedLimitExpiredRPMKeepsLongStreamConcurrency(t *testing.T) {
	for _, backend := range []string{"local", "real"} {
		t.Run(backend, func(t *testing.T) {
			group := useSharedLimitFixture(t, backend)
			group.ConcurrencyLimit = 3
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, false))
			var first *ChannelConcurrencyLease
			for i := 0; i < 3; i++ {
				lease, ok, _, err := AcquireChannelConcurrency(t.Context(), 1)
				require.NoError(t, err)
				require.True(t, ok)
				t.Cleanup(lease.Release)
				if i == 0 {
					first = lease
				}
			}
			if common.RedisEnabled {
				now, err := common.RDB.Time(t.Context()).Result()
				require.NoError(t, err)
				for _, key := range []string{channelConcurrencyRedisRPMPrefix + "1"} {
					ids, err := common.RDB.ZRange(t.Context(), key, 0, -1).Result()
					require.NoError(t, err)
					for _, id := range ids {
						require.NoError(t, common.RDB.ZAdd(t.Context(), key, &redis.Z{Member: id, Score: float64(now.Add(-time.Minute - time.Second).UnixMilli())}).Err())
					}
				}
			} else {
				channelConcurrency.Lock()
				channelConcurrency.rpm[1] = []int64{time.Now().Add(-time.Minute - time.Second).UnixMilli()}
				channelConcurrency.Unlock()
			}
			_, ok, status, err := AcquireChannelConcurrency(t.Context(), 2)
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Equal(t, "group_concurrency", status.Shared.Reason)
			assert.Zero(t, status.Shared.RPM)
			assert.Equal(t, 3, status.Shared.Active)
			first.Release()
			lease, ok, status, err := AcquireChannelConcurrency(t.Context(), 2)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(lease.Release)
			assert.Equal(t, 1, status.Shared.RPM)
			assert.Equal(t, 3, status.Shared.Active)
		})
	}
}

func TestSharedLimitDeletionRemovesBindingWithoutStoppingRequests(t *testing.T) {
	for _, backend := range []string{"local", "real"} {
		t.Run(backend, func(t *testing.T) {
			group := useSharedLimitFixture(t, backend)
			held, ok, _, err := AcquireChannelConcurrency(t.Context(), 1)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(held.Release)
			require.NoError(t, SaveChannelLimitGroup(t.Context(), group, true))
			lease, ok, status, err := AcquireChannelConcurrency(t.Context(), 1)
			require.NoError(t, err)
			require.True(t, ok)
			t.Cleanup(lease.Release)
			assert.Nil(t, status.Shared)
			assert.Equal(t, 2, status.Active)
			held.Release()
			usage, err := GetChannelConcurrencySnapshotForChannelIDs(t.Context(), []int{1})
			require.NoError(t, err)
			assert.Equal(t, 1, usage[1].Active)
			assert.Equal(t, 2, usage[1].CurrentRPM)
			views, err := ListChannelLimitGroupViews(t.Context())
			require.NoError(t, err)
			assert.Empty(t, views)
		})
	}
}

func TestSharedLimitRegistryRebuildFailsWhenDatabaseIsUnavailable(t *testing.T) {
	useSharedLimitFixture(t, "real")
	require.NoError(t, common.RDB.Del(t.Context(), channelLimitRegistryKey).Err())
	connection, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	_, ok, _, err := AcquireChannelConcurrency(t.Context(), 1)
	require.Error(t, err)
	assert.False(t, ok)
	assert.False(t, common.RDB.HExists(t.Context(), channelLimitRegistryKey, "ready").Val())
}
