package controller

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func runChannelMonitorAnalyticsCostCoverageCases(t *testing.T, db *gorm.DB, day int64) {
	t.Helper()
	query := channelMonitorAnalyticsQuery{Metric: "cost", GroupBy: "user", From: day, To: day + 86400, Page: 1, PageSize: 20, Sort: "cost", Direction: "desc"}
	matched, err := queryChannelMonitorHistoricalCostAnalytics(context.Background(), query)
	require.NoError(t, err)
	assert.Equal(t, service.ChannelMonitorCoverageComplete, matched.Coverage.Status)
	// The global amount stays equal, but the two channels no longer reconcile.
	require.NoError(t, db.Model(&model.ChannelDailyCost{}).Where("channel_id = ? AND day_start = ?", 7, day).Update("cost_nano_cny", 750).Error)
	require.NoError(t, db.Model(&model.ChannelDailyCost{}).Where("channel_id = ? AND day_start = ?", 8, day).Update("cost_nano_cny", 250).Error)
	gap, err := queryChannelMonitorHistoricalCostAnalytics(context.Background(), query)
	require.NoError(t, err)
	assert.Equal(t, service.ChannelMonitorCoveragePartial, gap.Coverage.Status)
	assert.Contains(t, gap.Coverage.Reasons, "cost_attribution_incomplete")
	assert.Equal(t, int64(1000), gap.Summary["cost_nano_cny"])
	query.GroupBy = "channel"
	ledger, err := queryChannelMonitorHistoricalCostAnalytics(context.Background(), query)
	require.NoError(t, err)
	assert.Equal(t, service.ChannelMonitorCoverageComplete, ledger.Coverage.Status)
	assert.Equal(t, int64(1000), ledger.Summary["cost_nano_cny"])
}

func TestChannelMonitorAnalyticsHistoricalCostCoverageReconcilesEachChannel(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	day := model.ChannelDailyCostDayStart(common.GetTimestamp()) - 2*86400
	seedChannelMonitorAnalyticsFilterFixture(t, db, day)
	runChannelMonitorAnalyticsCostCoverageCases(t, db, day)
}

func TestChannelMonitorAnalyticsHistoricalEmptyCostDetailsRemainPartial(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	day := model.ChannelDailyCostDayStart(common.GetTimestamp()) - 2*86400
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorDailyCostDetail{}))
	require.NoError(t, db.Create(&model.ChannelDailyCost{DayStart: day, ChannelId: 7, CostNanoCNY: 10_000_000_000, SettledCount: 1}).Error)
	response, err := queryChannelMonitorHistoricalCostAnalytics(context.Background(), channelMonitorAnalyticsQuery{
		Metric: "cost", GroupBy: "user", From: day, To: day + 86400, Page: 1, PageSize: 20, Sort: "cost", Direction: "desc",
	})
	require.NoError(t, err)
	assert.Empty(t, response.Items)
	assert.Equal(t, service.ChannelMonitorCoveragePartial, response.Coverage.Status)
	assert.Contains(t, response.Coverage.Reasons, "cost_attribution_incomplete")
}

func runChannelMonitorAnalyticsSuccessCoverageCases(t *testing.T, db *gorm.DB, day int64) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorDailyCheckpoint{}, &model.ChannelMonitorDailySuccessLedger{}))
	require.NoError(t, db.Create(&model.ChannelMonitorDailyCheckpoint{DayStart: day, Revision: 1, UpdatedAt: day + 86400}).Error)
	query := channelMonitorAnalyticsQuery{Metric: "success", GroupBy: "user", From: day, To: day + 2*86400, Page: 1, PageSize: 20, Sort: "samples", Direction: "desc"}
	missing, err := queryChannelMonitorHistoricalSuccessAnalytics(context.Background(), query)
	require.NoError(t, err)
	assert.Equal(t, service.ChannelMonitorCoveragePartial, missing.Coverage.Status)
	assert.Contains(t, missing.Coverage.Reasons, "daily_checkpoint_missing")
	require.NoError(t, db.Create(&model.ChannelMonitorDailyCheckpoint{DayStart: day + 86400, Revision: 1, UpdatedAt: day + 2*86400}).Error)
	complete, err := queryChannelMonitorHistoricalSuccessAnalytics(context.Background(), query)
	require.NoError(t, err)
	assert.Equal(t, service.ChannelMonitorCoverageComplete, complete.Coverage.Status, "checkpointed idle days are complete without recent business samples")
	require.NoError(t, db.Model(&model.ChannelMonitorDailyCheckpoint{}).Where("day_start = ?", day).Update("coverage_partial", true).Error)
	partial, err := queryChannelMonitorHistoricalSuccessAnalytics(context.Background(), query)
	require.NoError(t, err)
	assert.Equal(t, service.ChannelMonitorCoveragePartial, partial.Coverage.Status)
	assert.Contains(t, partial.Coverage.Reasons, "daily_replay_incomplete")
}

func TestChannelMonitorAnalyticsHistoricalSuccessCoverageRequiresDailyCheckpoints(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	day := model.ChannelDailyCostDayStart(common.GetTimestamp()) - 3*86400
	runChannelMonitorAnalyticsSuccessCoverageCases(t, db, day)
}
