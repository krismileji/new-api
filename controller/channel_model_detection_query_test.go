package controller

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupChannelModelDetectionQueryControllerTest(t *testing.T) {
	t.Helper()
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelModelDetectionGlobalConfig{},
		&model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionTarget{},
		&model.ChannelModelDetectionRun{},
		&model.ChannelModelDetectionExecution{},
		&model.ChannelModelDetectionCostEvent{},
	))
}

func TestChannelModelDetectionOverviewAPIUsesSuccessEnvelope(t *testing.T) {
	setupChannelModelDetectionQueryControllerTest(t)
	db := model.DB
	require.NoError(t, db.Create(&model.ChannelModelDetectionGlobalConfig{
		DetectorURL: "http://127.0.0.1:18080", ScheduledPreset: model.ChannelModelDetectionPresetMedium,
		IntervalHours: 24, ScheduleTime: "02:30", Timezone: "Asia/Shanghai", Revision: 1,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 401, Name: "overview", Key: "overview-secret", Status: common.ChannelStatusEnabled}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/model_detection", nil)
	GetChannelModelDetectionOverview(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.Contains(t, recorder.Body.String(), `"channels"`)
	assert.NotContains(t, recorder.Body.String(), `"snapshot_version":`)
	assert.NotContains(t, recorder.Body.String(), `"snapshot_revision":`)
	assert.NotContains(t, recorder.Body.String(), `"event_watermark":`)
	assert.NotContains(t, recorder.Body.String(), `"generated_at":`)
	assert.NotContains(t, recorder.Body.String(), `"data_cutoff_at":`)
	assert.NotContains(t, recorder.Body.String(), `"snapshot_age_seconds":`)
	assert.NotContains(t, recorder.Body.String(), "overview-secret")
}

func TestChannelModelDetectionOverviewAPIReadsCurrentData(t *testing.T) {
	setupChannelModelDetectionQueryControllerTest(t)
	require.NoError(t, model.DB.Create(&model.Channel{
		Id: 404, Name: "before-update", Status: common.ChannelStatusEnabled,
	}).Error)

	readChannelName := func() string {
		ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/model_detection", nil)
		GetChannelModelDetectionOverview(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Data struct {
				Channels []struct {
					ID   int    `json:"id"`
					Name string `json:"name"`
				} `json:"channels"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.Len(t, response.Data.Channels, 1)
		assert.Equal(t, 404, response.Data.Channels[0].ID)
		return response.Data.Channels[0].Name
	}

	assert.Equal(t, "before-update", readChannelName())
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 404).Update("name", "after-update").Error)
	assert.Equal(t, "after-update", readChannelName())
}

func TestChannelModelDetectionHistoryAPIRejectsPaginationAndEnums(t *testing.T) {
	setupChannelModelDetectionQueryControllerTest(t)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 402, Name: "history", Key: "history-secret", Status: common.ChannelStatusEnabled}).Error)

	tests := []string{
		"?page_size=0",
		"?page_size=101",
		"?page_size=invalid",
		"?page=0",
		"?trigger=automatic",
		"?status=done",
		"?outcome=future_detector_outcome",
	}
	for _, query := range tests {
		t.Run(query, func(t *testing.T) {
			ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/model_detection/channel/402/runs"+query, nil)
			ctx.Params = append(ctx.Params, struct {
				Key   string
				Value string
			}{Key: "id", Value: "402"})
			ListChannelModelDetectionRuns(ctx)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), `"success":false`)
		})
	}

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/model_detection/channel/999/runs", nil)
	ctx.Params = append(ctx.Params, struct {
		Key   string
		Value string
	}{Key: "id", Value: "999"})
	ListChannelModelDetectionRuns(ctx)
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestChannelModelDetectionReportAPIRejectsOversizedReports(t *testing.T) {
	setupChannelModelDetectionQueryControllerTest(t)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 403, Name: "report", Key: "report-secret", Status: common.ChannelStatusEnabled}).Error)
	run := model.ChannelModelDetectionRun{
		RunId: "controller-report", ChannelId: 403, Trigger: model.ChannelModelDetectionTriggerManual,
		Preset: model.ChannelModelDetectionPresetLow, PresetSource: model.ChannelModelDetectionPresetSourceManualSelected,
		Status: model.ChannelModelDetectionRunStatusCompleted, TargetCount: 1, CompletedTargetCount: 1,
	}
	require.NoError(t, model.DB.Create(&run).Error)
	execution := model.ChannelModelDetectionExecution{
		RunId: run.RunId, TargetKey: "controller-target", TargetId: 1, ChannelId: 403,
		RequestModel: "gpt-5.6-sol", ClaimedModel: model.ChannelModelDetectionClaimedModelSol,
		Preset: run.Preset, Status: model.ChannelModelDetectionExecutionStatusCompleted,
	}
	require.NoError(t, model.DB.Create(&execution).Error)
	overLimit := strings.Repeat("x", model.ChannelModelDetectionMaxReportBytes+1)
	require.NoError(t, model.DB.Model(&model.ChannelModelDetectionExecution{}).Where("id = ?", execution.Id).Update("report_json", overLimit).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/model_detection/runs/controller-report", nil)
	ctx.Params = append(ctx.Params, struct {
		Key   string
		Value string
	}{Key: "run_id", Value: run.RunId})
	GetChannelModelDetectionRunDetail(ctx)
	assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.NotContains(t, recorder.Body.String(), "report-secret")
}
