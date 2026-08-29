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

func TestChannelMonitorRedisConsumerBoundsHandlerExecution(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("handler-deadline"))
	config := channelMonitorRedisConsumerTestConfig()
	config.HandlerTimeout = 5 * time.Millisecond
	consumer := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"handler-deadline",
		func(ctx context.Context, _ []model.ChannelMonitorEvent) error {
			<-ctx.Done()
			return ctx.Err()
		},
		config,
	)

	_, acquired, err := consumer.consumeOnce(context.Background())
	assert.True(t, acquired)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	pending, pendingErr := client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, pendingErr)
	assert.Equal(t, int64(1), pending.Count)
}

func TestChannelMonitorRedisConsumerGivesFreshEventsATurnAfterPendingRetries(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("pending-poison"))
	config := channelMonitorRedisConsumerTestConfig()
	config.PendingRetryLimit = 1
	var handled atomic.Int64
	consumer := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"pending-fairness",
		func(_ context.Context, events []model.ChannelMonitorEvent) error {
			if events[0].EventId == "pending-poison" {
				return errors.New("poison")
			}
			handled.Add(int64(len(events)))
			return nil
		},
		config,
	)

	_, _, firstErr := consumer.consumeOnce(context.Background())
	require.Error(t, firstErr)
	addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("fresh-event"))
	_, _, retryErr := consumer.consumeOnce(context.Background())
	require.Error(t, retryErr)
	processed, acquired, err := consumer.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 1, processed)
	assert.Equal(t, int64(1), handled.Load())
	pending, pendingErr := client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, pendingErr)
	assert.Equal(t, int64(1), pending.Count)
}

func TestChannelMonitorRedisConsumerFairnessCounterResetsWhenNoFreshEventsExist(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("pending-only"))
	config := channelMonitorRedisConsumerTestConfig()
	config.PendingRetryLimit = 1
	consumer := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"pending-only-fairness",
		func(_ context.Context, _ []model.ChannelMonitorEvent) error {
			return errors.New("poison")
		},
		config,
	)
	_, _, err := consumer.consumeOnce(context.Background())
	require.Error(t, err)
	consumer.pendingRetryCycles.Store(int64(config.PendingRetryLimit))

	// No fresh stream entry is available. The force-new turn must not leave the
	// counter latched forever, otherwise the pending poison message would never
	// be retried after this fairness probe.
	_, _, err = consumer.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.Zero(t, consumer.pendingRetryCycles.Load())
}

func TestChannelMonitorRedisConsumerWorkerPartitionsPreserveChannelOrder(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	events := []model.ChannelMonitorEvent{
		newChannelMonitorRedisConsumerTestEvent("channel-1-first"),
		newChannelMonitorRedisConsumerTestEvent("channel-2-first"),
		newChannelMonitorRedisConsumerTestEvent("channel-1-second"),
		newChannelMonitorRedisConsumerTestEvent("channel-2-second"),
	}
	events[0].ChannelId = 1
	events[1].ChannelId = 2
	events[2].ChannelId = 1
	events[3].ChannelId = 2
	for _, event := range events {
		addChannelMonitorRedisConsumerTestEvent(t, client, event)
	}
	config := channelMonitorRedisConsumerTestConfig()
	config.WorkerCount = 2
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var mu sync.Mutex
	seen := make(map[int][]string)
	consumer := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"partition-order",
		func(_ context.Context, batch []model.ChannelMonitorEvent) error {
			if len(batch) != 2 {
				return errors.New("worker partition contained an unexpected event count")
			}
			mu.Lock()
			for _, event := range batch {
				seen[event.ChannelId] = append(seen[event.ChannelId], event.EventId)
			}
			mu.Unlock()
			started <- struct{}{}
			<-release
			return nil
		},
		config,
	)

	result := make(chan error, 1)
	go func() {
		_, _, err := consumer.consumeOnce(context.Background())
		result <- err
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("worker partitions did not start concurrently")
		}
	}
	close(release)
	require.NoError(t, <-result)
	mu.Lock()
	assert.Equal(t, []string{"channel-1-first", "channel-1-second"}, seen[1])
	assert.Equal(t, []string{"channel-2-first", "channel-2-second"}, seen[2])
	mu.Unlock()
}

func TestChannelMonitorRedisConsumerHandlerDeadlineReturnsWhenHandlerIgnoresContext(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("handler-ignores-context"))
	config := channelMonitorRedisConsumerTestConfig()
	config.HandlerTimeout = 10 * time.Millisecond
	started := make(chan struct{})
	release := make(chan struct{})
	consumer := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"handler-hard-deadline",
		func(_ context.Context, _ []model.ChannelMonitorEvent) error {
			close(started)
			<-release
			return nil
		},
		config,
	)

	result := make(chan error, 1)
	go func() {
		_, _, err := consumer.consumeOnce(context.Background())
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	select {
	case err := <-result:
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(time.Second):
		t.Fatal("handler deadline was not enforced")
	}
	close(release)
}
