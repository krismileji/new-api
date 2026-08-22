package model

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestVerify01SnapshotDatabaseMatrix(t *testing.T) {
	requireVerify01Enabled(t)
	tests := []struct {
		name string
		env  string
		open func(string, string) (*gorm.DB, error)
	}{
		{
			name: "sqlite",
			open: func(_ string, prefix string) (*gorm.DB, error) {
				return gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "verify01-snapshot.db")), &gorm.Config{
					NamingStrategy: schema.NamingStrategy{TablePrefix: prefix},
				})
			},
		},
		{
			name: "mysql",
			env:  "TEST_MYSQL_DSN",
			open: func(dsn string, prefix string) (*gorm.DB, error) {
				return gorm.Open(mysql.Open(dsn), &gorm.Config{
					NamingStrategy: schema.NamingStrategy{TablePrefix: prefix},
				})
			},
		},
		{
			name: "postgres",
			env:  "TEST_POSTGRES_DSN",
			open: func(dsn string, prefix string) (*gorm.DB, error) {
				return gorm.Open(postgres.New(postgres.Config{
					DSN:                  dsn,
					PreferSimpleProtocol: true,
				}), &gorm.Config{
					NamingStrategy: schema.NamingStrategy{TablePrefix: prefix},
				})
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

			prefix := fmt.Sprintf("verify01_%010x_", uint64(time.Now().UnixNano())&0xffffffffff)
			db, err := test.open(dsn, prefix)
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)

			postgresSchema := ""
			if test.name == "postgres" {
				postgresSchema = strings.TrimSuffix(prefix, "_")
				require.NoError(t, db.Exec("CREATE SCHEMA "+postgresSchema).Error)
				require.NoError(t, db.Exec("SET search_path TO "+postgresSchema).Error)
			}

			originalDB := DB
			DB = db
			t.Cleanup(func() {
				DB = originalDB
				if test.name == "postgres" {
					assert.NoError(t, db.Exec("SET search_path TO public").Error)
					assert.NoError(t, db.Exec("DROP SCHEMA "+postgresSchema+" CASCADE").Error)
				} else {
					statement := &gorm.Statement{DB: db}
					parseErr := statement.Parse(&ChannelSmartScheduleExecutionDetail{})
					assert.NoError(t, parseErr)
					if parseErr == nil {
						safeToDrop := assert.True(t, strings.HasPrefix(statement.Schema.Table, "verify01_"))
						if safeToDrop {
							assert.NoError(t, db.Migrator().DropTable(&ChannelSmartScheduleExecutionDetail{}))
						}
					}
				}
				assert.NoError(t, sqlDB.Close())
			})

			require.NoError(t, db.AutoMigrate(&ChannelSmartScheduleExecutionDetail{}))
			require.NoError(t, db.AutoMigrate(&ChannelSmartScheduleExecutionDetail{}))
			statement := &gorm.Statement{DB: db}
			require.NoError(t, statement.Parse(&ChannelSmartScheduleExecutionDetail{}))
			assert.True(t, strings.HasPrefix(statement.Schema.Table, "verify01_"))

			inputs, expectedJSON := verify01SnapshotInputs(t, 5_000)
			currentTaskID := prefix + "current"
			require.NoError(t, SaveChannelSmartScheduleExecutionDetails(currentTaskID, inputs))
			loaded, err := GetChannelSmartScheduleExecutionDetails([]string{currentTaskID})
			require.NoError(t, err)
			require.Len(t, loaded[currentTaskID], len(inputs))
			loadedPayloads := make([]json.RawMessage, 0, len(loaded[currentTaskID]))
			for _, detail := range loaded[currentTaskID] {
				loadedPayloads = append(loadedPayloads, json.RawMessage(detail.Payload))
			}
			actualJSON, err := common.Marshal(loadedPayloads)
			require.NoError(t, err)
			assert.True(t, bytes.Equal(expectedJSON, actualJSON), "database JSON must round-trip byte-for-byte")

			var currentRow ChannelSmartScheduleExecutionDetail
			require.NoError(t, db.Where("task_id = ?", currentTaskID).First(&currentRow).Error)
			maxAllowedPacket := int64(0)
			if test.name == "mysql" {
				require.NoError(t, db.Raw("SELECT @@max_allowed_packet").Scan(&maxAllowedPacket).Error)
				assert.Less(t, int64(len(currentRow.PayloadBlob)), maxAllowedPacket)
			}

			now := common.GetTimestamp()
			threeDayCutoff := now - int64((72*time.Hour)/time.Second)
			oldTaskID := prefix + "expired"
			oldRow, err := channelSmartScheduleExecutionDetailSnapshot(
				oldTaskID,
				inputs[:120],
				threeDayCutoff-1,
			)
			require.NoError(t, err)
			require.NoError(t, db.Create(oldRow).Error)

			cleanupResult, err := DeleteChannelMonitorHistoryBefore(
				context.Background(),
				ChannelMonitorHistoryRetentionCutoffs{
					ExecutionDetail: threeDayCutoff,
					Task:            threeDayCutoff,
					RatioHistory:    threeDayCutoff,
				},
				[]string{"channel_smart_schedule"},
				64,
				ChannelMonitorCleanupBudget{},
			)
			require.NoError(t, err)
			assert.Equal(t, int64(1), cleanupResult.ExecutionDetailRowsDeleted)
			assert.False(t, cleanupResult.Incomplete)

			var remainingTaskIDs []string
			require.NoError(t, db.Model(&ChannelSmartScheduleExecutionDetail{}).
				Order("task_id ASC").
				Pluck("task_id", &remainingTaskIDs).Error)
			assert.Equal(t, []string{currentTaskID}, remainingTaskIDs)
			t.Logf(
				"VERIFY01_DATABASE database=%s table=%s adjustments=%d raw_bytes=%d gzip_bytes=%d max_allowed_packet=%d cleanup_deleted=%d",
				test.name,
				statement.Schema.Table,
				len(inputs),
				len(expectedJSON),
				len(currentRow.PayloadBlob),
				maxAllowedPacket,
				cleanupResult.ExecutionDetailRowsDeleted,
			)
		})
	}
}
