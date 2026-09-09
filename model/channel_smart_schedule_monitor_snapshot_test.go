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
		Where("channel_id = ?", 9701).Updates(map[string]any{
		"last_schedule_score_details": encodedDetails,
		"last_schedule_score":         score, "revision": 7,
		"manual_primary_saved": true, "manual_primary_saved_priority": 23,
		"manual_primary_saved_weight": 45,
	}).Error)
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
	assert.EqualValues(t, 7, routes[0].State.Revision)
	assert.True(t, routes[0].State.ManualPrimarySaved)
	assert.EqualValues(t, 23, routes[0].State.ManualPrimarySavedPriority)
	assert.EqualValues(t, 45, routes[0].State.ManualPrimarySavedWeight)
	apiPayload, err := common.Marshal(routes)
	require.NoError(t, err)
	assert.NotContains(t, string(apiPayload), "state_revision")
	assert.NotContains(t, string(apiPayload), "manual_primary_saved")
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
	require.NotNil(t, routes[0].State.LastScheduleScore)
	*routes[0].State.LastScheduleScore = 0
	economics.GroupRatios["vip"] = 100
	again, err := GetChannelSmartScheduleRoutesWithContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, 4096, again[0].State.StabilityReleaseMaxPromptTokens)
	require.NotNil(t, again[0].State.LastScheduleScore)
	assert.Equal(t, score, *again[0].State.LastScheduleScore)
	assert.Zero(t, queries)
}

func TestChannelSmartScheduleMonitorPublishedRoutesApplyWithGuards(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			for _, reload := range []bool{false, true} {
				name := "local"
				if reload {
					name = "redis_reload"
				}
				t.Run(name, func(t *testing.T) {
					db, _, _ := setupChannelSmartScheduleRedisSnapshotTest(t)
					db = setupChannelSmartScheduleMonitorSnapshotMatrixDB(t, engine, db)
					runChannelSmartScheduleMonitorPublishedRouteGuards(t, db, reload)
				})
			}
		})
	}
}

func runChannelSmartScheduleMonitorPublishedRouteGuards(t *testing.T, db *gorm.DB, reload bool) {
	t.Helper()
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = true
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })
	seedChannelSmartScheduleRedisSnapshotTest(t, db)
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).Where("channel_id = ?", 9701).
		Updates(map[string]any{"revision": 7, "stability_state": "", "stability_since": 0}).Error)
	require.NoError(t, db.Save(&Option{Key: ChannelSmartScheduleControlRevisionOption, Value: "control-v1"}).Error)
	require.NoError(t, db.Save(&Option{Key: ChannelMonitorEconomicRevisionOption, Value: "economic-v1"}).Error)
	ctx := context.Background()
	// A second publication must carry the revision advanced by the first run.
	for range 2 {
		require.NoError(t, publishChannelSmartScheduleRouteSnapshot(ctx))
		if reload {
			channelSyncLock.Lock()
			channelSmartScheduleMonitorReadCache = nil
			channelSyncLock.Unlock()
			require.NoError(t, loadChannelSmartScheduleRouteSnapshot(ctx))
		}
		routes, err := GetChannelSmartScheduleRoutesWithContext(ctx)
		require.NoError(t, err)
		require.Len(t, routes, 1)
		route := routes[0]
		controlRevision, err := GetChannelSmartScheduleControlRevision()
		require.NoError(t, err)
		economics, err := GetChannelSmartScheduleEconomicSnapshotWithContext(ctx)
		require.NoError(t, err)
		update := ChannelSmartScheduleRouteResultUpdate{
			ChannelId: route.ChannelId, Group: route.Group, Model: route.Model,
			Status: ChannelSmartScheduleStatusSucceeded, Time: common.GetTimestamp(),
			Priority: route.Priority + 1, Weight: route.Weight + 3, ApplyPriorityWeight: true,
			PoolGuard: true, ExpectedRevision: route.State.Revision,
			ExpectedControlRevision: controlRevision, ExpectedEconomicRevision: economics.Revision,
			ExpectedParticipationSet: route.State.ParticipationSet, ExpectedExcluded: route.State.Excluded,
			ExpectedAbilityEnabled: route.Enabled, ExpectedChannelStatus: route.ChannelStatus,
			ExpectedPriority: route.Priority, ExpectedWeight: route.Weight,
		}
		outcomes, err := ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{update})
		require.NoError(t, err)
		require.Len(t, outcomes, 1)
		require.True(t, outcomes[0].Applied, "unchanged configuration must accept a published route")
		assert.True(t, outcomes[0].RoutingChanged)
		var ability Ability
		require.NoError(t, db.Where(&Ability{ChannelId: route.ChannelId, Group: route.Group, Model: route.Model}).First(&ability).Error)
		require.NotNil(t, ability.Priority)
		assert.Equal(t, update.Priority, *ability.Priority)
		assert.Equal(t, update.Weight, ability.Weight)
		var state ChannelSmartScheduleRouteState
		require.NoError(t, db.Where("channel_id = ?", route.ChannelId).First(&state).Error)
		assert.Equal(t, route.State.Revision+1, state.Revision)
		assert.Equal(t, ChannelSmartScheduleStatusSucceeded, state.LastScheduleStatus)

		update.Priority++
		outcomes, err = ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{update})
		require.NoError(t, err)
		require.Len(t, outcomes, 1)
		assert.False(t, outcomes[0].Applied, "stale snapshots must still be rejected")
		var unchanged ChannelSmartScheduleRouteState
		require.NoError(t, db.Where("channel_id = ?", route.ChannelId).First(&unchanged).Error)
		assert.Equal(t, state, unchanged)
	}
}
