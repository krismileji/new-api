package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

// An action failure must not turn a successful metric fetch into a failure or
// enter the monitor's automatic fetch retry loop.
func runChannelMonitorCustomActions(ctx context.Context, monitor model.ChannelRatioMonitor, metric string, value float64, proxy string, timeout time.Duration, options channelMonitorRefreshOptions) bool {
	succeeded, err := service.RunChannelMonitorCustomActions(ctx, monitor, metric, value, proxy, timeout, !options.SkipCustomActions)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("渠道 %d 自定义触发接口未完成: %v", monitor.ChannelId, err))
	}
	return succeeded
}

// Replace the pre-action sample before the caller evaluates balance and ratio
// policies. Follow-up reads can rearm rules, but cannot execute another action.
func refreshChannelMonitorAfterCustomAction(ctx context.Context, monitor model.ChannelRatioMonitor, channelKeys []string, proxy string, timeout time.Duration, operatorID int, operatorUsername string) (outcome channelMonitorFetchOutcome, err error) {
	defer func() {
		outcome.CustomActionSucceeded = true
		if err != nil {
			err = fmt.Errorf("触发接口已成功，但重新获取上游指标失败: %w", err)
		}
	}()
	current, err := model.GetChannelRatioMonitorWithContext(ctx, monitor.ChannelId)
	if err != nil {
		return outcome, err
	}
	if current.UpstreamRevision != monitor.UpstreamRevision {
		return outcome, model.ErrChannelRatioMonitorConfigChanged
	}
	// Preserve the caller's one-off manual refresh or automatic sync pause.
	current.UpstreamRatioSyncDisabled = monitor.UpstreamRatioSyncDisabled
	current.UpstreamBalanceSyncDisabled = monitor.UpstreamBalanceSyncDisabled
	options := channelMonitorRefreshOptions{IncludeSeparateBalance: true, SkipCustomActions: true}
	if !current.UpstreamRatioSyncDisabled {
		return fetchAndRecordChannelMonitorUpstreamRatio(ctx, current, channelKeys, proxy, timeout, options, operatorID, operatorUsername)
	}
	return fetchAndRecordChannelMonitorUpstreamBalance(ctx, current, channelKeys, proxy, timeout, options)
}
