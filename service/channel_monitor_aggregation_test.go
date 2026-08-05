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

func TestRunChannelMonitorAggregationOnceRebuildsOnlyRecentCompletedMinutes(t *testing.T) {
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
		&model.ChannelMonitorMinuteMetric{},
		&model.ChannelMonitorAggregationState{},
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
	oldMinute := targetEnd - int64(channelMonitorAggregationRecentTail/time.Second) - 60
	require.NoError(t, db.Create(&[]model.Log{
		{ChannelId: 1, ModelName: "recent", CreatedAt: completedMinute + 1, Type: model.LogTypeConsume},
		{ChannelId: 2, ModelName: "old", CreatedAt: oldMinute + 1, Type: model.LogTypeConsume},
	}).Error)

	require.NoError(t, runChannelMonitorAggregationAt(context.Background(), now, false))
	var metrics []model.ChannelMonitorMinuteMetric
	require.NoError(t, db.Order("channel_id ASC").Find(&metrics).Error)
	require.Len(t, metrics, 1)
	assert.Equal(t, 1, metrics[0].ChannelId)
	assert.Equal(t, int64(1), metrics[0].ActualSuccessCount)

	require.NoError(t, db.Create(&model.Log{
		ChannelId: 1,
		ModelName: "recent",
		CreatedAt: completedMinute + 2,
		Type:      model.LogTypeError,
	}).Error)
	require.NoError(t, runChannelMonitorAggregationAt(context.Background(), now, false))
	require.NoError(t, db.Order("channel_id ASC").Find(&metrics).Error)
	require.Len(t, metrics, 1)
	assert.Equal(t, int64(1), metrics[0].ActualSuccessCount)
	assert.Equal(t, int64(1), metrics[0].ActualFailureCount)

	require.NoError(t, runChannelMonitorAggregationAt(context.Background(), now, true))
	require.NoError(t, db.Order("channel_id ASC").Find(&metrics).Error)
	require.Len(t, metrics, 2)
	assert.Equal(t, []int{1, 2}, []int{metrics[0].ChannelId, metrics[1].ChannelId})

	laterNow := now + int64(4*time.Minute/time.Second)
	gapMinute := targetEnd + int64(time.Minute/time.Second)
	require.NoError(t, db.Create(&model.Log{
		ChannelId: 3, ModelName: "gap", CreatedAt: gapMinute + 1, Type: model.LogTypeConsume,
	}).Error)
	require.NoError(t, runChannelMonitorAggregationAt(context.Background(), laterNow, false))
	var gapMetric model.ChannelMonitorMinuteMetric
	require.NoError(t, db.Where("channel_id = ?", 3).First(&gapMetric).Error)
	assert.Equal(t, gapMinute, gapMetric.MinuteStart)
	completedThrough, err := model.GetChannelMonitorAggregationCompletedThrough(context.Background())
	require.NoError(t, err)
	assert.Equal(t, laterNow-laterNow%60, completedThrough)
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
			expectedTail: channelMonitorAggregationRecentTail,
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

func TestChannelMonitorAggregationRepairWindowRunsOnlyOnHourBoundary(t *testing.T) {
	hourEnd := int64(11 * time.Hour / time.Second)
	start, end, repair := channelMonitorAggregationRepairWindow(hourEnd)
	require.True(t, repair)
	assert.Equal(t, hourEnd, end)
	assert.Equal(t, int64(channelMonitorAggregationRepairTail/time.Second), end-start)

	_, _, repair = channelMonitorAggregationRepairWindow(hourEnd + 60)
	assert.False(t, repair)
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
	targetEnd, err := channelMonitorAggregationFreshTarget(context.Background(), minuteStart)
	require.NoError(t, err)
	assert.Equal(t, minuteStart.Unix(), targetEnd)
}

func TestEnsureChannelMonitorAggregationFreshRunsOncePerCompletedMinute(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	originalIsMasterNode := common.IsMasterNode

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-freshness.db")), &gorm.Config{})
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
		&model.ChannelMonitorMinuteMetric{},
		&model.ChannelMonitorAggregationState{},
	))
	key := channelMonitorAggregationDatabaseKey{db: db, logDB: db}
	t.Cleanup(func() {
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

	now := time.Unix(int64(10*time.Hour/time.Second+17*time.Minute/time.Second), 0).Add(2 * time.Second)
	firstMinute := channelMonitorAggregationReadyEnd(now) - int64(time.Minute/time.Second)
	require.NoError(t, db.Create(&model.Log{
		ChannelId: 1, ModelName: "model-a", CreatedAt: firstMinute + 1, Type: model.LogTypeConsume,
	}).Error)

	require.NoError(t, EnsureChannelMonitorAggregationFresh(context.Background(), now))
	var metric model.ChannelMonitorMinuteMetric
	require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", firstMinute, 1).First(&metric).Error)
	assert.Equal(t, int64(1), metric.ActualSuccessCount)

	require.NoError(t, db.Create(&model.Log{
		ChannelId: 1, ModelName: "model-a", CreatedAt: firstMinute + 2, Type: model.LogTypeConsume,
	}).Error)
	require.NoError(t, EnsureChannelMonitorAggregationFresh(context.Background(), now.Add(20*time.Second)))
	require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", firstMinute, 1).First(&metric).Error)
	assert.Equal(t, int64(1), metric.ActualSuccessCount)

	nextMinute := now.Add(time.Minute)
	require.NoError(t, EnsureChannelMonitorAggregationFresh(context.Background(), nextMinute))
	require.NoError(t, db.Where("minute_start = ? AND channel_id = ?", firstMinute, 1).First(&metric).Error)
	assert.Equal(t, int64(2), metric.ActualSuccessCount)
	completedThrough, err := model.GetChannelMonitorAggregationCompletedThrough(context.Background())
	require.NoError(t, err)
	assert.Equal(t, channelMonitorAggregationReadyEnd(nextMinute), completedThrough)
}

func TestEnsureChannelMonitorAggregationFreshHonorsContextWhileAggregationIsBusy(t *testing.T) {
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	originalIsMasterNode := common.IsMasterNode
	common.LogConsumeEnabled = true
	constant.ErrorLogEnabled = true
	common.IsMasterNode = true
	key := channelMonitorAggregationDatabaseKey{db: model.DB, logDB: model.LOG_DB}
	channelMonitorAggregationStateMu.Lock()
	previousCompletedThrough, hadPreviousCompletedThrough := channelMonitorAggregationLocalCompletedThrough[key]
	delete(channelMonitorAggregationLocalCompletedThrough, key)
	channelMonitorAggregationStateMu.Unlock()
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
		common.IsMasterNode = originalIsMasterNode
		channelMonitorAggregationStateMu.Lock()
		if hadPreviousCompletedThrough {
			channelMonitorAggregationLocalCompletedThrough[key] = previousCompletedThrough
		} else {
			delete(channelMonitorAggregationLocalCompletedThrough, key)
		}
		channelMonitorAggregationStateMu.Unlock()
	})

	channelMonitorAggregationRunMu.Lock()
	defer channelMonitorAggregationRunMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	now := time.Unix(int64(10*time.Hour/time.Second+17*time.Minute/time.Second), 0).Add(2 * time.Second)

	err := EnsureChannelMonitorAggregationFresh(ctx, now)

	assert.ErrorIs(t, err, context.Canceled)
}

func TestEnsureChannelMonitorAggregationFreshInvalidatesCacheAfterSharedAdvance(t *testing.T) {
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
		&model.ChannelMonitorMinuteMetric{},
		&model.ChannelMonitorAggregationState{},
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
	require.NoError(t, db.Create(&model.ChannelMonitorMinuteMetric{
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
	require.NoError(t, db.Model(&model.ChannelMonitorMinuteMetric{}).
		Where("channel_id = ?", 1).
		Updates(map[string]any{
			"first_token_total_ms":  250,
			"latest_first_token_ms": 250,
		}).Error)

	require.NoError(t, EnsureChannelMonitorAggregationFresh(
		context.Background(),
		time.Unix(120, 0).Add(2*time.Second),
	))
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
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(70), *ability.Priority)
	assert.Equal(t, uint(40), ability.Weight)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.First(&state).Error)
	assert.Zero(t, state.ManualPrimaryUntil)
	assert.False(t, state.ManualPrimarySaved)
}
