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
	))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	const cutoff = int64(10_000)
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
		{MinuteStart: cutoff - 1, ChannelId: 1, ModelKey: "old", GroupKey: "old", APIKeyKey: "old"},
		{MinuteStart: cutoff, ChannelId: 2, ModelKey: "keep", GroupKey: "keep", APIKeyKey: "keep"},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelMonitorMinuteDurationBucket{
		{MinuteStart: cutoff - 1, ChannelId: 1, ModelKey: "old", GroupKey: "old", BucketIndex: 1, Count: 1, TotalMs: 30},
		{MinuteStart: cutoff, ChannelId: 2, ModelKey: "keep", GroupKey: "keep", BucketIndex: 2, Count: 1, TotalMs: 70},
	}).Error)

	result, err := DeleteChannelMonitorCostsBefore(context.Background(), cutoff, 1)
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
	require.NoError(t, db.Find(&minuteRows).Error)
	require.Len(t, minuteRows, 1)
	assert.Equal(t, 2, minuteRows[0].ChannelId)

	var durationBucketRows []ChannelMonitorMinuteDurationBucket
	require.NoError(t, db.Find(&durationBucketRows).Error)
	require.Len(t, durationBucketRows, 1)
	assert.Equal(t, 2, durationBucketRows[0].ChannelId)
}

func TestDeleteChannelMonitorCostsBeforeRejectsInvalidArguments(t *testing.T) {
	_, err := DeleteChannelMonitorCostsBefore(context.Background(), 0, 100)
	assert.Error(t, err)

	_, err = DeleteChannelMonitorCostsBefore(context.Background(), 100, 0)
	assert.Error(t, err)
}
