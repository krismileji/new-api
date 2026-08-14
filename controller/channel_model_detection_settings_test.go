package controller

import (
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

func setupChannelModelDetectionSettingsControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-model-detection-settings-controller.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelModelDetectionGlobalConfig{},
		&model.ChannelModelDetectionExecution{},
	))
	previousDB := model.DB
	model.DB = db
	service.ResetChannelModelDetectionServiceCache()
	t.Cleanup(func() {
		model.DB = previousDB
		service.ResetChannelModelDetectionServiceCache()
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestChannelModelDetectionSettingsAPIShowsConfiguredURLAndReturnsRevisionConflict(t *testing.T) {
	db := setupChannelModelDetectionSettingsControllerTest(t)
	require.NoError(t, db.Create(&model.ChannelModelDetectionGlobalConfig{
		Id: model.ChannelModelDetectionConfigID, DetectorURL: "http://127.0.0.1:18080/private",
		ScheduledPreset: model.ChannelModelDetectionPresetMedium, IntervalHours: 24,
		ScheduleTime: "02:30", Timezone: "Asia/Shanghai", Revision: 3,
	}).Error)

	getContext, getRecorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/model_detection/settings", nil)
	GetChannelModelDetectionSettings(getContext)
	require.Equal(t, http.StatusOK, getRecorder.Code)
	assert.Contains(t, getRecorder.Body.String(), `"detector_url_masked":"http://127.0.0.1:18080/private"`)

	putContext, putRecorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/model_detection/settings", map[string]any{
		"scheduled_preset": "medium", "schedule_enabled": false, "interval_hours": 24,
		"schedule_time": "02:30", "timezone": "Asia/Shanghai", "revision": 2,
	})
	UpdateChannelModelDetectionSettings(putContext)
	assert.Equal(t, http.StatusConflict, putRecorder.Code)
	assert.Contains(t, putRecorder.Body.String(), `"code":"revision_conflict"`)
}

func TestChannelModelDetectionSettingsAPIRejectsAmbiguousAddressAndUnconfirmedHigh(t *testing.T) {
	db := setupChannelModelDetectionSettingsControllerTest(t)
	require.NoError(t, db.Create(&model.ChannelModelDetectionGlobalConfig{
		Id: model.ChannelModelDetectionConfigID, DetectorURL: "http://127.0.0.1:18080",
		ScheduledPreset: model.ChannelModelDetectionPresetMedium, IntervalHours: 24,
		ScheduleTime: "02:30", Timezone: "Asia/Shanghai", Revision: 1,
	}).Error)

	for _, request := range []map[string]any{
		{
			"detector_url": "http://127.0.0.1:18081", "clear_detector_url": true,
			"scheduled_preset": "medium", "schedule_enabled": false, "interval_hours": 24,
			"schedule_time": "02:30", "timezone": "Asia/Shanghai", "revision": 1,
		},
		{
			"scheduled_preset": "high", "schedule_enabled": true, "interval_hours": 24,
			"schedule_time": "02:30", "timezone": "Asia/Shanghai", "revision": 1,
		},
	} {
		context, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/model_detection/settings", request)
		UpdateChannelModelDetectionSettings(context)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		assert.Contains(t, recorder.Body.String(), `"success":false`)
	}

	var stored model.ChannelModelDetectionGlobalConfig
	require.NoError(t, db.First(&stored, model.ChannelModelDetectionConfigID).Error)
	assert.Equal(t, int64(1), stored.Revision)
}

func TestChannelModelDetectionServiceAPIUnconfiguredResponseHasNoSecrets(t *testing.T) {
	setupChannelModelDetectionSettingsControllerTest(t)
	context, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/model_detection/service", nil)
	GetChannelModelDetectionService(context)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"state":"unconfigured"`)
	assert.NotContains(t, recorder.Body.String(), "session_token")
	assert.NotContains(t, recorder.Body.String(), common.GetUUID())
}
