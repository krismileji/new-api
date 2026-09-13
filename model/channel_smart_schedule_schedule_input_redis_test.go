package model

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSmartSchedulePublishedRouteGuardsWithRealRedis(t *testing.T) {
	addr := os.Getenv("SCHEDULE_RETRY_REDIS_ADDR")
	if addr == "" {
		t.Skip("SCHEDULE_RETRY_REDIS_ADDR is not set")
	}
	host, _, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", host, "use a disposable local Redis instance")
	db, _, originalClient := setupChannelSmartScheduleRedisSnapshotTest(t)
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	ctx := context.Background()
	size, err := client.DBSize(ctx).Result()
	require.NoError(t, err)
	require.Zero(t, size, "use an empty disposable Redis database")
	t.Cleanup(func() { assert.NoError(t, client.FlushDB(ctx).Err()) })
	common.RDB = client
	t.Cleanup(func() { common.RDB = originalClient })
	info, err := client.Info(ctx, "server").Result()
	require.NoError(t, err)
	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "redis_version:") {
			t.Log(strings.TrimSpace(line))
		}
	}
	runChannelSmartScheduleMonitorPublishedRouteGuards(t, db, true)
}
