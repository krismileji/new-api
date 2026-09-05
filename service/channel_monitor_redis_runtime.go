package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	channelMonitorRedisRuntimeInitialRetryDelay = time.Second
	channelMonitorRedisRuntimeMaxRetryDelay     = 30 * time.Second
)

type channelMonitorRedisConsumerRunner interface {
	Run(context.Context) error
}

// ChannelMonitorRedisRuntime supervises the shared Stream consumer. Redis
// disconnects stop one consumer run, then the supervisor recreates the group
// if needed and resumes from pending or unread messages.
type ChannelMonitorRedisRuntime struct {
	cancel        context.CancelFunc
	done          chan struct{}
	stopOnce      sync.Once
	initStream    func(context.Context) error
	consumer      channelMonitorRedisConsumerRunner
	initialRetry  time.Duration
	maximumRetry  time.Duration
	reportFailure func(error)
}

func StartChannelMonitorRedisRuntime() (*ChannelMonitorRedisRuntime, error) {
	if err := InitChannelMonitorRedisStream(context.Background()); err != nil {
		return nil, err
	}
	aggregator, err := NewChannelMonitorRedisLogicalAggregator()
	if err != nil {
		return nil, fmt.Errorf("渠道监控 Redis 聚合器初始化失败: %w", err)
	}
	consumer, err := NewChannelMonitorRedisEventConsumer(aggregator)
	if err != nil {
		return nil, fmt.Errorf("渠道监控 Redis 消费者初始化失败: %w", err)
	}
	return startChannelMonitorRedisRuntime(
		InitChannelMonitorRedisStream,
		consumer,
		channelMonitorRedisRuntimeInitialRetryDelay,
		channelMonitorRedisRuntimeMaxRetryDelay,
		func(err error) {
			common.SysError("渠道监控 Redis 消费者中断，将自动恢复: " + err.Error())
		},
	), nil
}

func startChannelMonitorRedisRuntime(
	initStream func(context.Context) error,
	consumer channelMonitorRedisConsumerRunner,
	initialRetry time.Duration,
	maximumRetry time.Duration,
	reportFailure func(error),
) *ChannelMonitorRedisRuntime {
	if initialRetry <= 0 {
		initialRetry = channelMonitorRedisRuntimeInitialRetryDelay
	}
	if maximumRetry < initialRetry {
		maximumRetry = initialRetry
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &ChannelMonitorRedisRuntime{
		cancel:        cancel,
		done:          make(chan struct{}),
		initStream:    initStream,
		consumer:      consumer,
		initialRetry:  initialRetry,
		maximumRetry:  maximumRetry,
		reportFailure: reportFailure,
	}
	go runtime.run(ctx)
	return runtime
}

func (runtime *ChannelMonitorRedisRuntime) run(ctx context.Context) {
	defer close(runtime.done)
	retryDelay := runtime.initialRetry
	for {
		err := runtime.initStream(ctx)
		if err == nil {
			err = runtime.consumer.Run(ctx)
		}
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("渠道监控 Redis 消费者意外退出")
		}
		if runtime.reportFailure != nil {
			runtime.reportFailure(err)
		}
		NotifyChannelMonitorHealthFromCurrentConfig(
			string(ChannelMonitorHealthDegraded),
			[]string{ChannelMonitorRedisDegradedReasonConsumerStopped},
			0,
		)
		incrementChannelMonitorRedisObservation(
			common.RedisMonitorConsumerClient(),
			ChannelMonitorRedisObservabilityFieldRetryCount,
			1,
		)
		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
		if retryDelay < runtime.maximumRetry {
			retryDelay = min(retryDelay*2, runtime.maximumRetry)
		}
	}
}

func (runtime *ChannelMonitorRedisRuntime) Stop(ctx context.Context) error {
	if runtime == nil {
		return nil
	}
	runtime.stopOnce.Do(runtime.cancel)
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-runtime.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
