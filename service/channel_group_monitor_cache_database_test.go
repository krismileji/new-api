package service

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
			t.Run("logical probe and outbox commit atomically", func(t *testing.T) {
				for _, table := range []any{&model.ChannelGroupMonitorExecution{}, &model.ChannelGroupMonitorState{}, &model.ChannelMonitorEventOutbox{}} {
					require.False(t, db.Migrator().HasTable(table))
					require.NoError(t, db.AutoMigrate(table))
					t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(table)) })
				}
				oldWrite, oldEnabled, oldPolicy := common.RDBMonitorWrite, common.RedisEnabled, channelGroupMonitorCachePolicyState.Load()
				common.RDBMonitorWrite, common.RedisEnabled = client, true
				channelGroupMonitorCachePolicyState.Store(nil)
				t.Cleanup(func() {
					common.RDBMonitorWrite, common.RedisEnabled = oldWrite, oldEnabled
					channelGroupMonitorCachePolicyState.Store(oldPolicy)
				})
				config := model.ChannelGroupMonitorConfig{Revision: 1, Enabled: true, DisplayValue: 60, DisplayUnit: "minute", GroupsJSON: `{"groups":[{"group_name":"vip","probe_model":"gpt-test"}]}`}
				require.NoError(t, UpdateChannelGroupMonitorCachePolicy(config))
				generation, err := SyncChannelGroupMonitorGeneration(t.Context(), config)
				require.NoError(t, err)
				now := generation.StartedAt
				probe := model.ChannelGroupMonitorExecution{RunId: "committed-probe", GroupName: "vip", ProbeModel: "gpt-test", ConfigRevision: 1, StartedAt: now, FinishedAt: now, Result: model.ChannelGroupMonitorResultSuccess, ErrorMessage: "admin-only"}
				created, err := model.SaveChannelGroupMonitorExecution(&probe)
				require.NoError(t, err)
				assert.True(t, created)
				created, err = model.SaveChannelGroupMonitorExecution(&probe)
				require.NoError(t, err)
				assert.False(t, created)
				var rows []model.ChannelMonitorEventOutbox
				require.NoError(t, db.Find(&rows).Error)
				require.Len(t, rows, 1)
				event, err := model.UnmarshalChannelMonitorEvent([]byte(rows[0].Payload))
				require.NoError(t, err)
				assert.NotContains(t, rows[0].Payload, "admin-only")
				require.NoError(t, ProjectChannelGroupMonitorEvents(t.Context(), client, []model.ChannelMonitorEvent{event, event}, now))
				groups, err := ReadChannelGroupMonitorProjection(t.Context(), generation, now)
				require.NoError(t, err)
				require.NotNil(t, groups["vip"].State)
				assert.Equal(t, model.ChannelGroupMonitorResultSuccess, groups["vip"].State.Result)
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("group_test:outbox_failure", func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "channel_monitor_event_outboxes" {
						tx.AddError(errors.New("outbox unavailable"))
					}
				}))
				t.Cleanup(func() { assert.NoError(t, db.Callback().Create().Remove("group_test:outbox_failure")) })
				probe.Id = 0
				probe.RunId = "rolled-back-probe"
				_, err = model.SaveChannelGroupMonitorExecution(&probe)
				require.ErrorContains(t, err, "outbox unavailable")
				var count int64
				require.NoError(t, db.Model(&model.ChannelGroupMonitorExecution{}).Where("run_id = ?", probe.RunId).Count(&count).Error)
				assert.Zero(t, count, "execution must roll back when its event cannot commit")
			})
			const daySeconds = int64(24 * 60 * 60)
			today := model.ChannelDailyCostDayStart(1_750_032_000)
			now := today + 10*60*60 + 37*60 + 25
			var rows []model.ChannelMonitorDailySuccessLedger
			for index, fixture := range []struct {
				daysAgo int64
				group   string
				read    int64
				input   int64
			}{
				{30, "vip", 9000, 10000}, // Outside even the longest display window.
				{29, "vip", 300, 1000},
				{7, "vip", 0, 4000},
				{6, "vip", 1200, 3000},
				{1, "vip", 0, 2000},
				{1, "vip", 800, 1000}, // Another channel in the same group.
				{1, "zero", 0, 1000},
				{1, "unknown", 0, 0},
				{1, "private", 9900, 9900},
				{6, "historical", 500, 1000},
				{0, "vip", 5000, 5000}, // Today's stored snapshot must not be added again.
			} {
				aggregate, err := common.Marshal(ChannelMonitorRedisSharedAggregate{
					GroupCacheReadTokens: fixture.read, GroupCacheInputTokens: fixture.input,
				})
				require.NoError(t, err)
				rows = append(rows, model.ChannelMonitorDailySuccessLedger{
					DayStart: today - fixture.daysAgo*daySeconds, ChannelId: index + 1,
					GroupName: fixture.group, GroupKey: fixture.group,
					UserAttribution: string(model.ChannelMonitorEventUserAttributionUnknown),
					CacheReadTokens: fixture.read, InputTokens: fixture.input,
					AggregateJSON: string(aggregate),
				})
			}
			require.NoError(t, db.Create(&rows).Error)
			projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
			hit := newChannelMonitorRedisSharedProjectionTestEvent("today-hit", today)
			hit.IsStream = true
			hit.InputTokens, hit.CacheReadTokens = common.GetPointer(int64(1000)), common.GetPointer(int64(600))
			miss := newChannelMonitorRedisSharedProjectionTestEvent("today-miss", now)
			miss.IsStream = true
			miss.InputTokens, miss.CacheReadTokens = common.GetPointer(int64(2000)), common.GetPointer(int64(0))
			previousDay := hit
			previousDay.EventId, previousDay.OccurredAt = "previous-day-already-persisted", today-60
			require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{hit, miss, previousDay}))
			common.RedisEnabled = true
			for _, tc := range []struct {
				name string
				days int64
				want map[string]float64
			}{
				{"one day uses realtime only", 1, map[string]float64{"vip": 20}},
				{"two days combine token totals", 2, map[string]float64{"vip": 1400.0 / 6000 * 100, "zero": 0}},
				{"seven days include first day and historical-only groups", 7, map[string]float64{"vip": 2600.0 / 9000 * 100, "zero": 0, "historical": 50}},
				{"thirty days exclude older history", 30, map[string]float64{"vip": 2900.0 / 14000 * 100, "zero": 0, "historical": 50}},
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

			t.Run("per-key rates combine days and routes before equal-weight averaging", func(t *testing.T) {
				ctx := t.Context()
				for index, fixture := range []struct {
					key         int
					daysAgo     int64
					read, input int64
				}{
					{101, 2, 200, 1000},
					{101, 1, 600, 1000},
					{102, 1, 50, 100},
					{0, 1, 1000, 1000}, // Unknown keys affect only the legacy weighted rate.
					{103, 1, 0, 0},
					{104, 3, 1000, 1000}, // Outside the requested window.
				} {
					aggregate, err := common.Marshal(ChannelMonitorRedisSharedAggregate{
						GroupCacheReadTokens: fixture.read, GroupCacheInputTokens: fixture.input,
					})
					require.NoError(t, err)
					row := (model.ChannelMonitorDailyMetricIdentity{
						ChannelID: 201 + index, APIKeyID: fixture.key, Group: "key-stats",
					}).LedgerRow(today - fixture.daysAgo*daySeconds)
					row.AggregateJSON = string(aggregate)
					require.NoError(t, db.Create(&row).Error)
				}
				var events []model.ChannelMonitorEvent
				for index, fixture := range []struct {
					group            string
					key              int
					read, input      int64
					stream, excluded bool
				}{
					{"key-stats", 101, 0, 2000, true, false},
					{"key-stats", 102, 50, 100, true, false},
					{"key-stats", 103, 0, 0, true, false},
					{"key-stats", 105, 1000, 1000, false, false},
					{"key-stats", 106, 1000, 1000, true, true},
					{"key-stats-private", 101, 1000, 1000, true, false},
					{"key-stats-zero", 107, 0, 100, true, false},
				} {
					event := newChannelMonitorRedisSharedProjectionTestEvent("key-stats-"+fixture.group, now)
					event.EventId += strconv.Itoa(index)
					event.EventSequence = uint64(2000 + index)
					event.ChannelId, event.GroupName, event.APIKeyId = 301+index, fixture.group, fixture.key
					event.InputTokens, event.CacheReadTokens = &fixture.input, &fixture.read
					event.IsStream, event.GroupCacheExcluded = fixture.stream, &fixture.excluded
					events = append(events, event)
				}
				common.RedisEnabled = false
				require.NoError(t, projection.WriteChannelMonitorEvents(ctx, events))
				require.NoError(t, projection.WriteChannelMonitorEvents(ctx, events)) // Replays must not alter the rates.
				common.RedisEnabled = true
				for _, tc := range []struct {
					start            int64
					maximum, average float64
				}{
					{today - 2*daySeconds, 50, 35},
					{today, 50, 25},
				} {
					statistics, err := GetChannelGroupMonitorCacheStatistics(ctx, []string{"key-stats", "key-stats-zero", "key-stats-empty"}, tc.start, now+1)
					require.NoError(t, err)
					require.NotNil(t, statistics["key-stats"].APIKeyMax)
					require.NotNil(t, statistics["key-stats"].APIKeyAverage)
					assert.InDelta(t, tc.maximum, *statistics["key-stats"].APIKeyMax, 0.000001)
					assert.InDelta(t, tc.average, *statistics["key-stats"].APIKeyAverage, 0.000001)
					require.NotNil(t, statistics["key-stats-zero"].APIKeyAverage)
					assert.Zero(t, *statistics["key-stats-zero"].APIKeyAverage)
					assert.Zero(t, *statistics["key-stats-zero"].APIKeyMax)
					assert.NotContains(t, statistics, "key-stats-private")
					assert.NotContains(t, statistics, "key-stats-empty")
				}
			})
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
				// Legacy rows lack eligible token totals; their unfiltered tokens
				// and request counts must not be mixed into the new rate.
				legacy := (model.ChannelMonitorDailyMetricIdentity{ChannelID: 98, Group: "context-vip"}).LedgerRow(day)
				legacy.CacheHitCount, legacy.CacheSampleCount = 50, 50
				legacy.CacheReadTokens, legacy.InputTokens = 500000, 500000
				legacy.AggregateJSON = `{"cache_hit_count":50,"cache_sample_count":50,"cache_read_tokens":500000,"input_tokens":500000,"group_cache_excluded_hits":1,"group_cache_excluded_samples":1}`
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
					{10, map[string]float64{"context-vip": 10000.0 / 60000 * 100}},
					{11, map[string]float64{"context-vip": 10000.0 / 60000 * 100}},
					{21, map[string]float64{"context-vip": 10000.0 / 60000 * 100}},
				} {
					previousPolicy := channelGroupMonitorCachePolicyState.Load()
					channelGroupMonitorCachePolicyState.Store(&channelGroupMonitorCachePolicy{MinContextK: tc.min})
					t.Cleanup(func() { channelGroupMonitorCachePolicyState.Store(previousPolicy) })
					rates, err := GetChannelGroupMonitorCacheRates(ctx, []string{"context-vip"}, day, now+1)
					require.NoError(t, err)
					assert.InDeltaMapValues(t, tc.want, rates, 0.000001)
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
