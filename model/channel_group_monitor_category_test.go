package model

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// External cases require empty, disposable local databases named
// new_api_group_category_test and remove only their own fixture table.
func TestChannelGroupMonitorCategoryDatabaseCompatibility(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialector gorm.Dialector
			databaseType := common.DatabaseTypeSQLite
			versionQuery := "SELECT sqlite_version()"
			switch engine {
			case "sqlite":
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "category.db"))
			case "mysql":
				dsn := os.Getenv("GROUP_MONITOR_CATEGORY_MYSQL_DSN")
				if dsn == "" {
					t.Skip("GROUP_MONITOR_CATEGORY_MYSQL_DSN is not set")
				}
				config, err := mysqlDriver.ParseDSN(dsn)
				require.NoError(t, err)
				require.Equal(t, "new_api_group_category_test", config.DBName)
				require.Equal(t, "tcp", config.Net)
				require.True(t, strings.HasPrefix(config.Addr, "127.0.0.1:"))
				dialector, databaseType = mysql.Open(dsn), common.DatabaseTypeMySQL
				versionQuery = "SELECT version()"
			case "postgres":
				dsn := os.Getenv("GROUP_MONITOR_CATEGORY_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("GROUP_MONITOR_CATEGORY_POSTGRES_DSN is not set")
				}
				parsed, err := url.Parse(dsn)
				require.NoError(t, err)
				require.Equal(t, "127.0.0.1", parsed.Hostname())
				require.Equal(t, "/new_api_group_category_test", parsed.Path)
				dialector, databaseType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
				versionQuery = "SELECT version()"
			}
			db, err := gorm.Open(dialector, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			originalDB := DB
			originalMain, originalLog := common.MainDatabaseType(), common.LogDatabaseType()
			DB = db
			common.SetDatabaseTypes(databaseType, originalLog)
			t.Cleanup(func() {
				DB = originalDB
				common.SetDatabaseTypes(originalMain, originalLog)
				assert.NoError(t, sqlDB.Close())
			})
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database=%s version=%s", engine, version)
			require.False(t, db.Migrator().HasTable(&ChannelGroupMonitorConfig{}), "matrix database must be empty")
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&ChannelGroupMonitorConfig{})) })
			require.NoError(t, db.AutoMigrate(&ChannelGroupMonitorConfig{}))

			input := ChannelGroupMonitorConfigInput{
				Enabled:       true,
				ShowCacheRate: true,
				Categories:    []string{"空分类", "编程🚀", "未分类"},
				Groups: []ChannelGroupMonitorGroup{
					{GroupName: "vip", ProbeModel: "gpt-4.1", DisplayInitial: "V", Category: "编程🚀"},
					{GroupName: "default", ProbeModel: "gpt-4.1-mini"},
				},
				IntervalSeconds: 60, DisplayValue: 60, DisplayUnit: ChannelStatusProbeDisplayUnitMinute,
			}
			_, err = SaveChannelGroupMonitorConfig(input, 1_000)
			require.NoError(t, err)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&ChannelGroupMonitorConfig{}))
				stored, readErr := GetChannelGroupMonitorConfig()
				require.NoError(t, readErr)
				groups, parseErr := stored.Groups()
				require.NoError(t, parseErr)
				assert.Equal(t, input.Groups, groups)
				categories, parseErr := stored.Categories()
				require.NoError(t, parseErr)
				assert.Equal(t, input.Categories, categories)
				showCacheRate, parseErr := stored.ShowCacheRate()
				require.NoError(t, parseErr)
				assert.True(t, showCacheRate)
			}

			// Existing installations have the same table with category absent from JSON.
			legacyJSON := `[{"group_name":"vip","probe_model":"gpt-4.1","display_initial":"V"},{"group_name":"default","probe_model":"gpt-4.1-mini"}]`
			require.NoError(t, db.Model(&ChannelGroupMonitorConfig{}).Where("id = ?", 1).Update("groups_json", legacyJSON).Error)
			legacy, err := GetChannelGroupMonitorConfig()
			require.NoError(t, err)
			showCacheRate, err := legacy.ShowCacheRate()
			require.NoError(t, err)
			assert.False(t, showCacheRate)
			groups, err := legacy.Groups()
			require.NoError(t, err)
			require.Len(t, groups, 2)
			assert.Empty(t, groups[0].Category)
			assert.Empty(t, groups[1].Category)
			categories, err := legacy.Categories()
			require.NoError(t, err)
			assert.Equal(t, []string{"未分类"}, categories)
			groups[0].Category = "编程模型"
			input.Groups, input.Revision = groups, legacy.Revision
			input.Categories = []string{"编程模型", "未分类", "预留分类"}
			updated, err := SaveChannelGroupMonitorConfig(input, 1_100)
			require.NoError(t, err)
			assert.Equal(t, legacy.CreatedAt, updated.CreatedAt)
			assert.Equal(t, legacy.Revision+1, updated.Revision)
			stored, err := GetChannelGroupMonitorConfig()
			require.NoError(t, err)
			groups, err = stored.Groups()
			require.NoError(t, err)
			assert.Equal(t, input.Groups, groups)
			categories, err = stored.Categories()
			require.NoError(t, err)
			assert.Equal(t, input.Categories, categories)

			input.Groups[0].Category = ""
			input.ShowCacheRate = false
			input.Categories = []string{"未分类"}
			input.Revision = stored.Revision
			_, err = SaveChannelGroupMonitorConfig(input, 1_200)
			require.NoError(t, err)
			stored, err = GetChannelGroupMonitorConfig()
			require.NoError(t, err)
			groups, err = stored.Groups()
			require.NoError(t, err)
			assert.Equal(t, input.Groups, groups)

			input.Revision, input.Groups = stored.Revision, []ChannelGroupMonitorGroup{}
			showCacheRate, err = stored.ShowCacheRate()
			require.NoError(t, err)
			assert.False(t, showCacheRate)
			input.Categories = []string{"预留分类"}
			_, err = SaveChannelGroupMonitorConfig(input, 1_300)
			require.NoError(t, err)
			stored, err = GetChannelGroupMonitorConfig()
			require.NoError(t, err)
			groups, err = stored.Groups()
			require.NoError(t, err)
			assert.Empty(t, groups)
			categories, err = stored.Categories()
			require.NoError(t, err)
			assert.Equal(t, []string{"预留分类"}, categories)
			assert.Zero(t, stored.NextRunAt)
			_, err = RequestChannelGroupMonitorManualRun(1_310)
			assert.ErrorContains(t, err, "请先保存至少一个监控分组")
		})
	}
}

func TestChannelGroupMonitorCategoryConfigurationFeedsScheduledProbes(t *testing.T) {
	setupChannelGroupMonitorTestDB(t)
	wantGroups := []ChannelGroupMonitorGroup{
		{GroupName: "vip", ProbeModel: "gpt-4.1", Category: "编程模型"},
		{GroupName: "default", ProbeModel: "gpt-4.1-mini", Category: "通用模型"},
	}
	_, err := SaveChannelGroupMonitorConfig(ChannelGroupMonitorConfigInput{
		Enabled: true, Categories: []string{"预留分类", "编程模型", "通用模型"}, Groups: wantGroups,
		IntervalSeconds: 60, DisplayValue: 60, DisplayUnit: ChannelStatusProbeDisplayUnitMinute,
	}, 1_000)
	require.NoError(t, err)
	claim, err := ClaimDueChannelGroupMonitor(1_020)
	require.NoError(t, err)
	require.NotNil(t, claim)
	assert.Equal(t, wantGroups, claim.Groups)
	assert.Equal(t, ChannelGroupMonitorTriggerScheduled, claim.Trigger)
}
