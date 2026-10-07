package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
		require.NoError(t, db.AutoMigrate(&ChannelMonitorIncome{}, &ChannelMonitorIncomeRecoveryCursor{}, &ChannelMonitorIncomeGap{}, &ChannelDailyCostOutbox{}, &User{}, &Token{}))
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

func TestChannelMonitorIncomeIndependentReservationsDoNotWaitForBatchLocks(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			tables := []any{&ChannelMonitorIncome{}, &User{}, &Token{}}
			require.NoError(t, db.AutoMigrate(tables...))
			oldBatch, oldRedis := common.BatchUpdateEnabled, common.RedisEnabled
			common.BatchUpdateEnabled, common.RedisEnabled = false, false
			t.Cleanup(func() {
				common.BatchUpdateEnabled, common.RedisEnabled = oldBatch, oldRedis
				assert.NoError(t, db.Migrator().DropTable(tables...))
			})
			for _, id := range []int{98901, 98902} {
				require.NoError(t, db.Create(&User{Id: id, Username: fmt.Sprint(id), AffCode: fmt.Sprint(id), Quota: 1000}).Error)
				require.NoError(t, db.Create(&Token{Id: id, UserId: id, Key: fmt.Sprint(id), RemainQuota: 1000}).Error)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register("hold-first-reservation", func(tx *gorm.DB) {
				if income, ok := tx.Statement.Dest.(*ChannelMonitorIncome); ok && income.UserID == 98901 {
					close(entered)
					<-release
				}
			}))
			firstDone, secondDone := make(chan error, 1), make(chan error, 1)
			go func() {
				income := ChannelMonitorIncome{SettlementKey: ChannelMonitorIncomeKey("independent-first", "request"), UserID: 98901, ChannelID: 1, FundingTokenID: 98901, BillingSource: "wallet", Quota: 100, QuotaPerUnit: "100"}
				_, err := ReserveChannelMonitorIncome(t.Context(), &income, "independent-first")
				firstDone <- err
			}()
			select {
			case <-entered:
			case err := <-firstDone:
				t.Fatalf("first reservation did not reach the held transaction: %v", err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			go func() {
				income := ChannelMonitorIncome{SettlementKey: ChannelMonitorIncomeKey("independent-second", "request"), UserID: 98902, ChannelID: 1, FundingTokenID: 98902, BillingSource: "wallet", Quota: 100, QuotaPerUnit: "100"}
				_, err := ReserveChannelMonitorIncome(ctx, &income, "independent-second")
				secondDone <- err
			}()
			select {
			case err := <-secondDone:
				assert.NoError(t, err, "independent account must finish while the first transaction has not written")
				close(release)
			case <-ctx.Done():
				close(release)
				assert.NoError(t, <-secondDone, "batching disabled: another account must not consume this request's deadline")
				t.Error("independent reservation waited for the first account")
			}
			require.NoError(t, <-firstDone)
			require.NoError(t, db.Callback().Create().Remove("hold-first-reservation"))
			for _, id := range []int{98901, 98902} {
				var user User
				var token Token
				require.NoError(t, db.First(&user, id).Error)
				require.NoError(t, db.First(&token, id).Error)
				assert.Equal(t, 900, user.Quota)
				assert.Equal(t, 900, token.RemainQuota)
			}
			common.BatchUpdateEnabled = true
			addNewRecord(BatchUpdateTypeUserQuota, 98902, -10)
			addNewRecord(BatchUpdateTypeTokenQuota, 98902, -10)
			income := ChannelMonitorIncome{SettlementKey: ChannelMonitorIncomeKey("batch-protected", "request"), UserID: 98902, ChannelID: 1, FundingTokenID: 98902, BillingSource: "wallet", Quota: 100, QuotaPerUnit: "100"}
			_, err := ReserveChannelMonitorIncome(t.Context(), &income, "batch-protected")
			require.ErrorContains(t, err, "批次待落库")
			batchUpdate()
			_, err = ReserveChannelMonitorIncome(t.Context(), &income, "batch-protected")
			require.NoError(t, err)
			var user User
			var token Token
			require.NoError(t, db.First(&user, 98902).Error)
			require.NoError(t, db.First(&token, 98902).Error)
			assert.Equal(t, 790, user.Quota)
			assert.Equal(t, 790, token.RemainQuota)
			assert.Equal(t, 210, token.UsedQuota)
		})
	}
}

func TestChannelMonitorIncomeRefundPreservesSubscriptionPeriodAndMissingRecords(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			tables := []any{&ChannelMonitorIncome{}, &ChannelLocalResponseRefund{}, &User{}, &Token{}, &SubscriptionPlan{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{}}
			require.NoError(t, db.AutoMigrate(tables...))
			ready, redisEnabled := ChannelMonitorIncomeReady.Swap(true), common.RedisEnabled
			common.RedisEnabled = false
			t.Cleanup(func() {
				ChannelMonitorIncomeReady.Store(ready)
				common.RedisEnabled = redisEnabled
				assert.NoError(t, db.Migrator().DropTable(tables...))
			})
			for i, scenario := range []string{"reset", "expired", "deleted_subscription", "deleted_token"} {
				t.Run(scenario, func(t *testing.T) {
					id := 98800 + i
					key := fmt.Sprintf("refund-period-%d", i)
					user := User{Id: id, Username: key, AffCode: key, Quota: 1000}
					token := Token{Id: id, UserId: id, Key: key, RemainQuota: 1000}
					plan := SubscriptionPlan{Id: id, Title: key, QuotaResetPeriod: "never"}
					sub := UserSubscription{Id: id, UserId: id, PlanId: id, AmountTotal: 1000, Status: "active", EndTime: time.Now().Unix() + 86400}
					require.NoError(t, db.Create(&user).Error)
					require.NoError(t, db.Create(&token).Error)
					require.NoError(t, db.Create(&plan).Error)
					require.NoError(t, db.Create(&sub).Error)
					income := ChannelMonitorIncome{SettlementKey: ChannelMonitorIncomeKey(key, "request"), UserID: id, ChannelID: id, FundingTokenID: id, BillingSource: "subscription", Quota: 100, QuotaPerUnit: "100"}
					_, err := ReserveChannelMonitorIncome(t.Context(), &income, key)
					require.NoError(t, err)
					refund := ChannelLocalResponseRefund{RequestID: key, UserID: id, TokenID: id, SubscriptionID: id, TokenQuota: 100, SubscriptionQuota: 100}
					require.NoError(t, QueueChannelLocalResponseRefund(t.Context(), &refund))
					require.NoError(t, db.First(&sub, id).Error)
					require.NoError(t, db.First(&token, id).Error)
					wantUsed := int64(0)
					switch scenario {
					case "reset":
						var receipt SubscriptionPreConsumeRecord
						require.NoError(t, db.Where("request_id = ?", key).First(&receipt).Error)
						require.NoError(t, db.Model(&sub).Updates(map[string]any{"last_reset_time": receipt.CreatedAt + 1, "amount_used": 25}).Error)
						wantUsed = 25
					case "expired":
						require.NoError(t, db.Model(&sub).Updates(map[string]any{"status": "expired", "end_time": time.Now().Unix() - 1}).Error)
					case "deleted_subscription":
						require.NoError(t, db.Unscoped().Delete(&sub).Error)
					case "deleted_token":
						require.NoError(t, db.Unscoped().Delete(&token).Error)
					}
					if scenario == "deleted_subscription" || scenario == "deleted_token" {
						require.Error(t, ApplyChannelLocalResponseRefund(t.Context(), key))
						require.NoError(t, db.First(&income, income.ID).Error)
						assert.Equal(t, "refund_pending", income.Status)
						require.NoError(t, db.First(&refund, refund.ID).Error)
						assert.False(t, refund.Applied)
						if scenario == "deleted_subscription" {
							require.NoError(t, db.Create(&sub).Error)
						} else {
							require.NoError(t, db.Create(&token).Error)
						}
					}
					for range 2 {
						require.NoError(t, ApplyChannelLocalResponseRefund(t.Context(), key))
					}
					require.NoError(t, db.First(&sub, id).Error)
					require.NoError(t, db.First(&token, id).Error)
					require.NoError(t, db.First(&user, id).Error)
					require.NoError(t, db.First(&income, income.ID).Error)
					assert.Equal(t, wantUsed, sub.AmountUsed)
					assert.Equal(t, 1000, token.RemainQuota)
					assert.Zero(t, token.UsedQuota)
					assert.Equal(t, 1000, user.Quota)
					assert.Equal(t, "settled", income.Status)
					assert.Zero(t, income.Quota)
				})
			}
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

type channelMonitorIncomeCommitPool struct {
	*sql.DB
	afterCommit func() error
}

func (pool channelMonitorIncomeCommitPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := pool.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &channelMonitorIncomeCommitTx{Tx: tx, afterCommit: pool.afterCommit}, nil
}

type channelMonitorIncomeCommitTx struct {
	*sql.Tx
	afterCommit func() error
}

func (tx channelMonitorIncomeCommitTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	return tx.afterCommit()
}

func TestChannelMonitorIncomePreservesCacheFirstReads(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			t.Cleanup(initCol)
			db := setupChannelDailyCostBatchDatabase(t, engine)
			initCol()
			tables := []any{&ChannelMonitorIncome{}, &User{}, &Token{}}
			require.NoError(t, db.AutoMigrate(tables...))
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
			oldRDB, oldRedis, oldReady := common.RDB, common.RedisEnabled, ChannelMonitorIncomeReady.Swap(true)
			common.RDB, common.RedisEnabled = client, true
			t.Cleanup(func() {
				common.RDB, common.RedisEnabled = oldRDB, oldRedis
				ChannelMonitorIncomeReady.Store(oldReady)
				assert.NoError(t, client.Close())
			})
			for i, scenario := range []string{"invalidation_failure", "late_snapshot", "debit_failure", "debit_late_snapshot"} {
				t.Run(scenario, func(t *testing.T) {
					defer server.SetError("")
					delta := -50
					if i >= 2 {
						delta = 50
					}
					id := 98101 + i
					key := fmt.Sprintf("stale-funding-%d", id)
					user := User{Id: id, Username: key, AffCode: key, Quota: 9900}
					token := Token{Id: id, UserId: id, Key: key, Status: common.TokenStatusEnabled,
						ExpiredTime: -1, RemainQuota: 9900, UsedQuota: 100}
					income := ChannelMonitorIncome{SettlementKey: key, UserID: id, BillingSource: "wallet",
						Status: "funding_pending", FundingTokenID: id, FundingDelta: delta}
					require.NoError(t, db.Create(&user).Error)
					require.NoError(t, db.Create(&token).Error)
					require.NoError(t, db.Create(&income).Error)
					_, err := GetUserCache(id)
					require.NoError(t, err)
					_, err = GetTokenByKey(key, false)
					require.NoError(t, err)
					// A warm read must not query the database just because monitoring
					// is ready. Treat any attempted SQL query as a regression.
					require.NoError(t, db.Callback().Query().Before("gorm:query").Register("audit:cache_hit_query", func(tx *gorm.DB) {
						tx.AddError(errors.New("warm cache read unexpectedly queried database"))
					}))
					t.Cleanup(func() { assert.NoError(t, db.Callback().Query().Remove("audit:cache_hit_query")) })
					warmQuota, err := GetUserQuota(id, false)
					require.NoError(t, err)
					assert.Equal(t, 9900, warmQuota)
					warmUser, err := GetUserCache(id)
					require.NoError(t, err)
					assert.Equal(t, 9900, warmUser.Quota)
					warmToken, err := ValidateUserToken(key)
					require.NoError(t, err)
					assert.Equal(t, 9900, warmToken.RemainQuota)
					require.NoError(t, db.Callback().Query().Remove("audit:cache_hit_query"))
					if i%2 == 0 {
						server.SetError("injected Redis outage")
					}
					err = SettleChannelMonitorIncomeFunding(t.Context(), key, id, 0, id, delta)
					server.SetError("")
					require.NoError(t, err)
					if i%2 == 1 {
						// Generation captured before the concurrent DB read.
						require.NoError(t, populateUserCache(user, ""))
						_, err := cacheInitToken(token, "")
						require.NoError(t, err)
						assert.False(t, server.Exists(getUserCacheKey(id)))
						assert.False(t, server.Exists(getTokenCacheKey(key)))
					} else {
						quota, err := GetUserQuota(id, false)
						require.NoError(t, err)
						assert.Equal(t, 9900, quota, "temporary outage lag is allowed")
						server.SetError("still unavailable")
						_, err = RecoverChannelMonitorFundingCaches(t.Context(), 10)
						require.Error(t, err)
						server.SetError("")
					}
					recovered, err := RecoverChannelMonitorFundingCaches(t.Context(), 10)
					require.NoError(t, err)
					assert.Equal(t, 2, recovered)
					recovered, err = RecoverChannelMonitorFundingCaches(t.Context(), 10)
					require.NoError(t, err)
					assert.Zero(t, recovered)
					// No TTL wait: durable repair removed the stale hashes.
					require.NoError(t, populateUserCache(user, ""))
					_, err = cacheInitToken(token, "")
					require.NoError(t, err)
					quota, err := GetUserQuota(id, false)
					require.NoError(t, err)
					assert.Equal(t, 9900-delta, quota)
					cachedUser, err := GetUserCache(id)
					require.NoError(t, err)
					assert.Equal(t, 9900-delta, cachedUser.Quota)
					cachedToken, err := ValidateUserToken(key)
					require.NoError(t, err)
					assert.Equal(t, 9900-delta, cachedToken.RemainQuota)
					assert.Equal(t, 100+delta, cachedToken.UsedQuota)
					server.SetError("injected Redis read outage")
					fallbackQuota, err := GetUserQuota(id, false)
					require.NoError(t, err)
					assert.Equal(t, 9900-delta, fallbackQuota)
					fallbackToken, err := GetTokenByKey(key, false)
					require.NoError(t, err)
					assert.Equal(t, 9900-delta, fallbackToken.RemainQuota)
					require.NoError(t, SettleChannelMonitorIncomeFunding(t.Context(), key, id, 0, id, delta))
					require.NoError(t, db.First(&user, id).Error)
					assert.Equal(t, 9900-delta, user.Quota, "cache failure must not repeat a committed refund")
				})
			}
		})
	}
}

func TestChannelMonitorIncomeFundingCacheMatchesCommittedBalance(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			t.Cleanup(initCol)
			db := setupChannelDailyCostBatchDatabase(t, engine)
			initCol()
			tables := []any{&ChannelMonitorIncome{}, &User{}, &Token{}, &UserSubscription{}}
			require.NoError(t, db.AutoMigrate(tables...))
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			oldRDB, oldRedis := common.RDB, common.RedisEnabled
			common.RDB, common.RedisEnabled = client, true
			t.Cleanup(func() {
				common.RDB, common.RedisEnabled = oldRDB, oldRedis
				assert.NoError(t, client.Close())
			})
			sqlDB, err := db.DB()
			require.NoError(t, err)
			id := 9900
			for _, source := range []string{"wallet", "subscription"} {
				for _, delta := range []int{-50, 50} {
					for _, scenario := range []string{"warm", "hydrate_after_commit", "ack_lost", "ack_and_readback_lost"} {
						t.Run(fmt.Sprintf("%s/%d/%s", source, delta, scenario), func(t *testing.T) {
							t.Cleanup(func() { DB = db })
							id++
							key := fmt.Sprintf("funding-cache-%d", id)
							user := User{Id: id, Username: key, AffCode: key, Quota: 9900}
							token := Token{Id: id, UserId: id, Key: key, RemainQuota: 9900, UsedQuota: 100}
							sub := UserSubscription{Id: id, UserId: id, AmountUsed: 100}
							income := ChannelMonitorIncome{SettlementKey: key, UserID: id, BillingSource: source, Status: "funding_pending", FundingTokenID: id, FundingSubscriptionID: id, FundingDelta: delta}
							require.NoError(t, db.Create(&user).Error)
							require.NoError(t, db.Create(&token).Error)
							require.NoError(t, db.Create(&sub).Error)
							require.NoError(t, db.Create(&income).Error)
							if scenario == "warm" || scenario == "ack_and_readback_lost" {
								_, err := GetUserCache(id)
								require.NoError(t, err)
								_, err = GetTokenByKey(key, false)
								require.NoError(t, err)
							}
							commitDB := db.WithContext(t.Context())
							commitDB.Statement.ConnPool = &channelMonitorIncomeCommitPool{DB: sqlDB, afterCommit: func() error {
								if scenario == "hydrate_after_commit" || scenario == "ack_lost" {
									// A concurrent reader sees the newly committed DB values
									// before the settlement caller updates Redis.
									_, err := GetUserCache(id)
									require.NoError(t, err)
									_, err = GetTokenByKey(key, false)
									require.NoError(t, err)
								}
								if scenario == "ack_and_readback_lost" {
									require.NoError(t, db.Callback().Query().Before("gorm:query").Register("income-readback-failure", func(tx *gorm.DB) {
										if tx.Statement.Table == "channel_monitor_incomes" {
											tx.AddError(errors.New("injected readback failure"))
										}
									}))
								}
								if scenario == "ack_lost" || scenario == "ack_and_readback_lost" {
									return context.DeadlineExceeded
								}
								return nil
							}}
							DB = commitDB
							err := SettleChannelMonitorIncomeFunding(t.Context(), key, id, id, id, delta)
							DB = db
							if scenario == "ack_and_readback_lost" {
								require.NoError(t, db.Callback().Query().Remove("income-readback-failure"))
								require.ErrorIs(t, err, ErrTaskBillingCommitUncertain)
							} else {
								require.NoError(t, err)
							}
							require.NoError(t, db.First(&user, id).Error)
							require.NoError(t, db.First(&token, id).Error)
							require.NoError(t, db.First(&sub, id).Error)
							wantWallet := 9900
							if source == "wallet" {
								wantWallet -= delta
								assert.EqualValues(t, 100, sub.AmountUsed)
							} else {
								assert.EqualValues(t, 100+delta, sub.AmountUsed)
							}
							assert.Equal(t, wantWallet, user.Quota)
							assert.Equal(t, 9900-delta, token.RemainQuota)
							for range 2 {
								cachedUser, err := GetUserCache(id)
								require.NoError(t, err)
								cachedToken, err := GetTokenByKey(key, false)
								require.NoError(t, err)
								assert.Equal(t, user.Quota, cachedUser.Quota)
								assert.Equal(t, token.RemainQuota, cachedToken.RemainQuota)
								assert.Equal(t, token.UsedQuota, cachedToken.UsedQuota)
								require.NoError(t, SettleChannelMonitorIncomeFunding(t.Context(), key, id, id, id, delta))
							}
							server.FlushAll()
						})
					}
				}
			}
		})
	}
}

func TestChannelMonitorIncomeTaskCacheMatchesCommittedBalance(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			t.Cleanup(initCol)
			db := setupChannelDailyCostBatchDatabase(t, engine)
			initCol()
			tables := []any{&User{}, &Token{}, &Channel{}, &Task{}, &Midjourney{}, &UserSubscription{}}
			require.NoError(t, db.AutoMigrate(tables...))
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			oldRDB, oldRedis, ready := common.RDB, common.RedisEnabled, ChannelMonitorIncomeReady.Swap(false)
			common.RDB, common.RedisEnabled = client, true
			t.Cleanup(func() {
				common.RDB, common.RedisEnabled = oldRDB, oldRedis
				ChannelMonitorIncomeReady.Store(ready)
				assert.NoError(t, client.Close())
				assert.NoError(t, db.Migrator().DropTable(tables...))
			})
			sqlDB, err := db.DB()
			require.NoError(t, err)
			id := 9950
			for _, tc := range []struct {
				operation, source string
				delta             int
			}{
				{"initial", "wallet", -50}, {"initial", "subscription", 50},
				{"settle", "wallet", 50}, {"settle", "subscription", -50},
				{"refund", "wallet", -100}, {"refund", "subscription", -100},
				{"mj_charge", "wallet", 100}, {"mj_refund", "wallet", -100},
			} {
				for _, ackLost := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/ack_lost_%t", tc.operation, tc.source, ackLost), func(t *testing.T) {
						t.Cleanup(func() { DB = db })
						id++
						key := fmt.Sprintf("task-cache-%d", id)
						user := User{Id: id, Username: key, AffCode: key, Quota: 9900, UsedQuota: 100}
						token := Token{Id: id, UserId: id, Key: key, RemainQuota: 9900, UsedQuota: 100}
						sub := UserSubscription{Id: id, UserId: id, AmountUsed: 100}
						channel := Channel{Id: id, UsedQuota: 100}
						require.NoError(t, db.Create(&user).Error)
						require.NoError(t, db.Create(&token).Error)
						require.NoError(t, db.Create(&sub).Error)
						require.NoError(t, db.Create(&channel).Error)
						task := Task{TaskID: key, UserId: id, ChannelId: id, Quota: 100, Status: TaskStatusInProgress, PrivateData: TaskPrivateData{TokenId: id, SubscriptionId: id, BillingSource: tc.source}}
						mj := Midjourney{UserId: id, ChannelId: id, TokenId: id, Status: "IN_PROGRESS"}
						switch tc.operation {
						case "initial":
							task.Quota += tc.delta
						case "mj_charge", "mj_refund":
							if tc.operation == "mj_refund" {
								mj.Quota, mj.BillingChannelId = 100, id
							}
							require.NoError(t, db.Create(&mj).Error)
						default:
							require.NoError(t, db.Create(&task).Error)
						}
						commitDB := db.WithContext(t.Context())
						commitDB.Statement.ConnPool = &channelMonitorIncomeCommitPool{DB: sqlDB, afterCommit: func() error {
							_, err := GetUserCache(id)
							require.NoError(t, err)
							_, err = GetTokenByKey(key, false)
							require.NoError(t, err)
							if ackLost {
								return context.DeadlineExceeded
							}
							return nil
						}}
						DB = commitDB
						switch tc.operation {
						case "initial":
							err = InsertTaskWithBilling(t.Context(), &task, 100)
						case "settle":
							_, err = ApplyTaskBilling(t.Context(), &task, TaskBillingOperationSettle, 100+tc.delta)
						case "refund":
							_, err = ApplyTaskBilling(t.Context(), &task, TaskBillingOperationRefund, 0)
						case "mj_charge":
							_, _, err = SettleMidjourneyBilling(t.Context(), mj.Id, MidjourneyPendingBilling{Quota: 100, ChannelID: id}, id)
						case "mj_refund":
							_, err = RefundMidjourneyBilling(t.Context(), mj.Id)
						}
						DB = db
						if ackLost && tc.operation != "initial" {
							require.Error(t, err)
						} else {
							require.NoError(t, err)
						}
						cachedUser, err := GetUserCache(id)
						require.NoError(t, err)
						cachedToken, err := GetTokenByKey(key, false)
						require.NoError(t, err)
						wantWallet := 9900
						if tc.source == "wallet" {
							wantWallet -= tc.delta
						}
						assert.Equal(t, wantWallet, cachedUser.Quota)
						assert.Equal(t, 9900-tc.delta, cachedToken.RemainQuota)
						assert.Equal(t, 100+tc.delta, cachedToken.UsedQuota)
						server.FlushAll()
					})
				}
			}
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

func TestChannelMonitorIncomeRecoveryFairnessAndConcurrentWorkers(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			tables := []any{&ChannelMonitorIncome{}, &User{}, &Token{}}
			require.NoError(t, db.AutoMigrate(tables...))
			previousRedis := common.RedisEnabled
			common.RedisEnabled = false
			t.Cleanup(func() {
				common.RedisEnabled = previousRedis
				assert.NoError(t, db.Migrator().DropTable(tables...))
			})
			user := User{Id: 98201, Username: "income-worker", AffCode: "income-worker", Quota: 900}
			token := Token{Id: user.Id, UserId: user.Id, Key: "income-worker", RemainQuota: 900, UsedQuota: 100}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, db.Create(&token).Error)
			invalid := ChannelMonitorIncome{SettlementKey: "invalid-worker-record", UserID: user.Id,
				BillingSource: "wallet", Status: "funding_pending", Quota: 150, FundingDelta: 50,
				FundingTokenID: token.Id + 1, UpdatedAt: 1}
			valid := invalid
			valid.SettlementKey, valid.FundingTokenID, valid.UpdatedAt = "valid-worker-record", token.Id, 2
			require.NoError(t, db.Create(&invalid).Error)
			require.NoError(t, db.Create(&valid).Error)
			completed, err := RecoverChannelMonitorIncomeFunding(t.Context(), 1)
			require.Error(t, err)
			assert.Zero(t, completed)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 900, user.Quota, "missing token rolls back the wallet")
			completed, err = RecoverChannelMonitorIncomeFunding(t.Context(), 1)
			require.NoError(t, err)
			assert.Equal(t, 1, completed, "failed oldest record must not starve the next batch")
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 850, user.Quota)
			require.NoError(t, db.Delete(&invalid).Error)
			// A separate final instruction refunds 50. Both workers must read it
			// before either enters settlement; the transaction owns idempotency.
			valid.ID, valid.SettlementKey, valid.Status = 0, "two-worker-record", "funding_pending"
			valid.Quota, valid.FundingDelta = 50, -50
			require.NoError(t, db.Create(&valid).Error)
			canceled, cancel := context.WithCancel(t.Context())
			cancel()
			completed, err = RecoverChannelMonitorIncomeFunding(canceled, 1)
			require.ErrorIs(t, err, context.Canceled)
			assert.Zero(t, completed)
			ctx, stop := context.WithTimeout(t.Context(), 10*time.Second)
			defer stop()
			loaded, release := make(chan struct{}, 2), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			var workers sync.WaitGroup
			t.Cleanup(func() {
				unblock()
				stop()
				workers.Wait()
				assert.NoError(t, db.Callback().Query().Remove("audit:recovery_snapshot"))
			})
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("audit:recovery_snapshot", func(tx *gorm.DB) {
				if _, ok := tx.Statement.Dest.(*[]ChannelMonitorIncome); !ok || tx.Error != nil {
					return
				}
				loaded <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					tx.AddError(ctx.Err())
				}
			}))
			results := make(chan error, 2)
			for range 2 {
				workers.Go(func() {
					_, err := RecoverChannelMonitorIncomeFunding(ctx, 1)
					results <- err
				})
			}
			for range 2 {
				select {
				case <-loaded:
				case <-ctx.Done():
					require.NoError(t, ctx.Err())
				}
			}
			unblock()
			workers.Wait()
			for range 2 {
				require.NoError(t, <-results)
			}
			require.NoError(t, db.First(&user, user.Id).Error)
			require.NoError(t, db.First(&token, token.Id).Error)
			require.NoError(t, db.First(&valid, valid.ID).Error)
			assert.Equal(t, 900, user.Quota)
			assert.Equal(t, 900, token.RemainQuota)
			assert.Equal(t, 100, token.UsedQuota)
			assert.Equal(t, "settled", valid.Status)
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

func TestChannelMonitorIncomeSubscriptionRefundPeriods(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			tables := []any{&ChannelMonitorIncome{}, &User{}, &Token{}, &Channel{}, &Task{}, &SubscriptionPlan{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{}, &ChannelLocalResponseRefund{}}
			require.NoError(t, db.AutoMigrate(tables...))
			ready, redisEnabled, batchEnabled := ChannelMonitorIncomeReady.Swap(true), common.RedisEnabled, common.BatchUpdateEnabled
			common.RedisEnabled, common.BatchUpdateEnabled = false, false
			t.Cleanup(func() {
				ChannelMonitorIncomeReady.Store(ready)
				common.RedisEnabled, common.BatchUpdateEnabled = redisEnabled, batchEnabled
				assert.NoError(t, db.Migrator().DropTable(tables...))
			})
			id := 98300
			for _, reset := range []bool{false, true} {
				for _, path := range []string{"official_delta", "direct_settlement", "recovery", "task_settle", "task_refund", "extra_reserve", "local_refund", "extra_then_local_refund", "extra_reset_again", "receipt_cleanup_refund", "receipt_cleanup_before_queue", "refund_reset_after_queue"} {
					t.Run(fmt.Sprintf("%s/reset_%t", path, reset), func(t *testing.T) {
						id++
						key := fmt.Sprintf("audit-closure-%d", id)
						require.NoError(t, db.Create(&User{Id: id, Username: key, AffCode: key, Quota: 1000, UsedQuota: 100}).Error)
						require.NoError(t, db.Create(&Token{Id: id, UserId: id, Key: key, RemainQuota: 1000}).Error)
						require.NoError(t, db.Create(&Channel{Id: id, UsedQuota: 100}).Error)
						require.NoError(t, db.Create(&SubscriptionPlan{Id: id, Title: key, QuotaResetPeriod: "never"}).Error)
						require.NoError(t, db.Create(&UserSubscription{Id: id, UserId: id, PlanId: id, AmountTotal: 1000, Status: "active", EndTime: time.Now().Unix() + 86400}).Error)
						income := ChannelMonitorIncome{SettlementKey: ChannelMonitorIncomeKey(key, "request"), UserID: id, ChannelID: id, FundingTokenID: id, BillingSource: "subscription", Quota: 100, QuotaPerUnit: "100"}
						_, err := ReserveChannelMonitorIncome(t.Context(), &income, key)
						require.NoError(t, err)
						task := Task{TaskID: key, UserId: id, ChannelId: id, Quota: 100, Status: TaskStatusInProgress, SubmitTime: time.Now().Unix(), PrivateData: TaskPrivateData{BillingSource: "subscription", SubscriptionId: id, TokenId: id, Execution: &TaskExecutionSnapshot{RequestID: key}}}
						if path == "task_settle" || path == "task_refund" {
							require.NoError(t, InsertTaskWithBilling(t.Context(), &task, 100))
						}
						if reset {
							var receipt SubscriptionPreConsumeRecord
							require.NoError(t, db.Where("request_id = ?", key).First(&receipt).Error)
							require.NoError(t, db.Model(&UserSubscription{}).Where("id = ?", id).Updates(map[string]any{"last_reset_time": receipt.CreatedAt + 1, "amount_used": 25}).Error)
						}
						wantUsed, wantToken := int64(50), 950
						if reset {
							wantUsed = 0
						}
						switch path {
						case "official_delta":
							require.NoError(t, PostConsumeUserSubscriptionDelta(id, -50))
							wantToken = 900
						case "direct_settlement", "recovery":
							final := income
							final.ID, final.Quota, final.Status, final.FundingDelta = 0, 50, "funding_pending", -50
							require.NoError(t, PrepareChannelMonitorIncome(t.Context(), &final))
							if path == "recovery" {
								n, err := RecoverChannelMonitorIncomeFunding(t.Context(), 100)
								require.NoError(t, err)
								require.Equal(t, 1, n)
								n, err = RecoverChannelMonitorIncomeFunding(t.Context(), 100)
								require.NoError(t, err)
								require.Zero(t, n)
							} else {
								for range 2 {
									require.NoError(t, SettleChannelMonitorIncomeFunding(t.Context(), income.SettlementKey, id, id, id, -50))
								}
							}
						case "task_settle":
							for range 2 {
								_, err := ApplyTaskBilling(t.Context(), &task, TaskBillingOperationSettle, 50)
								require.NoError(t, err)
							}
						case "task_refund":
							wantUsed, wantToken = 0, 1000
							for range 2 {
								_, err := ApplyTaskBilling(t.Context(), &task, TaskBillingOperationRefund, 0)
								require.NoError(t, err)
							}
						case "extra_reserve":
							wantUsed, wantToken = 150, 850
							if reset {
								wantUsed = 75
							}
							require.NoError(t, AdjustChannelMonitorReservation(t.Context(), income.SettlementKey, id, 100, 150, true))
						case "extra_then_local_refund", "extra_reset_again":
							wantUsed, wantToken = 0, 1000
							if reset {
								wantUsed = 25
							}
							require.NoError(t, AdjustChannelMonitorReservation(t.Context(), income.SettlementKey, id, 100, 150, true))
							if path == "extra_reset_again" {
								require.NoError(t, db.Model(&UserSubscription{}).Where("id = ?", id).Updates(map[string]any{"last_reset_time": time.Now().Unix() + 10, "amount_used": 30}).Error)
								require.NoError(t, AdjustChannelMonitorReservation(t.Context(), income.SettlementKey, id, 150, 200, true))
								wantUsed = 30
							}
							refund := ChannelLocalResponseRefund{RequestID: key, UserID: id, TokenID: id, SubscriptionID: id, TokenQuota: 150, SubscriptionQuota: 150}
							if path == "extra_reset_again" {
								refund.TokenQuota, refund.SubscriptionQuota = 200, 200
							}
							require.NoError(t, QueueChannelLocalResponseRefund(t.Context(), &refund))
							for range 2 {
								require.NoError(t, ApplyChannelLocalResponseRefund(t.Context(), key))
							}
						case "receipt_cleanup_refund", "receipt_cleanup_before_queue", "refund_reset_after_queue":
							wantUsed, wantToken = 0, 1000
							if reset {
								wantUsed = 25
							}
							refund := ChannelLocalResponseRefund{RequestID: key, UserID: id, TokenID: id, SubscriptionID: id, TokenQuota: 100, SubscriptionQuota: 100}
							if path != "receipt_cleanup_before_queue" {
								require.NoError(t, QueueChannelLocalResponseRefund(t.Context(), &refund))
							}
							require.NoError(t, db.Model(&SubscriptionPreConsumeRecord{}).Where("request_id = ?", key).Updates(map[string]any{"created_at": GetDBTimestamp() - 8*86400, "updated_at": GetDBTimestamp() - 8*86400}).Error)
							deleted, err := CleanupSubscriptionPreConsumeRecords(7 * 86400)
							require.NoError(t, err)
							require.EqualValues(t, 1, deleted)
							if path == "receipt_cleanup_before_queue" {
								require.NoError(t, QueueChannelLocalResponseRefund(t.Context(), &refund))
							}
							if path == "refund_reset_after_queue" {
								wantUsed = 30
								require.NoError(t, db.Model(&UserSubscription{}).Where("id = ?", id).Updates(map[string]any{"last_reset_time": time.Now().Unix() + 10, "amount_used": 30}).Error)
							}
							for range 2 {
								require.NoError(t, ApplyChannelLocalResponseRefund(t.Context(), key))
							}
							require.NoError(t, db.First(&refund, refund.ID).Error)
							assert.True(t, refund.Applied)
						case "local_refund":
							wantUsed, wantToken = 0, 1000
							if reset {
								wantUsed = 25
							}
							refund := ChannelLocalResponseRefund{RequestID: key, UserID: id, TokenID: id, SubscriptionID: id, TokenQuota: 100, SubscriptionQuota: 100}
							require.NoError(t, QueueChannelLocalResponseRefund(t.Context(), &refund))
							for range 2 {
								require.NoError(t, ApplyChannelLocalResponseRefund(t.Context(), key))
							}
						}
						var sub UserSubscription
						var token Token
						require.NoError(t, db.First(&sub, id).Error)
						require.NoError(t, db.First(&token, id).Error)
						t.Logf("observed_used=%d observed_token=%d", sub.AmountUsed, token.RemainQuota)
						assert.Equal(t, wantUsed, sub.AmountUsed, "official delta behavior is preserved; downstream refunds follow their reservation periods")
						assert.Equal(t, wantToken, token.RemainQuota)
					})
				}
			}
		})
	}

}

func TestChannelMonitorIncomeZeroMidjourneyCharge(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			tables := []any{&User{}, &Token{}, &Channel{}, &Midjourney{}, &ChannelMonitorIncome{}}
			require.NoError(t, db.AutoMigrate(tables...))
			ready, redisEnabled := ChannelMonitorIncomeReady.Swap(true), common.RedisEnabled
			common.RedisEnabled = false
			t.Cleanup(func() {
				ChannelMonitorIncomeReady.Store(ready)
				common.RedisEnabled = redisEnabled
				assert.NoError(t, db.Migrator().DropTable(tables...))
			})
			require.NoError(t, db.Create(&User{Id: 98001, Username: "audit-mj-zero", AffCode: "audit-mj-zero", Quota: 1000}).Error)
			require.NoError(t, db.Create(&Channel{Id: 98001}).Error)
			task := Midjourney{UserId: 98001, ChannelId: 98001, Status: "IN_PROGRESS"}
			require.NoError(t, db.Create(&task).Error)
			record := ChannelMonitorIncome{SettlementKey: ChannelMonitorIncomeKey(fmt.Sprint(task.Id), "midjourney"), UserID: 98001, ChannelID: 98001, QuotaPerUnit: "100", BillingSource: "wallet", Status: "pending"}
			require.NoError(t, db.Create(&record).Error)
			_, _, err := SettleMidjourneyBilling(t.Context(), task.Id, MidjourneyPendingBilling{Quota: 0, ChannelID: 98001}, 0)
			require.NoError(t, db.First(&record, record.ID).Error)
			t.Logf("income_status=%s billing_error=%v", record.Status, err)
			require.NoError(t, err, "a free task should settle without quota mutation")
			assert.Equal(t, "settled", record.Status)
			require.NoError(t, db.First(&task, task.Id).Error)
			assert.Equal(t, 98001, task.BillingChannelId)
		})
	}
}

func TestChannelMonitorIncomeTaskCostReplay(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			tables := []any{&Task{}, &User{}, &Channel{}, &ChannelTaskCostEvent{}}
			require.NoError(t, db.AutoMigrate(tables...))
			fixedNow := time.Now()
			previousNow := db.Config.NowFunc
			db.Config.NowFunc = func() time.Time { return fixedNow }
			t.Cleanup(func() { db.Config.NowFunc = previousNow })
			ready, redis := ChannelMonitorIncomeReady.Swap(false), common.RedisEnabled
			common.RedisEnabled = false
			t.Cleanup(func() {
				ChannelMonitorIncomeReady.Store(ready)
				common.RedisEnabled = redis
				assert.NoError(t, db.Migrator().DropTable(tables...))
			})
			user := User{Id: 99761, Username: "audit-cost-replay", AffCode: "audit-cost-replay", Quota: 9900, UsedQuota: 100}
			channel := Channel{Id: 99761, UsedQuota: 100}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, db.Create(&channel).Error)
			task := Task{TaskID: "audit-cost-replay", UserId: user.Id, ChannelId: channel.Id, SubmitTime: time.Now().Unix(), Status: TaskStatusInProgress, Quota: 100, PrivateData: TaskPrivateData{BillingContext: &TaskBillingContext{ChannelCostEventId: "audit-cost-replay"}}}
			require.NoError(t, db.Create(&task).Error)
			_, err := RegisterChannelTaskCostEvent(t.Context(), ChannelTaskCostEventInput{TaskID: task.ID, CostEventId: "audit-cost-replay", ChannelId: channel.Id, OccurredAt: task.SubmitTime, InitialQuota: 100, CostNanoCNY: 80})
			require.NoError(t, err)
			for _, target := range []int{100, 150, 150} {
				result, err := ApplyTaskBilling(t.Context(), &task, TaskBillingOperationSettle, target)
				t.Logf("target=%d applied=%t error=%v", target, result.Applied, err)
				assert.NoError(t, err, "unchanged or repeated final cost settlement must succeed")
			}
		})
	}
}

func TestChannelMonitorIncomeFundingCacheRepairKeepsNewerIntent(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
			oldRDB, oldRedis := common.RDB, common.RedisEnabled
			common.RDB, common.RedisEnabled = client, true
			t.Cleanup(func() { common.RDB, common.RedisEnabled = oldRDB, oldRedis; assert.NoError(t, client.Close()) })
			key := getUserCacheKey(98150)
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return queueChannelMonitorFundingCacheRepair(tx, key) }))
			// A newer commit between Redis repair and SQL ACK must remain pending.
			require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("test:new-repair", func(tx *gorm.DB) {
				if tx.Statement.Table == "channel_monitor_funding_cache_repairs" {
					tx.AddError(tx.Session(&gorm.Session{NewDB: true}).Model(&ChannelMonitorFundingCacheRepair{}).Where("cache_key = ?", key).Update("revision", "new-commit").Error)
				}
			}))
			n, err := RecoverChannelMonitorFundingCaches(t.Context(), 10)
			require.NoError(t, err)
			assert.Zero(t, n)
			require.NoError(t, db.Callback().Delete().Remove("test:new-repair"))
			var pending ChannelMonitorFundingCacheRepair
			require.NoError(t, db.First(&pending).Error)
			assert.Equal(t, "new-commit", pending.Revision)
			n, err = RecoverChannelMonitorFundingCaches(t.Context(), 10)
			require.NoError(t, err)
			assert.Equal(t, 1, n)
			require.Error(t, db.Transaction(func(tx *gorm.DB) error {
				require.NoError(t, queueChannelMonitorFundingCacheRepair(tx, key))
				return errors.New("rollback debit")
			}))
			var count int64
			require.NoError(t, db.Model(&ChannelMonitorFundingCacheRepair{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestChannelMonitorIncomeFundingCacheRecoveryIsolation(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		for _, mode := range []string{"same_batch", "across_batches", "row_lock"} {
			if engine == "sqlite" && mode == "row_lock" {
				continue // SQLite has database-wide writer locking, not row locks.
			}
			t.Run(engine+"/"+mode, func(t *testing.T) {
				db := setupChannelDailyCostBatchDatabase(t, engine)
				server := miniredis.RunT(t)
				client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
				oldRDB, oldWriter, oldRedis := common.RDB, common.RDBMonitorWrite, common.RedisEnabled
				common.RDB, common.RDBMonitorWrite, common.RedisEnabled = client, client, true
				t.Cleanup(func() {
					common.RDB, common.RDBMonitorWrite, common.RedisEnabled = oldRDB, oldWriter, oldRedis
					assert.NoError(t, client.Close())
				})
				first, second, third := getUserCacheKey(98601), getUserCacheKey(98602), getUserCacheKey(98603)
				for i, key := range []string{first, second, third} {
					require.NoError(t, client.HSet(t.Context(), key, "Quota", 9900).Err())
					require.NoError(t, db.Create(&ChannelMonitorFundingCacheRepair{CacheKey: key, Revision: "pending", UpdatedAt: int64(i + 1)}).Error)
				}
				var release func()
				if mode == "row_lock" {
					tx := db.Begin()
					require.NoError(t, tx.Error)
					release = sync.OnceFunc(func() { assert.NoError(t, tx.Rollback().Error) })
					t.Cleanup(release)
					var locked ChannelMonitorFundingCacheRepair
					require.NoError(t, lockForUpdate(tx).Where("cache_key = ?", first).First(&locked).Error)
				} else {
					require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("test:cache_ack_failure", func(tx *gorm.DB) {
						if tx.Statement.Table != "channel_monitor_funding_cache_repairs" {
							return
						}
						for _, expression := range tx.Statement.Clauses["WHERE"].Expression.(clause.Where).Exprs {
							if expression, ok := expression.(clause.Expr); ok && len(expression.Vars) > 0 && expression.Vars[0] == first {
								tx.AddError(errors.New("repair ACK unavailable"))
							}
						}
					}))
					release = sync.OnceFunc(func() { assert.NoError(t, db.Callback().Delete().Remove("test:cache_ack_failure")) })
					t.Cleanup(release)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				limit := 3
				if mode == "across_batches" {
					limit = 1
				}
				count, err := RecoverChannelMonitorFundingCaches(ctx, limit)
				require.Error(t, err)
				assert.NoError(t, ctx.Err(), "one ACK must not exhaust the parent budget")
				assert.False(t, server.Exists(first))
				if mode == "across_batches" {
					assert.Zero(t, count)
					// A fresh worker client resumes from Redis, with no in-memory cursor.
					require.NoError(t, client.Close())
					client = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
					common.RDB, common.RDBMonitorWrite = client, client
					for _, key := range []string{second, third} {
						count, err = RecoverChannelMonitorFundingCaches(ctx, 1)
						require.NoError(t, err)
						assert.Equal(t, 1, count)
						assert.False(t, server.Exists(key))
					}
					// New keys above the captured bound cannot postpone revisiting
					// the earlier failed record at the start of the next sweep.
					require.NoError(t, db.Create(&ChannelMonitorFundingCacheRepair{CacheKey: getUserCacheKey(98604), Revision: "new"}).Error)
					count, err = RecoverChannelMonitorFundingCaches(ctx, 1)
					require.Error(t, err)
					assert.Zero(t, count)
				} else {
					assert.Equal(t, 2, count)
					assert.False(t, server.Exists(second))
					assert.False(t, server.Exists(third))
				}
				var pending ChannelMonitorFundingCacheRepair
				require.NoError(t, db.Where("cache_key = ?", first).Take(&pending).Error)
				assert.Equal(t, "pending", pending.Revision, "failed ACK must preserve the durable intent")
				release()
				// Redis cursor loss/corruption can rescan but cannot lose an intent.
				require.NoError(t, client.Set(ctx, "channel_monitor:funding_cache_recovery:cursor", "invalid-json", 0).Err())
				count, err = RecoverChannelMonitorFundingCaches(ctx, 10)
				require.NoError(t, err)
				if mode == "across_batches" {
					assert.Equal(t, 2, count)
				} else {
					assert.Equal(t, 1, count)
				}
				count, err = RecoverChannelMonitorFundingCaches(ctx, 10)
				require.NoError(t, err)
				assert.Zero(t, count)
			})
		}
	}
}

func TestChannelMonitorIncomeDirtyLeaseCompletionMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			require.NoError(t, db.AutoMigrate(&ChannelMonitorDirtyMinute{}))
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&ChannelMonitorDirtyMinute{})) })
			for _, mode := range []string{"unchanged_deadline", "expired_owned", "taken_over", "remarked"} {
				t.Run(mode, func(t *testing.T) {
					require.NoError(t, db.Where("1 = 1").Delete(&ChannelMonitorDirtyMinute{}).Error)
					now := common.GetTimestamp()
					claim := ChannelMonitorDirtyMinute{MinuteStart: 120, ClaimedBy: "worker-a", ClaimedAt: now, ClaimedUntil: now + 60, MarkCount: 1}
					require.NoError(t, db.Create(&claim).Error)
					switch mode {
					case "unchanged_deadline":
						require.NoError(t, RenewChannelMonitorDirtyMinutes(t.Context(), "worker-a", []ChannelMonitorDirtyMinute{claim}, now+60))
					case "expired_owned":
						require.NoError(t, db.Model(&claim).Update("claimed_until", now-1).Error)
					case "taken_over":
						require.NoError(t, db.Model(&ChannelMonitorDirtyMinute{}).Where("id = ?", claim.Id).Updates(map[string]any{"claimed_by": "worker-b", "claimed_at": now + 1}).Error)
					case "remarked":
						require.NoError(t, db.Model(&ChannelMonitorDirtyMinute{}).Where("id = ?", claim.Id).Update("mark_count", 2).Error)
					}
					require.NoError(t, CompleteChannelMonitorDirtyMinutes(t.Context(), "worker-a", []ChannelMonitorDirtyMinute{claim}))
					var rows []ChannelMonitorDirtyMinute
					require.NoError(t, db.Find(&rows).Error)
					if mode == "taken_over" {
						require.Len(t, rows, 1)
						assert.Equal(t, "worker-b", rows[0].ClaimedBy)
					} else if mode == "remarked" {
						require.Len(t, rows, 1)
						assert.Empty(t, rows[0].ClaimedBy)
					} else {
						assert.Empty(t, rows)
					}
				})
			}
		})
	}
}

type profitBeforePeriodIncome struct {
	ID            int64  `gorm:"primaryKey"`
	SettlementKey string `gorm:"size:64;not null;uniqueIndex"`
	DayStart      int64  `gorm:"not null;index:idx_cm_income_day_channel,priority:1"`
	ChannelID     int    `gorm:"not null;index:idx_cm_income_day_channel,priority:2"`
	UserID        int    `gorm:"not null;index"`
	APIKeyID      int    `gorm:"not null;index"`
	APIKeyKey     string `gorm:"size:64;not null"`
	APIKeyName    string `gorm:"size:255;not null"`
	ModelKey      string `gorm:"size:64;not null"`
	ModelName     string `gorm:"size:255;not null"`
	GroupName     string `gorm:"size:255;not null"`
	BillingSource string `gorm:"size:16;not null"`
	QuotaPerUnit  string `gorm:"size:64;not null"`
	USDToCNY      string `gorm:"size:64;not null"`
	Quota         int64  `gorm:"not null"`
	IncomeNanoCNY int64  `gorm:"not null"`
	Status        string `gorm:"size:16;not null;index"`
	CostEventID   string `gorm:"size:64;not null;index"`
	CostRecorded  int    `gorm:"not null"`
	CreatedAt     int64  `gorm:"not null"`
	UpdatedAt     int64  `gorm:"not null"`
	// Only funding_pending rows carry replayable final-settlement intent.
	// Older pending rows have no such evidence and must never be replayed.
	FundingDelta          int
	FundingTokenID        int
	FundingSubscriptionID int
}

func (profitBeforePeriodIncome) TableName() string { return "channel_monitor_incomes" }

type profitBeforePeriodRefund struct {
	ID                int64  `gorm:"primaryKey"`
	RequestID         string `gorm:"type:varchar(64);uniqueIndex"`
	UserID            int
	TokenID           int
	SubscriptionID    int
	WalletQuota       int64  `gorm:"type:bigint"`
	TokenQuota        int64  `gorm:"type:bigint"`
	SubscriptionQuota int64  `gorm:"type:bigint"`
	UserCacheEpoch    string `gorm:"type:varchar(64)"`
	TokenCacheEpoch   string `gorm:"type:varchar(64)"`
	Applied           bool
	CacheApplied      bool  `gorm:"index"`
	CreatedAt         int64 `gorm:"type:bigint"`
	UpdatedAt         int64 `gorm:"type:bigint"`
}

func (profitBeforePeriodRefund) TableName() string { return "channel_local_response_refunds" }

func TestChannelMonitorIncomePeriodUpgradePreservesLegacyEvidence(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			tables := []any{&ChannelMonitorIncome{}, &ChannelLocalResponseRefund{}}
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
			require.NoError(t, db.AutoMigrate(&profitBeforePeriodIncome{}, &profitBeforePeriodRefund{}))
			require.NoError(t, db.Create(&profitBeforePeriodIncome{SettlementKey: "legacy-period", UserID: 42, Status: "reserved", Quota: 100}).Error)
			require.NoError(t, db.Create(&profitBeforePeriodRefund{RequestID: "legacy-period", UserID: 42, SubscriptionID: 7, SubscriptionQuota: 100}).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(tables...))
				var income ChannelMonitorIncome
				var refund ChannelLocalResponseRefund
				require.NoError(t, db.First(&income).Error)
				require.NoError(t, db.First(&refund).Error)
				assert.EqualValues(t, 100, income.Quota)
				assert.Equal(t, "reserved", income.Status)
				assert.Nil(t, income.SubscriptionPeriod)
				assert.Nil(t, refund.SubscriptionPeriod)
				assert.EqualValues(t, 100, refund.SubscriptionQuota)
				assert.False(t, refund.Applied)
				assert.True(t, db.Migrator().HasIndex(&ChannelMonitorIncome{}, "idx_channel_monitor_incomes_settlement_key"))
				assert.True(t, db.Migrator().HasIndex(&ChannelLocalResponseRefund{}, "idx_channel_local_response_refunds_request_id"))
				assert.Error(t, db.Create(&ChannelMonitorIncome{SettlementKey: "legacy-period"}).Error)
				assert.Error(t, db.Create(&ChannelLocalResponseRefund{RequestID: "legacy-period"}).Error)
			}
		})
	}
}

func TestChannelMonitorIncomeFundingGenerationPreservesAuthFence(t *testing.T) {
	server := useUserCacheMiniRedis(t)
	const id = 98555
	require.NoError(t, common.RDB.Set(t.Context(), "funding:generation:"+getUserCacheKey(id), "new", 0).Err())
	require.NoError(t, SetUserAuthVersionFence(id, 2))
	err := writeUserCache(&UserBase{Id: id, AuthVersion: 1, Status: common.UserStatusEnabled}, true, "old")
	assert.ErrorIs(t, err, ErrUserAuthCachePending)
	assert.False(t, server.Exists(getUserCacheKey(id)))
	// A stale funding snapshot with current auth cannot populate quota either.
	require.NoError(t, writeUserCache(&UserBase{Id: id, AuthVersion: 2, Status: common.UserStatusDisabled}, true, "old"))
	assert.False(t, server.Exists(getUserCacheKey(id)))
	require.NoError(t, writeUserCache(&UserBase{Id: id, AuthVersion: 2, Status: common.UserStatusDisabled}, true, "new"))
	cached, err := cacheGetUserBase(id)
	require.NoError(t, err)
	assert.Equal(t, common.UserStatusDisabled, cached.Status)
}

func TestChannelMonitorIncomeRecoveryContinuesAfterSlowRecord(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		for _, phase := range []string{"funding", "cost"} {
			t.Run(engine+"/"+phase, func(t *testing.T) {
				db := setupChannelDailyCostBatchDatabase(t, engine)
				tables := []any{&ChannelMonitorIncome{}, &User{}}
				require.NoError(t, db.AutoMigrate(tables...))
				t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
				oldRedis := common.RedisEnabled
				common.RedisEnabled = false
				t.Cleanup(func() { common.RedisEnabled = oldRedis })
				day := ChannelDailyCostDayStart(time.Now().Unix()) - 86400
				for i := range 2 {
					id := 98571 + i
					key := fmt.Sprintf("slow-recovery-%d", id)
					require.NoError(t, db.Create(&User{Id: id, Username: key, AffCode: key, Quota: 900}).Error)
					require.NoError(t, db.Create(&ChannelMonitorIncome{SettlementKey: key, UserID: id, BillingSource: "wallet", Status: "funding_pending", FundingDelta: 50, Quota: 150, CostEventID: key, DayStart: day + 86400, UpdatedAt: int64(i + 1)}).Error)
					require.NoError(t, db.Create(&ChannelDailyCostOutbox{EventId: key, OccurredAt: day, ProcessedAt: day + 1}).Error)
				}
				// The first account/event stalls until its operation deadline. The
				// batch must still visit the independent healthy second record.
				require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:slow_income_record", func(tx *gorm.DB) {
					if tx.Error != nil {
						return
					}
					blocked := false
					switch row := tx.Statement.Dest.(type) {
					case *User:
						blocked = phase == "funding" && row.Id == 98571
					case *ChannelDailyCostOutbox:
						blocked = phase == "cost" && row.EventId == "slow-recovery-98571"
					}
					if blocked {
						<-tx.Statement.Context.Done()
						tx.AddError(tx.Statement.Context.Err())
					}
				}))
				t.Cleanup(func() { assert.NoError(t, db.Callback().Query().Remove("test:slow_income_record")) })
				// Failure bookkeeping itself must also be bounded. Waiting for
				// this update may not consume all time left for the next account.
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:slow_income_rotation", func(tx *gorm.DB) {
					updates, ok := tx.Statement.Dest.(map[string]any)
					if tx.Statement.Table == "channel_monitor_incomes" && ok && len(updates) == 1 && updates["updated_at"] != nil {
						<-tx.Statement.Context.Done()
						tx.AddError(tx.Statement.Context.Err())
					}
				}))
				t.Cleanup(func() { assert.NoError(t, db.Callback().Update().Remove("test:slow_income_rotation")) })
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				var completed int
				var err error
				if phase == "funding" {
					completed, err = RecoverChannelMonitorIncomeFunding(ctx, 2)
				} else {
					completed, err = RecoverChannelMonitorIncomeCosts(ctx, 2)
				}
				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.NoError(t, ctx.Err(), "one record must not consume the whole batch deadline")
				assert.Equal(t, 1, completed, "healthy record must recover in the same batch")
				require.NoError(t, db.Callback().Query().Remove("test:slow_income_record"))
				require.NoError(t, db.Callback().Update().Remove("test:slow_income_rotation"))
				var users []User
				require.NoError(t, db.Order("id").Find(&users).Error)
				require.Len(t, users, 2)
				assert.Equal(t, 900, users[0].Quota, "timed-out transaction must roll back")
				var incomes []ChannelMonitorIncome
				require.NoError(t, db.Order("id").Find(&incomes).Error)
				require.Len(t, incomes, 2)
				assert.Equal(t, "funding_pending", incomes[0].Status)
				if phase == "funding" {
					assert.Equal(t, 850, users[1].Quota)
					assert.Equal(t, "settled", incomes[1].Status)
				} else {
					assert.Equal(t, 900, users[1].Quota)
					assert.Equal(t, 1, incomes[1].CostRecorded)
					assert.Equal(t, day, incomes[1].DayStart)
				}
				// Once the slow dependency recovers, its durable intent remains
				// replayable; retry does not repeat the already successful record.
				if phase == "funding" {
					completed, err = RecoverChannelMonitorIncomeFunding(t.Context(), 2)
				} else {
					completed, err = RecoverChannelMonitorIncomeCosts(t.Context(), 2)
				}
				require.NoError(t, err)
				assert.Equal(t, 1, completed)
			})
		}
	}
}

func TestChannelMonitorIncomeRecoveryAdvancesWhenRotationFails(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		for _, phase := range []string{"funding", "cost"} {
			t.Run(engine+"/"+phase, func(t *testing.T) {
				db := setupChannelDailyCostBatchDatabase(t, engine)
				tables := []any{&ChannelMonitorIncome{}, &User{}}
				require.NoError(t, db.AutoMigrate(tables...))
				t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
				oldRedis := common.RedisEnabled
				common.RedisEnabled = false
				t.Cleanup(func() { common.RedisEnabled = oldRedis })
				for i := range 2 {
					key := fmt.Sprintf("rotation-failed-%d", i)
					require.NoError(t, db.Create(&User{Id: 98651 + i, Username: key, AffCode: key, Quota: 900}).Error)
					require.NoError(t, db.Create(&ChannelMonitorIncome{SettlementKey: key, UserID: 98651 + i, BillingSource: "wallet", Status: "funding_pending", FundingDelta: 50, Quota: 150, CostEventID: key, UpdatedAt: int64(i + 1)}).Error)
					require.NoError(t, db.Create(&ChannelDailyCostOutbox{EventId: key, OccurredAt: 86400, ProcessedAt: 86401}).Error)
				}
				require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:failed_recovery_dependency", func(tx *gorm.DB) {
					switch row := tx.Statement.Dest.(type) {
					case *User:
						if phase == "funding" && row.Id == 98651 {
							tx.AddError(errors.New("first account unavailable"))
						}
					case *ChannelDailyCostOutbox:
						if phase == "cost" && row.EventId == "rotation-failed-0" {
							tx.AddError(errors.New("first cost unavailable"))
						}
					}
				}))
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:failed_recovery_rotation", func(tx *gorm.DB) {
					if updates, ok := tx.Statement.Dest.(map[string]any); tx.Statement.Table == "channel_monitor_incomes" && ok && len(updates) == 1 && updates["updated_at"] != nil {
						tx.AddError(errors.New("rotation unavailable"))
					}
				}))
				release := sync.OnceFunc(func() {
					assert.NoError(t, db.Callback().Query().Remove("test:failed_recovery_dependency"))
					assert.NoError(t, db.Callback().Update().Remove("test:failed_recovery_rotation"))
				})
				t.Cleanup(release)
				recover := RecoverChannelMonitorIncomeFunding
				if phase == "cost" {
					recover = RecoverChannelMonitorIncomeCosts
				}
				n, err := recover(t.Context(), 1)
				require.Error(t, err)
				assert.Zero(t, n)
				n, err = recover(t.Context(), 1)
				require.NoError(t, err, "the next batch must reach the independent healthy record")
				assert.Equal(t, 1, n)
				release()
				n, err = recover(t.Context(), 1)
				require.NoError(t, err)
				assert.Equal(t, 1, n, "the next sweep must retry the preserved failed intent")
				var users []User
				require.NoError(t, db.Order("id").Find(&users).Error)
				expected := 900
				if phase == "funding" {
					expected = 850
				}
				for _, user := range users {
					assert.Equal(t, expected, user.Quota)
				}
			})
		}
	}
}

func TestChannelMonitorIncomeRefundRecoveryIsolation(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		for _, scenario := range []string{"same_batch", "next_batch", "row_lock", "batch_busy", "token_batch_busy", "direct_with_busy_batches"} {
			if engine == "sqlite" && scenario == "row_lock" {
				continue
			}
			t.Run(engine+"/"+scenario, func(t *testing.T) {
				db := setupChannelDailyCostBatchDatabase(t, engine)
				tables := []any{&ChannelLocalResponseRefund{}, &User{}}
				require.NoError(t, db.AutoMigrate(tables...))
				t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
				oldRedis, oldBatch, ready := common.RedisEnabled, common.BatchUpdateEnabled, ChannelMonitorIncomeReady.Swap(false)
				batchBusy := scenario == "batch_busy" || scenario == "token_batch_busy"
				common.RedisEnabled, common.BatchUpdateEnabled = false, batchBusy
				t.Cleanup(func() {
					common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch
					ChannelMonitorIncomeReady.Store(ready)
				})
				for i := range 2 {
					id, key := 98901+i, fmt.Sprintf("refund-isolation-%d", i)
					require.NoError(t, db.Create(&User{Id: id, Username: key, AffCode: key, Quota: 900}).Error)
					require.NoError(t, db.Create(&ChannelLocalResponseRefund{RequestID: key, UserID: id, WalletQuota: 100}).Error)
				}
				var release func()
				switch scenario {
				case "row_lock":
					tx := db.Begin()
					require.NoError(t, tx.Error)
					release = sync.OnceFunc(func() { assert.NoError(t, tx.Rollback().Error) })
					t.Cleanup(release)
					var user User
					require.NoError(t, lockForUpdate(tx).First(&user, 98901).Error)
				case "batch_busy":
					userQuotaBatchMutationLock.Lock()
					release = sync.OnceFunc(userQuotaBatchMutationLock.Unlock)
					t.Cleanup(release)
				case "token_batch_busy":
					tokenQuotaBatchMutationLock.Lock()
					release = sync.OnceFunc(tokenQuotaBatchMutationLock.Unlock)
					t.Cleanup(release)
				case "direct_with_busy_batches":
					userQuotaBatchMutationLock.Lock()
					tokenQuotaBatchMutationLock.Lock()
					release = sync.OnceFunc(func() {
						tokenQuotaBatchMutationLock.Unlock()
						userQuotaBatchMutationLock.Unlock()
					})
					t.Cleanup(release)
				default:
					require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:refund_dependency", func(tx *gorm.DB) {
						if user, ok := tx.Statement.Dest.(*User); ok && user.Id == 98901 {
							if scenario == "same_batch" {
								<-tx.Statement.Context.Done()
								tx.AddError(tx.Statement.Context.Err())
							} else {
								tx.AddError(errors.New("refund dependency unavailable"))
							}
						}
					}))
					release = sync.OnceFunc(func() { assert.NoError(t, db.Callback().Query().Remove("test:refund_dependency")) })
					t.Cleanup(release)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				limit := 2
				if scenario == "next_batch" {
					limit = 1
				}
				n, err := RecoverChannelLocalResponseRefunds(ctx, limit)
				if scenario == "direct_with_busy_batches" || batchBusy {
					require.NoError(t, err)
					assert.Equal(t, 2, n)
					release()
					var users []User
					require.NoError(t, db.Find(&users).Error)
					for _, user := range users {
						assert.Equal(t, 1000, user.Quota)
					}
					return
				}
				require.Error(t, err)
				assert.NoError(t, ctx.Err())
				if scenario == "next_batch" {
					assert.Zero(t, n)
					n, err = RecoverChannelLocalResponseRefunds(ctx, 1)
					require.NoError(t, err)
					assert.Equal(t, 1, n)
				} else {
					assert.Equal(t, 1, n)
				}
				release()
				var first, second User
				require.NoError(t, db.First(&first, 98901).Error)
				require.NoError(t, db.First(&second, 98902).Error)
				assert.Equal(t, 900, first.Quota)
				assert.Equal(t, 1000, second.Quota)
				n, err = RecoverChannelLocalResponseRefunds(ctx, 2)
				require.NoError(t, err)
				assert.Equal(t, 1, n)
				n, err = RecoverChannelLocalResponseRefunds(ctx, 2)
				require.NoError(t, err)
				assert.Zero(t, n)
				var users []User
				require.NoError(t, db.Find(&users).Error)
				for _, user := range users {
					assert.Equal(t, 1000, user.Quota)
				}
			})
		}
	}
}

func TestChannelMonitorIncomeFundingAndCostRecoverConcurrently(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelDailyCostBatchDatabase(t, engine)
			tables := []any{&ChannelMonitorIncome{}, &User{}, &Token{}}
			require.NoError(t, db.AutoMigrate(tables...))
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
			oldRedis := common.RedisEnabled
			common.RedisEnabled = false
			t.Cleanup(func() { common.RedisEnabled = oldRedis })
			const id, key = 98591, "concurrent-funding-cost"
			day := ChannelDailyCostDayStart(time.Now().Unix()) - 86400
			require.NoError(t, db.Create(&User{Id: id, Username: key, AffCode: key, Quota: 900}).Error)
			require.NoError(t, db.Create(&Token{Id: id, UserId: id, Key: key, RemainQuota: 900, UsedQuota: 100}).Error)
			record := ChannelMonitorIncome{SettlementKey: key, UserID: id, BillingSource: "wallet", Status: "funding_pending", FundingTokenID: id, FundingDelta: 50, Quota: 150, CostEventID: key, DayStart: day + 86400}
			require.NoError(t, db.Create(&record).Error)
			require.NoError(t, db.Create(&ChannelDailyCostOutbox{EventId: key, OccurredAt: day, ProcessedAt: day + 1}).Error)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			loaded, release := make(chan struct{}, 2), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			var workers sync.WaitGroup
			t.Cleanup(func() {
				unblock()
				cancel()
				workers.Wait()
				assert.NoError(t, db.Callback().Query().Remove("test:parallel_income_snapshots"))
			})
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:parallel_income_snapshots", func(tx *gorm.DB) {
				if _, ok := tx.Statement.Dest.(*[]ChannelMonitorIncome); ok && tx.Error == nil {
					loaded <- struct{}{}
					select {
					case <-release:
					case <-ctx.Done():
						tx.AddError(ctx.Err())
					}
				}
			}))
			results := make(chan error, 2)
			for _, recover := range []func(context.Context, int) (int, error){RecoverChannelMonitorIncomeFunding, RecoverChannelMonitorIncomeCosts} {
				workers.Go(func() { _, err := recover(ctx, 10); results <- err })
			}
			for range 2 {
				select {
				case <-loaded:
				case <-ctx.Done():
					t.Fatal("both recovery queues did not load their snapshot")
				}
			}
			unblock()
			workers.Wait()
			require.NoError(t, db.Callback().Query().Remove("test:parallel_income_snapshots"))
			for range 2 {
				err := <-results
				if engine == "sqlite" && isRetryableSQLiteLock(err) {
					continue // A rolled-back SQLite write is recovered on the next run.
				}
				require.NoError(t, err)
			}
			_, err := RecoverChannelMonitorIncomeFunding(t.Context(), 10)
			require.NoError(t, err)
			_, err = RecoverChannelMonitorIncomeCosts(t.Context(), 10)
			require.NoError(t, err)
			var user User
			var token Token
			require.NoError(t, db.First(&user, id).Error)
			require.NoError(t, db.First(&token, id).Error)
			require.NoError(t, db.First(&record, record.ID).Error)
			assert.Equal(t, 850, user.Quota)
			assert.Equal(t, 850, token.RemainQuota)
			assert.Equal(t, 150, token.UsedQuota)
			assert.Equal(t, "settled", record.Status)
			assert.Equal(t, int64(150), record.Quota)
			assert.Equal(t, 1, record.CostRecorded)
			assert.Equal(t, day, record.DayStart)
		})
	}
}
func TestChannelMonitorIncomeRefundBatchCommitLoss(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		for _, delta := range []int{-25, 25} {
			for _, stage := range []string{"enqueue", "apply", "enqueue_rollback", "enqueue_other_node", "before_commit"} {
				t.Run(fmt.Sprintf("%s/%s/%d", engine, stage, delta), func(t *testing.T) {
					db := setupChannelDailyCostBatchDatabase(t, engine)
					tables := []any{&User{}, &Token{}, &ChannelLocalResponseRefund{}}
					require.NoError(t, db.AutoMigrate(tables...))
					t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
					oldBatch, oldRedis, ready := common.BatchUpdateEnabled, common.RedisEnabled, ChannelMonitorIncomeReady.Swap(false)
					common.BatchUpdateEnabled, common.RedisEnabled = true, false
					t.Cleanup(func() {
						common.BatchUpdateEnabled, common.RedisEnabled = oldBatch, oldRedis
						ChannelMonitorIncomeReady.Store(ready)
						DB = db
						delete(batchUpdateStores[BatchUpdateTypeUserQuota], 98977)
						delete(batchUpdateStores[BatchUpdateTypeTokenQuota], 98977)
						channelLocalRefundTransfers.Delete("refund-commit-batch")
					})
					require.NoError(t, db.Create(&User{Id: 98977, Username: "refund-commit-batch", AffCode: "refund-commit-batch", Quota: 900}).Error)
					require.NoError(t, db.Create(&Token{Id: 98977, UserId: 98977, Key: "refund-commit-batch", RemainQuota: 900, UsedQuota: 100}).Error)
					refund := ChannelLocalResponseRefund{RequestID: "refund-commit-batch", UserID: 98977, TokenID: 98977, WalletQuota: 100, TokenQuota: 100}
					if stage == "apply" {
						require.NoError(t, QueueChannelLocalResponseRefund(t.Context(), &refund))
					}
					addNewRecord(BatchUpdateTypeUserQuota, 98977, delta)
					addNewRecord(BatchUpdateTypeTokenQuota, 98977, delta)
					sqlDB, err := db.DB()
					require.NoError(t, err)
					lost := db.WithContext(t.Context())
					lost.Statement.ConnPool = &channelDailyCostCommitAckLossPool{sqlDB}
					if stage == "enqueue_rollback" || stage == "enqueue_other_node" {
						lost.Statement.ConnPool = &channelMonitorRefundRollbackPool{sqlDB}
					}
					if stage == "before_commit" {
						require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:refund_rollback", func(tx *gorm.DB) {
							tx.AddError(errors.New("definite rollback before commit"))
						}))
					}
					DB = lost
					if stage != "apply" {
						err = QueueChannelLocalResponseRefund(t.Context(), &refund)
					} else {
						err = ApplyChannelLocalResponseRefund(t.Context(), refund.RequestID)
					}
					require.Error(t, err)
					DB = db
					if stage == "before_commit" {
						require.NoError(t, db.Callback().Update().Remove("test:refund_rollback"))
						assert.Equal(t, delta, batchUpdateStores[BatchUpdateTypeUserQuota][98977])
						assert.Equal(t, delta, batchUpdateStores[BatchUpdateTypeTokenQuota][98977])
					} else if stage != "apply" {
						require.ErrorIs(t, err, ErrTaskBillingCommitUncertain)
					}
					// Subsequent requests and the ordinary batch writer cannot replay the
					// captured batch, even while acknowledgement queries keep failing.
					addNewRecord(BatchUpdateTypeUserQuota, 98977, -10)
					addNewRecord(BatchUpdateTypeTokenQuota, 98977, -10)
					require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:refund_readback", func(tx *gorm.DB) {
						if tx.Statement.Table == "channel_local_response_refunds" {
							tx.AddError(errors.New("readback unavailable"))
						}
					}))
					_, recoverErr := RecoverChannelLocalRefundTransfers(t.Context(), 100)
					if stage != "apply" && stage != "before_commit" {
						require.Error(t, recoverErr)
					}
					batchUpdate()
					require.NoError(t, db.Callback().Query().Remove("test:refund_readback"))
					if stage == "enqueue_other_node" {
						other := refund
						other.ID, other.BatchTransferID = 0, "other-node"
						require.NoError(t, db.Create(&other).Error)
					}
					_, recoverErr = RecoverChannelLocalRefundTransfers(t.Context(), 100)
					require.NoError(t, recoverErr)
					if stage == "before_commit" {
						require.NoError(t, QueueChannelLocalResponseRefund(t.Context(), &refund))
					}
					require.NoError(t, ApplyChannelLocalResponseRefund(t.Context(), refund.RequestID))
					// Re-enqueueing a completed refund must not capture a later debit.
					addNewRecord(BatchUpdateTypeUserQuota, 98977, -5)
					addNewRecord(BatchUpdateTypeTokenQuota, 98977, -5)
					require.NoError(t, QueueChannelLocalResponseRefund(t.Context(), &refund))
					require.NoError(t, ApplyChannelLocalResponseRefund(t.Context(), refund.RequestID))
					batchUpdate()
					var user User
					require.NoError(t, db.First(&user, 98977).Error)
					var token Token
					require.NoError(t, db.First(&token, 98977).Error)
					assert.Equal(t, 1000+delta-15, user.Quota, "each independent batch delta applies exactly once")
					assert.Equal(t, 1000+delta-15, token.RemainQuota)
					assert.Equal(t, -delta+15, token.UsedQuota)
				})
			}
		}
	}
}

type channelMonitorRefundRollbackPool struct{ *sql.DB }

func (pool channelMonitorRefundRollbackPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := pool.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &channelMonitorRefundRollbackTx{tx}, nil
}

type channelMonitorRefundRollbackTx struct{ *sql.Tx }

func (tx channelMonitorRefundRollbackTx) Commit() error {
	if err := tx.Tx.Rollback(); err != nil {
		return err
	}
	return context.DeadlineExceeded
}

func TestChannelMonitorIncomeRefundTransferRecoveryIsolation(t *testing.T) {
	db := setupChannelDailyCostBatchDatabase(t, "sqlite")
	require.NoError(t, db.AutoMigrate(&User{}, &ChannelLocalResponseRefund{}))
	oldBatch, oldRedis := common.BatchUpdateEnabled, common.RedisEnabled
	common.BatchUpdateEnabled, common.RedisEnabled = true, false
	t.Cleanup(func() {
		common.BatchUpdateEnabled, common.RedisEnabled = oldBatch, oldRedis
		channelLocalRefundTransfers.Clear()
	})
	sqlDB, err := db.DB()
	require.NoError(t, err)
	for i := range 2 {
		id := 99101 + i
		key := fmt.Sprintf("transfer-fairness-%d", i)
		require.NoError(t, db.Create(&User{Id: id, Username: key, AffCode: key, Quota: 900}).Error)
		addNewRecord(BatchUpdateTypeUserQuota, id, -25)
		lost := db.WithContext(t.Context())
		lost.Statement.ConnPool = &channelMonitorRefundRollbackPool{sqlDB}
		DB = lost
		require.Error(t, QueueChannelLocalResponseRefund(t.Context(), &ChannelLocalResponseRefund{RequestID: key, UserID: id, WalletQuota: 100}))
		DB = db
	}
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:transfer_dependency", func(tx *gorm.DB) {
		if user, ok := tx.Statement.Dest.(*User); ok && user.Id == 99101 {
			tx.AddError(errors.New("first dependency unavailable"))
		}
	}))
	n, err := RecoverChannelLocalRefundTransfers(t.Context(), 1)
	require.Error(t, err)
	assert.Zero(t, n)
	// Cancelling the next invocation must retain both transferred batches.
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = RecoverChannelLocalRefundTransfers(cancelled, 1)
	require.Error(t, err)
	n, err = RecoverChannelLocalRefundTransfers(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.NoError(t, ApplyChannelLocalResponseRefund(t.Context(), "transfer-fairness-1"))
	require.NoError(t, db.Callback().Query().Remove("test:transfer_dependency"))
	n, err = RecoverChannelLocalRefundTransfers(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.NoError(t, ApplyChannelLocalResponseRefund(t.Context(), "transfer-fairness-0"))
	batchUpdate()
	var users []User
	require.NoError(t, db.Find(&users).Error)
	for _, user := range users {
		assert.Equal(t, 975, user.Quota)
	}
}

func TestChannelMonitorIncomeRefundTransferSurvivesProcessExit(t *testing.T) {
	if path := os.Getenv("PROFIT_REFUND_TRANSFER_DB"); path != "" {
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
		require.NoError(t, err)
		DB = db
		common.RedisEnabled, common.BatchUpdateEnabled = false, true
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
		require.NoError(t, db.AutoMigrate(&User{}, &ChannelLocalResponseRefund{}, &ChannelMonitorIncomeRecoveryCursor{}))
		require.NoError(t, db.Create(&User{Id: 99201, Username: "transfer-exit", AffCode: "transfer-exit", Quota: 900}).Error)
		addNewRecord(BatchUpdateTypeUserQuota, 99201, -25)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		lost := db.WithContext(t.Context())
		lost.Statement.ConnPool = &channelDailyCostCommitAckLossPool{sqlDB}
		DB = lost
		require.ErrorIs(t, QueueChannelLocalResponseRefund(t.Context(), &ChannelLocalResponseRefund{RequestID: "transfer-exit", UserID: 99201, WalletQuota: 100}), ErrTaskBillingCommitUncertain)
		os.Exit(23)
	}
	path := filepath.Join(t.TempDir(), "refund-transfer.db")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestChannelMonitorIncomeRefundTransferSurvivesProcessExit$", "-test.count=1")
	cmd.Env = append(os.Environ(), "PROFIT_REFUND_TRANSFER_DB="+path)
	output, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, string(output))
	require.Equal(t, 23, exitErr.ExitCode())
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	oldDB, oldRedis, oldBatch, ready := DB, common.RedisEnabled, common.BatchUpdateEnabled, ChannelMonitorIncomeReady.Swap(false)
	DB, common.RedisEnabled, common.BatchUpdateEnabled = db, false, false
	t.Cleanup(func() {
		DB, common.RedisEnabled, common.BatchUpdateEnabled = oldDB, oldRedis, oldBatch
		ChannelMonitorIncomeReady.Store(ready)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		assert.NoError(t, sqlDB.Close())
	})
	n, err := RecoverChannelLocalResponseRefunds(t.Context(), 100)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	n, err = RecoverChannelLocalResponseRefunds(t.Context(), 100)
	require.NoError(t, err)
	assert.Zero(t, n)
	var user User
	require.NoError(t, db.First(&user, 99201).Error)
	assert.Equal(t, 975, user.Quota)
}
