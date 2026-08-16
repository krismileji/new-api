package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmitChannelMonitorSuccessEventSkipsGenericChannelTestEmission(t *testing.T) {
	_, client := useChannelMonitorPublisherRedis(t)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set("channel_test", true)
	info := &relaycommon.RelayInfo{
		OriginModelName: "model-a",
		IsChannelTest:   true,
		StartTime:       time.Now(),
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 7},
	}

	status := EmitChannelMonitorSuccessEvent(ctx, info, ChannelMonitorSuccessEventInput{})

	assert.Equal(t, ChannelMonitorEventPublishStatusInvalid, status)
	exists, err := client.Exists(context.Background(), ChannelMonitorRedisEventStream).Result()
	require.NoError(t, err)
	assert.Zero(t, exists)
}

func TestEmitChannelMonitorSuccessEventLeavesModelDetectionCostToCostEvent(t *testing.T) {
	_, client := useChannelMonitorPublisherRedis(t)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set(channelModelDetectionTransportContextKey, &channelModelDetectionTransportState{})
	info := &relaycommon.RelayInfo{
		OriginModelName: "model-b",
		IsChannelTest:   true,
		StartTime:       time.Now(),
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 8},
	}

	status := EmitChannelMonitorSuccessEvent(ctx, info, ChannelMonitorSuccessEventInput{})
	require.Equal(t, ChannelMonitorEventPublishStatusPublished, status)
	messages, err := client.XRange(context.Background(), ChannelMonitorRedisEventStream, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, messages, 1)
	event, err := model.UnmarshalChannelMonitorEvent([]byte(fmt.Sprint(
		messages[0].Values[ChannelMonitorRedisEventFieldPayload],
	)))
	require.NoError(t, err)
	assert.Equal(t, model.ChannelMonitorEventSourceModelDetection, event.Source)
	assert.Equal(t, model.ChannelMonitorEventCostNone, event.CostStatus)
	assert.Zero(t, event.SettledCostNanoCNY)
	assert.Zero(t, event.UnresolvedCostNanoCNY)
}

func TestEmitChannelMonitorSuccessEventUsesCanonicalPerformanceTiming(t *testing.T) {
	_, client := useChannelMonitorPublisherRedis(t)

	ctx, _ := gin.CreateTestContext(nil)
	tokensPerSecond := 18.75
	firstTokenMs := 625.0
	completedAt := time.Unix(1000, 0)
	timing := RelayPerformanceTiming{
		CompletedAt:       completedAt,
		AttemptDurationMs: 3200,
		FirstTokenMs:      &firstTokenMs,
		OutputTokens:      60,
		TokensPerSecond:   &tokensPerSecond,
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "model-c",
		StartTime:       completedAt.Add(-3200 * time.Millisecond),
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 9},
	}

	status := EmitChannelMonitorSuccessEvent(ctx, info, ChannelMonitorSuccessEventInput{
		CompletionTokens:  100,
		PerformanceTiming: &timing,
	})
	require.Equal(t, ChannelMonitorEventPublishStatusPublished, status)
	messages, err := client.XRange(context.Background(), ChannelMonitorRedisEventStream, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, messages, 1)
	event, err := model.UnmarshalChannelMonitorEvent([]byte(fmt.Sprint(
		messages[0].Values[ChannelMonitorRedisEventFieldPayload],
	)))
	require.NoError(t, err)
	require.NotNil(t, event.FirstTokenMs)
	assert.InDelta(t, firstTokenMs, *event.FirstTokenMs, 1e-9)
	require.NotNil(t, event.TPS)
	assert.InDelta(t, tokensPerSecond, *event.TPS, 1e-9)
	require.NotNil(t, event.AttemptDurationMs)
	assert.Equal(t, int64(3200), *event.AttemptDurationMs)
}
