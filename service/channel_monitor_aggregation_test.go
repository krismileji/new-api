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

	now := common.GetTimestamp()
	completedMinute := now - now%60 - 60
	oldMinute := completedMinute - int64(channelMonitorAggregationTail/time.Second) - 60
	require.NoError(t, db.Create(&[]model.Log{
		{ChannelId: 1, ModelName: "recent", CreatedAt: completedMinute + 1, Type: model.LogTypeConsume},
		{ChannelId: 2, ModelName: "old", CreatedAt: oldMinute + 1, Type: model.LogTypeConsume},
	}).Error)

	require.NoError(t, runChannelMonitorAggregationOnce(context.Background()))
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
	require.NoError(t, runChannelMonitorAggregationOnce(context.Background()))
	require.NoError(t, db.Order("channel_id ASC").Find(&metrics).Error)
	require.Len(t, metrics, 1)
	assert.Equal(t, int64(1), metrics[0].ActualSuccessCount)
	assert.Equal(t, int64(1), metrics[0].ActualFailureCount)
}

func TestRunChannelMonitorAggregationOnceClearsExpiredManualPrimaryWithoutLogs(t *testing.T) {
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
	require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.ChannelSmartScheduleRouteState{}))
	t.Cleanup(func() {
		model.DB = originalDB
		common.SetMainDatabaseType(originalMainDatabaseType)
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		require.NoError(t, sqlDB.Close())
	})

	forcedPriority := int64(101)
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
