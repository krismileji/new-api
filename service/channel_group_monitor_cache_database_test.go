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
			require.False(t, db.Migrator().HasTable(&model.ChannelMonitorDailyCheckpoint{}))
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&model.ChannelMonitorDailyCheckpoint{})) })
			for _, table := range []any{&model.ChannelMonitorDailySuccessMinute{}, &model.Option{}} {
				require.False(t, db.Migrator().HasTable(table), "matrix database must be empty")
				t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(table)) })
				require.NoError(t, db.AutoMigrate(table))
			}
			require.NoError(t, db.AutoMigrate(&model.ChannelMonitorDailySuccessLedger{}, &model.ChannelMonitorDailyCheckpoint{}))

			_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
			previousDB, previousRead, previousEnabled := model.DB, common.RDBMonitorRead, common.RedisEnabled
			previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
			switch engine {
			case "sqlite":
				common.SetDatabaseTypes(common.DatabaseTypeSQLite, previousLogType)
			case "mysql":
				common.SetDatabaseTypes(common.DatabaseTypeMySQL, previousLogType)
			case "postgres":
				common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, previousLogType)
			}
			model.DB, common.RDBMonitorRead, common.RedisEnabled = db, client, false
			t.Cleanup(func() {
				model.DB, common.RDBMonitorRead, common.RedisEnabled = previousDB, previousRead, previousEnabled
				common.SetDatabaseTypes(previousMainType, previousLogType)
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

			t.Run("frozen cache decisions survive persistence restore and threshold changes", func(t *testing.T) {
				ctx := context.Background()
				common.RedisEnabled = false
				day := today - 2*daySeconds
				hit := newChannelMonitorRedisSharedProjectionTestEvent("context-history-hit", day+60)
				hit.GroupName, hit.IsStream, hit.EventSequence = "context-vip", true, 1001
				hit.InputTokens, hit.CacheReadTokens = common.GetPointer(int64(10000)), common.GetPointer(int64(5000))
				miss := hit
				miss.EventId, miss.EventSequence, miss.InputTokens, miss.CacheReadTokens = "context-history-miss", 1002, common.GetPointer(int64(20000)), common.GetPointer(int64(0))
				small := hit
				small.EventId, small.EventSequence, small.InputTokens = "context-history-small", 1003, common.GetPointer(int64(9999))
				nonStream := hit
				nonStream.EventId, nonStream.EventSequence, nonStream.IsStream = "context-history-non-stream", 1004, false
				small.GroupCacheExcluded, nonStream.GroupCacheExcluded = common.GetPointer(true), common.GetPointer(true)
				require.NoError(t, projection.WriteChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{hit, miss, small, nonStream}))
				for range 2 {
					require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
					require.NoError(t, db.AutoMigrate(&model.ChannelMonitorDailySuccessLedger{}, &model.ChannelMonitorDailyCheckpoint{}))
				}
				// Legacy rows retain the original counts; a new threshold never rewrites them.
				legacy := (model.ChannelMonitorDailyMetricIdentity{ChannelID: 98, Group: "context-vip"}).LedgerRow(day)
				legacy.CacheHitCount, legacy.CacheSampleCount = 50, 50
				require.NoError(t, db.Create(&legacy).Error)
				require.NoError(t, client.Del(ctx, ChannelMonitorRedisSuccessDayKey(day)).Err())
				require.NoError(t, rebuildChannelMonitorDailyMetrics(ctx, client, day))
				late := hit
				late.EventId, late.EventSequence = "context-history-late", 1005
				require.NoError(t, projection.WriteChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{hit, late}))
				require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
				todayMiss := miss
				todayMiss.EventId, todayMiss.EventSequence, todayMiss.OccurredAt = "context-today-miss", 1006, now
				require.NoError(t, projection.WriteChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{todayMiss}))
				require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, today))
				common.RedisEnabled = true
				for _, tc := range []struct {
					min  int
					want map[string]float64
				}{
					{10, map[string]float64{"context-vip": 52.0 / 54 * 100}},
					{11, map[string]float64{"context-vip": 52.0 / 54 * 100}},
					{21, map[string]float64{"context-vip": 52.0 / 54 * 100}},
				} {
					previousPolicy := channelGroupMonitorCachePolicyState.Load()
					channelGroupMonitorCachePolicyState.Store(&channelGroupMonitorCachePolicy{MinContextK: tc.min})
					t.Cleanup(func() { channelGroupMonitorCachePolicyState.Store(previousPolicy) })
					rates, err := GetChannelGroupMonitorCacheRates(ctx, []string{"context-vip"}, day, now+1)
					require.NoError(t, err)
					assert.Equal(t, tc.want, rates)
				}
			})
			t.Run("retention cleans all daily tables and prevents replay resurrection", func(t *testing.T) {
				ctx := t.Context()
				cutoff, err := model.GetChannelMonitorDailyRetentionCutoff(ctx, now)
				require.NoError(t, err)
				require.Equal(t, today-29*daySeconds, cutoff)
				for _, day := range []int64{cutoff - daySeconds, cutoff} {
					require.NoError(t, db.Create(&model.ChannelMonitorDailySuccessMinute{
						DayStart: day, MinuteStart: day + 60, ChannelId: 99,
						UserAttribution: string(model.ChannelMonitorEventUserAttributionUnknown),
					}).Error)
					require.NoError(t, db.Create(&model.ChannelMonitorDailyCheckpoint{DayStart: day, Revision: 1}).Error)
				}
				result, err := model.DeleteChannelMonitorDailyMetricsBefore(ctx, cutoff, 1, model.ChannelMonitorCleanupBudget{})
				require.NoError(t, err)
				assert.Equal(t, int64(1), result.DailyMetricRowsDeleted)
				assert.Equal(t, int64(1), result.DailyMinuteRowsDeleted)
				assert.Equal(t, int64(1), result.DailyCheckpointRowsDeleted)
				assert.False(t, result.Incomplete)
				for _, table := range []any{&model.ChannelMonitorDailySuccessLedger{}, &model.ChannelMonitorDailySuccessMinute{}, &model.ChannelMonitorDailyCheckpoint{}} {
					var expired, boundary int64
					require.NoError(t, db.Model(table).Where("day_start < ?", cutoff).Count(&expired).Error)
					require.NoError(t, db.Model(table).Where("day_start = ?", cutoff).Count(&boundary).Error)
					assert.Zero(t, expired)
					assert.Positive(t, boundary, "the boundary day must survive")
				}
				repeated, err := model.DeleteChannelMonitorDailyMetricsBefore(ctx, cutoff, 1, model.ChannelMonitorCleanupBudget{})
				require.NoError(t, err)
				assert.Equal(t, model.ChannelMonitorDailyRetentionResult{}, repeated)
				require.NoError(t, db.Create(&model.Option{Key: model.ChannelMonitorDailyMetricRetentionDaysOption, Value: "2"}).Error)
				cutoff, err = model.GetChannelMonitorDailyRetentionCutoff(ctx, now)
				require.NoError(t, err)
				assert.Equal(t, today-daySeconds, cutoff)
				_, err = model.DeleteChannelMonitorDailyMetricsBefore(ctx, cutoff, 2, model.ChannelMonitorCleanupBudget{})
				require.NoError(t, err)
				previousConsumer := common.RDBMonitorConsumer
				common.RDBMonitorConsumer = client
				t.Cleanup(func() { common.RDBMonitorConsumer = previousConsumer })
				oldDay := today - 2*daySeconds
				require.NoError(t, client.SAdd(ctx, channelMonitorDailyDirtyDaysKey, oldDay).Err())
				require.NoError(t, runChannelMonitorDailyPersistence(ctx, now))
				for _, table := range []any{&model.ChannelMonitorDailySuccessLedger{}, &model.ChannelMonitorDailyCheckpoint{}} {
					var expired int64
					require.NoError(t, db.Model(table).Where("day_start < ?", cutoff).Count(&expired).Error)
					assert.Zero(t, expired, "expired Redis snapshots cannot recreate database history")
				}
				assert.False(t, client.SIsMember(ctx, channelMonitorDailyDirtyDaysKey, oldDay).Val())
				require.NoError(t, db.Create(&model.Option{Key: "ChannelMonitorCleanupEnabled", Value: "false"}).Error)
				cutoff, err = model.GetChannelMonitorDailyRetentionCutoff(ctx, now)
				require.NoError(t, err)
				assert.Zero(t, cutoff, "disabling cleanup also disables the persistence retention limit")
			})

		})
	}
}
