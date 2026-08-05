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

func TestAdvanceChannelMonitorAggregationCompletedThroughIsMonotonic(t *testing.T) {
	originalDB := DB
	originalDatabaseType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-watermark.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&ChannelMonitorAggregationState{}))
	t.Cleanup(func() {
		DB = originalDB
		common.SetMainDatabaseType(originalDatabaseType)
		require.NoError(t, sqlDB.Close())
	})

	completedThrough, err := GetChannelMonitorAggregationCompletedThrough(context.Background())
	require.NoError(t, err)
	assert.Zero(t, completedThrough)

	require.NoError(t, AdvanceChannelMonitorAggregationCompletedThrough(context.Background(), 120))
	require.NoError(t, AdvanceChannelMonitorAggregationCompletedThrough(context.Background(), 60))
	completedThrough, err = GetChannelMonitorAggregationCompletedThrough(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(120), completedThrough)

	require.NoError(t, AdvanceChannelMonitorAggregationCompletedThrough(context.Background(), 180))
	completedThrough, err = GetChannelMonitorAggregationCompletedThrough(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(180), completedThrough)
}
