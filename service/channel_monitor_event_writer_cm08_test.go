package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cm08DrainGateAppender struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once

	mu       sync.Mutex
	eventIDs []string
}

func (appender *cm08DrainGateAppender) XAdd(ctx context.Context, args *redis.XAddArgs) *redis.StringCmd {
	appender.once.Do(func() {
		close(appender.started)
		<-appender.release
	})
	values, _ := args.Values.(map[string]interface{})
	appender.mu.Lock()
	appender.eventIDs = append(appender.eventIDs, fmt.Sprint(values[ChannelMonitorRedisEventFieldEventID]))
	appender.mu.Unlock()
	cmd := redis.NewStringCmd(ctx, "XADD")
	cmd.SetVal(fmt.Sprintf("cm08-%d", len(appender.eventIDs)))
	return cmd
}

func (appender *cm08DrainGateAppender) writtenEventIDs() []string {
	appender.mu.Lock()
	defer appender.mu.Unlock()
	return append([]string(nil), appender.eventIDs...)
}

type cm08AmbiguousXAddAppender struct {
	client *redis.Client
	err    error

	mu    sync.Mutex
	calls int
}

func (appender *cm08AmbiguousXAddAppender) XAdd(ctx context.Context, args *redis.XAddArgs) *redis.StringCmd {
	appender.mu.Lock()
	appender.calls++
	call := appender.calls
	appender.mu.Unlock()

	cmd := appender.client.XAdd(ctx, args)
	if call != 1 || cmd.Err() != nil {
		return cmd
	}
	ambiguous := redis.NewStringCmd(ctx, "XADD")
	ambiguous.SetErr(appender.err)
	return ambiguous
}

func (appender *cm08AmbiguousXAddAppender) callCount() int {
	appender.mu.Lock()
	defer appender.mu.Unlock()
	return appender.calls
}

type cm08FailingXAddAppender struct {
	err error

	mu    sync.Mutex
	calls int
}

func (appender *cm08FailingXAddAppender) XAdd(ctx context.Context, _ *redis.XAddArgs) *redis.StringCmd {
	appender.mu.Lock()
	appender.calls++
	appender.mu.Unlock()
	cmd := redis.NewStringCmd(ctx, "XADD")
	cmd.SetErr(appender.err)
	return cmd
}

func (appender *cm08FailingXAddAppender) callCount() int {
	appender.mu.Lock()
	defer appender.mu.Unlock()
	return appender.calls
}

func TestCM08ChannelMonitorEventWriterDropsOnQueueOverflowEvenWhenLegacyDirectFlagEnabled(t *testing.T) {
	useChannelMonitorEventPublishStatsIsolation(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	writer := newChannelMonitorEventWriter(client, channelMonitorEventWriterConfig{
		QueueCapacity:       1,
		MaxAttempts:         1,
		DirectPublishOnFull: true,
	})
	first := newChannelMonitorPublisherTestEvent("cm08-overflow-first")
	second := newChannelMonitorPublisherTestEvent("cm08-overflow-second")

	channelMonitorEventWriterState.Lock()
	previous := channelMonitorEventWriterState.writer
	channelMonitorEventWriterState.writer = writer
	channelMonitorEventWriterState.Unlock()
	t.Cleanup(func() {
		_ = writer.Stop(context.Background())
		channelMonitorEventWriterState.Lock()
		channelMonitorEventWriterState.writer = previous
		channelMonitorEventWriterState.Unlock()
	})
	status, err := EnqueueChannelMonitorEvent(first)
	require.NoError(t, err)
	require.Equal(t, ChannelMonitorEventPublishStatusQueued, status)
	status, err = EnqueueChannelMonitorEvent(second)
	require.ErrorIs(t, err, ErrChannelMonitorEventWriterQueueFull)
	assert.Equal(t, ChannelMonitorEventPublishStatusDropped, status)
	messages, err := client.XRange(context.Background(), ChannelMonitorRedisEventStream, "-", "+").Result()
	require.NoError(t, err)
	assert.Empty(t, messages)
}

func TestCM08ChannelMonitorEventWriterStopDrainsAcceptedEvents(t *testing.T) {
	useChannelMonitorEventPublishStatsIsolation(t)
	appender := &cm08DrainGateAppender{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	writer := newChannelMonitorEventWriter(appender, channelMonitorEventWriterConfig{
		QueueCapacity: 2,
		MaxAttempts:   1,
		RetryDelay:    time.Nanosecond,
	})

	channelMonitorEventWriterState.Lock()
	previousWriter := channelMonitorEventWriterState.writer
	channelMonitorEventWriterState.writer = writer
	channelMonitorEventWriterState.Unlock()
	t.Cleanup(func() {
		_ = writer.Stop(context.Background())
		channelMonitorEventWriterState.Lock()
		if channelMonitorEventWriterState.writer == nil || channelMonitorEventWriterState.writer == writer {
			channelMonitorEventWriterState.writer = previousWriter
		}
		channelMonitorEventWriterState.Unlock()
	})
	t.Cleanup(func() {
		select {
		case <-appender.release:
		default:
			close(appender.release)
		}
	})

	go writer.run()
	firstStatus, err := EnqueueChannelMonitorEvent(newChannelMonitorPublisherTestEvent("cm08-drain-first"))
	require.NoError(t, err)
	require.Equal(t, ChannelMonitorEventPublishStatusQueued, firstStatus)
	<-appender.started

	secondStatus, err := EnqueueChannelMonitorEvent(newChannelMonitorPublisherTestEvent("cm08-drain-second"))
	require.NoError(t, err)
	require.Equal(t, ChannelMonitorEventPublishStatusQueued, secondStatus)

	stopResult := make(chan error, 1)
	go func() {
		stopResult <- writer.Stop(context.Background())
	}()
	<-writer.stopCh

	afterStopStatus, err := EnqueueChannelMonitorEvent(newChannelMonitorPublisherTestEvent("cm08-after-stop"))
	require.ErrorIs(t, err, ErrChannelMonitorEventRedisUnavailable)
	assert.Equal(t, ChannelMonitorEventPublishStatusDropped, afterStopStatus)

	close(appender.release)
	require.NoError(t, <-stopResult)
	assert.Equal(t, []string{"cm08-drain-first", "cm08-drain-second"}, appender.writtenEventIDs())
	assert.Empty(t, writer.queue)
	assert.Equal(t, int64(2), writer.queuedEvents.Load())
	assert.Zero(t, writer.droppedEvents.Load())
}

func TestCM08ChannelMonitorEventWriterDropsAfterRedisFailuresExhaustRetries(t *testing.T) {
	useChannelMonitorEventPublishStatsIsolation(t)
	appender := &cm08FailingXAddAppender{err: errors.New("Redis XADD unavailable")}
	writer := newChannelMonitorEventWriter(appender, channelMonitorEventWriterConfig{
		QueueCapacity: 1,
		MaxAttempts:   3,
		RetryDelay:    time.Nanosecond,
	})
	event := newChannelMonitorPublisherTestEvent("cm08-redis-unavailable")
	payload, err := event.Marshal()
	require.NoError(t, err)

	writer.write(channelMonitorEventWriterItem{event: event, payload: payload})
	assert.Equal(t, 3, appender.callCount())
	assert.Equal(t, int64(2), writer.retryEvents.Load())
	assert.Equal(t, int64(1), writer.droppedEvents.Load())
	publishStats := GetChannelMonitorEventPublishStats()
	assert.Zero(t, publishStats.PublishedEvents)
	assert.Equal(t, int64(3), publishStats.FailedEvents)
	assert.False(t, publishStats.RealtimeAvailable)
}

func TestCM08ChannelMonitorEventWriterAmbiguousRedisFailureIsIdempotent(t *testing.T) {
	useChannelMonitorEventPublishStatsIsolation(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.XGroupCreateMkStream(
		context.Background(),
		ChannelMonitorRedisEventStream,
		ChannelMonitorRedisConsumerGroup,
		"0",
	).Err())

	appender := &cm08AmbiguousXAddAppender{
		client: client,
		err:    errors.New("Redis XADD acknowledgement lost"),
	}
	writer := newChannelMonitorEventWriter(appender, channelMonitorEventWriterConfig{
		QueueCapacity: 1,
		MaxAttempts:   2,
		RetryDelay:    time.Nanosecond,
	})
	event := newChannelMonitorPublisherTestEvent("cm08-ambiguous-xadd")
	event.ModelName = "gpt-4o-mini"
	event.GroupName = "default"
	event.RequestDispatched = true
	event.IsFinalAttempt = true
	payload, err := event.Marshal()
	require.NoError(t, err)

	writer.write(channelMonitorEventWriterItem{event: event, payload: payload})
	assert.Equal(t, 2, appender.callCount())
	assert.Equal(t, int64(1), writer.retryEvents.Load())
	assert.Zero(t, writer.droppedEvents.Load())
	messages, err := client.XRange(context.Background(), ChannelMonitorRedisEventStream, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, messages, 2)
	for _, message := range messages {
		assert.Equal(t, event.EventId, fmt.Sprint(message.Values[ChannelMonitorRedisEventFieldEventID]))
	}

	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	handlerCalls := 0
	handledEvents := 0
	consumer, err := newChannelMonitorRedisEventConsumer(
		client,
		ChannelMonitorRedisConsumerName("cm08-ambiguous-xadd"),
		ChannelMonitorRedisEventHandlerFunc(func(ctx context.Context, events []model.ChannelMonitorEvent) error {
			handlerCalls++
			handledEvents += len(events)
			return projection.HandleChannelMonitorEvents(ctx, events)
		}),
		channelMonitorRedisConsumerConfig{
			BatchSize:        10,
			Block:            -1,
			ClaimMinIdle:     time.Second,
			LeaseTTL:         time.Minute,
			LeaseHeartbeat:   10 * time.Second,
			OperationTimeout: time.Second,
			RetryDelay:       time.Nanosecond,
			DedupTTL:         channelMonitorRedisReplayProtectionTTL,
		},
	)
	require.NoError(t, err)
	processed, acquired, err := consumer.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 2, processed)
	assert.Equal(t, 1, handlerCalls)
	assert.Equal(t, 1, handledEvents)

	view, err := projection.Query(context.Background(), event.OccurredAt-60, event.OccurredAt+60)
	require.NoError(t, err)
	require.Len(t, view.Routes, 1)
	assert.Equal(t, int64(1), view.Routes[0].EventCount)
	assert.Equal(t, int64(1), view.Routes[0].ActualSuccessCount)
	pending, err := client.XPending(
		context.Background(),
		ChannelMonitorRedisEventStream,
		ChannelMonitorRedisConsumerGroup,
	).Result()
	require.NoError(t, err)
	assert.Zero(t, pending.Count)
}
