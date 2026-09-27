package controller

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelMonitorProfitAnalyticsCombinesIncomeAndAllCostSources(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	t.Setenv("CHANNEL_DAILY_COST_RELIABLE_OUTBOX", "true")
	require.NoError(t, db.AutoMigrate(
		&model.ChannelMonitorIncome{}, &model.ChannelMonitorIncomeState{},
		&model.ChannelMonitorDailyCostDetail{}, &model.ChannelDailyCostOutbox{},
	))

	day := model.ChannelDailyCostDayStart(common.GetTimestamp()) - 2*24*60*60
	runChannelMonitorProfitAnalyticsCases(t, db, day)
}

func runChannelMonitorProfitAnalyticsCases(t *testing.T, db *gorm.DB, day int64) {
	t.Helper()
	t.Setenv("CHANNEL_DAILY_COST_RELIABLE_OUTBOX", "true")
	const firstChannel, secondChannel = 900211, 900222
	channels := []int{firstChannel, secondChannel, 900233}
	settlementKeys := []string{"profit-income-channel-one", "profit-income-channel-two", "profit-income-pending"}
	for _, table := range []any{&model.ChannelDailyCost{}, &model.ChannelMonitorDailyCostDetail{}, &model.ChannelDailyCostOutbox{}, &model.ChannelMonitorIncome{}} {
		query := db.Where("channel_id IN ? AND day_start = ?", channels, day)
		switch table.(type) {
		case *model.ChannelDailyCostOutbox:
			query = db.Where("channel_id IN ? AND occurred_at >= ? AND occurred_at < ?", channels, day, day+24*60*60)
		case *model.ChannelMonitorIncome:
			query = db.Where("settlement_key IN ?", settlementKeys)
		}
		require.NoError(t, query.Delete(table).Error)
	}
	require.NoError(t, db.Where("id = ?", 1).Delete(&model.ChannelMonitorIncomeState{}).Error)
	state := model.ChannelMonitorIncomeState{ID: 1, StartedAt: day - 24*60*60}
	require.NoError(t, db.Create(&state).Error)

	modelKey := model.ChannelMonitorDailyCostModelKey("gpt-4.1")
	for _, income := range []model.ChannelMonitorIncome{
		{
			SettlementKey: settlementKeys[0], DayStart: day, ChannelID: firstChannel,
			UserID: 101, APIKeyID: 201, APIKeyKey: "key-one", APIKeyName: "Key One",
			ModelKey: modelKey, ModelName: "gpt-4.1", BillingSource: "wallet", Quota: 100,
			IncomeNanoCNY: 100_000_000_000, Status: "settled", CostEventID: "cost-one", CostRecorded: 1,
		},
		{
			SettlementKey: settlementKeys[1], DayStart: day, ChannelID: secondChannel,
			UserID: 102, APIKeyID: 202, APIKeyKey: "key-two", APIKeyName: "Key Two",
			ModelKey: modelKey, ModelName: "gpt-4.1", BillingSource: "subscription", Quota: 10,
			IncomeNanoCNY: 10_000_000_000, Status: "settled", CostEventID: "cost-two", CostRecorded: 1,
		},
	} {
		require.NoError(t, db.Create(&income).Error)
	}

	require.NoError(t, db.Create(&[]model.ChannelDailyCost{
		{ChannelId: firstChannel, DayStart: day, CostNanoCNY: 50_000_000_000, ProbeCostNanoCNY: 5_000_000_000, ModelDetectionCostNanoCNY: 10_000_000_000, SettledCount: 3},
		{ChannelId: secondChannel, DayStart: day, CostNanoCNY: 25_000_000_000, SettledCount: 1},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelMonitorDailyCostDetail{
		{DayStart: day, ChannelId: firstChannel, UserId: 101, APIKeyId: 201, APIKeyKey: "key-one", APIKeyName: "Key One", ModelKey: modelKey, ModelName: "gpt-4.1", SourceKind: "business", CostNanoCNY: 35_000_000_000, SettledCount: 1},
		{DayStart: day, ChannelId: firstChannel, ModelKey: modelKey, ModelName: "gpt-4.1", SourceKind: "model_detection", CostNanoCNY: 10_000_000_000, SettledCount: 1},
		{DayStart: day, ChannelId: firstChannel, ModelKey: modelKey, ModelName: "gpt-4.1", SourceKind: "status_probe", CostNanoCNY: 5_000_000_000, ProbeCostNanoCNY: 5_000_000_000, SettledCount: 1},
		{DayStart: day, ChannelId: secondChannel, UserId: 102, APIKeyId: 202, APIKeyKey: "key-two", APIKeyName: "Key Two", ModelKey: modelKey, ModelName: "gpt-4.1", SourceKind: "business", CostNanoCNY: 25_000_000_000, SettledCount: 1},
	}).Error)

	query := channelMonitorAnalyticsQuery{
		Metric: "profit", GroupBy: "channel", From: day, To: day + 24*60*60,
		Sort: "profit", Direction: "desc", Page: 1, PageSize: 20,
	}
	response, err := queryChannelMonitorProfitAnalytics(context.Background(), query)
	require.NoError(t, err)
	require.Len(t, response.Items, 2)
	assert.Equal(t, int64(110_000_000_000), response.ScopeSummary["income_nano_cny"])
	assert.Equal(t, int64(100_000_000_000), response.ScopeSummary["wallet_income_nano_cny"])
	assert.Equal(t, int64(10_000_000_000), response.ScopeSummary["subscription_income_nano_cny"])
	assert.Equal(t, int64(75_000_000_000), response.ScopeSummary["cost_nano_cny"])
	assert.Equal(t, int64(35_000_000_000), response.ScopeSummary["profit_nano_cny"])
	assert.Equal(t, int64(10_000_000_000), response.Items[0]["model_detection_cost_nano_cny"])
	assert.Equal(t, true, response.Items[0]["profit_confirmed"])
	assert.Equal(t, int64(5_000_000_000), response.ScopeSummary["probe_cost_nano_cny"])
	for _, dimension := range []string{"day", "user", "api_key", "model", "channel_model", "api_key_channel_model"} {
		detailQuery := query
		detailQuery.GroupBy, detailQuery.PageSize, detailQuery.Sort = dimension, 1, "profit_rate"
		detail, err := queryChannelMonitorProfitAnalytics(context.Background(), detailQuery)
		require.NoError(t, err, dimension)
		assert.Equal(t, int64(35_000_000_000), detail.ScopeSummary["profit_nano_cny"], dimension)
		assert.Equal(t, true, detail.ScopeSummary["profit_confirmed"], dimension)
		require.Len(t, detail.Items, 1)
	}
	filtered := query
	filtered.Channel, filtered.User, filtered.APIKey, filtered.Model = firstChannel, 101, 201, "gpt-4.1"
	detail, err := queryChannelMonitorProfitAnalytics(context.Background(), filtered)
	require.NoError(t, err)
	assert.Equal(t, int64(65_000_000_000), detail.ScopeSummary["profit_nano_cny"])

	require.NoError(t, db.Create(&model.ChannelMonitorIncome{
		SettlementKey: "profit-income-pending", DayStart: day, ChannelID: 900233,
		UserID: 103, APIKeyID: 203, APIKeyKey: "key-pending", APIKeyName: "Pending Key",
		ModelKey: modelKey, ModelName: "gpt-4.1", BillingSource: "wallet", Quota: 10,
		QuotaPerUnit: "100", USDToCNY: "7", IncomeNanoCNY: 7_000_000_000,
		Status: "pending", CostEventID: "cost-pending", CostRecorded: 1,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelDailyCost{
		ChannelId: 900233, DayStart: day, CostNanoCNY: 1_000_000_000, SettledCount: 1,
	}).Error)

	query.OnlyLoss = true
	losses, err := queryChannelMonitorProfitAnalytics(context.Background(), query)
	require.NoError(t, err)
	require.Len(t, losses.Items, 1)
	assert.Equal(t, secondChannel, losses.Items[0]["channel_id"])
	assert.Equal(t, int64(-15_000_000_000), losses.Items[0]["profit_nano_cny"])
	assert.Equal(t, int64(34_000_000_000), losses.ScopeSummary["profit_nano_cny"])
	assert.Equal(t, int64(1), losses.ScopeSummary["pending_income_count"])
	assert.Equal(t, true, losses.Items[0]["profit_confirmed"])
	assert.Equal(t, false, losses.ScopeSummary["profit_confirmed"])

	if common.RedisEnabled && common.RDB != nil {
		require.NoError(t, common.RDB.XAdd(context.Background(), &redis.XAddArgs{Stream: service.ChannelDailyCostRedisStream, Values: map[string]any{"pending": "cost-only-attempt"}}).Err())
		queued, err := queryChannelMonitorProfitAnalytics(context.Background(), query)
		require.NoError(t, err)
		assert.Empty(t, queued.Items)
		assert.Contains(t, queued.Coverage.Reasons, "cost_projection_pending")
		require.NoError(t, common.RDB.Del(context.Background(), service.ChannelDailyCostRedisStream).Err())
	}
	require.NoError(t, db.Model(&model.ChannelMonitorIncomeState{}).Where("id = 1").Update("retained_from", day+86400).Error)
	expired, err := queryChannelMonitorProfitAnalytics(context.Background(), query)
	require.NoError(t, err)
	assert.Empty(t, expired.Items)
	assert.Contains(t, expired.Coverage.Reasons, "profit_history_expired")
}
