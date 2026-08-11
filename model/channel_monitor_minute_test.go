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

type legacyChannelMonitorMinuteMetric struct {
	Id          int64  `gorm:"primaryKey"`
	MinuteStart int64  `gorm:"not null;uniqueIndex:idx_channel_monitor_minute_dimensions"`
	ChannelId   int    `gorm:"not null;uniqueIndex:idx_channel_monitor_minute_dimensions"`
	ModelKey    string `gorm:"size:32;not null;uniqueIndex:idx_channel_monitor_minute_dimensions"`
	GroupKey    string `gorm:"size:32;not null;uniqueIndex:idx_channel_monitor_minute_dimensions"`
	APIKeyKey   string `gorm:"size:32;not null;uniqueIndex:idx_channel_monitor_minute_dimensions"`
	ModelName   string `gorm:"size:255;not null"`
	GroupName   string `gorm:"size:255;not null"`
	APIKeyId    int    `gorm:"not null"`
	APIKeyName  string `gorm:"size:255;not null"`

	ActualSuccessCount int64 `gorm:"not null"`
	ActualFailureCount int64 `gorm:"not null"`
	FinalSuccessCount  int64 `gorm:"not null"`
	FinalFailureCount  int64 `gorm:"not null"`
	CacheHitCount      int64 `gorm:"not null"`
	CacheSampleCount   int64 `gorm:"not null"`
	CacheWriteCount    int64 `gorm:"not null"`

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

func (legacyChannelMonitorMinuteMetric) TableName() string {
	return "channel_monitor_minute_metrics"
}

func useChannelMonitorMinuteTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	originalDB := DB
	originalDatabaseType := common.MainDatabaseType()
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(
		&ChannelMonitorMinuteMetric{},
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
	assert.Equal(t, 1, result.DurationBucketRows)
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
	var metric ChannelMonitorMinuteMetric
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

	var metrics []ChannelMonitorMinuteMetric
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
	var earliestMetric ChannelMonitorMinuteMetric
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

	var metric ChannelMonitorMinuteMetric
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

	var metrics []ChannelMonitorMinuteMetric
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

	var metric ChannelMonitorMinuteMetric
	require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", 120, 1).First(&metric).Error)
	assert.Equal(t, int64(1), metric.ActualSuccessCount)
}

func TestChannelMonitorMinuteMetricMigrationBackfillsRetryColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-minute-migration.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&legacyChannelMonitorMinuteMetric{}))
	require.NoError(t, db.Create(&legacyChannelMonitorMinuteMetric{
		MinuteStart: 120, ChannelId: 1, ModelKey: "model", GroupKey: "group", APIKeyKey: "key",
		ModelName: "model-a", GroupName: "vip", ActualSuccessCount: 3,
	}).Error)

	require.NoError(t, db.AutoMigrate(&ChannelMonitorMinuteMetric{}))
	var metric ChannelMonitorMinuteMetric
	require.NoError(t, db.First(&metric).Error)
	assert.Equal(t, int64(3), metric.ActualSuccessCount)
	assert.Zero(t, metric.RetryFailureCount)
	assert.Zero(t, metric.RetryFailureDurationTotalMs)
	assert.Zero(t, metric.RetryFailureOver60sCount)
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

	var metric ChannelMonitorMinuteMetric
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
	var metric ChannelMonitorMinuteMetric
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

	var metric ChannelMonitorMinuteMetric
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

	var metric ChannelMonitorMinuteMetric
	require.NoError(t, db.Where(
		"minute_start = ? AND channel_id = ? AND model_name = ? AND group_name = ?",
		120, 1, "model-a", "vip",
	).First(&metric).Error)
	assert.Equal(t, int64(2), metric.RetryFailureCount)
	assert.Equal(t, int64(math.MaxInt64), metric.RetryFailureDurationTotalMs)
}

func TestAggregateChannelMonitorMinuteIgnoresMonitoringAndChannelTestConsumeLogs(t *testing.T) {
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	probeOther, err := common.Marshal(channelMonitorMinuteLogOther{SmartScheduleProbe: true})
	require.NoError(t, err)
	channelTestOther, err := common.Marshal(channelMonitorMinuteLogOther{ChannelTest: true})
	require.NoError(t, err)
	statusProbeOther, err := common.Marshal(channelMonitorMinuteLogOther{StatusProbe: true})
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
			ChannelId: 1, Group: "vip", ModelName: "model-a", TokenName: "业务令牌",
			CreatedAt: 124, Type: LogTypeConsume,
		},
	}).Error)

	aggregateChannelMonitorMinuteTestRange(t, 120, 180)

	var metrics []ChannelMonitorMinuteMetric
	require.NoError(t, db.Order("api_key_name ASC").Find(&metrics).Error)
	require.Len(t, metrics, 1)
	assert.Equal(t, "业务令牌", metrics[0].APIKeyName)
	assert.Equal(t, int64(1), metrics[0].ActualSuccessCount)
	assert.Equal(t, int64(1), metrics[0].FinalSuccessCount)
	assert.Zero(t, metrics[0].SampleCount)
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

	var metric ChannelMonitorMinuteMetric
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

	var metric ChannelMonitorMinuteMetric
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

	var metrics []ChannelMonitorMinuteMetric
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
