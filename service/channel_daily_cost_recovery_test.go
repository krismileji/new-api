package service

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupChannelDailyCostRecoveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cost-recovery.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ChannelDailyCost{}, &model.ChannelDailyAPIKeyCost{}, &model.ChannelDailyCostOutbox{}, &model.ChannelMonitorDailyCostDetail{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	previousDB, previousType := model.DB, common.MainDatabaseType()
	previousStats, previousObservedAt := GetChannelDailyCostReliableStats(), channelDailyCostOutboxStatsObservedAt.Load()
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	setCM07ChannelDailyCostReliableStats(ChannelDailyCostReliableStats{})
	channelDailyCostOutboxStatsObservedAt.Store(0)
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousType)
		setCM07ChannelDailyCostReliableStats(previousStats)
		channelDailyCostOutboxStatsObservedAt.Store(previousObservedAt)
		assert.NoError(t, sqlDB.Close())
	})
	return db
}

func TestChannelDailyCostRecoveryRetriesReadyRowsWithoutChasingNewEvents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := setupChannelDailyCostRecoveryTestDB(t)
		ctx, cancel := context.WithCancel(context.Background())
		first := newCM07ChannelDailyCostDelta("early-retry", 991, 50)
		require.NoError(t, model.StoreChannelDailyCostOutboxEvents(ctx, []model.ChannelDailyCostDelta{first}))
		// One previous attempt makes the first scheduled retry occur in two
		// virtual seconds, leaving time for another instance to append a row.
		require.NoError(t, db.Model(&model.ChannelDailyCostOutbox{}).Where("event_id = ?", first.EventId).Update("attempt_count", 1).Error)
		var failed atomic.Bool
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:retry_once", func(tx *gorm.DB) {
			if tx.Statement.Table == "channel_daily_costs" && !failed.Swap(true) {
				tx.AddError(context.DeadlineExceeded)
			}
		}))
		runtime := &ChannelDailyCostOutboxRuntime{consumerName: "early-retry-worker"}
		done := make(chan struct{})
		go func() { defer close(done); runtime.runDBRecovery(ctx) }()
		defer func() { cancel(); <-done }()
		synctest.Wait()
		var row model.ChannelDailyCostOutbox
		require.NoError(t, db.Where("event_id = ?", first.EventId).First(&row).Error)
		assert.Zero(t, row.ProcessedAt)
		assert.Empty(t, row.LeaseOwner)
		assert.Equal(t, time.Now().Add(2*time.Second).Unix(), row.NextAttemptAt)
		time.Sleep(time.Second) // Virtual time only.
		second := newCM07ChannelDailyCostDelta("new-during-retry", 992, 70)
		require.NoError(t, model.StoreChannelDailyCostOutboxEvents(ctx, []model.ChannelDailyCostDelta{second}))
		time.Sleep(time.Second)
		synctest.Wait()
		require.NoError(t, db.First(&row, row.Id).Error)
		assert.NotZero(t, row.ProcessedAt, "已到期的重试不应等下一分钟")
		assert.EqualValues(t, 3, row.AttemptCount)
		var newRow model.ChannelDailyCostOutbox
		require.NoError(t, db.Where("event_id = ?", second.EventId).First(&newRow).Error)
		assert.Zero(t, newRow.AttemptCount, "本轮固定 createdBefore，不追赶新事件")
		assert.EqualValues(t, 1, GetChannelDailyCostReliableStats().LedgerApplied)
		time.Sleep(58 * time.Second)
		synctest.Wait()
		require.NoError(t, db.First(&newRow, newRow.Id).Error)
		assert.NotZero(t, newRow.ProcessedAt)
		assert.EqualValues(t, 2, GetChannelDailyCostReliableStats().LedgerApplied)
		assertCM07ChannelDailyCostLedger(t, db, first)
		assertCM07ChannelDailyCostLedger(t, db, second)
	})
}

func TestChannelDailyCostRecoveryBoundsRetriesAndCancelsWaiting(t *testing.T) {
	for _, stage := range []string{"claim", "apply", "release"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				db := setupChannelDailyCostRecoveryTestDB(t)
				delta := newCM07ChannelDailyCostDelta("persistent-failure", 993, 80)
				require.NoError(t, model.StoreChannelDailyCostOutboxEvents(context.Background(), []model.ChannelDailyCostDelta{delta}))
				var failures atomic.Int64
				if stage == "claim" {
					require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:claim_failure", func(tx *gorm.DB) {
						if tx.Statement.Table == "channel_daily_cost_outboxes" && strings.Contains(fmt.Sprint(tx.Statement.Clauses["WHERE"].Expression), "next_attempt_at") {
							failures.Add(1)
							tx.AddError(context.DeadlineExceeded)
						}
					}))
				} else {
					require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:write_failure", func(tx *gorm.DB) {
						if tx.Statement.Table == "channel_daily_costs" {
							failures.Add(1)
							tx.AddError(context.DeadlineExceeded)
						}
						if stage == "release" && tx.Statement.Table == "channel_daily_cost_outboxes" {
							if values, ok := tx.Statement.Dest.(map[string]interface{}); ok && values["last_error"] != nil {
								tx.AddError(assert.AnError)
							}
						}
					}))
				}
				ctx, cancel := context.WithCancel(context.Background())
				runtime := &ChannelDailyCostOutboxRuntime{consumerName: "bounded-retry-worker"}
				done := make(chan struct{})
				go func() { defer close(done); runtime.runDBRecovery(ctx) }()
				defer func() { cancel(); <-done }()
				synctest.Wait()
				time.Sleep(59 * time.Second)
				synctest.Wait()
				wantAttempts := int64(5) // 0, 1, 3, 7, 15; the next attempt cannot fit.
				if stage == "release" {
					wantAttempts = 1
				}
				assert.Equal(t, wantAttempts, failures.Load(), "提前重试不能重置本轮预算，释放失败则等待租约恢复")
				cancel()
				<-done
				time.Sleep(2 * time.Minute)
				synctest.Wait()
				assert.Equal(t, wantAttempts, failures.Load(), "停止后不得再次领取")
				if stage == "claim" {
					require.NoError(t, db.Callback().Query().Remove("test:claim_failure"))
				}
				var row model.ChannelDailyCostOutbox
				require.NoError(t, db.First(&row).Error)
				assert.Zero(t, row.ProcessedAt)
				assert.Zero(t, GetChannelDailyCostReliableStats().LedgerApplied)
			})
		})
	}
}

func TestChannelDailyCostRecoveryOverflowFallbackSharesDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := setupChannelDailyCostRecoveryTestDB(t)
		deltas := []model.ChannelDailyCostDelta{newCM07ChannelDailyCostDelta("shared-deadline-a", 994, 30), newCM07ChannelDailyCostDelta("shared-deadline-b", 995, 40)}
		require.NoError(t, model.StoreChannelDailyCostOutboxEvents(context.Background(), deltas))
		refreshChannelDailyCostOutboxStats(context.Background())
		var calls int
		var deadlines []time.Time
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:shared_apply_deadline", func(tx *gorm.DB) {
			if tx.Statement.Table != "channel_daily_cost_outboxes" {
				return
			}
			if _, ok := tx.Statement.Dest.(*[]model.ChannelDailyCostOutbox); !ok {
				return
			}
			// Claim's final query uses the original three-second operation budget.
			deadline, _ := tx.Statement.Context.Deadline()
			if time.Until(deadline) <= 3*time.Second && calls == 0 {
				return
			}
			calls++
			deadlines = append(deadlines, deadline)
			if calls == 1 {
				tx.AddError(model.ErrChannelDailyCostLedgerOverflow)
				return
			}
			if calls == 3 {
				<-tx.Statement.Context.Done()
				tx.AddError(tx.Statement.Context.Err())
			}
		}))
		now := time.Now()
		result, err := applyChannelDailyCostOutboxBatch(context.Background(), "shared-deadline-worker", now.Unix(), now.Unix())
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Len(t, deadlines, 3)
		assert.Equal(t, deadlines[0], deadlines[1])
		assert.Equal(t, deadlines[0], deadlines[2], "溢出逐条复核不得重置整个事务预算")
		assert.Equal(t, now.Add(10*time.Second), time.Now())
		assert.EqualValues(t, 1, result.Applied)
		assert.EqualValues(t, 1, result.Released)
		assert.EqualValues(t, 1, GetChannelDailyCostReliableStats().OutboxPending)
		require.NoError(t, db.Callback().Query().Remove("test:shared_apply_deadline"))
		var rows []model.ChannelDailyCostOutbox
		require.NoError(t, db.Order("id ASC").Find(&rows).Error)
		require.Len(t, rows, 2)
		assert.NotZero(t, rows[0].ProcessedAt)
		assert.Zero(t, rows[1].ProcessedAt)
		assert.Empty(t, rows[1].LeaseOwner)
		require.NoError(t, FlushChannelDailyCostOutbox(context.Background()))
		assertCM07ChannelDailyCostLedger(t, db, deltas[0])
		assertCM07ChannelDailyCostLedger(t, db, deltas[1])
	})
}

func TestChannelDailyCostRecoveryBatchBoundAndPartialSuccess(t *testing.T) {
	db := setupChannelDailyCostRecoveryTestDB(t)
	verifyChannelDailyCostRecoveryBatch(t, db)
}

func TestChannelDailyCostRecoveryReservesCleanupBeforeDeadline(t *testing.T) {
	for _, scenario := range []string{"lease", "round", "shutdown"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				db := setupChannelDailyCostRecoveryTestDB(t)
				delta := newCM07ChannelDailyCostDelta("bounded-deadline", 999, 20)
				require.NoError(t, model.StoreChannelDailyCostOutboxEvents(context.Background(), []model.ChannelDailyCostDelta{delta}))
				started := time.Now()
				claimAt := started.Unix()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				wantElapsed := 10 * time.Second
				switch scenario {
				case "lease":
					claimAt -= 25
					wantElapsed = 2 * time.Second
				case "round":
					var deadlineCancel context.CancelFunc
					ctx, deadlineCancel = context.WithDeadline(ctx, started.Add(6*time.Second))
					defer deadlineCancel()
					wantElapsed = 3 * time.Second
				}
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:slow_ledger", func(tx *gorm.DB) {
					if tx.Statement.Table != "channel_daily_costs" {
						return
					}
					if scenario == "shutdown" {
						cancel()
					}
					<-tx.Statement.Context.Done()
					tx.AddError(tx.Statement.Context.Err())
				}))
				result, err := applyChannelDailyCostOutboxBatch(ctx, "cleanup-reserve-worker", claimAt, started.Unix())
				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.Equal(t, started.Add(wantElapsed), time.Now())
				assert.EqualValues(t, 1, result.Released, "记账期限结束后仍能使用独立预算释放租约")
				assert.Zero(t, result.Applied)
				var row model.ChannelDailyCostOutbox
				require.NoError(t, db.First(&row).Error)
				assert.Empty(t, row.LeaseOwner)
				assert.Zero(t, row.ProcessedAt)
				assert.NotZero(t, row.NextAttemptAt)
			})
		})
	}
}

func verifyChannelDailyCostRecoveryBatch(t *testing.T, db *gorm.DB) {
	t.Helper()
	deltas := make([]model.ChannelDailyCostDelta, 65)
	for i := range deltas {
		deltas[i] = newCM07ChannelDailyCostDelta(fmt.Sprintf("bounded-ledger-%d", i), 996, 2)
	}
	require.NoError(t, model.StoreChannelDailyCostOutboxEvents(context.Background(), deltas))
	refreshChannelDailyCostOutboxStats(context.Background())
	now := time.Now().Unix()
	result, err := applyChannelDailyCostOutboxBatch(context.Background(), "bounded-ledger-worker", now, now)
	require.NoError(t, err)
	assert.Equal(t, 64, result.Claimed)
	assert.EqualValues(t, 64, result.Applied)
	var remaining model.ChannelDailyCostOutbox
	require.NoError(t, db.Where("event_id = ?", deltas[64].EventId).First(&remaining).Error)
	assert.Zero(t, remaining.AttemptCount, "下一批的事件不能被预先领取")
	assert.Empty(t, remaining.LeaseOwner)
	require.NoError(t, FlushChannelDailyCostOutbox(context.Background()))
	expected := deltas[0]
	expected.CostNanoCNY, expected.SettledDelta = 130, 65
	assertCM07ChannelDailyCostLedger(t, db, expected)
	var detail model.ChannelMonitorDailyCostDetail
	require.NoError(t, db.Where("channel_id = ?", 996).First(&detail).Error)
	assert.EqualValues(t, 130, detail.CostNanoCNY)
	assert.EqualValues(t, 65, detail.SettledCount)

	seed := newCM07ChannelDailyCostDelta("", 997, math.MaxInt64)
	require.NoError(t, model.AddChannelDailyCostBatch(context.Background(), []model.ChannelDailyCostDelta{seed}))
	invalid, valid := newCM07ChannelDailyCostDelta("bounded-overflow", 997, 1), newCM07ChannelDailyCostDelta("bounded-valid", 998, 90)
	require.NoError(t, model.StoreChannelDailyCostOutboxEvents(context.Background(), []model.ChannelDailyCostDelta{invalid, valid}))
	refreshChannelDailyCostOutboxStats(context.Background())
	now = time.Now().Unix()
	result, err = applyChannelDailyCostOutboxBatch(context.Background(), "partial-ledger-worker", now, now)
	require.ErrorIs(t, err, model.ErrChannelDailyCostLedgerOverflow)
	assert.EqualValues(t, 1, result.Applied)
	assert.EqualValues(t, 1, result.Released)
	assert.NotZero(t, result.RetryAt)
	assertCM07ChannelDailyCostLedger(t, db, valid)
	stats, err := model.GetChannelDailyCostOutboxStats(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 1, stats.PendingCount)
	var pending model.ChannelDailyCostOutbox
	require.NoError(t, db.Where("event_id = ?", invalid.EventId).First(&pending).Error)
	assert.Empty(t, pending.LeaseOwner)
	assert.Equal(t, result.RetryAt.Unix(), pending.NextAttemptAt)
	result, err = applyChannelDailyCostOutboxBatch(context.Background(), "not-ready-worker", now, now)
	require.NoError(t, err)
	assert.Zero(t, result.Claimed, "普通恢复不能提前领取尚未到期的失败事件")
}
