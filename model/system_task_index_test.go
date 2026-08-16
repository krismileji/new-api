package model

import (
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
	"gorm.io/gorm/schema"
)

func TestSystemTaskCompositeIndexMigrationAndQueries(t *testing.T) {
	tests := []struct {
		name string
		env  string
		open func(string) (*gorm.DB, error)
	}{
		{
			name: "sqlite",
			open: func(_ string) (*gorm.DB, error) {
				return gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "system-task-index.db")), &gorm.Config{})
			},
		},
		{
			name: "mysql",
			env:  "TEST_MYSQL_DSN",
			open: func(dsn string) (*gorm.DB, error) {
				prefix := fmt.Sprintf("idx02%x_", time.Now().UnixNano())
				return gorm.Open(mysql.Open(dsn), &gorm.Config{
					NamingStrategy: schema.NamingStrategy{TablePrefix: prefix},
				})
			},
		},
		{
			name: "postgres",
			env:  "TEST_POSTGRES_DSN",
			open: func(dsn string) (*gorm.DB, error) {
				return gorm.Open(postgres.New(postgres.Config{
					DSN:                  dsn,
					PreferSimpleProtocol: true,
				}), &gorm.Config{})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := ""
			if test.env != "" {
				dsn = strings.TrimSpace(os.Getenv(test.env))
				if dsn == "" {
					t.Skip(test.env + " is not configured")
				}
			}

			db, err := test.open(dsn)
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

			if test.name == "postgres" {
				schemaName := fmt.Sprintf("idx02%x", time.Now().UnixNano())
				require.NoError(t, db.Exec("CREATE SCHEMA "+schemaName).Error)
				require.NoError(t, db.Exec("SET search_path TO "+schemaName).Error)
				t.Cleanup(func() {
					require.NoError(t, db.Exec("SET search_path TO public").Error)
					require.NoError(t, db.Exec("DROP SCHEMA "+schemaName+" CASCADE").Error)
				})
			} else {
				t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(&SystemTask{})) })
			}

			require.NoError(t, db.AutoMigrate(&SystemTask{}))
			assert.True(t, db.Migrator().HasIndex(&SystemTask{}, "idx_system_tasks_type_status_id"))

			statement := &gorm.Statement{DB: db}
			require.NoError(t, statement.Parse(&SystemTask{}))
			tableName := statement.Schema.Table
			var compositeColumns []string
			switch test.name {
			case "sqlite":
				require.NoError(t, db.Raw(
					"SELECT name FROM pragma_index_info(?) ORDER BY seqno",
					"idx_system_tasks_type_status_id",
				).Scan(&compositeColumns).Error)
			case "mysql":
				require.NoError(t, db.Raw(
					"SELECT column_name FROM information_schema.statistics "+
						"WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ? "+
						"ORDER BY seq_in_index",
					tableName,
					"idx_system_tasks_type_status_id",
				).Scan(&compositeColumns).Error)
			case "postgres":
				require.NoError(t, db.Raw(
					"SELECT attribute.attname "+
						"FROM pg_class table_class "+
						"JOIN pg_namespace namespace ON namespace.oid = table_class.relnamespace "+
						"JOIN pg_index index_definition ON index_definition.indrelid = table_class.oid "+
						"JOIN pg_class index_class ON index_class.oid = index_definition.indexrelid "+
						"CROSS JOIN LATERAL unnest(index_definition.indkey) WITH ORDINALITY AS indexed(attnum, position) "+
						"JOIN pg_attribute attribute ON attribute.attrelid = table_class.oid AND attribute.attnum = indexed.attnum "+
						"WHERE namespace.nspname = current_schema() AND table_class.relname = ? AND index_class.relname = ? "+
						"ORDER BY indexed.position",
					tableName,
					"idx_system_tasks_type_status_id",
				).Scan(&compositeColumns).Error)
			}
			assert.Equal(t, []string{"type", "status", "id"}, compositeColumns)
			require.NoError(t, db.AutoMigrate(&SystemTask{}))

			tasks := []SystemTask{
				{ID: 1, TaskID: "idx02-a-succeeded", Type: "type_a", Status: SystemTaskStatusSucceeded, CreatedAt: 1, UpdatedAt: 10},
				{ID: 2, TaskID: "idx02-a-pending-1", Type: "type_a", Status: SystemTaskStatusPending, CreatedAt: 2, UpdatedAt: 20},
				{ID: 3, TaskID: "idx02-a-pending-2", Type: "type_a", Status: SystemTaskStatusPending, CreatedAt: 3, UpdatedAt: 30},
				{ID: 4, TaskID: "idx02-a-running", Type: "type_a", Status: SystemTaskStatusRunning, CreatedAt: 4, UpdatedAt: 40},
				{ID: 5, TaskID: "idx02-a-failed", Type: "type_a", Status: SystemTaskStatusFailed, CreatedAt: 5, UpdatedAt: 5},
				{ID: 6, TaskID: "idx02-b-pending", Type: "type_b", Status: SystemTaskStatusPending, CreatedAt: 6, UpdatedAt: 25},
				{ID: 7, TaskID: "idx02-b-succeeded", Type: "type_b", Status: SystemTaskStatusSucceeded, CreatedAt: 7, UpdatedAt: 15},
			}
			require.NoError(t, db.Create(&tasks).Error)

			var active SystemTask
			require.NoError(t, db.Where("type = ? AND status IN ?", "type_a", []SystemTaskStatus{
				SystemTaskStatusPending,
				SystemTaskStatusRunning,
			}).Order("id DESC").First(&active).Error)
			assert.Equal(t, int64(4), active.ID)

			var pending []SystemTask
			require.NoError(t, db.Where("type = ? AND status = ?", "type_a", SystemTaskStatusPending).
				Order("id ASC").Limit(2).Find(&pending).Error)
			assert.Equal(t, []int64{2, 3}, []int64{pending[0].ID, pending[1].ID})

			subQuery := db.Model(&SystemTask{}).
				Select("MIN(id)").
				Where("type IN ? AND status = ?", []string{"type_a", "type_b"}, SystemTaskStatusPending).
				Group("type")
			var earliest []SystemTask
			require.NoError(t, db.Where("id IN (?)", subQuery).Order("id ASC").Find(&earliest).Error)
			assert.Equal(t, []int64{2, 6}, []int64{earliest[0].ID, earliest[1].ID})

			var listed []SystemTask
			require.NoError(t, db.Order("id DESC").Limit(3).Find(&listed).Error)
			assert.Equal(t, []int64{7, 6, 5}, []int64{listed[0].ID, listed[1].ID, listed[2].ID})

			var latest SystemTask
			require.NoError(t, db.Where("type = ?", "type_a").Order("id DESC").First(&latest).Error)
			assert.Equal(t, int64(5), latest.ID)

			var cleanupCandidates []SystemTask
			require.NoError(t, db.Where("type IN ?", []string{"type_a", "type_b"}).
				Where("status IN ?", []SystemTaskStatus{SystemTaskStatusSucceeded, SystemTaskStatusFailed}).
				Where("updated_at < ?", int64(20)).
				Order("updated_at ASC, id ASC").
				Limit(10).
				Find(&cleanupCandidates).Error)
			assert.Equal(t, []int64{5, 1, 7}, []int64{
				cleanupCandidates[0].ID,
				cleanupCandidates[1].ID,
				cleanupCandidates[2].ID,
			})

			var claimed SystemTask
			require.NoError(t, db.Where("id = ? AND type = ? AND status = ?", 2, "type_a", SystemTaskStatusPending).
				First(&claimed).Error)
			result := db.Model(&SystemTask{}).
				Where("id = ? AND type = ? AND status = ?", 2, "type_a", SystemTaskStatusPending).
				Updates(map[string]any{"status": SystemTaskStatusRunning, "locked_by": "idx02-runner"})
			require.NoError(t, result.Error)
			assert.Equal(t, int64(1), result.RowsAffected)

			staleResult := db.Model(&SystemTask{}).
				Where("id = ? AND type = ? AND status = ?", 2, "type_a", SystemTaskStatusPending).
				Update("locked_by", "stale-runner")
			require.NoError(t, staleResult.Error)
			assert.Equal(t, int64(0), staleResult.RowsAffected)
		})
	}
}
