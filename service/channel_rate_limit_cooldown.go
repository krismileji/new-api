package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/go-redis/redis/v8"
)

const (
	channelRateLimitCooldownRedisKey              = "channelRateLimitCooldown:v1:routes"
	channelRateLimitCooldownRedisRevisionKey      = "channelRateLimitCooldown:v1:control-revision"
	channelRateLimitCooldownRedisEventSequenceKey = "channelRateLimitCooldown:v1:event-sequences"
	maxManualChannelRateLimitCooldownSeconds      = 300
)

var ErrChannelRateLimitCooldownRedisUnavailable = errors.New("Redis 429 冷却读取不可用")

const channelRateLimitBypassRedisLua = `
local function channel_rate_limit_bypass_active(bypass_key, now, requested_member)
  redis.call('ZREMRANGEBYSCORE', bypass_key, '-inf', now)
  local separator = string.find(requested_member, '|', 1, true)
  if not separator then
    return false
  end
  local requested_channel = string.sub(requested_member, 1, separator - 1)
  local requested_model = string.sub(requested_member, separator + 1)
  local requested_wildcard = string.sub(requested_model, -1) == '*'
  local requested_prefix = requested_model
  if requested_wildcard then
    requested_prefix = string.sub(requested_model, 1, string.len(requested_model) - 1)
  end
  local active = redis.call('ZRANGEBYSCORE', bypass_key, '(' .. now, '+inf')
  for _, member in ipairs(active) do
    local member_separator = string.find(member, '|', 1, true)
    if member_separator and string.sub(member, 1, member_separator - 1) == requested_channel then
      local member_model = string.sub(member, member_separator + 1)
      if member_model == requested_model then
        return true
      end
      if requested_wildcard and string.sub(member_model, 1, string.len(requested_prefix)) == requested_prefix then
        return true
      end
      if string.sub(member_model, -1) == '*' then
        local member_prefix = string.sub(member_model, 1, string.len(member_model) - 1)
        if string.sub(requested_model, 1, string.len(member_prefix)) == member_prefix then
          return true
        end
      end
    end
  end
  return false
end
`

const channelRateLimitCooldownRedisExtendScript = channelRateLimitBypassRedisLua + `
local now = tonumber(ARGV[2]) or 0
local requested_until = tonumber(ARGV[3]) or 0
if channel_rate_limit_bypass_active(KEYS[2], now, ARGV[1]) then
  redis.call('ZREM', KEYS[1], ARGV[1])
  return -2
end
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
local current_until = tonumber(redis.call('ZSCORE', KEYS[1], ARGV[1]) or '0')
if requested_until > current_until then
  redis.call('ZADD', KEYS[1], requested_until, ARGV[1])
  return requested_until
end
return current_until
`

const channelRateLimitCooldownRedisGuardedExtendScript = channelRateLimitBypassRedisLua + `
local current_revision = redis.call('GET', KEYS[2])
if current_revision and current_revision ~= ARGV[4] then
  return -1
end
if not current_revision then
	redis.call('DEL', KEYS[1], KEYS[3])
  redis.call('SET', KEYS[2], ARGV[4])
end
local now = tonumber(ARGV[2]) or 0
local requested_until = tonumber(ARGV[3]) or 0
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
local current_until = tonumber(redis.call('ZSCORE', KEYS[1], ARGV[1]) or '0')
local event_sequence = ARGV[5]
if event_sequence ~= '' then
  local current_sequence = redis.call('HGET', KEYS[3], ARGV[1])
  if current_sequence and event_sequence <= current_sequence then
    return current_until
  end
  if channel_rate_limit_bypass_active(KEYS[4], now, ARGV[1]) then
    redis.call('HSET', KEYS[3], ARGV[1], event_sequence)
    redis.call('ZREM', KEYS[1], ARGV[1])
    return -2
  end
  redis.call('HSET', KEYS[3], ARGV[1], event_sequence)
elseif channel_rate_limit_bypass_active(KEYS[4], now, ARGV[1]) then
  redis.call('ZREM', KEYS[1], ARGV[1])
  return -2
end
if requested_until > now and requested_until > current_until then
  redis.call('ZADD', KEYS[1], requested_until, ARGV[1])
  return requested_until
end
return current_until
`

const channelRateLimitCooldownRedisRecoverExtendScript = channelRateLimitBypassRedisLua + `
local current_revision = redis.call('GET', KEYS[2]) or ''
if current_revision ~= ARGV[5] then
  return -1
end
redis.call('DEL', KEYS[1], KEYS[3])
redis.call('SET', KEYS[2], ARGV[4])
local now = tonumber(ARGV[2]) or 0
local requested_until = tonumber(ARGV[3]) or 0
local event_sequence = ARGV[6]
if event_sequence ~= '' then
  redis.call('HSET', KEYS[3], ARGV[1], event_sequence)
end
if channel_rate_limit_bypass_active(KEYS[4], now, ARGV[1]) then
  return -2
end
if requested_until > now then
  redis.call('ZADD', KEYS[1], requested_until, ARGV[1])
end
return requested_until
`

const channelRateLimitCooldownRedisRevisionScript = `
local previous_revision = ARGV[2]
local current_revision = redis.call('GET', KEYS[2])
if not current_revision then
  redis.call('SET', KEYS[2], ARGV[1])
  redis.call('DEL', KEYS[1], KEYS[3])
  return 1
end
if previous_revision ~= '' and current_revision and current_revision ~= previous_revision then
  return 0
end
redis.call('SET', KEYS[2], ARGV[1])
redis.call('DEL', KEYS[1], KEYS[3])
return 1
`

const channelRateLimitCooldownRedisRevisionRecoveryScript = `
local current_revision = redis.call('GET', KEYS[2]) or ''
if current_revision ~= ARGV[2] then
  return 0
end
redis.call('SET', KEYS[2], ARGV[1])
redis.call('DEL', KEYS[1], KEYS[3])
return 1
`

type channelRateLimitCooldownKey struct {
	channelId int
	modelName string
}

type channelRateLimitCooldownEntry struct {
	until    int64
	shared   bool
	revision string
}

var channelRateLimitCooldowns = struct {
	sync.Mutex
	untilByRoute map[channelRateLimitCooldownKey]channelRateLimitCooldownEntry
}{
	untilByRoute: make(map[channelRateLimitCooldownKey]channelRateLimitCooldownEntry),
}

// StartChannelRateLimitCooldown temporarily defers one upstream channel/model
// route while another candidate is available. It remains a final routing
// fallback. Repeated 429 responses may extend, but never shorten, an active
// cooldown.
func StartChannelRateLimitCooldown(channelId int, modelName string, durationSeconds int) {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if channelId <= 0 || modelName == "" || durationSeconds <= 0 {
		return
	}
	sharedRedis := common.RedisEnabled && common.RDB != nil
	if !sharedRedis {
		channelRateLimitLocalStateUpdate.Lock()
		defer channelRateLimitLocalStateUpdate.Unlock()
		if ChannelRateLimitBypassActive(context.Background(), channelId, modelName) {
			return
		}
	}
	now := common.GetTimestamp()
	until := now + int64(durationSeconds)
	key := channelRateLimitCooldownKey{channelId: channelId, modelName: modelName}
	revision := channelRateLimitCooldownControlRevision()

	ensureChannelRateLimitCooldownRedisSync()
	if !sharedRedis {
		channelRateLimitCooldowns.Lock()
		if current := channelRateLimitCooldowns.untilByRoute[key]; current.revision != revision || current.until < until {
			channelRateLimitCooldowns.untilByRoute[key] = channelRateLimitCooldownEntry{
				until: until, revision: revision,
			}
			publishChannelRateLimitCooldownSnapshotLocked()
		}
		channelRateLimitCooldowns.Unlock()
		return
	}
	sharedUntil, err := common.RDB.Eval(
		context.Background(),
		channelRateLimitCooldownRedisExtendScript,
		[]string{channelRateLimitCooldownRedisKey, channelRateLimitBypassRedisKey},
		channelRateLimitCooldownRedisMember(key),
		now,
		until,
	).Int64()
	if err != nil {
		common.SysError("同步 429 冷却到 Redis 失败: " + err.Error())
		return
	}
	if sharedUntil == -2 {
		removeChannelRateLimitCooldownRouteLocal(channelId, modelName)
		return
	}
	if channelRateLimitCooldownControlRevision() != revision {
		return
	}
	channelRateLimitCooldowns.Lock()
	if sharedUntil > now {
		current := channelRateLimitCooldowns.untilByRoute[key]
		if current.revision != revision || sharedUntil > current.until {
			current.until = sharedUntil
		}
		current.shared = true
		current.revision = revision
		channelRateLimitCooldowns.untilByRoute[key] = current
		publishChannelRateLimitCooldownSnapshotLocked()
	}
	channelRateLimitCooldowns.Unlock()
}

// StartChannelRateLimitCooldownIfControlRevision starts a configured 429
// cooldown only while the scheduler configuration revision that observed the
// response is still current.
func StartChannelRateLimitCooldownIfControlRevision(
	channelId int,
	modelName string,
	durationSeconds int,
	expectedControlRevision string,
) bool {
	if durationSeconds <= 0 {
		return false
	}
	accepted, err := startChannelRateLimitCooldownUntilIfControlRevision(
		context.Background(),
		channelId,
		modelName,
		common.GetTimestamp()+int64(durationSeconds),
		expectedControlRevision,
		0,
		false,
	)
	if err != nil {
		common.SysError("启动 429 冷却失败: " + err.Error())
		return false
	}
	return accepted
}

// StartChannelRateLimitCooldownUntilIfControlRevision applies an absolute
// event-time deadline and requires the shared Redis write to succeed.
func StartChannelRateLimitCooldownUntilIfControlRevision(
	ctx context.Context,
	channelId int,
	modelName string,
	requestedUntil int64,
	expectedControlRevision string,
	eventSequence int64,
) (bool, error) {
	return startChannelRateLimitCooldownUntilIfControlRevision(
		ctx,
		channelId,
		modelName,
		requestedUntil,
		expectedControlRevision,
		eventSequence,
		true,
	)
}

func startChannelRateLimitCooldownUntilIfControlRevision(
	ctx context.Context,
	channelId int,
	modelName string,
	requestedUntil int64,
	expectedControlRevision string,
	eventSequence int64,
	requireRedis bool,
) (accepted bool, resultErr error) {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	expectedControlRevision = strings.TrimSpace(expectedControlRevision)
	if channelId <= 0 || modelName == "" || requestedUntil <= 0 || eventSequence < 0 {
		return false, nil
	}
	if requireRedis && eventSequence == 0 {
		return false, errors.New("共享 Redis 429 冷却事件顺序无效")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sharedRedis := common.RedisEnabled && common.RDB != nil
	if !sharedRedis {
		channelRateLimitLocalStateUpdate.Lock()
		defer channelRateLimitLocalStateUpdate.Unlock()
		if ChannelRateLimitBypassActive(ctx, channelId, modelName) {
			return false, nil
		}
	}
	now := common.GetTimestamp()
	if !requireRedis && requestedUntil <= now {
		return false, nil
	}
	channelRateLimitCooldowns.Lock()
	key := channelRateLimitCooldownKey{channelId: channelId, modelName: modelName}
	previous, previousExists := channelRateLimitCooldowns.untilByRoute[key]
	localChanged := false
	defer func() {
		if !accepted && localChanged {
			if previousExists {
				channelRateLimitCooldowns.untilByRoute[key] = previous
			} else {
				delete(channelRateLimitCooldowns.untilByRoute, key)
			}
			publishChannelRateLimitCooldownSnapshotLocked()
		}
		channelRateLimitCooldowns.Unlock()
		if accepted {
			ensureChannelRateLimitCooldownRedisSync()
		}
	}()
	// Serialize the database revision check and local write with configuration cleanup.
	currentRevision, err := model.GetChannelSmartScheduleControlRevision()
	if err != nil {
		return false, fmt.Errorf("校验 429 冷却配置修订号失败: %w", err)
	}
	if currentRevision != expectedControlRevision {
		return false, nil
	}
	until := requestedUntil
	if current := channelRateLimitCooldowns.untilByRoute[key]; current.revision != expectedControlRevision || current.until < until {
		channelRateLimitCooldowns.untilByRoute[key] = channelRateLimitCooldownEntry{
			until: until, revision: expectedControlRevision,
		}
		localChanged = true
		publishChannelRateLimitCooldownSnapshotLocked()
	}
	shared := false
	eventSequenceText := ""
	if eventSequence > 0 {
		eventSequenceText = fmt.Sprintf("%019d", eventSequence)
	}
	if requireRedis && (!common.RedisEnabled || common.RDB == nil) {
		return false, errors.New("共享 Redis 429 冷却不可用")
	}
	if common.RedisEnabled && common.RDB != nil {
		sharedUntil, redisErr := common.RDB.Eval(
			ctx,
			channelRateLimitCooldownRedisGuardedExtendScript,
			[]string{
				channelRateLimitCooldownRedisKey,
				channelRateLimitCooldownRedisRevisionKey,
				channelRateLimitCooldownRedisEventSequenceKey,
				channelRateLimitBypassRedisKey,
			},
			channelRateLimitCooldownRedisMember(key),
			now,
			until,
			expectedControlRevision,
			eventSequenceText,
		).Int64()
		if redisErr != nil {
			if requireRedis {
				return false, fmt.Errorf("同步 429 冷却到 Redis 失败: %w", redisErr)
			}
			common.SysError("同步 429 冷却到 Redis 失败: " + redisErr.Error())
		} else if sharedUntil == -2 {
			return false, nil
		} else if sharedUntil < 0 {
			observedRevision, getErr := common.RDB.Get(
				ctx, channelRateLimitCooldownRedisRevisionKey,
			).Result()
			if errors.Is(getErr, redis.Nil) {
				observedRevision = ""
				getErr = nil
			}
			if getErr != nil {
				return false, fmt.Errorf("读取 Redis 429 冷却配置修订号失败: %w", getErr)
			}
			recheckedRevision, revisionErr := model.GetChannelSmartScheduleControlRevision()
			if revisionErr != nil {
				return false, fmt.Errorf("重新校验 429 冷却配置修订号失败: %w", revisionErr)
			}
			if recheckedRevision != expectedControlRevision {
				return false, nil
			}
			sharedUntil, redisErr = common.RDB.Eval(
				ctx,
				channelRateLimitCooldownRedisRecoverExtendScript,
				[]string{
					channelRateLimitCooldownRedisKey,
					channelRateLimitCooldownRedisRevisionKey,
					channelRateLimitCooldownRedisEventSequenceKey,
					channelRateLimitBypassRedisKey,
				},
				channelRateLimitCooldownRedisMember(key),
				now,
				until,
				expectedControlRevision,
				observedRevision,
				eventSequenceText,
			).Int64()
			if redisErr != nil {
				return false, fmt.Errorf("修复 Redis 429 冷却配置修订号失败: %w", redisErr)
			}
			if sharedUntil == -2 {
				return false, nil
			}
			if sharedUntil < 0 {
				return false, nil
			}
			channelRateLimitCooldowns.untilByRoute = make(map[channelRateLimitCooldownKey]channelRateLimitCooldownEntry)
			until = sharedUntil
			shared = true
		} else {
			until = sharedUntil
			shared = true
		}
	}

	current := channelRateLimitCooldowns.untilByRoute[key]
	if requireRedis && shared {
		if until > now {
			channelRateLimitCooldowns.untilByRoute[key] = channelRateLimitCooldownEntry{
				until: until, shared: true, revision: expectedControlRevision,
			}
		} else {
			delete(channelRateLimitCooldowns.untilByRoute, key)
		}
	} else if current.revision != expectedControlRevision || current.until < until {
		channelRateLimitCooldowns.untilByRoute[key] = channelRateLimitCooldownEntry{
			until: until, shared: shared, revision: expectedControlRevision,
		}
	} else if shared && until >= current.until {
		current.shared = true
		channelRateLimitCooldowns.untilByRoute[key] = current
	}
	publishChannelRateLimitCooldownSnapshotLocked()
	accepted = true
	return true, nil
}

// UpdateChannelRateLimitCooldownControlRevision advances the revision used by
// configured 429 cooldowns. Every scheduling configuration revision starts
// with an empty cooldown set.
func UpdateChannelRateLimitCooldownControlRevision(
	controlRevision string,
	previousControlRevision string,
) (bool, error) {
	controlRevision = strings.TrimSpace(controlRevision)
	previousRevision := strings.TrimSpace(previousControlRevision)
	if controlRevision == "" {
		return false, errors.New("429 冷却配置修订号不能为空")
	}
	channelRateLimitCooldowns.Lock()
	currentRevision, revisionErr := model.GetChannelSmartScheduleControlRevision()
	if revisionErr != nil {
		channelRateLimitCooldowns.Unlock()
		common.SysError("校验 429 冷却配置修订号失败: " + revisionErr.Error())
		return false, revisionErr
	}
	if currentRevision != controlRevision {
		channelRateLimitCooldowns.Unlock()
		return false, nil
	}
	channelRateLimitCooldowns.untilByRoute = make(map[channelRateLimitCooldownKey]channelRateLimitCooldownEntry)
	publishChannelRateLimitCooldownSnapshotLocked()
	if !common.RedisEnabled || common.RDB == nil {
		channelRateLimitCooldowns.Unlock()
		return true, nil
	}
	updated, err := common.RDB.Eval(
		context.Background(),
		channelRateLimitCooldownRedisRevisionScript,
		[]string{
			channelRateLimitCooldownRedisKey,
			channelRateLimitCooldownRedisRevisionKey,
			channelRateLimitCooldownRedisEventSequenceKey,
		},
		controlRevision,
		previousRevision,
	).Int64()
	if err != nil {
		channelRateLimitCooldowns.Unlock()
		common.SysError("更新 Redis 429 冷却配置修订号失败: " + err.Error())
		return false, err
	}
	if updated == 0 {
		observedRevision, getErr := common.RDB.Get(
			context.Background(), channelRateLimitCooldownRedisRevisionKey,
		).Result()
		if getErr != nil && !errors.Is(getErr, redis.Nil) {
			channelRateLimitCooldowns.Unlock()
			common.SysError("读取 Redis 429 冷却配置修订号失败: " + getErr.Error())
			return false, getErr
		}
		currentRevision, revisionErr = model.GetChannelSmartScheduleControlRevision()
		if revisionErr != nil {
			channelRateLimitCooldowns.Unlock()
			common.SysError("重新校验 429 冷却配置修订号失败: " + revisionErr.Error())
			return false, revisionErr
		}
		if currentRevision != controlRevision {
			channelRateLimitCooldowns.Unlock()
			return false, nil
		}
		updated, err = common.RDB.Eval(
			context.Background(),
			channelRateLimitCooldownRedisRevisionRecoveryScript,
			[]string{
				channelRateLimitCooldownRedisKey,
				channelRateLimitCooldownRedisRevisionKey,
				channelRateLimitCooldownRedisEventSequenceKey,
			},
			controlRevision,
			observedRevision,
		).Int64()
		if err != nil {
			channelRateLimitCooldowns.Unlock()
			common.SysError("修复 Redis 429 冷却配置修订号失败: " + err.Error())
			return false, err
		}
	}
	channelRateLimitCooldowns.Unlock()
	return updated == 1, nil
}

func ClearChannelRateLimitCooldowns() {
	channelRateLimitCooldowns.Lock()
	channelRateLimitCooldowns.untilByRoute = make(map[channelRateLimitCooldownKey]channelRateLimitCooldownEntry)
	publishChannelRateLimitCooldownSnapshotLocked()
	channelRateLimitCooldowns.Unlock()
	if common.RedisEnabled && common.RDB != nil {
		if err := common.RDB.Del(
			context.Background(),
			channelRateLimitCooldownRedisKey,
			channelRateLimitCooldownRedisRevisionKey,
			channelRateLimitCooldownRedisEventSequenceKey,
		).Err(); err != nil {
			common.SysError("清理 Redis 429 冷却失败: " + err.Error())
			return
		}
		channelRateLimitCooldowns.Lock()
		channelRateLimitCooldowns.untilByRoute = make(map[channelRateLimitCooldownKey]channelRateLimitCooldownEntry)
		publishChannelRateLimitCooldownSnapshotLocked()
		channelRateLimitCooldowns.Unlock()
	}
}

// ChannelRateLimitCooldownUpdateResult describes a manual update for one
// channel/model route. A zero CooldownUntil means that the route is not
// currently paused for 429 responses.
type ChannelRateLimitCooldownUpdateResult struct {
	CooldownUntil int64
	Changed       bool
}

// UpdateChannelRateLimitCooldown applies or clears a manually requested 429
// cooldown. Manual updates intentionally remain available when the automatic
// cooldown setting is zero; that setting only disables automatic protection.
func UpdateChannelRateLimitCooldown(
	ctx context.Context,
	channelId int,
	modelName string,
	durationSeconds int,
) (ChannelRateLimitCooldownUpdateResult, error) {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if channelId <= 0 || modelName == "" {
		return ChannelRateLimitCooldownUpdateResult{}, errors.New("渠道或模型无效")
	}
	if durationSeconds < 0 {
		return ChannelRateLimitCooldownUpdateResult{}, errors.New("429 冷却时间不能为负数")
	}
	if durationSeconds > maxManualChannelRateLimitCooldownSeconds {
		return ChannelRateLimitCooldownUpdateResult{}, errors.New("429 冷却时间不能超过 300 秒")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if durationSeconds == 0 {
		return ClearChannelRateLimitCooldownRoute(ctx, channelId, modelName)
	}

	controlRevision, err := model.GetChannelSmartScheduleControlRevision()
	if err != nil {
		return ChannelRateLimitCooldownUpdateResult{}, fmt.Errorf("读取 429 冷却配置修订号失败: %w", err)
	}
	previousUntil := ChannelRateLimitCooldownUntilMatching(channelId, modelName)
	accepted, err := startChannelRateLimitCooldownUntilIfControlRevision(
		ctx,
		channelId,
		modelName,
		common.GetTimestamp()+int64(durationSeconds),
		controlRevision,
		0,
		false,
	)
	if err != nil {
		return ChannelRateLimitCooldownUpdateResult{}, err
	}
	if !accepted {
		return ChannelRateLimitCooldownUpdateResult{
			CooldownUntil: ChannelRateLimitCooldownUntilMatching(channelId, modelName),
		}, nil
	}
	updatedUntil := ChannelRateLimitCooldownUntilMatching(channelId, modelName)
	return ChannelRateLimitCooldownUpdateResult{
		CooldownUntil: updatedUntil,
		Changed:       updatedUntil != previousUntil,
	}, nil
}

// ClearChannelRateLimitCooldownRoute removes the 429 cooldown entries that
// belong to one channel/model route without changing the global control
// revision or other channels' cooldowns.
func ClearChannelRateLimitCooldownRoute(
	ctx context.Context,
	channelId int,
	modelName string,
) (ChannelRateLimitCooldownUpdateResult, error) {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if channelId <= 0 || modelName == "" {
		return ChannelRateLimitCooldownUpdateResult{}, errors.New("渠道或模型无效")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	redisMembers := make([]string, 0)
	if common.RedisEnabled && common.RDB != nil {
		members, err := common.RDB.ZRange(ctx, channelRateLimitCooldownRedisKey, 0, -1).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return ChannelRateLimitCooldownUpdateResult{}, fmt.Errorf("读取 Redis 429 冷却路由失败: %w", err)
		}
		for _, member := range members {
			key, ok := parseChannelRateLimitCooldownRedisMember(member)
			if ok && channelRateLimitCooldownRouteMatches(channelId, modelName, key) {
				redisMembers = append(redisMembers, member)
			}
		}
		if len(redisMembers) > 0 {
			pipe := common.RDB.TxPipeline()
			redisMemberValues := make([]any, len(redisMembers))
			for index, member := range redisMembers {
				redisMemberValues[index] = member
			}
			pipe.ZRem(ctx, channelRateLimitCooldownRedisKey, redisMemberValues...)
			pipe.HDel(ctx, channelRateLimitCooldownRedisEventSequenceKey, redisMembers...)
			if _, err := pipe.Exec(ctx); err != nil {
				return ChannelRateLimitCooldownUpdateResult{}, fmt.Errorf("清理 Redis 429 冷却失败: %w", err)
			}
		}
	}

	changed := len(redisMembers) > 0
	channelRateLimitCooldowns.Lock()
	for key := range channelRateLimitCooldowns.untilByRoute {
		if !channelRateLimitCooldownRouteMatches(channelId, modelName, key) {
			continue
		}
		delete(channelRateLimitCooldowns.untilByRoute, key)
		changed = true
	}
	if changed {
		publishChannelRateLimitCooldownSnapshotLocked()
	}
	channelRateLimitCooldowns.Unlock()
	return ChannelRateLimitCooldownUpdateResult{Changed: changed}, nil
}

func removeChannelRateLimitCooldownRouteLocal(channelId int, modelName string) bool {
	changed := false
	channelRateLimitCooldowns.Lock()
	for key := range channelRateLimitCooldowns.untilByRoute {
		if !channelRateLimitCooldownRouteMatches(channelId, modelName, key) {
			continue
		}
		delete(channelRateLimitCooldowns.untilByRoute, key)
		changed = true
	}
	if changed {
		publishChannelRateLimitCooldownSnapshotLocked()
	}
	channelRateLimitCooldowns.Unlock()
	return changed
}

func ChannelRateLimitCooldownUntil(channelId int, modelName string) int64 {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if channelId <= 0 || modelName == "" {
		return 0
	}
	ensureChannelRateLimitCooldownRedisSync()
	key := channelRateLimitCooldownKey{channelId: channelId, modelName: modelName}
	now := common.GetTimestamp()
	controlRevision := channelRateLimitCooldownControlRevision()
	entry := loadChannelRateLimitCooldownSnapshot().untilByRoute[key]
	if entry.revision != controlRevision || entry.until <= now {
		return 0
	}
	return entry.until
}

// ChannelRateLimitCooldownUntilMatching returns the latest active cooldown for
// an exact route model or any concrete model covered by a wildcard route.
func ChannelRateLimitCooldownUntilMatching(channelId int, modelName string) int64 {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if channelId <= 0 || modelName == "" {
		return 0
	}
	if common.RedisEnabled && common.RDB != nil {
		bypassUntil, err := ChannelRateLimitBypassUntilMatchingFromRedis(
			context.Background(), channelId, modelName,
		)
		if err != nil {
			logChannelRateLimitBypassRedisError(err)
			return 0
		}
		if bypassUntil > 0 {
			return 0
		}
	} else if channelRateLimitBypassUntilMatchingLocal(channelId, modelName) > 0 {
		return 0
	}
	if !strings.HasSuffix(modelName, "*") {
		return ChannelRateLimitCooldownUntil(channelId, modelName)
	}
	ensureChannelRateLimitCooldownRedisSync()
	prefix := strings.TrimSuffix(modelName, "*")
	now := common.GetTimestamp()
	controlRevision := channelRateLimitCooldownControlRevision()
	latestUntil := int64(0)
	for key, entry := range loadChannelRateLimitCooldownSnapshot().untilByRoute {
		if key.channelId != channelId || entry.revision != controlRevision || entry.until <= now ||
			!strings.HasPrefix(key.modelName, prefix) {
			continue
		}
		latestUntil = max(latestUntil, entry.until)
	}
	return latestUntil
}

// ChannelRateLimitCooldownUntilMatchingFromRedis reads the shared cooldown
// state directly. Redis-backed aggregation must use this reader so a stale
// process-local snapshot cannot influence a replayed event.
func ChannelRateLimitCooldownUntilMatchingFromRedis(
	ctx context.Context,
	channelId int,
	modelName string,
) (int64, error) {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if channelId <= 0 || modelName == "" {
		return 0, nil
	}
	if !common.RedisEnabled || common.RDB == nil {
		return 0, ErrChannelRateLimitCooldownRedisUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if bypassUntil, err := ChannelRateLimitBypassUntilMatchingFromRedis(ctx, channelId, modelName); err != nil {
		return 0, fmt.Errorf("读取 Redis 429 限制暂停状态失败: %w", err)
	} else if bypassUntil > 0 {
		return 0, nil
	}

	client := common.RDB
	now := common.GetTimestamp()
	expectedRevision := channelRateLimitCooldownControlRevision()
	wildcard := strings.HasSuffix(modelName, "*")
	pipe := client.TxPipeline()
	revisionCommand := pipe.Get(ctx, channelRateLimitCooldownRedisRevisionKey)
	var exactCommand *redis.FloatCmd
	var activeCommand *redis.ZSliceCmd
	if wildcard {
		activeCommand = pipe.ZRangeByScoreWithScores(
			ctx,
			channelRateLimitCooldownRedisKey,
			&redis.ZRangeBy{Min: "(" + strconv.FormatInt(now, 10), Max: "+inf"},
		)
	} else {
		exactCommand = pipe.ZScore(
			ctx,
			channelRateLimitCooldownRedisKey,
			channelRateLimitCooldownRedisMember(channelRateLimitCooldownKey{
				channelId: channelId,
				modelName: modelName,
			}),
		)
	}
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return 0, fmt.Errorf("读取 Redis 429 冷却失败: %w", err)
	}
	observedRevision, revisionErr := revisionCommand.Result()
	if errors.Is(revisionErr, redis.Nil) {
		observedRevision = ""
		revisionErr = nil
	}
	if revisionErr != nil {
		return 0, fmt.Errorf("读取 Redis 429 冷却配置修订号失败: %w", revisionErr)
	}
	if observedRevision != expectedRevision || channelRateLimitCooldownControlRevision() != expectedRevision {
		return 0, fmt.Errorf(
			"Redis 429 冷却配置修订号不一致: got=%q want=%q",
			observedRevision,
			expectedRevision,
		)
	}

	if exactCommand != nil {
		if exactErr := exactCommand.Err(); exactErr != nil && !errors.Is(exactErr, redis.Nil) {
			return 0, fmt.Errorf("读取 Redis 429 冷却路由失败: %w", exactErr)
		}
		until := int64(exactCommand.Val())
		if until <= now {
			return 0, nil
		}
		return until, nil
	}
	if activeErr := activeCommand.Err(); activeErr != nil && !errors.Is(activeErr, redis.Nil) {
		return 0, fmt.Errorf("读取 Redis 429 冷却路由失败: %w", activeErr)
	}
	prefix := strings.TrimSuffix(modelName, "*")
	latestUntil := int64(0)
	for _, item := range activeCommand.Val() {
		key, ok := parseChannelRateLimitCooldownRedisMember(item.Member)
		until := int64(item.Score)
		if !ok || key.channelId != channelId || until <= now {
			continue
		}
		if wildcard && !strings.HasPrefix(key.modelName, prefix) {
			continue
		}
		if !wildcard && key.modelName != modelName {
			continue
		}
		latestUntil = max(latestUntil, until)
	}
	return latestUntil, nil
}

func channelRateLimitCooldownChannelIds(modelName string, now int64) []int {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if modelName == "" {
		return nil
	}

	ensureChannelRateLimitCooldownRedisSync()
	controlRevision := channelRateLimitCooldownControlRevision()
	snapshot := loadChannelRateLimitCooldownSnapshot()
	seenChannelIds := make(map[int]struct{})
	for key, entry := range snapshot.untilByRoute {
		if entry.revision == controlRevision && entry.until > now &&
			channelRateLimitCooldownModelMatches(modelName, key.modelName) {
			if _, seen := seenChannelIds[key.channelId]; seen {
				continue
			}
			seenChannelIds[key.channelId] = struct{}{}
		}
	}
	if len(seenChannelIds) == 0 {
		return nil
	}

	bypassedChannelIds := make(map[int]struct{})
	if common.RedisEnabled && common.RDB != nil {
		var err error
		bypassedChannelIds, err = channelRateLimitBypassChannelIdsFromRedis(
			context.Background(), modelName, seenChannelIds,
		)
		if err != nil {
			logChannelRateLimitBypassRedisError(err)
			// A shared bypass may be active but unreadable. Do not apply any
			// cooldown until the authoritative state can be checked.
			return nil
		}
	} else {
		for channelId := range seenChannelIds {
			if channelRateLimitBypassUntilMatchingLocal(channelId, modelName) > 0 {
				bypassedChannelIds[channelId] = struct{}{}
			}
		}
	}

	channelIds := make([]int, 0, len(seenChannelIds))
	for channelId := range seenChannelIds {
		if _, bypassed := bypassedChannelIds[channelId]; !bypassed {
			channelIds = append(channelIds, channelId)
		}
	}
	sort.Ints(channelIds)
	return channelIds
}

func channelRateLimitCooldownRouteMatches(
	channelId int,
	modelName string,
	key channelRateLimitCooldownKey,
) bool {
	if key.channelId != channelId {
		return false
	}
	if strings.HasSuffix(modelName, "*") {
		return strings.HasPrefix(key.modelName, strings.TrimSuffix(modelName, "*"))
	}
	return key.modelName == modelName
}

func channelRateLimitCooldownModelMatches(requestedModel string, cooldownModel string) bool {
	if requestedModel == cooldownModel {
		return true
	}
	if strings.HasSuffix(requestedModel, "*") {
		return strings.HasPrefix(cooldownModel, strings.TrimSuffix(requestedModel, "*"))
	}
	if !strings.HasSuffix(cooldownModel, "*") {
		return false
	}
	return strings.HasPrefix(requestedModel, strings.TrimSuffix(cooldownModel, "*"))
}

func channelRateLimitCooldownControlRevision() string {
	common.OptionMapRWMutex.RLock()
	revision := strings.TrimSpace(common.OptionMap[model.ChannelSmartScheduleControlRevisionOption])
	common.OptionMapRWMutex.RUnlock()
	return revision
}

func channelRateLimitCooldownRedisMember(key channelRateLimitCooldownKey) string {
	return strconv.Itoa(key.channelId) + "|" + key.modelName
}

func parseChannelRateLimitCooldownRedisMember(value any) (channelRateLimitCooldownKey, bool) {
	member, ok := value.(string)
	if !ok {
		return channelRateLimitCooldownKey{}, false
	}
	channelIdText, modelName, found := strings.Cut(member, "|")
	if !found {
		return channelRateLimitCooldownKey{}, false
	}
	channelId, err := strconv.Atoi(channelIdText)
	if err != nil || channelId <= 0 || modelName == "" {
		return channelRateLimitCooldownKey{}, false
	}
	return channelRateLimitCooldownKey{channelId: channelId, modelName: modelName}, true
}

func applyChannelRateLimitCooldowns(
	modelName string,
	options model.ChannelSelectionOptions,
) model.ChannelSelectionOptions {
	cooldownChannelIds := channelRateLimitCooldownChannelIds(modelName, common.GetTimestamp())
	if len(cooldownChannelIds) == 0 {
		return options
	}

	excluded := make(map[int]struct{}, len(options.ExcludedChannelIds)+len(cooldownChannelIds))
	for _, channelId := range options.ExcludedChannelIds {
		excluded[channelId] = struct{}{}
	}
	for _, channelId := range cooldownChannelIds {
		excluded[channelId] = struct{}{}
	}
	options.ExcludedChannelIds = make([]int, 0, len(excluded))
	for channelId := range excluded {
		options.ExcludedChannelIds = append(options.ExcludedChannelIds, channelId)
	}
	sort.Ints(options.ExcludedChannelIds)
	return options
}

// getRandomSatisfiedChannelWithRateLimitFallback keeps active cooldowns as a
// routing preference. Request-scoped exclusions remain enforced when the
// cooled route is selected as the final candidate.
func getRandomSatisfiedChannelWithRateLimitFallback(
	group string,
	modelName string,
	retry int,
	requestPath string,
	options model.ChannelSelectionOptions,
) (*model.Channel, error) {
	optionsWithoutRateLimitCooldown := options
	options = applyChannelRateLimitCooldowns(modelName, options)
	hardExcluded := make(map[int]struct{}, len(optionsWithoutRateLimitCooldown.ExcludedChannelIds))
	for _, channelId := range optionsWithoutRateLimitCooldown.ExcludedChannelIds {
		hardExcluded[channelId] = struct{}{}
	}
	hasFallbackCandidate := false
	for _, channelId := range options.ExcludedChannelIds {
		if _, alreadyExcluded := hardExcluded[channelId]; !alreadyExcluded {
			hasFallbackCandidate = true
			break
		}
	}
	channel, err := model.GetRandomSatisfiedChannel(group, modelName, retry, requestPath, options)
	if err != nil || channel != nil || !hasFallbackCandidate {
		return channel, err
	}
	return model.GetRandomSatisfiedChannel(
		group,
		modelName,
		retry,
		requestPath,
		optionsWithoutRateLimitCooldown,
	)
}
