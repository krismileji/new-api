package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/go-redis/redis/v8"
)

const (
	channelMonitorRedisSharedMinuteTTL        = 48 * time.Hour
	channelMonitorRedisSharedCostStateTTL     = 48 * time.Hour
	channelMonitorRedisSharedEventTTL         = 48 * time.Hour
	channelMonitorRedisSharedOperationTimeout = 5 * time.Second
	channelMonitorRedisSharedWriteRetries     = 128

	channelMonitorRedisSharedDefaultMaxQueryMinutes       = 1441
	channelMonitorRedisSharedDefaultMaxHashFields         = 500_000
	channelMonitorRedisSharedDefaultMaxTotalHashFields    = 5_000_000
	channelMonitorRedisSharedDefaultMaxResponseBytes      = 256 << 20
	channelMonitorRedisSharedDefaultMaxDimensionEntries   = 20_000
	channelMonitorRedisSharedDefaultMaxResponseDimensions = 50_000
	// Kept as an internal compatibility alias for existing service tests.
	channelMonitorRedisSharedMaxQueryMinutes = channelMonitorRedisSharedDefaultMaxQueryMinutes

	channelMonitorRedisSharedHardMaxQueryMinutes       = 1441
	channelMonitorRedisSharedHardMaxHashFields         = 2_000_000
	channelMonitorRedisSharedHardMaxTotalHashFields    = 20_000_000
	channelMonitorRedisSharedHardMaxResponseBytes      = 512 << 20
	channelMonitorRedisSharedHardMaxDimensionEntries   = 100_000
	channelMonitorRedisSharedHardMaxResponseDimensions = 500_000
)

// This hook keeps retry behaviour deterministic in tests while allowing the
// production path to yield between WATCH conflicts.
var channelMonitorRedisSharedRetrySleepOverride func(time.Duration)

func channelMonitorRedisSharedSleep(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	if channelMonitorRedisSharedRetrySleepOverride != nil {
		channelMonitorRedisSharedRetrySleepOverride(delay)
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func channelMonitorRedisSharedRetryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 7 {
		shift = 7
	}
	base := time.Millisecond * time.Duration(1<<shift)
	if base > 250*time.Millisecond {
		base = 250 * time.Millisecond
	}
	// Deterministic pseudo-jitter avoids lock-step retries without introducing
	// a shared random source into the hot path.
	jitter := time.Duration((attempt*7919)%1000) * time.Microsecond
	return base + jitter
}

var ErrChannelMonitorRedisSharedProjectionUnavailable = errors.New("渠道监控 Redis 共享投影不可用")

// ErrChannelMonitorRedisSharedProjectionReplayable identifies a write that
// was not committed after bounded optimistic-concurrency retries. Callers may
// safely replay the same logical batch because event markers are written only
// after all projection mutations have committed.
var ErrChannelMonitorRedisSharedProjectionReplayable = errors.New("渠道监控 Redis 共享投影写入未提交，可重放")

type channelMonitorRedisSharedProjectionRetryError struct {
	Attempts int
}

func (err *channelMonitorRedisSharedProjectionRetryError) Error() string {
	return fmt.Sprintf("%s: attempts=%d", ErrChannelMonitorRedisSharedProjectionReplayable, err.Attempts)
}

func (err *channelMonitorRedisSharedProjectionRetryError) Unwrap() error {
	return ErrChannelMonitorRedisSharedProjectionReplayable
}

// ErrChannelMonitorRedisSharedProjectionLimitExceeded identifies a query that
// was rejected before its response could exceed the configured read budget.
var ErrChannelMonitorRedisSharedProjectionLimitExceeded = errors.New("渠道监控 Redis 共享投影读取规模超过上限")

// ChannelMonitorRedisSharedProjectionLimitError describes which read budget
// prevented a projection query from completing.
type ChannelMonitorRedisSharedProjectionLimitError struct {
	Resource string
	Limit    int64
	Actual   int64
}

func (err *ChannelMonitorRedisSharedProjectionLimitError) Error() string {
	return fmt.Sprintf("%s: %s=%d, limit=%d", ErrChannelMonitorRedisSharedProjectionLimitExceeded, err.Resource, err.Actual, err.Limit)
}

func (err *ChannelMonitorRedisSharedProjectionLimitError) Unwrap() error {
	return ErrChannelMonitorRedisSharedProjectionLimitExceeded
}

// ChannelMonitorRedisSharedProjectionLimits bounds the amount of Redis data
// and response dimensions a single projection query may materialize. Values
// are normalized to safe hard ceilings by constructors and SetLimits.
type ChannelMonitorRedisSharedProjectionLimits struct {
	MaxQueryMinutes       int
	MaxHashFields         int
	MaxTotalHashFields    int
	MaxResponseBytes      int64
	MaxDimensionEntries   int
	MaxResponseDimensions int
}

func defaultChannelMonitorRedisSharedProjectionLimits() ChannelMonitorRedisSharedProjectionLimits {
	return ChannelMonitorRedisSharedProjectionLimits{
		MaxQueryMinutes:       channelMonitorRedisSharedDefaultMaxQueryMinutes,
		MaxHashFields:         channelMonitorRedisSharedDefaultMaxHashFields,
		MaxTotalHashFields:    channelMonitorRedisSharedDefaultMaxTotalHashFields,
		MaxResponseBytes:      channelMonitorRedisSharedDefaultMaxResponseBytes,
		MaxDimensionEntries:   channelMonitorRedisSharedDefaultMaxDimensionEntries,
		MaxResponseDimensions: channelMonitorRedisSharedDefaultMaxResponseDimensions,
	}
}

func channelMonitorRedisSharedProjectionLimitsFromEnv() ChannelMonitorRedisSharedProjectionLimits {
	limits := defaultChannelMonitorRedisSharedProjectionLimits()
	limits.MaxQueryMinutes = common.GetEnvOrDefault("CHANNEL_MONITOR_REDIS_SHARED_MAX_QUERY_MINUTES", limits.MaxQueryMinutes)
	limits.MaxHashFields = common.GetEnvOrDefault("CHANNEL_MONITOR_REDIS_SHARED_MAX_HASH_FIELDS", limits.MaxHashFields)
	limits.MaxTotalHashFields = common.GetEnvOrDefault("CHANNEL_MONITOR_REDIS_SHARED_MAX_TOTAL_HASH_FIELDS", limits.MaxTotalHashFields)
	limits.MaxResponseBytes = int64(common.GetEnvOrDefault("CHANNEL_MONITOR_REDIS_SHARED_MAX_RESPONSE_BYTES", int(limits.MaxResponseBytes)))
	limits.MaxDimensionEntries = common.GetEnvOrDefault("CHANNEL_MONITOR_REDIS_SHARED_MAX_DIMENSION_ENTRIES", limits.MaxDimensionEntries)
	limits.MaxResponseDimensions = common.GetEnvOrDefault("CHANNEL_MONITOR_REDIS_SHARED_MAX_RESPONSE_DIMENSIONS", limits.MaxResponseDimensions)
	return normalizeChannelMonitorRedisSharedProjectionLimits(limits)
}

func normalizeChannelMonitorRedisSharedProjectionLimits(limits ChannelMonitorRedisSharedProjectionLimits) ChannelMonitorRedisSharedProjectionLimits {
	defaults := defaultChannelMonitorRedisSharedProjectionLimits()
	if limits.MaxQueryMinutes <= 0 {
		limits.MaxQueryMinutes = defaults.MaxQueryMinutes
	}
	if limits.MaxQueryMinutes > channelMonitorRedisSharedHardMaxQueryMinutes {
		limits.MaxQueryMinutes = channelMonitorRedisSharedHardMaxQueryMinutes
	}
	if limits.MaxHashFields <= 0 {
		limits.MaxHashFields = defaults.MaxHashFields
	}
	if limits.MaxHashFields > channelMonitorRedisSharedHardMaxHashFields {
		limits.MaxHashFields = channelMonitorRedisSharedHardMaxHashFields
	}
	if limits.MaxTotalHashFields <= 0 {
		limits.MaxTotalHashFields = defaults.MaxTotalHashFields
	}
	if limits.MaxTotalHashFields > channelMonitorRedisSharedHardMaxTotalHashFields {
		limits.MaxTotalHashFields = channelMonitorRedisSharedHardMaxTotalHashFields
	}
	if limits.MaxResponseBytes <= 0 {
		limits.MaxResponseBytes = defaults.MaxResponseBytes
	}
	if limits.MaxResponseBytes > channelMonitorRedisSharedHardMaxResponseBytes {
		limits.MaxResponseBytes = channelMonitorRedisSharedHardMaxResponseBytes
	}
	if limits.MaxDimensionEntries <= 0 {
		limits.MaxDimensionEntries = defaults.MaxDimensionEntries
	}
	if limits.MaxDimensionEntries > channelMonitorRedisSharedHardMaxDimensionEntries {
		limits.MaxDimensionEntries = channelMonitorRedisSharedHardMaxDimensionEntries
	}
	if limits.MaxResponseDimensions <= 0 {
		limits.MaxResponseDimensions = defaults.MaxResponseDimensions
	}
	if limits.MaxResponseDimensions > channelMonitorRedisSharedHardMaxResponseDimensions {
		limits.MaxResponseDimensions = channelMonitorRedisSharedHardMaxResponseDimensions
	}
	if limits.MaxTotalHashFields < limits.MaxHashFields {
		limits.MaxTotalHashFields = limits.MaxHashFields
	}
	if limits.MaxResponseDimensions < limits.MaxDimensionEntries {
		limits.MaxResponseDimensions = limits.MaxDimensionEntries
	}
	return limits
}

const (
	channelMonitorRedisSharedScopeGlobal      = "global"
	channelMonitorRedisSharedScopeChannel     = "channel"
	channelMonitorRedisSharedScopeModel       = "model"
	channelMonitorRedisSharedScopeGroup       = "group"
	channelMonitorRedisSharedScopeAPIKey      = "apikey"
	channelMonitorRedisSharedScopePerformance = "performance"
	channelMonitorRedisSharedScopeRoute       = "route"
	channelMonitorRedisSharedScopeGroupRoute  = "group_channel"
	channelMonitorRedisSharedScopeAPIKeyRoute = "apikey_scope"
	channelMonitorRedisSharedScopeFailure     = "failure"
	channelMonitorRedisSharedScopeMetadata    = "meta"

	channelMonitorRedisSharedMetricEventCount             = "event_count"
	channelMonitorRedisSharedMetricBusinessRequests       = "business_request_count"
	channelMonitorRedisSharedMetricActualSuccess          = "actual_success_count"
	channelMonitorRedisSharedMetricActualFailure          = "actual_failure_count"
	channelMonitorRedisSharedMetricFinalSuccess           = "final_success_count"
	channelMonitorRedisSharedMetricFinalFailure           = "final_failure_count"
	channelMonitorRedisSharedMetricFirstTokenSamples      = "first_token_sample_count"
	channelMonitorRedisSharedMetricFirstTokenTotalMs      = "first_token_total_ms"
	channelMonitorRedisSharedMetricAttemptDurationSamples = "attempt_duration_sample_count"
	channelMonitorRedisSharedMetricAttemptDurationTotalMs = "attempt_duration_total_ms"
	channelMonitorRedisSharedMetricTPSSamples             = "tps_sample_count_v2"
	channelMonitorRedisSharedMetricTPSOutputTokens        = "tps_output_tokens_v2"
	channelMonitorRedisSharedMetricTPSGenerationMs        = "tps_generation_duration_ms_v2"
	channelMonitorRedisSharedMetricCacheSamples           = "cache_sample_count"
	channelMonitorRedisSharedMetricCacheHits              = "cache_hit_count"
	channelMonitorRedisSharedMetricCacheReadTokens        = "cache_read_tokens"
	channelMonitorRedisSharedMetricCacheWriteRequests     = "cache_write_request_count"
	channelMonitorRedisSharedMetricCacheWriteTokens       = "cache_write_tokens"
	channelMonitorRedisSharedMetricInputTokens            = "input_tokens"
	channelMonitorRedisSharedMetricSettledCost            = "settled_cost_nano_cny"
	channelMonitorRedisSharedMetricSettledRequests        = "settled_request_count"
	channelMonitorRedisSharedMetricUnresolvedCost         = "unresolved_cost_nano_cny"
	channelMonitorRedisSharedMetricUnresolvedRequests     = "unresolved_request_count"
	channelMonitorRedisSharedMetricAPIKeyName             = "api_key_name"
	channelMonitorRedisSharedMetricProbeSettledCost       = "probe_settled_cost_nano_cny"
	channelMonitorRedisSharedMetricGroupProbeSettledCost  = "group_probe_settled_cost_nano_cny"
	channelMonitorRedisSharedMetricDetectionSettledCost   = "model_detection_settled_cost_nano_cny"
	channelMonitorRedisSharedMetricLastUsedTime           = "last_used_time"
	channelMonitorRedisSharedMetricLatestFirstToken       = "latest_first_token_ms"
	channelMonitorRedisSharedMetricLatestFirstTokenAt     = "latest_first_token_at"
	channelMonitorRedisSharedMetricLatestFirstTokenSeq    = "latest_first_token_sequence"
	channelMonitorRedisSharedMetricLatestTPS              = "latest_tps"
	channelMonitorRedisSharedMetricLatestTPSAt            = "latest_tps_at"
	channelMonitorRedisSharedMetricLatestTPSSeq           = "latest_tps_sequence"
	channelMonitorRedisSharedMetricFailureActual          = "actual_count"
	channelMonitorRedisSharedMetricFailureFinal           = "final_count"
	channelMonitorRedisSharedMetricFailureLast            = "last_occurred_at"
	channelMonitorRedisSharedMetricFailureLastSeq         = "last_event_sequence"
	channelMonitorRedisSharedMetricFailureSample          = "sample_content"
	channelMonitorRedisSharedMetricDataCutoffAt           = "data_cutoff_at"
	channelMonitorRedisSharedMetricProcessedAt            = "processed_at"
	channelMonitorRedisSharedMetricEventWatermark         = "event_watermark"

	channelMonitorRedisSharedCostStatus        = "cost_status"
	channelMonitorRedisSharedCostSettled       = "settled_cost_nano_cny"
	channelMonitorRedisSharedCostUnresolved    = "unresolved_cost_nano_cny"
	channelMonitorRedisSharedCostDayStart      = "day_start"
	channelMonitorRedisSharedCostSource        = "source"
	channelMonitorRedisSharedCostMinuteStart   = "minute_start"
	channelMonitorRedisSharedCostChannelID     = "channel_id"
	channelMonitorRedisSharedCostModel         = "model"
	channelMonitorRedisSharedCostGroup         = "group"
	channelMonitorRedisSharedCostAPIKeyID      = "api_key_id"
	channelMonitorRedisSharedCostAPIKeyName    = "api_key_name"
	channelMonitorRedisSharedCostEventSequence = "event_sequence"
	channelMonitorRedisSharedCostCreatedAt     = "created_at"
	channelMonitorRedisSharedCostEventID       = "event_id"
)

// ChannelMonitorRedisSharedAggregate contains compact counters, costs and
// latency values. Failure samples are bounded separately per category.
type ChannelMonitorRedisSharedAggregate struct {
	APIKeyName string `json:"api_key_name,omitempty"`

	EventCount           int64 `json:"event_count"`
	BusinessRequestCount int64 `json:"business_request_count"`
	ActualSuccessCount   int64 `json:"actual_success_count"`
	ActualFailureCount   int64 `json:"actual_failure_count"`
	FinalSuccessCount    int64 `json:"final_success_count"`
	FinalFailureCount    int64 `json:"final_failure_count"`

	FirstTokenSampleCount      int64   `json:"first_token_sample_count"`
	FirstTokenTotalMs          float64 `json:"first_token_total_ms"`
	AttemptDurationSampleCount int64   `json:"attempt_duration_sample_count"`
	AttemptDurationTotalMs     int64   `json:"attempt_duration_total_ms"`
	TPSSampleCount             int64   `json:"tps_sample_count"`
	TPSOutputTokens            int64   `json:"tps_output_tokens"`
	TPSGenerationDurationMs    int64   `json:"tps_generation_duration_ms"`

	CacheSampleCount       int64 `json:"cache_sample_count"`
	CacheHitCount          int64 `json:"cache_hit_count"`
	CacheReadTokens        int64 `json:"cache_read_tokens"`
	CacheWriteRequestCount int64 `json:"cache_write_request_count"`
	CacheWriteTokens       int64 `json:"cache_write_tokens"`
	InputTokens            int64 `json:"input_tokens"`

	SettledCostNanoCNY               int64 `json:"settled_cost_nano_cny"`
	SettledRequestCount              int64 `json:"settled_request_count"`
	UnresolvedCostNanoCNY            int64 `json:"unresolved_cost_nano_cny"`
	UnresolvedRequestCount           int64 `json:"unresolved_request_count"`
	ProbeSettledCostNanoCNY          int64 `json:"probe_settled_cost_nano_cny"`
	GroupProbeSettledCostNanoCNY     int64 `json:"group_probe_settled_cost_nano_cny"`
	ModelDetectionSettledCostNanoCNY int64 `json:"model_detection_settled_cost_nano_cny"`

	LatestFirstTokenMs *float64 `json:"latest_first_token_ms,omitempty"`
	LatestTPS          *float64 `json:"latest_tps,omitempty"`
	LastUsedTime       int64    `json:"last_used_time"`

	latestFirstTokenAt       int64
	latestFirstTokenSequence uint64
	latestTPSAt              int64
	latestTPSSequence        uint64
}

type ChannelMonitorRedisSharedRouteAggregate struct {
	ChannelID int    `json:"channel_id"`
	ModelName string `json:"model"`
	ChannelMonitorRedisSharedAggregate
}

type ChannelMonitorRedisSharedGroupChannelAggregate struct {
	GroupName string `json:"group"`
	ChannelID int    `json:"channel_id"`
	ChannelMonitorRedisSharedAggregate
}

type ChannelMonitorRedisSharedAPIKeyScopeAggregate struct {
	APIKeyID  int    `json:"api_key_id"`
	ChannelID int    `json:"channel_id"`
	ModelName string `json:"model"`
	GroupName string `json:"group"`
	ChannelMonitorRedisSharedAggregate
}

type ChannelMonitorRedisSharedFailureCategory struct {
	ChannelID     int    `json:"channel_id"`
	ModelName     string `json:"model"`
	GroupName     string `json:"group"`
	StatusCode    int    `json:"status_code"`
	ErrorType     string `json:"error_type"`
	ErrorCode     string `json:"error_code"`
	SampleContent string `json:"sample_content"`
	ActualCount   int64  `json:"actual_count"`
	FinalCount    int64  `json:"final_count"`
	LastOccurred  int64  `json:"last_occurred_at"`

	lastEventSequence uint64
}

type ChannelMonitorRedisSharedDailyCostView struct {
	Global   ChannelMonitorRedisSharedAggregate            `json:"global"`
	Channels map[int]ChannelMonitorRedisSharedAggregate    `json:"channels"`
	Models   map[string]ChannelMonitorRedisSharedAggregate `json:"models"`
	Groups   map[string]ChannelMonitorRedisSharedAggregate `json:"groups"`
	APIKeys  map[int]ChannelMonitorRedisSharedAggregate    `json:"api_keys"`
}

// ChannelMonitorRedisSharedProjectionView is the read model returned by the
// independent REDIS-05 query API.
type ChannelMonitorRedisSharedProjectionView struct {
	Summary        ChannelMonitorRedisSharedAggregate               `json:"summary"`
	Performance    ChannelMonitorRedisSharedAggregate               `json:"performance"`
	Channels       map[int]ChannelMonitorRedisSharedAggregate       `json:"channels"`
	Models         map[string]ChannelMonitorRedisSharedAggregate    `json:"models"`
	Groups         map[string]ChannelMonitorRedisSharedAggregate    `json:"groups"`
	APIKeys        map[int]ChannelMonitorRedisSharedAggregate       `json:"api_keys"`
	Routes         []ChannelMonitorRedisSharedRouteAggregate        `json:"routes"`
	GroupChannels  []ChannelMonitorRedisSharedGroupChannelAggregate `json:"group_channels"`
	APIKeyScopes   []ChannelMonitorRedisSharedAPIKeyScopeAggregate  `json:"api_key_scopes"`
	Failures       []ChannelMonitorRedisSharedFailureCategory       `json:"failure_categories"`
	DailyCosts     map[int64]ChannelMonitorRedisSharedDailyCostView `json:"daily_costs"`
	WindowStart    int64                                            `json:"window_start"`
	WindowEnd      int64                                            `json:"window_end"`
	DataCutoffAt   int64                                            `json:"data_cutoff_at"`
	ProcessedAt    int64                                            `json:"processed_at"`
	EventWatermark uint64                                           `json:"event_watermark"`
}

// ChannelMonitorRedisSharedProjection is a Redis-backed compact projection.
// It is an independent REDIS-03 handler and is not installed into the
// existing local queue or query path by this plan.
type ChannelMonitorRedisSharedProjection struct {
	client *redis.Client
	now    func() time.Time
	limits ChannelMonitorRedisSharedProjectionLimits
}

const channelMonitorRedisSharedMetadataMemoTTL = 2 * time.Second

type channelMonitorRedisSharedMetadataMemoKey struct {
	client                 *redis.Client
	windowStart, windowEnd int64
}

type channelMonitorRedisSharedMetadataMemoValue struct {
	dataCutoffAt, processedAt int64
	eventWatermark            uint64
	expiresAt                 time.Time
}

var channelMonitorRedisSharedMetadataMemo = struct {
	sync.Mutex
	values map[channelMonitorRedisSharedMetadataMemoKey]channelMonitorRedisSharedMetadataMemoValue
}{values: make(map[channelMonitorRedisSharedMetadataMemoKey]channelMonitorRedisSharedMetadataMemoValue)}

func channelMonitorRedisSharedRememberMetadata(client *redis.Client, view ChannelMonitorRedisSharedProjectionView) {
	if client == nil {
		return
	}
	channelMonitorRedisSharedMetadataMemo.Lock()
	defer channelMonitorRedisSharedMetadataMemo.Unlock()
	now := time.Now()
	for key, value := range channelMonitorRedisSharedMetadataMemo.values {
		if !value.expiresAt.After(now) {
			delete(channelMonitorRedisSharedMetadataMemo.values, key)
		}
	}
	channelMonitorRedisSharedMetadataMemo.values[channelMonitorRedisSharedMetadataMemoKey{
		client: client, windowStart: view.WindowStart, windowEnd: view.WindowEnd,
	}] = channelMonitorRedisSharedMetadataMemoValue{
		dataCutoffAt: view.DataCutoffAt, processedAt: view.ProcessedAt,
		eventWatermark: view.EventWatermark, expiresAt: now.Add(channelMonitorRedisSharedMetadataMemoTTL),
	}
}

func channelMonitorRedisSharedLookupMetadata(client *redis.Client, windowStart, windowEnd int64) (int64, int64, uint64, bool) {
	if client == nil {
		return 0, 0, 0, false
	}
	channelMonitorRedisSharedMetadataMemo.Lock()
	defer channelMonitorRedisSharedMetadataMemo.Unlock()
	key := channelMonitorRedisSharedMetadataMemoKey{client: client, windowStart: windowStart, windowEnd: windowEnd}
	value, ok := channelMonitorRedisSharedMetadataMemo.values[key]
	if !ok {
		return 0, 0, 0, false
	}
	if !value.expiresAt.After(time.Now()) {
		delete(channelMonitorRedisSharedMetadataMemo.values, key)
		return 0, 0, 0, false
	}
	return value.dataCutoffAt, value.processedAt, value.eventWatermark, true
}

// SetLimits replaces the read budgets used by subsequent queries. Callers
// should configure a projection before sharing it between goroutines.
func (projection *ChannelMonitorRedisSharedProjection) SetLimits(limits ChannelMonitorRedisSharedProjectionLimits) {
	if projection == nil {
		return
	}
	projection.limits = normalizeChannelMonitorRedisSharedProjectionLimits(limits)
}

var _ ChannelMonitorRedisEventHandler = (*ChannelMonitorRedisSharedProjection)(nil)

func NewChannelMonitorRedisSharedProjection() (*ChannelMonitorRedisSharedProjection, error) {
	client := common.RedisMonitorReadClient()
	if !common.RedisEnabled || client == nil {
		return nil, ErrChannelMonitorRedisSharedProjectionUnavailable
	}
	return NewChannelMonitorRedisSharedProjectionWithClient(client), nil
}

func NewChannelMonitorRedisSharedProjectionWithClient(client *redis.Client) *ChannelMonitorRedisSharedProjection {
	return &ChannelMonitorRedisSharedProjection{client: client, now: time.Now, limits: channelMonitorRedisSharedProjectionLimitsFromEnv()}
}

// HandleChannelMonitorEvents implements ChannelMonitorRedisEventHandler from
// REDIS-03. The caller supplies an already deduplicated logical batch.
func (projection *ChannelMonitorRedisSharedProjection) HandleChannelMonitorEvents(ctx context.Context, events []model.ChannelMonitorEvent) error {
	return projection.WriteChannelMonitorEvents(ctx, events)
}

func (projection *ChannelMonitorRedisSharedProjection) WriteChannelMonitorEvents(ctx context.Context, events []model.ChannelMonitorEvent) error {
	if projection == nil || projection.client == nil {
		return ErrChannelMonitorRedisSharedProjectionUnavailable
	}
	if len(events) == 0 {
		return nil
	}
	normalizedEvents := make([]model.ChannelMonitorEvent, len(events))
	for index, event := range events {
		if err := event.Validate(); err != nil {
			return err
		}
		event.ModelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(event.ModelName))
		event.GroupName = strings.TrimSpace(event.GroupName)
		event.APIKeyName = strings.TrimSpace(event.APIKeyName)
		event.ErrorType = strings.TrimSpace(event.ErrorType)
		event.ErrorCode = strings.TrimSpace(event.ErrorCode)
		event.ErrorMessage = strings.TrimSpace(event.ErrorMessage)
		normalizedEvents[index] = event
	}
	events = normalizedEvents
	if ctx == nil {
		ctx = context.Background()
	}
	processedAt := projection.now().Unix()
	opCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisSharedOperationTimeout)
	defer cancel()

	eventKeys := make([]string, 0, len(events))
	costIDs := make([]string, 0)
	seenCostIDs := make(map[string]struct{})
	for _, event := range events {
		eventKeys = append(eventKeys, ChannelMonitorRedisSharedEventKey(event.EventId))
		if costID := channelMonitorRealtimeCostEventId(event); costID != "" {
			if _, seen := seenCostIDs[costID]; !seen {
				seenCostIDs[costID] = struct{}{}
				costIDs = append(costIDs, costID)
			}
		}
	}
	watchKeys := append([]string(nil), eventKeys...)
	for _, costID := range costIDs {
		watchKeys = append(watchKeys, ChannelMonitorRedisCostEventStateKey(costID))
	}
	for attempt := 1; attempt <= channelMonitorRedisSharedWriteRetries; attempt++ {
		err := projection.client.Watch(opCtx, func(tx *redis.Tx) error {
			applied, err := tx.MGet(opCtx, eventKeys...).Result()
			if err != nil {
				return err
			}
			states, err := projection.loadCostStates(opCtx, tx, costIDs)
			if err != nil {
				return err
			}
			// The cost state determines which day/minute hashes may be corrected.
			// Add those keys to the same optimistic-concurrency boundary before
			// validating or queuing any writes.
			writeHashKeys, markerKeys := channelMonitorRedisSharedWriteKeys(events, states)
			if len(writeHashKeys) > 0 {
				if err := tx.Watch(opCtx, writeHashKeys...).Err(); err != nil {
					return err
				}
			}
			if err := channelMonitorRedisSharedValidateWriteKeys(opCtx, tx, writeHashKeys, markerKeys); err != nil {
				return err
			}
			seenEventIDs := make(map[string]struct{}, len(events))
			_, err = tx.TxPipelined(opCtx, func(pipe redis.Pipeliner) error {
				for index, event := range events {
					if applied[index] != nil {
						continue
					}
					if _, duplicate := seenEventIDs[event.EventId]; duplicate {
						continue
					}
					seenEventIDs[event.EventId] = struct{}{}
					costEventIsCurrent := true
					if !event.FinalRetrySummary {
						costID := channelMonitorRealtimeCostEventId(event)
						if costID != "" {
							state, exists := states[costID]
							if exists && !channelMonitorRedisSharedCostEventIsNewer(state, event) {
								costEventIsCurrent = false
							} else {
								if exists {
									projection.appendCostDelta(opCtx, pipe, state, -1, state.Source == string(model.ChannelMonitorEventSourceBusiness))
								}
								if event.CostStatus != model.ChannelMonitorEventCostNone {
									current := channelMonitorRedisSharedCostStateFromEvent(costID, event)
									projection.appendCostDelta(opCtx, pipe, current, 1, event.Source == model.ChannelMonitorEventSourceBusiness)
									projection.setCostState(opCtx, pipe, costID, current)
									states[costID] = current
								} else {
									projection.deleteCostState(opCtx, pipe, costID)
									delete(states, costID)
								}
							}
						} else if event.CostStatus != model.ChannelMonitorEventCostNone {
							projection.appendCostDelta(opCtx, pipe, channelMonitorRedisSharedCostStateFromEvent("", event), 1, event.Source == model.ChannelMonitorEventSourceBusiness)
						}
					}
					if event.Source == model.ChannelMonitorEventSourceBusiness && (event.FinalRetrySummary || costEventIsCurrent) {
						projection.appendDashboardEventDelta(opCtx, pipe, event)
					}
					projection.appendMetadata(opCtx, pipe, event, processedAt)
					pipe.Set(opCtx, eventKeys[index], "1", channelMonitorRedisSharedEventTTL)
				}
				return nil
			})
			return err
		}, watchKeys...)
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
		if attempt < channelMonitorRedisSharedWriteRetries {
			if sleepErr := channelMonitorRedisSharedSleep(opCtx, channelMonitorRedisSharedRetryBackoff(attempt)); sleepErr != nil {
				return sleepErr
			}
		}
	}
	return &channelMonitorRedisSharedProjectionRetryError{Attempts: channelMonitorRedisSharedWriteRetries}
}

// channelMonitorRedisSharedWriteKeys returns every hash and marker key that a
// batch can touch. The keys are watched before the transaction is queued so a
// concurrent writer cannot invalidate the write-time validation snapshot.
func channelMonitorRedisSharedWriteKeys(events []model.ChannelMonitorEvent, states map[string]channelMonitorRedisSharedCostState) ([]string, []string) {
	hashSet := make(map[string]struct{})
	markerSet := make(map[string]struct{}, len(events))
	addDashboard := func(minute int64) { hashSet[ChannelMonitorRedisDashboardMinuteKey(minute)] = struct{}{} }
	addCost := func(state channelMonitorRedisSharedCostState) {
		hashSet[ChannelMonitorRedisCostDayKey(state.DayStart)] = struct{}{}
		if state.MinuteStart != 0 {
			addDashboard(state.MinuteStart)
		}
	}
	for _, event := range events {
		minute := event.OccurredAt - event.OccurredAt%60
		addDashboard(minute) // metadata is written for every event
		markerSet[ChannelMonitorRedisSharedEventKey(event.EventId)] = struct{}{}
		if costID := channelMonitorRealtimeCostEventId(event); costID != "" {
			if state, ok := states[costID]; ok {
				addCost(state)
			}
		}
		if !event.FinalRetrySummary && event.CostStatus != model.ChannelMonitorEventCostNone {
			addCost(channelMonitorRedisSharedCostStateFromEvent("", event))
		}
	}
	hashKeys := make([]string, 0, len(hashSet))
	for key := range hashSet {
		hashKeys = append(hashKeys, key)
	}
	markerKeys := make([]string, 0, len(markerSet))
	for key := range markerSet {
		markerKeys = append(markerKeys, key)
	}
	sort.Strings(hashKeys)
	sort.Strings(markerKeys)
	return hashKeys, markerKeys
}

var channelMonitorRedisSharedIntegerMetrics = map[string]struct{}{
	channelMonitorRedisSharedMetricEventCount: {}, channelMonitorRedisSharedMetricBusinessRequests: {},
	channelMonitorRedisSharedMetricActualSuccess: {}, channelMonitorRedisSharedMetricActualFailure: {},
	channelMonitorRedisSharedMetricFinalSuccess: {}, channelMonitorRedisSharedMetricFinalFailure: {},
	channelMonitorRedisSharedMetricFirstTokenSamples: {}, channelMonitorRedisSharedMetricAttemptDurationSamples: {},
	channelMonitorRedisSharedMetricAttemptDurationTotalMs: {}, channelMonitorRedisSharedMetricTPSSamples: {},
	channelMonitorRedisSharedMetricTPSOutputTokens: {}, channelMonitorRedisSharedMetricTPSGenerationMs: {},
	channelMonitorRedisSharedMetricCacheSamples: {}, channelMonitorRedisSharedMetricCacheHits: {},
	channelMonitorRedisSharedMetricCacheReadTokens: {}, channelMonitorRedisSharedMetricCacheWriteRequests: {},
	channelMonitorRedisSharedMetricCacheWriteTokens: {}, channelMonitorRedisSharedMetricInputTokens: {},
	channelMonitorRedisSharedMetricSettledCost: {}, channelMonitorRedisSharedMetricSettledRequests: {},
	channelMonitorRedisSharedMetricUnresolvedCost: {}, channelMonitorRedisSharedMetricUnresolvedRequests: {},
	channelMonitorRedisSharedMetricProbeSettledCost: {}, channelMonitorRedisSharedMetricGroupProbeSettledCost: {},
	channelMonitorRedisSharedMetricDetectionSettledCost: {}, channelMonitorRedisSharedMetricLastUsedTime: {},
	channelMonitorRedisSharedMetricLatestFirstTokenAt: {}, channelMonitorRedisSharedMetricLatestTPSAt: {},
	channelMonitorRedisSharedMetricFailureActual: {}, channelMonitorRedisSharedMetricFailureFinal: {},
	channelMonitorRedisSharedMetricFailureLast: {}, channelMonitorRedisSharedMetricDataCutoffAt: {},
	channelMonitorRedisSharedMetricProcessedAt: {}, channelMonitorRedisSharedMetricEventWatermark: {},
}

var channelMonitorRedisSharedFloatMetrics = map[string]struct{}{
	channelMonitorRedisSharedMetricFirstTokenTotalMs: {}, channelMonitorRedisSharedMetricLatestFirstToken: {},
	channelMonitorRedisSharedMetricLatestTPS: {},
}

var channelMonitorRedisSharedCostIntegerFields = map[string]struct{}{
	channelMonitorRedisSharedCostSettled: {}, channelMonitorRedisSharedCostUnresolved: {},
	channelMonitorRedisSharedCostDayStart: {}, channelMonitorRedisSharedCostMinuteStart: {},
	channelMonitorRedisSharedCostChannelID: {}, channelMonitorRedisSharedCostAPIKeyID: {},
	channelMonitorRedisSharedCostEventSequence: {}, channelMonitorRedisSharedCostCreatedAt: {},
}

func channelMonitorRedisSharedValidateWriteKeys(ctx context.Context, tx *redis.Tx, hashKeys, markerKeys []string) error {
	for _, key := range markerKeys {
		typeName, err := tx.Type(ctx, key).Result()
		if err != nil {
			return err
		}
		if typeName != "none" && typeName != "string" {
			return fmt.Errorf("渠道监控 Redis 事件 marker 类型无效: key=%s type=%s", key, typeName)
		}
	}
	for _, key := range hashKeys {
		typeName, err := tx.Type(ctx, key).Result()
		if err != nil {
			return err
		}
		if typeName != "none" && typeName != "hash" {
			return fmt.Errorf("渠道监控 Redis 投影键类型无效: key=%s type=%s", key, typeName)
		}
		values, err := tx.HGetAll(ctx, key).Result()
		if err != nil {
			return err
		}
		for field, raw := range values {
			metric := field
			if index := strings.LastIndexByte(field, ':'); index >= 0 {
				metric = field[index+1:]
			}
			if strings.HasPrefix(key, ChannelMonitorRedisCostProjectionPrefix+"event:") {
				if _, ok := channelMonitorRedisSharedCostIntegerFields[field]; !ok {
					continue
				}
				var parseErr error
				if field == channelMonitorRedisSharedCostEventSequence {
					_, parseErr = strconv.ParseUint(raw, 10, 64)
				} else {
					_, parseErr = strconv.ParseInt(raw, 10, 64)
				}
				if parseErr != nil {
					return fmt.Errorf("渠道监控 Redis 成本状态数值无效: key=%s field=%s: %w", key, field, parseErr)
				}
				continue
			}
			if _, ok := channelMonitorRedisSharedFloatMetrics[metric]; ok {
				value, err := strconv.ParseFloat(raw, 64)
				if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
					if err == nil {
						err = errors.New("非有限数值")
					}
					return fmt.Errorf("渠道监控 Redis 投影浮点数值无效: key=%s field=%s: %w", key, field, err)
				}
				continue
			}
			if _, ok := channelMonitorRedisSharedIntegerMetrics[metric]; ok {
				if _, err := strconv.ParseInt(raw, 10, 64); err != nil {
					return fmt.Errorf("渠道监控 Redis 投影整数数值无效: key=%s field=%s: %w", key, field, err)
				}
			}
		}
	}
	return nil
}

func (projection *ChannelMonitorRedisSharedProjection) loadCostStates(
	ctx context.Context,
	tx *redis.Tx,
	costIDs []string,
) (map[string]channelMonitorRedisSharedCostState, error) {
	states := make(map[string]channelMonitorRedisSharedCostState, len(costIDs))
	if len(costIDs) == 0 {
		return states, nil
	}
	pipe := tx.Pipeline()
	commands := make(map[string]*redis.StringStringMapCmd, len(costIDs))
	for _, costID := range costIDs {
		commands[costID] = pipe.HGetAll(ctx, ChannelMonitorRedisCostEventStateKey(costID))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	for costID, command := range commands {
		values, err := command.Result()
		if err != nil {
			return nil, err
		}
		if len(values) == 0 {
			continue
		}
		state, err := parseChannelMonitorRedisSharedCostState(values)
		if err != nil {
			return nil, fmt.Errorf("读取渠道监控成本状态 %s 失败: %w", costID, err)
		}
		states[costID] = state
	}
	return states, nil
}

type channelMonitorRedisSharedCostState struct {
	CostID         string
	Status         model.ChannelMonitorEventCostStatus
	SettledCost    int64
	UnresolvedCost int64
	DayStart       int64
	Source         string
	MinuteStart    int64
	ChannelID      int
	Model          string
	Group          string
	APIKeyID       int
	APIKeyName     string
	EventSequence  uint64
	CreatedAt      int64
	EventID        string
}

func channelMonitorRedisSharedCostStateFromEvent(costID string, event model.ChannelMonitorEvent) channelMonitorRedisSharedCostState {
	return channelMonitorRedisSharedCostState{
		CostID: costID, Status: event.CostStatus, SettledCost: event.SettledCostNanoCNY,
		UnresolvedCost: event.UnresolvedCostNanoCNY, DayStart: model.ChannelDailyCostDayStart(event.OccurredAt),
		Source: string(event.Source), MinuteStart: event.OccurredAt - event.OccurredAt%60,
		ChannelID: event.ChannelId, Model: ratio_setting.FormatMatchingModelName(strings.TrimSpace(event.ModelName)), Group: strings.TrimSpace(event.GroupName),
		APIKeyID: event.APIKeyId, APIKeyName: strings.TrimSpace(event.APIKeyName),
		EventSequence: event.EventSequence, CreatedAt: event.CreatedAt, EventID: event.EventId,
	}
}

func parseChannelMonitorRedisSharedCostState(values map[string]string) (channelMonitorRedisSharedCostState, error) {
	state := channelMonitorRedisSharedCostState{Status: model.ChannelMonitorEventCostStatus(values[channelMonitorRedisSharedCostStatus])}
	var err error
	state.SettledCost, err = parseInt64Field(values, channelMonitorRedisSharedCostSettled)
	if err != nil {
		return state, err
	}
	state.UnresolvedCost, err = parseInt64Field(values, channelMonitorRedisSharedCostUnresolved)
	if err != nil {
		return state, err
	}
	state.DayStart, err = parseInt64Field(values, channelMonitorRedisSharedCostDayStart)
	if err != nil {
		return state, err
	}
	state.MinuteStart, err = parseInt64Field(values, channelMonitorRedisSharedCostMinuteStart)
	if err != nil {
		return state, err
	}
	state.ChannelID, err = parseIntField(values, channelMonitorRedisSharedCostChannelID)
	if err != nil {
		return state, err
	}
	state.APIKeyID, err = parseIntField(values, channelMonitorRedisSharedCostAPIKeyID)
	if err != nil {
		return state, err
	}
	state.EventSequence, err = parseUint64Field(values, channelMonitorRedisSharedCostEventSequence)
	if err != nil {
		return state, err
	}
	state.CreatedAt, err = parseInt64Field(values, channelMonitorRedisSharedCostCreatedAt)
	if err != nil {
		return state, err
	}
	state.Source = values[channelMonitorRedisSharedCostSource]
	state.Model = values[channelMonitorRedisSharedCostModel]
	state.Group = values[channelMonitorRedisSharedCostGroup]
	state.APIKeyName = values[channelMonitorRedisSharedCostAPIKeyName]
	state.EventID = values[channelMonitorRedisSharedCostEventID]
	return state, nil
}

func (projection *ChannelMonitorRedisSharedProjection) setCostState(ctx context.Context, pipe redis.Pipeliner, costID string, state channelMonitorRedisSharedCostState) {
	pipe.HSet(ctx, ChannelMonitorRedisCostEventStateKey(costID), map[string]interface{}{
		channelMonitorRedisSharedCostStatus: string(state.Status), channelMonitorRedisSharedCostSettled: state.SettledCost,
		channelMonitorRedisSharedCostUnresolved: state.UnresolvedCost, channelMonitorRedisSharedCostDayStart: state.DayStart,
		channelMonitorRedisSharedCostSource: state.Source, channelMonitorRedisSharedCostMinuteStart: state.MinuteStart,
		channelMonitorRedisSharedCostChannelID: state.ChannelID, channelMonitorRedisSharedCostModel: state.Model,
		channelMonitorRedisSharedCostGroup: state.Group, channelMonitorRedisSharedCostAPIKeyID: state.APIKeyID,
		channelMonitorRedisSharedCostAPIKeyName: state.APIKeyName, channelMonitorRedisSharedCostEventSequence: state.EventSequence,
		channelMonitorRedisSharedCostCreatedAt: state.CreatedAt, channelMonitorRedisSharedCostEventID: state.EventID,
	})
	pipe.ExpireNX(ctx, ChannelMonitorRedisCostEventStateKey(costID), channelMonitorRedisSharedCostStateTTL)
}

func (projection *ChannelMonitorRedisSharedProjection) deleteCostState(ctx context.Context, pipe redis.Pipeliner, costID string) {
	pipe.Del(ctx, ChannelMonitorRedisCostEventStateKey(costID))
}

func channelMonitorRedisSharedCostEventIsNewer(previous channelMonitorRedisSharedCostState, current model.ChannelMonitorEvent) bool {
	if previous.EventID == current.EventId {
		return false
	}
	previousRank := channelMonitorRealtimeCostStatusRank(previous.Status)
	currentRank := channelMonitorRealtimeCostStatusRank(current.CostStatus)
	if currentRank != previousRank {
		return currentRank > previousRank
	}
	if current.EventSequence != previous.EventSequence {
		return current.EventSequence > previous.EventSequence
	}
	return current.CreatedAt >= previous.CreatedAt
}

const channelMonitorRedisSharedLatestValuesScript = `
local occurred = tonumber(ARGV[1])
local sequence = ARGV[2]
local has_first_token = ARGV[3]
local first_token = ARGV[4]
local has_tps = ARGV[5]
local tps = ARGV[6]

local function sequence_greater(left, right)
  if not right then
    return true
  end
  if string.len(left) ~= string.len(right) then
    return string.len(left) > string.len(right)
  end
  return left > right
end

local function should_update(at_field, sequence_field)
  local current_at = redis.call('HGET', KEYS[1], at_field)
  if not current_at then
    return true
  end
  local current_at_number = tonumber(current_at)
  if occurred ~= current_at_number then
    return occurred > current_at_number
  end
  return sequence_greater(sequence, redis.call('HGET', KEYS[1], sequence_field))
end

for index = 7, #ARGV do
  local scope = ARGV[index]
  local last_used_field = scope .. ':last_used_time'
  local current_last_used = redis.call('HGET', KEYS[1], last_used_field)
  if not current_last_used or occurred > tonumber(current_last_used) then
    redis.call('HSET', KEYS[1], last_used_field, ARGV[1])
  end

  if has_first_token == '1' then
    local value_field = scope .. ':latest_first_token_ms'
    local at_field = scope .. ':latest_first_token_at'
    local sequence_field = scope .. ':latest_first_token_sequence'
    if should_update(at_field, sequence_field) then
      redis.call('HSET', KEYS[1], value_field, first_token, at_field, ARGV[1], sequence_field, sequence)
    end
  end

  if has_tps == '1' then
    local value_field = scope .. ':latest_tps'
    local at_field = scope .. ':latest_tps_at'
    local sequence_field = scope .. ':latest_tps_sequence'
    if should_update(at_field, sequence_field) then
      redis.call('HSET', KEYS[1], value_field, tps, at_field, ARGV[1], sequence_field, sequence)
    end
  end
end
return 1
`

const channelMonitorRedisSharedFailureScript = `
local actual_delta = tonumber(ARGV[1])
local final_delta = tonumber(ARGV[2])
if actual_delta ~= 0 then
  redis.call('HINCRBY', KEYS[1], ARGV[6], actual_delta)
end
if final_delta ~= 0 then
  redis.call('HINCRBY', KEYS[1], ARGV[7], final_delta)
end

local current_at = redis.call('HGET', KEYS[1], ARGV[8])
local should_update = not current_at or tonumber(ARGV[3]) > tonumber(current_at)
if current_at and tonumber(ARGV[3]) == tonumber(current_at) then
  local current_sequence = redis.call('HGET', KEYS[1], ARGV[9])
  should_update = not current_sequence or string.len(ARGV[4]) > string.len(current_sequence) or
    string.len(ARGV[4]) == string.len(current_sequence) and ARGV[4] > current_sequence
end
if should_update then
  redis.call('HSET', KEYS[1], ARGV[8], ARGV[3], ARGV[9], ARGV[4], ARGV[10], ARGV[5])
end
return 1
`

const channelMonitorRedisSharedMetadataScript = `
local function set_max_number(field, value)
  local current = redis.call('HGET', KEYS[1], field)
  if not current or tonumber(value) > tonumber(current) then
    redis.call('HSET', KEYS[1], field, value)
  end
end

set_max_number(ARGV[4], ARGV[1])
set_max_number(ARGV[5], ARGV[2])
local current_sequence = redis.call('HGET', KEYS[1], ARGV[6])
if not current_sequence or string.len(ARGV[3]) > string.len(current_sequence) or
  string.len(ARGV[3]) == string.len(current_sequence) and ARGV[3] > current_sequence then
  redis.call('HSET', KEYS[1], ARGV[6], ARGV[3])
end
return 1
`

func (projection *ChannelMonitorRedisSharedProjection) appendDashboardEventDelta(ctx context.Context, pipe redis.Pipeliner, event model.ChannelMonitorEvent) {
	delta, ok := channelMonitorRedisSharedEventDeltaFromEvent(event)
	if !ok {
		return
	}
	minuteKey := ChannelMonitorRedisDashboardMinuteKey(event.OccurredAt - event.OccurredAt%60)
	scopes := channelMonitorRedisSharedDashboardScopes(event.ChannelId, event.ModelName, event.GroupName, event.APIKeyId)
	for _, scope := range scopes {
		projection.appendAggregateDelta(ctx, pipe, minuteKey, scope, delta, channelMonitorRedisSharedMinuteTTL)
		projection.setAPIKeyName(ctx, pipe, minuteKey, scope, event.APIKeyId, event.APIKeyName)
	}
	projection.appendAggregateDelta(ctx, pipe, minuteKey, channelMonitorRedisSharedScopePerformance, delta, channelMonitorRedisSharedMinuteTTL)
	scopes = append(scopes, channelMonitorRedisSharedScopePerformance)
	projection.appendLatestValues(ctx, pipe, minuteKey, scopes, event)
	projection.appendFailure(ctx, pipe, minuteKey, event)
}

func (projection *ChannelMonitorRedisSharedProjection) appendLatestValues(
	ctx context.Context,
	pipe redis.Pipeliner,
	key string,
	scopes []string,
	event model.ChannelMonitorEvent,
) {
	if event.FinalRetrySummary || !event.RequestDispatched {
		return
	}
	arguments := []interface{}{
		event.OccurredAt,
		strconv.FormatUint(event.EventSequence, 10),
		"0", "", "0", "",
	}
	if event.FirstTokenMs != nil {
		arguments[2] = "1"
		arguments[3] = strconv.FormatFloat(*event.FirstTokenMs, 'g', -1, 64)
	}
	if event.TPS != nil {
		arguments[4] = "1"
		arguments[5] = strconv.FormatFloat(*event.TPS, 'g', -1, 64)
	}
	for _, scope := range scopes {
		arguments = append(arguments, scope)
	}
	pipe.Eval(ctx, channelMonitorRedisSharedLatestValuesScript, []string{key}, arguments...)
}

func (projection *ChannelMonitorRedisSharedProjection) appendFailure(
	ctx context.Context,
	pipe redis.Pipeliner,
	key string,
	event model.ChannelMonitorEvent,
) {
	if event.Outcome != model.ChannelMonitorEventOutcomeFailure || !event.RequestDispatched && !event.FinalRetrySummary {
		return
	}
	statusCode := 0
	if event.StatusCode != nil {
		statusCode = *event.StatusCode
	}
	identity := channelMonitorRedisSharedFailureIdentity(
		event.ChannelId,
		event.ModelName,
		event.GroupName,
		statusCode,
		event.ErrorType,
		event.ErrorCode,
	)
	scope := channelMonitorRedisSharedScopeFailure + ":" + identity
	actualDelta := int64(0)
	if !event.FinalRetrySummary {
		actualDelta = 1
	}
	finalDelta := int64(0)
	if event.IsFinalAttempt || event.FinalRetrySummary {
		finalDelta = 1
	}
	pipe.Eval(ctx, channelMonitorRedisSharedFailureScript, []string{key},
		actualDelta,
		finalDelta,
		event.OccurredAt,
		strconv.FormatUint(event.EventSequence, 10),
		event.ErrorMessage,
		scope+":"+channelMonitorRedisSharedMetricFailureActual,
		scope+":"+channelMonitorRedisSharedMetricFailureFinal,
		scope+":"+channelMonitorRedisSharedMetricFailureLast,
		scope+":"+channelMonitorRedisSharedMetricFailureLastSeq,
		scope+":"+channelMonitorRedisSharedMetricFailureSample,
	)
	pipe.ExpireNX(ctx, key, channelMonitorRedisSharedMinuteTTL)
}

func (projection *ChannelMonitorRedisSharedProjection) appendMetadata(
	ctx context.Context,
	pipe redis.Pipeliner,
	event model.ChannelMonitorEvent,
	processedAt int64,
) {
	key := ChannelMonitorRedisDashboardMinuteKey(event.OccurredAt - event.OccurredAt%60)
	scope := channelMonitorRedisSharedScopeMetadata + ":"
	pipe.Eval(ctx, channelMonitorRedisSharedMetadataScript, []string{key},
		event.OccurredAt,
		processedAt,
		strconv.FormatUint(event.EventSequence, 10),
		scope+channelMonitorRedisSharedMetricDataCutoffAt,
		scope+channelMonitorRedisSharedMetricProcessedAt,
		scope+channelMonitorRedisSharedMetricEventWatermark,
	)
	pipe.ExpireNX(ctx, key, channelMonitorRedisSharedMinuteTTL)
}

type channelMonitorRedisSharedEventDelta struct {
	Integers map[string]int64
	Floats   map[string]float64
}

func channelMonitorRedisSharedEventDeltaFromEvent(event model.ChannelMonitorEvent) (channelMonitorRedisSharedEventDelta, bool) {
	delta := channelMonitorRedisSharedEventDelta{Integers: make(map[string]int64), Floats: make(map[string]float64)}
	if event.FinalRetrySummary {
		if event.Outcome == model.ChannelMonitorEventOutcomeFailure {
			delta.Integers[channelMonitorRedisSharedMetricFinalFailure] = 1
			return delta, true
		}
		return delta, false
	}
	if !event.RequestDispatched {
		return delta, false
	}
	delta.Integers[channelMonitorRedisSharedMetricEventCount] = 1
	switch event.Outcome {
	case model.ChannelMonitorEventOutcomeSuccess:
		delta.Integers[channelMonitorRedisSharedMetricActualSuccess] = 1
		if event.IsFinalAttempt {
			delta.Integers[channelMonitorRedisSharedMetricFinalSuccess] = 1
		}
	case model.ChannelMonitorEventOutcomeFailure:
		delta.Integers[channelMonitorRedisSharedMetricActualFailure] = 1
		if event.IsFinalAttempt {
			delta.Integers[channelMonitorRedisSharedMetricFinalFailure] = 1
		}
	default:
		return channelMonitorRedisSharedEventDelta{}, false
	}
	delta.Integers[channelMonitorRedisSharedMetricBusinessRequests] = 1
	if event.FirstTokenMs != nil {
		delta.Integers[channelMonitorRedisSharedMetricFirstTokenSamples] = 1
		delta.Floats[channelMonitorRedisSharedMetricFirstTokenTotalMs] = *event.FirstTokenMs
	}
	if event.AttemptDurationMs != nil {
		delta.Integers[channelMonitorRedisSharedMetricAttemptDurationSamples] = 1
		delta.Integers[channelMonitorRedisSharedMetricAttemptDurationTotalMs] = *event.AttemptDurationMs
	}
	if outputTokens, generationDurationMs, ok := event.TPSMeasurement(); ok {
		delta.Integers[channelMonitorRedisSharedMetricTPSSamples] = 1
		delta.Integers[channelMonitorRedisSharedMetricTPSOutputTokens] = outputTokens
		delta.Integers[channelMonitorRedisSharedMetricTPSGenerationMs] = generationDurationMs
	}
	inputTokens := int64(0)
	if event.InputTokens != nil {
		inputTokens = *event.InputTokens
	} else if event.PromptTokens != nil {
		inputTokens = *event.PromptTokens
	}
	if inputTokens > 0 {
		delta.Integers[channelMonitorRedisSharedMetricCacheSamples] = 1
	}
	// Keep request-based hit counters, but only include streaming responses in
	// the token-weighted cache-utilization denominator/numerator.
	if event.IsStream && inputTokens > 0 {
		delta.Integers[channelMonitorRedisSharedMetricInputTokens] = inputTokens
	}
	if event.CacheReadTokens != nil && *event.CacheReadTokens > 0 {
		delta.Integers[channelMonitorRedisSharedMetricCacheHits] = 1
	}
	if event.IsStream && event.CacheReadTokens != nil && *event.CacheReadTokens > 0 {
		delta.Integers[channelMonitorRedisSharedMetricCacheReadTokens] = *event.CacheReadTokens
	}
	if event.CacheWriteTokens != nil && *event.CacheWriteTokens > 0 {
		delta.Integers[channelMonitorRedisSharedMetricCacheWriteRequests] = 1
		delta.Integers[channelMonitorRedisSharedMetricCacheWriteTokens] = *event.CacheWriteTokens
	}
	return delta, true
}

func (projection *ChannelMonitorRedisSharedProjection) appendCostDelta(ctx context.Context, pipe redis.Pipeliner, state channelMonitorRedisSharedCostState, sign int64, includeDashboard bool) {
	delta := channelMonitorRedisSharedEventDelta{Integers: make(map[string]int64)}
	if state.Status == model.ChannelMonitorEventCostSettled {
		delta.Integers[channelMonitorRedisSharedMetricSettledCost] = sign * state.SettledCost
		delta.Integers[channelMonitorRedisSharedMetricSettledRequests] = sign
		switch model.ChannelMonitorEventSource(state.Source) {
		case model.ChannelMonitorEventSourceStatusProbe, model.ChannelMonitorEventSourceSmartProbe:
			delta.Integers[channelMonitorRedisSharedMetricProbeSettledCost] = sign * state.SettledCost
		case model.ChannelMonitorEventSourceGroupProbe:
			delta.Integers[channelMonitorRedisSharedMetricProbeSettledCost] = sign * state.SettledCost
			delta.Integers[channelMonitorRedisSharedMetricGroupProbeSettledCost] = sign * state.SettledCost
		case model.ChannelMonitorEventSourceModelDetection:
			delta.Integers[channelMonitorRedisSharedMetricDetectionSettledCost] = sign * state.SettledCost
		}
	} else if state.Status == model.ChannelMonitorEventCostUnresolved {
		delta.Integers[channelMonitorRedisSharedMetricUnresolvedCost] = sign * state.UnresolvedCost
		delta.Integers[channelMonitorRedisSharedMetricUnresolvedRequests] = sign
	}
	if len(delta.Integers) == 0 {
		return
	}
	dayKey := ChannelMonitorRedisCostDayKey(state.DayStart)
	for _, scope := range channelMonitorRedisSharedScopes(state.ChannelID, state.Model, state.Group, state.APIKeyID) {
		projection.appendAggregateDelta(ctx, pipe, dayKey, scope, delta, channelMonitorRedisSharedMinuteTTL)
		projection.setAPIKeyName(ctx, pipe, dayKey, scope, state.APIKeyID, state.APIKeyName)
	}
	if includeDashboard {
		minuteKey := ChannelMonitorRedisDashboardMinuteKey(state.MinuteStart)
		for _, scope := range channelMonitorRedisSharedDashboardScopes(state.ChannelID, state.Model, state.Group, state.APIKeyID) {
			projection.appendAggregateDelta(ctx, pipe, minuteKey, scope, delta, channelMonitorRedisSharedMinuteTTL)
			projection.setAPIKeyName(ctx, pipe, minuteKey, scope, state.APIKeyID, state.APIKeyName)
		}
	}
}

func (projection *ChannelMonitorRedisSharedProjection) setAPIKeyName(ctx context.Context, pipe redis.Pipeliner, key, scope string, apiKeyID int, apiKeyName string) {
	isAPIKeyScope := strings.HasPrefix(scope, channelMonitorRedisSharedScopeAPIKey+":") ||
		strings.HasPrefix(scope, channelMonitorRedisSharedScopeAPIKeyRoute+":")
	if apiKeyID <= 0 || strings.TrimSpace(apiKeyName) == "" || !isAPIKeyScope {
		return
	}
	pipe.HSetNX(ctx, key, scope+":"+channelMonitorRedisSharedMetricAPIKeyName, strings.TrimSpace(apiKeyName))
}

func (projection *ChannelMonitorRedisSharedProjection) appendAggregateDelta(ctx context.Context, pipe redis.Pipeliner, key, scope string, delta channelMonitorRedisSharedEventDelta, ttl time.Duration) {
	for metric, value := range delta.Integers {
		pipe.HIncrBy(ctx, key, scope+":"+metric, value)
	}
	for metric, value := range delta.Floats {
		pipe.HIncrByFloat(ctx, key, scope+":"+metric, value)
	}
	pipe.ExpireNX(ctx, key, ttl)
}

func channelMonitorRedisSharedScopes(channelID int, modelName, groupName string, apiKeyID int) []string {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	groupName = strings.TrimSpace(groupName)
	scopes := []string{
		channelMonitorRedisSharedScopeGlobal,
		channelMonitorRedisSharedScopeChannel + ":" + strconv.Itoa(channelID),
		channelMonitorRedisSharedScopeModel + ":" + channelMonitorRedisSharedDimension(modelName),
		channelMonitorRedisSharedScopeGroup + ":" + channelMonitorRedisSharedDimension(groupName),
	}
	if apiKeyID > 0 {
		scopes = append(scopes, channelMonitorRedisSharedScopeAPIKey+":"+strconv.Itoa(apiKeyID))
	}
	return scopes
}

func channelMonitorRedisSharedDashboardScopes(channelID int, modelName, groupName string, apiKeyID int) []string {
	modelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	groupName = strings.TrimSpace(groupName)
	scopes := channelMonitorRedisSharedScopes(channelID, modelName, groupName, apiKeyID)
	scopes = append(scopes,
		channelMonitorRedisSharedScopeRoute+":"+channelMonitorRedisSharedRouteIdentity(channelID, modelName),
		channelMonitorRedisSharedScopeGroupRoute+":"+channelMonitorRedisSharedGroupChannelIdentity(groupName, channelID),
	)
	if apiKeyID > 0 {
		scopes = append(scopes,
			channelMonitorRedisSharedScopeAPIKeyRoute+":"+channelMonitorRedisSharedAPIKeyScopeIdentity(apiKeyID, channelID, modelName, groupName),
		)
	}
	return scopes
}

func channelMonitorRedisSharedRouteIdentity(channelID int, modelName string) string {
	return strconv.Itoa(channelID) + "." + channelMonitorRedisSharedDimension(modelName)
}

func channelMonitorRedisSharedGroupChannelIdentity(groupName string, channelID int) string {
	return channelMonitorRedisSharedDimension(groupName) + "." + strconv.Itoa(channelID)
}

func channelMonitorRedisSharedAPIKeyScopeIdentity(apiKeyID, channelID int, modelName, groupName string) string {
	return strings.Join([]string{
		strconv.Itoa(apiKeyID),
		strconv.Itoa(channelID),
		channelMonitorRedisSharedDimension(modelName),
		channelMonitorRedisSharedDimension(groupName),
	}, ".")
}

func channelMonitorRedisSharedFailureIdentity(
	channelID int,
	modelName string,
	groupName string,
	statusCode int,
	errorType string,
	errorCode string,
) string {
	return strings.Join([]string{
		strconv.Itoa(channelID),
		channelMonitorRedisSharedDimension(modelName),
		channelMonitorRedisSharedDimension(groupName),
		strconv.Itoa(statusCode),
		channelMonitorRedisSharedDimension(errorType),
		channelMonitorRedisSharedDimension(errorCode),
	}, ".")
}

func channelMonitorRedisSharedDimension(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(value)))
}

func channelMonitorRedisSharedDimensionDecode(value string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func (projection *ChannelMonitorRedisSharedProjection) QueryChannelMonitorRedisSharedProjection(ctx context.Context, startAt, endAt int64) (ChannelMonitorRedisSharedProjectionView, error) {
	return projection.Query(ctx, startAt, endAt)
}

func (projection *ChannelMonitorRedisSharedProjection) queryWindow(startAt, endAt int64) (startMinute, endMinute, normalizedStartAt, normalizedEndAt int64, limits ChannelMonitorRedisSharedProjectionLimits) {
	limits = normalizeChannelMonitorRedisSharedProjectionLimits(projection.limits)
	if endAt <= 0 {
		endAt = projection.now().Unix() + 1
	}
	// Round endAt up to the next minute without allowing the addition of the
	// minute width to overflow int64. Controller callers use Unix timestamps,
	// but this exported projection API is also used by maintenance and replay
	// code where malformed bounds must not turn into a negative slice capacity.
	if endAt > math.MaxInt64-60 {
		endAt = math.MaxInt64 - 60
	}
	maxWindowSeconds := int64(limits.MaxQueryMinutes) * 60
	if startAt <= 0 || startAt > endAt {
		startAt = endAt - maxWindowSeconds
	}
	startMinute = startAt - startAt%60
	endMinute = (endAt - 1) - (endAt-1)%60 + 60
	if endMinute <= startMinute || endMinute-startMinute > maxWindowSeconds {
		startMinute = endMinute - maxWindowSeconds
	}
	return startMinute, endMinute, startAt, endAt, limits
}

func (projection *ChannelMonitorRedisSharedProjection) Query(ctx context.Context, startAt, endAt int64) (ChannelMonitorRedisSharedProjectionView, error) {
	view := ChannelMonitorRedisSharedProjectionView{
		Channels:   make(map[int]ChannelMonitorRedisSharedAggregate),
		Models:     make(map[string]ChannelMonitorRedisSharedAggregate),
		Groups:     make(map[string]ChannelMonitorRedisSharedAggregate),
		APIKeys:    make(map[int]ChannelMonitorRedisSharedAggregate),
		DailyCosts: make(map[int64]ChannelMonitorRedisSharedDailyCostView),
	}
	if projection == nil || projection.client == nil {
		return view, ErrChannelMonitorRedisSharedProjectionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	startMinute, endMinute, _, _, limits := projection.queryWindow(startAt, endAt)
	view.WindowStart, view.WindowEnd = startMinute, endMinute
	opCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisSharedOperationTimeout)
	defer cancel()
	accumulator := newChannelMonitorRedisSharedQueryAccumulatorWithLimits(limits)

	minuteKeys := make([]string, 0, (endMinute-startMinute)/60)
	for minute := startMinute; minute < endMinute; minute += 60 {
		minuteKeys = append(minuteKeys, ChannelMonitorRedisDashboardMinuteKey(minute))
	}
	dayStart := model.ChannelDailyCostDayStart(startMinute)
	lastDay := model.ChannelDailyCostDayStart(endMinute - 1)
	type dayHash struct {
		day int64
		key string
	}
	dayKeys := make([]dayHash, 0, 2)
	for day := dayStart; day <= lastDay; day += 24 * 60 * 60 {
		dayKeys = append(dayKeys, dayHash{day: day, key: ChannelMonitorRedisCostDayKey(day)})
	}
	allKeys := append([]string(nil), minuteKeys...)
	for _, dayHash := range dayKeys {
		allKeys = append(allKeys, dayHash.key)
	}
	pipe := projection.client.Pipeline()
	fieldCounts := make(map[string]*redis.IntCmd, len(allKeys))
	for _, key := range allKeys {
		fieldCounts[key] = pipe.HLen(opCtx, key)
	}
	if _, err := pipe.Exec(opCtx); err != nil {
		return view, err
	}
	var totalFields int64
	for _, key := range allKeys {
		count, err := fieldCounts[key].Result()
		if err != nil {
			return view, err
		}
		if count > int64(limits.MaxHashFields) {
			return view, &ChannelMonitorRedisSharedProjectionLimitError{
				Resource: "hash_fields", Limit: int64(limits.MaxHashFields), Actual: count,
			}
		}
		nextTotal, addErr := channelMonitorRedisSharedCheckedAddInt64(totalFields, count)
		if addErr != nil || nextTotal > int64(limits.MaxTotalHashFields) {
			return view, &ChannelMonitorRedisSharedProjectionLimitError{
				Resource: "total_hash_fields", Limit: int64(limits.MaxTotalHashFields), Actual: nextTotal,
			}
		}
		totalFields = nextTotal
	}

	pipe = projection.client.Pipeline()
	minuteCommands := make(map[string]*redis.StringStringMapCmd, len(minuteKeys))
	for _, key := range minuteKeys {
		minuteCommands[key] = pipe.HGetAll(opCtx, key)
	}
	dayCommands := make(map[string]*redis.StringStringMapCmd, len(dayKeys))
	for _, dayHash := range dayKeys {
		dayCommands[dayHash.key] = pipe.HGetAll(opCtx, dayHash.key)
	}
	if _, err := pipe.Exec(opCtx); err != nil {
		return view, err
	}
	var responseBytes int64
	for _, key := range minuteKeys {
		values, err := minuteCommands[key].Result()
		if err != nil {
			return view, err
		}
		responseBytes, err = channelMonitorRedisSharedCheckedAddInt64(responseBytes, channelMonitorRedisSharedHashResponseBytes(values))
		if err != nil || responseBytes > limits.MaxResponseBytes {
			return view, &ChannelMonitorRedisSharedProjectionLimitError{
				Resource: "response_bytes", Limit: limits.MaxResponseBytes, Actual: responseBytes,
			}
		}
		if err := accumulator.add(values); err != nil {
			return view, err
		}
	}
	accumulator.apply(&view)
	for _, dayHash := range dayKeys {
		values, err := dayCommands[dayHash.key].Result()
		if err != nil {
			return view, err
		}
		responseBytes, err = channelMonitorRedisSharedCheckedAddInt64(responseBytes, channelMonitorRedisSharedHashResponseBytes(values))
		if err != nil || responseBytes > limits.MaxResponseBytes {
			return view, &ChannelMonitorRedisSharedProjectionLimitError{
				Resource: "response_bytes", Limit: limits.MaxResponseBytes, Actual: responseBytes,
			}
		}
		costView := view.DailyCosts[dayHash.day]
		initializeDailyCostView(&costView)
		if err := addChannelMonitorRedisDailyCostHashValuesWithLimits(&costView, values, limits); err != nil {
			return view, err
		}
		view.DailyCosts[dayHash.day] = costView
	}
	view.DataCutoffAt = accumulator.dataCutoffAt
	view.ProcessedAt = accumulator.processedAt
	view.EventWatermark = accumulator.eventWatermark
	finalizeChannelMonitorRedisSharedView(&view, accumulator)
	channelMonitorRedisSharedRememberMetadata(projection.client, view)
	return view, nil
}

func channelMonitorRedisSharedHashResponseBytes(values map[string]string) int64 {
	var total int64
	for field, value := range values {
		// Include a small RESP framing allowance for each field/value pair. The
		// estimate is deliberately conservative and is only used as a final
		// guard after HLEN has already bounded the amount fetched.
		pairBytes := int64(len(field) + len(value) + 16)
		next, err := channelMonitorRedisSharedCheckedAddInt64(total, pairBytes)
		if err != nil {
			return math.MaxInt64
		}
		total = next
	}
	return total
}

// QueryMetadata reads only the three metadata fields needed by controllers.
// It intentionally avoids HGETALL so a metadata lookup remains bounded even
// when the corresponding minute hashes contain many dimensions.
func (projection *ChannelMonitorRedisSharedProjection) QueryMetadata(ctx context.Context, startAt, endAt int64) (dataCutoffAt, processedAt int64, eventWatermark uint64, windowStart, windowEnd int64, err error) {
	if projection == nil || projection.client == nil {
		return 0, 0, 0, 0, 0, ErrChannelMonitorRedisSharedProjectionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	windowStart, windowEnd, _, _, _ = projection.queryWindow(startAt, endAt)
	opCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisSharedOperationTimeout)
	defer cancel()
	pipe := projection.client.Pipeline()
	commands := make([]*redis.SliceCmd, 0, (windowEnd-windowStart)/60)
	for minute := windowStart; minute < windowEnd; minute += 60 {
		commands = append(commands, pipe.HMGet(opCtx, ChannelMonitorRedisDashboardMinuteKey(minute),
			channelMonitorRedisSharedScopeMetadata+":"+channelMonitorRedisSharedMetricDataCutoffAt,
			channelMonitorRedisSharedScopeMetadata+":"+channelMonitorRedisSharedMetricProcessedAt,
			channelMonitorRedisSharedScopeMetadata+":"+channelMonitorRedisSharedMetricEventWatermark,
		))
	}
	if _, err := pipe.Exec(opCtx); err != nil {
		return 0, 0, 0, windowStart, windowEnd, err
	}
	for _, command := range commands {
		values, resultErr := command.Result()
		if resultErr != nil {
			return 0, 0, 0, windowStart, windowEnd, resultErr
		}
		if len(values) != 3 {
			continue
		}
		if raw := channelMonitorRedisSharedRedisValueString(values[0]); raw != "" {
			value, parseErr := strconv.ParseInt(raw, 10, 64)
			if parseErr != nil {
				return 0, 0, 0, windowStart, windowEnd, parseErr
			}
			dataCutoffAt = max(dataCutoffAt, value)
		}
		if raw := channelMonitorRedisSharedRedisValueString(values[1]); raw != "" {
			value, parseErr := strconv.ParseInt(raw, 10, 64)
			if parseErr != nil {
				return 0, 0, 0, windowStart, windowEnd, parseErr
			}
			processedAt = max(processedAt, value)
		}
		if raw := channelMonitorRedisSharedRedisValueString(values[2]); raw != "" {
			value, parseErr := strconv.ParseUint(raw, 10, 64)
			if parseErr != nil {
				return 0, 0, 0, windowStart, windowEnd, parseErr
			}
			eventWatermark = max(eventWatermark, value)
		}
	}
	return dataCutoffAt, processedAt, eventWatermark, windowStart, windowEnd, nil
}

func channelMonitorRedisSharedRedisValueString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}

type channelMonitorRedisSharedRouteKey struct {
	channelID int
	modelName string
}

type channelMonitorRedisSharedGroupChannelKey struct {
	groupName string
	channelID int
}

type channelMonitorRedisSharedAPIKeyScopeKey struct {
	apiKeyID  int
	channelID int
	modelName string
	groupName string
}

type channelMonitorRedisSharedFailureKey struct {
	channelID  int
	modelName  string
	groupName  string
	statusCode int
	errorType  string
	errorCode  string
}

type channelMonitorRedisSharedQueryAccumulator struct {
	limits         ChannelMonitorRedisSharedProjectionLimits
	summary        ChannelMonitorRedisSharedAggregate
	performance    ChannelMonitorRedisSharedAggregate
	channels       map[int]ChannelMonitorRedisSharedAggregate
	models         map[string]ChannelMonitorRedisSharedAggregate
	groups         map[string]ChannelMonitorRedisSharedAggregate
	apiKeys        map[int]ChannelMonitorRedisSharedAggregate
	routes         map[channelMonitorRedisSharedRouteKey]ChannelMonitorRedisSharedAggregate
	groupChannels  map[channelMonitorRedisSharedGroupChannelKey]ChannelMonitorRedisSharedAggregate
	apiKeyScopes   map[channelMonitorRedisSharedAPIKeyScopeKey]ChannelMonitorRedisSharedAggregate
	failures       map[channelMonitorRedisSharedFailureKey]ChannelMonitorRedisSharedFailureCategory
	dataCutoffAt   int64
	processedAt    int64
	eventWatermark uint64
}

func newChannelMonitorRedisSharedQueryAccumulator() *channelMonitorRedisSharedQueryAccumulator {
	return newChannelMonitorRedisSharedQueryAccumulatorWithLimits(defaultChannelMonitorRedisSharedProjectionLimits())
}

func newChannelMonitorRedisSharedQueryAccumulatorWithLimits(limits ChannelMonitorRedisSharedProjectionLimits) *channelMonitorRedisSharedQueryAccumulator {
	limits = normalizeChannelMonitorRedisSharedProjectionLimits(limits)
	return &channelMonitorRedisSharedQueryAccumulator{
		limits:        limits,
		channels:      make(map[int]ChannelMonitorRedisSharedAggregate),
		models:        make(map[string]ChannelMonitorRedisSharedAggregate),
		groups:        make(map[string]ChannelMonitorRedisSharedAggregate),
		apiKeys:       make(map[int]ChannelMonitorRedisSharedAggregate),
		routes:        make(map[channelMonitorRedisSharedRouteKey]ChannelMonitorRedisSharedAggregate),
		groupChannels: make(map[channelMonitorRedisSharedGroupChannelKey]ChannelMonitorRedisSharedAggregate),
		apiKeyScopes:  make(map[channelMonitorRedisSharedAPIKeyScopeKey]ChannelMonitorRedisSharedAggregate),
		failures:      make(map[channelMonitorRedisSharedFailureKey]ChannelMonitorRedisSharedFailureCategory),
	}
}

type channelMonitorRedisSharedScopeFields struct {
	scope    string
	identity string
	metrics  map[string]string
}

func (accumulator *channelMonitorRedisSharedQueryAccumulator) add(values map[string]string) error {
	entries := make(map[string]*channelMonitorRedisSharedScopeFields)
	for field, raw := range values {
		parts := strings.SplitN(field, ":", 3)
		if len(parts) < 2 {
			continue
		}
		scope, metric, identity := parts[0], parts[len(parts)-1], ""
		if len(parts) == 3 {
			identity = parts[1]
		}
		entryKey := scope + "\x00" + identity
		entry := entries[entryKey]
		if entry == nil {
			entry = &channelMonitorRedisSharedScopeFields{scope: scope, identity: identity, metrics: make(map[string]string)}
			entries[entryKey] = entry
		}
		entry.metrics[metric] = raw
	}
	for _, entry := range entries {
		switch entry.scope {
		case channelMonitorRedisSharedScopeMetadata:
			if err := accumulator.addMetadata(entry.metrics); err != nil {
				return err
			}
		case channelMonitorRedisSharedScopeFailure:
			if err := accumulator.addFailure(entry.identity, entry.metrics); err != nil {
				return err
			}
		default:
			if err := accumulator.addAggregate(entry.scope, entry.identity, entry.metrics); err != nil {
				return err
			}
		}
	}
	return nil
}

func (accumulator *channelMonitorRedisSharedQueryAccumulator) ensureDimensionCapacity(existing bool) error {
	if existing {
		return nil
	}
	if len(accumulator.channels)+len(accumulator.models)+len(accumulator.groups)+len(accumulator.apiKeys)+
		len(accumulator.routes)+len(accumulator.groupChannels)+len(accumulator.apiKeyScopes)+len(accumulator.failures) >= accumulator.limits.MaxResponseDimensions {
		return &ChannelMonitorRedisSharedProjectionLimitError{
			Resource: "response_dimensions", Limit: int64(accumulator.limits.MaxResponseDimensions),
			Actual: int64(len(accumulator.channels) + len(accumulator.models) + len(accumulator.groups) + len(accumulator.apiKeys) +
				len(accumulator.routes) + len(accumulator.groupChannels) + len(accumulator.apiKeyScopes) + len(accumulator.failures) + 1),
		}
	}
	return nil
}

func channelMonitorRedisSharedMergeAggregateMapWithLimits[K comparable](accumulator *channelMonitorRedisSharedQueryAccumulator, items map[K]ChannelMonitorRedisSharedAggregate, key K, aggregate ChannelMonitorRedisSharedAggregate) error {
	_, exists := items[key]
	if !exists && len(items) >= accumulator.limits.MaxDimensionEntries {
		return &ChannelMonitorRedisSharedProjectionLimitError{
			Resource: "dimension_entries", Limit: int64(accumulator.limits.MaxDimensionEntries), Actual: int64(len(items) + 1),
		}
	}
	if err := accumulator.ensureDimensionCapacity(exists); err != nil {
		return err
	}
	return mergeChannelMonitorRedisSharedAggregateMap(items, key, aggregate)
}

func (accumulator *channelMonitorRedisSharedQueryAccumulator) addMetadata(metrics map[string]string) error {
	for metric, raw := range metrics {
		switch metric {
		case channelMonitorRedisSharedMetricDataCutoffAt:
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return err
			}
			accumulator.dataCutoffAt = max(accumulator.dataCutoffAt, value)
		case channelMonitorRedisSharedMetricProcessedAt:
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return err
			}
			accumulator.processedAt = max(accumulator.processedAt, value)
		case channelMonitorRedisSharedMetricEventWatermark:
			value, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				return err
			}
			accumulator.eventWatermark = max(accumulator.eventWatermark, value)
		}
	}
	return nil
}

func (accumulator *channelMonitorRedisSharedQueryAccumulator) addAggregate(scope, identity string, metrics map[string]string) error {
	aggregate := ChannelMonitorRedisSharedAggregate{}
	for metric, raw := range metrics {
		if err := addChannelMonitorRedisAggregateField(&aggregate, metric, raw); err != nil {
			return err
		}
	}
	switch scope {
	case channelMonitorRedisSharedScopePerformance:
		return mergeChannelMonitorRedisSharedAggregate(&accumulator.performance, aggregate)
	case channelMonitorRedisSharedScopeGlobal:
		return mergeChannelMonitorRedisSharedAggregate(&accumulator.summary, aggregate)
	case channelMonitorRedisSharedScopeChannel:
		id, err := strconv.Atoi(identity)
		if err != nil {
			return err
		}
		return channelMonitorRedisSharedMergeAggregateMapWithLimits(accumulator, accumulator.channels, id, aggregate)
	case channelMonitorRedisSharedScopeModel, channelMonitorRedisSharedScopeGroup:
		name, err := channelMonitorRedisSharedDimensionDecode(identity)
		if err != nil {
			return err
		}
		if scope == channelMonitorRedisSharedScopeModel {
			return channelMonitorRedisSharedMergeAggregateMapWithLimits(accumulator, accumulator.models, name, aggregate)
		} else {
			return channelMonitorRedisSharedMergeAggregateMapWithLimits(accumulator, accumulator.groups, name, aggregate)
		}
	case channelMonitorRedisSharedScopeAPIKey:
		id, err := strconv.Atoi(identity)
		if err != nil {
			return err
		}
		return channelMonitorRedisSharedMergeAggregateMapWithLimits(accumulator, accumulator.apiKeys, id, aggregate)
	case channelMonitorRedisSharedScopeRoute:
		channelID, modelName, err := channelMonitorRedisSharedParseRouteIdentity(identity)
		if err != nil {
			return err
		}
		return channelMonitorRedisSharedMergeAggregateMapWithLimits(accumulator, accumulator.routes, channelMonitorRedisSharedRouteKey{channelID, modelName}, aggregate)
	case channelMonitorRedisSharedScopeGroupRoute:
		groupName, channelID, err := channelMonitorRedisSharedParseGroupChannelIdentity(identity)
		if err != nil {
			return err
		}
		return channelMonitorRedisSharedMergeAggregateMapWithLimits(accumulator, accumulator.groupChannels, channelMonitorRedisSharedGroupChannelKey{groupName, channelID}, aggregate)
	case channelMonitorRedisSharedScopeAPIKeyRoute:
		apiKeyID, channelID, modelName, groupName, err := channelMonitorRedisSharedParseAPIKeyScopeIdentity(identity)
		if err != nil {
			return err
		}
		return channelMonitorRedisSharedMergeAggregateMapWithLimits(accumulator, accumulator.apiKeyScopes, channelMonitorRedisSharedAPIKeyScopeKey{apiKeyID, channelID, modelName, groupName}, aggregate)
	}
	return nil
}

func (accumulator *channelMonitorRedisSharedQueryAccumulator) addFailure(identity string, metrics map[string]string) error {
	channelID, modelName, groupName, statusCode, errorType, errorCode, err := channelMonitorRedisSharedParseFailureIdentity(identity)
	if err != nil {
		return err
	}
	key := channelMonitorRedisSharedFailureKey{channelID, modelName, groupName, statusCode, errorType, errorCode}
	if _, exists := accumulator.failures[key]; !exists {
		if len(accumulator.failures) >= accumulator.limits.MaxDimensionEntries {
			return &ChannelMonitorRedisSharedProjectionLimitError{
				Resource: "dimension_entries", Limit: int64(accumulator.limits.MaxDimensionEntries), Actual: int64(len(accumulator.failures) + 1),
			}
		}
		if err := accumulator.ensureDimensionCapacity(false); err != nil {
			return err
		}
	}
	category := accumulator.failures[key]
	category.ChannelID, category.ModelName, category.GroupName = channelID, modelName, groupName
	category.StatusCode, category.ErrorType, category.ErrorCode = statusCode, errorType, errorCode
	for metric, raw := range metrics {
		switch metric {
		case channelMonitorRedisSharedMetricFailureActual:
			value, parseErr := strconv.ParseInt(raw, 10, 64)
			if parseErr != nil {
				return parseErr
			}
			category.ActualCount += value
		case channelMonitorRedisSharedMetricFailureFinal:
			value, parseErr := strconv.ParseInt(raw, 10, 64)
			if parseErr != nil {
				return parseErr
			}
			category.FinalCount += value
		case channelMonitorRedisSharedMetricFailureLast:
			value, parseErr := strconv.ParseInt(raw, 10, 64)
			if parseErr != nil {
				return parseErr
			}
			category.LastOccurred = value
		case channelMonitorRedisSharedMetricFailureLastSeq:
			value, parseErr := strconv.ParseUint(raw, 10, 64)
			if parseErr != nil {
				return parseErr
			}
			category.lastEventSequence = value
		case channelMonitorRedisSharedMetricFailureSample:
			category.SampleContent = raw
		}
	}
	previous := accumulator.failures[key]
	if category.LastOccurred < previous.LastOccurred ||
		category.LastOccurred == previous.LastOccurred && category.lastEventSequence < previous.lastEventSequence {
		category.LastOccurred = previous.LastOccurred
		category.lastEventSequence = previous.lastEventSequence
		category.SampleContent = previous.SampleContent
	}
	category.ActualCount += previous.ActualCount
	category.FinalCount += previous.FinalCount
	accumulator.failures[key] = category
	return nil
}

func (accumulator *channelMonitorRedisSharedQueryAccumulator) apply(view *ChannelMonitorRedisSharedProjectionView) {
	view.Summary = accumulator.summary
	view.Performance = accumulator.performance
	view.Channels = accumulator.channels
	view.Models = accumulator.models
	view.Groups = accumulator.groups
	view.APIKeys = accumulator.apiKeys
}

func mergeChannelMonitorRedisSharedAggregateMap[K comparable](items map[K]ChannelMonitorRedisSharedAggregate, key K, source ChannelMonitorRedisSharedAggregate) error {
	target := items[key]
	if err := mergeChannelMonitorRedisSharedAggregate(&target, source); err != nil {
		return err
	}
	items[key] = target
	return nil
}

func mergeChannelMonitorRedisSharedAggregate(target *ChannelMonitorRedisSharedAggregate, source ChannelMonitorRedisSharedAggregate) error {
	for _, value := range []struct {
		target *int64
		delta  int64
	}{
		{&target.EventCount, source.EventCount},
		{&target.BusinessRequestCount, source.BusinessRequestCount},
		{&target.ActualSuccessCount, source.ActualSuccessCount},
		{&target.ActualFailureCount, source.ActualFailureCount},
		{&target.FinalSuccessCount, source.FinalSuccessCount},
		{&target.FinalFailureCount, source.FinalFailureCount},
		{&target.FirstTokenSampleCount, source.FirstTokenSampleCount},
		{&target.AttemptDurationSampleCount, source.AttemptDurationSampleCount},
		{&target.AttemptDurationTotalMs, source.AttemptDurationTotalMs},
		{&target.TPSSampleCount, source.TPSSampleCount},
		{&target.TPSOutputTokens, source.TPSOutputTokens},
		{&target.TPSGenerationDurationMs, source.TPSGenerationDurationMs},
		{&target.CacheSampleCount, source.CacheSampleCount},
		{&target.CacheHitCount, source.CacheHitCount},
		{&target.CacheReadTokens, source.CacheReadTokens},
		{&target.CacheWriteRequestCount, source.CacheWriteRequestCount},
		{&target.CacheWriteTokens, source.CacheWriteTokens},
		{&target.InputTokens, source.InputTokens},
		{&target.SettledCostNanoCNY, source.SettledCostNanoCNY},
		{&target.SettledRequestCount, source.SettledRequestCount},
		{&target.UnresolvedCostNanoCNY, source.UnresolvedCostNanoCNY},
		{&target.UnresolvedRequestCount, source.UnresolvedRequestCount},
		{&target.ProbeSettledCostNanoCNY, source.ProbeSettledCostNanoCNY},
		{&target.GroupProbeSettledCostNanoCNY, source.GroupProbeSettledCostNanoCNY},
		{&target.ModelDetectionSettledCostNanoCNY, source.ModelDetectionSettledCostNanoCNY},
	} {
		if _, err := channelMonitorRedisSharedCheckedAddInt64(*value.target, value.delta); err != nil {
			return err
		}
	}
	target.EventCount += source.EventCount
	target.BusinessRequestCount += source.BusinessRequestCount
	target.ActualSuccessCount += source.ActualSuccessCount
	target.ActualFailureCount += source.ActualFailureCount
	target.FinalSuccessCount += source.FinalSuccessCount
	target.FinalFailureCount += source.FinalFailureCount
	target.FirstTokenSampleCount += source.FirstTokenSampleCount
	target.FirstTokenTotalMs += source.FirstTokenTotalMs
	target.AttemptDurationSampleCount += source.AttemptDurationSampleCount
	target.AttemptDurationTotalMs += source.AttemptDurationTotalMs
	target.TPSSampleCount += source.TPSSampleCount
	target.TPSOutputTokens += source.TPSOutputTokens
	target.TPSGenerationDurationMs += source.TPSGenerationDurationMs
	target.CacheSampleCount += source.CacheSampleCount
	target.CacheHitCount += source.CacheHitCount
	target.CacheReadTokens += source.CacheReadTokens
	target.CacheWriteRequestCount += source.CacheWriteRequestCount
	target.CacheWriteTokens += source.CacheWriteTokens
	target.InputTokens += source.InputTokens
	target.SettledCostNanoCNY += source.SettledCostNanoCNY
	target.SettledRequestCount += source.SettledRequestCount
	target.UnresolvedCostNanoCNY += source.UnresolvedCostNanoCNY
	target.UnresolvedRequestCount += source.UnresolvedRequestCount
	target.ProbeSettledCostNanoCNY += source.ProbeSettledCostNanoCNY
	target.GroupProbeSettledCostNanoCNY += source.GroupProbeSettledCostNanoCNY
	target.ModelDetectionSettledCostNanoCNY += source.ModelDetectionSettledCostNanoCNY
	if source.APIKeyName != "" && (target.APIKeyName == "" || source.LastUsedTime >= target.LastUsedTime) {
		target.APIKeyName = source.APIKeyName
	}
	if source.LastUsedTime > target.LastUsedTime {
		target.LastUsedTime = source.LastUsedTime
	}
	if source.LatestFirstTokenMs != nil && (target.LatestFirstTokenMs == nil ||
		source.latestFirstTokenAt > target.latestFirstTokenAt ||
		source.latestFirstTokenAt == target.latestFirstTokenAt && source.latestFirstTokenSequence > target.latestFirstTokenSequence) {
		value := *source.LatestFirstTokenMs
		target.LatestFirstTokenMs = &value
		target.latestFirstTokenAt = source.latestFirstTokenAt
		target.latestFirstTokenSequence = source.latestFirstTokenSequence
	}
	if source.LatestTPS != nil && (target.LatestTPS == nil ||
		source.latestTPSAt > target.latestTPSAt ||
		source.latestTPSAt == target.latestTPSAt && source.latestTPSSequence > target.latestTPSSequence) {
		value := *source.LatestTPS
		target.LatestTPS = &value
		target.latestTPSAt = source.latestTPSAt
		target.latestTPSSequence = source.latestTPSSequence
	}
	return nil
}

func channelMonitorRedisSharedCheckedAddInt64(left int64, right int64) (int64, error) {
	if right > 0 && left > math.MaxInt64-right || right < 0 && left < math.MinInt64-right {
		return 0, errors.New("渠道监控 Redis 汇总超过 int64 范围")
	}
	return left + right, nil
}

func finalizeChannelMonitorRedisSharedView(view *ChannelMonitorRedisSharedProjectionView, accumulator *channelMonitorRedisSharedQueryAccumulator) {
	view.Routes = make([]ChannelMonitorRedisSharedRouteAggregate, 0, len(accumulator.routes))
	for key, aggregate := range accumulator.routes {
		view.Routes = append(view.Routes, ChannelMonitorRedisSharedRouteAggregate{ChannelID: key.channelID, ModelName: key.modelName, ChannelMonitorRedisSharedAggregate: aggregate})
	}
	view.GroupChannels = make([]ChannelMonitorRedisSharedGroupChannelAggregate, 0, len(accumulator.groupChannels))
	for key, aggregate := range accumulator.groupChannels {
		view.GroupChannels = append(view.GroupChannels, ChannelMonitorRedisSharedGroupChannelAggregate{GroupName: key.groupName, ChannelID: key.channelID, ChannelMonitorRedisSharedAggregate: aggregate})
	}
	view.APIKeyScopes = make([]ChannelMonitorRedisSharedAPIKeyScopeAggregate, 0, len(accumulator.apiKeyScopes))
	for key, aggregate := range accumulator.apiKeyScopes {
		view.APIKeyScopes = append(view.APIKeyScopes, ChannelMonitorRedisSharedAPIKeyScopeAggregate{APIKeyID: key.apiKeyID, ChannelID: key.channelID, ModelName: key.modelName, GroupName: key.groupName, ChannelMonitorRedisSharedAggregate: aggregate})
	}
	view.Failures = make([]ChannelMonitorRedisSharedFailureCategory, 0, len(accumulator.failures))
	for _, category := range accumulator.failures {
		view.Failures = append(view.Failures, category)
	}
	sort.Slice(view.Routes, func(i, j int) bool {
		if view.Routes[i].ModelName != view.Routes[j].ModelName {
			return view.Routes[i].ModelName < view.Routes[j].ModelName
		}
		return view.Routes[i].ChannelID < view.Routes[j].ChannelID
	})
	sort.Slice(view.GroupChannels, func(i, j int) bool {
		if view.GroupChannels[i].GroupName != view.GroupChannels[j].GroupName {
			return view.GroupChannels[i].GroupName < view.GroupChannels[j].GroupName
		}
		return view.GroupChannels[i].ChannelID < view.GroupChannels[j].ChannelID
	})
	sort.Slice(view.APIKeyScopes, func(i, j int) bool {
		if view.APIKeyScopes[i].APIKeyID != view.APIKeyScopes[j].APIKeyID {
			return view.APIKeyScopes[i].APIKeyID < view.APIKeyScopes[j].APIKeyID
		}
		if view.APIKeyScopes[i].ChannelID != view.APIKeyScopes[j].ChannelID {
			return view.APIKeyScopes[i].ChannelID < view.APIKeyScopes[j].ChannelID
		}
		if view.APIKeyScopes[i].ModelName != view.APIKeyScopes[j].ModelName {
			return view.APIKeyScopes[i].ModelName < view.APIKeyScopes[j].ModelName
		}
		return view.APIKeyScopes[i].GroupName < view.APIKeyScopes[j].GroupName
	})
	sort.Slice(view.Failures, func(i, j int) bool {
		if view.Failures[i].ChannelID != view.Failures[j].ChannelID {
			return view.Failures[i].ChannelID < view.Failures[j].ChannelID
		}
		if view.Failures[i].ModelName != view.Failures[j].ModelName {
			return view.Failures[i].ModelName < view.Failures[j].ModelName
		}
		if view.Failures[i].GroupName != view.Failures[j].GroupName {
			return view.Failures[i].GroupName < view.Failures[j].GroupName
		}
		if view.Failures[i].StatusCode != view.Failures[j].StatusCode {
			return view.Failures[i].StatusCode < view.Failures[j].StatusCode
		}
		if view.Failures[i].ErrorType != view.Failures[j].ErrorType {
			return view.Failures[i].ErrorType < view.Failures[j].ErrorType
		}
		return view.Failures[i].ErrorCode < view.Failures[j].ErrorCode
	})
	clearChannelMonitorRedisSharedInternalMetadata(view)
}

func clearChannelMonitorRedisSharedInternalMetadata(view *ChannelMonitorRedisSharedProjectionView) {
	clearAggregate := func(aggregate *ChannelMonitorRedisSharedAggregate) {
		aggregate.latestFirstTokenAt = 0
		aggregate.latestFirstTokenSequence = 0
		aggregate.latestTPSAt = 0
		aggregate.latestTPSSequence = 0
	}
	clearAggregate(&view.Summary)
	clearAggregate(&view.Performance)
	for key, aggregate := range view.Channels {
		clearAggregate(&aggregate)
		view.Channels[key] = aggregate
	}
	for key, aggregate := range view.Models {
		clearAggregate(&aggregate)
		view.Models[key] = aggregate
	}
	for key, aggregate := range view.Groups {
		clearAggregate(&aggregate)
		view.Groups[key] = aggregate
	}
	for key, aggregate := range view.APIKeys {
		clearAggregate(&aggregate)
		view.APIKeys[key] = aggregate
	}
	for index := range view.Routes {
		clearAggregate(&view.Routes[index].ChannelMonitorRedisSharedAggregate)
	}
	for index := range view.GroupChannels {
		clearAggregate(&view.GroupChannels[index].ChannelMonitorRedisSharedAggregate)
	}
	for index := range view.APIKeyScopes {
		clearAggregate(&view.APIKeyScopes[index].ChannelMonitorRedisSharedAggregate)
	}
	for index := range view.Failures {
		view.Failures[index].lastEventSequence = 0
	}
}

func addChannelMonitorRedisSharedHashValues(view *ChannelMonitorRedisSharedProjectionView, values map[string]string) error {
	accumulator := newChannelMonitorRedisSharedQueryAccumulator()
	if err := accumulator.add(values); err != nil {
		return err
	}
	accumulator.apply(view)
	return nil
}

func channelMonitorRedisSharedParseRouteIdentity(identity string) (int, string, error) {
	parts := strings.Split(identity, ".")
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("渠道监控 Redis route identity 无效: %s", identity)
	}
	channelID, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", err
	}
	modelName, err := channelMonitorRedisSharedDimensionDecode(parts[1])
	return channelID, modelName, err
}

func channelMonitorRedisSharedParseGroupChannelIdentity(identity string) (string, int, error) {
	parts := strings.Split(identity, ".")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("渠道监控 Redis group/channel identity 无效: %s", identity)
	}
	groupName, err := channelMonitorRedisSharedDimensionDecode(parts[0])
	if err != nil {
		return "", 0, err
	}
	channelID, err := strconv.Atoi(parts[1])
	return groupName, channelID, err
}

func channelMonitorRedisSharedParseAPIKeyScopeIdentity(identity string) (int, int, string, string, error) {
	parts := strings.Split(identity, ".")
	if len(parts) != 4 {
		return 0, 0, "", "", fmt.Errorf("渠道监控 Redis API Key scope identity 无效: %s", identity)
	}
	apiKeyID, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, "", "", err
	}
	channelID, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, "", "", err
	}
	modelName, err := channelMonitorRedisSharedDimensionDecode(parts[2])
	if err != nil {
		return 0, 0, "", "", err
	}
	groupName, err := channelMonitorRedisSharedDimensionDecode(parts[3])
	return apiKeyID, channelID, modelName, groupName, err
}

func channelMonitorRedisSharedParseFailureIdentity(identity string) (int, string, string, int, string, string, error) {
	parts := strings.Split(identity, ".")
	if len(parts) != 6 {
		return 0, "", "", 0, "", "", fmt.Errorf("渠道监控 Redis failure identity 无效: %s", identity)
	}
	channelID, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", "", 0, "", "", err
	}
	modelName, err := channelMonitorRedisSharedDimensionDecode(parts[1])
	if err != nil {
		return 0, "", "", 0, "", "", err
	}
	groupName, err := channelMonitorRedisSharedDimensionDecode(parts[2])
	if err != nil {
		return 0, "", "", 0, "", "", err
	}
	statusCode, err := strconv.Atoi(parts[3])
	if err != nil {
		return 0, "", "", 0, "", "", err
	}
	errorType, err := channelMonitorRedisSharedDimensionDecode(parts[4])
	if err != nil {
		return 0, "", "", 0, "", "", err
	}
	errorCode, err := channelMonitorRedisSharedDimensionDecode(parts[5])
	return channelID, modelName, groupName, statusCode, errorType, errorCode, err
}

func addChannelMonitorRedisDailyCostHashValues(view *ChannelMonitorRedisSharedDailyCostView, values map[string]string) error {
	return addChannelMonitorRedisDailyCostHashValuesWithLimits(view, values, defaultChannelMonitorRedisSharedProjectionLimits())
}

func addChannelMonitorRedisDailyCostHashValuesWithLimits(view *ChannelMonitorRedisSharedDailyCostView, values map[string]string, limits ChannelMonitorRedisSharedProjectionLimits) error {
	temporary := ChannelMonitorRedisSharedProjectionView{Channels: make(map[int]ChannelMonitorRedisSharedAggregate), Models: make(map[string]ChannelMonitorRedisSharedAggregate), Groups: make(map[string]ChannelMonitorRedisSharedAggregate), APIKeys: make(map[int]ChannelMonitorRedisSharedAggregate)}
	accumulator := newChannelMonitorRedisSharedQueryAccumulatorWithLimits(limits)
	if err := accumulator.add(values); err != nil {
		return err
	}
	accumulator.apply(&temporary)
	view.Global = temporary.Summary
	view.Channels = temporary.Channels
	view.Models = temporary.Models
	view.Groups = temporary.Groups
	view.APIKeys = temporary.APIKeys
	return nil
}

func initializeDailyCostView(view *ChannelMonitorRedisSharedDailyCostView) {
	view.Channels = make(map[int]ChannelMonitorRedisSharedAggregate)
	view.Models = make(map[string]ChannelMonitorRedisSharedAggregate)
	view.Groups = make(map[string]ChannelMonitorRedisSharedAggregate)
	view.APIKeys = make(map[int]ChannelMonitorRedisSharedAggregate)
}

func addChannelMonitorRedisAggregateField(aggregate *ChannelMonitorRedisSharedAggregate, metric, raw string) error {
	switch metric {
	case channelMonitorRedisSharedMetricAPIKeyName:
		aggregate.APIKeyName = raw
		return nil
	case channelMonitorRedisSharedMetricFirstTokenTotalMs,
		channelMonitorRedisSharedMetricLatestFirstToken, channelMonitorRedisSharedMetricLatestTPS:
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return err
		}
		switch metric {
		case channelMonitorRedisSharedMetricFirstTokenTotalMs:
			aggregate.FirstTokenTotalMs += value
		case channelMonitorRedisSharedMetricLatestFirstToken:
			aggregate.LatestFirstTokenMs = &value
		case channelMonitorRedisSharedMetricLatestTPS:
			aggregate.LatestTPS = &value
		}
		return nil
	case channelMonitorRedisSharedMetricAttemptDurationTotalMs:
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		aggregate.AttemptDurationTotalMs += value
		return nil
	case channelMonitorRedisSharedMetricLatestFirstTokenAt, channelMonitorRedisSharedMetricLatestTPSAt:
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		if metric == channelMonitorRedisSharedMetricLatestFirstTokenAt {
			aggregate.latestFirstTokenAt = value
		} else {
			aggregate.latestTPSAt = value
		}
		return nil
	case channelMonitorRedisSharedMetricLatestFirstTokenSeq, channelMonitorRedisSharedMetricLatestTPSSeq:
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return err
		}
		if metric == channelMonitorRedisSharedMetricLatestFirstTokenSeq {
			aggregate.latestFirstTokenSequence = value
		} else {
			aggregate.latestTPSSequence = value
		}
		return nil
	case channelMonitorRedisSharedMetricLastUsedTime:
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		aggregate.LastUsedTime = max(aggregate.LastUsedTime, value)
		return nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	switch metric {
	case channelMonitorRedisSharedMetricEventCount:
		aggregate.EventCount += value
	case channelMonitorRedisSharedMetricBusinessRequests:
		aggregate.BusinessRequestCount += value
	case channelMonitorRedisSharedMetricActualSuccess:
		aggregate.ActualSuccessCount += value
	case channelMonitorRedisSharedMetricActualFailure:
		aggregate.ActualFailureCount += value
	case channelMonitorRedisSharedMetricFinalSuccess:
		aggregate.FinalSuccessCount += value
	case channelMonitorRedisSharedMetricFinalFailure:
		aggregate.FinalFailureCount += value
	case channelMonitorRedisSharedMetricFirstTokenSamples:
		aggregate.FirstTokenSampleCount += value
	case channelMonitorRedisSharedMetricAttemptDurationSamples:
		aggregate.AttemptDurationSampleCount += value
	case channelMonitorRedisSharedMetricTPSSamples:
		aggregate.TPSSampleCount += value
	case channelMonitorRedisSharedMetricTPSOutputTokens:
		aggregate.TPSOutputTokens += value
	case channelMonitorRedisSharedMetricTPSGenerationMs:
		aggregate.TPSGenerationDurationMs += value
	case channelMonitorRedisSharedMetricCacheSamples:
		aggregate.CacheSampleCount += value
	case channelMonitorRedisSharedMetricCacheHits:
		aggregate.CacheHitCount += value
	case channelMonitorRedisSharedMetricCacheReadTokens:
		aggregate.CacheReadTokens += value
	case channelMonitorRedisSharedMetricCacheWriteRequests:
		aggregate.CacheWriteRequestCount += value
	case channelMonitorRedisSharedMetricCacheWriteTokens:
		aggregate.CacheWriteTokens += value
	case channelMonitorRedisSharedMetricInputTokens:
		aggregate.InputTokens += value
	case channelMonitorRedisSharedMetricSettledCost:
		aggregate.SettledCostNanoCNY += value
	case channelMonitorRedisSharedMetricSettledRequests:
		aggregate.SettledRequestCount += value
	case channelMonitorRedisSharedMetricUnresolvedCost:
		aggregate.UnresolvedCostNanoCNY += value
	case channelMonitorRedisSharedMetricUnresolvedRequests:
		aggregate.UnresolvedRequestCount += value
	case channelMonitorRedisSharedMetricProbeSettledCost:
		aggregate.ProbeSettledCostNanoCNY += value
	case channelMonitorRedisSharedMetricGroupProbeSettledCost:
		aggregate.GroupProbeSettledCostNanoCNY += value
	case channelMonitorRedisSharedMetricDetectionSettledCost:
		aggregate.ModelDetectionSettledCostNanoCNY += value
	default:
		return nil
	}
	return nil
}

func parseInt64Field(values map[string]string, key string) (int64, error) {
	raw := values[key]
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}
func parseIntField(values map[string]string, key string) (int, error) {
	value, err := parseInt64Field(values, key)
	return int(value), err
}
func parseUint64Field(values map[string]string, key string) (uint64, error) {
	raw := values[key]
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseUint(raw, 10, 64)
}
