package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelModelDetectionCancelRunsForChannelsCancelsOnlySelectedActiveRuns(t *testing.T) {
	db := setupChannelModelDetectionRunAPITestDB(t)
	seedChannelModelDetectionRunAPI(t, db, 401, model.ChannelModelDetectionPresetLow)
	require.NoError(t, db.Create(&model.Channel{Id: 402, Name: "other", Status: common.ChannelStatusEnabled}).Error)
	config := model.ChannelModelDetectionConfig{ChannelId: 402}
	require.NoError(t, db.Create(&config).Error)
	target := model.ChannelModelDetectionTarget{ConfigId: config.Id, ChannelId: 402, RequestModel: "gpt-5.6", ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Enabled: true}
	require.NoError(t, db.Create(&target).Error)

	selected, err := CreateChannelModelDetectionManualRun(context.Background(), db, ChannelModelDetectionManualRunInput{ChannelID: 401, Preset: model.ChannelModelDetectionPresetLow}, time.Now())
	require.NoError(t, err)
	other, err := CreateChannelModelDetectionManualRun(context.Background(), db, ChannelModelDetectionManualRunInput{ChannelID: 402, Preset: model.ChannelModelDetectionPresetLow}, time.Now())
	require.NoError(t, err)
	stub := &channelModelDetectionRunCancelerStub{db: db, status: model.ChannelModelDetectionRunStatusCanceled}
	restore := SetChannelModelDetectionRunCancelerFactory(func(*gorm.DB) (ChannelModelDetectionRunCanceler, error) { return stub, nil })
	t.Cleanup(restore)

	require.NoError(t, CancelChannelModelDetectionRunsForChannels(context.Background(), db, []int{401, 401, 0}))
	assert.Equal(t, 1, stub.calls)
	assert.Equal(t, selected.RunId, stub.runID)
	var otherStatus string
	require.NoError(t, db.Model(&model.ChannelModelDetectionRun{}).Where("run_id = ?", other.RunId).Pluck("status", &otherStatus).Error)
	assert.Equal(t, model.ChannelModelDetectionRunStatusQueued, otherStatus)
}

func TestChannelModelDetectionCancelRunsForStatusesPropagatesCancellationFailure(t *testing.T) {
	db := setupChannelModelDetectionRunAPITestDB(t)
	seedChannelModelDetectionRunAPI(t, db, 403, model.ChannelModelDetectionPresetLow)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 403).Update("status", common.ChannelStatusManuallyDisabled).Error)
	_, err := CreateChannelModelDetectionManualRun(context.Background(), db, ChannelModelDetectionManualRunInput{ChannelID: 403, Preset: model.ChannelModelDetectionPresetLow}, time.Now())
	require.NoError(t, err)
	want := errors.New("stop failed")
	restore := SetChannelModelDetectionRunCancelerFactory(func(*gorm.DB) (ChannelModelDetectionRunCanceler, error) {
		return &channelModelDetectionRunCancelerStub{db: db, err: want}, nil
	})
	t.Cleanup(restore)

	err = CancelChannelModelDetectionRunsForStatuses(context.Background(), db, []int64{common.ChannelStatusManuallyDisabled})
	assert.ErrorIs(t, err, want)
}

func TestChannelModelDetectionCancelRunsForDeletedFrozenOrActualMember(t *testing.T) {
	db := setupChannelModelDetectionRunAPITestDB(t)
	run := model.ChannelModelDetectionRun{
		RunId: "grouped-delete-run", ChannelId: 410, LogicalChannelID: 900, LogicalRevision: 4,
		Trigger: model.ChannelModelDetectionTriggerManual, Preset: model.ChannelModelDetectionPresetLow,
		PresetSource: model.ChannelModelDetectionPresetSourceManualSelected, Status: model.ChannelModelDetectionRunStatusRunning,
	}
	require.NoError(t, run.SetLogicalMemberSnapshot([]model.ChannelModelDetectionMemberSnapshot{{ChannelID: 410, Weight: 1}, {ChannelID: 411, Weight: 1}}))
	require.NoError(t, db.Create(&run).Error)
	require.NoError(t, db.Create(&model.ChannelModelDetectionExecution{
		RunId: run.RunId, TargetKey: "grouped-delete-target", TargetId: 1, ChannelId: 411,
		LogicalChannelID: run.LogicalChannelID, LogicalRevision: run.LogicalRevision, RequestModel: "model",
		ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Preset: run.Preset, Status: model.ChannelModelDetectionExecutionStatusRunning,
	}).Error)
	stub := &channelModelDetectionRunCancelerStub{db: db, status: model.ChannelModelDetectionRunStatusCanceled}
	restore := SetChannelModelDetectionRunCancelerFactory(func(*gorm.DB) (ChannelModelDetectionRunCanceler, error) { return stub, nil })
	t.Cleanup(restore)

	require.NoError(t, CancelChannelModelDetectionRunsForChannels(context.Background(), db, []int{411}))
	assert.Equal(t, 1, stub.calls)
	assert.Equal(t, run.RunId, stub.runID)
}
