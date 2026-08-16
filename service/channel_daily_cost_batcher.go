package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

const (
	channelDailyCostMaxPending    = 4096
	channelDailyCostMaxBatchSize  = 256
	channelDailyCostFlushInterval = time.Second
	channelDailyCostDBTimeout     = 2 * time.Second
	channelDailyCostMaxAttempts   = 3
	channelDailyCostRetryDelay    = 100 * time.Millisecond
)

type channelDailyCostBatcherConfig struct {
	MaxPending    int
	MaxBatchSize  int
	FlushInterval time.Duration
	DBTimeout     time.Duration
	MaxAttempts   int
	RetryDelay    time.Duration
	AutoFlush     bool
}

type channelDailyCostBatchWriter func(context.Context, []model.ChannelDailyCostDelta) error

type channelDailyCostEnqueueResult int

const (
	channelDailyCostEnqueueAccepted channelDailyCostEnqueueResult = iota
	channelDailyCostEnqueueInvalid
	channelDailyCostEnqueueStopped
	channelDailyCostEnqueueFull
	channelDailyCostEnqueueOverflow
)

type channelDailyCostAggregateKey struct {
	ChannelId      int
	DayStart       int64
	KeyFingerprint string
}

type channelDailyCostBatcher struct {
	config channelDailyCostBatcherConfig
	write  channelDailyCostBatchWriter

	mu           sync.Mutex
	pending      map[channelDailyCostAggregateKey]model.ChannelDailyCostDelta
	retryBatch   []model.ChannelDailyCostDelta
	lastErrorLog time.Time
	stopped      bool

	flushMu  sync.Mutex
	wake     chan struct{}
	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once
}

func defaultChannelDailyCostBatcherConfig() channelDailyCostBatcherConfig {
	return channelDailyCostBatcherConfig{
		MaxPending:    channelDailyCostMaxPending,
		MaxBatchSize:  channelDailyCostMaxBatchSize,
		FlushInterval: channelDailyCostFlushInterval,
		DBTimeout:     channelDailyCostDBTimeout,
		MaxAttempts:   channelDailyCostMaxAttempts,
		RetryDelay:    channelDailyCostRetryDelay,
		AutoFlush:     true,
	}
}

func newChannelDailyCostBatcher(config channelDailyCostBatcherConfig, write channelDailyCostBatchWriter) *channelDailyCostBatcher {
	if config.MaxPending <= 0 {
		config.MaxPending = channelDailyCostMaxPending
	}
	if config.MaxBatchSize <= 0 {
		config.MaxBatchSize = channelDailyCostMaxBatchSize
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = channelDailyCostFlushInterval
	}
	if config.DBTimeout <= 0 {
		config.DBTimeout = channelDailyCostDBTimeout
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 1
	}
	if write == nil {
		write = model.AddChannelDailyCostBatch
	}
	batcher := &channelDailyCostBatcher{
		config:  config,
		write:   write,
		pending: make(map[channelDailyCostAggregateKey]model.ChannelDailyCostDelta, config.MaxPending),
		wake:    make(chan struct{}, 1),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	if config.AutoFlush {
		go batcher.run()
	} else {
		close(batcher.doneCh)
	}
	return batcher
}

func (b *channelDailyCostBatcher) enqueue(delta model.ChannelDailyCostDelta) bool {
	return b.enqueueResult(delta) == channelDailyCostEnqueueAccepted
}

func (b *channelDailyCostBatcher) enqueueResult(delta model.ChannelDailyCostDelta) channelDailyCostEnqueueResult {
	if !channelDailyCostDeltaIsValid(delta) {
		return channelDailyCostEnqueueInvalid
	}
	key := channelDailyCostAggregateKey{
		ChannelId:      delta.ChannelId,
		DayStart:       model.ChannelDailyCostDayStart(delta.OccurredAt),
		KeyFingerprint: delta.KeyFingerprint,
	}

	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return channelDailyCostEnqueueStopped
	}
	if current, exists := b.pending[key]; exists {
		if current.CostNanoCNY > math.MaxInt64-delta.CostNanoCNY || current.ProbeCostNanoCNY > math.MaxInt64-delta.ProbeCostNanoCNY || current.SettledDelta > math.MaxInt64-delta.SettledDelta || current.UnresolvedDelta > math.MaxInt64-delta.UnresolvedDelta {
			b.mu.Unlock()
			return channelDailyCostEnqueueOverflow
		}
		current.CostNanoCNY += delta.CostNanoCNY
		current.ProbeCostNanoCNY += delta.ProbeCostNanoCNY
		current.SettledDelta += delta.SettledDelta
		current.UnresolvedDelta += delta.UnresolvedDelta
		if delta.OccurredAt >= current.OccurredAt {
			current.OccurredAt = delta.OccurredAt
			current.APIKeyId = delta.APIKeyId
			current.APIKeyName = delta.APIKeyName
			current.KeyDisplay = delta.KeyDisplay
		}
		b.pending[key] = current
		shouldFlush := len(b.pending) >= b.config.MaxBatchSize
		b.mu.Unlock()
		if shouldFlush {
			b.signal()
		}
		return channelDailyCostEnqueueAccepted
	}
	if len(b.pending) >= b.config.MaxPending {
		b.mu.Unlock()
		b.signal()
		return channelDailyCostEnqueueFull
	}
	b.pending[key] = delta
	shouldFlush := len(b.pending) >= b.config.MaxBatchSize
	b.mu.Unlock()
	if shouldFlush {
		b.signal()
	}
	return channelDailyCostEnqueueAccepted
}

func channelDailyCostDeltaIsValid(delta model.ChannelDailyCostDelta) bool {
	return delta.ChannelId > 0 &&
		delta.CostNanoCNY >= 0 &&
		delta.ProbeCostNanoCNY >= 0 &&
		delta.ProbeCostNanoCNY <= delta.CostNanoCNY &&
		delta.SettledDelta >= 0 &&
		delta.UnresolvedDelta >= 0 &&
		(delta.SettledDelta > 0 || delta.UnresolvedDelta > 0)
}

func (b *channelDailyCostBatcher) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *channelDailyCostBatcher) run() {
	defer close(b.doneCh)
	ticker := time.NewTicker(b.config.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			err := b.flushOne()
			if err != nil {
				b.reportFlushError(err)
			}
			if err == nil {
				b.signalIfPending()
			}
		case <-b.wake:
			err := b.flushOne()
			if err != nil {
				b.reportFlushError(err)
			}
			if err == nil {
				b.signalIfPending()
			}
		case <-b.stopCh:
			return
		}
	}
}

func (b *channelDailyCostBatcher) flushOne() error {
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
	batch := b.takeBatch()
	if len(batch) == 0 {
		return nil
	}
	err := b.writeWithRetry(batch)
	if err != nil {
		b.requeueBatch(batch)
	}
	return err
}

func (b *channelDailyCostBatcher) flushAll() error {
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
	for {
		batch := b.takeBatch()
		if len(batch) == 0 {
			return nil
		}
		if err := b.writeWithRetry(batch); err != nil {
			b.requeueBatch(batch)
			return err
		}
	}
}

func (b *channelDailyCostBatcher) requeueBatch(batch []model.ChannelDailyCostDelta) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Keep a failed database batch intact. Merging it back into newer pending
	// events can overflow an aggregate and silently discard the older charge.
	b.retryBatch = batch
}

func (b *channelDailyCostBatcher) takeBatch() []model.ChannelDailyCostDelta {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.retryBatch) > 0 {
		batch := b.retryBatch
		b.retryBatch = nil
		return batch
	}
	limit := min(len(b.pending), b.config.MaxBatchSize)
	if limit == 0 {
		return nil
	}
	batch := make([]model.ChannelDailyCostDelta, 0, limit)
	for key, delta := range b.pending {
		batch = append(batch, delta)
		delete(b.pending, key)
		if len(batch) == limit {
			break
		}
	}
	return batch
}

func (b *channelDailyCostBatcher) writeSynchronously(delta model.ChannelDailyCostDelta) error {
	b.mu.Lock()
	stopped := b.stopped
	b.mu.Unlock()
	if stopped {
		return errors.New("渠道每日成本批处理器已停止")
	}
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
	return b.writeWithRetry([]model.ChannelDailyCostDelta{delta})
}

func (b *channelDailyCostBatcher) writeWithRetry(batch []model.ChannelDailyCostDelta) error {
	var err error
	for attempt := 0; attempt < b.config.MaxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), b.config.DBTimeout)
		err = b.write(ctx, batch)
		cancel()
		if err == nil {
			return nil
		}
		if attempt+1 < b.config.MaxAttempts && b.config.RetryDelay > 0 {
			timer := time.NewTimer(b.config.RetryDelay)
			select {
			case <-timer.C:
			case <-b.stopCh:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return errors.New("渠道每日成本批处理器已停止")
			}
		}
	}
	return err
}

func (b *channelDailyCostBatcher) signalIfPending() {
	b.mu.Lock()
	hasPending := len(b.retryBatch) > 0 || len(b.pending) > 0
	b.mu.Unlock()
	if hasPending {
		b.signal()
	}
}

func (b *channelDailyCostBatcher) reportFlushError(err error) {
	b.mu.Lock()
	now := time.Now()
	if !b.lastErrorLog.IsZero() && now.Sub(b.lastErrorLog) < time.Minute {
		b.mu.Unlock()
		return
	}
	b.lastErrorLog = now
	b.mu.Unlock()
	logger.LogError(context.Background(), fmt.Sprintf("批量记录渠道每日成本失败，当前批次已保留等待重试: %s", err.Error()))
}

func (b *channelDailyCostBatcher) pendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.retryBatch) + len(b.pending)
}

func (b *channelDailyCostBatcher) stop() {
	b.stopOnce.Do(func() {
		b.mu.Lock()
		b.stopped = true
		b.mu.Unlock()
		close(b.stopCh)
		<-b.doneCh
	})
}

var (
	channelDailyCostBatcherMu sync.RWMutex
	dailyCostBatcher          = newChannelDailyCostBatcher(defaultChannelDailyCostBatcherConfig(), model.AddChannelDailyCostBatch)
)

func enqueueChannelDailyCost(delta model.ChannelDailyCostDelta) bool {
	if !channelDailyCostDeltaIsValid(delta) {
		return false
	}
	channelDailyCostBatcherMu.RLock()
	batcher := dailyCostBatcher
	result := batcher.enqueueResult(delta)
	accepted := result == channelDailyCostEnqueueAccepted
	if result == channelDailyCostEnqueueFull {
		accepted = batcher.writeSynchronously(delta) == nil
	}
	channelDailyCostBatcherMu.RUnlock()
	return accepted
}

func writeChannelDailyCostSynchronously(delta model.ChannelDailyCostDelta) bool {
	if !channelDailyCostDeltaIsValid(delta) {
		return false
	}
	channelDailyCostBatcherMu.RLock()
	batcher := dailyCostBatcher
	err := batcher.writeSynchronously(delta)
	channelDailyCostBatcherMu.RUnlock()
	return err == nil
}

func resetChannelDailyCostBatcherForTest(config channelDailyCostBatcherConfig, write channelDailyCostBatchWriter) {
	replacement := newChannelDailyCostBatcher(config, write)
	channelDailyCostBatcherMu.Lock()
	previous := dailyCostBatcher
	dailyCostBatcher = replacement
	channelDailyCostBatcherMu.Unlock()
	previous.stop()
}

func flushChannelDailyCostEventsForTest() error {
	return FlushChannelDailyCostEvents()
}

// FlushChannelDailyCostEvents persists all currently buffered channel cost
// events. It is called during graceful shutdown before the database closes.
func FlushChannelDailyCostEvents() error {
	channelDailyCostBatcherMu.RLock()
	batcher := dailyCostBatcher
	err := batcher.flushAll()
	channelDailyCostBatcherMu.RUnlock()
	return err
}

func pendingChannelDailyCostEventsForTest() int {
	channelDailyCostBatcherMu.RLock()
	batcher := dailyCostBatcher
	pending := batcher.pendingCount()
	channelDailyCostBatcherMu.RUnlock()
	return pending
}
