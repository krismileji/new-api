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

func TestChannelDailyCostUpsertUsesBeijingDayAndOneRowPerChannel(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "daily-cost.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})
	require.Error(t, AddChannelDailyCost(context.Background(), 1, time.Now().Unix(), -1, 1, 0))

	beforeMidnight := time.Date(2026, 7, 21, 15, 59, 59, 0, time.UTC).Unix()
	afterMidnight := beforeMidnight + 1
	require.NoError(t, AddChannelDailyCost(context.Background(), 1, beforeMidnight, 100, 1, 0))
	require.NoError(t, AddChannelDailyCost(context.Background(), 1, beforeMidnight-60, 25, 1, 1))
	require.NoError(t, AddChannelDailyCost(context.Background(), 1, afterMidnight, 50, 1, 0))
	require.NoError(t, AddChannelDailyCost(context.Background(), 2, afterMidnight, 75, 1, 0))

	var rows []ChannelDailyCost
	require.NoError(t, db.Order("day_start ASC, channel_id ASC").Find(&rows).Error)
	require.Len(t, rows, 3)
	assert.Equal(t, ChannelDailyCostDayStart(beforeMidnight), rows[0].DayStart)
	assert.Equal(t, int64(125), rows[0].CostNanoCNY)
	assert.Equal(t, int64(2), rows[0].SettledCount)
	assert.Equal(t, int64(1), rows[0].UnresolvedCount)
	assert.Equal(t, ChannelDailyCostDayStart(afterMidnight), rows[1].DayStart)
	assert.Equal(t, 1, rows[1].ChannelId)
	assert.Equal(t, 2, rows[2].ChannelId)
}
