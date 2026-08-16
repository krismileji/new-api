package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelMonitorRedisTestClient struct {
	*redis.Client
	version        string
	commandInfoErr error
}

func (c *channelMonitorRedisTestClient) Info(ctx context.Context, _ ...string) *redis.StringCmd {
	cmd := redis.NewStringCmd(ctx, "INFO", "server")
	cmd.SetVal("# Server\r\nredis_version:" + c.version + "\r\n")
	return cmd
}

func (c *channelMonitorRedisTestClient) Do(ctx context.Context, args ...interface{}) *redis.Cmd {
	if len(args) >= 3 && strings.EqualFold(args[0].(string), "COMMAND") &&
		strings.EqualFold(args[1].(string), "INFO") && strings.EqualFold(args[2].(string), "XAUTOCLAIM") {
		cmd := redis.NewCmd(ctx, args...)
		if c.commandInfoErr != nil {
			cmd.SetErr(c.commandInfoErr)
			return cmd
		}
		cmd.SetVal([]interface{}{[]interface{}{"XAUTOCLAIM"}})
		return cmd
	}
	return c.Client.Do(ctx, args...)
}

func TestChannelMonitorRedisVersionSupport(t *testing.T) {
	tests := []struct {
		version   string
		supported bool
	}{
		{version: "6.2.0", supported: true},
		{version: "6.2", supported: true},
		{version: "7.0.0", supported: true},
		{version: "6.1.9", supported: false},
		{version: "5.0.14", supported: false},
		{version: "6.2foo", supported: false},
		{version: "6.2.x", supported: false},
		{version: "", supported: false},
		{version: "invalid", supported: false},
	}
	for _, test := range tests {
		assert.Equal(t, test.supported, channelMonitorRedisVersionSupported(test.version), test.version)
	}
}

func TestChannelMonitorRedisKeyContract(t *testing.T) {
	assert.Equal(t, "channel_monitor:v1", ChannelMonitorRedisKeyPrefix)
	assert.Equal(t, "channel_monitor:v1:events", ChannelMonitorRedisEventStream)
	assert.Equal(t, "channel_monitor:v1:aggregators", ChannelMonitorRedisConsumerGroup)
	assert.Equal(t, "channel_monitor:v1:consumer:node~3Aa", ChannelMonitorRedisConsumerName("node:a"))
	assert.Equal(t, "channel_monitor:v1:aggregator:lease", ChannelMonitorRedisAggregatorLeaseKey)
	assert.Equal(t, "channel_monitor:v1:projection:route:channel~3Amodel", ChannelMonitorRedisProjectionKeyForRoute("channel:model"))
	assert.Equal(t, "channel_monitor:v1:projection:shared:node~3Aa", ChannelMonitorRedisProjectionKey("shared", "node:a"))
	assert.Equal(t, "channel_monitor:v1:projection:dedup:event~3A1", ChannelMonitorRedisProjectionDedupKey("event:1"))
	assert.True(t, strings.HasPrefix(ChannelMonitorRedisProjectionKeyForDashboard("global"), ChannelMonitorRedisKeyPrefix+":"))
	assert.True(t, strings.HasPrefix(ChannelMonitorRedisProjectionKeyForCost("2026-08-15"), ChannelMonitorRedisKeyPrefix+":"))
}

func TestInitChannelMonitorRedisStreamCreatesIdempotentGroup(t *testing.T) {
	server := miniredis.RunT(t)
	client := &channelMonitorRedisTestClient{
		Client:  redis.NewClient(&redis.Options{Addr: server.Addr()}),
		version: "6.2.0",
	}
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, initChannelMonitorRedisStream(context.Background(), client, ChannelMonitorRedisConsumerName("test-node")))
	require.NoError(t, initChannelMonitorRedisStream(context.Background(), client, ChannelMonitorRedisConsumerName("test-node")))

	groups, err := client.Do(context.Background(), "XINFO", "GROUPS", ChannelMonitorRedisEventStream).Result()
	require.NoError(t, err)
	assert.NotEmpty(t, groups)
	assert.True(t, server.Exists(ChannelMonitorRedisEventStream))
}

func TestInitChannelMonitorRedisStreamStartsBeforeExistingMessages(t *testing.T) {
	server := miniredis.RunT(t)
	client := &channelMonitorRedisTestClient{
		Client:  redis.NewClient(&redis.Options{Addr: server.Addr()}),
		version: "6.2.0",
	}
	t.Cleanup(func() { _ = client.Close() })

	_, err := client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: ChannelMonitorRedisEventStream,
		Values: map[string]interface{}{ChannelMonitorRedisEventFieldEventID: "before-group"},
	}).Result()
	require.NoError(t, err)
	require.NoError(t, initChannelMonitorRedisStream(
		context.Background(), client, ChannelMonitorRedisConsumerName("test-node"),
	))

	streams, err := client.XReadGroup(context.Background(), &redis.XReadGroupArgs{
		Group:    ChannelMonitorRedisConsumerGroup,
		Consumer: ChannelMonitorRedisConsumerName("test-node"),
		Streams:  []string{ChannelMonitorRedisEventStream, ">"},
		Count:    1,
		Block:    -1,
	}).Result()
	require.NoError(t, err)
	require.Len(t, streams, 1)
	require.Len(t, streams[0].Messages, 1)
	assert.Equal(t, "before-group", streams[0].Messages[0].Values[ChannelMonitorRedisEventFieldEventID])
}

func TestInitChannelMonitorRedisStreamRejectsOldRedis(t *testing.T) {
	server := miniredis.RunT(t)
	client := &channelMonitorRedisTestClient{
		Client:  redis.NewClient(&redis.Options{Addr: server.Addr()}),
		version: "6.0.16",
	}
	t.Cleanup(func() { _ = client.Close() })

	err := initChannelMonitorRedisStream(context.Background(), client, ChannelMonitorRedisConsumerName("test-node"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "低于最低要求 6.2")
}

func TestInitChannelMonitorRedisStreamRejectsRedisConnectionFailure(t *testing.T) {
	server := miniredis.RunT(t)
	client := &channelMonitorRedisTestClient{
		Client:  redis.NewClient(&redis.Options{Addr: server.Addr()}),
		version: "6.2.0",
	}
	server.Close()
	t.Cleanup(func() { _ = client.Close() })

	err := initChannelMonitorRedisStream(context.Background(), client, ChannelMonitorRedisConsumerName("test-node"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Redis 连接检查失败")
}

func TestInitChannelMonitorRedisStreamRejectsUnavailableAutoClaim(t *testing.T) {
	server := miniredis.RunT(t)
	client := &channelMonitorRedisTestClient{
		Client:         redis.NewClient(&redis.Options{Addr: server.Addr()}),
		version:        "6.2.0",
		commandInfoErr: errors.New("ERR unknown command 'COMMAND'"),
	}
	t.Cleanup(func() { _ = client.Close() })

	err := initChannelMonitorRedisStream(context.Background(), client, ChannelMonitorRedisConsumerName("test-node"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Stream 能力不可用")
}

func TestInitChannelMonitorRedisStreamRejectsMissingRedis(t *testing.T) {
	previousEnabled, previousClient := common.RedisEnabled, common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = previousEnabled
		common.RDB = previousClient
	})

	err := InitChannelMonitorRedisStream(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未启用 Redis")
}

func TestInitChannelMonitorRedisStreamRejectsNilClient(t *testing.T) {
	previousEnabled, previousClient := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = previousEnabled
		common.RDB = previousClient
	})

	err := InitChannelMonitorRedisStream(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "客户端不可用")
}
