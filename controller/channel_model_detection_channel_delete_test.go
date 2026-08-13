package controller

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelModelDetectionDeleteCanceler struct {
	db    *gorm.DB
	err   error
	calls int
}

func (canceler *channelModelDetectionDeleteCanceler) CancelRun(ctx context.Context, runID string) error {
	canceler.calls++
	if canceler.err != nil {
		return canceler.err
	}
	now := common.GetTimestamp()
	return canceler.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ChannelModelDetectionExecution{}).Where("run_id = ?", runID).
			Updates(map[string]any{"status": model.ChannelModelDetectionExecutionStatusCanceled, "finished_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ChannelModelDetectionRun{}).Where("run_id = ?", runID).
			Updates(map[string]any{"status": model.ChannelModelDetectionRunStatusCanceled, "finished_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&model.ChannelModelDetectionConfig{}).Where("running_run_id = ?", runID).
			Updates(map[string]any{"running_run_id": "", "updated_at": now}).Error
	})
}

func TestChannelModelDetectionDeleteChannelRequiresSuccessfulCancellation(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelModelDetectionGlobalConfig{},
		&model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionTarget{},
		&model.ChannelModelDetectionRun{},
		&model.ChannelModelDetectionExecution{},
		&model.ChannelModelDetectionCostEvent{},
	))

	const channelID = 98201
	require.NoError(t, db.Create(&model.Channel{
		Id: channelID, Name: "delete-integration", Key: "delete-secret", Status: common.ChannelStatusEnabled,
	}).Error)
	config := model.ChannelModelDetectionConfig{ChannelId: channelID, RunningRunId: "delete-active-run"}
	require.NoError(t, db.Create(&config).Error)
	target := model.ChannelModelDetectionTarget{
		ConfigId: config.Id, ChannelId: channelID, TargetKey: "delete-target", RequestModel: "delete-model",
		ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Enabled: true,
	}
	require.NoError(t, db.Create(&target).Error)
	run := model.ChannelModelDetectionRun{
		RunId: "delete-active-run", ChannelId: channelID, ConfigRevision: config.Revision, GlobalConfigRevision: 1,
		Trigger: model.ChannelModelDetectionTriggerManual, Preset: model.ChannelModelDetectionPresetLow,
		Status: model.ChannelModelDetectionRunStatusRunning, TargetCount: 1,
	}
	require.NoError(t, db.Create(&run).Error)
	require.NoError(t, db.Create(&model.ChannelModelDetectionExecution{
		RunId: run.RunId, TargetKey: target.TargetKey, TargetId: target.Id, ChannelId: channelID,
		RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel, Preset: run.Preset,
		Status: model.ChannelModelDetectionExecutionStatusRunning,
	}).Error)

	canceler := &channelModelDetectionDeleteCanceler{db: db, err: errors.New("official stop failed")}
	restore := service.SetChannelModelDetectionRunCancelerFactory(func(*gorm.DB) (service.ChannelModelDetectionRunCanceler, error) {
		return canceler, nil
	})
	t.Cleanup(restore)

	failedContext, failedRecorder := newChannelMonitorControllerContext(t, http.MethodDelete, "/api/channel/98201", nil)
	failedContext.Params = gin.Params{{Key: "id", Value: "98201"}}
	DeleteChannel(failedContext)

	require.Equal(t, http.StatusOK, failedRecorder.Code)
	assert.Contains(t, failedRecorder.Body.String(), `"success":false`)
	assert.Contains(t, failedRecorder.Body.String(), "删除渠道前取消模型检测轮次")
	assert.Equal(t, 1, canceler.calls)
	var channelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channelID).Count(&channelCount).Error)
	assert.EqualValues(t, 1, channelCount)

	canceler.err = nil
	successContext, successRecorder := newChannelMonitorControllerContext(t, http.MethodDelete, "/api/channel/98201", nil)
	successContext.Params = gin.Params{{Key: "id", Value: "98201"}}
	DeleteChannel(successContext)

	require.Equal(t, http.StatusOK, successRecorder.Code)
	assert.Contains(t, successRecorder.Body.String(), `"success":true`)
	assert.Equal(t, 2, canceler.calls)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channelID).Count(&channelCount).Error)
	assert.Zero(t, channelCount)
	for _, table := range []any{
		&model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionTarget{},
		&model.ChannelModelDetectionRun{},
		&model.ChannelModelDetectionExecution{},
		&model.ChannelModelDetectionCostEvent{},
	} {
		var count int64
		require.NoError(t, db.Model(table).Count(&count).Error)
		assert.Zero(t, count)
	}
}
