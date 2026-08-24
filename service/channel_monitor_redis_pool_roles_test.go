package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorRedisComponentsUseDedicatedRoleClients(t *testing.T) {
	server := miniredis.RunT(t)
	newClient := func() *redis.Client {
		return redis.NewClient(&redis.Options{Addr: server.Addr()})
	}
	userClient := newClient()
	writeClient := newClient()
	readClient := newClient()
	consumerClient := newClient()
	previousEnabled := common.RedisEnabled
	previousUser := common.RDB
	previousWrite := common.RDBMonitorWrite
	previousRead := common.RDBMonitorRead
	previousConsumer := common.RDBMonitorConsumer
	previousRuntimeHandler := channelMonitorRedisRuntimeEffectHandler.Load()
	common.RedisEnabled = true
	common.RDB = userClient
	common.RDBMonitorWrite = writeClient
	common.RDBMonitorRead = readClient
	common.RDBMonitorConsumer = consumerClient
	t.Cleanup(func() {
		common.RedisEnabled = previousEnabled
		common.RDB = previousUser
		common.RDBMonitorWrite = previousWrite
		common.RDBMonitorRead = previousRead
		common.RDBMonitorConsumer = previousConsumer
		channelMonitorRedisRuntimeEffectHandler.Store(previousRuntimeHandler)
		for _, client := range []*redis.Client{userClient, writeClient, readClient, consumerClient} {
			_ = client.Close()
		}
	})

	require.NoError(t, InitChannelMonitorRedisStream(context.Background()))
	writer, err := StartChannelMonitorEventWriter()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, writer.Stop(context.Background())) })
	consumer, err := NewChannelMonitorRedisEventConsumer(
		ChannelMonitorRedisEventHandlerFunc(func(context.Context, []model.ChannelMonitorEvent) error {
			return nil
		}),
	)
	require.NoError(t, err)
	sharedProjection, err := NewChannelMonitorRedisSharedProjection()
	require.NoError(t, err)
	routeProjection, err := NewChannelMonitorRedisRouteHealthProjection()
	require.NoError(t, err)
	require.True(t, RegisterChannelMonitorRedisRuntimeEffectHandler(
		func(context.Context, []model.ChannelMonitorEvent) error { return nil },
	))
	aggregator, err := NewChannelMonitorRedisLogicalAggregator()
	require.NoError(t, err)
	aggregatorSharedProjection, ok := aggregator.sharedProjection.(*ChannelMonitorRedisSharedProjection)
	require.True(t, ok)
	aggregatorRouteProjection, ok := aggregator.routeHealth.(*ChannelMonitorRedisRouteHealthProjection)
	require.True(t, ok)

	assert.Same(t, writeClient, writer.client)
	assert.Same(t, consumerClient, consumer.client)
	assert.Same(t, readClient, sharedProjection.client)
	assert.Same(t, readClient, routeProjection.client)
	assert.Same(t, consumerClient, aggregator.client)
	assert.Same(t, consumerClient, aggregatorSharedProjection.client)
	assert.Same(t, consumerClient, aggregatorRouteProjection.client)
}
