package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelMonitorRedisRuntimeTestConsumer struct {
	callCount atomic.Int64
	calls     chan int64
}

func (consumer *channelMonitorRedisRuntimeTestConsumer) Run(ctx context.Context) error {
	call := consumer.callCount.Add(1)
	consumer.calls <- call
	if call == 1 {
		return errors.New("redis disconnected")
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestChannelMonitorRedisRuntimeRestartsConsumerAndStopsIdempotently(t *testing.T) {
	consumer := &channelMonitorRedisRuntimeTestConsumer{calls: make(chan int64, 2)}
	var initCalls atomic.Int64
	runtime := startChannelMonitorRedisRuntime(
		func(context.Context) error {
			initCalls.Add(1)
			return nil
		},
		consumer,
		time.Millisecond,
		time.Millisecond,
		func(error) {},
	)

	assert.Equal(t, int64(1), <-consumer.calls)
	assert.Equal(t, int64(2), <-consumer.calls)
	assert.GreaterOrEqual(t, initCalls.Load(), int64(2))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runtime.Stop(ctx))
	require.NoError(t, runtime.Stop(ctx))
}

func TestChannelMonitorRedisRuntimeRetriesStreamInitialization(t *testing.T) {
	consumer := &channelMonitorRedisRuntimeTestConsumer{calls: make(chan int64, 1)}
	var initCalls atomic.Int64
	runtime := startChannelMonitorRedisRuntime(
		func(context.Context) error {
			if initCalls.Add(1) == 1 {
				return errors.New("group missing")
			}
			return nil
		},
		consumer,
		time.Millisecond,
		time.Millisecond,
		func(error) {},
	)

	assert.Equal(t, int64(1), <-consumer.calls)
	assert.GreaterOrEqual(t, initCalls.Load(), int64(2))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runtime.Stop(ctx))
}

func TestChannelMonitorRedisRuntimeStopCancelsBlockedStreamInitialization(t *testing.T) {
	started := make(chan struct{})
	runtime := startChannelMonitorRedisRuntime(
		func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
		&channelMonitorRedisRuntimeTestConsumer{calls: make(chan int64, 1)},
		time.Hour,
		time.Hour,
		func(error) {},
	)
	<-started

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, runtime.Stop(stopCtx))
}
