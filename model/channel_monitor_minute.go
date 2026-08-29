package model

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	channelMonitorMinuteSeconds              = int64(60)
	channelMonitorMinuteRetryLookupBatchSize = 500
)

const (
	ChannelMonitorSmartScheduleProbeLogKey = "channel_monitor_smart_schedule_probe"
	ChannelMonitorChannelTestLogKey        = "channel_monitor_channel_test"
)

// ChannelMonitorMinuteRouteMetric stores one minute of channel-monitor metrics
// for one channel/model/group combination in the primary database.
type ChannelMonitorMinuteRouteMetric struct {
	Id          int64  `gorm:"primaryKey"`
	MinuteStart int64  `gorm:"not null;uniqueIndex:idx_channel_monitor_minute_route_dimensions;index:idx_cm_route_lookup,priority:3;index:idx_cm_route_channel_window,priority:2;index:idx_cm_route_group_window,priority:2;index:idx_cm_route_model_window,priority:2"`
	ChannelId   int    `gorm:"not null;uniqueIndex:idx_channel_monitor_minute_route_dimensions;index:idx_cm_route_lookup,priority:1;index:idx_cm_route_channel_window,priority:1;index:idx_cm_route_group_window,priority:3;index:idx_cm_route_model_window,priority:3"`
	ModelKey    string `gorm:"size:32;not null;uniqueIndex:idx_channel_monitor_minute_route_dimensions;index:idx_cm_route_lookup,priority:2;index:idx_cm_route_channel_window,priority:3;index:idx_cm_route_group_window,priority:4;index:idx_cm_route_model_window,priority:1"`
	GroupKey    string `gorm:"size:32;not null;uniqueIndex:idx_channel_monitor_minute_route_dimensions;index:idx_cm_route_lookup,priority:4;index:idx_cm_route_channel_window,priority:4;index:idx_cm_route_group_window,priority:1;index:idx_cm_route_model_window,priority:4"`
	ModelName   string `gorm:"size:255;not null"`
	GroupName   string `gorm:"size:255;not null"`

	ActualSuccessCount          int64 `gorm:"not null"`
	ActualFailureCount          int64 `gorm:"not null"`
	FinalSuccessCount           int64 `gorm:"not null"`
	FinalFailureCount           int64 `gorm:"not null"`
	RateLimitActualFailureCount int64 `gorm:"not null;default:0"`
	RateLimitFinalFailureCount  int64 `gorm:"not null;default:0"`
	RetryFailureCount           int64 `gorm:"not null;default:0"`
	RetryFailureDurationTotalMs int64 `gorm:"not null;default:0"`
	RetryFailureUnder1sCount    int64 `gorm:"column:retry_failure_under_1s_count;not null;default:0"`
	RetryFailure1To3sCount      int64 `gorm:"column:retry_failure_1_to_3s_count;not null;default:0"`
	RetryFailure3To10sCount     int64 `gorm:"column:retry_failure_3_to_10s_count;not null;default:0"`
	RetryFailure10To30sCount    int64 `gorm:"column:retry_failure_10_to_30s_count;not null;default:0"`
	RetryFailure30To60sCount    int64 `gorm:"column:retry_failure_30_to_60s_count;not null;default:0"`
	RetryFailureOver60sCount    int64 `gorm:"column:retry_failure_over_60s_count;not null;default:0"`
	CacheHitCount               int64 `gorm:"not null"`
	CacheSampleCount            int64 `gorm:"not null"`
	CacheReadTokens             int64 `gorm:"not null;default:0"`
	InputTokens                 int64 `gorm:"not null;default:0"`
	CacheWriteCount             int64 `gorm:"not null"`

	SampleCount           int64   `gorm:"not null"`
	FirstTokenSampleCount int64   `gorm:"not null"`
	FirstTokenTotalMs     float64 `gorm:"not null"`
	LatestFirstTokenMs    *float64
	LatestFirstTokenAt    int64   `gorm:"not null"`
	TPSSampleCount        int64   `gorm:"not null"`
	TPSTotal              float64 `gorm:"not null"`
	LatestTPS             *float64
	LatestTPSAt           int64  `gorm:"not null"`
	LastUsedTime          int64  `gorm:"not null"`
	APIKeyKey             string `gorm:"-" json:"-"`
	APIKeyId              int    `gorm:"-" json:"-"`
	APIKeyName            string `gorm:"-" json:"-"`
}

// ChannelMonitorMinuteAPIKeyMetric stores minute-level success/failure/cache
// detail at API-key grain. Route performance fields intentionally do not
// exist here.
type ChannelMonitorMinuteAPIKeyMetric struct {
	Id          int64  `gorm:"primaryKey"`
	MinuteStart int64  `gorm:"not null;uniqueIndex:idx_channel_monitor_minute_api_key_dimensions;index:idx_cm_api_route_lookup,priority:3;index:idx_cm_api_channel_window,priority:2;index:idx_cm_api_group_window,priority:2;index:idx_cm_api_model_window,priority:2"`
	ChannelId   int    `gorm:"not null;uniqueIndex:idx_channel_monitor_minute_api_key_dimensions;index:idx_cm_api_route_lookup,priority:1;index:idx_cm_api_channel_window,priority:1;index:idx_cm_api_group_window,priority:3;index:idx_cm_api_model_window,priority:3"`
	ModelKey    string `gorm:"size:32;not null;uniqueIndex:idx_channel_monitor_minute_api_key_dimensions;index:idx_cm_api_route_lookup,priority:2;index:idx_cm_api_channel_window,priority:3;index:idx_cm_api_group_window,priority:4;index:idx_cm_api_model_window,priority:1"`
	GroupKey    string `gorm:"size:32;not null;uniqueIndex:idx_channel_monitor_minute_api_key_dimensions;index:idx_cm_api_route_lookup,priority:4;index:idx_cm_api_channel_window,priority:4;index:idx_cm_api_group_window,priority:1;index:idx_cm_api_model_window,priority:4"`
	APIKeyKey   string `gorm:"size:32;not null;uniqueIndex:idx_channel_monitor_minute_api_key_dimensions;index:idx_cm_api_route_lookup,priority:5;index:idx_cm_api_channel_window,priority:5;index:idx_cm_api_group_window,priority:5"`
	ModelName   string `gorm:"size:255;not null"`
	GroupName   string `gorm:"size:255;not null"`
	APIKeyId    int    `gorm:"not null"`
	APIKeyName  string `gorm:"size:255;not null"`

	ActualSuccessCount          int64 `gorm:"not null"`
	ActualFailureCount          int64 `gorm:"not null"`
	FinalSuccessCount           int64 `gorm:"not null"`
	FinalFailureCount           int64 `gorm:"not null"`
	RateLimitActualFailureCount int64 `gorm:"not null;default:0"`
	RateLimitFinalFailureCount  int64 `gorm:"not null;default:0"`
	RetryFailureCount           int64 `gorm:"not null;default:0"`
	RetryFailureDurationTotalMs int64 `gorm:"not null;default:0"`
	RetryFailureUnder1sCount    int64 `gorm:"column:retry_failure_under_1s_count;not null;default:0"`
	RetryFailure1To3sCount      int64 `gorm:"column:retry_failure_1_to_3s_count;not null;default:0"`
	RetryFailure3To10sCount     int64 `gorm:"column:retry_failure_3_to_10s_count;not null;default:0"`
	RetryFailure10To30sCount    int64 `gorm:"column:retry_failure_10_to_30s_count;not null;default:0"`
	RetryFailure30To60sCount    int64 `gorm:"column:retry_failure_30_to_60s_count;not null;default:0"`
	RetryFailureOver60sCount    int64 `gorm:"column:retry_failure_over_60s_count;not null;default:0"`
	CacheHitCount               int64 `gorm:"not null"`
	CacheSampleCount            int64 `gorm:"not null"`
	CacheReadTokens             int64 `gorm:"not null;default:0"`
	InputTokens                 int64 `gorm:"not null;default:0"`
	CacheWriteCount             int64 `gorm:"not null"`
}

func (ChannelMonitorMinuteRouteMetric) TableName() string {
	return channelMonitorMinuteRouteMetricTable
}

func (ChannelMonitorMinuteAPIKeyMetric) TableName() string {
	return channelMonitorMinuteAPIKeyMetricTable
}

type channelMonitorMinuteLog struct {
	ChannelId         int
	ModelName         string
	GroupName         string `gorm:"column:group_name"`
	TokenId           int
	TokenName         string
	Type              int
	IsRetryAttempt    bool
	IsStream          bool
	PromptTokens      int
	CompletionTokens  int
	UseTime           int
	Other             string
	CreatedAt         int64
	RequestId         string
	AttemptDurationMs int64
	FinalRetrySummary bool
	RateLimited       bool
}

type channelMonitorMinuteLogOther struct {
	FirstResponseTime     *float64 `json:"frt"`
	CacheTokens           *float64 `json:"cache_tokens"`
	CacheWriteTokens      *float64 `json:"cache_write_tokens"`
	CacheCreationTokens   *float64 `json:"cache_creation_tokens"`
	CacheCreationTokens5m *float64 `json:"cache_creation_tokens_5m"`
	CacheCreationTokens1h *float64 `json:"cache_creation_tokens_1h"`
	InputTokensTotal      *float64 `json:"input_tokens_total"`
	UsageSemantic         string   `json:"usage_semantic"`
	AttemptDurationMs     *int64   `json:"channel_monitor_attempt_duration_ms"`
	FinalRetrySummary     bool     `json:"channel_monitor_final_retry_summary"`
	SmartScheduleProbe    bool     `json:"channel_monitor_smart_schedule_probe"`
	ChannelTest           bool     `json:"channel_monitor_channel_test"`
	GroupProbe            bool     `json:"channel_monitor_group_probe"`
	StatusProbe           bool     `json:"channel_monitor_status_probe"`
	StatusCode            any      `json:"status_code"`
}

type channelMonitorMinuteAggregateKey struct {
	MinuteStart int64
	ChannelId   int
	ModelKey    string
	GroupKey    string
	APIKeyKey   string
}

type channelMonitorMinuteRouteAggregateKey struct {
	MinuteStart int64
	ChannelId   int
	ModelKey    string
	GroupKey    string
}

func channelMonitorMinuteStart(timestamp int64) int64 {
	return timestamp - timestamp%channelMonitorMinuteSeconds
}

func channelMonitorMinuteDimensionKey(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:16])
}

func channelMonitorMinuteAPIKeyIdentity(tokenId int, tokenName string) string {
	if tokenId > 0 {
		return fmt.Sprintf("id:%d", tokenId)
	}
	return "name:" + strings.TrimSpace(tokenName)
}

func channelMonitorMinuteMetricNames(value string, maxLength int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > maxLength {
		return string(runes[:maxLength])
	}
	return value
}

func channelMonitorMinuteOther(other string) (channelMonitorMinuteLogOther, bool) {
	if strings.TrimSpace(other) == "" {
		return channelMonitorMinuteLogOther{}, false
	}
	var parsed channelMonitorMinuteLogOther
	if err := common.UnmarshalJsonStr(other, &parsed); err != nil {
		return channelMonitorMinuteLogOther{}, false
	}
	return parsed, true
}

func channelMonitorMinuteAttemptDurationMs(useTime int, other channelMonitorMinuteLogOther, parsed bool) int64 {
	durationMs := int64(useTime)
	if durationMs < 0 {
		durationMs = 0
	} else if durationMs > math.MaxInt64/1000 {
		durationMs = math.MaxInt64
	} else {
		durationMs *= 1000
	}
	if parsed && other.AttemptDurationMs != nil && *other.AttemptDurationMs >= 0 {
		return *other.AttemptDurationMs
	}
	return durationMs
}

func channelMonitorMinuteNonZero(value *float64) bool {
	return value != nil && *value != 0
}

func channelMonitorMinuteTokenCount(value *float64) int64 {
	if value == nil || *value <= 0 || math.IsNaN(*value) {
		return 0
	}
	if math.IsInf(*value, 1) || *value >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(*value)
}

func channelMonitorMinuteAddTokens(total *int64, value int64) {
	if value <= 0 {
		return
	}
	if *total > math.MaxInt64-value {
		*total = math.MaxInt64
		return
	}
	*total += value
}

func (aggregate *ChannelMonitorMinuteRouteMetric) addCacheUtilization(log channelMonitorMinuteLog, other channelMonitorMinuteLogOther) {
	cacheReadTokens := channelMonitorMinuteTokenCount(other.CacheTokens)
	inputTokens := int64(max(log.PromptTokens, 0))
	if normalizedInputTokens := channelMonitorMinuteTokenCount(other.InputTokensTotal); normalizedInputTokens > 0 {
		inputTokens = normalizedInputTokens
	} else if strings.EqualFold(strings.TrimSpace(other.UsageSemantic), "anthropic") {
		channelMonitorMinuteAddTokens(&inputTokens, cacheReadTokens)
		cacheWriteTokens := channelMonitorMinuteTokenCount(other.CacheCreationTokens5m)
		channelMonitorMinuteAddTokens(&cacheWriteTokens, channelMonitorMinuteTokenCount(other.CacheCreationTokens1h))
		if cacheWriteTokens == 0 {
			cacheWriteTokens = channelMonitorMinuteTokenCount(other.CacheCreationTokens)
		}
		channelMonitorMinuteAddTokens(&inputTokens, cacheWriteTokens)
	}
	if inputTokens <= 0 {
		return
	}
	cacheReadTokens = min(cacheReadTokens, inputTokens)
	channelMonitorMinuteAddTokens(&aggregate.CacheReadTokens, cacheReadTokens)
	channelMonitorMinuteAddTokens(&aggregate.InputTokens, inputTokens)
}

func channelMonitorMinuteRateLimited(value any) bool {
	switch statusCode := value.(type) {
	case float64:
		return statusCode == 429
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(statusCode))
		return err == nil && parsed == 429
	case int:
		return statusCode == 429
	case int64:
		return statusCode == 429
	}
	return false
}

func (aggregate *ChannelMonitorMinuteRouteMetric) addLog(log channelMonitorMinuteLog) {
	if log.Type == LogTypeConsume {
		aggregate.ActualSuccessCount++
		aggregate.FinalSuccessCount++
	} else if log.Type == LogTypeError {
		if log.FinalRetrySummary {
			aggregate.FinalFailureCount++
			if log.RateLimited {
				aggregate.RateLimitFinalFailureCount++
			}
		} else {
			aggregate.ActualFailureCount++
			if log.RateLimited {
				aggregate.RateLimitActualFailureCount++
			}
			if log.IsRetryAttempt {
				if !log.RateLimited {
					aggregate.addRetryFailureDuration(log.AttemptDurationMs)
				}
			} else {
				aggregate.FinalFailureCount++
				if log.RateLimited {
					aggregate.RateLimitFinalFailureCount++
				}
			}
		}
	}

	parsedOther, parsed := channelMonitorMinuteOther(log.Other)
	// Cache utilization is defined for streaming responses; cache hit-rate
	// counters below remain request-based for compatibility.
	if log.Type == LogTypeConsume && log.IsStream {
		aggregate.addCacheUtilization(log, parsedOther)
	}
	if parsed && parsedOther.CacheTokens != nil {
		aggregate.CacheSampleCount++
		if channelMonitorMinuteNonZero(parsedOther.CacheTokens) {
			aggregate.CacheHitCount++
		}
	}
	if log.Type == LogTypeConsume && parsed {
		if channelMonitorMinuteNonZero(parsedOther.CacheWriteTokens) ||
			channelMonitorMinuteNonZero(parsedOther.CacheCreationTokens) ||
			channelMonitorMinuteNonZero(parsedOther.CacheCreationTokens5m) ||
			channelMonitorMinuteNonZero(parsedOther.CacheCreationTokens1h) {
			aggregate.CacheWriteCount++
		}
	}

	if log.Type != LogTypeConsume || !log.IsStream || strings.TrimSpace(log.ModelName) == "" {
		return
	}
	firstTokenMs := float64(0)
	hasFirstToken := parsed && parsedOther.FirstResponseTime != nil &&
		*parsedOther.FirstResponseTime > 0 &&
		!math.IsNaN(*parsedOther.FirstResponseTime) &&
		!math.IsInf(*parsedOther.FirstResponseTime, 0)
	if hasFirstToken {
		firstTokenMs = *parsedOther.FirstResponseTime
	}
	hasTPS := log.UseTime > 0 && log.CompletionTokens > 0
	tps := float64(0)
	if hasTPS {
		tps = float64(log.CompletionTokens) / float64(log.UseTime)
		hasTPS = !math.IsNaN(tps) && !math.IsInf(tps, 0)
	}
	if !hasFirstToken && !hasTPS {
		return
	}
	aggregate.SampleCount++
	if hasFirstToken {
		aggregate.FirstTokenSampleCount++
		aggregate.FirstTokenTotalMs += firstTokenMs
		if aggregate.LatestFirstTokenMs == nil || log.CreatedAt >= aggregate.LatestFirstTokenAt {
			value := firstTokenMs
			aggregate.LatestFirstTokenMs = &value
			aggregate.LatestFirstTokenAt = log.CreatedAt
		}
	}
	if hasTPS {
		aggregate.TPSSampleCount++
		aggregate.TPSTotal += tps
		if aggregate.LatestTPS == nil || log.CreatedAt >= aggregate.LatestTPSAt {
			value := tps
			aggregate.LatestTPS = &value
			aggregate.LatestTPSAt = log.CreatedAt
		}
	}
	if log.CreatedAt >= aggregate.LastUsedTime {
		aggregate.LastUsedTime = log.CreatedAt
	}
}

func (aggregate *ChannelMonitorMinuteRouteMetric) addRetryFailureDuration(durationMs int64) {
	if durationMs < 0 {
		durationMs = 0
	}
	aggregate.RetryFailureCount++
	if durationMs > math.MaxInt64-aggregate.RetryFailureDurationTotalMs {
		aggregate.RetryFailureDurationTotalMs = math.MaxInt64
	} else {
		aggregate.RetryFailureDurationTotalMs += durationMs
	}
	switch {
	case durationMs < 1000:
		aggregate.RetryFailureUnder1sCount++
	case durationMs < 3000:
		aggregate.RetryFailure1To3sCount++
	case durationMs < 10000:
		aggregate.RetryFailure3To10sCount++
	case durationMs < 30000:
		aggregate.RetryFailure10To30sCount++
	case durationMs < 60000:
		aggregate.RetryFailure30To60sCount++
	default:
		aggregate.RetryFailureOver60sCount++
	}
}

func (aggregate *ChannelMonitorMinuteRouteMetric) removeRetryFailureDuration(durationMs int64) {
	if aggregate.RetryFailureCount <= 0 {
		return
	}
	if durationMs < 0 {
		durationMs = 0
	}
	aggregate.RetryFailureCount--
	if durationMs >= aggregate.RetryFailureDurationTotalMs {
		aggregate.RetryFailureDurationTotalMs = 0
	} else {
		aggregate.RetryFailureDurationTotalMs -= durationMs
	}
	switch {
	case durationMs < 1000 && aggregate.RetryFailureUnder1sCount > 0:
		aggregate.RetryFailureUnder1sCount--
	case durationMs < 3000 && aggregate.RetryFailure1To3sCount > 0:
		aggregate.RetryFailure1To3sCount--
	case durationMs < 10000 && aggregate.RetryFailure3To10sCount > 0:
		aggregate.RetryFailure3To10sCount--
	case durationMs < 30000 && aggregate.RetryFailure10To30sCount > 0:
		aggregate.RetryFailure10To30sCount--
	case durationMs < 60000 && aggregate.RetryFailure30To60sCount > 0:
		aggregate.RetryFailure30To60sCount--
	case durationMs >= 60000 && aggregate.RetryFailureOver60sCount > 0:
		aggregate.RetryFailureOver60sCount--
	}
}

// addLog mirrors route counters into the API-key detail row while deliberately
// dropping route performance fields.
func (aggregate *ChannelMonitorMinuteAPIKeyMetric) addLog(log channelMonitorMinuteLog) {
	route := ChannelMonitorMinuteRouteMetric{}
	route.ActualSuccessCount = aggregate.ActualSuccessCount
	route.ActualFailureCount = aggregate.ActualFailureCount
	route.FinalSuccessCount = aggregate.FinalSuccessCount
	route.FinalFailureCount = aggregate.FinalFailureCount
	route.RateLimitActualFailureCount = aggregate.RateLimitActualFailureCount
	route.RateLimitFinalFailureCount = aggregate.RateLimitFinalFailureCount
	route.RetryFailureCount = aggregate.RetryFailureCount
	route.RetryFailureDurationTotalMs = aggregate.RetryFailureDurationTotalMs
	route.RetryFailureUnder1sCount = aggregate.RetryFailureUnder1sCount
	route.RetryFailure1To3sCount = aggregate.RetryFailure1To3sCount
	route.RetryFailure3To10sCount = aggregate.RetryFailure3To10sCount
	route.RetryFailure10To30sCount = aggregate.RetryFailure10To30sCount
	route.RetryFailure30To60sCount = aggregate.RetryFailure30To60sCount
	route.RetryFailureOver60sCount = aggregate.RetryFailureOver60sCount
	route.CacheHitCount = aggregate.CacheHitCount
	route.CacheSampleCount = aggregate.CacheSampleCount
	route.CacheReadTokens = aggregate.CacheReadTokens
	route.InputTokens = aggregate.InputTokens
	route.CacheWriteCount = aggregate.CacheWriteCount
	route.addLog(log)
	aggregate.ActualSuccessCount = route.ActualSuccessCount
	aggregate.ActualFailureCount = route.ActualFailureCount
	aggregate.FinalSuccessCount = route.FinalSuccessCount
	aggregate.FinalFailureCount = route.FinalFailureCount
	aggregate.RateLimitActualFailureCount = route.RateLimitActualFailureCount
	aggregate.RateLimitFinalFailureCount = route.RateLimitFinalFailureCount
	aggregate.RetryFailureCount = route.RetryFailureCount
	aggregate.RetryFailureDurationTotalMs = route.RetryFailureDurationTotalMs
	aggregate.RetryFailureUnder1sCount = route.RetryFailureUnder1sCount
	aggregate.RetryFailure1To3sCount = route.RetryFailure1To3sCount
	aggregate.RetryFailure3To10sCount = route.RetryFailure3To10sCount
	aggregate.RetryFailure10To30sCount = route.RetryFailure10To30sCount
	aggregate.RetryFailure30To60sCount = route.RetryFailure30To60sCount
	aggregate.RetryFailureOver60sCount = route.RetryFailureOver60sCount
	aggregate.CacheHitCount = route.CacheHitCount
	aggregate.CacheSampleCount = route.CacheSampleCount
	aggregate.CacheReadTokens = route.CacheReadTokens
	aggregate.InputTokens = route.InputTokens
	aggregate.CacheWriteCount = route.CacheWriteCount
}

func (aggregate *ChannelMonitorMinuteAPIKeyMetric) removeRetryFailureDuration(durationMs int64) {
	route := ChannelMonitorMinuteRouteMetric{
		RetryFailureCount:           aggregate.RetryFailureCount,
		RetryFailureDurationTotalMs: aggregate.RetryFailureDurationTotalMs,
		RetryFailureUnder1sCount:    aggregate.RetryFailureUnder1sCount,
		RetryFailure1To3sCount:      aggregate.RetryFailure1To3sCount,
		RetryFailure3To10sCount:     aggregate.RetryFailure3To10sCount,
		RetryFailure10To30sCount:    aggregate.RetryFailure10To30sCount,
		RetryFailure30To60sCount:    aggregate.RetryFailure30To60sCount,
		RetryFailureOver60sCount:    aggregate.RetryFailureOver60sCount,
	}
	route.removeRetryFailureDuration(durationMs)
	aggregate.RetryFailureCount = route.RetryFailureCount
	aggregate.RetryFailureDurationTotalMs = route.RetryFailureDurationTotalMs
	aggregate.RetryFailureUnder1sCount = route.RetryFailureUnder1sCount
	aggregate.RetryFailure1To3sCount = route.RetryFailure1To3sCount
	aggregate.RetryFailure3To10sCount = route.RetryFailure3To10sCount
	aggregate.RetryFailure10To30sCount = route.RetryFailure10To30sCount
	aggregate.RetryFailure30To60sCount = route.RetryFailure30To60sCount
	aggregate.RetryFailureOver60sCount = route.RetryFailureOver60sCount
}

func aggregateChannelMonitorMinuteLogs(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,

) ([]ChannelMonitorMinuteRouteMetric, []ChannelMonitorMinuteAPIKeyMetric, []ChannelMonitorMinuteDurationBucket, int, error) {
	return aggregateChannelMonitorMinuteLogsFromDatabase(ctx, LOG_DB, startTimestamp, endTimestamp)
}

func aggregateChannelMonitorMinuteLogsFromDatabase(
	ctx context.Context,
	logDB *gorm.DB,
	startTimestamp int64,
	endTimestamp int64,

) ([]ChannelMonitorMinuteRouteMetric, []ChannelMonitorMinuteAPIKeyMetric, []ChannelMonitorMinuteDurationBucket, int, error) {
	if startTimestamp >= endTimestamp {
		return []ChannelMonitorMinuteRouteMetric{}, []ChannelMonitorMinuteAPIKeyMetric{}, []ChannelMonitorMinuteDurationBucket{}, 0, nil
	}
	groupColumn := channelMonitorLogGroupColumn()
	selectColumns := "channel_id, model_name, " + groupColumn + " AS group_name, token_id, token_name, type, is_retry_attempt, is_stream, prompt_tokens, completion_tokens, use_time, other, created_at, request_id"
	rows, err := logDB.WithContext(ctx).
		Model(&Log{}).
		Select(selectColumns).
		Where("type IN ?", []int{LogTypeConsume, LogTypeError}).
		Where("channel_id > ?", 0).
		Where("created_at >= ? AND created_at < ?", startTimestamp, endTimestamp).
		Order("created_at ASC").
		Rows()
	if err != nil {
		return nil, nil, nil, 0, err
	}
	defer rows.Close()

	scannedLogRows := 0
	routeAggregates := make(map[channelMonitorMinuteRouteAggregateKey]*ChannelMonitorMinuteRouteMetric)
	apiKeyAggregates := make(map[channelMonitorMinuteAggregateKey]*ChannelMonitorMinuteAPIKeyMetric)
	durationBuckets := make(map[channelMonitorMinuteDurationBucketKey]*ChannelMonitorMinuteDurationBucket)
	type retryMatchKey struct {
		requestId  string
		channelId  int
		modelKey   string
		groupKey   string
		apiKeyKey  string
		durationMs int64
	}
	type pendingRetry struct {
		route     *ChannelMonitorMinuteRouteMetric
		apiKey    *ChannelMonitorMinuteAPIKeyMetric
		createdAt int64
	}
	retriesByKey := make(map[retryMatchKey][]pendingRetry)
	finalSummaryCountsByKey := make(map[retryMatchKey]int)
	targetRetryKeys := make(map[retryMatchKey]struct{})
	targetRetryRequestIds := make(map[string]struct{})
	retryKeys := make([]retryMatchKey, 0)
	for rows.Next() {
		var log channelMonitorMinuteLog
		if err := rows.Scan(
			&log.ChannelId,
			&log.ModelName,
			&log.GroupName,
			&log.TokenId,
			&log.TokenName,
			&log.Type,
			&log.IsRetryAttempt,
			&log.IsStream,
			&log.PromptTokens,
			&log.CompletionTokens,
			&log.UseTime,
			&log.Other,
			&log.CreatedAt,
			&log.RequestId,
		); err != nil {
			return nil, nil, nil, scannedLogRows, err
		}
		scannedLogRows++
		parsedOther, parsed := channelMonitorMinuteOther(log.Other)
		if parsed && (parsedOther.SmartScheduleProbe || parsedOther.ChannelTest || parsedOther.GroupProbe || parsedOther.StatusProbe) {
			continue
		}
		durationMs := channelMonitorMinuteAttemptDurationMs(log.UseTime, parsedOther, parsed)
		if parsed {
			log.FinalRetrySummary = parsedOther.FinalRetrySummary
			log.RateLimited = channelMonitorMinuteRateLimited(parsedOther.StatusCode)
		}
		log.AttemptDurationMs = durationMs
		modelName := channelMonitorMinuteMetricNames(channelSmartScheduleModelName(log.ModelName), 255)
		groupName := channelMonitorMinuteMetricNames(log.GroupName, 255)
		apiKeyName := channelMonitorMinuteMetricNames(log.TokenName, 255)
		modelKey := channelMonitorMinuteDimensionKey(modelName)
		groupKey := channelMonitorMinuteDimensionKey(groupName)
		apiKeyDimensionKey := channelMonitorMinuteDimensionKey(channelMonitorMinuteAPIKeyIdentity(log.TokenId, apiKeyName))
		routeKey := channelMonitorMinuteRouteAggregateKey{
			MinuteStart: channelMonitorMinuteStart(log.CreatedAt),
			ChannelId:   log.ChannelId,
			ModelKey:    modelKey,
			GroupKey:    groupKey,
		}
		apiAggregateKey := channelMonitorMinuteAggregateKey{
			MinuteStart: routeKey.MinuteStart,
			ChannelId:   routeKey.ChannelId,
			ModelKey:    routeKey.ModelKey,
			GroupKey:    routeKey.GroupKey,
			APIKeyKey:   apiKeyDimensionKey,
		}
		routeAggregate := routeAggregates[routeKey]
		if routeAggregate == nil {
			routeAggregate = &ChannelMonitorMinuteRouteMetric{
				MinuteStart: routeKey.MinuteStart,
				ChannelId:   log.ChannelId,
				ModelKey:    modelKey,
				GroupKey:    groupKey,
				ModelName:   modelName,
				GroupName:   groupName,
			}
			routeAggregates[routeKey] = routeAggregate
		}
		apiAggregate := apiKeyAggregates[apiAggregateKey]
		if apiAggregate == nil {
			apiAggregate = &ChannelMonitorMinuteAPIKeyMetric{
				MinuteStart: routeKey.MinuteStart,
				ChannelId:   log.ChannelId,
				ModelKey:    modelKey,
				GroupKey:    groupKey,
				APIKeyKey:   apiKeyDimensionKey,
				ModelName:   modelName,
				GroupName:   groupName,
				APIKeyId:    log.TokenId,
				APIKeyName:  apiKeyName,
			}
			apiKeyAggregates[apiAggregateKey] = apiAggregate
		} else if apiAggregate.APIKeyName == "" {
			apiAggregate.APIKeyName = apiKeyName
		}
		routeAggregate.addLog(log)
		apiAggregate.addLog(log)
		if log.Type == LogTypeConsume && log.IsStream && modelName != "" && parsed &&
			parsedOther.FirstResponseTime != nil && *parsedOther.FirstResponseTime > 0 &&
			!math.IsNaN(*parsedOther.FirstResponseTime) && !math.IsInf(*parsedOther.FirstResponseTime, 0) {
			bucketIndex := channelMonitorDurationBucketIndex(*parsedOther.FirstResponseTime)
			bucketKey := channelMonitorMinuteDurationBucketKey{
				MinuteStart: routeKey.MinuteStart,
				ChannelId:   log.ChannelId,
				ModelKey:    modelKey,
				GroupKey:    groupKey,
				BucketIndex: bucketIndex,
			}
			bucket := durationBuckets[bucketKey]
			if bucket == nil {
				bucket = &ChannelMonitorMinuteDurationBucket{
					MinuteStart: routeKey.MinuteStart,
					ChannelId:   log.ChannelId,
					ModelKey:    modelKey,
					GroupKey:    groupKey,
					BucketIndex: bucketIndex,
					ModelName:   modelName,
					GroupName:   groupName,
				}
				durationBuckets[bucketKey] = bucket
			}
			bucket.Count++
			bucket.TotalMs += *parsedOther.FirstResponseTime
		}
		if log.Type != LogTypeError || log.RequestId == "" || log.RateLimited {
			continue
		}
		retryKey := retryMatchKey{
			requestId:  log.RequestId,
			channelId:  log.ChannelId,
			modelKey:   modelKey,
			groupKey:   groupKey,
			apiKeyKey:  apiKeyDimensionKey,
			durationMs: durationMs,
		}
		if log.FinalRetrySummary {
			finalSummaryCountsByKey[retryKey]++
			continue
		}
		if log.IsRetryAttempt {
			retriesByKey[retryKey] = append(retriesByKey[retryKey], pendingRetry{
				route: routeAggregate, apiKey: apiAggregate, createdAt: log.CreatedAt,
			})
			if _, exists := targetRetryKeys[retryKey]; !exists {
				targetRetryKeys[retryKey] = struct{}{}
				targetRetryRequestIds[log.RequestId] = struct{}{}
				retryKeys = append(retryKeys, retryKey)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, scannedLogRows, err
	}
	requestIds := make([]string, 0, len(targetRetryRequestIds))
	for requestId := range targetRetryRequestIds {
		requestIds = append(requestIds, requestId)
	}
	sort.Strings(requestIds)
	outsideSelectColumns := "channel_id, model_name, " + groupColumn + " AS group_name, token_id, token_name, type, is_retry_attempt, use_time, other, created_at, request_id"
	for start := 0; start < len(requestIds); start += channelMonitorMinuteRetryLookupBatchSize {
		end := min(start+channelMonitorMinuteRetryLookupBatchSize, len(requestIds))
		var outsideLogs []channelMonitorMinuteLog
		if err := logDB.WithContext(ctx).
			Model(&Log{}).
			Select(outsideSelectColumns).
			Where("type = ?", LogTypeError).
			Where("channel_id > ?", 0).
			Where("request_id IN ?", requestIds[start:end]).
			Where("created_at < ? OR created_at >= ?", startTimestamp, endTimestamp).
			Order("created_at ASC").
			Find(&outsideLogs).Error; err != nil {
			return nil, nil, nil, scannedLogRows, err
		}
		scannedLogRows += len(outsideLogs)
		for _, log := range outsideLogs {
			parsedOther, parsed := channelMonitorMinuteOther(log.Other)
			if parsed && (parsedOther.SmartScheduleProbe || parsedOther.ChannelTest || parsedOther.GroupProbe || parsedOther.StatusProbe) {
				continue
			}
			durationMs := channelMonitorMinuteAttemptDurationMs(log.UseTime, parsedOther, parsed)
			modelName := channelMonitorMinuteMetricNames(channelSmartScheduleModelName(log.ModelName), 255)
			groupName := channelMonitorMinuteMetricNames(log.GroupName, 255)
			apiKeyName := channelMonitorMinuteMetricNames(log.TokenName, 255)
			retryKey := retryMatchKey{
				requestId:  log.RequestId,
				channelId:  log.ChannelId,
				modelKey:   channelMonitorMinuteDimensionKey(modelName),
				groupKey:   channelMonitorMinuteDimensionKey(groupName),
				apiKeyKey:  channelMonitorMinuteDimensionKey(channelMonitorMinuteAPIKeyIdentity(log.TokenId, apiKeyName)),
				durationMs: durationMs,
			}
			if _, exists := targetRetryKeys[retryKey]; !exists || parsed && channelMonitorMinuteRateLimited(parsedOther.StatusCode) {
				continue
			}
			if parsed && parsedOther.FinalRetrySummary {
				finalSummaryCountsByKey[retryKey]++
				continue
			}
			if log.IsRetryAttempt {
				retriesByKey[retryKey] = append(retriesByKey[retryKey], pendingRetry{
					createdAt: log.CreatedAt,
				})
			}
		}
	}
	for _, retryKey := range retryKeys {
		retries := retriesByKey[retryKey]
		summaryCount := finalSummaryCountsByKey[retryKey]
		if summaryCount == 0 {
			continue
		}
		sort.SliceStable(retries, func(i int, j int) bool {
			return retries[i].createdAt < retries[j].createdAt
		})
		matchedStart := max(len(retries)-summaryCount, 0)
		for _, retry := range retries[matchedStart:] {
			if retry.route == nil {
				continue
			}
			retry.route.removeRetryFailureDuration(retryKey.durationMs)
			retry.apiKey.removeRetryFailureDuration(retryKey.durationMs)
		}
	}

	metrics := make([]ChannelMonitorMinuteRouteMetric, 0, len(routeAggregates))
	for _, aggregate := range routeAggregates {
		metrics = append(metrics, *aggregate)
	}
	apiKeyMetrics := make([]ChannelMonitorMinuteAPIKeyMetric, 0, len(apiKeyAggregates))
	for _, aggregate := range apiKeyAggregates {
		apiKeyMetrics = append(apiKeyMetrics, *aggregate)
	}
	buckets := make([]ChannelMonitorMinuteDurationBucket, 0, len(durationBuckets))
	for _, bucket := range durationBuckets {
		buckets = append(buckets, *bucket)
	}
	return metrics, apiKeyMetrics, buckets, scannedLogRows, nil
}

// ChannelMonitorMinuteAggregationResult describes the work performed while
// rebuilding a minute range.
type ChannelMonitorMinuteAggregationResult struct {
	StartTimestamp     int64
	EndTimestamp       int64
	ScannedLogRows     int
	MetricRows         int
	APIKeyMetricRows   int
	DurationBucketRows int
}

func (result ChannelMonitorMinuteAggregationResult) GeneratedRows() int {
	return result.MetricRows + result.APIKeyMetricRows + result.DurationBucketRows
}

// AggregateChannelMonitorMinuteRange replaces the selected minute range with
// fresh aggregates. Replacing a short range makes delayed log writes harmless
// and keeps the task independent of the log database dialect.
func AggregateChannelMonitorMinuteRange(ctx context.Context, startTimestamp int64, endTimestamp int64) (int, error) {
	result, err := AggregateChannelMonitorMinuteRangeWithResult(ctx, startTimestamp, endTimestamp)
	if err != nil {
		return 0, err
	}
	return result.MetricRows, nil
}

// AggregateChannelMonitorMinuteRangeWithResult rebuilds the selected range and
// reports how many source and aggregate rows were processed.
func AggregateChannelMonitorMinuteRangeWithResult(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
) (ChannelMonitorMinuteAggregationResult, error) {
	startTimestamp, endTimestamp = channelMonitorMinuteRange(startTimestamp, endTimestamp)
	result := ChannelMonitorMinuteAggregationResult{
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	}
	if startTimestamp >= endTimestamp {
		return result, nil
	}
	metrics, apiKeyMetrics, durationBuckets, scannedLogRows, err := aggregateChannelMonitorMinuteLogs(ctx, startTimestamp, endTimestamp)
	result.ScannedLogRows = scannedLogRows
	result.MetricRows = len(metrics)
	result.APIKeyMetricRows = len(apiKeyMetrics)
	result.DurationBucketRows = len(durationBuckets)
	if err != nil {
		return result, err
	}
	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return replaceChannelMonitorMinuteAggregates(
			tx, startTimestamp, endTimestamp, metrics, apiKeyMetrics, durationBuckets,
		)
	})
	if err != nil {
		return result, err
	}
	InvalidateChannelMonitorAggregateCaches()
	return result, nil
}

// AggregateChannelMonitorMinuteRangeWithState serializes monitor writers on
// the primary database and commits aggregate rows and the shared watermark in
// one transaction.
func AggregateChannelMonitorMinuteRangeWithState(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
	publishWatermark bool,
) (ChannelMonitorMinuteAggregationResult, error) {
	return aggregateChannelMonitorMinuteRangeWithState(
		ctx, startTimestamp, endTimestamp, publishWatermark, publishWatermark,
	)
}

// BackfillChannelMonitorMinuteRangeWithState extends the continuous coverage
// start without moving the latest completed-minute watermark backwards.
func BackfillChannelMonitorMinuteRangeWithState(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
) (ChannelMonitorMinuteAggregationResult, error) {
	return aggregateChannelMonitorMinuteRangeWithState(
		ctx, startTimestamp, endTimestamp, false, true,
	)
}

// UpgradeChannelMonitorCacheUtilizationMetrics rebuilds the current Beijing
// day before switching cache displays from request hit rate to token
// utilization. Existing deployments keep their older aggregates until the
// cache-specific backfill replaces them in bounded chunks.
func UpgradeChannelMonitorCacheUtilizationMetrics(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
) (ChannelMonitorMinuteAggregationResult, bool, error) {
	startTimestamp, endTimestamp = channelMonitorMinuteRange(startTimestamp, endTimestamp)
	result := ChannelMonitorMinuteAggregationResult{
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	}
	if err := ensureChannelMonitorAggregationState(ctx); err != nil {
		return result, false, err
	}
	upgraded := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, err := lockChannelMonitorAggregationState(tx)
		if err != nil {
			return err
		}
		if state.CacheUtilizationVersion >= ChannelMonitorCacheUtilizationVersion {
			return nil
		}
		logDB := LOG_DB
		if LOG_DB == DB {
			logDB = tx
		}
		metrics, apiKeyMetrics, durationBuckets, scannedLogRows, err := aggregateChannelMonitorMinuteLogsFromDatabase(
			ctx, logDB, startTimestamp, endTimestamp,
		)
		result.ScannedLogRows = scannedLogRows
		result.MetricRows = len(metrics)
		result.APIKeyMetricRows = len(apiKeyMetrics)
		result.DurationBucketRows = len(durationBuckets)
		if err != nil {
			return err
		}
		if err := replaceChannelMonitorMinuteAggregates(
			tx, startTimestamp, endTimestamp, metrics, apiKeyMetrics, durationBuckets,
		); err != nil {
			return err
		}
		if err := tx.Model(&ChannelMonitorAggregationState{}).
			Where("id = ?", channelMonitorAggregationStateID).
			Updates(map[string]any{
				"cache_utilization_version":      ChannelMonitorCacheUtilizationVersion,
				"cache_utilization_covered_from": startTimestamp,
				"revision":                       gorm.Expr("revision + ?", 1),
				"updated_at":                     common.GetTimestamp(),
			}).Error; err != nil {
			return err
		}
		upgraded = true
		return nil
	})
	if err != nil {
		return result, false, err
	}
	if upgraded {
		InvalidateChannelMonitorAggregateCaches()
	}
	return result, upgraded, nil
}

// BackfillChannelMonitorCacheUtilizationRangeWithState extends only the cache
// utilization coverage. The regular aggregate coverage remains valid while
// legacy cache rows are replaced in the background.
func BackfillChannelMonitorCacheUtilizationRangeWithState(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
) (ChannelMonitorMinuteAggregationResult, error) {
	startTimestamp, endTimestamp = channelMonitorMinuteRange(startTimestamp, endTimestamp)
	result := ChannelMonitorMinuteAggregationResult{
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	}
	if startTimestamp >= endTimestamp {
		return result, nil
	}
	if err := ensureChannelMonitorAggregationState(ctx); err != nil {
		return result, err
	}
	skipped := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, err := lockChannelMonitorAggregationState(tx)
		if err != nil {
			return err
		}
		if state.CacheUtilizationVersion < ChannelMonitorCacheUtilizationVersion ||
			state.CacheUtilizationCoveredFrom <= 0 {
			skipped = true
			return nil
		}
		if state.CacheUtilizationCoveredFrom <= startTimestamp {
			skipped = true
			return nil
		}
		if startTimestamp > state.CacheUtilizationCoveredFrom ||
			endTimestamp < state.CacheUtilizationCoveredFrom {
			return fmt.Errorf(
				"cache utilization backfill range [%d, %d) does not connect to %d",
				startTimestamp, endTimestamp, state.CacheUtilizationCoveredFrom,
			)
		}
		logDB := LOG_DB
		if LOG_DB == DB {
			logDB = tx
		}
		metrics, apiKeyMetrics, durationBuckets, scannedLogRows, err := aggregateChannelMonitorMinuteLogsFromDatabase(
			ctx, logDB, startTimestamp, endTimestamp,
		)
		result.ScannedLogRows = scannedLogRows
		result.MetricRows = len(metrics)
		result.APIKeyMetricRows = len(apiKeyMetrics)
		result.DurationBucketRows = len(durationBuckets)
		if err != nil {
			return err
		}
		if err := replaceChannelMonitorMinuteAggregates(
			tx, startTimestamp, endTimestamp, metrics, apiKeyMetrics, durationBuckets,
		); err != nil {
			return err
		}
		return tx.Model(&ChannelMonitorAggregationState{}).
			Where("id = ?", channelMonitorAggregationStateID).
			Updates(map[string]any{
				"cache_utilization_covered_from": startTimestamp,
				"revision":                       gorm.Expr("revision + ?", 1),
				"updated_at":                     common.GetTimestamp(),
			}).Error
	})
	if err != nil {
		return result, err
	}
	if !skipped {
		InvalidateChannelMonitorAggregateCaches()
	}
	return result, nil
}

func aggregateChannelMonitorMinuteRangeWithState(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
	publishWatermark bool,
	extendCoverage bool,
) (ChannelMonitorMinuteAggregationResult, error) {
	startTimestamp, endTimestamp = channelMonitorMinuteRange(startTimestamp, endTimestamp)
	result := ChannelMonitorMinuteAggregationResult{
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	}
	if startTimestamp >= endTimestamp {
		return result, nil
	}
	if err := ensureChannelMonitorAggregationState(ctx); err != nil {
		return result, err
	}
	observedCoverage, err := GetChannelMonitorAggregationCoverage(ctx)
	if err != nil {
		return result, err
	}
	return aggregateChannelMonitorMinuteRangeFromObservation(
		ctx,
		startTimestamp,
		endTimestamp,
		publishWatermark,
		extendCoverage,
		observedCoverage,
	)
}

func aggregateChannelMonitorMinuteRangeFromObservation(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
	publishWatermark bool,
	extendCoverage bool,
	observedCoverage ChannelMonitorAggregationCoverage,
) (ChannelMonitorMinuteAggregationResult, error) {
	result := ChannelMonitorMinuteAggregationResult{
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	}
	skipped := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, err := lockChannelMonitorAggregationState(tx)
		if err != nil {
			return err
		}
		if publishWatermark &&
			state.LastPublishedRevision > observedCoverage.Revision &&
			state.LastPublishedStart <= startTimestamp &&
			state.LastPublishedEnd >= endTimestamp {
			skipped = true
			return nil
		}
		if extendCoverage && !publishWatermark {
			coveredFrom := state.CoveredFrom
			if coveredFrom <= 0 {
				coveredFrom = state.CompletedThrough
			}
			coverageAdvanced := state.CompletedThrough > observedCoverage.CompletedThrough ||
				(state.CoveredFrom > 0 &&
					(observedCoverage.CoveredFrom <= 0 || state.CoveredFrom < observedCoverage.CoveredFrom))
			if coverageAdvanced &&
				coveredFrom > 0 && coveredFrom <= startTimestamp && state.CompletedThrough >= endTimestamp {
				skipped = true
				return nil
			}
		}
		logDB := LOG_DB
		if LOG_DB == DB {
			logDB = tx
		}
		metrics, apiKeyMetrics, durationBuckets, scannedLogRows, err := aggregateChannelMonitorMinuteLogsFromDatabase(
			ctx, logDB, startTimestamp, endTimestamp,
		)
		result.ScannedLogRows = scannedLogRows
		result.MetricRows = len(metrics)
		result.APIKeyMetricRows = len(apiKeyMetrics)
		result.DurationBucketRows = len(durationBuckets)
		if err != nil {
			return err
		}
		if err := replaceChannelMonitorMinuteAggregates(
			tx, startTimestamp, endTimestamp, metrics, apiKeyMetrics, durationBuckets,
		); err != nil {
			return err
		}
		return updateChannelMonitorAggregationStateWithTx(
			tx, state, startTimestamp, endTimestamp, publishWatermark, extendCoverage,
		)
	})
	if err != nil {
		return result, err
	}
	if skipped {
		return result, nil
	}
	InvalidateChannelMonitorAggregateCaches()
	return result, nil
}

func replaceChannelMonitorMinuteAggregates(
	tx *gorm.DB,
	startTimestamp int64,
	endTimestamp int64,
	metrics []ChannelMonitorMinuteRouteMetric,
	apiKeyMetrics []ChannelMonitorMinuteAPIKeyMetric,
	durationBuckets []ChannelMonitorMinuteDurationBucket,
) error {
	if err := tx.Where("minute_start >= ? AND minute_start < ?", startTimestamp, endTimestamp).
		Delete(&ChannelMonitorMinuteRouteMetric{}).Error; err != nil {
		return err
	}
	if err := tx.Where("minute_start >= ? AND minute_start < ?", startTimestamp, endTimestamp).
		Delete(&ChannelMonitorMinuteAPIKeyMetric{}).Error; err != nil {
		return err
	}
	if tx.Migrator().HasTable(&ChannelMonitorMinuteDurationBucket{}) {
		if err := tx.Where("minute_start >= ? AND minute_start < ?", startTimestamp, endTimestamp).
			Delete(&ChannelMonitorMinuteDurationBucket{}).Error; err != nil {
			return err
		}
	}
	if len(metrics) > 0 {
		if err := tx.CreateInBatches(metrics, 500).Error; err != nil {
			return err
		}
	}
	if len(apiKeyMetrics) > 0 {
		if err := tx.CreateInBatches(apiKeyMetrics, 500).Error; err != nil {
			return err
		}
	}
	if tx.Migrator().HasTable(&ChannelMonitorMinuteDurationBucket{}) && len(durationBuckets) > 0 {
		return tx.CreateInBatches(durationBuckets, 500).Error
	}
	return nil
}

func channelMonitorMinuteRange(startTimestamp int64, endTimestamp int64) (int64, int64) {
	if endTimestamp <= 0 {
		endTimestamp = common.GetTimestamp()
	}
	return channelMonitorMinuteStart(startTimestamp), channelMonitorMinuteStart(endTimestamp)
}

type channelMonitorMinuteSuccessAggregateRow struct {
	ChannelId          int
	ModelName          string
	GroupName          string
	APIKeyId           int
	APIKeyName         string
	ActualSuccessCount int64
	ActualFailureCount int64
	FinalSuccessCount  int64
	FinalFailureCount  int64
	CacheHitCount      int64
	CacheSampleCount   int64
	CacheReadTokens    int64
	InputTokens        int64
	CacheWriteCount    int64
}

func getChannelMonitorMinuteSuccessRows(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
	filter ChannelMonitorSuccessFilter,
	includeCacheMetrics bool,
	includeAPIKeyMetrics bool,
) ([]channelMonitorSuccessRow, error) {
	return getChannelMonitorMinuteSuccessRowsWithObservationBoundary(
		ctx,
		startTimestamp,
		endTimestamp,
		filter,
		includeCacheMetrics,
		includeAPIKeyMetrics,
		false,
	)
}

func getChannelMonitorObservedMinuteSuccessRows(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
	filter ChannelMonitorSuccessFilter,
	includeCacheMetrics bool,
	includeAPIKeyMetrics bool,
) ([]channelMonitorSuccessRow, error) {
	return getChannelMonitorMinuteSuccessRowsWithObservationBoundary(
		ctx,
		startTimestamp,
		endTimestamp,
		filter,
		includeCacheMetrics,
		includeAPIKeyMetrics,
		true,
	)
}

func getChannelMonitorMinuteSuccessRowsWithObservationBoundary(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
	filter ChannelMonitorSuccessFilter,
	includeCacheMetrics bool,
	includeAPIKeyMetrics bool,
	applyObservationBoundary bool,
) ([]channelMonitorSuccessRow, error) {
	startTimestamp, endTimestamp = channelMonitorMinuteRange(startTimestamp, endTimestamp)
	if startTimestamp >= endTimestamp {
		return []channelMonitorSuccessRow{}, nil
	}
	metricTable := channelMonitorMinuteRouteMetricTable
	metricModel := any(&ChannelMonitorMinuteRouteMetric{})
	if includeAPIKeyMetrics {
		metricTable = channelMonitorMinuteAPIKeyMetricTable
		metricModel = &ChannelMonitorMinuteAPIKeyMetric{}
	}
	sumColumns :=
		"SUM(" + metricTable + ".actual_success_count) AS actual_success_count, " +
			"SUM(" + metricTable + ".actual_failure_count) AS actual_failure_count, " +
			"SUM(" + metricTable + ".final_success_count) AS final_success_count, " +
			"SUM(" + metricTable + ".final_failure_count) AS final_failure_count, " +
			"SUM(" + metricTable + ".cache_hit_count) AS cache_hit_count, " +
			"SUM(" + metricTable + ".cache_sample_count) AS cache_sample_count, " +
			"SUM(" + metricTable + ".cache_read_tokens) AS cache_read_tokens, " +
			"SUM(" + metricTable + ".input_tokens) AS input_tokens, " +
			"SUM(" + metricTable + ".cache_write_count) AS cache_write_count"
	selectColumns := metricTable + ".channel_id AS channel_id, " +
		"MIN(" + metricTable + ".model_name) AS model_name, " +
		"MIN(" + metricTable + ".group_name) AS group_name, " + sumColumns
	groupColumns := metricTable + ".channel_id, " + metricTable + ".model_key, " + metricTable + ".group_key"
	if includeAPIKeyMetrics {
		selectColumns = metricTable + ".channel_id AS channel_id, " +
			"MIN(" + metricTable + ".model_name) AS model_name, " +
			"MIN(" + metricTable + ".group_name) AS group_name, " +
			metricTable + ".api_key_id AS api_key_id, " +
			metricTable + ".api_key_name AS api_key_name, " +
			metricTable + ".api_key_key AS api_key_key, " + sumColumns
		groupColumns += ", " + metricTable + ".api_key_id, " + metricTable + ".api_key_name, " + metricTable + ".api_key_key"
	}
	query := DB.WithContext(ctx).
		Model(metricModel).
		Select(selectColumns).
		Where(metricTable+".minute_start >= ? AND "+metricTable+".minute_start < ?", startTimestamp, endTimestamp)
	if applyObservationBoundary {
		query = applyChannelMonitorObservationBoundary(query, metricTable)
	}
	if filter.ChannelId > 0 {
		query = query.Where(metricTable+".channel_id = ?", filter.ChannelId)
	}
	if filter.ModelName != "" {
		query = query.Where(metricTable+".model_key = ?", channelMonitorMinuteDimensionKey(filter.ModelName))
	}
	if filter.Group != "" {
		query = query.Where(metricTable+".group_key = ?", channelMonitorMinuteDimensionKey(filter.Group))
	}
	var aggregates []channelMonitorMinuteSuccessAggregateRow
	if err := query.Group(groupColumns).Scan(&aggregates).Error; err != nil {
		return nil, err
	}

	rows := make([]channelMonitorSuccessRow, 0, len(aggregates)*3)
	for _, aggregate := range aggregates {
		counts := channelMonitorSuccessCounts{
			actualSuccess: aggregate.ActualSuccessCount,
			actualFailure: aggregate.ActualFailureCount,
			finalSuccess:  aggregate.FinalSuccessCount,
			finalFailure:  aggregate.FinalFailureCount,
		}
		cacheHitCount, cacheSampleCount, cacheReadTokens, inputTokens, cacheWriteCount := int64(0), int64(0), int64(0), int64(0), int64(0)
		if includeCacheMetrics {
			cacheHitCount = aggregate.CacheHitCount
			cacheSampleCount = aggregate.CacheSampleCount
			cacheReadTokens = aggregate.CacheReadTokens
			inputTokens = aggregate.InputTokens
			cacheWriteCount = aggregate.CacheWriteCount
		}
		base := channelMonitorSuccessRow{
			ChannelId: aggregate.ChannelId,
			ModelName: aggregate.ModelName,
			GroupName: aggregate.GroupName,
			TokenId:   aggregate.APIKeyId,
			TokenName: aggregate.APIKeyName,
		}
		if counts.actualSuccess > 0 {
			row := base
			row.Type = LogTypeConsume
			row.Count = counts.actualSuccess
			row.CacheHitCount = cacheHitCount
			row.CacheSampleCount = cacheSampleCount
			row.CacheReadTokens = cacheReadTokens
			row.InputTokens = inputTokens
			row.CacheWriteCount = cacheWriteCount
			rows = append(rows, row)
		}
		if counts.finalFailure > 0 {
			row := base
			row.Type = LogTypeError
			row.Count = counts.finalFailure
			rows = append(rows, row)
		}
		retryFailures := counts.actualFailure - counts.finalFailure
		if retryFailures > 0 {
			row := base
			row.Type = LogTypeError
			row.IsRetryAttempt = new(bool)
			*row.IsRetryAttempt = true
			row.Count = retryFailures
			rows = append(rows, row)
		}
	}
	return rows, nil
}

type channelMonitorMinutePerformanceKey struct {
	channelId int
	modelKey  string
}

type channelMonitorMinuteLatestPerformanceValue struct {
	value float64
}

func getChannelMonitorMinuteLatestPerformanceValues(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
	valueColumn string,
	timeColumn string,
	applyObservationBoundary bool,
) (map[channelMonitorMinutePerformanceKey]channelMonitorMinuteLatestPerformanceValue, error) {
	metricTable := channelMonitorMinuteRouteMetricTable
	// Reduce the candidate set in SQL before reading values into Go. The
	// previous implementation sorted every row in the window and discarded all
	// but the first row per channel/model pair in Go, which made memory and sort
	// work proportional to the full retention window. The grouped subquery is
	// supported by SQLite, MySQL 5.7+, and PostgreSQL (unlike window functions,
	// which are not available on all supported MySQL versions).
	latestQuery := DB.WithContext(ctx).
		Model(&ChannelMonitorMinuteRouteMetric{}).
		Select(
			metricTable+".channel_id AS channel_id, "+
				metricTable+".model_key AS model_key, "+
				"MAX("+metricTable+"."+timeColumn+") AS latest_at",
		).
		Where(metricTable+".minute_start >= ? AND "+metricTable+".minute_start < ?", startTimestamp, endTimestamp).
		Where(metricTable+"."+timeColumn+" > ?", 0).
		// Match the old row-reader semantics: a NULL value is skipped and the
		// latest non-NULL sample is returned instead.
		Where(metricTable + "." + valueColumn + " IS NOT NULL")
	if applyObservationBoundary {
		latestQuery = applyChannelMonitorObservationBoundary(latestQuery, metricTable)
	}
	latestQuery = latestQuery.Group(metricTable + ".channel_id, " + metricTable + ".model_key")

	type latestPerformanceRow struct {
		ChannelId int
		ModelKey  string
		Value     sql.NullFloat64
	}
	var rows []latestPerformanceRow
	query := DB.WithContext(ctx).
		Model(&ChannelMonitorMinuteRouteMetric{}).
		Select(
			metricTable+".channel_id AS channel_id, "+
				metricTable+".model_key AS model_key, "+
				// Multiple dimensions (group/API key) may share the same
				// timestamp. MAX keeps the result one row per channel/model pair
				// without relying on dialect-specific DISTINCT ON or window syntax.
				"MAX("+metricTable+"."+valueColumn+") AS value",
		).
		Joins(
			"JOIN (?) AS latest ON latest.channel_id = "+metricTable+".channel_id"+
				" AND latest.model_key = "+metricTable+".model_key"+
				" AND latest.latest_at = "+metricTable+"."+timeColumn,
			latestQuery,
		).
		Where(metricTable+".minute_start >= ? AND "+metricTable+".minute_start < ?", startTimestamp, endTimestamp).
		Where(metricTable+"."+timeColumn+" > ?", 0)
	if applyObservationBoundary {
		query = applyChannelMonitorObservationBoundary(query, metricTable)
	}
	if err := query.Group(metricTable + ".channel_id, " + metricTable + ".model_key").Scan(&rows).Error; err != nil {
		return nil, err
	}

	values := make(map[channelMonitorMinutePerformanceKey]channelMonitorMinuteLatestPerformanceValue, len(rows))
	for _, row := range rows {
		if !row.Value.Valid {
			continue
		}
		key := channelMonitorMinutePerformanceKey{channelId: row.ChannelId, modelKey: row.ModelKey}
		values[key] = channelMonitorMinuteLatestPerformanceValue{value: row.Value.Float64}
	}
	return values, nil
}

func getChannelMonitorMinutePerformanceMetrics(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelMonitorPerformanceMetric, error) {
	return getChannelMonitorMinutePerformanceMetricsWithObservationBoundary(
		ctx, startTimestamp, endTimestamp, false,
	)
}

func getChannelMonitorObservedMinutePerformanceMetrics(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelMonitorPerformanceMetric, error) {
	return getChannelMonitorMinutePerformanceMetricsWithObservationBoundary(
		ctx, startTimestamp, endTimestamp, true,
	)
}

func getChannelMonitorMinutePerformanceMetricsWithObservationBoundary(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
	applyObservationBoundary bool,
) ([]ChannelMonitorPerformanceMetric, error) {
	startTimestamp, endTimestamp = channelMonitorMinuteRange(startTimestamp, endTimestamp)
	if startTimestamp >= endTimestamp {
		return []ChannelMonitorPerformanceMetric{}, nil
	}
	type performanceAggregate struct {
		ChannelId             int
		ModelKey              string
		ModelName             string
		SampleCount           int64
		FirstTokenSampleCount int64
		TPSSampleCount        int64
		FirstTokenTotalMs     float64
		TPSTotal              float64
		LastUsedTime          int64
	}
	metricTable := channelMonitorMinuteRouteMetricTable
	var aggregates []performanceAggregate
	query := DB.WithContext(ctx).
		Model(&ChannelMonitorMinuteRouteMetric{}).
		Select(
			metricTable+".channel_id AS channel_id, "+metricTable+".model_key AS model_key, "+
				"MIN("+metricTable+".model_name) AS model_name, "+
				"SUM("+metricTable+".sample_count) AS sample_count, "+
				"SUM("+metricTable+".first_token_sample_count) AS first_token_sample_count, "+
				"SUM("+metricTable+".tps_sample_count) AS tps_sample_count, "+
				"SUM("+metricTable+".first_token_total_ms) AS first_token_total_ms, "+
				"SUM("+metricTable+".tps_total) AS tps_total, "+
				"MAX("+metricTable+".last_used_time) AS last_used_time",
		).
		Where(metricTable+".minute_start >= ? AND "+metricTable+".minute_start < ?", startTimestamp, endTimestamp).
		Where(metricTable+".sample_count > ?", 0)
	if applyObservationBoundary {
		query = applyChannelMonitorObservationBoundary(query, metricTable)
	}
	err := query.
		Group(metricTable + ".channel_id, " + metricTable + ".model_key").
		Scan(&aggregates).Error
	if err != nil {
		return nil, err
	}
	latestFirstTokens, err := getChannelMonitorMinuteLatestPerformanceValues(
		ctx, startTimestamp, endTimestamp, "latest_first_token_ms", "latest_first_token_at", applyObservationBoundary,
	)
	if err != nil {
		return nil, err
	}
	latestTPSValues, err := getChannelMonitorMinuteLatestPerformanceValues(
		ctx, startTimestamp, endTimestamp, "latest_tps", "latest_tps_at", applyObservationBoundary,
	)
	if err != nil {
		return nil, err
	}

	result := make([]ChannelMonitorPerformanceMetric, 0, len(aggregates))
	for _, aggregate := range aggregates {
		metric := ChannelMonitorPerformanceMetric{
			ChannelId:             aggregate.ChannelId,
			ModelName:             aggregate.ModelName,
			SampleCount:           int(aggregate.SampleCount),
			FirstTokenSampleCount: int(aggregate.FirstTokenSampleCount),
			TPSSampleCount:        int(aggregate.TPSSampleCount),
			LastUsedTime:          aggregate.LastUsedTime,
		}
		if aggregate.FirstTokenSampleCount > 0 {
			value := aggregate.FirstTokenTotalMs / float64(aggregate.FirstTokenSampleCount)
			metric.AverageFirstTokenMs = &value
		}
		if aggregate.TPSSampleCount > 0 {
			value := aggregate.TPSTotal / float64(aggregate.TPSSampleCount)
			metric.AverageTPS = &value
		}
		key := channelMonitorMinutePerformanceKey{channelId: aggregate.ChannelId, modelKey: aggregate.ModelKey}
		if latest, exists := latestFirstTokens[key]; exists {
			value := latest.value
			metric.LatestFirstTokenMs = &value
		}
		if latest, exists := latestTPSValues[key]; exists {
			value := latest.value
			metric.LatestTPS = &value
		}
		result = append(result, metric)
	}
	sort.Slice(result, func(i int, j int) bool {
		if result[i].ModelName == result[j].ModelName {
			return result[i].ChannelId < result[j].ChannelId
		}
		return result[i].ModelName < result[j].ModelName
	})
	return result, nil
}

func channelMonitorMinuteDayBucketSQL() string {
	const offset = channelMonitorCostTimezoneOffsetSeconds
	switch {
	case common.UsingMainDatabase(common.DatabaseTypeMySQL):
		return fmt.Sprintf("FLOOR((minute_start + %d) / %d)", offset, channelMonitorCostDaySeconds)
	case common.UsingMainDatabase(common.DatabaseTypeClickHouse):
		return fmt.Sprintf("intDiv(minute_start + %d, %d)", offset, channelMonitorCostDaySeconds)
	default:
		return fmt.Sprintf("(minute_start + %d) / %d", offset, channelMonitorCostDaySeconds)
	}
}

func getChannelMonitorMinuteDailySuccessMetrics(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelMonitorDailySuccessMetric, error) {
	startTimestamp, endTimestamp = channelMonitorMinuteRange(startTimestamp, endTimestamp)
	if startTimestamp >= endTimestamp {
		return []ChannelMonitorDailySuccessMetric{}, nil
	}
	type dailyRow struct {
		DayBucket          int64 `gorm:"column:day_bucket"`
		ChannelId          int
		ActualSuccessCount int64
		ActualFailureCount int64
		FinalSuccessCount  int64
		FinalFailureCount  int64
		CacheHitCount      int64
		CacheSampleCount   int64
		CacheReadTokens    int64
		InputTokens        int64
		CacheWriteCount    int64
	}
	dayBucket := channelMonitorMinuteDayBucketSQL()
	var rows []dailyRow
	err := DB.WithContext(ctx).
		Model(&ChannelMonitorMinuteRouteMetric{}).
		Select(
			dayBucket+" AS day_bucket, channel_id, "+
				"SUM(actual_success_count) AS actual_success_count, "+
				"SUM(actual_failure_count) AS actual_failure_count, "+
				"SUM(final_success_count) AS final_success_count, "+
				"SUM(final_failure_count) AS final_failure_count, "+
				"SUM(cache_hit_count) AS cache_hit_count, "+
				"SUM(cache_sample_count) AS cache_sample_count, "+
				"SUM(cache_read_tokens) AS cache_read_tokens, "+
				"SUM(input_tokens) AS input_tokens, "+
				"SUM(cache_write_count) AS cache_write_count",
		).
		Where("minute_start >= ? AND minute_start < ?", startTimestamp, endTimestamp).
		Group(dayBucket + ", channel_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	type dailyAggregate struct {
		counts             channelMonitorSuccessCounts
		cacheWriteChannels map[int]struct{}
		cacheWriteRequests int64
	}
	aggregates := make(map[int64]*dailyAggregate)
	for _, row := range rows {
		dayStart := row.DayBucket*channelMonitorCostDaySeconds - channelMonitorCostTimezoneOffsetSeconds
		aggregate := aggregates[dayStart]
		if aggregate == nil {
			aggregate = &dailyAggregate{cacheWriteChannels: make(map[int]struct{})}
			aggregates[dayStart] = aggregate
		}
		aggregate.counts.actualSuccess += row.ActualSuccessCount
		aggregate.counts.actualFailure += row.ActualFailureCount
		aggregate.counts.finalSuccess += row.FinalSuccessCount
		aggregate.counts.finalFailure += row.FinalFailureCount
		aggregate.counts.cacheHit += row.CacheHitCount
		aggregate.counts.cacheSample += row.CacheSampleCount
		aggregate.counts.cacheRead += row.CacheReadTokens
		aggregate.counts.inputTokens += row.InputTokens
		if row.CacheWriteCount > 0 {
			aggregate.cacheWriteChannels[row.ChannelId] = struct{}{}
			aggregate.cacheWriteRequests += row.CacheWriteCount
		}
	}
	items := make([]ChannelMonitorDailySuccessMetric, 0, len(aggregates))
	for dayStart, aggregate := range aggregates {
		items = append(items, ChannelMonitorDailySuccessMetric{
			DayStart:               dayStart,
			Summary:                aggregate.counts.summary(),
			CacheWriteChannelCount: len(aggregate.cacheWriteChannels),
			CacheWriteRequestCount: aggregate.cacheWriteRequests,
		})
	}
	sort.Slice(items, func(i int, j int) bool {
		return items[i].DayStart < items[j].DayStart
	})
	return items, nil
}

func getChannelMonitorMinuteTodaySuccessMetrics(ctx context.Context, dayStart int64, generatedAt int64) (ChannelMonitorTodaySuccessMetrics, error) {
	dayEnd := dayStart + channelMonitorCostDaySeconds
	if ChannelDailyCostDayStart(generatedAt) == dayStart && generatedAt < dayEnd {
		dayEnd = generatedAt
	}
	routeRows, err := getChannelMonitorMinuteSuccessRows(
		ctx,
		dayStart,
		dayEnd,
		ChannelMonitorSuccessFilter{},
		true,
		false,
	)
	if err != nil {
		return ChannelMonitorTodaySuccessMetrics{}, err
	}
	apiKeyRows, err := getChannelMonitorMinuteSuccessRows(
		ctx,
		dayStart,
		dayEnd,
		ChannelMonitorSuccessFilter{},
		true,
		true,
	)
	if err != nil {
		return ChannelMonitorTodaySuccessMetrics{}, err
	}
	totalCounts := channelMonitorSuccessCounts{}
	channelCounts := make(map[int]*channelMonitorSuccessCounts)
	apiKeyCounts := make(map[channelMonitorSuccessAPIKeyKey]*channelMonitorSuccessAPIKeyAggregate)
	cacheWriteCounts := make(map[int]int64)
	for _, row := range routeRows {
		isRetryAttempt := row.IsRetryAttempt != nil && *row.IsRetryAttempt
		totalCounts.add(
			row.Type, isRetryAttempt, row.Count,
			row.CacheHitCount, row.CacheSampleCount,
			row.CacheReadTokens, row.InputTokens,
		)
		counts := channelCounts[row.ChannelId]
		if counts == nil {
			counts = &channelMonitorSuccessCounts{}
			channelCounts[row.ChannelId] = counts
		}
		counts.add(
			row.Type, isRetryAttempt, row.Count,
			row.CacheHitCount, row.CacheSampleCount,
			row.CacheReadTokens, row.InputTokens,
		)
		cacheWriteCounts[row.ChannelId] += row.CacheWriteCount
	}
	for _, row := range apiKeyRows {
		addChannelMonitorSuccessAPIKeyCount(apiKeyCounts, row)
	}

	channelItems := make([]ChannelMonitorChannelSuccessMetric, 0, len(channelCounts))
	for channelId, counts := range channelCounts {
		channelItems = append(channelItems, ChannelMonitorChannelSuccessMetric{
			ChannelId:                    channelId,
			ChannelMonitorSuccessSummary: counts.summary(),
		})
	}
	sort.Slice(channelItems, func(i int, j int) bool {
		return channelItems[i].ChannelId < channelItems[j].ChannelId
	})
	cacheWriteItems := make([]ChannelMonitorTodayCacheWriteMetric, 0, len(cacheWriteCounts))
	for channelId, requestCount := range cacheWriteCounts {
		if requestCount <= 0 {
			continue
		}
		cacheWriteItems = append(cacheWriteItems, ChannelMonitorTodayCacheWriteMetric{
			ChannelId:    channelId,
			RequestCount: requestCount,
		})
	}
	sort.Slice(cacheWriteItems, func(i int, j int) bool {
		return cacheWriteItems[i].ChannelId < cacheWriteItems[j].ChannelId
	})
	return ChannelMonitorTodaySuccessMetrics{
		Summary:         totalCounts.summary(),
		ChannelItems:    channelItems,
		APIKeyItems:     channelMonitorSuccessAPIKeyMetrics(apiKeyCounts),
		CacheWriteItems: cacheWriteItems,
	}, nil
}
