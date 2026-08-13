package model

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedChannelModelDetectionCleanupRun(t *testing.T, db *gorm.DB, runID string, channelID int, status string, finishedAt int64, dispatchState string, settlementStatus string) {
	t.Helper()
	run := ChannelModelDetectionRun{
		RunId: runID, ChannelId: channelID, ConfigRevision: 1, GlobalConfigRevision: 1,
		Trigger: ChannelModelDetectionTriggerManual, Preset: ChannelModelDetectionPresetLow,
		Status: status, TargetCount: 1, CompletedTargetCount: 1,
		FinishedAt: finishedAt, CreatedAt: finishedAt - 10, UpdatedAt: finishedAt,
	}
	require.NoError(t, db.Create(&run).Error)
	execution := ChannelModelDetectionExecution{
		RunId: runID, TargetKey: runID + "-target", TargetId: run.Id, ChannelId: channelID,
		RequestModel: "gpt-5.6", ClaimedModel: ChannelModelDetectionClaimedModelSol,
		Preset: ChannelModelDetectionPresetLow, Status: ChannelModelDetectionExecutionStatusCompleted,
		FinishedAt: finishedAt, CreatedAt: finishedAt - 10, UpdatedAt: finishedAt,
	}
	require.NoError(t, db.Create(&execution).Error)
	if dispatchState == "" {
		return
	}
	event := ChannelModelDetectionCostEvent{
		RunId: runID, TargetId: execution.TargetId, ExecutionId: execution.Id, ChannelId: channelID,
		RequestModel: execution.RequestModel, ClaimedModel: execution.ClaimedModel, Preset: execution.Preset,
		DetectorRequestId: runID + "-request", AttemptNo: 1,
		DispatchState: dispatchState, SettlementStatus: settlementStatus,
		UsageSource: ChannelModelDetectionUsageUnavailable, CostScope: ChannelModelDetectionCostScopeChannelUpstreamAPI,
		CreatedAt: finishedAt - 5, UpdatedAt: finishedAt,
	}
	if settlementStatus == ChannelModelDetectionSettlementSettled {
		zero := int64(0)
		event.SettledQuota = &zero
	}
	require.NoError(t, db.Create(&event).Error)
}

func TestChannelModelDetectionHistoryCleanupDeletesOnlyTerminalResolvedRuns(t *testing.T) {
	db := setupChannelModelDetectionTestDB(t)
	originalDB := DB
	DB = db
	t.Cleanup(func() { DB = originalDB })

	seedChannelModelDetectionCleanupRun(t, db, "expired-settled", 101, ChannelModelDetectionRunStatusCompleted, 90, ChannelModelDetectionDispatchDispatched, ChannelModelDetectionSettlementSettled)
	seedChannelModelDetectionCleanupRun(t, db, "cutoff-exact", 102, ChannelModelDetectionRunStatusCompleted, 100, ChannelModelDetectionDispatchDispatched, ChannelModelDetectionSettlementSettled)
	seedChannelModelDetectionCleanupRun(t, db, "active", 103, ChannelModelDetectionRunStatusRunning, 90, "", "")
	seedChannelModelDetectionCleanupRun(t, db, "prepared", 104, ChannelModelDetectionRunStatusFailed, 90, ChannelModelDetectionDispatchPrepared, ChannelModelDetectionSettlementPending)
	seedChannelModelDetectionCleanupRun(t, db, "pending", 105, ChannelModelDetectionRunStatusPartial, 90, ChannelModelDetectionDispatchDispatched, ChannelModelDetectionSettlementPending)

	result, err := DeleteChannelModelDetectionHistoryBefore(context.Background(), 100, 1, ChannelMonitorCleanupBudget{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, result.RunRowsDeleted)
	assert.EqualValues(t, 1, result.ExecutionRowsDeleted)
	assert.EqualValues(t, 1, result.CostEventRowsDeleted)
	assert.False(t, result.Incomplete)

	for _, runID := range []string{"cutoff-exact", "active", "prepared", "pending"} {
		var count int64
		require.NoError(t, db.Model(&ChannelModelDetectionRun{}).Where("run_id = ?", runID).Count(&count).Error)
		assert.EqualValues(t, 1, count, runID)
	}
	var deleted int64
	require.NoError(t, db.Model(&ChannelModelDetectionRun{}).Where("run_id = ?", "expired-settled").Count(&deleted).Error)
	assert.Zero(t, deleted)
}

func TestChannelModelDetectionHistoryCleanupHonorsBudgetAndArguments(t *testing.T) {
	db := setupChannelModelDetectionTestDB(t)
	originalDB := DB
	DB = db
	t.Cleanup(func() { DB = originalDB })
	seedChannelModelDetectionCleanupRun(t, db, "expired", 201, ChannelModelDetectionRunStatusCanceled, 90, "", "")

	exhausted := ChannelMonitorCleanupBudget{deadline: time.Now().Add(-time.Second)}
	result, err := DeleteChannelModelDetectionHistoryBefore(context.Background(), 100, 1, exhausted)
	require.NoError(t, err)
	assert.True(t, result.Incomplete)
	assert.Zero(t, result.RunRowsDeleted)

	_, err = DeleteChannelModelDetectionHistoryBefore(context.Background(), 0, 1, ChannelMonitorCleanupBudget{})
	assert.Error(t, err)
	_, err = DeleteChannelModelDetectionHistoryBefore(context.Background(), 100, 0, ChannelMonitorCleanupBudget{})
	assert.Error(t, err)
}

func TestDeleteChannelModelDetectionDataRejectsActiveRunAndKeepsBatch(t *testing.T) {
	db := setupChannelModelDetectionTestDB(t)
	config := ChannelModelDetectionConfig{ChannelId: 301}
	require.NoError(t, db.Create(&config).Error)
	target := ChannelModelDetectionTarget{ConfigId: config.Id, ChannelId: 301, RequestModel: "gpt-5.6", ClaimedModel: ChannelModelDetectionClaimedModelSol, Enabled: true}
	require.NoError(t, db.Create(&target).Error)
	batch := ChannelModelDetectionBatch{BatchId: "kept-batch", Preset: ChannelModelDetectionPresetLow, ScheduledFor: 100, Status: ChannelModelDetectionBatchStatusCompleted, ChannelCount: 1, RunCount: 1}
	require.NoError(t, db.Create(&batch).Error)
	run := ChannelModelDetectionRun{RunId: "active-run", BatchId: &batch.BatchId, ChannelId: 301, ConfigRevision: 1, GlobalConfigRevision: 1, Trigger: ChannelModelDetectionTriggerScheduled, Preset: ChannelModelDetectionPresetLow, Status: ChannelModelDetectionRunStatusRunning}
	require.NoError(t, db.Create(&run).Error)

	err := db.Transaction(func(tx *gorm.DB) error { return deleteChannelModelDetectionDataTx(tx, []int{301}) })
	assert.ErrorIs(t, err, ErrChannelModelDetectionChannelActive)

	require.NoError(t, db.Model(&ChannelModelDetectionRun{}).Where("run_id = ?", run.RunId).Updates(map[string]any{"status": ChannelModelDetectionRunStatusCanceled, "finished_at": int64(100)}).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return deleteChannelModelDetectionDataTx(tx, []int{301}) }))
	for _, table := range []any{&ChannelModelDetectionConfig{}, &ChannelModelDetectionTarget{}, &ChannelModelDetectionRun{}, &ChannelModelDetectionExecution{}, &ChannelModelDetectionCostEvent{}} {
		var count int64
		require.NoError(t, db.Model(table).Where("channel_id = ?", 301).Count(&count).Error)
		assert.Zero(t, count, "%T", table)
	}
	var batchCount int64
	require.NoError(t, db.Model(&ChannelModelDetectionBatch{}).Where("batch_id = ?", batch.BatchId).Count(&batchCount).Error)
	assert.EqualValues(t, 1, batchCount)
}
