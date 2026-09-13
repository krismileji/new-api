package service

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// All engines exercise the same immediate-delivery, retry, and ledger contracts
// on disposable local databases. Redis DBs must be empty before each case.
func TestChannelDailyCostStreamProjectionDatabaseMatrix(t *testing.T) {
	redisAddr := os.Getenv("TEST_COST_PROJECTION_REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("设置 TEST_COST_PROJECTION_REDIS_ADDR 运行成本实时统计真实数据库矩阵")
	}
	require.Equal(t, "127.0.0.1:26382", redisAddr)
	t.Setenv("CHANNEL_DAILY_COST_RELIABLE_OUTBOX", "true")
	for index, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialector gorm.Dialector
			versionQuery := "SELECT version()"
			switch engine {
			case "sqlite":
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "cost-projection.db"))
				versionQuery = "SELECT sqlite_version()"
			case "mysql":
				dsn := os.Getenv("TEST_COST_PROJECTION_MYSQL_DSN")
				require.NotEmpty(t, dsn)
				config, err := mysqlDriver.ParseDSN(dsn)
				require.NoError(t, err)
				require.Equal(t, "new_api_cost_projection_test", config.DBName)
				require.Equal(t, "tcp", config.Net)
				require.True(t, strings.HasPrefix(config.Addr, "127.0.0.1:"))
				dialector = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_COST_PROJECTION_POSTGRES_DSN")
				require.NotEmpty(t, dsn)
				parsed, err := url.Parse(dsn)
				require.NoError(t, err)
				require.Equal(t, "127.0.0.1", parsed.Hostname())
				require.Equal(t, "/new_api_cost_projection_test", parsed.Path)
				dialector = postgres.Open(dsn)
			}
			db, err := gorm.Open(dialector, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
			tables := []any{&model.ChannelDailyCost{}, &model.ChannelDailyAPIKeyCost{}, &model.ChannelDailyCostOutbox{}, &model.ChannelTaskCostEvent{}, &model.ChannelMonitorDailyCostDetail{}}
			for _, table := range tables {
				require.False(t, db.Migrator().HasTable(table), "验证数据库必须为空")
			}
			t.Cleanup(func() {
				for i := len(tables) - 1; i >= 0; i-- {
					assert.NoError(t, db.Migrator().DropTable(tables[i]))
				}
			})
			require.NoError(t, db.AutoMigrate(tables...))
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database=%s version=%s", engine, version)

			client := redis.NewClient(&redis.Options{Addr: redisAddr, DB: index + 1})
			t.Cleanup(func() { assert.NoError(t, client.Close()) })
			size, err := client.DBSize(context.Background()).Result()
			require.NoError(t, err)
			require.Zero(t, size, "只能使用空的独立 Redis 测试数据库")
			t.Cleanup(func() { assert.NoError(t, client.FlushDB(context.Background()).Err()) })
			previousDB, previousType := model.DB, common.MainDatabaseType()
			previousRedisEnabled := common.RedisEnabled
			previousRDB, previousWrite, previousRead, previousConsumer := common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer
			previousStats := GetChannelDailyCostReliableStats()
			model.DB = db
			common.SetMainDatabaseType(common.DatabaseType(engine))
			common.RedisEnabled = true
			common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer = client, client, client, client
			setCM07ChannelDailyCostReliableStats(ChannelDailyCostReliableStats{})
			t.Cleanup(func() {
				model.DB = previousDB
				common.SetMainDatabaseType(previousType)
				common.RedisEnabled = previousRedisEnabled
				common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer = previousRDB, previousWrite, previousRead, previousConsumer
				setCM07ChannelDailyCostReliableStats(previousStats)
			})
			verifyChannelDailyCostStreamProjection(t, db, client)
		})
	}
}
