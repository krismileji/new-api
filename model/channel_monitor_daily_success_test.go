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

func TestUpdateChannelMonitorDailySuccessForMinuteIsIdempotentAndReplacesContribution(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-daily-success.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Token{}, &ChannelMonitorMinuteAPIKeyMetric{}, &ChannelMonitorDailySuccessLedger{}, &ChannelMonitorDailySuccessMinute{}))
	previousDB := DB
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	require.NoError(t, db.Create(&Token{Id: 301, UserId: 41, Name: "key-301"}).Error)
	minuteStart := channelMonitorMinuteStart(1_750_000_000)
	require.NoError(t, db.Create(&ChannelMonitorMinuteAPIKeyMetric{
		MinuteStart:        minuteStart,
		ChannelId:          7,
		ModelKey:           "model-key",
		GroupKey:           "group-key",
		APIKeyKey:          "api-key-key",
		ModelName:          "model-a",
		GroupName:          "default",
		APIKeyId:           301,
		APIKeyName:         "key-301",
		ActualSuccessCount: 2,
		FinalSuccessCount:  2,
		CacheHitCount:      1,
		CacheSampleCount:   2,
		CacheReadTokens:    20,
		InputTokens:        100,
	}).Error)

	ctx := context.Background()
	require.NoError(t, UpdateChannelMonitorDailySuccessForMinuteRange(ctx, minuteStart, minuteStart+60))
	require.NoError(t, UpdateChannelMonitorDailySuccessForMinuteRange(ctx, minuteStart, minuteStart+60))

	var ledger ChannelMonitorDailySuccessLedger
	require.NoError(t, db.First(&ledger).Error)
	assert.Equal(t, 41, ledger.UserId)
	assert.Equal(t, "inferred", ledger.UserAttribution)
	assert.Equal(t, int64(2), ledger.ActualSuccessCount)
	assert.Equal(t, int64(20), ledger.CacheReadTokens)

	require.NoError(t, db.Model(&ChannelMonitorMinuteAPIKeyMetric{}).
		Where("minute_start = ?", minuteStart).
		Updates(map[string]any{"actual_success_count": 5, "final_success_count": 4, "cache_read_tokens": 50}).Error)
	require.NoError(t, UpdateChannelMonitorDailySuccessForMinuteRange(ctx, minuteStart, minuteStart+60))
	require.NoError(t, db.First(&ledger).Error)
	assert.Equal(t, int64(5), ledger.ActualSuccessCount)
	assert.Equal(t, int64(4), ledger.FinalSuccessCount)
	assert.Equal(t, int64(50), ledger.CacheReadTokens)
}
