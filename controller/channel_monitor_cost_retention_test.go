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
	}{
		{
			name:                "missing uses defaults",
			values:              map[string]string{},
			wantExecutionDetail: defaultChannelMonitorExecutionDetailRetentionDays,
			wantTask:            defaultChannelMonitorTaskRetentionDays,
			wantRatioHistory:    defaultChannelMonitorRatioHistoryRetentionDays,
			wantStatusProbe:     defaultChannelMonitorStatusProbeHistoryRetentionDays,
		},
		{
			name: "valid values",
			values: map[string]string{
				channelMonitorExecutionDetailRetentionDaysOption:    "30",
				channelMonitorTaskRetentionDaysOption:               "180",
				channelMonitorRatioHistoryRetentionDaysOption:       "730",
				channelMonitorStatusProbeHistoryRetentionDaysOption: "21",
			},
			wantExecutionDetail: 30,
			wantTask:            180,
			wantRatioHistory:    730,
			wantStatusProbe:     21,
		},
		{
			name: "invalid values use defaults",
			values: map[string]string{
				channelMonitorExecutionDetailRetentionDaysOption:    "0",
				channelMonitorTaskRetentionDaysOption:               "3651",
				channelMonitorRatioHistoryRetentionDaysOption:       "invalid",
				channelMonitorStatusProbeHistoryRetentionDaysOption: "91",
			},
			wantExecutionDetail: defaultChannelMonitorExecutionDetailRetentionDays,
			wantTask:            defaultChannelMonitorTaskRetentionDays,
			wantRatioHistory:    defaultChannelMonitorRatioHistoryRetentionDays,
			wantStatusProbe:     defaultChannelMonitorStatusProbeHistoryRetentionDays,
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
		})
	}
}

func TestLoadChannelMonitorRetentionSettingsUsesDatabaseInsteadOfStaleNodeCache(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorCostRetentionDaysOption:               "7",
		channelMonitorExecutionDetailRetentionDaysOption:    "7",
		channelMonitorTaskRetentionDaysOption:               "7",
		channelMonitorRatioHistoryRetentionDaysOption:       "7",
		channelMonitorStatusProbeHistoryRetentionDaysOption: "7",
	})
	require.NoError(t, db.Create(&[]model.Option{
		{Key: channelMonitorCostRetentionDaysOption, Value: "180"},
		{Key: channelMonitorExecutionDetailRetentionDaysOption, Value: "30"},
		{Key: channelMonitorTaskRetentionDaysOption, Value: "120"},
		{Key: channelMonitorRatioHistoryRetentionDaysOption, Value: "730"},
		{Key: channelMonitorStatusProbeHistoryRetentionDaysOption, Value: "21"},
	}).Error)

	settings, err := loadChannelMonitorRetentionSettings(t.Context())

	require.NoError(t, err)
	assert.Equal(t, 180, settings.CostRetentionDays)
	assert.Equal(t, 30, settings.ExecutionDetailRetentionDays)
	assert.Equal(t, 120, settings.TaskRetentionDays)
	assert.Equal(t, 730, settings.RatioHistoryRetentionDays)
	assert.Equal(t, 21, settings.StatusProbeHistoryRetentionDays)
}

func TestUpdateChannelMonitorSettingsPersistsHistoryRetentionDays(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"execution_detail_retention_days":     30,
		"task_retention_days":                 180,
		"ratio_history_retention_days":        730,
		"status_probe_history_retention_days": 21,
	})

	UpdateChannelMonitorSettings(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response channelMonitorSettingsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 30, response.Data.ExecutionDetailRetentionDays)
	assert.Equal(t, 180, response.Data.TaskRetentionDays)
	assert.Equal(t, 730, response.Data.RatioHistoryRetentionDays)
	assert.Equal(t, 21, response.Data.StatusProbeHistoryRetentionDays)

	wantOptions := map[string]string{
		channelMonitorExecutionDetailRetentionDaysOption:    "30",
		channelMonitorTaskRetentionDaysOption:               "180",
		channelMonitorRatioHistoryRetentionDaysOption:       "730",
		channelMonitorStatusProbeHistoryRetentionDaysOption: "21",
	}
	for key, want := range wantOptions {
		var option model.Option
		require.NoError(t, db.Where("key = ?", key).First(&option).Error)
		assert.Equal(t, want, option.Value)
	}
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
