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

// StartChannelRateLimitCooldown temporarily defers one upstream channel/model
// route while another candidate is available. It remains a final routing
// fallback. Repeated 429 responses may extend, but never shorten, an active
// cooldown.
func StartChannelRateLimitCooldown(channelId int, modelName string, durationSeconds int) {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if channelId <= 0 || modelName == "" || durationSeconds <= 0 {
		return
	}
	until := common.GetTimestamp() + int64(durationSeconds)
	key := channelRateLimitCooldownKey{channelId: channelId, modelName: modelName}
	revision := channelRateLimitCooldownControlRevision()

	channelRateLimitCooldowns.Lock()
	if current := channelRateLimitCooldowns.untilByRoute[key]; current.revision != revision || current.until < until {
		channelRateLimitCooldowns.untilByRoute[key] = channelRateLimitCooldownEntry{
			until: until, revision: revision,
		}
		publishChannelRateLimitCooldownSnapshotLocked()
	}
	channelRateLimitCooldowns.Unlock()

	ensureChannelRateLimitCooldownRedisSync()
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
	if channelRateLimitCooldownControlRevision() != revision {
		return
	}
	channelRateLimitCooldowns.Lock()
	current := channelRateLimitCooldowns.untilByRoute[key]
	if current.revision != revision {
		channelRateLimitCooldowns.Unlock()
		return
	}
	if sharedUntil > current.until {
		current.until = sharedUntil
	}
	current.shared = true
	current.revision = revision
	channelRateLimitCooldowns.untilByRoute[key] = current
	publishChannelRateLimitCooldownSnapshotLocked()
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
) (accepted bool) {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	expectedControlRevision = strings.TrimSpace(expectedControlRevision)
	if channelId <= 0 || modelName == "" || durationSeconds <= 0 {
		return false
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
		common.SysError("校验 429 冷却配置修订号失败: " + err.Error())
		return false
	}
	if currentRevision != expectedControlRevision {
		return false
	}
	now := common.GetTimestamp()
	until := now + int64(durationSeconds)
	if current := channelRateLimitCooldowns.untilByRoute[key]; current.revision != expectedControlRevision || current.until < until {
		channelRateLimitCooldowns.untilByRoute[key] = channelRateLimitCooldownEntry{
			until: until, revision: expectedControlRevision,
		}
		localChanged = true
		publishChannelRateLimitCooldownSnapshotLocked()
	}
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

	current := channelRateLimitCooldowns.untilByRoute[key]
	if current.revision != expectedControlRevision || current.until < until {
		channelRateLimitCooldowns.untilByRoute[key] = channelRateLimitCooldownEntry{
			until: until, shared: shared, revision: expectedControlRevision,
		}
	} else if shared && until >= current.until {
		current.shared = true
		channelRateLimitCooldowns.untilByRoute[key] = current
	}
	publishChannelRateLimitCooldownSnapshotLocked()
	accepted = true
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
	publishChannelRateLimitCooldownSnapshotLocked()
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
	publishChannelRateLimitCooldownSnapshotLocked()
	channelRateLimitCooldowns.Unlock()
	if common.RedisEnabled && common.RDB != nil {
		if err := common.RDB.Del(
			context.Background(), channelRateLimitCooldownRedisKey, channelRateLimitCooldownRedisRevisionKey,
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

func channelRateLimitCooldownChannelIds(modelName string, now int64) []int {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if modelName == "" {
		return nil
	}

	ensureChannelRateLimitCooldownRedisSync()
	controlRevision := channelRateLimitCooldownControlRevision()
	snapshot := loadChannelRateLimitCooldownSnapshot()
	channelIds := make([]int, 0)
	for key, entry := range snapshot.untilByRoute {
		if entry.revision == controlRevision && entry.until > now && key.modelName == modelName {
			channelIds = append(channelIds, key.channelId)
		}
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
