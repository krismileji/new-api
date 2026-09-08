package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelSmartScheduleMonitorUsesPublishedEconomicsAndRoutesWithoutSQL(t *testing.T) {
	db, _, client := setupChannelSmartScheduleRedisSnapshotTest(t)
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = true
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })
	seedChannelSmartScheduleRedisSnapshotTest(t, db)
	score := 0.84
	details := &ChannelSmartScheduleScoreDetails{
		Version: ChannelSmartScheduleScoreDetailsVersion, FinalScore: &score,
		Decision: ChannelSmartScheduleScoreDecision{SelectedPrimaryChannelId: 9701},
	}
	encodedDetails, err := EncodeChannelSmartScheduleScoreDetails(details)
	require.NoError(t, err)
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).
		Where("channel_id = ?", 9701).Update("last_schedule_score_details", encodedDetails).Error)
	require.NoError(t, db.AutoMigrate(&ChannelRatioMonitor{}, &Option{}))
	require.NoError(t, db.Create(&ChannelRatioMonitor{ChannelId: 9701, Ratio: 0.5, CostConversion: "1"}).Error)
	require.NoError(t, db.Save(&Option{Key: "GroupRatio", Value: `{"vip":2}`}).Error)
	ctx := context.Background()
	require.NoError(t, publishChannelSmartScheduleRouteSnapshot(ctx))
	revision := GetChannelSmartScheduleRouteSnapshotStatus().Revision
	assert.True(t, client.Exists(ctx, channelSmartScheduleRouteSnapshotVersionKey(revision)+":monitor").Val() > 0)
	// Configuration changed in SQL but is not yet published: the page must
	// still describe the exact routing/economic snapshot serving requests.
	require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", 9701).Update("weight", 999).Error)
	require.NoError(t, db.Model(&ChannelRatioMonitor{}).Where("channel_id = ?", 9701).Update("ratio", 9).Error)
	channelSyncLock.Lock()
	channelSmartScheduleMonitorReadCache = nil
	channelSyncLock.Unlock()
	require.NoError(t, loadChannelSmartScheduleRouteSnapshot(ctx), "reload a missing companion even when the route version is unchanged")
	queries := 0
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:monitor_shared_snapshot_sql", func(tx *gorm.DB) {
		queries++
		tx.AddError(assert.AnError)
	}))
	t.Cleanup(func() { assert.NoError(t, db.Callback().Query().Remove("test:monitor_shared_snapshot_sql")) })
	routes, err := GetChannelSmartScheduleRoutesWithContext(ctx)
	require.NoError(t, err)
	require.Len(t, routes, 1)
	assert.EqualValues(t, 61, routes[0].Weight)
	decodedDetails, err := routes[0].State.LastScheduleScoreDetails.Decode()
	require.NoError(t, err)
	assert.Equal(t, details, decodedDetails)
	economics, err := GetChannelSmartScheduleEconomicSnapshotWithContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2.0, economics.GroupRatios["vip"])
	require.Len(t, economics.Monitors, 1)
	assert.Equal(t, 0.5, economics.Monitors[0].Ratio)
	rows, views, status, err := GetChannelSmartScheduleMonitorRuntimeSnapshot(ctx, routes)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, revision, status.Revision)
	require.NotNil(t, status.MonitorEconomics)
	assert.Equal(t, 0.5, status.MonitorEconomics.Monitors[0].Ratio)
	assert.EqualValues(t, 61, views[channelSmartScheduleRouteKey(9701, "vip", "model-a")].Weight)
	// Read results are detached from the shared snapshot.
	routes[0].State.StabilityReleaseMaxPromptTokens = 0
	economics.GroupRatios["vip"] = 100
	again, err := GetChannelSmartScheduleRoutesWithContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, 4096, again[0].State.StabilityReleaseMaxPromptTokens)
	assert.Zero(t, queries)
}
