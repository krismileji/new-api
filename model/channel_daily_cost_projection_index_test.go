package model

import (
	"bytes"
	"context"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Frozen from 39485b5be, matching the deployed single-column projection index.
type channelDailyCostProjectionLegacyOutbox struct {
	Id                        int64  `gorm:"primaryKey"`
	EventId                   string `gorm:"size:64;not null;uniqueIndex"`
	ChannelId                 int    `gorm:"not null;index:idx_channel_daily_cost_outbox_pending,priority:2"`
	OccurredAt                int64  `gorm:"not null"`
	CostNanoCNY               int64  `gorm:"not null"`
	ProbeCostNanoCNY          int64  `gorm:"not null"`
	GroupProbeCostNanoCNY     int64  `gorm:"not null"`
	SettledDelta              int64  `gorm:"not null"`
	UnresolvedDelta           int64  `gorm:"not null"`
	APIKeyId                  int    `gorm:"not null"`
	APIKeyName                string `gorm:"size:255;not null"`
	KeyFingerprint            string `gorm:"size:64;not null"`
	KeyDisplay                string `gorm:"size:64;not null"`
	UserId                    int    `gorm:"not null;default:0"`
	UserAttribution           string `gorm:"size:16;not null;default:''"`
	ModelName                 string `gorm:"size:255;not null;default:''"`
	SourceKind                string `gorm:"size:32;not null;default:''"`
	ProjectionEventId         string `gorm:"size:128;not null;default:''"`
	ModelDetectionCostNanoCNY int64  `gorm:"not null;default:0"`
	RedisProjectedAt          int64  `gorm:"not null;default:0;index:idx_channel_daily_cost_outbox_projection"`
	AttemptCount              int64  `gorm:"not null"`
	NextAttemptAt             int64  `gorm:"not null;index:idx_channel_daily_cost_outbox_pending,priority:3"`
	LeaseOwner                string `gorm:"size:128;not null;index"`
	LeaseUntil                int64  `gorm:"not null;index:idx_channel_daily_cost_outbox_pending,priority:4"`
	ProcessedAt               int64  `gorm:"not null;index:idx_channel_daily_cost_outbox_pending,priority:1"`
	LastError                 string `gorm:"size:512;not null"`
	CreatedAt                 int64  `gorm:"not null"`
	UpdatedAt                 int64  `gorm:"not null"`
}

func (channelDailyCostProjectionLegacyOutbox) TableName() string {
	return "channel_daily_cost_outboxes"
}

// The upstream v1.0.0-rc.35 release has options but no downstream cost outbox.
type channelDailyCostProjectionReleaseOption struct {
	Key   string `gorm:"primaryKey"`
	Value string
}

func (channelDailyCostProjectionReleaseOption) TableName() string { return "options" }

type channelDailyCostProjectionIndexDefinition struct {
	Columns []string
	Unique  bool
}

func channelDailyCostProjectionIndexes(t *testing.T, db *gorm.DB) map[string]channelDailyCostProjectionIndexDefinition {
	t.Helper()
	definitions := make(map[string]channelDailyCostProjectionIndexDefinition)
	if db.Dialector.Name() == "sqlite" {
		var indexes []struct {
			Name   string
			Unique bool
		}
		require.NoError(t, db.Raw("SELECT name, \"unique\" FROM pragma_index_list(?)", "channel_daily_cost_outboxes").Scan(&indexes).Error)
		for _, index := range indexes {
			var columns []string
			require.NoError(t, db.Raw("SELECT name FROM pragma_index_info(?) ORDER BY seqno", index.Name).Scan(&columns).Error)
			definitions[index.Name] = channelDailyCostProjectionIndexDefinition{Columns: columns, Unique: index.Unique}
		}
		return definitions
	}
	if db.Dialector.Name() == "postgres" {
		// This driver's GetIndexes orders columns by table position, not index position.
		var columns []struct {
			Name       string
			ColumnName string
			IsUnique   bool
		}
		require.NoError(t, db.Raw(`SELECT idx.relname AS name, a.attname AS column_name, i.indisunique AS is_unique
FROM pg_index i
JOIN pg_class tbl ON tbl.oid = i.indrelid
JOIN pg_namespace ns ON ns.oid = tbl.relnamespace
JOIN pg_class idx ON idx.oid = i.indexrelid
CROSS JOIN LATERAL unnest(i.indkey::smallint[]) WITH ORDINALITY AS key(attnum, position)
JOIN pg_attribute a ON a.attrelid = tbl.oid AND a.attnum = key.attnum
WHERE ns.nspname = current_schema() AND tbl.relname = ?
ORDER BY idx.relname, key.position`, "channel_daily_cost_outboxes").Scan(&columns).Error)
		for _, column := range columns {
			definition := definitions[column.Name]
			definition.Columns = append(definition.Columns, column.ColumnName)
			definition.Unique = column.IsUnique
			definitions[column.Name] = definition
		}
		return definitions
	}
	indexes, err := db.Migrator().GetIndexes(&ChannelDailyCostOutbox{})
	require.NoError(t, err)
	for _, index := range indexes {
		unique, _ := index.Unique()
		definitions[index.Name()] = channelDailyCostProjectionIndexDefinition{Columns: index.Columns(), Unique: unique}
	}
	return definitions
}

func openChannelDailyCostProjectionIndexDB(t *testing.T, engine string) (*gorm.DB, *bytes.Buffer) {
	t.Helper()
	var dialector gorm.Dialector
	switch engine {
	case "sqlite":
		dialector = sqlite.Open(filepath.Join(t.TempDir(), "projection-index.db"))
	case "mysql":
		dsn := os.Getenv("TEST_COST_INDEX_MYSQL_DSN")
		if dsn == "" {
			t.Skip("设置 TEST_COST_INDEX_MYSQL_DSN 验证真实 MySQL")
		}
		config, err := mysqlDriver.ParseDSN(dsn)
		require.NoError(t, err)
		require.Equal(t, "new_api_projection_index_test", config.DBName)
		require.Equal(t, "tcp", config.Net)
		require.True(t, strings.HasPrefix(config.Addr, "127.0.0.1:"))
		dialector = mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_COST_INDEX_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("设置 TEST_COST_INDEX_POSTGRES_DSN 验证真实 PostgreSQL")
		}
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		require.Equal(t, "127.0.0.1", parsed.Hostname())
		require.Equal(t, "/new_api_projection_index_test", parsed.Path)
		dialector = postgres.Open(dsn)
	default:
		t.Fatalf("未知数据库类型 %s", engine)
	}
	statements := new(bytes.Buffer)
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.New(log.New(statements, "", 0), logger.Config{LogLevel: logger.Info})})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	require.False(t, db.Migrator().HasTable(&ChannelDailyCostOutbox{}), "必须使用独立空测试数据库")
	require.False(t, db.Migrator().HasTable(&channelDailyCostProjectionReleaseOption{}), "必须使用独立空测试数据库")
	t.Cleanup(func() {
		assert.NoError(t, db.Migrator().DropTable(&ChannelDailyCostOutbox{}, &channelDailyCostProjectionReleaseOption{}))
	})
	var version string
	versionQuery := "SELECT version()"
	if engine == "sqlite" {
		versionQuery = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("database=%s version=%s", engine, version)
	previousDB, previousType := DB, common.MainDatabaseType()
	DB = db
	common.SetMainDatabaseType(common.DatabaseType(engine))
	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousType)
	})
	return db, statements
}

func TestChannelDailyCostProjectionIndexMigration(t *testing.T) {
	const indexName = "idx_channel_daily_cost_outbox_projection_time"
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			for _, initialSchema := range []string{"fresh", "upstream-release", "downstream-upgrade", "operator-index"} {
				t.Run(initialSchema, func(t *testing.T) {
					db, statements := openChannelDailyCostProjectionIndexDB(t, engine)
					if initialSchema == "upstream-release" {
						require.NoError(t, db.AutoMigrate(&channelDailyCostProjectionReleaseOption{}))
						require.NoError(t, db.Create(&channelDailyCostProjectionReleaseOption{Key: "SystemName", Value: "new-api"}).Error)
					}
					if initialSchema == "downstream-upgrade" || initialSchema == "operator-index" {
						require.NoError(t, db.AutoMigrate(&channelDailyCostProjectionLegacyOutbox{}))
					} else {
						require.NoError(t, db.AutoMigrate(&ChannelDailyCostOutbox{}))
					}
					if initialSchema == "operator-index" {
						statement := "CREATE INDEX idx_channel_daily_cost_outbox_projection_time ON channel_daily_cost_outboxes (redis_projected_at, occurred_at)"
						if engine == "mysql" {
							statement += " ALGORITHM=INPLACE LOCK=NONE"
						}
						require.NoError(t, db.Exec(statement).Error)
					}
					now := time.Now().Unix()
					retainedFrom := ChannelDailyCostDayStart(now) - 86400
					rows := []ChannelDailyCostOutbox{
						{EventId: "expired", ChannelId: 7, OccurredAt: retainedFrom - 1, CostNanoCNY: 101},
						{EventId: "boundary", ChannelId: 7, OccurredAt: retainedFrom, CostNanoCNY: 202, ProjectionEventId: "task-7", APIKeyId: 8, APIKeyName: "生产 Key", UserId: 9},
						{EventId: "delivered", ChannelId: 7, OccurredAt: now, CostNanoCNY: 303, RedisProjectedAt: now, ProcessedAt: now},
						{EventId: "leased", ChannelId: 7, OccurredAt: now, CostNanoCNY: 404, AttemptCount: 2, NextAttemptAt: now + 40, LeaseOwner: "worker", LeaseUntil: now + 30, LastError: "temporary failure"},
						{EventId: "late-arrival", ChannelId: 7, OccurredAt: retainedFrom + 1, CostNanoCNY: 505, CreatedAt: now, UpdatedAt: now},
					}
					require.NoError(t, db.Create(&rows).Error)
					indexesBefore := channelDailyCostProjectionIndexes(t, db)
					if engine == "sqlite" && initialSchema == "downstream-upgrade" {
						statements.Reset()
						require.NoError(t, db.AutoMigrate(&channelDailyCostProjectionLegacyOutbox{}))
						t.Logf("旧模型重复迁移是否重建表：%t", strings.Contains(statements.String(), "DROP TABLE"))
						assert.Equal(t, indexesBefore, channelDailyCostProjectionIndexes(t, db))
					}
					for pass := 0; pass < 2; pass++ {
						statements.Reset()
						require.NoError(t, db.AutoMigrate(&ChannelDailyCostOutbox{}))
						// The existing SQLite driver rebuilds this table for its unique index.
						// Check final schema/data below; MySQL and PostgreSQL must also avoid DDL.
						if engine != "sqlite" && (pass > 0 || initialSchema == "operator-index") {
							assert.NotRegexp(t, `(?i)\b(?:CREATE|ALTER|DROP)\s+(?:TABLE|INDEX|UNIQUE)\b`, statements.String(), "重复启动和已有运维索引不应触发 DDL")
						}
						indexesAfter := channelDailyCostProjectionIndexes(t, db)
						require.Contains(t, indexesAfter, indexName)
						assert.Equal(t, channelDailyCostProjectionIndexDefinition{Columns: []string{"redis_projected_at", "occurred_at"}}, indexesAfter[indexName])
						for name, definition := range indexesBefore {
							assert.Equal(t, definition, indexesAfter[name], "已有索引 %s 必须保留", name)
						}
						expectedCount := len(indexesBefore)
						if _, existed := indexesBefore[indexName]; !existed {
							expectedCount++
						}
						assert.Len(t, indexesAfter, expectedCount)
						var stored []ChannelDailyCostOutbox
						require.NoError(t, db.Order("id ASC").Find(&stored).Error)
						assert.Equal(t, rows, stored, "迁移不得改变金额、投影状态、租约、重试或身份信息")
					}
					pending, err := PendingChannelDailyCostProjections(context.Background(), 2)
					require.NoError(t, err)
					assert.Equal(t, []ChannelDailyCostOutbox{rows[1], rows[3]}, pending, "按 ID 分批，仅投递保留范围内未完成的事件")
					require.NoError(t, MarkChannelDailyCostProjectionsApplied(context.Background(), []int64{rows[1].Id, rows[3].Id}, now))
					pending, err = PendingChannelDailyCostProjections(context.Background(), 2)
					require.NoError(t, err)
					assert.Equal(t, []ChannelDailyCostOutbox{rows[4]}, pending, "确认后继续下一批，已完成和过期事件不重复投递")
					duplicate := rows[0]
					duplicate.Id = 0
					require.Error(t, db.Create(&duplicate).Error, "事件 ID 唯一约束必须保留")
					if initialSchema == "upstream-release" {
						var options []channelDailyCostProjectionReleaseOption
						require.NoError(t, db.Find(&options).Error)
						assert.Equal(t, []channelDailyCostProjectionReleaseOption{{Key: "SystemName", Value: "new-api"}}, options)
					}
				})
			}
		})
	}
}
