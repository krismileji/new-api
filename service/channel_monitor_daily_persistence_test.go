package service

import (
	"context"
	"errors"
	"fmt"
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

func TestDailyPersistenceUpdatesOneRowAndAcceptsLateEventsAfterRestore(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorDailySuccessLedger{}, &model.ChannelMonitorDailyCheckpoint{}))
	server, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	ctx := context.Background()
	const at = int64(1_750_000_000)
	day := model.ChannelDailyCostDayStart(at)
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	first := newChannelMonitorRedisSharedProjectionTestEvent("daily-first", at)
	first.EventSequence = 1001
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{first}))
	require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
	var stored []model.ChannelMonitorDailySuccessLedger
	require.NoError(t, db.Where("day_start = ?", day).Find(&stored).Error)
	require.Len(t, stored, 1)
	id := stored[0].Id
	assert.Equal(t, int64(1), stored[0].ActualSuccessCount)
	second := first
	second.EventId, second.EventSequence, second.OccurredAt = "daily-second", 1002, at+60
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{second}))
	require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
	require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
	stored = nil
	require.NoError(t, db.Where("day_start = ?", day).Find(&stored).Error)
	require.Len(t, stored, 1)
	assert.Equal(t, id, stored[0].Id)
	assert.Equal(t, int64(2), stored[0].ActualSuccessCount)
	assert.NotEmpty(t, stored[0].AggregateJSON)
	server.FlushAll()
	require.NoError(t, rebuildChannelMonitorDailyMetrics(ctx, client, day))
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{first}))
	late := first
	late.EventId, late.EventSequence, late.OccurredAt = "daily-late", 1003, at-15
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{late}))
	require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
	stored = nil
	require.NoError(t, db.Where("day_start = ?", day).Find(&stored).Error)
	require.Len(t, stored, 1)
	assert.Equal(t, id, stored[0].Id)
	assert.Equal(t, int64(3), stored[0].ActualSuccessCount)
	var checkpoints int64
	require.NoError(t, db.Model(&model.ChannelMonitorDailyCheckpoint{}).Count(&checkpoints).Error)
	assert.Equal(t, int64(1), checkpoints)
	var checkpoint model.ChannelMonitorDailyCheckpoint
	require.NoError(t, db.First(&checkpoint, "day_start = ?", day).Error)
	assert.True(t, checkpoint.CoveragePartial, "losing the stream tail must remain visible after the next daily flush")
}

func TestDailyPersistenceWaitsForHolesAndReplaysRetainedTailWithoutDoubleCounting(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorDailySuccessLedger{}, &model.ChannelMonitorDailyCheckpoint{}))
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	ctx := context.Background()
	const at = int64(1_750_000_000)
	day := model.ChannelDailyCostDayStart(at)
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	events := []model.ChannelMonitorEvent{
		newChannelMonitorRedisSharedProjectionTestEvent("daily-hole-first", at),
		newChannelMonitorRedisSharedProjectionTestEvent("daily-hole-second", at+1),
		newChannelMonitorRedisSharedProjectionTestEvent("daily-retained-tail", at-60),
	}
	for index := range events {
		id := fmt.Sprintf("%d-%d", at*1000, index)
		sequence, err := channelMonitorRedisEventSequenceFromStreamID(id)
		require.NoError(t, err)
		events[index].EventSequence = sequence
		events[index].InputTokens = common.GetPointer(int64(10000))
		events[index].IsStream = true
		events[index].CacheReadTokens = common.GetPointer(int64(0))
		if index != 1 {
			events[index].CacheReadTokens = common.GetPointer(int64(5000))
		}
		events[index].GroupCacheExcluded = common.GetPointer(index == 2)

		payload, err := common.Marshal(events[index])
		require.NoError(t, err)
		require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: ChannelMonitorRedisEventStream, ID: id, Values: map[string]any{
			ChannelMonitorRedisEventFieldEventID: events[index].EventId,
			ChannelMonitorRedisEventFieldPayload: string(payload),
		}}).Err())
	}
	require.NoError(t, client.XGroupCreate(ctx, ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup, "0").Err())
	_, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: ChannelMonitorRedisConsumerGroup, Consumer: "daily-test", Streams: []string{ChannelMonitorRedisEventStream, ">"}, Count: 2, Block: -1}).Result()
	require.NoError(t, err)
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, events[1:2]))
	require.ErrorContains(t, persistChannelMonitorDailyMetrics(ctx, client, day), "未聚合事件")
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, events[:1]))
	require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, events[2:]))
	require.NoError(t, client.Del(ctx, ChannelMonitorRedisSuccessDayKey(day)).Err())
	require.NoError(t, rebuildChannelMonitorDailyMetrics(ctx, client, day))
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, events))
	require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
	var rows []model.ChannelMonitorDailySuccessLedger
	require.NoError(t, db.Where("day_start = ?", day).Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(3), rows[0].ActualSuccessCount)
	view, err := queryChannelMonitorRedisDailySuccessWithClient(ctx, client, day, nil)
	require.NoError(t, err)
	assert.False(t, view.CoveragePartial)
	counts, err := model.GetChannelGroupMonitorHistoricalCacheCounts(ctx, []string{"vip"}, day, day+24*60*60)
	require.NoError(t, err)
	assert.Equal(t, []model.ChannelGroupMonitorCacheCounts{{GroupName: "vip", APIKeyId: 42, CacheReadTokens: 5000, InputTokens: 20000}}, counts,
		"tail replay preserves the original excluded sample and does not count it twice")
}

func TestReliableDailyCostModelDetectionResolvesOnlyOnce(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelDailyCostOutbox{}, &model.ChannelMonitorDailyCostDetail{}))
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	ctx := context.Background()
	const at = int64(1_750_000_000)
	key := ChannelMonitorRedisCostDayKey(model.ChannelDailyCostDayStart(at))
	require.NoError(t, model.AddChannelDailyCostWithModelDetectionAndModel(ctx, nil, 7, at, 0, 0, 0, 1, "model-a"))
	require.NoError(t, rebuildChannelMonitorRedisDailyCosts(ctx, client, at))
	require.NoError(t, model.SettleUnresolvedChannelDailyModelDetectionCostWithModel(ctx, nil, 7, at, 120, "model-a"))
	var rows []model.ChannelDailyCostOutbox
	require.NoError(t, db.Order("id ASC").Find(&rows).Error)
	require.NoError(t, projectChannelDailyCostOutboxRows(ctx, client, rows))
	require.NoError(t, projectChannelDailyCostOutboxRows(ctx, client, rows))
	assert.Equal(t, "120", client.HGet(ctx, key, "global:"+channelMonitorRedisSharedMetricSettledCost).Val())
	assert.Equal(t, "120", client.HGet(ctx, key, "global:"+channelMonitorRedisSharedMetricDetectionSettledCost).Val())
	assert.Equal(t, "1", client.HGet(ctx, key, "global:"+channelMonitorRedisSharedMetricSettledRequests).Val())
	assert.Equal(t, "0", client.HGet(ctx, key, "global:"+channelMonitorRedisSharedMetricUnresolvedRequests).Val())
}

func TestChannelMonitorDailyReadRecovery(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	verifyChannelMonitorDailyReadRecovery(t, db, client)
}

func verifyChannelMonitorDailyReadRecovery(t *testing.T, db *gorm.DB, client *redis.Client) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorDailySuccessLedger{}, &model.ChannelMonitorDailyCheckpoint{},
		&model.ChannelDailyCostOutbox{}, &model.ChannelMonitorDailyCostDetail{}, &model.ChannelMonitorAggregationState{}))
	previousEnabled := common.RedisEnabled
	previousClients := []*redis.Client{common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer}
	common.RedisEnabled = true
	common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer = client, client, client, client
	t.Cleanup(func() {
		common.RedisEnabled = previousEnabled
		common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer = previousClients[0], previousClients[1], previousClients[2], previousClients[3]
	})
	ctx := context.Background()
	at := common.GetTimestamp()
	day := model.ChannelDailyCostDayStart(at)
	const channel = 908811
	for _, kind := range []string{"cost", "success"} {
		t.Run(kind, func(t *testing.T) {
			scope := channelMonitorDailyRecoveryScope{db: db, client: client, kind: kind, day: day}
			key := scope.key()
			require.NoError(t, client.Del(ctx, key, key+":events", key+":recovery:baseline").Err())
			// Hold the existing rebuild lease to make the temporary state
			// deterministic while checking repeated reads and another node.
			require.NoError(t, client.Set(ctx, key+":rebuild:lease", "test-recovery", time.Minute).Err())
			t.Cleanup(func() {
				channelMonitorDailyReadRecovery.mu.Lock()
				session := channelMonitorDailyReadRecovery.sessions[scope]
				var done chan struct{}
				if session != nil && session.cancel != nil {
					session.cancel()
					done = session.rebuildDone
				}
				channelMonitorDailyReadRecovery.mu.Unlock()
				if done != nil {
					<-done
				}
				channelMonitorDailyReadRecovery.finish(scope)
				assert.NoError(t, client.Del(ctx, key, key+":events", key+":rebuild:lease", key+":recovery:baseline").Err())
			})
			metric := channelMonitorRedisSharedMetricSettledCost
			if kind == "cost" {
				require.NoError(t, model.AddChannelDailyCostBatch(ctx, []model.ChannelDailyCostDelta{{
					ChannelId: channel, OccurredAt: at, CostNanoCNY: 300, SettledDelta: 1,
					UserId: 908812, APIKeyId: 908813, ModelName: "recovery-model", SourceKind: "business",
				}}))
			} else {
				metric = channelMonitorRedisSharedMetricActualSuccess
				identity := model.ChannelMonitorDailyMetricIdentity{ChannelID: channel, UserID: 908812, APIKeyID: 908813, Model: "recovery-model"}
				row := identity.LedgerRow(day)
				row.ActualSuccessCount, row.FinalSuccessCount = 3, 3
				require.NoError(t, db.Create(&row).Error)
				require.NoError(t, db.Create(&model.ChannelMonitorDailyCheckpoint{DayStart: day, Revision: 1, DataCutoffAt: at}).Error)
			}
			var queries atomic.Int64
			callback := "daily_recovery_read_count_" + kind
			require.NoError(t, db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) { queries.Add(1) }))
			t.Cleanup(func() { assert.NoError(t, db.Callback().Query().Remove(callback)) })
			fields, err := readChannelMonitorDailyHashWithRecovery(ctx, client, kind, day, []string{"*"}, 10000)
			require.NoError(t, err)
			assert.Equal(t, "database_daily", channelMonitorDailyReadSource(fields))
			assert.Equal(t, "1", fields["meta:coverage_partial"])
			assert.NotEmpty(t, fields["meta:processed_at"])
			want := "300"
			if kind == "success" {
				want = "3"
			}
			fixtureField := fmt.Sprintf("channel:%d:%s", channel, metric)
			assert.Equal(t, want, fields[fixtureField])
			channelMonitorDailyReadRecovery.mu.Lock()
			session := channelMonitorDailyReadRecovery.sessions[scope]
			done := session.rebuildDone
			channelMonitorDailyReadRecovery.mu.Unlock()
			<-done
			loadedQueries := queries.Load()
			assert.Positive(t, loadedQueries)
			selected, err := readChannelMonitorDailyHashWithRecovery(ctx, client, kind, day, []string{"meta:*", "channel:*"}, 10000)
			require.NoError(t, err)
			assert.Equal(t, want, selected[fmt.Sprintf("channel:%d:%s", channel, metric)])
			assert.NotContains(t, selected, "global:"+metric)
			// A second process has no local session. Its initialization must
			// use the shared database baseline rather than querying again.
			anotherNode := &channelMonitorDailyRecoveryManager{sessions: make(map[channelMonitorDailyRecoveryScope]*channelMonitorDailyRecoverySession)}
			shared, err := anotherNode.baseline(ctx, scope, 10000)
			require.NoError(t, err)
			assert.Equal(t, fields, shared)
			if kind == "cost" {
				view, err := QueryChannelMonitorRedisDailyCosts(ctx, day)
				require.NoError(t, err)
				assert.Equal(t, "database_daily", view.Source)
				assert.Equal(t, int64(300), view.Channels[channel].SettledCostNanoCNY)
				assert.True(t, view.Projection.Failed)
				view, err = QueryChannelMonitorRedisDailyCostTotals(ctx, day)
				require.NoError(t, err)
				assert.Equal(t, int64(300), view.Channels[channel].SettledCostNanoCNY)
			} else {
				view, err := QueryChannelMonitorRedisDailySuccessAnalytics(ctx, day)
				require.NoError(t, err)
				assert.Equal(t, "database_daily", view.Source)
				assert.True(t, view.CoveragePartial)
				assert.Equal(t, int64(3), view.Facts[0].Aggregate.ActualSuccessCount)
				page, err := QueryChannelMonitorRealtimeTodaySuccessFromRedis(ctx, day, at+1)
				require.NoError(t, err)
				assert.Equal(t, "database_daily", page.Source)
				assert.Equal(t, int64(3), page.Summary.Summary.ActualSuccessCount)
			}
			assert.Equal(t, loadedQueries, queries.Load(), "refreshes and other readers reuse the recovery baseline")
			if kind == "success" {
				require.NoError(t, client.HSet(ctx, key, map[string]any{
					"meta:revision": "99", "fact:invalid:actual_success_count": "-1",
				}).Err())
				assert.ErrorIs(t, persistChannelMonitorDailyMetrics(ctx, client, day), ErrChannelMonitorRedisSharedProjectionUnavailable,
					"a temporary database baseline must never overwrite the durable ledger as a new Redis revision")
				var checkpoint model.ChannelMonitorDailyCheckpoint
				require.NoError(t, db.First(&checkpoint, "day_start = ?", day).Error)
				assert.Equal(t, int64(1), checkpoint.Revision)
				require.NoError(t, client.Del(ctx, key).Err())
			}
			require.NoError(t, client.Del(ctx, key+":rebuild:lease").Err())
			if kind == "cost" {
				require.NoError(t, model.StoreChannelDailyCostOutboxEvents(ctx, []model.ChannelDailyCostDelta{{
					EventId: "daily-read-recovery-tail", ChannelId: channel, OccurredAt: at, CostNanoCNY: 50, SettledDelta: 1,
					UserId: 908812, APIKeyId: 908813, ModelName: "recovery-model", SourceKind: "business",
				}}))
				require.NoError(t, rebuildChannelMonitorReliableDailyCosts(ctx, client, day))
				want = "350"
			} else {
				event := newChannelMonitorRedisSharedProjectionTestEvent("daily-read-recovery-tail", at)
				event.ChannelId, event.UserId, event.APIKeyId = channel, 908812, 908813
				payload, err := common.Marshal(event)
				require.NoError(t, err)
				require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: ChannelMonitorRedisEventStream,
					Values: map[string]any{ChannelMonitorRedisEventFieldEventID: event.EventId, ChannelMonitorRedisEventFieldPayload: string(payload)}}).Err())
				require.NoError(t, rebuildChannelMonitorDailyMetrics(ctx, client, day))
				want = "4"
			}
			live, err := readChannelMonitorDailyHashWithRecovery(ctx, client, kind, day, []string{"*"}, 10000)
			require.NoError(t, err)
			assert.Equal(t, "redis_daily", channelMonitorDailyReadSource(live))
			assert.Equal(t, want, live[fixtureField], "database baseline and new events are counted once")
			assert.Zero(t, client.Exists(ctx, key+":recovery:baseline").Val())
			liveQueries := queries.Load()
			_, err = readChannelMonitorDailyHashWithRecovery(ctx, client, kind, day, []string{"meta:*", "global:*"}, 10000)
			require.NoError(t, err)
			assert.Equal(t, liveQueries, queries.Load(), "live refreshes do not query the database")

			for _, publishLive := range []bool{true, false} {
				t.Run(fmt.Sprintf("waiting_for_live_%t", publishLive), func(t *testing.T) {
					require.NoError(t, client.Del(ctx, key, key+":recovery:baseline").Err())
					require.NoError(t, client.Set(ctx, key+":recovery:lease", "other-node", time.Minute).Err())
					options := *client.Options()
					waitingClient := redis.NewClient(&options)
					t.Cleanup(func() { assert.NoError(t, waitingClient.Close()) })
					waiting := make(chan struct{}, 1)
					waitingClient.AddHook(channelMonitorRecoveryLeaseWaitHook{key: key + ":recovery:lease", waiting: waiting})
					waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					result := make(chan struct{})
					var recovered map[string]string
					var recoveryErr error
					before := queries.Load()
					go func() {
						recovered, recoveryErr = loadChannelMonitorDailyRecoveryBaseline(waitCtx,
							channelMonitorDailyRecoveryScope{db: db, client: waitingClient, kind: kind, day: day}, 10000)
						close(result)
					}()
					t.Cleanup(func() {
						cancel()
						<-result
						assert.NoError(t, client.Del(ctx, key+":recovery:lease").Err())
					})
					select {
					case <-waiting:
					case <-waitCtx.Done():
						t.Fatal("another node did not wait for the initialization lease")
					}
					if publishLive {
						published := map[string]string{"meta:revision": "99", "global:" + metric: want}
						if kind == "cost" {
							published[channelMonitorReliableCostVersionField] = "1"
						}
						require.NoError(t, client.HSet(ctx, key, published).Err())
					} else {
						require.NoError(t, client.Del(ctx, key+":recovery:lease").Err())
					}
					<-result
					require.NoError(t, recoveryErr)
					if publishLive {
						assert.Equal(t, "redis_daily", channelMonitorDailyReadSource(recovered))
						assert.Equal(t, want, recovered["global:"+metric])
						assert.Equal(t, before, queries.Load(), "a completed rebuild needs no database fallback")
					} else {
						assert.Equal(t, "database_daily", channelMonitorDailyReadSource(recovered))
						assert.Greater(t, queries.Load(), before, "a failed holder releases initialization to the waiting node")
					}
				})
			}

			if kind == "success" {
				t.Run("legacy_checkpoint", func(t *testing.T) {
					require.NoError(t, db.Where("day_start = ?", day).Delete(&model.ChannelMonitorDailyCheckpoint{}).Error)
					require.NoError(t, model.AdvanceChannelMonitorAggregationCompletedThrough(ctx, at/60*60))
					legacy := newChannelMonitorRedisSharedProjectionTestEvent("daily-read-recovery-legacy", at-120)
					legacy.ChannelId, legacy.UserId, legacy.APIKeyId = channel, 908812, 908813
					payload, err := common.Marshal(legacy)
					require.NoError(t, err)
					require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: ChannelMonitorRedisEventStream,
						Values: map[string]any{ChannelMonitorRedisEventFieldEventID: legacy.EventId, ChannelMonitorRedisEventFieldPayload: string(payload)}}).Err())
					sqlDB, err := db.DB()
					require.NoError(t, err)
					connections := sqlDB.Stats().MaxOpenConnections
					sqlDB.SetMaxOpenConns(1)
					t.Cleanup(func() { sqlDB.SetMaxOpenConns(connections) })
					rebuildCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					require.NoError(t, rebuildChannelMonitorDailyMetrics(rebuildCtx, client, day))
					assert.Equal(t, "4", client.HGet(ctx, key, fixtureField).Val(), "legacy coverage excludes already persisted events while replaying the newer tail")
				})
			}

			t.Run("database_failure", func(t *testing.T) {
				require.NoError(t, client.Del(ctx, key, key+":recovery:baseline").Err())
				channelMonitorDailyReadRecovery.finish(scope)
				failure := errors.New("database recovery unavailable")
				failureCallback := "daily_recovery_database_failure_" + kind
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register(failureCallback, func(tx *gorm.DB) {
					tx.AddError(failure)
				}))
				t.Cleanup(func() { assert.NoError(t, db.Callback().Query().Remove(failureCallback)) })
				missing, err := readChannelMonitorDailyHashWithRecovery(ctx, client, kind, day, nil, 10000)
				assert.ErrorIs(t, err, failure)
				assert.Nil(t, missing, "failed recovery must not display an empty complete statistic")
				failedQueries := queries.Load()
				_, err = readChannelMonitorDailyHashWithRecovery(ctx, client, kind, day, nil, 10000)
				assert.ErrorIs(t, err, failure)
				assert.Equal(t, failedQueries, queries.Load(), "failed fallback retries are bounded during refreshes")
			})

			t.Run("redis_failure", func(t *testing.T) {
				failedClient := redis.NewClient(client.Options())
				require.NoError(t, failedClient.Close())
				previousConsumer := common.RDBMonitorConsumer
				common.RDBMonitorConsumer = failedClient
				failedScope := channelMonitorDailyRecoveryScope{db: db, client: failedClient, kind: kind, day: day}
				t.Cleanup(func() {
					channelMonitorDailyReadRecovery.mu.Lock()
					session := channelMonitorDailyReadRecovery.sessions[failedScope]
					var done chan struct{}
					if session != nil && session.cancel != nil {
						session.cancel()
						done = session.rebuildDone
					}
					channelMonitorDailyReadRecovery.mu.Unlock()
					if done != nil {
						<-done
					}
					channelMonitorDailyReadRecovery.finish(failedScope)
					common.RDBMonitorConsumer = previousConsumer
				})
				fallback, err := readChannelMonitorDailyHashWithRecovery(ctx, failedClient, kind, day, nil, 10000)
				require.NoError(t, err)
				assert.Equal(t, "database_daily", channelMonitorDailyReadSource(fallback))
				assert.Equal(t, "1", fallback["meta:coverage_partial"])
				channelMonitorDailyReadRecovery.mu.Lock()
				done := channelMonitorDailyReadRecovery.sessions[failedScope].rebuildDone
				channelMonitorDailyReadRecovery.mu.Unlock()
				<-done
				fallbackQueries := queries.Load()
				_, err = readChannelMonitorDailyHashWithRecovery(ctx, failedClient, kind, day, nil, 10000)
				require.NoError(t, err)
				assert.Equal(t, fallbackQueries, queries.Load(), "a Redis outage reuses the process recovery baseline")
				_, err = readChannelMonitorDailyHashWithRecovery(ctx, failedClient, kind, day, nil, 1)
				assert.ErrorIs(t, err, ErrChannelMonitorRedisSharedProjectionLimitExceeded)
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				_, err = readChannelMonitorDailyHashWithRecovery(cancelled, failedClient, kind, day, nil, 10000)
				assert.Error(t, err)
				assert.Equal(t, fallbackQueries, queries.Load(), "read limits and cancellation must not trigger another database load")
			})

			t.Run("redis_disabled", func(t *testing.T) {
				common.RedisEnabled = false
				t.Cleanup(func() { common.RedisEnabled = true })
				first, err := readChannelMonitorDailyHashWithRecovery(ctx, nil, kind, day, nil, 10000)
				require.NoError(t, err)
				assert.Equal(t, "database_daily", channelMonitorDailyReadSource(first))
				before := queries.Load()
				increment := int64(1)
				if kind == "cost" {
					increment = 25
					require.NoError(t, model.AddChannelDailyCostBatch(ctx, []model.ChannelDailyCostDelta{{
						ChannelId: channel, OccurredAt: at, CostNanoCNY: 25, SettledDelta: 1,
						UserId: 908812, APIKeyId: 908813, ModelName: "recovery-model", SourceKind: "business",
					}}))
				} else {
					require.NoError(t, db.Model(&model.ChannelMonitorDailySuccessLedger{}).
						Where("day_start = ? AND channel_id = ?", day, channel).
						Update("actual_success_count", gorm.Expr("actual_success_count + 1")).Error)
				}
				latest, err := readChannelMonitorDailyHashWithRecovery(ctx, nil, kind, day, nil, 10000)
				require.NoError(t, err)
				assert.Equal(t, parseDailySuccessInt64(first[fixtureField])+increment,
					parseDailySuccessInt64(latest[fixtureField]), "disabled Redis must not retain a frozen initialization result")
				assert.Greater(t, queries.Load(), before)
			})
		})
	}
}

type channelMonitorRecoveryLeaseWaitHook struct {
	key     string
	waiting chan struct{}
}

func (hook channelMonitorRecoveryLeaseWaitHook) BeforeProcess(ctx context.Context, _ redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (hook channelMonitorRecoveryLeaseWaitHook) AfterProcess(_ context.Context, cmd redis.Cmder) error {
	if result, ok := cmd.(*redis.BoolCmd); ok && len(cmd.Args()) > 1 && cmd.Args()[1] == hook.key {
		acquired, err := result.Result()
		if err == nil && !acquired {
			select {
			case hook.waiting <- struct{}{}:
			default:
			}
		}
	}
	return nil
}

func (hook channelMonitorRecoveryLeaseWaitHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (hook channelMonitorRecoveryLeaseWaitHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}
