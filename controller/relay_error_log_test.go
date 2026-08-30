package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
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
	processChannelError(c, *types.NewChannelError(9, 1, "test-channel", true, "", false), apiErr, true)

	var logs []model.Log
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.True(t, logs[0].IsRetryAttempt)
	assert.Equal(t, 9, logs[0].ChannelId)
	assert.Equal(t, "standard", logs[0].Group)
	assert.Contains(t, logs[0].Content, "upstream error: do request failed")
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
	assert.Equal(t, float64(9), other["channel_id"])
	assert.Equal(t, "test-channel", other["channel_name"])
	assert.Equal(t, float64(1), other["channel_type"])
	adminInfo, ok := other["admin_info"].(map[string]any)
	require.True(t, ok)
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
	processChannelError(c, *types.NewChannelError(9, 1, "test-channel", false, "", false), channelTestErr, false)
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 1)

	service.BeginChannelDailyCostAttempt(c, 9)
	service.MarkChannelDailyCostRequestDispatched(c)
	processChannelError(c, *types.NewChannelError(9, 1, "test-channel", false, "", false), channelTestErr, false)
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

	processChannelError(c, *types.NewChannelError(9, 1, "test-channel", false, "", false), types.NewClientGoneError(context.Canceled), false)
	require.NoError(t, db.Find(&logs).Error)
	assert.Len(t, logs, 2)
}
