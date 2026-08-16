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
	channelMonitorRedisConsumerBatchSize        = int64(100)
	channelMonitorRedisConsumerBlock            = time.Second
	channelMonitorRedisConsumerClaimMinIdle     = 30 * time.Second
	channelMonitorRedisAggregatorLeaseTTL       = 15 * time.Second
	channelMonitorRedisAggregatorLeaseHeartbeat = 5 * time.Second
	channelMonitorRedisConsumerOperationTimeout = 3 * time.Second
	channelMonitorRedisConsumerRetryDelay       = time.Second
	channelMonitorRedisDedupTTL                 = channelMonitorRedisReplayProtectionTTL
	channelMonitorRedisStreamSequenceBits       = 20
	channelMonitorRedisStreamSequenceLimit      = uint64(1<<channelMonitorRedisStreamSequenceBits) - 1
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
  redis.call('SET', KEYS[index + 2], '1', 'PX', ARGV[3])
end
local ack_args = {KEYS[2], ARGV[4]}
for index = 5, #ARGV do
  table.insert(ack_args, ARGV[index])
end
return redis.call('XACK', unpack(ack_args))
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
	BatchSize        int64
	Block            time.Duration
	ClaimMinIdle     time.Duration
	LeaseTTL         time.Duration
	LeaseHeartbeat   time.Duration
	OperationTimeout time.Duration
	RetryDelay       time.Duration
	DedupTTL         time.Duration
}

// ChannelMonitorRedisEventConsumer reliably consumes the versioned raw event
// Stream. It does not install projections or replace the legacy local queue.
type ChannelMonitorRedisEventConsumer struct {
	client       *redis.Client
	consumerName string
	handler      ChannelMonitorRedisEventHandler
	config       channelMonitorRedisConsumerConfig
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
	if !common.RedisEnabled || common.RDB == nil {
		return nil, ErrChannelMonitorRedisConsumerUnavailable
	}
	return newChannelMonitorRedisEventConsumer(
		common.RDB,
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
	return channelMonitorRedisConsumerConfig{
		BatchSize:        channelMonitorRedisConsumerBatchSize,
		Block:            channelMonitorRedisConsumerBlock,
		ClaimMinIdle:     channelMonitorRedisConsumerClaimMinIdle,
		LeaseTTL:         channelMonitorRedisAggregatorLeaseTTL,
		LeaseHeartbeat:   channelMonitorRedisAggregatorLeaseHeartbeat,
		OperationTimeout: channelMonitorRedisConsumerOperationTimeout,
		RetryDelay:       channelMonitorRedisConsumerRetryDelay,
		DedupTTL:         channelMonitorRedisDedupTTL,
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
	if config.RetryDelay <= 0 {
		config.RetryDelay = defaults.RetryDelay
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

// Run consumes until ctx is canceled or a Redis/handler error occurs. Handler
// errors deliberately stop the loop with the batch left pending for takeover.
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
			return err
		}
		if acquired {
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

	messages, err := consumer.claimPending(lease.ctx)
	if err != nil {
		return 0, true, err
	}
	if len(messages) == 0 {
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
		messages, err = consumer.readNew(lease.ctx)
		if err != nil {
			return 0, true, err
		}
	}
	if len(messages) == 0 {
		return 0, true, nil
	}
	if err := consumer.processMessages(lease, messages); err != nil {
		return 0, true, err
	}
	return len(messages), true, nil
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
	events := make([]model.ChannelMonitorEvent, 0, len(messages))
	messageIDs := make([]string, 0, len(messages))
	eventIDs := make([]string, 0, len(messages))
	dedupKeys := make([]string, 0, len(messages))
	for _, message := range messages {
		eventID, err := channelMonitorRedisMessageValue(message, ChannelMonitorRedisEventFieldEventID)
		if err != nil {
			return consumer.retryPendingMessages(lease.ctx, len(messages), err)
		}
		payload, err := channelMonitorRedisMessageValue(message, ChannelMonitorRedisEventFieldPayload)
		if err != nil {
			return consumer.retryPendingMessages(lease.ctx, len(messages), err)
		}
		event, err := model.UnmarshalChannelMonitorEvent([]byte(payload))
		if err != nil {
			return consumer.retryPendingMessages(lease.ctx, len(messages), fmt.Errorf("渠道监控 Redis Stream 事件 %s 无效: %w", message.ID, err))
		}
		if event.EventId != eventID {
			return consumer.retryPendingMessages(lease.ctx, len(messages), fmt.Errorf("渠道监控 Redis Stream 事件 %s 的 event_id 与 payload 不一致", message.ID))
		}
		event.EventSequence, err = channelMonitorRedisEventSequenceFromStreamID(message.ID)
		if err != nil {
			return consumer.retryPendingMessages(lease.ctx, len(messages), err)
		}
		events = append(events, event)
		messageIDs = append(messageIDs, message.ID)
		eventIDs = append(eventIDs, eventID)
		dedupKeys = append(dedupKeys, ChannelMonitorRedisProjectionDedupKey(eventID))
	}

	opCtx, cancel := context.WithTimeout(lease.ctx, consumer.config.OperationTimeout)
	dedupValues, err := consumer.client.MGet(opCtx, dedupKeys...).Result()
	cancel()
	if err != nil {
		return err
	}
	uniqueEvents := make([]model.ChannelMonitorEvent, 0, len(events))
	uniqueDedupKeys := make([]string, 0, len(events))
	seenEventIDs := make(map[string]struct{}, len(events))
	for index, event := range events {
		if dedupValues[index] != nil {
			continue
		}
		if _, duplicate := seenEventIDs[eventIDs[index]]; duplicate {
			continue
		}
		seenEventIDs[eventIDs[index]] = struct{}{}
		uniqueEvents = append(uniqueEvents, event)
		uniqueDedupKeys = append(uniqueDedupKeys, dedupKeys[index])
	}

	if len(uniqueEvents) > 0 {
		handlerCtx := context.WithValue(
			lease.ctx,
			channelMonitorRedisEffectOwnerContextKey{},
			lease.token,
		)
		if err := consumer.handler.HandleChannelMonitorEvents(handlerCtx, uniqueEvents); err != nil {
			return consumer.retryPendingMessages(lease.ctx, len(messages), err)
		}
	}
	if lease.lost.Load() {
		return consumer.retryPendingMessages(lease.ctx, len(messages), ErrChannelMonitorRedisAggregatorLeaseLost)
	}
	if err := consumer.finalizeMessages(lease, uniqueDedupKeys, messageIDs); err != nil {
		return consumer.retryPendingMessages(lease.ctx, len(messages), err)
	}
	recordChannelMonitorRedisProcessedAt(consumer.client, time.Now().Unix())
	if err := consumer.trimAcknowledged(lease.ctx); err != nil {
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
	keys := make([]string, 0, len(dedupKeys)+2)
	keys = append(keys, ChannelMonitorRedisAggregatorLeaseKey, ChannelMonitorRedisEventStream)
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
