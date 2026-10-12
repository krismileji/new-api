package controller

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelMonitorOverviewAnalytics(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorIncome{}, &model.ChannelMonitorIncomeState{}, &model.ChannelMonitorIncomeGap{}, &model.ChannelMonitorDailyCostDetail{}, &model.ChannelDailyCostOutbox{}, &model.ChannelMonitorDailySuccessLedger{}, &model.ChannelMonitorDailyCheckpoint{}))
	runChannelMonitorOverviewAnalyticsCases(t, db, model.ChannelDailyCostDayStart(common.GetTimestamp())-5*86400)

	t.Run("today_joins_live_requests_with_settled_cost", func(t *testing.T) {
		common.RedisEnabled = true
		now := common.GetTimestamp()
		day := model.ChannelDailyCostDayStart(now)
		event := model.NewChannelMonitorEvent(900280, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, now)
		event.EventId, event.ModelName = "overview-live", "live-model"
		event.RequestDispatched, event.IsFinalAttempt = true, true
		require.NoError(t, service.NewChannelMonitorRedisSharedProjectionWithClient(common.RDB).HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event}))
		require.NoError(t, db.Create(&model.ChannelDailyCost{ChannelId: 900280, DayStart: day, CostNanoCNY: 42, SettledCount: 1}).Error)
		response, err := queryChannelMonitorHistoricalAnalytics(context.Background(), channelMonitorAnalyticsQuery{Metric: "overview", GroupBy: "channel", From: day, To: day + 86400, Channel: 900280, Sort: "cost", Direction: "desc", Page: 1, PageSize: 20})
		require.NoError(t, err)
		require.Len(t, response.Items, 1)
		assert.Equal(t, "redis_daily", response.Source)
		assert.EqualValues(t, 42, response.Items[0]["cost_nano_cny"])
		assert.EqualValues(t, 1, response.Items[0]["actual_success_count"])
		assert.Equal(t, 1.0, response.Items[0]["actual_success_rate"])
	})
}

func runChannelMonitorOverviewAnalyticsCases(t *testing.T, db *gorm.DB, day int64) {
	t.Helper()
	t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", t.TempDir())
	t.Setenv("CHANNEL_DAILY_COST_RELIABLE_OUTBOX", "true")
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedis })
	require.NoError(t, db.Where("id = 1").Delete(&model.ChannelMonitorIncomeState{}).Error)
	require.NoError(t, db.Create(&model.ChannelMonitorIncomeState{ID: 1, StartedAt: day}).Error)
	const channel = 900271
	const user = 900272
	const apiKey = 900273
	modelName := "overview-model"
	modelKey := model.ChannelMonitorDailyCostModelKey(modelName)
	require.NoError(t, db.Create(&model.ChannelMonitorDailyCheckpoint{DayStart: day, Revision: 1, UpdatedAt: day + 86399}).Error)
	require.NoError(t, db.Create(&model.ChannelMonitorIncome{
		SettlementKey: "overview-income", DayStart: day, ChannelID: channel, UserID: user, APIKeyID: apiKey, APIKeyKey: "cost-fingerprint", APIKeyName: "Overview Key",
		ModelKey: modelKey, ModelName: modelName, IncomeNanoCNY: 100, Status: "settled", BillingSource: "wallet", CostRecorded: 1, QuotaPerUnit: "1", USDToCNY: "1",
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelDailyCost{
		{ChannelId: channel, DayStart: day, CostNanoCNY: 150, SettledCount: 1},
		{ChannelId: channel + 1, DayStart: day, CostNanoCNY: 20, ProbeCostNanoCNY: 20, SettledCount: 1},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelMonitorDailyCostDetail{
		{DayStart: day, ChannelId: channel, UserId: user, APIKeyId: apiKey, APIKeyKey: "cost-fingerprint", APIKeyName: "Overview Key", ModelKey: modelKey, ModelName: modelName, SourceKind: "business", CostNanoCNY: 150, SettledCount: 1},
		{DayStart: day, ChannelId: channel + 1, ModelKey: modelKey, ModelName: modelName, SourceKind: "status_probe", CostNanoCNY: 20, ProbeCostNanoCNY: 20, SettledCount: 1},
	}).Error)
	identity := model.ChannelMonitorDailyMetricIdentity{ChannelID: channel, UserID: user, APIKeyID: apiKey, APIKeyKey: "request-fingerprint-one", Model: modelName}
	first := identity.LedgerRow(day)
	first.ActualSuccessCount, first.ActualFailureCount = 3, 1
	first.CacheReadTokens, first.InputTokens, first.CacheWriteCount = 30, 100, 1
	identity.APIKeyKey = "request-fingerprint-two"
	second := identity.LedgerRow(day)
	second.ActualSuccessCount, second.CacheReadTokens, second.InputTokens = 1, 10, 100
	identity.ChannelID, identity.UserID, identity.APIKeyID = channel+2, 0, 0
	identity.Model = "failed-only-model"
	failure := identity.LedgerRow(day)
	failure.ActualFailureCount = 2
	require.NoError(t, db.Create(&[]model.ChannelMonitorDailySuccessLedger{first, second, failure}).Error)
	query := channelMonitorAnalyticsQuery{Metric: "overview", GroupBy: "channel", From: day, To: day + 86400, Sort: "samples", Direction: "desc", Page: 1, PageSize: 1}
	ctx := context.Background()
	response, err := queryChannelMonitorHistoricalAnalytics(ctx, query)
	require.NoError(t, err)
	require.Len(t, response.Items, 1)
	assert.EqualValues(t, 3, response.Total)
	assert.Equal(t, channel, response.Items[0]["channel_id"])
	assert.EqualValues(t, 150, response.Items[0]["cost_nano_cny"])
	assert.EqualValues(t, 5, response.Items[0]["actual_sample_count"])
	assert.Equal(t, 0.8, response.Items[0]["actual_success_rate"])
	assert.Equal(t, 0.2, response.Items[0]["cache_utilization_rate"])
	assert.EqualValues(t, 170, response.ScopeSummary["cost_nano_cny"])
	assert.EqualValues(t, 7, response.ScopeSummary["actual_sample_count"])
	assert.Equal(t, float64(4)/7, response.ScopeSummary["actual_success_rate"])
	query.Page = 2
	pageTwo, err := queryChannelMonitorHistoricalAnalytics(ctx, query)
	require.NoError(t, err)
	assert.Equal(t, response.ScopeSummary, pageTwo.ScopeSummary)
	require.Len(t, pageTwo.Items, 1)
	assert.Equal(t, channel+2, pageTwo.Items[0]["channel_id"], "failed-only requests remain visible")
	assert.EqualValues(t, 0, pageTwo.Items[0]["cost_nano_cny"])
	query.Page = 3
	pageThree, err := queryChannelMonitorHistoricalAnalytics(ctx, query)
	require.NoError(t, err)
	require.Len(t, pageThree.Items, 1)
	assert.Equal(t, channel+1, pageThree.Items[0]["channel_id"], "probe-only costs remain visible")
	assert.EqualValues(t, 0, pageThree.Items[0]["actual_sample_count"])

	query.Page, query.PageSize, query.OnlyLoss = 1, 20, true
	losses, err := queryChannelMonitorHistoricalAnalytics(ctx, query)
	require.NoError(t, err)
	assert.EqualValues(t, 2, losses.Total)
	assert.Equal(t, response.ScopeSummary, losses.ScopeSummary)
	assert.EqualValues(t, 5, losses.Items[0]["actual_sample_count"])
	query.OnlyLoss, query.Channel, query.GroupBy = false, channel, "model"
	models, err := queryChannelMonitorHistoricalAnalytics(ctx, query)
	require.NoError(t, err)
	require.Len(t, models.Items, 1, "different model hash formats must join")
	assert.Equal(t, modelKey, models.Items[0]["model_key"])
	assert.EqualValues(t, 5, models.Items[0]["actual_sample_count"])
	query.Model, query.ModelKey, query.GroupBy = modelName, &modelKey, "user"
	users, err := queryChannelMonitorHistoricalAnalytics(ctx, query)
	require.NoError(t, err)
	require.Len(t, users.Items, 1)
	assert.EqualValues(t, 5, users.Items[0]["actual_sample_count"])
	query.User, query.UserSet, query.GroupBy = user, true, "api_key"
	keys, err := queryChannelMonitorHistoricalAnalytics(ctx, query)
	require.NoError(t, err)
	require.Len(t, keys.Items, 1, "inbound API key fingerprints must join by ID")
	assert.EqualValues(t, 150, keys.Items[0]["cost_nano_cny"])
	assert.EqualValues(t, 5, keys.Items[0]["actual_sample_count"])
	assert.Equal(t, 0.2, keys.Items[0]["cache_utilization_rate"])
	query.APIKey, query.APIKeySet, query.GroupBy = apiKey, true, "model"
	keyModels, err := queryChannelMonitorHistoricalAnalytics(ctx, query)
	require.NoError(t, err)
	require.Len(t, keyModels.Items, 1)
	assert.EqualValues(t, 5, keyModels.Items[0]["actual_sample_count"])
	assert.EqualValues(t, 150, keyModels.Items[0]["cost_nano_cny"])

	params := url.Values{"metric": {"overview"}, "group_by": {"channel"}, "from": {time.Unix(day, 0).In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02")}, "to": {time.Unix(day+86400, 0).In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02")}, "only_loss": {"true"}, "page_size": {"1"}}
	httpResponse := requestChannelMonitorAnalytics(t, params)
	require.Len(t, httpResponse.Items, 1)
	assert.EqualValues(t, channel, httpResponse.Items[0]["channel_id"])
	assert.EqualValues(t, 5, httpResponse.Items[0]["actual_sample_count"])

	query.Channel, query.UserSet, query.APIKey, query.APIKeySet, query.ModelKey, query.Model, query.GroupBy = 0, false, 0, false, nil, "", "channel"
	require.NoError(t, db.Where("day_start = ?", day).Delete(&model.ChannelMonitorDailyCheckpoint{}).Error)
	partial, err := queryChannelMonitorHistoricalAnalytics(ctx, query)
	require.NoError(t, err)
	require.NotNil(t, partial.RequestCoverage)
	assert.NotEqual(t, "complete", partial.RequestCoverage.Status)
	for _, item := range partial.Items {
		if item["channel_id"] == channel+1 {
			assert.Equal(t, false, item["request_metrics_available"], "missing request coverage must not display a false zero")
		}
	}
	require.NoError(t, db.Model(&model.ChannelMonitorIncomeState{}).Where("id = 1").Update("started_at", day+3600).Error)
	partialFinance, err := queryChannelMonitorHistoricalAnalytics(ctx, query)
	require.NoError(t, err)
	for _, item := range partialFinance.Items {
		if item["channel_id"] == channel+2 {
			assert.NotContains(t, item, "cost_nano_cny", "request-only rows cannot invent zero cost in incomplete finance history")
			assert.NotContains(t, item, "profit_nano_cny")
			assert.EqualValues(t, 2, item["actual_failure_count"])
		}
	}
}
