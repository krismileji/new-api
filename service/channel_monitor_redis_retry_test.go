package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorRedisConsumerRetainsConfigurationConflictsUntilRecovery(t *testing.T) {
	for _, isolationPass := range []bool{false, true} {
		t.Run(fmt.Sprintf("isolation_pass_%t", isolationPass), func(t *testing.T) {
			_, client := useChannelMonitorRedisConsumerTestClient(t)
			blocked := newChannelMonitorRedisConsumerTestEvent("configuration-conflict")
			fresh := newChannelMonitorRedisConsumerTestEvent("unaffected-channel")
			fresh.ChannelId++
			addChannelMonitorRedisConsumerTestEvent(t, client, blocked)
			addChannelMonitorRedisConsumerTestEvent(t, client, fresh)
			config := channelMonitorRedisConsumerTestConfig()
			config.MaxDeliveryAttempts = 2
			config.PendingRetryLimit = 10
			config.WorkerCount = 2
			var recovered atomic.Bool
			var blockedCalls atomic.Int64
			var handledBlocked, handledFresh atomic.Int64
			consumer := newChannelMonitorRedisConsumerForTest(t, client, "configuration-conflict",
				func(_ context.Context, events []model.ChannelMonitorEvent) error {
					if events[0].EventId == fresh.EventId {
						handledFresh.Add(1)
						return nil
					}
					attempt := blockedCalls.Add(1)
					if recovered.Load() {
						handledBlocked.Add(1)
						return nil
					}
					if isolationPass && attempt <= 2 {
						return errors.New("projection failed before configuration changed")
					}
					return fmt.Errorf("configuration changed: %w", ErrChannelMonitorRedisRetryable)
				}, config)

			// Cross the configured isolation boundary without relying on wall time.
			for range 3 {
				_, _, err := consumer.consumeOnce(context.Background())
				require.Error(t, err)
			}
			pending, err := client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
			require.NoError(t, err)
			assert.Equal(t, int64(1), pending.Count)
			quarantined, err := client.XRange(context.Background(), ChannelMonitorRedisDeadLetterStream, "-", "+").Result()
			require.NoError(t, err)
			assert.Empty(t, quarantined)
			assert.Equal(t, int64(1), handledFresh.Load())

			recovered.Store(true)
			_, _, err = consumer.consumeOnce(context.Background())
			require.NoError(t, err)
			pending, err = client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
			require.NoError(t, err)
			assert.Zero(t, pending.Count)
			assert.Equal(t, int64(1), handledBlocked.Load())
			assert.Equal(t, int64(1), handledFresh.Load())
		})
	}
}

func TestChannelMonitorRedisConsumerRetainsTransientFailuresUntilRecovery(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{"deadline", context.DeadlineExceeded},
		{"canceled", context.Canceled},
		{"lease_contention", ErrChannelMonitorRedisEffectProcessing},
		{"lease_lost", ErrChannelMonitorRedisEffectOwnershipLost},
		{"connection_closed", io.EOF},
		{"network", &net.OpError{Op: "read", Net: "tcp", Err: io.ErrUnexpectedEOF}},
		{"redis_pool_timeout", errors.New("redis: connection pool timeout")},
		{"logical_revision", model.ErrChannelLogicalGroupRevisionConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, client := useChannelMonitorRedisConsumerTestClient(t)
			addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent(test.name))
			config := channelMonitorRedisConsumerTestConfig()
			config.MaxDeliveryAttempts = 1
			var recovered atomic.Bool
			consumer := newChannelMonitorRedisConsumerForTest(t, client, test.name,
				func(context.Context, []model.ChannelMonitorEvent) error {
					if recovered.Load() {
						return nil
					}
					return fmt.Errorf("handler dependency: %w", test.err)
				}, config)
			_, _, err := consumer.consumeOnce(context.Background())
			require.ErrorIs(t, err, test.err)
			assert.Zero(t, client.XLen(context.Background(), ChannelMonitorRedisDeadLetterStream).Val())
			pending, err := client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
			require.NoError(t, err)
			assert.Equal(t, int64(1), pending.Count)
			recovered.Store(true)
			_, _, err = consumer.consumeOnce(context.Background())
			require.NoError(t, err)
			pending, err = client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
			require.NoError(t, err)
			assert.Zero(t, pending.Count)
		})
	}
}
