package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupChannelModelDetectionChannelControllerTest(t *testing.T) {
	t.Helper()
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelModelDetectionGlobalConfig{},
		&model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionTarget{},
		&model.ChannelModelDetectionRun{},
		&model.ChannelModelDetectionExecution{},
		&model.ChannelModelDetectionCostEvent{},
		&model.ChannelRatioMonitor{},
	))
	service.ResetChannelDailyCostSnapshotCache()
	t.Cleanup(service.ResetChannelDailyCostSnapshotCache)
}

func channelModelDetectionControllerContext(t *testing.T, method, target string, channelID int, body any) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	ctx, recorder := newChannelMonitorControllerContext(t, method, target, body)
	ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: strconv.Itoa(channelID)})
	return ctx, recorder
}

func TestChannelModelDetectionChannelConfigCreateUpdateAndCallback(t *testing.T) {
	setupChannelModelDetectionChannelControllerTest(t)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 801, Name: "config", Key: "config-secret", Models: "gpt-5.6-sol,gpt-5.6-terra", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, model.DB.Create(&model.ChannelModelDetectionGlobalConfig{DetectorURL: "http://127.0.0.1:18080/private", ScheduledPreset: model.ChannelModelDetectionPresetMedium, IntervalHours: 24, ScheduleTime: "02:30", Timezone: "Asia/Shanghai", Revision: 1}).Error)

	changes := make(chan service.ChannelModelDetectionConfigChange, 2)
	restore := service.SetChannelModelDetectionConfigChangeHook(func(_ context.Context, change service.ChannelModelDetectionConfigChange) { changes <- change })
	t.Cleanup(restore)
	createContext, createRecorder := channelModelDetectionControllerContext(t, http.MethodPut, "/api/channel_monitor/model_detection/channel/801/config", 801, map[string]any{
		"schedule_enabled": true,
		"targets":          []map[string]any{{"target_key": "", "request_model": "gpt-5.6-sol", "claimed_model": "gpt-5.6-sol"}},
		"revision":         0,
	})
	UpdateChannelModelDetectionConfig(createContext)
	require.Equal(t, http.StatusOK, createRecorder.Code, createRecorder.Body.String())
	assert.NotContains(t, createRecorder.Body.String(), "config-secret")
	assert.NotContains(t, createRecorder.Body.String(), "18080")
	assert.NotContains(t, createRecorder.Body.String(), "/private")
	var created struct {
		Data service.ChannelModelDetectionConfigResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal(createRecorder.Body.Bytes(), &created))
	require.Len(t, created.Data.Targets, 1)
	assert.NotEmpty(t, created.Data.Targets[0].TargetKey)
	assert.Equal(t, int64(1), created.Data.Revision)
	assert.Equal(t, service.ChannelModelDetectionConfigChange{ChannelID: 801, OldRevision: 0, NewRevision: 1}, <-changes)

	updateContext, updateRecorder := channelModelDetectionControllerContext(t, http.MethodPut, "/api/channel_monitor/model_detection/channel/801/config", 801, map[string]any{
		"schedule_enabled": false,
		"targets":          []map[string]any{{"target_key": created.Data.Targets[0].TargetKey, "request_model": "gpt-5.6-terra", "claimed_model": "gpt-5.6-terra"}},
		"revision":         1,
	})
	UpdateChannelModelDetectionConfig(updateContext)
	require.Equal(t, http.StatusOK, updateRecorder.Code, updateRecorder.Body.String())
	var updated struct {
		Data service.ChannelModelDetectionConfigResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal(updateRecorder.Body.Bytes(), &updated))
	assert.Equal(t, created.Data.Targets[0].TargetKey, updated.Data.Targets[0].TargetKey)
	assert.Equal(t, int64(2), updated.Data.Revision)
	assert.Equal(t, service.ChannelModelDetectionConfigChange{ChannelID: 801, OldRevision: 1, NewRevision: 2}, <-changes)
}

func TestChannelModelDetectionChannelConfigRejectsRevisionTargetsAndModels(t *testing.T) {
	setupChannelModelDetectionChannelControllerTest(t)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 802, Name: "validation", Key: "secret", Models: "gpt-5.6-sol,gpt-5.6-terra", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, model.DB.Create(&model.ChannelModelDetectionGlobalConfig{ScheduledPreset: model.ChannelModelDetectionPresetMedium, IntervalHours: 24, ScheduleTime: "02:30", Timezone: "Asia/Shanghai", Revision: 1}).Error)

	valid := []map[string]any{{"target_key": "", "request_model": "gpt-5.6-sol", "claimed_model": "gpt-5.6-sol"}}
	tests := []struct {
		name     string
		targets  []map[string]any
		schedule bool
	}{
		{name: "zero", targets: nil},
		{name: "eleven", targets: repeatDetectionTargets(11)},
		{name: "unsupported", targets: []map[string]any{{"target_key": "", "request_model": "gpt-5.6-luna", "claimed_model": "gpt-5.6-luna"}}},
		{name: "case", targets: []map[string]any{{"target_key": "", "request_model": "GPT-5.6-SOL", "claimed_model": "gpt-5.6-sol"}}},
		{name: "prefix", targets: []map[string]any{{"target_key": "", "request_model": "gpt-5.6", "claimed_model": "gpt-5.6-sol"}}},
		{name: "duplicate-key", targets: []map[string]any{{"target_key": "same", "request_model": "gpt-5.6-sol", "claimed_model": "gpt-5.6-sol"}, {"target_key": "same", "request_model": "gpt-5.6-terra", "claimed_model": "gpt-5.6-terra"}}},
		{name: "duplicate-pair", targets: []map[string]any{{"target_key": "", "request_model": "gpt-5.6-sol", "claimed_model": "gpt-5.6-sol"}, {"target_key": "", "request_model": "gpt-5.6-sol", "claimed_model": "gpt-5.6-sol"}}},
		{name: "claimed", targets: []map[string]any{{"target_key": "", "request_model": "gpt-5.6-sol", "claimed_model": "gpt-5.6-future"}}},
		{name: "schedule-detector", targets: valid, schedule: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, recorder := channelModelDetectionControllerContext(t, http.MethodPut, "/config", 802, map[string]any{"schedule_enabled": test.schedule, "targets": test.targets, "revision": 0})
			UpdateChannelModelDetectionConfig(ctx)
			assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			assert.False(t, strings.Contains(recorder.Body.String(), `"success":true`), recorder.Body.String())
		})
	}

	ctx, recorder := channelModelDetectionControllerContext(t, http.MethodPut, "/config", 802, map[string]any{"schedule_enabled": false, "targets": valid, "revision": 0})
	UpdateChannelModelDetectionConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	staleContext, staleRecorder := channelModelDetectionControllerContext(t, http.MethodPut, "/config", 802, map[string]any{"schedule_enabled": false, "targets": valid, "revision": 0})
	UpdateChannelModelDetectionConfig(staleContext)
	assert.Equal(t, http.StatusConflict, staleRecorder.Code)
	assert.Contains(t, staleRecorder.Body.String(), `"code":"revision_conflict"`)
}

func repeatDetectionTargets(count int) []map[string]any {
	targets := make([]map[string]any, 0, count)
	for index := 0; index < count; index++ {
		targets = append(targets, map[string]any{"target_key": "", "request_model": "gpt-5.6-sol", "claimed_model": "gpt-5.6-sol"})
	}
	return targets
}
