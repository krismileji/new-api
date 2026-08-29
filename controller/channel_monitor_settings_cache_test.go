package controller

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLoadChannelMonitorSettingsSnapshotColdLoadIsShared(t *testing.T) {
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

	const callers = 8
	errs := make(chan error, callers)
	var waitGroup sync.WaitGroup
	for range callers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			settings, loadErr := loadChannelMonitorSettingsSnapshot(t.Context())
			if loadErr == nil && settings.CostRetentionDays != 123 {
				loadErr = fmt.Errorf("unexpected cost retention days: %d", settings.CostRetentionDays)
			}
			errs <- loadErr
		}()
	}
	waitGroup.Wait()
	close(errs)
	for loadErr := range errs {
		require.NoError(t, loadErr)
	}
	assert.EqualValues(t, 1, queryCount.Load())

	// A warm cache hit must not query the options table again.
	_, err = loadChannelMonitorSettingsSnapshot(t.Context())
	require.NoError(t, err)
	assert.EqualValues(t, 1, queryCount.Load())
}
