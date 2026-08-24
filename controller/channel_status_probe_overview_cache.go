package controller

import (
	"context"
	"crypto/sha256"
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
	channelStatusProbeOverviewSnapshotSchemaVersion         = 2
	channelStatusProbeOverviewCacheDefaultTTLMilliseconds   = 3000
	channelStatusProbeOverviewCacheMaxTTLMilliseconds       = 30000
	channelStatusProbeOverviewStaleDefaultTTLMilliseconds   = 30000
	channelStatusProbeOverviewStaleMaxTTLMilliseconds       = 300000
	channelStatusProbeOverviewRedisDefaultTTLSeconds        = 60
	channelStatusProbeOverviewRedisMaxTTLSeconds            = 600
	channelStatusProbeOverviewRedisReadTimeoutMilliseconds  = 100
	channelStatusProbeOverviewRedisWriteTimeoutMilliseconds = 250
	channelStatusProbeOverviewBuildLeaseTTL                 = 30 * time.Second
	channelStatusProbeOverviewBuildWait                     = 2 * time.Second
	channelStatusProbeOverviewBuildPollInterval             = 25 * time.Millisecond
	channelStatusProbeOverviewMaxFutureSkew                 = 5 * time.Second
	channelStatusProbeOverviewRedisKeyPrefix                = "channel_monitor:status_probe:overview:v2:"
	channelStatusProbeOverviewRevisionKey                   = "channel_monitor:status_probe:overview:v2:revision"
	channelStatusProbeOverviewEventWatermarkKey             = "channel_monitor:status_probe:overview:v2:event_watermark"
)

var errChannelStatusProbeOverviewSnapshotUnavailable = errors.New("渠道状态探测总览快照暂不可用")

// A fresh entry is served directly from this process. Once it expires, the
// last complete entry remains usable for stale-while-revalidate until
// staleUntil. This keeps a slow DB/Redis operation off the HTTP request path.
type channelStatusProbeOverviewCacheKey struct {
	db            *gorm.DB
	generation    uint64
	selectedModel string
}

type channelStatusProbeOverviewCacheEntry struct {
	expiresAt             time.Time
	staleUntil            time.Time
	revision              uint64
	eventWatermark        uint64
	generatedAt           int64
	generatedAtUnixMillis int64
	invalidated           bool
	response              channelStatusProbeOverviewResponse
}

type channelStatusProbeOverviewRedisSnapshot struct {
	SchemaVersion         int                                `json:"schema_version"`
	Revision              uint64                             `json:"revision"`
	EventWatermark        uint64                             `json:"event_watermark"`
	SelectedModel         string                             `json:"selected_model"`
	GeneratedAt           int64                              `json:"generated_at"`
	GeneratedAtUnixMillis int64                              `json:"generated_at_unix_millis"`
	Response              channelStatusProbeOverviewResponse `json:"data"`
}

type channelStatusProbeOverviewBuildLease struct {
	client         *redis.Client
	key            string
	fencingToken   uint64
	eventWatermark uint64
}

var channelStatusProbeOverviewCache = struct {
	sync.RWMutex
	items map[channelStatusProbeOverviewCacheKey]channelStatusProbeOverviewCacheEntry
}{
	items: make(map[channelStatusProbeOverviewCacheKey]channelStatusProbeOverviewCacheEntry),
}

var channelStatusProbeOverviewSingleflight singleflight.Group
var channelStatusProbeOverviewRefreshSingleflight singleflight.Group
var channelStatusProbeOverviewCacheGeneration atomic.Uint64
var channelStatusProbeOverviewLocalRevision atomic.Uint64
var channelStatusProbeOverviewLocalEventWatermark atomic.Uint64

var channelStatusProbeOverviewRefreshRuntime = struct {
	sync.Mutex
	stopped bool
	workers sync.WaitGroup
}{}

func StartChannelStatusProbeOverviewRefreshRuntime() {
	channelStatusProbeOverviewRefreshRuntime.Lock()
	channelStatusProbeOverviewRefreshRuntime.stopped = false
	channelStatusProbeOverviewRefreshRuntime.Unlock()
}

func StopChannelStatusProbeOverviewRefreshRuntime(ctx context.Context) error {
	channelStatusProbeOverviewRefreshRuntime.Lock()
	channelStatusProbeOverviewRefreshRuntime.stopped = true
	channelStatusProbeOverviewRefreshRuntime.Unlock()

	done := make(chan struct{})
	go func() {
		channelStatusProbeOverviewRefreshRuntime.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func channelStatusProbeOverviewCacheTTL() time.Duration {
	milliseconds := common.GetEnvOrDefault(
		"CHANNEL_STATUS_PROBE_OVERVIEW_CACHE_TTL_MS",
		channelStatusProbeOverviewCacheDefaultTTLMilliseconds,
	)
	if milliseconds <= 0 {
		return 0
	}
	milliseconds = min(milliseconds, channelStatusProbeOverviewCacheMaxTTLMilliseconds)
	return time.Duration(milliseconds) * time.Millisecond
}

func channelStatusProbeOverviewStaleTTL() time.Duration {
	milliseconds := common.GetEnvOrDefault(
		"CHANNEL_STATUS_PROBE_OVERVIEW_STALE_TTL_MS",
		channelStatusProbeOverviewStaleDefaultTTLMilliseconds,
	)
	if milliseconds <= 0 {
		return 0
	}
	milliseconds = min(milliseconds, channelStatusProbeOverviewStaleMaxTTLMilliseconds)
	return time.Duration(milliseconds) * time.Millisecond
}

func channelStatusProbeOverviewRedisTTL() time.Duration {
	seconds := common.GetEnvOrDefault(
		"CHANNEL_STATUS_PROBE_OVERVIEW_REDIS_TTL_SECONDS",
		channelStatusProbeOverviewRedisDefaultTTLSeconds,
	)
	if seconds <= 0 {
		return 0
	}
	seconds = min(seconds, channelStatusProbeOverviewRedisMaxTTLSeconds)
	return time.Duration(seconds) * time.Second
}

func (key channelStatusProbeOverviewCacheKey) singleflightKey() string {
	return fmt.Sprintf("%p:%d:%q", key.db, key.generation, key.selectedModel)
}

func channelStatusProbeOverviewRedisKey(selectedModel string) string {
	// Model names are user supplied query values. Hashing keeps the key bounded
	// and prevents separators in a model name from changing the key layout.
	digest := sha256.Sum256([]byte(selectedModel))
	return fmt.Sprintf("%s%x", channelStatusProbeOverviewRedisKeyPrefix, digest[:])
}

func channelStatusProbeOverviewResponseWithMetadata(
	snapshot channelStatusProbeOverviewRedisSnapshot,
	stale bool,
) channelStatusProbeOverviewResponse {
	response := snapshot.Response
	serverNow := common.GetTimestamp()
	if response.ServerNow < serverNow {
		response.ServerNow = serverNow
	}
	response.SnapshotVersion = snapshot.SchemaVersion
	response.SnapshotRevision = snapshot.Revision
	response.EventWatermark = snapshot.EventWatermark
	response.GeneratedAt = snapshot.GeneratedAt
	response.Stale = stale
	if snapshot.GeneratedAt > 0 {
		age := serverNow - snapshot.GeneratedAt
		if age < 0 {
			age = 0
		}
		response.SnapshotAgeSeconds = age
	}
	return response
}

func channelStatusProbeOverviewSnapshotGeneratedTime(snapshot channelStatusProbeOverviewRedisSnapshot) time.Time {
	return time.UnixMilli(snapshot.GeneratedAtUnixMillis)
}

func updateChannelStatusProbeOverviewCounter(counter *atomic.Uint64, value uint64) {
	for current := counter.Load(); current < value; current = counter.Load() {
		if counter.CompareAndSwap(current, value) {
			return
		}
	}
}

func newChannelStatusProbeOverviewSnapshot(
	selectedModel string,
	revision uint64,
	eventWatermark uint64,
	response channelStatusProbeOverviewResponse,
) channelStatusProbeOverviewRedisSnapshot {
	generatedTime := time.Now()
	return channelStatusProbeOverviewRedisSnapshot{
		SchemaVersion: channelStatusProbeOverviewSnapshotSchemaVersion,
		Revision:      revision, EventWatermark: eventWatermark, SelectedModel: selectedModel,
		GeneratedAt: generatedTime.Unix(), GeneratedAtUnixMillis: generatedTime.UnixMilli(),
		Response: response,
	}
}

func channelStatusProbeOverviewCacheEntryFromSnapshot(
	snapshot channelStatusProbeOverviewRedisSnapshot,
	ttl time.Duration,
	staleTTL time.Duration,
) channelStatusProbeOverviewCacheEntry {
	generatedTime := channelStatusProbeOverviewSnapshotGeneratedTime(snapshot)
	return channelStatusProbeOverviewCacheEntry{
		expiresAt: generatedTime.Add(ttl), staleUntil: generatedTime.Add(ttl + staleTTL),
		revision: snapshot.Revision, eventWatermark: snapshot.EventWatermark,
		generatedAt: snapshot.GeneratedAt, generatedAtUnixMillis: snapshot.GeneratedAtUnixMillis,
		response: snapshot.Response,
	}
}

func channelStatusProbeOverviewEntryNewer(
	candidate channelStatusProbeOverviewCacheEntry,
	current channelStatusProbeOverviewCacheEntry,
) bool {
	return candidate.revision > current.revision &&
		candidate.eventWatermark >= current.eventWatermark &&
		candidate.generatedAtUnixMillis >= current.generatedAtUnixMillis
}

func loadChannelStatusProbeOverviewCacheEntry(
	key channelStatusProbeOverviewCacheKey,
	now time.Time,
) (channelStatusProbeOverviewCacheEntry, bool, bool) {
	channelStatusProbeOverviewCache.RLock()
	entry, exists := channelStatusProbeOverviewCache.items[key]
	channelStatusProbeOverviewCache.RUnlock()
	if !exists {
		return channelStatusProbeOverviewCacheEntry{}, false, false
	}
	if !entry.invalidated && now.Before(entry.expiresAt) {
		return entry, true, false
	}
	if now.Before(entry.staleUntil) {
		return entry, true, true
	}
	return channelStatusProbeOverviewCacheEntry{}, false, false
}

func storeChannelStatusProbeOverviewCacheEntry(
	key channelStatusProbeOverviewCacheKey,
	now time.Time,
	ttl time.Duration,
	staleTTL time.Duration,
	snapshot channelStatusProbeOverviewRedisSnapshot,
) bool {
	if snapshot.SchemaVersion != channelStatusProbeOverviewSnapshotSchemaVersion ||
		snapshot.Revision == 0 || snapshot.GeneratedAt <= 0 || snapshot.GeneratedAtUnixMillis <= 0 {
		return false
	}
	entry := channelStatusProbeOverviewCacheEntryFromSnapshot(snapshot, ttl, staleTTL)
	if time.Unix(snapshot.GeneratedAt, 0).After(now.Add(channelStatusProbeOverviewMaxFutureSkew)) ||
		time.UnixMilli(snapshot.GeneratedAtUnixMillis).After(now.Add(channelStatusProbeOverviewMaxFutureSkew)) {
		return false
	}
	if !now.Before(entry.staleUntil) {
		return false
	}
	channelStatusProbeOverviewCache.Lock()
	defer channelStatusProbeOverviewCache.Unlock()
	if key.generation != channelStatusProbeOverviewCacheGeneration.Load() {
		return false
	}
	for cachedKey, entry := range channelStatusProbeOverviewCache.items {
		if !now.Before(entry.staleUntil) {
			delete(channelStatusProbeOverviewCache.items, cachedKey)
		}
	}
	if current, exists := channelStatusProbeOverviewCache.items[key]; exists &&
		!channelStatusProbeOverviewEntryNewer(entry, current) {
		return false
	}
	channelStatusProbeOverviewCache.items[key] = entry
	updateChannelStatusProbeOverviewCounter(&channelStatusProbeOverviewLocalRevision, snapshot.Revision)
	updateChannelStatusProbeOverviewCounter(&channelStatusProbeOverviewLocalEventWatermark, snapshot.EventWatermark)
	return true
}

func channelStatusProbeOverviewRedisClient(read bool) *redis.Client {
	if !common.RedisEnabled {
		return nil
	}
	if read {
		return common.RedisMonitorReadClient()
	}
	return common.RedisMonitorWriteClient()
}

func loadChannelStatusProbeOverviewRedisSnapshot(
	selectedModel string,
	ttl time.Duration,
	staleTTL time.Duration,
) (channelStatusProbeOverviewCacheEntry, bool) {
	if ttl <= 0 || staleTTL <= 0 || channelStatusProbeOverviewRedisTTL() <= 0 {
		return channelStatusProbeOverviewCacheEntry{}, false
	}
	client := channelStatusProbeOverviewRedisClient(true)
	if client == nil {
		return channelStatusProbeOverviewCacheEntry{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelStatusProbeOverviewRedisReadTimeoutMilliseconds*time.Millisecond)
	defer cancel()
	redisKey := channelStatusProbeOverviewRedisKey(selectedModel)
	values, err := redis.NewScript(`
local payload = redis.call("GET", KEYS[1])
if not payload then
  return {}
end
return {
  payload,
  redis.call("HGET", KEYS[2], "revision") or "0",
  redis.call("HGET", KEYS[2], "event_watermark") or "0",
  redis.call("GET", KEYS[3]) or "0"
}
`).Run(ctx, client, []string{
		redisKey,
		redisKey + ":meta",
		channelStatusProbeOverviewEventWatermarkKey,
	}).StringSlice()
	if err != nil {
		if err != redis.Nil {
			common.SysError("读取渠道状态探测 Redis 快照失败: " + err.Error())
		}
		return channelStatusProbeOverviewCacheEntry{}, false
	}
	if len(values) != 4 {
		return channelStatusProbeOverviewCacheEntry{}, false
	}
	var snapshot channelStatusProbeOverviewRedisSnapshot
	if err = common.Unmarshal([]byte(values[0]), &snapshot); err != nil ||
		snapshot.SchemaVersion != channelStatusProbeOverviewSnapshotSchemaVersion ||
		snapshot.Revision == 0 || snapshot.SelectedModel != selectedModel ||
		snapshot.GeneratedAt <= 0 || snapshot.GeneratedAtUnixMillis <= 0 {
		return channelStatusProbeOverviewCacheEntry{}, false
	}
	metadataRevision, revisionErr := strconv.ParseUint(values[1], 10, 64)
	metadataWatermark, watermarkErr := strconv.ParseUint(values[2], 10, 64)
	currentWatermark, currentWatermarkErr := strconv.ParseUint(values[3], 10, 64)
	if revisionErr != nil || watermarkErr != nil || currentWatermarkErr != nil ||
		snapshot.Revision != metadataRevision || snapshot.EventWatermark != metadataWatermark ||
		snapshot.EventWatermark < currentWatermark {
		return channelStatusProbeOverviewCacheEntry{}, false
	}
	now := time.Now()
	generatedTime := channelStatusProbeOverviewSnapshotGeneratedTime(snapshot)
	// Redis is an internal source, but a malformed or clock-skewed payload
	// must not make the API report a permanently zero-age snapshot. Keep the
	// same bounded-future guard used by the model-detection snapshot.
	if time.Unix(snapshot.GeneratedAt, 0).After(now.Add(channelStatusProbeOverviewMaxFutureSkew)) ||
		generatedTime.After(now.Add(channelStatusProbeOverviewMaxFutureSkew)) {
		return channelStatusProbeOverviewCacheEntry{}, false
	}
	entry := channelStatusProbeOverviewCacheEntryFromSnapshot(snapshot, ttl, staleTTL)
	if !now.Before(entry.staleUntil) {
		return channelStatusProbeOverviewCacheEntry{}, false
	}
	updateChannelStatusProbeOverviewCounter(&channelStatusProbeOverviewLocalRevision, snapshot.Revision)
	// The envelope is authoritative even when the standalone watermark key is
	// missing/expired. Keeping only currentWatermark here would let the local
	// monotonic counter move backwards after a Redis key expiry.
	updateChannelStatusProbeOverviewCounter(&channelStatusProbeOverviewLocalEventWatermark, snapshot.EventWatermark)
	return entry, true
}

func storeChannelStatusProbeOverviewRedisSnapshot(
	selectedModel string,
	lease channelStatusProbeOverviewBuildLease,
	snapshot channelStatusProbeOverviewRedisSnapshot,
) (bool, error) {
	redisTTL := channelStatusProbeOverviewRedisTTL()
	if lease.client == nil || lease.fencingToken == 0 || redisTTL <= 0 ||
		snapshot.Revision != lease.fencingToken || snapshot.EventWatermark != lease.eventWatermark {
		return false, nil
	}
	now := time.Now()
	if snapshot.GeneratedAt <= 0 || snapshot.GeneratedAtUnixMillis <= 0 ||
		time.Unix(snapshot.GeneratedAt, 0).After(now.Add(channelStatusProbeOverviewMaxFutureSkew)) ||
		time.UnixMilli(snapshot.GeneratedAtUnixMillis).After(now.Add(channelStatusProbeOverviewMaxFutureSkew)) {
		return false, nil
	}
	payload, err := common.Marshal(snapshot)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelStatusProbeOverviewRedisWriteTimeoutMilliseconds*time.Millisecond)
	defer cancel()
	redisKey := channelStatusProbeOverviewRedisKey(selectedModel)
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

if redis.call("GET", KEYS[3]) ~= ARGV[1] then
  return -1
end
local current_watermark = redis.call("GET", KEYS[4]) or "0"
if less(ARGV[4], current_watermark) then
  return -2
end
local old_revision = redis.call("HGET", KEYS[2], "revision") or "0"
local old_watermark = redis.call("HGET", KEYS[2], "event_watermark") or "0"
local old_generated = redis.call("HGET", KEYS[2], "generated_at_unix_millis") or "0"
if not less(old_revision, ARGV[3]) or less(ARGV[4], old_watermark) or
   less(ARGV[5], old_generated) then
  return 0
end
redis.call("SET", KEYS[1], ARGV[2], "PX", ARGV[6])
redis.call("HSET", KEYS[2],
  "revision", ARGV[3],
  "event_watermark", ARGV[4],
  "generated_at_unix_millis", ARGV[5])
redis.call("PEXPIRE", KEYS[2], ARGV[6])
return 1
`).Run(ctx, lease.client, []string{
		redisKey,
		redisKey + ":meta",
		lease.key,
		channelStatusProbeOverviewEventWatermarkKey,
	},
		strconv.FormatUint(lease.fencingToken, 10),
		payload,
		strconv.FormatUint(snapshot.Revision, 10),
		strconv.FormatUint(snapshot.EventWatermark, 10),
		strconv.FormatInt(snapshot.GeneratedAtUnixMillis, 10),
		strconv.FormatInt(redisTTL.Milliseconds(), 10),
	).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func channelStatusProbeOverviewSnapshotFromEntry(
	selectedModel string,
	entry channelStatusProbeOverviewCacheEntry,
) channelStatusProbeOverviewRedisSnapshot {
	return channelStatusProbeOverviewRedisSnapshot{
		SchemaVersion: channelStatusProbeOverviewSnapshotSchemaVersion,
		Revision:      entry.revision, EventWatermark: entry.eventWatermark,
		SelectedModel: selectedModel, GeneratedAt: entry.generatedAt,
		GeneratedAtUnixMillis: entry.generatedAtUnixMillis, Response: entry.response,
	}
}

func acquireChannelStatusProbeOverviewBuildLease(
	selectedModel string,
) (channelStatusProbeOverviewBuildLease, bool, error) {
	client := channelStatusProbeOverviewRedisClient(false)
	if client == nil || channelStatusProbeOverviewRedisTTL() <= 0 {
		return channelStatusProbeOverviewBuildLease{}, false, errChannelStatusProbeOverviewSnapshotUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelStatusProbeOverviewRedisWriteTimeoutMilliseconds*time.Millisecond)
	defer cancel()
	leaseKey := channelStatusProbeOverviewRedisKey(selectedModel) + ":lease"
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
local token = redis.call("GET", KEYS[2])
redis.call("PSETEX", KEYS[1], ARGV[1], token)
return {token, watermark}
`).Run(ctx, client, []string{
		leaseKey,
		channelStatusProbeOverviewRevisionKey,
		channelStatusProbeOverviewEventWatermarkKey,
	},
		strconv.FormatInt(channelStatusProbeOverviewBuildLeaseTTL.Milliseconds(), 10),
		strconv.FormatUint(channelStatusProbeOverviewLocalRevision.Load(), 10),
		strconv.FormatUint(channelStatusProbeOverviewLocalEventWatermark.Load(), 10),
	).StringSlice()
	if err != nil {
		return channelStatusProbeOverviewBuildLease{}, false, err
	}
	if len(values) != 2 {
		return channelStatusProbeOverviewBuildLease{}, false, errChannelStatusProbeOverviewSnapshotUnavailable
	}
	token, tokenErr := strconv.ParseUint(values[0], 10, 64)
	watermark, watermarkErr := strconv.ParseUint(values[1], 10, 64)
	if tokenErr != nil || watermarkErr != nil {
		return channelStatusProbeOverviewBuildLease{}, false, errChannelStatusProbeOverviewSnapshotUnavailable
	}
	updateChannelStatusProbeOverviewCounter(&channelStatusProbeOverviewLocalEventWatermark, watermark)
	if token == 0 {
		return channelStatusProbeOverviewBuildLease{}, false, nil
	}
	updateChannelStatusProbeOverviewCounter(&channelStatusProbeOverviewLocalRevision, token)
	return channelStatusProbeOverviewBuildLease{
		client: client, key: leaseKey, fencingToken: token, eventWatermark: watermark,
	}, true, nil
}

func releaseChannelStatusProbeOverviewBuildLease(lease channelStatusProbeOverviewBuildLease) {
	if lease.client == nil || lease.key == "" || lease.fencingToken == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelStatusProbeOverviewRedisWriteTimeoutMilliseconds*time.Millisecond)
	defer cancel()
	_ = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`).Run(ctx, lease.client, []string{lease.key}, strconv.FormatUint(lease.fencingToken, 10)).Err()
}

func waitForChannelStatusProbeOverviewRedisSnapshot(
	selectedModel string,
	ttl time.Duration,
	staleTTL time.Duration,
	previousRevision uint64,
	previousEventWatermark uint64,
) (channelStatusProbeOverviewCacheEntry, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), channelStatusProbeOverviewBuildWait)
	defer cancel()
	ticker := time.NewTicker(channelStatusProbeOverviewBuildPollInterval)
	defer ticker.Stop()
	for {
		if entry, exists := loadChannelStatusProbeOverviewRedisSnapshot(selectedModel, ttl, staleTTL); exists &&
			(entry.revision > previousRevision || entry.eventWatermark > previousEventWatermark) {
			return entry, true
		}
		select {
		case <-ctx.Done():
			return channelStatusProbeOverviewCacheEntry{}, false
		case <-ticker.C:
		}
	}
}

func buildChannelStatusProbeOverviewSnapshot(
	key channelStatusProbeOverviewCacheKey,
	ttl time.Duration,
	staleTTL time.Duration,
	previousRevision uint64,
	previousEventWatermark uint64,
) (channelStatusProbeOverviewRedisSnapshot, error) {
	lease, acquired, leaseErr := acquireChannelStatusProbeOverviewBuildLease(key.selectedModel)
	if leaseErr == nil && !acquired {
		if entry, exists := waitForChannelStatusProbeOverviewRedisSnapshot(
			key.selectedModel, ttl, staleTTL, previousRevision, previousEventWatermark,
		); exists {
			return channelStatusProbeOverviewSnapshotFromEntry(key.selectedModel, entry), nil
		}
		return channelStatusProbeOverviewRedisSnapshot{}, errChannelStatusProbeOverviewSnapshotUnavailable
	}
	if acquired {
		defer releaseChannelStatusProbeOverviewBuildLease(lease)
	} else if leaseErr != nil && !errors.Is(leaseErr, errChannelStatusProbeOverviewSnapshotUnavailable) {
		common.SysError("获取渠道状态探测快照重建租约失败，使用本地重建: " + leaseErr.Error())
	}

	response, err := buildChannelStatusProbeOverview(key.selectedModel, common.GetTimestamp())
	if err != nil {
		return channelStatusProbeOverviewRedisSnapshot{}, err
	}
	if key.generation != channelStatusProbeOverviewCacheGeneration.Load() {
		return channelStatusProbeOverviewRedisSnapshot{}, errChannelStatusProbeOverviewSnapshotUnavailable
	}
	if !acquired {
		return newChannelStatusProbeOverviewSnapshot(
			key.selectedModel,
			channelStatusProbeOverviewLocalRevision.Add(1),
			channelStatusProbeOverviewLocalEventWatermark.Load(),
			response,
		), nil
	}
	snapshot := newChannelStatusProbeOverviewSnapshot(
		key.selectedModel, lease.fencingToken, lease.eventWatermark, response,
	)
	published, publishErr := storeChannelStatusProbeOverviewRedisSnapshot(key.selectedModel, lease, snapshot)
	if publishErr != nil {
		common.SysError("写入渠道状态探测 Redis 快照失败，保留本地完整副本: " + publishErr.Error())
		return snapshot, nil
	}
	if published {
		return snapshot, nil
	}
	if entry, exists := loadChannelStatusProbeOverviewRedisSnapshot(key.selectedModel, ttl, staleTTL); exists {
		return channelStatusProbeOverviewSnapshotFromEntry(key.selectedModel, entry), nil
	}
	return channelStatusProbeOverviewRedisSnapshot{}, errChannelStatusProbeOverviewSnapshotUnavailable
}

func refreshChannelStatusProbeOverviewInBackground(
	key channelStatusProbeOverviewCacheKey,
	ttl time.Duration,
	staleTTL time.Duration,
	previousRevision uint64,
	previousEventWatermark uint64,
) {
	if ttl <= 0 || staleTTL <= 0 {
		return
	}
	channelStatusProbeOverviewRefreshRuntime.Lock()
	if channelStatusProbeOverviewRefreshRuntime.stopped {
		channelStatusProbeOverviewRefreshRuntime.Unlock()
		return
	}
	channelStatusProbeOverviewRefreshRuntime.workers.Add(1)
	channelStatusProbeOverviewRefreshRuntime.Unlock()
	gopool.Go(func() {
		defer channelStatusProbeOverviewRefreshRuntime.workers.Done()
		_, _, _ = channelStatusProbeOverviewRefreshSingleflight.Do("refresh:"+key.singleflightKey(), func() (any, error) {
			if entry, exists := loadChannelStatusProbeOverviewRedisSnapshot(key.selectedModel, ttl, staleTTL); exists &&
				(entry.revision > previousRevision || entry.eventWatermark > previousEventWatermark) {
				storeChannelStatusProbeOverviewCacheEntry(
					key, time.Now(), ttl, staleTTL,
					channelStatusProbeOverviewSnapshotFromEntry(key.selectedModel, entry),
				)
				return nil, nil
			}
			snapshot, err := buildChannelStatusProbeOverviewSnapshot(
				key, ttl, staleTTL, previousRevision, previousEventWatermark,
			)
			if err != nil {
				if !errors.Is(err, errChannelStatusProbeOverviewSnapshotUnavailable) {
					common.SysError("后台刷新渠道状态探测快照失败: " + err.Error())
				}
				return nil, err
			}
			storeChannelStatusProbeOverviewCacheEntry(key, time.Now(), ttl, staleTTL, snapshot)
			return nil, nil
		})
	})
}

func getChannelStatusProbeOverviewCached(selectedModel string) (channelStatusProbeOverviewResponse, error) {
	ttl := channelStatusProbeOverviewCacheTTL()
	if ttl <= 0 {
		response, err := buildChannelStatusProbeOverview(selectedModel, common.GetTimestamp())
		if err != nil {
			return channelStatusProbeOverviewResponse{}, err
		}
		snapshot := newChannelStatusProbeOverviewSnapshot(
			selectedModel,
			channelStatusProbeOverviewLocalRevision.Add(1),
			channelStatusProbeOverviewLocalEventWatermark.Load(),
			response,
		)
		return channelStatusProbeOverviewResponseWithMetadata(snapshot, false), nil
	}
	staleTTL := channelStatusProbeOverviewStaleTTL()
	for {
		key := channelStatusProbeOverviewCacheKey{
			db: model.DB, generation: channelStatusProbeOverviewCacheGeneration.Load(), selectedModel: selectedModel,
		}
		now := time.Now()
		if entry, exists, stale := loadChannelStatusProbeOverviewCacheEntry(key, now); exists {
			if key.generation != channelStatusProbeOverviewCacheGeneration.Load() {
				continue
			}
			if stale {
				refreshChannelStatusProbeOverviewInBackground(
					key, ttl, staleTTL, entry.revision, entry.eventWatermark,
				)
			}
			return channelStatusProbeOverviewResponseWithMetadata(
				channelStatusProbeOverviewSnapshotFromEntry(selectedModel, entry), stale,
			), nil
		}

		result, err, _ := channelStatusProbeOverviewSingleflight.Do(key.singleflightKey(), func() (any, error) {
			loadTime := time.Now()
			if entry, exists, stale := loadChannelStatusProbeOverviewCacheEntry(key, loadTime); exists {
				if stale {
					refreshChannelStatusProbeOverviewInBackground(
						key, ttl, staleTTL, entry.revision, entry.eventWatermark,
					)
				}
				return channelStatusProbeOverviewResponseWithMetadata(
					channelStatusProbeOverviewSnapshotFromEntry(selectedModel, entry), stale,
				), nil
			}
			// First miss: use a shared Redis snapshot before falling back to the
			// controlled synchronous DB build. This preserves API availability
			// during startup while avoiding per-request DB scans thereafter.
			if entry, exists := loadChannelStatusProbeOverviewRedisSnapshot(selectedModel, ttl, staleTTL); exists {
				storeChannelStatusProbeOverviewCacheEntry(
					key, loadTime, ttl, staleTTL,
					channelStatusProbeOverviewSnapshotFromEntry(selectedModel, entry),
				)
				stale := !loadTime.Before(entry.expiresAt)
				if stale {
					refreshChannelStatusProbeOverviewInBackground(
						key, ttl, staleTTL, entry.revision, entry.eventWatermark,
					)
				}
				return channelStatusProbeOverviewResponseWithMetadata(
					channelStatusProbeOverviewSnapshotFromEntry(selectedModel, entry), stale,
				), nil
			}
			snapshot, loadErr := buildChannelStatusProbeOverviewSnapshot(key, ttl, staleTTL, 0, 0)
			if loadErr != nil {
				return nil, loadErr
			}
			if !storeChannelStatusProbeOverviewCacheEntry(key, loadTime, ttl, staleTTL, snapshot) {
				return nil, errChannelStatusProbeOverviewSnapshotUnavailable
			}
			return channelStatusProbeOverviewResponseWithMetadata(snapshot, false), nil
		})
		if key.generation != channelStatusProbeOverviewCacheGeneration.Load() {
			continue
		}
		if err != nil {
			return channelStatusProbeOverviewResponse{}, err
		}
		return result.(channelStatusProbeOverviewResponse), nil
	}
}

func advanceChannelStatusProbeOverviewEventWatermark() uint64 {
	client := channelStatusProbeOverviewRedisClient(false)
	if client == nil || channelStatusProbeOverviewRedisTTL() <= 0 {
		return channelStatusProbeOverviewLocalEventWatermark.Add(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelStatusProbeOverviewRedisWriteTimeoutMilliseconds*time.Millisecond)
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
		[]string{channelStatusProbeOverviewEventWatermarkKey},
		strconv.FormatUint(channelStatusProbeOverviewLocalEventWatermark.Load(), 10),
	).Uint64()
	if err != nil {
		common.SysError("推进渠道状态探测快照事件水位失败，使用本地水位: " + err.Error())
		return channelStatusProbeOverviewLocalEventWatermark.Add(1)
	}
	updateChannelStatusProbeOverviewCounter(&channelStatusProbeOverviewLocalEventWatermark, value)
	return value
}

func invalidateChannelStatusProbeOverviewCache() {
	advanceChannelStatusProbeOverviewEventWatermark()
	now := time.Now()
	channelStatusProbeOverviewCache.Lock()
	newGeneration := channelStatusProbeOverviewCacheGeneration.Add(1)
	newItems := make(map[channelStatusProbeOverviewCacheKey]channelStatusProbeOverviewCacheEntry, len(channelStatusProbeOverviewCache.items))
	for key, entry := range channelStatusProbeOverviewCache.items {
		if !now.Before(entry.staleUntil) {
			continue
		}
		entry.expiresAt = now
		entry.invalidated = true
		newKey := key
		newKey.generation = newGeneration
		newItems[newKey] = entry
	}
	channelStatusProbeOverviewCache.items = newItems
	channelStatusProbeOverviewCache.Unlock()

	// Keep the last complete snapshot within its original stale deadline. The
	// event watermark prevents a builder that started before this invalidation
	// from publishing over a newer cross-instance view.
}
