package common

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
