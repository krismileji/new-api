package model

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelDailyAPIKeyCostTracksSelectedKeysWithoutPersistingSecrets(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "daily-api-key-cost.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}, &ChannelDailyAPIKeyCost{}))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	keyA := "sk-upstream-key-alpha"
	keyB := "sk-upstream-key-beta"
	fingerprintA, displayA := ChannelDailyCostAPIKeyIdentity(keyA)
	fingerprintB, displayB := ChannelDailyCostAPIKeyIdentity(keyB)
	require.NotEmpty(t, fingerprintA)
	require.NotEmpty(t, displayA)
	assert.NotEqual(t, fingerprintA, fingerprintB)
	assert.NotContains(t, displayA, keyA)
	assert.NotContains(t, displayB, keyB)

	beforeMidnight := time.Date(2026, 7, 21, 15, 59, 59, 0, time.UTC).Unix()
	afterMidnight := beforeMidnight + 1
	require.NoError(t, AddChannelDailyCostWithAPIKey(context.Background(), 1, beforeMidnight, 100, 1, 0, fingerprintA, displayA))
	require.NoError(t, AddChannelDailyCostWithAPIKey(context.Background(), 1, beforeMidnight-60, 25, 1, 1, fingerprintA, displayA))
	require.NoError(t, AddChannelDailyCostWithAPIKey(context.Background(), 1, beforeMidnight, 75, 1, 0, fingerprintB, displayB))
	require.NoError(t, AddChannelDailyCostWithAPIKey(context.Background(), 1, afterMidnight, 50, 1, 0, fingerprintA, displayA))

	var channelRows []ChannelDailyCost
	require.NoError(t, db.Order("day_start ASC").Find(&channelRows).Error)
	require.Len(t, channelRows, 2)
	assert.Equal(t, int64(200), channelRows[0].CostNanoCNY)
	assert.Equal(t, int64(3), channelRows[0].SettledCount)
	assert.Equal(t, int64(1), channelRows[0].UnresolvedCount)

	keyRows, err := GetChannelDailyAPIKeyCosts(context.Background(), ChannelDailyCostDayStart(beforeMidnight), ChannelDailyCostDayStart(afterMidnight)+channelDailyCostDaySeconds)
	require.NoError(t, err)
	require.Len(t, keyRows, 3)
	firstDayRows := make(map[string]ChannelDailyAPIKeyCost, 2)
	for _, row := range keyRows[:2] {
		firstDayRows[row.KeyFingerprint] = row
	}
	assert.Equal(t, displayA, firstDayRows[fingerprintA].KeyDisplay)
	assert.Equal(t, int64(125), firstDayRows[fingerprintA].CostNanoCNY)
	assert.Equal(t, int64(2), firstDayRows[fingerprintA].SettledCount)
	assert.Equal(t, int64(1), firstDayRows[fingerprintA].UnresolvedCount)
	assert.Equal(t, displayB, firstDayRows[fingerprintB].KeyDisplay)
	assert.Equal(t, int64(75), firstDayRows[fingerprintB].CostNanoCNY)
	assert.Equal(t, ChannelDailyCostDayStart(afterMidnight), keyRows[2].DayStart)
	for _, row := range keyRows {
		assert.NotContains(t, row.KeyFingerprint, keyA)
		assert.NotContains(t, row.KeyFingerprint, keyB)
		assert.NotContains(t, row.KeyDisplay, keyA)
		assert.NotContains(t, row.KeyDisplay, keyB)
	}
}

func TestChannelDailyAPIKeyCostRollsBackChannelTotalWhenKeyWriteFails(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "invalid-daily-api-key-cost.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	fingerprint, display := ChannelDailyCostAPIKeyIdentity("sk-rollback-check")
	err = AddChannelDailyCostWithAPIKey(context.Background(), 1, time.Now().Unix(), 100, 1, 0, fingerprint, display)
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&ChannelDailyCost{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestChannelDailyAPIKeyCostStoresInboundAPIKeyNames(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "daily-api-key-name.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}, &ChannelDailyAPIKeyCost{}))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	when := time.Date(2026, 7, 22, 4, 0, 0, 0, time.UTC).Unix()
	firstFingerprint, firstDisplay := ChannelDailyCostAPIKeyIdentityForToken(11, "sk-shared-upstream")
	secondFingerprint, secondDisplay := ChannelDailyCostAPIKeyIdentityForToken(12, "sk-shared-upstream")
	require.NoError(t, AddChannelDailyCostWithAPIKeyAndToken(context.Background(), 1, when, 100, 1, 0, 11, "生产 Key", firstFingerprint, firstDisplay))
	require.NoError(t, AddChannelDailyCostWithAPIKeyAndToken(context.Background(), 1, when, 200, 1, 0, 12, "备用 Key", secondFingerprint, secondDisplay))

	var rows []ChannelDailyAPIKeyCost
	require.NoError(t, db.Order("api_key_id ASC").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, 11, rows[0].APIKeyId)
	assert.Equal(t, "生产 Key", rows[0].APIKeyName)
	assert.Equal(t, 12, rows[1].APIKeyId)
	assert.Equal(t, "备用 Key", rows[1].APIKeyName)
	assert.NotEqual(t, rows[0].KeyFingerprint, rows[1].KeyFingerprint)
}

func TestGetChannelDailyAPIKeyCostsResolvesAStoredTokenName(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "daily-api-key-resolve-name.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&Token{}, &ChannelDailyAPIKeyCost{}))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, db.Create(&Token{Id: 27, UserId: 1, Key: "inbound-key", Name: "当前 API Key 名称"}).Error)
	when := time.Date(2026, 7, 22, 4, 0, 0, 0, time.UTC).Unix()
	fingerprint, display := ChannelDailyCostAPIKeyIdentityForToken(27, "sk-upstream")
	require.NoError(t, db.Create(&ChannelDailyAPIKeyCost{
		ChannelId:      3,
		DayStart:       ChannelDailyCostDayStart(when),
		APIKeyId:       27,
		KeyFingerprint: fingerprint,
		KeyDisplay:     display,
		CreatedAt:      when,
		UpdatedAt:      when,
	}).Error)

	rows, err := GetChannelDailyAPIKeyCosts(context.Background(), ChannelDailyCostDayStart(when), ChannelDailyCostDayStart(when)+channelDailyCostDaySeconds)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "当前 API Key 名称", rows[0].APIKeyName)
}
