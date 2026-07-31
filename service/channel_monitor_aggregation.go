package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	channelMonitorAggregationInterval = time.Minute
	channelMonitorAggregationTail     = 5 * time.Minute
)

var channelMonitorAggregationOnce sync.Once

// StartChannelMonitorAggregationWorker refreshes the persisted minute
// aggregates in the background. Only the master node starts the worker.
func StartChannelMonitorAggregationWorker() {
	channelMonitorAggregationOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			run := func() {
				if err := runChannelMonitorAggregationOnce(context.Background()); err != nil {
					logger.LogWarn(context.Background(), fmt.Sprintf("渠道监控分钟聚合失败: %v", err))
				}
			}
			run()
			ticker := time.NewTicker(channelMonitorAggregationInterval)
			defer ticker.Stop()
			for range ticker.C {
				run()
			}
		})
	})
}

func runChannelMonitorAggregationOnce(ctx context.Context) error {
	routingChanged, err := model.ClearExpiredChannelSmartScheduleRoutePrimaries(common.GetTimestamp())
	if err != nil {
		return fmt.Errorf("清理到期的固定主渠道失败: %w", err)
	}
	if routingChanged && common.MemoryCacheEnabled {
		model.InitChannelCache()
	}
	if !common.LogConsumeEnabled && !constant.ErrorLogEnabled {
		return nil
	}
	now := common.GetTimestamp()
	targetEnd := now - now%int64(channelMonitorAggregationInterval/time.Second)
	start := targetEnd - int64(channelMonitorAggregationTail/time.Second)
	if start < 0 {
		start = 0
	}
	_, err = model.AggregateChannelMonitorMinuteRange(ctx, start, targetEnd)
	return err
}
