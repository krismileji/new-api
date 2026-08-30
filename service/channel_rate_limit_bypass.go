package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/go-redis/redis/v8"
)

const (
	channelRateLimitBypassRedisKey         = "channelRateLimitBypass:v1:routes"
	maxChannelRateLimitBypassDuration      = 300 * 60
	channelRateLimitBypassSecondsPerMinute = 60
)

const channelRateLimitBypassRedisUpdateScript = `
local now = tonumber(ARGV[1]) or 0
local requested_until = tonumber(ARGV[2]) or 0
local requested_channel = ARGV[3]
local requested_model = ARGV[4]
local requested_member = requested_channel .. '|' .. requested_model

local function route_matches(member)
  local separator = string.find(member, '|', 1, true)
  if not separator then
    return false
  end
  if string.sub(member, 1, separator - 1) ~= requested_channel then
    return false
  end
  local member_model = string.sub(member, separator + 1)
  if string.sub(requested_model, -1) == '*' then
    return string.sub(member_model, 1, string.len(requested_model) - 1) ==
      string.sub(requested_model, 1, string.len(requested_model) - 1)
  end
  return member_model == requested_model
end

redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
local changed = 0
if requested_until > now then
  local previous = tonumber(redis.call('ZSCORE', KEYS[1], requested_member) or '0')
  if previous ~= requested_until then
    changed = 1
  end
  redis.call('ZADD', KEYS[1], requested_until, requested_member)
else
  local bypass_members = redis.call('ZRANGE', KEYS[1], 0, -1)
  for _, member in ipairs(bypass_members) do
    if route_matches(member) then
      redis.call('ZREM', KEYS[1], member)
      changed = 1
    end
  end
end

local cooldown_members = redis.call('ZRANGE', KEYS[2], 0, -1)
for _, member in ipairs(cooldown_members) do
  if route_matches(member) then
    redis.call('ZREM', KEYS[2], member)
    changed = 1
  end
end
return changed
`

type ChannelRateLimitBypassUpdateResult struct {
	BypassUntil int64
	Changed     bool
}

var channelRateLimitBypasses = struct {
	sync.RWMutex
	untilByRoute map[channelRateLimitCooldownKey]int64
}{
	untilByRoute: make(map[channelRateLimitCooldownKey]int64),
}

var channelRateLimitBypassGeneration atomic.Uint64
var channelRateLimitBypassRedisLastErrorLog atomic.Int64
var channelRateLimitLocalStateUpdate sync.Mutex

func UpdateChannelRateLimitBypass(
	ctx context.Context,
	channelId int,
	modelName string,
	durationSeconds int,
) (ChannelRateLimitBypassUpdateResult, error) {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if channelId <= 0 || modelName == "" {
		return ChannelRateLimitBypassUpdateResult{}, errors.New("渠道或模型无效")
	}
	if durationSeconds < 0 || durationSeconds > maxChannelRateLimitBypassDuration ||
		durationSeconds%channelRateLimitBypassSecondsPerMinute != 0 {
		return ChannelRateLimitBypassUpdateResult{}, errors.New("429 限制暂停时间必须在 0 到 300 分钟之间")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	until := int64(0)
	if durationSeconds > 0 {
		until = common.GetTimestamp() + int64(durationSeconds)
	}
	sharedRedis := common.RedisEnabled && common.RDB != nil
	if !sharedRedis {
		channelRateLimitLocalStateUpdate.Lock()
		defer channelRateLimitLocalStateUpdate.Unlock()
	}

	changed := false
	if sharedRedis {
		updated, err := common.RDB.Eval(
			ctx,
			channelRateLimitBypassRedisUpdateScript,
			[]string{
				channelRateLimitBypassRedisKey,
				channelRateLimitCooldownRedisKey,
			},
			common.GetTimestamp(),
			until,
			channelId,
			modelName,
		).Int64()
		if err != nil {
			return ChannelRateLimitBypassUpdateResult{}, fmt.Errorf("原子更新 Redis 429 限制暂停状态失败: %w", err)
		}
		changed = updated == 1
	}

	localChanged := updateChannelRateLimitBypassLocal(channelId, modelName, until)
	removeChannelRateLimitCooldownRouteLocal(channelId, modelName)
	if !sharedRedis {
		changed = localChanged
	}
	ensureChannelRateLimitCooldownRedisSync()
	return ChannelRateLimitBypassUpdateResult{
		BypassUntil: until,
		Changed:     changed,
	}, nil
}

func ChannelRateLimitBypassUntilMatching(channelId int, modelName string) int64 {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if channelId <= 0 || modelName == "" {
		return 0
	}
	if common.RedisEnabled && common.RDB != nil {
		until, err := ChannelRateLimitBypassUntilMatchingFromRedis(
			context.Background(), channelId, modelName,
		)
		if err == nil {
			return until
		}
		logChannelRateLimitBypassRedisError(err)
	}
	return channelRateLimitBypassUntilMatchingLocal(channelId, modelName)
}

func channelRateLimitBypassUntilMatchingLocal(channelId int, modelName string) int64 {
	ensureChannelRateLimitCooldownRedisSync()
	now := common.GetTimestamp()
	latestUntil := int64(0)
	channelRateLimitBypasses.RLock()
	for key, until := range channelRateLimitBypasses.untilByRoute {
		if key.channelId == channelId && until > now &&
			channelRateLimitCooldownModelMatches(modelName, key.modelName) {
			latestUntil = max(latestUntil, until)
		}
	}
	channelRateLimitBypasses.RUnlock()
	return latestUntil
}

func ChannelRateLimitBypassActive(ctx context.Context, channelId int, modelName string) bool {
	if common.RedisEnabled && common.RDB != nil {
		until, err := ChannelRateLimitBypassUntilMatchingFromRedis(ctx, channelId, modelName)
		if err == nil {
			return until > 0
		}
		logChannelRateLimitBypassRedisError(err)
		// Shared bypass state is authoritative. When it cannot be read, do not
		// create a cooldown or schedule a 429 that may have been bypassed.
		return true
	}
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	return channelRateLimitBypassUntilMatchingLocal(channelId, modelName) > 0
}

func ChannelRateLimitBypassUntilMatchingFromRedis(
	ctx context.Context,
	channelId int,
	modelName string,
) (int64, error) {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if channelId <= 0 || modelName == "" {
		return 0, nil
	}
	if !common.RedisEnabled || common.RDB == nil {
		return channelRateLimitBypassUntilMatchingLocal(channelId, modelName), nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := common.GetTimestamp()
	items, err := common.RDB.ZRangeByScoreWithScores(
		ctx,
		channelRateLimitBypassRedisKey,
		&redis.ZRangeBy{Min: "(" + strconv.FormatInt(now, 10), Max: "+inf"},
	).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return 0, err
	}
	latestUntil := int64(0)
	for _, item := range items {
		key, ok := parseChannelRateLimitCooldownRedisMember(item.Member)
		until := int64(item.Score)
		if ok && key.channelId == channelId && until > now &&
			channelRateLimitCooldownModelMatches(modelName, key.modelName) {
			latestUntil = max(latestUntil, until)
		}
	}
	return latestUntil, nil
}

func channelRateLimitBypassChannelIdsFromRedis(
	ctx context.Context,
	modelName string,
	candidates map[int]struct{},
) (map[int]struct{}, error) {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	bypassed := make(map[int]struct{})
	if modelName == "" || len(candidates) == 0 {
		return bypassed, nil
	}
	if !common.RedisEnabled || common.RDB == nil {
		for channelId := range candidates {
			if channelRateLimitBypassUntilMatchingLocal(channelId, modelName) > 0 {
				bypassed[channelId] = struct{}{}
			}
		}
		return bypassed, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := common.GetTimestamp()
	items, err := common.RDB.ZRangeByScoreWithScores(
		ctx,
		channelRateLimitBypassRedisKey,
		&redis.ZRangeBy{Min: "(" + strconv.FormatInt(now, 10), Max: "+inf"},
	).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	for _, item := range items {
		key, ok := parseChannelRateLimitCooldownRedisMember(item.Member)
		if !ok || int64(item.Score) <= now {
			continue
		}
		if _, candidate := candidates[key.channelId]; !candidate {
			continue
		}
		if channelRateLimitCooldownModelMatches(modelName, key.modelName) {
			bypassed[key.channelId] = struct{}{}
		}
	}
	return bypassed, nil
}

func updateChannelRateLimitBypassLocal(channelId int, modelName string, until int64) bool {
	changed := false
	channelRateLimitBypasses.Lock()
	if until > 0 {
		key := channelRateLimitCooldownKey{channelId: channelId, modelName: modelName}
		changed = channelRateLimitBypasses.untilByRoute[key] != until
		channelRateLimitBypasses.untilByRoute[key] = until
	} else {
		for key := range channelRateLimitBypasses.untilByRoute {
			if channelRateLimitCooldownRouteMatches(channelId, modelName, key) {
				delete(channelRateLimitBypasses.untilByRoute, key)
				changed = true
			}
		}
	}
	if changed {
		channelRateLimitBypassGeneration.Add(1)
	}
	channelRateLimitBypasses.Unlock()
	return changed
}

func ClearChannelRateLimitBypasses() {
	channelRateLimitBypasses.Lock()
	channelRateLimitBypasses.untilByRoute = make(map[channelRateLimitCooldownKey]int64)
	channelRateLimitBypassGeneration.Add(1)
	channelRateLimitBypasses.Unlock()
	if common.RedisEnabled && common.RDB != nil {
		if err := common.RDB.Del(context.Background(), channelRateLimitBypassRedisKey).Err(); err != nil {
			common.SysError("清理 Redis 429 限制暂停状态失败: " + err.Error())
		}
	}
}

func pruneExpiredChannelRateLimitBypasses() {
	now := common.GetTimestamp()
	channelRateLimitBypasses.Lock()
	changed := false
	for key, until := range channelRateLimitBypasses.untilByRoute {
		if until <= now {
			delete(channelRateLimitBypasses.untilByRoute, key)
			changed = true
		}
	}
	if changed {
		channelRateLimitBypassGeneration.Add(1)
	}
	channelRateLimitBypasses.Unlock()
}

func syncChannelRateLimitBypassesFromRedis(ctx context.Context, client *redis.Client) {
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, channelRateLimitCooldownRedisSyncTimeout)
	defer cancel()
	generation := channelRateLimitBypassGeneration.Load()
	now := common.GetTimestamp()
	pipe := client.TxPipeline()
	pipe.ZRemRangeByScore(ctx, channelRateLimitBypassRedisKey, "-inf", strconv.FormatInt(now, 10))
	active := pipe.ZRangeByScoreWithScores(
		ctx,
		channelRateLimitBypassRedisKey,
		&redis.ZRangeBy{Min: "(" + strconv.FormatInt(now, 10), Max: "+inf"},
	)
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		logChannelRateLimitBypassRedisError(err)
		return
	}
	if channelRateLimitBypassGeneration.Load() != generation {
		return
	}
	next := make(map[channelRateLimitCooldownKey]int64, len(active.Val()))
	for _, item := range active.Val() {
		key, ok := parseChannelRateLimitCooldownRedisMember(item.Member)
		until := int64(item.Score)
		if ok && until > now {
			next[key] = until
		}
	}
	channelRateLimitBypasses.Lock()
	if channelRateLimitBypassGeneration.Load() == generation {
		channelRateLimitBypasses.untilByRoute = next
		channelRateLimitBypassGeneration.Add(1)
	}
	channelRateLimitBypasses.Unlock()
}

func logChannelRateLimitBypassRedisError(err error) {
	if err == nil {
		return
	}
	now := time.Now().Unix()
	last := channelRateLimitBypassRedisLastErrorLog.Load()
	if now-last < int64(channelRateLimitCooldownRedisErrorLogGap/time.Second) ||
		!channelRateLimitBypassRedisLastErrorLog.CompareAndSwap(last, now) {
		return
	}
	common.SysError("读取或同步 Redis 429 限制暂停状态失败: " + err.Error())
}
