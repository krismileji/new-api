package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelModelDetectionRunAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-model-detection-run-api.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.ChannelModelDetectionGlobalConfig{},
		&model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionTarget{},
		&model.ChannelModelDetectionRun{},
		&model.ChannelModelDetectionExecution{},
	))
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func seedChannelModelDetectionRunAPI(t *testing.T, db *gorm.DB, channelID int, preset string) model.ChannelModelDetectionGlobalConfig {
	t.Helper()
	require.NoError(t, db.Create(&model.Channel{Id: channelID, Name: "manual-run", Key: "secret", Status: common.ChannelStatusEnabled}).Error)
	global := model.ChannelModelDetectionGlobalConfig{
		Id: model.ChannelModelDetectionConfigID, DetectorURL: "http://127.0.0.1:18080",
		ScheduledPreset: preset, ScheduleEnabled: true, IntervalHours: 24,
		ScheduleTime: "02:30", Timezone: "Asia/Shanghai", ScheduleAnchorAt: 100, NextBatchAt: 200, Revision: 3,
	}
	if preset == model.ChannelModelDetectionPresetHigh {
		global.ScheduledHighConfirmedRevision = global.Revision
	}
	require.NoError(t, db.Create(&global).Error)
	config := model.ChannelModelDetectionConfig{ChannelId: channelID, Revision: 4}
	require.NoError(t, db.Create(&config).Error)
	require.NoError(t, db.Create(&model.ChannelModelDetectionTarget{
		ConfigId: config.Id, ChannelId: channelID, TargetKey: "manual-target", RequestModel: "gpt-5.6-sol",
		ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Enabled: true,
	}).Error)
	return global
}

func TestChannelModelDetectionManualRunDefaultsPresetWithoutChangingSchedule(t *testing.T) {
	db := setupChannelModelDetectionRunAPITestDB(t)
	global := seedChannelModelDetectionRunAPI(t, db, 301, model.ChannelModelDetectionPresetMedium)
	response, err := StartChannelModelDetectionManualRun(context.Background(), db, ChannelModelDetectionManualRunRequest{
		ChannelID: 301, CreatedByUserID: 1, CreatedByUsername: "root",
	}, time.Unix(1_700_000_000, 0).UTC())
	require.NoError(t, err)
	assert.Equal(t, model.ChannelModelDetectionPresetMedium, response.Preset)
	assert.Equal(t, model.ChannelModelDetectionPresetSourceManualSelected, response.PresetSource)
	assert.Equal(t, model.ChannelModelDetectionRunStatusQueued, response.Status)

	var after model.ChannelModelDetectionGlobalConfig
	require.NoError(t, db.First(&after, model.ChannelModelDetectionConfigID).Error)
	assert.Equal(t, global.NextBatchAt, after.NextBatchAt)
	assert.Equal(t, global.Revision, after.Revision)
	assert.Equal(t, global.ScheduledPreset, after.ScheduledPreset)
}

func TestChannelModelDetectionManualRunRequiresCurrentHighConfirmationAndConfiguration(t *testing.T) {
	db := setupChannelModelDetectionRunAPITestDB(t)
	seedChannelModelDetectionRunAPI(t, db, 302, model.ChannelModelDetectionPresetHigh)
	_, err := StartChannelModelDetectionManualRun(context.Background(), db, ChannelModelDetectionManualRunRequest{ChannelID: 302}, time.Now())
	assert.ErrorIs(t, err, ErrChannelModelDetectionManualHighUnconfirmed)

	response, err := StartChannelModelDetectionManualRun(context.Background(), db, ChannelModelDetectionManualRunRequest{ChannelID: 302, ConfirmHighCost: true}, time.Now())
	require.NoError(t, err)
	assert.Equal(t, model.ChannelModelDetectionPresetHigh, response.Preset)

	db2 := setupChannelModelDetectionRunAPITestDB(t)
	seedChannelModelDetectionRunAPI(t, db2, 303, model.ChannelModelDetectionPresetLow)
	require.NoError(t, db2.Model(&model.ChannelModelDetectionGlobalConfig{}).Where("id = ?", model.ChannelModelDetectionConfigID).Update("detector_url", "").Error)
	_, err = StartChannelModelDetectionManualRun(context.Background(), db2, ChannelModelDetectionManualRunRequest{ChannelID: 303}, time.Now())
	assert.ErrorIs(t, err, ErrChannelModelDetectionDetectorURLMissing)
}

type channelModelDetectionRunCancelerStub struct {
	db     *gorm.DB
	calls  int
	runID  string
	status string
	err    error
}

func (stub *channelModelDetectionRunCancelerStub) CancelRun(_ context.Context, runID string) error {
	stub.calls++
	stub.runID = runID
	if stub.err != nil {
		return stub.err
	}
	return stub.db.Model(&model.ChannelModelDetectionRun{}).Where("run_id = ?", runID).Update("status", stub.status).Error
}

func TestChannelModelDetectionCancelAPIUsesNarrowCancelerAndTerminalIsIdempotent(t *testing.T) {
	db := setupChannelModelDetectionRunAPITestDB(t)
	seedChannelModelDetectionRunAPI(t, db, 304, model.ChannelModelDetectionPresetLow)
	started, err := StartChannelModelDetectionManualRun(context.Background(), db, ChannelModelDetectionManualRunRequest{ChannelID: 304}, time.Now())
	require.NoError(t, err)
	stub := &channelModelDetectionRunCancelerStub{db: db, status: model.ChannelModelDetectionRunStatusCanceled}
	restore := SetChannelModelDetectionRunCancelerFactory(func(*gorm.DB) (ChannelModelDetectionRunCanceler, error) { return stub, nil })
	t.Cleanup(restore)

	response, err := CancelChannelModelDetectionRun(context.Background(), db, started.RunID)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelModelDetectionRunStatusCanceled, response.Status)
	assert.Equal(t, 1, stub.calls)
	assert.Equal(t, started.RunID, stub.runID)

	response, err = CancelChannelModelDetectionRun(context.Background(), db, started.RunID)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelModelDetectionRunStatusCanceled, response.Status)
	assert.Equal(t, 1, stub.calls)

	_, err = CancelChannelModelDetectionRun(context.Background(), db, "not-a-model-detection-run")
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestChannelModelDetectionCancelAPIPropagatesCancelerFailure(t *testing.T) {
	db := setupChannelModelDetectionRunAPITestDB(t)
	seedChannelModelDetectionRunAPI(t, db, 305, model.ChannelModelDetectionPresetLow)
	started, err := StartChannelModelDetectionManualRun(context.Background(), db, ChannelModelDetectionManualRunRequest{ChannelID: 305}, time.Now())
	require.NoError(t, err)
	want := errors.New("取消失败")
	restore := SetChannelModelDetectionRunCancelerFactory(func(*gorm.DB) (ChannelModelDetectionRunCanceler, error) {
		return &channelModelDetectionRunCancelerStub{db: db, err: want}, nil
	})
	t.Cleanup(restore)
	_, err = CancelChannelModelDetectionRun(context.Background(), db, started.RunID)
	assert.ErrorIs(t, err, want)
}
