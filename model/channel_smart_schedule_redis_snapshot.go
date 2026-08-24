package model

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

const (
	channelSmartScheduleRouteSnapshotSchemaVersion = 1
	channelSmartScheduleRouteSnapshotKeyPrefix     = "channel_smart_schedule:v1:route_snapshot"
	channelSmartScheduleRouteSnapshotPointerKey    = channelSmartScheduleRouteSnapshotKeyPrefix + ":current"
	channelSmartScheduleRouteSnapshotRevisionKey   = channelSmartScheduleRouteSnapshotKeyPrefix + ":revision"
	channelSmartScheduleRouteSnapshotWatermarkKey  = channelSmartScheduleRouteSnapshotKeyPrefix + ":source_watermark"
	channelSmartScheduleRouteSnapshotGeneratedKey  = channelSmartScheduleRouteSnapshotKeyPrefix + ":generated_at"
	channelSmartScheduleRouteSnapshotLeaseKey      = channelSmartScheduleRouteSnapshotKeyPrefix + ":lease"

	channelSmartScheduleRouteSnapshotTTL          = 7 * 24 * time.Hour
	channelSmartScheduleRouteSnapshotTemporaryTTL = time.Minute
	channelSmartScheduleRouteSnapshotLeaseTTL     = time.Minute
	channelSmartScheduleRouteSnapshotIOTimeout    = 2 * time.Second
	channelSmartScheduleRouteSnapshotBuildTimeout = 30 * time.Second
	channelSmartScheduleRouteSnapshotPollInterval = time.Second
	channelSmartScheduleRouteSnapshotMaxAge       = 5 * time.Minute
)

var (
	ErrChannelSmartScheduleRouteSnapshotUnavailable = errors.New("智能调度路由快照不可用")
	ErrChannelSmartScheduleRouteSnapshotInvalid     = errors.New("智能调度路由快照无效")
)

type channelSmartScheduleRouteSnapshot struct {
	SchemaVersion   int                                                             `json:"schema_version"`
	Revision        int64                                                           `json:"revision"`
	GeneratedAt     int64                                                           `json:"generated_at"`
	SourceWatermark int64                                                           `json:"source_watermark"`
	Routes          map[string]map[string][]channelSmartScheduleCachedRouteSnapshot `json:"routes"`
	LogicalRuntime  *LogicalChannelRuntimeSnapshot                                  `json:"logical_runtime"`
	LogicalRouting  []channelLogicalSmartScheduleRoutingSnapshot                    `json:"logical_routing"`
	Checksum        string                                                          `json:"checksum"`

	localDirtyGeneration         uint64
	localDirtyGenerationCaptured bool
	fromRedis                    bool
}

type channelSmartScheduleCachedRouteSnapshot struct {
	ChannelID                       int                                         `json:"channel_id"`
	LogicalChannelID                int64                                       `json:"logical_channel_id,omitempty"`
	LogicalRevision                 int64                                       `json:"logical_revision,omitempty"`
	LogicalMembers                  []channelSmartScheduleLogicalMemberSnapshot `json:"logical_members,omitempty"`
	LogicalPriority                 int64                                       `json:"logical_priority,omitempty"`
	LogicalWeight                   uint                                        `json:"logical_weight,omitempty"`
	LogicalOfficialPriority         int64                                       `json:"logical_official_priority,omitempty"`
	LogicalOfficialWeight           uint                                        `json:"logical_official_weight,omitempty"`
	Priority                        int64                                       `json:"priority"`
	Weight                          uint                                        `json:"weight"`
	OfficialPriority                int64                                       `json:"official_priority"`
	OfficialWeight                  uint                                        `json:"official_weight"`
	OfficialInherited               bool                                        `json:"official_inherited"`
	Participates                    bool                                        `json:"participates"`
	TrafficPausedUntil              int64                                       `json:"traffic_paused_until,omitempty"`
	TemporaryTrafficKind            string                                      `json:"temporary_traffic_kind,omitempty"`
	TemporaryTrafficSince           int64                                       `json:"temporary_traffic_since,omitempty"`
	StabilityState                  string                                      `json:"stability_state,omitempty"`
	StabilitySince                  int64                                       `json:"stability_since,omitempty"`
	ExplorationMaxPromptTokens      int                                         `json:"exploration_max_prompt_tokens,omitempty"`
	StabilityReleaseMaxPromptTokens int                                         `json:"stability_release_max_prompt_tokens,omitempty"`
}

type channelSmartScheduleLogicalMemberSnapshot struct {
	ChannelID int  `json:"channel_id"`
	Weight    uint `json:"weight"`
}

type channelLogicalSmartScheduleRoutingSnapshot struct {
	LogicalID                    int64  `json:"logical_id"`
	LogicalRevision              int64  `json:"logical_revision"`
	Group                        string `json:"group"`
	Model                        string `json:"model"`
	Priority                     int64  `json:"priority"`
	Weight                       uint   `json:"weight"`
	Participates                 bool   `json:"participates"`
	TemporaryTrafficKind         string `json:"temporary_traffic_kind,omitempty"`
	TemporaryTrafficSince        int64  `json:"temporary_traffic_since,omitempty"`
	StabilityState               string `json:"stability_state,omitempty"`
	StabilitySince               int64  `json:"stability_since,omitempty"`
	ExplorationMaxPromptTokens   int    `json:"exploration_max_prompt_tokens,omitempty"`
	StabilityReleasePromptTokens int    `json:"stability_release_max_prompt_tokens,omitempty"`
}

type channelSmartScheduleLocalSnapshotMetadata struct {
	Revision        int64
	GeneratedAt     int64
	SourceWatermark int64
	FromRedis       bool
}

var channelSmartScheduleLocalSnapshotMetadataCache *channelSmartScheduleLocalSnapshotMetadata
var channelSmartScheduleRouteSnapshotDirtySince int64
var channelSmartScheduleRouteSnapshotDirtyGeneration uint64
var channelSmartScheduleRouteSnapshotDirtyWatermark int64

var channelSmartScheduleRouteSnapshotHealth struct {
	sync.RWMutex
	lastRedisSuccessAt int64
	lastRedisFailureAt int64
	lastRedisError     string
}

// ChannelSmartScheduleRouteSnapshotStatus exposes routing snapshot freshness
// without exposing channel credentials or the payload itself.
type ChannelSmartScheduleRouteSnapshotStatus struct {
	Available          bool   `json:"available"`
	Revision           int64  `json:"revision"`
	GeneratedAt        int64  `json:"generated_at"`
	SourceWatermark    int64  `json:"source_watermark"`
	SnapshotAgeSeconds int64  `json:"snapshot_age_seconds"`
	MaxAgeSeconds      int64  `json:"max_age_seconds"`
	RedisBacked        bool   `json:"redis_backed"`
	Dirty              bool   `json:"dirty"`
	Stale              bool   `json:"stale"`
	Degraded           bool   `json:"degraded"`
	ProtectionMode     bool   `json:"protection_mode"`
	LastRedisSuccessAt int64  `json:"last_redis_success_at"`
	LastRedisFailureAt int64  `json:"last_redis_failure_at"`
	LastRedisError     string `json:"last_redis_error,omitempty"`
}

func channelSmartScheduleRouteSnapshotVersionKey(revision int64) string {
	return fmt.Sprintf("%s:version:%d", channelSmartScheduleRouteSnapshotKeyPrefix, revision)
}

func channelSmartScheduleRouteSnapshotTemporaryKey(revision int64, token string) string {
	return fmt.Sprintf("%s:temporary:%d:%s", channelSmartScheduleRouteSnapshotKeyPrefix, revision, token)
}

func channelSmartScheduleRouteSnapshotMaxAgeDuration() time.Duration {
	raw := strings.TrimSpace(os.Getenv("CHANNEL_SMART_SCHEDULE_ROUTE_SNAPSHOT_MAX_AGE_SECONDS"))
	if raw == "" {
		return channelSmartScheduleRouteSnapshotMaxAge
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return channelSmartScheduleRouteSnapshotMaxAge
	}
	return time.Duration(seconds) * time.Second
}

func channelSmartScheduleRouteSnapshotUsable(now time.Time) bool {
	channelSyncLock.RLock()
	metadata := channelSmartScheduleLocalSnapshotMetadataCache
	routesAvailable := channelSmartScheduleRouteCache != nil
	channelSyncLock.RUnlock()
	if !routesAvailable || metadata == nil || (metadata.FromRedis && metadata.Revision <= 0) ||
		metadata.GeneratedAt <= 0 {
		return false
	}
	age := now.Sub(time.UnixMilli(metadata.GeneratedAt))
	return age >= -5*time.Minute && age <= channelSmartScheduleRouteSnapshotMaxAgeDuration()
}

func channelSmartScheduleRouteSnapshotNeedsRenewal(now time.Time) bool {
	channelSyncLock.RLock()
	metadata := channelSmartScheduleLocalSnapshotMetadataCache
	routesAvailable := channelSmartScheduleRouteCache != nil
	channelSyncLock.RUnlock()
	if !routesAvailable || metadata == nil || (metadata.FromRedis && metadata.Revision <= 0) ||
		metadata.GeneratedAt <= 0 {
		return true
	}
	age := now.Sub(time.UnixMilli(metadata.GeneratedAt))
	return age < -5*time.Minute || age >= channelSmartScheduleRouteSnapshotMaxAgeDuration()/2
}

func GetChannelSmartScheduleRouteSnapshotStatus() ChannelSmartScheduleRouteSnapshotStatus {
	now := time.Now()
	maxAge := channelSmartScheduleRouteSnapshotMaxAgeDuration()
	channelSyncLock.RLock()
	metadata := channelSmartScheduleLocalSnapshotMetadataCache
	routesAvailable := channelSmartScheduleRouteCache != nil
	dirty := logicalChannelRuntimeDirty || len(channelSmartScheduleRouteCacheDirty) > 0 ||
		channelSmartScheduleRouteSnapshotDirtySince > 0
	channelSyncLock.RUnlock()
	status := ChannelSmartScheduleRouteSnapshotStatus{
		Available: routesAvailable && metadata != nil && (!metadata.FromRedis || metadata.Revision > 0),
		Dirty:     dirty, MaxAgeSeconds: int64(maxAge / time.Second),
	}
	if metadata != nil {
		status.Revision = metadata.Revision
		status.GeneratedAt = time.UnixMilli(metadata.GeneratedAt).Unix()
		status.SourceWatermark = metadata.SourceWatermark
		status.RedisBacked = metadata.FromRedis
		age := now.Sub(time.UnixMilli(metadata.GeneratedAt))
		if age > 0 {
			status.SnapshotAgeSeconds = int64(age / time.Second)
		}
		status.Stale = age > maxAge
	}
	channelSmartScheduleRouteSnapshotHealth.RLock()
	status.LastRedisSuccessAt = channelSmartScheduleRouteSnapshotHealth.lastRedisSuccessAt
	status.LastRedisFailureAt = channelSmartScheduleRouteSnapshotHealth.lastRedisFailureAt
	status.LastRedisError = channelSmartScheduleRouteSnapshotHealth.lastRedisError
	channelSmartScheduleRouteSnapshotHealth.RUnlock()
	status.Degraded = !status.RedisBacked || status.Dirty || status.Stale ||
		status.LastRedisError != ""
	status.ProtectionMode = !status.Available || status.Stale
	return status
}

func buildChannelSmartScheduleRouteSnapshot(ctx context.Context) (*channelSmartScheduleRouteSnapshot, error) {
	if DB == nil {
		return nil, ErrChannelSmartScheduleRouteSnapshotUnavailable
	}
	channelSyncLock.RLock()
	dirtyGeneration := channelSmartScheduleRouteSnapshotDirtyGeneration
	channelSyncLock.RUnlock()
	databaseSnapshot, logicalRuntime, err := loadChannelSmartScheduleRouteSnapshotSource(ctx)
	if err != nil {
		return nil, err
	}
	channels := make(map[int]*Channel, len(databaseSnapshot.channels))
	for _, channel := range databaseSnapshot.channels {
		channels[channel.Id] = channel
	}
	routes := buildChannelSmartScheduleRouteCacheFromStates(
		databaseSnapshot.abilities,
		channels,
		databaseSnapshot.smartScheduleStates,
		databaseSnapshot.smartScheduleGroupPauses,
	)
	logicalRouting, err := logicalSmartScheduleRouteOverlaysFromStates(databaseSnapshot.logicalScheduleStates)
	if err != nil {
		return nil, err
	}
	snapshot := newChannelSmartScheduleRouteSnapshot(routes, logicalRuntime, logicalRouting)
	snapshot.localDirtyGeneration = dirtyGeneration
	snapshot.localDirtyGenerationCaptured = true
	return snapshot, nil
}

func newChannelSmartScheduleRouteSnapshot(
	routes map[string]map[string][]channelSmartScheduleCachedRoute,
	logicalRuntime *LogicalChannelRuntimeSnapshot,
	logicalRouting map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay,
) *channelSmartScheduleRouteSnapshot {
	snapshot := &channelSmartScheduleRouteSnapshot{
		SchemaVersion:  channelSmartScheduleRouteSnapshotSchemaVersion,
		GeneratedAt:    time.Now().UnixMilli(),
		Routes:         encodeChannelSmartScheduleRouteCache(routes),
		LogicalRuntime: cloneLogicalChannelRuntimeSnapshot(logicalRuntime),
		LogicalRouting: encodeChannelLogicalSmartScheduleRouting(logicalRouting),
	}
	return snapshot
}

func publishChannelSmartScheduleRouteSnapshot(ctx context.Context) (err error) {
	client := common.RedisMonitorWriteClient()
	if client == nil {
		return publishLocalChannelSmartScheduleRouteSnapshot(ctx)
	}
	defer func() { recordChannelSmartScheduleRouteSnapshotRedisResult(err) }()
	token := common.GetUUID()
	lease, err := client.SetNX(ctx, channelSmartScheduleRouteSnapshotLeaseKey, token, channelSmartScheduleRouteSnapshotLeaseTTL).Result()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrChannelSmartScheduleRouteSnapshotUnavailable, err)
	}
	if !lease {
		return loadChannelSmartScheduleRouteSnapshot(ctx)
	}
	defer releaseChannelSmartScheduleRouteSnapshotLease(client, token)

	sourceWatermark, err := currentChannelSmartScheduleRouteSourceWatermark(ctx, client)
	if err != nil {
		return err
	}
	snapshot, err := buildChannelSmartScheduleRouteSnapshot(ctx)
	if err != nil {
		return err
	}
	revision, err := nextChannelSmartScheduleRouteSnapshotRevision(ctx, client, token)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrChannelSmartScheduleRouteSnapshotUnavailable, err)
	}
	snapshot.Revision = revision
	snapshot.SourceWatermark = sourceWatermark
	snapshot.GeneratedAt, err = nextChannelSmartScheduleRouteSnapshotGeneratedAt(ctx, client)
	if err != nil {
		return err
	}
	payload, err := marshalChannelSmartScheduleRouteSnapshot(snapshot)
	if err != nil {
		return err
	}
	temporaryKey := channelSmartScheduleRouteSnapshotTemporaryKey(revision, token)
	if err := client.Set(ctx, temporaryKey, payload, channelSmartScheduleRouteSnapshotTemporaryTTL).Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrChannelSmartScheduleRouteSnapshotUnavailable, err)
	}
	versionKey := channelSmartScheduleRouteSnapshotVersionKey(revision)
	err = commitChannelSmartScheduleRouteSnapshot(ctx, client, token, temporaryKey, versionKey, snapshot)
	if err != nil {
		_ = client.Del(context.Background(), temporaryKey).Err()
		return fmt.Errorf("%w: %v", ErrChannelSmartScheduleRouteSnapshotUnavailable, err)
	}
	snapshot.fromRedis = true
	if !applyChannelSmartScheduleRouteSnapshot(snapshot) {
		return ErrChannelSmartScheduleRouteSnapshotInvalid
	}
	return nil
}

func publishLocalChannelSmartScheduleRouteSnapshot(ctx context.Context) error {
	snapshot, err := buildChannelSmartScheduleRouteSnapshot(ctx)
	if err != nil {
		return err
	}
	channelSyncLock.RLock()
	metadata := channelSmartScheduleLocalSnapshotMetadataCache
	revision := int64(1)
	watermark := int64(1)
	generatedAt := time.Now().UnixMilli()
	if metadata != nil {
		if metadata.Revision == math.MaxInt64 || metadata.SourceWatermark == math.MaxInt64 ||
			metadata.GeneratedAt == math.MaxInt64 {
			channelSyncLock.RUnlock()
			return ErrChannelSmartScheduleRouteSnapshotInvalid
		}
		revision = metadata.Revision + 1
		watermark = metadata.SourceWatermark + 1
		if generatedAt <= metadata.GeneratedAt {
			generatedAt = metadata.GeneratedAt + 1
		}
	}
	channelSyncLock.RUnlock()
	snapshot.Revision = revision
	snapshot.SourceWatermark = watermark
	snapshot.GeneratedAt = generatedAt
	snapshot.fromRedis = false
	if !applyChannelSmartScheduleRouteSnapshot(snapshot) {
		return ErrChannelSmartScheduleRouteSnapshotInvalid
	}
	return nil
}

func nextChannelSmartScheduleRouteSnapshotRevision(
	ctx context.Context,
	client *redis.Client,
	token string,
) (int64, error) {
	if client == nil {
		return 0, ErrChannelSmartScheduleRouteSnapshotUnavailable
	}
	channelSyncLock.RLock()
	highest := int64(0)
	if channelSmartScheduleLocalSnapshotMetadataCache != nil {
		highest = channelSmartScheduleLocalSnapshotMetadataCache.Revision
	}
	channelSyncLock.RUnlock()
	var next int64
	err := client.Watch(ctx, func(tx *redis.Tx) error {
		leaseToken, getErr := tx.Get(ctx, channelSmartScheduleRouteSnapshotLeaseKey).Result()
		if getErr != nil || leaseToken != token {
			return ErrChannelSmartScheduleRouteSnapshotUnavailable
		}
		values, getErr := tx.MGet(
			ctx,
			channelSmartScheduleRouteSnapshotRevisionKey,
			channelSmartScheduleRouteSnapshotPointerKey,
		).Result()
		if getErr != nil {
			return getErr
		}
		currentHighest := highest
		for _, value := range values {
			if value == nil {
				continue
			}
			revision, parseErr := strconv.ParseInt(fmt.Sprint(value), 10, 64)
			if parseErr != nil || revision < 0 {
				return ErrChannelSmartScheduleRouteSnapshotInvalid
			}
			if revision > currentHighest {
				currentHighest = revision
			}
		}
		if currentHighest == math.MaxInt64 {
			return ErrChannelSmartScheduleRouteSnapshotInvalid
		}
		next = currentHighest + 1
		_, getErr = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Set(ctx, channelSmartScheduleRouteSnapshotRevisionKey, next, 0)
			return nil
		})
		return getErr
	}, channelSmartScheduleRouteSnapshotLeaseKey, channelSmartScheduleRouteSnapshotRevisionKey,
		channelSmartScheduleRouteSnapshotPointerKey)
	if err != nil {
		return 0, err
	}
	return next, nil
}

func currentChannelSmartScheduleRouteSourceWatermark(ctx context.Context, client *redis.Client) (int64, error) {
	channelSyncLock.RLock()
	floor := int64(1)
	if channelSmartScheduleLocalSnapshotMetadataCache != nil &&
		channelSmartScheduleLocalSnapshotMetadataCache.SourceWatermark > floor {
		floor = channelSmartScheduleLocalSnapshotMetadataCache.SourceWatermark
	}
	channelSyncLock.RUnlock()
	var watermark int64
	for attempt := 0; attempt < 3; attempt++ {
		err := client.Watch(ctx, func(tx *redis.Tx) error {
			current, getErr := readChannelSmartScheduleRouteCounter(ctx, tx, channelSmartScheduleRouteSnapshotWatermarkKey)
			if getErr != nil {
				return getErr
			}
			if current >= floor {
				watermark = current
				return nil
			}
			watermark = floor
			_, getErr = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, channelSmartScheduleRouteSnapshotWatermarkKey, watermark, 0)
				return nil
			})
			return getErr
		}, channelSmartScheduleRouteSnapshotWatermarkKey)
		if !errors.Is(err, redis.TxFailedErr) {
			return watermark, err
		}
	}
	return 0, redis.TxFailedErr
}

func advanceChannelSmartScheduleRouteSourceWatermark(ctx context.Context, client *redis.Client) (int64, error) {
	channelSyncLock.RLock()
	floor := int64(0)
	if channelSmartScheduleLocalSnapshotMetadataCache != nil {
		floor = channelSmartScheduleLocalSnapshotMetadataCache.SourceWatermark
	}
	channelSyncLock.RUnlock()
	var watermark int64
	for attempt := 0; attempt < 3; attempt++ {
		err := client.Watch(ctx, func(tx *redis.Tx) error {
			current, getErr := readChannelSmartScheduleRouteCounter(ctx, tx, channelSmartScheduleRouteSnapshotWatermarkKey)
			if getErr != nil {
				return getErr
			}
			if current < floor {
				current = floor
			}
			if current == math.MaxInt64 {
				return ErrChannelSmartScheduleRouteSnapshotInvalid
			}
			watermark = current + 1
			_, getErr = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, channelSmartScheduleRouteSnapshotWatermarkKey, watermark, 0)
				return nil
			})
			return getErr
		}, channelSmartScheduleRouteSnapshotWatermarkKey)
		if !errors.Is(err, redis.TxFailedErr) {
			return watermark, err
		}
	}
	return 0, redis.TxFailedErr
}

type channelSmartScheduleRouteCounterReader interface {
	Get(context.Context, string) *redis.StringCmd
}

func readChannelSmartScheduleRouteCounter(
	ctx context.Context,
	reader channelSmartScheduleRouteCounterReader,
	key string,
) (int64, error) {
	raw, err := reader.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, ErrChannelSmartScheduleRouteSnapshotInvalid
	}
	return value, nil
}

func nextChannelSmartScheduleRouteSnapshotGeneratedAt(ctx context.Context, client *redis.Client) (int64, error) {
	generatedAt := time.Now().UnixMilli()
	lastGeneratedAt, err := readChannelSmartScheduleRouteCounter(ctx, client, channelSmartScheduleRouteSnapshotGeneratedKey)
	if err != nil {
		return 0, err
	}
	channelSyncLock.RLock()
	if channelSmartScheduleLocalSnapshotMetadataCache != nil &&
		channelSmartScheduleLocalSnapshotMetadataCache.GeneratedAt > lastGeneratedAt {
		lastGeneratedAt = channelSmartScheduleLocalSnapshotMetadataCache.GeneratedAt
	}
	channelSyncLock.RUnlock()
	if generatedAt <= lastGeneratedAt {
		if lastGeneratedAt == math.MaxInt64 {
			return 0, ErrChannelSmartScheduleRouteSnapshotInvalid
		}
		generatedAt = lastGeneratedAt + 1
	}
	return generatedAt, nil
}

func commitChannelSmartScheduleRouteSnapshot(
	ctx context.Context,
	client *redis.Client,
	token string,
	temporaryKey string,
	versionKey string,
	snapshot *channelSmartScheduleRouteSnapshot,
) error {
	return client.Watch(ctx, func(tx *redis.Tx) error {
		leaseToken, err := tx.Get(ctx, channelSmartScheduleRouteSnapshotLeaseKey).Result()
		if err != nil || leaseToken != token {
			return ErrChannelSmartScheduleRouteSnapshotUnavailable
		}
		pointer, err := readChannelSmartScheduleRouteCounter(ctx, tx, channelSmartScheduleRouteSnapshotPointerKey)
		if err != nil {
			return err
		}
		if pointer >= snapshot.Revision {
			return ErrChannelSmartScheduleRouteSnapshotInvalid
		}
		watermark, err := readChannelSmartScheduleRouteCounter(ctx, tx, channelSmartScheduleRouteSnapshotWatermarkKey)
		if err != nil {
			return err
		}
		if watermark != snapshot.SourceWatermark {
			return ErrChannelSmartScheduleRouteSnapshotUnavailable
		}
		generatedAt, err := readChannelSmartScheduleRouteCounter(ctx, tx, channelSmartScheduleRouteSnapshotGeneratedKey)
		if err != nil {
			return err
		}
		if generatedAt >= snapshot.GeneratedAt {
			return ErrChannelSmartScheduleRouteSnapshotInvalid
		}
		exists, err := tx.Exists(ctx, temporaryKey).Result()
		if err != nil {
			return err
		}
		if exists != 1 {
			return ErrChannelSmartScheduleRouteSnapshotUnavailable
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Rename(ctx, temporaryKey, versionKey)
			pipe.Expire(ctx, versionKey, channelSmartScheduleRouteSnapshotTTL)
			pipe.Set(ctx, channelSmartScheduleRouteSnapshotPointerKey,
				strconv.FormatInt(snapshot.Revision, 10), channelSmartScheduleRouteSnapshotTTL)
			pipe.Set(ctx, channelSmartScheduleRouteSnapshotGeneratedKey, snapshot.GeneratedAt, 0)
			return nil
		})
		return err
	}, channelSmartScheduleRouteSnapshotLeaseKey, channelSmartScheduleRouteSnapshotPointerKey,
		channelSmartScheduleRouteSnapshotWatermarkKey, channelSmartScheduleRouteSnapshotGeneratedKey, temporaryKey)
}

func releaseChannelSmartScheduleRouteSnapshotLease(client *redis.Client, token string) {
	if client == nil || token == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelSmartScheduleRouteSnapshotIOTimeout)
	defer cancel()
	const releaseScript = `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) end return 0`
	_ = client.Eval(ctx, releaseScript, []string{channelSmartScheduleRouteSnapshotLeaseKey}, token).Err()
}

func loadChannelSmartScheduleRouteSnapshot(ctx context.Context) (err error) {
	defer func() { recordChannelSmartScheduleRouteSnapshotRedisResult(err) }()
	client := common.RedisMonitorReadClient()
	if client == nil {
		return ErrChannelSmartScheduleRouteSnapshotUnavailable
	}
	values, err := client.MGet(
		ctx,
		channelSmartScheduleRouteSnapshotPointerKey,
		channelSmartScheduleRouteSnapshotWatermarkKey,
	).Result()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrChannelSmartScheduleRouteSnapshotUnavailable, err)
	}
	if len(values) != 2 || values[0] == nil {
		return ErrChannelSmartScheduleRouteSnapshotUnavailable
	}
	pointer := fmt.Sprint(values[0])
	revision, err := strconv.ParseInt(pointer, 10, 64)
	if err != nil || revision <= 0 {
		return ErrChannelSmartScheduleRouteSnapshotInvalid
	}
	if values[1] == nil {
		return ErrChannelSmartScheduleRouteSnapshotUnavailable
	}
	sourceWatermark, err := strconv.ParseInt(fmt.Sprint(values[1]), 10, 64)
	if err != nil || sourceWatermark <= 0 {
		return ErrChannelSmartScheduleRouteSnapshotInvalid
	}
	channelSyncLock.RLock()
	metadata := channelSmartScheduleLocalSnapshotMetadataCache
	channelSyncLock.RUnlock()
	if metadata != nil && metadata.Revision > 0 {
		if revision < metadata.Revision ||
			(sourceWatermark > 0 && sourceWatermark < metadata.SourceWatermark) {
			markChannelSmartScheduleRouteSnapshotDirtyFromRemote(metadata.SourceWatermark)
			return ErrChannelSmartScheduleRouteSnapshotInvalid
		}
		if revision == metadata.Revision {
			if sourceWatermark > metadata.SourceWatermark {
				markChannelSmartScheduleRouteSnapshotDirtyFromRemote(sourceWatermark)
				return ErrChannelSmartScheduleRouteSnapshotUnavailable
			}
			return nil
		}
	}
	payload, err := client.Get(ctx, channelSmartScheduleRouteSnapshotVersionKey(revision)).Bytes()
	if errors.Is(err, redis.Nil) {
		return ErrChannelSmartScheduleRouteSnapshotUnavailable
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrChannelSmartScheduleRouteSnapshotUnavailable, err)
	}
	snapshot, err := unmarshalChannelSmartScheduleRouteSnapshot(payload)
	if err != nil {
		return err
	}
	if snapshot.Revision != revision {
		return ErrChannelSmartScheduleRouteSnapshotInvalid
	}
	if sourceWatermark != snapshot.SourceWatermark {
		markChannelSmartScheduleRouteSnapshotDirtyFromRemote(sourceWatermark)
		return ErrChannelSmartScheduleRouteSnapshotInvalid
	}
	snapshot.fromRedis = true
	if !applyChannelSmartScheduleRouteSnapshot(snapshot) {
		return ErrChannelSmartScheduleRouteSnapshotInvalid
	}
	return nil
}

func recordChannelSmartScheduleRouteSnapshotRedisResult(err error) {
	now := time.Now().Unix()
	channelSmartScheduleRouteSnapshotHealth.Lock()
	defer channelSmartScheduleRouteSnapshotHealth.Unlock()
	if err == nil {
		channelSmartScheduleRouteSnapshotHealth.lastRedisSuccessAt = now
		channelSmartScheduleRouteSnapshotHealth.lastRedisError = ""
		return
	}
	channelSmartScheduleRouteSnapshotHealth.lastRedisFailureAt = now
	channelSmartScheduleRouteSnapshotHealth.lastRedisError = err.Error()
}

func marshalChannelSmartScheduleRouteSnapshot(snapshot *channelSmartScheduleRouteSnapshot) ([]byte, error) {
	if snapshot == nil {
		return nil, ErrChannelSmartScheduleRouteSnapshotInvalid
	}
	snapshot.Checksum = ""
	canonical, err := common.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	snapshot.Checksum = fmt.Sprintf("%x", sha256.Sum256(canonical))
	return common.Marshal(snapshot)
}

func unmarshalChannelSmartScheduleRouteSnapshot(payload []byte) (*channelSmartScheduleRouteSnapshot, error) {
	var snapshot channelSmartScheduleRouteSnapshot
	if err := common.Unmarshal(payload, &snapshot); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrChannelSmartScheduleRouteSnapshotInvalid, err)
	}
	if snapshot.SchemaVersion != channelSmartScheduleRouteSnapshotSchemaVersion ||
		snapshot.Revision <= 0 || snapshot.GeneratedAt <= 0 ||
		snapshot.SourceWatermark <= 0 || snapshot.Routes == nil || snapshot.Checksum == "" {
		return nil, ErrChannelSmartScheduleRouteSnapshotInvalid
	}
	if snapshot.GeneratedAt > time.Now().Add(5*time.Minute).UnixMilli() {
		return nil, ErrChannelSmartScheduleRouteSnapshotInvalid
	}
	wantChecksum := snapshot.Checksum
	snapshot.Checksum = ""
	canonical, err := common.Marshal(&snapshot)
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(canonical)) != wantChecksum {
		return nil, ErrChannelSmartScheduleRouteSnapshotInvalid
	}
	snapshot.Checksum = wantChecksum
	if err := validateChannelSmartScheduleRouteSnapshot(&snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func validateChannelSmartScheduleRouteSnapshot(snapshot *channelSmartScheduleRouteSnapshot) error {
	if snapshot == nil || snapshot.LogicalRuntime == nil ||
		snapshot.LogicalRuntime.Channels == nil || snapshot.LogicalRuntime.Groups == nil {
		return ErrChannelSmartScheduleRouteSnapshotInvalid
	}
	for group, modelRoutes := range snapshot.Routes {
		if strings.TrimSpace(group) == "" || modelRoutes == nil {
			return ErrChannelSmartScheduleRouteSnapshotInvalid
		}
		for modelName, routes := range modelRoutes {
			if strings.TrimSpace(modelName) == "" || routes == nil {
				return ErrChannelSmartScheduleRouteSnapshotInvalid
			}
			for _, route := range routes {
				if route.ChannelID <= 0 || route.ExplorationMaxPromptTokens < 0 ||
					route.StabilityReleaseMaxPromptTokens < 0 {
					return ErrChannelSmartScheduleRouteSnapshotInvalid
				}
				for _, member := range route.LogicalMembers {
					if member.ChannelID <= 0 {
						return ErrChannelSmartScheduleRouteSnapshotInvalid
					}
				}
			}
		}
	}
	return nil
}

func applyChannelSmartScheduleRouteSnapshot(snapshot *channelSmartScheduleRouteSnapshot) bool {
	if snapshot == nil {
		return false
	}
	routes := decodeChannelSmartScheduleRouteCache(snapshot.Routes)
	logicalRouting := decodeChannelLogicalSmartScheduleRouting(snapshot.LogicalRouting)
	logicalRuntime := cloneLogicalChannelRuntimeSnapshot(snapshot.LogicalRuntime)
	index := buildChannelSmartScheduleRuntimeRouteIndex(routes)
	channelSyncLock.Lock()
	if current := channelSmartScheduleLocalSnapshotMetadataCache; current != nil && current.Revision > 0 {
		if snapshot.Revision < current.Revision || snapshot.SourceWatermark < current.SourceWatermark ||
			snapshot.GeneratedAt < current.GeneratedAt {
			channelSyncLock.Unlock()
			return false
		}
		if snapshot.Revision == current.Revision {
			channelSyncLock.Unlock()
			return true
		}
	}
	dirtyGeneration := channelSmartScheduleRouteSnapshotDirtyGeneration
	dirtyWatermark := channelSmartScheduleRouteSnapshotDirtyWatermark
	hasDirty := logicalChannelRuntimeDirty || len(channelSmartScheduleRouteCacheDirty) > 0 ||
		channelSmartScheduleRouteSnapshotDirtySince > 0
	channelSmartScheduleRouteCache = routes
	channelLogicalSmartScheduleRoutingCache = logicalRouting
	logicalChannelRuntimeCache = logicalRuntime
	clearsDirty := !hasDirty
	if hasDirty && snapshot.localDirtyGenerationCaptured {
		clearsDirty = snapshot.localDirtyGeneration == dirtyGeneration
	}
	if hasDirty && !snapshot.localDirtyGenerationCaptured {
		clearsDirty = dirtyWatermark > 0 && snapshot.SourceWatermark >= dirtyWatermark
	}
	if clearsDirty {
		logicalChannelRuntimeDirty = false
		channelSmartScheduleRouteCacheDirty = make(map[channelSmartScheduleRoutePool]struct{})
		channelSmartScheduleRouteSnapshotDirtySince = 0
		channelSmartScheduleRouteSnapshotDirtyWatermark = 0
	}
	channelSmartScheduleLocalSnapshotMetadataCache = &channelSmartScheduleLocalSnapshotMetadata{
		Revision: snapshot.Revision, GeneratedAt: snapshot.GeneratedAt,
		SourceWatermark: snapshot.SourceWatermark, FromRedis: snapshot.fromRedis,
	}
	publishChannelSmartScheduleRuntimeRouteIndex(index)
	channelSyncLock.Unlock()
	return true
}

func markLocalChannelSmartScheduleRouteSnapshot(generatedAt int64) {
	if generatedAt <= 0 {
		generatedAt = time.Now().UnixMilli()
	}
	revision := int64(0)
	sourceWatermark := int64(0)
	current := channelSmartScheduleLocalSnapshotMetadataCache
	if current != nil {
		revision = current.Revision
		sourceWatermark = current.SourceWatermark
		if revision < math.MaxInt64 {
			revision = current.Revision + 1
		}
		if sourceWatermark < math.MaxInt64 {
			sourceWatermark = current.SourceWatermark + 1
		}
		if generatedAt <= current.GeneratedAt {
			generatedAt = current.GeneratedAt
			if generatedAt < math.MaxInt64 {
				generatedAt++
			}
		}
	} else if common.RedisMonitorWriteClient() == nil {
		revision = 1
		sourceWatermark = 1
	}
	channelSmartScheduleLocalSnapshotMetadataCache = &channelSmartScheduleLocalSnapshotMetadata{
		Revision: revision, GeneratedAt: generatedAt,
		SourceWatermark: sourceWatermark,
	}
}

// markChannelSmartScheduleRouteSnapshotDirty records the first local change
// after the last published snapshot. A remote snapshot generated before this
// watermark may still update the local copy, but it cannot clear the pending
// rebuild signal.
func markChannelSmartScheduleRouteSnapshotDirty() {
	channelSmartScheduleRouteSnapshotDirtyGeneration++
	if channelSmartScheduleRouteSnapshotDirtyGeneration == 0 {
		channelSmartScheduleRouteSnapshotDirtyGeneration = 1
	}
	if channelSmartScheduleRouteSnapshotDirtySince == 0 {
		channelSmartScheduleRouteSnapshotDirtySince = time.Now().UnixMilli()
	}
}

func markChannelSmartScheduleRouteSnapshotDirtyFromRemote(sourceWatermark int64) {
	channelSyncLock.Lock()
	markChannelSmartScheduleRouteSnapshotDirty()
	if sourceWatermark > channelSmartScheduleRouteSnapshotDirtyWatermark {
		channelSmartScheduleRouteSnapshotDirtyWatermark = sourceWatermark
	}
	channelSyncLock.Unlock()
}

func signalChannelSmartScheduleRouteSnapshotDirty() {
	client := common.RedisMonitorWriteClient()
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelSmartScheduleRouteSnapshotIOTimeout)
	watermark, err := advanceChannelSmartScheduleRouteSourceWatermark(ctx, client)
	cancel()
	recordChannelSmartScheduleRouteSnapshotRedisResult(err)
	if err != nil {
		common.SysError("推进智能调度路由快照水位失败: " + err.Error())
		return
	}
	channelSyncLock.Lock()
	if watermark > channelSmartScheduleRouteSnapshotDirtyWatermark {
		channelSmartScheduleRouteSnapshotDirtyWatermark = watermark
	}
	channelSyncLock.Unlock()
}

func encodeChannelSmartScheduleRouteCache(
	routes map[string]map[string][]channelSmartScheduleCachedRoute,
) map[string]map[string][]channelSmartScheduleCachedRouteSnapshot {
	encoded := make(map[string]map[string][]channelSmartScheduleCachedRouteSnapshot, len(routes))
	for group, modelRoutes := range routes {
		encodedModels := make(map[string][]channelSmartScheduleCachedRouteSnapshot, len(modelRoutes))
		for modelName, poolRoutes := range modelRoutes {
			encodedRoutes := make([]channelSmartScheduleCachedRouteSnapshot, len(poolRoutes))
			for index, route := range poolRoutes {
				members := make([]channelSmartScheduleLogicalMemberSnapshot, len(route.logicalMembers))
				for memberIndex, member := range route.logicalMembers {
					members[memberIndex] = channelSmartScheduleLogicalMemberSnapshot{ChannelID: member.channelID, Weight: member.weight}
				}
				encodedRoutes[index] = channelSmartScheduleCachedRouteSnapshot{
					ChannelID: route.channelId, LogicalChannelID: route.logicalChannelID,
					LogicalRevision: route.logicalRevision, LogicalMembers: members,
					LogicalPriority: route.logicalPriority, LogicalWeight: route.logicalWeight,
					LogicalOfficialPriority: route.logicalOfficialPriority, LogicalOfficialWeight: route.logicalOfficialWeight,
					Priority: route.priority, Weight: route.weight, OfficialPriority: route.officialPriority,
					OfficialWeight: route.officialWeight, OfficialInherited: route.officialInherited,
					Participates: route.participates, TrafficPausedUntil: route.trafficPausedUntil,
					TemporaryTrafficKind: route.temporaryTrafficKind, TemporaryTrafficSince: route.temporaryTrafficSince,
					StabilityState: route.stabilityState, StabilitySince: route.stabilitySince,
					ExplorationMaxPromptTokens:      route.explorationMaxPromptTokens,
					StabilityReleaseMaxPromptTokens: route.stabilityReleaseMaxPromptTokens,
				}
			}
			encodedModels[modelName] = encodedRoutes
		}
		encoded[group] = encodedModels
	}
	return encoded
}

func decodeChannelSmartScheduleRouteCache(
	routes map[string]map[string][]channelSmartScheduleCachedRouteSnapshot,
) map[string]map[string][]channelSmartScheduleCachedRoute {
	decoded := make(map[string]map[string][]channelSmartScheduleCachedRoute, len(routes))
	for group, modelRoutes := range routes {
		decodedModels := make(map[string][]channelSmartScheduleCachedRoute, len(modelRoutes))
		for modelName, poolRoutes := range modelRoutes {
			decodedRoutes := make([]channelSmartScheduleCachedRoute, len(poolRoutes))
			for index, route := range poolRoutes {
				members := make([]channelSmartScheduleLogicalMember, len(route.LogicalMembers))
				for memberIndex, member := range route.LogicalMembers {
					members[memberIndex] = channelSmartScheduleLogicalMember{channelID: member.ChannelID, weight: member.Weight}
				}
				decodedRoutes[index] = channelSmartScheduleCachedRoute{
					channelId: route.ChannelID, logicalChannelID: route.LogicalChannelID,
					logicalRevision: route.LogicalRevision, logicalMembers: members,
					logicalPriority: route.LogicalPriority, logicalWeight: route.LogicalWeight,
					logicalOfficialPriority: route.LogicalOfficialPriority, logicalOfficialWeight: route.LogicalOfficialWeight,
					priority: route.Priority, weight: route.Weight, officialPriority: route.OfficialPriority,
					officialWeight: route.OfficialWeight, officialInherited: route.OfficialInherited,
					participates: route.Participates, trafficPausedUntil: route.TrafficPausedUntil,
					temporaryTrafficKind: route.TemporaryTrafficKind, temporaryTrafficSince: route.TemporaryTrafficSince,
					stabilityState: route.StabilityState, stabilitySince: route.StabilitySince,
					explorationMaxPromptTokens:      route.ExplorationMaxPromptTokens,
					stabilityReleaseMaxPromptTokens: route.StabilityReleaseMaxPromptTokens,
				}
			}
			decodedModels[modelName] = decodedRoutes
		}
		decoded[group] = decodedModels
	}
	return decoded
}

func encodeChannelLogicalSmartScheduleRouting(
	routing map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay,
) []channelLogicalSmartScheduleRoutingSnapshot {
	encoded := make([]channelLogicalSmartScheduleRoutingSnapshot, 0, len(routing))
	for key, overlay := range routing {
		encoded = append(encoded, channelLogicalSmartScheduleRoutingSnapshot{
			LogicalID: key.logicalID, LogicalRevision: key.revision, Group: key.group, Model: key.model,
			Priority: overlay.routing.priority, Weight: overlay.routing.weight,
			Participates: overlay.state.Participates(), TemporaryTrafficKind: overlay.state.TemporaryTrafficKind,
			TemporaryTrafficSince: overlay.state.TemporaryTrafficSince, StabilityState: overlay.state.StabilityState,
			StabilitySince: overlay.state.StabilitySince, ExplorationMaxPromptTokens: overlay.state.ExplorationMaxPromptTokens,
			StabilityReleasePromptTokens: overlay.state.StabilityReleaseMaxPromptTokens,
		})
	}
	return encoded
}

func decodeChannelLogicalSmartScheduleRouting(
	routing []channelLogicalSmartScheduleRoutingSnapshot,
) map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay {
	decoded := make(map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay, len(routing))
	for _, item := range routing {
		decoded[channelLogicalSmartScheduleRouteKey{
			logicalID: item.LogicalID, revision: item.LogicalRevision, group: item.Group, model: item.Model,
		}] = channelLogicalSmartScheduleRouteOverlay{
			routing: channelLogicalSmartScheduleRouting{priority: item.Priority, weight: item.Weight},
			state: ChannelSmartScheduleRouteState{
				ParticipationSet: true, Excluded: !item.Participates,
				TemporaryTrafficKind: item.TemporaryTrafficKind, TemporaryTrafficSince: item.TemporaryTrafficSince,
				StabilityState: item.StabilityState, StabilitySince: item.StabilitySince,
				ExplorationMaxPromptTokens:      item.ExplorationMaxPromptTokens,
				StabilityReleaseMaxPromptTokens: item.StabilityReleasePromptTokens,
			},
		}
	}
	return decoded
}
