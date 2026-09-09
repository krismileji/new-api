package model

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// External cases require disposable local databases named
// new_api_schedule_snapshot_test; each case removes its fixture tables.
func setupChannelSmartScheduleMonitorSnapshotMatrixDB(t *testing.T, engine string, db *gorm.DB) *gorm.DB {
	t.Helper()
	versionQuery := "SELECT sqlite_version()"
	if engine != "sqlite" {
		var dialector gorm.Dialector
		var databaseType common.DatabaseType
		if engine == "mysql" {
			dsn := os.Getenv("SCHEDULE_SNAPSHOT_MYSQL_DSN")
			if dsn == "" {
				t.Skip("SCHEDULE_SNAPSHOT_MYSQL_DSN is not set")
			}
			config, err := mysqlDriver.ParseDSN(dsn)
			require.NoError(t, err)
			require.Equal(t, "new_api_schedule_snapshot_test", config.DBName)
			require.Equal(t, "tcp", config.Net)
			require.True(t, strings.HasPrefix(config.Addr, "127.0.0.1:"))
			dialector, databaseType = mysql.Open(dsn), common.DatabaseTypeMySQL
		} else {
			dsn := os.Getenv("SCHEDULE_SNAPSHOT_POSTGRES_DSN")
			if dsn == "" {
				t.Skip("SCHEDULE_SNAPSHOT_POSTGRES_DSN is not set")
			}
			parsed, err := url.Parse(dsn)
			require.NoError(t, err)
			require.Equal(t, "127.0.0.1", parsed.Hostname())
			require.Equal(t, "/new_api_schedule_snapshot_test", parsed.Path)
			dialector = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
			databaseType = common.DatabaseTypePostgreSQL
		}
		var err error
		db, err = gorm.Open(dialector, &gorm.Config{})
		require.NoError(t, err)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		originalDB, originalLogDB := DB, LOG_DB
		originalMain, originalLog := common.MainDatabaseType(), common.LogDatabaseType()
		DB, LOG_DB = db, db
		common.SetDatabaseTypes(databaseType, databaseType)
		initCol()
		t.Cleanup(func() {
			DB, LOG_DB = originalDB, originalLogDB
			common.SetDatabaseTypes(originalMain, originalLog)
			initCol()
			assert.NoError(t, sqlDB.Close())
		})
		tables := []any{
			&Option{}, &Channel{}, &Ability{}, &ChannelRatioMonitor{},
			&ChannelSmartScheduleRouteState{}, &ChannelSmartScheduleGroupPause{},
			&ChannelSmartScheduleModelSampleState{},
		}
		for _, table := range tables {
			require.False(t, db.Migrator().HasTable(table), "matrix database must be empty")
		}
		t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
		require.NoError(t, db.AutoMigrate(tables...))
		versionQuery = "SELECT version()"
	}
	var version string
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("database=%s version=%s", engine, version)
	return db
}
