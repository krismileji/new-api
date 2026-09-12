package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// ErrChannelMonitorRedisRetryable marks a recoverable dependency or scheduling
// conflict. Retrying such an event must not exhaust its poison-message budget.
var ErrChannelMonitorRedisRetryable = errors.New("渠道监控事件等待恢复后重试")

func isChannelMonitorRedisRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrChannelMonitorRedisRetryable) || errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, ErrChannelMonitorRedisAggregatorLeaseLost) || errors.Is(err, ErrChannelMonitorRedisEffectProcessing) ||
		errors.Is(err, ErrChannelMonitorRedisEffectOwnershipLost) || errors.Is(err, model.ErrChannelLogicalGroupRevisionConflict) {
		return true
	}
	// go-redis v8 keeps its pool timeout sentinel internal and it does not
	// implement net.Error. Pool congestion still says nothing about the event.
	if strings.Contains(err.Error(), "redis: connection pool timeout") {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}

func (consumer *ChannelMonitorRedisEventConsumer) recordRetryableFailure(err error, messageCount int) {
	incrementChannelMonitorRedisObservation(consumer.client, ChannelMonitorRedisObservabilityFieldRetryCount, int64(messageCount))
	now := time.Now().Unix()
	previous := consumer.lastRetryLogAt.Load()
	if now-previous < 30 || !consumer.lastRetryLogAt.CompareAndSwap(previous, now) {
		return
	}
	common.SysError(fmt.Sprintf("渠道监控 Redis 事件暂缓处理，保留待重试: count=%d error=%s", messageCount, err))
}
