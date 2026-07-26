package model

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetChannelMonitorTodaySuccessMetricsAggregatesModelsWithinBeijingDay(t *testing.T) {
	originalLogDB := LOG_DB
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = originalLogDB
		common.SetLogDatabaseType(originalLogDatabaseType)
	})

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "today-success.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	const dayStart = int64(100)
	const daySeconds = int64(24 * 60 * 60)
	logs := []*Log{
		{ChannelId: 1, ModelName: "model-a", TokenId: 11, TokenName: "主 Key", CreatedAt: dayStart + 1, Type: LogTypeConsume, Other: `{"cache_tokens":12,"cache_creation_tokens":100,"cache_write_tokens":100}`},
		{ChannelId: 1, ModelName: "model-b", TokenId: 11, TokenName: "主 Key", CreatedAt: dayStart + 2, Type: LogTypeConsume, Other: `{"cache_tokens":0,"cache_creation_tokens_5m":64}`},
		{ChannelId: 1, TokenId: 11, TokenName: "主 Key", CreatedAt: dayStart + 3, Type: LogTypeError, IsRetryAttempt: true},
		{ChannelId: 1, ModelName: "model-a", TokenId: 11, TokenName: "主 Key", CreatedAt: dayStart + 4, Type: LogTypeError},
		{ChannelId: 2, ModelName: "model-c", TokenId: 12, TokenName: "备用 Key", CreatedAt: dayStart + 5, Type: LogTypeConsume, Other: `{"cache_creation_tokens_1h":32}`},
		{ChannelId: 2, ModelName: "model-d", TokenId: 12, TokenName: "备用 Key", CreatedAt: dayStart + 6, Type: LogTypeConsume, Other: `{"cache_write_tokens":0,"cache_creation_tokens":0,"cache_creation_tokens_5m":0,"cache_creation_tokens_1h":0}`},
		{ChannelId: 1, ModelName: "old", CreatedAt: dayStart - 1, Type: LogTypeConsume},
		{ChannelId: 1, ModelName: "tomorrow", CreatedAt: dayStart + daySeconds, Type: LogTypeConsume},
		{ChannelId: 0, ModelName: "no-channel", CreatedAt: dayStart + 7, Type: LogTypeConsume},
		{ChannelId: 1, ModelName: "manage", CreatedAt: dayStart + 8, Type: LogTypeManage},
	}
	require.NoError(t, db.Create(&logs).Error)

	metrics, err := getChannelMonitorTodaySuccessMetrics(context.Background(), dayStart)
	require.NoError(t, err)
	assert.Equal(t, int64(4), metrics.Summary.ActualSuccessCount)
	assert.Equal(t, int64(2), metrics.Summary.ActualFailureCount)
	assert.Equal(t, int64(6), metrics.Summary.ActualSampleCount)
	assert.InDelta(t, 2.0/3.0, metrics.Summary.ActualSuccessRate, 0.0001)
	assert.Equal(t, int64(1), metrics.Summary.CacheHitCount)
	assert.Equal(t, int64(2), metrics.Summary.CacheSampleCount)
	assert.InDelta(t, 0.5, metrics.Summary.CacheHitRate, 0.0001)

	require.Len(t, metrics.ChannelItems, 2)
	assert.Equal(t, 1, metrics.ChannelItems[0].ChannelId)
	assert.Equal(t, int64(4), metrics.ChannelItems[0].ActualSampleCount)
	assert.InDelta(t, 0.5, metrics.ChannelItems[0].ActualSuccessRate, 0.0001)
	assert.Equal(t, int64(3), metrics.ChannelItems[0].FinalSampleCount)
	assert.InDelta(t, 2.0/3.0, metrics.ChannelItems[0].FinalSuccessRate, 0.0001)
	assert.Equal(t, int64(2), metrics.ChannelItems[0].CacheSampleCount)
	assert.InDelta(t, 0.5, metrics.ChannelItems[0].CacheHitRate, 0.0001)
	assert.Equal(t, 2, metrics.ChannelItems[1].ChannelId)
	assert.Equal(t, int64(2), metrics.ChannelItems[1].ActualSampleCount)
	assert.InDelta(t, 1, metrics.ChannelItems[1].ActualSuccessRate, 0.0001)
	assert.Zero(t, metrics.ChannelItems[1].CacheSampleCount)
	require.Len(t, metrics.CacheWriteItems, 2)
	assert.Equal(t, 1, metrics.CacheWriteItems[0].ChannelId)
	assert.Equal(t, int64(2), metrics.CacheWriteItems[0].RequestCount)
	assert.Equal(t, 2, metrics.CacheWriteItems[1].ChannelId)
	assert.Equal(t, int64(1), metrics.CacheWriteItems[1].RequestCount)
	require.Len(t, metrics.APIKeyItems, 2)
	assert.Equal(t, 11, metrics.APIKeyItems[0].APIKeyId)
	assert.Equal(t, "主 Key", metrics.APIKeyItems[0].APIKeyName)
	assert.Equal(t, int64(4), metrics.APIKeyItems[0].ActualSampleCount)
	assert.InDelta(t, 0.5, metrics.APIKeyItems[0].CacheHitRate, 0.0001)
	assert.Equal(t, 12, metrics.APIKeyItems[1].APIKeyId)
	assert.Equal(t, "备用 Key", metrics.APIKeyItems[1].APIKeyName)
	assert.Equal(t, int64(2), metrics.APIKeyItems[1].ActualSampleCount)
}

func TestGetChannelMonitorDailySuccessMetricsAggregatesEachBeijingDay(t *testing.T) {
	originalLogDB := LOG_DB
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = originalLogDB
		common.SetLogDatabaseType(originalLogDatabaseType)
	})

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "daily-success.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	firstDayStart := ChannelDailyCostDayStart(1_700_000_000)
	secondDayStart := firstDayStart + channelDailyCostDaySeconds
	logs := []*Log{
		{ChannelId: 1, CreatedAt: firstDayStart + 1, Type: LogTypeConsume, Other: `{"cache_tokens":12,"cache_write_tokens":100}`},
		{ChannelId: 1, CreatedAt: firstDayStart + 2, Type: LogTypeError, IsRetryAttempt: true},
		{ChannelId: 2, CreatedAt: firstDayStart + 3, Type: LogTypeConsume, Other: `{"cache_creation_tokens_5m":64}`},
		{ChannelId: 1, CreatedAt: secondDayStart + 1, Type: LogTypeConsume, Other: `{"cache_tokens":0}`},
		{ChannelId: 2, CreatedAt: secondDayStart + 2, Type: LogTypeError},
		{ChannelId: 1, CreatedAt: firstDayStart - 1, Type: LogTypeConsume},
		{ChannelId: 1, CreatedAt: secondDayStart + channelDailyCostDaySeconds, Type: LogTypeConsume},
	}
	require.NoError(t, db.Create(&logs).Error)

	items, err := getChannelMonitorDailySuccessMetrics(
		context.Background(), firstDayStart, secondDayStart+channelDailyCostDaySeconds,
	)
	require.NoError(t, err)
	require.Len(t, items, 2)

	assert.Equal(t, firstDayStart, items[0].DayStart)
	assert.Equal(t, int64(3), items[0].Summary.ActualSampleCount)
	assert.Equal(t, int64(2), items[0].Summary.FinalSampleCount)
	assert.InDelta(t, 1, items[0].Summary.FinalSuccessRate, 0.0001)
	assert.Equal(t, int64(1), items[0].Summary.CacheHitCount)
	assert.Equal(t, int64(1), items[0].Summary.CacheSampleCount)
	assert.Equal(t, int64(2), items[0].CacheWriteRequestCount)
	assert.Equal(t, 2, items[0].CacheWriteChannelCount)

	assert.Equal(t, secondDayStart, items[1].DayStart)
	assert.Equal(t, int64(2), items[1].Summary.ActualSampleCount)
	assert.InDelta(t, 0.5, items[1].Summary.ActualSuccessRate, 0.0001)
	assert.Zero(t, items[1].Summary.CacheHitCount)
	assert.Equal(t, int64(1), items[1].Summary.CacheSampleCount)
	assert.Zero(t, items[1].CacheWriteRequestCount)
	assert.Zero(t, items[1].CacheWriteChannelCount)
}

func TestGetChannelMonitorTodaySuccessMetricsCachedReusesResultAndReturnsCopy(t *testing.T) {
	originalLogDB := LOG_DB
	originalLogDatabaseType := common.LogDatabaseType()
	resetChannelMonitorTodaySuccessCache()
	t.Cleanup(func() {
		LOG_DB = originalLogDB
		common.SetLogDatabaseType(originalLogDatabaseType)
		resetChannelMonitorTodaySuccessCache()
	})

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "today-success-cache.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	generatedAt := int64(1_700_000_000)
	dayStart := ChannelDailyCostDayStart(generatedAt)
	require.NoError(t, db.Create(&Log{
		ChannelId: 1,
		ModelName: "model-a",
		CreatedAt: dayStart + 1,
		Type:      LogTypeConsume,
		Other:     `{"cache_write_tokens":10}`,
	}).Error)

	first, err := GetChannelMonitorTodaySuccessMetricsCached(context.Background(), generatedAt)
	require.NoError(t, err)
	require.Len(t, first.ChannelItems, 1)
	require.Len(t, first.APIKeyItems, 1)
	require.Len(t, first.CacheWriteItems, 1)
	first.ChannelItems[0].ActualSampleCount = -1
	first.APIKeyItems[0].ActualSampleCount = -1
	first.CacheWriteItems[0].RequestCount = -1
	require.NoError(t, db.Create(&Log{
		ChannelId: 1,
		ModelName: "model-b",
		CreatedAt: dayStart + 2,
		Type:      LogTypeConsume,
	}).Error)

	second, err := GetChannelMonitorTodaySuccessMetricsCached(context.Background(), generatedAt+1)
	require.NoError(t, err)
	require.Len(t, second.ChannelItems, 1)
	require.Len(t, second.APIKeyItems, 1)
	require.Len(t, second.CacheWriteItems, 1)
	assert.Equal(t, int64(1), second.Summary.ActualSampleCount)
	assert.Equal(t, int64(1), second.ChannelItems[0].ActualSampleCount)
	assert.Equal(t, int64(1), second.APIKeyItems[0].ActualSampleCount)
	assert.Equal(t, int64(1), second.CacheWriteItems[0].RequestCount)
}
