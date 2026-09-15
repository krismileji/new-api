package common

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisCommandHealthRequiresSameClientSuccessAfterQuietWindow(t *testing.T) {
	for _, tt := range []struct {
		name   string
		err    error
		reason string
	}{
		{"deadline", context.DeadlineExceeded, RedisClientPoolDegradedReasonContextDeadline},
		{"pool timeout", errors.New("redis: connection pool timeout"), RedisClientPoolDegradedReasonPoolTimeout},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Unix(100, 0)
			var affected, other redisCommandHealth
			affected.observe(tt.err, now)
			require.Equal(t, tt.reason, affected.degradedReason())
			affected.observe(nil, now.Add(29*time.Second))
			assert.Equal(t, tt.reason, affected.degradedReason())
			other.observe(nil, now.Add(time.Hour))
			assert.Equal(t, tt.reason, affected.degradedReason(), "another client and elapsed time cannot confirm recovery")
			affected.observe(tt.err, now.Add(30*time.Second))
			affected.observe(nil, now.Add(59*time.Second))
			assert.Equal(t, tt.reason, affected.degradedReason(), "a new failure restarts the quiet window")
			affected.observe(redis.Nil, now.Add(60*time.Second))
			assert.Empty(t, affected.degradedReason(), "a valid missing-key response proves a successful Redis operation")
			assert.Empty(t, affected.degradedReason(), "an idle recovered client must not become degraded again")
		})
	}
}

func TestRedisCommandHealthDoesNotRecoverDuringOtherFailures(t *testing.T) {
	var health redisCommandHealth
	now := time.Unix(100, 0)
	health.observe(context.DeadlineExceeded, now)
	health.observe(errors.New("connection refused"), now.Add(time.Minute))
	health.observe(nil, now.Add(61*time.Second))
	assert.Equal(t, RedisClientPoolDegradedReasonContextDeadline, health.degradedReason())
	health.observe(nil, now.Add(90*time.Second))
	assert.Empty(t, health.degradedReason())
}

func TestRedisPoolRecoveryPreservesCumulativeCounters(t *testing.T) {
	server := miniredis.RunT(t)
	metrics := &redisClientCommandMetrics{}
	client := newRedisClientWithMetrics(&redis.Options{Addr: server.Addr()}, metrics)
	t.Cleanup(func() { _ = client.Close() })
	metrics.recordError(context.DeadlineExceeded)
	metrics.recordError(errors.New("redis: connection pool timeout"))
	failed := redisClientPoolStats(RedisClientRoleMonitorWrite, client, 4, metrics)
	require.Equal(t, RedisClientPoolDegradedReasonPoolTimeout, failed.DegradedReason)
	metrics.health.observe(nil, time.Now().Add(time.Minute))
	recovered := redisClientPoolStats(RedisClientRoleMonitorWrite, client, 4, metrics)
	assert.Empty(t, recovered.DegradedReason)
	assert.Equal(t, uint64(1), recovered.ContextDeadlineCount)
	assert.Equal(t, uint64(1), recovered.PoolTimeoutCount)
	assert.Equal(t, failed.CommandErrorCount, recovered.CommandErrorCount)
}

func TestRedisCommandHealthExistingGroupDoesNotExtendFailure(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	ctx := context.Background()
	require.NoError(t, client.XGroupCreateMkStream(ctx, "events", "consumer", "0").Err())
	busyGroup := client.XGroupCreateMkStream(ctx, "events", "consumer", "0").Err()
	require.ErrorContains(t, busyGroup, "BUSYGROUP ")
	permissionDenied := client.Eval(ctx, "return redis.error_reply('NOPERM command is not allowed')", nil).Err()
	require.ErrorContains(t, permissionDenied, "NOPERM ")
	for _, tt := range []struct {
		name           string
		reply          error
		delaysRecovery bool
	}{
		{name: "existing group", reply: busyGroup},
		{name: "wrapped existing group", reply: fmt.Errorf("create consumer group: %w", busyGroup)},
		{name: "permission denied", reply: permissionDenied, delaysRecovery: true},
		{name: "transport error mentioning group", reply: errors.New("BUSYGROUP connection closed"), delaysRecovery: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Unix(100, 0)
			var health redisCommandHealth
			health.observe(context.DeadlineExceeded, now)
			health.observe(tt.reply, now.Add(29*time.Second))
			health.observe(nil, now.Add(29*time.Second))
			assert.Equal(t, RedisClientPoolDegradedReasonContextDeadline, health.degradedReason(), "successful operations must still wait for the quiet window")
			health.observe(tt.reply, now.Add(30*time.Second))
			assert.Equal(t, RedisClientPoolDegradedReasonContextDeadline, health.degradedReason(), "an existing-group response alone must not confirm recovery")
			health.observe(nil, now.Add(30*time.Second))
			if tt.delaysRecovery {
				assert.Equal(t, RedisClientPoolDegradedReasonContextDeadline, health.degradedReason())
			} else {
				assert.Empty(t, health.degradedReason(), "idempotent group creation must not keep an old timeout active")
			}
		})
	}
}

func TestRedisPoolRecoveryWithExistingConsumerGroup(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_HEALTH_ADDR")
	if addr == "" {
		addr = miniredis.RunT(t).Addr()
	}
	ctx := context.Background()
	metrics := &redisClientCommandMetrics{}
	client := newRedisClientWithMetrics(&redis.Options{Addr: addr, PoolSize: 4}, metrics)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	stream := "redis-health:" + t.Name()
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, "consumer", "0").Err())
	t.Cleanup(func() { assert.NoError(t, client.Del(ctx, stream).Err()) })
	metrics.recordError(context.DeadlineExceeded)
	// Advance the original failure beyond its quiet window without sleeping.
	metrics.health.observe(context.DeadlineExceeded, time.Unix(100, 0))
	require.ErrorContains(t, client.XGroupCreateMkStream(ctx, stream, "consumer", "0").Err(), "BUSYGROUP ")
	before := redisClientPoolStats(RedisClientRoleMonitorConsumer, client, 4, metrics)
	require.Equal(t, RedisClientPoolDegradedReasonContextDeadline, before.DegradedReason)
	require.NoError(t, client.Ping(ctx).Err())
	after := redisClientPoolStats(RedisClientRoleMonitorConsumer, client, 4, metrics)
	assert.Empty(t, after.DegradedReason)
	assert.Equal(t, uint64(1), after.ContextDeadlineCount, "recovery must preserve the historical timeout count")
	assert.Zero(t, after.PoolTimeoutCount)
	assert.Equal(t, before.CommandErrorCount, after.CommandErrorCount)
	assert.False(t, after.PoolCongested)
}
