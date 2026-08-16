package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVerify02SQLiteColdStartCreatesFinalChannelMonitorSchema(t *testing.T) {
	if os.Getenv("CHANNEL_MONITOR_VERIFY02") != "1" {
		t.Skip("set CHANNEL_MONITOR_VERIFY02=1 to run the full SQLite cold-start migration acceptance")
	}

	originalDB := DB
	originalLogDB := LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalIsMasterNode := common.IsMasterNode

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "verify02-cold-start.db")), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.IsMasterNode = true
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		common.IsMasterNode = originalIsMasterNode
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	require.NoError(t, migrateDB())

	finalTables := []string{
		"channel_ratio_monitors",
		"channel_ratio_histories",
		"channel_daily_costs",
		"channel_daily_api_key_costs",
		"channel_monitor_minute_route_metrics",
		"channel_monitor_minute_api_key_metrics",
		"channel_monitor_minute_duration_buckets",
		"channel_monitor_dirty_minutes",
		"channel_monitor_aggregation_states",
		"channel_monitor_redis_effect_states",
		"channel_smart_schedule_route_states",
		"channel_smart_schedule_group_pauses",
		"channel_smart_schedule_model_sample_states",
		"channel_smart_schedule_execution_details",
		"channel_status_probe_configs",
		"channel_status_probe_states",
		"channel_status_probe_executions",
		"channel_model_detection_global_configs",
		"channel_model_detection_configs",
		"channel_model_detection_targets",
		"channel_model_detection_batches",
		"channel_model_detection_runs",
		"channel_model_detection_executions",
		"channel_model_detection_cost_events",
	}
	for _, tableName := range finalTables {
		assert.True(t, db.Migrator().HasTable(tableName), tableName)
	}
	assert.False(t, db.Migrator().HasTable("channel_monitor_minute_metrics"))

	indexes := []struct {
		model any
		name  string
	}{
		{&ChannelSmartScheduleExecutionDetail{}, "idx_channel_smart_schedule_execution_details_task_id"},
		{&ChannelSmartScheduleExecutionDetail{}, "idx_channel_smart_schedule_execution_details_created_at"},
		{&ChannelSmartScheduleRouteState{}, "idx_channel_smart_schedule_route_pool"},
		{&SystemTask{}, "idx_system_tasks_type_status_id"},
		{&ChannelMonitorMinuteRouteMetric{}, "idx_channel_monitor_minute_route_dimensions"},
		{&ChannelMonitorMinuteRouteMetric{}, "idx_cm_route_lookup"},
		{&ChannelMonitorMinuteRouteMetric{}, "idx_cm_route_channel_window"},
		{&ChannelMonitorMinuteRouteMetric{}, "idx_cm_route_group_window"},
		{&ChannelMonitorMinuteAPIKeyMetric{}, "idx_channel_monitor_minute_api_key_dimensions"},
		{&ChannelMonitorMinuteAPIKeyMetric{}, "idx_cm_api_route_lookup"},
		{&ChannelMonitorMinuteAPIKeyMetric{}, "idx_cm_api_channel_window"},
		{&ChannelMonitorMinuteAPIKeyMetric{}, "idx_cm_api_group_window"},
		{&ChannelMonitorDirtyMinute{}, "idx_channel_monitor_dirty_minute_start"},
		{&ChannelMonitorDirtyMinute{}, "idx_channel_monitor_dirty_minute_marked"},
		{&ChannelMonitorDirtyMinute{}, "idx_channel_monitor_dirty_minute_claim"},
	}
	for _, index := range indexes {
		assert.True(t, db.Migrator().HasIndex(index.model, index.name), index.name)
	}
}
