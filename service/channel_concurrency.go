package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	MaxChannelRPMLimit                  = 100_000
	channelConcurrencyRedisConfigKey    = "channelConcurrency:v1:limits"
	channelConcurrencyRedisRPMConfigKey = "channelConcurrency:v1:rpm_limits"
	channelConcurrencyRedisRevisionKey  = "channelConcurrency:v1:revisions"
	channelConcurrencyRedisActivePrefix = "channelConcurrency:v1:active:"
	channelConcurrencyRedisRPMPrefix    = "channelConcurrency:v1:rpm:"
	channelConcurrencyRedisLoadedField  = "__loaded"
	channelConcurrencyLeaseTTL          = 2 * time.Minute
	channelConcurrencyRPMWindow         = time.Minute
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
  redis.call('DEL', KEYS[3])
end
for i = 2, #ARGV, 4 do
  local limit = tonumber(ARGV[i + 1]) or 0
  local rpm_limit = tonumber(ARGV[i + 2]) or 0
  local revision = tonumber(ARGV[i + 3]) or 0
  local current_revision = tonumber(redis.call('HGET', KEYS[3], ARGV[i]) or '-1')
  if revision >= current_revision then
    if limit > 0 then
      redis.call('HSET', KEYS[1], ARGV[i], limit)
    else
      redis.call('HDEL', KEYS[1], ARGV[i])
    end
    if rpm_limit > 0 then
      redis.call('HSET', KEYS[2], ARGV[i], rpm_limit)
    else
      redis.call('HDEL', KEYS[2], ARGV[i])
    end
    redis.call('HSET', KEYS[3], ARGV[i], revision)
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
local rpm_limit = tonumber(ARGV[4]) or 0
local revision = tonumber(ARGV[5]) or 0
local current_revision = tonumber(redis.call('HGET', KEYS[3], tostring(ARGV[2])) or '-1')
if revision < current_revision then
  return 2
end
if limit > 0 then
  redis.call('HSET', KEYS[1], ARGV[2], limit)
else
  redis.call('HDEL', KEYS[1], ARGV[2])
end
if rpm_limit > 0 then
  redis.call('HSET', KEYS[2], ARGV[2], rpm_limit)
else
  redis.call('HDEL', KEYS[2], ARGV[2])
end
redis.call('HSET', KEYS[3], ARGV[2], revision)
return 1
`

const channelConcurrencyRedisAcquireScript = `
if redis.call('HGET', KEYS[1], ARGV[1]) ~= '1' then
  return {-1, 0, 0, 0, 0}
end
local limit = tonumber(redis.call('HGET', KEYS[1], tostring(ARGV[2])) or '0')
local rpm_limit = tonumber(redis.call('HGET', KEYS[2], tostring(ARGV[2])) or '0')
local redis_time = redis.call('TIME')
local now = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)
local ttl = tonumber(ARGV[3])
local rpm_window = tonumber(ARGV[4])
local active_key = KEYS[3]
local rpm_key = ARGV[5] .. ARGV[2]
redis.call('ZREMRANGEBYSCORE', active_key, '-inf', now - ttl)
local active = redis.call('ZCARD', active_key)
redis.call('ZREMRANGEBYSCORE', rpm_key, '-inf', now - rpm_window)
local current_rpm = redis.call('ZCARD', rpm_key)
if limit > 0 and active >= limit then
  return {0, active, limit, current_rpm, rpm_limit}
end
if rpm_limit > 0 and current_rpm >= rpm_limit then
  return {0, active, limit, current_rpm, rpm_limit}
end
redis.call('ZADD', active_key, now, ARGV[6])
redis.call('PEXPIRE', active_key, ttl * 2)
if rpm_limit > 0 then
  redis.call('ZADD', rpm_key, now, ARGV[6])
  redis.call('PEXPIRE', rpm_key, rpm_window * 2)
end
return {1, active + 1, limit, current_rpm + (rpm_limit > 0 and 1 or 0), rpm_limit}
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
local rpm_window = tonumber(ARGV[4])
local rpm_prefix = ARGV[5]
local result = {}
for i = 6, #ARGV do
  local channel_id = tonumber(ARGV[i])
  local channel_id_string = ARGV[i]
  local limit = tonumber(redis.call('HGET', KEYS[1], channel_id_string) or '0') or 0
  local rpm_limit = tonumber(redis.call('HGET', KEYS[2], channel_id_string) or '0') or 0
  local active_key = prefix .. channel_id_string
  redis.call('ZREMRANGEBYSCORE', active_key, '-inf', now - ttl)
  local rpm_key = rpm_prefix .. channel_id_string
  redis.call('ZREMRANGEBYSCORE', rpm_key, '-inf', now - rpm_window)
  table.insert(result, channel_id)
  table.insert(result, redis.call('ZCARD', active_key))
  table.insert(result, limit)
  table.insert(result, redis.call('ZCARD', rpm_key))
  table.insert(result, rpm_limit)
end
return result
`

type ChannelConcurrencyStatus struct {
	Active     int `json:"active"`
	Limit      int `json:"limit"`
	CurrentRPM int `json:"current_rpm"`
	RPMLimit   int `json:"rpm_limit"`
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
	rpm        map[int][]int64
}{
	configs: make(map[int]model.ChannelConcurrencyConfig),
	active:  make(map[int]int),
	rpm:     make(map[int][]int64),
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
	return SaveChannelConcurrencyLimits(ctx, channelID, &limit, nil)
}

func SaveChannelConcurrencyLimits(ctx context.Context, channelID int, concurrencyLimit *int, rpmLimit *int) (model.ChannelRatioMonitor, error) {
	if channelID <= 0 {
		return model.ChannelRatioMonitor{}, errors.New("渠道 ID 必须大于 0")
	}
	if concurrencyLimit == nil && rpmLimit == nil {
		return model.ChannelRatioMonitor{}, errors.New("请至少提供渠道并发或 RPM 限制")
	}
	if concurrencyLimit != nil && (*concurrencyLimit < 0 || *concurrencyLimit > MaxChannelConcurrencyLimit) {
		return model.ChannelRatioMonitor{}, fmt.Errorf("渠道并发限制必须在 0 到 %d 之间", MaxChannelConcurrencyLimit)
	}
	if rpmLimit != nil && (*rpmLimit < 0 || *rpmLimit > MaxChannelRPMLimit) {
		return model.ChannelRatioMonitor{}, fmt.Errorf("渠道 RPM 限制必须在 0 到 %d 之间", MaxChannelRPMLimit)
	}
	monitor, err := model.SaveChannelConcurrencyLimits(channelID, concurrencyLimit, rpmLimit)
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
			Limit:    monitor.ConcurrencyLimit,
			RPMLimit: monitor.RPMLimit,
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
	if err = updateChannelConcurrencyRedisLimits(syncCtx, common.RDB, channelID, monitor.ConcurrencyLimit, monitor.RPMLimit, monitor.ConcurrencyRevision); err != nil {
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
		if common.RDB == nil {
			channelConcurrency.Lock()
			config := channelConcurrency.configs[channelID]
			channelConcurrency.Unlock()
			if config.Limit <= 0 && config.RPMLimit <= 0 {
				return acquireChannelConcurrencyLocal(channelID)
			}
		}
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
	return getChannelConcurrencySnapshot(ctx, nil)
}

// GetChannelConcurrencySnapshotForChannelIDs returns a snapshot for a caller
// that already loaded the channel list. Reusing those IDs avoids a second
// full channels query on aggregate monitor pages.
func GetChannelConcurrencySnapshotForChannelIDs(ctx context.Context, channelIDs []int) (map[int]ChannelConcurrencyStatus, error) {
	return getChannelConcurrencySnapshot(ctx, channelIDs)
}

func getChannelConcurrencySnapshot(ctx context.Context, providedChannelIDs []int) (map[int]ChannelConcurrencyStatus, error) {
	return getChannelConcurrencySnapshotAndConfigs(ctx, providedChannelIDs, nil)
}

// GetChannelConcurrencySnapshotWithRPM augments the live concurrency snapshot
// with the visible consume request count from the last minute.
func GetChannelConcurrencySnapshotWithRPM(ctx context.Context) (map[int]ChannelConcurrencyStatus, error) {
	return getChannelConcurrencySnapshotWithRPM(ctx, nil)
}

// GetChannelConcurrencySnapshotWithRPMForChannelIDs is the channel-list
// aware variant used by the monitor overview controller.
func GetChannelConcurrencySnapshotWithRPMForChannelIDs(ctx context.Context, channelIDs []int) (map[int]ChannelConcurrencyStatus, error) {
	return getChannelConcurrencySnapshotWithRPM(ctx, channelIDs)
}

// GetChannelConcurrencySnapshotWithRPMForChannelIDsAndConfigs returns a
// snapshot using the monitor rows already loaded by the caller.  Aggregate
// monitor responses fetch those rows for their own payload, so reusing the
// corresponding concurrency values avoids a second ChannelRatioMonitor query
// when the local configuration cache is cold or due for refresh.
func GetChannelConcurrencySnapshotWithRPMForChannelIDsAndConfigs(
	ctx context.Context,
	channelIDs []int,
	configs map[int]model.ChannelConcurrencyConfig,
) (map[int]ChannelConcurrencyStatus, error) {
	return getChannelConcurrencySnapshotWithRPMAndConfigs(ctx, channelIDs, configs)
}

func getChannelConcurrencySnapshotWithRPM(ctx context.Context, providedChannelIDs []int) (map[int]ChannelConcurrencyStatus, error) {
	return getChannelConcurrencySnapshotWithRPMAndConfigs(ctx, providedChannelIDs, nil)
}

func getChannelConcurrencySnapshotWithRPMAndConfigs(
	ctx context.Context,
	providedChannelIDs []int,
	providedConfigs map[int]model.ChannelConcurrencyConfig,
) (map[int]ChannelConcurrencyStatus, error) {
	snapshot, err := getChannelConcurrencySnapshotAndConfigs(ctx, providedChannelIDs, providedConfigs)
	if err != nil {
		return nil, err
	}
	currentRPM, err := model.GetChannelMonitorCurrentRPM(ctx, common.GetTimestamp()-60)
	if err != nil {
		return nil, err
	}
	for channelID, rpm := range currentRPM {
		status, exists := snapshot[channelID]
		if !exists {
			continue
		}
		status.CurrentRPM = rpm
		snapshot[channelID] = status
	}
	return snapshot, nil
}

func getChannelConcurrencySnapshotAndConfigs(
	ctx context.Context,
	providedChannelIDs []int,
	providedConfigs map[int]model.ChannelConcurrencyConfig,
) (map[int]ChannelConcurrencyStatus, error) {
	effectiveConfigs := providedConfigs
	if providedConfigs != nil {
		effectiveConfigs = copyChannelConcurrencyConfigs(providedConfigs)
		// The caller's monitor query is authoritative for this request.  Do not
		// blindly replace the shared cache: a concurrent settings write may
		// have advanced it after the caller loaded its rows.  Merge only rows
		// whose revisions are at least as new as the cached value.  Redis
		// revisions make stale initialization harmless, while local snapshots
		// use the caller's copy directly below.
		if changed := mergeProvidedChannelConcurrencyConfigs(effectiveConfigs); common.RedisEnabled && changed {
			if err := ensureChannelConcurrencyRedisConfig(ctx, common.RDB, effectiveConfigs); err != nil {
				return nil, err
			}
		}
	} else {
		refreshed, err := loadChannelConcurrencyLimits(false)
		if err != nil {
			return nil, err
		}
		effectiveConfigs = getChannelConcurrencyConfigsSnapshot()
		if common.RedisEnabled && refreshed {
			if err = ensureChannelConcurrencyRedisConfig(ctx, common.RDB, effectiveConfigs); err != nil {
				return nil, err
			}
		}
	}
	if common.RedisEnabled {
		channelIDs, err := getChannelConcurrencyChannelIDs(providedChannelIDs)
		if err != nil {
			return nil, err
		}
		return getChannelConcurrencyRedisSnapshot(ctx, common.RDB, channelIDs)
	}

	channelIDs, err := getChannelConcurrencyChannelIDs(providedChannelIDs)
	if err != nil {
		return nil, err
	}
	channelConcurrency.Lock()
	defer channelConcurrency.Unlock()
	snapshot := make(map[int]ChannelConcurrencyStatus, len(channelIDs))
	now := time.Now().UnixMilli()
	cutoff := now - channelConcurrencyRPMWindow.Milliseconds()
	for _, channelID := range channelIDs {
		config := effectiveConfigs[channelID]
		requests := channelConcurrency.rpm[channelID]
		first := 0
		for first < len(requests) && requests[first] <= cutoff {
			first++
		}
		if first > 0 {
			requests = requests[first:]
			channelConcurrency.rpm[channelID] = requests
		}
		currentRPM := len(requests)
		snapshot[channelID] = ChannelConcurrencyStatus{
			Active:     channelConcurrency.active[channelID],
			Limit:      config.Limit,
			CurrentRPM: currentRPM,
			RPMLimit:   config.RPMLimit,
		}
	}
	return snapshot, nil
}

func mergeProvidedChannelConcurrencyConfigs(configs map[int]model.ChannelConcurrencyConfig) bool {
	sourceDB := model.DB
	now := time.Now()
	channelConcurrency.Lock()
	defer channelConcurrency.Unlock()
	changed := !channelConcurrency.loaded || channelConcurrency.sourceDB != sourceDB
	merged := copyChannelConcurrencyConfigs(channelConcurrency.configs)
	if changed {
		merged = make(map[int]model.ChannelConcurrencyConfig, len(configs))
		channelConcurrency.active = make(map[int]int)
		channelConcurrency.rpm = make(map[int][]int64)
	}
	for channelID, config := range configs {
		current, exists := merged[channelID]
		if exists && config.Revision < current.Revision {
			continue
		}
		if !exists || current != config {
			merged[channelID] = config
			changed = true
		}
	}
	if !changed {
		return false
	}
	channelConcurrency.loaded = true
	channelConcurrency.sourceDB = sourceDB
	channelConcurrency.loadedAt = now
	channelConcurrency.configs = merged
	channelConcurrency.generation++
	return true
}

func getChannelConcurrencyChannelIDs(providedChannelIDs []int) ([]int, error) {
	ids := make(map[int]struct{}, len(providedChannelIDs))
	for _, channelID := range providedChannelIDs {
		if channelID > 0 {
			ids[channelID] = struct{}{}
		}
	}
	channelConcurrency.Lock()
	for channelID := range channelConcurrency.configs {
		ids[channelID] = struct{}{}
	}
	for channelID := range channelConcurrency.active {
		ids[channelID] = struct{}{}
	}
	channelConcurrency.Unlock()

	if model.DB != nil && providedChannelIDs == nil {
		channels, err := model.GetAllChannelsForMonitor()
		if err != nil {
			return nil, err
		}
		for _, channel := range channels {
			if channel != nil {
				ids[channel.Id] = struct{}{}
			}
		}
	}

	channelIDs := make([]int, 0, len(ids))
	for channelID := range ids {
		channelIDs = append(channelIDs, channelID)
	}
	sort.Ints(channelIDs)
	return channelIDs, nil
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
		channelConcurrency.rpm = make(map[int][]int64)
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
	config := channelConcurrency.configs[channelID]
	limit := config.Limit
	active := channelConcurrency.active[channelID]
	now := time.Now().UnixMilli()
	cutoff := now - channelConcurrencyRPMWindow.Milliseconds()
	requests := channelConcurrency.rpm[channelID]
	first := 0
	for first < len(requests) && requests[first] <= cutoff {
		first++
	}
	if first > 0 {
		requests = requests[first:]
	}
	channelConcurrency.rpm[channelID] = requests
	currentRPM := len(requests)
	status := ChannelConcurrencyStatus{Active: active, Limit: limit, CurrentRPM: currentRPM, RPMLimit: config.RPMLimit}
	if limit > 0 && active >= limit {
		channelConcurrency.Unlock()
		return nil, false, status, nil
	}
	if config.RPMLimit > 0 && currentRPM >= config.RPMLimit {
		channelConcurrency.Unlock()
		return nil, false, status, nil
	}
	channelConcurrency.active[channelID] = active + 1
	status.Active++
	if config.RPMLimit > 0 {
		channelConcurrency.rpm[channelID] = append(channelConcurrency.rpm[channelID], now)
		status.CurrentRPM++
	}
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
	args := make([]interface{}, 0, 1+len(configs)*4)
	args = append(args, channelConcurrencyRedisLoadedField)
	for channelID, config := range configs {
		args = append(args, channelID, config.Limit, config.RPMLimit, config.Revision)
	}
	return client.Eval(ctx, channelConcurrencyRedisInitScript, []string{channelConcurrencyRedisConfigKey, channelConcurrencyRedisRPMConfigKey, channelConcurrencyRedisRevisionKey}, args...).Err()
}

func updateChannelConcurrencyRedisLimit(ctx context.Context, client *redis.Client, channelID int, limit int, revision int64) error {
	channelConcurrency.Lock()
	rpmLimit := channelConcurrency.configs[channelID].RPMLimit
	channelConcurrency.Unlock()
	return updateChannelConcurrencyRedisLimits(ctx, client, channelID, limit, rpmLimit, revision)
}

func updateChannelConcurrencyRedisLimits(ctx context.Context, client *redis.Client, channelID int, limit int, rpmLimit int, revision int64) error {
	if client == nil {
		return errors.New("Redis 客户端未初始化")
	}
	updated, err := client.Eval(
		ctx,
		channelConcurrencyRedisUpdateScript,
		[]string{channelConcurrencyRedisConfigKey, channelConcurrencyRedisRPMConfigKey, channelConcurrencyRedisRevisionKey},
		channelConcurrencyRedisLoadedField,
		channelID,
		limit,
		rpmLimit,
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
		[]string{channelConcurrencyRedisConfigKey, channelConcurrencyRedisRPMConfigKey, channelConcurrencyRedisRevisionKey},
		channelConcurrencyRedisLoadedField,
		channelID,
		limit,
		rpmLimit,
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

	status := ChannelConcurrencyStatus{
		Active:     int(values[1]),
		Limit:      int(values[2]),
		CurrentRPM: int(values[3]),
		RPMLimit:   int(values[4]),
	}
	switch values[0] {
	case 0:
		return nil, false, status, nil
	case 1:
		return newChannelConcurrencyRedisLease(client, activeKey, member), true, status, nil
	default:
		return nil, false, ChannelConcurrencyStatus{}, fmt.Errorf("Redis 返回未知的渠道并发状态 %d", values[0])
	}
}

func takeChannelConcurrencyRedisLease(ctx context.Context, client *redis.Client, channelID int, activeKey string, member string) ([5]int64, error) {
	var values [5]int64
	reply, err := client.Eval(
		ctx,
		channelConcurrencyRedisAcquireScript,
		[]string{channelConcurrencyRedisConfigKey, channelConcurrencyRedisRPMConfigKey, activeKey},
		channelConcurrencyRedisLoadedField,
		channelID,
		channelConcurrencyLeaseTTL.Milliseconds(),
		channelConcurrencyRPMWindow.Milliseconds(),
		channelConcurrencyRedisRPMPrefix,
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
			return [5]int64{}, err
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

func getChannelConcurrencyRedisSnapshot(ctx context.Context, client *redis.Client, channelIDs []int) (map[int]ChannelConcurrencyStatus, error) {
	if client == nil {
		return nil, errors.New("Redis 客户端未初始化")
	}
	reply, err := queryChannelConcurrencyRedisSnapshot(ctx, client, channelIDs)
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
			reply, err = queryChannelConcurrencyRedisSnapshot(ctx, client, channelIDs)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(reply)%5 != 0 {
		return nil, fmt.Errorf("Redis 渠道并发快照长度无效: %d", len(reply))
	}

	snapshot := make(map[int]ChannelConcurrencyStatus, len(reply)/5)
	for index := 0; index < len(reply); index += 5 {
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
		currentRPM, parseErr := channelConcurrencyRedisInteger(reply[index+3])
		if parseErr != nil {
			return nil, parseErr
		}
		rpmLimit, parseErr := channelConcurrencyRedisInteger(reply[index+4])
		if parseErr != nil {
			return nil, parseErr
		}
		snapshot[int(channelID)] = ChannelConcurrencyStatus{
			Active:     int(active),
			Limit:      int(limit),
			CurrentRPM: int(currentRPM),
			RPMLimit:   int(rpmLimit),
		}
	}
	return snapshot, nil
}

func queryChannelConcurrencyRedisSnapshot(ctx context.Context, client *redis.Client, channelIDs []int) ([]interface{}, error) {
	args := make([]interface{}, 0, 5+len(channelIDs))
	args = append(args,
		channelConcurrencyRedisLoadedField,
		channelConcurrencyLeaseTTL.Milliseconds(),
		channelConcurrencyRedisActivePrefix,
		channelConcurrencyRPMWindow.Milliseconds(),
		channelConcurrencyRedisRPMPrefix,
	)
	for _, channelID := range channelIDs {
		args = append(args, channelID)
	}
	return client.Eval(
		ctx,
		channelConcurrencyRedisSnapshotScript,
		[]string{channelConcurrencyRedisConfigKey, channelConcurrencyRedisRPMConfigKey},
		args...,
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
