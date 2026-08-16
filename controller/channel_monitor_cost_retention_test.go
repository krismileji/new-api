package controller

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorCostRetentionSettingsUsePersistedDays(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "missing uses default", want: defaultChannelMonitorCostRetentionDays},
		{name: "valid value", raw: "365", want: 365},
		{name: "below minimum uses default", raw: "0", want: defaultChannelMonitorCostRetentionDays},
		{name: "above maximum uses default", raw: "3651", want: defaultChannelMonitorCostRetentionDays},
		{name: "invalid value uses default", raw: "invalid", want: defaultChannelMonitorCostRetentionDays},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := map[string]string{}
			if test.raw != "" {
				values[channelMonitorCostRetentionDaysOption] = test.raw
			}
			useChannelMonitorOptionMap(t, values)

			assert.Equal(t, test.want, getChannelMonitorSettings().CostRetentionDays)
		})
	}
}

func TestChannelMonitorHistoryRetentionSettingsUsePersistedDays(t *testing.T) {
	tests := []struct {
		name                string
		values              map[string]string
		wantExecutionDetail int
		wantTask            int
		wantRatioHistory    int
		wantStatusProbe     int
		wantModelDetection  int
		wantRouteMetric     int
		wantAPIKeyMetric    int
	}{
		{
			name:                "missing uses defaults",
			values:              map[string]string{},
			wantExecutionDetail: defaultChannelMonitorExecutionDetailRetentionDays,
			wantTask:            defaultChannelMonitorTaskRetentionDays,
			wantRatioHistory:    defaultChannelMonitorRatioHistoryRetentionDays,
			wantStatusProbe:     defaultChannelMonitorStatusProbeHistoryRetentionDays,
			wantModelDetection:  model.ChannelModelDetectionDefaultRetentionDays,
			wantRouteMetric:     defaultChannelMonitorRouteMetricRetentionDays,
			wantAPIKeyMetric:    defaultChannelMonitorAPIKeyMetricRetentionDays,
		},
		{
			name: "valid values",
			values: map[string]string{
				channelMonitorExecutionDetailRetentionDaysOption:    "30",
				channelMonitorTaskRetentionDaysOption:               "180",
				channelMonitorRatioHistoryRetentionDaysOption:       "730",
				channelMonitorStatusProbeHistoryRetentionDaysOption: "21",
				channelMonitorModelDetectionRetentionDaysOption:     "45",
				channelMonitorRouteMetricRetentionDaysOption:        "21",
				channelMonitorAPIKeyMetricRetentionDaysOption:       "5",
			},
			wantExecutionDetail: 30,
			wantTask:            180,
			wantRatioHistory:    730,
			wantStatusProbe:     21,
			wantModelDetection:  45,
			wantRouteMetric:     21,
			wantAPIKeyMetric:    5,
		},
		{
			name: "invalid values use defaults",
			values: map[string]string{
				channelMonitorExecutionDetailRetentionDaysOption:    "0",
				channelMonitorTaskRetentionDaysOption:               "3651",
				channelMonitorRatioHistoryRetentionDaysOption:       "invalid",
				channelMonitorStatusProbeHistoryRetentionDaysOption: "91",
				channelMonitorModelDetectionRetentionDaysOption:     "181",
				channelMonitorRouteMetricRetentionDaysOption:        "0",
				channelMonitorAPIKeyMetricRetentionDaysOption:       "3651",
			},
			wantExecutionDetail: defaultChannelMonitorExecutionDetailRetentionDays,
			wantTask:            defaultChannelMonitorTaskRetentionDays,
			wantRatioHistory:    defaultChannelMonitorRatioHistoryRetentionDays,
			wantStatusProbe:     defaultChannelMonitorStatusProbeHistoryRetentionDays,
			wantModelDetection:  model.ChannelModelDetectionDefaultRetentionDays,
			wantRouteMetric:     defaultChannelMonitorRouteMetricRetentionDays,
			wantAPIKeyMetric:    defaultChannelMonitorAPIKeyMetricRetentionDays,
		},
		{
			name: "task retention is raised to preserve execution details",
			values: map[string]string{
				channelMonitorExecutionDetailRetentionDaysOption: "365",
				channelMonitorTaskRetentionDaysOption:            "30",
			},
			wantExecutionDetail: 365,
			wantTask:            365,
			wantRatioHistory:    defaultChannelMonitorRatioHistoryRetentionDays,
			wantStatusProbe:     defaultChannelMonitorStatusProbeHistoryRetentionDays,
			wantModelDetection:  model.ChannelModelDetectionDefaultRetentionDays,
			wantRouteMetric:     defaultChannelMonitorRouteMetricRetentionDays,
			wantAPIKeyMetric:    defaultChannelMonitorAPIKeyMetricRetentionDays,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			useChannelMonitorOptionMap(t, test.values)

			settings := getChannelMonitorSettings()
			assert.Equal(t, test.wantExecutionDetail, settings.ExecutionDetailRetentionDays)
			assert.Equal(t, test.wantTask, settings.TaskRetentionDays)
			assert.Equal(t, test.wantRatioHistory, settings.RatioHistoryRetentionDays)
			assert.Equal(t, test.wantStatusProbe, settings.StatusProbeHistoryRetentionDays)
			assert.Equal(t, test.wantModelDetection, settings.ModelDetectionRetentionDays)
			assert.Equal(t, test.wantRouteMetric, settings.RouteMetricRetentionDays)
			assert.Equal(t, test.wantAPIKeyMetric, settings.APIKeyMetricRetentionDays)
		})
	}
}

func TestChannelMonitorCleanupSettingsUsePersistedValues(t *testing.T) {
	tests := []struct {
		name                    string
		values                  map[string]string
		wantEnabled             bool
		wantBatchSize           int
		wantBudgetSeconds       int
		wantContinuationSeconds int
		wantIntervalMinutes     int
	}{
		{
			name:                    "missing uses defaults",
			values:                  map[string]string{},
			wantEnabled:             defaultChannelMonitorCleanupEnabled,
			wantBatchSize:           defaultChannelMonitorCleanupBatchSize,
			wantBudgetSeconds:       defaultChannelMonitorCleanupBudgetSeconds,
			wantContinuationSeconds: defaultChannelMonitorCleanupContinuationSeconds,
			wantIntervalMinutes:     defaultChannelMonitorCleanupIntervalMinutes,
		},
		{
			name: "valid values",
			values: map[string]string{
				channelMonitorCleanupEnabledOption:             "false",
				channelMonitorCleanupBatchSizeOption:           "5000",
				channelMonitorCleanupBudgetSecondsOption:       "120",
				channelMonitorCleanupContinuationSecondsOption: "300",
				channelMonitorCleanupIntervalMinutesOption:     "720",
			},
			wantBatchSize:           5000,
			wantBudgetSeconds:       120,
			wantContinuationSeconds: 300,
			wantIntervalMinutes:     720,
		},
		{
			name: "invalid values use defaults",
			values: map[string]string{
				channelMonitorCleanupEnabledOption:             "invalid",
				channelMonitorCleanupBatchSizeOption:           "0",
				channelMonitorCleanupBudgetSecondsOption:       "301",
				channelMonitorCleanupContinuationSecondsOption: "14",
				channelMonitorCleanupIntervalMinutesOption:     "10081",
			},
			wantEnabled:             defaultChannelMonitorCleanupEnabled,
			wantBatchSize:           defaultChannelMonitorCleanupBatchSize,
			wantBudgetSeconds:       defaultChannelMonitorCleanupBudgetSeconds,
			wantContinuationSeconds: defaultChannelMonitorCleanupContinuationSeconds,
			wantIntervalMinutes:     defaultChannelMonitorCleanupIntervalMinutes,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			useChannelMonitorOptionMap(t, test.values)

			settings := getChannelMonitorSettings()
			assert.Equal(t, test.wantEnabled, settings.CleanupEnabled)
			assert.Equal(t, test.wantBatchSize, settings.CleanupBatchSize)
			assert.Equal(t, test.wantBudgetSeconds, settings.CleanupBudgetSeconds)
			assert.Equal(t, test.wantContinuationSeconds, settings.CleanupContinuationSeconds)
			assert.Equal(t, test.wantIntervalMinutes, settings.CleanupIntervalMinutes)
		})
	}
}

func TestGetChannelMonitorOverviewReturnsRetentionDefaultsWhenOptionsAreMissing(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor", nil)
	GetChannelMonitorOverview(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Settings channelMonitorSettings `json:"settings"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 30, response.Data.Settings.CostRetentionDays)
	assert.Equal(t, 3, response.Data.Settings.ExecutionDetailRetentionDays)
	assert.Equal(t, 90, response.Data.Settings.TaskRetentionDays)
	assert.Equal(t, 365, response.Data.Settings.RatioHistoryRetentionDays)
	assert.Equal(t, 7, response.Data.Settings.StatusProbeHistoryRetentionDays)
	assert.Equal(t, 30, response.Data.Settings.ModelDetectionRetentionDays)
	assert.Equal(t, 30, response.Data.Settings.RouteMetricRetentionDays)
	assert.Equal(t, 7, response.Data.Settings.APIKeyMetricRetentionDays)
	assert.True(t, response.Data.Settings.CleanupEnabled)
	assert.Equal(t, 1000, response.Data.Settings.CleanupBatchSize)
	assert.Equal(t, 10, response.Data.Settings.CleanupBudgetSeconds)
	assert.Equal(t, 60, response.Data.Settings.CleanupContinuationSeconds)
	assert.Equal(t, 1440, response.Data.Settings.CleanupIntervalMinutes)
}

func TestGetChannelMonitorOverviewUsesDatabaseSettingsInsteadOfStaleNodeCache(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorExecutionDetailRetentionDaysOption: "180",
		channelMonitorTaskRetentionDaysOption:            "180",
		channelMonitorModelDetectionRetentionDaysOption:  "7",
		channelMonitorCleanupEnabledOption:               "true",
		channelMonitorProbeResponseOption:                "false",
	})
	require.NoError(t, db.Create(&[]model.Option{
		{Key: channelMonitorExecutionDetailRetentionDaysOption, Value: "30"},
		{Key: channelMonitorTaskRetentionDaysOption, Value: "120"},
		{Key: channelMonitorModelDetectionRetentionDaysOption, Value: "45"},
		{Key: channelMonitorCleanupEnabledOption, Value: "false"},
		{Key: channelMonitorProbeResponseOption, Value: "true"},
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor", nil)
	GetChannelMonitorOverview(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Settings channelMonitorSettings `json:"settings"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 30, response.Data.Settings.ExecutionDetailRetentionDays)
	assert.Equal(t, 120, response.Data.Settings.TaskRetentionDays)
	assert.Equal(t, 45, response.Data.Settings.ModelDetectionRetentionDays)
	assert.False(t, response.Data.Settings.CleanupEnabled)
	assert.True(t, response.Data.Settings.ProbeResponseEnabled)
}

func TestLoadChannelMonitorRetentionSettingsUsesDatabaseInsteadOfStaleNodeCache(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorCostRetentionDaysOption:               "7",
		channelMonitorExecutionDetailRetentionDaysOption:    "7",
		channelMonitorTaskRetentionDaysOption:               "7",
		channelMonitorRatioHistoryRetentionDaysOption:       "7",
		channelMonitorStatusProbeHistoryRetentionDaysOption: "7",
		channelMonitorModelDetectionRetentionDaysOption:     "7",
		channelMonitorRouteMetricRetentionDaysOption:        "7",
		channelMonitorAPIKeyMetricRetentionDaysOption:       "7",
	})
	require.NoError(t, db.Create(&[]model.Option{
		{Key: channelMonitorCostRetentionDaysOption, Value: "180"},
		{Key: channelMonitorExecutionDetailRetentionDaysOption, Value: "30"},
		{Key: channelMonitorTaskRetentionDaysOption, Value: "120"},
		{Key: channelMonitorRatioHistoryRetentionDaysOption, Value: "730"},
		{Key: channelMonitorStatusProbeHistoryRetentionDaysOption, Value: "21"},
		{Key: channelMonitorModelDetectionRetentionDaysOption, Value: "45"},
		{Key: channelMonitorRouteMetricRetentionDaysOption, Value: "21"},
		{Key: channelMonitorAPIKeyMetricRetentionDaysOption, Value: "5"},
	}).Error)

	settings, err := loadChannelMonitorRetentionSettings(t.Context())

	require.NoError(t, err)
	assert.Equal(t, 180, settings.CostRetentionDays)
	assert.Equal(t, 30, settings.ExecutionDetailRetentionDays)
	assert.Equal(t, 120, settings.TaskRetentionDays)
	assert.Equal(t, 730, settings.RatioHistoryRetentionDays)
	assert.Equal(t, 21, settings.StatusProbeHistoryRetentionDays)
	assert.Equal(t, 45, settings.ModelDetectionRetentionDays)
	assert.Equal(t, 21, settings.RouteMetricRetentionDays)
	assert.Equal(t, 5, settings.APIKeyMetricRetentionDays)
}

func TestLoadChannelMonitorCleanupSettingsUsesDatabaseInsteadOfStaleNodeCache(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorCleanupEnabledOption:             "true",
		channelMonitorCleanupBatchSizeOption:           "1000",
		channelMonitorCleanupBudgetSecondsOption:       "10",
		channelMonitorCleanupContinuationSecondsOption: "60",
		channelMonitorCleanupIntervalMinutesOption:     "60",
	})
	require.NoError(t, db.Create(&[]model.Option{
		{Key: channelMonitorCleanupEnabledOption, Value: "false"},
		{Key: channelMonitorCleanupBatchSizeOption, Value: "2500"},
		{Key: channelMonitorCleanupBudgetSecondsOption, Value: "45"},
		{Key: channelMonitorCleanupContinuationSecondsOption, Value: "90"},
		{Key: channelMonitorCleanupIntervalMinutesOption, Value: "720"},
	}).Error)

	settings, err := loadChannelMonitorCleanupSettings(t.Context())

	require.NoError(t, err)
	assert.False(t, settings.Enabled)
	assert.Equal(t, 2500, settings.BatchSize)
	assert.Equal(t, 45, settings.BudgetSeconds)
	assert.Equal(t, 90, settings.ContinuationSeconds)
	assert.Equal(t, 720, settings.IntervalMinutes)
}

func TestUpdateChannelMonitorSettingsPersistsHistoryRetentionDays(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"cost_retention_days":                 45,
		"execution_detail_retention_days":     30,
		"task_retention_days":                 180,
		"ratio_history_retention_days":        730,
		"status_probe_history_retention_days": 21,
		"model_detection_retention_days":      45,
		"route_metric_retention_days":         21,
		"api_key_metric_retention_days":       5,
		"cleanup_enabled":                     false,
		"cleanup_batch_size":                  2500,
		"cleanup_budget_seconds":              45,
		"cleanup_continuation_seconds":        90,
		"cleanup_interval_minutes":            360,
	})

	UpdateChannelMonitorSettings(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response channelMonitorSettingsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 45, response.Data.CostRetentionDays)
	assert.Equal(t, 30, response.Data.ExecutionDetailRetentionDays)
	assert.Equal(t, 180, response.Data.TaskRetentionDays)
	assert.Equal(t, 730, response.Data.RatioHistoryRetentionDays)
	assert.Equal(t, 21, response.Data.StatusProbeHistoryRetentionDays)
	assert.Equal(t, 45, response.Data.ModelDetectionRetentionDays)
	assert.Equal(t, 21, response.Data.RouteMetricRetentionDays)
	assert.Equal(t, 5, response.Data.APIKeyMetricRetentionDays)
	assert.False(t, response.Data.CleanupEnabled)
	assert.Equal(t, 2500, response.Data.CleanupBatchSize)
	assert.Equal(t, 45, response.Data.CleanupBudgetSeconds)
	assert.Equal(t, 90, response.Data.CleanupContinuationSeconds)
	assert.Equal(t, 360, response.Data.CleanupIntervalMinutes)

	wantOptions := map[string]string{
		channelMonitorCostRetentionDaysOption:               "45",
		channelMonitorExecutionDetailRetentionDaysOption:    "30",
		channelMonitorTaskRetentionDaysOption:               "180",
		channelMonitorRatioHistoryRetentionDaysOption:       "730",
		channelMonitorStatusProbeHistoryRetentionDaysOption: "21",
		channelMonitorModelDetectionRetentionDaysOption:     "45",
		channelMonitorRouteMetricRetentionDaysOption:        "21",
		channelMonitorAPIKeyMetricRetentionDaysOption:       "5",
		channelMonitorCleanupEnabledOption:                  "false",
		channelMonitorCleanupBatchSizeOption:                "2500",
		channelMonitorCleanupBudgetSecondsOption:            "45",
		channelMonitorCleanupContinuationSecondsOption:      "90",
		channelMonitorCleanupIntervalMinutesOption:          "360",
	}
	for key, want := range wantOptions {
		var option model.Option
		require.NoError(t, db.Where("key = ?", key).First(&option).Error)
		assert.Equal(t, want, option.Value)
	}
	assert.Equal(t, 360*time.Minute, (channelMonitorCostRetentionTaskHandler{}).Interval())
}

func TestUpdateChannelMonitorSettingsValidatesAgainstDatabaseSnapshot(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorExecutionDetailRetentionDaysOption: "180",
		channelMonitorTaskRetentionDaysOption:            "180",
	})
	require.NoError(t, db.Create(&[]model.Option{
		{Key: channelMonitorExecutionDetailRetentionDaysOption, Value: "30"},
		{Key: channelMonitorTaskRetentionDaysOption, Value: "120"},
	}).Error)
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"task_retention_days": 60,
	})

	UpdateChannelMonitorSettings(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response channelMonitorSettingsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 30, response.Data.ExecutionDetailRetentionDays)
	assert.Equal(t, 60, response.Data.TaskRetentionDays)
	var executionDetailOption model.Option
	require.NoError(t, db.Where("key = ?", channelMonitorExecutionDetailRetentionDaysOption).First(&executionDetailOption).Error)
	assert.Equal(t, "30", executionDetailOption.Value)
}

func TestUpdateChannelMonitorSettingsRejectsInvalidHistoryRetentionDays(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value int
	}{
		{name: "execution detail below minimum", field: "execution_detail_retention_days", value: 0},
		{name: "execution detail above maximum", field: "execution_detail_retention_days", value: 3651},
		{name: "task below minimum", field: "task_retention_days", value: 0},
		{name: "task above maximum", field: "task_retention_days", value: 3651},
		{name: "ratio history below minimum", field: "ratio_history_retention_days", value: 0},
		{name: "ratio history above maximum", field: "ratio_history_retention_days", value: 3651},
		{name: "status probe below minimum", field: "status_probe_history_retention_days", value: 0},
		{name: "status probe above maximum", field: "status_probe_history_retention_days", value: 91},
		{name: "model detection below minimum", field: "model_detection_retention_days", value: 6},
		{name: "model detection above maximum", field: "model_detection_retention_days", value: 181},
		{name: "route metric below minimum", field: "route_metric_retention_days", value: 0},
		{name: "route metric above maximum", field: "route_metric_retention_days", value: 3651},
		{name: "API Key metric below minimum", field: "api_key_metric_retention_days", value: 0},
		{name: "API Key metric above maximum", field: "api_key_metric_retention_days", value: 3651},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			useChannelMonitorOptionMap(t, map[string]string{})
			ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
				test.field: test.value,
			})

			UpdateChannelMonitorSettings(ctx)

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "保留天数")
		})
	}
}

func TestUpdateChannelMonitorSettingsAcceptsCleanupSettingBounds(t *testing.T) {
	tests := []struct {
		name    string
		request map[string]any
	}{
		{
			name: "minimum",
			request: map[string]any{
				"cleanup_batch_size":           minChannelMonitorCleanupBatchSize,
				"cleanup_budget_seconds":       minChannelMonitorCleanupBudgetSeconds,
				"cleanup_continuation_seconds": minChannelMonitorCleanupContinuationSeconds,
				"cleanup_interval_minutes":     minChannelMonitorCleanupIntervalMinutes,
			},
		},
		{
			name: "maximum",
			request: map[string]any{
				"cleanup_batch_size":           maxChannelMonitorCleanupBatchSize,
				"cleanup_budget_seconds":       maxChannelMonitorCleanupBudgetSeconds,
				"cleanup_continuation_seconds": maxChannelMonitorCleanupContinuationSeconds,
				"cleanup_interval_minutes":     maxChannelMonitorCleanupIntervalMinutes,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupChannelMonitorControllerTestDB(t)
			useChannelMonitorOptionMap(t, map[string]string{})
			ctx, recorder := newChannelMonitorControllerContext(
				t, http.MethodPut, "/api/channel_monitor/settings", test.request,
			)

			UpdateChannelMonitorSettings(ctx)

			require.Equal(t, http.StatusOK, recorder.Code)
			var response channelMonitorSettingsAPIResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success)
			assert.Equal(t, test.request["cleanup_batch_size"], response.Data.CleanupBatchSize)
			assert.Equal(t, test.request["cleanup_budget_seconds"], response.Data.CleanupBudgetSeconds)
			assert.Equal(t, test.request["cleanup_continuation_seconds"], response.Data.CleanupContinuationSeconds)
			assert.Equal(t, test.request["cleanup_interval_minutes"], response.Data.CleanupIntervalMinutes)
		})
	}
}

func TestUpdateChannelMonitorSettingsRejectsInvalidCleanupSettings(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value int
	}{
		{name: "batch below minimum", field: "cleanup_batch_size", value: 0},
		{name: "batch above maximum", field: "cleanup_batch_size", value: 10001},
		{name: "budget below minimum", field: "cleanup_budget_seconds", value: 0},
		{name: "budget above maximum", field: "cleanup_budget_seconds", value: 301},
		{name: "continuation below minimum", field: "cleanup_continuation_seconds", value: 14},
		{name: "continuation above maximum", field: "cleanup_continuation_seconds", value: 3601},
		{name: "interval below minimum", field: "cleanup_interval_minutes", value: 59},
		{name: "interval above maximum", field: "cleanup_interval_minutes", value: 10081},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			useChannelMonitorOptionMap(t, map[string]string{})
			ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
				test.field: test.value,
			})

			UpdateChannelMonitorSettings(ctx)

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "清理")
		})
	}
}

func TestUpdateChannelMonitorSettingsRejectsTaskRetentionShorterThanExecutionDetails(t *testing.T) {
	useChannelMonitorOptionMap(t, map[string]string{})
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"execution_detail_retention_days": 180,
		"task_retention_days":             30,
	})

	UpdateChannelMonitorSettings(ctx)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "监控任务保留天数不能小于调度执行明细保留天数")
}

func TestChannelMonitorCostRetentionCutoffKeepsExactBeijingCalendarDays(t *testing.T) {
	now := time.Date(2026, 7, 25, 7, 30, 0, 0, time.UTC).Unix()
	todayStart := model.ChannelDailyCostDayStart(now)

	assert.Equal(t, todayStart, channelMonitorCostRetentionCutoff(now, 1))
	assert.Equal(
		t,
		todayStart-int64(defaultChannelMonitorCostRetentionDays-1)*channelMonitorCostDaySeconds,
		channelMonitorCostRetentionCutoff(now, defaultChannelMonitorCostRetentionDays),
	)
}

func TestChannelMonitorHistoryRetentionCutoffUsesFullDays(t *testing.T) {
	const now = int64(2_000_000)

	assert.Equal(t, now-14*channelMonitorCostDaySeconds, channelMonitorHistoryRetentionCutoff(now, 14))
}

func TestChannelMonitorModelDetectionRetentionSettingsUsePersistedBounds(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected int
	}{
		{name: "default", expected: model.ChannelModelDetectionDefaultRetentionDays},
		{name: "minimum", value: "7", expected: model.ChannelModelDetectionMinRetentionDays},
		{name: "maximum", value: "180", expected: model.ChannelModelDetectionMaxRetentionDays},
		{name: "below minimum", value: "6", expected: model.ChannelModelDetectionDefaultRetentionDays},
		{name: "above maximum", value: "181", expected: model.ChannelModelDetectionDefaultRetentionDays},
		{name: "invalid", value: "invalid", expected: model.ChannelModelDetectionDefaultRetentionDays},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := map[string]string{}
			if test.value != "" {
				values[channelMonitorModelDetectionRetentionDaysOption] = test.value
			}
			useChannelMonitorOptionMap(t, values)
			assert.Equal(t, test.expected, getChannelMonitorSettings().ModelDetectionRetentionDays)
		})
	}
}

func TestChannelMonitorMinuteRetentionCutoffProtectsLongestScheduleWindow(t *testing.T) {
	const now = int64(2_000_000)
	requiredStart := now - int64(180*time.Minute/time.Second)
	requiredStart -= requiredStart % 60

	cutoff, protectedMinutes := channelMonitorMinuteRetentionCutoff(
		now,
		now-int64(time.Hour/time.Second),
		60,
		180,
	)
	assert.Equal(t, requiredStart, cutoff)
	assert.Equal(t, 180, protectedMinutes)

	olderConfiguredCutoff := now - int64(24*time.Hour/time.Second)
	cutoff, protectedMinutes = channelMonitorMinuteRetentionCutoff(
		now,
		olderConfiguredCutoff,
		60,
		180,
	)
	assert.Equal(t, olderConfiguredCutoff, cutoff)
	assert.Equal(t, 180, protectedMinutes)
}

func TestChannelMonitorCleanupHandlerEnabledUsesDatabaseValue(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorCleanupEnabledOption: "false",
	})
	require.NoError(t, db.Create(&model.Option{
		Key:   channelMonitorCleanupEnabledOption,
		Value: "true",
	}).Error)

	handler := channelMonitorCostRetentionTaskHandler{}
	assert.True(t, handler.Enabled())
	require.NoError(t, db.Model(&model.Option{}).
		Where("key = ?", channelMonitorCleanupEnabledOption).
		Update("value", "false").Error)
	assert.False(t, handler.Enabled())
}

func TestChannelMonitorCleanupIntervalUsesLatestDatabaseValue(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorCleanupIntervalMinutesOption: "60",
	})
	require.NoError(t, db.Create(&model.Option{
		Key:   channelMonitorCleanupIntervalMinutesOption,
		Value: "720",
	}).Error)

	assert.Equal(t, 720*time.Minute, (channelMonitorCostRetentionTaskHandler{}).Interval())
}

func TestDisabledChannelMonitorCleanupFinishesQueuedTaskWithoutDeletingData(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.Create(&model.Option{
		Key:   channelMonitorCleanupEnabledOption,
		Value: "false",
	}).Error)
	require.NoError(t, db.Create(&model.ChannelDailyCost{
		ChannelId: 1,
		DayStart:  1,
	}).Error)
	task, err := model.CreateSystemTask(channelMonitorCostRetentionTaskType, nil, nil)
	require.NoError(t, err)
	const runnerID = "cleanup-disabled-test"
	claimedTask, claimed, err := model.ClaimSystemTask(
		task.ID,
		channelMonitorCostRetentionTaskType,
		runnerID,
		common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	require.True(t, claimed)

	(channelMonitorCostRetentionTaskHandler{}).Run(t.Context(), claimedTask, runnerID)

	storedTask, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, storedTask)
	assert.Equal(t, model.SystemTaskStatusSucceeded, storedTask.Status)
	var remaining int64
	require.NoError(t, db.Model(&model.ChannelDailyCost{}).Count(&remaining).Error)
	assert.Equal(t, int64(1), remaining)
}

func TestScheduleChannelMonitorCleanupContinuationUsesLatestDatabaseDelay(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorCleanupEnabledOption:             "false",
		channelMonitorCleanupContinuationSecondsOption: "15",
	})
	require.NoError(t, db.Create(&[]model.Option{
		{Key: channelMonitorCleanupEnabledOption, Value: "true"},
		{Key: channelMonitorCleanupContinuationSecondsOption, Value: "45"},
	}).Error)
	originalScheduler := channelMonitorCleanupContinuationScheduler
	t.Cleanup(func() { channelMonitorCleanupContinuationScheduler = originalScheduler })

	var scheduledDelay time.Duration
	channelMonitorCleanupContinuationScheduler = func(delay time.Duration, _ func()) {
		scheduledDelay = delay
	}
	scheduleChannelMonitorCleanupContinuation()

	assert.Equal(t, 45*time.Second, scheduledDelay)
}

func TestChannelMonitorCleanupContinuationDoesNotEnqueueAfterDisable(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.Create(&[]model.Option{
		{Key: channelMonitorCleanupEnabledOption, Value: "true"},
		{Key: channelMonitorCleanupContinuationSecondsOption, Value: "60"},
	}).Error)
	originalScheduler := channelMonitorCleanupContinuationScheduler
	t.Cleanup(func() { channelMonitorCleanupContinuationScheduler = originalScheduler })

	var continuation func()
	channelMonitorCleanupContinuationScheduler = func(_ time.Duration, callback func()) {
		continuation = callback
	}
	scheduleChannelMonitorCleanupContinuation()
	require.NotNil(t, continuation)
	require.NoError(t, db.Model(&model.Option{}).
		Where("key = ?", channelMonitorCleanupEnabledOption).
		Update("value", "false").Error)

	continuation()

	activeTask, err := model.GetActiveSystemTask(channelMonitorCostRetentionTaskType)
	require.NoError(t, err)
	assert.Nil(t, activeTask)
}
