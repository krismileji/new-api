package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorAnalyticsCurrentPaginationKeepsScopeSummaryStable(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	_ = db
	now := common.GetTimestamp()
	dayStart := model.ChannelDailyCostDayStart(now)
	first := model.NewChannelMonitorEvent(101, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, now)
	first.EventId = "analytics-page-channel-101"
	first.RequestDispatched = true
	first.IsFinalAttempt = true
	second := model.NewChannelMonitorEvent(202, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeFailure, now)
	second.EventId = "analytics-page-channel-202"
	second.RequestDispatched = true
	second.IsFinalAttempt = true
	statusCode := 500
	second.StatusCode = &statusCode
	require.NoError(t, service.NewChannelMonitorRedisSharedProjectionWithClient(common.RDB).
		HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{first, second}))

	query := channelMonitorAnalyticsQuery{
		Metric: "success", GroupBy: "channel", From: dayStart, To: dayStart + 24*60*60,
		Sort: "samples", Direction: "desc", Page: 1, PageSize: 1,
	}
	pageOne, err := queryChannelMonitorHistoricalAnalytics(context.Background(), query)
	require.NoError(t, err)
	query.Page = 2
	pageTwo, err := queryChannelMonitorHistoricalAnalytics(context.Background(), query)
	require.NoError(t, err)
	assert.Equal(t, int64(2), pageOne.Total)
	assert.Equal(t, pageOne.Total, pageTwo.Total)
	assert.Equal(t, pageOne.ScopeSummary, pageTwo.ScopeSummary)
	assert.Len(t, pageOne.Items, 1)
	assert.Len(t, pageTwo.Items, 1)
}

func TestChannelMonitorAnalyticsCurrentAPIKeyRowsRemainVisible(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	now := common.GetTimestamp()
	dayStart := model.ChannelDailyCostDayStart(now)
	event := model.NewChannelMonitorEvent(101, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, now)
	event.EventId = "analytics-current-api-key"
	event.UserId = 31
	event.APIKeyId = 201
	event.APIKeyName = "生产 Key"
	event.RequestDispatched = true
	event.IsFinalAttempt = true
	require.NoError(t, service.NewChannelMonitorRedisSharedProjectionWithClient(common.RDB).
		HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event}))

	response, err := queryChannelMonitorHistoricalAnalytics(context.Background(), channelMonitorAnalyticsQuery{
		Metric: "success", GroupBy: "api_key", From: dayStart, To: dayStart + 24*60*60,
		Page: 1, PageSize: 20, Sort: "samples", Direction: "desc",
	})
	require.NoError(t, err)
	require.Len(t, response.Items, 1)
	assert.Equal(t, 201, response.Items[0]["api_key_id"])
	assert.Equal(t, "生产 Key", response.Items[0]["api_key_name"])
}

func TestChannelMonitorAnalyticsCurrentRedisFailureDoesNotReturnZeroSummary(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	originalClient := common.RDB
	common.RDB = nil
	t.Cleanup(func() { common.RDB = originalClient })
	now := common.GetTimestamp()
	query := channelMonitorAnalyticsQuery{
		Metric: "success", GroupBy: "channel", From: model.ChannelDailyCostDayStart(now),
		To: model.ChannelDailyCostDayStart(now) + 24*60*60, Page: 1, PageSize: 20,
	}
	_, err := queryChannelMonitorHistoricalAnalytics(context.Background(), query)
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrChannelMonitorRedisSharedProjectionUnavailable))
}

func TestChannelMonitorAnalyticsCostAPIKeyDrillsIntoChannelAndModel(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorDailyCostDetail{}))
	dayStart := model.ChannelDailyCostDayStart(common.GetTimestamp()) - 24*60*60
	modelAKey := model.ChannelMonitorDailyCostModelKey("model-a")
	modelBKey := model.ChannelMonitorDailyCostModelKey("model-b")
	fingerprint := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	require.NoError(t, db.Create(&[]model.ChannelMonitorDailyCostDetail{
		{DayStart: dayStart, ChannelId: 7, UserId: 31, UserAttribution: string(model.ChannelMonitorEventUserAttributionRequest), APIKeyId: 201, APIKeyKey: fingerprint, APIKeyName: "生产 Key", ModelKey: modelAKey, ModelName: "model-a", SourceKind: "business", CostNanoCNY: 100, SettledCount: 1, CreatedAt: dayStart, UpdatedAt: dayStart},
		{DayStart: dayStart, ChannelId: 8, UserId: 31, UserAttribution: string(model.ChannelMonitorEventUserAttributionRequest), APIKeyId: 201, APIKeyKey: fingerprint, APIKeyName: "生产 Key", ModelKey: modelBKey, ModelName: "model-b", SourceKind: "business", CostNanoCNY: 200, SettledCount: 2, CreatedAt: dayStart, UpdatedAt: dayStart},
	}).Error)

	query := channelMonitorAnalyticsQuery{
		Metric: "cost", GroupBy: "api_key_channel_model", From: dayStart, To: dayStart + 24*60*60,
		APIKey: 201, Page: 1, PageSize: 1, Direction: "desc",
	}
	pageOne, err := queryChannelMonitorHistoricalAnalytics(context.Background(), query)
	require.NoError(t, err)
	query.Page = 2
	pageTwo, err := queryChannelMonitorHistoricalAnalytics(context.Background(), query)
	require.NoError(t, err)
	assert.Equal(t, int64(2), pageOne.Total)
	assert.Equal(t, pageOne.Total, pageTwo.Total)
	assert.Equal(t, pageOne.ScopeSummary, pageTwo.ScopeSummary)
	assert.Len(t, pageOne.Items, 1)
	assert.Len(t, pageTwo.Items, 1)
	assert.Equal(t, 201, pageOne.Items[0]["api_key_id"])
	assert.Equal(t, 201, pageTwo.Items[0]["api_key_id"])
	assert.NotEqual(t, pageOne.Items[0]["channel_id"], pageTwo.Items[0]["channel_id"])
}

func TestChannelMonitorAnalyticsHistoricalPaginationKeepsScopeSummaryStable(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorDailySuccessLedger{}))
	dayStart := model.ChannelDailyCostDayStart(common.GetTimestamp()) - 24*60*60
	require.NoError(t, db.Create(&[]model.ChannelMonitorDailySuccessLedger{
		{DayStart: dayStart, ChannelId: 7, UserId: 31, UserAttribution: string(model.ChannelMonitorEventUserAttributionRequest), APIKeyId: 201, APIKeyKey: "key-201", ModelKey: "model-a-key", ModelName: "model-a", GroupKey: "group", GroupName: "group", ActualSuccessCount: 3, FinalSuccessCount: 3, CreatedAt: dayStart, UpdatedAt: dayStart},
		{DayStart: dayStart, ChannelId: 8, UserId: 31, UserAttribution: string(model.ChannelMonitorEventUserAttributionRequest), APIKeyId: 201, APIKeyKey: "key-201", ModelKey: "model-b-key", ModelName: "model-b", GroupKey: "group", GroupName: "group", ActualFailureCount: 2, FinalFailureCount: 2, CreatedAt: dayStart, UpdatedAt: dayStart},
	}).Error)

	query := channelMonitorAnalyticsQuery{
		Metric: "success", GroupBy: "channel", From: dayStart, To: dayStart + 24*60*60,
		Page: 1, PageSize: 1, Sort: "samples", Direction: "desc",
	}
	pageOne, err := queryChannelMonitorHistoricalAnalytics(context.Background(), query)
	require.NoError(t, err)
	query.Page = 2
	pageTwo, err := queryChannelMonitorHistoricalAnalytics(context.Background(), query)
	require.NoError(t, err)
	assert.Equal(t, int64(2), pageOne.Total)
	assert.Equal(t, pageOne.Total, pageTwo.Total)
	assert.Equal(t, pageOne.ScopeSummary, pageTwo.ScopeSummary)
	assert.Equal(t, int64(5), pageOne.ScopeSummary["actual_sample_count"])
}
