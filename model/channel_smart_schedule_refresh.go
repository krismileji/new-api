package model

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// channelSmartScheduleRefreshKey identifies one independently rebuildable
// smart-schedule route pool. The runtime key is kept separate so relation
// changes can be coalesced into one complete snapshot rebuild.
type channelSmartScheduleRefreshKey struct {
	runtime bool
	group   string
	model   string
}

const channelSmartScheduleRefreshQueueCapacity = 1024

var channelSmartScheduleRefreshWorker struct {
	mu       sync.Mutex
	started  bool
	stopping bool
	queue    chan channelSmartScheduleRefreshKey
	stop     chan struct{}
	done     chan struct{}
	cancel   context.CancelFunc
	pending  map[channelSmartScheduleRefreshKey]struct{}
}

// StartChannelSmartScheduleRefreshWorker starts the process-local background
// refresher. It is safe to call more than once; callers normally start it
// immediately after the initial channel cache has been loaded.
func StartChannelSmartScheduleRefreshWorker() {
	channelSmartScheduleRefreshWorker.mu.Lock()
	if channelSmartScheduleRefreshWorker.started {
		channelSmartScheduleRefreshWorker.mu.Unlock()
		return
	}
	queue := make(chan channelSmartScheduleRefreshKey, channelSmartScheduleRefreshQueueCapacity)
	stop := make(chan struct{})
	done := make(chan struct{})
	workerContext, cancel := context.WithCancel(context.Background())
	channelSmartScheduleRefreshWorker.started = true
	channelSmartScheduleRefreshWorker.stopping = false
	channelSmartScheduleRefreshWorker.queue = queue
	channelSmartScheduleRefreshWorker.stop = stop
	channelSmartScheduleRefreshWorker.done = done
	channelSmartScheduleRefreshWorker.cancel = cancel
	channelSmartScheduleRefreshWorker.pending = make(map[channelSmartScheduleRefreshKey]struct{})
	channelSmartScheduleRefreshWorker.mu.Unlock()

	go runChannelSmartScheduleRefreshWorker(workerContext, queue, stop, done)
	queueDirtyChannelSmartScheduleRefreshes()
}

// StopChannelSmartScheduleRefreshWorker stops the refresher and waits for the
// current rebuild to finish. A caller-provided timeout prevents shutdown from
// waiting forever on a database that is already unavailable.
func StopChannelSmartScheduleRefreshWorker(ctx context.Context) error {
	channelSmartScheduleRefreshWorker.mu.Lock()
	if !channelSmartScheduleRefreshWorker.started {
		channelSmartScheduleRefreshWorker.mu.Unlock()
		return nil
	}
	stop := channelSmartScheduleRefreshWorker.stop
	done := channelSmartScheduleRefreshWorker.done
	if !channelSmartScheduleRefreshWorker.stopping {
		close(stop)
		if channelSmartScheduleRefreshWorker.cancel != nil {
			channelSmartScheduleRefreshWorker.cancel()
		}
		channelSmartScheduleRefreshWorker.stopping = true
	}
	channelSmartScheduleRefreshWorker.mu.Unlock()

	select {
	case <-done:
		channelSmartScheduleRefreshWorker.mu.Lock()
		channelSmartScheduleRefreshWorker.queue = nil
		channelSmartScheduleRefreshWorker.stop = nil
		channelSmartScheduleRefreshWorker.done = nil
		channelSmartScheduleRefreshWorker.cancel = nil
		channelSmartScheduleRefreshWorker.pending = nil
		channelSmartScheduleRefreshWorker.started = false
		channelSmartScheduleRefreshWorker.stopping = false
		channelSmartScheduleRefreshWorker.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func runChannelSmartScheduleRefreshWorker(
	workerContext context.Context,
	queue <-chan channelSmartScheduleRefreshKey,
	stop <-chan struct{},
	done chan<- struct{},
) {
	defer close(done)
	refreshContext, cancel := context.WithTimeout(workerContext, channelSmartScheduleRouteSnapshotBuildTimeout)
	if err := loadChannelSmartScheduleRouteSnapshot(refreshContext); err != nil {
		if publishErr := publishChannelSmartScheduleRouteSnapshot(refreshContext); publishErr != nil {
			if workerContext.Err() == nil {
				common.SysError("初始化智能调度 Redis 路由快照失败: " + publishErr.Error())
			}
		}
	}
	cancel()
	ticker := time.NewTicker(channelSmartScheduleRouteSnapshotPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-workerContext.Done():
			return
		case <-stop:
			return
		case key := <-queue:
		drainQueue:
			for {
				select {
				case <-queue:
					// Every key rebuilds the same complete versioned snapshot.
					// Drain the current burst so one relation change cannot trigger
					// one multi-table rebuild per group/model request.
				default:
					break drainQueue
				}
			}
			refreshContext, cancel := context.WithTimeout(workerContext, channelSmartScheduleRouteSnapshotBuildTimeout)
			err := refreshChannelSmartScheduleKey(refreshContext, key)
			cancel()
			if err == nil {
				clearPendingChannelSmartScheduleRefreshes()
			}
			if err != nil && workerContext.Err() == nil {
				common.SysError("后台刷新智能调度缓存失败: " + err.Error())
			}
		case <-ticker.C:
			refreshContext, cancel := context.WithTimeout(workerContext, channelSmartScheduleRouteSnapshotIOTimeout)
			loadErr := loadChannelSmartScheduleRouteSnapshot(refreshContext)
			cancel()
			dirty := channelSmartScheduleRouteSnapshotIsDirty()
			needsRenewal := channelSmartScheduleRouteSnapshotNeedsRenewal(time.Now())
			if loadErr == nil && !dirty && !needsRenewal {
				clearPendingChannelSmartScheduleRefreshes()
				continue
			}
			if !hasPendingChannelSmartScheduleRefreshes() && !dirty && !needsRenewal {
				continue
			}
			refreshContext, cancel = context.WithTimeout(workerContext, channelSmartScheduleRouteSnapshotBuildTimeout)
			refreshErr := publishChannelSmartScheduleRouteSnapshot(refreshContext)
			cancel()
			if refreshErr == nil {
				clearPendingChannelSmartScheduleRefreshes()
			}
		}
	}
}

func refreshChannelSmartScheduleKey(ctx context.Context, _ channelSmartScheduleRefreshKey) error {
	return publishChannelSmartScheduleRouteSnapshot(ctx)
}

func queueDirtyChannelSmartScheduleRefreshes() {
	channelSyncLock.RLock()
	runtimeDirty := logicalChannelRuntimeDirty
	dirtyPools := make([]channelSmartScheduleRefreshKey, 0, len(channelSmartScheduleRouteCacheDirty))
	for key := range channelSmartScheduleRouteCacheDirty {
		dirtyPools = append(dirtyPools, channelSmartScheduleRefreshKey{group: key.group, model: key.model})
	}
	channelSyncLock.RUnlock()
	if runtimeDirty {
		enqueueChannelSmartScheduleRefresh(channelSmartScheduleRefreshKey{runtime: true})
	}
	for _, key := range dirtyPools {
		enqueueChannelSmartScheduleRefresh(key)
	}
}

func enqueueChannelSmartScheduleRefresh(key channelSmartScheduleRefreshKey) bool {
	key.group = strings.TrimSpace(key.group)
	key.model = strings.TrimSpace(key.model)
	if !key.runtime && (key.group == "" || key.model == "") {
		return false
	}
	channelSmartScheduleRefreshWorker.mu.Lock()
	if !channelSmartScheduleRefreshWorker.started || channelSmartScheduleRefreshWorker.stopping {
		channelSmartScheduleRefreshWorker.mu.Unlock()
		return false
	}
	if _, exists := channelSmartScheduleRefreshWorker.pending[key]; exists {
		channelSmartScheduleRefreshWorker.mu.Unlock()
		return true
	}
	channelSmartScheduleRefreshWorker.pending[key] = struct{}{}
	queue := channelSmartScheduleRefreshWorker.queue
	channelSmartScheduleRefreshWorker.mu.Unlock()

	select {
	case queue <- key:
		return true
	default:
		channelSmartScheduleRefreshWorker.mu.Lock()
		delete(channelSmartScheduleRefreshWorker.pending, key)
		channelSmartScheduleRefreshWorker.mu.Unlock()
		common.SysError("智能调度缓存刷新队列已满，跳过后台刷新")
		return false
	}
}

func channelSmartScheduleRefreshWorkerIsStarted() bool {
	channelSmartScheduleRefreshWorker.mu.Lock()
	defer channelSmartScheduleRefreshWorker.mu.Unlock()
	return channelSmartScheduleRefreshWorker.started && !channelSmartScheduleRefreshWorker.stopping
}

func hasPendingChannelSmartScheduleRefreshes() bool {
	channelSmartScheduleRefreshWorker.mu.Lock()
	defer channelSmartScheduleRefreshWorker.mu.Unlock()
	return len(channelSmartScheduleRefreshWorker.pending) > 0
}

func clearPendingChannelSmartScheduleRefreshes() {
	channelSmartScheduleRefreshWorker.mu.Lock()
	clear(channelSmartScheduleRefreshWorker.pending)
	channelSmartScheduleRefreshWorker.mu.Unlock()
}

func channelSmartScheduleRouteSnapshotIsDirty() bool {
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	return logicalChannelRuntimeDirty || len(channelSmartScheduleRouteCacheDirty) > 0 ||
		channelSmartScheduleRouteSnapshotDirtySince > 0
}
