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
	channelMonitorEventWriterDefaultWorkerCount   = 1
	channelMonitorEventWriterMaxAttempts          = 3
	channelMonitorEventWriterRetryDelay           = 100 * time.Millisecond
	channelMonitorEventWriterOverflowTimeout      = 250 * time.Millisecond
	channelMonitorEventWriterOutboxLeaseDuration  = 30 * time.Second
	channelMonitorEventWriterOutboxBatchSize      = 100
	channelMonitorEventWriterOutboxPollInterval   = 500 * time.Millisecond
	channelMonitorEventOutboxStoreConcurrency     = 16
)

type channelMonitorEventWriterItem struct {
	event      model.ChannelMonitorEvent
	payload    []byte
	enqueuedAt int64
}

type channelMonitorEventWriterConfig struct {
	QueueCapacity          int
	WorkerCount            int
	MaxAttempts            int
	RetryDelay             time.Duration
	DirectPublishOnFull    bool
	PersistOverflow        bool
	OverflowPublishTimeout time.Duration
}

type channelMonitorEventWriter struct {
	client channelMonitorRedisStreamAppender
	queue  chan channelMonitorEventWriterItem
	config channelMonitorEventWriterConfig

	runCtx       context.Context
	cancelRun    context.CancelFunc
	stopCh       chan struct{}
	doneCh       chan struct{}
	stopOnce     sync.Once
	runOnce      sync.Once
	workerWg     sync.WaitGroup
	runStarted   atomic.Bool
	stopping     atomic.Bool
	outboxCtx    context.Context
	outboxCancel context.CancelFunc
	outboxDone   chan struct{}
	outboxOnce   sync.Once
	outboxMu     sync.Mutex
	outboxOwner  string

	queuedEvents       atomic.Int64
	droppedEvents      atomic.Int64
	retryEvents        atomic.Int64
	outboxStoredEvents atomic.Int64
	outboxFailedEvents atomic.Int64
	oldestQueuedAt     atomic.Int64
}

var channelMonitorEventWriterState struct {
	sync.RWMutex
	writer *channelMonitorEventWriter
}

var channelMonitorEventOutboxStoreSlots = make(chan struct{}, channelMonitorEventOutboxStoreConcurrency)

var ErrChannelMonitorEventOutboxStoreBusy = errors.New("渠道监控事件 outbox 写入并发已满")

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
			WorkerCount: common.GetEnvOrDefault(
				"CHANNEL_MONITOR_EVENT_WRITER_WORKERS",
				channelMonitorEventWriterDefaultWorkerCount,
			),
			MaxAttempts: channelMonitorEventWriterMaxAttempts,
			RetryDelay:  channelMonitorEventWriterRetryDelay,
			DirectPublishOnFull: common.GetEnvOrDefaultBool(
				"CHANNEL_MONITOR_EVENT_WRITER_DIRECT_OVERFLOW",
				true,
			),
			PersistOverflow: common.GetEnvOrDefaultBool(
				"CHANNEL_MONITOR_EVENT_WRITER_PERSIST_OVERFLOW",
				true,
			),
			OverflowPublishTimeout: channelMonitorEventWriterOverflowTimeout,
		},
	)
	channelMonitorEventWriterState.Lock()
	previous := channelMonitorEventWriterState.writer
	channelMonitorEventWriterState.writer = writer
	channelMonitorEventWriterState.Unlock()
	if previous != nil {
		_ = previous.Stop(context.Background())
	}
	writer.startOutbox()
	go writer.run()
	return writer, nil
}

func (writer *channelMonitorEventWriter) startOutbox() {
	if writer == nil || writer.stopping.Load() || !channelMonitorEventOutboxTableReady() {
		return
	}
	writer.outboxMu.Lock()
	defer writer.outboxMu.Unlock()
	if writer.stopping.Load() {
		return
	}
	writer.outboxOnce.Do(func() {
		writer.outboxCtx, writer.outboxCancel = context.WithCancel(context.Background())
		writer.outboxDone = make(chan struct{})
		writer.outboxOwner = "channel-monitor-event-writer:" + common.GetUUID()
		go writer.runOutbox()
	})
}

func (writer *channelMonitorEventWriter) runOutbox() {
	defer close(writer.outboxDone)
	ticker := time.NewTicker(channelMonitorEventWriterOutboxPollInterval)
	defer ticker.Stop()
	for {
		writer.replayOutboxBatch()
		select {
		case <-writer.outboxCtx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (writer *channelMonitorEventWriter) replayOutboxBatch() {
	if writer == nil || writer.outboxCtx == nil || writer.outboxCtx.Err() != nil || !channelMonitorEventOutboxTableReady() {
		return
	}
	now := time.Now().Unix()
	owner := writer.outboxOwner
	claimed, err := model.ClaimChannelMonitorEventOutbox(
		writer.outboxCtx,
		owner,
		now,
		channelMonitorEventWriterOutboxLeaseDuration,
		channelMonitorEventWriterOutboxBatchSize,
	)
	if err != nil || len(claimed) == 0 {
		return
	}
	for _, row := range claimed {
		publishCtx, cancel := context.WithTimeout(writer.outboxCtx, writer.config.OverflowPublishTimeout)
		status, publishErr := publishChannelMonitorEventWithPayload(
			publishCtx,
			writer.client,
			model.ChannelMonitorEvent{EventId: row.EventId},
			[]byte(row.Payload),
		)
		cancel()
		if status == ChannelMonitorEventPublishStatusPublished && publishErr == nil {
			if markErr := model.MarkChannelMonitorEventOutboxProcessed(
				writer.outboxCtx,
				owner,
				[]int64{row.Id},
				time.Now().Unix(),
			); markErr != nil {
				// Leave the row pending when finalization fails. The lease will
				// expire and another worker can recover it; projections dedupe by
				// EventId, so a replay is safe.
				writer.outboxFailedEvents.Add(1)
			}
			continue
		}
		nextAttemptAt := time.Now().Add(writer.config.RetryDelay).Unix()
		if nextAttemptAt <= now {
			nextAttemptAt = now + 1
		}
		writer.retryEvents.Add(1)
		_ = model.FailChannelMonitorEventOutbox(
			writer.outboxCtx,
			owner,
			[]int64{row.Id},
			nextAttemptAt,
			publishErr,
		)
	}
}

func newChannelMonitorEventWriter(
	client channelMonitorRedisStreamAppender,
	config channelMonitorEventWriterConfig,
) *channelMonitorEventWriter {
	if config.QueueCapacity <= 0 {
		config.QueueCapacity = channelMonitorEventWriterDefaultQueueCapacity
	}
	if config.WorkerCount <= 0 {
		config.WorkerCount = channelMonitorEventWriterDefaultWorkerCount
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 1
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = channelMonitorEventWriterRetryDelay
	}
	if config.OverflowPublishTimeout <= 0 {
		config.OverflowPublishTimeout = channelMonitorEventWriterOverflowTimeout
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
	event = event.Clone()
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
	writer := channelMonitorEventWriterState.writer
	if writer != nil {
		item := channelMonitorEventWriterItem{event: event, payload: payload, enqueuedAt: time.Now().Unix()}
		status, enqueueErr := writer.enqueue(item)
		channelMonitorEventWriterState.RUnlock()
		return status, enqueueErr
	}
	channelMonitorEventWriterState.RUnlock()

	// A disabled/stopped writer must not turn an accepted event into a silent
	// loss. The same idempotent DB outbox used by overflow/retry handling is
	// safe to call directly and allows Redis to be brought back independently.
	stored, storeErr := storeChannelMonitorEventOutboxBounded(
		event,
		payload,
		channelMonitorEventWriterOverflowTimeout,
	)
	if stored {
		recordChannelMonitorEventEnqueueStatus(ChannelMonitorEventPublishStatusPersisted, nil)
		return ChannelMonitorEventPublishStatusPersisted, nil
	} else if storeErr != nil {
		recordChannelMonitorEventEnqueueStatus(ChannelMonitorEventPublishStatusDropped, storeErr)
		return ChannelMonitorEventPublishStatusDropped, storeErr
	}
	recordChannelMonitorEventEnqueueStatus(ChannelMonitorEventPublishStatusDropped, ErrChannelMonitorEventRedisUnavailable)
	return ChannelMonitorEventPublishStatusDropped, ErrChannelMonitorEventRedisUnavailable
}

func (writer *channelMonitorEventWriter) enqueue(
	item channelMonitorEventWriterItem,
) (ChannelMonitorEventPublishStatus, error) {
	if writer == nil || writer.stopping.Load() {
		if writer != nil && writer.persistOverflowEnabled() {
			if stored, err := writer.persistOutbox(item); stored {
				recordChannelMonitorEventEnqueueStatus(ChannelMonitorEventPublishStatusPersisted, nil)
				return ChannelMonitorEventPublishStatusPersisted, nil
			} else if err != nil {
				recordChannelMonitorEventEnqueueStatus(ChannelMonitorEventPublishStatusDropped, err)
				return ChannelMonitorEventPublishStatusDropped, err
			}
		}
		recordChannelMonitorEventEnqueueStatus(ChannelMonitorEventPublishStatusDropped, ErrChannelMonitorEventRedisUnavailable)
		return ChannelMonitorEventPublishStatusDropped, ErrChannelMonitorEventRedisUnavailable
	}
	select {
	case writer.queue <- item:
		writer.queuedEvents.Add(1)
		writer.oldestQueuedAt.CompareAndSwap(0, item.enqueuedAt)
		recordChannelMonitorEventEnqueueStatus(ChannelMonitorEventPublishStatusQueued, nil)
		return ChannelMonitorEventPublishStatusQueued, nil
	default:
		if writer.config.DirectPublishOnFull {
			publishCtx, cancel := context.WithTimeout(context.Background(), writer.config.OverflowPublishTimeout)
			status, err := publishChannelMonitorEventWithPayload(publishCtx, writer.client, item.event, item.payload)
			cancel()
			if status == ChannelMonitorEventPublishStatusPublished && err == nil {
				recordChannelMonitorEventEnqueueStatus(status, nil)
				return status, nil
			}
			if err == nil {
				err = ErrChannelMonitorEventRedisUnavailable
			}
			err = fmt.Errorf("%w: %w", ErrChannelMonitorEventWriterQueueFull, err)
			if writer.persistOverflowEnabled() {
				stored, _ := writer.persistOutbox(item)
				if stored {
					recordChannelMonitorEventEnqueueStatus(ChannelMonitorEventPublishStatusPersisted, nil)
					return ChannelMonitorEventPublishStatusPersisted, nil
				}
			}
			writer.droppedEvents.Add(1)
			recordChannelMonitorEventEnqueueStatus(ChannelMonitorEventPublishStatusDropped, err)
			return ChannelMonitorEventPublishStatusDropped, err
		}
		if writer.persistOverflowEnabled() {
			stored, _ := writer.persistOutbox(item)
			if stored {
				recordChannelMonitorEventEnqueueStatus(ChannelMonitorEventPublishStatusPersisted, nil)
				return ChannelMonitorEventPublishStatusPersisted, nil
			}
		}
		writer.droppedEvents.Add(1)
		recordChannelMonitorEventEnqueueStatus(ChannelMonitorEventPublishStatusDropped, ErrChannelMonitorEventWriterQueueFull)
		return ChannelMonitorEventPublishStatusDropped, ErrChannelMonitorEventWriterQueueFull
	}
}

var ErrChannelMonitorEventWriterQueueFull = errors.New("渠道监控事件写入队列已满")

func (writer *channelMonitorEventWriter) run() {
	writer.runOnce.Do(func() {
		writer.runStarted.Store(true)
		writer.workerWg.Add(writer.config.WorkerCount)
		for index := 0; index < writer.config.WorkerCount; index++ {
			go writer.runWorker()
		}
		writer.workerWg.Wait()
		writer.cancelRun()
		close(writer.doneCh)
	})
}

func (writer *channelMonitorEventWriter) runWorker() {
	defer writer.workerWg.Done()
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
			return writer.persistOrDrop(item)
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
			return writer.persistOrDrop(item)
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
			return writer.persistOrDrop(item)
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

func (writer *channelMonitorEventWriter) persistOrDrop(item channelMonitorEventWriterItem) bool {
	if writer.persistOverflowEnabled() {
		stored, _ := writer.persistOutbox(item)
		if stored {
			return true
		}
	}
	writer.dropItem()
	return false
}

func (writer *channelMonitorEventWriter) persistOverflowEnabled() bool {
	// Once a database handle is configured, durability is mandatory. The
	// compatibility flag is retained for callers that still populate it, but
	// it must never re-enable silent loss.
	return writer != nil && model.DB != nil
}

func (writer *channelMonitorEventWriter) persistOutbox(item channelMonitorEventWriterItem) (bool, error) {
	if writer == nil || model.DB == nil {
		return false, ErrChannelMonitorEventRedisUnavailable
	}
	if len(item.payload) == 0 {
		payload, err := item.event.Marshal()
		if err != nil {
			writer.outboxFailedEvents.Add(1)
			return false, err
		}
		item.payload = payload
	}
	inserted, err := storeChannelMonitorEventOutboxBounded(
		item.event,
		item.payload,
		writer.config.OverflowPublishTimeout,
	)
	if err != nil {
		writer.outboxFailedEvents.Add(1)
		return false, err
	}
	if inserted {
		writer.outboxStoredEvents.Add(1)
	}
	return true, nil
}

func storeChannelMonitorEventOutbox(ctx context.Context, event model.ChannelMonitorEvent, payload []byte) (bool, error) {
	if model.DB == nil {
		return false, ErrChannelMonitorEventRedisUnavailable
	}
	// Do not probe the schema on the request/overflow path. Migrator.HasTable
	// issues a separate metadata query and is not context-aware on every
	// dialect, so a locked or unavailable database could make each fallback
	// event pay an extra unbounded wait before the actual insert. The durable
	// model operation below already returns a precise error when migrations are
	// incomplete; callers bound that operation with their own context/slot
	// budget.
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := model.StoreChannelMonitorEventOutbox(ctx, event.EventId, payload)
	if err != nil {
		// Keep the public availability contract stable even when the database
		// handle exists but migrations are incomplete or the table is offline.
		return false, fmt.Errorf("%w: outbox 持久化失败: %v", ErrChannelMonitorEventRedisUnavailable, err)
	}
	return true, nil
}

// storeChannelMonitorEventOutboxBounded keeps outbox durability attempts from
// blocking request or worker paths when the database driver cannot interrupt a
// locked statement promptly on context cancellation. The worker is detached
// only after the timeout; its result channel is buffered so it can finish
// safely once the database becomes available.
func storeChannelMonitorEventOutboxBounded(
	event model.ChannelMonitorEvent,
	payload []byte,
	timeout time.Duration,
) (bool, error) {
	select {
	case channelMonitorEventOutboxStoreSlots <- struct{}{}:
	default:
		return false, ErrChannelMonitorEventOutboxStoreBusy
	}
	if timeout <= 0 {
		timeout = channelMonitorEventWriterOverflowTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	type result struct {
		stored bool
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		defer func() { <-channelMonitorEventOutboxStoreSlots }()
		stored, err := storeChannelMonitorEventOutbox(ctx, event, payload)
		resultCh <- result{stored: stored, err: err}
	}()
	select {
	case value := <-resultCh:
		return value.stored, value.err
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func channelMonitorEventOutboxTableReady() bool {
	if model.DB == nil {
		return false
	}
	return model.DB.Migrator().HasTable(&model.ChannelMonitorEventOutbox{})
}

func (writer *channelMonitorEventWriter) dropQueued() {
	dropped := int64(0)
	for {
		select {
		case item := <-writer.queue:
			stored, _ := writer.persistOutbox(item)
			if !writer.persistOverflowEnabled() || !stored {
				dropped++
			}
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
	// Serialize the stop linearization point with startOutbox.  Marking
	// stopping while holding outboxMu prevents a concurrent startOutbox that
	// has already passed its initial check from launching the replay goroutine
	// after Stop has begun.  No code acquires channelMonitorEventWriterState
	// while holding outboxMu, so taking these locks in this order avoids an
	// inversion with enqueue/start paths.
	writer.outboxMu.Lock()
	writer.stopOnce.Do(func() {
		channelMonitorEventWriterState.Lock()
		if channelMonitorEventWriterState.writer == writer {
			channelMonitorEventWriterState.writer = nil
		}
		writer.stopping.Store(true)
		close(writer.stopCh)
		if writer.outboxCancel != nil {
			writer.outboxCancel()
		}
		channelMonitorEventWriterState.Unlock()
	})
	// Capture the channel while outboxMu is held.  This avoids a data race with
	// startOutbox assigning outboxDone and ensures a Stop concurrent with Start
	// waits for a replay goroutine that was started before the stop point.
	outboxDone := writer.outboxDone
	writer.outboxMu.Unlock()
	if !writer.runStarted.Load() {
		go writer.run()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-writer.doneCh:
		if outboxDone != nil {
			select {
			case <-outboxDone:
			case <-ctx.Done():
				return fmt.Errorf("渠道监控事件 outbox 停止超时: %w", ctx.Err())
			}
		}
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
		QueueDepth:         len(writer.queue),
		QueueCapacity:      cap(writer.queue),
		QueuedEvents:       writer.queuedEvents.Load(),
		DroppedEvents:      writer.droppedEvents.Load(),
		RetryEvents:        writer.retryEvents.Load(),
		OldestQueuedAt:     oldestQueuedAt,
		QueueAgeSeconds:    queueAgeSeconds,
		WorkerCount:        writer.config.WorkerCount,
		OutboxStoredEvents: writer.outboxStoredEvents.Load(),
		OutboxFailedEvents: writer.outboxFailedEvents.Load(),
	}
}

type ChannelMonitorEventWriterStats struct {
	QueueDepth         int   `json:"queue_depth"`
	QueueCapacity      int   `json:"queue_capacity"`
	QueuedEvents       int64 `json:"queued_events"`
	DroppedEvents      int64 `json:"dropped_events"`
	RetryEvents        int64 `json:"retry_events"`
	OldestQueuedAt     int64 `json:"oldest_queued_at"`
	QueueAgeSeconds    int64 `json:"queue_age_seconds"`
	WorkerCount        int   `json:"worker_count"`
	OutboxStoredEvents int64 `json:"outbox_stored_events"`
	OutboxFailedEvents int64 `json:"outbox_failed_events"`
}
