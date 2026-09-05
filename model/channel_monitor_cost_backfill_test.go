package model

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelMonitorCostBackfillTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/channel-monitor-cost-backfill.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&ChannelDailyCost{}, &ChannelDailyAPIKeyCost{}, &ChannelMonitorDailyCostDetail{},
		&ChannelMonitorCostBackfillCheckpoint{}, &ChannelMonitorCostReconciliation{}, &Token{},
	))
	previous := DB
	DB = db
	t.Cleanup(func() {
		DB = previous
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func TestBackfillChannelMonitorCostDetailsIsIdempotentAndRecordsUnknownResidual(t *testing.T) {
	db := setupChannelMonitorCostBackfillTestDB(t)
	dayStart := ChannelDailyCostDayStart(time.Date(2026, 8, 15, 4, 0, 0, 0, time.UTC).Unix())
	fingerprint, display := ChannelDailyCostAPIKeyIdentityForToken(11, "sk-history")
	require.NoError(t, db.Create(&Token{Id: 11, UserId: 42, Name: "历史 Key"}).Error)
	require.NoError(t, db.Create(&ChannelDailyCost{
		ChannelId: 7, DayStart: dayStart, CostNanoCNY: 1000, ProbeCostNanoCNY: 100,
		GroupProbeCostNanoCNY: 50, SettledCount: 3, UnresolvedCount: 1,
		CreatedAt: dayStart, UpdatedAt: dayStart,
	}).Error)
	require.NoError(t, db.Create(&ChannelDailyAPIKeyCost{
		ChannelId: 7, DayStart: dayStart, APIKeyId: 11, APIKeyName: "历史 Key",
		KeyFingerprint: fingerprint, KeyDisplay: display, CostNanoCNY: 700,
		SettledCount: 2, UnresolvedCount: 1, CreatedAt: dayStart, UpdatedAt: dayStart,
	}).Error)

	result, err := BackfillChannelMonitorCostDetails(context.Background(), "batch-history-1", dayStart, dayStart+channelDailyCostDaySeconds, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, result.CompletedDays)
	assert.Equal(t, int64(2), result.RowsWritten)
	assert.Equal(t, int64(300), result.UnknownResidualCost)
	assert.Equal(t, int64(100), result.ProbeCategoryGap)
	assert.Equal(t, int64(50), result.GroupProbeCategoryGap)

	var details []ChannelMonitorDailyCostDetail
	require.NoError(t, db.Where("day_start = ?", dayStart).Order("cost_nano_cny DESC").Find(&details).Error)
	require.Len(t, details, 2)
	assert.Equal(t, int64(700), details[0].CostNanoCNY)
	assert.Equal(t, 42, details[0].UserId)
	assert.Equal(t, string(ChannelMonitorEventUserAttributionInferred), details[0].UserAttribution)
	assert.Equal(t, "unknown", details[0].ModelName)
	assert.Equal(t, int64(300), details[1].CostNanoCNY)
	assert.Equal(t, 0, details[1].APIKeyId)
	assert.Equal(t, string(ChannelMonitorEventUserAttributionUnknown), details[1].UserAttribution)

	result, err = BackfillChannelMonitorCostDetails(context.Background(), "batch-history-1", dayStart, dayStart+channelDailyCostDaySeconds, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, result.SkippedDays)
	var detailCount int64
	require.NoError(t, db.Model(&ChannelMonitorDailyCostDetail{}).Where("day_start = ?", dayStart).Count(&detailCount).Error)
	assert.Equal(t, int64(2), detailCount)
	var reconciliation ChannelMonitorCostReconciliation
	require.NoError(t, db.Where("batch_id = ? AND day_start = ?", "batch-history-1", dayStart).First(&reconciliation).Error)
	assert.Equal(t, ChannelMonitorCostReconciliationMatched, reconciliation.Status)
	assert.Equal(t, int64(1000), reconciliation.LedgerCostNanoCNY)
	assert.Equal(t, int64(1000), reconciliation.DetailCostNanoCNY)
	assert.Equal(t, int64(300), reconciliation.UnknownResidualCost)
}

func TestBackfillChannelMonitorCostDetailsRejectsNegativeResidualAndKeepsCheckpointFailed(t *testing.T) {
	db := setupChannelMonitorCostBackfillTestDB(t)
	dayStart := ChannelDailyCostDayStart(time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC).Unix())
	require.NoError(t, db.Create(&ChannelDailyCost{
		ChannelId: 8, DayStart: dayStart, CostNanoCNY: 100, SettledCount: 1,
		CreatedAt: dayStart, UpdatedAt: dayStart,
	}).Error)
	require.NoError(t, db.Create(&ChannelMonitorDailyCostDetail{
		DayStart: dayStart, ChannelId: 8, UserAttribution: string(ChannelMonitorEventUserAttributionRequest),
		ModelKey: ChannelMonitorDailyCostModelKey("gpt-4.1"), ModelName: "gpt-4.1", SourceKind: "business",
		CostNanoCNY: 101, SettledCount: 1, CreatedAt: dayStart, UpdatedAt: dayStart,
	}).Error)

	_, err := BackfillChannelMonitorCostDetails(context.Background(), "batch-negative-1", dayStart, dayStart+channelDailyCostDaySeconds, 1)
	require.Error(t, err)
	var checkpoint ChannelMonitorCostBackfillCheckpoint
	require.NoError(t, db.Where("batch_id = ? AND day_start = ?", "batch-negative-1", dayStart).First(&checkpoint).Error)
	assert.Equal(t, ChannelMonitorCostBackfillStatusFailed, checkpoint.Status)
	var detail ChannelMonitorDailyCostDetail
	require.NoError(t, db.Where("source_kind = ?", "business").First(&detail).Error)
	assert.Equal(t, int64(101), detail.CostNanoCNY)
}

func TestModelDetectionCostUpdatesAndReplacesItsDrilldownDetail(t *testing.T) {
	db := setupChannelMonitorCostBackfillTestDB(t)
	dayStart := ChannelDailyCostDayStart(time.Date(2026, 8, 17, 4, 0, 0, 0, time.UTC).Unix())
	require.NoError(t, AddChannelDailyCostWithModelDetectionAndModel(
		context.Background(), nil, 9, dayStart, 0, 0, 0, 1, "detect-model",
	))
	require.NoError(t, SettleUnresolvedChannelDailyModelDetectionCostWithModel(
		context.Background(), nil, 9, dayStart, 250, "detect-model",
	))

	var total ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ? AND day_start = ?", 9, dayStart).First(&total).Error)
	assert.Equal(t, int64(250), total.CostNanoCNY)
	assert.Equal(t, int64(1), total.SettledCount)
	assert.Zero(t, total.UnresolvedCount)
	var detail ChannelMonitorDailyCostDetail
	require.NoError(t, db.Where("channel_id = ?", 9).First(&detail).Error)
	assert.Equal(t, "detect-model", detail.ModelName)
	assert.Equal(t, int64(250), detail.CostNanoCNY)
	assert.Equal(t, int64(1), detail.SettledCount)
	assert.Zero(t, detail.UnresolvedCount)
}
