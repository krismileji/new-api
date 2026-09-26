package controller

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/glebarez/sqlite"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupImmediateEjectionDatabase(t *testing.T, engine string) *gorm.DB {
	t.Helper()
	var dialector gorm.Dialector
	databaseType := common.DatabaseTypeSQLite
	switch engine {
	case "sqlite":
		dialector = sqlite.Open(filepath.Join(t.TempDir(), "immediate-ejection.db"))
	case "mysql":
		dsn := os.Getenv("IMMEDIATE_EJECTION_MYSQL_DSN")
		if dsn == "" {
			t.Skip("IMMEDIATE_EJECTION_MYSQL_DSN is not set")
		}
		config, err := mysqlDriver.ParseDSN(dsn)
		require.NoError(t, err)
		require.Equal(t, "new_api_immediate_ejection_test", config.DBName)
		require.Equal(t, "tcp", config.Net)
		require.True(t, strings.HasPrefix(config.Addr, "127.0.0.1:"))
		dialector, databaseType = mysql.Open(dsn), common.DatabaseTypeMySQL
	case "postgres":
		dsn := os.Getenv("IMMEDIATE_EJECTION_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("IMMEDIATE_EJECTION_POSTGRES_DSN is not set")
		}
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		require.Equal(t, "127.0.0.1", parsed.Hostname())
		require.Equal(t, "/new_api_immediate_ejection_test", parsed.Path)
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
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	t.Cleanup(func() {
		model.DB, model.LOG_DB = originalDB, originalLogDB
		common.SetDatabaseTypes(originalMain, originalLog)
		common.MemoryCacheEnabled, common.RedisEnabled = originalCache, originalRedis
	})
	tables := []any{
		&model.Option{}, &model.Channel{}, &model.Ability{}, &model.ChannelRatioMonitor{},
		&model.ChannelSmartScheduleRouteState{}, &model.ChannelSmartScheduleGroupPause{},
		&model.ChannelSmartScheduleModelSampleState{}, &model.ChannelMonitorRedisEffectState{},
		&model.ChannelLogicalSmartScheduleRouteState{},
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
	return db
}

func TestChannelSmartScheduleImmediateEjectionDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupImmediateEjectionDatabase(t, engine)
			cases := []struct {
				name             string
				enabled          *bool
				stabilityOff     bool
				windowRate       bool
				legacyThreshold  bool
				temporaryTraffic bool
				initialState     string
				scheduledProbe   bool
				wantProtection   bool
			}{
				{name: "legacy policy retains consecutive ejection", wantProtection: true},
				{name: "enabled consecutive failures eject", enabled: common.GetPointer(true), wantProtection: true},
				{name: "disabled consecutive failures preserve routing", enabled: common.GetPointer(false)},
				{name: "enabled failure rate ejects across successes", enabled: common.GetPointer(true), windowRate: true, wantProtection: true},
				{name: "disabled failure rate preserves routing", enabled: common.GetPointer(false), windowRate: true},
				{name: "disabled legacy failure count preserves routing", enabled: common.GetPointer(false), legacyThreshold: true},
				{name: "normal traffic still requires stability", enabled: common.GetPointer(true), stabilityOff: true},
				{name: "temporary traffic requires stability", enabled: common.GetPointer(true), stabilityOff: true, temporaryTraffic: true},
				{name: "temporary failure rate requires stability", enabled: common.GetPointer(true), stabilityOff: true, temporaryTraffic: true, windowRate: true},
				{name: "enabled temporary traffic ejects", enabled: common.GetPointer(true), temporaryTraffic: true, wantProtection: true},
				{name: "disabled stability does not renew trial protection", enabled: common.GetPointer(true), stabilityOff: true, initialState: model.ChannelSmartScheduleStabilityProbing},
				{name: "disabled temporary traffic preserves routing", enabled: common.GetPointer(false), stabilityOff: true, temporaryTraffic: true},
				{name: "disabled ejection preserves trial failure protection", enabled: common.GetPointer(false), initialState: model.ChannelSmartScheduleStabilityProbing, wantProtection: true},
				{name: "disabled ejection preserves degraded probe renewal", enabled: common.GetPointer(false), initialState: model.ChannelSmartScheduleStabilityDegraded, scheduledProbe: true, wantProtection: true},
			}
			for index, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					policy := channelSmartScheduleTestGroupPolicy(
						"vip", channelMonitorSmartScheduleStrategyRatio, !tc.stabilityOff,
						channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 100, 80, 30,
					)
					policy.ImmediateEjectionEnabled = tc.enabled
					policy.ConsecutiveFailureThreshold = common.GetPointer(2)
					policy.BurstFailureWindowMinutes = common.GetPointer(1)
					policy.BurstFailureWindowRequests = common.GetPointer(3)
					policy.BurstFailureThresholdPercent = common.GetPointer(100.0)
					policy.BurstFailureWindowSeconds, policy.BurstFailureThreshold = nil, nil
					policy.DegradedProbeEnabled = common.GetPointer(true)
					if tc.windowRate {
						policy.ConsecutiveFailureThreshold = common.GetPointer(3)
						policy.BurstFailureThresholdPercent = common.GetPointer(50.0)
					}
					if tc.legacyThreshold {
						policy.ConsecutiveFailureThreshold = common.GetPointer(100)
						policy.BurstFailureThresholdPercent = nil
						policy.BurstFailureThreshold = common.GetPointer(1)
					}
					usePersistedChannelMonitorOptions(t, db, map[string]string{
						channelMonitorSmartScheduleEnabledOption:           "true",
						channelMonitorSmartScheduleGroupPoliciesOption:     channelSmartScheduleTestGroupPoliciesJSON(t, policy),
						channelMonitorSmartScheduleControlRevisionOption:   "ejection-test",
						channelMonitorSmartScheduleRateLimitCooldownOption: "0",
					})
					var saved model.Option
					require.NoError(t, db.Where(&model.Option{Key: channelMonitorSmartScheduleGroupPoliciesOption}).First(&saved).Error)
					var decoded []channelSmartScheduleGroupPolicy
					require.NoError(t, common.UnmarshalJsonStr(saved.Value, &decoded))
					require.Len(t, decoded, 1)
					wantEnabled := tc.enabled == nil || *tc.enabled
					assert.Equal(t, wantEnabled, decoded[0].policy().ImmediateEjectionEnabled)
					normalized, err := normalizeChannelSmartScheduleGroupPolicies(decoded)
					require.NoError(t, err)
					require.NotNil(t, normalized[0].ImmediateEjectionEnabled)
					assert.Equal(t, wantEnabled, *normalized[0].ImmediateEjectionEnabled)

					channelID, now := 9100+index, common.GetTimestamp()
					priority, weight := int64(100), uint(40)
					require.NoError(t, db.Create(&model.Channel{
						Id: channelID, Name: tc.name, Status: common.ChannelStatusEnabled,
						Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
					}).Error)
					ability := model.Ability{ChannelId: channelID, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight}
					state := model.ChannelSmartScheduleRouteState{
						ChannelId: channelID, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
						Revision: 1, BaseRank: 1, BasePriority: priority, BaseWeight: weight, StabilityState: tc.initialState,
					}
					if tc.temporaryTraffic {
						state.TemporaryTrafficKind = model.ChannelSmartScheduleTemporaryTrafficExploration
						state.TemporaryTrafficSince = now - 30
					}
					if tc.initialState != "" {
						state.StabilitySince, state.StabilityUntil = now-30, now+60
						state.StabilitySavedPriority, state.StabilitySavedWeight = priority, weight
						ability.Priority, ability.Weight = common.GetPointer(int64(0)), 0
					}
					require.NoError(t, db.Create(&ability).Error)
					require.NoError(t, db.Create(&state).Error)
					health := channelSmartScheduleRuntimeHealthSnapshot{
						CoverageStart: now - 60,
						RequestEvents: []channelSmartScheduleRuntimeRequestEvent{
							{Timestamp: now - 3}, {Timestamp: now - 2, Failure: true}, {Timestamp: now - 1, Failure: true},
						},
					}
					if tc.windowRate {
						health.RequestEvents[0].Failure, health.RequestEvents[1].Failure = true, false
					}
					if tc.initialState != "" {
						health.RequestEvents = []channelSmartScheduleRuntimeRequestEvent{{Timestamp: now - 1, Failure: true}}
						request, err := channelSmartScheduleProbeRecoveryRequest(channelID, "model-a", now, tc.scheduledProbe, "")
						require.NoError(t, err)
						if tc.stabilityOff {
							assert.Nil(t, request)
						} else {
							require.NotNil(t, request, "disabling ejection must still allow recovery of existing protection")
							require.Len(t, request.Routes, 1)
							assert.Equal(t, *policy.RecoverySuccessThreshold, request.Routes[0].RecoverySuccessThreshold)
						}
					}
					runtimeError := types.NewErrorWithStatusCode(errors.New("上游返回 503"), types.ErrorCodeGetChannelFailed, 503)
					require.NoError(t, applyChannelSmartScheduleRuntimeFailureWithSource(
						channelID, "model-a", runtimeError, tc.scheduledProbe, false, false, &health, false, now, 0,
					))
					var actualState model.ChannelSmartScheduleRouteState
					var actualAbility model.Ability
					require.NoError(t, db.First(&actualState, state.Id).Error)
					require.NoError(t, db.Where(&model.Ability{ChannelId: channelID, Group: "vip", Model: "model-a"}).First(&actualAbility).Error)
					require.NotNil(t, actualAbility.Priority)
					if !tc.wantProtection {
						assert.Equal(t, state, actualState)
						assert.Equal(t, *ability.Priority, *actualAbility.Priority)
						assert.Equal(t, ability.Weight, actualAbility.Weight)
						return
					}
					assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, actualState.StabilityState)
					assert.Equal(t, now+30*60, actualState.StabilityUntil)
					assert.Empty(t, actualState.TemporaryTrafficKind)
					assert.Zero(t, *actualAbility.Priority)
					assert.Zero(t, actualAbility.Weight)
				})
			}
		})
	}
}
