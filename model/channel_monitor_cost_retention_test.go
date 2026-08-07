package model

import (
	"context"
	"path/filepath"
	"testing"

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
		&ChannelMonitorMinuteMetric{},
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
	require.NoError(t, db.Create(&[]ChannelMonitorMinuteMetric{
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

	result, err := DeleteChannelMonitorCostsBefore(context.Background(), cutoff, minuteCutoff, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.ChannelRowsDeleted)
	assert.Equal(t, int64(2), result.APIKeyRowsDeleted)
	assert.Equal(t, int64(1), result.MinuteRowsDeleted)
	assert.Equal(t, int64(1), result.DurationBucketRowsDeleted)

	var channelRows []ChannelDailyCost
	require.NoError(t, db.Order("channel_id ASC").Find(&channelRows).Error)
	require.Len(t, channelRows, 2)
	assert.Equal(t, []int{2, 3}, []int{channelRows[0].ChannelId, channelRows[1].ChannelId})

	var apiKeyRows []ChannelDailyAPIKeyCost
	require.NoError(t, db.Find(&apiKeyRows).Error)
	require.Len(t, apiKeyRows, 1)
	assert.Equal(t, 3, apiKeyRows[0].ChannelId)

	var minuteRows []ChannelMonitorMinuteMetric
	require.NoError(t, db.Order("minute_start ASC").Find(&minuteRows).Error)
	require.Len(t, minuteRows, 2)
	assert.Equal(t, []int64{cutoff - 1, cutoff}, []int64{minuteRows[0].MinuteStart, minuteRows[1].MinuteStart})

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
	_, err := DeleteChannelMonitorCostsBefore(context.Background(), 0, 100, 100)
	assert.Error(t, err)

	_, err = DeleteChannelMonitorCostsBefore(context.Background(), 100, 0, 100)
	assert.Error(t, err)

	_, err = DeleteChannelMonitorCostsBefore(context.Background(), 100, 100, 0)
	assert.Error(t, err)
}
