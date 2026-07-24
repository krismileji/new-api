package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	MaxChannelConcurrencyLimit          = 100_000
	channelConcurrencyRedisConfigKey    = "channelConcurrency:v1:limits"
	channelConcurrencyRedisRevisionKey  = "channelConcurrency:v1:revisions"
	channelConcurrencyRedisActivePrefix = "channelConcurrency:v1:active:"
	channelConcurrencyRedisLoadedField  = "__loaded"
	channelConcurrencyLeaseTTL          = 2 * time.Minute
	channelConcurrencyHeartbeatInterval = 30 * time.Second
	channelConcurrencyConfigRefresh     = time.Minute
	channelConcurrencyRedisLogInterval  = time.Minute
	channelConcurrencyRedisOpTimeout    = 5 * time.Second
)

const channelConcurrencyRedisInitScript = `
local initialized = redis.call('HGET', KEYS[1], ARGV[1]) == '1'
if not initialized then
  redis.call('DEL', KEYS[1])
  redis.call('DEL', KEYS[2])
end
for i = 2, #ARGV, 3 do
  local limit = tonumber(ARGV[i + 1]) or 0
  local revision = tonumber(ARGV[i + 2]) or 0
  local current_revision = tonumber(redis.call('HGET', KEYS[2], ARGV[i]) or '-1')
  if revision >= current_revision then
    if limit > 0 then
      redis.call('HSET', KEYS[1], ARGV[i], limit)
    else
      redis.call('HDEL', KEYS[1], ARGV[i])
    end
    redis.call('HSET', KEYS[2], ARGV[i], revision)
  end
end
redis.call('HSET', KEYS[1], ARGV[1], '1')
if initialized then
  return 0
end
return 1
`

const channelConcurrencyRedisUpdateScript = `
if redis.call('HGET', KEYS[1], ARGV[1]) ~= '1' then
  return 0
end
local limit = tonumber(ARGV[3]) or 0
local revision = tonumber(ARGV[4]) or 0
local current_revision = tonumber(redis.call('HGET', KEYS[2], ARGV[2]) or '-1')
if revision < current_revision then
  return 2
end
if limit > 0 then
  redis.call('HSET', KEYS[1], ARGV[2], limit)
else
  redis.call('HDEL', KEYS[1], ARGV[2])
end
redis.call('HSET', KEYS[2], ARGV[2], revision)
return 1
`

const channelConcurrencyRedisAcquireScript = `
if redis.call('HGET', KEYS[1], ARGV[1]) ~= '1' then
  return {-1, 0, 0}
end
local limit = tonumber(redis.call('HGET', KEYS[1], ARGV[2]) or '0')
if limit <= 0 then
  return {2, 0, 0}
end
local redis_time = redis.call('TIME')
local now = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)
local ttl = tonumber(ARGV[3])
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now - ttl)
local active = redis.call('ZCARD', KEYS[2])
if active >= limit then
  return {0, active, limit}
end
redis.call('ZADD', KEYS[2], now, ARGV[4])
redis.call('PEXPIRE', KEYS[2], ttl * 2)
return {1, active + 1, limit}
`

const channelConcurrencyRedisHeartbeatScript = `
if not redis.call('ZSCORE', KEYS[1], ARGV[1]) then
  return 0
end
local redis_time = redis.call('TIME')
local now = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)
redis.call('ZADD', KEYS[1], 'XX', now, ARGV[1])
redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[2]) * 2)
return 1
`

const channelConcurrencyRedisReleaseScript = `
redis.call('ZREM', KEYS[1], ARGV[1])
local active = redis.call('ZCARD', KEYS[1])
if active == 0 then
  redis.call('DEL', KEYS[1])
end
return active
`

const channelConcurrencyRedisSnapshotScript = `
if redis.call('HGET', KEYS[1], ARGV[1]) ~= '1' then
  return {-1}
end
local redis_time = redis.call('TIME')
local now = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)
local ttl = tonumber(ARGV[2])
local prefix = ARGV[3]
local config = redis.call('HGETALL', KEYS[1])
local result = {}
for i = 1, #config, 2 do
  local channel_id = tonumber(config[i])
  local limit = tonumber(config[i + 1])
  if channel_id and limit and limit > 0 then
    local active_key = prefix .. config[i]
    redis.call('ZREMRANGEBYSCORE', active_key, '-inf', now - ttl)
    table.insert(result, channel_id)
    table.insert(result, redis.call('ZCARD', active_key))
    table.insert(result, limit)
  end
end
return result
`

type ChannelConcurrencyStatus struct {
	Active int `json:"active"`
	Limit  int `json:"limit"`
}

type ChannelConcurrencyLease struct {
	once    sync.Once
	release func()
}

func (lease *ChannelConcurrencyLease) Release() {
	if lease == nil {
		return
	}
	lease.once.Do(func() {
		if lease.release != nil {
			lease.release()
		}
	})
}

var channelConcurrency = struct {
	sync.Mutex
	loaded     bool
	sourceDB   *gorm.DB
	generation uint64
	loadedAt   time.Time
	configs    map[int]model.ChannelConcurrencyConfig
	active     map[int]int
}{
	configs: make(map[int]model.ChannelConcurrencyConfig),
	active:  make(map[int]int),
}

var channelConcurrencyReload sync.Mutex

var channelConcurrencyRedisLogs = struct {
	sync.Mutex
	last map[string]time.Time
}{
	last: make(map[string]time.Time),
}

func ReloadChannelConcurrencyLimits(ctx context.Context) error {
	if _, err := loadChannelConcurrencyLimits(true); err != nil {
		return err
	}
	if !common.RedisEnabled {
		return nil
	}
	return ensureChannelConcurrencyRedisConfig(ctx, common.RDB, getChannelConcurrencyConfigsSnapshot())
}

func SaveChannelConcurrencyLimit(ctx context.Context, channelID int, limit int) (model.ChannelRatioMonitor, error) {
	if channelID <= 0 {
		return model.ChannelRatioMonitor{}, errors.New("渠道 ID 必须大于 0")
	}
	if limit < 0 || limit > MaxChannelConcurrencyLimit {
		return model.ChannelRatioMonitor{}, fmt.Errorf("渠道并发限制必须在 0 到 %d 之间", MaxChannelConcurrencyLimit)
	}
	monitor, err := model.SaveChannelConcurrencyLimit(channelID, limit)
	if err != nil {
		return model.ChannelRatioMonitor{}, err
	}
	if _, err = loadChannelConcurrencyLimits(false); err != nil {
		return monitor, err
	}

	channelConcurrency.Lock()
	current := channelConcurrency.configs[channelID]
	if monitor.ConcurrencyRevision >= current.Revision {
		channelConcurrency.configs[channelID] = model.ChannelConcurrencyConfig{
			Limit:    limit,
			Revision: monitor.ConcurrencyRevision,
		}
		channelConcurrency.generation++
		channelConcurrency.loadedAt = time.Now()
	}
	channelConcurrency.Unlock()

	if !common.RedisEnabled {
		return monitor, nil
	}
	syncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), channelConcurrencyRedisOpTimeout)
	defer cancel()
	if err = updateChannelConcurrencyRedisLimit(syncCtx, common.RDB, channelID, limit, monitor.ConcurrencyRevision); err != nil {
		return monitor, fmt.Errorf("同步渠道并发限制到 Redis 失败: %w", err)
	}
	return monitor, nil
}

func AcquireChannelConcurrency(ctx context.Context, channelID int) (*ChannelConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
	if channelID <= 0 {
		return &ChannelConcurrencyLease{}, true, ChannelConcurrencyStatus{}, nil
	}
	refreshed, err := loadChannelConcurrencyLimits(false)
	if err != nil {
		return nil, false, ChannelConcurrencyStatus{}, err
	}
	if common.RedisEnabled {
		if refreshed {
			if err = ensureChannelConcurrencyRedisConfig(ctx, common.RDB, getChannelConcurrencyConfigsSnapshot()); err != nil {
				return nil, false, ChannelConcurrencyStatus{}, err
			}
		}
		return acquireChannelConcurrencyRedis(ctx, common.RDB, channelID)
	}
	return acquireChannelConcurrencyLocal(channelID)
}

func GetChannelConcurrencySnapshot(ctx context.Context) (map[int]ChannelConcurrencyStatus, error) {
	refreshed, err := loadChannelConcurrencyLimits(false)
	if err != nil {
		return nil, err
	}
	if common.RedisEnabled {
		if refreshed {
			if err = ensureChannelConcurrencyRedisConfig(ctx, common.RDB, getChannelConcurrencyConfigsSnapshot()); err != nil {
				return nil, err
			}
		}
		return getChannelConcurrencyRedisSnapshot(ctx, common.RDB)
	}

	channelConcurrency.Lock()
	defer channelConcurrency.Unlock()
	snapshot := make(map[int]ChannelConcurrencyStatus, len(channelConcurrency.configs))
	for channelID, config := range channelConcurrency.configs {
		limit := config.Limit
		if limit <= 0 {
			continue
		}
		snapshot[channelID] = ChannelConcurrencyStatus{
			Active: channelConcurrency.active[channelID],
			Limit:  limit,
		}
	}
	return snapshot, nil
}

func loadChannelConcurrencyLimits(force bool) (bool, error) {
	sourceDB := model.DB
	now := time.Now()
	channelConcurrency.Lock()
	if !force && channelConcurrency.loaded && channelConcurrency.sourceDB == sourceDB && now.Sub(channelConcurrency.loadedAt) < channelConcurrencyConfigRefresh {
		channelConcurrency.Unlock()
		return false, nil
	}
	channelConcurrency.Unlock()

	channelConcurrencyReload.Lock()
	defer channelConcurrencyReload.Unlock()

	sourceDB = model.DB
	now = time.Now()
	channelConcurrency.Lock()
	if !force && channelConcurrency.loaded && channelConcurrency.sourceDB == sourceDB && now.Sub(channelConcurrency.loadedAt) < channelConcurrencyConfigRefresh {
		channelConcurrency.Unlock()
		return false, nil
	}
	generation := channelConcurrency.generation
	channelConcurrency.Unlock()

	configs, err := model.GetChannelConcurrencyConfigs()
	if err != nil {
		return false, err
	}

	channelConcurrency.Lock()
	if channelConcurrency.sourceDB == sourceDB && channelConcurrency.generation != generation {
		channelConcurrency.Unlock()
		return false, nil
	}
	if channelConcurrency.sourceDB != sourceDB {
		channelConcurrency.active = make(map[int]int)
	}
	channelConcurrency.loaded = true
	channelConcurrency.sourceDB = sourceDB
	channelConcurrency.loadedAt = now
	channelConcurrency.configs = configs
	channelConcurrency.generation++
	channelConcurrency.Unlock()
	return true, nil
}

func getChannelConcurrencyConfigsSnapshot() map[int]model.ChannelConcurrencyConfig {
	channelConcurrency.Lock()
	defer channelConcurrency.Unlock()
	return copyChannelConcurrencyConfigs(channelConcurrency.configs)
}

func copyChannelConcurrencyConfigs(configs map[int]model.ChannelConcurrencyConfig) map[int]model.ChannelConcurrencyConfig {
	result := make(map[int]model.ChannelConcurrencyConfig, len(configs))
	for channelID, config := range configs {
		result[channelID] = config
	}
	return result
}

func acquireChannelConcurrencyLocal(channelID int) (*ChannelConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
	channelConcurrency.Lock()
	limit := channelConcurrency.configs[channelID].Limit
	if limit <= 0 {
		channelConcurrency.Unlock()
		return &ChannelConcurrencyLease{}, true, ChannelConcurrencyStatus{}, nil
	}

	active := channelConcurrency.active[channelID]
	status := ChannelConcurrencyStatus{Active: active, Limit: limit}
	if active >= limit {
		channelConcurrency.Unlock()
		return nil, false, status, nil
	}
	channelConcurrency.active[channelID] = active + 1
	status.Active++
	channelConcurrency.Unlock()

	lease := &ChannelConcurrencyLease{release: func() {
		channelConcurrency.Lock()
		defer channelConcurrency.Unlock()
		current := channelConcurrency.active[channelID]
		if current <= 1 {
			delete(channelConcurrency.active, channelID)
			return
		}
		channelConcurrency.active[channelID] = current - 1
	}}
	return lease, true, status, nil
}

func ensureChannelConcurrencyRedisConfig(ctx context.Context, client *redis.Client, configs map[int]model.ChannelConcurrencyConfig) error {
	if client == nil {
		return errors.New("Redis 客户端未初始化")
	}
	args := make([]interface{}, 0, 1+len(configs)*3)
	args = append(args, channelConcurrencyRedisLoadedField)
	for channelID, config := range configs {
		args = append(args, channelID, config.Limit, config.Revision)
	}
	return client.Eval(ctx, channelConcurrencyRedisInitScript, []string{channelConcurrencyRedisConfigKey, channelConcurrencyRedisRevisionKey}, args...).Err()
}

func updateChannelConcurrencyRedisLimit(ctx context.Context, client *redis.Client, channelID int, limit int, revision int64) error {
	if client == nil {
		return errors.New("Redis 客户端未初始化")
	}
	updated, err := client.Eval(
		ctx,
		channelConcurrencyRedisUpdateScript,
		[]string{channelConcurrencyRedisConfigKey, channelConcurrencyRedisRevisionKey},
		channelConcurrencyRedisLoadedField,
		channelID,
		limit,
		revision,
	).Int()
	if err != nil {
		return err
	}
	if updated == 1 || updated == 2 {
		return nil
	}

	if _, err := loadChannelConcurrencyLimits(true); err != nil {
		return err
	}
	if err = ensureChannelConcurrencyRedisConfig(ctx, client, getChannelConcurrencyConfigsSnapshot()); err != nil {
		return err
	}
	updated, err = client.Eval(
		ctx,
		channelConcurrencyRedisUpdateScript,
		[]string{channelConcurrencyRedisConfigKey, channelConcurrencyRedisRevisionKey},
		channelConcurrencyRedisLoadedField,
		channelID,
		limit,
		revision,
	).Int()
	if err != nil {
		return err
	}
	if updated != 1 && updated != 2 {
		return errors.New("Redis 渠道并发配置尚未初始化")
	}
	return nil
}

func acquireChannelConcurrencyRedis(ctx context.Context, client *redis.Client, channelID int) (*ChannelConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
	if client == nil {
		return nil, false, ChannelConcurrencyStatus{}, errors.New("Redis 客户端未初始化")
	}
	member := fmt.Sprintf("%s:%s", common.NodeName, uuid.NewString())
	activeKey := channelConcurrencyRedisActivePrefix + strconv.Itoa(channelID)
	values, err := takeChannelConcurrencyRedisLease(ctx, client, channelID, activeKey, member)
	if err != nil {
		return nil, false, ChannelConcurrencyStatus{}, err
	}
	if values[0] == -1 {
		if _, err = loadChannelConcurrencyLimits(true); err != nil {
			return nil, false, ChannelConcurrencyStatus{}, err
		}
		if err = ensureChannelConcurrencyRedisConfig(ctx, client, getChannelConcurrencyConfigsSnapshot()); err != nil {
			return nil, false, ChannelConcurrencyStatus{}, err
		}
		values, err = takeChannelConcurrencyRedisLease(ctx, client, channelID, activeKey, member)
		if err != nil {
			return nil, false, ChannelConcurrencyStatus{}, err
		}
	}

	status := ChannelConcurrencyStatus{Active: int(values[1]), Limit: int(values[2])}
	switch values[0] {
	case 0:
		return nil, false, status, nil
	case 1:
		return newChannelConcurrencyRedisLease(client, activeKey, member), true, status, nil
	case 2:
		return &ChannelConcurrencyLease{}, true, ChannelConcurrencyStatus{}, nil
	default:
		return nil, false, ChannelConcurrencyStatus{}, fmt.Errorf("Redis 返回未知的渠道并发状态 %d", values[0])
	}
}

func takeChannelConcurrencyRedisLease(ctx context.Context, client *redis.Client, channelID int, activeKey string, member string) ([3]int64, error) {
	var values [3]int64
	reply, err := client.Eval(
		ctx,
		channelConcurrencyRedisAcquireScript,
		[]string{channelConcurrencyRedisConfigKey, activeKey},
		channelConcurrencyRedisLoadedField,
		channelID,
		channelConcurrencyLeaseTTL.Milliseconds(),
		member,
	).Slice()
	if err != nil {
		return values, err
	}
	if len(reply) != len(values) {
		return values, fmt.Errorf("Redis 渠道并发响应长度无效: %d", len(reply))
	}
	for index := range values {
		values[index], err = channelConcurrencyRedisInteger(reply[index])
		if err != nil {
			return [3]int64{}, err
		}
	}
	return values, nil
}

func newChannelConcurrencyRedisLease(client *redis.Client, activeKey string, member string) *ChannelConcurrencyLease {
	heartbeatCtx, stopHeartbeat := context.WithCancel(context.Background())
	lease := &ChannelConcurrencyLease{release: func() {
		stopHeartbeat()
		ctx, cancel := context.WithTimeout(context.Background(), channelConcurrencyRedisOpTimeout)
		defer cancel()
		if err := client.Eval(ctx, channelConcurrencyRedisReleaseScript, []string{activeKey}, member).Err(); err != nil {
			if shouldLogChannelConcurrencyRedisIssue("release", activeKey) {
				logger.LogError(context.Background(), fmt.Sprintf("释放渠道并发租约失败（%s）: %v", activeKey, err))
			}
		}
	}}

	go func() {
		ticker := time.NewTicker(channelConcurrencyHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), channelConcurrencyRedisOpTimeout)
				refreshed, err := client.Eval(
					ctx,
					channelConcurrencyRedisHeartbeatScript,
					[]string{activeKey},
					member,
					channelConcurrencyLeaseTTL.Milliseconds(),
				).Int()
				cancel()
				if err != nil {
					if shouldLogChannelConcurrencyRedisIssue("heartbeat", activeKey) {
						logger.LogError(context.Background(), fmt.Sprintf("续期渠道并发租约失败（%s）: %v", activeKey, err))
					}
					continue
				}
				if refreshed != 1 {
					if shouldLogChannelConcurrencyRedisIssue("expired", activeKey) {
						logger.LogWarn(context.Background(), fmt.Sprintf("渠道并发租约已过期（%s），停止续期", activeKey))
					}
					return
				}
			}
		}
	}()
	return lease
}

func shouldLogChannelConcurrencyRedisIssue(kind string, activeKey string) bool {
	now := time.Now()
	key := kind + ":" + activeKey
	channelConcurrencyRedisLogs.Lock()
	defer channelConcurrencyRedisLogs.Unlock()
	if last := channelConcurrencyRedisLogs.last[key]; !last.IsZero() && now.Sub(last) < channelConcurrencyRedisLogInterval {
		return false
	}
	channelConcurrencyRedisLogs.last[key] = now
	return true
}

func getChannelConcurrencyRedisSnapshot(ctx context.Context, client *redis.Client) (map[int]ChannelConcurrencyStatus, error) {
	if client == nil {
		return nil, errors.New("Redis 客户端未初始化")
	}
	reply, err := queryChannelConcurrencyRedisSnapshot(ctx, client)
	if err != nil {
		return nil, err
	}
	if len(reply) == 1 {
		value, parseErr := channelConcurrencyRedisInteger(reply[0])
		if parseErr == nil && value == -1 {
			if _, err = loadChannelConcurrencyLimits(true); err != nil {
				return nil, err
			}
			if err = ensureChannelConcurrencyRedisConfig(ctx, client, getChannelConcurrencyConfigsSnapshot()); err != nil {
				return nil, err
			}
			reply, err = queryChannelConcurrencyRedisSnapshot(ctx, client)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(reply)%3 != 0 {
		return nil, fmt.Errorf("Redis 渠道并发快照长度无效: %d", len(reply))
	}

	snapshot := make(map[int]ChannelConcurrencyStatus, len(reply)/3)
	for index := 0; index < len(reply); index += 3 {
		channelID, parseErr := channelConcurrencyRedisInteger(reply[index])
		if parseErr != nil {
			return nil, parseErr
		}
		active, parseErr := channelConcurrencyRedisInteger(reply[index+1])
		if parseErr != nil {
			return nil, parseErr
		}
		limit, parseErr := channelConcurrencyRedisInteger(reply[index+2])
		if parseErr != nil {
			return nil, parseErr
		}
		snapshot[int(channelID)] = ChannelConcurrencyStatus{Active: int(active), Limit: int(limit)}
	}
	return snapshot, nil
}

func queryChannelConcurrencyRedisSnapshot(ctx context.Context, client *redis.Client) ([]interface{}, error) {
	return client.Eval(
		ctx,
		channelConcurrencyRedisSnapshotScript,
		[]string{channelConcurrencyRedisConfigKey},
		channelConcurrencyRedisLoadedField,
		channelConcurrencyLeaseTTL.Milliseconds(),
		channelConcurrencyRedisActivePrefix,
	).Slice()
}

func channelConcurrencyRedisInteger(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("Redis 渠道并发整数类型无效: %T", value)
	}
}
