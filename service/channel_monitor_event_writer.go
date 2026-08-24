package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	channelMonitorEventWriterDefaultQueueCapacity = 8192
	channelMonitorEventWriterMaxAttempts          = 3
	channelMonitorEventWriterRetryDelay           = 100 * time.Millisecond
)

type channelMonitorEventWriterItem struct {
	event      model.ChannelMonitorEvent
	payload    []byte
	enqueuedAt int64
}

type channelMonitorEventWriterConfig struct {
	QueueCapacity int
	MaxAttempts   int
	RetryDelay    time.Duration
}

type channelMonitorEventWriter struct {
	client channelMonitorRedisStreamAppender
	queue  chan channelMonitorEventWriterItem
	config channelMonitorEventWriterConfig

	runCtx    context.Context
	cancelRun context.CancelFunc
	stopCh    chan struct{}
	doneCh    chan struct{}
	stopOnce  sync.Once
	stopping  atomic.Bool

	queuedEvents   atomic.Int64
	droppedEvents  atomic.Int64
	retryEvents    atomic.Int64
	oldestQueuedAt atomic.Int64
}

var channelMonitorEventWriterState struct {
	sync.RWMutex
	writer *channelMonitorEventWriter
}

// StartChannelMonitorEventWriter starts the bounded, non-blocking monitor
// event writer. It must be started after Redis has been initialized.
func StartChannelMonitorEventWriter() (*channelMonitorEventWriter, error) {
	client := common.RedisMonitorWriteClient()
	if !common.RedisEnabled || client == nil {
		return nil, ErrChannelMonitorEventRedisUnavailable
	}
	writer := newChannelMonitorEventWriter(
		client,
		channelMonitorEventWriterConfig{
			QueueCapacity: common.GetEnvOrDefault(
				"CHANNEL_MONITOR_EVENT_WRITER_QUEUE_CAPACITY",
				channelMonitorEventWriterDefaultQueueCapacity,
			),
			MaxAttempts: channelMonitorEventWriterMaxAttempts,
			RetryDelay:  channelMonitorEventWriterRetryDelay,
		},
	)
	channelMonitorEventWriterState.Lock()
	previous := channelMonitorEventWriterState.writer
	channelMonitorEventWriterState.writer = writer
	channelMonitorEventWriterState.Unlock()
	if previous != nil {
		_ = previous.Stop(context.Background())
	}
	go writer.run()
	return writer, nil
}

func newChannelMonitorEventWriter(
	client channelMonitorRedisStreamAppender,
	config channelMonitorEventWriterConfig,
) *channelMonitorEventWriter {
	if config.QueueCapacity <= 0 {
		config.QueueCapacity = channelMonitorEventWriterDefaultQueueCapacity
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 1
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = channelMonitorEventWriterRetryDelay
	}
	runCtx, cancelRun := context.WithCancel(context.Background())
	return &channelMonitorEventWriter{
		client: client,
		queue:  make(chan channelMonitorEventWriterItem, config.QueueCapacity),
		config: config,
		runCtx: runCtx, cancelRun: cancelRun,
		stopCh: make(chan struct{}), doneCh: make(chan struct{}),
	}
}

// EnqueueChannelMonitorEvent validates and queues one observation without
// waiting for Redis. Queue-full and unavailable outcomes are intentionally
// visible to callers so they can be counted without reintroducing a fallback.
func EnqueueChannelMonitorEvent(event model.ChannelMonitorEvent) (ChannelMonitorEventPublishStatus, error) {
	payload, err := event.Marshal()
	if err != nil {
		channelMonitorEventPublisherStatsState.invalidEvents.Add(1)
		return ChannelMonitorEventPublishStatusInvalid, err
	}
	// Keep the registry read lock through the non-blocking send. Stop takes the
	// write lock before marking the writer closed, so every enqueue is
	// linearized either before Stop (and must be drained) or after it (and is
	// rejected). No sender can retain a writer pointer and publish into a queue
	// whose run loop has already exited.
	channelMonitorEventWriterState.RLock()
	defer channelMonitorEventWriterState.RUnlock()
	writer := channelMonitorEventWriterState.writer
	if writer == nil {
		return ChannelMonitorEventPublishStatusDropped, ErrChannelMonitorEventRedisUnavailable
	}
	item := channelMonitorEventWriterItem{event: event, payload: payload, enqueuedAt: time.Now().Unix()}
	return writer.enqueue(item)
}

func (writer *channelMonitorEventWriter) enqueue(
	item channelMonitorEventWriterItem,
) (ChannelMonitorEventPublishStatus, error) {
	if writer == nil || writer.stopping.Load() {
		return ChannelMonitorEventPublishStatusDropped, ErrChannelMonitorEventRedisUnavailable
	}
	select {
	case writer.queue <- item:
		writer.queuedEvents.Add(1)
		writer.oldestQueuedAt.CompareAndSwap(0, item.enqueuedAt)
		return ChannelMonitorEventPublishStatusQueued, nil
	default:
		writer.droppedEvents.Add(1)
		return ChannelMonitorEventPublishStatusDropped, ErrChannelMonitorEventWriterQueueFull
	}
}

var ErrChannelMonitorEventWriterQueueFull = errors.New("渠道监控事件写入队列已满")

func (writer *channelMonitorEventWriter) run() {
	defer writer.cancelRun()
	defer close(writer.doneCh)
	for {
		select {
		case item := <-writer.queue:
			if !writer.write(item) && writer.runCtx.Err() != nil {
				writer.dropQueued()
				return
			}
		case <-writer.stopCh:
			writer.drain()
			return
		}
	}
}

func (writer *channelMonitorEventWriter) write(item channelMonitorEventWriterItem) bool {
	for attempt := 1; attempt <= writer.config.MaxAttempts; attempt++ {
		if writer.runCtx.Err() != nil {
			writer.dropItem()
			return false
		}
		status, err := publishChannelMonitorEventWithPayload(
			writer.runCtx, writer.client, item.event, item.payload,
		)
		if err == nil && status == ChannelMonitorEventPublishStatusPublished {
			if len(writer.queue) == 0 {
				writer.oldestQueuedAt.Store(0)
			}
			return true
		}
		if attempt == writer.config.MaxAttempts {
			writer.dropItem()
			return false
		}
		timer := time.NewTimer(writer.config.RetryDelay)
		select {
		case <-timer.C:
			writer.retryEvents.Add(1)
		case <-writer.runCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			writer.dropItem()
			return false
		}
	}
	return false
}

func (writer *channelMonitorEventWriter) drain() {
	for {
		if writer.runCtx.Err() != nil {
			writer.dropQueued()
			return
		}
		select {
		case item := <-writer.queue:
			if !writer.write(item) && writer.runCtx.Err() != nil {
				writer.dropQueued()
				return
			}
		default:
			return
		}
	}
}

func (writer *channelMonitorEventWriter) dropItem() {
	writer.droppedEvents.Add(1)
	if len(writer.queue) == 0 {
		writer.oldestQueuedAt.Store(0)
	}
}

func (writer *channelMonitorEventWriter) dropQueued() {
	dropped := int64(0)
	for {
		select {
		case <-writer.queue:
			dropped++
		default:
			if dropped > 0 {
				writer.droppedEvents.Add(dropped)
			}
			writer.oldestQueuedAt.Store(0)
			return
		}
	}
}

func (writer *channelMonitorEventWriter) Stop(ctx context.Context) error {
	if writer == nil {
		return nil
	}
	writer.stopOnce.Do(func() {
		channelMonitorEventWriterState.Lock()
		if channelMonitorEventWriterState.writer == writer {
			channelMonitorEventWriterState.writer = nil
		}
		writer.stopping.Store(true)
		close(writer.stopCh)
		channelMonitorEventWriterState.Unlock()
	})
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-writer.doneCh:
		return nil
	case <-ctx.Done():
		writer.cancelRun()
		return fmt.Errorf("渠道监控事件 writer 停止超时: %w", ctx.Err())
	}
}

func GetChannelMonitorEventWriterStats() ChannelMonitorEventWriterStats {
	channelMonitorEventWriterState.RLock()
	writer := channelMonitorEventWriterState.writer
	channelMonitorEventWriterState.RUnlock()
	if writer == nil {
		return ChannelMonitorEventWriterStats{}
	}
	oldestQueuedAt := writer.oldestQueuedAt.Load()
	queueAgeSeconds := int64(0)
	if oldestQueuedAt > 0 {
		queueAgeSeconds = max(0, time.Now().Unix()-oldestQueuedAt)
	}
	return ChannelMonitorEventWriterStats{
		QueueDepth:      len(writer.queue),
		QueueCapacity:   cap(writer.queue),
		QueuedEvents:    writer.queuedEvents.Load(),
		DroppedEvents:   writer.droppedEvents.Load(),
		RetryEvents:     writer.retryEvents.Load(),
		OldestQueuedAt:  oldestQueuedAt,
		QueueAgeSeconds: queueAgeSeconds,
	}
}

type ChannelMonitorEventWriterStats struct {
	QueueDepth      int   `json:"queue_depth"`
	QueueCapacity   int   `json:"queue_capacity"`
	QueuedEvents    int64 `json:"queued_events"`
	DroppedEvents   int64 `json:"dropped_events"`
	RetryEvents     int64 `json:"retry_events"`
	OldestQueuedAt  int64 `json:"oldest_queued_at"`
	QueueAgeSeconds int64 `json:"queue_age_seconds"`
}
