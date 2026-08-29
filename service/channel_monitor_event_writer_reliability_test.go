package service

import (
	"context"
	"errors"
	"fmt"
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

type channelMonitorEventWriterAlwaysFailAppender struct{}

func (channelMonitorEventWriterAlwaysFailAppender) XAdd(ctx context.Context, _ *redis.XAddArgs) *redis.StringCmd {
	cmd := redis.NewStringCmd(ctx, "XADD")
	cmd.SetErr(errors.New("writer reliability test failure"))
	return cmd
}

func setupChannelMonitorEventWriterReliabilityDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-event-writer.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorEventOutbox{}))
	previous := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestChannelMonitorEventWriterPersistsQueueOverflow(t *testing.T) {
	db := setupChannelMonitorEventWriterReliabilityDB(t)
	writer := newChannelMonitorEventWriter(nil, channelMonitorEventWriterConfig{
		QueueCapacity:       1,
		DirectPublishOnFull: false,
		PersistOverflow:     false, // durability must not be disabled by a legacy config value
	})
	first := newChannelMonitorPublisherTestEvent("writer-overflow-first")
	firstPayload, err := first.Marshal()
	require.NoError(t, err)
	writer.queue <- channelMonitorEventWriterItem{event: first, payload: firstPayload}

	second := newChannelMonitorPublisherTestEvent("writer-overflow-second")
	secondPayload, err := second.Marshal()
	require.NoError(t, err)
	status, enqueueErr := writer.enqueue(channelMonitorEventWriterItem{event: second, payload: secondPayload})
	require.NoError(t, enqueueErr)
	assert.Equal(t, ChannelMonitorEventPublishStatusPersisted, status)
	assert.Zero(t, writer.droppedEvents.Load())

	var row model.ChannelMonitorEventOutbox
	require.NoError(t, db.Where("event_id = ?", second.EventId).First(&row).Error)
	assert.Equal(t, string(secondPayload), row.Payload)
}

func TestChannelMonitorEventWriterStartOutboxSkipsStoppedWriter(t *testing.T) {
	setupChannelMonitorEventWriterReliabilityDB(t)
	writer := newChannelMonitorEventWriter(nil, channelMonitorEventWriterConfig{})
	writer.stopping.Store(true)

	writer.startOutbox()

	assert.Nil(t, writer.outboxDone)
}

func TestChannelMonitorEventWriterPersistsAfterRetryExhaustion(t *testing.T) {
	setupChannelMonitorEventWriterReliabilityDB(t)
	writer := newChannelMonitorEventWriter(channelMonitorEventWriterAlwaysFailAppender{}, channelMonitorEventWriterConfig{
		QueueCapacity: 1,
		MaxAttempts:   2,
		RetryDelay:    time.Nanosecond,
	})
	event := newChannelMonitorPublisherTestEvent("writer-retry-exhausted")
	payload, err := event.Marshal()
	require.NoError(t, err)

	assert.True(t, writer.write(channelMonitorEventWriterItem{event: event, payload: payload}))
	assert.Zero(t, writer.droppedEvents.Load())
	var row model.ChannelMonitorEventOutbox
	require.NoError(t, model.DB.Where("event_id = ?", event.EventId).First(&row).Error)
	assert.Equal(t, int64(0), row.AttemptCount)
}

func TestPublishChannelMonitorEventPersistsWhenRedisUnavailable(t *testing.T) {
	db := setupChannelMonitorEventWriterReliabilityDB(t)
	previousEnabled := common.RedisEnabled
	previousRDB := common.RDB
	previousWrite := common.RDBMonitorWrite
	previousRead := common.RDBMonitorRead
	previousConsumer := common.RDBMonitorConsumer
	common.RedisEnabled = false
	common.RDB = nil
	common.RDBMonitorWrite = nil
	common.RDBMonitorRead = nil
	common.RDBMonitorConsumer = nil
	t.Cleanup(func() {
		common.RedisEnabled = previousEnabled
		common.RDB = previousRDB
		common.RDBMonitorWrite = previousWrite
		common.RDBMonitorRead = previousRead
		common.RDBMonitorConsumer = previousConsumer
	})

	event := newChannelMonitorPublisherTestEvent("publisher-unavailable-persisted")
	status, err := PublishChannelMonitorEvent(context.Background(), event)
	require.NoError(t, err)
	assert.Equal(t, ChannelMonitorEventPublishStatusPersisted, status)
	var row model.ChannelMonitorEventOutbox
	require.NoError(t, db.Where("event_id = ?", event.EventId).First(&row).Error)
	assert.NotEmpty(t, row.Payload)
}

func TestEnqueueChannelMonitorEventWithoutWriterBoundsOutboxFallback(t *testing.T) {
	db := setupChannelMonitorEventWriterReliabilityDB(t)
	require.NoError(t, db.Exec("PRAGMA busy_timeout = 5000").Error)
	lockTx := db.Begin()
	require.NoError(t, lockTx.Error)
	lockEvent := newChannelMonitorPublisherTestEvent("writer-nil-outbox-lock")
	lockPayload, err := lockEvent.Marshal()
	require.NoError(t, err)
	require.NoError(t, lockTx.Create(&model.ChannelMonitorEventOutbox{
		EventId: lockEvent.EventId, Payload: string(lockPayload), NextAttemptAt: time.Now().Unix(),
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}).Error)
	t.Cleanup(func() { _ = lockTx.Rollback().Error })

	setChannelMonitorEventWriterForTest(t, nil)
	event := newChannelMonitorPublisherTestEvent("writer-nil-outbox-timeout")
	result := make(chan struct {
		status ChannelMonitorEventPublishStatus
		err    error
	}, 1)
	startedAt := time.Now()
	go func() {
		status, enqueueErr := EnqueueChannelMonitorEvent(event)
		result <- struct {
			status ChannelMonitorEventPublishStatus
			err    error
		}{status: status, err: enqueueErr}
	}()
	completed := false
	select {
	case outcome := <-result:
		completed = true
		assert.Less(t, time.Since(startedAt), 2*time.Second)
		assert.Equal(t, ChannelMonitorEventPublishStatusDropped, outcome.status)
		assert.Error(t, outcome.err)
	case <-time.After(2 * time.Second):
		t.Fatal("writer=nil outbox fallback blocked past its timeout")
	}
	require.NoError(t, lockTx.Rollback().Error)
	if !completed {
		select {
		case <-result:
		case <-time.After(2 * time.Second):
			t.Fatal("writer=nil outbox fallback worker did not exit after database lock release")
		}
	}
}

func TestEnqueueChannelMonitorEventWithoutOutboxTableFailsBoundedly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-event-writer-no-schema.db")), &gorm.Config{})
	require.NoError(t, err)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	setChannelMonitorEventWriterForTest(t, nil)

	startedAt := time.Now()
	status, enqueueErr := EnqueueChannelMonitorEvent(newChannelMonitorPublisherTestEvent("writer-nil-no-outbox-table"))

	assert.Less(t, time.Since(startedAt), 2*time.Second)
	assert.Equal(t, ChannelMonitorEventPublishStatusDropped, status)
	assert.Error(t, enqueueErr)
}

func TestChannelMonitorEventWriterQueueOverflowOutboxFallbackIsBounded(t *testing.T) {
	db := setupChannelMonitorEventWriterReliabilityDB(t)
	require.NoError(t, db.Exec("PRAGMA busy_timeout = 5000").Error)
	lockTx := db.Begin()
	require.NoError(t, lockTx.Error)
	lockEvent := newChannelMonitorPublisherTestEvent("writer-overflow-outbox-lock")
	lockPayload, err := lockEvent.Marshal()
	require.NoError(t, err)
	now := time.Now().Unix()
	require.NoError(t, lockTx.Create(&model.ChannelMonitorEventOutbox{
		EventId: lockEvent.EventId, Payload: string(lockPayload), NextAttemptAt: now,
		CreatedAt: now, UpdatedAt: now,
	}).Error)

	writer := newChannelMonitorEventWriter(nil, channelMonitorEventWriterConfig{
		QueueCapacity:       1,
		DirectPublishOnFull: false,
	})
	queued := newChannelMonitorPublisherTestEvent("writer-overflow-outbox-queued")
	queuedPayload, err := queued.Marshal()
	require.NoError(t, err)
	writer.queue <- channelMonitorEventWriterItem{event: queued, payload: queuedPayload}
	setChannelMonitorEventWriterForTest(t, writer)

	event := newChannelMonitorPublisherTestEvent("writer-overflow-outbox-timeout")
	startedAt := time.Now()
	status, enqueueErr := EnqueueChannelMonitorEvent(event)
	assert.Less(t, time.Since(startedAt), 2*time.Second)
	assert.Equal(t, ChannelMonitorEventPublishStatusDropped, status)
	assert.ErrorIs(t, enqueueErr, ErrChannelMonitorEventWriterQueueFull)

	require.NoError(t, lockTx.Rollback().Error)
}

func TestChannelMonitorEventOutboxFallbackCapsBlockedWriters(t *testing.T) {
	db := setupChannelMonitorEventWriterReliabilityDB(t)
	require.NoError(t, db.Exec("PRAGMA busy_timeout = 5000").Error)
	lockTx := db.Begin()
	require.NoError(t, lockTx.Error)
	lockEvent := newChannelMonitorPublisherTestEvent("writer-outbox-concurrency-lock")
	lockPayload, err := lockEvent.Marshal()
	require.NoError(t, err)
	now := time.Now().Unix()
	require.NoError(t, lockTx.Create(&model.ChannelMonitorEventOutbox{
		EventId: lockEvent.EventId, Payload: string(lockPayload), NextAttemptAt: now,
		CreatedAt: now, UpdatedAt: now,
	}).Error)
	t.Cleanup(func() { _ = lockTx.Rollback().Error })
	setChannelMonitorEventWriterForTest(t, nil)

	type enqueueResult struct {
		status ChannelMonitorEventPublishStatus
		err    error
	}
	requestCount := channelMonitorEventOutboxStoreConcurrency * 2
	startGate := make(chan struct{})
	results := make(chan enqueueResult, requestCount)
	startedAt := time.Now()
	for index := 0; index < requestCount; index++ {
		go func(index int) {
			<-startGate
			status, enqueueErr := EnqueueChannelMonitorEvent(
				newChannelMonitorPublisherTestEvent(fmt.Sprintf("writer-outbox-concurrency-%d", index)),
			)
			results <- enqueueResult{status: status, err: enqueueErr}
		}(index)
	}
	close(startGate)
	busyCount := 0
	for index := 0; index < requestCount; index++ {
		select {
		case outcome := <-results:
			assert.Equal(t, ChannelMonitorEventPublishStatusDropped, outcome.status)
			if errors.Is(outcome.err, ErrChannelMonitorEventOutboxStoreBusy) {
				busyCount++
			}
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent outbox fallback calls exceeded their bound")
		}
	}
	assert.Less(t, time.Since(startedAt), 2*time.Second)
	assert.Greater(t, busyCount, 0)
	require.NoError(t, lockTx.Rollback().Error)
	require.Eventually(t, func() bool {
		return len(channelMonitorEventOutboxStoreSlots) == 0
	}, 2*time.Second, 10*time.Millisecond)
}

func TestChannelMonitorEventWriterReplaysOutboxOnceByEventID(t *testing.T) {
	db := setupChannelMonitorEventWriterReliabilityDB(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	event := newChannelMonitorPublisherTestEvent("writer-replay-once")
	payload, err := event.Marshal()
	require.NoError(t, err)
	_, err = model.StoreChannelMonitorEventOutbox(context.Background(), event.EventId, payload)
	require.NoError(t, err)

	writer := newChannelMonitorEventWriter(client, channelMonitorEventWriterConfig{
		QueueCapacity: 1,
		RetryDelay:    time.Millisecond,
	})
	writer.outboxCtx, writer.outboxCancel = context.WithCancel(context.Background())
	writer.outboxDone = make(chan struct{})
	writer.outboxOwner = "writer-replay-test"
	t.Cleanup(func() { writer.outboxCancel() })
	writer.replayOutboxBatch()
	writer.replayOutboxBatch()

	messages, err := client.XRange(context.Background(), ChannelMonitorRedisEventStream, "-", "+").Result()
	require.NoError(t, err)
	assert.Len(t, messages, 1)
	var row model.ChannelMonitorEventOutbox
	require.NoError(t, db.Where("event_id = ?", event.EventId).First(&row).Error)
	assert.NotZero(t, row.ProcessedAt)
}
