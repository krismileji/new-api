package common

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisPoolCongestionDoesNotHideActiveTimeouts(t *testing.T) {
	for _, tt := range []struct {
		name   string
		err    error
		reason string
	}{
		{"busy without failure", nil, RedisClientPoolDegradedReasonPoolCongested},
		{"pool timeout", errors.New("redis: connection pool timeout"), RedisClientPoolDegradedReasonPoolTimeout},
		{"operation deadline", context.DeadlineExceeded, RedisClientPoolDegradedReasonContextDeadline},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			metrics := &redisClientCommandMetrics{}
			client := newRedisClientWithMetrics(&redis.Options{Addr: server.Addr(), PoolSize: 1}, metrics)
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			// A dedicated connection holds the only pool slot without sleeps or
			// racing a blocked command against the diagnostic read.
			conn := client.Conn(t.Context())
			t.Cleanup(func() { require.NoError(t, conn.Close()) })
			require.NoError(t, conn.Ping(t.Context()).Err())
			metrics.recordError(tt.err)
			stats := redisClientPoolStats(RedisClientRoleMonitorRead, client, 1, metrics)
			assert.True(t, stats.PoolCongested)
			assert.Equal(t, uint32(1), stats.InUse)
			assert.Equal(t, tt.reason, stats.DegradedReason)
		})
	}
}
