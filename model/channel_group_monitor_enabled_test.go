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

func TestChannelGroupMonitorEnabledGroupsReadsLegacyConfiguration(t *testing.T) {
	for _, raw := range []string{
		`[{"group_name":"default","probe_model":"gpt-4.1"},{"group_name":"vip","probe_model":"gpt-4.1","enabled":false}]`,
		`{"categories":["通用模型"],"groups":[{"group_name":"default","probe_model":"gpt-4.1","enabled":true},{"group_name":"vip","probe_model":"gpt-4.1","enabled":false}]}`,
	} {
		config := ChannelGroupMonitorConfig{GroupsJSON: raw}
		groups, err := config.Groups()
		require.NoError(t, err)
		require.Len(t, groups, 2)
		enabled, err := config.EnabledGroups()
		require.NoError(t, err)
		assert.Equal(t, groups[:1], enabled)
	}
}

// External cases use empty, disposable local databases and clean up only the
// three monitor tables they created.
func setupChannelGroupMonitorEnabledDatabase(t *testing.T, engine string) *gorm.DB {
	t.Helper()
	var dialector gorm.Dialector
	databaseType := common.DatabaseTypeSQLite
	versionQuery := "SELECT sqlite_version()"
	switch engine {
	case "sqlite":
		dialector = sqlite.Open(filepath.Join(t.TempDir(), "group-enabled.db"))
	case "mysql":
		dsn := os.Getenv("GROUP_MONITOR_ENABLED_MYSQL_DSN")
		if dsn == "" {
			t.Skip("GROUP_MONITOR_ENABLED_MYSQL_DSN is not set")
		}
		config, err := mysqlDriver.ParseDSN(dsn)
		require.NoError(t, err)
		require.Equal(t, "new_api_group_enabled_test", config.DBName)
		require.Equal(t, "tcp", config.Net)
		require.True(t, strings.HasPrefix(config.Addr, "127.0.0.1:"))
		dialector, databaseType = mysql.Open(dsn), common.DatabaseTypeMySQL
		versionQuery = "SELECT version()"
	case "postgres":
		dsn := os.Getenv("GROUP_MONITOR_ENABLED_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("GROUP_MONITOR_ENABLED_POSTGRES_DSN is not set")
		}
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		require.Equal(t, "127.0.0.1", parsed.Hostname())
		require.Equal(t, "/new_api_group_enabled_test", parsed.Path)
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
	for _, table := range []any{&ChannelGroupMonitorConfig{}, &ChannelGroupMonitorState{}, &ChannelGroupMonitorExecution{}} {
		require.False(t, db.Migrator().HasTable(table), "matrix database must be empty")
		t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(table)) })
		require.NoError(t, db.AutoMigrate(table))
	}
	return db
}

func TestChannelGroupMonitorEnabledDatabaseCompatibility(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelGroupMonitorEnabledDatabase(t, engine)
			input := ChannelGroupMonitorConfigInput{
				Enabled: true, Categories: []string{"通用模型", "预留分类"},
				Groups: []ChannelGroupMonitorGroup{
					{GroupName: "vip", ProbeModel: "gpt-4.1", DisplayInitial: "贵", Category: "通用模型", Enabled: common.GetPointer(false)},
					{GroupName: "default", ProbeModel: "gpt-4.1-mini", DisplayInitial: "D", Category: "通用模型", Enabled: common.GetPointer(false)},
				},
				IntervalSeconds: 60, DisplayValue: 60, DisplayUnit: ChannelStatusProbeDisplayUnitMinute,
			}
			created, err := SaveChannelGroupMonitorConfig(input, 1_000)
			require.NoError(t, err)
			assert.Zero(t, created.NextRunAt, "an initially paused configuration must not schedule probes")

			// A missing enabled flag retains the legacy default of enabled.
			input.Groups[1].Enabled = nil
			input.Revision = created.Revision
			saved, err := SaveChannelGroupMonitorConfig(input, 1_001)
			require.NoError(t, err)
			assert.EqualValues(t, 1_020, saved.NextRunAt)
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
			}
			historical := ChannelGroupMonitorExecution{
				RunId: "previous-run", GroupName: "vip", ProbeModel: "gpt-4.1",
				Result: ChannelGroupMonitorResultSuccess, StartedAt: 900, FinishedAt: 901,
			}
			_, err = SaveChannelGroupMonitorExecution(&historical)
			require.NoError(t, err)
			claim, err := ClaimDueChannelGroupMonitor(1_020)
			require.NoError(t, err)
			require.NotNil(t, claim)
			assert.Equal(t, input.Groups[1:], claim.Groups, "scheduled runs must skip paused groups")
			assert.Equal(t, ChannelGroupMonitorTriggerScheduled, claim.Trigger)
			timedOut, err := TimeoutOverdueChannelGroupMonitor(1_080, 0)
			require.NoError(t, err)
			assert.Equal(t, 1, timedOut)
			var executions []ChannelGroupMonitorExecution
			require.NoError(t, db.Where("run_id = ?", claim.RunId).Find(&executions).Error)
			require.Len(t, executions, 1)
			assert.Equal(t, "default", executions[0].GroupName, "paused groups must not receive synthetic timeout results")

			_, err = RequestChannelGroupMonitorManualRun(1_081)
			require.NoError(t, err)
			manual, err := ClaimDueChannelGroupMonitor(1_081)
			require.NoError(t, err)
			require.NotNil(t, manual)
			assert.Equal(t, ChannelGroupMonitorTriggerManual, manual.Trigger)
			assert.Equal(t, input.Groups[1:], manual.Groups, "manual runs must skip paused groups")
			require.NoError(t, CompleteChannelGroupMonitorClaim(*manual, 1_082))
			_, err = RequestChannelGroupMonitorManualRun(1_083)
			require.NoError(t, err)

			input.Groups[1].Enabled = common.GetPointer(false)
			input.Revision = saved.Revision
			paused, err := SaveChannelGroupMonitorConfig(input, 1_084)
			require.NoError(t, err)
			assert.Zero(t, paused.NextRunAt)
			assert.Empty(t, paused.ManualRequestId, "pausing all groups must clear queued manual work")
			assert.Zero(t, paused.ManualRequestedAt)
			_, err = RequestChannelGroupMonitorManualRun(1_085)
			assert.EqualError(t, err, "请先保存并启用至少一个监控分组")
			claim, err = ClaimDueChannelGroupMonitor(1_085)
			require.NoError(t, err)
			assert.Nil(t, claim)

			input.Groups[0].Enabled = common.GetPointer(true)
			input.Revision = paused.Revision
			resumed, err := SaveChannelGroupMonitorConfig(input, 1_086)
			require.NoError(t, err)
			assert.EqualValues(t, 1_140, resumed.NextRunAt)
			groups, err := resumed.Groups()
			require.NoError(t, err)
			assert.Equal(t, input.Groups, groups, "resuming must retain models, categories, display initials, and order")
			claim, err = ClaimDueChannelGroupMonitor(1_140)
			require.NoError(t, err)
			require.NotNil(t, claim)
			assert.Equal(t, input.Groups[:1], claim.Groups)
			var state ChannelGroupMonitorState
			require.NoError(t, db.Where("group_name = ?", "vip").First(&state).Error)
			assert.Equal(t, historical.Id, state.ExecutionId)
			assert.Equal(t, ChannelGroupMonitorResultSuccess, state.Result)
			var retained ChannelGroupMonitorExecution
			require.NoError(t, db.First(&retained, historical.Id).Error)
			assert.Equal(t, historical, retained, "pausing and resuming must preserve history")
		})
	}
}
