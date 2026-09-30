package service

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

const ChannelGroupMonitorRedisPrefix = ChannelMonitorRedisKeyPrefix + ":group_view:v1:"
const channelGroupMonitorGenerationIDKey = ChannelGroupMonitorRedisPrefix + "generation_id"
const channelGroupMonitorConfigurationKey = ChannelGroupMonitorRedisPrefix + "config"
const ChannelGroupMonitorSnapshotKey = ChannelGroupMonitorRedisPrefix + "snapshot"

var ErrChannelGroupMonitorSnapshotPending = errors.New("分组监控汇总暂不可用，请稍后重试")

// The generation fences stream replay, configuration races and Redis recovery.
// Only background workers/configuration saves install it; page reads never rebuild it.
type ChannelGroupMonitorGeneration struct {
	Schema             int             `json:"schema"`
	ID                 string          `json:"id"`
	Revision           int64           `json:"revision"`
	StartedAt          int64           `json:"started_at"`
	DisplayValue       int             `json:"display_value"`
	DisplayUnit        string          `json:"display_unit"`
	Groups             map[string]bool `json:"groups"`
	RoutingFingerprint string          `json:"routing_fingerprint,omitempty"`
}

func (generation ChannelGroupMonitorGeneration) Key(suffix string) string {
	return ChannelGroupMonitorRedisPrefix + generation.ID + ":" + suffix
}

// A revision change atomically hides the old snapshot and unlinks its bounded
// registry of bucket keys. UNLINK avoids freeing a large token hash on Redis's IO thread.
var installChannelGroupMonitorGeneration = redis.NewScript(`
local previous = redis.call('GET', KEYS[1])
local retired_id = redis.call('GET', KEYS[3])
if previous then
 local old = cjson.decode(previous)
 retired_id = old.id
 if old.schema == 2 then redis.call('SET', KEYS[3], old.id) end
 if old.revision > tonumber(ARGV[1]) then return previous end
 if old.revision < tonumber(ARGV[1]) then redis.call('DEL', ARGV[3] .. 'snapshot_lease') end
 if old.revision == tonumber(ARGV[1]) and old.schema == 2 then
  if ARGV[4] == '' or old.routing_fingerprint == ARGV[4] then return previous end
  if not old.routing_fingerprint then
   -- Preserve empty JSON objects; Lua cjson cannot round-trip {} consistently.
   local updated = string.sub(previous, 1, -2) .. ',"routing_fingerprint":' .. cjson.encode(ARGV[4]) .. '}'
   redis.call('SET', KEYS[1], updated)
   return updated
  end
 end
end
if retired_id then
 local registry = ARGV[3] .. retired_id .. ':keys'
 local keys = redis.call('ZRANGE', registry, 0, -1)
 for _, key in ipairs(keys) do redis.call('UNLINK', key) end
 redis.call('UNLINK', registry)
end
redis.call('SET', KEYS[1], ARGV[2])
redis.call('SET', KEYS[3], cjson.decode(ARGV[2]).id)
redis.call('DEL', KEYS[2])
return ARGV[2]
`)

func SyncChannelGroupMonitorGeneration(ctx context.Context, config model.ChannelGroupMonitorConfig, fingerprint ...string) (ChannelGroupMonitorGeneration, error) {
	client := common.RedisMonitorWriteClient()
	if !common.RedisEnabled || client == nil {
		return ChannelGroupMonitorGeneration{}, ErrChannelGroupMonitorSnapshotPending
	}
	groups, err := config.Groups()
	if err != nil {
		return ChannelGroupMonitorGeneration{}, err
	}
	value, unit := model.NormalizeChannelStatusProbeDisplay(config.DisplayValue, config.DisplayUnit)
	generation := ChannelGroupMonitorGeneration{Schema: 2, ID: uuid.NewString(), Revision: config.Revision, StartedAt: time.Now().Unix(), DisplayValue: value, DisplayUnit: unit, Groups: make(map[string]bool)}
	if len(fingerprint) > 0 {
		generation.RoutingFingerprint = fingerprint[0]
	}
	for _, group := range groups {
		generation.Groups[group.GroupName] = config.Enabled && group.IsEnabled()
	}
	payload, err := common.Marshal(generation)
	if err != nil {
		return generation, err
	}
	raw, err := installChannelGroupMonitorGeneration.Run(ctx, client, []string{channelGroupMonitorConfigurationKey, ChannelGroupMonitorSnapshotKey, channelGroupMonitorGenerationIDKey}, config.Revision, string(payload), ChannelGroupMonitorRedisPrefix, generation.RoutingFingerprint).Text()
	if err != nil {
		return generation, err
	}
	err = common.UnmarshalJsonStr(raw, &generation)
	if err == nil && generation.Revision != config.Revision {
		err = ErrChannelGroupMonitorSnapshotPending
	}
	return generation, err
}

func readChannelGroupMonitorGeneration(ctx context.Context, client *redis.Client) (ChannelGroupMonitorGeneration, error) {
	var generation ChannelGroupMonitorGeneration
	if client == nil {
		return generation, ErrChannelGroupMonitorSnapshotPending
	}
	raw, err := client.Get(ctx, channelGroupMonitorConfigurationKey).Bytes()
	if err != nil {
		return generation, err
	}
	err = common.Unmarshal(raw, &generation)
	return generation, err
}

// Both aggregates and deduplication commit in one script. The current generation
// is checked inside that same operation, after any concurrent configuration save.
var incrementChannelGroupMonitorBucket = redis.NewScript(channelGroupMonitorCacheDeltaSource + `
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
if redis.call('SISMEMBER', KEYS[3], ARGV[2]) == 1 then return 0 end
local floor = math.max(tonumber(redis.call('GET', KEYS[12]) or '0'), tonumber(ARGV[7]))
if tonumber(ARGV[6]) < floor then return 0 end
-- Validate before writing; Redis Lua does not roll back partial mutations.
for i = 13, #ARGV, 2 do
 local old = tonumber(redis.call('HGET', KEYS[2], ARGV[i]) or '0')
 local delta = tonumber(ARGV[i+1])
 if not old or not delta or delta < 0 or old + delta > 9007199254740991 then
  return redis.error_reply('group monitor aggregate overflow')
 end
end
local input, read = tonumber(ARGV[9]), tonumber(ARGV[10])
if input > 0 then
 local problem = validate_cache_delta(KEYS[8], KEYS[10], ARGV[8], input, read)
 if problem then return redis.error_reply(problem) end
 for _, suffix in ipairs({'input', 'read'}) do
  local old = tonumber(redis.call('HGET', KEYS[7], ARGV[8] .. ':' .. suffix) or '0')
  local delta = suffix == 'input' and input or read
  if not old or old + delta > 9007199254740991 then return redis.error_reply('group monitor bucket overflow') end
 end
end
for i = 13, #ARGV, 2 do redis.call('HINCRBYFLOAT', KEYS[2], ARGV[i], ARGV[i+1]) end
if ARGV[4] ~= '' then
 local previous = redis.call('HGET', KEYS[2], ARGV[4])
 local next = cjson.decode(ARGV[5])
 if not previous or (cjson.decode(previous).finished_at < next.finished_at) or
  (cjson.decode(previous).finished_at == next.finished_at and cjson.decode(previous).id < next.id) then
  redis.call('HSET', KEYS[2], ARGV[4], ARGV[5])
 end
 if next.result ~= 'skipped' then
  local raw = redis.call('HGET', KEYS[5], ARGV[4])
  local state = raw and cjson.decode(raw) or {finished_at=0,execution_id=0,last_success_at=0,last_failure_at=0,consecutive_success=0,consecutive_failure=0}
  if next.finished_at > state.finished_at or (next.finished_at == state.finished_at and next.id > state.execution_id) then
   state.finished_at = next.finished_at
   state.execution_id = next.id
   state.result = next.result
   state.first_token_ms = cjson.null
   if next.result == 'success' then
    state.first_token_ms = next.first_token_ms
    state.last_success_at = next.finished_at
    state.consecutive_success = state.consecutive_success + 1
    state.consecutive_failure = 0
   else
    state.last_failure_at = next.finished_at
    state.consecutive_failure = state.consecutive_failure + 1
    state.consecutive_success = 0
   end
   redis.call('HSET', KEYS[5], ARGV[4], cjson.encode(state))
  end
 end
end

if input > 0 then
 redis.call('HINCRBY', KEYS[7], ARGV[8] .. ':input', ARGV[9])
 redis.call('HINCRBY', KEYS[7], ARGV[8] .. ':read', ARGV[10])
 apply_cache_delta(KEYS[8], KEYS[9], KEYS[10], ARGV[8], input, read)
 redis.call('ZADD', KEYS[11], ARGV[6], KEYS[7])
 for i = 7, 11 do redis.call('ZADD', KEYS[4], '+inf', KEYS[i]) end
end
redis.call('SET', KEYS[12], floor)
redis.call('SADD', KEYS[3], ARGV[2])
redis.call('EXPIRE', KEYS[3], ARGV[3])
redis.call('EXPIRE', KEYS[2], ARGV[11])
local now = tonumber(redis.call('TIME')[1])
redis.call('ZADD', KEYS[4], now + tonumber(ARGV[11]), KEYS[2], now + tonumber(ARGV[3]), KEYS[3])
for _, i in ipairs({5, 6, 12, 13}) do redis.call('ZADD', KEYS[4], '+inf', KEYS[i]) end
redis.call('ZREMRANGEBYSCORE', KEYS[4], '-inf', now)
redis.call('INCR', KEYS[6])
redis.call('HINCRBY', KEYS[13], ARGV[12], 1)
return 1
`)

func ProjectChannelGroupMonitorEvents(ctx context.Context, client *redis.Client, events []model.ChannelMonitorEvent, now int64) error {
	generation, err := readChannelGroupMonitorGeneration(ctx, client)
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return err
	}
	seconds := model.ChannelStatusProbeDisplayBucketSeconds(generation.DisplayUnit)
	minimum := model.ChannelStatusProbeDisplayBucketStart(now, generation.DisplayUnit) - int64(generation.DisplayValue-1)*seconds
	type projectionDelta struct {
		keys []string
		args []any
	}
	var batch []projectionDelta
	for _, event := range events {
		createdAt := event.CreatedAt
		if createdAt <= 0 {
			createdAt = event.OccurredAt
		}
		if event.GroupMonitorGeneration != generation.ID || !generation.Groups[event.GroupName] || event.OccurredAt < generation.StartedAt || event.OccurredAt > now || createdAt > now+60 || createdAt <= now-int64(channelMonitorRedisReplayProtectionTTL/time.Second) {
			continue
		}
		var cacheInput, cacheRead int64
		at := event.OccurredAt
		prefix := base64.RawURLEncoding.EncodeToString([]byte(event.GroupName)) + ":"
		groupPrefix := generation.Key("group:" + prefix)
		latestField, latest := "", ""
		deltas := make([]any, 0, 24)
		if event.Source == model.ChannelMonitorEventSourceGroupSummary {
			probe := event.GroupMonitorProbe
			if probe == nil || probe.ConfigRevision != generation.Revision || probe.StartedAt < generation.StartedAt {
				continue
			}
			if probe.Result == model.ChannelGroupMonitorResultTimeout {
				at = probe.StartedAt
			}
			payload, marshalErr := common.Marshal(probe)
			if marshalErr != nil {
				return marshalErr
			}
			latestField, latest = prefix+"latest", string(payload)
			deltas = append(deltas, prefix+probe.Result, 1)
			if probe.Result == model.ChannelGroupMonitorResultSuccess {
				if probe.FirstTokenMs != nil {
					deltas = append(deltas, prefix+"first_total", *probe.FirstTokenMs, prefix+"first_count", 1)
				}
				if probe.TPS != nil {
					deltas = append(deltas, prefix+"tps_total", *probe.TPS, prefix+"tps_count", 1)
				}
			}
			if probe.ResponseTimeMs != nil {
				deltas = append(deltas, prefix+"response_total", *probe.ResponseTimeMs, prefix+"response_count", 1)
			}
		} else {
			if event.Source != model.ChannelMonitorEventSourceBusiness ||
				(event.Outcome != model.ChannelMonitorEventOutcomeSuccess && event.Outcome != model.ChannelMonitorEventOutcomeFailure) ||
				!event.IsStream || !event.RequestDispatched || event.FinalRetrySummary || event.GroupCacheExcluded == nil || *event.GroupCacheExcluded {
				continue
			}
			input := event.InputTokens
			if input == nil {
				input = event.PromptTokens
			}
			if input == nil || *input <= 0 {
				continue
			}
			read := int64(0)
			if event.CacheReadTokens != nil {
				read = *event.CacheReadTokens
			}
			cacheInput, cacheRead = *input, read
		}
		if at < minimum {
			continue
		}
		bucket := model.ChannelStatusProbeDisplayBucketStart(at, generation.DisplayUnit)
		key := groupPrefix + "probe:" + strconv.FormatInt(bucket, 10)
		dedupHour := createdAt / 3600 * 3600
		dedupTTL := dedupHour + 3600 + int64(channelMonitorRedisReplayProtectionTTL/time.Second) - now
		args := []any{generation.ID, event.EventId, dedupTTL, latestField, latest, bucket, minimum,
			max(0, event.APIKeyId), cacheInput, cacheRead, bucket + int64(generation.DisplayValue+1)*seconds - now, event.GroupName}
		args = append(args, deltas...)
		batch = append(batch, projectionDelta{keys: []string{channelGroupMonitorGenerationIDKey, key,
			generation.Key("seen:" + strconv.FormatInt(dedupHour, 10)), generation.Key("keys"), generation.Key("state"), generation.Key("version"),
			groupPrefix + "cache:" + strconv.FormatInt(bucket, 10), groupPrefix + "totals", groupPrefix + "rates", groupPrefix + "summary", groupPrefix + "buckets", groupPrefix + "floor", generation.Key("versions")}, args: args})
	}
	if len(batch) == 0 {
		return nil
	}
	// Send the script hash per event, not the full source. Retrying a pipeline
	// after NOSCRIPT is safe because each delta and its marker commit atomically.
	for attempt := range 2 {
		pipe := client.Pipeline()
		for _, delta := range batch {
			incrementChannelGroupMonitorBucket.EvalSha(ctx, pipe, delta.keys, delta.args...)
		}
		_, err = pipe.Exec(ctx)
		if err == nil || !strings.HasPrefix(err.Error(), "NOSCRIPT") || attempt == 1 {
			return err
		}
		if err = incrementChannelGroupMonitorBucket.Load(ctx, client).Err(); err != nil {
			return err
		}
	}
	return err
}

type ChannelGroupMonitorRedisBucket struct {
	StartedAt int64
	Counts    map[string]float64
	Latest    *model.ChannelGroupMonitorExecution
}

type ChannelGroupMonitorRedisGroup struct {
	Buckets []ChannelGroupMonitorRedisBucket
	Cache   ChannelGroupMonitorCacheStatistics
	State   *model.ChannelGroupMonitorState
}

// Read only fixed-size probe metrics and precomputed cache summaries. The optional
// selection lets snapshot builders reuse groups whose input version is unchanged.
func ReadChannelGroupMonitorProjection(ctx context.Context, generation ChannelGroupMonitorGeneration, now int64, selection ...map[string]bool) (map[string]ChannelGroupMonitorRedisGroup, error) {
	client := common.RedisMonitorWriteClient()
	if client == nil {
		return nil, ErrChannelGroupMonitorSnapshotPending
	}
	seconds := model.ChannelStatusProbeDisplayBucketSeconds(generation.DisplayUnit)
	start := model.ChannelStatusProbeDisplayBucketStart(now, generation.DisplayUnit) - int64(generation.DisplayValue-1)*seconds
	result := make(map[string]ChannelGroupMonitorRedisGroup)
	prefixes := make([]string, 0, len(generation.Groups))
	type groupRead struct {
		name, prefix string
		buckets      []*redis.StringStringMapCmd
		state        *redis.StringCmd
		cache        *redis.Cmd
	}
	reads := make([]groupRead, 0, len(generation.Groups))
	for name := range generation.Groups {
		if len(selection) > 0 && !selection[0][name] {
			continue
		}
		prefix := base64.RawURLEncoding.EncodeToString([]byte(name)) + ":"
		prefixes = append(prefixes, generation.Key("group:"+prefix))
		reads = append(reads, groupRead{name: name, prefix: prefix})
	}
	if err := expireChannelGroupMonitorCaches(ctx, client, generation, prefixes, start); err != nil {
		return nil, err
	}
	if len(reads) == 0 {
		return result, nil
	}
	if err := readChannelGroupMonitorCache.Load(ctx, client).Err(); err != nil {
		return nil, err
	}
	pipe := client.Pipeline()
	for index := range reads {
		entry := &reads[index]
		entry.buckets = make([]*redis.StringStringMapCmd, generation.DisplayValue)
		for i := range entry.buckets {
			entry.buckets[i] = pipe.HGetAll(ctx, prefixes[index]+"probe:"+strconv.FormatInt(start+int64(i)*seconds, 10))
		}
		entry.state = pipe.HGet(ctx, generation.Key("state"), entry.prefix+"latest")
		entry.cache = readChannelGroupMonitorCache.EvalSha(ctx, pipe, []string{prefixes[index] + "summary", prefixes[index] + "rates"})
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	for _, entry := range reads {
		name, prefix, commands, state, cache := entry.name, entry.prefix, entry.buckets, entry.state, entry.cache
		group := ChannelGroupMonitorRedisGroup{Buckets: make([]ChannelGroupMonitorRedisBucket, len(commands))}
		if state.Err() == nil {
			group.State = &model.ChannelGroupMonitorState{}
			if err := common.UnmarshalJsonStr(state.Val(), group.State); err != nil {
				return nil, err
			}
		} else if !errors.Is(state.Err(), redis.Nil) {
			return nil, state.Err()
		}
		for i, command := range commands {
			if command.Err() != nil {
				return nil, command.Err()
			}
			bucket := &group.Buckets[i]
			bucket.StartedAt, bucket.Counts = start+int64(i)*seconds, make(map[string]float64)
			for field, raw := range command.Val() {
				metric := strings.TrimPrefix(field, prefix)
				if metric == "latest" {
					bucket.Latest = &model.ChannelGroupMonitorExecution{}
					if err := common.UnmarshalJsonStr(raw, bucket.Latest); err != nil {
						return nil, err
					}
				} else {
					value, err := strconv.ParseFloat(raw, 64)
					if err != nil {
						return nil, err
					}
					bucket.Counts[metric] = value
				}
			}
		}
		values, err := cache.StringSlice()
		if err != nil {
			return nil, err
		}
		for i, target := range []**float64{&group.Cache.Weighted, &group.Cache.APIKeyAverage, &group.Cache.APIKeyMax} {
			if values[i] == "" {
				continue
			}
			value, err := strconv.ParseFloat(values[i], 64)
			if err != nil {
				return nil, err
			}
			*target = &value
		}
		result[name] = group
	}
	return result, nil
}

func ChannelGroupMonitorGroupVersions(ctx context.Context, generation ChannelGroupMonitorGeneration) (map[string]string, error) {
	return common.RedisMonitorWriteClient().HGetAll(ctx, generation.Key("versions")).Result()
}

// Snapshot writes are fenced against configuration changes and expire if the
// background worker stops. A page must never render an indefinitely healthy result.
var publishChannelGroupMonitorSnapshot = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if not current or cjson.decode(current).id ~= ARGV[1] then return 0 end
if #KEYS > 2 and redis.call('GET', KEYS[3]) ~= ARGV[3] then return 0 end
local value = cjson.decode(ARGV[2])
local ttl = 30
if value.next_refresh_at then
 ttl = math.max(1, math.min(ttl, value.next_refresh_at - tonumber(redis.call('TIME')[1]) + 5))
end
redis.call('SET', KEYS[2], ARGV[2], 'EX', ttl)
if #KEYS > 2 then redis.call('EXPIRE', KEYS[3], 4) end
return 1
`)

func PublishChannelGroupMonitorSnapshot(ctx context.Context, generation ChannelGroupMonitorGeneration, snapshot any, lease ...string) error {
	payload, err := common.Marshal(snapshot)
	if err != nil {
		return err
	}
	client := common.RedisMonitorWriteClient()
	if client == nil {
		return ErrChannelGroupMonitorSnapshotPending
	}
	keys, args := []string{channelGroupMonitorConfigurationKey, ChannelGroupMonitorSnapshotKey}, []any{generation.ID, string(payload)}
	if len(lease) == 2 {
		keys = append(keys, lease[0])
		args = append(args, lease[1])
	}
	ok, err := publishChannelGroupMonitorSnapshot.Run(ctx, client, keys, args...).Int()
	if err != nil {
		return err
	}
	if ok != 1 {
		return ErrChannelGroupMonitorSnapshotPending
	}
	return nil
}

func ReadChannelGroupMonitorSnapshot(ctx context.Context, target any) error {
	client := common.RedisMonitorReadClient()
	if !common.RedisEnabled || client == nil {
		return ErrChannelGroupMonitorSnapshotPending
	}
	payload, err := client.Get(ctx, ChannelGroupMonitorSnapshotKey).Bytes()
	if errors.Is(err, redis.Nil) {
		return ErrChannelGroupMonitorSnapshotPending
	}
	if err != nil {
		return err
	}
	return common.Unmarshal(payload, target)
}

// Capture before reading buckets, so an event arriving during a build remains
// newer than that snapshot and triggers another build on the next check.
func ChannelGroupMonitorProjectionVersion(ctx context.Context, generation ChannelGroupMonitorGeneration) (string, error) {
	client := common.RedisMonitorWriteClient()
	if client == nil {
		return "", ErrChannelGroupMonitorSnapshotPending
	}
	version, err := client.Get(ctx, generation.Key("version")).Result()
	if errors.Is(err, redis.Nil) {
		return "0", nil
	}
	return version, err
}

var renewChannelGroupMonitorSnapshot = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if not current or cjson.decode(current).id ~= ARGV[1] then return 0 end
if redis.call('GET', KEYS[3]) ~= ARGV[2] then return 0 end
if (redis.call('GET', KEYS[4]) or '0') ~= ARGV[3] then return 0 end
local snapshot = redis.call('GET', KEYS[2])
if not snapshot then return 0 end
local value = cjson.decode(snapshot)
if value.next_refresh_at ~= tonumber(ARGV[4]) or value.event_version ~= ARGV[3] then return 0 end
local now = tonumber(redis.call('TIME')[1])
if now >= tonumber(ARGV[4]) then return 0 end
redis.call('EXPIRE', KEYS[2], math.min(30, tonumber(ARGV[4]) - now + 5))
redis.call('EXPIRE', KEYS[3], 4)
return 1
`)

// Unchanged data needs only a heartbeat, not another bucket scan. The lease,
// generation and event version checks keep this heartbeat from hiding new work.
func RenewChannelGroupMonitorSnapshot(ctx context.Context, generation ChannelGroupMonitorGeneration, version string, nextRefreshAt int64, lease, owner string) (bool, error) {
	client := common.RedisMonitorWriteClient()
	if client == nil {
		return false, ErrChannelGroupMonitorSnapshotPending
	}
	result, err := renewChannelGroupMonitorSnapshot.Run(ctx, client,
		[]string{channelGroupMonitorConfigurationKey, ChannelGroupMonitorSnapshotKey, lease, generation.Key("version")},
		generation.ID, owner, version, nextRefreshAt).Int()
	return result == 1, err
}
