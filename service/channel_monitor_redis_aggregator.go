package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

// Replay protection uses one TTL so ACK deduplication and external effect
// markers have the same redelivery boundary.
const (
	channelMonitorRedisReplayProtectionTTL    = 48 * time.Hour
	channelMonitorRedisRuntimeEffectTTL       = channelMonitorRedisReplayProtectionTTL
	channelMonitorRedisSchedulingDedupTTL     = channelMonitorRedisReplayProtectionTTL
	channelMonitorRedisEffectProcessingTTL    = 25 * time.Second
	channelMonitorRedisEffectDoneValue        = "done"
	channelMonitorRedisEffectProcessingPrefix = "processing:"
)

var ErrChannelMonitorRedisRuntimeEffectUnavailable = errors.New("渠道监控 Redis 运行时副作用处理器不可用")
var ErrChannelMonitorRedisEffectProcessing = errors.New("渠道监控 Redis 副作用正在由其他 owner 处理")
var ErrChannelMonitorRedisEffectOwnershipLost = errors.New("渠道监控 Redis 副作用 owner 已失效")

const channelMonitorRedisAcquireEffectMarkersScript = `
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
  return {-1}
end
local processing = ARGV[2] .. ARGV[1]
for index = 2, #KEYS do
  local current = redis.call('GET', KEYS[index])
  if current and current ~= ARGV[3] and current ~= processing then
    return {-2}
  end
end
local result = {}
for index = 2, #KEYS do
  local current = redis.call('GET', KEYS[index])
  if current == ARGV[3] then
    table.insert(result, 0)
  else
    redis.call('SET', KEYS[index], processing, 'PX', ARGV[4])
    table.insert(result, 1)
  end
end
return result
`

const channelMonitorRedisCompleteEffectMarkersScript = `
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
  return -1
end
local processing = ARGV[2] .. ARGV[1]
for index = 2, #KEYS do
  local current = redis.call('GET', KEYS[index])
  if current and current ~= processing then
    return -2
  end
end
for index = 2, #KEYS do
  redis.call('SET', KEYS[index], ARGV[3], 'PX', ARGV[4])
end
return #KEYS - 1
`

const channelMonitorRedisRenewEffectMarkersScript = `
local processing = ARGV[2] .. ARGV[1]
local renewed = 0
for index = 1, #KEYS do
  if redis.call('GET', KEYS[index]) == processing then
    redis.call('PEXPIRE', KEYS[index], ARGV[3])
    renewed = renewed + 1
  end
end
return renewed
`

const channelMonitorRedisReleaseEffectMarkersScript = `
local processing = ARGV[2] .. ARGV[1]
local released = 0
for index = 1, #KEYS do
  if redis.call('GET', KEYS[index]) == processing then
    released = released + redis.call('DEL', KEYS[index])
  end
end
return released
`

type ChannelMonitorRedisRuntimeEffectHandler func(context.Context, []model.ChannelMonitorEvent) error

type channelMonitorRedisRuntimeEffectHandlerHolder struct {
	handle ChannelMonitorRedisRuntimeEffectHandler
}

var channelMonitorRedisRuntimeEffectHandler atomic.Pointer[channelMonitorRedisRuntimeEffectHandlerHolder]

func RegisterChannelMonitorRedisRuntimeEffectHandler(handle ChannelMonitorRedisRuntimeEffectHandler) bool {
	if handle == nil {
		return false
	}
	channelMonitorRedisRuntimeEffectHandler.Store(&channelMonitorRedisRuntimeEffectHandlerHolder{handle: handle})
	return true
}

// ChannelMonitorRedisSchedulingTrigger is an optional compatibility callback
// used by explicit test/integration constructors. The production aggregator
// deliberately omits it so request events never trigger full scheduling.
type ChannelMonitorRedisSchedulingTrigger func(context.Context, []model.ChannelMonitorEvent) error

// ChannelMonitorRedisLogicalAggregator is the only REDIS-03 handler that
// combines shared projections with runtime scheduling side effects. REDIS-08
// owns installing it into the runtime consumer.
type ChannelMonitorRedisLogicalAggregator struct {
	client           *redis.Client
	routeHealth      ChannelMonitorRedisEventHandler
	sharedProjection ChannelMonitorRedisEventHandler
	runtimeEffect    ChannelMonitorRedisRuntimeEffectHandler
	triggerSchedule  ChannelMonitorRedisSchedulingTrigger
}

var _ ChannelMonitorRedisEventHandler = (*ChannelMonitorRedisLogicalAggregator)(nil)

func NewChannelMonitorRedisLogicalAggregator() (*ChannelMonitorRedisLogicalAggregator, error) {
	client := common.RedisMonitorConsumerClient()
	if !common.RedisEnabled || client == nil {
		return nil, ErrChannelMonitorRedisConsumerUnavailable
	}
	runtimeHolder := channelMonitorRedisRuntimeEffectHandler.Load()
	if runtimeHolder == nil || runtimeHolder.handle == nil {
		return nil, ErrChannelMonitorRedisRuntimeEffectUnavailable
	}
	return NewChannelMonitorRedisLogicalAggregatorWithClient(client, runtimeHolder.handle, nil)
}

func NewChannelMonitorRedisLogicalAggregatorWithClient(
	client *redis.Client,
	runtimeEffect ChannelMonitorRedisRuntimeEffectHandler,
	trigger ChannelMonitorRedisSchedulingTrigger,
) (*ChannelMonitorRedisLogicalAggregator, error) {
	routeHealth, err := NewChannelMonitorRedisRouteHealthProjectionForClient(client)
	if err != nil {
		return nil, err
	}
	return newChannelMonitorRedisLogicalAggregator(
		client,
		routeHealth,
		NewChannelMonitorRedisSharedProjectionWithClient(client),
		runtimeEffect,
		trigger,
	)
}

func newChannelMonitorRedisLogicalAggregator(
	client *redis.Client,
	routeHealth ChannelMonitorRedisEventHandler,
	sharedProjection ChannelMonitorRedisEventHandler,
	runtimeEffect ChannelMonitorRedisRuntimeEffectHandler,
	trigger ChannelMonitorRedisSchedulingTrigger,
) (*ChannelMonitorRedisLogicalAggregator, error) {
	if client == nil || routeHealth == nil || sharedProjection == nil {
		return nil, ErrChannelMonitorRedisConsumerUnavailable
	}
	if runtimeEffect == nil {
		return nil, ErrChannelMonitorRedisRuntimeEffectUnavailable
	}
	return &ChannelMonitorRedisLogicalAggregator{
		client:           client,
		routeHealth:      routeHealth,
		sharedProjection: sharedProjection,
		runtimeEffect:    runtimeEffect,
		triggerSchedule:  trigger,
	}, nil
}

func (aggregator *ChannelMonitorRedisLogicalAggregator) HandleChannelMonitorEvents(
	ctx context.Context,
	events []model.ChannelMonitorEvent,
) error {
	if aggregator == nil || aggregator.client == nil || aggregator.routeHealth == nil ||
		aggregator.sharedProjection == nil || aggregator.runtimeEffect == nil {
		return ErrChannelMonitorRedisConsumerUnavailable
	}
	if len(events) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := aggregator.routeHealth.HandleChannelMonitorEvents(ctx, events); err != nil {
		return err
	}
	if err := aggregator.sharedProjection.HandleChannelMonitorEvents(ctx, events); err != nil {
		return err
	}

	eligibleEvents := make([]model.ChannelMonitorEvent, 0, len(events))
	seenEventIDs := make(map[string]struct{}, len(events))
	for _, event := range events {
		if !event.SchedulingEligible {
			continue
		}
		if event.EventSequence == 0 || event.EventSequence > math.MaxInt64 {
			return errors.New("渠道监控 Redis 副作用事件顺序无效")
		}
		if _, duplicate := seenEventIDs[event.EventId]; duplicate {
			continue
		}
		seenEventIDs[event.EventId] = struct{}{}
		eligibleEvents = append(eligibleEvents, event)
	}
	if len(eligibleEvents) == 0 {
		return nil
	}
	if err := aggregator.applyEffect(
		ctx,
		eligibleEvents,
		ChannelMonitorRedisRuntimeEffectKey,
		channelMonitorRedisRuntimeEffectTTL,
		ChannelMonitorRedisObservabilityFieldRuntimeMarkerFailureCount,
		aggregator.runtimeEffect,
	); err != nil {
		return err
	}
	if aggregator.triggerSchedule == nil {
		return nil
	}
	return aggregator.applyEffect(
		ctx,
		eligibleEvents,
		ChannelMonitorRedisSchedulingDedupKey,
		channelMonitorRedisSchedulingDedupTTL,
		ChannelMonitorRedisObservabilityFieldScheduleMarkerFailureCount,
		aggregator.triggerSchedule,
	)
}

func (aggregator *ChannelMonitorRedisLogicalAggregator) applyEffect(
	ctx context.Context,
	events []model.ChannelMonitorEvent,
	keyForEvent func(string) string,
	doneTTL time.Duration,
	markerFailureField string,
	apply func(context.Context, []model.ChannelMonitorEvent) error,
) error {
	owner, available := channelMonitorRedisEffectOwnerFromContext(ctx)
	if !available {
		return ErrChannelMonitorRedisEffectOwnershipLost
	}
	keys := make([]string, 0, len(events))
	for _, event := range events {
		keys = append(keys, keyForEvent(event.EventId))
	}
	claimed, err := aggregator.acquireEffectMarkers(ctx, owner, keys)
	if err != nil {
		aggregator.recordEffectMarkerFailure(markerFailureField, len(keys), err)
		return err
	}
	pendingEvents := make([]model.ChannelMonitorEvent, 0, len(events))
	pendingKeys := make([]string, 0, len(events))
	for index, wasClaimed := range claimed {
		if !wasClaimed {
			continue
		}
		pendingEvents = append(pendingEvents, events[index])
		pendingKeys = append(pendingKeys, keys[index])
	}
	if len(pendingEvents) == 0 {
		return nil
	}
	// A batch can include many side effects and exceed the marker TTL. Keep
	// processing markers alive while the callback is running so a takeover
	// cannot race an in-flight callback and cause an endless redelivery loop.
	renewCtx, stopRenew := context.WithCancel(ctx)
	renewDone := make(chan struct{})
	go aggregator.renewEffectMarkers(renewCtx, owner, pendingKeys, renewDone)
	err = apply(renewCtx, pendingEvents)
	stopRenew()
	<-renewDone
	if err != nil {
		return errors.Join(err, aggregator.releaseEffectMarkers(ctx, owner, pendingKeys))
	}
	if err := aggregator.completeEffectMarkers(ctx, owner, pendingKeys, doneTTL); err != nil {
		aggregator.recordEffectMarkerFailure(markerFailureField, len(pendingKeys), err)
		return err
	}
	return nil
}

func (aggregator *ChannelMonitorRedisLogicalAggregator) renewEffectMarkers(
	ctx context.Context,
	owner string,
	keys []string,
	done chan<- struct{},
) {
	defer close(done)
	interval := channelMonitorRedisEffectProcessingTTL / 3
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			opCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisSharedOperationTimeout)
			_, err := aggregator.client.Eval(
				opCtx,
				channelMonitorRedisRenewEffectMarkersScript,
				keys,
				owner,
				channelMonitorRedisEffectProcessingPrefix,
				channelMonitorRedisEffectProcessingTTL.Milliseconds(),
			).Result()
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (aggregator *ChannelMonitorRedisLogicalAggregator) acquireEffectMarkers(
	ctx context.Context,
	owner string,
	keys []string,
) ([]bool, error) {
	redisKeys := make([]string, 0, len(keys)+1)
	redisKeys = append(redisKeys, ChannelMonitorRedisAggregatorLeaseKey)
	redisKeys = append(redisKeys, keys...)
	result, err := aggregator.client.Eval(
		ctx,
		channelMonitorRedisAcquireEffectMarkersScript,
		redisKeys,
		owner,
		channelMonitorRedisEffectProcessingPrefix,
		channelMonitorRedisEffectDoneValue,
		channelMonitorRedisEffectProcessingTTL.Milliseconds(),
	).Result()
	if err != nil {
		return nil, err
	}
	values, ok := result.([]interface{})
	if !ok || len(values) == 0 {
		return nil, errors.New("渠道监控 Redis 副作用 marker 获取结果无效")
	}
	if len(values) == 1 {
		code, parseErr := channelMonitorRedisReplyInt64(values[0])
		if parseErr != nil {
			return nil, parseErr
		}
		switch code {
		case -1:
			return nil, ErrChannelMonitorRedisEffectOwnershipLost
		case -2:
			return nil, ErrChannelMonitorRedisEffectProcessing
		}
	}
	if len(values) != len(keys) {
		return nil, fmt.Errorf("渠道监控 Redis 副作用 marker 数量不一致: got=%d want=%d", len(values), len(keys))
	}
	claimed := make([]bool, len(values))
	for index, value := range values {
		code, parseErr := channelMonitorRedisReplyInt64(value)
		if parseErr != nil || (code != 0 && code != 1) {
			return nil, errors.New("渠道监控 Redis 副作用 marker 状态无效")
		}
		claimed[index] = code == 1
	}
	return claimed, nil
}

func (aggregator *ChannelMonitorRedisLogicalAggregator) completeEffectMarkers(
	ctx context.Context,
	owner string,
	keys []string,
	doneTTL time.Duration,
) error {
	redisKeys := make([]string, 0, len(keys)+1)
	redisKeys = append(redisKeys, ChannelMonitorRedisAggregatorLeaseKey)
	redisKeys = append(redisKeys, keys...)
	completed, err := aggregator.client.Eval(
		ctx,
		channelMonitorRedisCompleteEffectMarkersScript,
		redisKeys,
		owner,
		channelMonitorRedisEffectProcessingPrefix,
		channelMonitorRedisEffectDoneValue,
		doneTTL.Milliseconds(),
	).Int64()
	if err != nil {
		return err
	}
	switch completed {
	case -1, -2:
		return ErrChannelMonitorRedisEffectOwnershipLost
	}
	if completed != int64(len(keys)) {
		return fmt.Errorf("渠道监控 Redis 副作用 marker 完成数量不一致: got=%d want=%d", completed, len(keys))
	}
	return nil
}

func (aggregator *ChannelMonitorRedisLogicalAggregator) releaseEffectMarkers(
	ctx context.Context,
	owner string,
	keys []string,
) error {
	if len(keys) == 0 {
		return nil
	}
	err := aggregator.client.Eval(
		ctx,
		channelMonitorRedisReleaseEffectMarkersScript,
		keys,
		owner,
		channelMonitorRedisEffectProcessingPrefix,
	).Err()
	if err != nil {
		recordChannelMonitorRedisFault(
			aggregator.client,
			ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureCount,
			ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureActive,
			int64(len(keys)),
		)
		return err
	}
	clearChannelMonitorRedisFault(
		aggregator.client,
		ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureActive,
	)
	return nil
}

func (aggregator *ChannelMonitorRedisLogicalAggregator) recordEffectMarkerFailure(
	field string,
	count int,
	err error,
) {
	common.SysError("记录渠道监控 Redis 副作用幂等状态失败: " + err.Error())
	incrementChannelMonitorRedisObservation(aggregator.client, field, int64(count))
}
