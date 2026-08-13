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

type channelModelDetectionMigrationSQLRecorder struct {
	statements []string
}

func (recorder *channelModelDetectionMigrationSQLRecorder) LogMode(_ logger.LogLevel) logger.Interface {
	return recorder
}

func (recorder *channelModelDetectionMigrationSQLRecorder) Info(context.Context, string, ...any)  {}
func (recorder *channelModelDetectionMigrationSQLRecorder) Warn(context.Context, string, ...any)  {}
func (recorder *channelModelDetectionMigrationSQLRecorder) Error(context.Context, string, ...any) {}

func (recorder *channelModelDetectionMigrationSQLRecorder) Trace(
	_ context.Context,
	_ time.Time,
	sql func() (string, int64),
	_ error,
) {
	statement, _ := sql()
	recorder.statements = append(recorder.statements, statement)
}

func TestChannelModelDetectionSchemaMigrationSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-model-detection-migration.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	models := []any{
		&ChannelModelDetectionGlobalConfig{},
		&ChannelModelDetectionConfig{},
		&ChannelModelDetectionTarget{},
		&ChannelModelDetectionBatch{},
		&ChannelModelDetectionRun{},
		&ChannelModelDetectionExecution{},
		&ChannelModelDetectionCostEvent{},
	}
	require.NoError(t, db.AutoMigrate(models...))

	for _, model := range models {
		assert.True(t, db.Migrator().HasTable(model))
	}
	for _, index := range []struct {
		model any
		name  string
	}{
		{&ChannelModelDetectionConfig{}, "idx_channel_model_detection_configs_channel_id"},
		{&ChannelModelDetectionTarget{}, "idx_channel_model_detection_target_identity"},
		{&ChannelModelDetectionBatch{}, "idx_channel_model_detection_batches_batch_id"},
		{&ChannelModelDetectionBatch{}, "idx_channel_model_detection_batch_schedule"},
		{&ChannelModelDetectionRun{}, "idx_channel_model_detection_runs_run_id"},
		{&ChannelModelDetectionRun{}, "idx_channel_model_detection_run_channel_created"},
		{&ChannelModelDetectionExecution{}, "idx_channel_model_detection_execution_target"},
		{&ChannelModelDetectionCostEvent{}, "idx_channel_model_detection_cost_events_cost_event_id"},
		{&ChannelModelDetectionCostEvent{}, "idx_channel_model_detection_cost_attempt"},
	} {
		assert.True(t, db.Migrator().HasIndex(index.model, index.name), index.name)
	}

	columnTypes, err := db.Migrator().ColumnTypes(&ChannelModelDetectionExecution{})
	require.NoError(t, err)
	textColumns := map[string]bool{
		"official_config_json": false,
		"report_json":          false,
	}
	for _, columnType := range columnTypes {
		if _, ok := textColumns[columnType.Name()]; !ok {
			continue
		}
		textColumns[columnType.Name()] = true
		assert.Equal(t, "text", strings.ToLower(columnType.DatabaseTypeName()), columnType.Name())
	}
	for column, found := range textColumns {
		assert.True(t, found, column)
	}

	// A second startup must be able to run the same migration without error.
	require.NoError(t, db.AutoMigrate(models...))
}

func TestChannelModelDetectionSchemaMigrationDialectDDL(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
		quote     string
		forbidden []string
	}{
		{
			name: "mysql57",
			dialector: mysql.New(mysql.Config{
				DSN:                       "new_api:test@tcp(127.0.0.1:3306)/new_api?charset=utf8mb4&parseTime=True&loc=Local",
				SkipInitializeWithVersion: true,
			}),
			quote:     "`",
			forbidden: []string{"JSONB", "SERIAL", "TIMESTAMPTZ", `"`},
		},
		{
			name: "postgres96",
			dialector: postgres.New(postgres.Config{
				DSN:                  "host=127.0.0.1 user=new_api password=test dbname=new_api port=5432 sslmode=disable",
				PreferSimpleProtocol: true,
			}),
			quote:     `"`,
			forbidden: []string{"`", "UNSIGNED", "AUTO_INCREMENT", "DATETIME"},
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
			require.NoError(t, db.Migrator().CreateTable(
				&ChannelModelDetectionGlobalConfig{},
				&ChannelModelDetectionConfig{},
				&ChannelModelDetectionTarget{},
				&ChannelModelDetectionBatch{},
				&ChannelModelDetectionRun{},
				&ChannelModelDetectionExecution{},
				&ChannelModelDetectionCostEvent{},
			))

			ddl := strings.ToUpper(strings.Join(recorder.statements, "\n"))
			assert.Equal(t, 7, strings.Count(ddl, "CREATE TABLE "), "all model-detection tables must be emitted")
			for _, table := range []string{
				"channel_model_detection_global_configs",
				"channel_model_detection_configs",
				"channel_model_detection_targets",
				"channel_model_detection_batches",
				"channel_model_detection_runs",
				"channel_model_detection_executions",
				"channel_model_detection_cost_events",
			} {
				assert.Contains(t, ddl, strings.ToUpper("CREATE TABLE "+test.quote+table+test.quote), table)
			}
			for _, index := range []string{
				"idx_channel_model_detection_configs_channel_id",
				"idx_channel_model_detection_target_identity",
				"idx_channel_model_detection_batches_batch_id",
				"idx_channel_model_detection_batch_schedule",
				"idx_channel_model_detection_runs_run_id",
				"idx_channel_model_detection_run_channel_created",
				"idx_channel_model_detection_execution_target",
				"idx_channel_model_detection_cost_events_cost_event_id",
				"idx_channel_model_detection_cost_attempt",
			} {
				assert.Contains(t, ddl, strings.ToUpper(test.quote+index+test.quote), index)
			}

			for _, fragment := range []string{
				test.quote + "detector_url" + test.quote + " VARCHAR(1024)",
				test.quote + "batch_id" + test.quote + " VARCHAR(64)",
				test.quote + "estimated_quota" + test.quote + " BIGINT",
				test.quote + "official_config_json" + test.quote + " TEXT",
				test.quote + "report_json" + test.quote + " TEXT",
			} {
				assert.Contains(t, ddl, strings.ToUpper(fragment), fragment)
			}
			for _, forbidden := range test.forbidden {
				assert.NotContains(t, ddl, forbidden, forbidden)
			}
			duplicateTargetKey := strings.ToUpper(test.quote + "target_key" + test.quote + "," + test.quote + "target_key" + test.quote)
			assert.NotContains(t, ddl, duplicateTargetKey)
		})
	}
}

func TestChannelModelDetectionSchemaMigrationConfiguredDatabases(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		dialector func(string) gorm.Dialector
	}{
		{name: "mysql57", env: "TEST_MYSQL_DSN", dialector: func(dsn string) gorm.Dialector { return mysql.Open(dsn) }},
		{name: "postgres96", env: "TEST_POSTGRES_DSN", dialector: func(dsn string) gorm.Dialector {
			return postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
		}},
	}
	models := []any{
		&ChannelModelDetectionGlobalConfig{},
		&ChannelModelDetectionConfig{},
		&ChannelModelDetectionTarget{},
		&ChannelModelDetectionBatch{},
		&ChannelModelDetectionRun{},
		&ChannelModelDetectionExecution{},
		&ChannelModelDetectionCostEvent{},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := strings.TrimSpace(os.Getenv(test.env))
			if dsn == "" {
				t.Skip(test.env + " is not configured")
			}
			config := &gorm.Config{}
			if test.name == "mysql57" {
				config.NamingStrategy = schema.NamingStrategy{TablePrefix: fmt.Sprintf("m%x_", time.Now().UnixNano())}
			}
			db, err := gorm.Open(test.dialector(dsn), config)
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			if test.name == "postgres96" {
				schemaName := fmt.Sprintf("md%x", time.Now().UnixNano())
				require.NoError(t, db.Exec("CREATE SCHEMA "+schemaName).Error)
				require.NoError(t, db.Exec("SET search_path TO "+schemaName).Error)
				t.Cleanup(func() {
					require.NoError(t, db.Exec("SET search_path TO public").Error)
					require.NoError(t, db.Exec("DROP SCHEMA "+schemaName+" CASCADE").Error)
				})
			}

			t.Cleanup(func() {
				if test.name == "mysql57" {
					for index := len(models) - 1; index >= 0; index-- {
						_ = db.Migrator().DropTable(models[index])
					}
				}
			})

			require.NoError(t, db.AutoMigrate(models...))
			for _, model := range models {
				assert.True(t, db.Migrator().HasTable(model))
			}
			require.NoError(t, db.AutoMigrate(models...))
			for _, model := range models {
				assert.True(t, db.Migrator().HasTable(model))
			}
		})
	}
}
