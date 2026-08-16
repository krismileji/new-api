package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestAdvanceChannelMonitorRedisEffectStateSQLiteMultipleConnectionsCAS(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "redis-effect-cas.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)
	sqlDB.SetMaxIdleConns(2)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&ChannelMonitorRedisEffectState{}))

	effectKey := channelMonitorRedisEffectKey("sqlite_cas", "shared")
	require.NoError(t, db.Create(&ChannelMonitorRedisEffectState{
		EffectKey:     effectKey,
		EventSequence: 100,
	}).Error)

	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, sequence := range []int64{101, 102} {
		sequence := sequence
		go func() {
			results <- db.Connection(func(connection *gorm.DB) error {
				ready <- struct{}{}
				<-start
				return advanceChannelMonitorRedisEffectStateTx(
					connection,
					&ChannelMonitorRedisEffectState{EffectKey: effectKey, EventSequence: 100},
					sequence,
				)
			})
		}()
	}
	<-ready
	<-ready
	close(start)

	firstErr := <-results
	secondErr := <-results
	if firstErr != nil && secondErr == nil {
		firstErr, secondErr = secondErr, firstErr
	}
	require.NoError(t, firstErr)
	assert.ErrorIs(t, secondErr, errSystemTaskStateChanged)

	var state ChannelMonitorRedisEffectState
	require.NoError(t, db.First(&state, "effect_key = ?", effectKey).Error)
	assert.Contains(t, []int64{101, 102}, state.EventSequence)
}

func TestEnqueueRequiredSystemTaskAfterRedisSequenceIsIdempotent(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(&SystemTask{}, &ChannelMonitorRedisEffectState{}))

	firstTask, created, applied, err := EnqueueRequiredSystemTaskAfterRedisSequence(
		"channel_smart_schedule",
		map[string]string{"trigger": "first"},
		101,
	)
	require.NoError(t, err)
	require.NotNil(t, firstTask)
	assert.True(t, created)
	assert.True(t, applied)

	replayedTask, created, applied, err := EnqueueRequiredSystemTaskAfterRedisSequence(
		"channel_smart_schedule",
		map[string]string{"trigger": "replay"},
		101,
	)
	require.NoError(t, err)
	assert.Nil(t, replayedTask)
	assert.False(t, created)
	assert.False(t, applied)

	secondTask, created, applied, err := EnqueueRequiredSystemTaskAfterRedisSequence(
		"channel_smart_schedule",
		map[string]string{"trigger": "newer"},
		102,
	)
	require.NoError(t, err)
	require.NotNil(t, secondTask)
	assert.False(t, created)
	assert.True(t, applied)
	assert.Equal(t, firstTask.ID, secondTask.ID)

	var taskCount int64
	require.NoError(t, db.Model(&SystemTask{}).
		Where("type = ?", "channel_smart_schedule").
		Count(&taskCount).Error)
	assert.Equal(t, int64(1), taskCount)
	var state ChannelMonitorRedisEffectState
	require.NoError(t, db.First(
		&state,
		"effect_key = ?",
		channelMonitorRedisEffectKey("schedule", "channel_smart_schedule"),
	).Error)
	assert.Equal(t, int64(102), state.EventSequence)
}

func TestRedisRuntimeProtectionSequencePreventsReplayMutation(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelMonitorRedisEffectState{}))
	priority := int64(100)
	weight := uint(1000)
	require.NoError(t, db.Create(&Channel{
		Id: 5101, Name: "redis runtime protection", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 5101, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 5101, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1, BasePriority: priority, BaseWeight: weight,
	}).Error)

	now := common.GetTimestamp()
	result, err := ProtectChannelSmartScheduleRouteOnShortTermFailureFromRedis(
		5101, "vip", "model-a", now+60, "first", "", 201,
	)
	require.NoError(t, err)
	assert.True(t, result.Handled)
	var first ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 5101, GroupName: "vip", ModelName: "model-a",
	}).First(&first).Error)

	result, err = ProtectChannelSmartScheduleRouteOnShortTermFailureFromRedis(
		5101, "vip", "model-a", now+600, "replay", "", 201,
	)
	require.NoError(t, err)
	assert.False(t, result.Handled)
	var replayed ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 5101, GroupName: "vip", ModelName: "model-a",
	}).First(&replayed).Error)
	assert.Equal(t, first.Revision, replayed.Revision)
	assert.Equal(t, first.RuntimeProtectionUntil, replayed.RuntimeProtectionUntil)
	assert.Equal(t, first.LastScheduleError, replayed.LastScheduleError)

	result, err = ProtectChannelSmartScheduleRouteOnShortTermFailureFromRedis(
		5101, "vip", "model-a", now+120, "newer", "", 202,
	)
	require.NoError(t, err)
	assert.True(t, result.Handled)
	var newer ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 5101, GroupName: "vip", ModelName: "model-a",
	}).First(&newer).Error)
	assert.Greater(t, newer.Revision, first.Revision)
	assert.Equal(t, now+120, newer.RuntimeProtectionUntil)

	result, err = ProtectChannelSmartScheduleRouteOnShortTermFailureFromRedis(
		5101, "vip", "model-a", now-1, "expired", "", 203,
	)
	require.NoError(t, err)
	assert.False(t, result.Handled)
	var expired ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 5101, GroupName: "vip", ModelName: "model-a",
	}).First(&expired).Error)
	assert.Equal(t, newer.Revision, expired.Revision)
	assert.Equal(t, newer.RuntimeProtectionUntil, expired.RuntimeProtectionUntil)
}

func TestRedisAdaptiveSequencePreventsReplayMutation(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelMonitorRedisEffectState{}))
	priority := int64(80)
	weight := uint(1000)
	require.NoError(t, db.Create(&Channel{
		Id: 5201, Name: "redis adaptive", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 5201, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 5201, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
	}).Error)

	updates := []ChannelSmartScheduleRouteResultUpdate{{
		ChannelId: 5201, Group: "vip", Model: "model-a",
		AdaptiveOverlayOnly: true, AdaptiveHealthSet: true,
		AdaptiveHealthState: "healthy", RedisRuntimeEventSequence: 301,
	}}
	outcomes, err := ApplyChannelSmartScheduleRouteResults(updates)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.True(t, outcomes[0].Applied)
	var first ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 5201, GroupName: "vip", ModelName: "model-a",
	}).First(&first).Error)
	assert.Equal(t, "healthy", first.AdaptiveHealthState)

	updates[0].AdaptiveHealthState = "critical"
	outcomes, err = ApplyChannelSmartScheduleRouteResults(updates)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.True(t, outcomes[0].Applied)
	var replayed ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 5201, GroupName: "vip", ModelName: "model-a",
	}).First(&replayed).Error)
	assert.Equal(t, first.Revision, replayed.Revision)
	assert.Equal(t, "healthy", replayed.AdaptiveHealthState)

	updates[0].RedisRuntimeEventSequence = 302
	outcomes, err = ApplyChannelSmartScheduleRouteResults(updates)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.True(t, outcomes[0].Applied)
	var newer ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(&ChannelSmartScheduleRouteState{
		ChannelId: 5201, GroupName: "vip", ModelName: "model-a",
	}).First(&newer).Error)
	assert.Greater(t, newer.Revision, first.Revision)
	assert.Equal(t, "critical", newer.AdaptiveHealthState)
}

func TestChannelMonitorRedisEffectStateMigrationDialectDDL(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
		quote     string
	}{
		{
			name: "mysql57",
			dialector: mysql.New(mysql.Config{
				DSN:                       "new_api:test@tcp(127.0.0.1:3306)/new_api?charset=utf8mb4&parseTime=True&loc=Local",
				SkipInitializeWithVersion: true,
			}),
			quote: "`",
		},
		{
			name: "postgres96",
			dialector: postgres.New(postgres.Config{
				DSN:                  "host=127.0.0.1 user=new_api password=test dbname=new_api port=5432 sslmode=disable",
				PreferSimpleProtocol: true,
			}),
			quote: `"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := &channelModelDetectionMigrationSQLRecorder{}
			db, err := gorm.Open(test.dialector, &gorm.Config{
				DryRun:               true,
				DisableAutomaticPing: true,
				Logger:               recorder,
			})
			require.NoError(t, err)
			require.NoError(t, db.Migrator().CreateTable(&ChannelMonitorRedisEffectState{}))
			ddl := strings.ToUpper(strings.Join(recorder.statements, "\n"))
			assert.Contains(t, ddl, strings.ToUpper(
				"CREATE TABLE "+test.quote+"channel_monitor_redis_effect_states"+test.quote,
			))
			assert.Contains(t, ddl, strings.ToUpper(test.quote+"effect_key"+test.quote+" CHAR(64)"))
			assert.Contains(t, ddl, strings.ToUpper(test.quote+"event_sequence"+test.quote+" BIGINT"))
			assert.Contains(t, ddl, "PRIMARY KEY")
		})
	}
}

func TestChannelMonitorRedisEffectStateConfiguredDatabases(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		dialector func(string) gorm.Dialector
	}{
		{name: "mysql57", env: "TEST_MYSQL_DSN", dialector: func(dsn string) gorm.Dialector {
			return mysql.Open(dsn)
		}},
		{name: "postgres96", env: "TEST_POSTGRES_DSN", dialector: func(dsn string) gorm.Dialector {
			return postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := strings.TrimSpace(os.Getenv(test.env))
			if dsn == "" {
				t.Skip(test.env + " is not configured")
			}
			config := &gorm.Config{}
			if test.name == "mysql57" {
				config.NamingStrategy = schema.NamingStrategy{
					TablePrefix: fmt.Sprintf("cmre%x_", time.Now().UnixNano()),
				}
			}
			db, err := gorm.Open(test.dialector(dsn), config)
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

			if test.name == "postgres96" {
				schemaName := fmt.Sprintf("cmre%x", time.Now().UnixNano())
				require.NoError(t, db.Exec("CREATE SCHEMA "+schemaName).Error)
				require.NoError(t, db.Exec("SET search_path TO "+schemaName).Error)
				t.Cleanup(func() {
					require.NoError(t, db.Exec("SET search_path TO public").Error)
					require.NoError(t, db.Exec("DROP SCHEMA "+schemaName+" CASCADE").Error)
				})
			} else {
				t.Cleanup(func() { _ = db.Migrator().DropTable(&ChannelMonitorRedisEffectState{}) })
			}

			require.NoError(t, db.AutoMigrate(&ChannelMonitorRedisEffectState{}))
			expected := ChannelMonitorRedisEffectState{
				EffectKey:     channelMonitorRedisEffectKey("configured", test.name),
				EventSequence: 123,
				UpdatedAt:     456,
			}
			require.NoError(t, db.Create(&expected).Error)
			var actual ChannelMonitorRedisEffectState
			require.NoError(t, db.First(&actual, "effect_key = ?", expected.EffectKey).Error)
			assert.Equal(t, expected, actual)
		})
	}
}
