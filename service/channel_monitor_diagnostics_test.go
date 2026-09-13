package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newChannelMonitorDiagnosticsTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	server.SetTime(time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC))
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return server, client
}

func TestChannelMonitorDiagnosticsStartsWithoutImportingLifetimeHistory(t *testing.T) {
	_, client := newChannelMonitorDiagnosticsTestRedis(t)
	ctx := context.Background()
	require.NoError(t, client.HSet(ctx, ChannelMonitorRedisObservabilityKey,
		"retry_count", 131365, "quarantine_count", 2795, "marker_release_failure_count", 162,
	).Err())
	snapshot, err := queryChannelMonitorDiagnostics(ctx, client, 0)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 9, 12, 16, 0, 0, 0, time.UTC).Unix(), snapshot.DayStart)
	assert.Equal(t, snapshot.ObservedAt, snapshot.CountedSince)
	assert.Zero(t, snapshot.RetryCount)
	assert.Zero(t, snapshot.QuarantineCount)
	assert.Zero(t, snapshot.MarkerReleaseFailureCount)
	assert.Equal(t, "131365", client.HGet(ctx, ChannelMonitorRedisObservabilityKey, "retry_count").Val())
}

func TestChannelMonitorDiagnosticsResetPreservesFaultsAndEvents(t *testing.T) {
	_, client := newChannelMonitorDiagnosticsTestRedis(t)
	verifyChannelMonitorDiagnosticsReset(t, client)
}

func verifyChannelMonitorDiagnosticsReset(t *testing.T, client *redis.Client) {
	t.Helper()
	ctx := context.Background()
	initial, err := queryChannelMonitorDiagnostics(ctx, client, 0)
	require.NoError(t, err)
	incrementChannelMonitorRedisObservation(client, ChannelMonitorRedisObservabilityFieldRetryCount, 3)
	incrementChannelMonitorRedisObservation(client, ChannelMonitorRedisObservabilityFieldTakeoverCount, 2)
	incrementChannelMonitorRedisObservation(client, ChannelMonitorRedisObservabilityFieldQuarantineCount, 1)
	recordChannelMonitorRedisFault(client, ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureCount,
		ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureActive, 4)
	recordChannelMonitorRedisFault(client, ChannelMonitorRedisObservabilityFieldStreamTrimFailureCount,
		ChannelMonitorRedisObservabilityFieldStreamTrimFailureActive, 5)
	before, err := queryChannelMonitorDiagnostics(ctx, client, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(3), before.RetryCount)
	assert.Equal(t, int64(2), before.TakeoverCount)
	assert.Equal(t, int64(1), before.QuarantineCount)
	assert.Equal(t, int64(4), before.MarkerReleaseFailureCount)
	assert.Equal(t, int64(5), before.StreamTrimFailureCount)
	assert.Positive(t, before.LastQuarantinedAt)
	// Existing quarantine and pending work must survive a reporting reset.
	require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: ChannelMonitorRedisDeadLetterStream, Values: map[string]interface{}{"event_id": "quarantined"}}).Err())
	require.NoError(t, client.XGroupCreateMkStream(ctx, ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup, "0").Err())
	require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: ChannelMonitorRedisEventStream, Values: map[string]interface{}{"event_id": "pending"}}).Err())
	require.NoError(t, client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: ChannelMonitorRedisConsumerGroup,
		Consumer: "diagnostic-test", Streams: []string{ChannelMonitorRedisEventStream, ">"}, Count: 1, Block: -1}).Err())

	after, err := queryChannelMonitorDiagnostics(ctx, client, initial.DayStart)
	require.NoError(t, err)
	assert.Zero(t, after.RetryCount)
	assert.Zero(t, after.TakeoverCount)
	assert.Zero(t, after.QuarantineCount)
	assert.Zero(t, after.MarkerReleaseFailureCount)
	assert.Zero(t, after.StreamTrimFailureCount)
	assert.Zero(t, after.LastQuarantinedAt)
	assert.Equal(t, after.ObservedAt, after.CountedSince)
	assert.Equal(t, after.ObservedAt, after.LastResetAt)
	assert.Equal(t, "3", client.HGet(ctx, ChannelMonitorRedisObservabilityKey, "retry_count").Val())
	assert.Equal(t, "1", client.HGet(ctx, ChannelMonitorRedisObservabilityKey, "quarantine_count").Val())
	assert.Equal(t, "1", client.HGet(ctx, ChannelMonitorRedisObservabilityKey, "marker_release_failure_active").Val())
	assert.Equal(t, "1", client.HGet(ctx, ChannelMonitorRedisObservabilityKey, "stream_trim_failure_active").Val())
	pending, err := client.XPending(ctx, ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), pending.Count)
	assert.Equal(t, int64(1), client.XLen(ctx, ChannelMonitorRedisDeadLetterStream).Val())
	incrementChannelMonitorRedisObservation(client, ChannelMonitorRedisObservabilityFieldQuarantineCount, 2)
	latest, err := queryChannelMonitorDiagnostics(ctx, client, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(2), latest.QuarantineCount)
	assert.Equal(t, "3", client.HGet(ctx, ChannelMonitorRedisObservabilityKey, "quarantine_count").Val(), "new isolation must remain detectable by health notifications")
}

func TestChannelMonitorDiagnosticsRollsAtBeijingMidnightAndRejectsStaleReset(t *testing.T) {
	server, client := newChannelMonitorDiagnosticsTestRedis(t)
	ctx := context.Background()
	server.SetTime(time.Date(2026, 9, 13, 15, 59, 59, 0, time.UTC))
	incrementChannelMonitorRedisObservation(client, ChannelMonitorRedisObservabilityFieldRetryCount, 5)
	yesterday, err := queryChannelMonitorDiagnostics(ctx, client, 0)
	require.NoError(t, err)
	server.SetTime(time.Date(2026, 9, 13, 16, 0, 0, 0, time.UTC))
	today, err := queryChannelMonitorDiagnostics(ctx, client, 0)
	require.NoError(t, err)
	assert.Equal(t, yesterday.DayStart+86400, today.DayStart)
	assert.Equal(t, today.DayStart, today.CountedSince)
	assert.Zero(t, today.RetryCount)
	incrementChannelMonitorRedisObservation(client, ChannelMonitorRedisObservabilityFieldRetryCount, 2)
	_, err = queryChannelMonitorDiagnostics(ctx, client, yesterday.DayStart)
	require.ErrorIs(t, err, ErrChannelMonitorDiagnosticsDayChanged)
	after, err := queryChannelMonitorDiagnostics(ctx, client, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(2), after.RetryCount)
	assert.Equal(t, "7", client.HGet(ctx, ChannelMonitorRedisObservabilityKey, "retry_count").Val())
	keys, err := client.Keys(ctx, ChannelMonitorRedisKeyPrefix+":diagnostics:*").Result()
	require.NoError(t, err)
	assert.Len(t, keys, 1, "daily diagnostics must not accumulate a new storage key each day")
	server.FastForward(48 * time.Hour)
	assert.False(t, server.Exists(channelMonitorDiagnosticsTodayKey), "inactive daily diagnostics expire independently of database cleanup")
}

func TestChannelMonitorDiagnosticsStorageFailureDoesNotSuppressActiveFault(t *testing.T) {
	_, client := newChannelMonitorDiagnosticsTestRedis(t)
	ctx := context.Background()
	require.NoError(t, client.Set(ctx, channelMonitorDiagnosticsTodayKey, "damaged", 0).Err())
	recordChannelMonitorRedisFault(client, ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureCount,
		ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureActive, 1)
	assert.Equal(t, "1", client.HGet(ctx, ChannelMonitorRedisObservabilityKey, "marker_release_failure_active").Val())
	assert.Equal(t, "1", client.HGet(ctx, ChannelMonitorRedisObservabilityKey, "marker_release_failure_count").Val())
	_, err := queryChannelMonitorDiagnostics(ctx, client, 0)
	require.Error(t, err)
}

func TestChannelMonitorDiagnosticsRedisIntegration(t *testing.T) {
	address := os.Getenv("TEST_MONITOR_DIAGNOSTICS_REDIS_ADDR")
	if address == "" {
		t.Skip("set TEST_MONITOR_DIAGNOSTICS_REDIS_ADDR to a dedicated disposable Redis instance")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	ctx := context.Background()
	keys := []string{ChannelMonitorRedisObservabilityKey, channelMonitorDiagnosticsTodayKey, ChannelMonitorRedisEventStream, ChannelMonitorRedisDeadLetterStream}
	require.NoError(t, client.Del(ctx, keys...).Err())
	t.Cleanup(func() { require.NoError(t, client.Del(ctx, keys...).Err()) })
	verifyChannelMonitorDiagnosticsReset(t, client)
	ttl, err := client.TTL(ctx, channelMonitorDiagnosticsTodayKey).Result()
	require.NoError(t, err)
	assert.Positive(t, ttl)
	assert.LessOrEqual(t, ttl, 48*time.Hour)
}
