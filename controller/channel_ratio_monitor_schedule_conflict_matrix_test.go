package controller

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRedisAdaptiveRefreshConfigurationConflictDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var dialector gorm.Dialector
			databaseType := common.DatabaseTypeSQLite
			switch engine {
			case "sqlite":
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "conflict.db"))
			case "mysql":
				dsn := os.Getenv("MONITOR_CONFLICT_MYSQL_DSN")
				if dsn == "" {
					t.Skip("MONITOR_CONFLICT_MYSQL_DSN is not set")
				}
				config, err := mysqlDriver.ParseDSN(dsn)
				require.NoError(t, err)
				require.Equal(t, "new_api_monitor_conflict_test", config.DBName)
				require.Equal(t, "tcp", config.Net)
				require.True(t, strings.HasPrefix(config.Addr, "127.0.0.1:"))
				dialector, databaseType = mysql.Open(dsn), common.DatabaseTypeMySQL
			case "postgres":
				dsn := os.Getenv("MONITOR_CONFLICT_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("MONITOR_CONFLICT_POSTGRES_DSN is not set")
				}
				parsed, err := url.Parse(dsn)
				require.NoError(t, err)
				require.Equal(t, "127.0.0.1", parsed.Hostname())
				require.Equal(t, "/new_api_monitor_conflict_test", parsed.Path)
				dialector = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
				databaseType = common.DatabaseTypePostgreSQL
			}
			db, err := gorm.Open(dialector, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
			originalDB, originalLogDB := model.DB, model.LOG_DB
			originalMain, originalLog := common.MainDatabaseType(), common.LogDatabaseType()
			originalCache, originalRedis := common.MemoryCacheEnabled, common.RedisEnabled
			model.DB, model.LOG_DB = db, db
			common.SetDatabaseTypes(databaseType, databaseType)
			common.MemoryCacheEnabled, common.RedisEnabled = false, false
			t.Cleanup(func() {
				model.DB, model.LOG_DB = originalDB, originalLogDB
				common.SetDatabaseTypes(originalMain, originalLog)
				common.MemoryCacheEnabled, common.RedisEnabled = originalCache, originalRedis
			})
			tables := []any{
				&model.Option{}, &model.Channel{}, &model.Ability{}, &model.ChannelRatioMonitor{},
				&model.ChannelSmartScheduleRouteState{}, &model.ChannelSmartScheduleGroupPause{},
				&model.ChannelSmartScheduleModelSampleState{}, &model.ChannelMonitorRedisEffectState{},
			}
			for _, table := range tables {
				require.False(t, db.Migrator().HasTable(table), "use an empty disposable database")
			}
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
			require.NoError(t, db.AutoMigrate(tables...))
			versionQuery := "SELECT version()"
			if engine == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database=%s version=%s", engine, version)

			usePersistedChannelMonitorOptions(t, db, map[string]string{
				channelMonitorSmartScheduleControlRevisionOption: "control-current",
				model.ChannelMonitorEconomicRevisionOption:       "economics-current",
				"GroupRatio": `{"vip":1}`,
			})
			priority, weight := int64(10), uint(1000)
			require.NoError(t, db.Create(&model.Channel{
				Id: 1709, Name: "configuration conflict", Status: common.ChannelStatusEnabled,
				Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
			}).Error)
			require.NoError(t, db.Create(&model.Ability{
				ChannelId: 1709, Group: "vip", Model: "model-a", Enabled: true,
				Priority: &priority, Weight: weight,
			}).Error)
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 1709, Ratio: 0.5, CostConversion: "1"}).Error)
			originalState := model.ChannelSmartScheduleRouteState{
				ChannelId: 1709, GroupName: "vip", ModelName: "model-a",
				ParticipationSet: true, Revision: 7, BaseRank: 1, BasePriority: priority, BaseWeight: weight,
			}
			require.NoError(t, db.Create(&originalState).Error)
			policy := channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 2, 80, 30,
			).policy()
			policy.AdaptiveSamplingEnabled = true
			readMetric := func(context.Context, int, string, int64, int64, int, float64, float64) (model.ChannelSmartScheduleAdaptiveHealthMetric, error) {
				return model.ChannelSmartScheduleAdaptiveHealthMetric{RequestCount: 2, HealthyRequestCount: 2, LastUsedTime: 1750000000}, nil
			}
			readCooldown := func(context.Context, int, string) (int64, error) { return 0, nil }
			pool := channelSmartScheduleRoutePoolKey{group: "vip", model: "model-a"}
			conflict, err := refreshChannelSmartScheduleAdaptivePoolWithMetricReader(
				context.Background(), pool, policy, "control-stale", db,
				readMetric, readCooldown, 1750000000, 101,
			)
			require.ErrorIs(t, err, service.ErrChannelMonitorRedisRetryable)
			assert.True(t, conflict)
			assert.Contains(t, err.Error(), "渠道 1709")
			assert.Contains(t, err.Error(), "调度配置版本已变化")
			assert.Contains(t, err.Error(), "control-stale")
			assert.Contains(t, err.Error(), "control-current")
			var state model.ChannelSmartScheduleRouteState
			require.NoError(t, db.First(&state, originalState.Id).Error)
			assert.Equal(t, originalState, state, "a conflict must preserve the prior route state")
			var effectCount int64
			require.NoError(t, db.Model(&model.ChannelMonitorRedisEffectState{}).Count(&effectCount).Error)
			assert.Zero(t, effectCount, "a failed refresh must not advance the event watermark")

			conflict, err = refreshChannelSmartScheduleAdaptivePoolWithMetricReader(
				context.Background(), pool, policy, "control-current", db,
				readMetric, readCooldown, 1750000000, 101,
			)
			require.NoError(t, err)
			assert.False(t, conflict)
			require.NoError(t, db.First(&state, originalState.Id).Error)
			assert.Equal(t, originalState.Revision+1, state.Revision)
			var effect model.ChannelMonitorRedisEffectState
			require.NoError(t, db.First(&effect).Error)
			assert.Equal(t, int64(101), effect.EventSequence)
			appliedState := state
			conflict, err = refreshChannelSmartScheduleAdaptivePoolWithMetricReader(
				context.Background(), pool, policy, "control-current", db,
				readMetric, readCooldown, 1750000000, 101,
			)
			require.NoError(t, err)
			assert.False(t, conflict)
			require.NoError(t, db.First(&state, originalState.Id).Error)
			assert.Equal(t, appliedState, state, "replaying the recovered event must remain idempotent")

			// Production reads published snapshots while its background worker
			// applies newer runtime events. Two valid consecutive events must not
			// conflict with each other while snapshot publication catches up.
			previousClient := common.RDB
			client := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
			common.RDB = client
			common.RedisEnabled, common.MemoryCacheEnabled = true, true
			t.Cleanup(func() {
				common.RDB = previousClient
				assert.NoError(t, client.Close())
			})
			model.InitChannelCache()
			publishedRoutes, err := model.GetChannelSmartScheduleRoutePool("vip", "model-a")
			require.NoError(t, err)
			require.Len(t, publishedRoutes, 1)
			for _, sequence := range []int64{102, 103} {
				conflict, err = refreshChannelSmartScheduleAdaptivePoolWithMetricReader(
					context.Background(), pool, policy, "control-current", db,
					readMetric, readCooldown, 1750000000+sequence, sequence,
				)
				require.NoError(t, err, "a valid event must recover without waiting for snapshot publication")
				assert.False(t, conflict)
			}
			require.NoError(t, db.First(&state, originalState.Id).Error)
			assert.Equal(t, appliedState.Revision+2, state.Revision)
			require.NoError(t, db.First(&effect).Error)
			assert.Equal(t, int64(103), effect.EventSequence)
			publishedEconomics, err := model.GetChannelSmartScheduleEconomicSnapshot()
			require.NoError(t, err)
			require.NoError(t, db.Save(&model.Option{Key: model.ChannelMonitorEconomicRevisionOption, Value: "economics-next"}).Error)
			conflict, err = refreshChannelSmartScheduleAdaptivePoolWithMetricReader(
				context.Background(), pool, policy, "control-current", db,
				readMetric, readCooldown, 1750000104, 104,
			)
			require.NoError(t, err, "an economic snapshot awaiting publication must not block background updates")
			assert.False(t, conflict)
			stillPublished, err := model.GetChannelSmartScheduleEconomicSnapshot()
			require.NoError(t, err)
			assert.Equal(t, publishedEconomics.Revision, stillPublished.Revision, "dashboard reads retain their published snapshot contract")
			require.NoError(t, db.First(&effect).Error)
			assert.Equal(t, int64(104), effect.EventSequence)

			t.Run("full_schedule_recovers_without_snapshot_publication", func(t *testing.T) {
				require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))
				t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&model.SystemTask{}, &model.SystemTaskLock{})) })
				useChannelSmartScheduleGroupRatio(t, `{"vip":1}`)
				usePersistedChannelMonitorOptions(t, db, map[string]string{
					channelMonitorSmartScheduleEnabledOption:         "true",
					channelMonitorSmartScheduleControlRevisionOption: "control-current",
					model.ChannelMonitorEconomicRevisionOption:       "economics-current",
					"GroupRatio": `{"vip":1}`,
					channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
						channelSmartScheduleTestGroupPolicy(
							"vip", channelMonitorSmartScheduleStrategyRatio, false,
							channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 1, 80, 30,
						),
					),
				})
				require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).Where("channel_id = ?", 1709).
					Update("updated_time", common.GetTimestamp()).Error)
				cached, err := model.GetChannelSmartScheduleRoutes()
				require.NoError(t, err)
				require.Len(t, cached, 1)
				require.NoError(t, db.First(&state, originalState.Id).Error)
				require.Less(t, cached[0].State.Revision, state.Revision)
				before := state.Revision
				for attempt := range 2 {
					if attempt == 1 {
						require.NoError(t, db.Save(&model.Option{Key: model.ChannelMonitorEconomicRevisionOption, Value: "economics-next"}).Error)
					}
					result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
					require.NoError(t, err, "full scheduling must not reuse a published write revision or economic snapshot")
					assert.Zero(t, result.Failed)
					require.Len(t, result.Adjustments, 1)
				}
				require.NoError(t, db.First(&state, originalState.Id).Error)
				assert.Equal(t, before+2, state.Revision)
				assert.Equal(t, model.ChannelSmartScheduleStatusSkipped, state.LastScheduleStatus)
				refreshed, err := model.GetChannelSmartScheduleRoutes()
				require.NoError(t, err)
				require.Len(t, refreshed, 1)
				assert.Equal(t, state.Revision, refreshed[0].State.Revision, "state-only results must refresh the dashboard snapshot too")
				var tasks int64
				require.NoError(t, db.Model(&model.SystemTask{}).Count(&tasks).Error)
				assert.Zero(t, tasks, "an old dashboard snapshot must not create a retry storm")
			})
		})
	}
}
