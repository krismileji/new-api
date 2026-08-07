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

type legacyChannelMonitorAggregationState struct {
	ID               int   `gorm:"primaryKey"`
	CompletedThrough int64 `gorm:"not null"`
	Revision         int64 `gorm:"not null;default:0"`
	UpdatedAt        int64 `gorm:"not null"`
}

func (legacyChannelMonitorAggregationState) TableName() string {
	return "channel_monitor_aggregation_states"
}

func TestChannelMonitorAggregationStateMigrationBackfillsCoveredFrom(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-migration.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, db.AutoMigrate(&legacyChannelMonitorAggregationState{}))
	require.NoError(t, db.Create(&legacyChannelMonitorAggregationState{
		ID: channelMonitorAggregationStateID, CompletedThrough: 300, UpdatedAt: 300,
	}).Error)

	require.NoError(t, db.AutoMigrate(&ChannelMonitorAggregationState{}))
	var state ChannelMonitorAggregationState
	require.NoError(t, db.First(&state, channelMonitorAggregationStateID).Error)
	assert.Zero(t, state.CoveredFrom)
	assert.Equal(t, int64(300), state.CompletedThrough)
}

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
	coverage, err := GetChannelMonitorAggregationCoverage(context.Background())
	require.NoError(t, err)
	assert.Zero(t, coverage.CoveredFrom)
	assert.Zero(t, coverage.CompletedThrough)

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

func TestTrimChannelMonitorAggregationCoverageMovesOnlyTheStart(t *testing.T) {
	originalDB := DB
	originalDatabaseType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-coverage.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&ChannelMonitorAggregationState{}))
	require.NoError(t, db.Create(&ChannelMonitorAggregationState{
		ID: channelMonitorAggregationStateID, CoveredFrom: 60, CompletedThrough: 300,
	}).Error)
	t.Cleanup(func() {
		DB = originalDB
		common.SetMainDatabaseType(originalDatabaseType)
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, TrimChannelMonitorAggregationCoverage(context.Background(), 120))
	require.NoError(t, TrimChannelMonitorAggregationCoverage(context.Background(), 90))

	coverage, err := GetChannelMonitorAggregationCoverage(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(120), coverage.CoveredFrom)
	assert.Equal(t, int64(300), coverage.CompletedThrough)
}
