package controller

import (
	contextpkg "context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelMonitorPerformanceAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		RangeMinutes            int                                      `json:"range_minutes"`
		RangeSource             string                                   `json:"range_source"`
		Items                   []model.ChannelMonitorPerformanceMetric  `json:"items"`
		SuccessMetricsAvailable bool                                     `json:"success_metrics_available"`
		SuccessItems            []model.ChannelMonitorSuccessMetric      `json:"success_items"`
		GroupSuccessItems       []model.ChannelMonitorGroupSuccessMetric `json:"group_success_items"`
	} `json:"data"`
}

func TestGetChannelMonitorPerformanceReturnsUsageLogMetrics(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	originalIsMasterNode := common.IsMasterNode
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
		common.IsMasterNode = originalIsMasterNode
	})
	common.LogConsumeEnabled = true
	constant.ErrorLogEnabled = true
	common.IsMasterNode = true
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "false",
	})

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "performance-api.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(
		&model.Log{},
		&model.ChannelMonitorMinuteMetric{},
		&model.ChannelMonitorAggregationState{},
		&model.ChannelSmartScheduleModelSampleState{},
	))
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	now := time.Now().Unix()
	logTimestamp := now - 60
	require.NoError(t, db.Create(&model.Log{
		ChannelId:        7,
		ModelName:        "test-model",
		CreatedAt:        logTimestamp,
		Type:             model.LogTypeConsume,
		IsStream:         true,
		Group:            "vip",
		CompletionTokens: 120,
		UseTime:          4,
		Other:            `{"frt":1500}`,
	}).Error)
	require.NoError(t, db.Create(&model.Log{
		ChannelId:      7,
		ModelName:      "test-model",
		CreatedAt:      logTimestamp,
		Type:           model.LogTypeError,
		IsRetryAttempt: true,
		Group:          "vip",
		Content:        "status_code=503, upstream unavailable",
		Other:          `{"status_code":503,"error_type":"upstream_error","error_code":"bad_response_status_code"}`,
	}).Error)
	_, err = model.AggregateChannelMonitorMinuteRange(contextpkg.Background(), now-1800, now-now%60)
	require.NoError(t, err)

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
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, 7, response.Data.Items[0].ChannelId)
	require.NotNil(t, response.Data.Items[0].AverageFirstTokenMs)
	assert.InDelta(t, 1500, *response.Data.Items[0].AverageFirstTokenMs, 0.001)
	require.NotNil(t, response.Data.Items[0].AverageTPS)
	assert.InDelta(t, 30, *response.Data.Items[0].AverageTPS, 0.001)
	assert.True(t, response.Data.SuccessMetricsAvailable)
	require.Len(t, response.Data.SuccessItems, 1)
	assert.Equal(t, int64(1), response.Data.SuccessItems[0].ActualSuccessCount)
	assert.Equal(t, int64(1), response.Data.SuccessItems[0].ActualFailureCount)
	assert.InDelta(t, 0.5, response.Data.SuccessItems[0].ActualSuccessRate, 0.001)
	assert.Equal(t, int64(1), response.Data.SuccessItems[0].FinalSampleCount)
	assert.InDelta(t, 1, response.Data.SuccessItems[0].FinalSuccessRate, 0.001)
	require.Len(t, response.Data.GroupSuccessItems, 1)
	assert.Equal(t, "vip", response.Data.GroupSuccessItems[0].Group)
	assert.InDelta(t, 0.5, response.Data.GroupSuccessItems[0].ActualSuccessRate, 0.001)
	assert.InDelta(t, 1, response.Data.GroupSuccessItems[0].FinalSuccessRate, 0.001)

	detailRecorder := httptest.NewRecorder()
	detailContext, _ := gin.CreateTestContext(detailRecorder)
	detailContext.Request = httptest.NewRequest(http.MethodGet, "/api/channel_monitor/success/detail?minutes=30&channel_id=7&model_name=test-model", nil)
	GetChannelMonitorSuccessDetail(detailContext)

	assert.Equal(t, http.StatusOK, detailRecorder.Code)
	var detailResponse struct {
		Success bool `json:"success"`
		Data    struct {
			SuccessMetricsAvailable bool                              `json:"success_metrics_available"`
			Detail                  model.ChannelMonitorSuccessDetail `json:"detail"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(detailRecorder.Body.Bytes(), &detailResponse))
	assert.True(t, detailResponse.Success)
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
