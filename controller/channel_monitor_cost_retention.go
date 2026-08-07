package controller

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

const (
	channelMonitorCostRetentionTaskType       = "channel_monitor_cost_retention"
	channelMonitorCostRetentionDefaultBatch   = 1000
	channelMonitorCostRetentionMaxBatch       = 10000
	channelMonitorCostRetentionDefaultMinutes = 24 * 60
	channelMonitorTaskRetentionKeepLatest     = 100
)

type channelMonitorCostRetentionTaskHandler struct{}

type channelMonitorCostRetentionTaskResult struct {
	RetentionDays                int   `json:"retention_days"`
	Cutoff                       int64 `json:"cutoff"`
	MinuteCutoff                 int64 `json:"minute_cutoff"`
	ProtectedWindowMinutes       int   `json:"protected_window_minutes"`
	ExecutionDetailRetentionDays int   `json:"execution_detail_retention_days"`
	ExecutionDetailCutoff        int64 `json:"execution_detail_cutoff"`
	TaskRetentionDays            int   `json:"task_retention_days"`
	TaskCutoff                   int64 `json:"task_cutoff"`
	RatioHistoryRetentionDays    int   `json:"ratio_history_retention_days"`
	RatioHistoryCutoff           int64 `json:"ratio_history_cutoff"`
	model.ChannelMonitorCostRetentionResult
	model.ChannelMonitorHistoryRetentionResult
}

func init() {
	service.RegisterSystemTaskHandler(channelMonitorCostRetentionTaskHandler{})
}

func (channelMonitorCostRetentionTaskHandler) Type() string {
	return channelMonitorCostRetentionTaskType
}

func (channelMonitorCostRetentionTaskHandler) Enabled() bool {
	return common.GetEnvOrDefaultBool("CHANNEL_MONITOR_COST_RETENTION_ENABLED", true)
}

func (channelMonitorCostRetentionTaskHandler) Interval() time.Duration {
	minutes := common.GetEnvOrDefault("CHANNEL_MONITOR_COST_RETENTION_INTERVAL_MINUTES", channelMonitorCostRetentionDefaultMinutes)
	if minutes < 60 {
		minutes = channelMonitorCostRetentionDefaultMinutes
	}
	return time.Duration(minutes) * time.Minute
}

func (channelMonitorCostRetentionTaskHandler) NewPayload() any { return nil }

func channelMonitorCostRetentionCutoff(now int64, days int) int64 {
	todayStart := model.ChannelDailyCostDayStart(now)
	return todayStart - int64(days-1)*channelMonitorCostDaySeconds
}

func channelMonitorHistoryRetentionCutoff(now int64, days int) int64 {
	return now - int64(days)*channelMonitorCostDaySeconds
}

func loadChannelMonitorRetentionSettings(ctx context.Context) (channelMonitorSettings, error) {
	settings := channelMonitorSettings{
		CostRetentionDays:                     defaultChannelMonitorCostRetentionDays,
		ExecutionDetailRetentionDays:          defaultChannelMonitorExecutionDetailRetentionDays,
		TaskRetentionDays:                     defaultChannelMonitorTaskRetentionDays,
		RatioHistoryRetentionDays:             defaultChannelMonitorRatioHistoryRetentionDays,
		SmartSchedulePerformanceWindowMinutes: defaultChannelMonitorSmartSchedulePerformanceWindowMinutes,
		SmartScheduleStabilityWindowMinutes:   defaultChannelMonitorSmartScheduleStabilityWindowMinutes,
	}
	var options []model.Option
	retentionOptionKeys := []string{
		channelMonitorCostRetentionDaysOption,
		channelMonitorExecutionDetailRetentionDaysOption,
		channelMonitorTaskRetentionDaysOption,
		channelMonitorRatioHistoryRetentionDaysOption,
		channelMonitorSmartSchedulePerformanceWindowOption,
		channelMonitorSmartScheduleStabilityWindowOption,
	}
	if err := model.DB.WithContext(ctx).
		Select("key", "value").
		Where("key IN ?", retentionOptionKeys).
		Find(&options).Error; err != nil {
		return channelMonitorSettings{}, fmt.Errorf("读取渠道监控保留配置失败: %w", err)
	}
	for _, option := range options {
		switch option.Key {
		case channelMonitorCostRetentionDaysOption:
			days, err := strconv.Atoi(option.Value)
			if err != nil || days < minChannelMonitorCostRetentionDays || days > maxChannelMonitorCostRetentionDays {
				return channelMonitorSettings{}, fmt.Errorf("渠道监控保留配置 %s 无效", option.Key)
			}
			settings.CostRetentionDays = days
		case channelMonitorExecutionDetailRetentionDaysOption:
			days, err := strconv.Atoi(option.Value)
			if err != nil || days < minChannelMonitorCostRetentionDays || days > maxChannelMonitorCostRetentionDays {
				return channelMonitorSettings{}, fmt.Errorf("渠道监控保留配置 %s 无效", option.Key)
			}
			settings.ExecutionDetailRetentionDays = days
		case channelMonitorTaskRetentionDaysOption:
			days, err := strconv.Atoi(option.Value)
			if err != nil || days < minChannelMonitorCostRetentionDays || days > maxChannelMonitorCostRetentionDays {
				return channelMonitorSettings{}, fmt.Errorf("渠道监控保留配置 %s 无效", option.Key)
			}
			settings.TaskRetentionDays = days
		case channelMonitorRatioHistoryRetentionDaysOption:
			days, err := strconv.Atoi(option.Value)
			if err != nil || days < minChannelMonitorCostRetentionDays || days > maxChannelMonitorCostRetentionDays {
				return channelMonitorSettings{}, fmt.Errorf("渠道监控保留配置 %s 无效", option.Key)
			}
			settings.RatioHistoryRetentionDays = days
		case channelMonitorSmartSchedulePerformanceWindowOption:
			minutes, err := strconv.Atoi(option.Value)
			if err == nil && isChannelMonitorSmartScheduleWindowSupported(minutes) {
				settings.SmartSchedulePerformanceWindowMinutes = minutes
			}
		case channelMonitorSmartScheduleStabilityWindowOption:
			minutes, err := strconv.Atoi(option.Value)
			if err == nil && isChannelMonitorSmartScheduleWindowSupported(minutes) {
				settings.SmartScheduleStabilityWindowMinutes = minutes
			}
		}
	}
	if settings.TaskRetentionDays < settings.ExecutionDetailRetentionDays {
		return channelMonitorSettings{}, errors.New("监控任务保留天数不能小于调度执行明细保留天数")
	}
	return settings, nil
}

func channelMonitorMinuteRetentionCutoff(
	now int64,
	configuredCutoff int64,
	performanceWindowMinutes int,
	stabilityWindowMinutes int,
) (int64, int) {
	protectedWindowMinutes := max(performanceWindowMinutes, stabilityWindowMinutes)
	requiredStart := now - int64(protectedWindowMinutes*60)
	requiredStart -= requiredStart % 60
	return min(configuredCutoff, requiredStart), protectedWindowMinutes
}

func (channelMonitorCostRetentionTaskHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	settings, err := loadChannelMonitorRetentionSettings(ctx)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, channelMonitorCostRetentionTaskResult{}, err)
		return
	}
	batchSize := common.GetEnvOrDefault("CHANNEL_MONITOR_COST_RETENTION_BATCH_SIZE", channelMonitorCostRetentionDefaultBatch)
	if batchSize <= 0 || batchSize > channelMonitorCostRetentionMaxBatch {
		batchSize = channelMonitorCostRetentionDefaultBatch
	}
	now := common.GetTimestamp()
	costCutoff := channelMonitorCostRetentionCutoff(now, settings.CostRetentionDays)
	minuteCutoff, protectedWindowMinutes := channelMonitorMinuteRetentionCutoff(
		now,
		costCutoff,
		settings.SmartSchedulePerformanceWindowMinutes,
		settings.SmartScheduleStabilityWindowMinutes,
	)
	historyCutoffs := model.ChannelMonitorHistoryRetentionCutoffs{
		ExecutionDetail: channelMonitorHistoryRetentionCutoff(now, settings.ExecutionDetailRetentionDays),
		Task:            channelMonitorHistoryRetentionCutoff(now, settings.TaskRetentionDays),
		RatioHistory:    channelMonitorHistoryRetentionCutoff(now, settings.RatioHistoryRetentionDays),
	}
	result := channelMonitorCostRetentionTaskResult{
		RetentionDays:                settings.CostRetentionDays,
		Cutoff:                       costCutoff,
		MinuteCutoff:                 minuteCutoff,
		ProtectedWindowMinutes:       protectedWindowMinutes,
		ExecutionDetailRetentionDays: settings.ExecutionDetailRetentionDays,
		ExecutionDetailCutoff:        historyCutoffs.ExecutionDetail,
		TaskRetentionDays:            settings.TaskRetentionDays,
		TaskCutoff:                   historyCutoffs.Task,
		RatioHistoryRetentionDays:    settings.RatioHistoryRetentionDays,
		RatioHistoryCutoff:           historyCutoffs.RatioHistory,
	}
	historyDeleted, err := model.DeleteChannelMonitorHistoryBefore(
		ctx,
		historyCutoffs,
		[]string{
			model.SystemTaskTypeChannelRatioMonitor,
			channelMonitorSmartScheduleTaskType,
			channelMonitorSmartScheduleProbeTaskType,
			channelMonitorCostRetentionTaskType,
		},
		channelMonitorTaskRetentionKeepLatest,
		batchSize,
	)
	result.ChannelMonitorHistoryRetentionResult = historyDeleted
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, result, err)
		return
	}
	costDeleted, err := model.DeleteChannelMonitorCostsBefore(ctx, costCutoff, minuteCutoff, batchSize)
	result.ChannelMonitorCostRetentionResult = costDeleted
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, result, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, result, nil)
}
