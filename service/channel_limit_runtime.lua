local registry = 'channelLimitGroups:v1:registry'
local operation = ARGV[1]
local clock = redis.call('TIME')
local now = tonumber(clock[1])*1000 + math.floor(tonumber(clock[2])/1000)
local activePrefix = 'channelConcurrency:v1:active:'
local rpmPrefix = 'channelConcurrency:v1:rpm:'
local limits = 'channelConcurrency:v1:limits'
local rpmLimits = 'channelConcurrency:v1:rpm_limits'
local ttl = 120000
local groupID = tonumber(ARGV[2]) or 0
local channelID = tonumber(ARGV[3]) or 0
local attempt = ARGV[4] or ''
local waiting = tonumber(ARGV[5]) or 0
local probe = ARGV[6] == '1'
if groupID == 0 then groupID = tonumber(redis.call('HGET', registry, 'member:'..channelID) or '0') end
local prefix = 'channelLimitGroups:v1:group:'..groupID..':'
local raw = redis.call('HGET', registry, 'group:'..groupID)
local group = raw and cjson.decode(raw) or nil
local priority = -1
if group and not probe then
  for _, member in ipairs(group.members) do
    if member.channel_id == channelID then priority = member.priority end
  end
end
local active = {}
local rpm = {}
local totalActive, totalRPM = 0, 0
local function count(activeKey, rpmKey)
  redis.call('ZREMRANGEBYSCORE', activeKey, '-inf', now-ttl)
  redis.call('ZREMRANGEBYSCORE', rpmKey, '-inf', now-60000)
  return redis.call('ZCARD', activeKey), redis.call('ZCARD', rpmKey)
end
local state = ''
local priorities = {}
if group then
  active[-1], rpm[-1] = 0, 0
  for _, tier in ipairs(group.tiers) do active[tier.priority], rpm[tier.priority] = 0, 0 end
  for _, member in ipairs(group.members) do
    local id, p = member.channel_id, member.priority
    priorities[id] = p
    local a, r = count(activePrefix..id, rpmPrefix..id)
    local pa, pr = count(activePrefix..id..':probes', rpmPrefix..id..':probes')
    active[p], rpm[p] = active[p]+a-pa, rpm[p]+r-pr
    active[-1], rpm[-1] = active[-1]+pa, rpm[-1]+pr
    totalActive, totalRPM = totalActive+a, totalRPM+r
  end
  if not group.enabled then state = 'paused' end
end
if groupID > 0 and not group then state = 'unavailable' end
local channelActive, channelRPM = count(activePrefix..channelID, rpmPrefix..channelID)
local limit = tonumber(redis.call('HGET', limits, channelID) or '0')
local rpmLimit = tonumber(redis.call('HGET', rpmLimits, channelID) or '0')
local function eligible(id, p)
  local a = redis.call('ZCARD', activePrefix..id)
  local r = redis.call('ZCOUNT', rpmPrefix..id, '('..(now-60000), '+inf')
  local c = tonumber(redis.call('HGET', limits, id) or '0')
  local m = tonumber(redis.call('HGET', rpmLimits, id) or '0')
  if c > 0 and a >= c then return 'channel_concurrency' end
  if m > 0 and r >= m then return 'channel_rpm' end
  if not group then return '' end
  if group.concurrency_limit > 0 and totalActive >= group.concurrency_limit then return 'group_concurrency' end
  if group.rpm_limit > 0 and totalRPM >= group.rpm_limit then return 'group_rpm' end
  local reservedC, reservedR = 0, 0
  for _, tier in ipairs(group.tiers) do
    if p < tier.priority then
      reservedC, reservedR = reservedC+tier.reserved_concurrency, reservedR+tier.reserved_rpm
      local lowerC, lowerR = 0, 0
      for q, n in pairs(active) do if q < tier.priority then lowerC = lowerC+n end end
      for q, n in pairs(rpm) do if q < tier.priority then lowerR = lowerR+n end end
      if group.concurrency_limit > 0 and lowerC+1 > group.concurrency_limit-reservedC then return 'reserved_concurrency' end
      if group.rpm_limit > 0 and lowerR+1 > group.rpm_limit-reservedR then return 'reserved_rpm' end
    end
  end
  return ''
end
local queueKey, waiterKey = prefix..'queue', prefix..'waiters'
for _, id in ipairs(redis.call('ZRANGEBYSCORE', queueKey, '-inf', now)) do
  redis.call('ZREM', queueKey, id)
  redis.call('HDEL', waiterKey, id)
end
for _, id in ipairs(redis.call('ZRANGE', queueKey, 0, -1)) do
  local value = redis.call('HGET', waiterKey, id)
  if not value or priorities[cjson.decode(value).channel] == nil then
    redis.call('ZREM', queueKey, id)
    redis.call('HDEL', waiterKey, id)
  end
end
local function reply(ok, reason)
  local tiers, members = nil, nil
  if group then
    tiers, members = {}, {}
    for p, n in pairs(active) do table.insert(tiers, {priority=p, active=n, rpm=rpm[p] or 0}) end
    for _, m in ipairs(group.members) do table.insert(members,m.channel_id) end
  end
  return cjson.encode({acquired=ok, active=channelActive, limit=limit, current_rpm=channelRPM, rpm_limit=rpmLimit,
    group_id=groupID, priority=priority, group_active=totalActive, group_rpm=totalRPM,
    group_concurrency_limit=group and group.concurrency_limit or 0, group_rpm_limit=group and group.rpm_limit or 0,
    waiting=redis.call('ZCARD', queueKey), reason=reason, tiers=tiers, members=members})
end
if operation == 'snapshot' then return reply(false, state) end
if operation ~= 'acquire' then return redis.error_reply('无效的共享限流操作') end
if redis.call('HGET', limits, '__loaded') ~= '1' then return reply(false, 'uninitialized') end
if groupID > 0 and not group then return reply(false, 'unavailable') end
if state ~= '' then return reply(false, state) end
if redis.call('ZSCORE', activePrefix..channelID, attempt) then return reply(true, '') end
if redis.call('ZSCORE', rpmPrefix..channelID, attempt) then return reply(false, 'attempt_finished') end
local reason = eligible(channelID, priority)
if group then
  if waiting > 0 then
    local old = redis.call('HGET', waiterKey, attempt)
    local sequence
    if old then
      local previous = cjson.decode(old)
      if previous.channel == channelID then sequence = previous.sequence end
    end
    if not sequence then
      if redis.call('ZCARD', queueKey) >= 256 then return reply(false, 'queue_full') end
      sequence = redis.call('INCR', prefix..'sequence')
    end
    redis.call('HSET', waiterKey, attempt, cjson.encode({channel=channelID, probe=probe, sequence=sequence}))
    redis.call('ZADD', queueKey, now+math.min(waiting,15000), attempt)
    redis.call('PEXPIRE', waiterKey, 30000)
    redis.call('PEXPIRE', queueKey, 30000)
  end
  local winner, winnerPriority, winnerSequence = nil, -2, 0
  for _, id in ipairs(redis.call('ZRANGE', queueKey, 0, -1)) do
    local value = redis.call('HGET', waiterKey, id)
    if value then
      local w = cjson.decode(value)
      local p = priorities[w.channel]
      if p == nil then
        redis.call('ZREM', queueKey, id)
        redis.call('HDEL', waiterKey, id)
      else
        if w.probe then p = -1 end
        if eligible(w.channel, p) == '' and (p > winnerPriority or (p == winnerPriority and w.sequence < winnerSequence)) then
          winner, winnerPriority, winnerSequence = id, p, w.sequence
        end
      end
    end
  end
  if winner and winner ~= attempt and winnerPriority >= priority and reason == '' then reason = 'priority_wait' end
end
if reason ~= '' then return reply(false, reason) end
redis.call('ZREM', queueKey, attempt)
redis.call('HDEL', waiterKey, attempt)
redis.call('ZADD', activePrefix..channelID, now, attempt)
redis.call('PEXPIRE', activePrefix..channelID, ttl*2)
redis.call('ZADD', rpmPrefix..channelID, now, attempt)
redis.call('PEXPIRE', rpmPrefix..channelID, 120000)
if probe then
  redis.call('ZADD', activePrefix..channelID..':probes', now, attempt)
  redis.call('PEXPIRE', activePrefix..channelID..':probes', ttl*2)
  redis.call('ZADD', rpmPrefix..channelID..':probes', now, attempt)
  redis.call('PEXPIRE', rpmPrefix..channelID..':probes', 120000)
end
channelActive, channelRPM = channelActive+1, channelRPM+1
if group then
  totalActive, totalRPM = totalActive+1, totalRPM+1
  active[priority], rpm[priority] = (active[priority] or 0)+1, (rpm[priority] or 0)+1
end
return reply(true, '')
