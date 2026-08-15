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
	assert.Equal(t, int64(3), response.Data.Summary.ActualSampleCount)
	assert.InDelta(t, 2.0/3.0, response.Data.Summary.ActualSuccessRate, 0.0001)
	assert.Equal(t, int64(1), response.Data.Summary.CacheSampleCount)
	assert.InDelta(t, 1, response.Data.Summary.CacheHitRate, 0.0001)
	assert.Equal(t, int64(8), response.Data.Summary.CacheReadTokens)
	assert.Equal(t, int64(100), response.Data.Summary.InputTokens)
	assert.InDelta(t, 0.08, response.Data.Summary.CacheUtilization, 0.0001)
	require.Len(t, response.Data.ChannelItems, 3)
	assert.Equal(t, 7, response.Data.ChannelItems[0].ChannelId)
	assert.Equal(t, "渠道七", response.Data.ChannelItems[0].ChannelName)
	assert.Equal(t, "主渠道", response.Data.ChannelItems[0].ChannelRemark)
	assert.Equal(t, int64(2), response.Data.ChannelItems[0].ActualSamples)
	assert.Equal(t, int64(1), response.Data.ChannelItems[0].ActualSuccess)
	assert.Equal(t, int64(1), response.Data.ChannelItems[0].ActualFailures)
	assert.Equal(t, int64(1), response.Data.ChannelItems[0].CacheSamples)
	assert.Equal(t, int64(1), response.Data.ChannelItems[0].CacheHitCount)
	assert.Equal(t, 8, response.Data.ChannelItems[1].ChannelId)
	assert.Equal(t, "无请求渠道", response.Data.ChannelItems[1].ChannelName)
	assert.Zero(t, response.Data.ChannelItems[1].ActualSamples)
	assert.Equal(t, 9, response.Data.ChannelItems[2].ChannelId)
	assert.Empty(t, response.Data.ChannelItems[2].ChannelName)
	assert.Equal(t, int64(1), response.Data.ChannelItems[2].ActualSamples)
	assert.Equal(t, int64(1), response.Data.ChannelItems[2].ActualSuccess)
	require.Len(t, response.Data.APIKeyItems, 2)
	assert.Equal(t, 11, response.Data.APIKeyItems[0].APIKeyId)
	assert.Equal(t, "主 Key", response.Data.APIKeyItems[0].APIKeyName)
	assert.Equal(t, int64(2), response.Data.APIKeyItems[0].ActualSampleCount)
	assert.Equal(t, 12, response.Data.APIKeyItems[1].APIKeyId)
	assert.Equal(t, "备用 Key", response.Data.APIKeyItems[1].APIKeyName)
	require.Len(t, response.Data.CacheWriteItems, 2)
	assert.Equal(t, 7, response.Data.CacheWriteItems[0].ChannelId)
	assert.Equal(t, "渠道七", response.Data.CacheWriteItems[0].ChannelName)
	assert.Equal(t, "主渠道", response.Data.CacheWriteItems[0].ChannelRemark)
	assert.Equal(t, int64(1), response.Data.CacheWriteItems[0].RequestCount)
	assert.Equal(t, 9, response.Data.CacheWriteItems[1].ChannelId)
	assert.Empty(t, response.Data.CacheWriteItems[1].ChannelName)
	assert.Equal(t, int64(1), response.Data.CacheWriteItems[1].RequestCount)
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
	assert.Equal(t, int64(1), response.Data.ChartItems[2].RequestCount)
	assert.Zero(t, response.Data.ChartItems[2].SuccessRate)
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
	assert.False(t, response.Data.SuccessMetricsAvailable)
	assert.False(t, response.Data.CacheWriteMetricsAvailable)
	assert.Zero(t, response.Data.Summary.ActualSampleCount)
	assert.Empty(t, response.Data.ChannelItems)
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
	assert.False(t, response.Data.SuccessMetricsAvailable)
	assert.True(t, response.Data.CacheWriteMetricsAvailable)
	require.Len(t, response.Data.CacheWriteItems, 1)
	assert.Equal(t, 17, response.Data.CacheWriteItems[0].ChannelId)
	assert.Equal(t, "渠道十七", response.Data.CacheWriteItems[0].ChannelName)
	assert.Equal(t, "缓存线路", response.Data.CacheWriteItems[0].ChannelRemark)
	assert.Equal(t, int64(1), response.Data.CacheWriteItems[0].RequestCount)
}
