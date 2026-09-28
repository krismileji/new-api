package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProcessChannelErrorPersistsRetryAttempt(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalErrorLogEnabled := constant.ErrorLogEnabled
	originalRedisEnabled := common.RedisEnabled
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		constant.ErrorLogEnabled = originalErrorLogEnabled
		common.RedisEnabled = originalRedisEnabled
		common.SetLogDatabaseType(originalLogDatabaseType)
	})

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "logs.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	require.NoError(t, db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, setting TEXT, deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("INSERT INTO users (id, setting) VALUES (?, ?)", 1, "{}").Error)
	model.DB = db
	model.LOG_DB = db
	constant.ErrorLogEnabled = true
	common.RedisEnabled = false
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("id", 1)
	c.Set("username", "user")
	c.Set("token_name", "test-token")
	c.Set("original_model", "gpt-test")
	c.Set("token_id", 7)
	c.Set("group", "default")
	c.Set("channel_id", 99)
	c.Set("channel_name", "stale-channel")
	c.Set("channel_type", 8)
	c.Set("use_channel", []string{"9"})
	common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, false)
	c.Set(common.RequestIdKey, "retry-request")
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "vip")
	common.SetContextKey(c, constant.ContextKeyAutoGroup, "standard")
	common.SetContextKey(c, service.UpstreamErrorDiagnosticContextKey, service.UpstreamErrorDiagnostic{
		Category: service.UpstreamErrorCategoryDNS,
		Summary:  "上游域名解析失败",
		Host:     "api.example.com",
		Detail:   "lookup ***.***.com: no such host",
	})

	apiErr := types.NewOpenAIError(errors.New("upstream error: do request failed"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	processChannelErrorWithRetry(c, *types.NewChannelError(9, 1, "test-channel", true, "", false), apiErr, true)

	var logs []model.Log
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.True(t, logs[0].IsRetryAttempt)
	assert.Equal(t, 9, logs[0].ChannelId)
	assert.Equal(t, "standard", logs[0].Group)
	assert.Contains(t, logs[0].Content, "upstream error: do request failed")
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
	adminInfo, ok := other["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(9), adminInfo["channel_id"])
	assert.Equal(t, "test-channel", adminInfo["channel_name"])
	assert.Equal(t, float64(1), adminInfo["channel_type"])
	assert.NotContains(t, other, "channel_id")
	assert.NotContains(t, other, "channel_name")
	assert.NotContains(t, other, "channel_type")
	assert.Equal(t, true, adminInfo["is_multi_key"])
	upstreamError, ok := adminInfo["upstream_error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, service.UpstreamErrorCategoryDNS, upstreamError["category"])
	assert.Equal(t, "上游域名解析失败", upstreamError["summary"])
	assert.Equal(t, "api.example.com", upstreamError["host"])
	_, recorded := other["channel_monitor_attempt_duration_ms"]
	assert.False(t, recorded)

	// Maintenance channel tests keep their group and marker in the error log so
	// scheduler aggregation can exclude them from production stability data.
	c.Set(channelTestContextKey, true)
	c.Set("group", "channel-test-group")
	common.SetContextKey(c, constant.ContextKeyAutoGroup, "")
	common.SetContextKey(c, service.UpstreamErrorDiagnosticContextKey, nil)
	channelTestErr := types.NewOpenAIError(errors.New("channel test upstream failure"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	processChannelErrorWithRetry(c, *types.NewChannelError(9, 1, "test-channel", false, "", false), channelTestErr, false)
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 1)

	service.BeginChannelDailyCostAttempt(c, 9)
	service.MarkChannelDailyCostRequestDispatched(c)
	processChannelErrorWithRetry(c, *types.NewChannelError(9, 1, "test-channel", false, "", false), channelTestErr, false)
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 2)
	assert.Equal(t, "channel-test-group", logs[1].Group)
	var channelTestOther map[string]any
	require.NoError(t, common.UnmarshalJsonStr(logs[1].Other, &channelTestOther))
	assert.Equal(t, true, channelTestOther[model.ChannelMonitorChannelTestLogKey])

	processChannelErrorWithTiming(
		c,
		*types.NewChannelError(9, 1, "test-channel", false, "", false),
		channelTestErr,
		false,
		false,
		nil,
		true,
	)
	require.NoError(t, db.Find(&logs).Error)
	assert.Len(t, logs, 2)

	processChannelErrorWithRetry(c, *types.NewChannelError(9, 1, "test-channel", false, "", false), types.NewClientGoneError(context.Canceled), false)
	require.NoError(t, db.Find(&logs).Error)
	assert.Len(t, logs, 2)
}

func TestProcessChannelErrorUsesSnapshotWithoutLeakingChannelMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	previousErrorLogEnabled := constant.ErrorLogEnabled

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Log{}))
	model.DB, model.LOG_DB = database, database
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		constant.ErrorLogEnabled = previousErrorLogEnabled
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, database.Create(&model.User{Id: 7, Username: "log-owner", Group: "default"}).Error)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("id", 7)
	ctx.Set("username", "log-owner")
	ctx.Set("token_name", "test-token")
	ctx.Set("token_id", 11)
	ctx.Set("original_model", "gpt-test")
	ctx.Set("group", "default")
	ctx.Set("channel_id", 202)
	ctx.Set("channel_name", "mutable-context-channel")
	ctx.Set("channel_type", 9)
	ctx.Set("use_channel", []string{"101"})
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now().Add(-time.Second))

	channelSnapshot := types.ChannelError{
		ChannelId:   101,
		ChannelType: 1,
		ChannelName: "snapshot-channel",
		AutoBan:     false,
	}
	apiErr := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)

	relayInfo := &relaycommon.RelayInfo{OriginModelName: "gpt-test", BillingModelName: "billing-model"}
	relayInfo.ObserveResponseModel("returned-model")
	processChannelErrorWithTiming(ctx, channelSnapshot, apiErr, false, false, nil, false, relayInfo)

	var stored model.Log
	require.NoError(t, database.First(&stored).Error)
	assert.Equal(t, channelSnapshot.ChannelId, stored.ChannelId)
	storedOther, err := common.StrToMap(stored.Other)
	require.NoError(t, err)
	assert.Equal(t, float64(http.StatusBadGateway), storedOther["status_code"])
	for _, key := range []string{"channel_id", "channel_name", "channel_type"} {
		assert.NotContains(t, storedOther, key)
	}
	adminInfo, ok := storedOther["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"101"}, adminInfo["use_channel"])
	assert.Equal(t, "billing-model", adminInfo["billing_model"])
	assert.Equal(t, map[string]any{"requested_model": "gpt-test", "upstream_model": "", "returned_model": "returned-model"}, storedOther["response_model"])

	logs, total, err := model.GetUserLogs(7, model.LogTypeError, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, logs, 1)
	assert.Equal(t, channelSnapshot.ChannelId, logs[0].ChannelId)
	assert.Empty(t, logs[0].ChannelName)
	userOther, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, userOther, "admin_info")
	for _, key := range []string{"channel_id", "channel_name", "channel_type"} {
		assert.NotContains(t, userOther, key)
	}
}
