package model

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCM08DirtyRoutingConcurrentReadsDoNotFallbackToDatabase(t *testing.T) {
	db := setupDirtyLogicalSelectionTest(t)
	seedDirtyLogicalSelectionTest(t, db)
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, StopChannelSmartScheduleRefreshWorker(stopCtx))
	cancel()

	InvalidateLogicalChannelRuntimeCache()
	var queryCount atomic.Int64
	callbackName := "test:cm08_dirty_routing_no_db_fallback"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(*gorm.DB) {
		queryCount.Add(1)
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	const readers = 16
	start := make(chan struct{})
	results := make(chan int, readers)
	errs := make(chan error, readers)
	var wait sync.WaitGroup
	wait.Add(readers)
	for range readers {
		go func() {
			defer wait.Done()
			<-start
			selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, "")
			if err != nil {
				errs <- err
				return
			}
			if selected == nil {
				errs <- errors.New("dirty routing returned no channel")
				return
			}
			results <- selected.Id
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	for channelID := range results {
		assert.Equal(t, dirtySelectionOldMember, channelID)
	}
	assert.Zero(t, queryCount.Load(), "dirty request reads must serve the last complete snapshot without DB fallback")
}

func TestCM08RefreshQueueCoalescesConcurrentIdenticalDirtyEvents(t *testing.T) {
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, StopChannelSmartScheduleRefreshWorker(stopCtx))
	cancel()

	queue := make(chan channelSmartScheduleRefreshKey, 32)
	channelSmartScheduleRefreshWorker.mu.Lock()
	require.False(t, channelSmartScheduleRefreshWorker.started)
	channelSmartScheduleRefreshWorker.started = true
	channelSmartScheduleRefreshWorker.stopping = false
	channelSmartScheduleRefreshWorker.queue = queue
	channelSmartScheduleRefreshWorker.stop = make(chan struct{})
	channelSmartScheduleRefreshWorker.done = make(chan struct{})
	channelSmartScheduleRefreshWorker.pending = make(map[channelSmartScheduleRefreshKey]struct{})
	channelSmartScheduleRefreshWorker.mu.Unlock()
	t.Cleanup(func() {
		channelSmartScheduleRefreshWorker.mu.Lock()
		channelSmartScheduleRefreshWorker.started = false
		channelSmartScheduleRefreshWorker.stopping = false
		channelSmartScheduleRefreshWorker.queue = nil
		channelSmartScheduleRefreshWorker.stop = nil
		channelSmartScheduleRefreshWorker.done = nil
		channelSmartScheduleRefreshWorker.pending = nil
		channelSmartScheduleRefreshWorker.mu.Unlock()
	})

	key := channelSmartScheduleRefreshKey{group: "vip", model: "model-a"}
	const publishers = 16
	start := make(chan struct{})
	accepted := make(chan bool, publishers)
	var wait sync.WaitGroup
	wait.Add(publishers)
	for range publishers {
		go func() {
			defer wait.Done()
			<-start
			accepted <- enqueueChannelSmartScheduleRefresh(key)
		}()
	}
	close(start)
	wait.Wait()
	close(accepted)

	for ok := range accepted {
		assert.True(t, ok)
	}
	require.Len(t, queue, 1, "identical dirty events must produce one queued rebuild")
	assert.Equal(t, key, <-queue)
	channelSmartScheduleRefreshWorker.mu.Lock()
	assert.Len(t, channelSmartScheduleRefreshWorker.pending, 1)
	_, pending := channelSmartScheduleRefreshWorker.pending[key]
	channelSmartScheduleRefreshWorker.mu.Unlock()
	assert.True(t, pending)
}
