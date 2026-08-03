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

const channelMonitorMinuteSeconds = int64(60)

const (
	ChannelMonitorSmartScheduleProbeLogKey = "channel_monitor_smart_schedule_probe"
	ChannelMonitorChannelTestLogKey        = "channel_monitor_channel_test"
)

// ChannelMonitorMinuteMetric stores one minute of channel-monitor metrics for
// one channel/model/group/API-key combination. It is deliberately kept in the
// primary database so the dashboard does not rescan the log database.
type ChannelMonitorMinuteMetric struct {
	Id          int64  `gorm:"primaryKey"`
	MinuteStart int64  `gorm:"not null;uniqueIndex:idx_channel_monitor_minute_dimensions;index:idx_channel_monitor_minute_start"`
	ChannelId   int    `gorm:"not null;uniqueIndex:idx_channel_monitor_minute_dimensions;index:idx_channel_monitor_minute_channel"`
	ModelKey    string `gorm:"size:32;not null;uniqueIndex:idx_channel_monitor_minute_dimensions"`
	GroupKey    string `gorm:"size:32;not null;uniqueIndex:idx_channel_monitor_minute_dimensions"`
	APIKeyKey   string `gorm:"size:32;not null;uniqueIndex:idx_channel_monitor_minute_dimensions"`
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
	CacheWriteCount             int64 `gorm:"not null"`

	SampleCount           int64   `gorm:"not null"`
	FirstTokenSampleCount int64   `gorm:"not null"`
	FirstTokenTotalMs     float64 `gorm:"not null"`
	LatestFirstTokenMs    *float64
	LatestFirstTokenAt    int64   `gorm:"not null"`
	TPSSampleCount        int64   `gorm:"not null"`
	TPSTotal              float64 `gorm:"not null"`
	LatestTPS             *float64
	LatestTPSAt           int64 `gorm:"not null"`
	LastUsedTime          int64 `gorm:"not null"`
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
	AttemptDurationMs     *int64   `json:"channel_monitor_attempt_duration_ms"`
	FinalRetrySummary     bool     `json:"channel_monitor_final_retry_summary"`
	SmartScheduleProbe    bool     `json:"channel_monitor_smart_schedule_probe"`
	ChannelTest           bool     `json:"channel_monitor_channel_test"`
	StatusCode            any      `json:"status_code"`
}

type channelMonitorMinuteAggregateKey struct {
	MinuteStart int64
	ChannelId   int
	ModelKey    string
	GroupKey    string
	APIKeyKey   string
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

func channelMonitorMinuteNonZero(value *float64) bool {
	return value != nil && *value != 0
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

func (aggregate *ChannelMonitorMinuteMetric) addLog(log channelMonitorMinuteLog) {
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

func (aggregate *ChannelMonitorMinuteMetric) addRetryFailureDuration(durationMs int64) {
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

func (aggregate *ChannelMonitorMinuteMetric) removeRetryFailureDuration(durationMs int64) {
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

func aggregateChannelMonitorMinuteLogs(
	ctx context.Context,
	startTimestamp int64,
	endTimestamp int64,
) ([]ChannelMonitorMinuteMetric, []ChannelMonitorMinuteDurationBucket, error) {
	if startTimestamp >= endTimestamp {
		return []ChannelMonitorMinuteMetric{}, []ChannelMonitorMinuteDurationBucket{}, nil
	}
	groupColumn := channelMonitorLogGroupColumn()
	selectColumns := "channel_id, model_name, " + groupColumn + " AS group_name, token_id, token_name, type, is_retry_attempt, is_stream, completion_tokens, use_time, other, created_at, request_id"
	rows, err := LOG_DB.WithContext(ctx).
		Model(&Log{}).
		Select(selectColumns).
		Where("type IN ?", []int{LogTypeConsume, LogTypeError}).
		Where("channel_id > ?", 0).
		Where("created_at >= ? AND created_at < ?", startTimestamp, endTimestamp).
		Order("created_at ASC").
		Rows()
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	aggregates := make(map[channelMonitorMinuteAggregateKey]*ChannelMonitorMinuteMetric)
	durationBuckets := make(map[channelMonitorMinuteDurationBucketKey]*ChannelMonitorMinuteDurationBucket)
	type pendingRetry struct {
		aggregate  *ChannelMonitorMinuteMetric
		durationMs int64
	}
	retriesByRequest := make(map[string][]pendingRetry)
	finalSummariesByRequest := make(map[string][]pendingRetry)
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
			&log.CompletionTokens,
			&log.UseTime,
			&log.Other,
			&log.CreatedAt,
			&log.RequestId,
		); err != nil {
			return nil, nil, err
		}
		parsedOther, parsed := channelMonitorMinuteOther(log.Other)
		if parsed && (parsedOther.SmartScheduleProbe || parsedOther.ChannelTest) {
			continue
		}
		durationMs := int64(log.UseTime)
		if durationMs < 0 {
			durationMs = 0
		} else if durationMs > math.MaxInt64/1000 {
			durationMs = math.MaxInt64
		} else {
			durationMs *= 1000
		}
		if parsed {
			if parsedOther.AttemptDurationMs != nil && *parsedOther.AttemptDurationMs >= 0 {
				durationMs = *parsedOther.AttemptDurationMs
			}
			log.FinalRetrySummary = parsedOther.FinalRetrySummary
			log.RateLimited = channelMonitorMinuteRateLimited(parsedOther.StatusCode)
		}
		log.AttemptDurationMs = durationMs
		modelName := channelMonitorMinuteMetricNames(channelSmartScheduleModelName(log.ModelName), 255)
		groupName := channelMonitorMinuteMetricNames(log.GroupName, 255)
		apiKeyName := channelMonitorMinuteMetricNames(log.TokenName, 255)
		modelKey := channelMonitorMinuteDimensionKey(modelName)
		groupKey := channelMonitorMinuteDimensionKey(groupName)
		apiKeyKey := channelMonitorMinuteDimensionKey(channelMonitorMinuteAPIKeyIdentity(log.TokenId, apiKeyName))
		key := channelMonitorMinuteAggregateKey{
			MinuteStart: channelMonitorMinuteStart(log.CreatedAt),
			ChannelId:   log.ChannelId,
			ModelKey:    modelKey,
			GroupKey:    groupKey,
			APIKeyKey:   apiKeyKey,
		}
		aggregate := aggregates[key]
		if aggregate == nil {
			aggregate = &ChannelMonitorMinuteMetric{
				MinuteStart: key.MinuteStart,
				ChannelId:   log.ChannelId,
				ModelKey:    modelKey,
				GroupKey:    groupKey,
				APIKeyKey:   apiKeyKey,
				ModelName:   modelName,
				GroupName:   groupName,
				APIKeyId:    log.TokenId,
				APIKeyName:  apiKeyName,
			}
			aggregates[key] = aggregate
		} else if aggregate.APIKeyName == "" {
			aggregate.APIKeyName = apiKeyName
		}
		aggregate.addLog(log)
		if log.Type == LogTypeConsume && log.IsStream && modelName != "" && parsed &&
			parsedOther.FirstResponseTime != nil && *parsedOther.FirstResponseTime > 0 &&
			!math.IsNaN(*parsedOther.FirstResponseTime) && !math.IsInf(*parsedOther.FirstResponseTime, 0) {
			bucketIndex := channelMonitorDurationBucketIndex(*parsedOther.FirstResponseTime)
			bucketKey := channelMonitorMinuteDurationBucketKey{
				MinuteStart: key.MinuteStart,
				ChannelId:   log.ChannelId,
				ModelKey:    modelKey,
				GroupKey:    groupKey,
				BucketIndex: bucketIndex,
			}
			bucket := durationBuckets[bucketKey]
			if bucket == nil {
				bucket = &ChannelMonitorMinuteDurationBucket{
					MinuteStart: key.MinuteStart,
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
		if log.FinalRetrySummary {
			finalSummariesByRequest[log.RequestId] = append(
				finalSummariesByRequest[log.RequestId],
				pendingRetry{aggregate: aggregate, durationMs: durationMs},
			)
			continue
		}
		if log.IsRetryAttempt {
			retriesByRequest[log.RequestId] = append(
				retriesByRequest[log.RequestId],
				pendingRetry{aggregate: aggregate, durationMs: durationMs},
			)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	for requestId, summaries := range finalSummariesByRequest {
		retries := retriesByRequest[requestId]
		for _, summary := range summaries {
			matchedIndex := -1
			for index := len(retries) - 1; index >= 0; index-- {
				retry := retries[index]
				if retry.durationMs != summary.durationMs ||
					retry.aggregate.ChannelId != summary.aggregate.ChannelId ||
					retry.aggregate.ModelKey != summary.aggregate.ModelKey ||
					retry.aggregate.GroupKey != summary.aggregate.GroupKey ||
					retry.aggregate.APIKeyKey != summary.aggregate.APIKeyKey {
					continue
				}
				matchedIndex = index
				break
			}
			if matchedIndex < 0 {
				continue
			}
			retries[matchedIndex].aggregate.removeRetryFailureDuration(retries[matchedIndex].durationMs)
			retries = append(retries[:matchedIndex], retries[matchedIndex+1:]...)
		}
	}

	metrics := make([]ChannelMonitorMinuteMetric, 0, len(aggregates))
	for _, aggregate := range aggregates {
		metrics = append(metrics, *aggregate)
	}
	buckets := make([]ChannelMonitorMinuteDurationBucket, 0, len(durationBuckets))
	for _, bucket := range durationBuckets {
		buckets = append(buckets, *bucket)
	}
	return metrics, buckets, nil
}

// AggregateChannelMonitorMinuteRange replaces the selected minute range with
// fresh aggregates. Replacing a short range makes delayed log writes harmless
// and keeps the task independent of the log database dialect.
func AggregateChannelMonitorMinuteRange(ctx context.Context, startTimestamp int64, endTimestamp int64) (int, error) {
	startTimestamp, endTimestamp = channelMonitorMinuteRange(startTimestamp, endTimestamp)
	if startTimestamp >= endTimestamp {
		return 0, nil
	}
	metrics, durationBuckets, err := aggregateChannelMonitorMinuteLogs(ctx, startTimestamp, endTimestamp)
	if err != nil {
		return 0, err
	}
	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		hasDurationBuckets := tx.Migrator().HasTable(&ChannelMonitorMinuteDurationBucket{})
		if hasDurationBuckets {
			if err := tx.Where("minute_start >= ? AND minute_start < ?", startTimestamp, endTimestamp).
				Delete(&ChannelMonitorMinuteDurationBucket{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("minute_start >= ? AND minute_start < ?", startTimestamp, endTimestamp).
			Delete(&ChannelMonitorMinuteMetric{}).Error; err != nil {
			return err
		}
		if len(metrics) > 0 {
			if err := tx.CreateInBatches(metrics, 500).Error; err != nil {
				return err
			}
		}
		if hasDurationBuckets && len(durationBuckets) > 0 {
			return tx.CreateInBatches(durationBuckets, 500).Error
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(metrics), nil
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
	startTimestamp, endTimestamp = channelMonitorMinuteRange(startTimestamp, endTimestamp)
	if startTimestamp >= endTimestamp {
		return []channelMonitorSuccessRow{}, nil
	}
	sumColumns :=
		"SUM(actual_success_count) AS actual_success_count, " +
			"SUM(actual_failure_count) AS actual_failure_count, " +
			"SUM(final_success_count) AS final_success_count, " +
			"SUM(final_failure_count) AS final_failure_count, " +
			"SUM(cache_hit_count) AS cache_hit_count, " +
			"SUM(cache_sample_count) AS cache_sample_count, " +
			"SUM(cache_write_count) AS cache_write_count"
	selectColumns := "channel_id, model_name, group_name, " + sumColumns
	groupColumns := "channel_id, model_name, group_name"
	if includeAPIKeyMetrics {
		selectColumns = "channel_id, model_name, group_name, api_key_id, api_key_name, api_key_key, " + sumColumns
		groupColumns += ", api_key_id, api_key_name, api_key_key"
	}
	query := DB.WithContext(ctx).
		Model(&ChannelMonitorMinuteMetric{}).
		Select(selectColumns).
		Where("minute_start >= ? AND minute_start < ?", startTimestamp, endTimestamp)
	if filter.ChannelId > 0 {
		query = query.Where("channel_id = ?", filter.ChannelId)
	}
	if filter.ModelName != "" {
		query = query.Where("model_name = ?", filter.ModelName)
	}
	if filter.Group != "" {
		query = query.Where("group_name = ?", filter.Group)
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
		cacheHitCount, cacheSampleCount, cacheWriteCount := int64(0), int64(0), int64(0)
		if includeCacheMetrics {
			cacheHitCount = aggregate.CacheHitCount
			cacheSampleCount = aggregate.CacheSampleCount
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
	modelName string
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
) (map[channelMonitorMinutePerformanceKey]channelMonitorMinuteLatestPerformanceValue, error) {
	rows, err := DB.WithContext(ctx).
		Model(&ChannelMonitorMinuteMetric{}).
		Select("channel_id, model_name, "+valueColumn+", "+timeColumn).
		Where("minute_start >= ? AND minute_start < ?", startTimestamp, endTimestamp).
		Where(timeColumn+" > ?", 0).
		Order("channel_id ASC, model_name ASC, " + timeColumn + " DESC").
		Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make(map[channelMonitorMinutePerformanceKey]channelMonitorMinuteLatestPerformanceValue)
	for rows.Next() {
		var channelId int
		var modelName string
		var value sql.NullFloat64
		var occurredAt int64
		if err := rows.Scan(&channelId, &modelName, &value, &occurredAt); err != nil {
			return nil, err
		}
		key := channelMonitorMinutePerformanceKey{channelId: channelId, modelName: modelName}
		if _, exists := values[key]; exists || !value.Valid {
			continue
		}
		values[key] = channelMonitorMinuteLatestPerformanceValue{value: value.Float64}
	}
	return values, rows.Err()
}

func getChannelMonitorMinutePerformanceMetrics(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelMonitorPerformanceMetric, error) {
	startTimestamp, endTimestamp = channelMonitorMinuteRange(startTimestamp, endTimestamp)
	if startTimestamp >= endTimestamp {
		return []ChannelMonitorPerformanceMetric{}, nil
	}
	type performanceAggregate struct {
		ChannelId             int
		ModelName             string
		SampleCount           int64
		FirstTokenSampleCount int64
		TPSSampleCount        int64
		FirstTokenTotalMs     float64
		TPSTotal              float64
		LastUsedTime          int64
	}
	var aggregates []performanceAggregate
	err := DB.WithContext(ctx).
		Model(&ChannelMonitorMinuteMetric{}).
		Select(
			"channel_id, model_name, "+
				"SUM(sample_count) AS sample_count, "+
				"SUM(first_token_sample_count) AS first_token_sample_count, "+
				"SUM(tps_sample_count) AS tps_sample_count, "+
				"SUM(first_token_total_ms) AS first_token_total_ms, "+
				"SUM(tps_total) AS tps_total, "+
				"MAX(last_used_time) AS last_used_time",
		).
		Where("minute_start >= ? AND minute_start < ?", startTimestamp, endTimestamp).
		Where("sample_count > ?", 0).
		Group("channel_id, model_name").
		Scan(&aggregates).Error
	if err != nil {
		return nil, err
	}
	latestFirstTokens, err := getChannelMonitorMinuteLatestPerformanceValues(
		ctx, startTimestamp, endTimestamp, "latest_first_token_ms", "latest_first_token_at",
	)
	if err != nil {
		return nil, err
	}
	latestTPSValues, err := getChannelMonitorMinuteLatestPerformanceValues(
		ctx, startTimestamp, endTimestamp, "latest_tps", "latest_tps_at",
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
		key := channelMonitorMinutePerformanceKey{channelId: aggregate.ChannelId, modelName: aggregate.ModelName}
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
		CacheWriteCount    int64
	}
	dayBucket := channelMonitorMinuteDayBucketSQL()
	var rows []dailyRow
	err := DB.WithContext(ctx).
		Model(&ChannelMonitorMinuteMetric{}).
		Select(
			dayBucket+" AS day_bucket, channel_id, "+
				"SUM(actual_success_count) AS actual_success_count, "+
				"SUM(actual_failure_count) AS actual_failure_count, "+
				"SUM(final_success_count) AS final_success_count, "+
				"SUM(final_failure_count) AS final_failure_count, "+
				"SUM(cache_hit_count) AS cache_hit_count, "+
				"SUM(cache_sample_count) AS cache_sample_count, "+
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
	rows, err := getChannelMonitorMinuteSuccessRows(
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
	for _, row := range rows {
		isRetryAttempt := row.IsRetryAttempt != nil && *row.IsRetryAttempt
		totalCounts.add(row.Type, isRetryAttempt, row.Count, row.CacheHitCount, row.CacheSampleCount)
		addChannelMonitorSuccessAPIKeyCount(apiKeyCounts, row)
		counts := channelCounts[row.ChannelId]
		if counts == nil {
			counts = &channelMonitorSuccessCounts{}
			channelCounts[row.ChannelId] = counts
		}
		counts.add(row.Type, isRetryAttempt, row.Count, row.CacheHitCount, row.CacheSampleCount)
		cacheWriteCounts[row.ChannelId] += row.CacheWriteCount
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
