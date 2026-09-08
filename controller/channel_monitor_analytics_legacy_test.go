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

func TestChannelMonitorAnalyticsLegacyUserScopesMatchCurrentModelIdentity(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	day := model.ChannelDailyCostDayStart(common.GetTimestamp())
	rows := []service.ChannelMonitorRedisDailySuccessAnalyticsRow{
		{ChannelID: 7, UserID: 31, APIKeyID: 201, APIKeyName: "生产 Key", ModelName: "model-a", Aggregate: service.ChannelMonitorRedisSharedAggregate{ActualSuccessCount: 2}},
		{ChannelID: 8, UserID: 31, APIKeyID: 201, APIKeyName: "生产 Key", ModelName: "model-a", Aggregate: service.ChannelMonitorRedisSharedAggregate{ActualSuccessCount: 3}},
		{ChannelID: 7, UserID: 32, APIKeyID: 201, APIKeyName: "生产 Key", ModelName: "model-a", Aggregate: service.ChannelMonitorRedisSharedAggregate{ActualSuccessCount: 4}},
	}
	query := channelMonitorAnalyticsQuery{Metric: "success", GroupBy: "model", User: 31, APIKey: 201, From: day, To: day + 86400, Sort: "samples", Direction: "desc", Page: 1, PageSize: 20}
	legacyView := service.ChannelMonitorRedisDailySuccessAnalyticsView{DayStart: day, Rows: rows}
	legacy, err := queryChannelMonitorLegacySuccessAnalytics(context.Background(), query, legacyView)
	require.NoError(t, err)
	require.Len(t, legacy.Items, 1, "one key using one model through two channels is one model row")
	assert.Equal(t, int64(5), legacy.Summary["actual_sample_count"])
	currentView := service.ChannelMonitorRedisDailySuccessAnalyticsView{DayStart: day, Revision: 1}
	for _, row := range rows {
		identity := model.ChannelMonitorDailyMetricIdentity{ChannelID: row.ChannelID, UserID: row.UserID, APIKeyID: row.APIKeyID, Model: row.ModelName}
		row.ModelKey = identity.LedgerRow(day).ModelKey
		currentView.Facts = append(currentView.Facts, row)
	}
	current, err := queryChannelMonitorCurrentSuccessFacts(context.Background(), query, currentView)
	require.NoError(t, err)
	require.Len(t, current.Items, 1)
	assert.Equal(t, current.Items[0]["model_key"], legacy.Items[0]["model_key"], "historical and current scopes must merge into the same model")
	modelKey, ok := current.Items[0]["model_key"].(string)
	require.True(t, ok)
	query.GroupBy, query.ModelKey = "channel", &modelKey
	channels, err := queryChannelMonitorLegacySuccessAnalytics(context.Background(), query, legacyView)
	require.NoError(t, err)
	assert.Len(t, channels.Items, 2)
	assert.Equal(t, int64(5), channels.Summary["actual_sample_count"])
}

func TestChannelMonitorAnalyticsLegacyUnknownOwnerDoesNotReuseGlobalRollups(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	day := model.ChannelDailyCostDayStart(common.GetTimestamp())
	response, err := queryChannelMonitorLegacySuccessAnalytics(context.Background(), channelMonitorAnalyticsQuery{
		Metric: "success", GroupBy: "api_key", UserSet: true, User: 0,
		From: day, To: day + 86400, Page: 1, PageSize: 20,
	}, service.ChannelMonitorRedisDailySuccessAnalyticsView{DayStart: day, Rows: []service.ChannelMonitorRedisDailySuccessAnalyticsRow{
		{APIKeyID: 201, Aggregate: service.ChannelMonitorRedisSharedAggregate{ActualSuccessCount: 9}},
	}})
	require.NoError(t, err)
	assert.Empty(t, response.Items)
	assert.Equal(t, int64(0), response.Summary["actual_sample_count"])
	assert.Equal(t, service.ChannelMonitorCoveragePartial, response.Coverage.Status)
	assert.Contains(t, response.Coverage.Reasons, "daily_legacy_attribution_incomplete")
}

func TestChannelMonitorAnalyticsLegacyCompositeScopeDoesNotDoubleCountUserRollups(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	day := model.ChannelDailyCostDayStart(common.GetTimestamp())
	response, err := queryChannelMonitorLegacySuccessAnalytics(context.Background(), channelMonitorAnalyticsQuery{
		Metric: "success", GroupBy: "api_key_channel_model", From: day, To: day + 86400, Page: 1, PageSize: 20,
	}, service.ChannelMonitorRedisDailySuccessAnalyticsView{DayStart: day, Rows: []service.ChannelMonitorRedisDailySuccessAnalyticsRow{
		{ChannelID: 7, APIKeyID: 201, ModelName: "model-a", Aggregate: service.ChannelMonitorRedisSharedAggregate{ActualSuccessCount: 2}},
		{ChannelID: 7, UserID: 31, APIKeyID: 201, ModelName: "model-a", Aggregate: service.ChannelMonitorRedisSharedAggregate{ActualSuccessCount: 2}},
	}})
	require.NoError(t, err)
	assert.Len(t, response.Items, 1)
	assert.Equal(t, int64(2), response.Summary["actual_sample_count"])
}
