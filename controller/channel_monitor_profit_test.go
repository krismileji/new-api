package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		&model.ChannelMonitorIncome{}, &model.ChannelMonitorIncomeState{}, &model.ChannelMonitorIncomeGap{},
		&model.ChannelMonitorDailyCostDetail{}, &model.ChannelDailyCostOutbox{},
	))

	day := model.ChannelDailyCostDayStart(common.GetTimestamp()) - 2*24*60*60
	runChannelMonitorProfitAnalyticsCases(t, db, day)
	runChannelMonitorProfitCoverageCases(t, db, day+3*86400)
}

func runChannelMonitorProfitCoverageCases(t *testing.T, db *gorm.DB, day int64) {
	t.Helper()
	journalDir := t.TempDir()
	t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", journalDir)
	ctx := context.Background()
	const first, second = 900241, 900242
	for _, table := range []any{&model.ChannelMonitorIncome{}, &model.ChannelDailyCost{}, &model.ChannelDailyCostOutbox{}, &model.ChannelMonitorIncomeGap{}} {
		require.NoError(t, db.Where("channel_id IN ?", []int{first, second}).Delete(table).Error)
	}
	require.NoError(t, db.Model(&model.ChannelMonitorIncomeState{}).Where("id = 1").Updates(map[string]any{"started_at": day + 3600, "retained_from": 0, "gap_since": 0}).Error)
	for i := range 3 {
		for _, channel := range []int{first, second} {
			at := day + int64(i)*86400
			require.NoError(t, db.Create(&model.ChannelDailyCost{ChannelId: channel, DayStart: at, CostNanoCNY: 30, SettledCount: 1}).Error)
			require.NoError(t, db.Create(&model.ChannelMonitorIncome{SettlementKey: model.ChannelMonitorIncomeKey(fmt.Sprintf("%d:%d", at, channel), "coverage"), DayStart: at, ChannelID: channel, UserID: 1, BillingSource: "wallet", QuotaPerUnit: "1", USDToCNY: "1", Quota: 20, IncomeNanoCNY: 20, Status: "settled", CostRecorded: 1}).Error)
		}
	}
	query := channelMonitorAnalyticsQuery{Metric: "profit", GroupBy: "day", From: day, To: day + 3*86400, Channel: first, Sort: "profit", Direction: "desc", Page: 1, PageSize: 20}
	response, err := queryChannelMonitorProfitAnalytics(ctx, query)
	require.NoError(t, err)
	require.Len(t, response.Items, 3)
	for _, item := range response.Items {
		assert.Equal(t, item["day_start"] != day, item["profit_confirmed"])
	}
	assert.Equal(t, false, response.Summary["profit_confirmed"])
	query.OnlyLoss = true
	response, err = queryChannelMonitorProfitAnalytics(ctx, query)
	require.NoError(t, err)
	assert.EqualValues(t, 2, response.Total, "完整日期的亏损不能被启用日遮蔽")
	require.NoError(t, db.Model(&model.ChannelMonitorIncomeState{}).Where("id = 1").Update("started_at", day).Error)
	gap := model.ChannelMonitorIncomeGap{GapKey: "profit-scoped-gap", ChannelID: first, From: day + 86400, To: day + 2*86400}
	require.NoError(t, db.Create(&gap).Error)
	response, err = queryChannelMonitorProfitAnalytics(ctx, query)
	require.NoError(t, err)
	assert.EqualValues(t, 2, response.Total)
	query.GroupBy, query.Channel = "channel", 0
	response, err = queryChannelMonitorProfitAnalytics(ctx, query)
	require.NoError(t, err)
	require.Len(t, response.Items, 1)
	assert.Equal(t, second, response.Items[0]["channel_id"], "其他渠道仍可确认")
	query.Channel = second
	response, err = queryChannelMonitorProfitAnalytics(ctx, query)
	require.NoError(t, err)
	assert.Equal(t, true, response.Summary["profit_confirmed"])
	require.NoError(t, db.Delete(&gap).Error)
	// A second node reads the shared journal even when no DB marker or local
	// in-memory marker exists. Only the affected day/channel loses coverage.
	journalPath := filepath.Join(journalDir, fmt.Sprintf("%d_%d.gap", day+86400, first))
	require.NoError(t, os.WriteFile(journalPath, nil, 0600))
	query.GroupBy, query.Channel = "day", first
	response, err = queryChannelMonitorProfitAnalytics(ctx, query)
	require.NoError(t, err)
	assert.EqualValues(t, 2, response.Total)
	assert.Contains(t, response.Coverage.Reasons, "income_recording_gap")
	query.Channel = second
	response, err = queryChannelMonitorProfitAnalytics(ctx, query)
	require.NoError(t, err)
	assert.True(t, response.Summary["profit_confirmed"].(bool))
	require.NoError(t, os.Remove(journalPath))
	t.Run("cross_day_income_gap", func(t *testing.T) {
		t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", t.TempDir())
		wasReady := model.ChannelMonitorIncomeReady.Swap(true)
		t.Cleanup(func() { model.ChannelMonitorIncomeReady.Store(wasReady) })
		const eventID = "profit-cross-day-gap"
		model.MarkChannelMonitorIncomeGapForCost(ctx, first, day, eventID)
		event := model.ChannelDailyCostOutbox{EventId: eventID, ChannelId: first, OccurredAt: day + 86400, ProcessedAt: day + 86401}
		require.NoError(t, db.Create(&event).Error)
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			return model.ConfirmChannelMonitorCostIncome(tx, []model.ChannelDailyCostOutbox{event})
		}))
		gapQuery := query
		gapQuery.Channel, gapQuery.From, gapQuery.To, gapQuery.OnlyLoss = first, day+86400, day+2*86400, false
		response, err := queryChannelMonitorProfitAnalytics(ctx, gapQuery)
		require.NoError(t, err)
		assert.False(t, response.Summary["profit_confirmed"].(bool), "the end day must retain the missing income gap")
		assert.Contains(t, response.Coverage.Reasons, "income_recording_gap")
		gapQuery.Channel = second
		response, err = queryChannelMonitorProfitAnalytics(ctx, gapQuery)
		require.NoError(t, err)
		assert.True(t, response.Summary["profit_confirmed"].(bool))
		gapQuery.Channel, gapQuery.From, gapQuery.To = first, day+2*86400, day+3*86400
		response, err = queryChannelMonitorProfitAnalytics(ctx, gapQuery)
		require.NoError(t, err)
		assert.True(t, response.Summary["profit_confirmed"].(bool), "later complete days remain usable")
		require.NoError(t, db.Where("gap_key = ?", model.ChannelMonitorIncomeKey(eventID, "gap")).Delete(&model.ChannelMonitorIncomeGap{}).Error)
		require.NoError(t, db.Delete(&event).Error)
	})
	t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", filepath.Join(journalDir, "unavailable"))
	response, err = queryChannelMonitorProfitAnalytics(ctx, query)
	require.NoError(t, err)
	assert.False(t, response.Summary["profit_confirmed"].(bool))
	assert.Zero(t, response.Total)
	t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", journalDir)
	outbox := model.ChannelDailyCostOutbox{EventId: "profit-scope-outbox", ChannelId: first, OccurredAt: day + 86400}
	require.NoError(t, db.Create(&outbox).Error)
	query.GroupBy, query.Channel = "day", first
	response, err = queryChannelMonitorProfitAnalytics(ctx, query)
	require.NoError(t, err)
	assert.EqualValues(t, 2, response.Total, "待入账事件只影响其归属日期")
	require.NoError(t, db.Delete(&outbox).Error)
	query.GroupBy, query.Channel = "channel", second
	if common.RedisEnabled && common.RDB != nil {
		for _, stream := range []string{service.ChannelDailyCostRedisStream, service.ChannelDailyCostRedisDeadLetter} {
			payload, err := common.Marshal(map[string]any{"channel_id": first, "occurred_at": day + 86400})
			require.NoError(t, err)
			require.NoError(t, common.RDB.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{"payload": string(payload)}}).Err())
			response, err = queryChannelMonitorProfitAnalytics(ctx, query)
			require.NoError(t, err)
			assert.Equal(t, true, response.Summary["profit_confirmed"], "其他渠道的队列不能阻塞此渠道")
			query.Channel, query.To = first, day+86400
			response, err = queryChannelMonitorProfitAnalytics(ctx, query)
			require.NoError(t, err)
			assert.Equal(t, true, response.Summary["profit_confirmed"], "之后日期的队列不能阻塞历史")
			query.GroupBy, query.To = "day", day+3*86400
			response, err = queryChannelMonitorProfitAnalytics(ctx, query)
			require.NoError(t, err)
			assert.EqualValues(t, 2, response.Total, "队列中的归属日期仍须待确认")
			assert.Equal(t, false, response.Summary["profit_confirmed"])
			query.GroupBy = "channel"
			query.Channel, query.To = second, day+3*86400
			require.NoError(t, common.RDB.Del(ctx, stream).Err())
		}
		// A trimmed message can remain pending without a payload identifying
		// its channel/day. It must not silently disappear from coverage.
		require.NoError(t, common.RDB.XGroupCreateMkStream(ctx, service.ChannelDailyCostRedisStream, service.ChannelDailyCostRedisConsumerGroup, "0").Err())
		id, err := common.RDB.XAdd(ctx, &redis.XAddArgs{Stream: service.ChannelDailyCostRedisStream, Values: map[string]any{"payload": "{}"}}).Result()
		require.NoError(t, err)
		require.NoError(t, common.RDB.XReadGroup(ctx, &redis.XReadGroupArgs{Group: service.ChannelDailyCostRedisConsumerGroup, Consumer: "profit-test", Streams: []string{service.ChannelDailyCostRedisStream, ">"}, Count: 1}).Err())
		require.NoError(t, common.RDB.XDel(ctx, service.ChannelDailyCostRedisStream, id).Err())
		response, err = queryChannelMonitorProfitAnalytics(ctx, query)
		require.NoError(t, err)
		assert.Equal(t, false, response.Summary["profit_confirmed"])
		assert.Contains(t, response.Coverage.Reasons, "cost_projection_pending")
		require.NoError(t, common.RDB.Del(ctx, service.ChannelDailyCostRedisStream).Err())
	}
	t.Run("retained_dates_preserve_independent_daily_coverage", func(t *testing.T) {
		retainedFrom := day + 86400
		require.NoError(t, db.Model(&model.ChannelMonitorIncomeState{}).Where("id = 1").Update("retained_from", retainedFrom).Error)
		require.NoError(t, db.Model(&model.ChannelMonitorIncome{}).Where("channel_id = ? AND day_start = ?", second, day+2*86400).Update("status", "pending").Error)
		t.Cleanup(func() {
			assert.NoError(t, db.Model(&model.ChannelMonitorIncomeState{}).Where("id = 1").Update("retained_from", 0).Error)
			assert.NoError(t, db.Model(&model.ChannelMonitorIncome{}).Where("channel_id = ? AND day_start = ?", second, day+2*86400).Update("status", "settled").Error)
		})
		dailyQuery := query
		dailyQuery.GroupBy, dailyQuery.Channel, dailyQuery.OnlyLoss = "day", second, false
		response, err := queryChannelMonitorProfitAnalytics(ctx, dailyQuery)
		require.NoError(t, err)
		assert.Equal(t, retainedFrom, response.Coverage.CoveredFrom, "coverage must begin at the reporting retention boundary")
		assert.Contains(t, response.Coverage.Reasons, "profit_history_expired")
		assert.Contains(t, response.Coverage.Reasons, "income_settlement_pending")
		require.Len(t, response.Items, 3)
		for _, item := range response.Items {
			assert.Equal(t, item["day_start"] == retainedFrom, item["profit_confirmed"])
		}
	})
}

func runChannelMonitorProfitAnalyticsCases(t *testing.T, db *gorm.DB, day int64) {
	t.Helper()
	t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", t.TempDir())
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
			QuotaPerUnit: "1", USDToCNY: "1",
			IncomeNanoCNY: 100_000_000_000, Status: "settled", CostEventID: "cost-one", CostRecorded: 1,
		},
		{
			SettlementKey: settlementKeys[1], DayStart: day, ChannelID: secondChannel,
			UserID: 102, APIKeyID: 202, APIKeyKey: "key-two", APIKeyName: "Key Two",
			ModelKey: modelKey, ModelName: "gpt-4.1", BillingSource: "subscription", Quota: 10,
			QuotaPerUnit: "1", USDToCNY: "1",
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
