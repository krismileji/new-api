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
