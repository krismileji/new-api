package service

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
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
	require.Equal(t, ChannelMonitorEventPublishStatusQueued, status)
	var messages []redis.XMessage
	require.Eventually(t, func() bool {
		var err error
		messages, err = client.XRange(context.Background(), ChannelMonitorRedisEventStream, "-", "+").Result()
		return err == nil && len(messages) == 1
	}, time.Second, time.Millisecond)
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

func TestEmitChannelMonitorSuccessEventUsesAttemptPerformanceTiming(t *testing.T) {
	_, client := useChannelMonitorPublisherRedis(t)

	ctx, _ := gin.CreateTestContext(nil)
	completedAt := time.Unix(1000, 0)
	attemptStartedAt := completedAt.Add(-2500 * time.Millisecond)
	firstResponseAt := attemptStartedAt.Add(500 * time.Millisecond)
	BeginChannelMonitorPerformanceAttempt(ctx, attemptStartedAt)
	timing := RelayPerformanceTiming{
		CompletedAt:       completedAt,
		AttemptDurationMs: 9999,
	}
	info := &relaycommon.RelayInfo{
		OriginModelName:   "model-c",
		StartTime:         completedAt.Add(-10 * time.Second),
		FirstResponseTime: firstResponseAt,
		IsStream:          true,
		ChannelMeta:       &relaycommon.ChannelMeta{ChannelId: 9},
	}

	status := EmitChannelMonitorSuccessEvent(ctx, info, ChannelMonitorSuccessEventInput{
		CompletionTokens:  100,
		PerformanceTiming: &timing,
	})
	require.Equal(t, ChannelMonitorEventPublishStatusQueued, status)
	var messages []redis.XMessage
	require.Eventually(t, func() bool {
		var err error
		messages, err = client.XRange(context.Background(), ChannelMonitorRedisEventStream, "-", "+").Result()
		return err == nil && len(messages) == 1
	}, time.Second, time.Millisecond)
	require.Len(t, messages, 1)
	event, err := model.UnmarshalChannelMonitorEvent([]byte(fmt.Sprint(
		messages[0].Values[ChannelMonitorRedisEventFieldPayload],
	)))
	require.NoError(t, err)
	require.NotNil(t, event.FirstTokenMs)
	assert.InDelta(t, 500, *event.FirstTokenMs, 1e-9)
	require.NotNil(t, event.TPS)
	assert.InDelta(t, 50, *event.TPS, 1e-9)
	require.NotNil(t, event.AttemptDurationMs)
	assert.Equal(t, int64(2500), *event.AttemptDurationMs)
}

func TestEmitChannelMonitorSuccessEventWithCanceledRequestOnlyQueues(t *testing.T) {
	useChannelMonitorEventPublishStatsIsolation(t)
	writer := newChannelMonitorEventWriter(nil, channelMonitorEventWriterConfig{
		QueueCapacity: 1,
		MaxAttempts:   1,
	})
	setChannelMonitorEventWriterForTest(t, writer)
	t.Cleanup(writer.cancelRun)

	ctx, _ := gin.CreateTestContext(nil)
	request := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	requestContext, cancel := context.WithCancel(request.Context())
	cancel()
	ctx.Request = request.WithContext(requestContext)
	info := &relaycommon.RelayInfo{
		OriginModelName: "model-canceled",
		StartTime:       time.Now(),
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 10},
	}

	status := EmitChannelMonitorSuccessEvent(ctx, info, ChannelMonitorSuccessEventInput{})

	assert.Equal(t, ChannelMonitorEventPublishStatusQueued, status)
	require.Len(t, writer.queue, 1)
	item := <-writer.queue
	assert.Equal(t, 10, item.event.ChannelId)
	assert.Equal(t, "model-canceled", item.event.ModelName)
	publishStats := GetChannelMonitorEventPublishStats()
	assert.Zero(t, publishStats.PublishedEvents)
	assert.Zero(t, publishStats.FailedEvents)
	assert.Zero(t, publishStats.TimeoutEvents)
}

func TestChannelMonitorEventSourcePrefersGroupProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(model.ChannelMonitorGroupProbeLogKey, true)
	ctx.Set(model.ChannelMonitorStatusProbeLogKey, true)

	assert.Equal(t, model.ChannelMonitorEventSourceGroupProbe, channelMonitorEventSource(ctx))
}
