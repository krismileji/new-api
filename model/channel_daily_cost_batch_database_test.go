package model

import (
	"context"
	"database/sql"
	"fmt"
	"math"
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

type channelDailyCostBatchSQLRecorder struct {
	logger.Interface
	statements int
}

// Simulate loss of the commit acknowledgment after the real database has
// committed. Retrying the same outbox IDs must not increment any ledger again.
type channelDailyCostCommitAckLossPool struct {
	*sql.DB
}

func (pool channelDailyCostCommitAckLossPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := pool.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &channelDailyCostCommitAckLossTx{tx}, nil
}

type channelDailyCostCommitAckLossTx struct {
	*sql.Tx
}

func (tx channelDailyCostCommitAckLossTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	return context.DeadlineExceeded
}

func (recorder *channelDailyCostBatchSQLRecorder) Trace(_ context.Context, _ time.Time, _ func() (string, int64), _ error) {
	recorder.statements++
}

func setupChannelDailyCostBatchDatabase(t *testing.T, engine string) *gorm.DB {
	t.Helper()
	var dialector gorm.Dialector
	versionQuery := "SELECT version()"
	switch engine {
	case "sqlite":
		dialector = sqlite.Open(filepath.Join(t.TempDir(), "cost-batch.db"))
		versionQuery = "SELECT sqlite_version()"
	case "mysql":
		dsn := os.Getenv("TEST_COST_BACKLOG_MYSQL_DSN")
		if dsn == "" {
			t.Skip("设置 TEST_COST_BACKLOG_MYSQL_DSN 验证真实 MySQL")
		}
		config, err := mysqlDriver.ParseDSN(dsn)
		require.NoError(t, err)
		require.Equal(t, "new_api_cost_backlog_test", config.DBName)
		require.Equal(t, "tcp", config.Net)
		require.True(t, strings.HasPrefix(config.Addr, "127.0.0.1:"))
		dialector = mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_COST_BACKLOG_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("设置 TEST_COST_BACKLOG_POSTGRES_DSN 验证真实 PostgreSQL")
		}
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		require.Equal(t, "127.0.0.1", parsed.Hostname())
		require.Equal(t, "/new_api_cost_backlog_test", parsed.Path)
		dialector = postgres.Open(dsn)
	default:
		t.Fatalf("未知验证数据库: %s", engine)
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	tables := []any{&ChannelDailyCost{}, &ChannelDailyAPIKeyCost{}, &ChannelDailyCostOutbox{}, &ChannelMonitorDailyCostDetail{}}
	for _, table := range tables {
		require.False(t, db.Migrator().HasTable(table), "验证必须使用空的独立测试数据库")
	}
	t.Cleanup(func() {
		for i := len(tables) - 1; i >= 0; i-- {
			assert.NoError(t, db.Migrator().DropTable(tables[i]))
		}
	})
	require.NoError(t, db.AutoMigrate(tables...))
	previousDB, previousType := DB, common.MainDatabaseType()
	DB = db
	common.SetMainDatabaseType(common.DatabaseType(engine))
	t.Cleanup(func() { DB = previousDB; common.SetMainDatabaseType(previousType) })
	var version string
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("database=%s version=%s", engine, version)
	return db
}

func TestChannelDailyCostBatchDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			t.Run("metadata_and_dimensions", func(t *testing.T) { verifyChannelDailyCostBatchMetadata(t, db) })
			t.Run("rollback_and_takeover", func(t *testing.T) { verifyChannelDailyCostBatchRecovery(t, db) })
			t.Run("cancel_before_commit", func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				delta := ChannelDailyCostDelta{EventId: "cancel-before-commit", ChannelId: 930, OccurredAt: 1_750_000_000, CostNanoCNY: 45, SettledDelta: 1}
				require.NoError(t, StoreChannelDailyCostOutboxEvents(ctx, []ChannelDailyCostDelta{delta}))
				now := time.Now().Unix()
				claimed, err := ClaimChannelDailyCostOutboxEvents(ctx, "cancel-worker", now, now, time.Minute, 1)
				require.NoError(t, err)
				require.Len(t, claimed, 1)
				require.NoError(t, db.Callback().Update().After("gorm:update").Register("test:cancel_before_commit", func(tx *gorm.DB) {
					if tx.Statement.Table == "channel_daily_cost_outboxes" {
						cancel()
					}
				}))
				applied, err := ApplyClaimedChannelDailyCostOutboxEventsWithResult(ctx, "cancel-worker", []int64{claimed[0].Id}, now)
				require.NoError(t, db.Callback().Update().Remove("test:cancel_before_commit"))
				require.Error(t, err)
				assert.Zero(t, applied, "完成标记更新后、事务提交前取消，不能报告记账成功")
				var count int64
				require.NoError(t, db.Model(&ChannelDailyCost{}).Where("channel_id = ?", 930).Count(&count).Error)
				assert.Zero(t, count)
				applied, err = ApplyClaimedChannelDailyCostOutboxEventsWithResult(context.Background(), "cancel-worker", []int64{claimed[0].Id}, now)
				require.NoError(t, err)
				assert.EqualValues(t, 1, applied)
			})
			t.Run("commit_ack_lost", func(t *testing.T) {
				fingerprint, display := ChannelDailyCostAPIKeyIdentityForToken(23, "commit-ack-test")
				delta := ChannelDailyCostDelta{EventId: "commit-ack-lost", ChannelId: 931, OccurredAt: 1_750_000_000, CostNanoCNY: 55, SettledDelta: 1,
					APIKeyId: 23, KeyFingerprint: fingerprint, KeyDisplay: display}
				require.NoError(t, StoreChannelDailyCostOutboxEvents(context.Background(), []ChannelDailyCostDelta{delta}))
				now := time.Now().Unix()
				claimed, err := ClaimChannelDailyCostOutboxEvents(context.Background(), "commit-ack-worker", now, now, time.Minute, 1)
				require.NoError(t, err)
				require.Len(t, claimed, 1)
				sqlDB, err := db.DB()
				require.NoError(t, err)
				ackLossDB := db.WithContext(context.Background())
				ackLossDB.Statement.ConnPool = &channelDailyCostCommitAckLossPool{sqlDB}
				defer func() { DB = db }()
				DB = ackLossDB
				applied, err := ApplyClaimedChannelDailyCostOutboxEventsWithResult(context.Background(), "commit-ack-worker", []int64{claimed[0].Id}, now)
				DB = db
				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.Zero(t, applied, "提交结果不确定时不能报告已确认的成功数")
				var row ChannelDailyCostOutbox
				require.NoError(t, db.First(&row, claimed[0].Id).Error)
				assert.NotZero(t, row.ProcessedAt, "真实数据库已经提交")
				released, err := FailClaimedChannelDailyCostOutboxEventsWithResult(context.Background(), "commit-ack-worker", []int64{row.Id}, now+1, context.DeadlineExceeded)
				require.NoError(t, err)
				assert.Zero(t, released)
				applied, err = ApplyClaimedChannelDailyCostOutboxEventsWithResult(context.Background(), "commit-ack-worker", []int64{row.Id}, now+1)
				require.NoError(t, err)
				assert.Zero(t, applied)
				var ledger ChannelDailyCost
				var key ChannelDailyAPIKeyCost
				var detail ChannelMonitorDailyCostDetail
				require.NoError(t, db.Where("channel_id = ?", 931).First(&ledger).Error)
				require.NoError(t, db.Where("channel_id = ?", 931).First(&key).Error)
				require.NoError(t, db.Where("channel_id = ?", 931).First(&detail).Error)
				assert.Equal(t, delta.CostNanoCNY, ledger.CostNanoCNY)
				assert.Equal(t, delta.CostNanoCNY, key.CostNanoCNY)
				assert.Equal(t, delta.CostNanoCNY, detail.CostNanoCNY)
			})
			for sampleIndex, sample := range []string{"repeated", "distinct"} {
				t.Run(sample, func(t *testing.T) {
					channelID := 901 + sampleIndex
					when := int64(1_750_000_000)
					deltas := make([]ChannelDailyCostDelta, 64)
					for i := range deltas {
						keyID := 1
						if sample == "distinct" {
							keyID += i
						}
						fingerprint, display := ChannelDailyCostAPIKeyIdentityForToken(keyID, "cost-batch-test")
						deltas[i] = ChannelDailyCostDelta{
							EventId: fmt.Sprintf("%s-%d", sample, i), ChannelId: channelID, OccurredAt: when + int64(i),
							CostNanoCNY: 100, ProbeCostNanoCNY: 20, GroupProbeCostNanoCNY: 10, SettledDelta: 1,
							APIKeyId: keyID, APIKeyName: "验证 Key", KeyFingerprint: fingerprint, KeyDisplay: display,
							UserId: 7, ModelName: "model-a", SourceKind: "business",
						}
					}
					// Both fresh inserts and updates of existing rows must retain exact amounts.
					for pass := range 2 {
						for i := range deltas {
							deltas[i].EventId = fmt.Sprintf("%s-%d-%d", sample, pass, i)
						}
						require.NoError(t, StoreChannelDailyCostOutboxEvents(context.Background(), deltas))
						now := time.Now().Unix()
						claimed, err := ClaimChannelDailyCostOutboxEvents(context.Background(), "batch-worker", now, now, time.Minute, len(deltas))
						require.NoError(t, err)
						require.Len(t, claimed, len(deltas))
						ids := make([]int64, len(claimed))
						for i := range claimed {
							ids[i] = claimed[i].Id
						}
						recorder := &channelDailyCostBatchSQLRecorder{Interface: db.Logger}
						db.Logger = recorder
						started := time.Now()
						applied, err := ApplyClaimedChannelDailyCostOutboxEventsWithResult(context.Background(), "batch-worker", ids, now)
						elapsed := time.Since(started)
						db.Logger = recorder.Interface
						require.NoError(t, err)
						assert.EqualValues(t, 64, applied)
						t.Logf("pass=%d events=%d statements=%d transaction=%s", pass, applied, recorder.statements, elapsed)
						applied, err = ApplyClaimedChannelDailyCostOutboxEventsWithResult(context.Background(), "batch-worker", ids, now)
						require.NoError(t, err)
						assert.Zero(t, applied, "重复提交不能重复记账")
					}
					var ledger ChannelDailyCost
					require.NoError(t, db.Where("channel_id = ?", channelID).First(&ledger).Error)
					assert.EqualValues(t, 12800, ledger.CostNanoCNY)
					assert.EqualValues(t, 2560, ledger.ProbeCostNanoCNY)
					assert.EqualValues(t, 1280, ledger.GroupProbeCostNanoCNY)
					assert.EqualValues(t, 128, ledger.SettledCount)
					var keys []ChannelDailyAPIKeyCost
					var details []ChannelMonitorDailyCostDetail
					require.NoError(t, db.Where("channel_id = ?", channelID).Find(&keys).Error)
					require.NoError(t, db.Where("channel_id = ?", channelID).Find(&details).Error)
					rowCount, rowCost, rowEvents := 1, int64(12800), int64(128)
					if sample == "distinct" {
						rowCount, rowCost, rowEvents = 64, 200, 2
					}
					require.Len(t, keys, rowCount)
					require.Len(t, details, rowCount)
					for _, key := range keys {
						assert.Equal(t, rowCost, key.CostNanoCNY)
						assert.Equal(t, rowEvents, key.SettledCount)
					}
					for _, detail := range details {
						assert.Equal(t, rowCost, detail.CostNanoCNY)
						assert.Equal(t, rowEvents, detail.SettledCount)
						assert.Equal(t, rowCost/5, detail.ProbeCostNanoCNY)
						assert.Equal(t, rowCost/10, detail.GroupProbeCostNanoCNY)
					}
				})
			}
		})
	}
}

func verifyChannelDailyCostBatchMetadata(t *testing.T, db *gorm.DB) {
	t.Helper()
	when := int64(1_750_000_000)
	fingerprint, display := ChannelDailyCostAPIKeyIdentityForToken(11, "batch-metadata")
	base := ChannelDailyCostDelta{ChannelId: 910, OccurredAt: when, CostNanoCNY: 10, SettledDelta: 1,
		APIKeyId: 11, APIKeyName: "最早名称", KeyFingerprint: fingerprint, KeyDisplay: display,
		UserId: 7, ModelName: " model-a ", SourceKind: "", UserAttribution: "unknown"}
	late := base
	late.OccurredAt += 20
	late.APIKeyName = "中间名称"
	last := late
	last.APIKeyName, last.UserAttribution, last.KeyDisplay = "最后名称", "inferred", "masked-last"
	last.CostNanoCNY, last.SettledDelta, last.UnresolvedDelta = 0, 0, 1
	last.SourceKind = "unknown"
	require.NoError(t, AddChannelDailyCostBatch(context.Background(), []ChannelDailyCostDelta{late, base, last}))
	var key ChannelDailyAPIKeyCost
	var detail ChannelMonitorDailyCostDetail
	require.NoError(t, db.Where("channel_id = ?", 910).First(&key).Error)
	require.NoError(t, db.Where("channel_id = ?", 910).First(&detail).Error)
	assert.Equal(t, when, key.CreatedAt)
	assert.Equal(t, when+20, key.UpdatedAt)
	assert.Equal(t, "最后名称", key.APIKeyName)
	assert.Equal(t, "masked-last", key.KeyDisplay)
	assert.EqualValues(t, 20, key.CostNanoCNY)
	assert.EqualValues(t, 2, key.SettledCount)
	assert.EqualValues(t, 1, key.UnresolvedCount)
	assert.Equal(t, when, detail.CreatedAt)
	assert.Equal(t, when+20, detail.UpdatedAt)
	assert.Equal(t, "最后名称", detail.APIKeyName)
	assert.Equal(t, "inferred", detail.UserAttribution)
	assert.Equal(t, "unknown", detail.SourceKind)
	assert.Equal(t, "model-a", detail.ModelName)
	assert.EqualValues(t, 20, detail.CostNanoCNY)
	assert.EqualValues(t, 1, detail.UnresolvedCount)
	// An older event arriving later preserves the original update semantics,
	// while the row's creation timestamp must not change.
	older := base
	older.OccurredAt--
	older.APIKeyName = "补到的旧事件"
	require.NoError(t, AddChannelDailyCostBatch(context.Background(), []ChannelDailyCostDelta{older}))
	require.NoError(t, db.First(&key, key.Id).Error)
	require.NoError(t, db.First(&detail, detail.Id).Error)
	assert.Equal(t, when, key.CreatedAt)
	assert.Equal(t, when-1, key.UpdatedAt)
	assert.Equal(t, older.APIKeyName, key.APIKeyName)
	assert.Equal(t, when, detail.CreatedAt)
	assert.Equal(t, when-1, detail.UpdatedAt)
	assert.Equal(t, older.APIKeyName, detail.APIKeyName)
	assert.Equal(t, "request", detail.UserAttribution)

	base.ChannelId = 911
	base.ModelName, base.SourceKind = "model-a", "business"
	variants := []ChannelDailyCostDelta{base, base, base, base, base, base, base, base}
	variants[1].OccurredAt += 86400
	variants[2].UserId++
	variants[3].APIKeyId++
	variants[4].KeyFingerprint, variants[4].KeyDisplay = ChannelDailyCostAPIKeyIdentityForToken(11, "another-upstream")
	variants[5].ModelName = "model-b"
	variants[6].SourceKind = "probe"
	variants[7].KeyFingerprint, variants[7].KeyDisplay = "", ""
	require.NoError(t, AddChannelDailyCostBatch(context.Background(), variants))
	var details []ChannelMonitorDailyCostDetail
	require.NoError(t, db.Where("channel_id = ?", 911).Find(&details).Error)
	require.Len(t, details, 8, "完整维度的任一字段不同，都不能合并明细")
	for _, row := range details {
		assert.EqualValues(t, 10, row.CostNanoCNY)
		assert.EqualValues(t, 1, row.SettledCount)
	}
	var keys []ChannelDailyAPIKeyCost
	require.NoError(t, db.Where("channel_id = ?", 911).Find(&keys).Error)
	require.Len(t, keys, 3, "Key 日账本只按日期、渠道和指纹分组，空指纹不生成 Key 行")
	var totals []ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", 911).Order("day_start ASC").Find(&totals).Error)
	require.Len(t, totals, 2)
	assert.EqualValues(t, 70, totals[0].CostNanoCNY)
	assert.EqualValues(t, 10, totals[1].CostNanoCNY)
}

func verifyChannelDailyCostBatchRecovery(t *testing.T, db *gorm.DB) {
	t.Helper()
	fingerprint, display := ChannelDailyCostAPIKeyIdentityForToken(22, "batch-recovery")
	when := int64(1_750_000_000)
	for i, table := range []string{"channel_daily_costs", "channel_daily_api_key_costs", "channel_monitor_daily_cost_details", "channel_daily_cost_outboxes"} {
		t.Run(table, func(t *testing.T) {
			channelID := 920 + i
			first := ChannelDailyCostDelta{EventId: "rollback-" + table, ChannelId: channelID, OccurredAt: when, CostNanoCNY: 50, SettledDelta: 1,
				APIKeyId: 22, KeyFingerprint: fingerprint, KeyDisplay: display, UserId: 8, ModelName: "recovery", SourceKind: "business"}
			second := first
			second.EventId += "-second"
			require.NoError(t, StoreChannelDailyCostOutboxEvents(context.Background(), []ChannelDailyCostDelta{first, second}))
			now := time.Now().Unix()
			claimed, err := ClaimChannelDailyCostOutboxEvents(context.Background(), "before-restart", now, now, time.Minute, 2)
			require.NoError(t, err)
			require.Len(t, claimed, 2)
			ids := []int64{claimed[0].Id, claimed[1].Id}
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:cost_batch_rollback", func(tx *gorm.DB) {
				if tx.Statement.Table == table {
					tx.AddError(context.DeadlineExceeded)
				}
			}))
			applied, applyErr := ApplyClaimedChannelDailyCostOutboxEventsWithResult(context.Background(), "before-restart", ids, now)
			require.NoError(t, db.Callback().Update().Remove("test:cost_batch_rollback"))
			require.ErrorIs(t, applyErr, context.DeadlineExceeded)
			assert.Zero(t, applied)
			for _, target := range []any{&ChannelDailyCost{}, &ChannelDailyAPIKeyCost{}, &ChannelMonitorDailyCostDetail{}} {
				var count int64
				require.NoError(t, db.Model(target).Where("channel_id = ?", channelID).Count(&count).Error)
				assert.Zero(t, count, "任一阶段失败，三类账本均回滚")
			}
			// An expired lease is recovered by a new worker. The previous owner
			// must neither release nor apply rows now owned by the replacement.
			claimed, err = ClaimChannelDailyCostOutboxEvents(context.Background(), "after-restart", now+61, now+61, time.Minute, 2)
			require.NoError(t, err)
			require.Len(t, claimed, 2)
			applied, err = ApplyClaimedChannelDailyCostOutboxEventsWithResult(context.Background(), "before-restart", ids, now+62)
			require.NoError(t, err)
			assert.Zero(t, applied)
			released, err := FailClaimedChannelDailyCostOutboxEventsWithResult(context.Background(), "before-restart", ids, now+62, applyErr)
			require.NoError(t, err)
			assert.Zero(t, released)
			applied, err = ApplyClaimedChannelDailyCostOutboxEventsWithResult(context.Background(), "after-restart", ids, now+62)
			require.NoError(t, err)
			assert.EqualValues(t, 2, applied)
			applied, err = ApplyClaimedChannelDailyCostOutboxEventsWithResult(context.Background(), "after-restart", ids, now+63)
			require.NoError(t, err)
			assert.Zero(t, applied)
			var ledger ChannelDailyCost
			require.NoError(t, db.Where("channel_id = ?", channelID).First(&ledger).Error)
			assert.EqualValues(t, 100, ledger.CostNanoCNY)
			assert.EqualValues(t, 2, ledger.SettledCount)
			var detail ChannelMonitorDailyCostDetail
			require.NoError(t, db.Where("channel_id = ?", channelID).First(&detail).Error)
			assert.EqualValues(t, 100, detail.CostNanoCNY)
			// A detail-only overflow must roll back the otherwise valid total/Key
			// updates and expose the same overflow classification to the worker.
			require.NoError(t, db.Model(&detail).Update("cost_nano_cny", int64(math.MaxInt64)).Error)
			first.EventId = ""
			err = AddChannelDailyCostBatch(context.Background(), []ChannelDailyCostDelta{first})
			require.ErrorIs(t, err, ErrChannelDailyCostLedgerOverflow)
			require.NoError(t, db.First(&ledger, ledger.Id).Error)
			assert.EqualValues(t, 100, ledger.CostNanoCNY)
		})
	}
}
