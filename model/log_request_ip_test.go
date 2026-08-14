package model

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRequestIPLogTest(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := DB
	originalLogDB := LOG_DB
	originalRedisEnabled := common.RedisEnabled
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.RedisEnabled = originalRedisEnabled
		common.LogConsumeEnabled = originalLogConsumeEnabled
		common.SetLogDatabaseType(originalLogDatabaseType)
	})

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "logs.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, db.AutoMigrate(&Log{}))
	require.NoError(t, db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, setting TEXT, deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("INSERT INTO users (id, setting) VALUES (?, ?)", 1, "{}").Error)

	DB = db
	LOG_DB = db
	common.RedisEnabled = false
	common.LogConsumeEnabled = true
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	return db
}

func requestIPTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.RemoteAddr = "192.0.2.44:4321"
	c.Set("username", "user")
	return c
}

func TestUsageLogsStoreAdminRequestIPWhenUserOptedOut(t *testing.T) {
	db := setupRequestIPLogTest(t)
	c := requestIPTestContext()

	RecordConsumeLog(c, 1, RecordConsumeLogParams{
		ModelName: "gpt-test",
		Other: map[string]interface{}{
			"admin_info": map[string]interface{}{"existing": true},
		},
	})
	RecordErrorLog(c, 1, 9, "gpt-test", "token", "status_code=500", 7, 1, false, "default", map[string]interface{}{
		"admin_info": map[string]interface{}{"existing": true},
	}, false)

	var logs []Log
	require.NoError(t, db.Order("id asc").Find(&logs).Error)
	require.Len(t, logs, 2)
	for _, log := range logs {
		assert.Empty(t, log.Ip)
		other, err := common.StrToMap(log.Other)
		require.NoError(t, err)
		adminInfo, ok := other["admin_info"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, adminInfo["existing"])
		assert.Equal(t, "192.0.2.44", adminInfo["request_ip"])
	}
}

func TestUsageLogsKeepRequestIPVisibleWhenUserOptedIn(t *testing.T) {
	db := setupRequestIPLogTest(t)
	require.NoError(t, db.Exec("UPDATE users SET setting = ? WHERE id = ?", `{"record_ip_log":true}`, 1).Error)
	c := requestIPTestContext()

	RecordConsumeLog(c, 1, RecordConsumeLogParams{ModelName: "gpt-test"})
	RecordErrorLog(c, 1, 9, "gpt-test", "token", "status_code=500", 7, 1, false, "default", nil, false)

	userLogs, _, err := GetUserLogs(1, LogTypeUnknown, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	require.Len(t, userLogs, 2)
	for _, log := range userLogs {
		assert.Equal(t, "192.0.2.44", log.Ip)
		other, err := common.StrToMap(log.Other)
		require.NoError(t, err)
		assert.NotContains(t, other, "admin_info")
	}
}

func TestAdminLogViewsExposeAdminRequestIPWithoutLeakingItToUser(t *testing.T) {
	db := setupRequestIPLogTest(t)
	require.NoError(t, db.Create(&Log{
		UserId:    1,
		CreatedAt: 1,
		Type:      LogTypeConsume,
		TokenId:   7,
		RequestId: "admin-request-ip",
		Other: common.MapToJsonStr(map[string]interface{}{
			"admin_info": map[string]interface{}{
				"request_ip": "192.0.2.44",
				"private":    true,
			},
		}),
	}).Error)

	adminLogs, _, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 10, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, adminLogs, 1)
	assert.Equal(t, "192.0.2.44", adminLogs[0].Ip)

	userVisibleLogs, _, err := GetAllUserVisibleLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	require.Len(t, userVisibleLogs, 1)
	assert.Equal(t, "192.0.2.44", userVisibleLogs[0].Ip)
	userVisibleOther, err := common.StrToMap(userVisibleLogs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, userVisibleOther, "admin_info")

	userLogs, _, err := GetUserLogs(1, LogTypeUnknown, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	require.Len(t, userLogs, 1)
	assert.Empty(t, userLogs[0].Ip)
	userOther, err := common.StrToMap(userLogs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, userOther, "admin_info")
}
