package model

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserLogQueriesHideRetryAttempts(t *testing.T) {
	originalLogDB := LOG_DB
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = originalLogDB
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
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	logs := []*Log{
		{
			UserId:         1,
			CreatedAt:      1,
			Type:           LogTypeError,
			Content:        "status_code=503, temporary upstream failure",
			TokenId:        7,
			RequestId:      "request-retried-successfully",
			Other:          `{"status_code":503}`,
			IsRetryAttempt: true,
		},
		{
			UserId:    1,
			CreatedAt: 2,
			Type:      LogTypeConsume,
			Content:   "final success",
			TokenId:   7,
			RequestId: "request-retried-successfully",
			Other:     `{}`,
		},
		{
			UserId:         1,
			CreatedAt:      3,
			Type:           LogTypeError,
			Content:        "status_code=502, retryable upstream failure",
			TokenId:        7,
			RequestId:      "request-final-failure",
			Other:          `{"status_code":502}`,
			IsRetryAttempt: true,
		},
		{
			UserId:    1,
			CreatedAt: 4,
			Type:      LogTypeError,
			Content:   "status_code=500, final upstream failure",
			TokenId:   7,
			RequestId: "request-final-failure",
			Other:     `{"status_code":500}`,
		},
		{
			UserId:           2,
			Username:         "other-user",
			CreatedAt:        5,
			Type:             LogTypeConsume,
			Content:          "other user final success",
			Quota:            23,
			PromptTokens:     4,
			CompletionTokens: 5,
			TokenId:          8,
			ChannelId:        12,
			ChannelName:      "private-channel",
			RequestId:        "other-user-request",
			Other:            `{"admin_info":{"upstream":"private"},"audit_info":{"route":"private"}}`,
		},
		{
			UserId:           2,
			Username:         "other-user",
			CreatedAt:        6,
			Type:             LogTypeConsume,
			Content:          "other user retry attempt",
			Quota:            999,
			PromptTokens:     100,
			CompletionTokens: 100,
			ChannelId:        12,
			RequestId:        "other-user-request",
			IsRetryAttempt:   true,
		},
	}
	require.NoError(t, db.Create(&logs).Error)

	userLogs, total, err := GetUserLogs(1, LogTypeUnknown, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, userLogs, 2)
	assert.ElementsMatch(t,
		[]string{"request-retried-successfully", "request-final-failure"},
		[]string{userLogs[0].RequestId, userLogs[1].RequestId},
	)
	for _, log := range userLogs {
		assert.NotContains(t, log.Content, "temporary upstream failure")
	}

	allUserVisibleLogs, allUserVisibleTotal, err := GetAllUserVisibleLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, int64(3), allUserVisibleTotal)
	require.Len(t, allUserVisibleLogs, 3)
	assert.ElementsMatch(t,
		[]string{"request-retried-successfully", "request-final-failure", "other-user-request"},
		[]string{allUserVisibleLogs[0].RequestId, allUserVisibleLogs[1].RequestId, allUserVisibleLogs[2].RequestId},
	)

	filteredUserVisibleLogs, filteredTotal, err := GetAllUserVisibleLogsWithChannel(LogTypeUnknown, 0, 0, "", "other-user", "", 0, 10, 12, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), filteredTotal)
	require.Len(t, filteredUserVisibleLogs, 1)
	assert.Equal(t, "other-user-request", filteredUserVisibleLogs[0].RequestId)
	assert.Empty(t, filteredUserVisibleLogs[0].ChannelName)
	other, err := common.StrToMap(filteredUserVisibleLogs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, other, "admin_info")
	assert.NotContains(t, other, "audit_info")
	visibleStat, err := SumUserVisibleQuota(LogTypeUnknown, 0, 0, "", "", "", 12, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, 23, visibleStat.Quota)

	userErrorLogs, errorTotal, err := GetUserLogs(1, LogTypeError, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), errorTotal)
	require.Len(t, userErrorLogs, 1)
	assert.Equal(t, "request-final-failure", userErrorLogs[0].RequestId)
	assert.Equal(t, "status_code=500", userErrorLogs[0].Content)

	tokenLogs, err := GetLogByTokenId(7)
	require.NoError(t, err)
	assert.Len(t, tokenLogs, 2)

	adminLogs, adminTotal, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 10, 0, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, int64(6), adminTotal)
	require.Len(t, adminLogs, 6)
	retryAttemptCount := 0
	for _, log := range adminLogs {
		if log.IsRetryAttempt {
			retryAttemptCount++
		}
	}
	assert.Equal(t, 3, retryAttemptCount)
	assert.Contains(t, adminLogs[5].Content, "temporary upstream failure")
	assert.Contains(t, adminLogs[2].Content, "final upstream failure")
}

func TestClickHouseRetryAttemptColumn(t *testing.T) {
	assert.Contains(t, clickHouseLogCreateTableSQL(0), "is_retry_attempt UInt8 DEFAULT 0")
	assert.Equal(t, "ALTER TABLE logs ADD COLUMN IF NOT EXISTS is_retry_attempt UInt8 DEFAULT 0", clickHouseLogRetryAttemptColumnSQL)
}
