package model

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// channelLogicalGroupDDLRecorder captures GORM's dialect-specific DDL without
// requiring a live MySQL/PostgreSQL server. The configured-database test below
// is opt-in via TEST_MYSQL_DSN/TEST_POSTGRES_DSN when those servers are present.
type channelLogicalGroupDDLRecorder struct {
	statements []string
}

func (r *channelLogicalGroupDDLRecorder) LogMode(_ logger.LogLevel) logger.Interface { return r }
func (r *channelLogicalGroupDDLRecorder) Info(context.Context, string, ...any)       {}
func (r *channelLogicalGroupDDLRecorder) Warn(context.Context, string, ...any)       {}
func (r *channelLogicalGroupDDLRecorder) Error(context.Context, string, ...any)      {}
func (r *channelLogicalGroupDDLRecorder) Trace(_ context.Context, _ time.Time, sql func() (string, int64), _ error) {
	statement, _ := sql()
	r.statements = append(r.statements, statement)
}

const (
	channelLogicalGroupLegacyChannelID          = 81001
	channelLogicalGroupLegacyStatusConfigID     = 82001
	channelLogicalGroupLegacyStatusStateID      = 83001
	channelLogicalGroupLegacyStatusExecutionID  = 84001
	channelLogicalGroupLegacyDetectionRunID     = 85001
	channelLogicalGroupLegacyDetectionExecution = 86001
)

func channelLogicalGroupMigrationModels() []any {
	return []any{
		&Channel{},
		&ChannelLogicalGroup{},
		&ChannelLogicalGroupMember{},
		&ChannelLogicalSmartScheduleRouteState{},
		&ChannelLogicalSmartScheduleSampleState{},
		&ChannelStatusProbeConfig{},
		&ChannelStatusProbeLogicalConfig{},
		&ChannelStatusProbeState{},
		&ChannelStatusProbeLogicalState{},
		&ChannelStatusProbeExecution{},
		&ChannelModelDetectionLogicalConfig{},
		&ChannelModelDetectionLogicalTarget{},
		&ChannelModelDetectionRun{},
		&ChannelModelDetectionExecution{},
	}
}

// prepareChannelLogicalGroupLegacySchema creates populated pre-feature tables
// by dropping only the columns introduced for logical-channel sharing. The
// current models still define every unrelated legacy column, so this fixture
// cannot silently drift away from the production table shape.
func prepareChannelLogicalGroupLegacySchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(
		&Channel{},
		&ChannelStatusProbeConfig{},
		&ChannelStatusProbeState{},
		&ChannelStatusProbeExecution{},
		&ChannelModelDetectionRun{},
		&ChannelModelDetectionExecution{},
	))

	logicalID := int64(88001)
	require.NoError(t, db.Model(&Channel{}).Create(map[string]any{
		"id": channelLogicalGroupLegacyChannelID, "name": "legacy-channel", "key": "legacy-key",
		"logical_channel_id": logicalID, "channel_info": "{}",
	}).Error)
	legacyRows := []any{
		&ChannelStatusProbeConfig{
			Id: channelLogicalGroupLegacyStatusConfigID, ChannelId: channelLogicalGroupLegacyChannelID,
			LogicalChannelId: logicalID, LogicalRevision: 7, ModelsJSON: "[]",
		},
		&ChannelStatusProbeState{
			Id: channelLogicalGroupLegacyStatusStateID, ChannelId: channelLogicalGroupLegacyChannelID,
			LogicalChannelId: logicalID, LogicalRevision: 7, ModelName: "legacy-status-model",
		},
		&ChannelStatusProbeExecution{
			Id: channelLogicalGroupLegacyStatusExecutionID, RunId: "legacy-status-run",
			ChannelId: channelLogicalGroupLegacyChannelID, LogicalChannelId: logicalID, LogicalRevision: 7,
			ActualChannelId: channelLogicalGroupLegacyChannelID, ModelName: "legacy-status-model",
		},
		&ChannelModelDetectionRun{
			Id: channelLogicalGroupLegacyDetectionRunID, RunId: "legacy-detection-run",
			ChannelId: channelLogicalGroupLegacyChannelID, LogicalChannelID: logicalID, LogicalRevision: 7,
			LogicalMemberSnapshotJSON: "[{\"channel_id\":81001,\"weight\":1}]",
			Trigger:                   ChannelModelDetectionTriggerManual, Preset: ChannelModelDetectionPresetMedium,
			PresetSource: ChannelModelDetectionPresetSourceManualSelected,
		},
		&ChannelModelDetectionExecution{
			Id: channelLogicalGroupLegacyDetectionExecution, RunId: "legacy-detection-run",
			TargetKey: "legacy-target", TargetId: 1, ChannelId: channelLogicalGroupLegacyChannelID,
			LogicalChannelID: logicalID, LogicalRevision: 7, RequestModel: "legacy-detection-model",
			Preset: ChannelModelDetectionPresetMedium,
		},
	}
	for _, row := range legacyRows {
		require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(row).Error)
	}

	removedColumns := []struct {
		model  any
		fields []string
	}{
		{&Channel{}, []string{"LogicalChannelID"}},
		{&ChannelStatusProbeConfig{}, []string{"LogicalChannelId", "LogicalRevision"}},
		{&ChannelStatusProbeState{}, []string{"LogicalChannelId", "LogicalRevision"}},
		{&ChannelStatusProbeExecution{}, []string{"LogicalChannelId", "LogicalRevision", "ActualChannelId"}},
		{&ChannelModelDetectionRun{}, []string{"LogicalChannelID", "LogicalRevision", "LogicalMemberSnapshotJSON"}},
		{&ChannelModelDetectionExecution{}, []string{"LogicalChannelID", "LogicalRevision", "LogicalTargetId"}},
	}
	for _, item := range removedColumns {
		for _, field := range item.fields {
			if db.Migrator().HasIndex(item.model, field) {
				require.NoError(t, db.Migrator().DropIndex(item.model, field))
			}
			require.NoError(t, db.Migrator().DropColumn(item.model, field))
			assert.False(t, db.Migrator().HasColumn(item.model, field), field)
		}
	}
}

func assertChannelLogicalGroupMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, model := range channelLogicalGroupMigrationModels() {
		assert.True(t, db.Migrator().HasTable(model), fmt.Sprintf("%T", model))
	}
	for _, column := range []struct {
		model any
		name  string
	}{
		{&Channel{}, "logical_channel_id"},
		{&ChannelStatusProbeConfig{}, "logical_channel_id"},
		{&ChannelStatusProbeConfig{}, "logical_revision"},
		{&ChannelStatusProbeState{}, "logical_channel_id"},
		{&ChannelStatusProbeState{}, "logical_revision"},
		{&ChannelStatusProbeExecution{}, "logical_channel_id"},
		{&ChannelStatusProbeExecution{}, "logical_revision"},
		{&ChannelStatusProbeExecution{}, "actual_channel_id"},
		{&ChannelModelDetectionRun{}, "logical_channel_id"},
		{&ChannelModelDetectionRun{}, "logical_revision"},
		{&ChannelModelDetectionRun{}, "logical_member_snapshot_json"},
		{&ChannelModelDetectionExecution{}, "logical_channel_id"},
		{&ChannelModelDetectionExecution{}, "logical_revision"},
		{&ChannelModelDetectionExecution{}, "logical_target_id"},
	} {
		assert.True(t, db.Migrator().HasColumn(column.model, column.name), column.name)
	}
	for _, index := range []struct {
		model any
		name  string
	}{
		{&Channel{}, "LogicalChannelID"},
		{&ChannelLogicalGroupMember{}, "uk_channel_logical_group_member_channel"},
		{&ChannelLogicalSmartScheduleRouteState{}, "uk_logical_smart_route"},
		{&ChannelLogicalSmartScheduleSampleState{}, "uk_logical_smart_sample"},
		{&ChannelStatusProbeLogicalState{}, "idx_channel_status_probe_logical_state_model"},
	} {
		assert.True(t, db.Migrator().HasIndex(index.model, index.name), index.name)
	}

	var channel Channel
	require.NoError(t, db.Select("id", "name", "logical_channel_id").First(&channel, channelLogicalGroupLegacyChannelID).Error)
	assert.Equal(t, "legacy-channel", channel.Name)
	assert.Nil(t, channel.LogicalChannelID)

	var statusConfig ChannelStatusProbeConfig
	require.NoError(t, db.First(&statusConfig, channelLogicalGroupLegacyStatusConfigID).Error)
	assert.Equal(t, "[]", statusConfig.ModelsJSON)
	assert.Zero(t, statusConfig.LogicalChannelId)
	assert.Zero(t, statusConfig.LogicalRevision)

	var statusState ChannelStatusProbeState
	require.NoError(t, db.First(&statusState, channelLogicalGroupLegacyStatusStateID).Error)
	assert.Equal(t, "legacy-status-model", statusState.ModelName)
	assert.Zero(t, statusState.LogicalChannelId)
	assert.Zero(t, statusState.LogicalRevision)

	var statusExecution ChannelStatusProbeExecution
	require.NoError(t, db.First(&statusExecution, channelLogicalGroupLegacyStatusExecutionID).Error)
	assert.Equal(t, "legacy-status-run", statusExecution.RunId)
	assert.Zero(t, statusExecution.LogicalChannelId)
	assert.Zero(t, statusExecution.LogicalRevision)
	assert.Zero(t, statusExecution.ActualChannelId)

	var detectionRun ChannelModelDetectionRun
	require.NoError(t, db.First(&detectionRun, channelLogicalGroupLegacyDetectionRunID).Error)
	assert.Equal(t, "legacy-detection-run", detectionRun.RunId)
	assert.Zero(t, detectionRun.LogicalChannelID)
	assert.Zero(t, detectionRun.LogicalRevision)
	assert.Empty(t, detectionRun.LogicalMemberSnapshotJSON)

	var detectionExecution ChannelModelDetectionExecution
	require.NoError(t, db.First(&detectionExecution, channelLogicalGroupLegacyDetectionExecution).Error)
	assert.Equal(t, "legacy-target", detectionExecution.TargetKey)
	assert.Zero(t, detectionExecution.LogicalChannelID)
	assert.Zero(t, detectionExecution.LogicalRevision)
	assert.Nil(t, detectionExecution.LogicalTargetId)
}

// TestChannelLogicalGroupSchemaMigrationSQLite exercises the actual migration
// path used by the application. It covers the nullable channel projection,
// member uniqueness, pagination, and transaction rollback on SQLite. No
// foreign-key relationship is required: the service validates membership and
// deliberately keeps the schema portable across all supported databases.
func TestChannelLogicalGroupSchemaMigrationSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-logical-group-compat.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	prepareChannelLogicalGroupLegacySchema(t, db)
	models := channelLogicalGroupMigrationModels()
	require.NoError(t, db.AutoMigrate(models...))
	// A second startup must be idempotent for SQLite's table-rebuild migrator.
	require.NoError(t, db.AutoMigrate(models...))
	assertChannelLogicalGroupMigration(t, db)

	groupA := &ChannelLogicalGroup{Name: "sqlite-a"}
	groupB := &ChannelLogicalGroup{Name: "sqlite-b"}
	require.NoError(t, db.Create(groupA).Error)
	require.NoError(t, db.Create(groupB).Error)
	fingerprint := strings.Repeat("a", 64)
	require.NoError(t, db.Create(&ChannelLogicalGroupMember{
		LogicalGroupID: groupA.Id, ChannelID: 1001, Weight: 1, AddressFingerprint: fingerprint,
	}).Error)
	duplicate := &ChannelLogicalGroupMember{
		LogicalGroupID: groupB.Id, ChannelID: 1001, Weight: 1, AddressFingerprint: fingerprint,
	}
	require.Error(t, db.Create(duplicate).Error, "a physical channel must belong to at most one logical group")

	// Verify ordinary GORM pagination on the member index.
	require.NoError(t, db.Create(&ChannelLogicalGroupMember{
		LogicalGroupID: groupA.Id, ChannelID: 1002, Weight: 1, AddressFingerprint: fingerprint,
	}).Error)
	var page []ChannelLogicalGroupMember
	require.NoError(t, db.Where("logical_group_id = ?", groupA.Id).Order("channel_id asc").Limit(1).Offset(1).Find(&page).Error)
	require.Len(t, page, 1)
	assert.Equal(t, 1002, page[0].ChannelID)

	// A failed transaction must not leave a partially-created member behind.
	err = db.Transaction(func(tx *gorm.DB) error {
		if createErr := tx.Create(&ChannelLogicalGroupMember{
			LogicalGroupID: groupA.Id, ChannelID: 1003, Weight: 1, AddressFingerprint: fingerprint,
		}).Error; createErr != nil {
			return createErr
		}
		return gorm.ErrInvalidTransaction
	})
	require.ErrorIs(t, err, gorm.ErrInvalidTransaction)
	var rolledBack int64
	require.NoError(t, db.Model(&ChannelLogicalGroupMember{}).Where("channel_id = ?", 1003).Count(&rolledBack).Error)
	assert.Zero(t, rolledBack)
}

// TestChannelLogicalGroupRevisionCASSQLite proves that stale writers are
// rejected by the revision predicate and that the operation is represented by
// ordinary GORM UPDATE syntax supported by SQLite, MySQL, and PostgreSQL.
func TestChannelLogicalGroupRevisionCASSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-logical-group-cas.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&ChannelLogicalGroup{}))

	group := &ChannelLogicalGroup{Name: "cas"}
	require.NoError(t, db.Create(group).Error)
	oldRevision := group.Revision
	updated := db.Model(&ChannelLogicalGroup{}).
		Where("id = ? AND revision = ?", group.Id, oldRevision).
		Updates(map[string]any{"revision": oldRevision + 1})
	require.NoError(t, updated.Error)
	assert.EqualValues(t, 1, updated.RowsAffected)

	stale := db.Model(&ChannelLogicalGroup{}).
		Where("id = ? AND revision = ?", group.Id, oldRevision).
		Updates(map[string]any{"revision": oldRevision + 2})
	require.NoError(t, stale.Error)
	assert.Zero(t, stale.RowsAffected, "a stale revision must not overwrite a newer relation")

	var persisted ChannelLogicalGroup
	require.NoError(t, db.First(&persisted, group.Id).Error)
	assert.EqualValues(t, oldRevision+1, persisted.Revision)
}

func TestChannelLogicalGroupSchemaDialectDDLIsPortable(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
	}{
		{name: "sqlite", dialector: sqlite.Open(":memory:")},
		{name: "mysql57", dialector: mysql.New(mysql.Config{
			DSN:                       "new_api:test@tcp(127.0.0.1:3306)/new_api?charset=utf8mb4&parseTime=True&loc=Local",
			SkipInitializeWithVersion: true,
		})},
		{name: "postgres96", dialector: postgres.New(postgres.Config{
			DSN:                  "host=127.0.0.1 user=new_api password=test dbname=new_api port=5432 sslmode=disable",
			PreferSimpleProtocol: true,
		})},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := &channelLogicalGroupDDLRecorder{}
			db, err := gorm.Open(test.dialector, &gorm.Config{
				DryRun: true, DisableAutomaticPing: true, Logger: recorder,
			})
			require.NoError(t, err)
			require.NoError(t, db.Migrator().CreateTable(channelLogicalGroupMigrationModels()...))

			ddl := strings.ToUpper(strings.Join(recorder.statements, "\n"))
			for _, table := range []string{
				"channels",
				"channel_logical_groups",
				"channel_logical_group_members",
				"channel_logical_smart_schedule_route_states",
				"channel_logical_smart_schedule_sample_states",
				"channel_status_probe_logical_configs",
				"channel_status_probe_logical_states",
				"channel_status_probe_executions",
				"channel_model_detection_logical_configs",
				"channel_model_detection_logical_targets",
				"channel_model_detection_runs",
				"channel_model_detection_executions",
			} {
				assert.Contains(t, ddl, strings.ToUpper(table), table)
			}
			for _, column := range []string{
				"logical_group_id",
				"address_fingerprint",
				"logical_channel_id",
				"logical_revision",
				"actual_channel_id",
				"logical_member_snapshot_json",
				"logical_target_id",
				"recovery_success_count",
				"recovery_success_at",
				"state_json",
			} {
				assert.Contains(t, ddl, strings.ToUpper(column), column)
			}
			assert.NotContains(t, ddl, "FOREIGN KEY", "logical-group relations intentionally avoid database foreign keys")
			assert.NotContains(t, ddl, "JSONB")
			assert.NotContains(t, ddl, "TIMESTAMPTZ")
			if test.name != "mysql57" {
				assert.NotContains(t, ddl, "UNSIGNED")
				assert.NotContains(t, ddl, "AUTO_INCREMENT")
			}
		})
	}
}

// TestChannelLogicalGroupSchemaMigrationConfiguredDatabases runs the same
// migration against opt-in MySQL 5.7.8+ and PostgreSQL 9.6+ instances. Keep
// this test skipped by default so local/unit runs do not require services; CI
// can provide TEST_MYSQL_DSN and TEST_POSTGRES_DSN to make the compatibility
// claim executable.
func TestChannelLogicalGroupSchemaMigrationConfiguredDatabases(t *testing.T) {
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

			// Isolate this run from a shared test database without relying on
			// database-specific CREATE DATABASE or schema permissions.
			config := &gorm.Config{NamingStrategy: schema.NamingStrategy{
				TablePrefix: fmt.Sprintf("lg%x_", time.Now().UnixNano()),
			}}
			db, err := gorm.Open(test.dialector(dsn), config)
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

			models := channelLogicalGroupMigrationModels()
			t.Cleanup(func() {
				for index := len(models) - 1; index >= 0; index-- {
					_ = db.Migrator().DropTable(models[index])
				}
			})

			prepareChannelLogicalGroupLegacySchema(t, db)
			require.NoError(t, db.AutoMigrate(models...))
			require.NoError(t, db.AutoMigrate(models...))
			assertChannelLogicalGroupMigration(t, db)

			group := &ChannelLogicalGroup{Name: "configured-db"}
			require.NoError(t, db.Create(group).Error)
			fingerprint := strings.Repeat("b", 64)
			require.NoError(t, db.Create(&ChannelLogicalGroupMember{LogicalGroupID: group.Id, ChannelID: 2001, Weight: 1, AddressFingerprint: fingerprint}).Error)
			duplicate := &ChannelLogicalGroupMember{LogicalGroupID: group.Id, ChannelID: 2001, Weight: 1, AddressFingerprint: fingerprint}
			require.Error(t, db.Create(duplicate).Error)

			updated := db.Model(&ChannelLogicalGroup{}).Where("id = ? AND revision = ?", group.Id, group.Revision).Update("revision", group.Revision+1)
			require.NoError(t, updated.Error)
			assert.EqualValues(t, 1, updated.RowsAffected)
			stale := db.Model(&ChannelLogicalGroup{}).Where("id = ? AND revision = ?", group.Id, group.Revision).Update("revision", group.Revision+2)
			require.NoError(t, stale.Error)
			assert.Zero(t, stale.RowsAffected)
		})
	}
}
