package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRunChannelMonitorAggregationAggregatesEachNormalMinuteOnceAndRepairsDirtyMinute(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-aggregation.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.LogConsumeEnabled = true
	constant.ErrorLogEnabled = true
	require.NoError(t, db.AutoMigrate(
		&model.Log{},
		&model.ChannelMonitorMinuteRouteMetric{},
		&model.ChannelMonitorMinuteAPIKeyMetric{},
		&model.ChannelMonitorAggregationState{},
		&model.ChannelMonitorDirtyMinute{},
		&model.ChannelSmartScheduleRouteState{},
	))
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
		require.NoError(t, sqlDB.Close())
	})

	now := int64(10*time.Hour/time.Second + 17*time.Minute/time.Second + 23)
	targetEnd := now - now%int64(channelMonitorAggregationInterval/time.Second)
	completedMinute := now - now%60 - 60
	oldMinute := completedMinute - 60
	require.NoError(t, db.Create(&[]model.Log{
		{ChannelId: 1, ModelName: "recent", CreatedAt: completedMinute + 1, Type: model.LogTypeConsume},
		{ChannelId: 2, ModelName: "old", CreatedAt: oldMinute + 1, Type: model.LogTypeConsume},
	}).Error)

	require.NoError(t, runChannelMonitorAggregationAt(context.Background(), now, false))
	var metrics []model.ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Order("channel_id ASC").Find(&metrics).Error)
	require.Len(t, metrics, 1)
	assert.Equal(t, 1, metrics[0].ChannelId)
	assert.Equal(t, int64(1), metrics[0].ActualSuccessCount)
	coverage, err := model.GetChannelMonitorAggregationCoverage(context.Background())
	require.NoError(t, err)
	firstRevision := coverage.Revision

	require.NoError(t, runChannelMonitorAggregationAt(context.Background(), now, false))
	coverage, err = model.GetChannelMonitorAggregationCoverage(context.Background())
	require.NoError(t, err)
	assert.Equal(t, firstRevision, coverage.Revision)

	require.NoError(t, db.Create(&model.Log{
		ChannelId: 1,
		ModelName: "recent",
		CreatedAt: completedMinute + 2,
		Type:      model.LogTypeError,
	}).Error)
	require.NoError(t, model.MarkChannelMonitorDirtyMinute(
		context.Background(), completedMinute, model.ChannelMonitorDirtyReasonLateLog,
	))
	require.NoError(t, runChannelMonitorAggregationAt(context.Background(), now, false))
	require.NoError(t, db.Order("channel_id ASC").Find(&metrics).Error)
	require.Len(t, metrics, 1)
	assert.Equal(t, int64(1), metrics[0].ActualSuccessCount)
	assert.Equal(t, int64(1), metrics[0].ActualFailureCount)
	var dirtyCount int64
	require.NoError(t, db.Model(&model.ChannelMonitorDirtyMinute{}).Count(&dirtyCount).Error)
	assert.Zero(t, dirtyCount)
	require.NoError(t, model.MarkChannelMonitorDirtyMinute(
		context.Background(), completedMinute, model.ChannelMonitorDirtyReasonLateLog,
	))
	require.NoError(t, runChannelMonitorAggregationAt(context.Background(), now, false))
	require.NoError(t, db.Order("channel_id ASC").Find(&metrics).Error)
	require.Len(t, metrics, 1)
	assert.Equal(t, int64(1), metrics[0].ActualSuccessCount)
	assert.Equal(t, int64(1), metrics[0].ActualFailureCount)

	_, err = model.AggregateChannelMonitorMinuteRangeWithResult(
		context.Background(), completedMinute, targetEnd,
	)
	require.NoError(t, err)
	require.NoError(t, db.Order("channel_id ASC").Find(&metrics).Error)
	require.Len(t, metrics, 1)
	assert.Equal(t, int64(1), metrics[0].ActualSuccessCount)
	assert.Equal(t, int64(1), metrics[0].ActualFailureCount)

	laterNow := now + int64(4*time.Minute/time.Second)
	gapMinute := targetEnd + int64(time.Minute/time.Second)
	require.NoError(t, db.Create(&model.Log{
		ChannelId: 3, ModelName: "gap", CreatedAt: gapMinute + 1, Type: model.LogTypeConsume,
	}).Error)
	require.NoError(t, runChannelMonitorAggregationAt(context.Background(), laterNow, false))
	var gapMetric model.ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where("channel_id = ?", 3).First(&gapMetric).Error)
	assert.Equal(t, gapMinute, gapMetric.MinuteStart)
	completedThrough, err := model.GetChannelMonitorAggregationCompletedThrough(context.Background())
	require.NoError(t, err)
	assert.Equal(t, laterNow-laterNow%60, completedThrough)
}

func TestRepairChannelMonitorDirtyMinutesKeepsMarkerAfterFailure(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-dirty-repair.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(
		&model.Log{},
		&model.ChannelMonitorAggregationState{},
		&model.ChannelMonitorDirtyMinute{},
	))
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		require.NoError(t, sqlDB.Close())
	})

	const minuteStart = int64(120)
	require.NoError(t, db.Create(&model.Log{
		ChannelId: 7, ModelName: "model-a", CreatedAt: minuteStart + 1, Type: model.LogTypeConsume,
	}).Error)
	require.NoError(t, model.MarkChannelMonitorDirtyMinute(
		context.Background(), minuteStart, model.ChannelMonitorDirtyReasonLateLog,
	))
	key := channelMonitorAggregationDatabaseKey{db: db, logDB: db}
	err = repairChannelMonitorDirtyMinutes(context.Background(), key, minuteStart+120)
	require.Error(t, err)

	var dirty model.ChannelMonitorDirtyMinute
	require.NoError(t, db.First(&dirty).Error)
	assert.Empty(t, dirty.ClaimedBy)
	assert.Zero(t, dirty.ClaimedUntil)

	require.NoError(t, db.AutoMigrate(
		&model.ChannelMonitorMinuteRouteMetric{},
		&model.ChannelMonitorMinuteAPIKeyMetric{},
	))
	require.NoError(t, repairChannelMonitorDirtyMinutes(context.Background(), key, minuteStart+120))
	var dirtyCount int64
	require.NoError(t, db.Model(&model.ChannelMonitorDirtyMinute{}).Count(&dirtyCount).Error)
	assert.Zero(t, dirtyCount)
	var metric model.ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", minuteStart, 7).First(&metric).Error)
	assert.Equal(t, int64(1), metric.ActualSuccessCount)
}

func TestRunChannelMonitorAggregationUpgradesLegacyCacheUtilizationForCurrentDay(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	originalIsMasterNode := common.IsMasterNode

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-cache-upgrade.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.LogConsumeEnabled = true
	constant.ErrorLogEnabled = true
	common.IsMasterNode = true
	require.NoError(t, db.AutoMigrate(
		&model.Log{},
		&model.ChannelMonitorMinuteRouteMetric{},
		&model.ChannelMonitorMinuteAPIKeyMetric{},
		&model.ChannelMonitorAggregationState{},
		&model.ChannelMonitorDirtyMinute{},
		&model.ChannelSmartScheduleRouteState{},
	))
	t.Cleanup(func() {
		key := channelMonitorAggregationDatabaseKey{db: db, logDB: db}
		channelMonitorAggregationStateMu.Lock()
		delete(channelMonitorAggregationLocalCompletedThrough, key)
		channelMonitorAggregationStateMu.Unlock()
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
		common.IsMasterNode = originalIsMasterNode
		require.NoError(t, sqlDB.Close())
	})

	beijing := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, beijing).Unix()
	dayStart := model.ChannelDailyCostDayStart(now)
	oldMinute := now - int64(2*time.Hour/time.Second)
	require.NoError(t, model.AdvanceChannelMonitorAggregationCompletedThrough(
		context.Background(), now-60,
	))
	require.NoError(t, db.Model(&model.ChannelMonitorAggregationState{}).
		Where("completed_through = ?", now-60).
		Updates(map[string]any{
			"covered_from":                   dayStart,
			"cache_utilization_version":      0,
			"cache_utilization_covered_from": 0,
		}).Error)
	require.NoError(t, db.Create(&model.Log{
		ChannelId: 7, ModelName: "model-a", CreatedAt: oldMinute + 1,
		Type: model.LogTypeConsume, PromptTokens: 1000, Other: `{"cache_tokens":250}`,
	}).Error)

	require.NoError(t, runChannelMonitorAggregationAt(context.Background(), now+20, false))

	var metric model.ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where(
		"minute_start = ? AND channel_id = ?", oldMinute, 7,
	).First(&metric).Error)
	assert.Equal(t, int64(250), metric.CacheReadTokens)
	assert.Equal(t, int64(1000), metric.InputTokens)
	coverage, err := model.GetChannelMonitorAggregationCoverage(context.Background())
	require.NoError(t, err)
	assert.Equal(t, model.ChannelMonitorCacheUtilizationVersion, coverage.CacheUtilizationVersion)
	assert.Equal(t, dayStart, coverage.CacheUtilizationCoveredFrom)
}

func TestChannelMonitorAggregationBackfillCoversConfiguredScheduleWindow(t *testing.T) {
	t.Setenv("CHANNEL_MONITOR_AGGREGATION_BACKFILL_MAX_CHUNKS", "1")
	t.Setenv("CHANNEL_MONITOR_AGGREGATION_BACKFILL_BUDGET_SECONDS", "10")
	t.Setenv("CHANNEL_MONITOR_AGGREGATION_BACKFILL_YIELD_MS", "0")
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-backfill.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.LogConsumeEnabled = true
	constant.ErrorLogEnabled = true
	require.NoError(t, db.AutoMigrate(
		&model.Log{},
		&model.ChannelMonitorMinuteRouteMetric{},
		&model.ChannelMonitorMinuteAPIKeyMetric{},
		&model.ChannelMonitorAggregationState{},
		&model.ChannelMonitorDirtyMinute{},
		&model.ChannelSmartScheduleRouteState{},
	))
	common.OptionMapRWMutex.Lock()
	originalOptionMap := common.OptionMap
	common.OptionMap = map[string]string{
		model.ChannelMonitorSmartSchedulePerformanceWindowOption: "180",
		model.ChannelMonitorSmartScheduleGroupPoliciesOption:     `[{"stability_window_minutes":60}]`,
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		key := channelMonitorAggregationDatabaseKey{db: db, logDB: db}
		channelMonitorAggregationStateMu.Lock()
		delete(channelMonitorAggregationLocalCompletedThrough, key)
		channelMonitorAggregationStateMu.Unlock()
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
		require.NoError(t, sqlDB.Close())
	})

	targetEnd := int64(6 * time.Hour / time.Second)
	oldMinute := targetEnd - int64(170*time.Minute/time.Second)
	require.NoError(t, db.Create(&model.Log{
		ChannelId: 9, ModelName: "model-a", CreatedAt: oldMinute + 1, Type: model.LogTypeConsume,
	}).Error)

	require.NoError(t, runChannelMonitorAggregationAt(context.Background(), targetEnd, true))
	var before int64
	require.NoError(t, db.Model(&model.ChannelMonitorMinuteRouteMetric{}).Count(&before).Error)
	assert.Zero(t, before)

	expectedCoverageStarts := []int64{
		targetEnd - int64(65*time.Minute/time.Second),
		targetEnd - int64(125*time.Minute/time.Second),
		targetEnd - int64(180*time.Minute/time.Second),
	}
	for round, expectedCoveredFrom := range expectedCoverageStarts {
		require.NoError(t, runChannelMonitorAggregationBackfill(context.Background(), targetEnd))
		coverage, err := model.GetChannelMonitorAggregationCoverage(context.Background())
		require.NoError(t, err)
		assert.Equal(t, expectedCoveredFrom, coverage.CoveredFrom)
		if round < len(expectedCoverageStarts)-1 {
			var metricCount int64
			require.NoError(t, db.Model(&model.ChannelMonitorMinuteRouteMetric{}).Count(&metricCount).Error)
			assert.Zero(t, metricCount)
		}
	}

	var metric model.ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where("channel_id = ?", 9).First(&metric).Error)
	assert.Equal(t, oldMinute, metric.MinuteStart)
	coverage, err := model.GetChannelMonitorAggregationCoverage(context.Background())
	require.NoError(t, err)
	assert.Equal(t, targetEnd-int64(180*time.Minute/time.Second), coverage.CoveredFrom)
	assert.Equal(t, targetEnd, coverage.CompletedThrough)
}

func TestChannelMonitorAggregationBackfillLimitsUseDocumentedBounds(t *testing.T) {
	t.Setenv("CHANNEL_MONITOR_AGGREGATION_BACKFILL_MAX_CHUNKS", "")
	t.Setenv("CHANNEL_MONITOR_AGGREGATION_BACKFILL_BUDGET_SECONDS", "")
	t.Setenv("CHANNEL_MONITOR_AGGREGATION_BACKFILL_YIELD_MS", "")
	maxChunks, budget, yield := channelMonitorAggregationBackfillLimits()
	assert.Equal(t, 1, maxChunks)
	assert.Equal(t, 10*time.Second, budget)
	assert.Equal(t, 50*time.Millisecond, yield)

	t.Setenv("CHANNEL_MONITOR_AGGREGATION_BACKFILL_MAX_CHUNKS", "24")
	t.Setenv("CHANNEL_MONITOR_AGGREGATION_BACKFILL_BUDGET_SECONDS", "300")
	t.Setenv("CHANNEL_MONITOR_AGGREGATION_BACKFILL_YIELD_MS", "0")
	maxChunks, budget, yield = channelMonitorAggregationBackfillLimits()
	assert.Equal(t, 24, maxChunks)
	assert.Equal(t, 300*time.Second, budget)
	assert.Zero(t, yield)

	t.Setenv("CHANNEL_MONITOR_AGGREGATION_BACKFILL_MAX_CHUNKS", "25")
	t.Setenv("CHANNEL_MONITOR_AGGREGATION_BACKFILL_BUDGET_SECONDS", "0")
	t.Setenv("CHANNEL_MONITOR_AGGREGATION_BACKFILL_YIELD_MS", "5001")
	maxChunks, budget, yield = channelMonitorAggregationBackfillLimits()
	assert.Equal(t, 1, maxChunks)
	assert.Equal(t, 10*time.Second, budget)
	assert.Equal(t, 50*time.Millisecond, yield)
}

func TestChannelMonitorAggregationWindowUsesRecentAndStartupWindows(t *testing.T) {
	regularNow := int64(10*time.Hour/time.Second + 17*time.Minute/time.Second + 23)

	tests := []struct {
		name         string
		now          int64
		startup      bool
		expectedTail time.Duration
		expectedMode string
	}{
		{
			name:         "regular minute",
			now:          regularNow,
			expectedTail: channelMonitorAggregationNormalTail,
			expectedMode: "minute",
		},
		{
			name:         "startup repair",
			now:          regularNow,
			startup:      true,
			expectedTail: channelMonitorAggregationStartupTail,
			expectedMode: "startup_repair",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, end, mode := channelMonitorAggregationWindow(test.now, test.startup)
			expectedEnd := test.now - test.now%int64(channelMonitorAggregationInterval/time.Second)

			assert.Equal(t, expectedEnd, end)
			assert.Equal(t, int64(test.expectedTail/time.Second), end-start)
			assert.Equal(t, test.expectedMode, mode)
		})
	}
}

func TestNextChannelMonitorAggregationRunAlignsToMinuteBoundary(t *testing.T) {
	location := time.FixedZone("test", 8*60*60)
	minuteStart := time.Date(2026, 8, 4, 10, 1, 0, 0, location)
	tests := []struct {
		name     string
		now      time.Time
		expected time.Time
	}{
		{
			name:     "before boundary delay",
			now:      minuteStart.Add(500 * time.Millisecond),
			expected: minuteStart.Add(time.Second),
		},
		{
			name:     "before next minute",
			now:      minuteStart.Add(59*time.Second + 900*time.Millisecond),
			expected: minuteStart.Add(time.Minute + time.Second),
		},
		{
			name:     "at scheduled instant",
			now:      minuteStart.Add(time.Second),
			expected: minuteStart.Add(time.Minute + time.Second),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, nextChannelMonitorAggregationRun(test.now))
		})
	}
}

func TestChannelMonitorAggregationReadyEndAllowsCommitDelay(t *testing.T) {
	minuteStart := time.Unix(int64(10*time.Hour/time.Second), 0)
	assert.Equal(t, minuteStart.Add(-time.Minute).Unix(), channelMonitorAggregationReadyEnd(minuteStart))
	assert.Equal(t, minuteStart.Unix(), channelMonitorAggregationReadyEnd(minuteStart.Add(time.Second)))
}

func TestChannelMonitorAggregationInvalidatesCacheAfterSharedAdvance(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	originalIsMasterNode := common.IsMasterNode

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-shared-advance.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.LogConsumeEnabled = true
	constant.ErrorLogEnabled = true
	common.IsMasterNode = true
	require.NoError(t, db.AutoMigrate(
		&model.ChannelMonitorMinuteRouteMetric{},
		&model.ChannelMonitorMinuteAPIKeyMetric{},
		&model.ChannelMonitorAggregationState{},
		&model.ChannelSmartScheduleModelSampleState{},
	))
	key := channelMonitorAggregationDatabaseKey{db: db, logDB: db}
	channelMonitorAggregationStateMu.Lock()
	channelMonitorAggregationLocalCompletedThrough[key] = 60
	channelMonitorAggregationStateMu.Unlock()
	model.InvalidateChannelMonitorAggregateCaches()
	t.Cleanup(func() {
		channelMonitorAggregationStateMu.Lock()
		delete(channelMonitorAggregationLocalCompletedThrough, key)
		channelMonitorAggregationStateMu.Unlock()
		model.InvalidateChannelMonitorAggregateCaches()
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetMainDatabaseType(originalMainDatabaseType)
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
		common.IsMasterNode = originalIsMasterNode
		require.NoError(t, sqlDB.Close())
	})

	latestFirstToken := 100.0
	require.NoError(t, db.Create(&model.ChannelMonitorMinuteRouteMetric{
		MinuteStart:           60,
		ChannelId:             1,
		ModelKey:              "model-key",
		GroupKey:              "group-key",
		APIKeyKey:             "api-key",
		ModelName:             "model-a",
		FirstTokenSampleCount: 1,
		FirstTokenTotalMs:     100,
		LatestFirstTokenMs:    &latestFirstToken,
		LatestFirstTokenAt:    61,
		SampleCount:           1,
		LastUsedTime:          61,
	}).Error)
	require.NoError(t, model.AdvanceChannelMonitorAggregationCompletedThrough(context.Background(), 120))

	metrics, err := model.GetChannelMonitorPerformanceMetricsCached(context.Background(), 120, 1)
	require.NoError(t, err)
	require.Len(t, metrics, 1)
	require.NotNil(t, metrics[0].AverageFirstTokenMs)
	assert.Equal(t, 100.0, *metrics[0].AverageFirstTokenMs)
	require.NoError(t, db.Model(&model.ChannelMonitorMinuteRouteMetric{}).
		Where("channel_id = ?", 1).
		Updates(map[string]any{
			"first_token_total_ms":  250,
			"latest_first_token_ms": 250,
		}).Error)

	_, _, err = channelMonitorAggregationStart(context.Background(), key, 120, 120)
	require.NoError(t, err)
	metrics, err = model.GetChannelMonitorPerformanceMetricsCached(context.Background(), 120, 1)
	require.NoError(t, err)
	require.Len(t, metrics, 1)
	require.NotNil(t, metrics[0].AverageFirstTokenMs)
	assert.Equal(t, 250.0, *metrics[0].AverageFirstTokenMs)
}

func TestRunChannelMonitorAggregationOnceClearsExpiredManualPrimaryWhenSchedulingIsDisabled(t *testing.T) {
	originalDB := model.DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	originalMemoryCacheEnabled := common.MemoryCacheEnabled

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "manual-primary-expiry.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.LogConsumeEnabled = false
	constant.ErrorLogEnabled = false
	common.MemoryCacheEnabled = false
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.Ability{}, &model.ChannelSmartScheduleRouteState{}))
	t.Cleanup(func() {
		model.DB = originalDB
		common.SetMainDatabaseType(originalMainDatabaseType)
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		require.NoError(t, sqlDB.Close())
	})

	forcedPriority := int64(101)
	require.NoError(t, db.Create(&model.Option{
		Key: "ChannelMonitorSmartScheduleEnabled", Value: "false",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "vip", Model: "model-a", ChannelId: 1, Enabled: true,
		Priority: &forcedPriority, Weight: 1000,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1, GroupName: "vip", ModelName: "model-a",
		ManualPrimaryUntil: common.GetTimestamp() - 1, ManualPrimarySaved: true,
		ManualPrimarySavedPriority: 70, ManualPrimarySavedWeight: 40,
	}).Error)

	require.NoError(t, runChannelMonitorAggregationOnce(context.Background()))

	var ability model.Ability
	require.NoError(t, db.First(&ability).Error)
	assert.Nil(t, ability.Priority)
	assert.Zero(t, ability.Weight)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.First(&state).Error)
	assert.Zero(t, state.ManualPrimaryUntil)
	assert.False(t, state.ManualPrimarySaved)
}
