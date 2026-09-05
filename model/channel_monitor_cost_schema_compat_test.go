package model

import (
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestChannelMonitorCostSchemaFreshUpgradeAndSecondMigration(t *testing.T) {
	dialect := strings.TrimSpace(os.Getenv("CHANNEL_MONITOR_SCHEMA_DIALECT"))
	if dialect == "" {
		t.Skip("设置 CHANNEL_MONITOR_SCHEMA_DIALECT=sqlite|mysql|postgres 运行渠道监控成本 schema 验收")
	}
	var dialector gorm.Dialector
	switch dialect {
	case "sqlite":
		dialector = sqlite.Open(t.TempDir() + "/channel-monitor-cost-schema.db")
	case "mysql":
		dsn := strings.TrimSpace(os.Getenv("CHANNEL_MONITOR_SCHEMA_DSN"))
		require.Contains(t, dsn, "new_api_cm_schema", "MySQL schema 验收只能使用独立数据库")
		dialector = mysql.Open(dsn)
	case "postgres":
		dsn := strings.TrimSpace(os.Getenv("CHANNEL_MONITOR_SCHEMA_DSN"))
		require.Contains(t, dsn, "new_api_cm_schema", "PostgreSQL schema 验收只能使用独立数据库")
		dialector = postgres.Open(dsn)
	default:
		t.Fatalf("不支持的 schema 验收数据库: %s", dialect)
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		if dialect != "sqlite" {
			for index := len(channelMonitorCostSchemaModels()) - 1; index >= 0; index-- {
				assert.NoError(t, db.Migrator().DropTable(channelMonitorCostSchemaModels()[index]))
			}
		}
		assert.NoError(t, sqlDB.Close())
	})

	models := channelMonitorCostSchemaModels()
	for index := len(models) - 1; index >= 0; index-- {
		assert.NoError(t, db.Migrator().DropTable(models[index]))
	}
	require.NoError(t, db.AutoMigrate(models...))
	for _, value := range models {
		assert.True(t, db.Migrator().HasTable(value))
	}
	for index := len(models) - 1; index >= 0; index-- {
		require.NoError(t, db.Migrator().DropTable(models[index]))
	}
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}, &ChannelDailyAPIKeyCost{}))
	require.NoError(t, db.AutoMigrate(models...))
	require.NoError(t, db.AutoMigrate(models...))
	for _, value := range models {
		assert.True(t, db.Migrator().HasTable(value))
	}
	for _, index := range []struct {
		model any
		name  string
	}{
		{&ChannelMonitorDailySuccessLedger{}, "idx_cm_daily_success_dim"},
		{&ChannelMonitorDailySuccessMinute{}, "idx_cm_daily_success_minute_dim"},
		{&ChannelMonitorDailyCostDetail{}, "idx_cm_daily_cost_detail_dim"},
		{&ChannelMonitorCostBackfillCheckpoint{}, "idx_cm_cost_backfill_batch_day"},
		{&ChannelMonitorCostReconciliation{}, "idx_cm_cost_reconcile_batch_day"},
	} {
		assert.True(t, db.Migrator().HasIndex(index.model, index.name), index.name)
	}
}

func channelMonitorCostSchemaModels() []any {
	return []any{
		&ChannelDailyCost{}, &ChannelDailyAPIKeyCost{}, &ChannelMonitorDailySuccessLedger{},
		&ChannelMonitorDailySuccessMinute{}, &ChannelMonitorDailyCostDetail{},
		&ChannelMonitorCostBackfillCheckpoint{}, &ChannelMonitorCostReconciliation{},
	}
}
