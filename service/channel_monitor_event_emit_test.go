package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmitChannelMonitorSuccessEventSkipsGenericChannelTestEmission(t *testing.T) {
	resetChannelMonitorEventQueueForTest(
		newChannelMonitorQueueTestConfig(),
		consumeChannelMonitorEventProjectionBatch,
	)
	t.Cleanup(func() {
		resetChannelMonitorEventQueueForTest(
			defaultChannelMonitorEventQueueConfig(),
			consumeChannelMonitorEventProjectionBatch,
		)
	})

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set("channel_test", true)
	info := &relaycommon.RelayInfo{
		OriginModelName: "model-a",
		IsChannelTest:   true,
		StartTime:       time.Now(),
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 7},
	}

	status := EmitChannelMonitorSuccessEvent(ctx, info, ChannelMonitorSuccessEventInput{})

	assert.Equal(t, ChannelMonitorEventEnqueueInvalid, status)
	assert.Zero(t, GetChannelMonitorEventQueueStats().AcceptedEvents)
}

func TestEmitChannelMonitorSuccessEventLeavesModelDetectionCostToCostEvent(t *testing.T) {
	received := make(chan []model.ChannelMonitorEvent, 1)
	resetChannelMonitorEventQueueForTest(
		newChannelMonitorQueueTestConfig(),
		func(_ context.Context, events []model.ChannelMonitorEvent) error {
			received <- append([]model.ChannelMonitorEvent(nil), events...)
			return nil
		},
	)
	t.Cleanup(func() {
		resetChannelMonitorEventQueueForTest(
			defaultChannelMonitorEventQueueConfig(),
			consumeChannelMonitorEventProjectionBatch,
		)
	})

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set(channelModelDetectionTransportContextKey, &channelModelDetectionTransportState{})
	info := &relaycommon.RelayInfo{
		OriginModelName: "model-b",
		IsChannelTest:   true,
		StartTime:       time.Now(),
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 8},
	}

	status := EmitChannelMonitorSuccessEvent(ctx, info, ChannelMonitorSuccessEventInput{})
	require.Equal(t, ChannelMonitorEventEnqueueAccepted, status)
	flushCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, FlushChannelMonitorEvents(flushCtx))

	events := <-received
	require.Len(t, events, 1)
	assert.Equal(t, model.ChannelMonitorEventSourceModelDetection, events[0].Source)
	assert.Equal(t, model.ChannelMonitorEventCostNone, events[0].CostStatus)
	assert.Zero(t, events[0].SettledCostNanoCNY)
	assert.Zero(t, events[0].UnresolvedCostNanoCNY)
}
