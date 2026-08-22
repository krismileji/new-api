package model

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDeleteChannelMonitorCostsBeforeRemovesOnlyExpiredRows(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-cost-retention.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(
		&ChannelDailyCost{},
		&ChannelDailyAPIKeyCost{},
		&ChannelTaskCostEvent{},
		&ChannelMonitorMinuteRouteMetric{},
		&ChannelMonitorMinuteAPIKeyMetric{},
		&ChannelMonitorMinuteDurationBucket{},
		&ChannelMonitorAggregationState{},
	))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	const cutoff = int64(10_000)
	const minuteCutoff = int64(9_000)
	require.NoError(t, db.Create(&[]ChannelDailyCost{
		{ChannelId: 1, DayStart: cutoff - 1, SettledCount: 1, CreatedAt: 1, UpdatedAt: 1},
		{ChannelId: 2, DayStart: cutoff, SettledCount: 1, CreatedAt: 1, UpdatedAt: 1},
		{ChannelId: 3, DayStart: cutoff + 1, SettledCount: 1, CreatedAt: 1, UpdatedAt: 1},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelDailyAPIKeyCost{
		{ChannelId: 1, DayStart: cutoff - 1, KeyFingerprint: "old-a", KeyDisplay: "old", SettledCount: 1, CreatedAt: 1, UpdatedAt: 1},
		{ChannelId: 2, DayStart: cutoff - 2, KeyFingerprint: "old-b", KeyDisplay: "old", SettledCount: 1, CreatedAt: 1, UpdatedAt: 1},
		{ChannelId: 3, DayStart: cutoff, KeyFingerprint: "keep", KeyDisplay: "keep", SettledCount: 1, CreatedAt: 1, UpdatedAt: 1},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelTaskCostEvent{
		{CostEventId: "task:expired", ChannelId: 1, DayStart: cutoff - 1, OccurredAt: cutoff - 1, InitialQuota: 10, InitialCostNanoCNY: 100, CostNanoCNY: 100, CreatedAt: 1, UpdatedAt: 1},
		{CostEventId: "task:keep", ChannelId: 2, DayStart: cutoff, OccurredAt: cutoff, InitialQuota: 10, InitialCostNanoCNY: 100, CostNanoCNY: 100, CreatedAt: 1, UpdatedAt: 1},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelMonitorMinuteRouteMetric{
		{MinuteStart: minuteCutoff - 1, ChannelId: 1, ModelKey: "expired", GroupKey: "expired", APIKeyKey: "expired"},
		{MinuteStart: cutoff - 1, ChannelId: 1, ModelKey: "protected", GroupKey: "protected", APIKeyKey: "protected"},
		{MinuteStart: cutoff, ChannelId: 2, ModelKey: "keep", GroupKey: "keep", APIKeyKey: "keep"},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelMonitorMinuteAPIKeyMetric{
		{MinuteStart: minuteCutoff - 1, ChannelId: 1, ModelKey: "expired", GroupKey: "expired", APIKeyKey: "expired"},
		{MinuteStart: cutoff - 1, ChannelId: 1, ModelKey: "protected", GroupKey: "protected", APIKeyKey: "protected"},
		{MinuteStart: cutoff, ChannelId: 2, ModelKey: "keep", GroupKey: "keep", APIKeyKey: "keep"},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelMonitorMinuteDurationBucket{
		{MinuteStart: minuteCutoff - 1, ChannelId: 1, ModelKey: "expired", GroupKey: "expired", BucketIndex: 1, Count: 1, TotalMs: 30},
		{MinuteStart: cutoff - 1, ChannelId: 1, ModelKey: "protected", GroupKey: "protected", BucketIndex: 1, Count: 1, TotalMs: 30},
		{MinuteStart: cutoff, ChannelId: 2, ModelKey: "keep", GroupKey: "keep", BucketIndex: 2, Count: 1, TotalMs: 70},
	}).Error)
	require.NoError(t, db.Create(&ChannelMonitorAggregationState{
		ID: channelMonitorAggregationStateID, CoveredFrom: 1_000, CompletedThrough: 11_000,
	}).Error)

	exhaustedBudget := ChannelMonitorCleanupBudget{deadline: time.Now().Add(-time.Second)}
	result, err := DeleteChannelMonitorCostsBefore(
		context.Background(), cutoff, minuteCutoff, minuteCutoff, 1, exhaustedBudget,
	)
	require.NoError(t, err)
	assert.True(t, result.Incomplete)
	assert.Zero(t, result.ChannelRowsDeleted)
	assert.Zero(t, result.APIKeyRowsDeleted)
	assert.Zero(t, result.TaskCostEventRowsDeleted)
	assert.Zero(t, result.MinuteRowsDeleted)
	assert.Zero(t, result.DurationBucketRowsDeleted)
	var beforeResume int64
	require.NoError(t, db.Model(&ChannelDailyCost{}).Count(&beforeResume).Error)
	assert.EqualValues(t, 3, beforeResume)

	result, err = DeleteChannelMonitorCostsBefore(
		context.Background(), cutoff, minuteCutoff, minuteCutoff, 1, ChannelMonitorCleanupBudget{},
	)
	require.NoError(t, err)
	assert.False(t, result.Incomplete)
	assert.Equal(t, int64(1), result.ChannelRowsDeleted)
	assert.Equal(t, int64(2), result.APIKeyRowsDeleted)
	assert.Equal(t, int64(1), result.TaskCostEventRowsDeleted)
	assert.Equal(t, int64(2), result.MinuteRowsDeleted)
	assert.Equal(t, int64(1), result.DurationBucketRowsDeleted)

	var channelRows []ChannelDailyCost
	require.NoError(t, db.Order("channel_id ASC").Find(&channelRows).Error)
	require.Len(t, channelRows, 2)
	assert.Equal(t, []int{2, 3}, []int{channelRows[0].ChannelId, channelRows[1].ChannelId})

	var apiKeyRows []ChannelDailyAPIKeyCost
	require.NoError(t, db.Find(&apiKeyRows).Error)
	require.Len(t, apiKeyRows, 1)
	assert.Equal(t, 3, apiKeyRows[0].ChannelId)

	var taskCostEvents []ChannelTaskCostEvent
	require.NoError(t, db.Find(&taskCostEvents).Error)
	require.Len(t, taskCostEvents, 1)
	assert.Equal(t, "task:keep", taskCostEvents[0].CostEventId)

	var minuteRows []ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Order("minute_start ASC").Find(&minuteRows).Error)
	require.Len(t, minuteRows, 2)
	assert.Equal(t, []int64{cutoff - 1, cutoff}, []int64{minuteRows[0].MinuteStart, minuteRows[1].MinuteStart})
	var apiMinuteRows []ChannelMonitorMinuteAPIKeyMetric
	require.NoError(t, db.Order("minute_start ASC").Find(&apiMinuteRows).Error)
	require.Len(t, apiMinuteRows, 2)
	assert.Equal(t, []int64{cutoff - 1, cutoff}, []int64{apiMinuteRows[0].MinuteStart, apiMinuteRows[1].MinuteStart})

	var durationBucketRows []ChannelMonitorMinuteDurationBucket
	require.NoError(t, db.Order("minute_start ASC").Find(&durationBucketRows).Error)
	require.Len(t, durationBucketRows, 2)
	assert.Equal(t, []int64{cutoff - 1, cutoff}, []int64{durationBucketRows[0].MinuteStart, durationBucketRows[1].MinuteStart})

	coverage, err := GetChannelMonitorAggregationCoverage(context.Background())
	require.NoError(t, err)
	assert.Equal(t, minuteCutoff, coverage.CoveredFrom)
	assert.Equal(t, int64(11_000), coverage.CompletedThrough)
}

func TestDeleteChannelMonitorCostsBeforeRejectsInvalidArguments(t *testing.T) {
	_, err := DeleteChannelMonitorCostsBefore(context.Background(), 0, 100, 100, 100, ChannelMonitorCleanupBudget{})
	assert.Error(t, err)

	_, err = DeleteChannelMonitorCostsBefore(context.Background(), 100, 0, 100, 100, ChannelMonitorCleanupBudget{})
	assert.Error(t, err)

	_, err = DeleteChannelMonitorCostsBefore(context.Background(), 100, 100, 0, 100, ChannelMonitorCleanupBudget{})
	assert.Error(t, err)
}

func TestDeleteChannelMonitorCostsBeforeUsesIndependentMetricCutoffs(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-independent-retention.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(
		&ChannelDailyCost{},
		&ChannelDailyAPIKeyCost{},
		&ChannelTaskCostEvent{},
		&ChannelMonitorMinuteRouteMetric{},
		&ChannelMonitorMinuteAPIKeyMetric{},
		&ChannelMonitorMinuteDurationBucket{},
		&ChannelMonitorAggregationState{},
	))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	const (
		costCutoff     = int64(1000)
		routeCutoff    = int64(800)
		durationCutoff = int64(700)
		apiKeyCutoff   = int64(500)
		currentMinute  = int64(900)
	)
	require.NoError(t, db.Create(&[]ChannelDailyCost{
		{ChannelId: 1, DayStart: costCutoff - 1, SettledCount: 1, CreatedAt: 1, UpdatedAt: 1},
		{ChannelId: 2, DayStart: costCutoff, SettledCount: 1, CreatedAt: 1, UpdatedAt: 1},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelDailyAPIKeyCost{
		{ChannelId: 1, DayStart: costCutoff - 1, KeyFingerprint: "old", KeyDisplay: "old", SettledCount: 1, CreatedAt: 1, UpdatedAt: 1},
		{ChannelId: 2, DayStart: costCutoff, KeyFingerprint: "keep", KeyDisplay: "keep", SettledCount: 1, CreatedAt: 1, UpdatedAt: 1},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelTaskCostEvent{
		{CostEventId: "task:old", ChannelId: 1, DayStart: costCutoff - 1, OccurredAt: costCutoff - 1, InitialQuota: 10, InitialCostNanoCNY: 100, CostNanoCNY: 100, CreatedAt: 1, UpdatedAt: 1},
		{CostEventId: "task:current", ChannelId: 2, DayStart: costCutoff, OccurredAt: costCutoff, InitialQuota: 10, InitialCostNanoCNY: 100, CostNanoCNY: 100, CreatedAt: 1, UpdatedAt: 1},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelMonitorMinuteRouteMetric{
		{MinuteStart: routeCutoff - 1, ChannelId: 1, ModelKey: "old", GroupKey: "old", APIKeyKey: "old"},
		{MinuteStart: routeCutoff, ChannelId: 2, ModelKey: "keep", GroupKey: "keep", APIKeyKey: "keep"},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelMonitorMinuteAPIKeyMetric{
		{MinuteStart: apiKeyCutoff - 1, ChannelId: 1, ModelKey: "old", GroupKey: "old", APIKeyKey: "old"},
		{MinuteStart: apiKeyCutoff, ChannelId: 2, ModelKey: "keep", GroupKey: "keep", APIKeyKey: "keep"},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelMonitorMinuteDurationBucket{
		{MinuteStart: durationCutoff - 1, ChannelId: 1, ModelKey: "old", GroupKey: "old", BucketIndex: 1, Count: 1, TotalMs: 30},
		{MinuteStart: durationCutoff, ChannelId: 2, ModelKey: "keep", GroupKey: "keep", BucketIndex: 1, Count: 1, TotalMs: 30},
	}).Error)
	require.NoError(t, db.Create(&ChannelMonitorAggregationState{
		ID: channelMonitorAggregationStateID, CoveredFrom: 1, CompletedThrough: currentMinute,
	}).Error)

	result, err := DeleteChannelMonitorCostsBeforeWithDurationBucketCutoff(
		context.Background(), costCutoff, routeCutoff, durationCutoff, apiKeyCutoff, 10, ChannelMonitorCleanupBudget{},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.ChannelRowsDeleted)
	assert.Equal(t, int64(1), result.APIKeyRowsDeleted)
	assert.Equal(t, int64(1), result.TaskCostEventRowsDeleted)
	assert.Equal(t, int64(1), result.RouteMetricRowsDeleted)
	assert.Equal(t, int64(1), result.APIKeyMetricRowsDeleted)
	assert.Equal(t, int64(2), result.MinuteRowsDeleted)
	assert.Equal(t, int64(1), result.DurationBucketRowsDeleted)
}
