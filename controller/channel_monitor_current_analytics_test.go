package controller

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelMonitorCurrentAndMixedCostAnalyticsExcludeDatabaseToday(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	ctx := context.Background()
	now := common.GetTimestamp()
	day := model.ChannelDailyCostDayStart(now)
	require.NoError(t, model.AddChannelDailyCost(ctx, 7, day-1, 200, 1, 0))
	require.NoError(t, model.AddChannelDailyCost(ctx, 8, day-1, 50, 1, 0))
	require.NoError(t, model.AddChannelDailyCost(ctx, 7, now, 999, 1, 0))
	require.NoError(t, service.RebuildChannelMonitorRedisDailyCosts(ctx, now))
	require.NoError(t, common.RDB.HSet(ctx, service.ChannelMonitorRedisCostDayKey(day), map[string]any{
		"global:settled_cost_nano_cny": 300, "channel:7:settled_cost_nano_cny": 300,
	}).Err())
	query := channelMonitorAnalyticsQuery{Metric: "cost", GroupBy: "channel", From: day, To: day + 86400,
		Sort: "cost", Direction: "desc", Page: 1, PageSize: 20}
	current, err := queryChannelMonitorHistoricalAnalytics(ctx, query)
	require.NoError(t, err)
	assert.Equal(t, "redis_daily", current.Source)
	assert.Equal(t, int64(300), current.Summary["cost_nano_cny"])
	require.Len(t, current.Items, 1)
	assert.Equal(t, int64(300), current.Items[0]["cost_nano_cny"])
	query.From -= 86400
	mixed, err := queryChannelMonitorHistoricalAnalytics(ctx, query)
	require.NoError(t, err)
	assert.Equal(t, "redis_and_database_daily", mixed.Source)
	assert.Equal(t, int64(550), mixed.Summary["cost_nano_cny"])
	require.Len(t, mixed.Items, 2)
	assert.Equal(t, int64(500), mixed.Items[0]["cost_nano_cny"])
	query.From, query.GroupBy = day, "user"
	unknown, err := queryChannelMonitorHistoricalAnalytics(ctx, query)
	require.NoError(t, err)
	require.Len(t, unknown.Items, 1)
	assert.Equal(t, int64(300), unknown.Summary["cost_nano_cny"], "unattributed legacy costs still belong in the drill-down total")
	assert.Equal(t, 0, unknown.Items[0]["user_id"])
	assert.Contains(t, unknown.Coverage.Reasons, "cost_attribution_incomplete")
}

func TestChannelMonitorCurrentSuccessFiltersFactsBeforeGrouping(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	now := common.GetTimestamp()
	for index, channel := range []int{7, 8, 7} {
		event := model.NewChannelMonitorEvent(channel, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, now)
		event.UserId = 9
		event.UserAttribution = model.ChannelMonitorEventUserAttributionRequest
		event.APIKeyId = 11
		event.APIKeyName = "key-11"
		event.ModelName = "model-a"
		event.GroupName = "vip"
		if index == 2 {
			event.UserId = 10
		}
		event.RequestDispatched = true
		event.IsFinalAttempt = true
		emitChannelMonitorControllerRealtimeEvents(t, event)
	}
	result, err := queryChannelMonitorHistoricalAnalytics(context.Background(), channelMonitorAnalyticsQuery{
		Metric: "success", GroupBy: "channel", From: model.ChannelDailyCostDayStart(now),
		To: model.ChannelDailyCostDayStart(now) + 86400, User: 9, Sort: "samples", Direction: "desc", Page: 1, PageSize: 20,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), result.Summary["actual_sample_count"])
	require.Len(t, result.Items, 2)
	for _, item := range result.Items {
		assert.Equal(t, int64(1), item["actual_sample_count"])
	}
}

func TestChannelMonitorScopedPendingCostCoverage(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	runChannelMonitorScopedPendingCostCoverageCases(t, db)
}

func runChannelMonitorScopedPendingCostCoverageCases(t *testing.T, db *gorm.DB) {
	t.Helper()
	t.Setenv("CHANNEL_MONITOR_INCOME_GAP_DIR", t.TempDir())
	t.Setenv("CHANNEL_DAILY_COST_RELIABLE_OUTBOX", "true")
	addr := os.Getenv("TEST_COST_PROJECTION_REDIS_ADDR")
	if addr == "" {
		addr = miniredis.RunT(t).Addr()
	} else {
		require.Equal(t, "127.0.0.1:26382", addr)
	}
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 8})
	ctx := context.Background()
	size, err := client.DBSize(ctx).Result()
	require.NoError(t, err)
	require.Zero(t, size, "使用独立空 Redis 数据库")
	previousRedis := common.RedisEnabled
	previousClients := []*redis.Client{common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer}
	common.RedisEnabled = true
	common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer = client, client, client, client
	t.Cleanup(func() {
		common.RedisEnabled = previousRedis
		common.RDB, common.RDBMonitorWrite, common.RDBMonitorRead, common.RDBMonitorConsumer = previousClients[0], previousClients[1], previousClients[2], previousClients[3]
		assert.NoError(t, client.FlushDB(ctx).Err())
		assert.NoError(t, client.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.ChannelDailyCost{}, &model.ChannelDailyAPIKeyCost{}, &model.ChannelDailyCostOutbox{},
		&model.ChannelMonitorDailyCostDetail{}, &model.ChannelMonitorIncome{}, &model.ChannelMonitorIncomeState{}, &model.ChannelMonitorIncomeGap{}))
	now := common.GetTimestamp()
	day := model.ChannelDailyCostDayStart(now)
	const channel, user, key = 902701, 902711, 902721
	const modelName = "claude-opus-4-8"
	modelKey := model.ChannelMonitorDailyCostModelKey(modelName)
	fingerprint := strings.Repeat("a", 64)
	seed := model.ChannelDailyCostDelta{ChannelId: channel, UserId: user, UserAttribution: "request", APIKeyId: key,
		APIKeyName: "模型成本 Key", KeyFingerprint: fingerprint, ModelName: modelName, SourceKind: "business", OccurredAt: now, CostNanoCNY: 100, SettledDelta: 1}
	require.NoError(t, model.AddChannelDailyCostBatch(ctx, []model.ChannelDailyCostDelta{seed}))
	require.NoError(t, db.Model(&model.ChannelDailyCostOutbox{}).Where("channel_id = ?", channel).Update("redis_projected_at", now).Error)
	require.NoError(t, db.Where("id = ?", 1).Assign(model.ChannelMonitorIncomeState{StartedAt: day, RetainedFrom: 0, GapSince: 0}).FirstOrCreate(&model.ChannelMonitorIncomeState{ID: 1}).Error)
	require.NoError(t, db.Create(&model.ChannelMonitorIncome{SettlementKey: "scoped-cost-income", DayStart: day, ChannelID: channel,
		UserID: user, APIKeyID: key, APIKeyName: seed.APIKeyName, ModelKey: modelKey, ModelName: modelName,
		BillingSource: "wallet", QuotaPerUnit: "1", USDToCNY: "1", IncomeNanoCNY: 300, Status: "settled", CostRecorded: 1}).Error)
	require.NoError(t, service.RebuildChannelMonitorRedisDailyCosts(ctx, now))
	status, err := common.Marshal(service.ChannelMonitorReliableCostStatus{CheckedAt: now, Pending: true})
	require.NoError(t, err)
	require.NoError(t, client.Set(ctx, "channel_cost:v1:projection:status", status, 0).Err())
	base := channelMonitorAnalyticsQuery{Metric: "cost", GroupBy: "user", From: day, To: day + 86400,
		Channel: channel, Model: modelName, ModelKey: &modelKey, Page: 1, PageSize: 20}
	cases := []struct {
		name  string
		query channelMonitorAnalyticsQuery
		event model.ChannelDailyCostDelta
		want  bool
	}{
		{name: "matching_model", query: base, event: seed, want: true},
		{name: "other_model", query: base, event: seed},
		{name: "other_channel", query: base, event: seed},
		{name: "previous_day", query: base, event: seed},
		{name: "exclusive_end", query: base, event: seed},
		{name: "explicit_unknown_user", query: base, event: seed},
		{name: "matching_unknown_user", query: base, event: seed, want: true},
		{name: "other_key", query: base, event: seed},
		{name: "other_fingerprint", query: base, event: seed},
		{name: "search_excludes_cost", query: base, event: seed},
		{name: "legacy_smart_probe", query: base, event: seed, want: true},
		{name: "real_key_named_as_probe", query: base, event: seed},
		{name: "normalized_model", query: base, event: seed, want: true},
	}
	unknownFingerprint := strings.Repeat("b", 64)
	smartProbe := "smart_probe"
	for index := range cases {
		tc := &cases[index]
		switch tc.name {
		case "other_model":
			tc.event.ModelName = "gpt-4.1"
		case "other_channel":
			tc.event.ChannelId++
		case "previous_day":
			tc.event.OccurredAt = day - 1
		case "exclusive_end":
			tc.event.OccurredAt = day + 86400
		case "explicit_unknown_user", "matching_unknown_user":
			tc.query.User, tc.query.UserSet = 0, true
			if tc.want {
				tc.event.UserId = 0
			}
		case "other_key":
			tc.query.APIKey = key + 1
		case "other_fingerprint":
			tc.query.APIKeyKey = &unknownFingerprint
		case "search_excludes_cost":
			tc.query.Search = "different-model"
		case "legacy_smart_probe", "real_key_named_as_probe":
			tc.query.APIKeyKey = &smartProbe
			tc.event.APIKeyName = "智能调度探测"
			if tc.want {
				tc.event.APIKeyId = 0
			}
		case "normalized_model":
			tc.event.ModelName = "gemini-2.5-flash-thinking-8192"
			tc.query.Model = "gemini-2.5-flash-thinking-*"
			tc.query.ModelKey = nil
		}
	}
	for _, source := range []string{"stream", "database"} {
		for _, tc := range cases {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				query := tc.query
				if source == "stream" {
					payload, err := common.Marshal(map[string]any{"channel_id": tc.event.ChannelId, "occurred_at": tc.event.OccurredAt,
						"user_id": tc.event.UserId, "api_key_id": tc.event.APIKeyId, "api_key_name": tc.event.APIKeyName,
						"key_fingerprint": tc.event.KeyFingerprint, "model_name": tc.event.ModelName, "source_kind": tc.event.SourceKind})
					require.NoError(t, err)
					id, err := client.XAdd(ctx, &redis.XAddArgs{Stream: service.ChannelDailyCostRedisStream, Values: map[string]any{"payload": string(payload)}}).Result()
					require.NoError(t, err)
					defer func() { assert.NoError(t, client.XDel(ctx, service.ChannelDailyCostRedisStream, id).Err()) }()
				} else {
					event := tc.event
					event.EventId = "scoped-cost-" + tc.name
					require.NoError(t, model.StoreChannelDailyCostOutboxEvents(ctx, []model.ChannelDailyCostDelta{event}))
					defer func() {
						assert.NoError(t, db.Where("event_id = ?", event.EventId).Delete(&model.ChannelDailyCostOutbox{}).Error)
					}()
				}
				response, err := queryChannelMonitorCurrentCostAnalytics(ctx, query)
				require.NoError(t, err)
				assert.Equal(t, tc.want, slices.Contains(response.Coverage.Reasons, "cost_projection_pending"), response.Coverage.Reasons)
				assert.NotContains(t, response.Coverage.Reasons, "cost_projection_unavailable")
				if tc.name == "normalized_model" {
					return // Historical profits retain the original model names.
				}
				query.Metric = "profit"
				profit, err := queryChannelMonitorProfitAnalytics(ctx, query)
				require.NoError(t, err)
				assert.Equal(t, tc.want, slices.Contains(profit.Coverage.Reasons, "cost_projection_pending"), profit.Coverage.Reasons)
			})
		}
	}
	complete, err := queryChannelMonitorCurrentCostAnalytics(ctx, base)
	require.NoError(t, err)
	assert.Equal(t, service.ChannelMonitorCoverageComplete, complete.Coverage.Status, "a stale global pending flag cannot keep a completed model partial")
	assert.Equal(t, int64(100), complete.Summary["cost_nano_cny"])
	t.Run("projected_cost_waiting_for_ledger", func(t *testing.T) {
		event := seed
		event.EventId = "scoped-cost-ledger-pending"
		require.NoError(t, model.StoreChannelDailyCostOutboxEvents(ctx, []model.ChannelDailyCostDelta{event}))
		require.NoError(t, service.RebuildChannelMonitorRedisDailyCosts(ctx, now))
		require.NoError(t, db.Model(&model.ChannelDailyCostOutbox{}).Where("event_id = ?", event.EventId).Update("redis_projected_at", now).Error)
		defer func() {
			assert.NoError(t, db.Where("event_id = ?", event.EventId).Delete(&model.ChannelDailyCostOutbox{}).Error)
			assert.NoError(t, service.RebuildChannelMonitorRedisDailyCosts(ctx, now))
		}()
		cost, err := queryChannelMonitorCurrentCostAnalytics(ctx, base)
		require.NoError(t, err)
		assert.Equal(t, service.ChannelMonitorCoverageComplete, cost.Coverage.Status)
		assert.Equal(t, int64(200), cost.Summary["cost_nano_cny"])
		query := base
		query.Metric = "profit"
		profit, err := queryChannelMonitorProfitAnalytics(ctx, query)
		require.NoError(t, err)
		assert.Contains(t, profit.Coverage.Reasons, "cost_projection_pending")
	})
	t.Run("unattributed_queue_entry", func(t *testing.T) {
		id, err := client.XAdd(ctx, &redis.XAddArgs{Stream: service.ChannelDailyCostRedisStream, Values: map[string]any{"payload": "{}"}}).Result()
		require.NoError(t, err)
		defer func() { assert.NoError(t, client.XDel(ctx, service.ChannelDailyCostRedisStream, id).Err()) }()
		cost, err := queryChannelMonitorCurrentCostAnalytics(ctx, base)
		require.NoError(t, err)
		assert.Contains(t, cost.Coverage.Reasons, "cost_projection_pending")
	})
	t.Run("pending_message_without_payload", func(t *testing.T) {
		require.NoError(t, client.XGroupCreateMkStream(ctx, service.ChannelDailyCostRedisStream, service.ChannelDailyCostRedisConsumerGroup, "0").Err())
		id, err := client.XAdd(ctx, &redis.XAddArgs{Stream: service.ChannelDailyCostRedisStream, Values: map[string]any{"payload": "{}"}}).Result()
		require.NoError(t, err)
		require.NoError(t, client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: service.ChannelDailyCostRedisConsumerGroup, Consumer: "cost-coverage",
			Streams: []string{service.ChannelDailyCostRedisStream, ">"}, Count: 1}).Err())
		require.NoError(t, client.XDel(ctx, service.ChannelDailyCostRedisStream, id).Err())
		defer func() { assert.NoError(t, client.Del(ctx, service.ChannelDailyCostRedisStream).Err()) }()
		cost, err := queryChannelMonitorCurrentCostAnalytics(ctx, base)
		require.NoError(t, err)
		assert.Contains(t, cost.Coverage.Reasons, "cost_projection_pending")
	})
	t.Run("queue_read_failure", func(t *testing.T) {
		require.NoError(t, client.Set(ctx, service.ChannelDailyCostRedisStream, "invalid-stream-type", 0).Err())
		defer func() { assert.NoError(t, client.Del(ctx, service.ChannelDailyCostRedisStream).Err()) }()
		cost, err := queryChannelMonitorCurrentCostAnalytics(ctx, base)
		require.NoError(t, err)
		assert.Contains(t, cost.Coverage.Reasons, "cost_projection_unavailable")
		assert.Equal(t, int64(100), cost.Summary["cost_nano_cny"])
	})
	t.Run("projection_heartbeat_unavailable", func(t *testing.T) {
		status, err := common.Marshal(service.ChannelMonitorReliableCostStatus{CheckedAt: now, Failed: true})
		require.NoError(t, err)
		require.NoError(t, client.Set(ctx, "channel_cost:v1:projection:status", status, 0).Err())
		cost, err := queryChannelMonitorCurrentCostAnalytics(ctx, base)
		require.NoError(t, err)
		assert.Contains(t, cost.Coverage.Reasons, "cost_projection_unavailable")
	})
}
