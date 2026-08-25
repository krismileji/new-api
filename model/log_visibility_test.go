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

func TestUserLogQueriesHideRetryAndSystemRequests(t *testing.T) {
	originalDB := DB
	originalLogDB := LOG_DB
	originalLogDatabaseType := common.LogDatabaseType()
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.SetLogDatabaseType(originalLogDatabaseType)
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "logs.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&Log{}))
	require.NoError(t, db.Exec("CREATE TABLE channels (id INTEGER PRIMARY KEY, name TEXT NOT NULL)").Error)
	require.NoError(t, db.Exec("INSERT INTO channels (id, name) VALUES (?, ?)", 12, "private-channel").Error)
	DB = db
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled = false

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
		{
			UserId:    2,
			Username:  "other-user",
			CreatedAt: 7,
			Type:      LogTypeTopup,
			Content:   "account top-up",
			RequestId: "account-top-up",
			Other:     `{}`,
		},
		{
			UserId:    2,
			Username:  "other-user",
			CreatedAt: 8,
			Type:      LogTypeConsume,
			Content:   "manual channel test",
			Quota:     1000,
			ChannelId: 12,
			RequestId: "manual-channel-test",
			Other:     `{"channel_monitor_channel_test":true}`,
		},
		{
			UserId:    2,
			Username:  "other-user",
			CreatedAt: 9,
			Type:      LogTypeConsume,
			Content:   "smart schedule probe",
			Quota:     2000,
			ChannelId: 12,
			RequestId: "smart-schedule-probe",
			Other:     `{"channel_monitor_smart_schedule_probe":true}`,
		},
		{
			UserId:    2,
			Username:  "other-user",
			CreatedAt: 10,
			Type:      LogTypeError,
			Content:   "status probe failed",
			Quota:     3000,
			ChannelId: 12,
			RequestId: "status-probe",
			Other:     `{"channel_monitor_status_probe":true}`,
		},
		{
			UserId:    2,
			Username:  "other-user",
			CreatedAt: 11,
			Type:      LogTypeConsume,
			Content:   "violation fee",
			Quota:     4000,
			ChannelId: 12,
			RequestId: "violation-fee",
			Other:     `{"violation_fee":true}`,
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
	var userVisibleError *Log
	for _, log := range allUserVisibleLogs {
		if log.RequestId == "request-final-failure" {
			userVisibleError = log
			break
		}
	}
	require.NotNil(t, userVisibleError)
	assert.Equal(t, "status_code=500", userVisibleError.Content)

	filteredUserVisibleLogs, filteredTotal, err := GetAllUserVisibleLogsWithChannel(LogTypeUnknown, 0, 0, "", "other-user", "", 0, 10, 12, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), filteredTotal)
	require.Len(t, filteredUserVisibleLogs, 1)
	assert.Equal(t, "other-user-request", filteredUserVisibleLogs[0].RequestId)
	assert.Equal(t, "private-channel", filteredUserVisibleLogs[0].ChannelName)
	other, err := common.StrToMap(filteredUserVisibleLogs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, other, "admin_info")
	assert.NotContains(t, other, "audit_info")

	otherUserLogs, otherUserTotal, err := GetUserLogs(2, LogTypeUnknown, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), otherUserTotal)
	require.Len(t, otherUserLogs, 1)
	assert.Empty(t, otherUserLogs[0].ChannelName)
	otherUserLogInfo, err := common.StrToMap(otherUserLogs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, otherUserLogInfo, "admin_info")
	assert.NotContains(t, otherUserLogInfo, "audit_info")

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

	adminLogs, adminTotal, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 20, 0, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, int64(11), adminTotal)
	require.Len(t, adminLogs, 11)
	retryAttemptCount := 0
	adminContent := make([]string, 0, len(adminLogs))
	for _, log := range adminLogs {
		if log.IsRetryAttempt {
			retryAttemptCount++
		}
		adminContent = append(adminContent, log.Content)
	}
	assert.Equal(t, 3, retryAttemptCount)
	assert.Contains(t, adminContent, "status_code=503, temporary upstream failure")
	assert.Contains(t, adminContent, "status_code=500, final upstream failure")
	assert.Contains(t, adminContent, "manual channel test")
}

func TestUserVisibleLogsHydrateChannelNameAfterCacheMiss(t *testing.T) {
	originalDB := DB
	originalLogDB := LOG_DB
	originalLogDatabaseType := common.LogDatabaseType()
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalChannels := channelsIDM
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.SetLogDatabaseType(originalLogDatabaseType)
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelsIDM = originalChannels
	})

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "logs-cache-miss.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&Log{}))
	require.NoError(t, db.Exec("CREATE TABLE channels (id INTEGER PRIMARY KEY, name TEXT NOT NULL)").Error)
	const channelID = 987654321
	require.NoError(t, db.Exec("INSERT INTO channels (id, name) VALUES (?, ?)", channelID, "cache-miss-channel").Error)
	require.NoError(t, db.Create(&Log{
		UserId:    1,
		CreatedAt: 1,
		Type:      LogTypeConsume,
		ChannelId: channelID,
		Content:   "visible request",
	}).Error)

	DB = db
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled = true
	channelsIDM = map[int]*Channel{}

	logs, total, err := GetAllUserVisibleLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, logs, 1)
	assert.Equal(t, "cache-miss-channel", logs[0].ChannelName)
}

func TestClickHouseRetryAttemptColumn(t *testing.T) {
	assert.Contains(t, clickHouseLogCreateTableSQL(0), "is_retry_attempt UInt8 DEFAULT 0")
	assert.Equal(t, "ALTER TABLE logs ADD COLUMN IF NOT EXISTS is_retry_attempt UInt8 DEFAULT 0", clickHouseLogRetryAttemptColumnSQL)
}
