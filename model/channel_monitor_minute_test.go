package model

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useChannelMonitorMinuteTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	originalDB := DB
	originalDatabaseType := common.MainDatabaseType()
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(
		&ChannelMonitorMinuteRouteMetric{},
		&ChannelMonitorMinuteAPIKeyMetric{},
		&ChannelMonitorMinuteDurationBucket{},
		&ChannelMonitorAggregationState{},
		&ChannelSmartScheduleModelSampleState{},
	))
	t.Cleanup(func() {
		DB = originalDB
		common.SetMainDatabaseType(originalDatabaseType)
	})
}

func aggregateChannelMonitorMinuteTestRange(t *testing.T, startTimestamp int64, endTimestamp int64) {
	t.Helper()
	_, err := AggregateChannelMonitorMinuteRange(context.Background(), startTimestamp, endTimestamp)
	require.NoError(t, err)
}

func setupChannelMonitorMinuteAggregationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalLogDB := LOG_DB
	originalLogDatabaseType := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-minute.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	useChannelMonitorMinuteTestDB(t, db)
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		LOG_DB = originalLogDB
		common.SetLogDatabaseType(originalLogDatabaseType)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func TestAggregateChannelMonitorMinuteRangeWithResultReportsWork(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	firstTokenMs := 250.0
	consumeOther, err := common.Marshal(channelMonitorMinuteLogOther{FirstResponseTime: &firstTokenMs})
	require.NoError(t, err)
	probeOther, err := common.Marshal(channelMonitorMinuteLogOther{SmartScheduleProbe: true})
	require.NoError(t, err)
	require.NoError(t, db.Create(&[]Log{
		{
			ChannelId: 1, ModelName: "model-a", Group: "vip", TokenId: 1, TokenName: "key-a",
			CreatedAt: 61, Type: LogTypeConsume, IsStream: true, Other: string(consumeOther),
		},
		{
			ChannelId: 1, ModelName: "model-a", Group: "vip", TokenId: 1, TokenName: "key-a",
			CreatedAt: 62, Type: LogTypeError,
		},
		{
			ChannelId: 2, ModelName: "probe", Group: "vip", TokenId: 1, TokenName: "key-a",
			CreatedAt: 63, Type: LogTypeConsume, Other: string(probeOther),
		},
		{
			ChannelId: 3, ModelName: "outside", Group: "vip", TokenId: 1, TokenName: "key-a",
			CreatedAt: 181, Type: LogTypeConsume,
		},
	}).Error)

	result, err := AggregateChannelMonitorMinuteRangeWithResult(context.Background(), 61, 180)
	require.NoError(t, err)
	assert.Equal(t, int64(60), result.StartTimestamp)
	assert.Equal(t, int64(180), result.EndTimestamp)
	assert.Equal(t, 3, result.ScannedLogRows)
	assert.Equal(t, 1, result.MetricRows)
	assert.Equal(t, 1, result.APIKeyMetricRows)
	assert.Equal(t, 1, result.DurationBucketRows)
	assert.Equal(t, 3, result.GeneratedRows())
}

func TestAggregateChannelMonitorMinuteCalculatesCacheUtilizationFromTokens(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.Create(&[]Log{
		{
			ChannelId: 1, ModelName: "model-a", CreatedAt: 61, Type: LogTypeConsume,
			PromptTokens: 1000, Other: `{"cache_tokens":1}`,
		},
		{
			ChannelId: 1, ModelName: "model-a", CreatedAt: 62, Type: LogTypeConsume,
			PromptTokens: 1000, Other: `{"cache_tokens":999}`,
		},
		{
			ChannelId: 1, ModelName: "model-a", CreatedAt: 63, Type: LogTypeConsume,
			PromptTokens: 100, Other: `{"usage_semantic":"anthropic","cache_tokens":400,"cache_creation_tokens":500}`,
		},
		{
			ChannelId: 1, ModelName: "model-a", CreatedAt: 64, Type: LogTypeConsume,
			PromptTokens: 1000,
		},
		{
			ChannelId: 1, ModelName: "model-a", CreatedAt: 65, Type: LogTypeConsume,
			PromptTokens: 9999, Other: `{"input_tokens_total":200,"cache_tokens":300}`,
		},
	}).Error)

	aggregateChannelMonitorMinuteTestRange(t, 60, 120)

	var metric ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where(
		"minute_start = ? AND channel_id = ?", 60, 1,
	).First(&metric).Error)
	assert.Equal(t, int64(1600), metric.CacheReadTokens)
	assert.Equal(t, int64(4200), metric.InputTokens)

	channelMetrics, _, err := GetChannelMonitorSuccessMetrics(context.Background(), 60)
	require.NoError(t, err)
	require.Len(t, channelMetrics, 1)
	assert.Equal(t, int64(1600), channelMetrics[0].CacheReadTokens)
	assert.Equal(t, int64(4200), channelMetrics[0].InputTokens)
	assert.InDelta(t, 1600.0/4200.0, channelMetrics[0].CacheUtilization, 0.0001)
}

func TestUpgradeChannelMonitorCacheUtilizationMetricsRebuildsCurrentDayOnce(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.Create(&ChannelMonitorAggregationState{
		ID:                          channelMonitorAggregationStateID,
		CoveredFrom:                 60,
		CompletedThrough:            180,
		CacheUtilizationVersion:     0,
		CacheUtilizationCoveredFrom: 0,
	}).Error)
	require.NoError(t, db.Create(&Log{
		ChannelId: 1, ModelName: "model-a", CreatedAt: 121, Type: LogTypeConsume,
		PromptTokens: 1000, Other: `{"cache_tokens":250}`,
	}).Error)

	result, upgraded, err := UpgradeChannelMonitorCacheUtilizationMetrics(
		context.Background(), 120, 180,
	)
	require.NoError(t, err)
	require.True(t, upgraded)
	assert.Equal(t, 1, result.MetricRows)

	var state ChannelMonitorAggregationState
	require.NoError(t, db.First(&state, channelMonitorAggregationStateID).Error)
	assert.Equal(t, ChannelMonitorCacheUtilizationVersion, state.CacheUtilizationVersion)
	assert.Equal(t, int64(120), state.CacheUtilizationCoveredFrom)
	var metric ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", 120, 1).First(&metric).Error)
	assert.Equal(t, int64(250), metric.CacheReadTokens)
	assert.Equal(t, int64(1000), metric.InputTokens)

	require.NoError(t, db.Create(&Log{
		ChannelId: 1, ModelName: "model-a", CreatedAt: 122, Type: LogTypeConsume,
		PromptTokens: 1000, Other: `{"cache_tokens":1000}`,
	}).Error)
	result, upgraded, err = UpgradeChannelMonitorCacheUtilizationMetrics(
		context.Background(), 120, 180,
	)
	require.NoError(t, err)
	assert.False(t, upgraded)
	assert.Zero(t, result.ScannedLogRows)
	require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", 120, 1).First(&metric).Error)
	assert.Equal(t, int64(250), metric.CacheReadTokens)
}

func TestBackfillChannelMonitorCacheUtilizationRangeExtendsCacheCoverage(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.Create(&ChannelMonitorAggregationState{
		ID:                          channelMonitorAggregationStateID,
		CoveredFrom:                 60,
		CompletedThrough:            180,
		CacheUtilizationVersion:     ChannelMonitorCacheUtilizationVersion,
		CacheUtilizationCoveredFrom: 120,
	}).Error)
	require.NoError(t, db.Create(&Log{
		ChannelId: 1, ModelName: "model-a", CreatedAt: 61, Type: LogTypeConsume,
		PromptTokens: 2000, Other: `{"cache_tokens":500}`,
	}).Error)

	result, err := BackfillChannelMonitorCacheUtilizationRangeWithState(
		context.Background(), 60, 120,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, result.MetricRows)

	var state ChannelMonitorAggregationState
	require.NoError(t, db.First(&state, channelMonitorAggregationStateID).Error)
	assert.Equal(t, int64(60), state.CoveredFrom)
	assert.Equal(t, int64(60), state.CacheUtilizationCoveredFrom)
	var metric ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", 60, 1).First(&metric).Error)
	assert.Equal(t, int64(500), metric.CacheReadTokens)
	assert.Equal(t, int64(2000), metric.InputTokens)
}

func TestAggregateChannelMonitorMinuteRangeWithStateCommitsRowsAndWatermark(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.Create(&Log{
		ChannelId: 1, ModelName: "model-a", CreatedAt: 61, Type: LogTypeConsume,
	}).Error)

	result, err := AggregateChannelMonitorMinuteRangeWithState(context.Background(), 60, 120, true)
	require.NoError(t, err)
	assert.Equal(t, 1, result.MetricRows)
	var state ChannelMonitorAggregationState
	require.NoError(t, db.First(&state, channelMonitorAggregationStateID).Error)
	assert.Equal(t, int64(120), state.CompletedThrough)
	assert.Equal(t, int64(60), state.CoveredFrom)
	assert.Equal(t, int64(1), state.Revision)

	require.NoError(t, db.Create(&Log{
		ChannelId: 1, ModelName: "model-a", CreatedAt: 62, Type: LogTypeConsume,
	}).Error)
	rescanned, err := AggregateChannelMonitorMinuteRangeWithState(context.Background(), 60, 120, true)
	require.NoError(t, err)
	assert.Equal(t, 2, rescanned.ScannedLogRows)
	assert.Equal(t, 1, rescanned.MetricRows)
	require.NoError(t, db.First(&state, channelMonitorAggregationStateID).Error)
	assert.Equal(t, int64(2), state.Revision)
	var metric ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", 60, 1).First(&metric).Error)
	assert.Equal(t, int64(2), metric.ActualSuccessCount)

	_, err = AggregateChannelMonitorMinuteRangeWithState(context.Background(), 60, 120, false)
	require.NoError(t, err)
	require.NoError(t, db.First(&state, channelMonitorAggregationStateID).Error)
	assert.Equal(t, int64(120), state.CompletedThrough)
	assert.Equal(t, int64(60), state.CoveredFrom)
	assert.Equal(t, int64(3), state.Revision)
	require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", 60, 1).First(&metric).Error)
	assert.Equal(t, int64(2), metric.ActualSuccessCount)
}

func TestBackfillChannelMonitorMinuteRangeExtendsCoverageWithoutMovingCompletion(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.Create(&[]Log{
		{ChannelId: 1, ModelName: "model-a", CreatedAt: 181, Type: LogTypeConsume},
		{ChannelId: 1, ModelName: "model-a", CreatedAt: 61, Type: LogTypeConsume},
	}).Error)

	_, err := AggregateChannelMonitorMinuteRangeWithState(context.Background(), 180, 240, true)
	require.NoError(t, err)
	_, err = BackfillChannelMonitorMinuteRangeWithState(context.Background(), 60, 180)
	require.NoError(t, err)

	coverage, err := GetChannelMonitorAggregationCoverage(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(60), coverage.CoveredFrom)
	assert.Equal(t, int64(240), coverage.CompletedThrough)

	var metrics []ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Order("minute_start ASC").Find(&metrics).Error)
	require.Len(t, metrics, 2)
	assert.Equal(t, []int64{60, 180}, []int64{metrics[0].MinuteStart, metrics[1].MinuteStart})

	require.NoError(t, db.Create(&Log{
		ChannelId: 1, ModelName: "model-a", CreatedAt: 62, Type: LogTypeConsume,
	}).Error)
	rescanned, err := BackfillChannelMonitorMinuteRangeWithState(context.Background(), 60, 180)
	require.NoError(t, err)
	assert.Equal(t, 2, rescanned.ScannedLogRows)
	assert.Equal(t, 1, rescanned.MetricRows)
	var state ChannelMonitorAggregationState
	require.NoError(t, db.First(&state, channelMonitorAggregationStateID).Error)
	assert.Equal(t, int64(3), state.Revision)
	var earliestMetric ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where("minute_start = ?", 60).First(&earliestMetric).Error)
	assert.Equal(t, int64(2), earliestMetric.ActualSuccessCount)
}

func TestAggregateChannelMonitorMinuteRangeSkipsOnlyAfterConcurrentWatermarkAdvance(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.Create(&Log{
		ChannelId: 1, ModelName: "model-a", CreatedAt: 121, Type: LogTypeConsume,
	}).Error)
	require.NoError(t, AdvanceChannelMonitorAggregationCompletedThrough(context.Background(), 120))
	observedCoverage, err := GetChannelMonitorAggregationCoverage(context.Background())
	require.NoError(t, err)
	_, err = AggregateChannelMonitorMinuteRangeWithState(context.Background(), 120, 180, true)
	require.NoError(t, err)

	require.NoError(t, db.Create(&Log{
		ChannelId: 1, ModelName: "model-a", CreatedAt: 122, Type: LogTypeConsume,
	}).Error)
	skipped, err := aggregateChannelMonitorMinuteRangeFromObservation(
		context.Background(), 120, 180, true, true, observedCoverage,
	)
	require.NoError(t, err)
	assert.Zero(t, skipped.ScannedLogRows)
	assert.Zero(t, skipped.MetricRows)

	var metric ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", 120, 1).First(&metric).Error)
	assert.Equal(t, int64(1), metric.ActualSuccessCount)
}

func TestAggregateChannelMonitorMinuteRangeDoesNotSkipWiderConcurrentRepair(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.Create(&[]Log{
		{ChannelId: 1, ModelName: "model-a", CreatedAt: 61, Type: LogTypeConsume},
		{ChannelId: 1, ModelName: "model-a", CreatedAt: 121, Type: LogTypeConsume},
	}).Error)
	require.NoError(t, AdvanceChannelMonitorAggregationCompletedThrough(context.Background(), 120))
	observedCoverage, err := GetChannelMonitorAggregationCoverage(context.Background())
	require.NoError(t, err)
	_, err = AggregateChannelMonitorMinuteRangeWithState(context.Background(), 120, 180, true)
	require.NoError(t, err)

	rebuilt, err := aggregateChannelMonitorMinuteRangeFromObservation(
		context.Background(), 60, 180, true, true, observedCoverage,
	)
	require.NoError(t, err)
	assert.Equal(t, 2, rebuilt.ScannedLogRows)
	assert.Equal(t, 2, rebuilt.MetricRows)

	var metrics []ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Order("minute_start ASC").Find(&metrics).Error)
	require.Len(t, metrics, 2)
	assert.Equal(t, []int64{60, 120}, []int64{metrics[0].MinuteStart, metrics[1].MinuteStart})
}

func TestBackfillChannelMonitorMinuteRangeSkipsOnlyAfterConcurrentCoverageAdvance(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.Create(&Log{
		ChannelId: 1, ModelName: "model-a", CreatedAt: 121, Type: LogTypeConsume,
	}).Error)
	require.NoError(t, AdvanceChannelMonitorAggregationCompletedThrough(context.Background(), 180))
	observedCoverage, err := GetChannelMonitorAggregationCoverage(context.Background())
	require.NoError(t, err)
	_, err = BackfillChannelMonitorMinuteRangeWithState(context.Background(), 120, 180)
	require.NoError(t, err)

	require.NoError(t, db.Create(&Log{
		ChannelId: 1, ModelName: "model-a", CreatedAt: 122, Type: LogTypeConsume,
	}).Error)
	skipped, err := aggregateChannelMonitorMinuteRangeFromObservation(
		context.Background(), 120, 180, false, true, observedCoverage,
	)
	require.NoError(t, err)
	assert.Zero(t, skipped.ScannedLogRows)
	assert.Zero(t, skipped.MetricRows)

	var metric ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", 120, 1).First(&metric).Error)
	assert.Equal(t, int64(1), metric.ActualSuccessCount)
}

func TestAggregateChannelMonitorMinuteBucketsRetryFailureDurations(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	durations := []int64{500, 1_500, 5_000, 15_000, 45_000, 60_000}
	logs := make([]Log, 0, len(durations))
	for index, durationMs := range durations {
		other, err := common.Marshal(channelMonitorMinuteLogOther{AttemptDurationMs: &durationMs})
		require.NoError(t, err)
		logs = append(logs, Log{
			ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 121 + int64(index),
			Type: LogTypeError, IsRetryAttempt: true,
			RequestId: fmt.Sprintf("retry-%d", index), Other: string(other),
		})
	}
	require.NoError(t, db.Create(&logs).Error)
	aggregateChannelMonitorMinuteTestRange(t, 120, 180)

	var metric ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where(
		"minute_start = ? AND channel_id = ? AND model_name = ? AND group_name = ?",
		120, 1, "model-a", "vip",
	).First(&metric).Error)
	assert.Equal(t, int64(6), metric.ActualFailureCount)
	assert.Zero(t, metric.FinalFailureCount)
	assert.Equal(t, int64(6), metric.RetryFailureCount)
	assert.Equal(t, int64(127_000), metric.RetryFailureDurationTotalMs)
	assert.Equal(t, int64(1), metric.RetryFailureUnder1sCount)
	assert.Equal(t, int64(1), metric.RetryFailure1To3sCount)
	assert.Equal(t, int64(1), metric.RetryFailure3To10sCount)
	assert.Equal(t, int64(1), metric.RetryFailure10To30sCount)
	assert.Equal(t, int64(1), metric.RetryFailure30To60sCount)
	assert.Equal(t, int64(1), metric.RetryFailureOver60sCount)
}

func TestAggregateChannelMonitorMinuteUsesFormattedRoutingModelName(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.Create(&Log{
		ChannelId: 1,
		Group:     "vip",
		ModelName: "gemini-2.5-pro-thinking-2048",
		CreatedAt: 121,
		Type:      LogTypeConsume,
	}).Error)

	aggregateChannelMonitorMinuteTestRange(t, 120, 180)
	var metric ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where(
		"minute_start = ? AND channel_id = ?", 120, 1,
	).First(&metric).Error)
	assert.Equal(t, "gemini-2.5-pro-thinking-*", metric.ModelName)
	assert.Equal(t, int64(1), metric.ActualSuccessCount)
}

func TestAggregateChannelMonitorMinuteKeeps429ErrorsButExcludesThemFromStability(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	rateLimitDuration := int64(5_000)
	retryDuration := int64(500)
	rateLimitRetryOther, err := common.Marshal(channelMonitorMinuteLogOther{
		AttemptDurationMs: &rateLimitDuration,
		StatusCode:        429,
	})
	require.NoError(t, err)
	rateLimitFinalOther, err := common.Marshal(channelMonitorMinuteLogOther{
		AttemptDurationMs: &rateLimitDuration,
		FinalRetrySummary: true,
		StatusCode:        "429",
	})
	require.NoError(t, err)
	retryOther, err := common.Marshal(channelMonitorMinuteLogOther{
		AttemptDurationMs: &retryDuration,
		StatusCode:        503,
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&[]Log{
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 121,
			Type: LogTypeConsume,
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 122,
			Type: LogTypeError, IsRetryAttempt: true, RequestId: "rate-limit",
			Other: string(rateLimitRetryOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 123,
			Type: LogTypeError, RequestId: "rate-limit", Other: string(rateLimitFinalOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 124,
			Type: LogTypeError, IsRetryAttempt: true, RequestId: "upstream-error",
			Other: string(retryOther),
		},
	}).Error)

	aggregateChannelMonitorMinuteTestRange(t, 120, 180)

	var metric ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where(
		"minute_start = ? AND channel_id = ? AND model_name = ? AND group_name = ?",
		120, 1, "model-a", "vip",
	).First(&metric).Error)
	assert.Equal(t, int64(2), metric.ActualFailureCount)
	assert.Equal(t, int64(1), metric.FinalFailureCount)
	assert.Equal(t, int64(1), metric.RateLimitActualFailureCount)
	assert.Equal(t, int64(1), metric.RateLimitFinalFailureCount)
	assert.Equal(t, int64(1), metric.RetryFailureCount)
	assert.Equal(t, retryDuration, metric.RetryFailureDurationTotalMs)
	assert.Equal(t, int64(1), metric.RetryFailureUnder1sCount)
	assert.Zero(t, metric.RetryFailure3To10sCount)

	stabilityMetrics, err := GetChannelMonitorRouteStabilityMetrics(context.Background(), 120, 180)
	require.NoError(t, err)
	require.Len(t, stabilityMetrics, 1)
	assert.Equal(t, int64(1), stabilityMetrics[0].SuccessCount)
	assert.Equal(t, int64(1), stabilityMetrics[0].FailureCount)
	assert.Zero(t, stabilityMetrics[0].FinalFailureCount)
	assert.Equal(t, int64(1), stabilityMetrics[0].RetryFailureCount)
	assert.Equal(t, int64(2), stabilityMetrics[0].SampleCount)
	assert.InDelta(t, 0.5, stabilityMetrics[0].SuccessRate, 0.0001)
}

func TestAggregateChannelMonitorMinuteSaturatesRetryFailureDuration(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	durationMs := int64(math.MaxInt64)
	other, err := common.Marshal(channelMonitorMinuteLogOther{AttemptDurationMs: &durationMs})
	require.NoError(t, err)
	require.NoError(t, db.Create(&[]Log{
		{ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 121, Type: LogTypeError, IsRetryAttempt: true, Other: string(other)},
		{ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 122, Type: LogTypeError, IsRetryAttempt: true, Other: string(other)},
	}).Error)

	aggregateChannelMonitorMinuteTestRange(t, 120, 180)

	var metric ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where(
		"minute_start = ? AND channel_id = ? AND model_name = ? AND group_name = ?",
		120, 1, "model-a", "vip",
	).First(&metric).Error)
	assert.Equal(t, int64(2), metric.RetryFailureCount)
	assert.Equal(t, int64(math.MaxInt64), metric.RetryFailureDurationTotalMs)
}

func TestAggregateChannelMonitorMinuteIgnoresMonitoringProbeConsumeLogs(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	probeOther, err := common.Marshal(channelMonitorMinuteLogOther{SmartScheduleProbe: true})
	require.NoError(t, err)
	channelTestOther, err := common.Marshal(channelMonitorMinuteLogOther{ChannelTest: true})
	require.NoError(t, err)
	statusProbeOther, err := common.Marshal(channelMonitorMinuteLogOther{StatusProbe: true})
	require.NoError(t, err)
	groupProbeOther, err := common.Marshal(channelMonitorMinuteLogOther{GroupProbe: true})
	require.NoError(t, err)
	require.NoError(t, db.Create(&[]Log{
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenName: "智能调度探测",
			CreatedAt: 121, Type: LogTypeConsume, IsStream: true, CompletionTokens: 20,
			UseTime: 2, Other: string(probeOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenName: "模型测试",
			CreatedAt: 122, Type: LogTypeConsume, IsStream: true, CompletionTokens: 20,
			UseTime: 2, Other: string(channelTestOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenName: "状态监测",
			CreatedAt: 123, Type: LogTypeConsume, IsStream: true, CompletionTokens: 20,
			UseTime: 2, Other: string(statusProbeOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenName: "分组监控探测",
			CreatedAt: 124, Type: LogTypeConsume, IsStream: true, CompletionTokens: 20,
			UseTime: 2, Other: string(groupProbeOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenName: "业务令牌",
			CreatedAt: 125, Type: LogTypeConsume,
		},
	}).Error)

	aggregateChannelMonitorMinuteTestRange(t, 120, 180)

	var metrics []ChannelMonitorMinuteAPIKeyMetric
	require.NoError(t, db.Order("api_key_name ASC").Find(&metrics).Error)
	require.Len(t, metrics, 1)
	assert.Equal(t, "业务令牌", metrics[0].APIKeyName)
	assert.Equal(t, int64(1), metrics[0].ActualSuccessCount)
	assert.Equal(t, int64(1), metrics[0].FinalSuccessCount)
	assert.Zero(t, metrics[0].CacheSampleCount)
}

func TestAggregateChannelMonitorMinuteCountsFinalRetryFailureOnce(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	requestId := "request-final-failure"
	firstDuration := int64(500)
	lastDuration := int64(15_000)
	firstOther, err := common.Marshal(channelMonitorMinuteLogOther{AttemptDurationMs: &firstDuration})
	require.NoError(t, err)
	lastOther, err := common.Marshal(channelMonitorMinuteLogOther{AttemptDurationMs: &lastDuration})
	require.NoError(t, err)
	finalOther, err := common.Marshal(channelMonitorMinuteLogOther{
		AttemptDurationMs: &lastDuration,
		FinalRetrySummary: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&[]Log{
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 121,
			Type: LogTypeError, IsRetryAttempt: true, RequestId: requestId, Other: string(firstOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 122,
			Type: LogTypeError, IsRetryAttempt: true, RequestId: requestId, Other: string(lastOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 123,
			Type: LogTypeError, RequestId: requestId, Other: string(finalOther),
		},
	}).Error)
	aggregateChannelMonitorMinuteTestRange(t, 120, 180)

	var metric ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where(
		"minute_start = ? AND channel_id = ? AND model_name = ? AND group_name = ?",
		120, 1, "model-a", "vip",
	).First(&metric).Error)
	assert.Equal(t, int64(2), metric.ActualFailureCount)
	assert.Equal(t, int64(1), metric.FinalFailureCount)
	assert.Equal(t, int64(1), metric.RetryFailureCount)
	assert.Equal(t, firstDuration, metric.RetryFailureDurationTotalMs)
	assert.Equal(t, int64(1), metric.RetryFailureUnder1sCount)
	assert.Zero(t, metric.RetryFailure10To30sCount)
}

func TestAggregateChannelMonitorMinuteMatchesFinalRetryRegardlessOfLogOrder(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	requestId := "request-out-of-order-final-failure"
	durationMs := int64(5_000)
	retryOther, err := common.Marshal(channelMonitorMinuteLogOther{AttemptDurationMs: &durationMs})
	require.NoError(t, err)
	finalOther, err := common.Marshal(channelMonitorMinuteLogOther{
		AttemptDurationMs: &durationMs,
		FinalRetrySummary: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&[]Log{
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 121,
			Type: LogTypeError, RequestId: requestId, Other: string(finalOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 122,
			Type: LogTypeError, IsRetryAttempt: true, RequestId: requestId, Other: string(retryOther),
		},
	}).Error)
	aggregateChannelMonitorMinuteTestRange(t, 120, 180)

	var metric ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where(
		"minute_start = ? AND channel_id = ? AND model_name = ? AND group_name = ?",
		120, 1, "model-a", "vip",
	).First(&metric).Error)
	assert.Equal(t, int64(1), metric.ActualFailureCount)
	assert.Equal(t, int64(1), metric.FinalFailureCount)
	assert.Zero(t, metric.RetryFailureCount)
	assert.Zero(t, metric.RetryFailureDurationTotalMs)
	assert.Zero(t, metric.RetryFailure3To10sCount)
}

func TestAggregateChannelMonitorMinuteDeduplicatesFinalRetryAcrossMinutes(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	requestId := "request-cross-minute-final-failure"
	durationMs := int64(15_000)
	retryOther, err := common.Marshal(channelMonitorMinuteLogOther{AttemptDurationMs: &durationMs})
	require.NoError(t, err)
	finalOther, err := common.Marshal(channelMonitorMinuteLogOther{
		AttemptDurationMs: &durationMs,
		FinalRetrySummary: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&[]Log{
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 179,
			Type: LogTypeError, IsRetryAttempt: true, RequestId: requestId, Other: string(retryOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", CreatedAt: 180,
			Type: LogTypeError, RequestId: requestId, Other: string(finalOther),
		},
	}).Error)
	aggregateChannelMonitorMinuteTestRange(t, 120, 240)

	var metrics []ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ? AND group_name = ?", 1, "model-a", "vip",
	).Order("minute_start ASC").Find(&metrics).Error)
	require.Len(t, metrics, 2)
	assert.Equal(t, int64(120), metrics[0].MinuteStart)
	assert.Equal(t, int64(1), metrics[0].ActualFailureCount)
	assert.Zero(t, metrics[0].FinalFailureCount)
	assert.Zero(t, metrics[0].RetryFailureCount)
	assert.Zero(t, metrics[0].RetryFailureDurationTotalMs)
	assert.Equal(t, int64(180), metrics[1].MinuteStart)
	assert.Zero(t, metrics[1].ActualFailureCount)
	assert.Equal(t, int64(1), metrics[1].FinalFailureCount)
	assert.Zero(t, metrics[1].RetryFailureCount)

	stabilityMetrics, err := GetChannelMonitorRouteStabilityMetrics(context.Background(), 120, 240)
	require.NoError(t, err)
	require.Len(t, stabilityMetrics, 1)
	assert.Equal(t, int64(1), stabilityMetrics[0].FailureCount)
	assert.Equal(t, int64(1), stabilityMetrics[0].FinalFailureCount)
	assert.Zero(t, stabilityMetrics[0].RetryFailureCount)

	boundaryMetrics, err := GetChannelMonitorRouteStabilityMetrics(context.Background(), 180, 240)
	require.NoError(t, err)
	require.Len(t, boundaryMetrics, 1)
	assert.Equal(t, int64(1), boundaryMetrics[0].FailureCount)
	assert.Equal(t, int64(1), boundaryMetrics[0].FinalFailureCount)
	assert.Equal(t, int64(1), boundaryMetrics[0].SampleCount)
}

func TestAggregateChannelMonitorMinutePointRepairMatchesFullRangeForCrossMinuteRetries(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	requestId := "request-cross-minute-point-repair"
	durationMs := int64(1_500)
	retryOther, err := common.Marshal(channelMonitorMinuteLogOther{AttemptDurationMs: &durationMs})
	require.NoError(t, err)
	finalOther, err := common.Marshal(channelMonitorMinuteLogOther{
		AttemptDurationMs: &durationMs,
		FinalRetrySummary: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&[]Log{
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenId: 11, TokenName: "key-a",
			CreatedAt: 61, Type: LogTypeError, IsRetryAttempt: true, RequestId: requestId, Other: string(retryOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenId: 11, TokenName: "key-a",
			CreatedAt: 121, Type: LogTypeError, IsRetryAttempt: true, RequestId: requestId, Other: string(retryOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenId: 11, TokenName: "key-a",
			CreatedAt: 122, Type: LogTypeError, IsRetryAttempt: true, RequestId: requestId, Other: string(retryOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenId: 11, TokenName: "key-a",
			CreatedAt: 123, Type: LogTypeError, RequestId: requestId, Other: string(finalOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenId: 12, TokenName: "key-b",
			CreatedAt: 124, Type: LogTypeError, IsRetryAttempt: true, RequestId: requestId, Other: string(retryOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenId: 11, TokenName: "key-a",
			CreatedAt: 301, Type: LogTypeError, IsRetryAttempt: true, RequestId: requestId, Other: string(retryOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenId: 11, TokenName: "key-a",
			CreatedAt: 302, Type: LogTypeError, RequestId: requestId, Other: string(finalOther),
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenId: 12, TokenName: "key-b",
			CreatedAt: 303, Type: LogTypeError, RequestId: requestId, Other: string(finalOther),
		},
	}).Error)

	fullResult, err := AggregateChannelMonitorMinuteRangeWithResult(context.Background(), 60, 360)
	require.NoError(t, err)
	assert.Equal(t, 8, fullResult.ScannedLogRows)
	var fullRoute ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", 120, 1).First(&fullRoute).Error)
	fullRoute.Id = 0
	var fullAPIKeys []ChannelMonitorMinuteAPIKeyMetric
	require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", 120, 1).
		Order("api_key_id ASC").Find(&fullAPIKeys).Error)
	require.Len(t, fullAPIKeys, 2)
	for index := range fullAPIKeys {
		fullAPIKeys[index].Id = 0
	}
	assert.Equal(t, int64(1), fullRoute.RetryFailureCount)
	assert.Equal(t, durationMs, fullRoute.RetryFailureDurationTotalMs)
	assert.Equal(t, int64(1), fullRoute.RetryFailure1To3sCount)
	assert.Equal(t, int64(1), fullAPIKeys[0].RetryFailureCount)
	assert.Equal(t, durationMs, fullAPIKeys[0].RetryFailureDurationTotalMs)
	assert.Equal(t, int64(1), fullAPIKeys[0].RetryFailure1To3sCount)
	assert.Zero(t, fullAPIKeys[1].RetryFailureCount)
	assert.Zero(t, fullAPIKeys[1].RetryFailureDurationTotalMs)
	assert.Zero(t, fullAPIKeys[1].RetryFailure1To3sCount)

	for repair := 0; repair < 2; repair++ {
		repairResult, err := AggregateChannelMonitorMinuteRangeWithResult(context.Background(), 120, 180)
		require.NoError(t, err)
		assert.Equal(t, 8, repairResult.ScannedLogRows)
		var repairedRoute ChannelMonitorMinuteRouteMetric
		require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", 120, 1).First(&repairedRoute).Error)
		repairedRoute.Id = 0
		assert.Equal(t, fullRoute, repairedRoute)

		var repairedAPIKeys []ChannelMonitorMinuteAPIKeyMetric
		require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", 120, 1).
			Order("api_key_id ASC").Find(&repairedAPIKeys).Error)
		require.Len(t, repairedAPIKeys, 2)
		for index := range repairedAPIKeys {
			repairedAPIKeys[index].Id = 0
		}
		assert.Equal(t, fullAPIKeys, repairedAPIKeys)
	}
}

func TestAggregateChannelMonitorMinuteSplitsRouteAndAPIKeyMetrics(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.Create(&[]Log{
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenId: 11, TokenName: "key-a",
			CreatedAt: 121, Type: LogTypeConsume, IsStream: true, PromptTokens: 1000, CompletionTokens: 20, UseTime: 2,
			Other: `{"frt":100,"cache_tokens":20}`,
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenId: 12, TokenName: "key-b",
			CreatedAt: 122, Type: LogTypeConsume, IsStream: true, PromptTokens: 1000, CompletionTokens: 40, UseTime: 4,
			Other: `{"frt":200,"cache_tokens":40}`,
		},
		{
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenId: 12, TokenName: "key-b",
			CreatedAt: 123, Type: LogTypeError, IsRetryAttempt: true,
			Other: `{"channel_monitor_attempt_duration_ms":1500}`,
		},
	}).Error)
	aggregateChannelMonitorMinuteTestRange(t, 120, 180)

	var routeRows []ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where("minute_start = ?", 120).Find(&routeRows).Error)
	require.Len(t, routeRows, 1)
	assert.Equal(t, int64(2), routeRows[0].ActualSuccessCount)
	assert.Equal(t, int64(1), routeRows[0].ActualFailureCount)
	assert.Equal(t, int64(2), routeRows[0].SampleCount)
	assert.Equal(t, int64(2), routeRows[0].FirstTokenSampleCount)
	assert.Equal(t, int64(1), routeRows[0].RetryFailureCount)

	var apiKeyRows []ChannelMonitorMinuteAPIKeyMetric
	require.NoError(t, db.Where("minute_start = ?", 120).Order("api_key_id ASC").Find(&apiKeyRows).Error)
	require.Len(t, apiKeyRows, 2)
	assert.Equal(t, int64(1), apiKeyRows[0].ActualSuccessCount)
	assert.Equal(t, int64(1), apiKeyRows[1].ActualSuccessCount)
	assert.Equal(t, int64(1), apiKeyRows[1].ActualFailureCount)
	assert.Equal(t, int64(20), apiKeyRows[0].CacheReadTokens)
	assert.Equal(t, int64(40), apiKeyRows[1].CacheReadTokens)

	performance, err := GetChannelMonitorPerformanceMetrics(context.Background(), 120)
	require.NoError(t, err)
	require.Len(t, performance, 1)
	assert.Equal(t, 2, performance[0].SampleCount)
	assert.Equal(t, 2, performance[0].FirstTokenSampleCount)
	assert.Equal(t, 2, performance[0].TPSSampleCount)
}
