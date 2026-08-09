package service

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/go-redis/redis/v8"
)

const (
	channelRateLimitCooldownRedisSyncInterval = 500 * time.Millisecond
	channelRateLimitCooldownRedisSyncTimeout  = 400 * time.Millisecond
	channelRateLimitCooldownRedisErrorLogGap  = time.Minute
)

type channelRateLimitCooldownSnapshot struct {
	untilByRoute map[channelRateLimitCooldownKey]channelRateLimitCooldownEntry
}

var channelRateLimitCooldownSnapshotValue atomic.Pointer[channelRateLimitCooldownSnapshot]
var channelRateLimitCooldownGeneration atomic.Uint64
var channelRateLimitCooldownRedisLastErrorLog atomic.Int64

var channelRateLimitCooldownExpiredHandler = struct {
	sync.RWMutex
	handler func(channelId int, modelName string)
}{}

var channelRateLimitCooldownRedisSync = struct {
	sync.Mutex
	client  atomic.Pointer[redis.Client]
	running atomic.Bool
	cancel  context.CancelFunc
	done    chan struct{}
}{}

func publishChannelRateLimitCooldownSnapshotLocked() {
	entries := make(map[channelRateLimitCooldownKey]channelRateLimitCooldownEntry, len(channelRateLimitCooldowns.untilByRoute))
	for key, entry := range channelRateLimitCooldowns.untilByRoute {
		entries[key] = entry
	}
	channelRateLimitCooldownGeneration.Add(1)
	channelRateLimitCooldownSnapshotValue.Store(&channelRateLimitCooldownSnapshot{untilByRoute: entries})
}

func loadChannelRateLimitCooldownSnapshot() *channelRateLimitCooldownSnapshot {
	snapshot := channelRateLimitCooldownSnapshotValue.Load()
	if snapshot != nil {
		return snapshot
	}
	return &channelRateLimitCooldownSnapshot{untilByRoute: map[channelRateLimitCooldownKey]channelRateLimitCooldownEntry{}}
}

func ensureChannelRateLimitCooldownRedisSync() {
	var client *redis.Client
	if common.RedisEnabled {
		client = common.RDB
	}
	if channelRateLimitCooldownRedisSync.client.Load() != client {
		channelRateLimitCooldownRedisSync.client.Store(client)
	}
	if channelRateLimitCooldownRedisSync.running.Load() {
		return
	}

	channelRateLimitCooldownRedisSync.Lock()
	defer channelRateLimitCooldownRedisSync.Unlock()
	if channelRateLimitCooldownRedisSync.running.Load() {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	channelRateLimitCooldownRedisSync.running.Store(true)
	channelRateLimitCooldownRedisSync.cancel = cancel
	channelRateLimitCooldownRedisSync.done = done
	go runChannelRateLimitCooldownRedisSync(ctx, done)
}

func runChannelRateLimitCooldownRedisSync(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(channelRateLimitCooldownRedisSyncInterval)
	defer ticker.Stop()
	for {
		pruneExpiredChannelRateLimitCooldowns()
		if client := channelRateLimitCooldownRedisSync.client.Load(); client != nil {
			syncChannelRateLimitCooldownsFromRedis(ctx, client)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func pruneExpiredChannelRateLimitCooldowns() {
	now := common.GetTimestamp()
	revision := channelRateLimitCooldownControlRevision()
	channelRateLimitCooldowns.Lock()
	changed := false
	expired := make([]channelRateLimitCooldownKey, 0)
	for key, entry := range channelRateLimitCooldowns.untilByRoute {
		if entry.until <= now || entry.revision != revision {
			delete(channelRateLimitCooldowns.untilByRoute, key)
			changed = true
			if entry.until <= now && entry.revision == revision {
				expired = append(expired, key)
			}
		}
	}
	if changed {
		publishChannelRateLimitCooldownSnapshotLocked()
	}
	channelRateLimitCooldowns.Unlock()
	for _, key := range expired {
		notifyChannelRateLimitCooldownExpired(key.channelId, key.modelName)
	}
}

// RegisterChannelRateLimitCooldownExpiredHandler installs the process-local
// callback used to recompute smart-scheduling overlays when a current-revision
// 429 gate expires naturally. Revision invalidation intentionally does not
// emit expiry events because configuration cleanup schedules its own replay.
func RegisterChannelRateLimitCooldownExpiredHandler(handler func(channelId int, modelName string)) {
	channelRateLimitCooldownExpiredHandler.Lock()
	channelRateLimitCooldownExpiredHandler.handler = handler
	channelRateLimitCooldownExpiredHandler.Unlock()
}

func notifyChannelRateLimitCooldownExpired(channelId int, modelName string) {
	channelRateLimitCooldownExpiredHandler.RLock()
	handler := channelRateLimitCooldownExpiredHandler.handler
	channelRateLimitCooldownExpiredHandler.RUnlock()
	if handler != nil {
		handler(channelId, modelName)
	}
}

func syncChannelRateLimitCooldownsFromRedis(parent context.Context, client *redis.Client) {
	if client == nil || channelRateLimitCooldownRedisSync.client.Load() != client {
		return
	}
	generation := channelRateLimitCooldownGeneration.Load()
	revision := channelRateLimitCooldownControlRevision()
	now := common.GetTimestamp()
	ctx, cancel := context.WithTimeout(parent, channelRateLimitCooldownRedisSyncTimeout)
	defer cancel()

	pipe := client.TxPipeline()
	revisionCommand := pipe.Get(ctx, channelRateLimitCooldownRedisRevisionKey)
	pipe.ZRemRangeByScore(ctx, channelRateLimitCooldownRedisKey, "-inf", strconv.FormatInt(now, 10))
	activeCommand := pipe.ZRangeByScoreWithScores(
		ctx,
		channelRateLimitCooldownRedisKey,
		&redis.ZRangeBy{Min: "(" + strconv.FormatInt(now, 10), Max: "+inf"},
	)
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		logChannelRateLimitCooldownRedisSyncError(err)
		return
	}
	observedRevision, revisionErr := revisionCommand.Result()
	if errors.Is(revisionErr, redis.Nil) {
		observedRevision = ""
		revisionErr = nil
	}
	if revisionErr != nil {
		logChannelRateLimitCooldownRedisSyncError(revisionErr)
		return
	}
	if activeCommand.Err() != nil && !errors.Is(activeCommand.Err(), redis.Nil) {
		logChannelRateLimitCooldownRedisSyncError(activeCommand.Err())
		return
	}
	if observedRevision != revision || channelRateLimitCooldownControlRevision() != revision {
		return
	}
	if channelRateLimitCooldownRedisSync.client.Load() != client || channelRateLimitCooldownGeneration.Load() != generation {
		return
	}

	sharedEntries := make(map[channelRateLimitCooldownKey]int64, len(activeCommand.Val()))
	for _, item := range activeCommand.Val() {
		key, ok := parseChannelRateLimitCooldownRedisMember(item.Member)
		until := int64(item.Score)
		if ok && until > now {
			sharedEntries[key] = until
		}
	}

	channelRateLimitCooldowns.Lock()
	defer channelRateLimitCooldowns.Unlock()
	if channelRateLimitCooldownRedisSync.client.Load() != client ||
		channelRateLimitCooldownGeneration.Load() != generation ||
		channelRateLimitCooldownControlRevision() != revision {
		return
	}
	next := make(map[channelRateLimitCooldownKey]channelRateLimitCooldownEntry, len(sharedEntries))
	for key, entry := range channelRateLimitCooldowns.untilByRoute {
		if entry.revision == revision && entry.until > now && !entry.shared {
			next[key] = entry
		}
	}
	for key, until := range sharedEntries {
		if local, ok := next[key]; ok && local.until > until {
			continue
		}
		next[key] = channelRateLimitCooldownEntry{until: until, shared: true, revision: revision}
	}
	channelRateLimitCooldowns.untilByRoute = next
	publishChannelRateLimitCooldownSnapshotLocked()
}

func logChannelRateLimitCooldownRedisSyncError(err error) {
	now := time.Now().Unix()
	last := channelRateLimitCooldownRedisLastErrorLog.Load()
	if now-last < int64(channelRateLimitCooldownRedisErrorLogGap/time.Second) ||
		!channelRateLimitCooldownRedisLastErrorLog.CompareAndSwap(last, now) {
		return
	}
	common.SysError("后台同步 Redis 429 冷却失败: " + err.Error())
}

func stopChannelRateLimitCooldownRedisSync() {
	channelRateLimitCooldownRedisSync.Lock()
	cancel := channelRateLimitCooldownRedisSync.cancel
	done := channelRateLimitCooldownRedisSync.done
	channelRateLimitCooldownRedisSync.cancel = nil
	channelRateLimitCooldownRedisSync.done = nil
	channelRateLimitCooldownRedisSync.client.Store(nil)
	channelRateLimitCooldownRedisSync.running.Store(false)
	channelRateLimitCooldownRedisSync.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}
