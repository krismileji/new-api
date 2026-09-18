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
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// External cases require empty, disposable local databases named
// new_api_group_cache_test and remove only the fixture table they create.
func TestChannelGroupMonitorCacheRatesDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialector gorm.Dialector
			versionQuery := "SELECT version()"
			switch engine {
			case "sqlite":
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "group-cache.db"))
				versionQuery = "SELECT sqlite_version()"
			case "mysql":
				dsn := os.Getenv("GROUP_MONITOR_CACHE_MYSQL_DSN")
				if dsn == "" {
					t.Skip("GROUP_MONITOR_CACHE_MYSQL_DSN is not set")
				}
				config, err := mysqlDriver.ParseDSN(dsn)
				require.NoError(t, err)
				require.Equal(t, "new_api_group_cache_test", config.DBName)
				require.Equal(t, "tcp", config.Net)
				require.True(t, strings.HasPrefix(config.Addr, "127.0.0.1:"))
				dialector = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("GROUP_MONITOR_CACHE_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("GROUP_MONITOR_CACHE_POSTGRES_DSN is not set")
				}
				parsed, err := url.Parse(dsn)
				require.NoError(t, err)
				require.Equal(t, "127.0.0.1", parsed.Hostname())
				require.Equal(t, "/new_api_group_cache_test", parsed.Path)
				dialector = postgres.Open(dsn)
			}
			db, err := gorm.Open(dialector, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database=%s version=%s", engine, version)
			require.False(t, db.Migrator().HasTable(&model.ChannelMonitorDailySuccessLedger{}), "matrix database must be empty")
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&model.ChannelMonitorDailySuccessLedger{})) })
			require.NoError(t, db.AutoMigrate(&model.ChannelMonitorDailySuccessLedger{}))

			_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
			previousDB, previousRead, previousEnabled := model.DB, common.RDBMonitorRead, common.RedisEnabled
			model.DB, common.RDBMonitorRead, common.RedisEnabled = db, client, false
			t.Cleanup(func() {
				model.DB, common.RDBMonitorRead, common.RedisEnabled = previousDB, previousRead, previousEnabled
			})
			const daySeconds = int64(24 * 60 * 60)
			today := model.ChannelDailyCostDayStart(1_750_032_000)
			now := today + 10*60*60 + 37*60 + 25
			var rows []model.ChannelMonitorDailySuccessLedger
			for index, fixture := range []struct {
				daysAgo int64
				group   string
				hits    int64
				samples int64
			}{
				{30, "vip", 100, 100}, // Outside even the longest display window.
				{29, "vip", 1, 1},
				{7, "vip", 0, 4},
				{6, "vip", 2, 3},
				{1, "vip", 0, 2},
				{1, "vip", 1, 1}, // Another channel in the same group.
				{1, "zero", 0, 1},
				{1, "unknown", 0, 0},
				{1, "private", 99, 99},
				{6, "historical", 1, 1},
				{0, "vip", 50, 50}, // Today's stored snapshot must not be added again.
			} {
				rows = append(rows, model.ChannelMonitorDailySuccessLedger{
					DayStart: today - fixture.daysAgo*daySeconds, ChannelId: index + 1,
					GroupName: fixture.group, GroupKey: fixture.group,
					UserAttribution: string(model.ChannelMonitorEventUserAttributionUnknown),
					CacheHitCount:   fixture.hits, CacheSampleCount: fixture.samples,
				})
			}
			require.NoError(t, db.Create(&rows).Error)
			projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
			hit := newChannelMonitorRedisSharedProjectionTestEvent("today-hit", today)
			hit.InputTokens, hit.CacheReadTokens = common.GetPointer(int64(100)), common.GetPointer(int64(20))
			miss := newChannelMonitorRedisSharedProjectionTestEvent("today-miss", now)
			miss.InputTokens, miss.CacheReadTokens = common.GetPointer(int64(100)), common.GetPointer(int64(0))
			previousDay := hit
			previousDay.EventId, previousDay.OccurredAt = "previous-day-already-persisted", today-60
			require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{hit, miss, previousDay}))
			common.RedisEnabled = true
			for _, tc := range []struct {
				name string
				days int64
				want map[string]float64
			}{
				{"one day uses realtime only", 1, map[string]float64{"vip": 50}},
				{"two days combine sample counts", 2, map[string]float64{"vip": 40, "zero": 0}},
				{"seven days include first day and historical-only groups", 7, map[string]float64{"vip": 50, "zero": 0, "historical": 100}},
				{"thirty days exclude older history", 30, map[string]float64{"vip": 500.0 / 13, "zero": 0, "historical": 100}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					rates, err := GetChannelGroupMonitorCacheRates(context.Background(), []string{"vip", "zero", "unknown", "empty", "historical"}, today-(tc.days-1)*daySeconds, now+1)
					require.NoError(t, err)
					require.Len(t, rates, len(tc.want))
					for group, expected := range tc.want {
						require.Contains(t, rates, group)
						assert.InDelta(t, expected, rates[group], 0.000001)
					}
				})
			}
		})
	}
}
