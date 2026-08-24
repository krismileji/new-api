package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type retryingChannelMonitorEventAppender struct {
	calls atomic.Int64
}

type cancelableChannelMonitorEventAppender struct {
	started  chan struct{}
	canceled chan struct{}
	release  chan struct{}
	once     sync.Once
	calls    atomic.Int64
}

func (appender *cancelableChannelMonitorEventAppender) XAdd(ctx context.Context, _ *redis.XAddArgs) *redis.StringCmd {
	appender.calls.Add(1)
	appender.once.Do(func() { close(appender.started) })
	<-ctx.Done()
	close(appender.canceled)
	<-appender.release
	cmd := redis.NewStringCmd(ctx, "XADD")
	cmd.SetErr(ctx.Err())
	return cmd
}

type signalingFailedChannelMonitorEventAppender struct {
	called chan struct{}
	calls  atomic.Int64
}

func (appender *signalingFailedChannelMonitorEventAppender) XAdd(ctx context.Context, _ *redis.XAddArgs) *redis.StringCmd {
	appender.calls.Add(1)
	select {
	case <-appender.called:
	default:
		close(appender.called)
	}
	cmd := redis.NewStringCmd(ctx, "XADD")
	cmd.SetErr(errors.New("temporary XADD failure"))
	return cmd
}

func (appender *retryingChannelMonitorEventAppender) XAdd(ctx context.Context, _ *redis.XAddArgs) *redis.StringCmd {
	call := appender.calls.Add(1)
	cmd := redis.NewStringCmd(ctx, "XADD")
	if call < 3 {
		cmd.SetErr(errors.New("temporary XADD failure"))
		return cmd
	}
	cmd.SetVal("1-0")
	return cmd
}

func setChannelMonitorEventWriterForTest(t *testing.T, writer *channelMonitorEventWriter) {
	t.Helper()
	channelMonitorEventWriterState.Lock()
	previous := channelMonitorEventWriterState.writer
	channelMonitorEventWriterState.writer = writer
	channelMonitorEventWriterState.Unlock()
	t.Cleanup(func() {
		channelMonitorEventWriterState.Lock()
		if channelMonitorEventWriterState.writer == writer {
			channelMonitorEventWriterState.writer = previous
		}
		channelMonitorEventWriterState.Unlock()
	})
}

func TestStartChannelMonitorEventWriterRequiresRedis(t *testing.T) {
	previousEnabled := common.RedisEnabled
	previousClient := common.RDB
	previousWriteClient := common.RDBMonitorWrite
	common.RedisEnabled = false
	common.RDB = nil
	common.RDBMonitorWrite = nil
	setChannelMonitorEventWriterForTest(t, nil)
	t.Cleanup(func() {
		common.RedisEnabled = previousEnabled
		common.RDB = previousClient
		common.RDBMonitorWrite = previousWriteClient
	})

	writer, err := StartChannelMonitorEventWriter()
	require.ErrorIs(t, err, ErrChannelMonitorEventRedisUnavailable)
	assert.Nil(t, writer)
	status, err := EnqueueChannelMonitorEvent(newChannelMonitorPublisherTestEvent("redis-disabled"))
	require.ErrorIs(t, err, ErrChannelMonitorEventRedisUnavailable)
	assert.Equal(t, ChannelMonitorEventPublishStatusDropped, status)
}

func TestEnqueueChannelMonitorEventDropsWhenQueueIsFull(t *testing.T) {
	event := newChannelMonitorPublisherTestEvent("queue-full")
	writer := newChannelMonitorEventWriter(nil, channelMonitorEventWriterConfig{QueueCapacity: 1})
	writer.queue <- channelMonitorEventWriterItem{event: event}
	setChannelMonitorEventWriterForTest(t, writer)

	status, err := EnqueueChannelMonitorEvent(event)
	require.ErrorIs(t, err, ErrChannelMonitorEventWriterQueueFull)
	assert.Equal(t, ChannelMonitorEventPublishStatusDropped, status)
	assert.Equal(t, int64(1), writer.droppedEvents.Load())
}

func TestChannelMonitorEventWriterFlushesQueuedEventOnStop(t *testing.T) {
	useChannelMonitorEventPublishStatsIsolation(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	previousEnabled, previousClient := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = client
	defer func() {
		common.RedisEnabled = previousEnabled
		common.RDB = previousClient
	}()

	writer := newChannelMonitorEventWriter(client, channelMonitorEventWriterConfig{
		QueueCapacity: 2,
		MaxAttempts:   1,
		RetryDelay:    time.Millisecond,
	})
	setChannelMonitorEventWriterForTest(t, writer)
	go writer.run()
	status, err := EnqueueChannelMonitorEvent(newChannelMonitorPublisherTestEvent("flush-on-stop"))
	require.NoError(t, err)
	assert.Equal(t, ChannelMonitorEventPublishStatusQueued, status)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, writer.Stop(stopCtx))
	messages, err := client.XRange(context.Background(), ChannelMonitorRedisEventStream, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "flush-on-stop", messages[0].Values[ChannelMonitorRedisEventFieldEventID])
}

func TestChannelMonitorEventWriterRetriesFailedPublish(t *testing.T) {
	useChannelMonitorEventPublishStatsIsolation(t)
	appender := &retryingChannelMonitorEventAppender{}
	writer := newChannelMonitorEventWriter(appender, channelMonitorEventWriterConfig{
		QueueCapacity: 1,
		MaxAttempts:   3,
		RetryDelay:    time.Millisecond,
	})
	writer.write(channelMonitorEventWriterItem{event: newChannelMonitorPublisherTestEvent("retry")})
	assert.Equal(t, int64(3), appender.calls.Load())
	assert.Equal(t, int64(2), writer.retryEvents.Load())
	assert.Zero(t, writer.droppedEvents.Load())
}

func TestChannelMonitorEventWriterCapturedBeforeStopCannotEnqueueAfterExit(t *testing.T) {
	useChannelMonitorEventPublishStatsIsolation(t)
	writer := newChannelMonitorEventWriter(nil, channelMonitorEventWriterConfig{
		QueueCapacity: 1,
		MaxAttempts:   1,
	})
	setChannelMonitorEventWriterForTest(t, writer)
	go writer.run()

	channelMonitorEventWriterState.RLock()
	captured := channelMonitorEventWriterState.writer
	channelMonitorEventWriterState.RUnlock()
	require.Same(t, writer, captured)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, writer.Stop(stopCtx))

	event := newChannelMonitorPublisherTestEvent("captured-after-stop")
	payload, err := event.Marshal()
	require.NoError(t, err)
	status, err := captured.enqueue(channelMonitorEventWriterItem{
		event: event, payload: payload, enqueuedAt: time.Now().Unix(),
	})
	require.ErrorIs(t, err, ErrChannelMonitorEventRedisUnavailable)
	assert.Equal(t, ChannelMonitorEventPublishStatusDropped, status)
	assert.Empty(t, writer.queue)
	assert.Zero(t, writer.queuedEvents.Load())
}

func TestChannelMonitorEventWriterStopDrainsEnqueueLinearizedBeforeStop(t *testing.T) {
	useChannelMonitorEventPublishStatsIsolation(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	writer := newChannelMonitorEventWriter(client, channelMonitorEventWriterConfig{
		QueueCapacity: 1,
		MaxAttempts:   1,
	})
	setChannelMonitorEventWriterForTest(t, writer)
	go writer.run()

	// This is the critical section used by EnqueueChannelMonitorEvent after it
	// resolves the active writer. Stop cannot pass its linearization point until
	// this accepted send completes and the registry read lock is released.
	channelMonitorEventWriterState.RLock()
	stopStarted := make(chan struct{})
	stopResult := make(chan error, 1)
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		close(stopStarted)
		stopResult <- writer.Stop(stopCtx)
	}()
	<-stopStarted

	event := newChannelMonitorPublisherTestEvent("linearized-before-stop")
	payload, err := event.Marshal()
	require.NoError(t, err)
	status, err := writer.enqueue(channelMonitorEventWriterItem{
		event: event, payload: payload, enqueuedAt: time.Now().Unix(),
	})
	require.NoError(t, err)
	assert.Equal(t, ChannelMonitorEventPublishStatusQueued, status)
	channelMonitorEventWriterState.RUnlock()

	require.NoError(t, <-stopResult)
	messages, err := client.XRange(context.Background(), ChannelMonitorRedisEventStream, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "linearized-before-stop", messages[0].Values[ChannelMonitorRedisEventFieldEventID])
}

func TestChannelMonitorEventWriterStopContextCancelsBlockedPublish(t *testing.T) {
	useChannelMonitorEventPublishStatsIsolation(t)
	appender := &cancelableChannelMonitorEventAppender{
		started:  make(chan struct{}),
		canceled: make(chan struct{}),
		release:  make(chan struct{}),
	}
	writer := newChannelMonitorEventWriter(appender, channelMonitorEventWriterConfig{
		QueueCapacity: 1,
		MaxAttempts:   3,
		RetryDelay:    time.Hour,
	})
	setChannelMonitorEventWriterForTest(t, writer)
	go writer.run()
	status, err := EnqueueChannelMonitorEvent(newChannelMonitorPublisherTestEvent("cancel-blocked-publish"))
	require.NoError(t, err)
	require.Equal(t, ChannelMonitorEventPublishStatusQueued, status)
	<-appender.started

	stopCtx, cancelStop := context.WithCancel(context.Background())
	stopResult := make(chan error, 1)
	go func() { stopResult <- writer.Stop(stopCtx) }()
	<-writer.stopCh
	cancelStop()
	require.ErrorIs(t, <-stopResult, context.Canceled)
	<-appender.canceled
	close(appender.release)
	<-writer.doneCh

	assert.Equal(t, int64(1), appender.calls.Load())
	assert.Zero(t, writer.retryEvents.Load())
	assert.Equal(t, int64(1), writer.droppedEvents.Load())
}

func TestChannelMonitorEventWriterStopContextCancelsRetryDelay(t *testing.T) {
	useChannelMonitorEventPublishStatsIsolation(t)
	appender := &signalingFailedChannelMonitorEventAppender{called: make(chan struct{})}
	writer := newChannelMonitorEventWriter(appender, channelMonitorEventWriterConfig{
		QueueCapacity: 1,
		MaxAttempts:   3,
		RetryDelay:    time.Hour,
	})
	setChannelMonitorEventWriterForTest(t, writer)
	go writer.run()
	status, err := EnqueueChannelMonitorEvent(newChannelMonitorPublisherTestEvent("cancel-retry"))
	require.NoError(t, err)
	require.Equal(t, ChannelMonitorEventPublishStatusQueued, status)
	<-appender.called

	stopCtx, cancelStop := context.WithCancel(context.Background())
	cancelStop()
	require.ErrorIs(t, writer.Stop(stopCtx), context.Canceled)
	<-writer.doneCh

	assert.Equal(t, int64(1), appender.calls.Load())
	assert.Zero(t, writer.retryEvents.Load())
	assert.Equal(t, int64(1), writer.droppedEvents.Load())
}
