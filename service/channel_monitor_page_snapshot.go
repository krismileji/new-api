package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"golang.org/x/sync/singleflight"
)

const (
	channelMonitorPageSnapshotSchemaVersion = 1
	channelMonitorPageSnapshotFreshTTL      = time.Second
	channelMonitorPageSnapshotRetention     = 5 * time.Minute
	channelMonitorPageSnapshotLeaseTTL      = 30 * time.Second
	channelMonitorPageSnapshotPollInterval  = 25 * time.Millisecond
	channelMonitorPageSnapshotMaxLocalItems = 256
	// Keep the process-local outage fallback bounded by bytes as well as entry
	// count. A single valid response may be as large as MaxPayload, so the
	// count-only limit could otherwise retain roughly 4 GiB per process.
	channelMonitorPageSnapshotMaxLocalBytes = 128 << 20
	channelMonitorPageSnapshotMaxPayload    = 16 << 20
	channelMonitorPageSnapshotMaxFutureSkew = 5 * time.Second
	// Page builders can fan out to several database and Redis reads. Keep the
	// process-wide rebuild concurrency bounded even when management refresh
	// fans out across several identities at once.
	channelMonitorPageSnapshotMaxConcurrentBuilds = 2

	channelMonitorPageSnapshotPrefix = ChannelMonitorRedisKeyPrefix + ":page_snapshot:"
)

var (
	ErrChannelMonitorPageSnapshotUnavailable  = errors.New("渠道监控页面快照存储不可用")
	ErrChannelMonitorPageSnapshotMissing      = errors.New("渠道监控页面快照不存在")
	ErrChannelMonitorPageSnapshotRefreshing   = errors.New("渠道监控页面快照正在生成")
	ErrChannelMonitorPageSnapshotNotCacheable = errors.New(
		"渠道监控页面响应不可缓存",
	)
)

var channelMonitorPageSnapshotBuildSemaphore = make(
	chan struct{}, channelMonitorPageSnapshotMaxConcurrentBuilds,
)

func acquireChannelMonitorPageSnapshotBuild(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case channelMonitorPageSnapshotBuildSemaphore <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func releaseChannelMonitorPageSnapshotBuild() {
	select {
	case <-channelMonitorPageSnapshotBuildSemaphore:
	default:
	}
}

type ChannelMonitorPageSnapshotState string

const (
	ChannelMonitorPageSnapshotFresh   ChannelMonitorPageSnapshotState = "fresh"
	ChannelMonitorPageSnapshotStale   ChannelMonitorPageSnapshotState = "stale"
	ChannelMonitorPageSnapshotMissing ChannelMonitorPageSnapshotState = "missing"
)

// ChannelMonitorPageSnapshotQuery is the complete cache identity. Permission
// scope and filters are hashed before becoming part of a Redis key.
type ChannelMonitorPageSnapshotQuery struct {
	Page            string
	Version         string
	PermissionScope string
	WindowStart     int64
	WindowEnd       int64
	Filters         map[string][]string
}

// ChannelMonitorPageSnapshot stores one complete HTTP response and its
// projection metadata. Payload must already be sanitized for the caller's
// permission scope.
type ChannelMonitorPageSnapshot struct {
	SchemaVersion        int    `json:"schema_version"`
	IdentityHash         string `json:"identity_hash"`
	Revision             uint64 `json:"revision"`
	GeneratedAt          int64  `json:"generated_at"`
	GeneratedAtUnixMilli int64  `json:"generated_at_unix_milli"`
	DataCutoffAt         int64  `json:"data_cutoff_at"`
	EventWatermark       uint64 `json:"event_watermark"`
	StatusCode           int    `json:"status_code"`
	ContentType          string `json:"content_type"`
	Payload              []byte `json:"payload"`
}

type ChannelMonitorPageSnapshotBuilder func(context.Context) (ChannelMonitorPageSnapshot, error)

type channelMonitorPageSnapshotStore struct {
	readClient  func() *redis.Client
	writeClient func() *redis.Client

	refresh singleflight.Group
	pending sync.Map

	localMu    sync.RWMutex
	local      map[string]ChannelMonitorPageSnapshot
	localBytes int64
}

var defaultChannelMonitorPageSnapshotStore = &channelMonitorPageSnapshotStore{
	readClient:  common.RedisMonitorReadClient,
	writeClient: common.RedisMonitorWriteClient,
}

func ChannelMonitorPageSnapshotKey(query ChannelMonitorPageSnapshotQuery) (string, error) {
	identityHash, err := channelMonitorPageSnapshotIdentityHash(query)
	if err != nil {
		return "", err
	}
	return channelMonitorPageSnapshotPrefix +
		channelMonitorRedisKeyPart(strings.ToLower(strings.TrimSpace(query.Page)), "page") + ":" + identityHash, nil
}

func LoadChannelMonitorPageSnapshot(
	ctx context.Context,
	query ChannelMonitorPageSnapshotQuery,
) (ChannelMonitorPageSnapshot, ChannelMonitorPageSnapshotState, error) {
	if !common.RedisEnabled {
		// Redis may be disabled or temporarily fenced off while this process
		// still has a complete local copy. Serve that copy within the same stale
		// deadline; do not turn a Redis outage into a synchronous DB rebuild.
		return defaultChannelMonitorPageSnapshotStore.loadLocal(
			query, time.Now(), ErrChannelMonitorPageSnapshotUnavailable,
		)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return defaultChannelMonitorPageSnapshotStore.load(ctx, query, time.Now())
}

func RefreshChannelMonitorPageSnapshot(
	ctx context.Context,
	query ChannelMonitorPageSnapshotQuery,
	builder ChannelMonitorPageSnapshotBuilder,
) (ChannelMonitorPageSnapshot, error) {
	if !common.RedisEnabled {
		return ChannelMonitorPageSnapshot{}, ErrChannelMonitorPageSnapshotUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return defaultChannelMonitorPageSnapshotStore.refreshSnapshotForce(ctx, query, builder)
}

// RequestChannelMonitorPageSnapshotRefresh submits at most one local refresh
// per cache identity. The Redis lease inside refreshSnapshot provides the
// cross-instance single-writer guarantee.
func RequestChannelMonitorPageSnapshotRefresh(
	query ChannelMonitorPageSnapshotQuery,
	builder ChannelMonitorPageSnapshotBuilder,
) bool {
	if builder == nil {
		return false
	}
	return defaultChannelMonitorPageSnapshotStore.requestRefresh(query, builder)
}

func (store *channelMonitorPageSnapshotStore) requestRefresh(
	query ChannelMonitorPageSnapshotQuery,
	builder ChannelMonitorPageSnapshotBuilder,
) bool {
	if builder == nil {
		return false
	}
	key, err := ChannelMonitorPageSnapshotKey(query)
	if err != nil {
		return false
	}
	if _, loaded := store.pending.LoadOrStore(key, struct{}{}); loaded {
		return false
	}
	go func() {
		defer store.pending.Delete(key)
		ctx, cancel := context.WithTimeout(context.Background(), channelMonitorPageSnapshotLeaseTTL)
		defer cancel()
		if !common.RedisEnabled || store.writeClient() == nil {
			_, _ = store.rebuildLocal(ctx, query, builder)
			return
		}
		if _, err := store.refreshSnapshot(ctx, query, builder); err != nil {
			// A stale local copy remains useful while Redis is fenced, disabled,
			// or unavailable. Build it locally as a best-effort fallback so a
			// transient Redis error does not leave the process stuck at 503.
			if isChannelMonitorPageSnapshotRedisUnavailable(err) {
				_, _ = store.rebuildLocal(ctx, query, builder)
			}
		}
	}()
	return true
}

func isChannelMonitorPageSnapshotRedisUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrChannelMonitorPageSnapshotUnavailable) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func (store *channelMonitorPageSnapshotStore) load(
	ctx context.Context,
	query ChannelMonitorPageSnapshotQuery,
	now time.Time,
) (ChannelMonitorPageSnapshot, ChannelMonitorPageSnapshotState, error) {
	client := store.readClient()
	if client == nil {
		return store.loadLocal(query, now, ErrChannelMonitorPageSnapshotUnavailable)
	}
	key, err := ChannelMonitorPageSnapshotKey(query)
	if err != nil {
		return ChannelMonitorPageSnapshot{}, ChannelMonitorPageSnapshotMissing, err
	}
	payload, err := client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return store.loadLocal(query, now, ErrChannelMonitorPageSnapshotMissing)
	}
	if err != nil {
		return store.loadLocal(query, now, err)
	}
	var snapshot ChannelMonitorPageSnapshot
	if err := common.Unmarshal(payload, &snapshot); err != nil {
		return store.loadLocal(query, now, err)
	}
	identityHash, err := channelMonitorPageSnapshotIdentityHash(query)
	if err != nil {
		return ChannelMonitorPageSnapshot{}, ChannelMonitorPageSnapshotMissing, err
	}
	if snapshot.SchemaVersion != channelMonitorPageSnapshotSchemaVersion ||
		snapshot.IdentityHash != identityHash ||
		snapshot.GeneratedAtUnixMilli <= 0 ||
		snapshot.StatusCode < 200 || snapshot.StatusCode >= 300 ||
		len(snapshot.Payload) == 0 || len(snapshot.Payload) > channelMonitorPageSnapshotMaxPayload {
		return store.loadLocal(query, now, ErrChannelMonitorPageSnapshotMissing)
	}
	stateSnapshot, state, stateErr := channelMonitorPageSnapshotState(snapshot, now)
	if stateErr != nil {
		// Do not let a malformed Redis envelope replace a valid bounded local
		// copy. If an older complete copy exists, it remains the only safe
		// fallback while the background rebuild repairs the shared snapshot.
		return store.loadLocal(query, now, stateErr)
	}
	store.rememberLocal(key, stateSnapshot)
	// Redis can recover with an older complete envelope than the process-local
	// copy retained during an outage. rememberLocal accepts only a snapshot
	// that is monotonic across revision, watermark, cutoff, and generation, so
	// its retained value is authoritative even when the two envelopes are
	// incomparable on different dimensions.
	store.localMu.RLock()
	localSnapshot, localExists := store.local[key]
	store.localMu.RUnlock()
	if localExists {
		localSnapshot.Payload = append([]byte(nil), localSnapshot.Payload...)
		localSnapshot, localState, localErr := channelMonitorPageSnapshotState(localSnapshot, now)
		if localErr != nil {
			return store.loadLocal(query, now, localErr)
		}
		return localSnapshot, localState, nil
	}
	return stateSnapshot, state, nil
}

func channelMonitorPageSnapshotState(
	snapshot ChannelMonitorPageSnapshot,
	now time.Time,
) (ChannelMonitorPageSnapshot, ChannelMonitorPageSnapshotState, error) {
	if snapshot.GeneratedAt <= 0 || snapshot.GeneratedAtUnixMilli <= 0 {
		return ChannelMonitorPageSnapshot{}, ChannelMonitorPageSnapshotMissing,
			ErrChannelMonitorPageSnapshotMissing
	}
	// A malformed or clock-skewed snapshot must not be treated as fresh and
	// extend its usable lifetime indefinitely. Keep the same bounded future
	// tolerance used by the status/model task snapshots.
	if time.Unix(snapshot.GeneratedAt, 0).After(now.Add(channelMonitorPageSnapshotMaxFutureSkew)) ||
		time.UnixMilli(snapshot.GeneratedAtUnixMilli).After(now.Add(channelMonitorPageSnapshotMaxFutureSkew)) ||
		snapshot.DataCutoffAt < 0 ||
		(snapshot.DataCutoffAt > 0 &&
			time.Unix(snapshot.DataCutoffAt, 0).After(now.Add(channelMonitorPageSnapshotMaxFutureSkew))) {
		return ChannelMonitorPageSnapshot{}, ChannelMonitorPageSnapshotMissing,
			ErrChannelMonitorPageSnapshotMissing
	}
	age := now.Sub(time.UnixMilli(snapshot.GeneratedAtUnixMilli))
	if age < channelMonitorPageSnapshotFreshTTL {
		return snapshot, ChannelMonitorPageSnapshotFresh, nil
	}
	if age >= channelMonitorPageSnapshotRetention {
		return ChannelMonitorPageSnapshot{}, ChannelMonitorPageSnapshotMissing,
			ErrChannelMonitorPageSnapshotMissing
	}
	return snapshot, ChannelMonitorPageSnapshotStale, nil
}

func (store *channelMonitorPageSnapshotStore) refreshSnapshot(
	ctx context.Context,
	query ChannelMonitorPageSnapshotQuery,
	builder ChannelMonitorPageSnapshotBuilder,
) (ChannelMonitorPageSnapshot, error) {
	return store.refreshSnapshotWithMode(ctx, query, builder, false)
}

func (store *channelMonitorPageSnapshotStore) refreshSnapshotForce(
	ctx context.Context,
	query ChannelMonitorPageSnapshotQuery,
	builder ChannelMonitorPageSnapshotBuilder,
) (ChannelMonitorPageSnapshot, error) {
	return store.refreshSnapshotWithMode(ctx, query, builder, true)
}

// rebuildLocal performs the same bounded, sanitized build as a Redis-backed
// refresh but retains the complete response only in this process. It is used
// while Redis is disabled or temporarily unavailable so an existing stale
// response can continue serving and can still be refreshed asynchronously.
func (store *channelMonitorPageSnapshotStore) rebuildLocal(
	ctx context.Context,
	query ChannelMonitorPageSnapshotQuery,
	builder ChannelMonitorPageSnapshotBuilder,
) (ChannelMonitorPageSnapshot, error) {
	if builder == nil {
		return ChannelMonitorPageSnapshot{}, ErrChannelMonitorPageSnapshotNotCacheable
	}
	key, err := ChannelMonitorPageSnapshotKey(query)
	if err != nil {
		return ChannelMonitorPageSnapshot{}, err
	}
	resultChannel := store.refresh.DoChan(key+":local", func() (any, error) {
		if !acquireChannelMonitorPageSnapshotBuild(ctx) {
			return ChannelMonitorPageSnapshot{}, ErrChannelMonitorPageSnapshotRefreshing
		}
		snapshot, buildErr := func() (ChannelMonitorPageSnapshot, error) {
			defer releaseChannelMonitorPageSnapshotBuild()
			return builder(ctx)
		}()
		if buildErr != nil {
			return snapshot, buildErr
		}
		if snapshot.StatusCode < 200 || snapshot.StatusCode >= 300 ||
			len(snapshot.Payload) == 0 || len(snapshot.Payload) > channelMonitorPageSnapshotMaxPayload {
			return snapshot, ErrChannelMonitorPageSnapshotNotCacheable
		}
		now := time.Now()
		if (snapshot.GeneratedAt > 0 &&
			time.Unix(snapshot.GeneratedAt, 0).After(now.Add(channelMonitorPageSnapshotMaxFutureSkew))) ||
			(snapshot.GeneratedAtUnixMilli > 0 &&
				time.UnixMilli(snapshot.GeneratedAtUnixMilli).After(now.Add(channelMonitorPageSnapshotMaxFutureSkew))) ||
			snapshot.DataCutoffAt < 0 ||
			(snapshot.DataCutoffAt > 0 &&
				time.Unix(snapshot.DataCutoffAt, 0).After(now.Add(channelMonitorPageSnapshotMaxFutureSkew))) {
			return snapshot, ErrChannelMonitorPageSnapshotNotCacheable
		}
		identityHash, identityErr := channelMonitorPageSnapshotIdentityHash(query)
		if identityErr != nil {
			return ChannelMonitorPageSnapshot{}, identityErr
		}
		snapshot.SchemaVersion = channelMonitorPageSnapshotSchemaVersion
		snapshot.IdentityHash = identityHash
		if snapshot.GeneratedAt <= 0 {
			snapshot.GeneratedAt = now.Unix()
		}
		snapshot.GeneratedAtUnixMilli = now.UnixMilli()
		if snapshot.ContentType == "" {
			snapshot.ContentType = "application/json; charset=utf-8"
		}
		store.rememberLocalAllowEqualGeneration(key, snapshot)
		return snapshot, nil
	})
	select {
	case result := <-resultChannel:
		snapshot, ok := result.Val.(ChannelMonitorPageSnapshot)
		if !ok {
			return ChannelMonitorPageSnapshot{}, result.Err
		}
		return snapshot, result.Err
	case <-ctx.Done():
		return ChannelMonitorPageSnapshot{}, ErrChannelMonitorPageSnapshotRefreshing
	}
}

func (store *channelMonitorPageSnapshotStore) refreshSnapshotWithMode(
	ctx context.Context,
	query ChannelMonitorPageSnapshotQuery,
	builder ChannelMonitorPageSnapshotBuilder,
	force bool,
) (ChannelMonitorPageSnapshot, error) {
	if builder == nil {
		return ChannelMonitorPageSnapshot{}, ErrChannelMonitorPageSnapshotNotCacheable
	}
	key, err := ChannelMonitorPageSnapshotKey(query)
	if err != nil {
		return ChannelMonitorPageSnapshot{}, err
	}
	singleflightKey := key
	if force {
		singleflightKey += ":force"
	}
	resultChannel := store.refresh.DoChan(singleflightKey, func() (any, error) {
		if !force {
			if snapshot, state, loadErr := store.load(ctx, query, time.Now()); loadErr == nil &&
				state == ChannelMonitorPageSnapshotFresh {
				return snapshot, nil
			}
		}
		token, acquired, acquireErr := store.acquireLease(ctx, key)
		if acquireErr != nil {
			return ChannelMonitorPageSnapshot{}, fmt.Errorf("%w: %v", ErrChannelMonitorPageSnapshotUnavailable, acquireErr)
		}
		if !acquired {
			return store.waitForSnapshot(ctx, query, time.Now(), builder)
		}
		return store.rebuildWithLeaseWithMode(ctx, query, key, token, builder, force)
	})
	select {
	case result := <-resultChannel:
		snapshot, ok := result.Val.(ChannelMonitorPageSnapshot)
		if !ok {
			return ChannelMonitorPageSnapshot{}, result.Err
		}
		return snapshot, result.Err
	case <-ctx.Done():
		return ChannelMonitorPageSnapshot{}, ErrChannelMonitorPageSnapshotRefreshing
	}
}

func (store *channelMonitorPageSnapshotStore) rebuildWithLease(
	ctx context.Context,
	query ChannelMonitorPageSnapshotQuery,
	key string,
	token int64,
	builder ChannelMonitorPageSnapshotBuilder,
) (ChannelMonitorPageSnapshot, error) {
	return store.rebuildWithLeaseWithMode(ctx, query, key, token, builder, false)
}

func (store *channelMonitorPageSnapshotStore) rebuildWithLeaseWithMode(
	ctx context.Context,
	query ChannelMonitorPageSnapshotQuery,
	key string,
	token int64,
	builder ChannelMonitorPageSnapshotBuilder,
	force bool,
) (ChannelMonitorPageSnapshot, error) {
	defer store.releaseLease(key, token)
	if !acquireChannelMonitorPageSnapshotBuild(ctx) {
		return ChannelMonitorPageSnapshot{}, ErrChannelMonitorPageSnapshotRefreshing
	}
	snapshot, err := func() (ChannelMonitorPageSnapshot, error) {
		defer releaseChannelMonitorPageSnapshotBuild()
		return builder(ctx)
	}()
	if err != nil {
		return snapshot, err
	}
	if snapshot.StatusCode < 200 || snapshot.StatusCode >= 300 ||
		len(snapshot.Payload) == 0 || len(snapshot.Payload) > channelMonitorPageSnapshotMaxPayload {
		return snapshot, ErrChannelMonitorPageSnapshotNotCacheable
	}
	now := time.Now()
	if (snapshot.GeneratedAt > 0 &&
		time.Unix(snapshot.GeneratedAt, 0).After(now.Add(channelMonitorPageSnapshotMaxFutureSkew))) ||
		(snapshot.GeneratedAtUnixMilli > 0 &&
			time.UnixMilli(snapshot.GeneratedAtUnixMilli).After(now.Add(channelMonitorPageSnapshotMaxFutureSkew))) ||
		snapshot.DataCutoffAt < 0 ||
		(snapshot.DataCutoffAt > 0 &&
			time.Unix(snapshot.DataCutoffAt, 0).After(now.Add(channelMonitorPageSnapshotMaxFutureSkew))) {
		return snapshot, ErrChannelMonitorPageSnapshotNotCacheable
	}
	identityHash, err := channelMonitorPageSnapshotIdentityHash(query)
	if err != nil {
		return ChannelMonitorPageSnapshot{}, err
	}
	snapshot.SchemaVersion = channelMonitorPageSnapshotSchemaVersion
	snapshot.IdentityHash = identityHash
	if snapshot.GeneratedAt <= 0 {
		snapshot.GeneratedAt = now.Unix()
	}
	snapshot.GeneratedAtUnixMilli = now.UnixMilli()
	if snapshot.ContentType == "" {
		snapshot.ContentType = "application/json; charset=utf-8"
	}
	published, err := store.publishWithMode(ctx, key, token, snapshot, force)
	if err != nil {
		return snapshot, err
	}
	if published {
		return snapshot, nil
	}
	current, _, loadErr := store.load(ctx, query, time.Now())
	if loadErr == nil {
		return current, nil
	}
	return ChannelMonitorPageSnapshot{}, ErrChannelMonitorPageSnapshotRefreshing
}

func (store *channelMonitorPageSnapshotStore) publish(
	ctx context.Context,
	key string,
	token int64,
	snapshot ChannelMonitorPageSnapshot,
) (bool, error) {
	return store.publishWithMode(ctx, key, token, snapshot, false)
}

func (store *channelMonitorPageSnapshotStore) publishWithMode(
	ctx context.Context,
	key string,
	token int64,
	snapshot ChannelMonitorPageSnapshot,
	force bool,
) (bool, error) {
	client := store.writeClient()
	if client == nil {
		return false, ErrChannelMonitorPageSnapshotUnavailable
	}
	payload, err := common.Marshal(snapshot)
	if err != nil {
		return false, err
	}
	forceArg := "0"
	if force {
		forceArg = "1"
	}
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
local old_revision = redis.call("HGET", KEYS[2], "revision") or "0"
local old_watermark = redis.call("HGET", KEYS[2], "event_watermark") or "0"
local old_generated = redis.call("HGET", KEYS[2], "generated_at_unix_milli") or "0"
local old_data_cutoff = redis.call("HGET", KEYS[2], "data_cutoff_at") or "0"
local same_revision = not less(ARGV[3], old_revision) and not less(old_revision, ARGV[3])
local same_watermark = not less(ARGV[4], old_watermark) and not less(old_watermark, ARGV[4])
local same_data_cutoff = not less(ARGV[6], old_data_cutoff) and not less(old_data_cutoff, ARGV[6])
local force_publish = ARGV[8] == "1"
if less(ARGV[3], old_revision) or less(ARGV[4], old_watermark) or
   less(ARGV[5], old_generated) or less(ARGV[6], old_data_cutoff) or
   (same_revision and same_watermark and same_data_cutoff and not less(old_generated, ARGV[5]) and not force_publish) then
  return 0
end
redis.call("SET", KEYS[1], ARGV[2], "PX", ARGV[7])
redis.call("HSET", KEYS[2],
  "revision", ARGV[3],
  "event_watermark", ARGV[4],
  "generated_at_unix_milli", ARGV[5],
  "data_cutoff_at", ARGV[6])
redis.call("PEXPIRE", KEYS[2], ARGV[7])
return 1
`).Run(
		ctx,
		client,
		[]string{key, key + ":meta", key + ":lease"},
		strconv.FormatInt(token, 10),
		payload,
		strconv.FormatUint(snapshot.Revision, 10),
		strconv.FormatUint(snapshot.EventWatermark, 10),
		strconv.FormatInt(snapshot.GeneratedAtUnixMilli, 10),
		strconv.FormatInt(snapshot.DataCutoffAt, 10),
		strconv.FormatInt(channelMonitorPageSnapshotRetention.Milliseconds(), 10),
		forceArg,
	).Int64()
	if err != nil {
		return false, err
	}
	if result == 1 {
		store.rememberLocalWithMode(key, snapshot, force)
		return true, nil
	}
	return false, nil
}

func (store *channelMonitorPageSnapshotStore) acquireLease(
	ctx context.Context,
	key string,
) (int64, bool, error) {
	client := store.writeClient()
	if client == nil {
		return 0, false, ErrChannelMonitorPageSnapshotUnavailable
	}
	result, err := redis.NewScript(`
if redis.call("EXISTS", KEYS[1]) == 1 then
  return 0
end
local token = redis.call("INCR", KEYS[2])
redis.call("PSETEX", KEYS[1], ARGV[1], tostring(token))
redis.call("PEXPIRE", KEYS[2], ARGV[2])
return token
`).Run(
		ctx,
		client,
		[]string{key + ":lease", key + ":fence"},
		strconv.FormatInt(channelMonitorPageSnapshotLeaseTTL.Milliseconds(), 10),
		strconv.FormatInt((channelMonitorPageSnapshotRetention+channelMonitorPageSnapshotLeaseTTL).Milliseconds(), 10),
	).Int64()
	return result, result > 0, err
}

func (store *channelMonitorPageSnapshotStore) releaseLease(key string, token int64) {
	if token <= 0 {
		return
	}
	client := store.writeClient()
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`).Run(ctx, client, []string{key + ":lease"}, strconv.FormatInt(token, 10)).Err()
}

func (store *channelMonitorPageSnapshotStore) rememberLocal(
	key string,
	snapshot ChannelMonitorPageSnapshot,
) {
	store.rememberLocalWithMode(key, snapshot, false)
}

func (store *channelMonitorPageSnapshotStore) rememberLocalAllowEqualGeneration(
	key string,
	snapshot ChannelMonitorPageSnapshot,
) {
	store.rememberLocalWithOptions(key, snapshot, false, true)
}

func (store *channelMonitorPageSnapshotStore) rememberLocalWithMode(
	key string,
	snapshot ChannelMonitorPageSnapshot,
	force bool,
) {
	store.rememberLocalWithOptions(key, snapshot, force, false)
}

func (store *channelMonitorPageSnapshotStore) rememberLocalWithOptions(
	key string,
	snapshot ChannelMonitorPageSnapshot,
	force bool,
	allowEqualGeneration bool,
) {
	if int64(len(snapshot.Payload)) > channelMonitorPageSnapshotMaxLocalBytes {
		return
	}
	snapshot.Payload = append([]byte(nil), snapshot.Payload...)
	payloadBytes := int64(len(snapshot.Payload))
	store.localMu.Lock()
	defer store.localMu.Unlock()
	if store.local == nil {
		store.local = make(map[string]ChannelMonitorPageSnapshot)
	}
	cutoff := time.Now().Add(-channelMonitorPageSnapshotRetention).UnixMilli()
	for localKey, localSnapshot := range store.local {
		if localSnapshot.GeneratedAtUnixMilli <= cutoff {
			delete(store.local, localKey)
			store.localBytes -= int64(len(localSnapshot.Payload))
		}
	}
	current, exists := store.local[key]
	if exists && !((force && channelMonitorPageSnapshotNotOlder(snapshot, current)) ||
		(allowEqualGeneration && channelMonitorPageSnapshotNotOlder(snapshot, current)) ||
		channelMonitorPageSnapshotNewer(snapshot, current)) {
		if store.localBytes < 0 {
			store.localBytes = 0
		}
		return
	}
	if exists {
		delete(store.local, key)
		store.localBytes -= int64(len(current.Payload))
	}
	for len(store.local) >= channelMonitorPageSnapshotMaxLocalItems ||
		store.localBytes+payloadBytes > channelMonitorPageSnapshotMaxLocalBytes {
		var oldestKey string
		var oldestGeneratedAt int64
		for localKey, localSnapshot := range store.local {
			if oldestKey == "" || localSnapshot.GeneratedAtUnixMilli < oldestGeneratedAt {
				oldestKey = localKey
				oldestGeneratedAt = localSnapshot.GeneratedAtUnixMilli
			}
		}
		if oldestKey == "" {
			break
		}
		oldestSnapshot := store.local[oldestKey]
		delete(store.local, oldestKey)
		store.localBytes -= int64(len(oldestSnapshot.Payload))
	}
	if store.localBytes < 0 {
		store.localBytes = 0
	}
	store.local[key] = snapshot
	store.localBytes += payloadBytes
}

func (store *channelMonitorPageSnapshotStore) loadLocal(
	query ChannelMonitorPageSnapshotQuery,
	now time.Time,
	fallbackErr error,
) (ChannelMonitorPageSnapshot, ChannelMonitorPageSnapshotState, error) {
	key, err := ChannelMonitorPageSnapshotKey(query)
	if err != nil {
		return ChannelMonitorPageSnapshot{}, ChannelMonitorPageSnapshotMissing, err
	}
	store.localMu.RLock()
	snapshot, ok := store.local[key]
	store.localMu.RUnlock()
	if !ok {
		return ChannelMonitorPageSnapshot{}, ChannelMonitorPageSnapshotMissing, fallbackErr
	}
	snapshot.Payload = append([]byte(nil), snapshot.Payload...)
	snapshot, state, err := channelMonitorPageSnapshotState(snapshot, now)
	if err == nil && state == ChannelMonitorPageSnapshotFresh {
		state = ChannelMonitorPageSnapshotStale
	}
	return snapshot, state, err
}

func channelMonitorPageSnapshotNewer(
	candidate ChannelMonitorPageSnapshot,
	current ChannelMonitorPageSnapshot,
) bool {
	if !channelMonitorPageSnapshotNotOlder(candidate, current) {
		return false
	}
	if candidate.Revision == current.Revision &&
		candidate.EventWatermark == current.EventWatermark &&
		candidate.DataCutoffAt == current.DataCutoffAt {
		return candidate.GeneratedAtUnixMilli > current.GeneratedAtUnixMilli
	}
	return true
}

func channelMonitorPageSnapshotNotOlder(
	candidate ChannelMonitorPageSnapshot,
	current ChannelMonitorPageSnapshot,
) bool {
	return !(candidate.Revision < current.Revision ||
		candidate.EventWatermark < current.EventWatermark ||
		candidate.DataCutoffAt < current.DataCutoffAt ||
		candidate.GeneratedAtUnixMilli < current.GeneratedAtUnixMilli)
}

func (store *channelMonitorPageSnapshotStore) waitForSnapshot(
	ctx context.Context,
	query ChannelMonitorPageSnapshotQuery,
	waitStartedAt time.Time,
	builder ChannelMonitorPageSnapshotBuilder,
) (ChannelMonitorPageSnapshot, error) {
	key, keyErr := ChannelMonitorPageSnapshotKey(query)
	if keyErr != nil {
		return ChannelMonitorPageSnapshot{}, keyErr
	}
	ticker := time.NewTicker(channelMonitorPageSnapshotPollInterval)
	defer ticker.Stop()
	for {
		snapshot, state, err := store.load(ctx, query, time.Now())
		if err == nil && state != ChannelMonitorPageSnapshotMissing &&
			snapshot.GeneratedAtUnixMilli >= waitStartedAt.UnixMilli() {
			return snapshot, nil
		}
		if err != nil && !errors.Is(err, ErrChannelMonitorPageSnapshotMissing) {
			return ChannelMonitorPageSnapshot{}, err
		}
		client := store.readClient()
		if client == nil {
			return ChannelMonitorPageSnapshot{}, ErrChannelMonitorPageSnapshotUnavailable
		}
		leaseExists, leaseErr := client.Exists(ctx, key+":lease").Result()
		if leaseErr != nil {
			return ChannelMonitorPageSnapshot{}, leaseErr
		}
		if leaseExists == 0 {
			token, acquired, acquireErr := store.acquireLease(ctx, key)
			if acquireErr != nil {
				return ChannelMonitorPageSnapshot{}, acquireErr
			}
			if acquired {
				if builder == nil {
					store.releaseLease(key, token)
					return ChannelMonitorPageSnapshot{}, ErrChannelMonitorPageSnapshotMissing
				}
				return store.rebuildWithLease(ctx, query, key, token, builder)
			}
		}
		select {
		case <-ctx.Done():
			return ChannelMonitorPageSnapshot{}, ErrChannelMonitorPageSnapshotRefreshing
		case <-ticker.C:
		}
	}
}

func channelMonitorPageSnapshotIdentityHash(
	query ChannelMonitorPageSnapshotQuery,
) (string, error) {
	type filterEntry struct {
		Key    string   `json:"key"`
		Values []string `json:"values"`
	}
	type identity struct {
		Page            string        `json:"page"`
		Version         string        `json:"version"`
		PermissionScope string        `json:"permission_scope"`
		WindowStart     int64         `json:"window_start"`
		WindowEnd       int64         `json:"window_end"`
		Filters         []filterEntry `json:"filters"`
	}
	normalized := identity{
		Page:            strings.TrimSpace(strings.ToLower(query.Page)),
		Version:         strings.TrimSpace(query.Version),
		PermissionScope: strings.TrimSpace(query.PermissionScope),
		WindowStart:     query.WindowStart,
		WindowEnd:       query.WindowEnd,
		Filters:         make([]filterEntry, 0, len(query.Filters)),
	}
	normalizedFilters := make(map[string][]string, len(query.Filters))
	for key, values := range query.Filters {
		normalizedKey := strings.TrimSpace(strings.ToLower(key))
		normalizedFilters[normalizedKey] = append(normalizedFilters[normalizedKey], values...)
	}
	filterKeys := make([]string, 0, len(normalizedFilters))
	for key := range normalizedFilters {
		filterKeys = append(filterKeys, key)
	}
	sort.Strings(filterKeys)
	for _, key := range filterKeys {
		if key == "" {
			continue
		}
		values := append([]string(nil), normalizedFilters[key]...)
		for index := range values {
			values[index] = strings.TrimSpace(values[index])
		}
		sort.Strings(values)
		normalized.Filters = append(normalized.Filters, filterEntry{Key: key, Values: values})
	}
	payload, err := common.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(payload)), nil
}
