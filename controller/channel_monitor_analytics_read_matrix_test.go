package controller

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Run only against the disposable databases created for the analytics audit.
// The same HTTP/query contract cases execute on each real database engine.
func TestChannelMonitorAnalyticsReadDatabaseMatrix(t *testing.T) {
	dialect := os.Getenv("CHANNEL_MONITOR_CONSISTENCY_DIALECT")
	if dialect == "" {
		t.Skip("需要指定渠道监控分析专用数据库")
	}
	var dialector gorm.Dialector
	var databaseType common.DatabaseType
	versionSQL := "SELECT VERSION()"
	dsn := os.Getenv("SQL_DSN")
	switch dialect {
	case "sqlite":
		path := os.Getenv("CM_UPGRADE_SQLITE_PATH")
		require.Contains(t, path, "cm-consistency-verification")
		dialector = sqlite.Open(path)
		databaseType = common.DatabaseTypeSQLite
		versionSQL = "SELECT sqlite_version()"
	case "mysql", "postgres":
		if dialect == "mysql" {
			config, parseErr := mysqlDriver.ParseDSN(dsn)
			require.NoError(t, parseErr)
			require.Equal(t, "new_api_cm_schema_analytics", config.DBName)
			require.True(t, strings.HasPrefix(config.Addr, "127.0.0.1:"))
			dialector = mysql.Open(dsn)
			databaseType = common.DatabaseTypeMySQL
		} else {
			parsed, parseErr := url.Parse(dsn)
			require.NoError(t, parseErr)
			require.Equal(t, "127.0.0.1", parsed.Hostname())
			require.Equal(t, "/new_api_cm_schema_analytics", parsed.Path)
			dialector = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
			databaseType = common.DatabaseTypePostgreSQL
		}
	default:
		t.Fatalf("不支持的测试数据库: %s", dialect)
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	oldDB, oldLogDB, oldRedis := model.DB, model.LOG_DB, common.RedisEnabled
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	common.SetDatabaseTypes(databaseType, databaseType)
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = oldDB, oldLogDB, oldRedis
		common.SetDatabaseTypes(oldMainType, oldLogType)
		assert.NoError(t, sqlDB.Close())
	})
	var version string
	require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
	t.Logf("database=%s version=%s", dialect, strings.TrimSpace(version))
	tables := []any{
		&model.User{}, &model.Channel{}, &model.ChannelDailyCost{},
		&model.ChannelMonitorDailyCostDetail{}, &model.ChannelMonitorDailySuccessLedger{},
		&model.ChannelMonitorDailyCheckpoint{},
	}
	require.NoError(t, db.AutoMigrate(tables...))
	for _, table := range tables {
		require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Unscoped().Where("1 = 1").Delete(table).Error)
	}
	day := model.ChannelDailyCostDayStart(common.GetTimestamp()) - 3*86400
	seedChannelMonitorAnalyticsFilterFixture(t, db, day)
	runChannelMonitorAnalyticsFilterCases(t, day)
	runChannelMonitorAnalyticsCostCoverageCases(t, db, day)
	runChannelMonitorAnalyticsSuccessCoverageCases(t, db, day)
	runChannelMonitorCostAPIKeyGroupingCases(t, db, day)
}
