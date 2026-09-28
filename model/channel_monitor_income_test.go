package model

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelMonitorIncomeAmountUsesSavedConversion(t *testing.T) {
	amount, err := ChannelMonitorIncomeAmount(150, "100", "7")
	require.NoError(t, err)
	assert.Equal(t, int64(10_500_000_000), amount)

	for _, input := range []struct {
		quota int64
		unit  string
		rate  string
	}{
		{quota: -1, unit: "100", rate: "7"},
		{quota: int64(^uint32(0)), unit: "100", rate: "7"},
		{quota: 100, unit: "0", rate: "7"},
		{quota: 100, unit: "100", rate: "NaN"},
		{quota: 100, unit: "0.0000000000000000000001", rate: "1e100"},
	} {
		_, err := ChannelMonitorIncomeAmount(input.quota, input.unit, input.rate)
		assert.Error(t, err)
	}
}

func TestChannelMonitorIncomePreparationIsIdempotentAndRestoresGap(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			runChannelMonitorIncomeLedgerCases(t, setupChannelDailyCostBatchDatabase(t, engine))
		})
	}
}

func runChannelMonitorIncomeLedgerCases(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&ChannelMonitorIncome{}, &ChannelMonitorIncomeState{}, &ChannelDailyCostOutbox{}))

	previousDB := DB
	previousReady := ChannelMonitorIncomeReady.Load()
	previousGap := channelMonitorIncomeGap.Load()
	DB = db
	ChannelMonitorIncomeReady.Store(false)
	channelMonitorIncomeGap.Store(0)
	t.Cleanup(func() {
		assert.NoError(t, db.Migrator().DropTable(&ChannelMonitorIncome{}, &ChannelMonitorIncomeState{}))
		DB = previousDB
		ChannelMonitorIncomeReady.Store(previousReady)
		channelMonitorIncomeGap.Store(previousGap)
	})

	state := ChannelMonitorIncomeState{ID: 1, StartedAt: 1_700_000_000, GapSince: 1_700_000_123}
	require.NoError(t, db.Create(&state).Error)
	require.NoError(t, InitializeChannelMonitorIncome(db, false))
	assert.True(t, ChannelMonitorIncomeReady.Load())
	assert.Equal(t, state.GapSince, ChannelMonitorIncomeGapSince())

	record := &ChannelMonitorIncome{
		SettlementKey: ChannelMonitorIncomeKey("request-1", "request"), ChannelID: 9,
		UserID: 17, APIKeyID: 21, APIKeyKey: "key-fingerprint", APIKeyName: "生产 Key",
		ModelName: "gpt-4.1", GroupName: "default", BillingSource: "wallet", Quota: 100,
		QuotaPerUnit: "100", USDToCNY: "7", CostEventID: "cost-event-1",
	}
	require.NoError(t, PrepareChannelMonitorIncome(context.Background(), record))
	assert.Equal(t, int64(1_000_000_000), record.IncomeNanoCNY)
	assert.Equal(t, ChannelMonitorDailyCostModelKey("gpt-4.1"), record.ModelKey)
	assert.Equal(t, "pending", record.Status)

	duplicate := *record
	duplicate.ID = 0
	duplicate.Quota = 200
	require.NoError(t, PrepareChannelMonitorIncome(context.Background(), &duplicate))
	assert.Equal(t, record.SettlementKey, duplicate.SettlementKey)
	assert.Equal(t, int64(100), duplicate.Quota)
	assert.Equal(t, int64(1_000_000_000), duplicate.IncomeNanoCNY)

	require.NoError(t, ConfirmChannelMonitorIncome(context.Background(), record.SettlementKey))
	now := time.Now().Unix()
	costAt := now - 86400
	require.NoError(t, StoreChannelDailyCostOutboxEvents(context.Background(), []ChannelDailyCostDelta{{EventId: "cost-event-1", ChannelId: 9, OccurredAt: costAt, CostNanoCNY: 123, SettledDelta: 1}}))
	claimed, err := ClaimChannelDailyCostOutboxEvents(context.Background(), "income-test", now, now, time.Minute, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, ApplyClaimedChannelDailyCostOutboxEvents(context.Background(), "income-test", []int64{claimed[0].Id}, now))
	var saved ChannelMonitorIncome
	require.NoError(t, db.Where("settlement_key = ?", record.SettlementKey).First(&saved).Error)
	assert.Equal(t, "settled", saved.Status)
	assert.Equal(t, 1, saved.CostRecorded)
	assert.Equal(t, ChannelDailyCostDayStart(costAt), saved.DayStart, "income and cost stay on the same day")
	var count int64
	require.NoError(t, db.Model(&ChannelMonitorIncome{}).Where("settlement_key = ?", record.SettlementKey).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	lateCostEvent := ChannelDailyCostOutbox{
		EventId: "cost-event-already-applied", ChannelId: 9, OccurredAt: 1_700_000_000, ProcessedAt: 1_700_000_010,
	}
	require.NoError(t, db.Create(&lateCostEvent).Error)
	lateIncome := &ChannelMonitorIncome{
		SettlementKey: ChannelMonitorIncomeKey("request-2", "violation"), ChannelID: 9,
		UserID: 17, APIKeyID: 21, APIKeyKey: "key-fingerprint", APIKeyName: "生产 Key",
		ModelName: "gpt-4.1", GroupName: "default", BillingSource: "wallet", Quota: 100,
		QuotaPerUnit: "100", USDToCNY: "7", CostEventID: lateCostEvent.EventId,
	}
	require.NoError(t, PrepareChannelMonitorIncome(context.Background(), lateIncome))
	assert.Equal(t, 1, lateIncome.CostRecorded, "income inserted after its shared cost event was applied must not stay pending")
	assert.Equal(t, ChannelDailyCostDayStart(lateCostEvent.OccurredAt), lateIncome.DayStart)
	require.NoError(t, ConfirmChannelMonitorIncome(context.Background(), lateIncome.SettlementKey))
	found, changed, err := MarkChannelMonitorIncomeRefundPending(context.Background(), lateIncome.SettlementKey)
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, changed)
	var refundIncome ChannelMonitorIncome
	require.NoError(t, db.Where("settlement_key = ?", lateIncome.SettlementKey).First(&refundIncome).Error)
	assert.Equal(t, "pending", refundIncome.Status)
	assert.Equal(t, int64(1_000_000_000), refundIncome.IncomeNanoCNY, "a refund in progress must not count as confirmed profit")
	require.NoError(t, CancelChannelMonitorIncomeRefund(context.Background(), lateIncome.SettlementKey))
	require.NoError(t, db.Where("settlement_key = ?", lateIncome.SettlementKey).First(&refundIncome).Error)
	assert.Equal(t, "settled", refundIncome.Status, "a failed refund can restore the original charge")
	found, changed, err = MarkChannelMonitorIncomeRefundPending(context.Background(), lateIncome.SettlementKey)
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, changed)
	require.NoError(t, RefundChannelMonitorIncome(context.Background(), lateIncome.SettlementKey))
	require.NoError(t, db.Where("settlement_key = ?", lateIncome.SettlementKey).First(&refundIncome).Error)
	assert.Equal(t, "settled", refundIncome.Status)
	assert.Zero(t, refundIncome.IncomeNanoCNY)

	task := &Task{PrivateData: TaskPrivateData{Execution: &TaskExecutionSnapshot{RequestID: "request-1"}}}
	// A task whose cost was initially unresolved becomes confirmed when its
	// final settlement resolves the cost in the same funding transaction.
	require.NoError(t, db.Model(&saved).Update("cost_recorded", 0).Error)
	task.PrivateData.BillingContext = &TaskBillingContext{ChannelCostResolved: true}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return correctTaskChannelMonitorIncome(tx, task, 50) }))
	require.NoError(t, db.Where("settlement_key = ?", record.SettlementKey).First(&saved).Error)
	assert.Equal(t, int64(500_000_000), saved.IncomeNanoCNY)
	assert.Equal(t, 1, saved.CostRecorded)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return correctTaskChannelMonitorIncome(tx, task, 0) }))
	require.NoError(t, db.Where("settlement_key = ?", record.SettlementKey).First(&saved).Error)
	assert.Zero(t, saved.IncomeNanoCNY)

	cutoff := record.DayStart + 86400
	for i := 0; i < 2; i++ {
		incomplete, err := DeleteChannelMonitorIncomeBefore(context.Background(), cutoff, 10, ChannelMonitorCleanupBudget{})
		require.NoError(t, err)
		assert.False(t, incomplete)
	}
	require.NoError(t, db.First(&state, 1).Error)
	assert.Equal(t, cutoff, state.RetainedFrom)
	require.NoError(t, db.Model(&ChannelMonitorIncome{}).Count(&count).Error)
	assert.Zero(t, count)
}
