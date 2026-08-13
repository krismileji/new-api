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
	channelMonitorCleanupDefaultBudgetSeconds = 10
	channelMonitorCleanupMaxBudgetSeconds     = 300
	channelMonitorCleanupContinuationDefault  = 60
	channelMonitorCleanupContinuationMin      = 15
	channelMonitorCleanupContinuationMax      = 3600
)

var channelMonitorCleanupContinuationScheduler = func(delay time.Duration) {
	time.AfterFunc(delay, func() {
		if _, _, err := service.EnqueueSystemTask(channelMonitorCostRetentionTaskType, nil); err != nil {
			common.SysError("续排渠道监控清理任务失败: " + err.Error())
		}
	})
}

type channelMonitorCostRetentionTaskHandler struct{}

type channelMonitorCostRetentionTaskResult struct {
	RetentionDays                   int   `json:"retention_days"`
	Cutoff                          int64 `json:"cutoff"`
	MinuteCutoff                    int64 `json:"minute_cutoff"`
	ProtectedWindowMinutes          int   `json:"protected_window_minutes"`
	ExecutionDetailRetentionDays    int   `json:"execution_detail_retention_days"`
	ExecutionDetailCutoff           int64 `json:"execution_detail_cutoff"`
	TaskRetentionDays               int   `json:"task_retention_days"`
	TaskCutoff                      int64 `json:"task_cutoff"`
	RatioHistoryRetentionDays       int   `json:"ratio_history_retention_days"`
	RatioHistoryCutoff              int64 `json:"ratio_history_cutoff"`
	StatusProbeHistoryRetentionDays int   `json:"status_probe_history_retention_days"`
	StatusProbeHistoryCutoff        int64 `json:"status_probe_history_cutoff"`
	StatusProbeExecutionsDeleted    int64 `json:"status_probe_executions_deleted"`
	ModelDetectionRetentionDays     int   `json:"model_detection_retention_days"`
	ModelDetectionCutoff            int64 `json:"model_detection_cutoff"`
	BudgetExhausted                 bool  `json:"budget_exhausted"`
	model.ChannelMonitorCostRetentionResult
	model.ChannelMonitorHistoryRetentionResult
	model.ChannelModelDetectionRetentionResult
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

func channelMonitorCleanupContinuationDelay() time.Duration {
	seconds := common.GetEnvOrDefault(
		"CHANNEL_MONITOR_CLEANUP_CONTINUATION_SECONDS",
		channelMonitorCleanupContinuationDefault,
	)
	if seconds < channelMonitorCleanupContinuationMin || seconds > channelMonitorCleanupContinuationMax {
		seconds = channelMonitorCleanupContinuationDefault
	}
	return time.Duration(seconds) * time.Second
}

func scheduleChannelMonitorCleanupContinuation() {
	channelMonitorCleanupContinuationScheduler(channelMonitorCleanupContinuationDelay())
}

func channelMonitorCostRetentionCutoff(now int64, days int) int64 {
	todayStart := model.ChannelDailyCostDayStart(now)
	return todayStart - int64(days-1)*channelMonitorCostDaySeconds
}

func channelMonitorHistoryRetentionCutoff(now int64, days int) int64 {
	return now - int64(days)*channelMonitorCostDaySeconds
}

func channelModelDetectionRetentionDays() int {
	days := common.GetEnvOrDefault(
		"CHANNEL_MODEL_DETECTION_RETENTION_DAYS",
		model.ChannelModelDetectionDefaultRetentionDays,
	)
	if days < model.ChannelModelDetectionMinRetentionDays || days > model.ChannelModelDetectionMaxRetentionDays {
		return model.ChannelModelDetectionDefaultRetentionDays
	}
	return days
}

func loadChannelMonitorRetentionSettings(ctx context.Context) (channelMonitorSettings, error) {
	settings := channelMonitorSettings{
		CostRetentionDays:                     defaultChannelMonitorCostRetentionDays,
		ExecutionDetailRetentionDays:          defaultChannelMonitorExecutionDetailRetentionDays,
		TaskRetentionDays:                     defaultChannelMonitorTaskRetentionDays,
		RatioHistoryRetentionDays:             defaultChannelMonitorRatioHistoryRetentionDays,
		StatusProbeHistoryRetentionDays:       defaultChannelMonitorStatusProbeHistoryRetentionDays,
		SmartSchedulePerformanceWindowMinutes: defaultChannelMonitorSmartSchedulePerformanceWindowMinutes,
		SmartScheduleStabilityWindowMinutes:   defaultChannelMonitorSmartScheduleStabilityWindowMinutes,
	}
	var options []model.Option
	retentionOptionKeys := []string{
		channelMonitorCostRetentionDaysOption,
		channelMonitorExecutionDetailRetentionDaysOption,
		channelMonitorTaskRetentionDaysOption,
		channelMonitorRatioHistoryRetentionDaysOption,
		channelMonitorStatusProbeHistoryRetentionDaysOption,
		channelMonitorSmartSchedulePerformanceWindowOption,
		channelMonitorSmartScheduleStabilityWindowOption,
	}
	if err := model.DB.WithContext(ctx).
		Select("key", "value").
		Where(map[string]any{"key": retentionOptionKeys}).
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
		case channelMonitorStatusProbeHistoryRetentionDaysOption:
			days, err := strconv.Atoi(option.Value)
			if err != nil || days < minChannelMonitorCostRetentionDays ||
				days > maxChannelMonitorStatusProbeHistoryRetentionDays {
				return channelMonitorSettings{}, fmt.Errorf("渠道监控保留配置 %s 无效", option.Key)
			}
			settings.StatusProbeHistoryRetentionDays = days
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
	budgetSeconds := common.GetEnvOrDefault(
		"CHANNEL_MONITOR_CLEANUP_BUDGET_SECONDS",
		channelMonitorCleanupDefaultBudgetSeconds,
	)
	if budgetSeconds <= 0 || budgetSeconds > channelMonitorCleanupMaxBudgetSeconds {
		budgetSeconds = channelMonitorCleanupDefaultBudgetSeconds
	}
	cleanupBudget := model.NewChannelMonitorCleanupBudget(time.Duration(budgetSeconds) * time.Second)
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
	statusProbeHistoryCutoff := channelMonitorHistoryRetentionCutoff(now, settings.StatusProbeHistoryRetentionDays)
	modelDetectionRetentionDays := channelModelDetectionRetentionDays()
	modelDetectionCutoff := channelMonitorHistoryRetentionCutoff(now, modelDetectionRetentionDays)
	result := channelMonitorCostRetentionTaskResult{
		RetentionDays:                   settings.CostRetentionDays,
		Cutoff:                          costCutoff,
		MinuteCutoff:                    minuteCutoff,
		ProtectedWindowMinutes:          protectedWindowMinutes,
		ExecutionDetailRetentionDays:    settings.ExecutionDetailRetentionDays,
		ExecutionDetailCutoff:           historyCutoffs.ExecutionDetail,
		TaskRetentionDays:               settings.TaskRetentionDays,
		TaskCutoff:                      historyCutoffs.Task,
		RatioHistoryRetentionDays:       settings.RatioHistoryRetentionDays,
		RatioHistoryCutoff:              historyCutoffs.RatioHistory,
		StatusProbeHistoryRetentionDays: settings.StatusProbeHistoryRetentionDays,
		StatusProbeHistoryCutoff:        statusProbeHistoryCutoff,
		ModelDetectionRetentionDays:     modelDetectionRetentionDays,
		ModelDetectionCutoff:            modelDetectionCutoff,
	}
	modelDetectionDeleted, err := model.DeleteChannelModelDetectionHistoryBefore(
		ctx,
		modelDetectionCutoff,
		batchSize,
		cleanupBudget.Slice(4),
	)
	result.ChannelModelDetectionRetentionResult = modelDetectionDeleted
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, result, err)
		return
	}
	result.BudgetExhausted = modelDetectionDeleted.Incomplete
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
		cleanupBudget.Slice(3),
	)
	result.ChannelMonitorHistoryRetentionResult = historyDeleted
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, result, err)
		return
	}
	result.BudgetExhausted = result.BudgetExhausted || historyDeleted.Incomplete
	statusProbeBudget := cleanupBudget.Slice(2)
	statusProbeDeleted, err := model.DeleteChannelStatusProbeExecutionsBefore(
		ctx,
		statusProbeHistoryCutoff,
		batchSize,
		statusProbeBudget,
	)
	result.StatusProbeExecutionsDeleted = statusProbeDeleted
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, result, err)
		return
	}
	result.BudgetExhausted = result.BudgetExhausted || statusProbeBudget.Exhausted()
	costDeleted, err := model.DeleteChannelMonitorCostsBefore(
		ctx, costCutoff, minuteCutoff, batchSize, cleanupBudget.Slice(1),
	)
	result.ChannelMonitorCostRetentionResult = costDeleted
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, result, err)
		return
	}
	result.BudgetExhausted = result.BudgetExhausted || costDeleted.Incomplete || cleanupBudget.Exhausted()
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, result, nil)
	if result.BudgetExhausted {
		scheduleChannelMonitorCleanupContinuation()
	}
}
