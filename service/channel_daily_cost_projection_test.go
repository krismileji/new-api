package service

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelDailyCostProjectionFailureHook struct {
	projection atomic.Bool
	ack        atomic.Bool
}

func (*channelDailyCostProjectionFailureHook) BeforeProcess(ctx context.Context, _ redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (*channelDailyCostProjectionFailureHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (hook *channelDailyCostProjectionFailureHook) BeforeProcessPipeline(ctx context.Context, commands []redis.Cmder) (context.Context, error) {
	for _, command := range commands {
		if command.Name() == "hset" && hook.projection.Load() || command.Name() == "xack" && hook.ack.CompareAndSwap(true, false) {
			return ctx, assert.AnError
		}
	}
	return ctx, nil
}

func (*channelDailyCostProjectionFailureHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func TestChannelDailyCostStreamProjection(t *testing.T) {
	previousType := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() { common.SetMainDatabaseType(previousType) })
	db, _, client := setupCM07ChannelDailyCostOutboxTest(t)
	verifyChannelDailyCostStreamProjection(t, db, client)
}

// Exercise the same handoff and recovery contracts against both the local
// fixture and each real database/Redis combination in the database matrix.
func verifyChannelDailyCostStreamProjection(t *testing.T, db *gorm.DB, client *redis.Client) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&model.ChannelTaskCostEvent{}, &model.ChannelMonitorDailyCostDetail{}))
	ctx := context.Background()
	runtime := newCM07ChannelDailyCostRuntime(client, "cost-projection-"+common.GetUUID())
	require.NoError(t, runtime.initRedisStream(ctx))
	now := time.Now().Unix()
	day := model.ChannelDailyCostDayStart(now)
	key := ChannelMonitorRedisCostDayKey(day)
	require.NoError(t, rebuildChannelMonitorReliableDailyCosts(ctx, client, day))
	hook := &channelDailyCostProjectionFailureHook{}
	client.AddHook(hook)
	var failConfirmation atomic.Bool
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:cost_projection_confirmation", func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]interface{})
		if ok && tx.Statement.Table == "channel_daily_cost_outboxes" && values["redis_projected_at"] != nil && failConfirmation.Load() {
			tx.AddError(assert.AnError)
		}
	}))

	t.Run("立即更新并去重", func(t *testing.T) {
		first := newCM07ChannelDailyCostDelta("immediate-first", 101, 125)
		first.OccurredAt = now
		second := first
		second.EventId, second.CostNanoCNY = "immediate-second", 75
		require.NoError(t, publishChannelDailyCostReliableEvent(ctx, first))
		require.NoError(t, publishChannelDailyCostReliableEvent(ctx, second))
		originalLogger := db.Logger
		recorder := &channelMonitorOutboxPollingSQLRecorder{Interface: originalLogger}
		db.Logger = recorder
		require.NoError(t, runtime.consumeRedisBatch(ctx))
		db.Logger = originalLogger
		assert.Zero(t, recorder.selects.Load(), "a warm projection must use committed records without rereading the database")
		assert.Equal(t, "200", client.HGet(ctx, key, "channel:101:"+channelMonitorRedisSharedMetricSettledCost).Val())
		var rows []model.ChannelDailyCostOutbox
		require.NoError(t, db.Where("channel_id = ?", 101).Find(&rows).Error)
		require.Len(t, rows, 2)
		for _, row := range rows {
			assert.NotZero(t, row.RedisProjectedAt)
			assert.Zero(t, row.ProcessedAt, "immediate Redis delivery must not advance the minute ledger")
		}
		require.NoError(t, publishChannelDailyCostReliableEvent(ctx, first))
		require.NoError(t, runtime.consumeRedisBatch(ctx))
		assert.Equal(t, "200", client.HGet(ctx, key, "channel:101:"+channelMonitorRedisSharedMetricSettledCost).Val())
		assert.Equal(t, "2", client.HGet(ctx, key, "channel:101:"+channelMonitorRedisSharedMetricSettledRequests).Val())
	})

	t.Run("确认失败后重放不重复累计", func(t *testing.T) {
		delta := newCM07ChannelDailyCostDelta("ack-replay", 102, 225)
		delta.OccurredAt = now
		require.NoError(t, publishChannelDailyCostReliableEvent(ctx, delta))
		hook.ack.Store(true)
		require.Error(t, runtime.consumeRedisBatch(ctx))
		assert.Equal(t, "225", client.HGet(ctx, key, "channel:102:"+channelMonitorRedisSharedMetricSettledCost).Val())
		pending, err := client.XPending(ctx, ChannelDailyCostRedisStream, ChannelDailyCostRedisConsumerGroup).Result()
		require.NoError(t, err)
		assert.EqualValues(t, 1, pending.Count)
		require.NoError(t, runtime.consumeRedisBatch(ctx))
		assert.Equal(t, "225", client.HGet(ctx, key, "channel:102:"+channelMonitorRedisSharedMetricSettledCost).Val())
		pending, err = client.XPending(ctx, ChannelDailyCostRedisStream, ChannelDailyCostRedisConsumerGroup).Result()
		require.NoError(t, err)
		assert.Zero(t, pending.Count)
	})

	t.Run("统计失败仍可靠确认并由数据库补偿", func(t *testing.T) {
		delta := newCM07ChannelDailyCostDelta("projection-recovery", 103, 325)
		delta.OccurredAt = now
		require.NoError(t, publishChannelDailyCostReliableEvent(ctx, delta))
		hook.projection.Store(true)
		require.NoError(t, runtime.consumeRedisBatch(ctx))
		hook.projection.Store(false)
		assert.Empty(t, client.HGet(ctx, key, "channel:103:"+channelMonitorRedisSharedMetricSettledCost).Val())
		assert.True(t, runtime.projectionRetry.Load())
		var row model.ChannelDailyCostOutbox
		require.NoError(t, db.First(&row, "event_id = ?", delta.EventId).Error)
		assert.Zero(t, row.RedisProjectedAt)
		assert.Zero(t, client.XLen(ctx, ChannelDailyCostRedisStream).Val(), "database commit permits acknowledgment even when projection fails")
		pending, err := runtime.recoverRedisCostProjections(ctx)
		require.NoError(t, err)
		assert.False(t, pending)
		assert.Equal(t, "325", client.HGet(ctx, key, "channel:103:"+channelMonitorRedisSharedMetricSettledCost).Val())
		require.NoError(t, db.First(&row, row.Id).Error)
		assert.NotZero(t, row.RedisProjectedAt)
	})

	t.Run("统计成功但确认标记失败不重复累计", func(t *testing.T) {
		delta := newCM07ChannelDailyCostDelta("confirmation-recovery", 104, 425)
		delta.OccurredAt = now
		require.NoError(t, publishChannelDailyCostReliableEvent(ctx, delta))
		failConfirmation.Store(true)
		require.NoError(t, runtime.consumeRedisBatch(ctx))
		failConfirmation.Store(false)
		assert.Equal(t, "425", client.HGet(ctx, key, "channel:104:"+channelMonitorRedisSharedMetricSettledCost).Val())
		var row model.ChannelDailyCostOutbox
		require.NoError(t, db.First(&row, "event_id = ?", delta.EventId).Error)
		assert.Zero(t, row.RedisProjectedAt)
		_, err := runtime.recoverRedisCostProjections(ctx)
		require.NoError(t, err)
		assert.Equal(t, "425", client.HGet(ctx, key, "channel:104:"+channelMonitorRedisSharedMetricSettledCost).Val())
		require.NoError(t, db.First(&row, row.Id).Error)
		assert.NotZero(t, row.RedisProjectedAt)
	})

	t.Run("冲突批次只统计最终提交的有效记录", func(t *testing.T) {
		existing := newCM07ChannelDailyCostDelta("projection-collision", 105, 100)
		existing.OccurredAt = now
		require.NoError(t, publishChannelDailyCostReliableEvent(ctx, existing))
		require.NoError(t, runtime.consumeRedisBatch(ctx))
		valid := existing
		valid.EventId, valid.CostNanoCNY = "projection-valid", 50
		collision := existing
		collision.CostNanoCNY = 999
		require.NoError(t, publishChannelDailyCostReliableEvent(ctx, valid))
		require.NoError(t, publishChannelDailyCostReliableEvent(ctx, collision))
		require.NoError(t, runtime.consumeRedisBatch(ctx))
		assert.Equal(t, "150", client.HGet(ctx, key, "channel:105:"+channelMonitorRedisSharedMetricSettledCost).Val())
		dead, err := client.XRange(ctx, ChannelDailyCostRedisDeadLetter, "-", "+").Result()
		require.NoError(t, err)
		require.Len(t, dead, 1)
		assert.Equal(t, collision.EventId, dead[0].Values["event_id"])
	})

	t.Run("数据库兜底和任务修正可恢复重建", func(t *testing.T) {
		delta := newCM07ChannelDailyCostDelta("projection-db-fallback", 106, 100)
		delta.OccurredAt = now
		require.NoError(t, model.StoreChannelDailyCostOutboxEvents(ctx, []model.ChannelDailyCostDelta{delta}))
		_, err := model.RegisterChannelTaskCostEvent(ctx, model.ChannelTaskCostEventInput{
			CostEventId: "task:stream-projection", ChannelId: 106, OccurredAt: now, InitialQuota: 6, CostNanoCNY: 600,
		})
		require.NoError(t, err)
		_, err = model.SetChannelTaskCostEventCost(ctx, "task:stream-projection", 200, now+1)
		require.NoError(t, err)
		_, err = runtime.recoverRedisCostProjections(ctx)
		require.NoError(t, err)
		assert.Equal(t, "300", client.HGet(ctx, key, "channel:106:"+channelMonitorRedisSharedMetricSettledCost).Val())
		require.NoError(t, recoverChannelDailyCostOutboxBatch(ctx, "projection-ledger"))
		require.NoError(t, client.Del(ctx, key, key+":events").Err())
		require.NoError(t, rebuildChannelMonitorReliableDailyCosts(ctx, client, day))
		assert.Equal(t, "300", client.HGet(ctx, key, "channel:106:"+channelMonitorRedisSharedMetricSettledCost).Val())
		assert.Equal(t, "2", client.HGet(ctx, key, "channel:106:"+channelMonitorRedisSharedMetricSettledRequests).Val())
	})

	t.Run("旧消息不重建过期统计", func(t *testing.T) {
		delta := newCM07ChannelDailyCostDelta("projection-old-stream", 107, 100)
		delta.OccurredAt = day - 2*86400
		require.NoError(t, publishChannelDailyCostReliableEvent(ctx, delta))
		require.NoError(t, runtime.consumeRedisBatch(ctx))
		assert.Zero(t, client.Exists(ctx, ChannelMonitorRedisCostDayKey(delta.OccurredAt)).Val())
		var row model.ChannelDailyCostOutbox
		require.NoError(t, db.First(&row, "event_id = ?", delta.EventId).Error)
		assert.Equal(t, delta.CostNanoCNY, row.CostNanoCNY, "historical costs must remain available for ledger recovery")
	})

	t.Run("统计丢失时新事件与重建不重复累计", func(t *testing.T) {
		require.NoError(t, client.Del(ctx, key, key+":events").Err())
		delta := newCM07ChannelDailyCostDelta("projection-cold-start", 109, 225)
		delta.OccurredAt = now
		require.NoError(t, publishChannelDailyCostReliableEvent(ctx, delta))
		require.NoError(t, runtime.consumeRedisBatch(ctx))
		assert.Equal(t, "225", client.HGet(ctx, key, "channel:109:"+channelMonitorRedisSharedMetricSettledCost).Val())
		assert.Equal(t, "1", client.HGet(ctx, key, "channel:109:"+channelMonitorRedisSharedMetricSettledRequests).Val())
		assert.Equal(t, "300", client.HGet(ctx, key, "channel:106:"+channelMonitorRedisSharedMetricSettledCost).Val())
		var row model.ChannelDailyCostOutbox
		require.NoError(t, db.First(&row, "event_id = ?", delta.EventId).Error)
		assert.NotZero(t, row.RedisProjectedAt)
		assert.Zero(t, row.ProcessedAt)
	})
}

func TestChannelDailyCostProjectionPollingKeepsHeartbeatWithoutIdleDatabaseQueries(t *testing.T) {
	previousType := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() { common.SetMainDatabaseType(previousType) })
	db, _, client := setupCM07ChannelDailyCostOutboxTest(t)
	// Network server and connection pool goroutines live outside the virtual
	// clock. Only the recovery worker's timers are advanced by this test.
	require.NoError(t, client.Ping(context.Background()).Err())
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		day := model.ChannelDailyCostDayStart(time.Now().Unix())
		key := ChannelMonitorRedisCostDayKey(day)
		require.NoError(t, rebuildChannelMonitorReliableDailyCosts(ctx, client, day))
		recorder := &channelMonitorOutboxPollingSQLRecorder{Interface: db.Logger}
		db.Logger = recorder
		runtime := newCM07ChannelDailyCostRuntime(client, "projection-polling")
		done := make(chan struct{})
		go func() {
			defer close(done)
			runtime.runRedisProjection(ctx)
		}()
		defer func() { cancel(); <-done }()
		synctest.Wait()
		assert.EqualValues(t, 1, recorder.selects.Load())
		time.Sleep(4 * time.Second)
		synctest.Wait()
		assert.EqualValues(t, 1, recorder.selects.Load(), "healthy idle heartbeats must not poll the database every second")
		var status ChannelMonitorReliableCostStatus
		require.NoError(t, common.UnmarshalJsonStr(client.Get(ctx, channelMonitorReliableCostStatusKey).Val(), &status))
		assert.Equal(t, time.Now().Unix(), status.CheckedAt)
		assert.False(t, status.Failed)

		delta := newCM07ChannelDailyCostDelta("projection-poll-fallback", 108, 125)
		delta.OccurredAt = time.Now().Unix()
		require.NoError(t, model.StoreChannelDailyCostOutboxEvents(ctx, []model.ChannelDailyCostDelta{delta}))
		time.Sleep(time.Second)
		synctest.Wait()
		assert.EqualValues(t, 2, recorder.selects.Load())
		assert.Equal(t, "125", client.HGet(ctx, key, "channel:108:"+channelMonitorRedisSharedMetricSettledCost).Val())

		// An immediate projection failure requests recovery on the next
		// heartbeat rather than waiting for the next five-second scan.
		require.NoError(t, runtime.initRedisStream(ctx))
		hook := &channelDailyCostProjectionFailureHook{}
		client.AddHook(hook)
		hook.projection.Store(true)
		delta.EventId, delta.CostNanoCNY = "projection-poll-retry", 75
		require.NoError(t, publishChannelDailyCostReliableEvent(ctx, delta))
		require.NoError(t, runtime.consumeRedisBatch(ctx))
		hook.projection.Store(false)
		time.Sleep(time.Second)
		synctest.Wait()
		assert.EqualValues(t, 3, recorder.selects.Load())
		assert.Equal(t, "200", client.HGet(ctx, key, "channel:108:"+channelMonitorRedisSharedMetricSettledCost).Val())
		require.NoError(t, common.UnmarshalJsonStr(client.Get(ctx, channelMonitorReliableCostStatusKey).Val(), &status))
		assert.False(t, status.Failed)
		assert.False(t, status.Pending)
		cancel()
		<-done
		queriesAtStop := recorder.selects.Load()
		time.Sleep(10 * time.Second)
		synctest.Wait()
		assert.Equal(t, queriesAtStop, recorder.selects.Load())
	})
}
