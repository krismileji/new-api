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
	channelMonitorCostRetentionTaskType   = "channel_monitor_cost_retention"
	channelMonitorTaskRetentionKeepLatest = 100
)

var channelMonitorCleanupContinuationScheduler = func(delay time.Duration, callback func()) {
	time.AfterFunc(delay, callback)
}

type channelMonitorCostRetentionTaskHandler struct{}

type channelMonitorCostRetentionTaskResult struct {
	RetentionDays                   int   `json:"retention_days"`
	Cutoff                          int64 `json:"cutoff"`
	MinuteCutoff                    int64 `json:"minute_cutoff"`
	RouteMetricRetentionDays        int   `json:"route_metric_retention_days"`
	RouteMetricCutoff               int64 `json:"route_metric_cutoff"`
	APIKeyMetricRetentionDays       int   `json:"api_key_metric_retention_days"`
	APIKeyMetricCutoff              int64 `json:"api_key_metric_cutoff"`
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

type channelMonitorCleanupSettings struct {
	Enabled             bool
	BatchSize           int
	BudgetSeconds       int
	ContinuationSeconds int
	IntervalMinutes     int
}

func init() {
	service.RegisterSystemTaskHandler(channelMonitorCostRetentionTaskHandler{})
}

func (channelMonitorCostRetentionTaskHandler) Type() string {
	return channelMonitorCostRetentionTaskType
}

func (channelMonitorCostRetentionTaskHandler) Enabled() bool {
	settings, err := loadChannelMonitorCleanupSettings(context.Background())
	if err != nil {
		common.SysError("读取渠道监控清理配置失败: " + err.Error())
		return false
	}
	return settings.Enabled
}

func (channelMonitorCostRetentionTaskHandler) Interval() time.Duration {
	settings, err := loadChannelMonitorCleanupSettings(context.Background())
	if err != nil {
		common.SysError("读取渠道监控清理周期失败: " + err.Error())
		return time.Duration(defaultChannelMonitorCleanupIntervalMinutes) * time.Minute
	}
	return time.Duration(settings.IntervalMinutes) * time.Minute
}

func (channelMonitorCostRetentionTaskHandler) NewPayload() any { return nil }

func scheduleChannelMonitorCleanupContinuation() {
	settings, err := loadChannelMonitorCleanupSettings(context.Background())
	if err != nil {
		common.SysError("读取渠道监控清理续跑配置失败: " + err.Error())
		return
	}
	if !settings.Enabled {
		return
	}
	channelMonitorCleanupContinuationScheduler(
		time.Duration(settings.ContinuationSeconds)*time.Second,
		func() {
			latest, err := loadChannelMonitorCleanupSettings(context.Background())
			if err != nil {
				common.SysError("续排渠道监控清理任务前读取配置失败: " + err.Error())
				return
			}
			if !latest.Enabled {
				return
			}
			if _, _, err := service.EnqueueSystemTask(channelMonitorCostRetentionTaskType, nil); err != nil {
				common.SysError("续排渠道监控清理任务失败: " + err.Error())
			}
		},
	)
}

func loadChannelMonitorCleanupSettings(ctx context.Context) (channelMonitorCleanupSettings, error) {
	settings := channelMonitorCleanupSettings{
		Enabled:             defaultChannelMonitorCleanupEnabled,
		BatchSize:           defaultChannelMonitorCleanupBatchSize,
		BudgetSeconds:       defaultChannelMonitorCleanupBudgetSeconds,
		ContinuationSeconds: defaultChannelMonitorCleanupContinuationSeconds,
		IntervalMinutes:     defaultChannelMonitorCleanupIntervalMinutes,
	}
	var options []model.Option
	keys := []string{
		channelMonitorCleanupEnabledOption,
		channelMonitorCleanupBatchSizeOption,
		channelMonitorCleanupBudgetSecondsOption,
		channelMonitorCleanupContinuationSecondsOption,
		channelMonitorCleanupIntervalMinutesOption,
	}
	if err := model.DB.WithContext(ctx).
		Select("key", "value").
		Where(map[string]any{"key": keys}).
		Find(&options).Error; err != nil {
		return channelMonitorCleanupSettings{}, fmt.Errorf("读取渠道监控清理配置失败: %w", err)
	}
	for _, option := range options {
		switch option.Key {
		case channelMonitorCleanupEnabledOption:
			enabled, err := strconv.ParseBool(option.Value)
			if err != nil {
				return channelMonitorCleanupSettings{}, fmt.Errorf("渠道监控清理配置 %s 无效", option.Key)
			}
			settings.Enabled = enabled
		case channelMonitorCleanupBatchSizeOption:
			batchSize, err := strconv.Atoi(option.Value)
			if err != nil || batchSize < minChannelMonitorCleanupBatchSize || batchSize > maxChannelMonitorCleanupBatchSize {
				return channelMonitorCleanupSettings{}, fmt.Errorf("渠道监控清理配置 %s 无效", option.Key)
			}
			settings.BatchSize = batchSize
		case channelMonitorCleanupBudgetSecondsOption:
			budgetSeconds, err := strconv.Atoi(option.Value)
			if err != nil || budgetSeconds < minChannelMonitorCleanupBudgetSeconds || budgetSeconds > maxChannelMonitorCleanupBudgetSeconds {
				return channelMonitorCleanupSettings{}, fmt.Errorf("渠道监控清理配置 %s 无效", option.Key)
			}
			settings.BudgetSeconds = budgetSeconds
		case channelMonitorCleanupContinuationSecondsOption:
			continuationSeconds, err := strconv.Atoi(option.Value)
			if err != nil || continuationSeconds < minChannelMonitorCleanupContinuationSeconds ||
				continuationSeconds > maxChannelMonitorCleanupContinuationSeconds {
				return channelMonitorCleanupSettings{}, fmt.Errorf("渠道监控清理配置 %s 无效", option.Key)
			}
			settings.ContinuationSeconds = continuationSeconds
		case channelMonitorCleanupIntervalMinutesOption:
			intervalMinutes, err := strconv.Atoi(option.Value)
			if err != nil || intervalMinutes < minChannelMonitorCleanupIntervalMinutes ||
				intervalMinutes > maxChannelMonitorCleanupIntervalMinutes {
				return channelMonitorCleanupSettings{}, fmt.Errorf("渠道监控清理配置 %s 无效", option.Key)
			}
			settings.IntervalMinutes = intervalMinutes
		}
	}
	return settings, nil
}

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
		RouteMetricRetentionDays:              defaultChannelMonitorRouteMetricRetentionDays,
		APIKeyMetricRetentionDays:             defaultChannelMonitorAPIKeyMetricRetentionDays,
		ExecutionDetailRetentionDays:          defaultChannelMonitorExecutionDetailRetentionDays,
		TaskRetentionDays:                     defaultChannelMonitorTaskRetentionDays,
		RatioHistoryRetentionDays:             defaultChannelMonitorRatioHistoryRetentionDays,
		StatusProbeHistoryRetentionDays:       defaultChannelMonitorStatusProbeHistoryRetentionDays,
		ModelDetectionRetentionDays:           model.ChannelModelDetectionDefaultRetentionDays,
		SmartSchedulePerformanceWindowMinutes: defaultChannelMonitorSmartSchedulePerformanceWindowMinutes,
		SmartScheduleStabilityWindowMinutes:   defaultChannelMonitorSmartScheduleStabilityWindowMinutes,
	}
	var options []model.Option
	retentionOptionKeys := []string{
		channelMonitorCostRetentionDaysOption,
		channelMonitorRouteMetricRetentionDaysOption,
		channelMonitorAPIKeyMetricRetentionDaysOption,
		channelMonitorExecutionDetailRetentionDaysOption,
		channelMonitorTaskRetentionDaysOption,
		channelMonitorRatioHistoryRetentionDaysOption,
		channelMonitorStatusProbeHistoryRetentionDaysOption,
		channelMonitorModelDetectionRetentionDaysOption,
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
		case channelMonitorRouteMetricRetentionDaysOption:
			days, err := strconv.Atoi(option.Value)
			if err != nil || days < minChannelMonitorCostRetentionDays || days > maxChannelMonitorCostRetentionDays {
				return channelMonitorSettings{}, fmt.Errorf("渠道监控保留配置 %s 无效", option.Key)
			}
			settings.RouteMetricRetentionDays = days
		case channelMonitorAPIKeyMetricRetentionDaysOption:
			days, err := strconv.Atoi(option.Value)
			if err != nil || days < minChannelMonitorCostRetentionDays || days > maxChannelMonitorCostRetentionDays {
				return channelMonitorSettings{}, fmt.Errorf("渠道监控保留配置 %s 无效", option.Key)
			}
			settings.APIKeyMetricRetentionDays = days
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
		case channelMonitorModelDetectionRetentionDaysOption:
			days, err := strconv.Atoi(option.Value)
			if err != nil || days < model.ChannelModelDetectionMinRetentionDays ||
				days > model.ChannelModelDetectionMaxRetentionDays {
				return channelMonitorSettings{}, fmt.Errorf("渠道监控保留配置 %s 无效", option.Key)
			}
			settings.ModelDetectionRetentionDays = days
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
	cleanupSettings, err := loadChannelMonitorCleanupSettings(ctx)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, channelMonitorCostRetentionTaskResult{}, err)
		return
	}
	if !cleanupSettings.Enabled {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, channelMonitorCostRetentionTaskResult{}, nil)
		return
	}
	settings, err := loadChannelMonitorRetentionSettings(ctx)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, channelMonitorCostRetentionTaskResult{}, err)
		return
	}
	batchSize := cleanupSettings.BatchSize
	cleanupBudget := model.NewChannelMonitorCleanupBudget(time.Duration(cleanupSettings.BudgetSeconds) * time.Second)
	now := common.GetTimestamp()
	costCutoff := channelMonitorCostRetentionCutoff(now, settings.CostRetentionDays)
	routeMetricConfiguredCutoff := channelMonitorHistoryRetentionCutoff(now, settings.RouteMetricRetentionDays)
	routeMetricCutoff, protectedWindowMinutes := channelMonitorMinuteRetentionCutoff(
		now,
		routeMetricConfiguredCutoff,
		settings.SmartSchedulePerformanceWindowMinutes,
		settings.SmartScheduleStabilityWindowMinutes,
	)
	apiKeyMetricCutoff := channelMonitorHistoryRetentionCutoff(now, settings.APIKeyMetricRetentionDays)
	historyCutoffs := model.ChannelMonitorHistoryRetentionCutoffs{
		ExecutionDetail: channelMonitorHistoryRetentionCutoff(now, settings.ExecutionDetailRetentionDays),
		Task:            channelMonitorHistoryRetentionCutoff(now, settings.TaskRetentionDays),
		RatioHistory:    channelMonitorHistoryRetentionCutoff(now, settings.RatioHistoryRetentionDays),
	}
	statusProbeHistoryCutoff := channelMonitorHistoryRetentionCutoff(now, settings.StatusProbeHistoryRetentionDays)
	modelDetectionRetentionDays := settings.ModelDetectionRetentionDays
	modelDetectionCutoff := channelMonitorHistoryRetentionCutoff(now, modelDetectionRetentionDays)
	result := channelMonitorCostRetentionTaskResult{
		RetentionDays:                   settings.CostRetentionDays,
		Cutoff:                          costCutoff,
		MinuteCutoff:                    routeMetricCutoff,
		RouteMetricRetentionDays:        settings.RouteMetricRetentionDays,
		RouteMetricCutoff:               routeMetricCutoff,
		APIKeyMetricRetentionDays:       settings.APIKeyMetricRetentionDays,
		APIKeyMetricCutoff:              apiKeyMetricCutoff,
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
		ctx, costCutoff, routeMetricCutoff, apiKeyMetricCutoff, batchSize, cleanupBudget.Slice(1),
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
