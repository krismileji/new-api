package controller

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorAnalyticsDatabaseMatrix(t *testing.T) {
	if os.Getenv("CHANNEL_MONITOR_CONSISTENCY_DIALECT") == "" {
		t.Skip("使用渠道监控专用三数据库环境运行")
	}
	dsn, path := os.Getenv("SQL_DSN"), os.Getenv("CM_UPGRADE_SQLITE_PATH")
	require.True(t, strings.Contains(dsn, "new_api_cm_schema_") || strings.Contains(path, "cm-consistency-verification"))
	oldDB, oldLogDB, oldMaster, oldRedis, oldPath := model.DB, model.LOG_DB, common.IsMasterNode, common.RedisEnabled, common.SQLitePath
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	common.IsMasterNode, common.RedisEnabled = true, false
	if path != "" {
		common.SQLitePath = path
	}
	require.NoError(t, model.InitDB())
	db := model.DB
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, sqlDB.Close())
		model.DB, model.LOG_DB, common.IsMasterNode, common.RedisEnabled, common.SQLitePath = oldDB, oldLogDB, oldMaster, oldRedis, oldPath
		common.SetDatabaseTypes(oldMainType, oldLogType)
	})
	for _, table := range []any{&model.ChannelDailyCost{}, &model.ChannelDailyCostOutbox{}, &model.ChannelMonitorDailyCostDetail{}} {
		require.NoError(t, db.Where("channel_id IN ?", []int{900201, 900202}).Delete(table).Error)
	}
	const at = int64(1750000000)
	day := model.ChannelDailyCostDayStart(at)
	ctx := context.Background()
	require.NoError(t, model.AddChannelDailyCostBatch(ctx, []model.ChannelDailyCostDelta{
		{ChannelId: 900201, UserId: 900002, UserAttribution: "request", ModelName: "model-a", OccurredAt: at, CostNanoCNY: 200, SettledDelta: 1},
		{ChannelId: 900202, UserId: 900002, UserAttribution: "request", ModelName: "model-a", OccurredAt: at, CostNanoCNY: 100, SettledDelta: 1},
		{ChannelId: 900201, UserId: 900002, UserAttribution: "request", ModelName: "model-a", OccurredAt: at + 86400, CostNanoCNY: 300, SettledDelta: 1},
	}))
	query := channelMonitorAnalyticsQuery{Metric: "cost", GroupBy: "channel", From: day, To: day + 2*86400, Channel: 900201, Sort: "cost", Direction: "desc", Page: 1, PageSize: 1}
	result, err := queryChannelMonitorHistoricalCostAnalytics(ctx, query)
	require.NoError(t, err)
	assert.Equal(t, int64(500), result.Summary["cost_nano_cny"])
	// The filtered detail path must preserve user/model filters when grouped
	// by day, and summary totals must not inherit pagination or GROUP BY.
	query.GroupBy, query.User, query.Model = "day", 900002, "model-a"
	result, err = queryChannelMonitorHistoricalCostAnalytics(ctx, query)
	require.NoError(t, err)
	assert.Equal(t, int64(2), result.Total)
	require.Len(t, result.Items, 1)
	assert.Equal(t, int64(500), result.Summary["cost_nano_cny"])
	assert.Equal(t, int64(300), result.Items[0]["cost_nano_cny"])
	query.User = 900003
	result, err = queryChannelMonitorHistoricalCostAnalytics(ctx, query)
	require.NoError(t, err)
	assert.Zero(t, result.Total)
	assert.Equal(t, int64(0), result.Summary["cost_nano_cny"])
	require.NoError(t, db.Where("day_start = ?", day).Assign(model.ChannelMonitorDailyCheckpoint{CoveragePartial: true}).FirstOrCreate(&model.ChannelMonitorDailyCheckpoint{DayStart: day}).Error)
	query.Metric, query.Sort = "success", "samples"
	result, err = queryChannelMonitorHistoricalSuccessAnalytics(ctx, query)
	require.NoError(t, err)
	assert.Contains(t, result.Coverage.Reasons, "daily_replay_incomplete", "recovery gaps must remain visible after the day becomes historical")
}
