package service

import (
	"context"

	"github.com/go-redis/redis/v8"
)

// Keep exact integer token totals. Rates are indexed per Key so changing one
// Key updates the equal-weight sum and maximum without scanning other Keys.
const channelGroupMonitorCacheDeltaSource = `
local function validate_cache_delta(totals, summary, key, input, read)
 for _, entry in ipairs({{totals, key .. ':input', input}, {totals, key .. ':read', read}, {summary, 'input', input}, {summary, 'read', read}}) do
  local old = tonumber(redis.call('HGET', entry[1], entry[2]) or '0')
  if not old or old + entry[3] < 0 or old + entry[3] > 9007199254740991 then return 'group monitor window overflow' end
 end
end
local function apply_cache_delta(totals, rates, summary, key, input, read)
 local oldrate = tonumber(redis.call('ZSCORE', rates, key) or '0')
 -- Redis rejects the noncanonical integer "-0" produced when subtracting zero.
 local input_arg = input == 0 and '0' or string.format('%.0f', input)
 local read_arg = read == 0 and '0' or string.format('%.0f', read)
 local totalinput = redis.call('HINCRBY', totals, key .. ':input', input_arg)
 local totalread = redis.call('HINCRBY', totals, key .. ':read', read_arg)
 redis.call('HINCRBY', summary, 'input', input_arg)
 redis.call('HINCRBY', summary, 'read', read_arg)
 local rate = 0
 if totalinput > 0 and key ~= '0' then
  rate = totalread / totalinput * 100
  redis.call('ZADD', rates, rate, key)
 else
  redis.call('ZREM', rates, key)
 end
 if totalinput == 0 then redis.call('HDEL', totals, key .. ':input', key .. ':read') end
 redis.call('HINCRBYFLOAT', summary, 'rate_sum', rate - oldrate)
 if redis.call('ZCARD', rates) == 0 then redis.call('HSET', summary, 'rate_sum', '0') end
end
`

// Drain a retired bucket in bounded atomic chunks. Persist the HSCAN cursor:
// sparse hashes may return empty pages before the scan has completed. Each
// removal and cursor advance commit together, so a restart cannot subtract a
// Key twice. The floor fences late writers.
var expireChannelGroupMonitorCacheChunk = redis.NewScript(channelGroupMonitorCacheDeltaSource + `
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return -1 end
local floor = math.max(tonumber(redis.call('GET', KEYS[6]) or '0'), tonumber(ARGV[2]))
redis.call('SET', KEYS[6], floor)
redis.call('ZADD', KEYS[7], '+inf', KEYS[6])
redis.call('ZADD', KEYS[7], '+inf', KEYS[8])
local retired = redis.call('ZRANGEBYSCORE', KEYS[5], '-inf', '(' .. floor, 'LIMIT', 0, 1)
if #retired == 0 then return 0 end
local bucket = retired[1]
local cursor = redis.call('HGET', KEYS[8], bucket) or '0'
local page = redis.call('HSCAN', bucket, cursor, 'COUNT', 128)
local fields = page[2]
local seen, deltas = {}, {}
for i = 1, #fields, 2 do
 local key = string.match(fields[i], '^(.*):[^:]+$')
 if key and not seen[key] and #deltas < 128 then
  seen[key] = true
  local input = tonumber(redis.call('HGET', bucket, key .. ':input') or '0')
  local read = tonumber(redis.call('HGET', bucket, key .. ':read') or '0')
  local problem = validate_cache_delta(KEYS[2], KEYS[4], key, -input, -read)
  if problem then return redis.error_reply(problem) end
  table.insert(deltas, {key, input, read})
 end
end
for _, delta in ipairs(deltas) do
 apply_cache_delta(KEYS[2], KEYS[3], KEYS[4], delta[1], -delta[2], -delta[3])
 redis.call('HDEL', bucket, delta[1] .. ':input', delta[1] .. ':read')
end
if redis.call('HLEN', bucket) == 0 then
 redis.call('UNLINK', bucket)
 redis.call('ZREM', KEYS[5], bucket)
 redis.call('ZREM', KEYS[7], bucket)
 redis.call('HDEL', KEYS[8], bucket)
else
 redis.call('HSET', KEYS[8], bucket, page[1])
end
return 1
`)

func expireChannelGroupMonitorCaches(ctx context.Context, client *redis.Client, generation ChannelGroupMonitorGeneration, prefixes []string, start int64) error {
	if len(prefixes) == 0 {
		return nil
	}
	if err := expireChannelGroupMonitorCacheChunk.Load(ctx, client).Err(); err != nil {
		return err
	}
	pending := append([]string(nil), prefixes...)
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		pipe := client.Pipeline()
		commands := make([]*redis.Cmd, len(pending))
		for i, prefix := range pending {
			commands[i] = expireChannelGroupMonitorCacheChunk.EvalSha(ctx, pipe, []string{
				channelGroupMonitorGenerationIDKey, prefix + "totals", prefix + "rates", prefix + "summary",
				prefix + "buckets", prefix + "floor", generation.Key("keys"), prefix + "retirement",
			}, generation.ID, start)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
		next := make([]string, 0, len(pending))
		for i, command := range commands {
			remaining, err := command.Int()
			if err != nil {
				return err
			}
			if remaining < 0 {
				return ErrChannelGroupMonitorSnapshotPending
			}
			if remaining > 0 {
				next = append(next, pending[i])
			}
		}
		pending = next
	}
	return nil
}

var readChannelGroupMonitorCache = redis.NewScript(`
local input = tonumber(redis.call('HGET', KEYS[1], 'input') or '0')
local read = tonumber(redis.call('HGET', KEYS[1], 'read') or '0')
local count = redis.call('ZCARD', KEYS[2])
local weighted, average, maximum = '', '', ''
if input > 0 then weighted = tostring(read / input * 100) end
if count > 0 then
 average = tostring(tonumber(redis.call('HGET', KEYS[1], 'rate_sum') or '0') / count)
 maximum = redis.call('ZREVRANGE', KEYS[2], 0, 0, 'WITHSCORES')[2]
end
return {weighted, average, maximum}
`)
