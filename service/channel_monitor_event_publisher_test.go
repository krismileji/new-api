package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newChannelMonitorPublisherTestEvent(eventID string) model.ChannelMonitorEvent {
	return model.ChannelMonitorEvent{
		EventId:       eventID,
		SchemaVersion: model.ChannelMonitorEventSchemaVersion,
		OccurredAt:    1_750_000_000,
		CreatedAt:     1_750_000_001,
		ChannelId:     7,
		Source:        model.ChannelMonitorEventSourceBusiness,
		Outcome:       model.ChannelMonitorEventOutcomeSuccess,
		CostStatus:    model.ChannelMonitorEventCostNone,
	}
}

func useChannelMonitorPublisherRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousEnabled, previousClient := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = client
	useChannelMonitorEventPublishStatsIsolation(t)
	writer := newChannelMonitorEventWriter(client, channelMonitorEventWriterConfig{
		QueueCapacity: 32,
		MaxAttempts:   2,
		RetryDelay:    time.Millisecond,
	})
	channelMonitorEventWriterState.Lock()
	previousWriter := channelMonitorEventWriterState.writer
	channelMonitorEventWriterState.writer = writer
	channelMonitorEventWriterState.Unlock()
	go writer.run()
	t.Cleanup(func() {
		if previousWriter != nil {
			_ = previousWriter.Stop(context.Background())
		}
		_ = writer.Stop(context.Background())
		common.RedisEnabled = previousEnabled
		common.RDB = previousClient
		_ = client.Close()
	})
	return server, client
}

func useChannelMonitorEventPublishStatsIsolation(t *testing.T) {
	t.Helper()
	resetChannelMonitorEventPublishStatsForTest()
	t.Cleanup(resetChannelMonitorEventPublishStatsForTest)
}

func TestPublishChannelMonitorEventConcurrentlyWritesDistinctEvents(t *testing.T) {
	server, client := useChannelMonitorPublisherRedis(t)
	const eventCount = 32

	statuses := make(chan ChannelMonitorEventPublishStatus, eventCount)
	errs := make(chan error, eventCount)
	var waitGroup sync.WaitGroup
	for index := 0; index < eventCount; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			status, err := PublishChannelMonitorEvent(context.Background(), newChannelMonitorPublisherTestEvent(fmt.Sprintf("event-%02d", index)))
			statuses <- status
			errs <- err
		}(index)
	}
	waitGroup.Wait()
	close(statuses)
	close(errs)

	for status := range statuses {
		assert.Equal(t, ChannelMonitorEventPublishStatusPublished, status)
	}
	for err := range errs {
		assert.NoError(t, err)
	}

	messages, err := client.XRange(context.Background(), ChannelMonitorRedisEventStream, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, messages, eventCount)
	seenIDs := make(map[string]struct{}, eventCount)
	for _, message := range messages {
		eventID := fmt.Sprint(message.Values[ChannelMonitorRedisEventFieldEventID])
		_, duplicate := seenIDs[eventID]
		assert.False(t, duplicate)
		seenIDs[eventID] = struct{}{}
		payload := fmt.Sprint(message.Values[ChannelMonitorRedisEventFieldPayload])
		event, decodeErr := model.UnmarshalChannelMonitorEvent([]byte(payload))
		require.NoError(t, decodeErr)
		assert.Equal(t, eventID, event.EventId)
		assert.Zero(t, event.EventSequence)
	}
	assert.Len(t, seenIDs, eventCount)
	stats := GetChannelMonitorEventPublishStats()
	assert.Equal(t, int64(eventCount), stats.PublishedEvents)
	assert.True(t, stats.RealtimeAvailable)
	assert.Zero(t, stats.FailedEvents)
	assert.Zero(t, stats.TimeoutEvents)
	assert.NotZero(t, stats.LastPublishedAt)
	assert.True(t, server.Exists(ChannelMonitorRedisEventStream))
}

func TestPublishChannelMonitorEventRejectsInvalidEventWithoutWriting(t *testing.T) {
	_, client := useChannelMonitorPublisherRedis(t)
	event := newChannelMonitorPublisherTestEvent("")

	status, err := PublishChannelMonitorEvent(context.Background(), event)
	require.Error(t, err)
	assert.Equal(t, ChannelMonitorEventPublishStatusInvalid, status)
	assert.Error(t, err)
	exists, existsErr := client.Exists(context.Background(), ChannelMonitorRedisEventStream).Result()
	require.NoError(t, existsErr)
	assert.Zero(t, exists)
	stats := GetChannelMonitorEventPublishStats()
	assert.Equal(t, int64(1), stats.InvalidEvents)
	assert.Zero(t, stats.FailedEvents)
}

func TestPublishChannelMonitorEventMarksRedisDisconnectUnavailable(t *testing.T) {
	server, _ := useChannelMonitorPublisherRedis(t)
	server.Close()

	status, err := PublishChannelMonitorEvent(context.Background(), newChannelMonitorPublisherTestEvent("disconnect"))
	require.Error(t, err)
	assert.Equal(t, ChannelMonitorEventPublishStatusUnavailable, status)
	assert.ErrorIs(t, err, ErrChannelMonitorEventRedisUnavailable)
	stats := GetChannelMonitorEventPublishStats()
	assert.Equal(t, int64(1), stats.FailedEvents)
	assert.False(t, stats.RealtimeAvailable)
	assert.NotZero(t, stats.LastFailureAt)
}

type blockingChannelMonitorEventAppender struct{}

func (blockingChannelMonitorEventAppender) XAdd(ctx context.Context, _ *redis.XAddArgs) *redis.StringCmd {
	<-ctx.Done()
	cmd := redis.NewStringCmd(ctx, "XADD")
	cmd.SetErr(ctx.Err())
	return cmd
}

func TestPublishChannelMonitorEventReturnsBoundedTimeoutStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	status, err := publishChannelMonitorEvent(ctx, blockingChannelMonitorEventAppender{}, newChannelMonitorPublisherTestEvent("timeout"))
	require.Error(t, err)
	assert.Equal(t, ChannelMonitorEventPublishStatusTimeout, status)
	assert.ErrorIs(t, err, ErrChannelMonitorEventPublishTimeout)
	stats := GetChannelMonitorEventPublishStats()
	assert.Equal(t, int64(1), stats.TimeoutEvents)
	assert.False(t, stats.RealtimeAvailable)
	assert.NotZero(t, stats.LastFailureAt)
}

func TestPublishChannelMonitorEventRequiresRedis(t *testing.T) {
	previousEnabled, previousClient := common.RedisEnabled, common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	useChannelMonitorEventPublishStatsIsolation(t)
	t.Cleanup(func() {
		common.RedisEnabled = previousEnabled
		common.RDB = previousClient
	})

	status, err := PublishChannelMonitorEvent(context.Background(), newChannelMonitorPublisherTestEvent("missing-redis"))
	require.Error(t, err)
	assert.Equal(t, ChannelMonitorEventPublishStatusUnavailable, status)
	assert.ErrorIs(t, err, ErrChannelMonitorEventRedisUnavailable)
}
