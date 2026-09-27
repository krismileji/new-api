package service

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, model.DB.AutoMigrate(&model.ChannelMonitorIncome{}, &model.ChannelDailyCostOutbox{}))
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
	assert.Equal(t, int64(7_000_000_000), income.IncomeNanoCNY)
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
	assert.Equal(t, int64(3_500_000_000), subscriptionIncome.IncomeNanoCNY)
	assert.Equal(t, "subscription", subscriptionIncome.BillingSource)
	assert.Equal(t, "settled", subscriptionIncome.Status)

	info.RequestId, info.SubscriptionId = "profit-failed-funding", 891
	require.Error(t, SettleBilling(subContext, &info, 50))
	var pendingIncome model.ChannelMonitorIncome
	require.NoError(t, model.DB.Where("settlement_key = ?", model.ChannelMonitorIncomeKey(info.RequestId, "request")).First(&pendingIncome).Error)
	assert.Equal(t, "pending", pendingIncome.Status)

	// The legacy task charge and its later callback must use the same income
	// record even though the callback no longer has an HTTP request context.
	task := &model.Midjourney{UserId: 890, ChannelId: 890, Action: "IMAGINE", Quota: 100, MjId: "profit-midjourney"}
	require.NoError(t, task.Insert())
	applied, err := SettleMonitoredMidjourneyBilling(ctx, relayInfo, task, true)
	require.NoError(t, err)
	assert.True(t, applied)
	key := model.ChannelMonitorIncomeKey(strconv.Itoa(task.Id), "midjourney")
	var midjourneyIncome model.ChannelMonitorIncome
	require.NoError(t, model.DB.Where("settlement_key = ?", key).First(&midjourneyIncome).Error)
	assert.Equal(t, int64(7_000_000_000), midjourneyIncome.IncomeNanoCNY)
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
	assert.Equal(t, quota*70_000_000, amount)
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
	assert.Equal(t, int64(21_000_000), saved.IncomeNanoCNY)

	assert.True(t, RefundTaskQuota(context.Background(), task, "test refund"))
	assert.True(t, RefundTaskQuota(context.Background(), task, "replayed refund"))
	require.NoError(t, model.DB.Where("settlement_key = ?", income.SettlementKey).First(&saved).Error)
	assert.Zero(t, saved.Quota)
	assert.Zero(t, saved.IncomeNanoCNY)
	assert.Equal(t, "settled", saved.Status)
}
