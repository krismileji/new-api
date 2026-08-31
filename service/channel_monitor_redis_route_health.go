package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/go-redis/redis/v8"
)

const (
	channelMonitorRedisRouteHealthSamplesSuffix = ":health:v2:samples"
	channelMonitorRedisRouteHealthMetaSuffix    = ":health:v2:meta"
	channelMonitorRedisRouteHealthIndexKey      = ChannelMonitorRedisRouteProjectionPrefix + "health:index:v2"
	channelMonitorRedisRouteHealthStartedAtKey  = ChannelMonitorRedisRouteProjectionPrefix + "health:started_at:v2"

	channelMonitorRedisRouteHealthMaxBatchRoutes       = 2048
	channelMonitorRedisRouteHealthMaxBatchSamples      = 200000
	channelMonitorRedisRouteHealthMaxBatchPayloadBytes = 128 << 20
	channelMonitorRedisRouteHealthMaxMetaFields        = 16
)

var channelMonitorRedisRouteHealthWriteScript = redis.NewScript(`
local samples_key = KEYS[1]
local meta_key = KEYS[2]
local index_key = KEYS[3]
local started_at_key = KEYS[4]
local now = tonumber(ARGV[1])
local cutoff = tonumber(ARGV[2])
local sample_limit = tonumber(ARGV[3])
local ttl_seconds = tonumber(ARGV[4])
local index_expires_at = tonumber(ARGV[5])
local channel_id = ARGV[6]
local model_name = ARGV[7]
local retention_minutes = tonumber(ARGV[8])
local index_member = ARGV[9]

redis.call('SETNX', started_at_key, tostring(now))
local projection_started_at = tonumber(redis.call('GET', started_at_key) or tostring(now))
local previous_retention = tonumber(redis.call('HGET', meta_key, 'retention_minutes') or '0')
local coverage_floor = tonumber(redis.call('HGET', meta_key, 'coverage_floor') or '0')
local limit_cutoff_at = tonumber(redis.call('HGET', meta_key, 'limit_cutoff_at') or '0')

if previous_retention > 0 and retention_minutes > previous_retention then
  local previous_cutoff = now - previous_retention * 60
  if previous_cutoff > coverage_floor then
    coverage_floor = previous_cutoff
  end
end
if coverage_floor < cutoff then
  coverage_floor = 0
end
if limit_cutoff_at < cutoff then
  limit_cutoff_at = 0
end

redis.call('ZREMRANGEBYSCORE', samples_key, '-inf', '(' .. tostring(cutoff))
for index = 10, #ARGV, 2 do
  redis.call('ZADD', samples_key, tonumber(ARGV[index]), ARGV[index + 1])
end

local sample_count = redis.call('ZCARD', samples_key)
if sample_count > sample_limit then
  local excess = sample_count - sample_limit
  local removed = redis.call('ZRANGE', samples_key, 0, excess - 1, 'WITHSCORES')
  for index = 2, #removed, 2 do
    local removed_at = tonumber(removed[index])
    if removed_at > limit_cutoff_at then
      limit_cutoff_at = removed_at
    end
  end
  redis.call('ZREMRANGEBYRANK', samples_key, 0, excess - 1)
end

if redis.call('ZCARD', samples_key) == 0 then
  redis.call('DEL', samples_key)
  redis.call('DEL', meta_key)
  redis.call('ZREM', index_key, index_member)
  redis.call('ZREMRANGEBYSCORE', index_key, '-inf', tostring(now * 1000))
  return {limit_cutoff_at, coverage_floor, projection_started_at}
end

redis.call('HSET', meta_key,
  'channel_id', channel_id,
  'model_name', model_name,
  'retention_minutes', tostring(retention_minutes),
  'sample_limit', tostring(sample_limit),
  'coverage_floor', tostring(coverage_floor),
  'limit_cutoff_at', tostring(limit_cutoff_at),
  'projection_started_at', tostring(projection_started_at),
  'updated_at', tostring(now))
redis.call('EXPIRE', samples_key, ttl_seconds)
redis.call('EXPIRE', meta_key, ttl_seconds)
redis.call('ZREMRANGEBYSCORE', index_key, '-inf', tostring(now * 1000))
redis.call('ZADD', index_key, index_expires_at, index_member)
return {limit_cutoff_at, coverage_floor, projection_started_at}
`)

var ErrChannelMonitorRedisRouteHealthUnavailable = errors.New("渠道监控 Redis 路由健康窗口不可用")

type ChannelMonitorRedisRouteHealthSample struct {
	EventID                   string                           `json:"event_id"`
	EventSequence             uint64                           `json:"event_sequence"`
	OccurredAt                int64                            `json:"occurred_at"`
	GroupName                 string                           `json:"group,omitempty"`
	Source                    model.ChannelMonitorEventSource  `json:"source"`
	Outcome                   model.ChannelMonitorEventOutcome `json:"outcome"`
	IsRetryAttempt            bool                             `json:"is_retry_attempt"`
	IsFinalAttempt            bool                             `json:"is_final_attempt"`
	FinalRetrySummary         bool                             `json:"final_retry_summary"`
	RequestDispatched         bool                             `json:"request_dispatched"`
	SchedulingEligible        bool                             `json:"scheduling_eligible"`
	RuntimeProtectionEligible bool                             `json:"runtime_protection_eligible"`
	StatusCode                *int                             `json:"status_code,omitempty"`
	ErrorCode                 string                           `json:"error_code,omitempty"`
	ErrorMessage              string                           `json:"error_message,omitempty"`
	FirstTokenMs              *float64                         `json:"first_token_ms,omitempty"`
	TPS                       *float64                         `json:"tps,omitempty"`
	TPSOutputTokens           *int64                           `json:"tps_output_tokens,omitempty"`
	TPSGenerationDurationMs   *int64                           `json:"tps_generation_duration_ms,omitempty"`
	AttemptDurationMs         *int64                           `json:"attempt_duration_ms,omitempty"`
}

type ChannelMonitorRedisRouteHealthSnapshot struct {
	ChannelID               int                                       `json:"channel_id"`
	ModelName               string                                    `json:"model"`
	EventCount              int64                                     `json:"event_count"`
	BusinessRequestCount    int64                                     `json:"business_request_count"`
	ActualSuccessCount      int64                                     `json:"actual_success_count"`
	ActualFailureCount      int64                                     `json:"actual_failure_count"`
	ActualSampleCount       int64                                     `json:"actual_sample_count"`
	ActualSuccessRate       float64                                   `json:"actual_success_rate"`
	FinalSuccessCount       int64                                     `json:"final_success_count"`
	FinalFailureCount       int64                                     `json:"final_failure_count"`
	FinalSampleCount        int64                                     `json:"final_sample_count"`
	FinalSuccessRate        float64                                   `json:"final_success_rate"`
	FirstTokenSampleCount   int64                                     `json:"first_token_sample_count"`
	FirstTokenTotalMs       float64                                   `json:"first_token_total_ms"`
	AverageFirstTokenMs     *float64                                  `json:"average_first_token_ms"`
	TPSSampleCount          int64                                     `json:"tps_sample_count"`
	TPSOutputTokens         int64                                     `json:"tps_output_tokens"`
	TPSGenerationDurationMs int64                                     `json:"tps_generation_duration_ms"`
	AverageTPS              *float64                                  `json:"average_tps"`
	SourceCounts            map[model.ChannelMonitorEventSource]int64 `json:"source_counts"`
	WindowStart             int64                                     `json:"window_start"`
	WindowEnd               int64                                     `json:"window_end"`
	CoverageStart           int64                                     `json:"coverage_start"`
	ProjectionStartedAt     int64                                     `json:"projection_started_at"`
	RetentionMinutes        int                                       `json:"retention_minutes"`
	SampleLimit             int                                       `json:"sample_limit"`
	SampleLimitTruncated    bool                                      `json:"sample_limit_truncated"`
	SampleLimitCutoffAt     int64                                     `json:"sample_limit_cutoff_at"`
	DataCutoffAt            int64                                     `json:"data_cutoff_at"`
	ProcessedAt             int64                                     `json:"processed_at"`
	EventWatermark          uint64                                    `json:"event_watermark"`
}

type ChannelMonitorRedisRouteHealthWindow struct {
	Snapshot ChannelMonitorRedisRouteHealthSnapshot `json:"snapshot"`
	Samples  []ChannelMonitorRedisRouteHealthSample `json:"samples"`
}

type ChannelMonitorRedisRouteHealthRouteKey struct {
	ChannelID int
	ModelName string
}

type ChannelMonitorRedisRouteHealthWindowBatch struct {
	Windows             map[ChannelMonitorRedisRouteHealthRouteKey]ChannelMonitorRedisRouteHealthWindow
	CoverageStart       int64
	ProjectionStartedAt int64
}

type channelMonitorRedisRouteHealthRouteKey struct {
	channelID int
	modelName string
}

type channelMonitorRedisRouteHealthBatchLimits struct {
	maxRoutes       int
	maxSamples      int
	maxPayloadBytes int
	maxMetaFields   int
}

type ChannelMonitorRedisRouteHealthProjection struct {
	client        *redis.Client
	now           func(context.Context) (time.Time, error)
	settingsFn    func() model.ChannelMonitorSmartScheduleRealtimeSettings
	batchLimitsFn func() channelMonitorRedisRouteHealthBatchLimits
}

var _ ChannelMonitorRedisEventHandler = (*ChannelMonitorRedisRouteHealthProjection)(nil)

func ChannelMonitorRedisRouteHealthWindowKey(channelID int, modelName string) string {
	identity := strconv.Itoa(channelID) + ":" + ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	return ChannelMonitorRedisProjectionKeyForRoute(identity) + channelMonitorRedisRouteHealthSamplesSuffix
}

func NewChannelMonitorRedisRouteHealthRouteKey(
	channelID int,
	modelName string,
) (ChannelMonitorRedisRouteHealthRouteKey, bool) {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	if channelID <= 0 || modelName == "" {
		return ChannelMonitorRedisRouteHealthRouteKey{}, false
	}
	return ChannelMonitorRedisRouteHealthRouteKey{ChannelID: channelID, ModelName: modelName}, true
}

func channelMonitorRedisRouteHealthMetaKey(channelID int, modelName string) string {
	identity := strconv.Itoa(channelID) + ":" + ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	return ChannelMonitorRedisProjectionKeyForRoute(identity) + channelMonitorRedisRouteHealthMetaSuffix
}

func channelMonitorRedisRouteHealthMetaKeyFromWindowKey(windowKey string) string {
	return strings.TrimSuffix(windowKey, channelMonitorRedisRouteHealthSamplesSuffix) + channelMonitorRedisRouteHealthMetaSuffix
}

func ChannelMonitorRedisRouteHealthIndexKey() string {
	return channelMonitorRedisRouteHealthIndexKey
}

func ChannelMonitorRedisRouteHealthCoverageStart() int64 {
	settings := model.GetChannelMonitorSmartScheduleRealtimeSettings()
	coverageStart := time.Now().Add(-time.Duration(settings.RetentionMinutes) * time.Minute).Unix()
	if projectionStartedAt := ChannelMonitorRedisRouteHealthProjectionStartedAt(context.Background()); projectionStartedAt > coverageStart {
		coverageStart = projectionStartedAt
	}
	return coverageStart
}

func ChannelMonitorRedisRouteHealthProjectionStartedAt(ctx context.Context) int64 {
	client := common.RedisMonitorReadClient()
	if !common.RedisEnabled || client == nil {
		return 0
	}
	if ctx == nil {
		ctx = context.Background()
	}
	value, err := client.Get(ctx, channelMonitorRedisRouteHealthStartedAtKey).Int64()
	if err != nil {
		return 0
	}
	return value
}

func NewChannelMonitorRedisRouteHealthProjection() (*ChannelMonitorRedisRouteHealthProjection, error) {
	client := common.RedisMonitorReadClient()
	if !common.RedisEnabled || client == nil {
		return nil, ErrChannelMonitorRedisRouteHealthUnavailable
	}
	return newChannelMonitorRedisRouteHealthProjection(client, nil)
}

func NewChannelMonitorRedisRouteHealthProjectionForClient(client *redis.Client) (*ChannelMonitorRedisRouteHealthProjection, error) {
	return newChannelMonitorRedisRouteHealthProjection(client, nil)
}

func GetChannelMonitorRedisRouteHealthWindow(
	ctx context.Context,
	channelID int,
	modelName string,
) (ChannelMonitorRedisRouteHealthWindow, bool, error) {
	key, valid := NewChannelMonitorRedisRouteHealthRouteKey(channelID, modelName)
	if !valid {
		return ChannelMonitorRedisRouteHealthWindow{}, false, nil
	}
	batch, err := GetChannelMonitorRedisRouteHealthWindows(ctx, []ChannelMonitorRedisRouteHealthRouteKey{key})
	if err != nil {
		return ChannelMonitorRedisRouteHealthWindow{}, false, err
	}
	window, available := batch.Windows[key]
	return window, available, nil
}

func GetChannelMonitorRedisRouteHealthWindows(
	ctx context.Context,
	routes []ChannelMonitorRedisRouteHealthRouteKey,
) (ChannelMonitorRedisRouteHealthWindowBatch, error) {
	if len(routes) == 0 {
		return ChannelMonitorRedisRouteHealthWindowBatch{
			Windows: make(map[ChannelMonitorRedisRouteHealthRouteKey]ChannelMonitorRedisRouteHealthWindow),
		}, nil
	}
	projection, err := NewChannelMonitorRedisRouteHealthProjection()
	if err != nil {
		return ChannelMonitorRedisRouteHealthWindowBatch{}, err
	}
	return projection.GetRouteHealthWindows(ctx, routes)
}

func newChannelMonitorRedisRouteHealthProjection(
	client *redis.Client,
	now func(context.Context) (time.Time, error),
) (*ChannelMonitorRedisRouteHealthProjection, error) {
	if client == nil {
		return nil, ErrChannelMonitorRedisRouteHealthUnavailable
	}
	projection := &ChannelMonitorRedisRouteHealthProjection{
		client:     client,
		settingsFn: model.GetChannelMonitorSmartScheduleRealtimeSettings,
		batchLimitsFn: func() channelMonitorRedisRouteHealthBatchLimits {
			return channelMonitorRedisRouteHealthBatchLimits{
				maxRoutes:       channelMonitorRedisRouteHealthMaxBatchRoutes,
				maxSamples:      channelMonitorRedisRouteHealthMaxBatchSamples,
				maxPayloadBytes: channelMonitorRedisRouteHealthMaxBatchPayloadBytes,
				maxMetaFields:   channelMonitorRedisRouteHealthMaxMetaFields,
			}
		},
	}
	if now == nil {
		projection.now = func(ctx context.Context) (time.Time, error) {
			return client.Time(ctx).Result()
		}
	} else {
		projection.now = now
	}
	return projection, nil
}

func (projection *ChannelMonitorRedisRouteHealthProjection) HandleChannelMonitorEvents(
	ctx context.Context,
	events []model.ChannelMonitorEvent,
) error {
	if projection == nil || projection.client == nil {
		return ErrChannelMonitorRedisRouteHealthUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(events) == 0 {
		return nil
	}

	grouped := make(map[channelMonitorRedisRouteHealthRouteKey][]ChannelMonitorRedisRouteHealthSample)
	for _, event := range events {
		if err := event.Validate(); err != nil {
			return err
		}
		modelName := ratio_setting.FormatMatchingModelName(strings.TrimSpace(event.ModelName))
		if !event.SchedulingEligible || event.ChannelId <= 0 || modelName == "" {
			continue
		}
		event.ModelName = modelName
		key := channelMonitorRedisRouteHealthRouteKey{channelID: event.ChannelId, modelName: modelName}
		grouped[key] = append(grouped[key], channelMonitorRedisRouteHealthSampleFromEvent(event))
	}
	if len(grouped) == 0 {
		return nil
	}
	now, err := projection.now(ctx)
	if err != nil {
		return err
	}
	settings := projection.settingsFn()
	keys := make([]channelMonitorRedisRouteHealthRouteKey, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].modelName != keys[j].modelName {
			return keys[i].modelName < keys[j].modelName
		}
		return keys[i].channelID < keys[j].channelID
	})
	for _, key := range keys {
		if err := projection.updateRoute(ctx, key, grouped[key], now, settings); err != nil {
			return err
		}
	}
	return nil
}

func (projection *ChannelMonitorRedisRouteHealthProjection) Write(
	ctx context.Context,
	events []model.ChannelMonitorEvent,
) error {
	return projection.HandleChannelMonitorEvents(ctx, events)
}

func (projection *ChannelMonitorRedisRouteHealthProjection) updateRoute(
	ctx context.Context,
	key channelMonitorRedisRouteHealthRouteKey,
	incoming []ChannelMonitorRedisRouteHealthSample,
	now time.Time,
	settings model.ChannelMonitorSmartScheduleRealtimeSettings,
) error {
	windowKey := ChannelMonitorRedisRouteHealthWindowKey(key.channelID, key.modelName)
	metaKey := channelMonitorRedisRouteHealthMetaKey(key.channelID, key.modelName)
	retention := time.Duration(settings.RetentionMinutes) * time.Minute
	args := []interface{}{
		now.Unix(),
		now.Add(-retention).Unix(),
		settings.SampleLimit,
		int64(retention / time.Second),
		now.Add(retention).UnixMilli(),
		key.channelID,
		key.modelName,
		settings.RetentionMinutes,
		windowKey,
	}
	for _, sample := range incoming {
		payload, err := common.Marshal(sample)
		if err != nil {
			return err
		}
		args = append(args, sample.OccurredAt, string(payload))
	}
	_, err := channelMonitorRedisRouteHealthWriteScript.Run(
		ctx,
		projection.client,
		[]string{windowKey, metaKey, channelMonitorRedisRouteHealthIndexKey, channelMonitorRedisRouteHealthStartedAtKey},
		args...,
	).Result()
	return err
}

func (projection *ChannelMonitorRedisRouteHealthProjection) GetRouteHealthWindow(
	ctx context.Context,
	channelID int,
	modelName string,
) (ChannelMonitorRedisRouteHealthWindow, bool, error) {
	key, valid := NewChannelMonitorRedisRouteHealthRouteKey(channelID, modelName)
	if !valid {
		return ChannelMonitorRedisRouteHealthWindow{}, false, nil
	}
	batch, err := projection.GetRouteHealthWindows(ctx, []ChannelMonitorRedisRouteHealthRouteKey{key})
	if err != nil {
		return ChannelMonitorRedisRouteHealthWindow{}, false, err
	}
	window, available := batch.Windows[key]
	return window, available, nil
}

func (projection *ChannelMonitorRedisRouteHealthProjection) GetRouteHealthWindows(
	ctx context.Context,
	routes []ChannelMonitorRedisRouteHealthRouteKey,
) (ChannelMonitorRedisRouteHealthWindowBatch, error) {
	empty := ChannelMonitorRedisRouteHealthWindowBatch{
		Windows: make(map[ChannelMonitorRedisRouteHealthRouteKey]ChannelMonitorRedisRouteHealthWindow),
	}
	if len(routes) == 0 {
		return empty, nil
	}
	if projection == nil || projection.client == nil {
		return ChannelMonitorRedisRouteHealthWindowBatch{}, ErrChannelMonitorRedisRouteHealthUnavailable
	}
	limits := projection.batchLimitsFn()
	if len(routes) > limits.maxRoutes {
		return ChannelMonitorRedisRouteHealthWindowBatch{}, fmt.Errorf(
			"Redis 路由健康批量查询路由数超过上限 %d", limits.maxRoutes,
		)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	normalizedRoutes := make([]ChannelMonitorRedisRouteHealthRouteKey, 0, len(routes))
	seen := make(map[ChannelMonitorRedisRouteHealthRouteKey]struct{}, len(routes))
	for _, route := range routes {
		key, valid := NewChannelMonitorRedisRouteHealthRouteKey(route.ChannelID, route.ModelName)
		if !valid {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalizedRoutes = append(normalizedRoutes, key)
	}
	if len(normalizedRoutes) == 0 {
		return empty, nil
	}
	now, err := projection.now(ctx)
	if err != nil {
		return ChannelMonitorRedisRouteHealthWindowBatch{}, err
	}
	settings := projection.settingsFn()
	cutoff := now.Add(-time.Duration(settings.RetentionMinutes) * time.Minute).Unix()
	type routeReadCommands struct {
		key            ChannelMonitorRedisRouteHealthRouteKey
		windowKey      string
		metaKey        string
		metaCommand    *redis.StringStringMapCmd
		samplesCommand *redis.ZSliceCmd
	}
	commands := make([]routeReadCommands, len(normalizedRoutes))
	var startedAtCommand *redis.StringCmd
	_, err = projection.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		startedAtCommand = pipe.Get(ctx, channelMonitorRedisRouteHealthStartedAtKey)
		for index, key := range normalizedRoutes {
			windowKey := ChannelMonitorRedisRouteHealthWindowKey(key.ChannelID, key.ModelName)
			commands[index] = routeReadCommands{
				key:            key,
				windowKey:      windowKey,
				metaKey:        channelMonitorRedisRouteHealthMetaKey(key.ChannelID, key.ModelName),
				metaCommand:    pipe.HGetAll(ctx, channelMonitorRedisRouteHealthMetaKey(key.ChannelID, key.ModelName)),
				// Read one sentinel sample beyond the configured budget.  Using
				// ZRANGE ... -1 would materialize an unbounded sorted set before
				// the length check below could reject it.
				samplesCommand: pipe.ZRangeWithScores(ctx, windowKey, 0, int64(limits.maxSamples)),
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, redis.Nil) {
		return ChannelMonitorRedisRouteHealthWindowBatch{}, err
	}
	projectionStartedAt, _ := startedAtCommand.Int64()
	result := ChannelMonitorRedisRouteHealthWindowBatch{
		Windows:             make(map[ChannelMonitorRedisRouteHealthRouteKey]ChannelMonitorRedisRouteHealthWindow, len(commands)),
		CoverageStart:       max(cutoff, projectionStartedAt),
		ProjectionStartedAt: projectionStartedAt,
	}
	totalSamples := 0
	totalPayloadBytes := 0
	for _, command := range commands {
		meta, commandErr := command.metaCommand.Result()
		if commandErr != nil {
			return ChannelMonitorRedisRouteHealthWindowBatch{}, commandErr
		}
		if len(meta) > limits.maxMetaFields {
			return ChannelMonitorRedisRouteHealthWindowBatch{}, fmt.Errorf(
				"Redis 路由健康元数据字段数超过上限 %d", limits.maxMetaFields,
			)
		}
		for field, value := range meta {
			totalPayloadBytes += len(field) + len(value)
		}
		if totalPayloadBytes > limits.maxPayloadBytes {
			return ChannelMonitorRedisRouteHealthWindowBatch{}, fmt.Errorf(
				"Redis 路由健康批量查询载荷超过上限 %d 字节",
				limits.maxPayloadBytes,
			)
		}
		stored, commandErr := command.samplesCommand.Result()
		if commandErr != nil {
			return ChannelMonitorRedisRouteHealthWindowBatch{}, commandErr
		}
		totalSamples += len(stored)
		if totalSamples > limits.maxSamples {
			return ChannelMonitorRedisRouteHealthWindowBatch{}, fmt.Errorf(
				"Redis 路由健康批量查询样本数超过上限 %d", limits.maxSamples,
			)
		}
		for _, item := range stored {
			member, ok := item.Member.(string)
			if !ok {
				return ChannelMonitorRedisRouteHealthWindowBatch{}, errors.New("Redis 路由健康样本格式无效")
			}
			totalPayloadBytes += len(member)
			if totalPayloadBytes > limits.maxPayloadBytes {
				return ChannelMonitorRedisRouteHealthWindowBatch{}, fmt.Errorf(
					"Redis 路由健康批量查询载荷超过上限 %d 字节",
					limits.maxPayloadBytes,
				)
			}
		}
	}

	type storedSample struct {
		member string
		sample ChannelMonitorRedisRouteHealthSample
	}
	type routeCleanup struct {
		windowKey     string
		metaKey       string
		removeMembers []interface{}
		deleteWindow  bool
		removeIndex   bool
		updateMeta    map[string]interface{}
	}
	cleanups := make([]routeCleanup, 0)
	for _, command := range commands {
		meta, _ := command.metaCommand.Result()
		stored, _ := command.samplesCommand.Result()
		if len(stored) == 0 {
			cleanups = append(cleanups, routeCleanup{windowKey: command.windowKey, removeIndex: true})
			continue
		}
		if meta["channel_id"] != strconv.Itoa(command.key.ChannelID) || meta["model_name"] != command.key.ModelName {
			return ChannelMonitorRedisRouteHealthWindowBatch{}, errors.New("Redis 路由健康窗口标识无效")
		}
		routeProjectionStartedAt := projectionStartedAt
		if routeProjectionStartedAt <= 0 {
			routeProjectionStartedAt, _ = strconv.ParseInt(meta["projection_started_at"], 10, 64)
			result.ProjectionStartedAt = max(result.ProjectionStartedAt, routeProjectionStartedAt)
			result.CoverageStart = max(result.CoverageStart, routeProjectionStartedAt)
		}
		previousRetention, _ := strconv.Atoi(meta["retention_minutes"])
		coverageFloor, _ := strconv.ParseInt(meta["coverage_floor"], 10, 64)
		if previousRetention > 0 && settings.RetentionMinutes > previousRetention {
			coverageFloor = max(coverageFloor, now.Add(-time.Duration(previousRetention)*time.Minute).Unix())
		}
		if coverageFloor < cutoff {
			coverageFloor = 0
		}
		limitCutoffAt, _ := strconv.ParseInt(meta["limit_cutoff_at"], 10, 64)
		if limitCutoffAt < cutoff {
			limitCutoffAt = 0
		}

		byEventID := make(map[string]storedSample, len(stored))
		removeMembers := make([]interface{}, 0)
		for _, item := range stored {
			member := item.Member.(string)
			var sample ChannelMonitorRedisRouteHealthSample
			if unmarshalErr := common.Unmarshal([]byte(member), &sample); unmarshalErr != nil {
				return ChannelMonitorRedisRouteHealthWindowBatch{}, fmt.Errorf(
					"解析 Redis 路由健康样本失败: %w", unmarshalErr,
				)
			}
			if sample.OccurredAt < cutoff {
				removeMembers = append(removeMembers, member)
				continue
			}
			candidate := storedSample{member: member, sample: sample}
			if existing, exists := byEventID[sample.EventID]; exists {
				if channelMonitorRedisRouteHealthSampleCanonicalLess(sample, existing.sample) {
					removeMembers = append(removeMembers, existing.member)
					byEventID[sample.EventID] = candidate
				} else {
					removeMembers = append(removeMembers, member)
				}
				continue
			}
			byEventID[sample.EventID] = candidate
		}
		ordered := make([]storedSample, 0, len(byEventID))
		for _, item := range byEventID {
			ordered = append(ordered, item)
		}
		sort.SliceStable(ordered, func(i, j int) bool {
			return channelMonitorRedisRouteHealthSampleCanonicalLess(ordered[i].sample, ordered[j].sample)
		})
		if len(ordered) > settings.SampleLimit {
			excess := len(ordered) - settings.SampleLimit
			for _, item := range ordered[:excess] {
				removeMembers = append(removeMembers, item.member)
				limitCutoffAt = max(limitCutoffAt, item.sample.OccurredAt)
			}
			ordered = ordered[excess:]
		}
		if len(ordered) == 0 {
			cleanups = append(cleanups, routeCleanup{
				windowKey: command.windowKey, metaKey: command.metaKey, deleteWindow: true, removeIndex: true,
			})
			continue
		}
		if len(removeMembers) > 0 || previousRetention != settings.RetentionMinutes {
			cleanups = append(cleanups, routeCleanup{
				windowKey:     command.windowKey,
				metaKey:       command.metaKey,
				removeMembers: removeMembers,
				updateMeta: map[string]interface{}{
					"retention_minutes": settings.RetentionMinutes,
					"sample_limit":      settings.SampleLimit,
					"coverage_floor":    coverageFloor,
					"limit_cutoff_at":   limitCutoffAt,
				},
			})
		}
		samples := make([]ChannelMonitorRedisRouteHealthSample, len(ordered))
		for index, item := range ordered {
			samples[index] = item.sample
		}
		coverageStart := max(cutoff, routeProjectionStartedAt)
		coverageStart = max(coverageStart, coverageFloor)
		if limitCutoffAt >= cutoff {
			coverageStart = max(coverageStart, limitCutoffAt+1)
		}
		result.Windows[command.key] = ChannelMonitorRedisRouteHealthWindow{
			Samples: samples,
			Snapshot: buildChannelMonitorRedisRouteHealthSnapshot(
				command.key.ChannelID,
				command.key.ModelName,
				samples,
				coverageStart,
				now.Unix(),
				routeProjectionStartedAt,
				settings,
				limitCutoffAt,
			),
		}
	}
	if len(cleanups) > 0 {
		_, err = projection.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			for _, cleanup := range cleanups {
				if cleanup.deleteWindow {
					pipe.Del(ctx, cleanup.windowKey, cleanup.metaKey)
				}
				if len(cleanup.removeMembers) > 0 {
					pipe.ZRem(ctx, cleanup.windowKey, cleanup.removeMembers...)
				}
				if cleanup.updateMeta != nil {
					pipe.HSet(ctx, cleanup.metaKey, cleanup.updateMeta)
				}
				if cleanup.removeIndex {
					pipe.ZRem(ctx, channelMonitorRedisRouteHealthIndexKey, cleanup.windowKey)
				}
			}
			return nil
		})
		if err != nil {
			return ChannelMonitorRedisRouteHealthWindowBatch{}, err
		}
	}
	return result, nil
}

func (projection *ChannelMonitorRedisRouteHealthProjection) GetSnapshot(
	ctx context.Context,
	channelID int,
	modelName string,
) (ChannelMonitorRedisRouteHealthSnapshot, bool, error) {
	window, available, err := projection.GetRouteHealthWindow(ctx, channelID, modelName)
	return window.Snapshot, available, err
}

func (projection *ChannelMonitorRedisRouteHealthProjection) ListRouteHealthWindows(
	ctx context.Context,
) ([]ChannelMonitorRedisRouteHealthWindow, error) {
	if projection == nil || projection.client == nil {
		return nil, ErrChannelMonitorRedisRouteHealthUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now, err := projection.now(ctx)
	if err != nil {
		return nil, err
	}
	var keysCommand *redis.StringSliceCmd
	_, err = projection.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		nowMilliseconds := strconv.FormatInt(now.UnixMilli(), 10)
		pipe.ZRemRangeByScore(ctx, channelMonitorRedisRouteHealthIndexKey, "-inf", nowMilliseconds)
		keysCommand = pipe.ZRangeByScore(ctx, channelMonitorRedisRouteHealthIndexKey, &redis.ZRangeBy{
			Min: "(" + nowMilliseconds,
			Max: "+inf",
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	keys, err := keysCommand.Result()
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	result := make([]ChannelMonitorRedisRouteHealthWindow, 0, len(keys))
	for _, key := range keys {
		meta, getErr := projection.client.HGetAll(ctx, channelMonitorRedisRouteHealthMetaKeyFromWindowKey(key)).Result()
		if getErr != nil {
			return nil, getErr
		}
		channelID, parseErr := strconv.Atoi(meta["channel_id"])
		modelName := meta["model_name"]
		if parseErr != nil || channelID <= 0 || modelName == "" {
			if removeErr := projection.removeCorruptRouteHealthIndexEntry(ctx, key); removeErr != nil {
				return nil, removeErr
			}
			continue
		}
		window, available, getErr := projection.GetRouteHealthWindow(ctx, channelID, modelName)
		if getErr != nil {
			return nil, getErr
		}
		if available {
			result = append(result, window)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Snapshot.ModelName != result[j].Snapshot.ModelName {
			return result[i].Snapshot.ModelName < result[j].Snapshot.ModelName
		}
		return result[i].Snapshot.ChannelID < result[j].Snapshot.ChannelID
	})
	return result, nil
}

func (projection *ChannelMonitorRedisRouteHealthProjection) removeMissingRouteHealthIndexEntry(
	ctx context.Context,
	windowKey string,
) error {
	exists, err := projection.client.Exists(ctx, windowKey).Result()
	if err != nil || exists > 0 {
		return err
	}
	_, err = projection.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, channelMonitorRedisRouteHealthMetaKeyFromWindowKey(windowKey))
		pipe.ZRem(ctx, channelMonitorRedisRouteHealthIndexKey, windowKey)
		return nil
	})
	return err
}

func (projection *ChannelMonitorRedisRouteHealthProjection) removeCorruptRouteHealthIndexEntry(
	ctx context.Context,
	windowKey string,
) error {
	_, err := projection.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, windowKey, channelMonitorRedisRouteHealthMetaKeyFromWindowKey(windowKey))
		pipe.ZRem(ctx, channelMonitorRedisRouteHealthIndexKey, windowKey)
		return nil
	})
	return err
}

func channelMonitorRedisRouteHealthSampleFromEvent(event model.ChannelMonitorEvent) ChannelMonitorRedisRouteHealthSample {
	errorMessage := event.ErrorMessage
	if messageRunes := []rune(errorMessage); len(messageRunes) > 256 {
		errorMessage = string(messageRunes[:256])
	}
	sample := ChannelMonitorRedisRouteHealthSample{
		EventID: event.EventId, EventSequence: event.EventSequence, OccurredAt: event.OccurredAt,
		GroupName: event.GroupName,
		Source:    event.Source, Outcome: event.Outcome, IsRetryAttempt: event.IsRetryAttempt,
		IsFinalAttempt: event.IsFinalAttempt, FinalRetrySummary: event.FinalRetrySummary,
		RequestDispatched: event.RequestDispatched, SchedulingEligible: event.SchedulingEligible,
		RuntimeProtectionEligible: event.RuntimeProtectionEligible, ErrorCode: event.ErrorCode,
		ErrorMessage: errorMessage,
	}
	if event.StatusCode != nil {
		value := *event.StatusCode
		sample.StatusCode = &value
	}
	if event.FirstTokenMs != nil {
		value := *event.FirstTokenMs
		sample.FirstTokenMs = &value
	}
	if event.TPS != nil {
		value := *event.TPS
		sample.TPS = &value
	}
	if outputTokens, generationDurationMs, ok := event.TPSMeasurement(); ok {
		sample.TPSOutputTokens = &outputTokens
		sample.TPSGenerationDurationMs = &generationDurationMs
	}
	if event.AttemptDurationMs != nil {
		value := *event.AttemptDurationMs
		sample.AttemptDurationMs = &value
	}
	return sample
}

func channelMonitorRedisRouteHealthSampleCanonicalLess(
	candidate ChannelMonitorRedisRouteHealthSample,
	current ChannelMonitorRedisRouteHealthSample,
) bool {
	if candidate.OccurredAt != current.OccurredAt {
		return candidate.OccurredAt < current.OccurredAt
	}
	if candidate.EventSequence != current.EventSequence {
		return candidate.EventSequence < current.EventSequence
	}
	if candidate.EventID != current.EventID {
		return candidate.EventID < current.EventID
	}
	candidatePayload, _ := common.Marshal(candidate)
	currentPayload, _ := common.Marshal(current)
	return string(candidatePayload) < string(currentPayload)
}

func buildChannelMonitorRedisRouteHealthSnapshot(
	channelID int,
	modelName string,
	samples []ChannelMonitorRedisRouteHealthSample,
	coverageStart int64,
	processedAt int64,
	projectionStartedAt int64,
	settings model.ChannelMonitorSmartScheduleRealtimeSettings,
	limitCutoffAt int64,
) ChannelMonitorRedisRouteHealthSnapshot {
	snapshot := ChannelMonitorRedisRouteHealthSnapshot{
		ChannelID: channelID, ModelName: modelName, CoverageStart: coverageStart,
		ProjectionStartedAt:  projectionStartedAt,
		RetentionMinutes:     settings.RetentionMinutes,
		SampleLimit:          settings.SampleLimit,
		SampleLimitTruncated: limitCutoffAt > 0,
		SampleLimitCutoffAt:  limitCutoffAt,
		ProcessedAt:          processedAt,
		SourceCounts:         make(map[model.ChannelMonitorEventSource]int64),
	}
	for _, sample := range samples {
		snapshot.EventCount++
		snapshot.SourceCounts[sample.Source]++
		snapshot.EventWatermark = max(snapshot.EventWatermark, sample.EventSequence)
		if sample.Source != model.ChannelMonitorEventSourceBusiness || !sample.RequestDispatched {
			continue
		}
		if sample.FinalRetrySummary {
			if sample.Outcome == model.ChannelMonitorEventOutcomeFailure {
				snapshot.FinalFailureCount++
			}
			continue
		}
		switch sample.Outcome {
		case model.ChannelMonitorEventOutcomeSuccess:
			snapshot.ActualSuccessCount++
			if sample.IsFinalAttempt {
				snapshot.FinalSuccessCount++
			}
		case model.ChannelMonitorEventOutcomeFailure:
			snapshot.ActualFailureCount++
			if sample.IsFinalAttempt {
				snapshot.FinalFailureCount++
			}
		default:
			continue
		}
		snapshot.BusinessRequestCount++
		if sample.FirstTokenMs != nil {
			snapshot.FirstTokenSampleCount++
			snapshot.FirstTokenTotalMs = channelMonitorRealtimeAddFloat64(snapshot.FirstTokenTotalMs, *sample.FirstTokenMs)
		}
		if sample.TPSOutputTokens != nil && sample.TPSGenerationDurationMs != nil &&
			*sample.TPSOutputTokens > 0 && *sample.TPSGenerationDurationMs > 0 {
			snapshot.TPSSampleCount++
			snapshot.TPSOutputTokens += *sample.TPSOutputTokens
			snapshot.TPSGenerationDurationMs += *sample.TPSGenerationDurationMs
		}
	}
	snapshot.ActualSampleCount = snapshot.ActualSuccessCount + snapshot.ActualFailureCount
	if snapshot.ActualSampleCount > 0 {
		snapshot.ActualSuccessRate = float64(snapshot.ActualSuccessCount) / float64(snapshot.ActualSampleCount)
	}
	snapshot.FinalSampleCount = snapshot.FinalSuccessCount + snapshot.FinalFailureCount
	if snapshot.FinalSampleCount > 0 {
		snapshot.FinalSuccessRate = float64(snapshot.FinalSuccessCount) / float64(snapshot.FinalSampleCount)
	}
	if snapshot.FirstTokenSampleCount > 0 {
		average := snapshot.FirstTokenTotalMs / float64(snapshot.FirstTokenSampleCount)
		snapshot.AverageFirstTokenMs = &average
	}
	if snapshot.TPSSampleCount > 0 {
		if snapshot.TPSOutputTokens > 0 && snapshot.TPSGenerationDurationMs > 0 {
			average := float64(snapshot.TPSOutputTokens) /
				(float64(snapshot.TPSGenerationDurationMs) / 1000.0)
			snapshot.AverageTPS = &average
		}
	}
	if len(samples) > 0 {
		snapshot.WindowStart = samples[0].OccurredAt
		snapshot.WindowEnd = samples[len(samples)-1].OccurredAt
		snapshot.DataCutoffAt = snapshot.WindowEnd
	}
	return snapshot
}
