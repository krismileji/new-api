package model

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelMonitorIncomePreparationDuringCostProjection(t *testing.T) {
	// PostgreSQL does not gap-lock a missing income row. A request can finish
	// funding while the cost worker is between matching income and committing.
	db := setupChannelDailyCostBatchDatabase(t, "postgres")
	require.NoError(t, db.AutoMigrate(&ChannelMonitorIncome{}, &ChannelMonitorIncomeState{}, &ChannelMonitorIncomeGap{}))
	previousReady, previousGap := ChannelMonitorIncomeReady.Load(), channelMonitorIncomeGap.Load()
	t.Cleanup(func() {
		assert.NoError(t, db.Migrator().DropTable(&ChannelMonitorIncome{}, &ChannelMonitorIncomeState{}, &ChannelMonitorIncomeGap{}))
		ChannelMonitorIncomeReady.Store(previousReady)
		channelMonitorIncomeGap.Store(previousGap)
	})
	require.NoError(t, InitializeChannelMonitorIncome(db, true))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	now := time.Now().Unix()
	event := ChannelDailyCostDelta{
		EventId: "income-during-cost-projection", ChannelId: 9,
		OccurredAt: 1_700_000_000, CostNanoCNY: 123, SettledDelta: 1,
	}
	require.NoError(t, StoreChannelDailyCostOutboxEvents(ctx, []ChannelDailyCostDelta{event}))
	claimed, err := ClaimChannelDailyCostOutboxEvents(ctx, "income-race-test", now, now, time.Minute, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	projectionConn, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, projectionConn.Close()) })
	incomeConn, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, incomeConn.Close()) })
	var projectionPID, incomePID int
	require.NoError(t, projectionConn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&projectionPID))
	require.NoError(t, incomeConn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&incomePID))

	paused, resume := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	var workers sync.WaitGroup
	t.Cleanup(func() {
		release()
		cancel()
		workers.Wait()
		DB = db
		assert.NoError(t, db.Callback().Update().Remove("test:pause_income_projection"))
	})
	projectionDB := db.WithContext(ctx)
	projectionDB.Statement.ConnPool = projectionConn
	DB = projectionDB
	var pauseOnce sync.Once
	require.NoError(t, db.Callback().Update().After("gorm:update").Register("test:pause_income_projection", func(tx *gorm.DB) {
		if tx.Statement.Table != "channel_monitor_incomes" {
			return
		}
		pauseOnce.Do(func() {
			close(paused)
			select {
			case <-resume:
			case <-ctx.Done():
				tx.AddError(ctx.Err())
			}
		})
	}))
	projected := make(chan error, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		projected <- ApplyClaimedChannelDailyCostOutboxEvents(ctx, "income-race-test", []int64{claimed[0].Id}, now)
	}()
	select {
	case <-paused:
	case err := <-projected:
		require.NoError(t, err)
		require.FailNow(t, "cost projection finished before reaching the income barrier")
	case <-ctx.Done():
		require.NoError(t, ctx.Err())
	}

	incomeDB := db.WithContext(ctx)
	incomeDB.Statement.ConnPool = incomeConn
	DB = incomeDB
	record := &ChannelMonitorIncome{
		SettlementKey: ChannelMonitorIncomeKey("concurrent-request", "request"), ChannelID: 9,
		UserID: 17, BillingSource: "wallet", Quota: 100,
		QuotaPerUnit: "100", USDToCNY: "7", CostEventID: event.EventId,
	}
	prepared := make(chan error, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		prepared <- PrepareChannelMonitorIncome(ctx, record)
	}()

	// Release the worker only after preparation has either completed or is
	// waiting on that worker's database lock. This fixes the interleaving
	// without assuming either goroutine finishes within an arbitrary sleep.
	var prepareErr, waitErr error
	finished := false
	require.Eventually(t, func() bool {
		select {
		case prepareErr = <-prepared:
			finished = true
			return true
		default:
		}
		var blocked bool
		waitErr = db.WithContext(ctx).Raw("SELECT ? = ANY(pg_blocking_pids(?))", projectionPID, incomePID).Scan(&blocked).Error
		return blocked || waitErr != nil
	}, 10*time.Second, 10*time.Millisecond)
	require.NoError(t, waitErr)
	release()
	require.NoError(t, <-projected)
	if !finished {
		prepareErr = <-prepared
	}
	require.NoError(t, prepareErr)
	DB = db

	var saved ChannelMonitorIncome
	require.NoError(t, db.Where("settlement_key = ?", record.SettlementKey).First(&saved).Error)
	assert.Equal(t, 1, saved.CostRecorded, "an applied cost must not leave profit permanently unconfirmed")
	assert.Equal(t, ChannelDailyCostDayStart(event.OccurredAt), saved.DayStart, "concurrent income and cost must use the same day")
	var cost ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", event.ChannelId).First(&cost).Error)
	assert.Equal(t, event.CostNanoCNY, cost.CostNanoCNY)
}
