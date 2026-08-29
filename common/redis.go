package common

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

var RDB *redis.Client

// RDBMonitorWrite, RDBMonitorRead and RDBMonitorConsumer are deliberately
// separate clients so monitor traffic cannot consume the user-request pool.
// They are initialized from the same Redis URL as RDB with role-specific pool
// sizes. The role getters below fall back to RDB for tests and older startup
// paths that only initialize the legacy client.
var (
	RDBMonitorWrite    *redis.Client
	RDBMonitorRead     *redis.Client
	RDBMonitorConsumer *redis.Client
)
var RedisEnabled = true

// RedisClientRole identifies one independently observable Redis connection
// pool. User remains the legacy/default client used by application paths.
type RedisClientRole string

const (
	RedisClientRoleUser            RedisClientRole = "user"
	RedisClientRoleMonitorWrite    RedisClientRole = "monitor_write"
	RedisClientRoleMonitorRead     RedisClientRole = "monitor_read"
	RedisClientRoleMonitorConsumer RedisClientRole = "monitor_consumer"
)

const (
	RedisClientPoolIsolationModeUnavailable = "unavailable"
	RedisClientPoolIsolationModeIsolated    = "isolated"
	RedisClientPoolIsolationModeShared      = "shared"
	RedisClientPoolIsolationModeMixed       = "mixed"
)

const (
	RedisClientPoolDegradedReasonUnavailable     = "redis_unavailable"
	RedisClientPoolDegradedReasonPoolCongested   = "pool_congested"
	RedisClientPoolDegradedReasonPoolTimeout     = "pool_timeout"
	RedisClientPoolDegradedReasonContextDeadline = "context_deadline"
)

// RedisClientPoolStats is a point-in-time snapshot of one role's go-redis
// connection pool. The counters are cumulative since process start (as
// provided by redis.PoolStats); callers should derive rates when graphing.
type RedisClientPoolStats struct {
	Role                      RedisClientRole `json:"role"`
	PoolSize                  int             `json:"pool_size"`
	TotalConns                uint32          `json:"total_conns"`
	IdleConns                 uint32          `json:"idle_conns"`
	InUse                     uint32          `json:"in_use"`
	PoolCongested             bool            `json:"pool_congested"`
	DegradedReason            string          `json:"degraded_reason"`
	Shared                    bool            `json:"shared"`
	SharedWith                RedisClientRole `json:"shared_with,omitempty"`
	StaleConns                uint32          `json:"stale_conns"`
	Hits                      uint32          `json:"hits"`
	Misses                    uint32          `json:"misses"`
	Timeouts                  uint32          `json:"timeouts"`
	ContextDeadlineCount      uint64          `json:"context_deadline_count"`
	PoolTimeoutCount          uint64          `json:"pool_timeout_count"`
	CommandCount              uint64          `json:"command_count"`
	CommandErrorCount         uint64          `json:"command_error_count"`
	CommandLatencyTotalMicros uint64          `json:"command_latency_total_micros"`
	CommandLatencyMaxMicros   uint64          `json:"command_latency_max_micros"`
	Unavailable               bool            `json:"unavailable"`
}

const (
	defaultRedisMonitorWritePoolSize    = 4
	defaultRedisMonitorReadPoolSize     = 8
	defaultRedisMonitorConsumerPoolSize = 4
)

var redisClientPoolSizes struct {
	user            int
	monitorWrite    int
	monitorRead     int
	monitorConsumer int
}

type redisClientCommandMetrics struct {
	commandCount              atomic.Uint64
	commandErrorCount         atomic.Uint64
	contextDeadlineCount      atomic.Uint64
	poolTimeoutCount          atomic.Uint64
	commandLatencyTotalMicros atomic.Uint64
	commandLatencyMaxMicros   atomic.Uint64
}

var redisClientCommandMetricsState struct {
	user            *redisClientCommandMetrics
	monitorWrite    *redisClientCommandMetrics
	monitorRead     *redisClientCommandMetrics
	monitorConsumer *redisClientCommandMetrics
}

type redisClientCommandStartContextKey struct{}

type redisClientCommandMetricsHook struct {
	metrics *redisClientCommandMetrics
}

var _ redis.Hook = (*redisClientCommandMetricsHook)(nil)

func (hook *redisClientCommandMetricsHook) BeforeProcess(ctx context.Context, _ redis.Cmder) (context.Context, error) {
	if hook == nil || hook.metrics == nil {
		return ctx, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, redisClientCommandStartContextKey{}, time.Now()), nil
}

func (hook *redisClientCommandMetricsHook) AfterProcess(ctx context.Context, cmd redis.Cmder) error {
	if hook == nil || hook.metrics == nil {
		return nil
	}
	var cmdErr error
	if cmd != nil {
		cmdErr = cmd.Err()
	}
	hook.metrics.record(ctx, cmdErr)
	return nil
}

func (hook *redisClientCommandMetricsHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return hook.BeforeProcess(ctx, nil)
}

func (hook *redisClientCommandMetricsHook) AfterProcessPipeline(ctx context.Context, cmds []redis.Cmder) error {
	if hook == nil || hook.metrics == nil {
		return nil
	}
	if len(cmds) == 0 {
		return nil
	}
	start, ok := redisClientCommandStart(ctx)
	if !ok {
		return nil
	}
	elapsed := time.Since(start)
	if elapsed < 0 {
		elapsed = 0
	}
	micros := uint64(elapsed / time.Microsecond)
	hook.metrics.commandCount.Add(uint64(len(cmds)))
	hook.metrics.commandLatencyTotalMicros.Add(micros)
	hook.metrics.updateMax(micros)
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		hook.metrics.recordError(cmd.Err())
	}
	return nil
}

func redisClientCommandStart(ctx context.Context) (time.Time, bool) {
	if ctx == nil {
		return time.Time{}, false
	}
	start, ok := ctx.Value(redisClientCommandStartContextKey{}).(time.Time)
	return start, ok
}

func (metrics *redisClientCommandMetrics) record(ctx context.Context, err error) {
	if metrics == nil {
		return
	}
	start, ok := redisClientCommandStart(ctx)
	if !ok {
		return
	}
	elapsed := time.Since(start)
	if elapsed < 0 {
		elapsed = 0
	}
	micros := uint64(elapsed / time.Microsecond)
	metrics.commandCount.Add(1)
	metrics.commandLatencyTotalMicros.Add(micros)
	metrics.updateMax(micros)
	metrics.recordError(err)
}

func (metrics *redisClientCommandMetrics) recordError(err error) {
	if metrics == nil || err == nil {
		return
	}
	metrics.commandErrorCount.Add(1)
	if errors.Is(err, context.DeadlineExceeded) {
		metrics.contextDeadlineCount.Add(1)
	}
	if strings.Contains(strings.ToLower(err.Error()), "connection pool timeout") {
		metrics.poolTimeoutCount.Add(1)
	}
}

func (metrics *redisClientCommandMetrics) updateMax(value uint64) {
	if metrics == nil {
		return
	}
	for current := metrics.commandLatencyMaxMicros.Load(); value > current; {
		if metrics.commandLatencyMaxMicros.CompareAndSwap(current, value) {
			return
		}
		current = metrics.commandLatencyMaxMicros.Load()
	}
}

func RedisKeyCacheSeconds() int {
	return SyncFrequency
}

// InitRedisClient This function is called after init()
func InitRedisClient() (err error) {
	if os.Getenv("REDIS_CONN_STRING") == "" {
		RedisEnabled = false
		SysLog("REDIS_CONN_STRING not set, Redis is not enabled")
		return nil
	}
	if os.Getenv("SYNC_FREQUENCY") == "" {
		SysLog("SYNC_FREQUENCY not set, use default value 60")
		SyncFrequency = 60
	}
	SysLog("Redis is enabled")
	opt, err := redis.ParseURL(os.Getenv("REDIS_CONN_STRING"))
	if err != nil {
		FatalLog("failed to parse Redis connection string: " + err.Error())
	}
	opt.PoolSize = GetEnvOrDefault("REDIS_POOL_SIZE", 10)
	if opt.PoolSize <= 0 {
		opt.PoolSize = 10
	}
	redisClientPoolSizes.user = opt.PoolSize
	redisClientCommandMetricsState.user = &redisClientCommandMetrics{}
	RDB = newRedisClientWithMetrics(opt, redisClientCommandMetricsState.user)

	// Monitor traffic is intentionally isolated by default. Set the flag to
	// false only as an emergency rollback; role getters then use RDB exactly as
	// older releases did. Pool sizes are independently tunable for load tests.
	if GetEnvOrDefaultBool("REDIS_CLIENT_POOL_ISOLATION", true) {
		redisClientCommandMetricsState.monitorWrite = &redisClientCommandMetrics{}
		redisClientCommandMetricsState.monitorRead = &redisClientCommandMetrics{}
		redisClientCommandMetricsState.monitorConsumer = &redisClientCommandMetrics{}
		RDBMonitorWrite = newRedisRoleClient(opt, "REDIS_MONITOR_WRITE_POOL_SIZE", defaultRedisMonitorWritePoolSize, redisClientCommandMetricsState.monitorWrite)
		RDBMonitorRead = newRedisRoleClient(opt, "REDIS_MONITOR_READ_POOL_SIZE", defaultRedisMonitorReadPoolSize, redisClientCommandMetricsState.monitorRead)
		RDBMonitorConsumer = newRedisRoleClient(opt, "REDIS_MONITOR_CONSUMER_POOL_SIZE", defaultRedisMonitorConsumerPoolSize, redisClientCommandMetricsState.monitorConsumer)
		redisClientPoolSizes.monitorWrite = redisRolePoolSize("REDIS_MONITOR_WRITE_POOL_SIZE", "", defaultRedisMonitorWritePoolSize)
		redisClientPoolSizes.monitorRead = redisRolePoolSize("REDIS_MONITOR_READ_POOL_SIZE", "", defaultRedisMonitorReadPoolSize)
		redisClientPoolSizes.monitorConsumer = redisRolePoolSize("REDIS_MONITOR_CONSUMER_POOL_SIZE", "REDIS_CONSUMER_POOL_SIZE", defaultRedisMonitorConsumerPoolSize)
	} else {
		RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer = nil, nil, nil
		redisClientCommandMetricsState.monitorWrite = nil
		redisClientCommandMetricsState.monitorRead = nil
		redisClientCommandMetricsState.monitorConsumer = nil
		redisClientPoolSizes.monitorWrite = opt.PoolSize
		redisClientPoolSizes.monitorRead = opt.PoolSize
		redisClientPoolSizes.monitorConsumer = opt.PoolSize
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = RDB.Ping(ctx).Result()
	if err != nil {
		FatalLog("Redis ping test failed: " + err.Error())
	}
	for role, client := range map[RedisClientRole]*redis.Client{
		RedisClientRoleMonitorWrite:    RDBMonitorWrite,
		RedisClientRoleMonitorRead:     RDBMonitorRead,
		RedisClientRoleMonitorConsumer: RDBMonitorConsumer,
	} {
		if client == nil {
			continue
		}
		if _, pingErr := client.Ping(ctx).Result(); pingErr != nil {
			FatalLog(fmt.Sprintf("Redis %s pool ping test failed: %s", role, pingErr))
		}
	}
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis connected to %s", opt.Addr))
		SysLog(fmt.Sprintf("Redis database: %d", opt.DB))
	}
	return err
}

func newRedisClientWithMetrics(options *redis.Options, metrics *redisClientCommandMetrics) *redis.Client {
	client := redis.NewClient(options)
	if metrics != nil {
		client.AddHook(&redisClientCommandMetricsHook{metrics: metrics})
	}
	return client
}

func newRedisRoleClient(base *redis.Options, poolEnv string, defaultPoolSize int, metrics *redisClientCommandMetrics) *redis.Client {
	options := *base
	fallbackEnv := ""
	if poolEnv == "REDIS_MONITOR_CONSUMER_POOL_SIZE" {
		fallbackEnv = "REDIS_CONSUMER_POOL_SIZE"
	}
	options.PoolSize = redisRolePoolSize(poolEnv, fallbackEnv, defaultPoolSize)
	return newRedisClientWithMetrics(&options, metrics)
}

func redisRolePoolSize(primaryEnv, fallbackEnv string, defaultPoolSize int) int {
	poolSize := GetEnvOrDefault(primaryEnv, 0)
	if poolSize <= 0 && fallbackEnv != "" {
		poolSize = GetEnvOrDefault(fallbackEnv, 0)
	}
	if poolSize <= 0 {
		poolSize = defaultPoolSize
	}
	return poolSize
}

// RedisMonitorWriteClient returns the pool reserved for monitor event and
// projection writes. It falls back to the legacy client for compatibility with
// tests and callers that initialize only RDB.
func RedisMonitorWriteClient() *redis.Client {
	if RDBMonitorWrite != nil {
		return RDBMonitorWrite
	}
	return RDB
}

// RedisMonitorReadClient returns the pool reserved for monitor page/query
// reads and route-health observations.
func RedisMonitorReadClient() *redis.Client {
	if RDBMonitorRead != nil {
		return RDBMonitorRead
	}
	return RDB
}

// RedisMonitorConsumerClient returns the pool reserved for the Stream
// consumer and its projection/lease writes.
func RedisMonitorConsumerClient() *redis.Client {
	if RDBMonitorConsumer != nil {
		return RDBMonitorConsumer
	}
	return RDB
}

// RedisClientPoolIsolationEnabled reports whether monitor roles currently use
// independently allocated clients. Pointer identity is used instead of an
// environment lookup so tests and runtime client swaps are reflected in the
// status response as well.
func RedisClientPoolIsolationEnabled() bool {
	return RedisClientPoolIsolationMode() == RedisClientPoolIsolationModeIsolated
}

// RedisClientPoolIsolationMode reports the effective relationship between the
// user pool and monitor pools. A shared mode is returned when all monitor
// getters resolve to RDB (the rollback/legacy configuration); mixed covers
// partial runtime setup so status consumers do not mistake it for isolation.
func RedisClientPoolIsolationMode() string {
	if RDB == nil {
		return RedisClientPoolIsolationModeUnavailable
	}
	monitorClients := []*redis.Client{
		RedisMonitorWriteClient(),
		RedisMonitorReadClient(),
		RedisMonitorConsumerClient(),
	}
	allShared := true
	seen := map[*redis.Client]struct{}{RDB: {}}
	for _, client := range monitorClients {
		if client == nil {
			return RedisClientPoolIsolationModeMixed
		}
		if client != RDB {
			allShared = false
		}
		if _, exists := seen[client]; exists {
			if client != RDB {
				return RedisClientPoolIsolationModeMixed
			}
			continue
		}
		seen[client] = struct{}{}
	}
	if allShared {
		return RedisClientPoolIsolationModeShared
	}
	if len(seen) == 4 {
		return RedisClientPoolIsolationModeIsolated
	}
	return RedisClientPoolIsolationModeMixed
}

// CloseRedisClients closes all role clients. RDB is included for symmetry and
// to make shutdown deterministic; callers may safely invoke it more than once.
func CloseRedisClients() error {
	clients := []*redis.Client{RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer, RDB}
	seen := make(map[*redis.Client]struct{}, len(clients))
	var closeErrors []error
	for _, client := range clients {
		if client == nil {
			continue
		}
		if _, ok := seen[client]; ok {
			continue
		}
		seen[client] = struct{}{}
		if err := client.Close(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	RDB, RDBMonitorWrite, RDBMonitorRead, RDBMonitorConsumer = nil, nil, nil, nil
	redisClientCommandMetricsState.user = nil
	redisClientCommandMetricsState.monitorWrite = nil
	redisClientCommandMetricsState.monitorRead = nil
	redisClientCommandMetricsState.monitorConsumer = nil
	return errors.Join(closeErrors...)
}

// GetRedisClientPoolStats reports independently named role pool metrics for
// dashboards and diagnostics. Missing role clients are marked unavailable.
func GetRedisClientPoolStats() map[RedisClientRole]RedisClientPoolStats {
	monitorWriteMetrics := redisClientCommandMetricsState.monitorWrite
	monitorReadMetrics := redisClientCommandMetricsState.monitorRead
	monitorConsumerMetrics := redisClientCommandMetricsState.monitorConsumer
	if monitorWriteMetrics == nil && RedisMonitorWriteClient() == RDB {
		monitorWriteMetrics = redisClientCommandMetricsState.user
	}
	if monitorReadMetrics == nil && RedisMonitorReadClient() == RDB {
		monitorReadMetrics = redisClientCommandMetricsState.user
	}
	if monitorConsumerMetrics == nil && RedisMonitorConsumerClient() == RDB {
		monitorConsumerMetrics = redisClientCommandMetricsState.user
	}
	clients := map[RedisClientRole]*redis.Client{
		RedisClientRoleUser:            RDB,
		RedisClientRoleMonitorWrite:    RedisMonitorWriteClient(),
		RedisClientRoleMonitorRead:     RedisMonitorReadClient(),
		RedisClientRoleMonitorConsumer: RedisMonitorConsumerClient(),
	}
	result := make(map[RedisClientRole]RedisClientPoolStats, len(clients))
	result[RedisClientRoleUser] = redisClientPoolStats(
		RedisClientRoleUser, clients[RedisClientRoleUser], redisClientPoolSizes.user, redisClientCommandMetricsState.user,
	)
	result[RedisClientRoleMonitorWrite] = redisClientPoolStats(
		RedisClientRoleMonitorWrite, clients[RedisClientRoleMonitorWrite], redisClientPoolSizes.monitorWrite, monitorWriteMetrics,
	)
	result[RedisClientRoleMonitorRead] = redisClientPoolStats(
		RedisClientRoleMonitorRead, clients[RedisClientRoleMonitorRead], redisClientPoolSizes.monitorRead, monitorReadMetrics,
	)
	result[RedisClientRoleMonitorConsumer] = redisClientPoolStats(
		RedisClientRoleMonitorConsumer, clients[RedisClientRoleMonitorConsumer], redisClientPoolSizes.monitorConsumer, monitorConsumerMetrics,
	)
	for role, stats := range result {
		if role == RedisClientRoleUser || clients[role] == nil {
			continue
		}
		for _, sharedRole := range []RedisClientRole{
			RedisClientRoleUser,
			RedisClientRoleMonitorWrite,
			RedisClientRoleMonitorRead,
			RedisClientRoleMonitorConsumer,
		} {
			if sharedRole == role || clients[sharedRole] == nil || clients[sharedRole] != clients[role] {
				continue
			}
			stats.Shared = true
			stats.SharedWith = sharedRole
			break
		}
		result[role] = stats
	}
	return result
}

func redisClientPoolStats(role RedisClientRole, client *redis.Client, poolSize int, metrics *redisClientCommandMetrics) RedisClientPoolStats {
	stats := RedisClientPoolStats{Role: role, PoolSize: poolSize, Unavailable: client == nil}
	if client == nil {
		stats.DegradedReason = RedisClientPoolDegradedReasonUnavailable
		return stats
	}
	pool := client.PoolStats()
	if pool == nil {
		stats.Unavailable = true
		stats.DegradedReason = RedisClientPoolDegradedReasonUnavailable
		return stats
	}
	if stats.PoolSize <= 0 {
		if options := client.Options(); options != nil {
			stats.PoolSize = options.PoolSize
		}
	}
	stats.TotalConns = pool.TotalConns
	stats.IdleConns = pool.IdleConns
	if pool.TotalConns >= pool.IdleConns {
		stats.InUse = pool.TotalConns - pool.IdleConns
	}
	if stats.PoolSize > 0 {
		stats.PoolCongested = stats.InUse >= uint32(stats.PoolSize)
	}
	stats.StaleConns = pool.StaleConns
	stats.Hits = pool.Hits
	stats.Misses = pool.Misses
	stats.Timeouts = pool.Timeouts
	stats.PoolTimeoutCount = uint64(pool.Timeouts)
	if metrics != nil {
		stats.ContextDeadlineCount = metrics.contextDeadlineCount.Load()
		if poolTimeoutCount := metrics.poolTimeoutCount.Load(); poolTimeoutCount > stats.PoolTimeoutCount {
			stats.PoolTimeoutCount = poolTimeoutCount
		}
		stats.CommandCount = metrics.commandCount.Load()
		stats.CommandErrorCount = metrics.commandErrorCount.Load()
		stats.CommandLatencyTotalMicros = metrics.commandLatencyTotalMicros.Load()
		stats.CommandLatencyMaxMicros = metrics.commandLatencyMaxMicros.Load()
	}
	if stats.PoolCongested {
		stats.DegradedReason = RedisClientPoolDegradedReasonPoolCongested
	} else if stats.PoolTimeoutCount > 0 {
		stats.DegradedReason = RedisClientPoolDegradedReasonPoolTimeout
	} else if stats.ContextDeadlineCount > 0 {
		stats.DegradedReason = RedisClientPoolDegradedReasonContextDeadline
	}
	return stats
}

func ParseRedisOption() *redis.Options {
	opt, err := redis.ParseURL(os.Getenv("REDIS_CONN_STRING"))
	if err != nil {
		FatalLog("failed to parse Redis connection string: " + err.Error())
	}
	return opt
}

func RedisSet(key string, value string, expiration time.Duration) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis SET: key=%s, value=%s, expiration=%v", key, value, expiration))
	}
	ctx := context.Background()
	return RDB.Set(ctx, key, value, expiration).Err()
}

func RedisGet(key string) (string, error) {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis GET: key=%s", key))
	}
	ctx := context.Background()
	val, err := RDB.Get(ctx, key).Result()
	return val, err
}

//func RedisExpire(key string, expiration time.Duration) error {
//	ctx := context.Background()
//	return RDB.Expire(ctx, key, expiration).Err()
//}
//
//func RedisGetEx(key string, expiration time.Duration) (string, error) {
//	ctx := context.Background()
//	return RDB.GetSet(ctx, key, expiration).Result()
//}

func RedisDel(key string) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis DEL: key=%s", key))
	}
	ctx := context.Background()
	return RDB.Del(ctx, key).Err()
}

func RedisDelKey(key string) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis DEL Key: key=%s", key))
	}
	ctx := context.Background()
	return RDB.Del(ctx, key).Err()
}

func RedisHSetObj(key string, obj interface{}, expiration time.Duration) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis HSET: key=%s, obj=%+v, expiration=%v", key, obj, expiration))
	}
	ctx := context.Background()

	data := make(map[string]interface{})

	// 使用反射遍历结构体字段
	v := reflect.ValueOf(obj).Elem()
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		// Skip DeletedAt field
		if field.Type.String() == "gorm.DeletedAt" {
			continue
		}

		// 处理指针类型
		if value.Kind() == reflect.Ptr {
			if value.IsNil() {
				data[field.Name] = ""
				continue
			}
			value = value.Elem()
		}

		// 处理布尔类型
		if value.Kind() == reflect.Bool {
			data[field.Name] = strconv.FormatBool(value.Bool())
			continue
		}

		// 其他类型直接转换为字符串
		data[field.Name] = fmt.Sprintf("%v", value.Interface())
	}

	txn := RDB.TxPipeline()
	txn.HSet(ctx, key, data)

	// 只有在 expiration 大于 0 时才设置过期时间
	if expiration > 0 {
		txn.Expire(ctx, key, expiration)
	}

	_, err := txn.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to execute transaction: %w", err)
	}
	return nil
}

func RedisHGetObj(key string, obj interface{}) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis HGETALL: key=%s", key))
	}
	ctx := context.Background()

	result, err := RDB.HGetAll(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to load hash from Redis: %w", err)
	}

	if len(result) == 0 {
		return fmt.Errorf("key %s not found in Redis", key)
	}

	// Handle both pointer and non-pointer values
	val := reflect.ValueOf(obj)
	if val.Kind() != reflect.Ptr {
		return fmt.Errorf("obj must be a pointer to a struct, got %T", obj)
	}

	v := val.Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("obj must be a pointer to a struct, got pointer to %T", v.Interface())
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		fieldName := field.Name
		if value, ok := result[fieldName]; ok {
			fieldValue := v.Field(i)

			// Handle pointer types
			if fieldValue.Kind() == reflect.Ptr {
				if value == "" {
					continue
				}
				if fieldValue.IsNil() {
					fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
				}
				fieldValue = fieldValue.Elem()
			}

			// Enhanced type handling for Token struct
			switch fieldValue.Kind() {
			case reflect.String:
				fieldValue.SetString(value)
			case reflect.Int, reflect.Int64:
				intValue, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return fmt.Errorf("failed to parse int field %s: %w", fieldName, err)
				}
				fieldValue.SetInt(intValue)
			case reflect.Bool:
				boolValue, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("failed to parse bool field %s: %w", fieldName, err)
				}
				fieldValue.SetBool(boolValue)
			case reflect.Struct:
				// Special handling for gorm.DeletedAt
				if fieldValue.Type().String() == "gorm.DeletedAt" {
					if value != "" {
						timeValue, err := time.Parse(time.RFC3339, value)
						if err != nil {
							return fmt.Errorf("failed to parse DeletedAt field %s: %w", fieldName, err)
						}
						fieldValue.Set(reflect.ValueOf(gorm.DeletedAt{Time: timeValue, Valid: true}))
					}
				}
			default:
				return fmt.Errorf("unsupported field type: %s for field %s", fieldValue.Kind(), fieldName)
			}
		}
	}

	return nil
}

// RedisIncr Add this function to handle atomic increments
func RedisIncr(key string, delta int64) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis INCR: key=%s, delta=%d", key, delta))
	}
	// 检查键的剩余生存时间
	ttlCmd := RDB.TTL(context.Background(), key)
	ttl, err := ttlCmd.Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("failed to get TTL: %w", err)
	}

	// 只有在 key 存在且有 TTL 时才需要特殊处理
	if ttl > 0 {
		ctx := context.Background()
		// 开始一个Redis事务
		txn := RDB.TxPipeline()

		// 减少余额
		decrCmd := txn.IncrBy(ctx, key, delta)
		if err := decrCmd.Err(); err != nil {
			return err // 如果减少失败，则直接返回错误
		}

		// 重新设置过期时间，使用原来的过期时间
		txn.Expire(ctx, key, ttl)

		// 执行事务
		_, err = txn.Exec(ctx)
		return err
	}
	return nil
}

func RedisHIncrBy(key, field string, delta int64) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis HINCRBY: key=%s, field=%s, delta=%d", key, field, delta))
	}
	ttlCmd := RDB.TTL(context.Background(), key)
	ttl, err := ttlCmd.Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("failed to get TTL: %w", err)
	}

	if ttl > 0 {
		ctx := context.Background()
		txn := RDB.TxPipeline()

		incrCmd := txn.HIncrBy(ctx, key, field, delta)
		if err := incrCmd.Err(); err != nil {
			return err
		}

		txn.Expire(ctx, key, ttl)

		_, err = txn.Exec(ctx)
		return err
	}
	return nil
}

func RedisHSetField(key, field string, value interface{}) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis HSET field: key=%s, field=%s, value=%v", key, field, value))
	}
	ttlCmd := RDB.TTL(context.Background(), key)
	ttl, err := ttlCmd.Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("failed to get TTL: %w", err)
	}

	if ttl > 0 {
		ctx := context.Background()
		txn := RDB.TxPipeline()

		hsetCmd := txn.HSet(ctx, key, field, value)
		if err := hsetCmd.Err(); err != nil {
			return err
		}

		txn.Expire(ctx, key, ttl)

		_, err = txn.Exec(ctx)
		return err
	}
	return nil
}
