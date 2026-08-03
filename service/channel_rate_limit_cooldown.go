package service

import (
	"context"
	"errors"
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
	channelRateLimitCooldownRedisKey         = "channelRateLimitCooldown:v1:routes"
	channelRateLimitCooldownRedisRevisionKey = "channelRateLimitCooldown:v1:control-revision"
)

const channelRateLimitCooldownRedisExtendScript = `
local now = tonumber(ARGV[2]) or 0
local requested_until = tonumber(ARGV[3]) or 0
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
local current_until = tonumber(redis.call('ZSCORE', KEYS[1], ARGV[1]) or '0')
if requested_until > current_until then
  redis.call('ZADD', KEYS[1], requested_until, ARGV[1])
  return requested_until
end
return current_until
`

const channelRateLimitCooldownRedisGuardedExtendScript = `
local current_revision = redis.call('GET', KEYS[2])
if current_revision and current_revision ~= ARGV[4] then
  return -1
end
if not current_revision then
	redis.call('DEL', KEYS[1])
  redis.call('SET', KEYS[2], ARGV[4])
end
local now = tonumber(ARGV[2]) or 0
local requested_until = tonumber(ARGV[3]) or 0
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
local current_until = tonumber(redis.call('ZSCORE', KEYS[1], ARGV[1]) or '0')
if requested_until > current_until then
  redis.call('ZADD', KEYS[1], requested_until, ARGV[1])
  return requested_until
end
return current_until
`

const channelRateLimitCooldownRedisRecoverExtendScript = `
local current_revision = redis.call('GET', KEYS[2]) or ''
if current_revision ~= ARGV[5] then
  return -1
end
redis.call('DEL', KEYS[1])
redis.call('SET', KEYS[2], ARGV[4])
local now = tonumber(ARGV[2]) or 0
local requested_until = tonumber(ARGV[3]) or 0
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
  redis.call('DEL', KEYS[1])
  return 1
end
if previous_revision ~= '' and current_revision and current_revision ~= previous_revision then
  return 0
end
redis.call('SET', KEYS[2], ARGV[1])
redis.call('DEL', KEYS[1])
return 1
`

const channelRateLimitCooldownRedisRevisionRecoveryScript = `
local current_revision = redis.call('GET', KEYS[2]) or ''
if current_revision ~= ARGV[2] then
  return 0
end
redis.call('SET', KEYS[2], ARGV[1])
redis.call('DEL', KEYS[1])
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

// StartChannelRateLimitCooldown temporarily removes one upstream channel/model
// route from new selections. Repeated 429 responses may extend, but never
// shorten, an active cooldown.
func StartChannelRateLimitCooldown(channelId int, modelName string, durationSeconds int) {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if channelId <= 0 || modelName == "" || durationSeconds <= 0 {
		return
	}
	until := common.GetTimestamp() + int64(durationSeconds)
	key := channelRateLimitCooldownKey{channelId: channelId, modelName: modelName}
	revision := channelRateLimitCooldownControlRevision()

	channelRateLimitCooldowns.Lock()
	if current := channelRateLimitCooldowns.untilByRoute[key]; current.until < until {
		channelRateLimitCooldowns.untilByRoute[key] = channelRateLimitCooldownEntry{
			until: until, revision: revision,
		}
	}
	channelRateLimitCooldowns.Unlock()

	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	sharedUntil, err := common.RDB.Eval(
		context.Background(),
		channelRateLimitCooldownRedisExtendScript,
		[]string{channelRateLimitCooldownRedisKey},
		channelRateLimitCooldownRedisMember(key),
		common.GetTimestamp(),
		until,
	).Int64()
	if err != nil {
		common.SysError("同步 429 冷却到 Redis 失败: " + err.Error())
		return
	}
	channelRateLimitCooldowns.Lock()
	current := channelRateLimitCooldowns.untilByRoute[key]
	if sharedUntil > current.until {
		current.until = sharedUntil
	}
	current.shared = true
	current.revision = revision
	channelRateLimitCooldowns.untilByRoute[key] = current
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
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	expectedControlRevision = strings.TrimSpace(expectedControlRevision)
	if channelId <= 0 || modelName == "" || durationSeconds <= 0 {
		return false
	}
	channelRateLimitCooldowns.Lock()
	defer channelRateLimitCooldowns.Unlock()
	// Serialize the database revision check and local write with configuration cleanup.
	currentRevision, err := model.GetChannelSmartScheduleControlRevision()
	if err != nil {
		common.SysError("校验 429 冷却配置修订号失败: " + err.Error())
		return false
	}
	if currentRevision != expectedControlRevision {
		return false
	}
	now := common.GetTimestamp()
	until := now + int64(durationSeconds)
	key := channelRateLimitCooldownKey{channelId: channelId, modelName: modelName}
	shared := false
	if common.RedisEnabled && common.RDB != nil {
		sharedUntil, redisErr := common.RDB.Eval(
			context.Background(),
			channelRateLimitCooldownRedisGuardedExtendScript,
			[]string{channelRateLimitCooldownRedisKey, channelRateLimitCooldownRedisRevisionKey},
			channelRateLimitCooldownRedisMember(key),
			now,
			until,
			expectedControlRevision,
		).Int64()
		if redisErr != nil {
			common.SysError("同步 429 冷却到 Redis 失败: " + redisErr.Error())
		} else if sharedUntil < 0 {
			observedRevision, getErr := common.RDB.Get(
				context.Background(), channelRateLimitCooldownRedisRevisionKey,
			).Result()
			if errors.Is(getErr, redis.Nil) {
				observedRevision = ""
				getErr = nil
			}
			if getErr != nil {
				common.SysError("读取 Redis 429 冷却配置修订号失败: " + getErr.Error())
				return false
			}
			recheckedRevision, revisionErr := model.GetChannelSmartScheduleControlRevision()
			if revisionErr != nil {
				common.SysError("重新校验 429 冷却配置修订号失败: " + revisionErr.Error())
				return false
			}
			if recheckedRevision != expectedControlRevision {
				return false
			}
			sharedUntil, redisErr = common.RDB.Eval(
				context.Background(),
				channelRateLimitCooldownRedisRecoverExtendScript,
				[]string{channelRateLimitCooldownRedisKey, channelRateLimitCooldownRedisRevisionKey},
				channelRateLimitCooldownRedisMember(key),
				now,
				until,
				expectedControlRevision,
				observedRevision,
			).Int64()
			if redisErr != nil {
				common.SysError("修复 Redis 429 冷却配置修订号失败: " + redisErr.Error())
				return false
			}
			if sharedUntil < 0 {
				return false
			}
			channelRateLimitCooldowns.untilByRoute = make(map[channelRateLimitCooldownKey]channelRateLimitCooldownEntry)
			until = sharedUntil
			shared = true
		} else {
			until = sharedUntil
			shared = true
		}
	}

	if current := channelRateLimitCooldowns.untilByRoute[key]; current.until < until {
		channelRateLimitCooldowns.untilByRoute[key] = channelRateLimitCooldownEntry{
			until: until, shared: shared, revision: expectedControlRevision,
		}
	}
	return true
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
	if !common.RedisEnabled || common.RDB == nil {
		channelRateLimitCooldowns.Unlock()
		return true, nil
	}
	updated, err := common.RDB.Eval(
		context.Background(),
		channelRateLimitCooldownRedisRevisionScript,
		[]string{channelRateLimitCooldownRedisKey, channelRateLimitCooldownRedisRevisionKey},
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
			[]string{channelRateLimitCooldownRedisKey, channelRateLimitCooldownRedisRevisionKey},
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
	channelRateLimitCooldowns.Unlock()
	if common.RedisEnabled && common.RDB != nil {
		if err := common.RDB.Del(
			context.Background(), channelRateLimitCooldownRedisKey, channelRateLimitCooldownRedisRevisionKey,
		).Err(); err != nil {
			common.SysError("清理 Redis 429 冷却失败: " + err.Error())
		}
	}
}

func ChannelRateLimitCooldownUntil(channelId int, modelName string) int64 {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if channelId <= 0 || modelName == "" {
		return 0
	}
	key := channelRateLimitCooldownKey{channelId: channelId, modelName: modelName}
	now := common.GetTimestamp()
	controlRevision := channelRateLimitCooldownControlRevision()
	if common.RedisEnabled && common.RDB != nil {
		pipe := common.RDB.TxPipeline()
		revisionCommand := pipe.Get(context.Background(), channelRateLimitCooldownRedisRevisionKey)
		untilCommand := pipe.ZScore(
			context.Background(), channelRateLimitCooldownRedisKey, channelRateLimitCooldownRedisMember(key),
		)
		_, err := pipe.Exec(context.Background())
		observedRevision, revisionErr := revisionCommand.Result()
		if errors.Is(revisionErr, redis.Nil) {
			observedRevision = ""
			revisionErr = nil
		}
		if (err == nil || errors.Is(err, redis.Nil)) && revisionErr == nil &&
			observedRevision == controlRevision &&
			(untilCommand.Err() == nil || errors.Is(untilCommand.Err(), redis.Nil)) {
			until := int64(untilCommand.Val())
			if until <= now {
				until = 0
			}
			channelRateLimitCooldowns.Lock()
			local := channelRateLimitCooldowns.untilByRoute[key]
			if until > 0 {
				channelRateLimitCooldowns.untilByRoute[key] = channelRateLimitCooldownEntry{
					until: until, shared: true, revision: controlRevision,
				}
			} else if local.shared {
				delete(channelRateLimitCooldowns.untilByRoute, key)
			}
			if local.revision == controlRevision && local.until > now && !local.shared && local.until > until {
				until = local.until
			}
			channelRateLimitCooldowns.Unlock()
			return until
		}
	}

	channelRateLimitCooldowns.Lock()
	entry := channelRateLimitCooldowns.untilByRoute[key]
	if entry.revision != controlRevision || entry.until <= now {
		delete(channelRateLimitCooldowns.untilByRoute, key)
		entry.until = 0
	}
	channelRateLimitCooldowns.Unlock()
	return entry.until
}

func channelRateLimitCooldownChannelIds(modelName string, now int64) []int {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if modelName == "" {
		return nil
	}

	controlRevision := channelRateLimitCooldownControlRevision()
	sharedUntilById := make(map[int]int64)
	sharedLoaded := false
	if common.RedisEnabled && common.RDB != nil {
		pipe := common.RDB.TxPipeline()
		revisionCommand := pipe.Get(context.Background(), channelRateLimitCooldownRedisRevisionKey)
		pipe.ZRemRangeByScore(context.Background(), channelRateLimitCooldownRedisKey, "-inf", strconv.FormatInt(now, 10))
		active := pipe.ZRangeByScoreWithScores(
			context.Background(),
			channelRateLimitCooldownRedisKey,
			&redis.ZRangeBy{Min: "(" + strconv.FormatInt(now, 10), Max: "+inf"},
		)
		_, err := pipe.Exec(context.Background())
		observedRevision, revisionErr := revisionCommand.Result()
		if errors.Is(revisionErr, redis.Nil) {
			observedRevision = ""
			revisionErr = nil
		}
		if (err == nil || errors.Is(err, redis.Nil)) && revisionErr == nil &&
			observedRevision == controlRevision {
			sharedLoaded = true
			for _, item := range active.Val() {
				key, ok := parseChannelRateLimitCooldownRedisMember(item.Member)
				if ok && key.modelName == modelName {
					sharedUntilById[key.channelId] = int64(item.Score)
				}
			}
		}
	}

	channelRateLimitCooldowns.Lock()
	channelIdsById := make(map[int]struct{}, len(sharedUntilById))
	for key, entry := range channelRateLimitCooldowns.untilByRoute {
		if entry.revision != controlRevision || entry.until <= now {
			delete(channelRateLimitCooldowns.untilByRoute, key)
			continue
		}
		if key.modelName != modelName {
			continue
		}
		if sharedLoaded && entry.shared {
			if _, exists := sharedUntilById[key.channelId]; !exists {
				delete(channelRateLimitCooldowns.untilByRoute, key)
				continue
			}
		}
		channelIdsById[key.channelId] = struct{}{}
	}
	for channelId, until := range sharedUntilById {
		key := channelRateLimitCooldownKey{channelId: channelId, modelName: modelName}
		channelRateLimitCooldowns.untilByRoute[key] = channelRateLimitCooldownEntry{
			until: until, shared: true, revision: controlRevision,
		}
		channelIdsById[channelId] = struct{}{}
	}
	channelRateLimitCooldowns.Unlock()
	channelIds := make([]int, 0, len(channelIdsById))
	for channelId := range channelIdsById {
		channelIds = append(channelIds, channelId)
	}
	sort.Ints(channelIds)
	return channelIds
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
