package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/go-redis/redis/v8"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

const (
	channelModelDetectionOverviewSnapshotVersion = 1

	channelModelDetectionOverviewCacheDefaultTTL = time.Second
	channelModelDetectionOverviewCacheMaxTTL     = 5 * time.Second
	channelModelDetectionOverviewStaleDefaultTTL = 5 * time.Minute
	channelModelDetectionOverviewStaleMaxTTL     = time.Hour
	channelModelDetectionOverviewRedisDefaultTTL = 10 * time.Minute
	channelModelDetectionOverviewRedisMaxTTL     = 24 * time.Hour
	channelModelDetectionOverviewRefreshTimeout  = 30 * time.Second

	channelModelDetectionOverviewRedisReadTimeout  = 100 * time.Millisecond
	channelModelDetectionOverviewRedisWriteTimeout = 250 * time.Millisecond
	channelModelDetectionOverviewBuildLeaseTTL     = 30 * time.Second
	channelModelDetectionOverviewBuildWait         = 2 * time.Second
	channelModelDetectionOverviewRefreshCoalesce   = time.Second
	channelModelDetectionOverviewHeartbeatInterval = 15 * time.Second

	channelModelDetectionOverviewRedisKey      = "channel_monitor:model_detection:overview:v1"
	channelModelDetectionOverviewMetadataKey   = "channel_monitor:model_detection:overview:v1:meta"
	channelModelDetectionOverviewRevisionKey   = "channel_monitor:model_detection:overview:revision"
	channelModelDetectionOverviewWatermarkKey  = "channel_monitor:model_detection:overview:event_watermark"
	channelModelDetectionOverviewBuildLeaseKey = "channel_monitor:model_detection:overview:build_lease"
)

var ErrChannelModelDetectionOverviewSnapshotUnavailable = errors.New("模型检测总览快照暂不可用，请稍后重试")

type channelModelDetectionOverviewSnapshot struct {
	Version                int                                   `json:"version"`
	Revision               uint64                                `json:"revision"`
	EventWatermark         uint64                                `json:"event_watermark"`
	GeneratedAt            int64                                 `json:"generated_at"`
	GeneratedAtUnixMillis  int64                                 `json:"generated_at_unix_millis"`
	DataCutoffAt           int64                                 `json:"data_cutoff_at"`
	DataCutoffAtUnixMillis int64                                 `json:"data_cutoff_at_unix_millis"`
	Response               ChannelModelDetectionOverviewResponse `json:"data"`
}

type channelModelDetectionOverviewBuildLease struct {
	client         *redis.Client
	fencingToken   uint64
	eventWatermark uint64
}

type channelModelDetectionOverviewCacheEntry struct {
	snapshot    channelModelDetectionOverviewSnapshot
	expiresAt   time.Time
	staleUntil  time.Time
	generation  uint64
	invalidated bool
}

var channelModelDetectionOverviewCache = struct {
	sync.RWMutex
	items        map[*gorm.DB]channelModelDetectionOverviewCacheEntry
	buildBackoff map[*gorm.DB]time.Time
}{
	items:        make(map[*gorm.DB]channelModelDetectionOverviewCacheEntry),
	buildBackoff: make(map[*gorm.DB]time.Time),
}

var channelModelDetectionOverviewLoadSingleflight singleflight.Group
var channelModelDetectionOverviewRefreshSingleflight singleflight.Group
var channelModelDetectionOverviewCacheGeneration atomic.Uint64
var channelModelDetectionOverviewLocalRevision atomic.Uint64
var channelModelDetectionOverviewLocalEventWatermark atomic.Uint64
var channelModelDetectionOverviewHeartbeatOnce sync.Once
var channelModelDetectionOverviewRefreshSchedule struct {
	sync.Mutex
	timer *time.Timer
	db    *gorm.DB
}

func channelModelDetectionOverviewCacheTTL() time.Duration {
	milliseconds := common.GetEnvOrDefault(
		"CHANNEL_MODEL_DETECTION_OVERVIEW_CACHE_TTL_MS",
		int(channelModelDetectionOverviewCacheDefaultTTL/time.Millisecond),
	)
	if milliseconds <= 0 {
		return 0
	}
	return min(time.Duration(milliseconds)*time.Millisecond, channelModelDetectionOverviewCacheMaxTTL)
}

func channelModelDetectionOverviewStaleTTL() time.Duration {
	milliseconds := common.GetEnvOrDefault(
		"CHANNEL_MODEL_DETECTION_OVERVIEW_STALE_TTL_MS",
		int(channelModelDetectionOverviewStaleDefaultTTL/time.Millisecond),
	)
	if milliseconds <= 0 {
		return 0
	}
	return min(time.Duration(milliseconds)*time.Millisecond, channelModelDetectionOverviewStaleMaxTTL)
}

func channelModelDetectionOverviewRedisTTL() time.Duration {
	seconds := common.GetEnvOrDefault(
		"CHANNEL_MODEL_DETECTION_OVERVIEW_REDIS_TTL_SECONDS",
		int(channelModelDetectionOverviewRedisDefaultTTL/time.Second),
	)
	if seconds <= 0 {
		return 0
	}
	return min(time.Duration(seconds)*time.Second, channelModelDetectionOverviewRedisMaxTTL)
}

func channelModelDetectionOverviewSingleflightKey(db *gorm.DB, generation uint64) string {
	return fmt.Sprintf("%p:%d", db, generation)
}

func channelModelDetectionOverviewRedisClient(read bool) *redis.Client {
	if !common.RedisEnabled {
		return nil
	}
	if read {
		return common.RedisMonitorReadClient()
	}
	return common.RedisMonitorWriteClient()
}

func channelModelDetectionOverviewResponseWithMetadata(
	snapshot channelModelDetectionOverviewSnapshot,
	stale bool,
	serverNow int64,
) ChannelModelDetectionOverviewResponse {
	currentTimestamp := common.GetTimestamp()
	if serverNow < currentTimestamp {
		serverNow = currentTimestamp
	}
	response := snapshot.Response
	response.ServerNow = serverNow
	response.SnapshotVersion = snapshot.Version
	response.SnapshotRevision = snapshot.Revision
	response.EventWatermark = snapshot.EventWatermark
	response.GeneratedAt = snapshot.GeneratedAt
	response.DataCutoffAt = snapshot.DataCutoffAt
	response.Stale = stale
	age := serverNow - snapshot.GeneratedAt
	if age < 0 {
		age = 0
	}
	response.SnapshotAgeSeconds = age
	return response
}

func channelModelDetectionOverviewSnapshotGeneratedTime(snapshot channelModelDetectionOverviewSnapshot) time.Time {
	if snapshot.GeneratedAtUnixMillis > 0 {
		return time.UnixMilli(snapshot.GeneratedAtUnixMillis)
	}
	return time.Unix(snapshot.GeneratedAt, 0)
}

func channelModelDetectionOverviewSnapshotDataCutoffTime(snapshot channelModelDetectionOverviewSnapshot) time.Time {
	if snapshot.DataCutoffAtUnixMillis > 0 {
		return time.UnixMilli(snapshot.DataCutoffAtUnixMillis)
	}
	return time.Unix(snapshot.DataCutoffAt, 0)
}

func updateChannelModelDetectionOverviewCounter(counter *atomic.Uint64, candidate uint64) {
	for {
		current := counter.Load()
		if current >= candidate || counter.CompareAndSwap(current, candidate) {
			return
		}
	}
}

func channelModelDetectionOverviewSnapshotNewer(left, right channelModelDetectionOverviewSnapshot) bool {
	return left.EventWatermark >= right.EventWatermark &&
		left.Revision > right.Revision &&
		left.DataCutoffAtUnixMillis >= right.DataCutoffAtUnixMillis &&
		left.GeneratedAtUnixMillis >= right.GeneratedAtUnixMillis
}

func loadChannelModelDetectionOverviewCacheEntry(
	db *gorm.DB,
	now time.Time,
) (channelModelDetectionOverviewCacheEntry, bool, bool) {
	channelModelDetectionOverviewCache.RLock()
	entry, exists := channelModelDetectionOverviewCache.items[db]
	channelModelDetectionOverviewCache.RUnlock()
	if !exists || !now.Before(entry.staleUntil) {
		return channelModelDetectionOverviewCacheEntry{}, false, false
	}
	stale := entry.invalidated || !now.Before(entry.expiresAt)
	return entry, true, stale
}

func storeChannelModelDetectionOverviewCacheEntry(
	db *gorm.DB,
	generation uint64,
	snapshot channelModelDetectionOverviewSnapshot,
	ttl time.Duration,
	staleTTL time.Duration,
) bool {
	if snapshot.Version != channelModelDetectionOverviewSnapshotVersion || snapshot.Revision == 0 ||
		snapshot.GeneratedAt <= 0 || snapshot.GeneratedAtUnixMillis <= 0 ||
		snapshot.DataCutoffAt <= 0 || snapshot.DataCutoffAtUnixMillis <= 0 {
		return false
	}
	generatedTime := channelModelDetectionOverviewSnapshotGeneratedTime(snapshot)
	now := time.Now()
	dataCutoffTime := channelModelDetectionOverviewSnapshotDataCutoffTime(snapshot)
	if time.Unix(snapshot.GeneratedAt, 0).After(now.Add(5*time.Second)) ||
		generatedTime.After(now.Add(5*time.Second)) || dataCutoffTime.After(generatedTime) {
		return false
	}
	staleUntil := generatedTime.Add(staleTTL)
	if !now.Before(staleUntil) {
		return false
	}
	entry := channelModelDetectionOverviewCacheEntry{
		snapshot: snapshot, expiresAt: generatedTime.Add(ttl), staleUntil: staleUntil, generation: generation,
	}
	channelModelDetectionOverviewCache.Lock()
	defer channelModelDetectionOverviewCache.Unlock()
	if generation != channelModelDetectionOverviewCacheGeneration.Load() {
		return false
	}
	if current, exists := channelModelDetectionOverviewCache.items[db]; exists &&
		!channelModelDetectionOverviewSnapshotNewer(snapshot, current.snapshot) {
		return false
	}
	channelModelDetectionOverviewCache.items[db] = entry
	delete(channelModelDetectionOverviewCache.buildBackoff, db)
	updateChannelModelDetectionOverviewCounter(&channelModelDetectionOverviewLocalRevision, snapshot.Revision)
	updateChannelModelDetectionOverviewCounter(&channelModelDetectionOverviewLocalEventWatermark, snapshot.EventWatermark)
	return true
}

func channelModelDetectionOverviewSnapshotsEqual(
	left channelModelDetectionOverviewSnapshot,
	right channelModelDetectionOverviewSnapshot,
) bool {
	return left.Version == right.Version &&
		left.Revision == right.Revision &&
		left.EventWatermark == right.EventWatermark &&
		left.GeneratedAtUnixMillis == right.GeneratedAtUnixMillis &&
		left.DataCutoffAtUnixMillis == right.DataCutoffAtUnixMillis
}

func revalidateChannelModelDetectionOverviewCacheEntry(
	db *gorm.DB,
	generation uint64,
	entry channelModelDetectionOverviewCacheEntry,
	ttl time.Duration,
) (channelModelDetectionOverviewSnapshot, bool) {
	if entry.invalidated || ttl <= 0 {
		return channelModelDetectionOverviewSnapshot{}, false
	}
	now := time.Now()
	if client := channelModelDetectionOverviewRedisClient(true); client != nil && channelModelDetectionOverviewRedisTTL() > 0 {
		shared, exists := loadChannelModelDetectionOverviewRedisSnapshot()
		if !exists {
			return channelModelDetectionOverviewSnapshot{}, false
		}
		if channelModelDetectionOverviewSnapshotNewer(shared, entry.snapshot) {
			if storeChannelModelDetectionOverviewCacheEntry(
				db, generation, shared, ttl, channelModelDetectionOverviewStaleTTL(),
			) {
				return shared, true
			}
			return channelModelDetectionOverviewSnapshot{}, false
		}
		if !channelModelDetectionOverviewSnapshotsEqual(shared, entry.snapshot) {
			return channelModelDetectionOverviewSnapshot{}, false
		}
	} else {
		// With Redis unavailable the local complete snapshot is the only safe
		// source. Revalidate it locally so stable polling does not fan out to the
		// database; the stale deadline still bounds its lifetime.
	}

	channelModelDetectionOverviewCache.Lock()
	defer channelModelDetectionOverviewCache.Unlock()
	if generation != channelModelDetectionOverviewCacheGeneration.Load() {
		return channelModelDetectionOverviewSnapshot{}, false
	}
	current, exists := channelModelDetectionOverviewCache.items[db]
	if !exists || current.invalidated || !now.Before(current.staleUntil) ||
		current.snapshot.EventWatermark < channelModelDetectionOverviewLocalEventWatermark.Load() ||
		!channelModelDetectionOverviewSnapshotsEqual(current.snapshot, entry.snapshot) {
		return channelModelDetectionOverviewSnapshot{}, false
	}
	current.expiresAt = now.Add(ttl)
	channelModelDetectionOverviewCache.items[db] = current
	return current.snapshot, true
}

func loadChannelModelDetectionOverviewRedisSnapshot() (channelModelDetectionOverviewSnapshot, bool) {
	client := channelModelDetectionOverviewRedisClient(true)
	if client == nil || channelModelDetectionOverviewRedisTTL() <= 0 {
		return channelModelDetectionOverviewSnapshot{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelModelDetectionOverviewRedisReadTimeout)
	defer cancel()
	values, err := client.MGet(ctx,
		channelModelDetectionOverviewRedisKey,
		channelModelDetectionOverviewMetadataKey,
		channelModelDetectionOverviewWatermarkKey,
	).Result()
	if err != nil {
		common.SysError("读取模型检测 Redis 快照失败: " + err.Error())
		return channelModelDetectionOverviewSnapshot{}, false
	}
	if len(values) != 3 || values[0] == nil || values[1] == nil {
		return channelModelDetectionOverviewSnapshot{}, false
	}
	payload, ok := values[0].(string)
	if !ok || payload == "" {
		return channelModelDetectionOverviewSnapshot{}, false
	}
	metadataPayload, ok := values[1].(string)
	if !ok || metadataPayload == "" {
		return channelModelDetectionOverviewSnapshot{}, false
	}
	var metadata struct {
		Revision               uint64 `json:"revision"`
		EventWatermark         uint64 `json:"event_watermark"`
		GeneratedAtUnixMillis  int64  `json:"generated_at_unix_millis"`
		DataCutoffAtUnixMillis int64  `json:"data_cutoff_at_unix_millis"`
	}
	if err = common.Unmarshal([]byte(metadataPayload), &metadata); err != nil {
		return channelModelDetectionOverviewSnapshot{}, false
	}
	var snapshot channelModelDetectionOverviewSnapshot
	if err = common.Unmarshal([]byte(payload), &snapshot); err != nil ||
		snapshot.Version != channelModelDetectionOverviewSnapshotVersion ||
		snapshot.Revision == 0 || snapshot.GeneratedAt <= 0 || snapshot.GeneratedAtUnixMillis <= 0 ||
		snapshot.DataCutoffAt <= 0 || snapshot.DataCutoffAtUnixMillis <= 0 ||
		snapshot.Revision != metadata.Revision || snapshot.EventWatermark != metadata.EventWatermark ||
		snapshot.GeneratedAtUnixMillis != metadata.GeneratedAtUnixMillis ||
		snapshot.DataCutoffAtUnixMillis != metadata.DataCutoffAtUnixMillis {
		return channelModelDetectionOverviewSnapshot{}, false
	}
	currentWatermark := uint64(0)
	if values[2] != nil {
		watermarkText, ok := values[2].(string)
		if !ok {
			return channelModelDetectionOverviewSnapshot{}, false
		}
		currentWatermark, err = strconv.ParseUint(watermarkText, 10, 64)
		if err != nil || snapshot.EventWatermark < currentWatermark {
			return channelModelDetectionOverviewSnapshot{}, false
		}
	}
	if !time.Now().Before(channelModelDetectionOverviewSnapshotGeneratedTime(snapshot).Add(channelModelDetectionOverviewStaleTTL())) {
		return channelModelDetectionOverviewSnapshot{}, false
	}
	now := time.Now()
	if time.Unix(snapshot.GeneratedAt, 0).After(now.Add(5*time.Second)) ||
		channelModelDetectionOverviewSnapshotGeneratedTime(snapshot).After(now.Add(5*time.Second)) ||
		channelModelDetectionOverviewSnapshotDataCutoffTime(snapshot).After(channelModelDetectionOverviewSnapshotGeneratedTime(snapshot)) {
		return channelModelDetectionOverviewSnapshot{}, false
	}
	updateChannelModelDetectionOverviewCounter(&channelModelDetectionOverviewLocalRevision, snapshot.Revision)
	// Preserve the watermark carried by the validated envelope, even if the
	// standalone Redis watermark key has expired. Using only currentWatermark
	// would allow a later local lease to start below an already observed value.
	updateChannelModelDetectionOverviewCounter(&channelModelDetectionOverviewLocalEventWatermark, snapshot.EventWatermark)
	return snapshot, true
}

func storeChannelModelDetectionOverviewRedisSnapshot(
	lease channelModelDetectionOverviewBuildLease,
	snapshot channelModelDetectionOverviewSnapshot,
) (bool, error) {
	redisTTL := channelModelDetectionOverviewRedisTTL()
	if lease.client == nil || lease.fencingToken == 0 || redisTTL <= 0 ||
		snapshot.Revision != lease.fencingToken || snapshot.EventWatermark != lease.eventWatermark {
		return false, nil
	}
	now := time.Now()
	if snapshot.GeneratedAt <= 0 || snapshot.GeneratedAtUnixMillis <= 0 ||
		time.Unix(snapshot.GeneratedAt, 0).After(now.Add(5*time.Second)) ||
		time.UnixMilli(snapshot.GeneratedAtUnixMillis).After(now.Add(5*time.Second)) ||
		time.Unix(snapshot.DataCutoffAt, 0).After(time.Unix(snapshot.GeneratedAt, 0)) ||
		time.UnixMilli(snapshot.DataCutoffAtUnixMillis).After(time.UnixMilli(snapshot.GeneratedAtUnixMillis)) {
		return false, nil
	}
	payload, err := common.Marshal(snapshot)
	if err != nil {
		return false, err
	}
	metadataPayload, err := common.Marshal(map[string]any{
		"revision": snapshot.Revision, "event_watermark": snapshot.EventWatermark,
		"generated_at_unix_millis":   snapshot.GeneratedAtUnixMillis,
		"data_cutoff_at_unix_millis": snapshot.DataCutoffAtUnixMillis,
	})
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelModelDetectionOverviewRedisWriteTimeout)
	defer cancel()
	result, err := redis.NewScript(`
local function less(a, b)
  a = string.gsub(a or "0", "^0+", "")
  b = string.gsub(b or "0", "^0+", "")
  if a == "" then a = "0" end
  if b == "" then b = "0" end
  if string.len(a) ~= string.len(b) then
    return string.len(a) < string.len(b)
  end
  return a < b
end

if redis.call("GET", KEYS[4]) ~= ARGV[1] then
  return -1
end
local current_watermark = redis.call("GET", KEYS[5]) or "0"
if less(ARGV[4], current_watermark) then
  return -2
end
local old_revision = redis.call("HGET", KEYS[3], "revision") or "0"
local old_watermark = redis.call("HGET", KEYS[3], "event_watermark") or "0"
local old_generated = redis.call("HGET", KEYS[3], "generated_at_unix_millis") or "0"
local old_cutoff = redis.call("HGET", KEYS[3], "data_cutoff_at_unix_millis") or "0"
if not less(old_revision, ARGV[3]) or less(ARGV[4], old_watermark) or
   less(ARGV[8], old_generated) or less(ARGV[5], old_cutoff) then
  return 0
end
redis.call("SET", KEYS[1], ARGV[2], "PX", ARGV[7])
redis.call("SET", KEYS[2], ARGV[6], "PX", ARGV[7])
redis.call("HSET", KEYS[3],
  "revision", ARGV[3],
  "event_watermark", ARGV[4],
  "generated_at_unix_millis", ARGV[8],
  "data_cutoff_at_unix_millis", ARGV[5])
redis.call("PEXPIRE", KEYS[3], ARGV[7])
return 1
`).Run(ctx, lease.client, []string{
		channelModelDetectionOverviewRedisKey,
		channelModelDetectionOverviewMetadataKey,
		channelModelDetectionOverviewRedisKey + ":publish_meta",
		channelModelDetectionOverviewBuildLeaseKey,
		channelModelDetectionOverviewWatermarkKey,
	},
		strconv.FormatUint(lease.fencingToken, 10), payload,
		strconv.FormatUint(snapshot.Revision, 10),
		strconv.FormatUint(snapshot.EventWatermark, 10),
		strconv.FormatInt(snapshot.DataCutoffAtUnixMillis, 10), metadataPayload,
		strconv.FormatInt(redisTTL.Milliseconds(), 10),
		strconv.FormatInt(snapshot.GeneratedAtUnixMillis, 10),
	).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func acquireChannelModelDetectionOverviewBuildLease() (channelModelDetectionOverviewBuildLease, bool, error) {
	client := channelModelDetectionOverviewRedisClient(false)
	if client == nil || channelModelDetectionOverviewRedisTTL() <= 0 {
		return channelModelDetectionOverviewBuildLease{
			fencingToken:   channelModelDetectionOverviewLocalRevision.Add(1),
			eventWatermark: channelModelDetectionOverviewLocalEventWatermark.Load(),
		}, true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelModelDetectionOverviewRedisWriteTimeout)
	defer cancel()
	values, err := redis.NewScript(`
local function less(a, b)
  a = string.gsub(a or "0", "^0+", "")
  b = string.gsub(b or "0", "^0+", "")
  if a == "" then a = "0" end
  if b == "" then b = "0" end
  if string.len(a) ~= string.len(b) then
    return string.len(a) < string.len(b)
  end
  return a < b
end

local watermark = redis.call("GET", KEYS[3]) or "0"
if less(watermark, ARGV[3]) then
  watermark = ARGV[3]
  redis.call("SET", KEYS[3], watermark)
end
if redis.call("EXISTS", KEYS[1]) == 1 then
  return {"0", watermark}
end
local revision = redis.call("GET", KEYS[2]) or "0"
if less(revision, ARGV[2]) then
  redis.call("SET", KEYS[2], ARGV[2])
end
redis.call("INCR", KEYS[2])
local fencing_token = redis.call("GET", KEYS[2])
redis.call("PSETEX", KEYS[1], ARGV[1], fencing_token)
return {fencing_token, watermark}
`).Run(ctx, client, []string{
		channelModelDetectionOverviewBuildLeaseKey,
		channelModelDetectionOverviewRevisionKey,
		channelModelDetectionOverviewWatermarkKey,
	},
		strconv.FormatInt(channelModelDetectionOverviewBuildLeaseTTL.Milliseconds(), 10),
		strconv.FormatUint(channelModelDetectionOverviewLocalRevision.Load(), 10),
		strconv.FormatUint(channelModelDetectionOverviewLocalEventWatermark.Load(), 10),
	).StringSlice()
	if err != nil {
		return channelModelDetectionOverviewBuildLease{}, false, err
	}
	if len(values) != 2 {
		return channelModelDetectionOverviewBuildLease{}, false, ErrChannelModelDetectionOverviewSnapshotUnavailable
	}
	fencingToken, tokenErr := strconv.ParseUint(values[0], 10, 64)
	watermark, watermarkErr := strconv.ParseUint(values[1], 10, 64)
	if tokenErr != nil || watermarkErr != nil {
		return channelModelDetectionOverviewBuildLease{}, false, ErrChannelModelDetectionOverviewSnapshotUnavailable
	}
	updateChannelModelDetectionOverviewCounter(&channelModelDetectionOverviewLocalEventWatermark, watermark)
	if fencingToken == 0 {
		return channelModelDetectionOverviewBuildLease{}, false, nil
	}
	updateChannelModelDetectionOverviewCounter(&channelModelDetectionOverviewLocalRevision, fencingToken)
	return channelModelDetectionOverviewBuildLease{
		client: client, fencingToken: fencingToken, eventWatermark: watermark,
	}, true, nil
}

func releaseChannelModelDetectionOverviewBuildLease(lease channelModelDetectionOverviewBuildLease) {
	if lease.client == nil || lease.fencingToken == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelModelDetectionOverviewRedisWriteTimeout)
	defer cancel()
	const releaseLeaseScript = `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) end return 0`
	_ = lease.client.Eval(ctx, releaseLeaseScript, []string{channelModelDetectionOverviewBuildLeaseKey}, strconv.FormatUint(lease.fencingToken, 10)).Err()
}

func waitForChannelModelDetectionOverviewRedisSnapshot(
	ctx context.Context,
	previousRevision uint64,
	previousEventWatermark uint64,
) (channelModelDetectionOverviewSnapshot, bool) {
	waitCtx, cancel := context.WithTimeout(ctx, channelModelDetectionOverviewBuildWait)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if snapshot, exists := loadChannelModelDetectionOverviewRedisSnapshot(); exists &&
			(snapshot.Revision > previousRevision || snapshot.EventWatermark > previousEventWatermark) {
			return snapshot, true
		}
		select {
		case <-waitCtx.Done():
			return channelModelDetectionOverviewSnapshot{}, false
		case <-ticker.C:
		}
	}
}

func buildChannelModelDetectionOverviewSnapshot(
	db *gorm.DB,
	generation uint64,
	previousRevision uint64,
	previousEventWatermark uint64,
	forceBuild bool,
) (channelModelDetectionOverviewSnapshot, error) {
	lease, acquired, leaseErr := acquireChannelModelDetectionOverviewBuildLease()
	if leaseErr != nil {
		common.SysError("获取模型检测快照重建租约失败，使用本地 singleflight: " + leaseErr.Error())
		lease = channelModelDetectionOverviewBuildLease{
			fencingToken:   channelModelDetectionOverviewLocalRevision.Add(1),
			eventWatermark: channelModelDetectionOverviewLocalEventWatermark.Load(),
		}
		acquired = true
	}
	if !acquired && forceBuild {
		deadline := time.Now().Add(channelModelDetectionOverviewRefreshTimeout)
		for !acquired && time.Now().Before(deadline) {
			time.Sleep(25 * time.Millisecond)
			lease, acquired, leaseErr = acquireChannelModelDetectionOverviewBuildLease()
			if leaseErr != nil {
				common.SysError("等待模型检测快照重建租约失败，使用本地 singleflight: " + leaseErr.Error())
				lease = channelModelDetectionOverviewBuildLease{
					fencingToken:   channelModelDetectionOverviewLocalRevision.Add(1),
					eventWatermark: channelModelDetectionOverviewLocalEventWatermark.Load(),
				}
				acquired = true
			}
		}
	}
	if !acquired {
		if snapshot, exists := waitForChannelModelDetectionOverviewRedisSnapshot(
			context.Background(), previousRevision, previousEventWatermark,
		); exists {
			return snapshot, nil
		}
		return channelModelDetectionOverviewSnapshot{}, ErrChannelModelDetectionOverviewSnapshotUnavailable
	}
	defer releaseChannelModelDetectionOverviewBuildLease(lease)

	ctx, cancel := context.WithTimeout(context.Background(), channelModelDetectionOverviewRefreshTimeout)
	defer cancel()
	dataCutoffTime := time.Now()
	response, err := GetChannelModelDetectionOverview(ctx, db, dataCutoffTime.Unix())
	if err != nil {
		return channelModelDetectionOverviewSnapshot{}, err
	}
	if generation != channelModelDetectionOverviewCacheGeneration.Load() {
		return channelModelDetectionOverviewSnapshot{}, ErrChannelModelDetectionOverviewSnapshotUnavailable
	}
	generatedTime := time.Now()
	snapshot := channelModelDetectionOverviewSnapshot{
		Version:  channelModelDetectionOverviewSnapshotVersion,
		Revision: lease.fencingToken, EventWatermark: lease.eventWatermark,
		GeneratedAt: generatedTime.Unix(), GeneratedAtUnixMillis: generatedTime.UnixMilli(),
		DataCutoffAt: dataCutoffTime.Unix(), DataCutoffAtUnixMillis: dataCutoffTime.UnixMilli(),
		Response: response,
	}
	if lease.client == nil {
		return snapshot, nil
	}
	published, publishErr := storeChannelModelDetectionOverviewRedisSnapshot(lease, snapshot)
	if publishErr != nil {
		err = publishErr
		common.SysError("写入模型检测 Redis 快照失败: " + err.Error())
		return snapshot, nil
	}
	if published {
		return snapshot, nil
	}
	if latest, exists := loadChannelModelDetectionOverviewRedisSnapshot(); exists {
		return latest, nil
	}
	return channelModelDetectionOverviewSnapshot{}, ErrChannelModelDetectionOverviewSnapshotUnavailable
}

func refreshChannelModelDetectionOverviewSnapshotInBackground(
	db *gorm.DB,
	generation uint64,
	previousRevision uint64,
	previousEventWatermark uint64,
	forceBuild bool,
) {
	if db == nil {
		return
	}
	gopool.Go(func() {
		key := "refresh:" + channelModelDetectionOverviewSingleflightKey(db, generation)
		_, _, _ = channelModelDetectionOverviewRefreshSingleflight.Do(key, func() (any, error) {
			if !forceBuild {
				if snapshot, exists := loadChannelModelDetectionOverviewRedisSnapshot(); exists &&
					(snapshot.Revision > previousRevision || snapshot.EventWatermark > previousEventWatermark) {
					storeChannelModelDetectionOverviewCacheEntry(db, generation, snapshot, channelModelDetectionOverviewCacheTTL(), channelModelDetectionOverviewStaleTTL())
					return nil, nil
				}
			}
			snapshot, err := buildChannelModelDetectionOverviewSnapshot(
				db, generation, previousRevision, previousEventWatermark, forceBuild,
			)
			if err != nil {
				if !errors.Is(err, ErrChannelModelDetectionOverviewSnapshotUnavailable) {
					common.SysError("后台刷新模型检测总览快照失败: " + err.Error())
				}
				return nil, err
			}
			storeChannelModelDetectionOverviewCacheEntry(db, generation, snapshot, channelModelDetectionOverviewCacheTTL(), channelModelDetectionOverviewStaleTTL())
			return nil, nil
		})
	})
}

// GetCachedChannelModelDetectionOverview returns a complete local or shared
// task snapshot. Only a process's first miss may synchronously build from DB;
// concurrent callers share that build, and other instances coordinate through
// a Redis lease instead of independently scanning the same tables.
func GetCachedChannelModelDetectionOverview(ctx context.Context, now int64) (ChannelModelDetectionOverviewResponse, error) {
	db := model.DB
	if db == nil {
		response, err := GetChannelModelDetectionOverview(ctx, nil, now)
		if err != nil {
			return ChannelModelDetectionOverviewResponse{}, err
		}
		generatedTime := time.Now()
		snapshot := channelModelDetectionOverviewSnapshot{
			Version:        channelModelDetectionOverviewSnapshotVersion,
			Revision:       channelModelDetectionOverviewLocalRevision.Add(1),
			EventWatermark: channelModelDetectionOverviewLocalEventWatermark.Load(),
			GeneratedAt:    generatedTime.Unix(), GeneratedAtUnixMillis: generatedTime.UnixMilli(),
			DataCutoffAt: response.ServerNow, DataCutoffAtUnixMillis: generatedTime.UnixMilli(), Response: response,
		}
		return channelModelDetectionOverviewResponseWithMetadata(snapshot, false, now), nil
	}
	ttl := channelModelDetectionOverviewCacheTTL()
	staleTTL := channelModelDetectionOverviewStaleTTL()
	if ttl <= 0 || staleTTL <= 0 {
		response, err := GetChannelModelDetectionOverview(ctx, db, now)
		if err != nil {
			return ChannelModelDetectionOverviewResponse{}, err
		}
		generatedTime := time.Now()
		snapshot := channelModelDetectionOverviewSnapshot{
			Version:        channelModelDetectionOverviewSnapshotVersion,
			Revision:       channelModelDetectionOverviewLocalRevision.Add(1),
			EventWatermark: channelModelDetectionOverviewLocalEventWatermark.Load(),
			GeneratedAt:    generatedTime.Unix(), GeneratedAtUnixMillis: generatedTime.UnixMilli(),
			DataCutoffAt: response.ServerNow, DataCutoffAtUnixMillis: generatedTime.UnixMilli(), Response: response,
		}
		return channelModelDetectionOverviewResponseWithMetadata(snapshot, false, now), nil
	}

	generation := channelModelDetectionOverviewCacheGeneration.Load()
	if entry, exists, stale := loadChannelModelDetectionOverviewCacheEntry(db, time.Now()); exists {
		if stale {
			if snapshot, revalidated := revalidateChannelModelDetectionOverviewCacheEntry(db, generation, entry, ttl); revalidated {
				return channelModelDetectionOverviewResponseWithMetadata(snapshot, false, now), nil
			}
			// Every expired snapshot is stale-while-revalidate. An entry can
			// expire naturally (TTL) or be explicitly invalidated by a mutation;
			// both cases must schedule a refresh after serving the last complete
			// value. Without this, a naturally expired entry would remain stale
			// until the maximum stale deadline with no rebuild.
			refreshChannelModelDetectionOverviewSnapshotInBackground(
				db, generation, entry.snapshot.Revision, entry.snapshot.EventWatermark, false,
			)
		}
		return channelModelDetectionOverviewResponseWithMetadata(entry.snapshot, stale, now), nil
	}

	key := channelModelDetectionOverviewSingleflightKey(db, generation)
	result, err, _ := channelModelDetectionOverviewLoadSingleflight.Do(key, func() (any, error) {
		if entry, exists, stale := loadChannelModelDetectionOverviewCacheEntry(db, time.Now()); exists {
			if stale {
				if snapshot, revalidated := revalidateChannelModelDetectionOverviewCacheEntry(db, generation, entry, ttl); revalidated {
					return channelModelDetectionOverviewResponseWithMetadata(snapshot, false, now), nil
				}
				refreshChannelModelDetectionOverviewSnapshotInBackground(
					db, generation, entry.snapshot.Revision, entry.snapshot.EventWatermark, false,
				)
			}
			return channelModelDetectionOverviewResponseWithMetadata(entry.snapshot, stale, now), nil
		}
		if snapshot, exists := loadChannelModelDetectionOverviewRedisSnapshot(); exists {
			if storeChannelModelDetectionOverviewCacheEntry(db, generation, snapshot, ttl, staleTTL) {
				stale := !time.Now().Before(channelModelDetectionOverviewSnapshotGeneratedTime(snapshot).Add(ttl))
				if stale {
					refreshChannelModelDetectionOverviewSnapshotInBackground(
						db, generation, snapshot.Revision, snapshot.EventWatermark, false,
					)
				}
				return channelModelDetectionOverviewResponseWithMetadata(snapshot, stale, now), nil
			}
		}

		channelModelDetectionOverviewCache.RLock()
		backoffUntil := channelModelDetectionOverviewCache.buildBackoff[db]
		channelModelDetectionOverviewCache.RUnlock()
		if time.Now().Before(backoffUntil) {
			return nil, ErrChannelModelDetectionOverviewSnapshotUnavailable
		}
		snapshot, buildErr := buildChannelModelDetectionOverviewSnapshot(db, generation, 0, 0, false)
		if buildErr != nil {
			channelModelDetectionOverviewCache.Lock()
			channelModelDetectionOverviewCache.buildBackoff[db] = time.Now().Add(ttl)
			channelModelDetectionOverviewCache.Unlock()
			return nil, buildErr
		}
		if !storeChannelModelDetectionOverviewCacheEntry(db, generation, snapshot, ttl, staleTTL) {
			return nil, ErrChannelModelDetectionOverviewSnapshotUnavailable
		}
		return channelModelDetectionOverviewResponseWithMetadata(snapshot, false, now), nil
	})
	if err != nil {
		return ChannelModelDetectionOverviewResponse{}, err
	}
	return result.(ChannelModelDetectionOverviewResponse), nil
}

func advanceChannelModelDetectionOverviewEventWatermark() uint64 {
	client := channelModelDetectionOverviewRedisClient(false)
	if client == nil || channelModelDetectionOverviewRedisTTL() <= 0 {
		return channelModelDetectionOverviewLocalEventWatermark.Add(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelModelDetectionOverviewRedisWriteTimeout)
	defer cancel()
	value, err := redis.NewScript(`
local function less(a, b)
  a = string.gsub(a or "0", "^0+", "")
  b = string.gsub(b or "0", "^0+", "")
  if a == "" then a = "0" end
  if b == "" then b = "0" end
  if string.len(a) ~= string.len(b) then
    return string.len(a) < string.len(b)
  end
  return a < b
end

local current = redis.call("GET", KEYS[1]) or "0"
if less(current, ARGV[1]) then
  redis.call("SET", KEYS[1], ARGV[1])
end
redis.call("INCR", KEYS[1])
return redis.call("GET", KEYS[1])
`).Run(
		ctx,
		client,
		[]string{channelModelDetectionOverviewWatermarkKey},
		strconv.FormatUint(channelModelDetectionOverviewLocalEventWatermark.Load(), 10),
	).Uint64()
	if err != nil {
		common.SysError("推进模型检测总览快照事件水位失败，使用本地水位: " + err.Error())
		return channelModelDetectionOverviewLocalEventWatermark.Add(1)
	}
	updateChannelModelDetectionOverviewCounter(&channelModelDetectionOverviewLocalEventWatermark, value)
	return value
}

// InvalidateChannelModelDetectionOverviewCache marks the last complete local
// snapshot stale without discarding it. Mutation boundaries should normally
// call NotifyChannelModelDetectionOverviewChanged to coalesce the rebuild.
func InvalidateChannelModelDetectionOverviewCache() {
	advanceChannelModelDetectionOverviewEventWatermark()
	now := time.Now()
	channelModelDetectionOverviewCache.Lock()
	newGeneration := channelModelDetectionOverviewCacheGeneration.Add(1)
	for db, entry := range channelModelDetectionOverviewCache.items {
		entry.generation = newGeneration
		entry.invalidated = true
		entry.expiresAt = now
		channelModelDetectionOverviewCache.items[db] = entry
	}
	channelModelDetectionOverviewCache.Unlock()
}

// NotifyChannelModelDetectionOverviewChanged advances the mutation watermark
// immediately and bounds rebuilds to one per coalescing window.
func NotifyChannelModelDetectionOverviewChanged() {
	InvalidateChannelModelDetectionOverviewCache()
	db := model.DB
	if db == nil {
		return
	}
	channelModelDetectionOverviewRefreshSchedule.Lock()
	defer channelModelDetectionOverviewRefreshSchedule.Unlock()
	if channelModelDetectionOverviewRefreshSchedule.timer != nil {
		if channelModelDetectionOverviewRefreshSchedule.db == db {
			return
		}
		channelModelDetectionOverviewRefreshSchedule.timer.Stop()
	}
	channelModelDetectionOverviewRefreshSchedule.db = db
	channelModelDetectionOverviewRefreshSchedule.timer = time.AfterFunc(
		channelModelDetectionOverviewRefreshCoalesce,
		func() {
			channelModelDetectionOverviewRefreshSchedule.Lock()
			if channelModelDetectionOverviewRefreshSchedule.db != db {
				channelModelDetectionOverviewRefreshSchedule.Unlock()
				return
			}
			channelModelDetectionOverviewRefreshSchedule.timer = nil
			channelModelDetectionOverviewRefreshSchedule.db = nil
			channelModelDetectionOverviewRefreshSchedule.Unlock()
			if model.DB != db {
				return
			}
			RefreshChannelModelDetectionOverviewSnapshot()
		},
	)
}

func refreshChannelModelDetectionOverviewSnapshot(forceBuild bool) {
	db := model.DB
	if db == nil {
		return
	}
	generation := channelModelDetectionOverviewCacheGeneration.Load()
	previousRevision := uint64(0)
	previousEventWatermark := uint64(0)
	if entry, exists, _ := loadChannelModelDetectionOverviewCacheEntry(db, time.Now()); exists {
		previousRevision = entry.snapshot.Revision
		previousEventWatermark = entry.snapshot.EventWatermark
	} else if snapshot, exists := loadChannelModelDetectionOverviewRedisSnapshot(); exists {
		previousRevision = snapshot.Revision
		previousEventWatermark = snapshot.EventWatermark
	}
	refreshChannelModelDetectionOverviewSnapshotInBackground(
		db, generation, previousRevision, previousEventWatermark, forceBuild,
	)
}

// RefreshChannelModelDetectionOverviewSnapshot asynchronously rebuilds and
// publishes the global model-detection task snapshot. It is safe to call from
// workers after every persisted state transition; local and Redis
// singleflight/leases collapse duplicate calls.
func RefreshChannelModelDetectionOverviewSnapshot() {
	refreshChannelModelDetectionOverviewSnapshot(true)
}

// WarmChannelModelDetectionOverviewSnapshot loads a shared snapshot during
// startup and only builds from DB when Redis does not already hold one.
func WarmChannelModelDetectionOverviewSnapshot() {
	heartbeatDB := model.DB
	if heartbeatDB == nil {
		return
	}
	generation := channelModelDetectionOverviewCacheGeneration.Load()
	if snapshot, exists := loadChannelModelDetectionOverviewRedisSnapshot(); exists {
		storeChannelModelDetectionOverviewCacheEntry(
			heartbeatDB,
			generation,
			snapshot,
			channelModelDetectionOverviewCacheTTL(),
			channelModelDetectionOverviewStaleTTL(),
		)
	} else {
		refreshChannelModelDetectionOverviewSnapshot(false)
	}
	channelModelDetectionOverviewHeartbeatOnce.Do(func() {
		gopool.Go(func() {
			ticker := time.NewTicker(channelModelDetectionOverviewHeartbeatInterval)
			defer ticker.Stop()
			for range ticker.C {
				if model.DB != heartbeatDB {
					return
				}
				refreshChannelModelDetectionOverviewSnapshot(false)
			}
		})
	})
}
