package model

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetChannelMonitorSuccessMetricsDistinguishesActualAndFinalResults(t *testing.T) {
	originalDB := DB
	originalLogDB := LOG_DB
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.SetLogDatabaseType(originalLogDatabaseType)
		initCol()
	})

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-success.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&Log{}, &ChannelMonitorMinuteMetric{}))
	DB = db
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	initCol()

	logs := []*Log{
		{ChannelId: 1, ModelName: "model-a", Group: "vip", TokenId: 11, TokenName: "主 Key", CreatedAt: 121, Type: LogTypeConsume, Other: `{"cache_tokens":25,"cache_ratio":0.5}`},
		{ChannelId: 1, ModelName: "model-a", Group: "vip", TokenId: 11, TokenName: "主 Key", CreatedAt: 122, Type: LogTypeConsume, Other: `{"cache_ratio":0.5,"cache_tokens":0}`},
		{ChannelId: 1, ModelName: "model-a", Group: "vip", TokenId: 11, TokenName: "主 Key", CreatedAt: 123, Type: LogTypeError, IsRetryAttempt: true, Content: "status_code=503, upstream unavailable", Other: `{"status_code":503,"error_type":"upstream_error","error_code":"bad_response_status_code"}`},
		{ChannelId: 1, ModelName: "model-a", Group: "vip", TokenId: 11, TokenName: "主 Key", CreatedAt: 124, Type: LogTypeError, Content: "status_code=429, rate limited", Other: `{"status_code":"429","error_type":"rate_limit","error_code":"rate_limit_exceeded"}`},
		{ChannelId: 2, ModelName: "model-b", Group: "vip", TokenId: 12, TokenName: "备用 Key", CreatedAt: 125, Type: LogTypeError, IsRetryAttempt: true, Content: "status_code=503, another upstream failure", Other: `{"error_type":"upstream_error","error_code":"bad_response_status_code"}`},
		{ChannelId: 2, ModelName: "model-b", Group: "standard", TokenId: 12, TokenName: "备用 Key", CreatedAt: 126, Type: LogTypeConsume, Other: `{}`},
		{ChannelId: 1, ModelName: "model-a", Group: "vip", CreatedAt: 119, Type: LogTypeError},
		{ChannelId: 0, ModelName: "model-a", Group: "vip", CreatedAt: 127, Type: LogTypeError},
		{ChannelId: 1, ModelName: "model-a", Group: "vip", CreatedAt: 128, Type: LogTypeManage},
	}
	require.NoError(t, db.Create(&logs).Error)
	_, err = AggregateChannelMonitorMinuteRange(context.Background(), 60, 180)
	require.NoError(t, err)

	channelMetrics, groupMetrics, err := GetChannelMonitorSuccessMetrics(context.Background(), 120)
	require.NoError(t, err)
	require.Len(t, channelMetrics, 2)

	assert.Equal(t, 1, channelMetrics[0].ChannelId)
	assert.Equal(t, "model-a", channelMetrics[0].ModelName)
	assert.Equal(t, int64(2), channelMetrics[0].ActualSuccessCount)
	assert.Equal(t, int64(2), channelMetrics[0].ActualFailureCount)
	assert.Equal(t, int64(4), channelMetrics[0].ActualSampleCount)
	assert.InDelta(t, 0.5, channelMetrics[0].ActualSuccessRate, 0.0001)
	assert.Equal(t, int64(2), channelMetrics[0].FinalSuccessCount)
	assert.Equal(t, int64(1), channelMetrics[0].FinalFailureCount)
	assert.Equal(t, int64(3), channelMetrics[0].FinalSampleCount)
	assert.InDelta(t, 2.0/3.0, channelMetrics[0].FinalSuccessRate, 0.0001)
	assert.Equal(t, int64(1), channelMetrics[0].CacheHitCount)
	assert.Equal(t, int64(2), channelMetrics[0].CacheSampleCount)
	assert.InDelta(t, 0.5, channelMetrics[0].CacheHitRate, 0.0001)

	assert.Equal(t, 2, channelMetrics[1].ChannelId)
	assert.Equal(t, "model-b", channelMetrics[1].ModelName)
	assert.Equal(t, int64(1), channelMetrics[1].ActualSuccessCount)
	assert.Equal(t, int64(1), channelMetrics[1].ActualFailureCount)
	assert.InDelta(t, 0.5, channelMetrics[1].ActualSuccessRate, 0.0001)
	assert.Equal(t, int64(1), channelMetrics[1].FinalSuccessCount)
	assert.Zero(t, channelMetrics[1].FinalFailureCount)
	assert.InDelta(t, 1, channelMetrics[1].FinalSuccessRate, 0.0001)
	assert.Zero(t, channelMetrics[1].CacheSampleCount)
	assert.Zero(t, channelMetrics[1].CacheHitRate)

	require.Len(t, groupMetrics, 2)
	assert.Equal(t, "standard", groupMetrics[0].Group)
	assert.Equal(t, int64(1), groupMetrics[0].ActualSampleCount)
	assert.InDelta(t, 1, groupMetrics[0].ActualSuccessRate, 0.0001)
	assert.InDelta(t, 1, groupMetrics[0].FinalSuccessRate, 0.0001)
	assert.Zero(t, groupMetrics[0].CacheSampleCount)

	assert.Equal(t, "vip", groupMetrics[1].Group)
	assert.Equal(t, int64(2), groupMetrics[1].ActualSuccessCount)
	assert.Equal(t, int64(3), groupMetrics[1].ActualFailureCount)
	assert.Equal(t, int64(5), groupMetrics[1].ActualSampleCount)
	assert.InDelta(t, 0.4, groupMetrics[1].ActualSuccessRate, 0.0001)
	assert.Equal(t, int64(2), groupMetrics[1].FinalSuccessCount)
	assert.Equal(t, int64(1), groupMetrics[1].FinalFailureCount)
	assert.Equal(t, int64(3), groupMetrics[1].FinalSampleCount)
	assert.InDelta(t, 2.0/3.0, groupMetrics[1].FinalSuccessRate, 0.0001)
	assert.Equal(t, int64(1), groupMetrics[1].CacheHitCount)
	assert.Equal(t, int64(2), groupMetrics[1].CacheSampleCount)
	assert.InDelta(t, 0.5, groupMetrics[1].CacheHitRate, 0.0001)

	channelDetail, err := GetChannelMonitorSuccessDetail(context.Background(), 120, ChannelMonitorSuccessFilter{
		ChannelId: 1,
		ModelName: "model-a",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), channelDetail.Summary.ActualSuccessCount)
	assert.Equal(t, int64(2), channelDetail.Summary.ActualFailureCount)
	assert.Equal(t, int64(1), channelDetail.Summary.FinalFailureCount)
	assert.Equal(t, int64(1), channelDetail.Summary.CacheHitCount)
	assert.Equal(t, int64(2), channelDetail.Summary.CacheSampleCount)
	assert.InDelta(t, 0.5, channelDetail.Summary.CacheHitRate, 0.0001)
	require.Len(t, channelDetail.ChannelItems, 1)
	require.Len(t, channelDetail.APIKeyItems, 1)
	assert.Equal(t, 11, channelDetail.APIKeyItems[0].APIKeyId)
	assert.Equal(t, "主 Key", channelDetail.APIKeyItems[0].APIKeyName)
	assert.Equal(t, int64(4), channelDetail.APIKeyItems[0].ActualSampleCount)
	assert.InDelta(t, 0.5, channelDetail.APIKeyItems[0].CacheHitRate, 0.0001)
	require.Len(t, channelDetail.FailureCategories, 2)
	assert.Equal(t, 429, channelDetail.FailureCategories[0].StatusCode)
	assert.Equal(t, "rate_limit_exceeded", channelDetail.FailureCategories[0].ErrorCode)
	assert.Equal(t, int64(1), channelDetail.FailureCategories[0].ActualCount)
	assert.Equal(t, int64(1), channelDetail.FailureCategories[0].FinalCount)
	assert.Equal(t, 503, channelDetail.FailureCategories[1].StatusCode)
	assert.Equal(t, int64(1), channelDetail.FailureCategories[1].ActualCount)
	assert.Zero(t, channelDetail.FailureCategories[1].FinalCount)

	groupDetail, err := GetChannelMonitorSuccessDetail(context.Background(), 120, ChannelMonitorSuccessFilter{Group: "vip"})
	require.NoError(t, err)
	assert.Equal(t, int64(5), groupDetail.Summary.ActualSampleCount)
	assert.Equal(t, int64(3), groupDetail.Summary.FinalSampleCount)
	require.Len(t, groupDetail.ChannelItems, 2)
	assert.Equal(t, 1, groupDetail.ChannelItems[0].ChannelId)
	assert.Equal(t, int64(4), groupDetail.ChannelItems[0].ActualSampleCount)
	assert.Equal(t, 2, groupDetail.ChannelItems[1].ChannelId)
	assert.Equal(t, int64(1), groupDetail.ChannelItems[1].ActualFailureCount)
	assert.Zero(t, groupDetail.ChannelItems[1].FinalSampleCount)
	require.Len(t, groupDetail.APIKeyItems, 2)
	assert.Equal(t, 11, groupDetail.APIKeyItems[0].APIKeyId)
	assert.Equal(t, 12, groupDetail.APIKeyItems[1].APIKeyId)
	assert.Empty(t, groupDetail.FailureCategories)

	require.NoError(t, db.Create(&Log{
		ChannelId:      1,
		ModelName:      "model-a",
		Group:          "vip",
		CreatedAt:      130,
		Type:           LogTypeError,
		IsRetryAttempt: true,
		Content:        "status_code=503, second unavailable response",
		Other:          `{"status_code":503,"error_type":"upstream_error","error_code":"bad_response_status_code"}`,
	}).Error)
	mergedDetail, err := GetChannelMonitorSuccessDetail(context.Background(), 120, ChannelMonitorSuccessFilter{
		ChannelId: 1,
		ModelName: "model-a",
	})
	require.NoError(t, err)
	require.Len(t, mergedDetail.FailureCategories, 2)
	assert.Equal(t, 503, mergedDetail.FailureCategories[0].StatusCode)
	assert.Equal(t, int64(2), mergedDetail.FailureCategories[0].ActualCount)
	assert.Zero(t, mergedDetail.FailureCategories[0].FinalCount)
	assert.Equal(t, int64(130), mergedDetail.FailureCategories[0].LastOccurred)
	assert.Contains(t, mergedDetail.FailureCategories[0].SampleContent, "second unavailable")
}
