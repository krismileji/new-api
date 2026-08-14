package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelModelDetectionSettingsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-model-detection-settings.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelModelDetectionGlobalConfig{},
		&model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionExecution{},
		&model.ChannelModelDetectionRun{},
	))
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func seedChannelModelDetectionSettings(t *testing.T, db *gorm.DB, detectorURL string) model.ChannelModelDetectionGlobalConfig {
	t.Helper()
	config := model.ChannelModelDetectionGlobalConfig{
		Id: model.ChannelModelDetectionConfigID, DetectorURL: detectorURL,
		ScheduledPreset: model.ChannelModelDetectionPresetMedium, ScheduleEnabled: false,
		IntervalHours: 24, ScheduleTime: "02:30", Timezone: "Asia/Shanghai", Revision: 1,
		CreatedAt: 1, UpdatedAt: 1,
	}
	require.NoError(t, db.Create(&config).Error)
	return config
}

func TestValidateChannelModelDetectorTargetAllowsLoopbackAndRejectsPublicOrDynamicTargets(t *testing.T) {
	for _, value := range []string{"http://127.0.0.1:18080/", "https://[::1]:9443/proxy"} {
		normalized, err := ValidateChannelModelDetectorTarget(context.Background(), value)
		require.NoError(t, err, value)
		assert.NotEmpty(t, normalized)
	}
	for _, value := range []string{"https://example.com", "http://169.254.169.254", "http://127.0.0.1:1?x=1", "http://user:pass@127.0.0.1"} {
		_, err := ValidateChannelModelDetectorTarget(context.Background(), value)
		assert.Error(t, err, value)
	}
}

func TestUpdateChannelModelDetectionSettingsUsesRevisionAndHighConfirmation(t *testing.T) {
	db := setupChannelModelDetectionSettingsTestDB(t)
	seed := seedChannelModelDetectionSettings(t, db, "http://127.0.0.1:18080")
	now := time.Unix(1_700_000_000, 0).UTC()
	base := ChannelModelDetectionSettingsUpdate{
		ScheduledPreset: model.ChannelModelDetectionPresetHigh, ConfirmHighCost: false,
		ScheduleEnabled: true, IntervalHours: 24, ScheduleTime: "03:15", Timezone: "Asia/Shanghai", ExpectedRevision: seed.Revision,
	}
	_, err := UpdateChannelModelDetectionSettings(context.Background(), db, base, now)
	assert.ErrorIs(t, err, model.ErrChannelModelDetectionScheduledHighUnconfirmed)

	base.ConfirmHighCost = true
	updated, err := UpdateChannelModelDetectionSettings(context.Background(), db, base, now)
	require.NoError(t, err)
	assert.Equal(t, int64(2), updated.Revision)
	assert.NotZero(t, updated.NextBatchAt)
	assert.NotContains(t, string(mustMarshalSettings(t, updated)), "session")

	base.ExpectedRevision = seed.Revision
	base.ScheduledPreset = model.ChannelModelDetectionPresetMedium
	base.ConfirmHighCost = false
	_, err = UpdateChannelModelDetectionSettings(context.Background(), db, base, now)
	assert.ErrorIs(t, err, ErrChannelModelDetectionSettingsConflict)
}

func TestUpdateChannelModelDetectionSettingsDefersAddressWhileSessionActive(t *testing.T) {
	db := setupChannelModelDetectionSettingsTestDB(t)
	seed := seedChannelModelDetectionSettings(t, db, "http://127.0.0.1:18080")
	run := model.ChannelModelDetectionRun{RunId: "settings-active-run", ChannelId: 7, Trigger: model.ChannelModelDetectionTriggerManual, Preset: model.ChannelModelDetectionPresetLow, PresetSource: model.ChannelModelDetectionPresetSourceManualSelected, Status: model.ChannelModelDetectionRunStatusRunning}
	require.NoError(t, db.Create(&run).Error)
	require.NoError(t, db.Create(&model.ChannelModelDetectionExecution{RunId: run.RunId, TargetKey: "settings-target", TargetId: 1, ChannelId: 7, RequestModel: "gpt-5.6-sol", ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Preset: run.Preset, Status: model.ChannelModelDetectionExecutionStatusRunning, OfficialSessionId: "official-session"}).Error)
	url := "http://127.0.0.1:18081"
	updated, err := UpdateChannelModelDetectionSettings(context.Background(), db, ChannelModelDetectionSettingsUpdate{
		DetectorURL: &url, ScheduledPreset: model.ChannelModelDetectionPresetMedium, ScheduleEnabled: false,
		IntervalHours: 24, ScheduleTime: "02:30", Timezone: "Asia/Shanghai", ExpectedRevision: seed.Revision,
	}, time.Unix(1_700_000_000, 0).UTC())
	require.NoError(t, err)
	assert.True(t, updated.ConnectionTestRequired)
	assert.Equal(t, "http://127.0.0.1:18081", updated.DetectorURLMasked)
	assert.True(t, updated.PendingDetectorURLConfigured)
	assert.Equal(t, "http://127.0.0.1:18081", updated.PendingDetectorURLMasked)
	var stored model.ChannelModelDetectionGlobalConfig
	require.NoError(t, db.First(&stored, model.ChannelModelDetectionConfigID).Error)
	assert.Equal(t, "http://127.0.0.1:18080", stored.DetectorURL)
	assert.Equal(t, "http://127.0.0.1:18081", stored.PendingDetectorURL)
}

func TestTestChannelModelDetectionServiceDoesNotExposeSessionToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case channelModelDetectorHealthPath:
			_, _ = writer.Write([]byte(`{"status":"ok"}`))
		case channelModelDetectorBootstrapPath:
			preset := `{"mode":"single","preset":"low","workers":1,"config_hash":"hash"}`
			_, _ = writer.Write([]byte(`{"session_token":"secret-session","single_presets":{"low":` + preset + `,"medium":` + preset + `,"high":` + preset + `}}`))
		case channelModelDetectorEstimatePath:
			_, _ = writer.Write([]byte(`{"total_requests":2,"fixed_32k_requests":1}`))
		case channelModelDetectorStatusPath:
			_, _ = writer.Write([]byte(`{"status":"idle"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	db := setupChannelModelDetectionSettingsTestDB(t)
	seedChannelModelDetectionSettings(t, db, server.URL)
	response, err := TestChannelModelDetectionService(context.Background(), db, time.Unix(1_700_000_000, 0).UTC(), ChannelModelDetectorClientOptions{HTTPClient: server.Client()})
	require.NoError(t, err)
	assert.Equal(t, "available", response.State)
	encoded, err := common.Marshal(response)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "secret-session")
	assert.Contains(t, string(encoded), server.URL)
	assert.NotContains(t, string(encoded), "session_token")
}

func mustMarshalSettings(t *testing.T, value ChannelModelDetectionSettingsResponse) []byte {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	return data
}

func TestMaskChannelModelDetectorURLReturnsConfiguredAddress(t *testing.T) {
	configured := " https://detector.internal:9443/private/path "
	assert.Equal(t, strings.TrimSpace(configured), MaskChannelModelDetectorURL(configured))
}
