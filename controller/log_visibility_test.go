package controller

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetLogsSelfStatUsesUserVisibleRequestScope(t *testing.T) {
	originalLogDB := model.LOG_DB
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		model.LOG_DB = originalLogDB
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
	model.LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	now := time.Now().Unix()
	const selfUserID = 42
	require.NoError(t, db.Create([]*model.Log{
		{
			UserId:           selfUserID,
			Username:         "self-user",
			CreatedAt:        now,
			Type:             model.LogTypeConsume,
			Quota:            23,
			PromptTokens:     4,
			CompletionTokens: 5,
			RequestId:        "target-request",
		},
		{
			UserId:           selfUserID,
			Username:         "self-user",
			CreatedAt:        now,
			Type:             model.LogTypeConsume,
			Quota:            999,
			PromptTokens:     100,
			CompletionTokens: 100,
			IsRetryAttempt:   true,
			RequestId:        "target-request",
		},
		{
			UserId:           selfUserID,
			Username:         "self-user",
			CreatedAt:        now,
			Type:             model.LogTypeConsume,
			Quota:            1000,
			PromptTokens:     200,
			CompletionTokens: 20,
			RequestId:        "target-request",
			Other:            `{"channel_monitor_status_probe":true}`,
		},
		{
			UserId:           selfUserID,
			Username:         "self-user",
			CreatedAt:        now,
			Type:             model.LogTypeConsume,
			Quota:            2000,
			PromptTokens:     300,
			CompletionTokens: 30,
			RequestId:        "target-request",
			Other:            `{"violation_fee":true,"fee_quota":2000}`,
		},
		{
			UserId:           selfUserID,
			Username:         "self-user",
			CreatedAt:        now,
			Type:             model.LogTypeConsume,
			Quota:            17,
			PromptTokens:     2,
			CompletionTokens: 3,
			RequestId:        "other-request",
		},
		{
			UserId:           43,
			Username:         "self-user",
			CreatedAt:        now,
			Type:             model.LogTypeConsume,
			Quota:            5000,
			PromptTokens:     300,
			CompletionTokens: 30,
			RequestId:        "target-request",
		},
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/log/self/stat?type=0&request_id=target-request", nil)
	context.Set("id", selfUserID)
	context.Set("username", "renamed-self-user")

	GetLogsSelfStat(context)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool       `json:"success"`
		Data    model.Stat `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, 23, response.Data.Quota)
	assert.Equal(t, 1, response.Data.Rpm)
	assert.Equal(t, 9, response.Data.Tpm)
}
