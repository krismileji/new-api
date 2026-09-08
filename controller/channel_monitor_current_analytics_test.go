package controller

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
