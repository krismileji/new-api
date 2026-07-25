package controller

import (
	"context"
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
)

type channelMonitorCostRetentionTaskHandler struct{}

type channelMonitorCostRetentionTaskResult struct {
	RetentionDays int   `json:"retention_days"`
	Cutoff        int64 `json:"cutoff"`
	model.ChannelMonitorCostRetentionResult
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

func (channelMonitorCostRetentionTaskHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	days := getChannelMonitorSettings().CostRetentionDays
	batchSize := common.GetEnvOrDefault("CHANNEL_MONITOR_COST_RETENTION_BATCH_SIZE", channelMonitorCostRetentionDefaultBatch)
	if batchSize <= 0 || batchSize > channelMonitorCostRetentionMaxBatch {
		batchSize = channelMonitorCostRetentionDefaultBatch
	}
	cutoff := channelMonitorCostRetentionCutoff(common.GetTimestamp(), days)
	deleted, err := model.DeleteChannelMonitorCostsBefore(ctx, cutoff, batchSize)
	result := channelMonitorCostRetentionTaskResult{
		RetentionDays:                     days,
		Cutoff:                            cutoff,
		ChannelMonitorCostRetentionResult: deleted,
	}
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, result, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, result, nil)
}
