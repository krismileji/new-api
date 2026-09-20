package model

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/glebarez/sqlite"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// External cases use empty, disposable local databases named
// new_api_group_descriptions_test and remove only the fixture options table.
func TestGroupDescriptionsOptionPersistence(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialector gorm.Dialector
			versionQuery := "SELECT sqlite_version()"
			switch engine {
			case "sqlite":
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "options.db"))
			case "mysql":
				dsn := os.Getenv("GROUP_DESCRIPTIONS_MYSQL_DSN")
				if dsn == "" {
					t.Skip("GROUP_DESCRIPTIONS_MYSQL_DSN is not set")
				}
				config, err := mysqlDriver.ParseDSN(dsn)
				require.NoError(t, err)
				require.Equal(t, "new_api_group_descriptions_test", config.DBName)
				require.Equal(t, "tcp", config.Net)
				require.True(t, strings.HasPrefix(config.Addr, "127.0.0.1:"))
				dialector = mysql.Open(dsn)
				versionQuery = "SELECT version()"
			case "postgres":
				dsn := os.Getenv("GROUP_DESCRIPTIONS_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("GROUP_DESCRIPTIONS_POSTGRES_DSN is not set")
				}
				parsed, err := url.Parse(dsn)
				require.NoError(t, err)
				require.Equal(t, "127.0.0.1", parsed.Hostname())
				require.Equal(t, "/new_api_group_descriptions_test", parsed.Path)
				dialector = postgres.Open(dsn)
				versionQuery = "SELECT version()"
			}

			db, err := gorm.Open(dialector, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			originalDB := DB
			originalUsableGroups := setting.UserUsableGroups2JSONString()
			common.OptionMapRWMutex.Lock()
			originalOptions := common.OptionMap
			common.OptionMap = make(map[string]string)
			common.OptionMapRWMutex.Unlock()
			DB = db
			t.Cleanup(func() {
				DB = originalDB
				common.OptionMapRWMutex.Lock()
				common.OptionMap = originalOptions
				common.OptionMapRWMutex.Unlock()
				assert.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
				assert.NoError(t, sqlDB.Close())
			})

			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database=%s version=%s", engine, version)
			require.False(t, db.Migrator().HasTable(&Option{}), "matrix database must be empty")
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&Option{})) })
			require.NoError(t, db.AutoMigrate(&Option{}))

			// Start with the legacy selectable-group format, then persist the
			// independent descriptions before making the group unselectable.
			const descriptions = `{"default":"默认分组，含\"说明\"🚀","vip":""}`
			require.NoError(t, UpdateOption("UserUsableGroups", descriptions))
			require.NoError(t, UpdateOption("GroupDescriptions", descriptions))
			require.NoError(t, UpdateOption("UserUsableGroups", `{"vip":""}`))

			for range 2 {
				common.OptionMapRWMutex.Lock()
				common.OptionMap = make(map[string]string)
				common.OptionMapRWMutex.Unlock()
				require.NoError(t, setting.UpdateUserUsableGroupsByJSONString("{}"))
				loadOptionsFromDatabase()
				common.OptionMapRWMutex.RLock()
				storedDescriptions := common.OptionMap["GroupDescriptions"]
				common.OptionMapRWMutex.RUnlock()
				assert.JSONEq(t, descriptions, storedDescriptions)
				assert.Equal(t, map[string]string{"vip": ""}, setting.GetUserUsableGroupsCopy())
			}

			// Re-enabling restores the public description; an explicit clear
			// updates the existing option instead of retaining the previous text.
			require.NoError(t, UpdateOption("UserUsableGroups", descriptions))
			assert.Equal(t, "默认分组，含\"说明\"🚀", setting.GetUsableGroupDescription("default"))
			require.NoError(t, UpdateOption("GroupDescriptions", `{"default":"","vip":""}`))
			options, err := AllOption()
			require.NoError(t, err)
			require.Len(t, options, 2)
			stored := make(map[string]string, len(options))
			for _, option := range options {
				stored[option.Key] = option.Value
			}
			assert.JSONEq(t, `{"default":"","vip":""}`, stored["GroupDescriptions"])
			assert.JSONEq(t, descriptions, stored["UserUsableGroups"])
		})
	}
}
