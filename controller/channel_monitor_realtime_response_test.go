package controller

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorRealtimeMetadataUsesRedisObservability(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousEnabled, previousClient := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		common.RedisEnabled = previousEnabled
		common.RDB = previousClient
		_ = client.Close()
	})
	ctx := context.Background()
	require.NoError(t, client.XGroupCreateMkStream(
		ctx,
		service.ChannelMonitorRedisEventStream,
		service.ChannelMonitorRedisConsumerGroup,
		"0",
	).Err())
	require.NoError(t, client.Set(
		ctx,
		service.ChannelMonitorRedisConsumerHeartbeatKey,
		service.ChannelMonitorRedisConsumerName("controller-status"),
		time.Minute,
	).Err())
	require.NoError(t, client.HSet(
		ctx,
		service.ChannelMonitorRedisObservabilityKey,
		service.ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureCount,
		2,
		service.ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureActive,
		0,
		service.ChannelMonitorRedisObservabilityFieldStreamTrimFailureCount,
		3,
		service.ChannelMonitorRedisObservabilityFieldStreamTrimFailureActive,
		0,
	).Err())

	metadata := channelMonitorRealtimeMetadata(0)
	assert.Equal(t, service.ChannelMonitorRedisStatusAvailable, metadata.RedisStatus)
	assert.True(t, metadata.RedisAvailable)
	assert.True(t, metadata.RedisConsumerRunning)
	assert.Zero(t, metadata.PendingCount)
	assert.Zero(t, metadata.QueueDepth)
	assert.Zero(t, metadata.ConsumerLagSeconds)
	assert.Equal(t, int64(2), metadata.MarkerReleaseFailureCount)
	assert.False(t, metadata.MarkerReleaseFailureActive)
	assert.Equal(t, int64(3), metadata.StreamTrimFailureCount)
	assert.False(t, metadata.StreamTrimFailureActive)
	assert.False(t, metadata.RealtimeDegraded)

	require.NoError(t, client.HSet(
		ctx,
		service.ChannelMonitorRedisObservabilityKey,
		service.ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureActive,
		1,
	).Err())
	degraded := channelMonitorRealtimeMetadata(0)
	assert.True(t, degraded.MarkerReleaseFailureActive)
	assert.True(t, degraded.RealtimeDegraded)
}
