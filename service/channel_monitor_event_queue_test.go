package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newChannelMonitorQueueTestEvent(eventId string) model.ChannelMonitorEvent {
	return model.ChannelMonitorEvent{
		EventId:       eventId,
		SchemaVersion: model.ChannelMonitorEventSchemaVersion,
		OccurredAt:    1_700_000_000,
		CreatedAt:     1_700_000_001,
		ChannelId:     1,
		Source:        model.ChannelMonitorEventSourceBusiness,
		Outcome:       model.ChannelMonitorEventOutcomeSuccess,
		CostStatus:    model.ChannelMonitorEventCostNone,
	}
}

func newChannelMonitorQueueTestConfig() channelMonitorEventQueueConfig {
	return channelMonitorEventQueueConfig{
		Capacity:        8,
		MaxBatchSize:    4,
		FlushInterval:   time.Hour,
		ConsumerTimeout: time.Second,
		MaxAttempts:     1,
		DedupCapacity:   16,
	}
}

func TestChannelMonitorEventQueueIsNonBlockingAndDropsNewestWhenFull(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseConsumer := func() {
		releaseOnce.Do(func() { close(release) })
	}
	config := newChannelMonitorQueueTestConfig()
	config.Capacity = 1
	config.MaxBatchSize = 1
	queue := newChannelMonitorEventQueue(config, func(_ context.Context, _ []model.ChannelMonitorEvent) error {
		started <- struct{}{}
		<-release
		return nil
	})
	t.Cleanup(func() {
		releaseConsumer()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = queue.stop(ctx)
	})

	assert.Equal(t, ChannelMonitorEventEnqueueAccepted, queue.enqueue(newChannelMonitorQueueTestEvent("event-1")))
	<-started
	assert.Equal(t, ChannelMonitorEventEnqueueAccepted, queue.enqueue(newChannelMonitorQueueTestEvent("event-2")))
	assert.Equal(t, ChannelMonitorEventEnqueueFull, queue.enqueue(newChannelMonitorQueueTestEvent("event-3")))
	releaseConsumer()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, queue.flush(ctx))
	stats := queue.stats()
	assert.Equal(t, int64(2), stats.AcceptedEvents)
	assert.Equal(t, int64(2), stats.ProcessedEvents)
	assert.Equal(t, int64(1), stats.QueueFullDrops)
	assert.Equal(t, int64(1), stats.DroppedEvents)
	assert.Zero(t, stats.PendingEvents)
}

func TestChannelMonitorEventQueueConsumesFullBatches(t *testing.T) {
	received := make(chan []model.ChannelMonitorEvent, 1)
	config := newChannelMonitorQueueTestConfig()
	config.MaxBatchSize = 3
	queue := newChannelMonitorEventQueue(config, func(_ context.Context, events []model.ChannelMonitorEvent) error {
		copied := append([]model.ChannelMonitorEvent(nil), events...)
		received <- copied
		return nil
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = queue.stop(ctx)
	})

	for _, eventId := range []string{"event-1", "event-2", "event-3"} {
		assert.Equal(t, ChannelMonitorEventEnqueueAccepted, queue.enqueue(newChannelMonitorQueueTestEvent(eventId)))
	}

	batch := <-received
	require.Len(t, batch, 3)
	assert.Equal(t, []string{"event-1", "event-2", "event-3"}, []string{batch[0].EventId, batch[1].EventId, batch[2].EventId})
	assert.NotZero(t, batch[0].EventSequence)
	assert.Less(t, batch[0].EventSequence, batch[1].EventSequence)
	assert.Less(t, batch[1].EventSequence, batch[2].EventSequence)
}

func TestChannelMonitorEventQueueDeduplicatesEventIdsBeforeConsumption(t *testing.T) {
	received := make(chan []model.ChannelMonitorEvent, 1)
	config := newChannelMonitorQueueTestConfig()
	config.MaxBatchSize = 3
	queue := newChannelMonitorEventQueue(config, func(_ context.Context, events []model.ChannelMonitorEvent) error {
		received <- append([]model.ChannelMonitorEvent(nil), events...)
		return nil
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = queue.stop(ctx)
	})

	duplicate := newChannelMonitorQueueTestEvent("event-duplicate")
	require.Equal(t, ChannelMonitorEventEnqueueAccepted, queue.enqueue(duplicate))
	require.Equal(t, ChannelMonitorEventEnqueueAccepted, queue.enqueue(duplicate))
	require.Equal(t, ChannelMonitorEventEnqueueAccepted, queue.enqueue(newChannelMonitorQueueTestEvent("event-unique")))

	batch := <-received
	require.Len(t, batch, 2)
	assert.Equal(t, "event-duplicate", batch[0].EventId)
	assert.Equal(t, "event-unique", batch[1].EventId)
	stats := queue.stats()
	assert.Equal(t, int64(1), stats.DuplicateEvents)
	assert.Equal(t, int64(2), stats.ProcessedEvents)
	assert.Zero(t, stats.PendingEvents)
}

func TestChannelMonitorEventQueueDeduplicatesEventIdsAcrossBatches(t *testing.T) {
	var consumed atomic.Int64
	config := newChannelMonitorQueueTestConfig()
	config.MaxBatchSize = 1
	queue := newChannelMonitorEventQueue(config, func(_ context.Context, events []model.ChannelMonitorEvent) error {
		consumed.Add(int64(len(events)))
		return nil
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = queue.stop(ctx)
	})

	event := newChannelMonitorQueueTestEvent("event-across-batches")
	require.Equal(t, ChannelMonitorEventEnqueueAccepted, queue.enqueue(event))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, queue.flush(ctx))
	cancel()
	require.Equal(t, ChannelMonitorEventEnqueueAccepted, queue.enqueue(event))
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, queue.flush(ctx))
	cancel()

	assert.Equal(t, int64(1), consumed.Load())
	stats := queue.stats()
	assert.Equal(t, int64(1), stats.DuplicateEvents)
	assert.Equal(t, int64(1), stats.ProcessedEvents)
	assert.Zero(t, stats.PendingEvents)
}

func TestChannelMonitorEventQueueRetriesConsumerWithoutDuplicatingBatch(t *testing.T) {
	var attempts atomic.Int32
	var consumed atomic.Int64
	config := newChannelMonitorQueueTestConfig()
	config.MaxBatchSize = 1
	config.MaxAttempts = 2
	queue := newChannelMonitorEventQueue(config, func(_ context.Context, events []model.ChannelMonitorEvent) error {
		if attempts.Add(1) == 1 {
			return errors.New("temporary failure")
		}
		consumed.Add(int64(len(events)))
		return nil
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = queue.stop(ctx)
	})

	require.Equal(t, ChannelMonitorEventEnqueueAccepted, queue.enqueue(newChannelMonitorQueueTestEvent("event-retry")))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, queue.flush(ctx))

	assert.Equal(t, int32(2), attempts.Load())
	assert.Equal(t, int64(1), consumed.Load())
	stats := queue.stats()
	assert.Equal(t, int64(1), stats.ConsumerErrors)
	assert.Equal(t, int64(1), stats.ConsumerRetries)
	assert.Equal(t, int64(1), stats.ProcessedEvents)
	assert.Zero(t, stats.FailedEvents)
}

func TestChannelMonitorEventQueueGracefulStopFlushesPartialBatch(t *testing.T) {
	received := make(chan []model.ChannelMonitorEvent, 1)
	queue := newChannelMonitorEventQueue(newChannelMonitorQueueTestConfig(), func(_ context.Context, events []model.ChannelMonitorEvent) error {
		received <- append([]model.ChannelMonitorEvent(nil), events...)
		return nil
	})

	require.Equal(t, ChannelMonitorEventEnqueueAccepted, queue.enqueue(newChannelMonitorQueueTestEvent("event-1")))
	require.Equal(t, ChannelMonitorEventEnqueueAccepted, queue.enqueue(newChannelMonitorQueueTestEvent("event-2")))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, queue.stop(ctx))

	batch := <-received
	assert.Len(t, batch, 2)
	assert.Equal(t, ChannelMonitorEventEnqueueStopped, queue.enqueue(newChannelMonitorQueueTestEvent("event-after-stop")))
	stats := queue.stats()
	assert.Equal(t, int64(2), stats.ProcessedEvents)
	assert.Equal(t, int64(1), stats.StoppedDrops)
	assert.Zero(t, stats.PendingEvents)
}

func TestChannelMonitorEventQueueTracksInvalidAndTerminalFailures(t *testing.T) {
	config := newChannelMonitorQueueTestConfig()
	config.MaxBatchSize = 1
	queue := newChannelMonitorEventQueue(config, func(context.Context, []model.ChannelMonitorEvent) error {
		return errors.New("permanent failure")
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = queue.stop(ctx)
	})

	assert.Equal(t, ChannelMonitorEventEnqueueInvalid, queue.enqueue(model.ChannelMonitorEvent{}))
	assert.Equal(t, ChannelMonitorEventEnqueueAccepted, queue.enqueue(newChannelMonitorQueueTestEvent("event-failed")))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assert.Error(t, queue.flush(ctx))

	stats := queue.stats()
	assert.Equal(t, int64(1), stats.InvalidEvents)
	assert.Equal(t, int64(1), stats.ConsumerErrors)
	assert.Equal(t, int64(1), stats.FailedEvents)
	assert.Equal(t, int64(1), stats.DroppedEvents)
	assert.Zero(t, stats.PendingEvents)
}

func TestChannelMonitorEventGlobalQueueCanRegisterConsumerAfterEnqueue(t *testing.T) {
	var consumed atomic.Int64
	resetChannelMonitorEventQueueForTest(newChannelMonitorQueueTestConfig(), nil)
	t.Cleanup(func() {
		resetChannelMonitorEventQueueForTest(
			defaultChannelMonitorEventQueueConfig(),
			consumeChannelMonitorEventProjectionBatch,
		)
	})

	assert.Equal(t, ChannelMonitorEventEnqueueAccepted, EmitChannelMonitorEvent(newChannelMonitorQueueTestEvent("event-global")))
	require.True(t, SetChannelMonitorEventConsumer(func(_ context.Context, events []model.ChannelMonitorEvent) error {
		consumed.Add(int64(len(events)))
		return nil
	}))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, FlushChannelMonitorEvents(ctx))
	assert.Equal(t, int64(1), consumed.Load())
	assert.Equal(t, int64(1), GetChannelMonitorEventQueueStats().ProcessedEvents)
}

func TestChannelMonitorEventQueueReplacesConsumerForLaterBatches(t *testing.T) {
	var firstConsumed atomic.Int64
	var secondConsumed atomic.Int64
	config := newChannelMonitorQueueTestConfig()
	config.MaxBatchSize = 1
	queue := newChannelMonitorEventQueue(config, func(_ context.Context, events []model.ChannelMonitorEvent) error {
		firstConsumed.Add(int64(len(events)))
		return nil
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = queue.stop(ctx)
	})

	require.Equal(t, ChannelMonitorEventEnqueueAccepted, queue.enqueue(newChannelMonitorQueueTestEvent("event-first-consumer")))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, queue.flush(ctx))
	cancel()
	require.True(t, queue.setConsumer(func(_ context.Context, events []model.ChannelMonitorEvent) error {
		secondConsumed.Add(int64(len(events)))
		return nil
	}))
	require.Equal(t, ChannelMonitorEventEnqueueAccepted, queue.enqueue(newChannelMonitorQueueTestEvent("event-second-consumer")))
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, queue.flush(ctx))
	cancel()

	assert.Equal(t, int64(1), firstConsumed.Load())
	assert.Equal(t, int64(1), secondConsumed.Load())
}

func TestChannelMonitorEventQueueRetainsEventsUntilConsumerIsConfigured(t *testing.T) {
	queue := newChannelMonitorEventQueue(newChannelMonitorQueueTestConfig(), nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = queue.stop(ctx)
	})

	require.Equal(t, ChannelMonitorEventEnqueueAccepted, queue.enqueue(newChannelMonitorQueueTestEvent("event-before-consumer")))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	assert.ErrorIs(t, queue.flush(ctx), errChannelMonitorEventConsumerUnavailable)
	cancel()
	assert.Equal(t, int64(1), queue.stats().PendingEvents)

	var consumed atomic.Int64
	require.True(t, queue.setConsumer(func(_ context.Context, events []model.ChannelMonitorEvent) error {
		consumed.Add(int64(len(events)))
		return nil
	}))
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, queue.flush(ctx))
	cancel()
	assert.Equal(t, int64(1), consumed.Load())
	assert.Zero(t, queue.stats().PendingEvents)
}
