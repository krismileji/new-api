package controller

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelModelDetectionRunControllerTest(t *testing.T, channelID int, scheduledPreset string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-model-detection-run-controller.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.ChannelModelDetectionGlobalConfig{},
		&model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionTarget{},
		&model.ChannelModelDetectionRun{},
		&model.ChannelModelDetectionExecution{},
	))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.Create(&model.Channel{Id: channelID, Name: "controller-run", Key: "secret", Status: common.ChannelStatusEnabled}).Error)
	global := model.ChannelModelDetectionGlobalConfig{
		Id: model.ChannelModelDetectionConfigID, DetectorURL: "http://127.0.0.1:18080",
		ScheduledPreset: scheduledPreset, IntervalHours: 24, ScheduleTime: "02:30", Timezone: "Asia/Shanghai", Revision: 1,
	}
	if scheduledPreset == model.ChannelModelDetectionPresetHigh {
		global.ScheduledHighConfirmedRevision = 0
	}
	require.NoError(t, db.Create(&global).Error)
	config := model.ChannelModelDetectionConfig{ChannelId: channelID, Revision: 1}
	require.NoError(t, db.Create(&config).Error)
	require.NoError(t, db.Create(&model.ChannelModelDetectionTarget{
		ConfigId: config.Id, ChannelId: channelID, TargetKey: "controller-target",
		RequestModel: "gpt-5.6-sol", ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Enabled: true,
	}).Error)
	return db
}

func TestChannelModelDetectionManualRunAPIReturnsAcceptedAndFreezesManualSource(t *testing.T) {
	db := setupChannelModelDetectionRunControllerTest(t, 401, model.ChannelModelDetectionPresetMedium)
	context, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/model_detection/channel/401/run", map[string]any{})
	context.Params = append(context.Params, struct{ Key, Value string }{Key: "id", Value: "401"})
	StartChannelModelDetectionManualRun(context)
	require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"status":"queued"`)
	assert.Contains(t, recorder.Body.String(), `"preset":"medium"`)
	assert.Contains(t, recorder.Body.String(), `"preset_source":"manual_selected"`)
	assert.NotContains(t, recorder.Body.String(), "127.0.0.1")
	assert.NotContains(t, recorder.Body.String(), "secret")

	var global model.ChannelModelDetectionGlobalConfig
	require.NoError(t, db.First(&global, model.ChannelModelDetectionConfigID).Error)
	assert.Equal(t, int64(0), global.NextBatchAt)
}

func TestChannelModelDetectionManualRunAPIRequiresHighConfirmation(t *testing.T) {
	setupChannelModelDetectionRunControllerTest(t, 402, model.ChannelModelDetectionPresetMedium)
	context, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/model_detection/channel/402/run", map[string]any{"preset": "high"})
	context.Params = append(context.Params, struct{ Key, Value string }{Key: "id", Value: "402"})
	StartChannelModelDetectionManualRun(context)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "需要确认本次成本风险")
}

type channelModelDetectionControllerCanceler struct {
	db    *gorm.DB
	calls int
}

func (canceler *channelModelDetectionControllerCanceler) CancelRun(_ context.Context, runID string) error {
	canceler.calls++
	return canceler.db.Model(&model.ChannelModelDetectionRun{}).Where("run_id = ?", runID).Update("status", model.ChannelModelDetectionRunStatusCanceled).Error
}

func TestChannelModelDetectionCancelAPIIsIdempotentAndRejectsUnknownRun(t *testing.T) {
	db := setupChannelModelDetectionRunControllerTest(t, 403, model.ChannelModelDetectionPresetLow)
	startContext, startRecorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/model_detection/channel/403/run", map[string]any{"preset": "low"})
	startContext.Params = append(startContext.Params, struct{ Key, Value string }{Key: "id", Value: "403"})
	StartChannelModelDetectionManualRun(startContext)
	require.Equal(t, http.StatusAccepted, startRecorder.Code)
	var run model.ChannelModelDetectionRun
	require.NoError(t, db.First(&run).Error)
	canceler := &channelModelDetectionControllerCanceler{db: db}
	restore := service.SetChannelModelDetectionRunCancelerFactory(func(*gorm.DB) (service.ChannelModelDetectionRunCanceler, error) { return canceler, nil })
	t.Cleanup(restore)

	for range 2 {
		context, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/model_detection/runs/"+run.RunId+"/cancel", nil)
		context.Params = append(context.Params, struct{ Key, Value string }{Key: "run_id", Value: run.RunId})
		CancelChannelModelDetectionRun(context)
		require.Equal(t, http.StatusOK, recorder.Code)
		assert.Contains(t, recorder.Body.String(), `"status":"canceled"`)
		assert.NotContains(t, recorder.Body.String(), "127.0.0.1")
	}
	assert.Equal(t, 1, canceler.calls)

	context, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/model_detection/runs/unknown/cancel", nil)
	context.Params = append(context.Params, struct{ Key, Value string }{Key: "run_id", Value: "unknown"})
	CancelChannelModelDetectionRun(context)
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}
