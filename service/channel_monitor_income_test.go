package service

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSettleBillingPersistsFinalWalletChargeForProfit(t *testing.T) {
	truncate(t)
	oldReady := model.ChannelMonitorIncomeReady.Load()
	oldQuotaPerUnit, oldExchangeRate := common.QuotaPerUnit, operation_setting.USDExchangeRate
	model.ChannelMonitorIncomeReady.Store(true)
	common.QuotaPerUnit, operation_setting.USDExchangeRate = 100, 7
	t.Cleanup(func() {
		model.ChannelMonitorIncomeReady.Store(oldReady)
		common.QuotaPerUnit, operation_setting.USDExchangeRate = oldQuotaPerUnit, oldExchangeRate
	})
	require.NoError(t, model.DB.AutoMigrate(&model.ChannelMonitorIncome{}, &model.ChannelMonitorIncomeRecoveryCursor{}, &model.ChannelDailyCostOutbox{}))
	require.NoError(t, model.DB.Exec("DELETE FROM channel_monitor_incomes").Error)

	seedUser(t, 890, 10_000)
	seedToken(t, 890, 890, "sk-profit-settlement", 10_000)
	seedChannel(t, 890)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	relayInfo := &relaycommon.RelayInfo{
		RequestId: "profit-settlement-request", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 890}, UserId: 890,
		TokenId: 890, TokenKey: "sk-profit-settlement", OriginModelName: "test-model",
		UsingGroup: "default", BillingSource: BillingSourceWallet,
	}
	require.NoError(t, SettleBilling(ctx, relayInfo, 100))

	var income model.ChannelMonitorIncome
	require.NoError(t, model.DB.Where("settlement_key = ?", model.ChannelMonitorIncomeKey(relayInfo.RequestId, "request")).First(&income).Error)
	assert.Equal(t, int64(100), income.Quota)
	assert.Equal(t, int64(1_000_000_000), income.IncomeNanoCNY)
	assert.Equal(t, "wallet", income.BillingSource)
	assert.Equal(t, "settled", income.Status)

	seedSubscription(t, 890, 890, 100_000, 0)
	subContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	subContext.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := *relayInfo
	info.RequestId, info.BillingSource, info.SubscriptionId = "profit-subscription", BillingSourceSubscription, 890
	require.NoError(t, SettleBilling(subContext, &info, 50))
	var subscriptionIncome model.ChannelMonitorIncome
	require.NoError(t, model.DB.Where("settlement_key = ?", model.ChannelMonitorIncomeKey(info.RequestId, "request")).First(&subscriptionIncome).Error)
	assert.Equal(t, int64(500_000_000), subscriptionIncome.IncomeNanoCNY)
	assert.Equal(t, "subscription", subscriptionIncome.BillingSource)
	assert.Equal(t, "settled", subscriptionIncome.Status)

	info.RequestId, info.SubscriptionId = "profit-failed-funding", 891
	require.NoError(t, SettleBilling(subContext, &info, 50), "durable recovery accepts the settlement but does not confirm its income")
	var pendingIncome model.ChannelMonitorIncome
	require.NoError(t, model.DB.Where("settlement_key = ?", model.ChannelMonitorIncomeKey(info.RequestId, "request")).First(&pendingIncome).Error)
	assert.Equal(t, "funding_pending", pendingIncome.Status)

	// The legacy task charge and its later callback must use the same income
	// record even though the callback no longer has an HTTP request context.
	task := &model.Midjourney{UserId: 890, ChannelId: 890, Action: "IMAGINE", Quota: 100, MjId: "profit-midjourney"}
	_, err := PrepareMidjourneyTaskBilling(relayInfo, task, 100, true)
	require.NoError(t, err)
	require.NoError(t, task.Insert())
	applied, err := SettleMonitoredMidjourneyBilling(ctx, relayInfo, task, true)
	require.NoError(t, err)
	assert.True(t, applied)
	key := model.ChannelMonitorIncomeKey(strconv.Itoa(task.Id), "midjourney")
	var midjourneyIncome model.ChannelMonitorIncome
	require.NoError(t, model.DB.Where("settlement_key = ?", key).First(&midjourneyIncome).Error)
	assert.Equal(t, int64(1_000_000_000), midjourneyIncome.IncomeNanoCNY)
	assert.True(t, RefundMidjourneyQuota(context.Background(), task, "test refund"))
	require.NoError(t, model.DB.Where("settlement_key = ?", key).First(&midjourneyIncome).Error)
	assert.Zero(t, midjourneyIncome.IncomeNanoCNY)

	failedRefundTask := &model.Midjourney{UserId: 999_999, ChannelId: 890, Action: "IMAGINE", Quota: 100, MjId: "profit-midjourney-refund-failure"}
	require.NoError(t, failedRefundTask.Insert())
	failedRefundKey := model.ChannelMonitorIncomeKey(strconv.Itoa(failedRefundTask.Id), "midjourney")
	require.NoError(t, model.DB.Create(&model.ChannelMonitorIncome{
		SettlementKey: failedRefundKey, DayStart: model.ChannelDailyCostDayStart(common.GetTimestamp()),
		ChannelID: 890, UserID: failedRefundTask.UserId, BillingSource: "wallet", QuotaPerUnit: "100",
		USDToCNY: "7", Quota: 100, IncomeNanoCNY: 7_000_000_000, Status: "settled",
	}).Error)
	assert.False(t, RefundMidjourneyQuota(context.Background(), failedRefundTask, "missing user"))
	var failedRefundIncome model.ChannelMonitorIncome
	require.NoError(t, model.DB.Where("settlement_key = ?", failedRefundKey).First(&failedRefundIncome).Error)
	assert.Equal(t, "settled", failedRefundIncome.Status, "a failed refund must keep the original charge confirmed")
	assert.Equal(t, int64(7_000_000_000), failedRefundIncome.IncomeNanoCNY)
}

func TestChannelMonitorIncomePreConsumeCompatibility(t *testing.T) {
	ready, oldTrust := model.ChannelMonitorIncomeReady.Swap(true), operation_setting.GetQuotaSetting().TrustQuotaUSD
	oldLogs, oldBatch := common.LogConsumeEnabled, common.BatchUpdateEnabled
	common.LogConsumeEnabled, common.BatchUpdateEnabled = false, false
	operation_setting.GetQuotaSetting().TrustQuotaUSD = 0
	t.Cleanup(func() {
		model.ChannelMonitorIncomeReady.Store(ready)
		operation_setting.GetQuotaSetting().TrustQuotaUSD = oldTrust
		common.LogConsumeEnabled, common.BatchUpdateEnabled = oldLogs, oldBatch
	})
	t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", t.TempDir())
	t.Setenv("CHANNEL_DAILY_COST_RELIABLE_OUTBOX", "false")
	require.NoError(t, model.DB.AutoMigrate(&model.ChannelMonitorIncome{}, &model.ChannelMonitorIncomeRecoveryCursor{}, &model.ChannelMonitorIncomeGap{}, &model.ChannelDailyCostOutbox{}, &model.SubscriptionPlan{}, &model.SubscriptionPreConsumeRecord{}))
	for i, tc := range []struct {
		name, preference, source              string
		wallet, total, reserve, final         int
		unlimited, playground, trusted, probe bool
	}{
		{name: "wallet", preference: "wallet_only", source: "wallet", wallet: 10000, reserve: 100, final: 50},
		{name: "subscription", preference: "subscription_only", source: "subscription", wallet: 10000, total: 1000, reserve: 100, final: 50},
		{name: "wallet_fallback", preference: "wallet_first", source: "subscription", wallet: 10, total: 1000, reserve: 100, final: 50},
		{name: "subscription_fallback", preference: "subscription_first", source: "wallet", wallet: 10000, total: 10, reserve: 100, final: 50},
		{name: "wallet_zero", preference: "wallet_only", source: "wallet", wallet: 10000},
		{name: "subscription_zero", preference: "subscription_only", source: "subscription", wallet: 10000, total: 1000},
		{name: "unlimited_token", preference: "wallet_only", source: "wallet", wallet: 10000, reserve: 100, final: 50, unlimited: true},
		{name: "playground", preference: "wallet_only", source: "wallet", wallet: 10000, reserve: 100, final: 50, playground: true},
		{name: "trusted", preference: "wallet_only", source: "wallet", wallet: 10000, reserve: 100, final: 50, trusted: true},
		{name: "probe", preference: "wallet_only", source: "wallet", wallet: 10000, reserve: 100, final: 50, probe: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			truncate(t)
			id := 9850 + i
			seedUser(t, id, tc.wallet)
			seedToken(t, id, id, "compat-"+tc.name, 10000)
			if tc.unlimited {
				require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", id).Updates(map[string]any{"unlimited_quota": true, "remain_quota": 0}).Error)
			}
			if tc.total > 0 {
				plan := model.SubscriptionPlan{Id: id, Title: tc.name, QuotaResetPeriod: "never"}
				require.NoError(t, model.DB.Create(&plan).Error)
				seedSubscription(t, id, id, int64(tc.total), 0)
				require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", id).Updates(map[string]any{"plan_id": id, "allow_wallet_overflow": true}).Error)
				t.Cleanup(func() { assert.NoError(t, model.DB.Delete(&plan).Error); model.InvalidateSubscriptionPlanCache(id) })
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			ctx.Set("channel_id", id)
			ctx.Set("token_quota", 10000)
			ctx.Set(model.ChannelMonitorStatusProbeLogKey, tc.probe)
			info := &relaycommon.RelayInfo{RequestId: "compat-" + tc.name, UserId: id, TokenId: id, TokenKey: "compat-" + tc.name, TokenUnlimited: tc.unlimited, IsPlayground: tc.playground, ForcePreConsume: !tc.trusted, UserSetting: dto.UserSetting{BillingPreference: tc.preference}}
			if tc.trusted {
				operation_setting.GetQuotaSetting().TrustQuotaUSD = 1 / common.QuotaPerUnit
			}
			t.Cleanup(func() {
				operation_setting.GetQuotaSetting().TrustQuotaUSD = 0
				assert.NoError(t, model.DB.Where("user_id = ?", id).Delete(&model.ChannelMonitorIncome{}).Error)
				assert.NoError(t, model.DB.Where("user_id = ?", id).Delete(&model.SubscriptionPreConsumeRecord{}).Error)
			})
			require.Nil(t, PreConsumeBilling(ctx, tc.reserve, info))
			assert.Equal(t, tc.source, info.BillingSource)
			if tc.trusted {
				assert.Zero(t, info.Billing.GetPreConsumedQuota())
			}
			if tc.name == "subscription_zero" {
				assert.Equal(t, 1, info.Billing.GetPreConsumedQuota())
			}
			info.InitChannelMeta(ctx)
			require.NoError(t, SettleBilling(ctx, info, tc.final))
			require.NoError(t, SettleBilling(ctx, info, tc.final))
			var user model.User
			var token model.Token
			require.NoError(t, model.DB.First(&user, id).Error)
			require.NoError(t, model.DB.First(&token, id).Error)
			wantWallet := tc.wallet
			if tc.source == "wallet" {
				wantWallet -= tc.final
			}
			assert.Equal(t, wantWallet, user.Quota)
			if tc.total > 0 {
				var sub model.UserSubscription
				require.NoError(t, model.DB.First(&sub, id).Error)
				wantUsed := 0
				if tc.source == "subscription" {
					wantUsed = tc.final
				}
				assert.EqualValues(t, wantUsed, sub.AmountUsed)
			}
			wantRemain, wantUsed := 10000-tc.final, tc.final
			if tc.unlimited {
				wantRemain = -tc.final
			}
			if tc.playground {
				wantRemain, wantUsed = 10000, 0
			}
			assert.Equal(t, wantRemain, token.RemainQuota)
			assert.Equal(t, wantUsed, token.UsedQuota)
			var records []model.ChannelMonitorIncome
			require.NoError(t, model.DB.Where("user_id = ?", id).Find(&records).Error)
			if tc.probe {
				assert.Empty(t, records)
			} else {
				require.Len(t, records, 1)
				assert.Equal(t, "settled", records[0].Status)
				assert.EqualValues(t, tc.final, records[0].Quota)
			}
		})
	}
}

func TestChannelMonitorIncomeRecoveryOwnsFailedFinalSettlement(t *testing.T) {
	oldReady := model.ChannelMonitorIncomeReady.Swap(true)
	t.Cleanup(func() { model.ChannelMonitorIncomeReady.Store(oldReady) })
	t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", t.TempDir())
	require.NoError(t, model.DB.AutoMigrate(&model.ChannelMonitorIncome{}, &model.ChannelMonitorIncomeRecoveryCursor{}, &model.ChannelMonitorIncomeGap{}, &model.ChannelDailyCostOutbox{}))
	for _, final := range []int{50, 100, 150} {
		t.Run(strconv.Itoa(final), func(t *testing.T) {
			truncate(t)
			require.NoError(t, model.DB.Exec("DELETE FROM channel_monitor_incomes").Error)
			t.Cleanup(func() { assert.NoError(t, model.DB.Exec("DELETE FROM channel_monitor_incomes").Error) })
			seedUser(t, 9831, 10000)
			seedToken(t, 9831, 9831, "recovery-final", 10000)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{RequestId: "recovery-final-" + strconv.Itoa(final), UserId: 9831, TokenId: 9831, TokenKey: "recovery-final", ForcePreConsume: true, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 9831}}
			session := &BillingSession{relayInfo: info, funding: &WalletFunding{userId: info.UserId}}
			require.Nil(t, session.preConsume(ctx, 100))
			info.Billing = session
			costDay := model.ChannelDailyCostDayStart(time.Now().Unix()) - 86400
			if final == 50 {
				event := model.ChannelDailyCostOutbox{EventId: channelDailyCostEventId(ctx, info.ChannelId), ChannelId: info.ChannelId, OccurredAt: costDay, ProcessedAt: costDay + 1}
				require.NoError(t, model.DB.Create(&event).Error)
				t.Cleanup(func() { assert.NoError(t, model.DB.Delete(&event).Error) })
				require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("income-cost-read-failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "channel_daily_cost_outboxes" {
						tx.AddError(errors.New("injected cost attribution read failure"))
					}
				}))
			}
			require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("income-final-failure", func(tx *gorm.DB) {
				updates, ok := tx.Statement.Dest.(map[string]any)
				if ok && tx.Statement.Table == "channel_monitor_incomes" && updates["status"] == "settled" {
					tx.AddError(errors.New("injected final confirmation failure"))
				}
			}))
			err := SettleBilling(ctx, info, final)
			require.NoError(t, model.DB.Callback().Update().Remove("income-final-failure"))
			if final == 50 {
				require.NoError(t, model.DB.Callback().Query().Remove("income-cost-read-failure"))
			}
			require.NoError(t, err, "accepted settlement lets callers record usage exactly once while funding waits for recovery")
			assert.False(t, session.NeedsRefund(), "durable recovery now owns the reservation")
			session.Refund(ctx)
			var user model.User
			require.NoError(t, model.DB.First(&user, info.UserId).Error)
			assert.Equal(t, 9900, user.Quota)
			var queued model.ChannelMonitorIncome
			require.NoError(t, model.DB.Where("settlement_key = ?", model.ChannelMonitorIncomeKey(info.RequestId, "request")).First(&queued).Error)
			assert.Equal(t, "funding_pending", queued.Status, "accepting recovery must not claim money is committed")
			assert.Equal(t, final-100, queued.FundingDelta)
			var recordedGaps int64
			require.NoError(t, model.DB.Model(&model.ChannelMonitorIncomeGap{}).Where("channel_id = ?", info.ChannelId).Count(&recordedGaps).Error)
			assert.Zero(t, recordedGaps, "post-insert read failures must remain recoverable without a permanent gap")
			info.Billing = nil // The recovery handler has no request or session.
			handlers := []SystemTaskHandler{channelMonitorIncomeRecoveryHandler{}, channelMonitorIncomeCostRecoveryHandler{}}
			if final == 50 {
				// Independent queues may attribute cost before funding succeeds.
				handlers[0], handlers[1] = handlers[1], handlers[0]
			}
			for _, handler := range handlers {
				task, err := model.CreateSystemTask(handler.Type(), nil, nil)
				require.NoError(t, err)
				task, claimed, err := model.ClaimSystemTask(task.ID, handler.Type(), "income-recovery-test", time.Now().Unix()+60)
				require.NoError(t, err)
				require.True(t, claimed)
				handler.Run(t.Context(), task, "income-recovery-test")
				finished, err := model.GetSystemTaskByTaskID(task.TaskID)
				require.NoError(t, err)
				assert.Equal(t, model.SystemTaskStatusSucceeded, finished.Status, finished.Error)
			}
			require.NoError(t, model.DB.First(&user, info.UserId).Error)
			assert.Equal(t, 10000-final, user.Quota)
			var token model.Token
			require.NoError(t, model.DB.First(&token, info.TokenId).Error)
			assert.Equal(t, 10000-final, token.RemainQuota)
			assert.Equal(t, final, token.UsedQuota)
			var income model.ChannelMonitorIncome
			require.NoError(t, model.DB.Where("settlement_key = ?", model.ChannelMonitorIncomeKey(info.RequestId, "request")).First(&income).Error)
			assert.Equal(t, "settled", income.Status)
			assert.EqualValues(t, final, income.Quota)
			if final == 50 {
				assert.Equal(t, 1, income.CostRecorded)
				assert.Equal(t, costDay, income.DayStart)
				var gaps int64
				require.NoError(t, model.DB.Model(&model.ChannelMonitorIncomeGap{}).Where("channel_id = ?", info.ChannelId).Count(&gaps).Error)
				assert.Zero(t, gaps, "a saved income with a retryable cost read must not create a permanent gap")
			}
			completed, err := model.RecoverChannelMonitorIncomeFunding(context.Background(), 100)
			require.NoError(t, err)
			assert.Zero(t, completed)
		})
	}
}

func TestChannelMonitorIncomeReservedSessionLifecycle(t *testing.T) {
	ready := model.ChannelMonitorIncomeReady.Swap(true)
	t.Cleanup(func() { model.ChannelMonitorIncomeReady.Store(ready) })
	t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", t.TempDir())
	require.NoError(t, model.DB.AutoMigrate(&model.ChannelMonitorIncome{}, &model.ChannelMonitorIncomeRecoveryCursor{}, &model.ChannelMonitorIncomeGap{}, &model.ChannelDailyCostOutbox{}, &model.ChannelLocalResponseRefund{}))
	for _, scenario := range []string{"zero_refund", "supplement_refund", "supplement_rejected", "local_refund_recovery", "lost_final_instruction", "channel_retry", "selected_before_handler"} {
		t.Run(scenario, func(t *testing.T) {
			truncate(t)
			seedUser(t, 9841, 10000)
			seedToken(t, 9841, 9841, "reserved-session", 10000)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{RequestId: "reserved-session-" + scenario, UserId: 9841, TokenId: 9841, TokenKey: "reserved-session", ForcePreConsume: true, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 9841}}
			session := &BillingSession{relayInfo: info, funding: &WalletFunding{userId: info.UserId}}
			quota := 100
			if scenario == "zero_refund" {
				quota = 0
			}
			if scenario == "selected_before_handler" {
				info.ChannelMeta = nil
				ctx.Set("channel_id", 9841)
			}
			require.Nil(t, session.preConsume(ctx, quota))
			if scenario == "selected_before_handler" {
				assert.Nil(t, info.ChannelMeta, "pre-consume must not initialize provider attempt state")
				BeginChannelDailyCostAttempt(ctx, 9841)
				info.InitChannelMeta(ctx)
			}
			info.Billing = session
			key := model.ChannelMonitorIncomeKey(info.RequestId, "request")
			t.Cleanup(func() {
				assert.NoError(t, model.DB.Where("settlement_key = ?", key).Delete(&model.ChannelMonitorIncome{}).Error)
				assert.NoError(t, model.DB.Where("request_id = ?", info.RequestId).Delete(&model.ChannelLocalResponseRefund{}).Error)
			})
			wantCharge, wantStatus := 0, "settled"
			switch scenario {
			case "zero_refund", "supplement_refund", "supplement_rejected":
				if scenario == "supplement_refund" {
					require.NoError(t, session.Reserve(150))
				}
				if scenario == "supplement_rejected" {
					apiErr := ReserveBilling(info, 10001)
					require.NotNil(t, apiErr)
					assert.Equal(t, 403, apiErr.StatusCode)
				}
				assert.True(t, session.NeedsRefund())
				session.Refund(ctx)
				session.Refund(ctx)
				assert.False(t, session.NeedsRefund())
			case "local_refund_recovery":
				require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("reserved-refund-failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" {
						tx.AddError(errors.New("injected refund outage"))
					}
				}))
				err := session.FinishWithoutCharge(ctx.Request.Context())
				require.NoError(t, model.DB.Callback().Update().Remove("reserved-refund-failure"))
				require.NoError(t, err, "durable intent accepts local response while refund waits")
				require.Error(t, SettleBilling(ctx, info, 150))
				session.Refund(ctx)
				require.NoError(t, model.ApplyChannelLocalResponseRefund(t.Context(), info.RequestId))
				require.NoError(t, model.ApplyChannelLocalResponseRefund(t.Context(), info.RequestId))
			case "lost_final_instruction":
				require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("reserved-read-failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "channel_monitor_incomes" {
						tx.AddError(errors.New("injected outage before final instruction"))
					}
				}))
				err := SettleBilling(ctx, info, 150)
				require.NoError(t, model.DB.Callback().Query().Remove("reserved-read-failure"))
				require.Error(t, err)
				session.Refund(ctx)
				require.Error(t, session.Reserve(200))
				completed, err := model.RecoverChannelMonitorIncomeFunding(t.Context(), 100)
				require.NoError(t, err)
				assert.Zero(t, completed)
				wantCharge, wantStatus = 100, "reserved"
			case "channel_retry":
				info.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: 9842}
				require.NoError(t, SettleBilling(ctx, info, 150))
				wantCharge = 150
			case "selected_before_handler":
				require.NoError(t, SettleBilling(ctx, info, 150))
				wantCharge = 150
			}
			var income model.ChannelMonitorIncome
			var user model.User
			var token model.Token
			require.NoError(t, model.DB.Where("settlement_key = ?", key).First(&income).Error)
			require.NoError(t, model.DB.First(&user, info.UserId).Error)
			require.NoError(t, model.DB.First(&token, info.TokenId).Error)
			assert.Equal(t, wantStatus, income.Status)
			assert.EqualValues(t, wantCharge, income.Quota)
			assert.Equal(t, info.ChannelId, income.ChannelID)
			if scenario == "selected_before_handler" {
				assert.Equal(t, channelDailyCostEventId(ctx, info.ChannelId), income.CostEventID)
			}
			assert.Equal(t, 10000-wantCharge, user.Quota)
			assert.Equal(t, 10000-wantCharge, token.RemainQuota)
			assert.Equal(t, wantCharge, token.UsedQuota)
		})
	}
}

func TestChannelMonitorIncomeEarlyRefundAndBatchFunding(t *testing.T) {
	previousReady, previousBatch := model.ChannelMonitorIncomeReady.Load(), common.BatchUpdateEnabled
	model.ChannelMonitorIncomeReady.Store(true)
	t.Cleanup(func() {
		model.ChannelMonitorIncomeReady.Store(previousReady)
		common.BatchUpdateEnabled = previousBatch
	})
	require.NoError(t, model.DB.AutoMigrate(&model.ChannelMonitorIncome{}, &model.ChannelMonitorIncomeRecoveryCursor{}, &model.ChannelDailyCostOutbox{}))
	for _, scenario := range []string{"early_midjourney_refund", "batch_funding"} {
		t.Run(scenario, func(t *testing.T) {
			truncate(t)
			common.BatchUpdateEnabled = scenario == "batch_funding"
			seedUser(t, 9801, 10000)
			seedToken(t, 9801, 9801, "review-profit", 10000)
			seedChannel(t, 9801)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{RequestId: scenario, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 9801}, UserId: 9801, TokenId: 9801, TokenKey: "review-profit", OriginModelName: "test-model", BillingSource: BillingSourceWallet, IsPlayground: true}
			key := model.ChannelMonitorIncomeKey(scenario, "request")
			wantCharge := int64(100)
			if scenario == "early_midjourney_refund" {
				task := &model.Midjourney{UserId: 9801, ChannelId: 9801, Action: "IMAGINE", Quota: 100, MjId: scenario}
				_, err := PrepareMidjourneyTaskBilling(info, task, 100, true)
				require.NoError(t, err)
				require.NoError(t, task.Insert())
				submission := *task
				require.True(t, RefundMidjourneyQuota(context.Background(), task, "upstream failed before initial income"))
				applied, err := SettleMonitoredMidjourneyBilling(ctx, info, &submission, true)
				require.NoError(t, err)
				require.False(t, applied, "an early failed task must never be charged")
				key = model.ChannelMonitorIncomeKey(strconv.Itoa(task.Id), "midjourney")
				wantCharge = 0
			} else {
				require.NoError(t, SettleBilling(ctx, info, 100))
			}
			var user model.User
			var income model.ChannelMonitorIncome
			require.NoError(t, model.DB.First(&user, 9801).Error)
			require.NoError(t, model.DB.Where("settlement_key = ?", key).First(&income).Error)
			assert.Equal(t, wantCharge, int64(10000-user.Quota))
			assert.Equal(t, wantCharge, income.Quota)
			assert.Equal(t, "settled", income.Status)
			assert.NotEmpty(t, income.CostEventID, "early refund must keep the eventual cost association")
		})
	}
}

func TestTaskIncomeAttributionFailureDoesNotCommitTaskFunding(t *testing.T) {
	if dsn := os.Getenv("TEST_PROFIT_TASK_MYSQL_DSN"); dsn != "" {
		require.Contains(t, dsn, "127.0.0.1:13318")
		require.Contains(t, dsn, "/new_api_cost_backlog_test?")
		db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
		require.NoError(t, err)
		previous, previousType := model.DB, common.MainDatabaseType()
		model.DB = db
		common.SetMainDatabaseType(common.DatabaseTypeMySQL)
		t.Cleanup(func() {
			model.DB = previous
			common.SetMainDatabaseType(previousType)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			assert.NoError(t, sqlDB.Close())
		})
	} else if dsn := os.Getenv("TEST_PROFIT_TASK_POSTGRES_DSN"); dsn != "" {
		require.Contains(t, dsn, "127.0.0.1:15438")
		require.Contains(t, dsn, "/new_api_cost_backlog_test?")
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		require.NoError(t, err)
		previous, previousType := model.DB, common.MainDatabaseType()
		model.DB = db
		common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
		t.Cleanup(func() {
			model.DB = previous
			common.SetMainDatabaseType(previousType)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			assert.NoError(t, sqlDB.Close())
		})
	}
	tables := []any{&model.Task{}, &model.User{}, &model.Token{}, &model.ChannelMonitorIncome{}, &model.ChannelMonitorIncomeRecoveryCursor{}, &model.ChannelMonitorIncomeGap{}, &model.ChannelDailyCostOutbox{}, &model.ChannelLocalResponseRefund{}}
	var created []any
	for _, table := range tables {
		if !model.DB.Migrator().HasTable(table) {
			created = append(created, table)
		}
	}
	require.NoError(t, model.DB.AutoMigrate(tables...))
	t.Cleanup(func() {
		for _, table := range created {
			assert.NoError(t, model.DB.Migrator().DropTable(table))
		}
	})
	ready := model.ChannelMonitorIncomeReady.Swap(true)
	t.Cleanup(func() { model.ChannelMonitorIncomeReady.Store(ready) })
	t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", t.TempDir())
	const id = 9865
	seedUser(t, id, 10000)
	seedToken(t, id, id, "task-attribution-failure", 10000)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/videos", nil)
	info := &relaycommon.RelayInfo{RequestId: "task-attribution-failure", UserId: id, TokenId: id, TokenKey: "task-attribution-failure", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: id}, ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
	require.Nil(t, PreConsumeBilling(ctx, 100, info))
	info.ChannelMeta.ChannelId = id + 1
	task := makeTask(id, id+1, 150, id, BillingSourceWallet, 0)
	task.PrivateData.Execution = &model.TaskExecutionSnapshot{RequestID: info.RequestId}
	failed := false
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("task-attribution-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "channel_monitor_incomes" && !failed {
			failed = true
			tx.AddError(errors.New("injected final channel attribution failure"))
		}
	}))
	err := PersistTaskWithBilling(ctx, info, task)
	require.NoError(t, model.DB.Callback().Update().Remove("task-attribution-failure"))
	assert.Error(t, err)
	var count int64
	require.NoError(t, model.DB.Model(&model.Task{}).Where("task_id = ?", task.TaskID).Count(&count).Error)
	assert.Zero(t, count, "an attribution failure must not commit a charge against the previous channel")
	var user model.User
	var income model.ChannelMonitorIncome
	require.NoError(t, model.DB.First(&user, id).Error)
	require.NoError(t, model.DB.Where("user_id = ?", id).First(&income).Error)
	assert.Equal(t, 9900, user.Quota)
	assert.Equal(t, "reserved", income.Status)
	info.Billing.Refund(ctx)
	require.NoError(t, model.DB.First(&user, id).Error)
	assert.Equal(t, 10000, user.Quota)
	t.Cleanup(func() {
		assert.NoError(t, model.DB.Where("user_id = ?", id).Delete(&model.ChannelMonitorIncome{}).Error)
		assert.NoError(t, model.DB.Where("request_id = ?", info.RequestId).Delete(&model.ChannelLocalResponseRefund{}).Error)
		assert.NoError(t, model.DB.Where("channel_id = ?", id+1).Delete(&model.ChannelMonitorIncomeGap{}).Error)
		assert.NoError(t, model.DB.Where("user_id = ?", id).Delete(&model.Task{}).Error)
		assert.NoError(t, model.DB.Unscoped().Delete(&model.Token{}, id).Error)
		assert.NoError(t, model.DB.Unscoped().Delete(&model.User{}, id).Error)
	})
}

func TestTaskIncomeReconcilesCompletionBeforeInitialSettlement(t *testing.T) {
	for _, target := range []int{0, 1500} {
		t.Run(strconv.Itoa(target), func(t *testing.T) {
			truncate(t)
			previous := model.ChannelMonitorIncomeReady.Load()
			model.ChannelMonitorIncomeReady.Store(true)
			t.Cleanup(func() { model.ChannelMonitorIncomeReady.Store(previous) })
			require.NoError(t, model.DB.AutoMigrate(&model.ChannelMonitorIncome{}, &model.ChannelMonitorIncomeRecoveryCursor{}, &model.ChannelDailyCostOutbox{}))
			require.NoError(t, model.DB.Where("channel_id = ?", 919).Delete(&model.ChannelMonitorIncome{}).Error)
			const identity, initial, charge = 919, 10000, 1000
			seedUser(t, identity, initial)
			seedToken(t, identity, identity, "income-early-task", initial)
			seedChannel(t, identity)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/videos", nil)
			info := &relaycommon.RelayInfo{RequestId: "income-early-task", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: identity}, UserId: identity, TokenId: identity, TokenKey: "income-early-task", OriginModelName: "test-model", UsingGroup: "default", ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
			require.Nil(t, PreConsumeBilling(ctx, charge, info))
			task := makeTask(identity, identity, charge, identity, BillingSourceWallet, 0)
			task.PrivateData.Execution = &model.TaskExecutionSnapshot{RequestID: info.RequestId}
			PrepareTaskChannelCost(ctx, task)
			require.NoError(t, PersistTaskWithBilling(ctx, info, task))
			staleSubmission := *task
			_, err := model.ApplyTaskBilling(context.Background(), task, model.TaskBillingOperationSettle, target)
			require.NoError(t, err)
			require.NoError(t, SettleBilling(ctx, info, charge))
			_, err = model.RegisterChannelTaskCostEvent(context.Background(), model.ChannelTaskCostEventInput{
				TaskID: task.ID, CostEventId: task.PrivateData.BillingContext.ChannelCostEventId,
				ChannelId: identity, OccurredAt: task.SubmitTime, UserId: identity,
				InitialQuota: charge, CostNanoCNY: 100,
			})
			require.NoError(t, err)
			require.NoError(t, model.CompleteChannelMonitorTaskIncome(context.Background(), staleSubmission.ID))
			var user model.User
			var income model.ChannelMonitorIncome
			require.NoError(t, model.DB.First(&user, identity).Error)
			require.NoError(t, model.DB.Where("settlement_key = ?", model.ChannelMonitorIncomeKey(info.RequestId, "request")).First(&income).Error)
			assert.Equal(t, target, initial-user.Quota)
			assert.Equal(t, int64(target), income.Quota)
			assert.Equal(t, "settled", income.Status)
			assert.Equal(t, 1, income.CostRecorded)
		})
	}
}

func TestRealtimeProfitIncludesEachFundingDeductionAndFinalSettlement(t *testing.T) {
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldMaster, oldPath := common.IsMasterNode, common.SQLitePath
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldReady := model.ChannelMonitorIncomeReady.Load()
	t.Setenv("SQL_DSN", "local")
	t.Setenv("LOG_SQL_DSN", "")
	common.IsMasterNode, common.SQLitePath = true, filepath.Join(t.TempDir(), "realtime-profit.db")
	t.Cleanup(func() {
		if sqlDB, err := model.DB.DB(); err == nil {
			assert.NoError(t, sqlDB.Close())
		}
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.IsMasterNode, common.SQLitePath = oldMaster, oldPath
		common.SetDatabaseTypes(oldMainType, oldLogType)
		model.ChannelMonitorIncomeReady.Store(oldReady)
	})
	require.NoError(t, model.InitDB())
	model.LOG_DB = model.DB
	truncate(t)
	oldQuotaPerUnit, oldExchangeRate := common.QuotaPerUnit, operation_setting.USDExchangeRate
	common.QuotaPerUnit, operation_setting.USDExchangeRate = 100, 7
	t.Cleanup(func() {
		common.QuotaPerUnit, operation_setting.USDExchangeRate = oldQuotaPerUnit, oldExchangeRate
	})
	seedUser(t, 892, 10_000)
	seedToken(t, 892, 892, "profit-realtime", 10_000)
	seedChannel(t, 892)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{
		RequestId: "profit-realtime-request", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 892},
		UserId: 892, TokenId: 892, TokenKey: "sk-profit-realtime", OriginModelName: "gpt-4o-realtime-preview",
		UsingGroup: "default", BillingSource: BillingSourceWallet,
	}
	usage := &dto.RealtimeUsage{}
	usage.InputTokenDetails.TextTokens = 10
	require.NoError(t, PreWssConsumeQuota(ctx, info, usage))
	require.NoError(t, PreWssConsumeQuota(ctx, info, usage))
	require.NoError(t, SettleBilling(ctx, info, 100))
	var user model.User
	require.NoError(t, model.DB.First(&user, 892).Error)
	var incomes []model.ChannelMonitorIncome
	require.NoError(t, model.DB.Where("user_id = ?", 892).Find(&incomes).Error)
	require.Len(t, incomes, 3)
	var quota, amount int64
	for _, income := range incomes {
		assert.Equal(t, "settled", income.Status)
		assert.Equal(t, incomes[0].CostEventID, income.CostEventID)
		quota += income.Quota
		amount += income.IncomeNanoCNY
	}
	assert.Equal(t, int64(10_000-user.Quota), quota, "profit follows every actual wallet deduction")
	assert.Equal(t, quota*10_000_000, amount)
}

func TestTaskBillingCorrectionsReplaceChannelMonitorIncomeSnapshot(t *testing.T) {
	truncate(t)
	oldReady := model.ChannelMonitorIncomeReady.Load()
	model.ChannelMonitorIncomeReady.Store(true)
	t.Cleanup(func() { model.ChannelMonitorIncomeReady.Store(oldReady) })
	require.NoError(t, model.DB.AutoMigrate(&model.ChannelMonitorIncome{}))
	require.NoError(t, model.DB.Exec("DELETE FROM channel_monitor_incomes").Error)

	const userID, tokenID, channelID = 900, 900, 900
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "sk-income-task", 10000)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, 1000, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task_income_correction"
	task.PrivateData.Execution = &model.TaskExecutionSnapshot{RequestID: "request-income-correction"}
	require.NoError(t, model.DB.Create(task).Error)

	income := model.ChannelMonitorIncome{
		SettlementKey: model.ChannelMonitorIncomeKey("request-income-correction", "request"),
		DayStart:      model.ChannelDailyCostDayStart(task.SubmitTime), ChannelID: channelID,
		UserID: userID, APIKeyID: tokenID, APIKeyKey: "income-task-key", APIKeyName: "任务 Key",
		ModelKey: model.ChannelMonitorDailyCostModelKey("test-model"), ModelName: "test-model",
		GroupName: "default", BillingSource: BillingSourceWallet,
		QuotaPerUnit: "500000", USDToCNY: "7", Quota: 1000,
		IncomeNanoCNY: 14_000_000, Status: "settled", CostRecorded: 1,
	}
	require.NoError(t, model.DB.Create(&income).Error)

	RecalculateTaskQuota(context.Background(), task, 1500, "test supplement")
	RecalculateTaskQuota(context.Background(), task, 1500, "replayed supplement")
	var saved model.ChannelMonitorIncome
	require.NoError(t, model.DB.Where("settlement_key = ?", income.SettlementKey).First(&saved).Error)
	assert.Equal(t, int64(1500), saved.Quota)
	assert.Equal(t, int64(3_000_000), saved.IncomeNanoCNY)

	assert.True(t, RefundTaskQuota(context.Background(), task, "test refund"))
	assert.True(t, RefundTaskQuota(context.Background(), task, "replayed refund"))
	require.NoError(t, model.DB.Where("settlement_key = ?", income.SettlementKey).First(&saved).Error)
	assert.Zero(t, saved.Quota)
	assert.Zero(t, saved.IncomeNanoCNY)
	assert.Equal(t, "settled", saved.Status)
}

func setupChannelMonitorIncomeRecoveryTest(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	tables := []any{&model.SystemTaskLock{}, &model.SystemTask{}, &model.ChannelMonitorIncome{}, &model.ChannelMonitorIncomeRecoveryCursor{}, &model.ChannelDailyCostOutbox{}, &model.ChannelMonitorFundingCacheRepair{}}
	require.NoError(t, model.DB.AutoMigrate(tables...))
	for _, table := range tables {
		require.NoError(t, model.DB.Where("1 = 1").Delete(table).Error)
		t.Cleanup(func() { assert.NoError(t, model.DB.Where("1 = 1").Delete(table).Error) })
	}
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	oldRedis, oldRDB, oldWriter := common.RedisEnabled, common.RDB, common.RDBMonitorWrite
	oldReady := model.ChannelMonitorIncomeReady.Swap(true)
	common.RedisEnabled, common.RDB, common.RDBMonitorWrite = true, client, client
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB, common.RDBMonitorWrite = oldRedis, oldRDB, oldWriter
		model.ChannelMonitorIncomeReady.Store(oldReady)
		assert.NoError(t, client.Close())
	})
	withSystemTaskRegistry(t, channelMonitorIncomeRecoveryHandler{}, channelMonitorIncomeCostRecoveryHandler{}, channelMonitorFundingCacheRecoveryHandler{})
	return server
}

func TestChannelMonitorIncomeRecoveryQueuesAreIndependent(t *testing.T) {
	for _, blockedHandler := range []ScheduledSystemTaskHandler{channelMonitorIncomeRecoveryHandler{}, channelMonitorIncomeCostRecoveryHandler{}} {
		t.Run(blockedHandler.Type(), func(t *testing.T) {
			server := setupChannelMonitorIncomeRecoveryTest(t)
			const key = "user:recovery-isolation"
			require.NoError(t, common.RDB.HSet(t.Context(), key, "Quota", 9900).Err())
			require.NoError(t, model.DB.Create(&model.ChannelMonitorFundingCacheRepair{CacheKey: key, Revision: "pending", UpdatedAt: 1}).Error)
			runSystemTaskScheduler()
			blockedTask, err := model.GetLatestSystemTask(blockedHandler.Type())
			require.NoError(t, err)
			require.NotNil(t, blockedTask)
			_, claimed, err := model.ClaimSystemTask(blockedTask.ID, blockedTask.Type, "blocked-runner", common.GetTimestamp()+120)
			require.NoError(t, err)
			require.True(t, claimed)

			// Stall the real recovery query. Other queues must finish while this
			// query is still waiting, without relying on expiry or sleep timing.
			type stalledQueryKey struct{}
			ctx, cancel := context.WithCancel(context.WithValue(t.Context(), stalledQueryKey{}, true))
			entered, finished := make(chan struct{}, 1), make(chan struct{})
			require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("test:stalled_income_recovery", func(tx *gorm.DB) {
				if tx.Statement.Table == "channel_monitor_incomes" && tx.Statement.Context.Value(stalledQueryKey{}) == true {
					entered <- struct{}{}
					<-tx.Statement.Context.Done()
					tx.AddError(tx.Statement.Context.Err())
				}
			}))
			t.Cleanup(func() {
				cancel()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("recovery handler did not honor parent cancellation")
				}
				assert.NoError(t, model.DB.Callback().Query().Remove("test:stalled_income_recovery"))
			})
			go func() {
				defer close(finished)
				blockedHandler.Run(ctx, blockedTask, "blocked-runner")
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("recovery query did not reach the barrier")
			}
			for _, handler := range registeredSystemTaskHandlers() {
				if handler.Type() == blockedHandler.Type() {
					continue
				}
				task, err := model.GetLatestSystemTask(handler.Type())
				require.NoError(t, err)
				require.NotNil(t, task, "scheduler must create each independent queue")
				_, claimed, err := model.ClaimSystemTask(task.ID, task.Type, "healthy-runner", common.GetTimestamp()+120)
				require.NoError(t, err)
				require.True(t, claimed, "a different queue must not share the blocked lease")
				handler.Run(t.Context(), task, "healthy-runner")
				saved, err := model.GetLatestSystemTask(task.Type)
				require.NoError(t, err)
				assert.Equal(t, model.SystemTaskStatusSucceeded, saved.Status)
			}
			assert.False(t, server.Exists(key), "cache repair must not wait for the income query")
			var pending int64
			require.NoError(t, model.DB.Model(&model.ChannelMonitorFundingCacheRepair{}).Count(&pending).Error)
			assert.Zero(t, pending)
			cancel()
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("recovery did not stop after parent cancellation")
			}
			saved, err := model.GetLatestSystemTask(blockedTask.Type)
			require.NoError(t, err)
			assert.Equal(t, model.SystemTaskStatusFailed, saved.Status)
		})
	}
}

func TestChannelMonitorIncomeCacheRecoveryRetriesAndHonorsCancellation(t *testing.T) {
	server := setupChannelMonitorIncomeRecoveryTest(t)
	model.ChannelMonitorIncomeReady.Store(false)
	const key = "user:recovery-retry"
	require.NoError(t, common.RDB.HSet(t.Context(), key, "Quota", 9900).Err())
	require.NoError(t, model.DB.Create(&model.ChannelMonitorFundingCacheRepair{CacheKey: key, Revision: "pending", UpdatedAt: 1}).Error)
	handler := channelMonitorFundingCacheRecoveryHandler{}
	for _, scenario := range []string{"redis_unavailable", "parent_canceled", "redis_recovered"} {
		t.Run(scenario, func(t *testing.T) {
			// Recovery remains scheduled with profit recording disabled. This
			// also covers durable intents left by local refunds or older runs.
			runSystemTaskScheduler()
			task, err := model.GetLatestSystemTask(handler.Type())
			require.NoError(t, err)
			require.NotNil(t, task)
			_, claimed, err := model.ClaimSystemTask(task.ID, task.Type, "cache-runner", common.GetTimestamp()+120)
			require.NoError(t, err)
			require.True(t, claimed)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "parent_canceled" {
				cancel()
			} else if scenario == "redis_unavailable" {
				server.SetError("Redis unavailable")
			}
			handler.Run(ctx, task, "cache-runner")
			server.SetError("")
			saved, err := model.GetLatestSystemTask(handler.Type())
			require.NoError(t, err)
			var pending int64
			require.NoError(t, model.DB.Model(&model.ChannelMonitorFundingCacheRepair{}).Count(&pending).Error)
			if scenario == "redis_recovered" {
				assert.Equal(t, model.SystemTaskStatusSucceeded, saved.Status)
				assert.Zero(t, pending)
				assert.False(t, server.Exists(key))
			} else {
				assert.Equal(t, model.SystemTaskStatusFailed, saved.Status)
				assert.Equal(t, int64(1), pending)
				assert.True(t, server.Exists(key))
			}
			require.NoError(t, model.DB.Model(saved).Update("updated_at", common.GetTimestamp()-120).Error)
		})
	}
}
