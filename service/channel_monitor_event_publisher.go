package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

const channelMonitorEventPublishTimeout = 2 * time.Second

var (
	ErrChannelMonitorEventRedisUnavailable = errors.New("渠道监控实时链路不可用")
	ErrChannelMonitorEventPublishTimeout   = errors.New("渠道监控事件发布超时")
)

type ChannelMonitorEventPublishStatus string

const (
	ChannelMonitorEventPublishStatusPublished   ChannelMonitorEventPublishStatus = "published"
	ChannelMonitorEventPublishStatusQueued      ChannelMonitorEventPublishStatus = "queued"
	ChannelMonitorEventPublishStatusDropped     ChannelMonitorEventPublishStatus = "dropped"
	ChannelMonitorEventPublishStatusInvalid     ChannelMonitorEventPublishStatus = "invalid"
	ChannelMonitorEventPublishStatusUnavailable ChannelMonitorEventPublishStatus = "unavailable"
	ChannelMonitorEventPublishStatusTimeout     ChannelMonitorEventPublishStatus = "timeout"
)

// ChannelMonitorEventPublishStats exposes the durable publisher health state.
// RealtimeAvailable becomes false after a Redis publish failure and true after
// the next successful publish.
type ChannelMonitorEventPublishStats struct {
	PublishedEvents   int64 `json:"published_events"`
	InvalidEvents     int64 `json:"invalid_events"`
	FailedEvents      int64 `json:"failed_events"`
	TimeoutEvents     int64 `json:"timeout_events"`
	LastPublishedAt   int64 `json:"last_published_at"`
	LastFailureAt     int64 `json:"last_failure_at"`
	RealtimeAvailable bool  `json:"realtime_available"`
}

type channelMonitorRedisStreamAppender interface {
	XAdd(context.Context, *redis.XAddArgs) *redis.StringCmd
}

type channelMonitorEventPublisherStats struct {
	publishedEvents   atomic.Int64
	invalidEvents     atomic.Int64
	failedEvents      atomic.Int64
	timeoutEvents     atomic.Int64
	lastPublishedAt   atomic.Int64
	lastFailureAt     atomic.Int64
	realtimeAvailable atomic.Bool
}

var channelMonitorEventPublisherStatsState channelMonitorEventPublisherStats

// PublishChannelMonitorEvent validates and publishes one event to the
// versioned Redis Stream.
func PublishChannelMonitorEvent(ctx context.Context, event model.ChannelMonitorEvent) (ChannelMonitorEventPublishStatus, error) {
	payload, err := event.Marshal()
	if err != nil {
		channelMonitorEventPublisherStatsState.invalidEvents.Add(1)
		return ChannelMonitorEventPublishStatusInvalid, err
	}
	client := common.RedisMonitorWriteClient()
	if !common.RedisEnabled || client == nil {
		return markChannelMonitorEventPublishFailure(
			ctx,
			ChannelMonitorEventPublishStatusUnavailable,
			ErrChannelMonitorEventRedisUnavailable,
		)
	}
	return publishChannelMonitorEventWithPayload(ctx, client, event, payload)
}

func publishChannelMonitorEvent(
	ctx context.Context,
	client channelMonitorRedisStreamAppender,
	event model.ChannelMonitorEvent,
) (ChannelMonitorEventPublishStatus, error) {
	payload, err := event.Marshal()
	if err != nil {
		channelMonitorEventPublisherStatsState.invalidEvents.Add(1)
		return ChannelMonitorEventPublishStatusInvalid, err
	}
	return publishChannelMonitorEventWithPayload(ctx, client, event, payload)
}

func publishChannelMonitorEventWithPayload(
	ctx context.Context,
	client channelMonitorRedisStreamAppender,
	event model.ChannelMonitorEvent,
	payload []byte,
) (ChannelMonitorEventPublishStatus, error) {
	if client == nil {
		return markChannelMonitorEventPublishFailure(ctx, ChannelMonitorEventPublishStatusUnavailable, ErrChannelMonitorEventRedisUnavailable)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	publishCtx, cancel := context.WithTimeout(ctx, channelMonitorEventPublishTimeout)
	defer cancel()

	_, err := client.XAdd(publishCtx, &redis.XAddArgs{
		Stream: ChannelMonitorRedisEventStream,
		ID:     "*",
		Values: map[string]interface{}{
			ChannelMonitorRedisEventFieldEventID: string(event.EventId),
			ChannelMonitorRedisEventFieldPayload: string(payload),
		},
	}).Result()
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || publishCtx.Err() != nil {
			return markChannelMonitorEventPublishFailure(ctx, ChannelMonitorEventPublishStatusTimeout, fmt.Errorf("%w: %w", ErrChannelMonitorEventPublishTimeout, err))
		}
		return markChannelMonitorEventPublishFailure(ctx, ChannelMonitorEventPublishStatusUnavailable, fmt.Errorf("%w: Redis Stream XADD 失败: %w", ErrChannelMonitorEventRedisUnavailable, err))
	}

	channelMonitorEventPublisherStatsState.publishedEvents.Add(1)
	channelMonitorEventPublisherStatsState.lastPublishedAt.Store(time.Now().Unix())
	channelMonitorEventPublisherStatsState.realtimeAvailable.Store(true)
	return ChannelMonitorEventPublishStatusPublished, nil
}

func markChannelMonitorEventPublishFailure(
	ctx context.Context,
	status ChannelMonitorEventPublishStatus,
	err error,
) (ChannelMonitorEventPublishStatus, error) {
	if status == ChannelMonitorEventPublishStatusTimeout {
		channelMonitorEventPublisherStatsState.timeoutEvents.Add(1)
	} else {
		channelMonitorEventPublisherStatsState.failedEvents.Add(1)
	}
	channelMonitorEventPublisherStatsState.lastFailureAt.Store(time.Now().Unix())
	channelMonitorEventPublisherStatsState.realtimeAvailable.Store(false)
	logger.LogWarn(ctx, fmt.Sprintf("渠道监控事件发布失败（%s）: %v", status, err))
	return status, err
}

func GetChannelMonitorEventPublishStats() ChannelMonitorEventPublishStats {
	return ChannelMonitorEventPublishStats{
		PublishedEvents:   channelMonitorEventPublisherStatsState.publishedEvents.Load(),
		InvalidEvents:     channelMonitorEventPublisherStatsState.invalidEvents.Load(),
		FailedEvents:      channelMonitorEventPublisherStatsState.failedEvents.Load(),
		TimeoutEvents:     channelMonitorEventPublisherStatsState.timeoutEvents.Load(),
		LastPublishedAt:   channelMonitorEventPublisherStatsState.lastPublishedAt.Load(),
		LastFailureAt:     channelMonitorEventPublisherStatsState.lastFailureAt.Load(),
		RealtimeAvailable: channelMonitorEventPublisherStatsState.realtimeAvailable.Load(),
	}
}

func resetChannelMonitorEventPublishStatsForTest() {
	channelMonitorEventPublisherStatsState.publishedEvents.Store(0)
	channelMonitorEventPublisherStatsState.invalidEvents.Store(0)
	channelMonitorEventPublisherStatsState.failedEvents.Store(0)
	channelMonitorEventPublisherStatsState.timeoutEvents.Store(0)
	channelMonitorEventPublisherStatsState.lastPublishedAt.Store(0)
	channelMonitorEventPublisherStatsState.lastFailureAt.Store(0)
	channelMonitorEventPublisherStatsState.realtimeAvailable.Store(false)
}
