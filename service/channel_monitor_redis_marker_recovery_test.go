package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorRedisCanceledEffectReleasesMarkersForNextAttempt(t *testing.T) {
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	aggregator, err := NewChannelMonitorRedisLogicalAggregatorWithClient(client,
		func(context.Context, []model.ChannelMonitorEvent) error { return nil }, nil)
	require.NoError(t, err)
	event := newChannelMonitorRedisConsumerTestEvent("canceled-effect")
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), channelMonitorRedisEffectOwnerContextKey{}, "first-owner"))
	defer cancel()
	require.NoError(t, client.Set(ctx, ChannelMonitorRedisAggregatorLeaseKey, "first-owner", time.Minute).Err())
	err = aggregator.applyEffect(ctx, []model.ChannelMonitorEvent{event}, ChannelMonitorRedisRuntimeEffectKey,
		channelMonitorRedisRuntimeEffectTTL, ChannelMonitorRedisObservabilityFieldRuntimeMarkerFailureCount,
		func(context.Context, []model.ChannelMonitorEvent) error { cancel(); return context.Canceled })
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, client.Exists(context.Background(), ChannelMonitorRedisRuntimeEffectKey(event.EventId)).Val())
	require.NoError(t, client.Set(context.Background(), ChannelMonitorRedisAggregatorLeaseKey, "next-owner", time.Minute).Err())
	nextCtx := context.WithValue(context.Background(), channelMonitorRedisEffectOwnerContextKey{}, "next-owner")
	called := false
	err = aggregator.applyEffect(nextCtx, []model.ChannelMonitorEvent{event}, ChannelMonitorRedisRuntimeEffectKey,
		channelMonitorRedisRuntimeEffectTTL, ChannelMonitorRedisObservabilityFieldRuntimeMarkerFailureCount,
		func(context.Context, []model.ChannelMonitorEvent) error { called = true; return nil })
	require.NoError(t, err)
	assert.True(t, called)
}
