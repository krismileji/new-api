package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useChannelMonitorMinuteTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	originalDB := DB
	originalDatabaseType := common.MainDatabaseType()
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&ChannelMonitorMinuteMetric{}))
	t.Cleanup(func() {
		DB = originalDB
		common.SetMainDatabaseType(originalDatabaseType)
	})
}

func aggregateChannelMonitorMinuteTestRange(t *testing.T, startTimestamp int64, endTimestamp int64) {
	t.Helper()
	_, err := AggregateChannelMonitorMinuteRange(context.Background(), startTimestamp, endTimestamp)
	require.NoError(t, err)
}
