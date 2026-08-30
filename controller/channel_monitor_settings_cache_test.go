package controller

import (
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLoadChannelMonitorSettingsReadsCurrentDatabaseState(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	db, err := gorm.Open(sqlite.Open("file:channel-monitor-settings-cache?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = nil
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	require.NoError(t, db.Create(&model.Option{
		Key:   channelMonitorCostRetentionDaysOption,
		Value: "123",
	}).Error)

	var queryCount atomic.Int64
	callbackName := "channel_monitor_settings_cache_test_query"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(*gorm.DB) {
		queryCount.Add(1)
	}))
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(callbackName)
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	settings, err := loadChannelMonitorSettings(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 123, settings.CostRetentionDays)
	assert.EqualValues(t, 1, queryCount.Load())

	require.NoError(t, db.Model(&model.Option{}).
		Where("key = ?", channelMonitorCostRetentionDaysOption).
		Update("value", "456").Error)

	settings, err = loadChannelMonitorSettings(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 456, settings.CostRetentionDays)
	assert.EqualValues(t, 2, queryCount.Load())
}
