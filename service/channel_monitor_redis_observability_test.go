package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorRedisRealtimeStatusReportsPendingAndRecovers(t *testing.T) {
	useChannelMonitorEventPublishStatsIsolation(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	require.NoError(t, client.XGroupCreateMkStream(
		ctx,
		ChannelMonitorRedisEventStream,
		ChannelMonitorRedisConsumerGroup,
		"0",
	).Err())
	addObservabilityTestEvent(t, client, "1750000000000-0", "status-first")
	addObservabilityTestEvent(t, client, "1750000060000-0", "status-second")
	initialMessages, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    ChannelMonitorRedisConsumerGroup,
		Consumer: ChannelMonitorRedisConsumerName("status-consumer"),
		Streams:  []string{ChannelMonitorRedisEventStream, ">"},
		Count:    1,
		Block:    -1,
	}).Result()
	require.NoError(t, err)
	require.Len(t, initialMessages, 1)
	require.Len(t, initialMessages[0].Messages, 1)
	require.NoError(t, client.Set(ctx, ChannelMonitorRedisConsumerHeartbeatKey, "status-consumer", time.Minute).Err())
	require.NoError(t, client.HSet(
		ctx,
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldLastProcessedAt,
		1750000050,
		ChannelMonitorRedisObservabilityFieldRetryCount,
		3,
		ChannelMonitorRedisObservabilityFieldTakeoverCount,
		2,
	).Err())

	status := getChannelMonitorRedisRealtimeStatus(
		ctx,
		client,
		time.Unix(1750000120, 0),
	)
	assert.Equal(t, ChannelMonitorRedisStatusAvailable, status.RedisStatus)
	assert.True(t, status.RedisAvailable)
	assert.True(t, status.RedisConsumerRunning)
	assert.Equal(t, int64(1), status.PendingCount)
	assert.Equal(t, int64(1750000000), status.OldestPendingAt)
	assert.Equal(t, int64(120), status.ConsumerLagSeconds)
	assert.Equal(t, int64(1750000060), status.LastPublishedAt)
	assert.Equal(t, int64(1750000050), status.LastProcessedAt)
	assert.Equal(t, int64(3), status.RetryCount)
	assert.Equal(t, int64(2), status.TakeoverCount)
	assert.Equal(t, []string{ChannelMonitorRedisDegradedReasonEventBacklog}, status.DegradedReasons)
	assert.True(t, status.RealtimeDegraded)

	require.NoError(t, client.XAck(
		ctx,
		ChannelMonitorRedisEventStream,
		ChannelMonitorRedisConsumerGroup,
		initialMessages[0].Messages[0].ID,
	).Err())
	undelivered := getChannelMonitorRedisRealtimeStatus(ctx, client, time.Unix(1750000120, 0))
	assert.Zero(t, undelivered.PendingCount)
	assert.Equal(t, int64(1750000060), undelivered.OldestPendingAt)
	assert.Equal(t, int64(60), undelivered.ConsumerLagSeconds)
	assert.Equal(t, []string{ChannelMonitorRedisDegradedReasonEventBacklog}, undelivered.DegradedReasons)
	assert.True(t, undelivered.RealtimeDegraded)
	messages, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    ChannelMonitorRedisConsumerGroup,
		Consumer: ChannelMonitorRedisConsumerName("status-consumer"),
		Streams:  []string{ChannelMonitorRedisEventStream, ">"},
		Count:    1,
		Block:    -1,
	}).Result()
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Len(t, messages[0].Messages, 1)
	require.NoError(t, client.XAck(
		ctx,
		ChannelMonitorRedisEventStream,
		ChannelMonitorRedisConsumerGroup,
		messages[0].Messages[0].ID,
	).Err())
	require.NoError(t, client.XAck(
		ctx,
		ChannelMonitorRedisEventStream,
		ChannelMonitorRedisConsumerGroup,
		initialMessages[0].Messages[0].ID,
	).Err())

	recovered := getChannelMonitorRedisRealtimeStatus(ctx, client, time.Unix(1750000120, 0))
	assert.Equal(t, int64(0), recovered.PendingCount)
	assert.Equal(t, int64(0), recovered.OldestPendingAt)
	assert.Equal(t, int64(0), recovered.ConsumerLagSeconds)
	assert.Empty(t, recovered.DegradedReasons)
	assert.False(t, recovered.RealtimeDegraded)

	require.NoError(t, client.Del(ctx, ChannelMonitorRedisConsumerHeartbeatKey).Err())
	stopped := getChannelMonitorRedisRealtimeStatus(ctx, client, time.Unix(1750000120, 0))
	assert.Equal(t, []string{ChannelMonitorRedisDegradedReasonConsumerStopped}, stopped.DegradedReasons)
	assert.True(t, stopped.RealtimeDegraded)

	server.Close()
	failed := getChannelMonitorRedisRealtimeStatus(ctx, client, time.Unix(1750000120, 0))
	assert.Equal(t, ChannelMonitorRedisStatusUnavailable, failed.RedisStatus)
	assert.False(t, failed.RedisAvailable)
	assert.Equal(t, []string{ChannelMonitorRedisDegradedReasonRedisUnavailable}, failed.DegradedReasons)
	assert.True(t, failed.RealtimeDegraded)
}

func TestChannelMonitorRedisRealtimeStatusReportsEveryStartupFailure(t *testing.T) {
	useChannelMonitorEventPublishStatsIsolation(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: ChannelMonitorRedisEventStream,
		Values: map[string]interface{}{"event": "startup"},
	}).Err())

	status := getChannelMonitorRedisRealtimeStatus(context.Background(), client, time.Now())

	assert.Equal(t, []string{
		ChannelMonitorRedisDegradedReasonConsumerStopped,
		ChannelMonitorRedisDegradedReasonConsumerGroupMissing,
	}, status.DegradedReasons)
	assert.True(t, status.RealtimeDegraded)
}

type failingChannelMonitorEventAppender struct{}

func (failingChannelMonitorEventAppender) XAdd(ctx context.Context, _ *redis.XAddArgs) *redis.StringCmd {
	cmd := redis.NewStringCmd(ctx, "XADD")
	cmd.SetErr(errors.New("redis xadd failed"))
	return cmd
}

func TestChannelMonitorRedisRealtimeStatusKeepsPublisherFailureDegradedUntilPublishSucceeds(t *testing.T) {
	useChannelMonitorEventPublishStatsIsolation(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	require.NoError(t, client.XGroupCreateMkStream(
		ctx,
		ChannelMonitorRedisEventStream,
		ChannelMonitorRedisConsumerGroup,
		"0",
	).Err())
	require.NoError(t, client.Set(
		ctx,
		ChannelMonitorRedisConsumerHeartbeatKey,
		"publisher-status-consumer",
		time.Minute,
	).Err())

	status, err := publishChannelMonitorEvent(
		ctx,
		failingChannelMonitorEventAppender{},
		newChannelMonitorPublisherTestEvent("failed-publish"),
	)
	require.Error(t, err)
	assert.Equal(t, ChannelMonitorEventPublishStatusUnavailable, status)
	degraded := getChannelMonitorRedisRealtimeStatus(ctx, client, time.Now())
	assert.Equal(t, []string{ChannelMonitorRedisDegradedReasonPublisherUnavailable}, degraded.DegradedReasons)
	assert.True(t, degraded.RealtimeDegraded)
	assert.True(t, getChannelMonitorRedisRealtimeStatus(ctx, client, time.Now()).RealtimeDegraded)

	status, err = publishChannelMonitorEvent(
		ctx,
		client,
		newChannelMonitorPublisherTestEvent("recovered-publish"),
	)
	require.NoError(t, err)
	assert.Equal(t, ChannelMonitorEventPublishStatusPublished, status)
	messages, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    ChannelMonitorRedisConsumerGroup,
		Consumer: "publisher-status-consumer",
		Streams:  []string{ChannelMonitorRedisEventStream, ">"},
		Count:    1,
		Block:    -1,
	}).Result()
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Len(t, messages[0].Messages, 1)
	require.NoError(t, client.XAck(
		ctx,
		ChannelMonitorRedisEventStream,
		ChannelMonitorRedisConsumerGroup,
		messages[0].Messages[0].ID,
	).Err())

	recovered := getChannelMonitorRedisRealtimeStatus(ctx, client, time.Now())
	assert.Empty(t, recovered.DegradedReasons)
	assert.False(t, recovered.RealtimeDegraded)
}

func TestChannelMonitorRedisRealtimeStatusReportsOperationalFaultsUntilRecovery(t *testing.T) {
	useChannelMonitorEventPublishStatsIsolation(t)
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	ctx := context.Background()
	require.NoError(t, client.Set(
		ctx,
		ChannelMonitorRedisConsumerHeartbeatKey,
		"operational-fault-consumer",
		time.Minute,
	).Err())
	require.NoError(t, client.HSet(
		ctx,
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldRuntimeMarkerFailureCount,
		4,
		ChannelMonitorRedisObservabilityFieldScheduleMarkerFailureCount,
		5,
		ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureCount,
		2,
		ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureActive,
		1,
		ChannelMonitorRedisObservabilityFieldStreamTrimFailureCount,
		3,
		ChannelMonitorRedisObservabilityFieldStreamTrimFailureActive,
		1,
	).Err())

	failed := getChannelMonitorRedisRealtimeStatus(ctx, client, time.Now())
	assert.Equal(t, int64(4), failed.RuntimeMarkerFailureCount)
	assert.Equal(t, int64(5), failed.ScheduleMarkerFailureCount)
	assert.Equal(t, int64(2), failed.MarkerReleaseFailureCount)
	assert.True(t, failed.MarkerReleaseFailureActive)
	assert.Equal(t, int64(3), failed.StreamTrimFailureCount)
	assert.True(t, failed.StreamTrimFailureActive)
	assert.Equal(t, []string{
		ChannelMonitorRedisDegradedReasonMarkerReleaseFailure,
		ChannelMonitorRedisDegradedReasonStreamTrimFailure,
	}, failed.DegradedReasons)
	assert.True(t, failed.RealtimeDegraded)

	require.NoError(t, client.HSet(
		ctx,
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureActive,
		0,
		ChannelMonitorRedisObservabilityFieldStreamTrimFailureActive,
		0,
	).Err())
	recovered := getChannelMonitorRedisRealtimeStatus(ctx, client, time.Now())
	assert.Equal(t, int64(4), recovered.RuntimeMarkerFailureCount)
	assert.Equal(t, int64(5), recovered.ScheduleMarkerFailureCount)
	assert.Equal(t, int64(2), recovered.MarkerReleaseFailureCount)
	assert.False(t, recovered.MarkerReleaseFailureActive)
	assert.Equal(t, int64(3), recovered.StreamTrimFailureCount)
	assert.False(t, recovered.StreamTrimFailureActive)
	assert.Empty(t, recovered.DegradedReasons)
	assert.False(t, recovered.RealtimeDegraded)
}

func addObservabilityTestEvent(t *testing.T, client *redis.Client, id, eventID string) {
	t.Helper()
	event := newChannelMonitorRedisConsumerTestEvent(eventID)
	payload, err := event.Marshal()
	require.NoError(t, err)
	_, err = client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: ChannelMonitorRedisEventStream,
		ID:     id,
		Values: map[string]interface{}{
			ChannelMonitorRedisEventFieldEventID: eventID,
			ChannelMonitorRedisEventFieldPayload: string(payload),
		},
	}).Result()
	require.NoError(t, err)
}
