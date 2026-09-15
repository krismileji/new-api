package service

import "github.com/go-redis/redis/v8"

// All amounts are integer micro-credits. Each operation is bounded: the only
// loop visits a model's at-most-100 samples, never the channel's request history.
// A refresh captures the completed counter before HTTP starts. Installing its
// result subtracts that counter, so ledger delivery time cannot affect balance.
var channelBalanceScript = redis.NewScript(`
local state = KEYS[1]
local op = ARGV[1]
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
local limit = 2251799813685247
local ttl = 172800
local dedup_ttl = 1800
local function integer(value) return string.format('%.0f', value) end
local function get(field) return tonumber(redis.call('HGET', state, field) or '0') end
local function put(field, value) redis.call('HSET', state, field, integer(value)) end
local function add(field, value)
  local total = get(field) + value
  if total < 0 or total > limit then
    redis.call('HSET', state, 'damaged', '1')
    return false
  end
  put(field, total)
  return true
end
local function sample_trim()
  local expired = redis.call('ZRANGEBYSCORE', KEYS[4], '-inf', now - 1800000)
  local sum = tonumber(redis.call('GET', KEYS[6]) or '0')
  for _, id in ipairs(expired) do
    sum = sum - tonumber(redis.call('HGET', KEYS[5], id) or '0')
    redis.call('HDEL', KEYS[5], id)
    redis.call('ZREM', KEYS[4], id)
  end
  redis.call('SET', KEYS[6], integer(math.max(0, sum)), 'EX', 1800)
  return math.max(0, sum), redis.call('ZCARD', KEYS[4])
end
if op == 'read' and redis.call('EXISTS', state) == 0 then return '' end
if redis.call('EXISTS', state) == 0 then
  redis.call('HSET', state, 'epoch', ARGV[2], 'created', integer(now), 'coverage', '0')
end
redis.call('EXPIRE', state, ttl)
local epoch = redis.call('HGET', state, 'epoch')
if op == 'configure' or op == 'sync_begin' then
  local revision = tonumber(ARGV[3])
  local cached = redis.call('GET', KEYS[8])
  if cached and revision < tonumber(cjson.decode(cached).Revision) then return 'stale' end
  if revision < get('revision') then return 'stale' end
  if op == 'configure' or revision > get('revision') or redis.call('HEXISTS', state, 'enabled') == 0 then
    redis.call('HSET', state, 'revision', ARGV[3], 'enabled', ARGV[4],
      'warning', ARGV[5], 'threshold', ARGV[6])
    redis.call('SET', KEYS[8], ARGV[7], 'EX', ttl)
  end
  redis.call('EXPIRE', KEYS[8], ttl)
  if op == 'sync_begin' then
    redis.call('HSET', state, 'fetch_id', ARGV[2], 'fetch_start', integer(now),
      'cut_completed', integer(get('completed')), 'cut_unknown', integer(get('unknown_completed')))
    return epoch
  end
elseif op == 'sync_commit' then
  if redis.call('HGET', state, 'fetch_id') ~= ARGV[2] or get('revision') ~= tonumber(ARGV[3]) then
    return 'stale'
  end
  local completed = get('completed') - get('cut_completed')
  if completed < 0 then return redis.error_reply('invalid balance counter') end
  put('completed', completed)
  put('uncertain', completed)
  put('unknown_completed', math.max(0, get('unknown_completed') - get('cut_unknown')))
  redis.call('HSET', state, 'balance', ARGV[4], 'baseline_start', integer(get('fetch_start')),
    'baseline_end', integer(now), 'baseline_id', ARGV[2], 'coverage', ARGV[5], 'sync_failed', '0')
  if ARGV[5] == '1' and get('active') == 0 then redis.call('HDEL', state, 'damaged') end
  redis.call('HDEL', state, 'fetch_id', 'cut_completed', 'cut_unknown')
elseif op == 'sync_fail' then
  if redis.call('HGET', state, 'fetch_id') ~= ARGV[2] or get('revision') ~= tonumber(ARGV[3]) then return 'stale' end
  redis.call('HSET', state, 'sync_failed', '1')
  redis.call('HDEL', state, 'fetch_id', 'cut_completed', 'cut_unknown')
elseif op == 'start' then
  local existing = redis.call('GET', KEYS[2])
  if not existing then
    local sum, count = sample_trim()
    local amount = tonumber(ARGV[3])
    local source = 'budget'
    if count >= 5 and ARGV[5] == '1' then
      amount = math.floor(sum / count + 0.5)
      source = 'average'
    end
    local known = amount >= 0
    if not known then amount = 0; source = 'unknown' end
    if not add('inflight', amount) then amount = 0; known = false; source = 'unknown' end
    add('active', 1)
    if not known then add('unknown_active', 1) end
    if source == 'average' then add('average_active', 1) end
    if source == 'budget' then add('budget_active', 1) end
    redis.call('HSET', state, 'last_samples', integer(count), 'last_model', ARGV[7] or '',
      'last_amount', integer(amount), 'last_source', source)
    redis.call('SET', KEYS[2], cjson.encode({epoch=epoch, amount=integer(amount), known=known,
      source=source, samples=count, started=integer(now), status='active'}), 'EX', ttl)
    redis.call('ZADD', KEYS[3], now, ARGV[4])
    redis.call('EXPIRE', KEYS[3], ttl)
    if ARGV[6] and string.sub(ARGV[6], 1, 1) == '{' then redis.call('SET', KEYS[7], ARGV[6], 'EX', ttl) end
  end
elseif op == 'finish' or op == 'release' or op == 'reconcile' then
  local existing = redis.call('GET', KEYS[2])
  local attempt = existing and cjson.decode(existing) or nil
  if not attempt or attempt.status == 'active' or attempt.status == 'unresolved' then
    if op == 'finish' and tonumber(ARGV[3]) < 0 then
      -- A disconnected client does not prove that upstream has stopped.
      -- Retain its reservation and incomplete coverage until authoritative
      -- settlement; a balance refresh alone must not declare it a free call.
      if not attempt or attempt.epoch ~= epoch then
        attempt = {epoch=epoch,amount='0',known=false,source='unknown',samples=0,started=integer(now)}
        add('active', 1)
        add('unknown_active', 1)
      elseif attempt.known then
        attempt.known = false
        add('unknown_active', 1)
      end
      attempt.status = 'unresolved'
      redis.call('SET', KEYS[2], cjson.encode(attempt), 'EX', ttl)
      redis.call('ZADD', KEYS[3], now, ARGV[4])
      redis.call('EXPIRE', KEYS[3], ttl)
    else
    if attempt and attempt.epoch == epoch then
      add('inflight', -tonumber(attempt.amount))
      add('active', -1)
      if not attempt.known then add('unknown_active', -1) end
      if attempt.source == 'average' then add('average_active', -1) end
      if attempt.source == 'budget' then add('budget_active', -1) end
      redis.call('ZREM', KEYS[3], ARGV[4])
    elseif attempt then
      redis.call('HSET', state, 'coverage', '0')
    end
    local at = tonumber(ARGV[6])
    if at <= 0 then at = now end
    local amount = tonumber(ARGV[3])
    local known = amount >= 0
    if op == 'reconcile' then
      -- The durable event proves completion, but its delivery time is not
      -- a debit boundary. Clear the reservation and require a fresh balance
      -- instead of counting a potentially old debit as a new consumption.
      redis.call('HSET', state, 'coverage', '0')
    end
    if op == 'finish' then
      if known then
        if at >= get('baseline_start') then
          add('completed', amount)
          if at <= get('baseline_end') then add('uncertain', amount) end
          -- A delayed event that finished before an outstanding query began
          -- is already included in that query's cutoff, even if delivered now.
          if redis.call('HEXISTS', state, 'fetch_id') == 1 and at < get('fetch_start') then
            add('cut_completed', amount)
          end
        end
        -- Late costs covered by the latest balance may still be useful samples.
        if ARGV[5] == '1' and at > now - 1800000 and at <= now and amount <= limit / 100 then
          local sum, count = sample_trim()
          if count >= 100 then
            local oldest = redis.call('ZRANGE', KEYS[4], 0, 0)[1]
            sum = sum - tonumber(redis.call('HGET', KEYS[5], oldest) or '0')
            redis.call('ZREM', KEYS[4], oldest)
            redis.call('HDEL', KEYS[5], oldest)
          end
          redis.call('ZADD', KEYS[4], at, ARGV[4])
          redis.call('HSET', KEYS[5], ARGV[4], ARGV[3])
          redis.call('SET', KEYS[6], integer(sum + amount), 'EX', 1800)
          redis.call('EXPIRE', KEYS[4], 1800)
          redis.call('EXPIRE', KEYS[5], 1800)
        end
      elseif at >= get('baseline_start') then
        add('unknown_completed', 1)
      end
    end
    -- Completed attempts only retain a short replay tombstone. The durable
    -- outbox marker is deleted, so late ledger delivery cannot re-add them.
    redis.call('SET', KEYS[2], cjson.encode({epoch=epoch,status=op,at=integer(at)}), 'EX', dedup_ttl)
    redis.call('DEL', KEYS[7])
    end
  end
elseif op == 'gap' then
  redis.call('HSET', state, 'coverage', '0')
end

-- Expired request records are not evidence of a free request. Keep the state
-- incomplete until a new snapshot can establish complete active coverage.
local expired = redis.call('ZRANGEBYSCORE', KEYS[3], '-inf', now - ttl * 1000, 'LIMIT', 0, 128)
if #expired > 0 then
  for _, id in ipairs(expired) do redis.call('ZREM', KEYS[3], id) end
  redis.call('HSET', state, 'coverage', '0', 'damaged', '1')
  if redis.call('ZCARD', KEYS[3]) == 0 then
    for _, field in ipairs({'inflight','active','average_active','budget_active','unknown_active'}) do put(field, 0) end
  end
end
local available = redis.call('HEXISTS', state, 'balance') == 1 and get('damaged') == 0
local complete = available and get('coverage') == 1 and get('unknown_active') == 0 and get('unknown_completed') == 0 and get('uncertain') == 0 and get('sync_failed') == 0
local balance = get('balance')
local consumption = get('completed') + get('inflight')
local low = false
local recover = false
local warning = tonumber(redis.call('HGET', state, 'warning') or '')
local threshold = tonumber(redis.call('HGET', state, 'threshold') or '')
if available and get('enabled') == 1 and threshold then
  local effective = balance
  if warning and balance < warning and get('coverage') == 1 then effective = balance - consumption end
  low = (effective + (warning and balance < warning and get('coverage') == 1 and get('uncertain') or 0)) < threshold
  recover = complete and effective >= threshold
end
local decision = low and 'low' or (recover and 'ok' or 'unknown')
redis.call('HSET', state, 'decision', decision)
local result = {}
for _, field in ipairs({'epoch','revision','balance','baseline_start','baseline_end','baseline_id',
  'completed','uncertain','inflight','active','average_active','budget_active','unknown_active','unknown_completed',
  'coverage','damaged','sync_failed','decision','applied_decision','last_samples','last_model','last_amount','last_source'}) do
  result[field] = redis.call('HGET', state, field) or ''
end
return cjson.encode(result)
`)
