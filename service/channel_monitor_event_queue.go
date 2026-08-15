package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/model"
)

const (
	channelMonitorEventQueueCapacity       = 4096
	channelMonitorEventMaxBatchSize        = 256
	channelMonitorEventFlushInterval       = time.Second
	channelMonitorEventConsumerTimeout     = 2 * time.Second
	channelMonitorEventConsumerMaxAttempts = 3
	channelMonitorEventConsumerRetryDelay  = 100 * time.Millisecond
	channelMonitorEventDedupCapacity       = 16384
)

var errChannelMonitorEventConsumerUnavailable = errors.New("渠道监控事件消费者未配置")

type ChannelMonitorEventEnqueueStatus string

const (
	ChannelMonitorEventEnqueueAccepted ChannelMonitorEventEnqueueStatus = "accepted"
	ChannelMonitorEventEnqueueInvalid  ChannelMonitorEventEnqueueStatus = "invalid"
	ChannelMonitorEventEnqueueFull     ChannelMonitorEventEnqueueStatus = "queue_full"
	ChannelMonitorEventEnqueueStopped  ChannelMonitorEventEnqueueStatus = "stopped"
)

type ChannelMonitorEventQueueStats struct {
	Capacity            int   `json:"capacity"`
	QueueDepth          int   `json:"queue_depth"`
	PendingEvents       int64 `json:"pending_events"`
	AcceptedEvents      int64 `json:"accepted_events"`
	ProcessedEvents     int64 `json:"processed_events"`
	DuplicateEvents     int64 `json:"duplicate_events"`
	InvalidEvents       int64 `json:"invalid_events"`
	DroppedEvents       int64 `json:"dropped_events"`
	QueueFullDrops      int64 `json:"queue_full_drops"`
	StoppedDrops        int64 `json:"stopped_drops"`
	FailedEvents        int64 `json:"failed_events"`
	ProcessedBatches    int64 `json:"processed_batches"`
	ConsumerErrors      int64 `json:"consumer_errors"`
	ConsumerRetries     int64 `json:"consumer_retries"`
	LastAcceptedAt      int64 `json:"last_accepted_at"`
	LastProcessedAt     int64 `json:"last_processed_at"`
	LastConsumerErrorAt int64 `json:"last_consumer_error_at"`
}

type channelMonitorEventQueueConfig struct {
	Capacity        int
	MaxBatchSize    int
	FlushInterval   time.Duration
	ConsumerTimeout time.Duration
	MaxAttempts     int
	RetryDelay      time.Duration
	DedupCapacity   int
}

// ChannelMonitorEventBatchConsumer must apply event IDs idempotently. The
// queue filters recent duplicates, while the eventual durable projection is
// responsible for idempotency beyond the bounded in-memory cache.
type ChannelMonitorEventBatchConsumer func(context.Context, []model.ChannelMonitorEvent) error

type channelMonitorEventConsumerHolder struct {
	consume ChannelMonitorEventBatchConsumer
}

type channelMonitorEventFlushRequest struct {
	result chan error
}

type channelMonitorEventDedupCache struct {
	ids   map[string]struct{}
	order []string
	next  int
}

type channelMonitorEventQueue struct {
	config        channelMonitorEventQueueConfig
	events        chan model.ChannelMonitorEvent
	flushCh       chan channelMonitorEventFlushRequest
	consumerReady chan struct{}
	stopCh        chan struct{}
	doneCh        chan struct{}

	gate     sync.RWMutex
	stopped  bool
	stopOnce sync.Once

	resultMu    sync.Mutex
	shutdownErr error

	sequence atomic.Uint64
	consumer atomic.Pointer[channelMonitorEventConsumerHolder]

	acceptedEvents      atomic.Int64
	processedEvents     atomic.Int64
	duplicateEvents     atomic.Int64
	invalidEvents       atomic.Int64
	droppedEvents       atomic.Int64
	queueFullDrops      atomic.Int64
	stoppedDrops        atomic.Int64
	failedEvents        atomic.Int64
	processedBatches    atomic.Int64
	consumerErrors      atomic.Int64
	consumerRetries     atomic.Int64
	pendingEvents       atomic.Int64
	lastAcceptedAt      atomic.Int64
	lastProcessedAt     atomic.Int64
	lastConsumerErrorAt atomic.Int64
}

func defaultChannelMonitorEventQueueConfig() channelMonitorEventQueueConfig {
	return channelMonitorEventQueueConfig{
		Capacity:        channelMonitorEventQueueCapacity,
		MaxBatchSize:    channelMonitorEventMaxBatchSize,
		FlushInterval:   channelMonitorEventFlushInterval,
		ConsumerTimeout: channelMonitorEventConsumerTimeout,
		MaxAttempts:     channelMonitorEventConsumerMaxAttempts,
		RetryDelay:      channelMonitorEventConsumerRetryDelay,
		DedupCapacity:   channelMonitorEventDedupCapacity,
	}
}

func newChannelMonitorEventQueue(config channelMonitorEventQueueConfig, consume ChannelMonitorEventBatchConsumer) *channelMonitorEventQueue {
	if config.Capacity <= 0 {
		config.Capacity = channelMonitorEventQueueCapacity
	}
	if config.MaxBatchSize <= 0 {
		config.MaxBatchSize = channelMonitorEventMaxBatchSize
	}
	if config.MaxBatchSize > config.Capacity {
		config.MaxBatchSize = config.Capacity
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = channelMonitorEventFlushInterval
	}
	if config.ConsumerTimeout <= 0 {
		config.ConsumerTimeout = channelMonitorEventConsumerTimeout
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 1
	}
	if config.DedupCapacity <= 0 {
		config.DedupCapacity = channelMonitorEventDedupCapacity
	}
	queue := &channelMonitorEventQueue{
		config:        config,
		events:        make(chan model.ChannelMonitorEvent, config.Capacity),
		flushCh:       make(chan channelMonitorEventFlushRequest),
		consumerReady: make(chan struct{}, 1),
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}
	if consume != nil {
		queue.consumer.Store(&channelMonitorEventConsumerHolder{consume: consume})
	}
	go queue.run()
	return queue
}

func (queue *channelMonitorEventQueue) setConsumer(consume ChannelMonitorEventBatchConsumer) bool {
	if consume == nil {
		return false
	}
	queue.gate.RLock()
	defer queue.gate.RUnlock()
	if queue.stopped {
		return false
	}
	queue.consumer.Store(&channelMonitorEventConsumerHolder{consume: consume})
	select {
	case queue.consumerReady <- struct{}{}:
	default:
	}
	return true
}

func (queue *channelMonitorEventQueue) enqueue(event model.ChannelMonitorEvent) ChannelMonitorEventEnqueueStatus {
	if err := event.Validate(); err != nil {
		queue.invalidEvents.Add(1)
		queue.droppedEvents.Add(1)
		return ChannelMonitorEventEnqueueInvalid
	}
	if event.EventSequence == 0 {
		event.EventSequence = queue.sequence.Add(1)
	} else {
		queue.advanceSequence(event.EventSequence)
	}
	event = event.Clone()

	queue.gate.RLock()
	defer queue.gate.RUnlock()
	if queue.stopped {
		queue.stoppedDrops.Add(1)
		queue.droppedEvents.Add(1)
		return ChannelMonitorEventEnqueueStopped
	}
	queue.pendingEvents.Add(1)
	select {
	case queue.events <- event:
		queue.acceptedEvents.Add(1)
		queue.lastAcceptedAt.Store(time.Now().Unix())
		return ChannelMonitorEventEnqueueAccepted
	default:
		queue.pendingEvents.Add(-1)
		// The request path never waits for capacity. The new event is dropped,
		// existing queued events keep their order, and the drop counters expose
		// the degraded realtime projection without reactivating an old data path.
		queue.queueFullDrops.Add(1)
		queue.droppedEvents.Add(1)
		return ChannelMonitorEventEnqueueFull
	}
}

func (queue *channelMonitorEventQueue) advanceSequence(value uint64) {
	for {
		current := queue.sequence.Load()
		if current >= value || queue.sequence.CompareAndSwap(current, value) {
			return
		}
	}
}

func (queue *channelMonitorEventQueue) run() {
	defer close(queue.doneCh)
	ticker := time.NewTicker(queue.config.FlushInterval)
	defer ticker.Stop()

	batch := make([]model.ChannelMonitorEvent, 0, queue.config.MaxBatchSize)
	dedup := channelMonitorEventDedupCache{
		ids:   make(map[string]struct{}, queue.config.DedupCapacity),
		order: make([]string, 0, queue.config.DedupCapacity),
	}
	var lastErr error

	process := func() {
		if len(batch) == 0 {
			return
		}
		if err := queue.processBatch(batch, &dedup); err != nil {
			lastErr = err
		}
		batch = batch[:0]
	}
	drain := func() {
		for {
			select {
			case event := <-queue.events:
				batch = append(batch, event)
				if len(batch) >= queue.config.MaxBatchSize {
					process()
				}
			default:
				process()
				return
			}
		}
	}

	for {
		if queue.consumer.Load() == nil {
			select {
			case <-queue.consumerReady:
				continue
			case request := <-queue.flushCh:
				if queue.pendingEvents.Load() == 0 {
					request.result <- nil
				} else {
					request.result <- errChannelMonitorEventConsumerUnavailable
				}
			case <-queue.stopCh:
				pending := queue.pendingEvents.Swap(0)
				if pending > 0 {
					queue.failedEvents.Add(pending)
					queue.droppedEvents.Add(pending)
					lastErr = errChannelMonitorEventConsumerUnavailable
				}
				queue.resultMu.Lock()
				queue.shutdownErr = lastErr
				queue.resultMu.Unlock()
				return
			}
			continue
		}
		select {
		case event := <-queue.events:
			batch = append(batch, event)
			if len(batch) >= queue.config.MaxBatchSize {
				process()
			}
		case <-ticker.C:
			process()
		case request := <-queue.flushCh:
			drain()
			request.result <- lastErr
			lastErr = nil
		case <-queue.stopCh:
			drain()
			queue.resultMu.Lock()
			queue.shutdownErr = lastErr
			queue.resultMu.Unlock()
			return
		}
	}
}

func (queue *channelMonitorEventQueue) processBatch(batch []model.ChannelMonitorEvent, dedup *channelMonitorEventDedupCache) error {
	unique := make([]model.ChannelMonitorEvent, 0, len(batch))
	batchIds := make(map[string]struct{}, len(batch))
	for _, event := range batch {
		if _, exists := dedup.ids[event.EventId]; exists {
			queue.duplicateEvents.Add(1)
			queue.pendingEvents.Add(-1)
			continue
		}
		if _, exists := batchIds[event.EventId]; exists {
			queue.duplicateEvents.Add(1)
			queue.pendingEvents.Add(-1)
			continue
		}
		batchIds[event.EventId] = struct{}{}
		unique = append(unique, event)
	}
	if len(unique) == 0 {
		return nil
	}

	var err error
	for attempt := 0; attempt < queue.config.MaxAttempts; attempt++ {
		consumer := queue.consumer.Load()
		if consumer == nil {
			err = errChannelMonitorEventConsumerUnavailable
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), queue.config.ConsumerTimeout)
		err = consumer.consume(ctx, unique)
		cancel()
		if err == nil {
			queue.processedBatches.Add(1)
			queue.processedEvents.Add(int64(len(unique)))
			queue.pendingEvents.Add(-int64(len(unique)))
			queue.lastProcessedAt.Store(time.Now().Unix())
			queue.rememberEventIds(unique, dedup)
			return nil
		}
		queue.consumerErrors.Add(1)
		queue.lastConsumerErrorAt.Store(time.Now().Unix())
		if attempt+1 < queue.config.MaxAttempts {
			queue.consumerRetries.Add(1)
			if queue.config.RetryDelay > 0 {
				time.Sleep(queue.config.RetryDelay)
			}
		}
	}
	queue.failedEvents.Add(int64(len(unique)))
	queue.pendingEvents.Add(-int64(len(unique)))
	return err
}

func (queue *channelMonitorEventQueue) rememberEventIds(events []model.ChannelMonitorEvent, dedup *channelMonitorEventDedupCache) {
	for _, event := range events {
		if len(dedup.order) < queue.config.DedupCapacity {
			dedup.ids[event.EventId] = struct{}{}
			dedup.order = append(dedup.order, event.EventId)
			continue
		}
		delete(dedup.ids, dedup.order[dedup.next])
		dedup.ids[event.EventId] = struct{}{}
		dedup.order[dedup.next] = event.EventId
		dedup.next = (dedup.next + 1) % queue.config.DedupCapacity
	}
}

func (queue *channelMonitorEventQueue) flush(ctx context.Context) error {
	request := channelMonitorEventFlushRequest{result: make(chan error, 1)}
	select {
	case queue.flushCh <- request:
	case <-queue.doneCh:
		return queue.shutdownResult()
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-request.result:
		return err
	case <-queue.doneCh:
		return queue.shutdownResult()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (queue *channelMonitorEventQueue) stop(ctx context.Context) error {
	queue.stopOnce.Do(func() {
		queue.gate.Lock()
		queue.stopped = true
		close(queue.stopCh)
		queue.gate.Unlock()
	})
	select {
	case <-queue.doneCh:
		return queue.shutdownResult()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (queue *channelMonitorEventQueue) shutdownResult() error {
	queue.resultMu.Lock()
	defer queue.resultMu.Unlock()
	return queue.shutdownErr
}

func (queue *channelMonitorEventQueue) stats() ChannelMonitorEventQueueStats {
	return ChannelMonitorEventQueueStats{
		Capacity:            queue.config.Capacity,
		QueueDepth:          len(queue.events),
		PendingEvents:       queue.pendingEvents.Load(),
		AcceptedEvents:      queue.acceptedEvents.Load(),
		ProcessedEvents:     queue.processedEvents.Load(),
		DuplicateEvents:     queue.duplicateEvents.Load(),
		InvalidEvents:       queue.invalidEvents.Load(),
		DroppedEvents:       queue.droppedEvents.Load(),
		QueueFullDrops:      queue.queueFullDrops.Load(),
		StoppedDrops:        queue.stoppedDrops.Load(),
		FailedEvents:        queue.failedEvents.Load(),
		ProcessedBatches:    queue.processedBatches.Load(),
		ConsumerErrors:      queue.consumerErrors.Load(),
		ConsumerRetries:     queue.consumerRetries.Load(),
		LastAcceptedAt:      queue.lastAcceptedAt.Load(),
		LastProcessedAt:     queue.lastProcessedAt.Load(),
		LastConsumerErrorAt: queue.lastConsumerErrorAt.Load(),
	}
}

var (
	channelMonitorEventQueueMu sync.RWMutex
	channelMonitorEvents       = newChannelMonitorEventQueue(defaultChannelMonitorEventQueueConfig(), nil)
)

// EmitChannelMonitorEvent only validates and enqueues an event in memory. It
// never performs database or network I/O and never waits for queue capacity.
func EmitChannelMonitorEvent(event model.ChannelMonitorEvent) ChannelMonitorEventEnqueueStatus {
	channelMonitorEventQueueMu.RLock()
	status := channelMonitorEvents.enqueue(event)
	channelMonitorEventQueueMu.RUnlock()
	return status
}

func GetChannelMonitorEventQueueStats() ChannelMonitorEventQueueStats {
	channelMonitorEventQueueMu.RLock()
	stats := channelMonitorEvents.stats()
	channelMonitorEventQueueMu.RUnlock()
	return stats
}

func FlushChannelMonitorEvents(ctx context.Context) error {
	channelMonitorEventQueueMu.RLock()
	err := channelMonitorEvents.flush(ctx)
	channelMonitorEventQueueMu.RUnlock()
	return err
}

func ShutdownChannelMonitorEventQueue(ctx context.Context) error {
	channelMonitorEventQueueMu.RLock()
	err := channelMonitorEvents.stop(ctx)
	channelMonitorEventQueueMu.RUnlock()
	return err
}

// SetChannelMonitorEventConsumer installs or replaces the asynchronous batch
// consumer without discarding events that are already queued.
func SetChannelMonitorEventConsumer(consume ChannelMonitorEventBatchConsumer) bool {
	channelMonitorEventQueueMu.RLock()
	configured := channelMonitorEvents.setConsumer(consume)
	channelMonitorEventQueueMu.RUnlock()
	return configured
}

func resetChannelMonitorEventQueueForTest(config channelMonitorEventQueueConfig, consume ChannelMonitorEventBatchConsumer) {
	replacement := newChannelMonitorEventQueue(config, consume)
	channelMonitorEventQueueMu.Lock()
	previous := channelMonitorEvents
	channelMonitorEvents = replacement
	channelMonitorEventQueueMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = previous.stop(ctx)
}
