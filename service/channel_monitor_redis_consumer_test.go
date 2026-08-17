package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newChannelMonitorRedisConsumerTestEvent(eventID string) model.ChannelMonitorEvent {
	return model.ChannelMonitorEvent{
		EventId:       eventID,
		SchemaVersion: model.ChannelMonitorEventSchemaVersion,
		OccurredAt:    1_750_000_000,
		CreatedAt:     1_750_000_001,
		ChannelId:     7,
		Source:        model.ChannelMonitorEventSourceBusiness,
		Outcome:       model.ChannelMonitorEventOutcomeSuccess,
		CostStatus:    model.ChannelMonitorEventCostNone,
	}
}

func useChannelMonitorRedisConsumerTestClient(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.XGroupCreateMkStream(
		context.Background(),
		ChannelMonitorRedisEventStream,
		ChannelMonitorRedisConsumerGroup,
		"0",
	).Err())
	return server, client
}

func channelMonitorRedisConsumerTestConfig() channelMonitorRedisConsumerConfig {
	return channelMonitorRedisConsumerConfig{
		BatchSize:        10,
		Block:            -1,
		ClaimMinIdle:     time.Second,
		LeaseTTL:         time.Minute,
		LeaseHeartbeat:   10 * time.Second,
		OperationTimeout: time.Second,
		RetryDelay:       time.Millisecond,
		DedupTTL:         channelMonitorRedisReplayProtectionTTL,
	}
}

func addChannelMonitorRedisConsumerTestEvent(
	t *testing.T,
	client *redis.Client,
	event model.ChannelMonitorEvent,
) string {
	t.Helper()
	payload, err := event.Marshal()
	require.NoError(t, err)
	messageID, err := client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: ChannelMonitorRedisEventStream,
		Values: map[string]interface{}{
			ChannelMonitorRedisEventFieldEventID: event.EventId,
			ChannelMonitorRedisEventFieldPayload: string(payload),
		},
	}).Result()
	require.NoError(t, err)
	return messageID
}

func addChannelMonitorRedisConsumerTestEventWithID(
	t *testing.T,
	client *redis.Client,
	messageID string,
	event model.ChannelMonitorEvent,
) string {
	t.Helper()
	payload, err := event.Marshal()
	require.NoError(t, err)
	addedID, err := client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: ChannelMonitorRedisEventStream,
		ID:     messageID,
		Values: map[string]interface{}{
			ChannelMonitorRedisEventFieldEventID: event.EventId,
			ChannelMonitorRedisEventFieldPayload: string(payload),
		},
	}).Result()
	require.NoError(t, err)
	return addedID
}

func newChannelMonitorRedisConsumerForTest(
	t *testing.T,
	client *redis.Client,
	identity string,
	handle ChannelMonitorRedisEventHandlerFunc,
	config channelMonitorRedisConsumerConfig,
) *ChannelMonitorRedisEventConsumer {
	t.Helper()
	consumer, err := newChannelMonitorRedisEventConsumer(
		client,
		ChannelMonitorRedisConsumerName(identity),
		handle,
		config,
	)
	require.NoError(t, err)
	return consumer
}

func TestChannelMonitorRedisConsumerLeavesPendingWhenProcessingStops(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("event-stop"))
	consumer := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"stopped",
		func(context.Context, []model.ChannelMonitorEvent) error { return context.Canceled },
		channelMonitorRedisConsumerTestConfig(),
	)

	processed, acquired, err := consumer.consumeOnce(context.Background())
	assert.ErrorIs(t, err, context.Canceled)
	assert.True(t, acquired)
	assert.Zero(t, processed)
	pending, pendingErr := client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, pendingErr)
	assert.Equal(t, int64(1), pending.Count)
	exists, existsErr := client.Exists(context.Background(), ChannelMonitorRedisProjectionDedupKey("event-stop")).Result()
	require.NoError(t, existsErr)
	assert.Zero(t, exists)
}

func TestParseChannelMonitorRedisAutoClaimMessagesAcceptsRedisSixAndSevenReplies(t *testing.T) {
	message := []interface{}{
		"1-0",
		[]interface{}{
			ChannelMonitorRedisEventFieldEventID, "event-versioned",
			ChannelMonitorRedisEventFieldPayload, "payload",
		},
	}
	for _, reply := range []interface{}{
		[]interface{}{"0-0", []interface{}{message}},
		[]interface{}{"0-0", []interface{}{message}, []interface{}{}},
	} {
		messages, err := parseChannelMonitorRedisAutoClaimMessages(reply)
		require.NoError(t, err)
		require.Len(t, messages, 1)
		assert.Equal(t, "1-0", messages[0].ID)
		assert.Equal(t, "event-versioned", messages[0].Values[ChannelMonitorRedisEventFieldEventID])
	}
}

func TestChannelMonitorRedisConsumerAutoClaimsFromStoppedConsumer(t *testing.T) {
	server, client := useChannelMonitorRedisConsumerTestClient(t)
	baseTime := time.Unix(1_750_000_000, 0)
	server.SetTime(baseTime)
	addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("event-takeover"))
	first := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"first",
		func(context.Context, []model.ChannelMonitorEvent) error { return errors.New("处理中止") },
		channelMonitorRedisConsumerTestConfig(),
	)
	_, _, err := first.consumeOnce(context.Background())
	require.Error(t, err)
	server.SetTime(baseTime.Add(2 * time.Second))

	var handled atomic.Int64
	second := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"second",
		func(_ context.Context, events []model.ChannelMonitorEvent) error {
			handled.Add(int64(len(events)))
			return nil
		},
		channelMonitorRedisConsumerTestConfig(),
	)
	processed, acquired, err := second.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 1, processed)
	assert.Equal(t, int64(1), handled.Load())
	pending, pendingErr := client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, pendingErr)
	assert.Zero(t, pending.Count)
	takeoverCount, takeoverErr := client.HGet(context.Background(), ChannelMonitorRedisObservabilityKey, ChannelMonitorRedisObservabilityFieldTakeoverCount).Int64()
	require.NoError(t, takeoverErr)
	assert.Equal(t, int64(1), takeoverCount)
	processedAt, processedErr := client.HGet(context.Background(), ChannelMonitorRedisObservabilityKey, ChannelMonitorRedisObservabilityFieldLastProcessedAt).Int64()
	require.NoError(t, processedErr)
	assert.NotZero(t, processedAt)
}

func TestChannelMonitorRedisConsumerDoesNotAckFailedHandler(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("event-failed"))
	handlerErr := errors.New("projection failed")
	consumer := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"failed",
		func(context.Context, []model.ChannelMonitorEvent) error { return handlerErr },
		channelMonitorRedisConsumerTestConfig(),
	)

	_, acquired, err := consumer.consumeOnce(context.Background())
	assert.ErrorIs(t, err, handlerErr)
	assert.True(t, acquired)
	pending, pendingErr := client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, pendingErr)
	assert.Equal(t, int64(1), pending.Count)
	retryCount, retryErr := client.HGet(context.Background(), ChannelMonitorRedisObservabilityKey, ChannelMonitorRedisObservabilityFieldRetryCount).Int64()
	require.NoError(t, retryErr)
	assert.Equal(t, int64(1), retryCount)
	exists, existsErr := client.Exists(context.Background(), ChannelMonitorRedisProjectionDedupKey("event-failed")).Result()
	require.NoError(t, existsErr)
	assert.Zero(t, exists)
}

func TestChannelMonitorRedisConsumerQuarantinesMalformedMessageWithoutBlockingValidEvent(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	_, err := client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: ChannelMonitorRedisEventStream,
		Values: map[string]interface{}{
			ChannelMonitorRedisEventFieldEventID: "event-malformed",
			ChannelMonitorRedisEventFieldPayload: "{",
		},
	}).Result()
	require.NoError(t, err)
	addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("event-valid"))
	var handled []string
	consumer := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"malformed-isolation",
		func(_ context.Context, events []model.ChannelMonitorEvent) error {
			for _, event := range events {
				handled = append(handled, event.EventId)
			}
			return nil
		},
		channelMonitorRedisConsumerTestConfig(),
	)

	processed, acquired, err := consumer.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 2, processed)
	assert.Equal(t, []string{"event-valid"}, handled)
	pending, pendingErr := client.XPending(
		context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup,
	).Result()
	require.NoError(t, pendingErr)
	assert.Zero(t, pending.Count)
	quarantined, quarantineErr := client.XRange(
		context.Background(), ChannelMonitorRedisDeadLetterStream, "-", "+",
	).Result()
	require.NoError(t, quarantineErr)
	require.Len(t, quarantined, 1)
	assert.Equal(t, "event-malformed", quarantined[0].Values[ChannelMonitorRedisEventFieldEventID])
	assert.Equal(t, "1", client.HGet(
		context.Background(),
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldQuarantineCount,
	).Val())
	assert.NotEmpty(t, client.HGet(
		context.Background(),
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldLastQuarantinedAt,
	).Val())
}

func TestChannelMonitorRedisConsumerIsolatesHandlerPoisonAfterBoundedRetries(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("event-poison"))
	addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("event-valid-after-poison"))
	config := channelMonitorRedisConsumerTestConfig()
	config.MaxDeliveryAttempts = 2
	var handled []string
	handlerErr := errors.New("poison projection")
	consumer := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"handler-isolation",
		func(_ context.Context, events []model.ChannelMonitorEvent) error {
			if len(events) > 1 || events[0].EventId == "event-poison" {
				return handlerErr
			}
			handled = append(handled, events[0].EventId)
			return nil
		},
		config,
	)

	_, _, err := consumer.consumeOnce(context.Background())
	assert.ErrorIs(t, err, handlerErr)
	processed, acquired, err := consumer.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 2, processed)
	assert.Equal(t, []string{"event-valid-after-poison"}, handled)
	pending, pendingErr := client.XPending(
		context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup,
	).Result()
	require.NoError(t, pendingErr)
	assert.Zero(t, pending.Count)
	quarantined, quarantineErr := client.XRange(
		context.Background(), ChannelMonitorRedisDeadLetterStream, "-", "+",
	).Result()
	require.NoError(t, quarantineErr)
	require.Len(t, quarantined, 1)
	assert.Equal(t, "event-poison", quarantined[0].Values[ChannelMonitorRedisEventFieldEventID])
	assert.Equal(t, int64(1), client.Exists(
		context.Background(), ChannelMonitorRedisProjectionDedupKey("event-valid-after-poison"),
	).Val())
}

func TestChannelMonitorRedisConsumerAppliesDuplicateEventOnce(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	event := newChannelMonitorRedisConsumerTestEvent("event-duplicate")
	addChannelMonitorRedisConsumerTestEvent(t, client, event)
	addChannelMonitorRedisConsumerTestEvent(t, client, event)
	var calls atomic.Int64
	var handled atomic.Int64
	consumer := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"dedup",
		func(_ context.Context, events []model.ChannelMonitorEvent) error {
			calls.Add(1)
			handled.Add(int64(len(events)))
			return nil
		},
		channelMonitorRedisConsumerTestConfig(),
	)

	processed, acquired, err := consumer.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 2, processed)
	assert.Equal(t, int64(1), calls.Load())
	assert.Equal(t, int64(1), handled.Load())
	dedupTTL, ttlErr := client.PTTL(context.Background(), ChannelMonitorRedisProjectionDedupKey(event.EventId)).Result()
	require.NoError(t, ttlErr)
	assert.Positive(t, dedupTTL)
	assert.LessOrEqual(t, dedupTTL, channelMonitorRedisReplayProtectionTTL)
	assert.Equal(t, channelMonitorRedisRuntimeEffectTTL, channelMonitorRedisDedupTTL)
	assert.Equal(t, channelMonitorRedisSchedulingDedupTTL, channelMonitorRedisDedupTTL)

	addChannelMonitorRedisConsumerTestEvent(t, client, event)
	processed, acquired, err = consumer.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 1, processed)
	assert.Equal(t, int64(1), calls.Load())
	assert.Equal(t, int64(1), handled.Load())
	pending, pendingErr := client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, pendingErr)
	assert.Zero(t, pending.Count)
}

func TestChannelMonitorRedisConsumerDedupTTLExpiresAfterMessagesAreConfirmed(t *testing.T) {
	server, client := useChannelMonitorRedisConsumerTestClient(t)
	first := newChannelMonitorRedisConsumerTestEvent("event-ttl-first")
	second := newChannelMonitorRedisConsumerTestEvent("event-ttl-second")
	addChannelMonitorRedisConsumerTestEvent(t, client, first)
	addChannelMonitorRedisConsumerTestEvent(t, client, second)
	var handled atomic.Int64
	config := channelMonitorRedisConsumerTestConfig()
	consumer := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"dedup-ttl",
		func(_ context.Context, events []model.ChannelMonitorEvent) error {
			handled.Add(int64(len(events)))
			return nil
		},
		config,
	)

	processed, acquired, err := consumer.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 2, processed)
	assert.Equal(t, int64(2), handled.Load())
	pending, pendingErr := client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, pendingErr)
	assert.Zero(t, pending.Count)
	messagesBeforeExpiry, rangeErr := client.XRange(context.Background(), ChannelMonitorRedisEventStream, "-", "+").Result()
	require.NoError(t, rangeErr)
	require.Len(t, messagesBeforeExpiry, 1, "safe MINID trim keeps the current delivered watermark")

	server.FastForward(config.DedupTTL - time.Second)
	for _, eventID := range []string{first.EventId, second.EventId} {
		exists, existsErr := client.Exists(context.Background(), ChannelMonitorRedisProjectionDedupKey(eventID)).Result()
		require.NoError(t, existsErr)
		assert.Equal(t, int64(1), exists)
	}
	server.FastForward(2 * time.Second)
	for _, eventID := range []string{first.EventId, second.EventId} {
		exists, existsErr := client.Exists(context.Background(), ChannelMonitorRedisProjectionDedupKey(eventID)).Result()
		require.NoError(t, existsErr)
		assert.Zero(t, exists)
	}
	processed, acquired, err = consumer.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Zero(t, processed)
	assert.Equal(t, int64(2), handled.Load())
	pending, pendingErr = client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, pendingErr)
	assert.Zero(t, pending.Count)
}

func TestChannelMonitorRedisConsumerLeasePreventsConcurrentSideEffects(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("event-lease"))
	started := make(chan struct{})
	release := make(chan struct{})
	first := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"lease-a",
		func(context.Context, []model.ChannelMonitorEvent) error {
			close(started)
			<-release
			return nil
		},
		channelMonitorRedisConsumerTestConfig(),
	)
	secondCalls := atomic.Int64{}
	second := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"lease-b",
		func(context.Context, []model.ChannelMonitorEvent) error {
			secondCalls.Add(1)
			return nil
		},
		channelMonitorRedisConsumerTestConfig(),
	)

	firstResult := make(chan error, 1)
	go func() {
		_, _, err := first.consumeOnce(context.Background())
		firstResult <- err
	}()
	<-started
	processed, acquired, err := second.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Zero(t, processed)
	assert.Zero(t, secondCalls.Load())
	close(release)
	require.NoError(t, <-firstResult)
}

func TestChannelMonitorRedisConsumerTrimKeepsOldestPendingMessage(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	oldMessageID := addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("event-pending"))
	streams, err := client.XReadGroup(context.Background(), &redis.XReadGroupArgs{
		Group:    ChannelMonitorRedisConsumerGroup,
		Consumer: ChannelMonitorRedisConsumerName("stopped-before-trim"),
		Streams:  []string{ChannelMonitorRedisEventStream, ">"},
		Count:    1,
		Block:    -1,
	}).Result()
	require.NoError(t, err)
	require.Len(t, streams, 1)
	require.Len(t, streams[0].Messages, 1)
	assert.Equal(t, oldMessageID, streams[0].Messages[0].ID)
	addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("event-new"))
	config := channelMonitorRedisConsumerTestConfig()
	config.ClaimMinIdle = time.Hour
	consumer := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"trim",
		func(context.Context, []model.ChannelMonitorEvent) error { return nil },
		config,
	)

	processed, acquired, err := consumer.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Zero(t, processed)
	messages, rangeErr := client.XRange(context.Background(), ChannelMonitorRedisEventStream, "-", "+").Result()
	require.NoError(t, rangeErr)
	require.NotEmpty(t, messages)
	assert.Equal(t, oldMessageID, messages[0].ID)
	pending, pendingErr := client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, pendingErr)
	assert.Equal(t, int64(1), pending.Count)
	assert.Equal(t, oldMessageID, pending.Lower)
}

func TestChannelMonitorRedisEventSequenceFromStreamIDIsStrictlyOrdered(t *testing.T) {
	first, err := channelMonitorRedisEventSequenceFromStreamID("1750000000000-0")
	require.NoError(t, err)
	second, err := channelMonitorRedisEventSequenceFromStreamID("1750000000000-1")
	require.NoError(t, err)
	nextMillisecond, err := channelMonitorRedisEventSequenceFromStreamID("1750000000001-0")
	require.NoError(t, err)

	assert.Less(t, first, second)
	assert.Less(t, second, nextMillisecond)
}

func TestChannelMonitorRedisEventSequenceFromStreamIDSupportsFullEncodingRange(t *testing.T) {
	maximumMilliseconds := uint64(math.MaxInt64) >> channelMonitorRedisStreamSequenceBits
	maximumID := fmt.Sprintf(
		"%d-%d",
		maximumMilliseconds,
		channelMonitorRedisStreamSequenceLimit,
	)
	sequence, err := channelMonitorRedisEventSequenceFromStreamID(maximumID)
	require.NoError(t, err)
	assert.Equal(t, uint64(math.MaxInt64), sequence)

	invalidIDs := []string{
		fmt.Sprintf("%d-0", maximumMilliseconds+1),
		fmt.Sprintf("%d-%d", maximumMilliseconds, channelMonitorRedisStreamSequenceLimit+1),
	}
	for _, invalidID := range invalidIDs {
		_, err := channelMonitorRedisEventSequenceFromStreamID(invalidID)
		assert.ErrorContains(t, err, "超出事件顺序编码范围")
	}
}

func TestChannelMonitorRedisConsumerOverridesPayloadEventSequenceWithStreamID(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	event := newChannelMonitorRedisConsumerTestEvent("event-explicit-sequence")
	event.EventSequence = 77
	messageID := addChannelMonitorRedisConsumerTestEvent(t, client, event)
	expected, err := channelMonitorRedisEventSequenceFromStreamID(messageID)
	require.NoError(t, err)
	var observed uint64
	consumer := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"explicit-sequence",
		func(_ context.Context, events []model.ChannelMonitorEvent) error {
			require.Len(t, events, 1)
			observed = events[0].EventSequence
			return nil
		},
		channelMonitorRedisConsumerTestConfig(),
	)

	processed, acquired, err := consumer.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 1, processed)
	assert.Equal(t, expected, observed)
	assert.NotEqual(t, uint64(77), observed)
}

func TestChannelMonitorRedisConsumerQuarantinesOutOfRangeSequence(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	event := newChannelMonitorRedisConsumerTestEvent("event-sequence-overflow")
	milliseconds := (uint64(math.MaxInt64) >> channelMonitorRedisStreamSequenceBits) + 1
	addChannelMonitorRedisConsumerTestEventWithID(
		t,
		client,
		fmt.Sprintf("%d-0", milliseconds),
		event,
	)
	var calls atomic.Int64
	consumer := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"sequence-overflow",
		func(context.Context, []model.ChannelMonitorEvent) error {
			calls.Add(1)
			return nil
		},
		channelMonitorRedisConsumerTestConfig(),
	)

	processed, acquired, err := consumer.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 1, processed)
	assert.Zero(t, calls.Load())
	pending, pendingErr := client.XPending(
		context.Background(),
		ChannelMonitorRedisEventStream,
		ChannelMonitorRedisConsumerGroup,
	).Result()
	require.NoError(t, pendingErr)
	assert.Zero(t, pending.Count)
	quarantined, quarantineErr := client.XRange(
		context.Background(), ChannelMonitorRedisDeadLetterStream, "-", "+",
	).Result()
	require.NoError(t, quarantineErr)
	require.Len(t, quarantined, 1)
	assert.Contains(t, quarantined[0].Values["error"], "超出事件顺序编码范围")
}

type channelMonitorRedisCommandErrorHook struct {
	command  string
	err      error
	failures int64
	calls    atomic.Int64
}

func (hook *channelMonitorRedisCommandErrorHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	if cmd.Name() != hook.command {
		return ctx, nil
	}
	if hook.calls.Add(1) <= hook.failures {
		return ctx, hook.err
	}
	return ctx, nil
}

func (hook *channelMonitorRedisCommandErrorHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (hook *channelMonitorRedisCommandErrorHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (hook *channelMonitorRedisCommandErrorHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func TestChannelMonitorRedisConsumerTrimFailureDoesNotFailAcknowledgedBatchAndRecovers(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("event-trim-failure"))
	hook := &channelMonitorRedisCommandErrorHook{
		command:  "xtrim",
		err:      errors.New("xtrim failed"),
		failures: 1,
	}
	client.AddHook(hook)
	consumer := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"trim-failure",
		func(context.Context, []model.ChannelMonitorEvent) error { return nil },
		channelMonitorRedisConsumerTestConfig(),
	)

	processed, acquired, err := consumer.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 1, processed)
	assert.Equal(t, int64(1), hook.calls.Load())
	assert.Equal(t, "1", client.HGet(
		context.Background(),
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldStreamTrimFailureCount,
	).Val())
	assert.Equal(t, "1", client.HGet(
		context.Background(),
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldStreamTrimFailureActive,
	).Val())
	pending, pendingErr := client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, pendingErr)
	assert.Zero(t, pending.Count)

	addChannelMonitorRedisConsumerTestEvent(t, client, newChannelMonitorRedisConsumerTestEvent("event-trim-recovery"))
	processed, acquired, err = consumer.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 1, processed)
	assert.Equal(t, int64(2), hook.calls.Load())
	assert.Equal(t, "1", client.HGet(
		context.Background(),
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldStreamTrimFailureCount,
	).Val())
	assert.Equal(t, "0", client.HGet(
		context.Background(),
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldStreamTrimFailureActive,
	).Val())
}
