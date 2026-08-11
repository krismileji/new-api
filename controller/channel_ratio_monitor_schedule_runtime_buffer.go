package controller

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const channelSmartScheduleRuntimeSuccessRedisScript = `
local now = tonumber(ARGV[1]) or 0
local retention = tonumber(ARGV[2]) or 0
local cutoff = now - retention
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', cutoff - 1)
local watermark = tonumber(redis.call('GET', KEYS[3]) or '0')
for index = 5, #ARGV, 2 do
  local timestamp = tonumber(ARGV[index]) or now
  local local_sequence = tonumber(ARGV[index + 1]) or 0
  if local_sequence > watermark then
    local shared_sequence = redis.call('INCR', KEYS[2])
    local member = string.format('%020d:0', shared_sequence)
    redis.call('ZADD', KEYS[1], timestamp, member)
    watermark = local_sequence
  end
end
if watermark > 0 then
  redis.call('SET', KEYS[3], watermark)
  redis.call('EXPIRE', KEYS[3], retention + 60)
end
local route_incomplete_until = tonumber(ARGV[3]) or 0
if route_incomplete_until > now then
  redis.call('SET', KEYS[4], '1')
  redis.call('EXPIREAT', KEYS[4], route_incomplete_until)
end
local global_incomplete_until = tonumber(ARGV[4]) or 0
if global_incomplete_until > now then
  redis.call('SET', KEYS[5], '1')
  redis.call('EXPIREAT', KEYS[5], global_incomplete_until)
end
local size = tonumber(redis.call('ZCARD', KEYS[1]) or '0')
if size > 1000 then
  redis.call('ZREMRANGEBYRANK', KEYS[1], 0, size - 1001)
end
redis.call('EXPIRE', KEYS[1], retention + 60)
redis.call('EXPIRE', KEYS[2], retention + 60)
return (#ARGV - 4) / 2
`

const channelSmartScheduleRuntimeRedisRouteLockCount = 64

type channelSmartScheduleRuntimeRedisSuccessKey struct {
	channelId       int
	modelName       string
	revision        string
	retentionSecond int
}

type channelSmartScheduleRuntimeRedisSuccessEvent struct {
	timestamp int64
	sequence  uint64
}

var channelSmartScheduleRuntimeRedisFlushInterval = time.Duration(
	channelMonitorRuntimeEnvInt("CHANNEL_MONITOR_RUNTIME_REDIS_FLUSH_INTERVAL_MS", 200, 20, 5000),
) * time.Millisecond

var channelSmartScheduleRuntimeRedisMaxPending = channelMonitorRuntimeEnvInt(
	"CHANNEL_MONITOR_RUNTIME_REDIS_MAX_PENDING", 4096, 128, 65536,
)

var channelSmartScheduleRuntimeRedisTimeout = time.Duration(
	channelMonitorRuntimeEnvInt("CHANNEL_MONITOR_RUNTIME_REDIS_TIMEOUT_MS", 500, 50, 5000),
) * time.Millisecond

var channelSmartScheduleAdaptiveRefreshMinInterval = time.Duration(
	channelMonitorRuntimeEnvInt("CHANNEL_MONITOR_ADAPTIVE_REFRESH_MIN_INTERVAL_SECONDS", 10, 1, 300),
) * time.Second

var channelSmartScheduleRuntimeRedisDroppedLogAt atomic.Int64
var channelSmartScheduleRuntimeRedisSuccessSequence atomic.Uint64
var channelSmartScheduleRuntimeRedisSuccessProcess = common.GetUUID()
var channelSmartScheduleRuntimeRedisRouteLocks [channelSmartScheduleRuntimeRedisRouteLockCount]sync.Mutex
var channelSmartScheduleRuntimeRedisBackgroundMu sync.Mutex

var channelSmartScheduleRuntimeRedisSuccessQueue = struct {
	sync.Mutex
	pending              map[channelSmartScheduleRuntimeRedisSuccessKey][]channelSmartScheduleRuntimeRedisSuccessEvent
	pendingSize          int
	incompleteUntil      map[channelSmartScheduleRuntimeRedisSuccessKey]int64
	incompleteDirty      map[channelSmartScheduleRuntimeRedisSuccessKey]int64
	incompleteAllUntil   int64
	incompleteAllIsDirty bool
	running              bool
}{
	pending:         make(map[channelSmartScheduleRuntimeRedisSuccessKey][]channelSmartScheduleRuntimeRedisSuccessEvent),
	incompleteUntil: make(map[channelSmartScheduleRuntimeRedisSuccessKey]int64),
	incompleteDirty: make(map[channelSmartScheduleRuntimeRedisSuccessKey]int64),
}

type channelSmartScheduleAdaptiveRefreshThrottleKey struct {
	database any
	pool     channelSmartScheduleRoutePoolKey
}

var channelSmartScheduleAdaptiveRefreshThrottle = struct {
	sync.Mutex
	database  any
	lastRun   map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time
	scheduled map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time
}{
	lastRun:   make(map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time),
	scheduled: make(map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time),
}

func channelMonitorRuntimeEnvInt(name string, fallback int, minimum int, maximum int) int {
	value := common.GetEnvOrDefault(name, fallback)
	if value < minimum || value > maximum {
		common.SysError(fmt.Sprintf("%s 超出范围，使用默认值 %d", name, fallback))
		return fallback
	}
	return value
}

func channelSmartScheduleRuntimeRedisContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), channelSmartScheduleRuntimeRedisTimeout)
}

func channelSmartScheduleRuntimeRedisSuccessWatermarkKey(redisKey string) string {
	return redisKey + ":success:" + channelSmartScheduleRuntimeRedisSuccessProcess
}

func channelSmartScheduleRuntimeRedisIncompleteKey(redisKey string) string {
	return redisKey + ":incomplete"
}

func channelSmartScheduleRuntimeRedisGlobalIncompleteKey() string {
	return channelSmartScheduleRuntimeFailureRedisKeyPrefix + "incomplete"
}

func channelSmartScheduleRuntimeRedisRouteLockIndex(
	key channelSmartScheduleRuntimeRedisSuccessKey,
) int {
	hash := uint64(1469598103934665603)
	hash ^= uint64(uint32(key.channelId))
	hash *= 1099511628211
	for _, value := range []string{key.modelName, key.revision} {
		for index := 0; index < len(value); index++ {
			hash ^= uint64(value[index])
			hash *= 1099511628211
		}
	}
	return int(hash % channelSmartScheduleRuntimeRedisRouteLockCount)
}

func markChannelSmartScheduleRuntimeRedisIncompleteLocked(
	key channelSmartScheduleRuntimeRedisSuccessKey,
	now int64,
) {
	if now <= 0 {
		now = common.GetTimestamp()
	}
	until := now + int64(key.retentionSecond) + 1
	for currentKey, currentUntil := range channelSmartScheduleRuntimeRedisSuccessQueue.incompleteUntil {
		if currentUntil > now {
			continue
		}
		delete(channelSmartScheduleRuntimeRedisSuccessQueue.incompleteUntil, currentKey)
		delete(channelSmartScheduleRuntimeRedisSuccessQueue.incompleteDirty, currentKey)
	}
	if currentUntil, exists := channelSmartScheduleRuntimeRedisSuccessQueue.incompleteUntil[key]; exists || len(channelSmartScheduleRuntimeRedisSuccessQueue.incompleteUntil) < channelSmartScheduleRuntimeRedisMaxPending {
		if until > currentUntil {
			channelSmartScheduleRuntimeRedisSuccessQueue.incompleteUntil[key] = until
			channelSmartScheduleRuntimeRedisSuccessQueue.incompleteDirty[key] = until
		}
		return
	}
	if until > channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllUntil {
		channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllUntil = until
		channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllIsDirty = true
	}
}

func startChannelSmartScheduleRuntimeRedisSuccessWorkerLocked() bool {
	if channelSmartScheduleRuntimeRedisSuccessQueue.running {
		return false
	}
	channelSmartScheduleRuntimeRedisSuccessQueue.running = true
	return true
}

func enqueueChannelSmartScheduleRuntimeRedisSuccess(
	channelId int,
	modelName string,
	now int64,
	retentionSeconds int,
	revision string,
) {
	if !common.RedisEnabled || common.RDB == nil || channelId <= 0 || modelName == "" {
		return
	}
	key := channelSmartScheduleRuntimeRedisSuccessKey{
		channelId: channelId, modelName: modelName, revision: revision,
		retentionSecond: retentionSeconds,
	}
	channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
	if channelSmartScheduleRuntimeRedisSuccessQueue.pendingSize >= channelSmartScheduleRuntimeRedisMaxPending {
		markChannelSmartScheduleRuntimeRedisIncompleteLocked(key, now)
		startWorker := startChannelSmartScheduleRuntimeRedisSuccessWorkerLocked()
		channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()
		logChannelSmartScheduleRuntimeRedisDrop()
		if startWorker {
			go runChannelSmartScheduleRuntimeRedisSuccessWorker()
		}
		return
	}
	channelSmartScheduleRuntimeRedisSuccessQueue.pending[key] = append(
		channelSmartScheduleRuntimeRedisSuccessQueue.pending[key],
		channelSmartScheduleRuntimeRedisSuccessEvent{
			timestamp: now,
			sequence:  channelSmartScheduleRuntimeRedisSuccessSequence.Add(1),
		},
	)
	channelSmartScheduleRuntimeRedisSuccessQueue.pendingSize++
	startWorker := startChannelSmartScheduleRuntimeRedisSuccessWorkerLocked()
	channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()
	if startWorker {
		go runChannelSmartScheduleRuntimeRedisSuccessWorker()
	}
}

func runChannelSmartScheduleRuntimeRedisSuccessWorker() {
	for {
		timer := time.NewTimer(channelSmartScheduleRuntimeRedisFlushInterval)
		<-timer.C
		flushChannelSmartScheduleRuntimeRedisSuccesses()

		channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
		now := common.GetTimestamp()
		for key, until := range channelSmartScheduleRuntimeRedisSuccessQueue.incompleteDirty {
			if until > now {
				continue
			}
			delete(channelSmartScheduleRuntimeRedisSuccessQueue.incompleteDirty, key)
			delete(channelSmartScheduleRuntimeRedisSuccessQueue.incompleteUntil, key)
		}
		if channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllUntil <= now {
			channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllUntil = 0
			channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllIsDirty = false
		}
		if channelSmartScheduleRuntimeRedisSuccessQueue.pendingSize == 0 &&
			len(channelSmartScheduleRuntimeRedisSuccessQueue.incompleteDirty) == 0 &&
			!channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllIsDirty {
			channelSmartScheduleRuntimeRedisSuccessQueue.running = false
			channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()
			return
		}
		channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()
	}
}

func flushChannelSmartScheduleRuntimeRedisSuccesses() {
	channelSmartScheduleRuntimeRedisBackgroundMu.Lock()
	defer channelSmartScheduleRuntimeRedisBackgroundMu.Unlock()
	if !common.RedisEnabled || common.RDB == nil {
		return
	}

	now := common.GetTimestamp()
	channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
	keysByLock := make([][]channelSmartScheduleRuntimeRedisSuccessKey, channelSmartScheduleRuntimeRedisRouteLockCount)
	seen := make(map[channelSmartScheduleRuntimeRedisSuccessKey]struct{},
		len(channelSmartScheduleRuntimeRedisSuccessQueue.pending)+len(channelSmartScheduleRuntimeRedisSuccessQueue.incompleteDirty))
	for key := range channelSmartScheduleRuntimeRedisSuccessQueue.pending {
		seen[key] = struct{}{}
	}
	for key, until := range channelSmartScheduleRuntimeRedisSuccessQueue.incompleteDirty {
		if until > now {
			seen[key] = struct{}{}
		}
	}
	globalIncompleteUntil := channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllUntil
	globalIncompleteDirty := channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllIsDirty &&
		globalIncompleteUntil > now
	channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()
	for key := range seen {
		index := channelSmartScheduleRuntimeRedisRouteLockIndex(key)
		keysByLock[index] = append(keysByLock[index], key)
	}

	if globalIncompleteDirty {
		expiresIn := time.Until(time.Unix(globalIncompleteUntil, 0))
		if expiresIn <= 0 {
			channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
			channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllIsDirty = false
			channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllUntil = 0
			channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()
		} else {
			ctx, cancel := channelSmartScheduleRuntimeRedisContext()
			err := common.RDB.Set(
				ctx, channelSmartScheduleRuntimeRedisGlobalIncompleteKey(), "1", expiresIn,
			).Err()
			cancel()
			if err == nil {
				channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
				if channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllUntil <= globalIncompleteUntil {
					channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllIsDirty = false
				}
				channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()
			} else {
				common.SysError("发布 Redis 智能调度全局不完整窗口失败: " + err.Error())
			}
		}
	}

	for lockIndex, keys := range keysByLock {
		if len(keys) == 0 {
			continue
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].channelId != keys[j].channelId {
				return keys[i].channelId < keys[j].channelId
			}
			if keys[i].modelName != keys[j].modelName {
				return keys[i].modelName < keys[j].modelName
			}
			return keys[i].revision < keys[j].revision
		})
		channelSmartScheduleRuntimeRedisRouteLocks[lockIndex].Lock()
		pending := make(map[channelSmartScheduleRuntimeRedisSuccessKey][]channelSmartScheduleRuntimeRedisSuccessEvent, len(keys))
		incompleteUntil := make(map[channelSmartScheduleRuntimeRedisSuccessKey]int64, len(keys))
		channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
		for _, key := range keys {
			events := channelSmartScheduleRuntimeRedisSuccessQueue.pending[key]
			if len(events) > 0 {
				delete(channelSmartScheduleRuntimeRedisSuccessQueue.pending, key)
				channelSmartScheduleRuntimeRedisSuccessQueue.pendingSize -= len(events)
				pending[key] = events
			}
			if until := channelSmartScheduleRuntimeRedisSuccessQueue.incompleteUntil[key]; until > now {
				incompleteUntil[key] = until
			}
		}
		currentGlobalIncompleteUntil := channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllUntil
		channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()

		pipeline := common.RDB.Pipeline()
		ctx, cancel := channelSmartScheduleRuntimeRedisContext()
		for _, key := range keys {
			events := pending[key]
			until := incompleteUntil[key]
			if len(events) == 0 && until <= now {
				continue
			}
			sort.Slice(events, func(i, j int) bool {
				return events[i].sequence < events[j].sequence
			})
			args := make([]any, 0, len(events)*2+4)
			args = append(args, now, key.retentionSecond, until, currentGlobalIncompleteUntil)
			for _, event := range events {
				args = append(args, event.timestamp, event.sequence)
			}
			redisKey := channelSmartScheduleRuntimeFailureRedisKey(key.channelId, key.modelName, key.revision)
			pipeline.Eval(
				ctx,
				channelSmartScheduleRuntimeSuccessRedisScript,
				[]string{
					redisKey,
					channelSmartScheduleRuntimeFailureRedisSequenceKey(redisKey),
					channelSmartScheduleRuntimeRedisSuccessWatermarkKey(redisKey),
					channelSmartScheduleRuntimeRedisIncompleteKey(redisKey),
					channelSmartScheduleRuntimeRedisGlobalIncompleteKey(),
				},
				args...,
			)
		}
		_, err := pipeline.Exec(ctx)
		cancel()
		if err != nil {
			requeueChannelSmartScheduleRuntimeRedisSuccesses(pending)
			common.SysError("批量同步 Redis 智能调度成功请求窗口失败: " + err.Error())
			channelSmartScheduleRuntimeRedisRouteLocks[lockIndex].Unlock()
			continue
		}
		channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
		for key, until := range incompleteUntil {
			if channelSmartScheduleRuntimeRedisSuccessQueue.incompleteDirty[key] <= until {
				delete(channelSmartScheduleRuntimeRedisSuccessQueue.incompleteDirty, key)
			}
		}
		if channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllUntil <= currentGlobalIncompleteUntil {
			channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllIsDirty = false
		}
		channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()
		channelSmartScheduleRuntimeRedisRouteLocks[lockIndex].Unlock()
	}
}

func requeueChannelSmartScheduleRuntimeRedisSuccesses(
	pending map[channelSmartScheduleRuntimeRedisSuccessKey][]channelSmartScheduleRuntimeRedisSuccessEvent,
) {
	channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
	dropped := 0
	startWorker := false
	for key, events := range pending {
		available := channelSmartScheduleRuntimeRedisMaxPending - channelSmartScheduleRuntimeRedisSuccessQueue.pendingSize
		if available <= 0 {
			dropped += len(events)
			markChannelSmartScheduleRuntimeRedisIncompleteLocked(key, common.GetTimestamp())
			continue
		}
		if len(events) > available {
			dropped += len(events) - available
			markChannelSmartScheduleRuntimeRedisIncompleteLocked(key, common.GetTimestamp())
			events = events[len(events)-available:]
		}
		current := channelSmartScheduleRuntimeRedisSuccessQueue.pending[key]
		merged := make([]channelSmartScheduleRuntimeRedisSuccessEvent, 0, len(events)+len(current))
		merged = append(merged, events...)
		merged = append(merged, current...)
		sort.Slice(merged, func(i, j int) bool {
			return merged[i].sequence < merged[j].sequence
		})
		channelSmartScheduleRuntimeRedisSuccessQueue.pending[key] = merged
		channelSmartScheduleRuntimeRedisSuccessQueue.pendingSize += len(events)
	}
	if len(pending) > 0 || len(channelSmartScheduleRuntimeRedisSuccessQueue.incompleteDirty) > 0 {
		startWorker = startChannelSmartScheduleRuntimeRedisSuccessWorkerLocked()
	}
	channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()
	if startWorker {
		go runChannelSmartScheduleRuntimeRedisSuccessWorker()
	}
	if dropped > 0 {
		logChannelSmartScheduleRuntimeRedisDrop()
	}
}

func takeChannelSmartScheduleRuntimeRedisSuccessesLocked(
	key channelSmartScheduleRuntimeRedisSuccessKey,
	now int64,
) ([]channelSmartScheduleRuntimeRedisSuccessEvent, int64, int64) {
	channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
	defer channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()
	events := channelSmartScheduleRuntimeRedisSuccessQueue.pending[key]
	if len(events) > 0 {
		delete(channelSmartScheduleRuntimeRedisSuccessQueue.pending, key)
		channelSmartScheduleRuntimeRedisSuccessQueue.pendingSize -= len(events)
	}
	routeIncompleteUntil := channelSmartScheduleRuntimeRedisSuccessQueue.incompleteUntil[key]
	if routeIncompleteUntil <= now {
		delete(channelSmartScheduleRuntimeRedisSuccessQueue.incompleteUntil, key)
		delete(channelSmartScheduleRuntimeRedisSuccessQueue.incompleteDirty, key)
		routeIncompleteUntil = 0
	}
	globalIncompleteUntil := channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllUntil
	if globalIncompleteUntil <= now {
		channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllUntil = 0
		channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllIsDirty = false
		globalIncompleteUntil = 0
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].sequence < events[j].sequence
	})
	return events, routeIncompleteUntil, globalIncompleteUntil
}

func discardChannelSmartScheduleRuntimeRedisSuccessesLocked(
	channelId int,
	modelName string,
	recoveryAt int64,
	revision string,
) {
	channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
	defer channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()
	for key, events := range channelSmartScheduleRuntimeRedisSuccessQueue.pending {
		if key.channelId != channelId || key.modelName != modelName || key.revision != revision {
			continue
		}
		retained := events[:0]
		for _, event := range events {
			if recoveryAt > 0 && event.timestamp > recoveryAt {
				retained = append(retained, event)
			}
		}
		channelSmartScheduleRuntimeRedisSuccessQueue.pendingSize -= len(events) - len(retained)
		if len(retained) == 0 {
			delete(channelSmartScheduleRuntimeRedisSuccessQueue.pending, key)
			continue
		}
		channelSmartScheduleRuntimeRedisSuccessQueue.pending[key] = retained
	}
	if recoveryAt > 0 {
		return
	}
	for key := range channelSmartScheduleRuntimeRedisSuccessQueue.incompleteUntil {
		if key.channelId != channelId || key.modelName != modelName || key.revision != revision {
			continue
		}
		delete(channelSmartScheduleRuntimeRedisSuccessQueue.incompleteUntil, key)
		delete(channelSmartScheduleRuntimeRedisSuccessQueue.incompleteDirty, key)
	}
}

func logChannelSmartScheduleRuntimeRedisDrop() {
	now := time.Now().Unix()
	last := channelSmartScheduleRuntimeRedisDroppedLogAt.Load()
	if now-last < 60 || !channelSmartScheduleRuntimeRedisDroppedLogAt.CompareAndSwap(last, now) {
		return
	}
	common.SysError("Redis 智能调度成功请求批处理队列已满，本次仅保留本地窗口")
}

func reserveChannelSmartScheduleAdaptiveRefresh(
	database any,
	pool channelSmartScheduleRoutePoolKey,
) bool {
	if channelSmartScheduleAdaptiveRefreshMinInterval <= 0 {
		return true
	}
	now := time.Now()
	key := channelSmartScheduleAdaptiveRefreshThrottleKey{database: database, pool: pool}
	channelSmartScheduleAdaptiveRefreshThrottle.Lock()
	if channelSmartScheduleAdaptiveRefreshThrottle.database != model.DB {
		channelSmartScheduleAdaptiveRefreshThrottle.database = model.DB
		channelSmartScheduleAdaptiveRefreshThrottle.lastRun = make(
			map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time,
		)
		channelSmartScheduleAdaptiveRefreshThrottle.scheduled = make(
			map[channelSmartScheduleAdaptiveRefreshThrottleKey]time.Time,
		)
	}
	lastRun := channelSmartScheduleAdaptiveRefreshThrottle.lastRun[key]
	if lastRun.IsZero() || now.Sub(lastRun) >= channelSmartScheduleAdaptiveRefreshMinInterval {
		channelSmartScheduleAdaptiveRefreshThrottle.lastRun[key] = now
		delete(channelSmartScheduleAdaptiveRefreshThrottle.scheduled, key)
		channelSmartScheduleAdaptiveRefreshThrottle.Unlock()
		return true
	}
	dueAt := lastRun.Add(channelSmartScheduleAdaptiveRefreshMinInterval)
	if scheduledAt, scheduled := channelSmartScheduleAdaptiveRefreshThrottle.scheduled[key]; scheduled && !scheduledAt.Before(dueAt) {
		channelSmartScheduleAdaptiveRefreshThrottle.Unlock()
		return false
	}
	channelSmartScheduleAdaptiveRefreshThrottle.scheduled[key] = dueAt
	channelSmartScheduleAdaptiveRefreshThrottle.Unlock()

	time.AfterFunc(time.Until(dueAt), func() {
		channelSmartScheduleAdaptiveRefreshThrottle.Lock()
		scheduledAt, scheduled := channelSmartScheduleAdaptiveRefreshThrottle.scheduled[key]
		if !scheduled || !scheduledAt.Equal(dueAt) {
			channelSmartScheduleAdaptiveRefreshThrottle.Unlock()
			return
		}
		delete(channelSmartScheduleAdaptiveRefreshThrottle.scheduled, key)
		channelSmartScheduleAdaptiveRefreshThrottle.Unlock()
		if database == model.DB {
			enqueueChannelSmartScheduleAdaptivePoolRefresh(pool.group, pool.model)
		}
	})
	return false
}

func resetChannelSmartScheduleRuntimeRedisSuccessQueueForTest() {
	channelSmartScheduleRuntimeRedisBackgroundMu.Lock()
	defer channelSmartScheduleRuntimeRedisBackgroundMu.Unlock()
	channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
	channelSmartScheduleRuntimeRedisSuccessQueue.pending = make(
		map[channelSmartScheduleRuntimeRedisSuccessKey][]channelSmartScheduleRuntimeRedisSuccessEvent,
	)
	channelSmartScheduleRuntimeRedisSuccessQueue.pendingSize = 0
	channelSmartScheduleRuntimeRedisSuccessQueue.incompleteUntil = make(
		map[channelSmartScheduleRuntimeRedisSuccessKey]int64,
	)
	channelSmartScheduleRuntimeRedisSuccessQueue.incompleteDirty = make(
		map[channelSmartScheduleRuntimeRedisSuccessKey]int64,
	)
	channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllUntil = 0
	channelSmartScheduleRuntimeRedisSuccessQueue.incompleteAllIsDirty = false
	channelSmartScheduleRuntimeRedisSuccessQueue.running = false
	channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()
}

func channelSmartScheduleRuntimeRedisSuccessPendingForTest() int {
	channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
	defer channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()
	return channelSmartScheduleRuntimeRedisSuccessQueue.pendingSize
}
