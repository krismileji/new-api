package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"

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

func TestChannelMonitorIncomeFundingSurvivesProcessExit(t *testing.T) {
	if path := os.Getenv("PROFIT_RECOVERY_PROCESS_DB"); path != "" {
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
		require.NoError(t, err)
		DB = db
		common.RedisEnabled, common.BatchUpdateEnabled = false, false
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
		require.NoError(t, db.AutoMigrate(&ChannelMonitorIncome{}, &ChannelMonitorIncomeGap{}, &ChannelDailyCostOutbox{}, &User{}, &Token{}))
		stage := os.Getenv("PROFIT_RECOVERY_PROCESS_STAGE")
		if stage == "reserve_before_commit" || stage == "reserve_after_commit" {
			require.NoError(t, db.Create(&User{Id: 61, Username: "exit-reservation", Quota: 10000}).Error)
			require.NoError(t, db.Create(&Token{Id: 61, UserId: 61, Key: "exit-reservation", RemainQuota: 10000}).Error)
			if stage == "reserve_before_commit" {
				require.NoError(t, db.Callback().Update().After("gorm:update").Register("exit-inside-reservation", func(tx *gorm.DB) {
					if tx.Statement.Table == "tokens" {
						os.Exit(23)
					}
				}))
			}
			record := ChannelMonitorIncome{SettlementKey: ChannelMonitorIncomeKey("exit-reservation", "request"), UserID: 61, ChannelID: 61, FundingTokenID: 61, BillingSource: "wallet", Quota: 100, QuotaPerUnit: "100"}
			_, err := ReserveChannelMonitorIncome(t.Context(), &record, "exit-reservation")
			require.NoError(t, err)
			os.Exit(23)
		}
		require.NoError(t, db.Create(&User{Id: 61, Username: "exit-recovery", Quota: 9900}).Error)
		require.NoError(t, db.Create(&Token{Id: 61, UserId: 61, Key: "exit-recovery", RemainQuota: 9900, UsedQuota: 100}).Error)
		record := &ChannelMonitorIncome{
			SettlementKey: ChannelMonitorIncomeKey("exit-recovery", "request"), ChannelID: 61, UserID: 61,
			BillingSource: "wallet", Quota: 50, QuotaPerUnit: "100", Status: "funding_pending", FundingDelta: -50, FundingTokenID: 61,
		}
		require.NoError(t, PrepareChannelMonitorIncome(context.Background(), record))
		if os.Getenv("PROFIT_RECOVERY_PROCESS_STAGE") == "after_commit" {
			require.NoError(t, SettleChannelMonitorIncomeFunding(context.Background(), record.SettlementKey, 61, 0, 61, -50))
		}
		// Exit without cleanup, deferred database close or in-process retry.
		os.Exit(23)
	}
	for _, stage := range []string{"before_commit", "after_commit", "reserve_before_commit", "reserve_after_commit"} {
		t.Run(stage, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profit-process-recovery.db")
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestChannelMonitorIncomeFundingSurvivesProcessExit$", "-test.count=1")
			cmd.Env = append(os.Environ(), "PROFIT_RECOVERY_PROCESS_DB="+path, "PROFIT_RECOVERY_PROCESS_STAGE="+stage)
			output, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr, string(output))
			require.Equal(t, 23, exitErr.ExitCode(), string(output))
			db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
			require.NoError(t, err)
			previous, previousRedis := DB, common.RedisEnabled
			DB, common.RedisEnabled = db, false
			t.Cleanup(func() {
				DB, common.RedisEnabled = previous, previousRedis
				sqlDB, err := db.DB()
				require.NoError(t, err)
				assert.NoError(t, sqlDB.Close())
			})
			completed, err := RecoverChannelMonitorIncomeFunding(ctx, 100)
			require.NoError(t, err)
			if stage == "before_commit" {
				assert.Equal(t, 1, completed)
			} else {
				assert.Zero(t, completed)
			}
			var user User
			var token Token
			var income ChannelMonitorIncome
			require.NoError(t, db.First(&user, 61).Error)
			require.NoError(t, db.First(&token, 61).Error)
			if stage == "reserve_before_commit" {
				assert.Equal(t, 10000, user.Quota)
				assert.Equal(t, 10000, token.RemainQuota)
				assert.Zero(t, token.UsedQuota)
				var count int64
				require.NoError(t, db.Model(&ChannelMonitorIncome{}).Count(&count).Error)
				assert.Zero(t, count, "an uncommitted debit must not leave a reservation")
				return
			}
			require.NoError(t, db.First(&income).Error)
			if stage == "reserve_after_commit" {
				assert.Equal(t, 9900, user.Quota)
				assert.Equal(t, 9900, token.RemainQuota)
				assert.Equal(t, 100, token.UsedQuota)
				assert.EqualValues(t, 100, income.Quota)
				assert.Equal(t, "reserved", income.Status)
				return
			}
			assert.Equal(t, 9950, user.Quota)
			assert.Equal(t, 9950, token.RemainQuota)
			assert.Equal(t, 50, token.UsedQuota)
			assert.Equal(t, "settled", income.Status)
			assert.EqualValues(t, 500_000_000, income.IncomeNanoCNY)
			completed, err = RecoverChannelMonitorIncomeFunding(ctx, 100)
			require.NoError(t, err)
			assert.Zero(t, completed)
		})
	}
}

func TestChannelMonitorIncomeReservationDatabase(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			tables := []any{&ChannelMonitorIncome{}, &ChannelMonitorIncomeState{}, &ChannelMonitorIncomeGap{}, &User{}, &Token{}, &SubscriptionPlan{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{}, &ChannelLocalResponseRefund{}}
			require.NoError(t, db.AutoMigrate(tables...))
			ready, redisEnabled := ChannelMonitorIncomeReady.Swap(true), common.RedisEnabled
			common.RedisEnabled = false
			t.Cleanup(func() {
				ChannelMonitorIncomeReady.Store(ready)
				common.RedisEnabled = redisEnabled
				assert.NoError(t, db.Migrator().DropTable(tables...))
			})
			t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", t.TempDir())
			require.NoError(t, db.Create(&ChannelMonitorIncomeState{ID: 1}).Error)
			plan := SubscriptionPlan{Id: 98721, Title: "reservation", QuotaResetPeriod: "never"}
			require.NoError(t, db.Create(&plan).Error)
			for i, source := range []string{"wallet", "subscription"} {
				id := 98721 + i
				user := User{Id: id, Username: fmt.Sprintf("reserve-%d", id), AffCode: fmt.Sprint(id), Quota: 10000}
				token := Token{Id: id, UserId: id, Key: fmt.Sprintf("reserve-%d", id), RemainQuota: 10000}
				sub := UserSubscription{Id: id, UserId: id, PlanId: plan.Id, AmountTotal: 10000, Status: "active", EndTime: time.Now().Unix() + 86400}
				require.NoError(t, db.Create(&user).Error)
				require.NoError(t, db.Create(&token).Error)
				require.NoError(t, db.Create(&sub).Error)
				requestID := "reserve-" + source
				record := ChannelMonitorIncome{SettlementKey: ChannelMonitorIncomeKey(requestID, "request"), ChannelID: id, UserID: id, FundingTokenID: id, BillingSource: source, Quota: 100, QuotaPerUnit: "100"}
				require.NoError(t, db.Model(&token).Update("remain_quota", 99).Error)
				_, err := ReserveChannelMonitorIncome(t.Context(), &record, requestID)
				require.ErrorIs(t, err, ErrChannelMonitorTokenInsufficient)
				require.NoError(t, db.Model(&token).Update("remain_quota", 10000).Error)
				record.ID = 0
				// Failure after the source debit must roll back all three facts.
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("reservation-token-failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "tokens" {
						tx.AddError(errors.New("injected token failure"))
					}
				}))
				_, err = ReserveChannelMonitorIncome(t.Context(), &record, requestID)
				require.NoError(t, db.Callback().Update().Remove("reservation-token-failure"))
				require.Error(t, err)
				var count int64
				require.NoError(t, db.Model(&ChannelMonitorIncome{}).Where("user_id = ?", id).Count(&count).Error)
				assert.Zero(t, count)
				require.NoError(t, db.First(&user, id).Error)
				require.NoError(t, db.First(&sub, id).Error)
				assert.Equal(t, 10000, user.Quota)
				assert.Zero(t, sub.AmountUsed)
				record.ID = 0
				_, err = ReserveChannelMonitorIncome(t.Context(), &record, requestID)
				require.NoError(t, err)
				duplicate := record
				duplicate.ID = 0
				_, err = ReserveChannelMonitorIncome(t.Context(), &duplicate, requestID)
				require.Error(t, err, "duplicate request must not authorize a second upstream execution")
				require.NoError(t, AdjustChannelMonitorReservation(t.Context(), record.SettlementKey, id, 100, 150, true))
				require.Error(t, AdjustChannelMonitorReservation(t.Context(), record.SettlementKey, id, 100, 150, true), "stale adjustment cannot charge twice")
				require.NoError(t, db.First(&record, record.ID).Error)
				assert.EqualValues(t, 150, record.Quota)
				assert.Equal(t, "reserved", record.Status)
				completed, err := RecoverChannelMonitorIncomeFunding(t.Context(), 100)
				require.NoError(t, err)
				assert.Zero(t, completed)
				require.NoError(t, db.First(&user, id).Error)
				require.NoError(t, db.First(&token, id).Error)
				require.NoError(t, db.First(&sub, id).Error)
				if source == "wallet" {
					assert.Equal(t, 9850, user.Quota)
					assert.Zero(t, sub.AmountUsed)
				} else {
					assert.Equal(t, 10000, user.Quota)
					assert.EqualValues(t, 150, sub.AmountUsed)
				}
				assert.Equal(t, 9850, token.RemainQuota)
				assert.Equal(t, 150, token.UsedQuota)
				_, err = DeleteChannelMonitorIncomeBefore(t.Context(), record.DayStart+86400, 100, ChannelMonitorCleanupBudget{})
				require.NoError(t, err)
				require.NoError(t, db.First(&record, record.ID).Error, "retention must preserve orphan evidence")
				refund := ChannelLocalResponseRefund{RequestID: requestID, UserID: id, TokenID: id, TokenQuota: 150}
				if source == "wallet" {
					refund.WalletQuota = 150
				} else {
					refund.SubscriptionID, refund.SubscriptionQuota = id, 150
				}
				require.NoError(t, QueueChannelLocalResponseRefund(t.Context(), &refund))
				require.Error(t, AdjustChannelMonitorReservation(t.Context(), record.SettlementKey, id, 150, 200, true))
				for range 2 {
					require.NoError(t, ApplyChannelLocalResponseRefund(t.Context(), requestID))
				}
				require.NoError(t, db.First(&user, id).Error)
				require.NoError(t, db.First(&token, id).Error)
				require.NoError(t, db.First(&sub, id).Error)
				require.NoError(t, db.First(&record, record.ID).Error)
				assert.Equal(t, 10000, user.Quota)
				assert.Equal(t, 10000, token.RemainQuota)
				assert.Zero(t, token.UsedQuota)
				assert.Zero(t, sub.AmountUsed)
				assert.Zero(t, record.Quota)
				assert.Equal(t, "settled", record.Status)
				if source == "subscription" {
					legacyID := "reservation-existing-subscription-receipt"
					require.NoError(t, db.Create(&SubscriptionPreConsumeRecord{RequestId: legacyID, UserId: id, UserSubscriptionId: id, PreConsumed: 100, Status: "consumed"}).Error)
					candidate := ChannelMonitorIncome{SettlementKey: ChannelMonitorIncomeKey(legacyID, "request"), ChannelID: id, UserID: id, FundingTokenID: id, BillingSource: source, Quota: 100, QuotaPerUnit: "100"}
					_, err := ReserveChannelMonitorIncome(t.Context(), &candidate, legacyID)
					require.ErrorContains(t, err, "已有预扣记录")
					require.NoError(t, db.First(&token, id).Error)
					assert.Equal(t, 10000, token.RemainQuota, "an old subscription receipt cannot authorize a new token charge")
				}
			}
			// A committed reservation with a lost reply stays reserved. No
			// automatic recovery is allowed to turn it into income or a refund.
			redisServer := miniredis.RunT(t)
			oldRDB := common.RDB
			client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
			common.RDB, common.RedisEnabled = client, true
			t.Cleanup(func() { common.RDB = oldRDB; assert.NoError(t, client.Close()) })
			require.NoError(t, client.HSet(t.Context(), getUserCacheKey(98721), "Id", 98721, "CacheSchema", userCacheSchemaVersion, "Quota", 10000).Err())
			sqlDB, err := db.DB()
			require.NoError(t, err)
			ackLossDB := db.WithContext(t.Context())
			ackLossDB.Statement.ConnPool = &channelDailyCostCommitAckLossPool{sqlDB}
			DB = ackLossDB
			record := ChannelMonitorIncome{SettlementKey: ChannelMonitorIncomeKey("reserve-ack-loss", "request"), UserID: 98721, ChannelID: 98721, FundingTokenID: 98721, BillingSource: "wallet", Quota: 100, QuotaPerUnit: "100"}
			_, err = ReserveChannelMonitorIncome(t.Context(), &record, "reserve-ack-loss")
			DB = db
			require.Error(t, err)
			require.NoError(t, db.First(&record, record.ID).Error)
			assert.Equal(t, "reserved", record.Status)
			var user User
			require.NoError(t, db.First(&user, 98721).Error)
			assert.Equal(t, 9900, user.Quota)
			cachedQuota, err := GetUserQuota(98721, false)
			require.NoError(t, err)
			assert.Equal(t, 9900, cachedQuota, "lost COMMIT reply must invalidate stale admission quota")
			DB = ackLossDB
			err = AdjustChannelMonitorReservation(t.Context(), record.SettlementKey, record.UserID, 100, 150, true)
			DB = db
			require.ErrorIs(t, err, ErrTaskBillingCommitUncertain)
			require.NoError(t, db.First(&record, record.ID).Error)
			assert.EqualValues(t, 150, record.Quota, "uncertain adjustment must retain its committed amount for review")
			assert.Equal(t, "reserved", record.Status)
			completed, err := RecoverChannelMonitorIncomeFunding(t.Context(), 100)
			require.NoError(t, err)
			assert.Zero(t, completed)
		})
	}
}

func TestChannelMonitorIncomeManualReconciliationPreservesFunds(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			tables := []any{&ChannelMonitorIncome{}, &User{}, &Token{}, &UserSubscription{}, &SystemTask{}}
			require.NoError(t, db.AutoMigrate(tables...))
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
			user := User{Id: 62, Username: "review-income", Quota: 9950}
			token := Token{Id: 62, UserId: 62, Key: "review-income", RemainQuota: 9950, UsedQuota: 50}
			sub := UserSubscription{Id: 62, UserId: 62, AmountUsed: 50}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, db.Create(&token).Error)
			require.NoError(t, db.Create(&sub).Error)
			for _, status := range []string{"pending", "reserved", "funding_pending", "settled", "refund_pending"} {
				record := ChannelMonitorIncome{SettlementKey: ChannelMonitorIncomeKey("manual-"+status, "request"), UserID: 62, Quota: 100, IncomeNanoCNY: 1000000000, QuotaPerUnit: "100", BillingSource: "wallet", Status: status, UpdatedAt: 12345}
				require.NoError(t, db.Create(&record).Error)
				actual := int64(50)
				input := ChannelMonitorIncomeReconciliation{SettlementKey: record.SettlementKey, UserID: 62, ExpectedQuota: 100, ExpectedUpdatedAt: record.UpdatedAt, NetChargedQuota: &actual, Operator: "test-operator", Evidence: "isolated fixture: opening 10000, final net charge 50"}
				if status == "reserved" {
					require.ErrorContains(t, ReconcileChannelMonitorIncome(context.Background(), input), "已终止")
					input.RequestTerminated = true
				}
				if status != "pending" && status != "reserved" {
					require.Error(t, ReconcileChannelMonitorIncome(context.Background(), input))
					continue
				}
				missing := input
				missing.NetChargedQuota = nil
				require.Error(t, ReconcileChannelMonitorIncome(context.Background(), missing), "omitted net charge must not silently mean zero")
				stale := input
				stale.ExpectedQuota++
				require.Error(t, ReconcileChannelMonitorIncome(context.Background(), stale))
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("review-audit-failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "system_tasks" {
						tx.AddError(errors.New("injected review audit failure"))
					}
				}))
				err := ReconcileChannelMonitorIncome(context.Background(), input)
				require.NoError(t, db.Callback().Create().Remove("review-audit-failure"))
				require.Error(t, err)
				require.NoError(t, db.First(&record, record.ID).Error)
				assert.Equal(t, status, record.Status)
				assert.EqualValues(t, 100, record.Quota, "audit failure rolls back the income correction")
				require.NoError(t, ReconcileChannelMonitorIncome(context.Background(), input))
				require.Error(t, ReconcileChannelMonitorIncome(context.Background(), input), "a repeated review cannot overwrite an already confirmed record")
				require.NoError(t, db.First(&record, record.ID).Error)
				assert.Equal(t, "settled", record.Status)
				assert.EqualValues(t, 50, record.Quota)
				assert.EqualValues(t, 500000000, record.IncomeNanoCNY)
				assert.Zero(t, record.CostRecorded, "manual income reconciliation does not claim complete cost")
				var audit SystemTask
				require.NoError(t, db.Where("type = ?", "channel_monitor_income_reconcile").First(&audit).Error)
				assert.Contains(t, audit.Payload, input.Evidence)
				assert.Contains(t, audit.Payload, input.Operator)
			}
			require.NoError(t, db.First(&user, 62).Error)
			require.NoError(t, db.First(&token, 62).Error)
			require.NoError(t, db.First(&sub, 62).Error)
			assert.Equal(t, 9950, user.Quota)
			assert.Equal(t, 9950, token.RemainQuota)
			assert.Equal(t, 50, token.UsedQuota)
			assert.EqualValues(t, 50, sub.AmountUsed)
		})
	}
}

func TestChannelMonitorIncomeFundingConfirmationIsAtomic(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			tables := []any{&ChannelMonitorIncome{}, &ChannelMonitorIncomeState{}, &ChannelMonitorIncomeGap{}, &User{}, &Token{}, &UserSubscription{}}
			require.NoError(t, db.AutoMigrate(tables...))
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
			// Upgrade a representative income table written before replay
			// instructions existed. Existing pending records remain untrusted.
			for _, field := range []string{"FundingDelta", "FundingTokenID", "FundingSubscriptionID"} {
				require.NoError(t, db.Migrator().DropColumn(&ChannelMonitorIncome{}, field))
			}
			require.NoError(t, db.Omit("FundingDelta", "FundingTokenID", "FundingSubscriptionID").Create(&ChannelMonitorIncome{
				SettlementKey: "pre-upgrade-income", UserID: 9998, Quota: 42, IncomeNanoCNY: 420, Status: "pending", BillingSource: "wallet",
				DayStart: ChannelDailyCostDayStart(time.Now().Unix()),
			}).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&ChannelMonitorIncome{}))
				var historical ChannelMonitorIncome
				require.NoError(t, db.Where("settlement_key = ?", "pre-upgrade-income").First(&historical).Error)
				assert.EqualValues(t, 42, historical.Quota)
				assert.EqualValues(t, 420, historical.IncomeNanoCNY)
				assert.Equal(t, "pending", historical.Status)
			}
			ready := ChannelMonitorIncomeReady.Swap(true)
			t.Cleanup(func() { ChannelMonitorIncomeReady.Store(ready) })
			t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", t.TempDir())
			require.NoError(t, db.Create(&ChannelMonitorIncomeState{ID: 1}).Error)
			legacy := ChannelMonitorIncome{SettlementKey: "legacy-without-funding-evidence", UserID: 9999, Quota: 100, Status: "pending", BillingSource: "wallet"}
			require.NoError(t, db.Create(&legacy).Error)
			for i, source := range []string{"wallet", "subscription"} {
				for j, delta := range []int{-50, 0, 50} {
					id := 600 + i*10 + j
					user := User{Id: id, Username: fmt.Sprintf("funding-%d", id), AffCode: fmt.Sprint(id), Quota: 9900}
					token := Token{Id: id, UserId: id, Key: fmt.Sprintf("funding-%d", id), RemainQuota: 9900, UsedQuota: 100}
					sub := UserSubscription{Id: id, UserId: id, AmountTotal: 10000, AmountUsed: 100}
					require.NoError(t, db.Create(&user).Error)
					require.NoError(t, db.Create(&token).Error)
					require.NoError(t, db.Create(&sub).Error)
					income := ChannelMonitorIncome{SettlementKey: fmt.Sprintf("funding-%d", id), UserID: id, Quota: int64(100 + delta), Status: "pending", BillingSource: source}
					require.NoError(t, db.Create(&income).Error)
					require.NoError(t, QueueChannelMonitorIncomeFunding(context.Background(), &income, id, id, delta))
					require.NoError(t, QueueChannelMonitorIncomeFunding(context.Background(), &income, id, id, delta))
					require.ErrorContains(t, QueueChannelMonitorIncomeFunding(context.Background(), &income, id, id, delta+1), "冲突")
					// Reporting retention must not delete a live financial intent.
					legacy.DayStart = ChannelDailyCostDayStart(time.Now().Unix())
					require.NoError(t, db.Model(&legacy).Update("day_start", legacy.DayStart).Error)
					_, err := DeleteChannelMonitorIncomeBefore(context.Background(), legacy.DayStart, 100, ChannelMonitorCleanupBudget{})
					require.NoError(t, err)
					require.NoError(t, db.First(&income, income.ID).Error)
					require.NoError(t, db.Callback().Update().Before("gorm:update").Register("confirm-failure", func(tx *gorm.DB) {
						if tx.Statement.Table == "channel_monitor_incomes" {
							tx.AddError(errors.New("injected confirmation failure after funding"))
						}
					}))
					err = SettleChannelMonitorIncomeFunding(context.Background(), income.SettlementKey, id, id, id, delta)
					require.NoError(t, db.Callback().Update().Remove("confirm-failure"))
					require.Error(t, err)
					require.NoError(t, db.First(&user, id).Error)
					require.NoError(t, db.First(&token, id).Error)
					require.NoError(t, db.First(&sub, id).Error)
					require.NoError(t, db.First(&income, income.ID).Error)
					assert.Equal(t, 9900, user.Quota)
					assert.Equal(t, 9900, token.RemainQuota)
					assert.EqualValues(t, 100, sub.AmountUsed)
					assert.Equal(t, "funding_pending", income.Status)
					// Commit succeeds but its acknowledgement is lost; checking
					// the durable confirmation makes the caller safe to retry.
					sqlDB, err := db.DB()
					require.NoError(t, err)
					ackLossDB := db.WithContext(context.Background())
					ackLossDB.Statement.ConnPool = &channelDailyCostCommitAckLossPool{sqlDB}
					DB = ackLossDB
					// Recovery receives no request/session or caller-provided delta.
					// It must restore the operation from the saved instruction alone.
					completed, err := RecoverChannelMonitorIncomeFunding(context.Background(), 100)
					DB = db
					require.NoError(t, err)
					assert.Equal(t, 1, completed)
					require.NoError(t, SettleChannelMonitorIncomeFunding(context.Background(), income.SettlementKey, id, id, id, delta))
					require.NoError(t, db.First(&user, id).Error)
					require.NoError(t, db.First(&token, id).Error)
					require.NoError(t, db.First(&sub, id).Error)
					require.NoError(t, db.First(&income, income.ID).Error)
					if source == "wallet" {
						assert.Equal(t, 9900-delta, user.Quota)
						assert.EqualValues(t, 100, sub.AmountUsed)
					} else {
						assert.Equal(t, 9900, user.Quota)
						assert.EqualValues(t, 100+delta, sub.AmountUsed)
					}
					assert.Equal(t, 9900-delta, token.RemainQuota)
					assert.Equal(t, 100+delta, token.UsedQuota)
					assert.Equal(t, "settled", income.Status)
				}
			}
			completed, err := RecoverChannelMonitorIncomeFunding(context.Background(), 100)
			require.NoError(t, err)
			assert.Zero(t, completed, "settled and legacy pending records are not replayed")
			require.NoError(t, db.First(&legacy, legacy.ID).Error)
			assert.Equal(t, "pending", legacy.Status)
		})
	}
}

func TestChannelMonitorIncomeGapFollowsCrossDayCost(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			tables := []any{&ChannelMonitorIncome{}, &ChannelMonitorIncomeState{}, &ChannelMonitorIncomeGap{}}
			require.NoError(t, db.AutoMigrate(tables...))
			ready := ChannelMonitorIncomeReady.Swap(true)
			t.Cleanup(func() {
				ChannelMonitorIncomeReady.Store(ready)
				channelMonitorPendingIncomeGaps.Clear()
				assert.NoError(t, db.Migrator().DropTable(tables...))
			})
			dir := t.TempDir()
			t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", dir)
			require.NoError(t, InitializeChannelMonitorIncome(db, true))
			day := ChannelDailyCostDayStart(time.Now().Unix()) - 86400
			for i, costFirst := range []bool{false, true} {
				eventID := fmt.Sprintf("cross-day-gap-%d", i)
				ctx := context.Background()
				if !costFirst {
					MarkChannelMonitorIncomeGapForCost(ctx, 42, day, eventID)
					gaps, err := ReadChannelMonitorIncomeGapJournal()
					require.NoError(t, err)
					assert.Equal(t, int64(math.MaxInt64), gaps[len(gaps)-1].To, "unfinished session has no known final day")
				}
				require.NoError(t, StoreChannelDailyCostOutboxEvents(ctx, []ChannelDailyCostDelta{{EventId: eventID, ChannelId: 42, OccurredAt: day + 86400, CostNanoCNY: 20, SettledDelta: 1}}))
				now := time.Now().Unix()
				claimed, err := ClaimChannelDailyCostOutboxEvents(ctx, "cross-day-test", now, now, time.Minute, 10)
				require.NoError(t, err)
				require.Len(t, claimed, 1)
				require.NoError(t, ApplyClaimedChannelDailyCostOutboxEvents(ctx, "cross-day-test", []int64{claimed[0].Id}, now))
				if costFirst {
					DB = nil
					MarkChannelMonitorIncomeGapForCost(ctx, 42, day, eventID)
					channelMonitorPendingIncomeGaps.Clear()
					DB = db
				}
				channelMonitorPendingIncomeGaps.Clear()
				require.NoError(t, RestoreChannelMonitorIncomeGaps(ctx))
				gaps, err := ReadChannelMonitorIncomeGapJournal()
				require.NoError(t, err)
				for _, gap := range gaps {
					assert.Equal(t, day+86400, gap.From)
					assert.Equal(t, day+2*86400, gap.To)
					assert.Equal(t, 42, gap.ChannelID)
				}
			}
			// Retaining the outbox away must not lose the final gap day.
			require.NoError(t, db.Where("channel_id = ?", 42).Delete(&ChannelDailyCostOutbox{}).Error)
			gaps, err := ReadChannelMonitorIncomeGapJournal()
			require.NoError(t, err)
			require.Len(t, gaps, 2)
			for _, gap := range gaps {
				assert.Equal(t, day+86400, gap.From)
			}
			_, err = DeleteChannelMonitorIncomeBefore(context.Background(), day+2*86400, 100, ChannelMonitorCleanupBudget{})
			require.NoError(t, err)
			gaps, err = ReadChannelMonitorIncomeGapJournal()
			require.NoError(t, err)
			assert.Empty(t, gaps)
		})
	}
}

func TestChannelMonitorIncomeMidjourneyRefundRecovery(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			tables := []any{&ChannelMonitorIncome{}, &Midjourney{}, &User{}, &Token{}, &Channel{}}
			require.NoError(t, db.AutoMigrate(tables...))
			ready := ChannelMonitorIncomeReady.Swap(true)
			t.Cleanup(func() {
				ChannelMonitorIncomeReady.Store(ready)
				assert.NoError(t, db.Migrator().DropTable(tables...))
			})
			const balance = common.MaxQuota + 10000
			user := User{Id: 420, Username: "mj-refund", Quota: balance}
			token := Token{Id: 420, UserId: user.Id, Key: "mj-refund", RemainQuota: balance}
			channel := Channel{Id: 420}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, db.Create(&token).Error)
			require.NoError(t, db.Create(&channel).Error)
			task := Midjourney{UserId: user.Id, ChannelId: channel.Id, MjId: "mj-refund", Status: "IN_PROGRESS", Progress: "0%"}
			require.NoError(t, db.Create(&task).Error)
			income := ChannelMonitorIncome{SettlementKey: ChannelMonitorIncomeKey(fmt.Sprint(task.Id), "midjourney"), Quota: 100, QuotaPerUnit: "100", USDToCNY: "1", Status: "pending"}
			require.NoError(t, db.Create(&income).Error)
			for _, operation := range []string{"charge", "refund"} {
				for _, table := range []string{"users", "tokens", "channels", "midjourneys", "channel_monitor_incomes"} {
					require.NoError(t, db.Callback().Update().After("gorm:update").Register("mj-fault", func(tx *gorm.DB) {
						if tx.Statement.Table == table {
							tx.AddError(errors.New("injected Midjourney write failure"))
						}
					}))
					var err error
					if operation == "charge" {
						_, _, err = SettleMidjourneyBilling(context.Background(), task.Id, MidjourneyPendingBilling{Quota: 100, ChannelID: channel.Id}, token.Id)
					} else {
						_, err = RefundMidjourneyBilling(context.Background(), task.Id)
					}
					require.NoError(t, db.Callback().Update().Remove("mj-fault"))
					require.Error(t, err)
					require.NoError(t, db.First(&user, user.Id).Error)
					require.NoError(t, db.First(&token, token.Id).Error)
					require.NoError(t, db.First(&task, task.Id).Error)
					require.NoError(t, db.First(&income, income.ID).Error)
					if operation == "charge" {
						assert.Equal(t, balance, user.Quota)
						assert.Equal(t, balance, token.RemainQuota)
						assert.Zero(t, task.Quota)
						assert.Equal(t, "pending", income.Status)
					} else {
						assert.Equal(t, balance-100, user.Quota)
						assert.Equal(t, balance-100, token.RemainQuota)
						assert.Equal(t, 100, task.Quota)
						assert.EqualValues(t, 100, income.Quota)
						assert.True(t, HasUnfinishedMidjourneyTasks())
						assert.Len(t, GetAllUnFinishTasks(), 1)
					}
				}
				if operation == "charge" {
					for attempt := range 2 {
						_, applied, err := SettleMidjourneyBilling(context.Background(), task.Id, MidjourneyPendingBilling{Quota: 100, ChannelID: channel.Id}, token.Id)
						require.NoError(t, err)
						assert.Equal(t, attempt == 0, applied)
					}
					require.NoError(t, db.Model(&task).Updates(map[string]any{"status": "FAILURE", "progress": "100%"}).Error)
				}
			}
			stale := task
			for range 2 {
				_, err := RefundMidjourneyBilling(context.Background(), task.Id)
				require.NoError(t, err)
			}
			_, err := stale.UpdateWithStatus("FAILURE")
			require.NoError(t, err)
			require.NoError(t, stale.Update())
			_, applied, err := SettleMidjourneyBilling(context.Background(), task.Id, MidjourneyPendingBilling{Quota: 100, ChannelID: channel.Id}, token.Id)
			require.NoError(t, err)
			assert.False(t, applied)
			require.NoError(t, db.First(&user, user.Id).Error)
			require.NoError(t, db.First(&token, token.Id).Error)
			require.NoError(t, db.First(&channel, channel.Id).Error)
			require.NoError(t, db.First(&income, income.ID).Error)
			assert.Equal(t, balance, user.Quota)
			assert.Equal(t, balance, token.RemainQuota)
			assert.Zero(t, user.UsedQuota)
			assert.Equal(t, 1, user.RequestCount)
			assert.Zero(t, token.UsedQuota)
			assert.Zero(t, channel.UsedQuota)
			assert.Zero(t, income.Quota)
			assert.False(t, HasUnfinishedMidjourneyTasks())
			// Removing an upstream after charging must not strand the refund.
			removedChannelTask := Midjourney{UserId: user.Id, ChannelId: channel.Id, MjId: "removed-channel"}
			require.NoError(t, db.Create(&removedChannelTask).Error)
			_, _, err = SettleMidjourneyBilling(context.Background(), removedChannelTask.Id, MidjourneyPendingBilling{Quota: 100, ChannelID: channel.Id}, token.Id)
			require.NoError(t, err)
			require.NoError(t, db.Delete(&channel).Error)
			_, err = RefundMidjourneyBilling(context.Background(), removedChannelTask.Id)
			require.NoError(t, err)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, balance, user.Quota)
		})
	}
}

func TestChannelMonitorIncomeInitialTaskFundingIsAtomic(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			tables := []any{&ChannelMonitorIncome{}, &Task{}, &User{}, &Token{}, &Channel{}, &UserSubscription{}}
			require.NoError(t, db.AutoMigrate(tables...))
			ready := ChannelMonitorIncomeReady.Swap(true)
			t.Cleanup(func() {
				ChannelMonitorIncomeReady.Store(ready)
				assert.NoError(t, db.Migrator().DropTable(tables...))
			})
			for i, source := range []string{"wallet", "subscription", "wallet", "subscription"} {
				target := 100
				if i >= 2 {
					target = 0
				}
				user := User{Id: 300 + i, Username: fmt.Sprintf("initial-task-%d", i), AffCode: fmt.Sprintf("it%d", i), Quota: 9850, UsedQuota: target}
				token := Token{Id: 300 + i, UserId: user.Id, Key: fmt.Sprintf("initial-task-%d", i), RemainQuota: 9850, UsedQuota: 150}
				channel := Channel{Id: 300 + i, UsedQuota: int64(target)}
				subscription := UserSubscription{Id: 300 + i, UserId: user.Id, AmountTotal: 10000, AmountUsed: 150}
				require.NoError(t, db.Create(&user).Error)
				require.NoError(t, db.Create(&token).Error)
				require.NoError(t, db.Create(&channel).Error)
				require.NoError(t, db.Create(&subscription).Error)
				task := Task{TaskID: fmt.Sprintf("initial-task-%d", i), UserId: user.Id, ChannelId: channel.Id, Status: TaskStatusInProgress, Quota: target,
					PrivateData: TaskPrivateData{BillingSource: source, TokenId: token.Id, SubscriptionId: subscription.Id,
						Execution: &TaskExecutionSnapshot{RequestID: fmt.Sprintf("initial-request-%d", i)}}}
				income := ChannelMonitorIncome{SettlementKey: ChannelMonitorIncomeKey(task.PrivateData.Execution.RequestID, "request"), Quota: int64(target), QuotaPerUnit: "100", USDToCNY: "1", Status: "pending"}
				require.NoError(t, db.Create(&income).Error)
				for _, failingTable := range []string{"tasks", "users", "tokens", "channel_monitor_incomes"} {
					if source == "subscription" && failingTable == "users" {
						failingTable = "user_subscriptions"
					}
					fault := func(tx *gorm.DB) {
						if tx.Statement.Table == failingTable {
							tx.AddError(errors.New("injected initial settlement failure"))
						}
					}
					require.NoError(t, db.Callback().Create().After("gorm:create").Register("initial-task-failure", fault))
					require.NoError(t, db.Callback().Update().After("gorm:update").Register("initial-task-failure", fault))
					err := InsertTaskWithBilling(context.Background(), &task, 150)
					require.NoError(t, db.Callback().Create().Remove("initial-task-failure"))
					require.NoError(t, db.Callback().Update().Remove("initial-task-failure"))
					require.Error(t, err)
					require.NoError(t, db.First(&user, user.Id).Error)
					require.NoError(t, db.First(&token, token.Id).Error)
					require.NoError(t, db.First(&subscription, subscription.Id).Error)
					require.NoError(t, db.First(&income, income.ID).Error)
					assert.Equal(t, 9850, user.Quota)
					assert.Equal(t, 9850, token.RemainQuota)
					assert.EqualValues(t, 150, subscription.AmountUsed)
					assert.Equal(t, "pending", income.Status)
					var count int64
					require.NoError(t, db.Model(&Task{}).Where("task_id = ?", task.TaskID).Count(&count).Error)
					assert.Zero(t, count, "polling must never see an unfunded target quota")
				}
				// The server committed but the client did not receive COMMIT's
				// acknowledgement. The durable row prevents a second refund.
				sqlDB, err := db.DB()
				require.NoError(t, err)
				ackLossDB := db.WithContext(context.Background())
				ackLossDB.Statement.ConnPool = &channelDailyCostCommitAckLossPool{sqlDB}
				DB = ackLossDB
				err = InsertTaskWithBilling(context.Background(), &task, 150)
				DB = db
				require.NoError(t, err)
				require.NoError(t, db.First(&income, income.ID).Error)
				assert.Equal(t, "settled", income.Status)
				assert.EqualValues(t, target, income.Quota)
				for range 2 {
					_, err := ApplyTaskBilling(context.Background(), &task, TaskBillingOperationRefund, 0)
					require.NoError(t, err)
				}
				require.NoError(t, db.First(&user, user.Id).Error)
				require.NoError(t, db.First(&token, token.Id).Error)
				require.NoError(t, db.First(&subscription, subscription.Id).Error)
				if source == "wallet" {
					assert.Equal(t, 10000, user.Quota)
				} else {
					assert.EqualValues(t, 0, subscription.AmountUsed)
					assert.Equal(t, 9850, user.Quota)
				}
				assert.Equal(t, 10000, token.RemainQuota)
				assert.Zero(t, token.UsedQuota)
			}
		})
	}
}

func TestChannelMonitorIncomeRetentionAllowsLateTaskBilling(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			tables := []any{&ChannelMonitorIncome{}, &ChannelMonitorIncomeState{}, &ChannelMonitorIncomeGap{}, &Task{}, &User{}, &Channel{}, &Token{}, &UserSubscription{}, &ChannelTaskCostEvent{}}
			require.NoError(t, db.AutoMigrate(tables...))
			previousReady := ChannelMonitorIncomeReady.Load()
			t.Cleanup(func() {
				ChannelMonitorIncomeReady.Store(previousReady)
				assert.NoError(t, db.Migrator().DropTable(tables...))
			})
			t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", t.TempDir())
			require.NoError(t, InitializeChannelMonitorIncome(db, true))
			oldDay := ChannelDailyCostDayStart(time.Now().Add(-48 * time.Hour).Unix())
			for i, target := range []int{0, 150, 0, 150} {
				user := User{Id: 100 + i, Username: fmt.Sprintf("late-task-%d", i), AffCode: fmt.Sprintf("late%d", i), Quota: 9900, UsedQuota: 100}
				channel := Channel{Id: 100 + i, UsedQuota: 100}
				token := Token{Id: 100 + i, UserId: user.Id, Key: fmt.Sprintf("late-task-token-%d", i), RemainQuota: 9900, UsedQuota: 100}
				require.NoError(t, db.Create(&user).Error)
				require.NoError(t, db.Create(&channel).Error)
				require.NoError(t, db.Create(&token).Error)
				task := Task{TaskID: fmt.Sprintf("late-task-%d", i), UserId: user.Id, ChannelId: channel.Id,
					SubmitTime: oldDay, Status: TaskStatusInProgress, Quota: 100,
					PrivateData: TaskPrivateData{TokenId: token.Id, BillingContext: &TaskBillingContext{ChannelCostEventId: fmt.Sprintf("late-cost-%d", i)}}}
				if i >= 2 {
					subscription := UserSubscription{Id: 100 + i, UserId: user.Id, AmountTotal: 10000, AmountUsed: 100}
					require.NoError(t, db.Create(&subscription).Error)
					task.PrivateData.BillingSource = taskBillingSubscriptionSource
					task.PrivateData.SubscriptionId = subscription.Id
				}
				require.NoError(t, db.Create(&task).Error)
				_, err := RegisterChannelTaskCostEvent(context.Background(), ChannelTaskCostEventInput{
					TaskID: task.ID, CostEventId: taskBillingCostEventID(&task), ChannelId: channel.Id, OccurredAt: oldDay, InitialQuota: 100, CostNanoCNY: 80,
				})
				require.NoError(t, err)
				retention, err := DeleteChannelMonitorCostsBefore(context.Background(), oldDay+86400, oldDay+86400, oldDay+86400, 100, ChannelMonitorCleanupBudget{})
				require.NoError(t, err)
				assert.EqualValues(t, 1, retention.TaskCostEventRowsDeleted)
				operation := TaskBillingOperationSettle
				if target == 0 {
					operation = TaskBillingOperationRefund
				}
				for range 2 {
					_, err = ApplyTaskBilling(context.Background(), &task, operation, target)
					require.NoError(t, err)
				}
				require.NoError(t, db.First(&user, user.Id).Error)
				require.NoError(t, db.First(&task, task.ID).Error)
				wantWallet := 10000 - target
				if i >= 2 {
					wantWallet = 9900
					var subscription UserSubscription
					require.NoError(t, db.First(&subscription, task.PrivateData.SubscriptionId).Error)
					assert.EqualValues(t, target, subscription.AmountUsed)
				}
				assert.Equal(t, wantWallet, user.Quota)
				assert.Equal(t, target, user.UsedQuota)
				assert.Equal(t, target, task.Quota)
				require.NoError(t, db.First(&token, token.Id).Error)
				require.NoError(t, db.First(&channel, channel.Id).Error)
				assert.Equal(t, 10000-target, token.RemainQuota)
				assert.Equal(t, target, token.UsedQuota)
				assert.EqualValues(t, target, channel.UsedQuota)
				var count int64
				require.NoError(t, db.Model(&ChannelDailyCost{}).Count(&count).Error)
				assert.Zero(t, count, "late funding corrections must not recreate expired reports")
				// A missing non-expired event still aborts and rolls back funding.
				require.NoError(t, db.Model(&task).Update("submit_time", oldDay+86400).Error)
				_, err = ApplyTaskBilling(context.Background(), &task, TaskBillingOperationSettle, 200)
				require.ErrorContains(t, err, "resolved task cost event is missing")
				require.NoError(t, db.First(&user, user.Id).Error)
				assert.Equal(t, wantWallet, user.Quota)
			}
		})
	}
}

func TestChannelMonitorIncomeJournalFaultDoesNotBlockStartupOrRetention(t *testing.T) {
	db := setupChannelDailyCostBatchDatabase(t, "sqlite")
	previousDB, previousReady := DB, ChannelMonitorIncomeReady.Load()
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		ChannelMonitorIncomeReady.Store(previousReady)
	})
	require.NoError(t, db.AutoMigrate(&ChannelMonitorIncome{}, &ChannelMonitorIncomeState{}, &ChannelMonitorIncomeGap{}))
	dir := t.TempDir()
	t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", dir)
	broken := filepath.Join(dir, "broken.gap")
	require.NoError(t, os.WriteFile(broken, nil, 0600))
	require.NoError(t, InitializeChannelMonitorIncome(db, true))
	assert.True(t, ChannelMonitorIncomeReady.Load(), "healthy database income recording must continue")
	_, err := ReadChannelMonitorIncomeGapJournal()
	require.Error(t, err, "profit must remain unconfirmed")

	day := ChannelDailyCostDayStart(time.Now().Add(-48 * time.Hour).Unix())
	for channel := range 10001 {
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d_%d.gap", day, channel)), nil, 0600))
	}
	active := filepath.Join(dir, fmt.Sprintf("%d_1.gap", day+86400))
	require.NoError(t, os.WriteFile(active, nil, 0600))
	oldIncome := ChannelMonitorIncome{SettlementKey: "expired-income", DayStart: day}
	require.NoError(t, db.Create(&oldIncome).Error)
	incomplete, err := DeleteChannelMonitorIncomeBefore(context.Background(), day+86400, 100, ChannelMonitorCleanupBudget{})
	require.NoError(t, err)
	assert.True(t, incomplete, "bad evidence is preserved and reported")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 2, "all expired entries are removed despite query limit and malformed entry")
	assert.FileExists(t, active)
	assert.FileExists(t, broken)
	var count int64
	require.NoError(t, db.Model(&ChannelMonitorIncome{}).Count(&count).Error)
	assert.Zero(t, count, "filesystem faults must not block database retention")
	require.NoError(t, os.Remove(broken))
	for range 2 {
		require.NoError(t, InitializeChannelMonitorIncome(db, true))
		incomplete, err = DeleteChannelMonitorIncomeBefore(context.Background(), day+86400, 100, ChannelMonitorCleanupBudget{})
		require.NoError(t, err)
		assert.False(t, incomplete)
	}
	file := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(file, nil, 0600))
	t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", filepath.Join(file, "journal"))
	require.NoError(t, InitializeChannelMonitorIncome(db, true))
	_, err = ReadChannelMonitorIncomeGapJournal()
	require.Error(t, err)
}

func runChannelMonitorIncomeLedgerCases(t *testing.T, db *gorm.DB) {
	t.Helper()
	journalDir := t.TempDir()
	t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", journalDir)
	require.NoError(t, db.AutoMigrate(&ChannelMonitorIncome{}, &ChannelMonitorIncomeState{}, &ChannelMonitorIncomeGap{}, &ChannelDailyCostOutbox{}, &Task{}, &User{}))

	previousDB := DB
	previousReady := ChannelMonitorIncomeReady.Load()
	previousGap := channelMonitorIncomeGap.Load()
	DB = db
	ChannelMonitorIncomeReady.Store(false)
	channelMonitorIncomeGap.Store(0)
	t.Cleanup(func() {
		assert.NoError(t, db.Migrator().DropTable(&ChannelMonitorIncome{}, &ChannelMonitorIncomeState{}, &ChannelMonitorIncomeGap{}, &Task{}, &User{}))
		DB = previousDB
		ChannelMonitorIncomeReady.Store(previousReady)
		channelMonitorIncomeGap.Store(previousGap)
	})

	state := ChannelMonitorIncomeState{ID: 1, StartedAt: 1_700_000_000, GapSince: 1_700_000_123}
	require.NoError(t, db.Create(&state).Error)
	require.NoError(t, InitializeChannelMonitorIncome(db, false))
	assert.True(t, ChannelMonitorIncomeReady.Load())
	assert.Equal(t, state.GapSince, ChannelMonitorIncomeGapSince())
	previousBatch := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() { common.BatchUpdateEnabled = previousBatch })
	user := User{Id: 991, Username: "income-durable-wallet", Quota: 1000}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, DecreaseUserQuota(user.Id, 100, false))
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 900, user.Quota, "confirmed income must have a durable wallet charge even in batch mode")
	require.NoError(t, IncreaseUserQuota(user.Id, 100, false))
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 1000, user.Quota, "confirmed refunds must also reach the database immediately")

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
	conflicting := *record
	conflicting.ID, conflicting.UserID = 0, 18
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("income-duplicate-read-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "channel_monitor_incomes" {
			tx.AddError(errors.New("injected duplicate income read failure"))
		}
	}))
	err := PrepareChannelMonitorIncome(context.Background(), &conflicting)
	require.NoError(t, db.Callback().Query().Remove("income-duplicate-read-failure"))
	require.Error(t, err, "a duplicate write plus failed verification cannot authorize different funding")

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
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("income-cost-read-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "channel_daily_cost_outboxes" {
			tx.AddError(errors.New("injected cost attribution read failure"))
		}
	}))
	require.NoError(t, PrepareChannelMonitorIncome(context.Background(), lateIncome))
	require.NoError(t, db.Callback().Query().Remove("income-cost-read-failure"))
	assert.Zero(t, lateIncome.CostRecorded, "a failed cost read must not imply confirmed cost")
	require.NoError(t, ConfirmChannelMonitorIncome(context.Background(), lateIncome.SettlementKey))
	_, err = DeleteProcessedChannelDailyCostOutboxEvents(context.Background(), time.Now().Unix(), 100)
	require.NoError(t, err)
	var retainedCost ChannelDailyCostOutbox
	require.NoError(t, db.Where("event_id = ?", lateCostEvent.EventId).First(&retainedCost).Error, "cleanup must preserve a pending attribution dependency")
	recovered, err := RecoverChannelMonitorIncomeCosts(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, 1, recovered, "already settled income also needs attribution recovery")
	require.NoError(t, db.First(lateIncome, lateIncome.ID).Error)
	assert.Equal(t, 1, lateIncome.CostRecorded)
	assert.Equal(t, ChannelDailyCostDayStart(lateCostEvent.OccurredAt), lateIncome.DayStart)
	assert.EqualValues(t, 100, lateIncome.Quota, "cost recovery must never change charged quota")
	recovered, err = RecoverChannelMonitorIncomeCosts(context.Background(), 100)
	require.NoError(t, err)
	assert.Zero(t, recovered)
	found, changed, err := MarkChannelMonitorIncomeRefundPending(context.Background(), lateIncome.SettlementKey)
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, changed)
	var refundIncome ChannelMonitorIncome
	require.NoError(t, db.Where("settlement_key = ?", lateIncome.SettlementKey).First(&refundIncome).Error)
	assert.Equal(t, "refund_pending", refundIncome.Status)
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
	// A refund can overlap the original funding confirmation. It must remain
	// excluded until the refund finishes, and failed refunds restore funding.
	for _, key := range []string{"refund-before-confirm", "refund-before-prepare"} {
		mj := &Midjourney{Id: 1, UserId: 17, ChannelId: 9}
		if key == "refund-before-prepare" {
			mj.Id = 2
		}
		require.NoError(t, PrepareChannelMonitorMidjourneyRefund(context.Background(), mj))
		refundKey := ChannelMonitorIncomeKey(fmt.Sprint(mj.Id), "midjourney")
		_, _, err := MarkChannelMonitorIncomeRefundPending(context.Background(), refundKey)
		require.NoError(t, err)
		if key == "refund-before-prepare" {
			require.NoError(t, RefundChannelMonitorIncome(context.Background(), refundKey))
		}
		initial := &ChannelMonitorIncome{SettlementKey: refundKey, ChannelID: 9, UserID: 17, BillingSource: "wallet", QuotaPerUnit: "100", Quota: 100, CostEventID: "refund-cost-" + key}
		require.NoError(t, PrepareChannelMonitorIncome(context.Background(), initial))
		require.NoError(t, ConfirmChannelMonitorIncome(context.Background(), refundKey))
		var observed ChannelMonitorIncome
		require.NoError(t, db.Where("settlement_key = ?", refundKey).First(&observed).Error)
		if key == "refund-before-prepare" {
			assert.Zero(t, observed.Quota)
			assert.Equal(t, "settled", observed.Status)
		} else {
			assert.Equal(t, "refund_pending", observed.Status)
			require.NoError(t, CancelChannelMonitorIncomeRefund(context.Background(), refundKey))
			require.NoError(t, db.Where("settlement_key = ?", refundKey).First(&observed).Error)
			assert.Equal(t, "settled", observed.Status)
			assert.EqualValues(t, 100, observed.Quota)
		}
		require.NoError(t, db.Delete(&observed).Error)
	}

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
	task.ChannelId, task.SubmitTime, task.Quota = 9, costAt, 25
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, CompleteChannelMonitorTaskIncome(context.Background(), task.ID))
	require.NoError(t, db.Where("settlement_key = ?", record.SettlementKey).First(&saved).Error)
	assert.EqualValues(t, 25, saved.Quota)
	assert.EqualValues(t, 250_000_000, saved.IncomeNanoCNY)
	assert.Equal(t, ChannelDailyCostDayStart(costAt), saved.DayStart)

	// A database outage retains the scoped failure in memory, and the next
	// healthy write persists it without spreading the gap to later days.
	DB = nil
	MarkChannelMonitorIncomeGapAt(context.Background(), 9, costAt)
	DB = db
	FlushChannelMonitorIncomeGaps(context.Background())
	MarkChannelMonitorIncomeGapAt(context.Background(), 9, costAt)
	var gaps []ChannelMonitorIncomeGap
	require.NoError(t, db.Where("channel_id = ?", 9).Find(&gaps).Error)
	require.Len(t, gaps, 1)
	assert.Equal(t, ChannelDailyCostDayStart(costAt), gaps[0].From)
	assert.Equal(t, gaps[0].From+86400, gaps[0].To)
	assert.Empty(t, PendingChannelMonitorIncomeGaps())
	// A different process has no access to the first node's in-memory map.
	// The immutable shared journal still preserves the failure after restart.
	DB = nil
	MarkChannelMonitorIncomeGapAt(context.Background(), 10, costAt)
	channelMonitorPendingIncomeGaps.Clear()
	DB = db
	require.NoError(t, RestoreChannelMonitorIncomeGaps(context.Background()))
	var restored ChannelMonitorIncomeGap
	require.NoError(t, db.Where("channel_id = ?", 10).First(&restored).Error)
	assert.Equal(t, ChannelDailyCostDayStart(costAt), restored.From)
	require.NoError(t, RestoreChannelMonitorIncomeGaps(context.Background()))
	require.NoError(t, db.Model(&ChannelMonitorIncomeGap{}).Where("channel_id = ?", 10).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	broken := filepath.Join(journalDir, "broken.gap")
	require.NoError(t, os.WriteFile(broken, nil, 0600))
	_, err = ReadChannelMonitorIncomeGapJournal()
	require.Error(t, err, "unreadable attribution must not imply complete coverage")
	require.NoError(t, os.Remove(broken))

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
	require.NoError(t, db.Model(&ChannelMonitorIncomeGap{}).Where("to_at <= ?", cutoff).Count(&count).Error)
	assert.Zero(t, count)
	assert.ErrorIs(t, CompleteChannelMonitorTaskIncome(context.Background(), task.ID), gorm.ErrRecordNotFound)
}
