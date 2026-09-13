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
func runChannelMonitorCustomActions(ctx context.Context, monitor model.ChannelRatioMonitor, metric string, value float64, proxy string, timeout time.Duration) {
	if err := service.RunChannelMonitorCustomActions(ctx, monitor, metric, value, proxy, timeout); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("渠道 %d 自定义触发接口未完成: %v", monitor.ChannelId, err))
	}
}
