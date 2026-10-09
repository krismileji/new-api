package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

const channelMonitorDailyReadSourceField = "meta:read_source"

type channelMonitorDailyRecoveryScope struct {
	db     *gorm.DB
	client *redis.Client
	kind   string
	day    int64
}

func (scope channelMonitorDailyRecoveryScope) key() string {
	if scope.kind == "cost" {
		return ChannelMonitorRedisCostDayKey(scope.day)
	}
	return ChannelMonitorRedisSuccessDayKey(scope.day)
}

type channelMonitorDailyRecoverySession struct {
	fields      map[string]string
	err         error
	retryAt     time.Time
	rebuilding  bool
	rebuildDone chan struct{}
	cancel      context.CancelFunc
}

type channelMonitorDailyRecoveryManager struct {
	mu       sync.Mutex
	sessions map[channelMonitorDailyRecoveryScope]*channelMonitorDailyRecoverySession
	loads    singleflight.Group
}

var channelMonitorDailyReadRecovery = &channelMonitorDailyRecoveryManager{
	sessions: make(map[channelMonitorDailyRecoveryScope]*channelMonitorDailyRecoverySession),
}

// All daily readers share a recovery baseline, independent of their page,
// filters or selected hash fields. It is discarded once a live hash is readable.
func readChannelMonitorDailyHashWithRecovery(ctx context.Context, client *redis.Client, kind string, day int64, patterns []string, maxFields int) (map[string]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	recoveryClient := client
	if common.RedisEnabled && common.RedisMonitorConsumerClient() != nil {
		recoveryClient = common.RedisMonitorConsumerClient()
	}
	scope := channelMonitorDailyRecoveryScope{db: model.DB, client: recoveryClient, kind: kind, day: day}
	var values map[string]string
	err := ErrChannelMonitorRedisSharedProjectionUnavailable
	if client != nil {
		readCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisSharedOperationTimeout)
		values, err = readChannelMonitorRedisDailyHash(readCtx, client, scope.key(), patterns, maxFields)
		cancel()
		if err == nil && kind == "cost" && values[channelMonitorReliableCostVersionField] != "1" {
			err = ErrChannelMonitorRedisSharedProjectionUnavailable
		}
	}
	if err == nil {
		channelMonitorDailyReadRecovery.finish(scope)
		return values, nil
	}
	if ctx.Err() != nil || scope.db == nil || errors.Is(err, ErrChannelMonitorRedisSharedProjectionLimitExceeded) {
		return nil, err
	}
	result := channelMonitorDailyReadRecovery.loads.DoChan(fmt.Sprintf("%p:%p:%s:%d", scope.db, scope.client, kind, day), func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), channelMonitorRedisSharedOperationTimeout)
		defer cancel()
		if scope.client == nil && !common.RedisEnabled {
			return loadChannelMonitorDailyRecoveryBaseline(loadCtx, scope, maxFields)
		}
		return channelMonitorDailyReadRecovery.baseline(loadCtx, scope, maxFields)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case loaded := <-result:
		if loaded.Err != nil {
			return nil, errors.Join(err, loaded.Err)
		}
		values = loaded.Val.(map[string]string)
	}
	if len(values) > maxFields {
		return nil, &ChannelMonitorRedisSharedProjectionLimitError{Resource: "hash_fields", Limit: int64(maxFields), Actual: int64(len(values))}
	}
	if channelMonitorDailyReadSource(values) == "database_daily" {
		channelMonitorDailyReadRecovery.rebuild(scope)
	} else {
		channelMonitorDailyReadRecovery.finish(scope)
	}
	selected := make(map[string]string)
	for field, value := range values {
		if len(patterns) == 0 {
			selected[field] = value
			continue
		}
		for _, pattern := range patterns {
			matched, matchErr := path.Match(pattern, field)
			if matchErr != nil {
				return nil, matchErr
			}
			if matched {
				selected[field] = value
				break
			}
		}
	}
	return selected, nil
}

func (manager *channelMonitorDailyRecoveryManager) baseline(ctx context.Context, scope channelMonitorDailyRecoveryScope, maxFields int) (map[string]string, error) {
	manager.mu.Lock()
	session := manager.sessions[scope]
	if session != nil && (session.fields != nil || time.Now().Before(session.retryAt)) {
		manager.mu.Unlock()
		return session.fields, session.err
	}
	manager.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	fields, err := loadChannelMonitorDailyRecoveryBaseline(ctx, scope, maxFields)
	manager.mu.Lock()
	defer manager.mu.Unlock()
	oldestDay := model.ChannelDailyCostDayStart(time.Now().Unix()) - 86400
	for key, previous := range manager.sessions {
		if key.db != scope.db || key.client != scope.client || key.day < oldestDay {
			if previous.cancel != nil {
				previous.cancel()
			}
			delete(manager.sessions, key)
		}
	}
	manager.sessions[scope] = &channelMonitorDailyRecoverySession{fields: fields, err: err, retryAt: time.Now().Add(5 * time.Second)}
	return fields, err
}

func readChannelMonitorDailyRecoverySnapshot(ctx context.Context, scope channelMonitorDailyRecoveryScope, maxFields int) (map[string]string, error) {
	fields, err := readChannelMonitorRedisDailyHash(ctx, scope.client, scope.key()+":recovery:baseline", nil, maxFields)
	if err == nil && fields[channelMonitorDailyReadSourceField] == "database_daily" {
		return fields, nil
	}
	if errors.Is(err, ErrChannelMonitorRedisSharedProjectionLimitExceeded) || ctx.Err() != nil {
		return nil, err
	}
	fields, err = readChannelMonitorRedisDailyHash(ctx, scope.client, scope.key(), nil, maxFields)
	if err == nil && scope.kind == "cost" && fields[channelMonitorReliableCostVersionField] != "1" {
		return nil, ErrChannelMonitorRedisSharedProjectionUnavailable
	}
	return fields, err
}

func loadChannelMonitorDailyRecoveryBaseline(ctx context.Context, scope channelMonitorDailyRecoveryScope, maxFields int) (map[string]string, error) {
	baselineKey := scope.key() + ":recovery:baseline"
	leaseKey := scope.key() + ":recovery:lease"
	token := common.GetUUID()
	shared := false
	previousRevision := ""
	redisAvailable := false
	if scope.client != nil {
		probeCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		redisAvailable = scope.client.Ping(probeCtx).Err() == nil
		cancel()
	}
	if redisAvailable {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			fields, err := readChannelMonitorDailyRecoverySnapshot(ctx, scope, maxFields)
			if err == nil {
				return fields, nil
			}
			if errors.Is(err, ErrChannelMonitorRedisSharedProjectionLimitExceeded) || ctx.Err() != nil {
				return nil, err
			}
			acquired, leaseErr := scope.client.SetNX(ctx, leaseKey, token, 30*time.Second).Result()
			if leaseErr != nil || acquired {
				shared = leaseErr == nil
				break
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ticker.C:
			}
		}
		if shared {
			defer func() {
				releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				defer cancel()
				_ = scope.client.Eval(releaseCtx, channelMonitorRedisLeaseReleaseScript, []string{leaseKey}, token).Err()
			}()
			// The previous holder may have published between our read and SETNX.
			fields, err := readChannelMonitorDailyRecoverySnapshot(ctx, scope, maxFields)
			if err == nil {
				return fields, nil
			}
			if errors.Is(err, ErrChannelMonitorRedisSharedProjectionLimitExceeded) || ctx.Err() != nil {
				return nil, err
			}
			previousRevision, err = scope.client.HGet(ctx, scope.key(), "meta:revision").Result()
			if err != nil && !errors.Is(err, redis.Nil) {
				shared = false
			}
		}
	}
	var fields map[string]string
	options := &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
	if scope.db.Dialector.Name() == "sqlite" {
		options = &sql.TxOptions{ReadOnly: true}
	}
	snapshotAt := time.Now().Unix()
	err := scope.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		var err error
		if scope.kind == "cost" {
			if !db.Migrator().HasTable(&model.ChannelDailyCost{}) {
				return ErrChannelMonitorRedisSharedProjectionUnavailable
			}
			fields, _, err = loadChannelMonitorRedisDailyCostFields(ctx, scope.day, db)
		} else {
			fields, _, err = loadChannelMonitorDailyMetricsDatabaseFields(ctx, db, scope.day)
		}
		return err
	}, options)
	if err != nil {
		return nil, err
	}
	if fields == nil {
		fields = make(map[string]string)
	}
	fields[channelMonitorDailyReadSourceField] = "database_daily"
	fields["meta:coverage_partial"] = "1"
	fields["meta:processed_at"] = strconv.FormatInt(snapshotAt, 10)
	fields["meta:database_snapshot_at"] = strconv.FormatInt(snapshotAt, 10)
	delete(fields, "meta:legacy_through")
	if scope.kind == "cost" {
		fields[channelMonitorReliableCostVersionField] = "1"
		fields["meta:data_cutoff_at"] = strconv.FormatInt(snapshotAt, 10)
	}
	if len(fields) > maxFields {
		return nil, &ChannelMonitorRedisSharedProjectionLimitError{Resource: "hash_fields", Limit: int64(maxFields), Actual: int64(len(fields))}
	}
	if shared {
		publishErr := scope.client.Watch(ctx, func(tx *redis.Tx) error {
			owner, err := tx.Get(ctx, leaseKey).Result()
			if err != nil || owner != token {
				return ErrChannelMonitorRedisAggregatorLeaseLost
			}
			version, err := tx.HGet(ctx, scope.key(), "meta:revision").Result()
			if err != nil && !errors.Is(err, redis.Nil) {
				return err
			}
			if version != previousRevision {
				return nil // Another worker already published the live snapshot.
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Del(ctx, baselineKey)
				pipe.HSet(ctx, baselineKey, fields)
				pipe.Expire(ctx, baselineKey, channelMonitorRedisSharedSuccessDayTTL)
				return nil
			})
			return err
		}, leaseKey, scope.key())
		if publishErr != nil {
			common.SysError("渠道监控初始化基线共享失败: " + publishErr.Error())
		}
	}
	return fields, nil
}

func (manager *channelMonitorDailyRecoveryManager) rebuild(scope channelMonitorDailyRecoveryScope) {
	if scope.client == nil {
		return
	}
	manager.mu.Lock()
	session := manager.sessions[scope]
	if session == nil || model.DB != scope.db || session.rebuilding || session.cancel != nil && time.Now().Before(session.retryAt) {
		manager.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	done := make(chan struct{})
	session.cancel, session.rebuilding, session.rebuildDone = cancel, true, done
	manager.mu.Unlock()
	go func() {
		defer cancel()
		defer close(done)
		var err error
		if scope.kind == "cost" {
			err = rebuildChannelMonitorReliableDailyCosts(ctx, scope.client, scope.day)
		} else {
			err = rebuildChannelMonitorDailyMetrics(ctx, scope.client, scope.day)
		}
		manager.mu.Lock()
		session.rebuilding, session.retryAt = false, time.Now().Add(5*time.Second)
		manager.mu.Unlock()
		if err != nil && !errors.Is(err, context.Canceled) {
			common.SysError("渠道监控日统计恢复失败，将重试: " + err.Error())
		}
	}()
}

func (manager *channelMonitorDailyRecoveryManager) finish(scope channelMonitorDailyRecoveryScope) {
	manager.mu.Lock()
	session := manager.sessions[scope]
	delete(manager.sessions, scope)
	if session != nil && session.cancel != nil {
		session.cancel()
	}
	manager.mu.Unlock()
}

func channelMonitorDailyReadSource(fields map[string]string) string {
	if fields[channelMonitorDailyReadSourceField] == "database_daily" {
		return "database_daily"
	}
	return "redis_daily"
}
