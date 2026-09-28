package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelMonitorIncomeParityUpgrade(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			require.NoError(t, db.AutoMigrate(&ChannelMonitorIncome{}, &ChannelMonitorIncomeState{}))
			ready, gap := ChannelMonitorIncomeReady.Load(), channelMonitorIncomeGap.Load()
			t.Cleanup(func() {
				assert.NoError(t, db.Migrator().DropTable(&ChannelMonitorIncome{}, &ChannelMonitorIncomeState{}))
				ChannelMonitorIncomeReady.Store(ready)
				channelMonitorIncomeGap.Store(gap)
			})
			// Fresh startup and repeated startup must both work with no income.
			require.NoError(t, InitializeChannelMonitorIncome(db, true))
			require.NoError(t, InitializeChannelMonitorIncome(db, true))
			state := ChannelMonitorIncomeState{ID: 1, StartedAt: 1700000000, GapSince: 1700000123, RetainedFrom: 1690000000}
			require.NoError(t, db.Model(&state).Updates(&state).Error)
			// Rows use the released schema and original FX-based values. Include
			// pending/refunded income, already-corrected data and a saved unit
			// different from today's unit; no log history is required.
			rows := []ChannelMonitorIncome{
				{SettlementKey: ChannelMonitorIncomeKey("parity-wallet", "request"), BillingSource: "wallet", Quota: 3_500_000, QuotaPerUnit: "500000", USDToCNY: "3", IncomeNanoCNY: 21_000_000_000, Status: "settled"},
				{SettlementKey: ChannelMonitorIncomeKey("parity-subscription", "request"), BillingSource: "subscription", Quota: 150, QuotaPerUnit: "100", USDToCNY: "7.3", IncomeNanoCNY: 10_950_000_000, Status: "pending"},
				{SettlementKey: ChannelMonitorIncomeKey("parity-refunded", "request"), BillingSource: "wallet", Quota: 0, QuotaPerUnit: "500000", USDToCNY: "7.3", Status: "settled"},
				{SettlementKey: ChannelMonitorIncomeKey("parity-corrected", "request"), BillingSource: "wallet", Quota: 500000, QuotaPerUnit: "500000", USDToCNY: "1", IncomeNanoCNY: 1_000_000_000, Status: "settled"},
			}
			for i := range rows {
				rows[i].ChannelID, rows[i].UserID, rows[i].APIKeyID = 9, 17, 21
				rows[i].APIKeyKey, rows[i].APIKeyName = "fingerprint", "同一 Key"
				rows[i].DayStart, rows[i].CreatedAt, rows[i].UpdatedAt = 1700000000, 1700000001, 1700000002
				rows[i].CostEventID, rows[i].CostRecorded = "original-cost", 1
			}
			require.NoError(t, db.Create(&rows).Error)
			cost := ChannelDailyCost{ChannelId: 9, DayStart: 1700000000, CostNanoCNY: 4_595_200_000}
			require.NoError(t, db.Create(&cost).Error)
			require.ErrorContains(t, InitializeChannelMonitorIncome(db, false), "1:1")
			assert.False(t, ChannelMonitorIncomeReady.Load())
			for i := 0; i < 2; i++ {
				require.NoError(t, InitializeChannelMonitorIncome(db, true))
				assert.True(t, ChannelMonitorIncomeReady.Load())
				var saved []ChannelMonitorIncome
				require.NoError(t, db.Order("id").Find(&saved).Error)
				expected := append([]ChannelMonitorIncome(nil), rows...)
				amounts := []int64{7_000_000_000, 1_500_000_000, 0, 1_000_000_000}
				for j := range expected {
					expected[j].USDToCNY, expected[j].IncomeNanoCNY = "1", amounts[j]
				}
				assert.Equal(t, expected, saved, "仅修正换算金额，保留身份、额度、日期和结算状态")
				var savedState ChannelMonitorIncomeState
				require.NoError(t, db.First(&savedState, 1).Error)
				assert.Equal(t, state, savedState, "历史缺口仍须标记，不能因收入修正而确认利润")
				var savedCost ChannelDailyCost
				require.NoError(t, db.First(&savedCost, cost.Id).Error)
				assert.Equal(t, cost, savedCost)
			}
			require.NoError(t, InitializeChannelMonitorIncome(db, false))
			duplicate := rows[0]
			duplicate.ID = 0
			assert.Error(t, db.Create(&duplicate).Error, "修正后结算标识仍唯一")
			task := &Task{PrivateData: TaskPrivateData{Execution: &TaskExecutionSnapshot{RequestID: "parity-wallet"}}}
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return correctTaskChannelMonitorIncome(tx, task, 1_000_000) }))
			var adjusted ChannelMonitorIncome
			require.NoError(t, db.First(&adjusted, rows[0].ID).Error)
			assert.Equal(t, int64(2_000_000_000), adjusted.IncomeNanoCNY)
			require.NoError(t, RefundChannelMonitorIncome(context.Background(), adjusted.SettlementKey))
			require.NoError(t, db.First(&adjusted, rows[0].ID).Error)
			assert.Zero(t, adjusted.IncomeNanoCNY)

			// Bad original units must roll back the batch and keep income
			// unavailable, so a partial repair cannot be presented as complete.
			retryRows := []ChannelMonitorIncome{rows[0], rows[1]}
			for j := range retryRows {
				retryRows[j].ID = 0
				retryRows[j].SettlementKey = ChannelMonitorIncomeKey(retryRows[j].SettlementKey, "repair-retry")
			}
			retryRows[1].QuotaPerUnit = "invalid"
			require.NoError(t, db.Create(&retryRows).Error)
			require.Error(t, InitializeChannelMonitorIncome(db, true))
			assert.False(t, ChannelMonitorIncomeReady.Load())
			var unchanged ChannelMonitorIncome
			require.NoError(t, db.First(&unchanged, retryRows[0].ID).Error)
			assert.Equal(t, retryRows[0], unchanged)
			require.NoError(t, db.Model(&retryRows[1]).Update("quota_per_unit", "100").Error)
			require.NoError(t, InitializeChannelMonitorIncome(db, true))
			require.NoError(t, db.First(&unchanged, retryRows[0].ID).Error)
			assert.Equal(t, int64(7_000_000_000), unchanged.IncomeNanoCNY)
		})
	}
}
