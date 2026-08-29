package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

const (
	channelMonitorRedisConsumerBatchSize           = int64(100)
	channelMonitorRedisConsumerBlock               = time.Second
	channelMonitorRedisConsumerClaimMinIdle        = 30 * time.Second
	channelMonitorRedisAggregatorLeaseTTL          = 15 * time.Second
	channelMonitorRedisAggregatorLeaseHeartbeat    = 5 * time.Second
	channelMonitorRedisConsumerOperationTimeout    = 3 * time.Second
	channelMonitorRedisConsumerHandlerTimeout      = 3 * time.Second
	channelMonitorRedisConsumerRetryDelay          = time.Second
	channelMonitorRedisConsumerMaxDeliveryAttempts = 20
	channelMonitorRedisConsumerPendingRetryLimit   = 3
	channelMonitorRedisConsumerWorkerCount         = 1
	channelMonitorRedisConsumerMaxWorkerCount      = 32
	channelMonitorRedisDeadLetterMaxLength         = 10_000
	channelMonitorRedisDedupTTL                    = channelMonitorRedisReplayProtectionTTL
	channelMonitorRedisStreamSequenceBits          = 20
	channelMonitorRedisStreamSequenceLimit         = uint64(1<<channelMonitorRedisStreamSequenceBits) - 1
)

var (
	ErrChannelMonitorRedisAggregatorLeaseLost = errors.New("渠道监控 Redis 聚合器租约已丢失")
	ErrChannelMonitorRedisConsumerUnavailable = errors.New("渠道监控 Redis 消费器不可用")
)

const channelMonitorRedisLeaseRenewScript = `
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
  return 0
end
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return 1
`

const channelMonitorRedisLeaseReleaseScript = `
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
  return 0
end
return redis.call('DEL', KEYS[1])
`

const channelMonitorRedisFinalizeBatchScript = `
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
  return -1
end
local dedup_count = tonumber(ARGV[2])
for index = 1, dedup_count do
  redis.call('SET', KEYS[index + 3], '1', 'PX', ARGV[3])
end
local ack_args = {KEYS[2], ARGV[4]}
for index = 5, #ARGV do
  table.insert(ack_args, ARGV[index])
end
local acknowledged = redis.call('XACK', unpack(ack_args))
if #ARGV >= 5 then
  local delete_args = {KEYS[3]}
  for index = 5, #ARGV do
    table.insert(delete_args, ARGV[index])
  end
  redis.call('HDEL', unpack(delete_args))
end
return acknowledged
`

const channelMonitorRedisIncrementFailureScript = `
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
  return {-1}
end
local counts = {}
for index = 2, #ARGV do
  counts[index - 1] = redis.call('HINCRBY', KEYS[2], ARGV[index], 1)
end
return counts
`

const channelMonitorRedisQuarantineScript = `
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
  return -1
end
local item_count = tonumber(ARGV[4])
local acknowledged = 0
local offset = 6
for index = 1, item_count do
  local message_id = ARGV[offset]
  redis.call(
    'XADD', KEYS[3], 'MAXLEN', '~', ARGV[5], '*',
    'original_message_id', message_id,
    'event_id', ARGV[offset + 1],
    'payload', ARGV[offset + 2],
    'error', ARGV[offset + 3],
    'failure_count', ARGV[offset + 4],
    'quarantined_at', ARGV[3]
  )
  redis.call('HDEL', KEYS[4], message_id)
  acknowledged = acknowledged + redis.call('XACK', KEYS[2], ARGV[2], message_id)
  offset = offset + 5
end
return acknowledged
`

// ChannelMonitorRedisEventHandler applies one batch of previously validated,
// event-id-deduplicated channel monitor events. Side effects outside Redis
// must also use EventId as their idempotency key because they cannot join the
// consumer's atomic dedup-marker-and-XACK script.
type ChannelMonitorRedisEventHandler interface {
	HandleChannelMonitorEvents(context.Context, []model.ChannelMonitorEvent) error
}

// ChannelMonitorRedisEventHandlerFunc adapts a function to the event handler
// interface while preserving processing errors for pending-message retries.
type ChannelMonitorRedisEventHandlerFunc func(context.Context, []model.ChannelMonitorEvent) error

func (handle ChannelMonitorRedisEventHandlerFunc) HandleChannelMonitorEvents(
	ctx context.Context,
	events []model.ChannelMonitorEvent,
) error {
	return handle(ctx, events)
}

type channelMonitorRedisConsumerConfig struct {
	BatchSize           int64
	Block               time.Duration
	ClaimMinIdle        time.Duration
	LeaseTTL            time.Duration
	LeaseHeartbeat      time.Duration
	OperationTimeout    time.Duration
	HandlerTimeout      time.Duration
	RetryDelay          time.Duration
	DedupTTL            time.Duration
	MaxDeliveryAttempts int
	PendingRetryLimit   int
	WorkerCount         int
}

type channelMonitorRedisParsedMessage struct {
	message  redis.XMessage
	eventID  string
	payload  string
	event    model.ChannelMonitorEvent
	dedupKey string
}

type channelMonitorRedisQuarantineItem struct {
	messageID    string
	eventID      string
	payload      string
	reason       string
	failureCount int64
}

type channelMonitorRedisHandlerRetryError struct {
	err error
}

func (retryErr *channelMonitorRedisHandlerRetryError) Error() string { return retryErr.err.Error() }
func (retryErr *channelMonitorRedisHandlerRetryError) Unwrap() error { return retryErr.err }

// ChannelMonitorRedisEventConsumer reliably consumes the versioned raw event
// Stream. It does not install projections or replace the legacy local queue.
type ChannelMonitorRedisEventConsumer struct {
	client             *redis.Client
	consumerName       string
	handler            ChannelMonitorRedisEventHandler
	config             channelMonitorRedisConsumerConfig
	pendingRetryCycles atomic.Int64
}

type channelMonitorRedisGroupInfo struct {
	Name            string
	Pending         int64
	LastDeliveredID string
}

type channelMonitorRedisAggregatorLease struct {
	client      *redis.Client
	token       string
	config      channelMonitorRedisConsumerConfig
	ctx         context.Context
	cancel      context.CancelFunc
	lost        atomic.Bool
	waitGroup   sync.WaitGroup
	releaseOnce sync.Once
}

type channelMonitorRedisEffectOwnerContextKey struct{}

func channelMonitorRedisEffectOwnerFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	owner, ok := ctx.Value(channelMonitorRedisEffectOwnerContextKey{}).(string)
	return owner, ok && strings.TrimSpace(owner) != ""
}

// NewChannelMonitorRedisEventConsumer builds the REDIS-03 consumer with the
// mandatory shared Redis client and the stable current-instance identity.
func NewChannelMonitorRedisEventConsumer(handler ChannelMonitorRedisEventHandler) (*ChannelMonitorRedisEventConsumer, error) {
	client := common.RedisMonitorConsumerClient()
	if !common.RedisEnabled || client == nil {
		return nil, ErrChannelMonitorRedisConsumerUnavailable
	}
	return newChannelMonitorRedisEventConsumer(
		client,
		ChannelMonitorRedisConsumerName(channelMonitorRedisInstanceIdentity()),
		handler,
		defaultChannelMonitorRedisConsumerConfig(),
	)
}

func newChannelMonitorRedisEventConsumer(
	client *redis.Client,
	consumerName string,
	handler ChannelMonitorRedisEventHandler,
	config channelMonitorRedisConsumerConfig,
) (*ChannelMonitorRedisEventConsumer, error) {
	if client == nil {
		return nil, ErrChannelMonitorRedisConsumerUnavailable
	}
	if handler == nil {
		return nil, errors.New("渠道监控 Redis 事件处理器不能为空")
	}
	if !strings.HasPrefix(strings.TrimSpace(consumerName), ChannelMonitorRedisConsumerPrefix) {
		return nil, errors.New("渠道监控 Redis Stream 消费者名称不符合版本契约")
	}
	config = normalizeChannelMonitorRedisConsumerConfig(config)
	return &ChannelMonitorRedisEventConsumer{
		client:       client,
		consumerName: consumerName,
		handler:      handler,
		config:       config,
	}, nil
}

func defaultChannelMonitorRedisConsumerConfig() channelMonitorRedisConsumerConfig {
	handlerTimeoutMS := common.GetEnvOrDefault(
		"CHANNEL_MONITOR_REDIS_HANDLER_TIMEOUT_MS",
		int(channelMonitorRedisConsumerHandlerTimeout/time.Millisecond),
	)
	if handlerTimeoutMS <= 0 {
		handlerTimeoutMS = int(channelMonitorRedisConsumerHandlerTimeout / time.Millisecond)
	}
	return channelMonitorRedisConsumerConfig{
		BatchSize:           channelMonitorRedisConsumerBatchSize,
		Block:               channelMonitorRedisConsumerBlock,
		ClaimMinIdle:        channelMonitorRedisConsumerClaimMinIdle,
		LeaseTTL:            channelMonitorRedisAggregatorLeaseTTL,
		LeaseHeartbeat:      channelMonitorRedisAggregatorLeaseHeartbeat,
		OperationTimeout:    channelMonitorRedisConsumerOperationTimeout,
		HandlerTimeout:      time.Duration(handlerTimeoutMS) * time.Millisecond,
		RetryDelay:          channelMonitorRedisConsumerRetryDelay,
		DedupTTL:            channelMonitorRedisDedupTTL,
		MaxDeliveryAttempts: channelMonitorRedisConsumerMaxDeliveryAttempts,
		PendingRetryLimit: common.GetEnvOrDefault(
			"CHANNEL_MONITOR_REDIS_PENDING_RETRY_LIMIT",
			channelMonitorRedisConsumerPendingRetryLimit,
		),
		WorkerCount: common.GetEnvOrDefault(
			"CHANNEL_MONITOR_REDIS_CONSUMER_WORKERS",
			channelMonitorRedisConsumerWorkerCount,
		),
	}
}

func normalizeChannelMonitorRedisConsumerConfig(config channelMonitorRedisConsumerConfig) channelMonitorRedisConsumerConfig {
	defaults := defaultChannelMonitorRedisConsumerConfig()
	if config.BatchSize <= 0 {
		config.BatchSize = defaults.BatchSize
	}
	if config.Block < -1 {
		config.Block = defaults.Block
	}
	if config.ClaimMinIdle <= 0 {
		config.ClaimMinIdle = defaults.ClaimMinIdle
	}
	if config.LeaseTTL <= 0 {
		config.LeaseTTL = defaults.LeaseTTL
	}
	if config.LeaseHeartbeat <= 0 || config.LeaseHeartbeat >= config.LeaseTTL {
		config.LeaseHeartbeat = config.LeaseTTL / 3
	}
	if config.LeaseHeartbeat <= 0 {
		config.LeaseHeartbeat = time.Millisecond
	}
	if config.OperationTimeout <= 0 {
		config.OperationTimeout = defaults.OperationTimeout
	}
	if config.HandlerTimeout <= 0 {
		config.HandlerTimeout = defaults.HandlerTimeout
	}
	if config.LeaseTTL > 0 && config.HandlerTimeout >= config.LeaseTTL {
		config.HandlerTimeout = config.LeaseTTL / 2
		if config.HandlerTimeout <= 0 {
			config.HandlerTimeout = config.LeaseTTL
		}
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = defaults.RetryDelay
	}
	if config.MaxDeliveryAttempts <= 0 {
		config.MaxDeliveryAttempts = defaults.MaxDeliveryAttempts
	}
	if config.PendingRetryLimit <= 0 {
		config.PendingRetryLimit = defaults.PendingRetryLimit
	}
	if config.WorkerCount <= 0 {
		config.WorkerCount = defaults.WorkerCount
	}
	if config.WorkerCount > channelMonitorRedisConsumerMaxWorkerCount {
		config.WorkerCount = channelMonitorRedisConsumerMaxWorkerCount
	}
	minimumDedupTTL := config.ClaimMinIdle + config.LeaseTTL
	if config.DedupTTL < minimumDedupTTL {
		config.DedupTTL = defaults.DedupTTL
		if config.DedupTTL < minimumDedupTTL {
			config.DedupTTL = minimumDedupTTL
		}
	}
	return config
}

// Run consumes until ctx is canceled or an infrastructure error requires the
// runtime supervisor to rebuild the consumer. Handler failures remain pending
// and are retried by this consumer while its heartbeat stays alive.
func (consumer *ChannelMonitorRedisEventConsumer) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		_, acquired, err := consumer.consumeOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var handlerRetry *channelMonitorRedisHandlerRetryError
			if !errors.As(err, &handlerRetry) {
				return err
			}
		}
		if acquired && err == nil {
			continue
		}
		timer := time.NewTimer(consumer.config.RetryDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (consumer *ChannelMonitorRedisEventConsumer) consumeOnce(ctx context.Context) (int, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := refreshChannelMonitorRedisConsumerHeartbeat(ctx, consumer.client, consumer.consumerName); err != nil {
		return 0, false, err
	}
	lease, acquired, err := consumer.acquireLease(ctx)
	if err != nil || !acquired {
		return 0, acquired, err
	}
	defer lease.release()

	// Pending retries are normally preferred for at-least-once delivery, but a
	// poison event must not hold the global lease forever while fresh traffic
	// accumulates. After a bounded number of retry cycles, give new entries a
	// turn; the pending event remains in the PEL for XAUTOCLAIM/retry.
	forceNew := consumer.pendingRetryCycles.Load() >= int64(consumer.config.PendingRetryLimit)
	fromPending := false
	var messages []redis.XMessage
	if !forceNew {
		messages, err = consumer.claimPending(lease.ctx)
		if err != nil {
			return 0, true, err
		}
		if len(messages) == 0 {
			messages, err = consumer.readOwnedPending(lease.ctx)
			if err != nil {
				return 0, true, err
			}
		}
		fromPending = len(messages) > 0
	}
	if len(messages) == 0 && !fromPending {
		if !forceNew {
			hasPending, pendingErr := consumer.hasPending(lease.ctx)
			if pendingErr != nil {
				return 0, true, pendingErr
			}
			if hasPending {
				timer := time.NewTimer(consumer.config.RetryDelay)
				defer timer.Stop()
				select {
				case <-lease.ctx.Done():
					return 0, true, lease.ctx.Err()
				case <-timer.C:
					return 0, true, nil
				}
			}
		}
		messages, err = consumer.readNew(lease.ctx)
		if err != nil {
			return 0, true, err
		}
	}
	if len(messages) == 0 {
		// A force-new turn is only a fairness escape hatch. If no fresh entry was
		// available, reset the counter so the pending entry gets another chance
		// on the next cycle instead of being starved indefinitely.
		if forceNew {
			consumer.pendingRetryCycles.Store(0)
		}
		return 0, true, nil
	}
	if err := consumer.processMessages(lease, messages); err != nil {
		var handlerRetry *channelMonitorRedisHandlerRetryError
		if fromPending && errors.As(err, &handlerRetry) {
			consumer.pendingRetryCycles.Add(1)
		} else if !fromPending {
			consumer.pendingRetryCycles.Store(0)
		}
		return 0, true, err
	}
	if fromPending {
		consumer.pendingRetryCycles.Store(0)
	} else if forceNew {
		consumer.pendingRetryCycles.Store(0)
	}
	return len(messages), true, nil
}

func (consumer *ChannelMonitorRedisEventConsumer) readOwnedPending(ctx context.Context) ([]redis.XMessage, error) {
	opCtx, cancel := context.WithTimeout(ctx, consumer.config.OperationTimeout)
	defer cancel()
	streams, err := consumer.client.XReadGroup(opCtx, &redis.XReadGroupArgs{
		Group:    ChannelMonitorRedisConsumerGroup,
		Consumer: consumer.consumerName,
		Streams:  []string{ChannelMonitorRedisEventStream, "0"},
		Count:    consumer.config.BatchSize,
		Block:    -1,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var messages []redis.XMessage
	for _, stream := range streams {
		messages = append(messages, stream.Messages...)
	}
	return messages, nil
}

func (consumer *ChannelMonitorRedisEventConsumer) hasPending(ctx context.Context) (bool, error) {
	opCtx, cancel := context.WithTimeout(ctx, consumer.config.OperationTimeout)
	defer cancel()
	pending, err := consumer.client.XPending(
		opCtx,
		ChannelMonitorRedisEventStream,
		ChannelMonitorRedisConsumerGroup,
	).Result()
	if err != nil {
		return false, err
	}
	return pending.Count > 0, nil
}

func (consumer *ChannelMonitorRedisEventConsumer) acquireLease(
	ctx context.Context,
) (*channelMonitorRedisAggregatorLease, bool, error) {
	opCtx, cancel := context.WithTimeout(ctx, consumer.config.OperationTimeout)
	defer cancel()
	token := consumer.consumerName + ":" + uuid.NewString()
	acquired, err := consumer.client.SetNX(
		opCtx,
		ChannelMonitorRedisAggregatorLeaseKey,
		token,
		consumer.config.LeaseTTL,
	).Result()
	if err != nil || !acquired {
		return nil, acquired, err
	}

	leaseCtx, leaseCancel := context.WithCancel(ctx)
	lease := &channelMonitorRedisAggregatorLease{
		client: consumer.client,
		token:  token,
		config: consumer.config,
		ctx:    leaseCtx,
		cancel: leaseCancel,
	}
	lease.startHeartbeat()
	return lease, true, nil
}

func (lease *channelMonitorRedisAggregatorLease) startHeartbeat() {
	lease.waitGroup.Add(1)
	go func() {
		defer lease.waitGroup.Done()
		ticker := time.NewTicker(lease.config.LeaseHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-lease.ctx.Done():
				return
			case <-ticker.C:
				opCtx, cancel := context.WithTimeout(context.Background(), lease.config.OperationTimeout)
				renewed, err := lease.client.Eval(
					opCtx,
					channelMonitorRedisLeaseRenewScript,
					[]string{ChannelMonitorRedisAggregatorLeaseKey},
					lease.token,
					lease.config.LeaseTTL.Milliseconds(),
				).Int()
				cancel()
				if err != nil || renewed != 1 {
					lease.lost.Store(true)
					lease.cancel()
					return
				}
			}
		}
	}()
}

func (lease *channelMonitorRedisAggregatorLease) release() {
	lease.releaseOnce.Do(func() {
		lease.cancel()
		lease.waitGroup.Wait()
		opCtx, cancel := context.WithTimeout(context.Background(), lease.config.OperationTimeout)
		defer cancel()
		_ = lease.client.Eval(
			opCtx,
			channelMonitorRedisLeaseReleaseScript,
			[]string{ChannelMonitorRedisAggregatorLeaseKey},
			lease.token,
		).Err()
	})
}

func (consumer *ChannelMonitorRedisEventConsumer) claimPending(ctx context.Context) ([]redis.XMessage, error) {
	opCtx, cancel := context.WithTimeout(ctx, consumer.config.OperationTimeout)
	defer cancel()
	result, err := consumer.client.Do(
		opCtx,
		"XAUTOCLAIM",
		ChannelMonitorRedisEventStream,
		ChannelMonitorRedisConsumerGroup,
		consumer.consumerName,
		consumer.config.ClaimMinIdle.Milliseconds(),
		"0-0",
		"COUNT",
		consumer.config.BatchSize,
	).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	messages, parseErr := parseChannelMonitorRedisAutoClaimMessages(result)
	if parseErr != nil {
		return nil, parseErr
	}
	if len(messages) > 0 {
		incrementChannelMonitorRedisObservation(consumer.client, ChannelMonitorRedisObservabilityFieldTakeoverCount, int64(len(messages)))
	}
	return messages, nil
}

// go-redis/v8 expects the Redis 6.2 two-element XAUTOCLAIM reply. Redis 7
// adds a third deleted-ID element, so parse the raw reply for both versions.
func parseChannelMonitorRedisAutoClaimMessages(result interface{}) ([]redis.XMessage, error) {
	reply, ok := result.([]interface{})
	if !ok || (len(reply) != 2 && len(reply) != 3) {
		return nil, fmt.Errorf("渠道监控 Redis XAUTOCLAIM 响应无效")
	}
	rawMessages, ok := reply[1].([]interface{})
	if !ok {
		return nil, fmt.Errorf("渠道监控 Redis XAUTOCLAIM 消息列表无效")
	}
	messages := make([]redis.XMessage, 0, len(rawMessages))
	for _, rawMessage := range rawMessages {
		entry, entryOK := rawMessage.([]interface{})
		if !entryOK || len(entry) != 2 {
			return nil, fmt.Errorf("渠道监控 Redis XAUTOCLAIM 消息无效")
		}
		messageID, err := channelMonitorRedisReplyString(entry[0])
		if err != nil {
			return nil, err
		}
		rawFields, fieldsOK := entry[1].([]interface{})
		if !fieldsOK || len(rawFields)%2 != 0 {
			return nil, fmt.Errorf("渠道监控 Redis XAUTOCLAIM 消息字段无效")
		}
		values := make(map[string]interface{}, len(rawFields)/2)
		for index := 0; index < len(rawFields); index += 2 {
			field, fieldErr := channelMonitorRedisReplyString(rawFields[index])
			if fieldErr != nil {
				return nil, fieldErr
			}
			value, valueErr := channelMonitorRedisReplyString(rawFields[index+1])
			if valueErr != nil {
				return nil, valueErr
			}
			values[field] = value
		}
		messages = append(messages, redis.XMessage{ID: messageID, Values: values})
	}
	return messages, nil
}

func channelMonitorRedisReplyString(value interface{}) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	default:
		return "", fmt.Errorf("渠道监控 Redis 响应字符串类型无效")
	}
}

func channelMonitorRedisReplyInt64(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("渠道监控 Redis 响应整数类型无效")
	}
}

func (consumer *ChannelMonitorRedisEventConsumer) readNew(ctx context.Context) ([]redis.XMessage, error) {
	timeout := consumer.config.OperationTimeout
	if consumer.config.Block > 0 {
		timeout += consumer.config.Block
	}
	opCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	streams, err := consumer.client.XReadGroup(opCtx, &redis.XReadGroupArgs{
		Group:    ChannelMonitorRedisConsumerGroup,
		Consumer: consumer.consumerName,
		Streams:  []string{ChannelMonitorRedisEventStream, ">"},
		Count:    consumer.config.BatchSize,
		Block:    consumer.config.Block,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var messages []redis.XMessage
	for _, stream := range streams {
		messages = append(messages, stream.Messages...)
	}
	return messages, nil
}

func (consumer *ChannelMonitorRedisEventConsumer) processMessages(
	lease *channelMonitorRedisAggregatorLease,
	messages []redis.XMessage,
) error {
	parsedMessages := make([]channelMonitorRedisParsedMessage, 0, len(messages))
	invalidMessages := make([]channelMonitorRedisQuarantineItem, 0)
	for _, message := range messages {
		eventID, err := channelMonitorRedisMessageValue(message, ChannelMonitorRedisEventFieldEventID)
		if err != nil {
			invalidMessages = append(invalidMessages, channelMonitorRedisQuarantineItem{
				messageID: message.ID, reason: err.Error(), failureCount: 1,
			})
			continue
		}
		payload, err := channelMonitorRedisMessageValue(message, ChannelMonitorRedisEventFieldPayload)
		if err != nil {
			invalidMessages = append(invalidMessages, channelMonitorRedisQuarantineItem{
				messageID: message.ID, eventID: eventID, reason: err.Error(), failureCount: 1,
			})
			continue
		}
		event, err := model.UnmarshalChannelMonitorEvent([]byte(payload))
		if err != nil {
			invalidMessages = append(invalidMessages, channelMonitorRedisQuarantineItem{
				messageID: message.ID, eventID: eventID, payload: payload,
				reason:       fmt.Sprintf("渠道监控 Redis Stream 事件 %s 无效: %v", message.ID, err),
				failureCount: 1,
			})
			continue
		}
		if event.EventId != eventID {
			invalidMessages = append(invalidMessages, channelMonitorRedisQuarantineItem{
				messageID: message.ID, eventID: eventID, payload: payload,
				reason:       fmt.Sprintf("渠道监控 Redis Stream 事件 %s 的 event_id 与 payload 不一致", message.ID),
				failureCount: 1,
			})
			continue
		}
		event.EventSequence, err = channelMonitorRedisEventSequenceFromStreamID(message.ID)
		if err != nil {
			invalidMessages = append(invalidMessages, channelMonitorRedisQuarantineItem{
				messageID: message.ID, eventID: eventID, payload: payload,
				reason: err.Error(), failureCount: 1,
			})
			continue
		}
		parsedMessages = append(parsedMessages, channelMonitorRedisParsedMessage{
			message: message, eventID: eventID, payload: payload, event: event,
			dedupKey: ChannelMonitorRedisProjectionDedupKey(eventID),
		})
	}
	if len(invalidMessages) > 0 {
		if err := consumer.quarantineMessages(lease, invalidMessages); err != nil {
			return consumer.retryPendingMessages(lease.ctx, len(invalidMessages), err)
		}
	}
	if len(parsedMessages) == 0 {
		return consumer.completeMessageProcessing(lease.ctx)
	}

	opCtx, cancel := context.WithTimeout(lease.ctx, consumer.config.OperationTimeout)
	dedupKeys := make([]string, 0, len(parsedMessages))
	for _, parsed := range parsedMessages {
		dedupKeys = append(dedupKeys, parsed.dedupKey)
	}
	dedupValues, err := consumer.client.MGet(opCtx, dedupKeys...).Result()
	cancel()
	if err != nil {
		return err
	}
	uniqueMessages := make([]channelMonitorRedisParsedMessage, 0, len(parsedMessages))
	alreadyProcessedMessageIDs := make([]string, 0)
	messageIDsByEventID := make(map[string][]string, len(parsedMessages))
	seenEventIDs := make(map[string]struct{}, len(parsedMessages))
	for index, parsed := range parsedMessages {
		if dedupValues[index] != nil {
			alreadyProcessedMessageIDs = append(alreadyProcessedMessageIDs, parsed.message.ID)
			continue
		}
		messageIDsByEventID[parsed.eventID] = append(messageIDsByEventID[parsed.eventID], parsed.message.ID)
		if _, duplicate := seenEventIDs[parsed.eventID]; duplicate {
			continue
		}
		seenEventIDs[parsed.eventID] = struct{}{}
		uniqueMessages = append(uniqueMessages, parsed)
	}
	if len(alreadyProcessedMessageIDs) > 0 {
		if err := consumer.finalizeMessages(lease, nil, alreadyProcessedMessageIDs); err != nil {
			return consumer.retryPendingMessages(lease.ctx, len(alreadyProcessedMessageIDs), err)
		}
	}
	if len(uniqueMessages) == 0 {
		return consumer.completeMessageProcessing(lease.ctx)
	}

	handlerCtx := context.WithValue(
		lease.ctx,
		channelMonitorRedisEffectOwnerContextKey{},
		lease.token,
	)
	handlerResults := consumer.handleEventPartitions(handlerCtx, uniqueMessages)
	failedMessages := make([]channelMonitorRedisParsedMessage, 0)
	successfulDedupKeys := make([]string, 0, len(uniqueMessages))
	successfulMessageIDs := make([]string, 0, len(parsedMessages))
	var handlerErr error
	for _, result := range handlerResults {
		if result.err != nil {
			if handlerErr == nil {
				handlerErr = result.err
			}
			failedMessages = append(failedMessages, result.messages...)
			continue
		}
		for _, parsed := range result.messages {
			successfulDedupKeys = append(successfulDedupKeys, parsed.dedupKey)
			successfulMessageIDs = append(successfulMessageIDs, messageIDsByEventID[parsed.eventID]...)
		}
	}
	unresolved := false
	if len(failedMessages) > 0 {
		pendingMessageIDs := make([]string, 0, len(failedMessages))
		for _, parsed := range failedMessages {
			pendingMessageIDs = append(pendingMessageIDs, messageIDsByEventID[parsed.eventID]...)
		}
		failureCounts, countErr := consumer.incrementFailureCounts(lease, pendingMessageIDs)
		if countErr != nil {
			return consumer.retryPendingMessages(lease.ctx, len(pendingMessageIDs), countErr)
		}
		incrementChannelMonitorRedisObservation(
			consumer.client,
			ChannelMonitorRedisObservabilityFieldRetryCount,
			int64(len(pendingMessageIDs)),
		)
		readyForIsolation := false
		for _, count := range failureCounts {
			if count >= int64(consumer.config.MaxDeliveryAttempts) {
				readyForIsolation = true
				break
			}
		}
		quarantined := make([]channelMonitorRedisQuarantineItem, 0)
		unresolved = !readyForIsolation
		if readyForIsolation {
			for _, parsed := range failedMessages {
				messageIDs := messageIDsByEventID[parsed.eventID]
				attempts := int64(0)
				for _, messageID := range messageIDs {
					attempts = max(attempts, failureCounts[messageID])
				}
				if attempts < int64(consumer.config.MaxDeliveryAttempts) {
					unresolved = true
					continue
				}
				if err := consumer.handleEventsWithDeadline(
					handlerCtx, []model.ChannelMonitorEvent{parsed.event},
				); err != nil {
					for _, messageID := range messageIDs {
						quarantined = append(quarantined, channelMonitorRedisQuarantineItem{
							messageID: messageID, eventID: parsed.eventID, payload: parsed.payload,
							reason: err.Error(), failureCount: attempts,
						})
					}
					continue
				}
				successfulDedupKeys = append(successfulDedupKeys, parsed.dedupKey)
				successfulMessageIDs = append(successfulMessageIDs, messageIDs...)
			}
			if len(quarantined) > 0 {
				if err := consumer.quarantineMessages(lease, quarantined); err != nil {
					return consumer.retryPendingMessages(lease.ctx, len(quarantined), err)
				}
			}
		}
	}
	if lease.lost.Load() {
		return consumer.retryPendingMessages(lease.ctx, len(parsedMessages), ErrChannelMonitorRedisAggregatorLeaseLost)
	}
	if len(successfulMessageIDs) > 0 {
		if err := consumer.finalizeMessages(lease, successfulDedupKeys, successfulMessageIDs); err != nil {
			return consumer.retryPendingMessages(lease.ctx, len(successfulMessageIDs), err)
		}
	}
	if unresolved {
		return &channelMonitorRedisHandlerRetryError{err: handlerErr}
	}
	return consumer.completeMessageProcessing(lease.ctx)
}

type channelMonitorRedisHandlerPartitionResult struct {
	messages []channelMonitorRedisParsedMessage
	err      error
}

// handleEventPartitions runs at most WorkerCount handler calls concurrently.
// Events sharing a channel are kept in one partition and retain stream order;
// channels may make progress independently when a deployment opts into more
// than one worker. The default remains one worker for strict legacy ordering.
func (consumer *ChannelMonitorRedisEventConsumer) handleEventPartitions(
	ctx context.Context,
	messages []channelMonitorRedisParsedMessage,
) []channelMonitorRedisHandlerPartitionResult {
	if len(messages) == 0 {
		return nil
	}
	workerCount := consumer.config.WorkerCount
	if workerCount <= 1 {
		workerCount = 1
	}
	if workerCount > len(messages) {
		workerCount = len(messages)
	}
	partitions := make([][]channelMonitorRedisParsedMessage, workerCount)
	partitionByKey := make(map[string]int, workerCount)
	nextPartition := 0
	for _, message := range messages {
		key := channelMonitorRedisEventOrderKey(message.event)
		partition, ok := partitionByKey[key]
		if !ok {
			partition = nextPartition % workerCount
			nextPartition++
			partitionByKey[key] = partition
		}
		partitions[partition] = append(partitions[partition], message)
	}

	results := make([]channelMonitorRedisHandlerPartitionResult, workerCount)
	var waitGroup sync.WaitGroup
	for index, partition := range partitions {
		if len(partition) == 0 {
			continue
		}
		waitGroup.Add(1)
		go func(index int, partition []channelMonitorRedisParsedMessage) {
			defer waitGroup.Done()
			events := make([]model.ChannelMonitorEvent, 0, len(partition))
			for _, parsed := range partition {
				events = append(events, parsed.event)
			}
			results[index] = channelMonitorRedisHandlerPartitionResult{
				messages: partition,
				err:      consumer.handleEventsWithDeadline(ctx, events),
			}
		}(index, partition)
	}
	waitGroup.Wait()
	return results
}

func channelMonitorRedisEventOrderKey(event model.ChannelMonitorEvent) string {
	if event.ChannelId <= 0 {
		return "event:" + event.EventId
	}
	return "channel:" + strconv.FormatInt(int64(event.ChannelId), 10)
}

// handleEventsWithDeadline gives every handler invocation a fresh deadline.
// In particular, an expired batch attempt must not cause the per-message
// poison-isolation pass to inherit an already-canceled context.
func (consumer *ChannelMonitorRedisEventConsumer) handleEventsWithDeadline(
	ctx context.Context,
	events []model.ChannelMonitorEvent,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	handlerCtx, cancel := context.WithTimeout(ctx, consumer.config.HandlerTimeout)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- consumer.handler.HandleChannelMonitorEvents(handlerCtx, events)
	}()
	select {
	case err := <-result:
		return err
	case <-handlerCtx.Done():
		return handlerCtx.Err()
	}
}

func (consumer *ChannelMonitorRedisEventConsumer) completeMessageProcessing(ctx context.Context) error {
	recordChannelMonitorRedisProcessedAt(consumer.client, time.Now().Unix())
	if err := consumer.trimAcknowledged(ctx); err != nil {
		recordChannelMonitorRedisFault(
			consumer.client,
			ChannelMonitorRedisObservabilityFieldStreamTrimFailureCount,
			ChannelMonitorRedisObservabilityFieldStreamTrimFailureActive,
			1,
		)
		common.SysError("渠道监控 Redis Stream 裁剪已确认事件失败: " + err.Error())
	} else {
		clearChannelMonitorRedisFault(
			consumer.client,
			ChannelMonitorRedisObservabilityFieldStreamTrimFailureActive,
		)
	}
	return nil
}

func (consumer *ChannelMonitorRedisEventConsumer) incrementFailureCounts(
	lease *channelMonitorRedisAggregatorLease,
	messageIDs []string,
) (map[string]int64, error) {
	keys := []string{ChannelMonitorRedisAggregatorLeaseKey, ChannelMonitorRedisConsumerFailureCountKey}
	args := make([]interface{}, 0, len(messageIDs)+1)
	args = append(args, lease.token)
	for _, messageID := range messageIDs {
		args = append(args, messageID)
	}
	opCtx, cancel := context.WithTimeout(lease.ctx, consumer.config.OperationTimeout)
	defer cancel()
	result, err := consumer.client.Eval(
		opCtx, channelMonitorRedisIncrementFailureScript, keys, args...,
	).Result()
	if err != nil {
		return nil, err
	}
	rawCounts, ok := result.([]interface{})
	if !ok || len(rawCounts) != len(messageIDs) {
		return nil, errors.New("渠道监控 Redis 失败计数响应无效")
	}
	counts := make(map[string]int64, len(messageIDs))
	for index, rawCount := range rawCounts {
		count, err := channelMonitorRedisReplyInt64(rawCount)
		if err != nil {
			return nil, err
		}
		if count < 0 {
			return nil, ErrChannelMonitorRedisAggregatorLeaseLost
		}
		counts[messageIDs[index]] = count
	}
	return counts, nil
}

func (consumer *ChannelMonitorRedisEventConsumer) quarantineMessages(
	lease *channelMonitorRedisAggregatorLease,
	items []channelMonitorRedisQuarantineItem,
) error {
	if len(items) == 0 {
		return nil
	}
	quarantinedAt := time.Now().Unix()
	args := make([]interface{}, 0, len(items)*5+5)
	args = append(
		args,
		lease.token,
		ChannelMonitorRedisConsumerGroup,
		quarantinedAt,
		len(items),
		channelMonitorRedisDeadLetterMaxLength,
	)
	for _, item := range items {
		args = append(args, item.messageID, item.eventID, item.payload, item.reason, item.failureCount)
	}
	opCtx, cancel := context.WithTimeout(lease.ctx, consumer.config.OperationTimeout)
	defer cancel()
	acknowledged, err := consumer.client.Eval(
		opCtx,
		channelMonitorRedisQuarantineScript,
		[]string{
			ChannelMonitorRedisAggregatorLeaseKey,
			ChannelMonitorRedisEventStream,
			ChannelMonitorRedisDeadLetterStream,
			ChannelMonitorRedisConsumerFailureCountKey,
		},
		args...,
	).Int64()
	if err != nil {
		return err
	}
	if acknowledged == -1 {
		return ErrChannelMonitorRedisAggregatorLeaseLost
	}
	if acknowledged != int64(len(items)) {
		return fmt.Errorf("渠道监控 Redis 隔离消息确认数量不一致: got=%d want=%d", acknowledged, len(items))
	}
	incrementChannelMonitorRedisObservation(
		consumer.client, ChannelMonitorRedisObservabilityFieldQuarantineCount, int64(len(items)),
	)
	ctx, observationCancel := context.WithTimeout(context.Background(), consumer.config.OperationTimeout)
	defer observationCancel()
	_ = consumer.client.HSet(
		ctx,
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldLastQuarantinedAt,
		quarantinedAt,
	).Err()
	for _, item := range items {
		common.SysError(fmt.Sprintf(
			"渠道监控 Redis 消息已隔离: message_id=%s event_id=%s attempts=%d error=%s",
			item.messageID, item.eventID, item.failureCount, item.reason,
		))
	}
	return nil
}

func (consumer *ChannelMonitorRedisEventConsumer) retryPendingMessages(
	ctx context.Context,
	messageCount int,
	err error,
) error {
	incrementChannelMonitorRedisObservation(consumer.client, ChannelMonitorRedisObservabilityFieldRetryCount, int64(messageCount))
	return err
}

func channelMonitorRedisMessageValue(message redis.XMessage, field string) (string, error) {
	value, exists := message.Values[field]
	if !exists {
		return "", fmt.Errorf("渠道监控 Redis Stream 事件 %s 缺少字段 %s", message.ID, field)
	}
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return "", fmt.Errorf("渠道监控 Redis Stream 事件 %s 的字段 %s 为空", message.ID, field)
		}
		return typed, nil
	case []byte:
		if len(typed) == 0 {
			return "", fmt.Errorf("渠道监控 Redis Stream 事件 %s 的字段 %s 为空", message.ID, field)
		}
		return string(typed), nil
	default:
		return "", fmt.Errorf("渠道监控 Redis Stream 事件 %s 的字段 %s 类型无效", message.ID, field)
	}
}

func (consumer *ChannelMonitorRedisEventConsumer) finalizeMessages(
	lease *channelMonitorRedisAggregatorLease,
	dedupKeys []string,
	messageIDs []string,
) error {
	keys := make([]string, 0, len(dedupKeys)+3)
	keys = append(
		keys,
		ChannelMonitorRedisAggregatorLeaseKey,
		ChannelMonitorRedisEventStream,
		ChannelMonitorRedisConsumerFailureCountKey,
	)
	keys = append(keys, dedupKeys...)
	args := make([]interface{}, 0, len(messageIDs)+4)
	args = append(
		args,
		lease.token,
		len(dedupKeys),
		consumer.config.DedupTTL.Milliseconds(),
		ChannelMonitorRedisConsumerGroup,
	)
	for _, messageID := range messageIDs {
		args = append(args, messageID)
	}
	opCtx, cancel := context.WithTimeout(lease.ctx, consumer.config.OperationTimeout)
	defer cancel()
	acknowledged, err := consumer.client.Eval(
		opCtx,
		channelMonitorRedisFinalizeBatchScript,
		keys,
		args...,
	).Int64()
	if err != nil {
		return err
	}
	if acknowledged == -1 {
		return ErrChannelMonitorRedisAggregatorLeaseLost
	}
	if acknowledged != int64(len(messageIDs)) {
		return fmt.Errorf("渠道监控 Redis Stream 确认数量不一致: got=%d want=%d", acknowledged, len(messageIDs))
	}
	return nil
}

func (consumer *ChannelMonitorRedisEventConsumer) trimAcknowledged(ctx context.Context) error {
	opCtx, cancel := context.WithTimeout(ctx, consumer.config.OperationTimeout)
	defer cancel()
	groups, err := consumer.loadGroups(opCtx)
	if err != nil {
		return err
	}
	minimumID := ""
	for _, group := range groups {
		watermark := group.LastDeliveredID
		if group.Pending > 0 {
			pending, pendingErr := consumer.client.XPending(opCtx, ChannelMonitorRedisEventStream, group.Name).Result()
			if pendingErr != nil {
				return pendingErr
			}
			watermark = pending.Lower
		}
		if watermark == "" || watermark == "0-0" {
			return nil
		}
		if minimumID == "" {
			minimumID = watermark
			continue
		}
		less, compareErr := channelMonitorRedisStreamIDLess(watermark, minimumID)
		if compareErr != nil {
			return compareErr
		}
		if less {
			minimumID = watermark
		}
	}
	if minimumID == "" {
		return nil
	}
	return consumer.client.XTrimMinID(opCtx, ChannelMonitorRedisEventStream, minimumID).Err()
}

// go-redis/v8 only understands the four fields returned by Redis 6.2. Redis
// 7 adds lag and entries-read, so parse the key/value reply and ignore fields
// that are not needed for the safe trim watermark.
func (consumer *ChannelMonitorRedisEventConsumer) loadGroups(ctx context.Context) ([]channelMonitorRedisGroupInfo, error) {
	return loadChannelMonitorRedisGroups(ctx, consumer.client)
}

func channelMonitorRedisStreamIDLess(left string, right string) (bool, error) {
	leftMilliseconds, leftSequence, err := parseChannelMonitorRedisStreamID(left)
	if err != nil {
		return false, err
	}
	rightMilliseconds, rightSequence, err := parseChannelMonitorRedisStreamID(right)
	if err != nil {
		return false, err
	}
	if leftMilliseconds != rightMilliseconds {
		return leftMilliseconds < rightMilliseconds, nil
	}
	return leftSequence < rightSequence, nil
}

func parseChannelMonitorRedisStreamID(value string) (uint64, uint64, error) {
	millisecondsText, sequenceText, found := strings.Cut(value, "-")
	if !found || millisecondsText == "" || sequenceText == "" {
		return 0, 0, fmt.Errorf("渠道监控 Redis Stream ID 无效: %s", value)
	}
	milliseconds, err := strconv.ParseUint(millisecondsText, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("渠道监控 Redis Stream ID 无效: %s", value)
	}
	sequence, err := strconv.ParseUint(sequenceText, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("渠道监控 Redis Stream ID 无效: %s", value)
	}
	return milliseconds, sequence, nil
}

func channelMonitorRedisEventSequenceFromStreamID(value string) (uint64, error) {
	milliseconds, sequence, err := parseChannelMonitorRedisStreamID(value)
	if err != nil {
		return 0, err
	}
	if milliseconds > (uint64(math.MaxInt64)>>channelMonitorRedisStreamSequenceBits) ||
		sequence > channelMonitorRedisStreamSequenceLimit {
		return 0, fmt.Errorf("渠道监控 Redis Stream ID 超出事件顺序编码范围: %s", value)
	}
	return milliseconds<<channelMonitorRedisStreamSequenceBits | sequence, nil
}
