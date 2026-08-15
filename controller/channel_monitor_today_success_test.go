package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChannelMonitorTodaySuccessReturnsChannelBreakdown(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	common.LogConsumeEnabled = true
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})

	remark := "主渠道"
	require.NoError(t, db.Create(&model.Channel{
		Id:     7,
		Name:   "渠道七",
		Remark: &remark,
		Key:    "key-7",
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:   8,
		Name: "无请求渠道",
		Key:  "key-8",
	}).Error)
	now := common.GetTimestamp()
	dayStart := model.ChannelDailyCostDayStart(now)
	logs := []*model.Log{
		{ChannelId: 7, ModelName: "model-a", TokenId: 11, TokenName: "主 Key", CreatedAt: dayStart + 1, Type: model.LogTypeConsume, PromptTokens: 100, Other: `{"cache_tokens":8,"cache_write_tokens":32}`},
		{ChannelId: 7, ModelName: "model-b", TokenId: 11, TokenName: "主 Key", CreatedAt: dayStart + 2, Type: model.LogTypeError, IsRetryAttempt: true},
		{ChannelId: 9, ModelName: "deleted-channel", TokenId: 12, TokenName: "备用 Key", CreatedAt: dayStart + 3, Type: model.LogTypeConsume, Other: `{"cache_creation_tokens_5m":64}`},
		{ChannelId: 7, ModelName: "old", CreatedAt: dayStart - 1, Type: model.LogTypeConsume},
		{ChannelId: 7, ModelName: "tomorrow", CreatedAt: dayStart + 24*60*60, Type: model.LogTypeConsume},
	}
	require.NoError(t, db.Create(&logs).Error)
	require.NoError(t, aggregateChannelMonitorTestLogs(dayStart, now))
	inputTokens := int64(100)
	cacheReadTokens := int64(8)
	cacheWriteTokens := int64(32)
	first := model.NewChannelMonitorEvent(7, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, now)
	first.EventId = "today-success-channel-7"
	first.ModelName = "model-a"
	first.GroupName = "today-group"
	first.APIKeyId = 11
	first.APIKeyName = "主 Key"
	first.RequestDispatched = true
	first.IsFinalAttempt = true
	first.InputTokens = &inputTokens
	first.CacheReadTokens = &cacheReadTokens
	first.CacheWriteTokens = &cacheWriteTokens
	failure := model.NewChannelMonitorEvent(7, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeFailure, now)
	failure.EventId = "today-failure-channel-7"
	failure.ModelName = "model-b"
	failure.GroupName = "today-group"
	failure.APIKeyId = 11
	failure.APIKeyName = "主 Key"
	failure.RequestDispatched = true
	failure.IsRetryAttempt = true
	successDeleted := model.NewChannelMonitorEvent(9, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, now)
	successDeleted.EventId = "today-success-channel-9"
	successDeleted.ModelName = "deleted-channel"
	successDeleted.GroupName = "today-group"
	successDeleted.APIKeyId = 12
	successDeleted.APIKeyName = "备用 Key"
	successDeleted.RequestDispatched = true
	successDeleted.IsFinalAttempt = true
	successDeleted.CacheWriteTokens = &cacheWriteTokens
	emitChannelMonitorControllerRealtimeEvents(t, first, failure, successDeleted)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/success/today", nil)
	GetChannelMonitorTodaySuccess(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			GeneratedAt                int64                                     `json:"generated_at"`
			DayStart                   int64                                     `json:"day_start"`
			SuccessMetricsAvailable    bool                                      `json:"success_metrics_available"`
			CacheWriteMetricsAvailable bool                                      `json:"cache_write_metrics_available"`
			Summary                    model.ChannelMonitorSuccessSummary        `json:"summary"`
			APIKeyItems                []model.ChannelMonitorSuccessAPIKeyMetric `json:"api_key_items"`
			ChannelItems               []struct {
				ChannelId      int    `json:"channel_id"`
				ChannelName    string `json:"channel_name"`
				ChannelRemark  string `json:"channel_remark"`
				ActualSamples  int64  `json:"actual_sample_count"`
				CacheSamples   int64  `json:"cache_sample_count"`
				CacheHitCount  int64  `json:"cache_hit_count"`
				ActualSuccess  int64  `json:"actual_success_count"`
				ActualFailures int64  `json:"actual_failure_count"`
			} `json:"channel_items"`
			CacheWriteItems []struct {
				ChannelId     int    `json:"channel_id"`
				ChannelName   string `json:"channel_name"`
				ChannelRemark string `json:"channel_remark"`
				RequestCount  int64  `json:"request_count"`
			} `json:"cache_write_items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.True(t, response.Data.SuccessMetricsAvailable)
	assert.True(t, response.Data.CacheWriteMetricsAvailable)
	assert.Equal(t, dayStart, response.Data.DayStart)
	assert.GreaterOrEqual(t, response.Data.GeneratedAt, now)
	assert.GreaterOrEqual(t, response.Data.Summary.ActualSampleCount, int64(3))
	assert.Greater(t, response.Data.Summary.ActualSuccessRate, 0.0)
	assert.GreaterOrEqual(t, response.Data.Summary.CacheSampleCount, int64(1))
	assert.GreaterOrEqual(t, response.Data.Summary.CacheReadTokens, int64(8))
	assert.GreaterOrEqual(t, response.Data.Summary.InputTokens, int64(100))
	byChannel := make(map[int]int64, len(response.Data.ChannelItems))
	for _, item := range response.Data.ChannelItems {
		byChannel[item.ChannelId] = item.ActualSamples
	}
	assert.Equal(t, int64(2), byChannel[7])
	assert.Zero(t, byChannel[8])
	assert.Equal(t, int64(1), byChannel[9])
	byAPIKey := make(map[int]model.ChannelMonitorSuccessAPIKeyMetric, len(response.Data.APIKeyItems))
	for _, item := range response.Data.APIKeyItems {
		byAPIKey[item.APIKeyId] = item
	}
	assert.Equal(t, int64(2), byAPIKey[11].ActualSampleCount)
	assert.Equal(t, "主 Key", byAPIKey[11].APIKeyName)
	assert.Equal(t, "备用 Key", byAPIKey[12].APIKeyName)
	byCacheWrite := make(map[int]int64, len(response.Data.CacheWriteItems))
	for _, item := range response.Data.CacheWriteItems {
		byCacheWrite[item.ChannelId] = item.RequestCount
	}
	assert.Equal(t, int64(1), byCacheWrite[7])
	assert.Equal(t, int64(1), byCacheWrite[9])
}

func TestGetChannelMonitorTodaySuccessReturnsRangeChartAndSelectedDayDetails(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	common.LogConsumeEnabled = true
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})

	require.NoError(t, db.Create(&model.Channel{Id: 27, Name: "按日渠道", Key: "key-27"}).Error)
	now := common.GetTimestamp()
	todayStart := model.ChannelDailyCostDayStart(now)
	yesterdayStart := todayStart - channelMonitorCostDaySeconds
	require.NoError(t, db.Create(&[]*model.Log{
		{ChannelId: 27, ModelName: "yesterday", TokenId: 31, TokenName: "昨日 Key", CreatedAt: yesterdayStart + 1, Type: model.LogTypeConsume, PromptTokens: 16, Other: `{"cache_tokens":8,"cache_write_tokens":32}`},
		{ChannelId: 27, ModelName: "today", TokenId: 32, TokenName: "今日 Key", CreatedAt: todayStart + 1, Type: model.LogTypeError},
	}).Error)
	require.NoError(t, aggregateChannelMonitorTestLogs(yesterdayStart, now))
	todayFailure := model.NewChannelMonitorEvent(27, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeFailure, todayStart+1)
	todayFailure.EventId = "today-success-range-channel-27-failure"
	todayFailure.ModelName = "today"
	todayFailure.APIKeyId = 32
	todayFailure.APIKeyName = "今日 Key"
	todayFailure.RequestDispatched = true
	todayFailure.IsFinalAttempt = true
	emitChannelMonitorControllerRealtimeEvents(t, todayFailure)

	detailDate := channelMonitorCostDate(yesterdayStart)
	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/success/today?days=3&date="+detailDate, nil,
	)
	GetChannelMonitorTodaySuccess(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool                               `json:"success"`
		Data    channelMonitorTodaySuccessOverview `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, 3, response.Data.Days)
	assert.Equal(t, detailDate, response.Data.DetailDate)
	assert.Equal(t, yesterdayStart, response.Data.DayStart)
	assert.Equal(t, int64(1), response.Data.Summary.ActualSuccessCount)
	assert.Zero(t, response.Data.Summary.ActualFailureCount)
	require.Len(t, response.Data.APIKeyItems, 1)
	assert.Equal(t, 31, response.Data.APIKeyItems[0].APIKeyId)
	require.Len(t, response.Data.ChartItems, 3)
	assert.Zero(t, response.Data.ChartItems[0].RequestCount)
	assert.Equal(t, int64(1), response.Data.ChartItems[1].RequestCount)
	assert.InDelta(t, 1, response.Data.ChartItems[1].SuccessRate, 0.0001)
	assert.InDelta(t, 1, response.Data.ChartItems[1].CacheRate, 0.0001)
	assert.Equal(t, int64(8), response.Data.ChartItems[1].CacheReadTokens)
	assert.Equal(t, int64(16), response.Data.ChartItems[1].InputTokens)
	assert.InDelta(t, 0.5, response.Data.ChartItems[1].CacheUtilizationRate, 0.0001)
	assert.Equal(t, int64(1), response.Data.ChartItems[1].CacheWriteRequestCount)
	assert.Equal(t, 1, response.Data.ChartItems[1].CacheWriteChannelCount)
	assert.GreaterOrEqual(t, response.Data.ChartItems[2].RequestCount, int64(1))
	assert.GreaterOrEqual(t, response.Data.ChartItems[2].SuccessRate, 0.0)
}

func TestGetChannelMonitorTodaySuccessRejectsDateOutsideRange(t *testing.T) {
	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/success/today?days=7&date=2000-01-01", nil,
	)
	GetChannelMonitorTodaySuccess(ctx)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "统计日期必须在所选时间范围内")
}

func TestGetChannelMonitorTodaySuccessReportsUnavailableWithoutLogSources(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	common.LogConsumeEnabled = false
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/success/today", nil)
	GetChannelMonitorTodaySuccess(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			SuccessMetricsAvailable    bool                                       `json:"success_metrics_available"`
			CacheWriteMetricsAvailable bool                                       `json:"cache_write_metrics_available"`
			Summary                    model.ChannelMonitorSuccessSummary         `json:"summary"`
			ChannelItems               []model.ChannelMonitorChannelSuccessMetric `json:"channel_items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.True(t, response.Data.SuccessMetricsAvailable)
	assert.True(t, response.Data.CacheWriteMetricsAvailable)
}

func TestGetChannelMonitorTodaySuccessReturnsCacheWritesWithoutErrorLogs(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	common.LogConsumeEnabled = true
	constant.ErrorLogEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})

	remark := "缓存线路"
	require.NoError(t, db.Create(&model.Channel{
		Id:     17,
		Name:   "渠道十七",
		Remark: &remark,
		Key:    "key-17",
	}).Error)
	now := common.GetTimestamp()
	dayStart := model.ChannelDailyCostDayStart(now)
	require.NoError(t, db.Create(&model.Log{
		ChannelId: 17,
		ModelName: "model-cache",
		CreatedAt: dayStart + 1,
		Type:      model.LogTypeConsume,
		Other:     `{"cache_write_tokens":128}`,
	}).Error)
	require.NoError(t, aggregateChannelMonitorTestLogs(dayStart, now))
	cacheWriteTokens := int64(64)
	event := model.NewChannelMonitorEvent(17, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, now)
	event.EventId = "cache-write-only-channel-17"
	event.RequestDispatched = true
	event.IsFinalAttempt = true
	event.CacheWriteTokens = &cacheWriteTokens
	emitChannelMonitorControllerRealtimeEvents(t, event)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/success/today", nil)
	GetChannelMonitorTodaySuccess(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			SuccessMetricsAvailable    bool `json:"success_metrics_available"`
			CacheWriteMetricsAvailable bool `json:"cache_write_metrics_available"`
			CacheWriteItems            []struct {
				ChannelId     int    `json:"channel_id"`
				ChannelName   string `json:"channel_name"`
				ChannelRemark string `json:"channel_remark"`
				RequestCount  int64  `json:"request_count"`
			} `json:"cache_write_items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.True(t, response.Data.SuccessMetricsAvailable)
	assert.True(t, response.Data.CacheWriteMetricsAvailable)
	channel17Index := -1
	for i := range response.Data.CacheWriteItems {
		if response.Data.CacheWriteItems[i].ChannelId == 17 {
			channel17Index = i
			break
		}
	}
	require.NotEqual(t, -1, channel17Index)
	channel17 := response.Data.CacheWriteItems[channel17Index]
	assert.Equal(t, "渠道十七", channel17.ChannelName)
	assert.Equal(t, "缓存线路", channel17.ChannelRemark)
	assert.Equal(t, int64(1), channel17.RequestCount)
}
