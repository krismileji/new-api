package controller

import (
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestChannelMonitorSyncFailureAlertThresholdDefaults(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
	}{
		{value: "", want: 10},
		{value: "0", want: 10},
		{value: "-1", want: 10},
		{value: "101", want: 10},
		{value: "invalid", want: 10},
		{value: "1", want: 1},
		{value: "7", want: 7},
		{value: "100", want: 100},
	} {
		t.Run(test.value, func(t *testing.T) {
			settings := channelMonitorSettingsFromOptions(map[string]string{
				channelMonitorAutoUpdateConsecutiveFailureLimitOption: "0",
				channelMonitorSyncFailureAlertThresholdOption:         test.value,
			})
			assert.Equal(t, test.want, settings.SyncFailureAlertThreshold)
			assert.Zero(t, settings.AutoUpdateConsecutiveFailureLimit)
		})
	}
}

func TestChannelMonitorSyncFailureAlertSettingsDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialector gorm.Dialector
			databaseType := common.DatabaseTypeSQLite
			switch engine {
			case "mysql":
				dsn := os.Getenv("MONITOR_ALERT_MYSQL_DSN")
				if dsn == "" {
					t.Skip("需要配置专用的 MONITOR_ALERT_MYSQL_DSN")
				}
				config, err := mysqlDriver.ParseDSN(dsn)
				require.NoError(t, err)
				require.Equal(t, "new_api_monitor_alert_test", config.DBName)
				require.Equal(t, "tcp", config.Net)
				require.True(t, strings.HasPrefix(config.Addr, "127.0.0.1:"))
				dialector, databaseType = mysql.Open(dsn), common.DatabaseTypeMySQL
			case "postgres":
				dsn := os.Getenv("MONITOR_ALERT_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("需要配置专用的 MONITOR_ALERT_POSTGRES_DSN")
				}
				parsed, err := url.Parse(dsn)
				require.NoError(t, err)
				require.Equal(t, "127.0.0.1", parsed.Hostname())
				require.Equal(t, "/new_api_monitor_alert_test", parsed.Path)
				dialector = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
				databaseType = common.DatabaseTypePostgreSQL
			}
			db := setupChannelMonitorControllerTestDB(t)
			if dialector != nil {
				sqliteDB := db
				var err error
				db, err = gorm.Open(dialector, &gorm.Config{})
				require.NoError(t, err)
				sqlDB, err := db.DB()
				require.NoError(t, err)
				model.DB = db
				common.SetDatabaseTypes(databaseType, common.DatabaseTypeSQLite)
				t.Cleanup(func() {
					model.DB = sqliteDB
					common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
					assert.NoError(t, sqlDB.Close())
				})
				tables := []any{&model.Option{}, &model.User{}}
				for _, table := range tables {
					require.False(t, db.Migrator().HasTable(table), "必须使用空的专用测试数据库")
				}
				t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
				require.NoError(t, db.AutoMigrate(tables...))
			}
			versionQuery := "SELECT version()"
			if engine == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database=%s version=%s", engine, version)
			require.NoError(t, db.Create(&model.User{
				Id: 1, Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default",
			}).Error)
			usePersistedChannelMonitorOptions(t, db, map[string]string{
				channelMonitorAutoUpdateConsecutiveFailureLimitOption: "0",
			})
			settings, err := loadChannelMonitorSettings(t.Context())
			require.NoError(t, err)
			assert.Equal(t, 10, settings.SyncFailureAlertThreshold)
			wantThreshold := 10
			for _, threshold := range []float64{-1, 0, 1.5, 101, 1, 7, 100} {
				ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
					"sync_failure_alert_threshold": threshold,
				})
				UpdateChannelMonitorSettings(ctx)
				if threshold < 1 || threshold > 100 || threshold == 1.5 {
					assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
				} else {
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					var response channelMonitorSettingsAPIResponse
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
					require.True(t, response.Success, response.Message)
					wantThreshold = int(threshold)
					assert.Equal(t, wantThreshold, response.Data.SyncFailureAlertThreshold)
					var option model.Option
					require.NoError(t, db.Where(&model.Option{Key: channelMonitorSyncFailureAlertThresholdOption}).First(&option).Error)
					assert.Equal(t, strconv.Itoa(wantThreshold), option.Value)
				}
				settings, err = loadChannelMonitorSettings(t.Context())
				require.NoError(t, err)
				assert.Equal(t, wantThreshold, settings.SyncFailureAlertThreshold)
				assert.Zero(t, settings.AutoUpdateConsecutiveFailureLimit)
			}
			ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
				"auto_update_retry_count": 2,
			})
			UpdateChannelMonitorSettings(ctx)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			settings, err = loadChannelMonitorSettings(t.Context())
			require.NoError(t, err)
			assert.Equal(t, 100, settings.SyncFailureAlertThreshold, "更新其他配置不能覆盖告警次数")
		})
	}
}
