package controller

import (
	contextpkg "context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelMonitorPerformanceAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		RangeMinutes            int                                             `json:"range_minutes"`
		RangeSource             string                                          `json:"range_source"`
		GeneratedAt             int64                                           `json:"generated_at"`
		ProjectionStartedAt     int64                                           `json:"projection_started_at"`
		RealtimeDegraded        bool                                            `json:"realtime_degraded"`
		MetricCoverage          channelMonitorPerformanceMetricCoverageResponse `json:"metric_coverage"`
		Items                   []model.ChannelMonitorPerformanceMetric         `json:"items"`
		SuccessMetricsAvailable bool                                            `json:"success_metrics_available"`
		SuccessItems            []model.ChannelMonitorSuccessMetric             `json:"success_items"`
		GroupSuccessItems       []model.ChannelMonitorGroupSuccessMetric        `json:"group_success_items"`
	} `json:"data"`
}

func TestGetChannelMonitorPerformanceReturnsUsageLogMetrics(t *testing.T) {
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "false",
	})
	now := time.Now().Unix()
	firstToken := 1500.0
	tPS := 30.0
	inputTokens := int64(100)
	cacheReadTokens := int64(20)
	success := model.NewChannelMonitorEvent(700007, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, now-10)
	success.EventId = "performance-controller-success"
	success.ModelName = "test-model-realtime"
	success.GroupName = "vip-realtime"
	success.RequestDispatched = true
	success.IsFinalAttempt = true
	success.FirstTokenMs = &firstToken
	success.TPS = &tPS
	success.InputTokens = &inputTokens
	success.CacheReadTokens = &cacheReadTokens
	failure := model.NewChannelMonitorEvent(700007, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeFailure, now-5)
	failure.EventId = "performance-controller-failure"
	failure.ModelName = "test-model-realtime"
	failure.GroupName = "vip-realtime"
	failure.RequestDispatched = true
	failure.IsRetryAttempt = true
	failure.APIKeyId = 77
	failure.APIKeyName = "实时 Key"
	statusCode := http.StatusServiceUnavailable
	failure.StatusCode = &statusCode
	failure.ErrorType = "upstream_error"
	failure.ErrorCode = "bad_response_status_code"
	failure.ErrorMessage = "status_code=503, upstream unavailable"
	require.NotEqual(t, service.ChannelMonitorEventEnqueueInvalid, service.EmitChannelMonitorEvent(success))
	require.NotEqual(t, service.ChannelMonitorEventEnqueueInvalid, service.EmitChannelMonitorEvent(failure))
	require.NoError(t, service.FlushChannelMonitorEvents(contextpkg.Background()))

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/channel_monitor/performance?minutes=30", nil)

	GetChannelMonitorPerformance(context)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response channelMonitorPerformanceAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, 30, response.Data.RangeMinutes)
	assert.Equal(t, channelMonitorPerformanceRangeManual, response.Data.RangeSource)
	assert.True(t, response.Data.MetricCoverage.AggregationEnabled)
	assert.NotZero(t, response.Data.ProjectionStartedAt)
	assert.True(t, response.Data.RealtimeDegraded)
	assert.False(t, response.Data.MetricCoverage.WindowComplete)
	windowStart := (response.Data.GeneratedAt - 30*60) - (response.Data.GeneratedAt-30*60)%60
	assert.Equal(t, windowStart, response.Data.MetricCoverage.WindowStart)
	var performanceItem model.ChannelMonitorPerformanceMetric
	for _, item := range response.Data.Items {
		if item.ChannelId == 700007 && item.ModelName == "test-model-realtime" {
			performanceItem = item
		}
	}
	require.NotNil(t, performanceItem.AverageFirstTokenMs)
	assert.InDelta(t, 1500, *performanceItem.AverageFirstTokenMs, 0.001)
	require.NotNil(t, performanceItem.AverageTPS)
	assert.InDelta(t, 30, *performanceItem.AverageTPS, 0.001)
	assert.True(t, response.Data.SuccessMetricsAvailable)
	var successItem model.ChannelMonitorSuccessMetric
	for _, item := range response.Data.SuccessItems {
		if item.ChannelId == 700007 && item.ModelName == "test-model-realtime" {
			successItem = item
		}
	}
	assert.Equal(t, int64(1), successItem.ActualSuccessCount)
	assert.Equal(t, int64(1), successItem.ActualFailureCount)
	assert.InDelta(t, 0.5, successItem.ActualSuccessRate, 0.001)
	assert.Equal(t, int64(1), successItem.FinalSampleCount)
	assert.InDelta(t, 1, successItem.FinalSuccessRate, 0.001)

	detailRecorder := httptest.NewRecorder()
	detailContext, _ := gin.CreateTestContext(detailRecorder)
	detailContext.Request = httptest.NewRequest(http.MethodGet, "/api/channel_monitor/success/detail?minutes=30&channel_id=700007&model_name=test-model-realtime", nil)
	GetChannelMonitorSuccessDetail(detailContext)

	assert.Equal(t, http.StatusOK, detailRecorder.Code)
	var detailResponse struct {
		Success bool `json:"success"`
		Data    struct {
			ProjectionStartedAt     int64                             `json:"projection_started_at"`
			RealtimeDegraded        bool                              `json:"realtime_degraded"`
			SuccessMetricsAvailable bool                              `json:"success_metrics_available"`
			Detail                  model.ChannelMonitorSuccessDetail `json:"detail"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(detailRecorder.Body.Bytes(), &detailResponse))
	assert.True(t, detailResponse.Success)
	assert.NotZero(t, detailResponse.Data.ProjectionStartedAt)
	assert.True(t, detailResponse.Data.RealtimeDegraded)
	assert.True(t, detailResponse.Data.SuccessMetricsAvailable)
	assert.Equal(t, int64(1), detailResponse.Data.Detail.Summary.ActualFailureCount)
	require.Len(t, detailResponse.Data.Detail.FailureCategories, 1)
	assert.Equal(t, 503, detailResponse.Data.Detail.FailureCategories[0].StatusCode)

	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"test-model"}, 2, 80, 30,
	)
	common.OptionMapRWMutex.Lock()
	common.OptionMap = map[string]string{
		channelMonitorSmartScheduleEnabledOption:           "true",
		channelMonitorSmartScheduleGroupPoliciesOption:     channelSmartScheduleTestGroupPoliciesJSON(t, policy),
		channelMonitorSmartSchedulePerformanceWindowOption: "120",
	}
	common.OptionMapRWMutex.Unlock()

	smartRecorder := httptest.NewRecorder()
	smartContext, _ := gin.CreateTestContext(smartRecorder)
	smartContext.Request = httptest.NewRequest(http.MethodGet, "/api/channel_monitor/performance?minutes=invalid", nil)
	GetChannelMonitorPerformance(smartContext)

	assert.Equal(t, http.StatusOK, smartRecorder.Code)
	var smartResponse channelMonitorPerformanceAPIResponse
	require.NoError(t, common.Unmarshal(smartRecorder.Body.Bytes(), &smartResponse))
	assert.True(t, smartResponse.Success)
	assert.Equal(t, 120, smartResponse.Data.RangeMinutes)
	assert.Equal(t, channelMonitorPerformanceRangeSmart, smartResponse.Data.RangeSource)
}

func TestGetChannelMonitorPerformanceRejectsInvalidRange(t *testing.T) {
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "false",
	})
	for _, minutes := range []string{"0", "1441", "invalid"} {
		t.Run(minutes, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodGet, "/api/channel_monitor/performance?minutes="+minutes, nil)

			GetChannelMonitorPerformance(context)

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "性能与成功率统计范围必须在 1 到 1440 分钟之间")
		})
	}
}
