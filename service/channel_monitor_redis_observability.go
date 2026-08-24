package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

const channelMonitorRedisObservabilityTimeout = time.Second

const (
	ChannelMonitorRedisStatusAvailable   = "available"
	ChannelMonitorRedisStatusUnavailable = "unavailable"

	ChannelMonitorRedisDegradedReasonRedisUnavailable     = "redis_unavailable"
	ChannelMonitorRedisDegradedReasonConsumerStopped      = "consumer_stopped"
	ChannelMonitorRedisDegradedReasonConsumerGroupMissing = "consumer_group_missing"
	ChannelMonitorRedisDegradedReasonEventBacklog         = "event_backlog"
	ChannelMonitorRedisDegradedReasonPublisherUnavailable = "publisher_unavailable"
	ChannelMonitorRedisDegradedReasonMarkerReleaseFailure = "marker_release_failure"
	ChannelMonitorRedisDegradedReasonStreamTrimFailure    = "stream_trim_failure"
	ChannelMonitorRedisDegradedReasonWriterQueueFull      = "writer_queue_full"
	ChannelMonitorRedisDegradedReasonCostStreamBacklog    = "cost_stream_backlog"
	ChannelMonitorRedisDegradedReasonCostOutboxBacklog    = "cost_outbox_backlog"
	ChannelMonitorRedisDegradedReasonCostPublishFailure   = "cost_publish_failure"
	ChannelMonitorRedisDegradedReasonCostDeadLetter       = "cost_dead_letter"
)

// ChannelMonitorRedisRealtimeStatus is the shared status contract returned by
// channel-monitor realtime APIs. PendingCount is the Redis consumer-group
// pending count; queue_depth remains an API alias for existing callers.
type ChannelMonitorRedisRealtimeStatus struct {
	RedisStatus                string                                                 `json:"redis_status"`
	RedisAvailable             bool                                                   `json:"redis_available"`
	RedisConsumerRunning       bool                                                   `json:"redis_consumer_running"`
	PendingCount               int64                                                  `json:"pending_count"`
	WriterQueueDepth           int                                                    `json:"writer_queue_depth"`
	WriterQueueCapacity        int                                                    `json:"writer_queue_capacity"`
	WriterQueuedEvents         int64                                                  `json:"writer_queued_events"`
	WriterDroppedEvents        int64                                                  `json:"writer_dropped_events"`
	WriterRetryEvents          int64                                                  `json:"writer_retry_events"`
	WriterOldestQueuedAt       int64                                                  `json:"writer_oldest_queued_at"`
	WriterQueueAgeSeconds      int64                                                  `json:"writer_queue_age_seconds"`
	OldestPendingAt            int64                                                  `json:"oldest_pending_at"`
	ConsumerLagSeconds         int64                                                  `json:"consumer_lag_seconds"`
	LastPublishedAt            int64                                                  `json:"last_published_at"`
	LastProcessedAt            int64                                                  `json:"last_processed_at"`
	RetryCount                 int64                                                  `json:"retry_count"`
	TakeoverCount              int64                                                  `json:"takeover_count"`
	QuarantineCount            int64                                                  `json:"quarantine_count"`
	LastQuarantinedAt          int64                                                  `json:"last_quarantined_at"`
	RuntimeMarkerFailureCount  int64                                                  `json:"runtime_marker_failure_count"`
	ScheduleMarkerFailureCount int64                                                  `json:"schedule_marker_failure_count"`
	MarkerReleaseFailureCount  int64                                                  `json:"marker_release_failure_count"`
	MarkerReleaseFailureActive bool                                                   `json:"marker_release_failure_active"`
	StreamTrimFailureCount     int64                                                  `json:"stream_trim_failure_count"`
	StreamTrimFailureActive    bool                                                   `json:"stream_trim_failure_active"`
	DegradedReasons            []string                                               `json:"degraded_reasons"`
	RealtimeDegraded           bool                                                   `json:"realtime_degraded"`
	RedisPoolStats             map[common.RedisClientRole]common.RedisClientPoolStats `json:"redis_pool_stats"`
	CostStreamPendingCount     int64                                                  `json:"cost_stream_pending_count"`
	CostStreamUnreadCount      int64                                                  `json:"cost_stream_unread_count"`
	CostOutboxPendingCount     int64                                                  `json:"cost_outbox_pending_count"`
	CostOutboxOldestPendingAt  int64                                                  `json:"cost_outbox_oldest_pending_at"`
	CostOutboxRetryCount       int64                                                  `json:"cost_outbox_retry_count"`
	CostLedgerFailedCount      int64                                                  `json:"cost_ledger_failed_count"`
	CostPublishFailedCount     int64                                                  `json:"cost_publish_failed_count"`
	CostDeadLetterCount        int64                                                  `json:"cost_dead_letter_count"`
}

func getChannelMonitorRedisRealtimeStatus(
	ctx context.Context,
	client *redis.Client,
	now time.Time,
) ChannelMonitorRedisRealtimeStatus {
	status := channelMonitorRedisUnavailableStatus()
	if client == nil {
		return status
	}
	if err := client.Ping(ctx).Err(); err != nil {
		return status
	}

	status.RedisStatus = ChannelMonitorRedisStatusAvailable
	status.RedisAvailable = true
	status.DegradedReasons = make([]string, 0)
	observability, err := client.HGetAll(ctx, ChannelMonitorRedisObservabilityKey).Result()
	if err != nil {
		return channelMonitorRedisUnavailableStatus()
	}
	status.LastProcessedAt = channelMonitorRedisObservationInt64(
		observability[ChannelMonitorRedisObservabilityFieldLastProcessedAt],
	)
	status.RetryCount = channelMonitorRedisObservationInt64(
		observability[ChannelMonitorRedisObservabilityFieldRetryCount],
	)
	status.TakeoverCount = channelMonitorRedisObservationInt64(
		observability[ChannelMonitorRedisObservabilityFieldTakeoverCount],
	)
	status.QuarantineCount = channelMonitorRedisObservationInt64(
		observability[ChannelMonitorRedisObservabilityFieldQuarantineCount],
	)
	status.LastQuarantinedAt = channelMonitorRedisObservationInt64(
		observability[ChannelMonitorRedisObservabilityFieldLastQuarantinedAt],
	)
	status.RuntimeMarkerFailureCount = channelMonitorRedisObservationInt64(
		observability[ChannelMonitorRedisObservabilityFieldRuntimeMarkerFailureCount],
	)
	status.ScheduleMarkerFailureCount = channelMonitorRedisObservationInt64(
		observability[ChannelMonitorRedisObservabilityFieldScheduleMarkerFailureCount],
	)
	status.MarkerReleaseFailureCount = channelMonitorRedisObservationInt64(
		observability[ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureCount],
	)
	status.MarkerReleaseFailureActive = channelMonitorRedisObservationInt64(
		observability[ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureActive],
	) > 0
	status.StreamTrimFailureCount = channelMonitorRedisObservationInt64(
		observability[ChannelMonitorRedisObservabilityFieldStreamTrimFailureCount],
	)
	status.StreamTrimFailureActive = channelMonitorRedisObservationInt64(
		observability[ChannelMonitorRedisObservabilityFieldStreamTrimFailureActive],
	) > 0
	heartbeatExists, err := client.Exists(ctx, ChannelMonitorRedisConsumerHeartbeatKey).Result()
	if err != nil {
		return channelMonitorRedisUnavailableStatus()
	}
	status.RedisConsumerRunning = heartbeatExists > 0

	groups, err := loadChannelMonitorRedisGroups(ctx, client)
	if err != nil {
		return channelMonitorRedisUnavailableStatus()
	}
	var group *channelMonitorRedisGroupInfo
	for index := range groups {
		if groups[index].Name == ChannelMonitorRedisConsumerGroup {
			group = &groups[index]
			break
		}
	}
	if group == nil {
		if !status.RedisConsumerRunning {
			status.DegradedReasons = append(status.DegradedReasons, ChannelMonitorRedisDegradedReasonConsumerStopped)
		}
		status.DegradedReasons = append(status.DegradedReasons, ChannelMonitorRedisDegradedReasonConsumerGroupMissing)
		status.RealtimeDegraded = true
		applyChannelMonitorEventWriterStats(&status)
		return status
	}

	lastGeneratedID, err := channelMonitorRedisLastGeneratedID(ctx, client)
	if err != nil {
		return channelMonitorRedisUnavailableStatus()
	}
	status.LastPublishedAt = channelMonitorRedisStreamIDTimestamp(lastGeneratedID)
	publisherStats := GetChannelMonitorEventPublishStats()
	if publisherStats.LastPublishedAt > status.LastPublishedAt {
		status.LastPublishedAt = publisherStats.LastPublishedAt
	}
	publisherUnavailable := publisherStats.LastFailureAt > 0 &&
		!publisherStats.RealtimeAvailable &&
		publisherStats.LastFailureAt >= publisherStats.LastPublishedAt
	if pending, pendingErr := client.XPending(ctx, ChannelMonitorRedisEventStream, group.Name).Result(); pendingErr == nil {
		status.PendingCount = pending.Count
	} else {
		return channelMonitorRedisUnavailableStatus()
	}

	oldestID, err := channelMonitorRedisOldestUnprocessedID(ctx, client, *group, lastGeneratedID)
	if err != nil {
		return channelMonitorRedisUnavailableStatus()
	}
	status.OldestPendingAt = channelMonitorRedisStreamIDTimestamp(oldestID)
	if status.OldestPendingAt > 0 {
		status.ConsumerLagSeconds = max(0, now.Unix()-status.OldestPendingAt)
	}
	if !status.RedisConsumerRunning {
		status.DegradedReasons = append(status.DegradedReasons, ChannelMonitorRedisDegradedReasonConsumerStopped)
	}
	if status.PendingCount > 0 || status.OldestPendingAt > 0 {
		status.DegradedReasons = append(status.DegradedReasons, ChannelMonitorRedisDegradedReasonEventBacklog)
	}
	if publisherUnavailable {
		status.DegradedReasons = append(status.DegradedReasons, ChannelMonitorRedisDegradedReasonPublisherUnavailable)
	}
	if status.MarkerReleaseFailureActive {
		status.DegradedReasons = append(status.DegradedReasons, ChannelMonitorRedisDegradedReasonMarkerReleaseFailure)
	}
	if status.StreamTrimFailureActive {
		status.DegradedReasons = append(status.DegradedReasons, ChannelMonitorRedisDegradedReasonStreamTrimFailure)
	}
	status.RealtimeDegraded = len(status.DegradedReasons) > 0
	applyChannelMonitorEventWriterStats(&status)
	return status
}

func channelMonitorRedisUnavailableStatus() ChannelMonitorRedisRealtimeStatus {
	status := ChannelMonitorRedisRealtimeStatus{
		RedisStatus:      ChannelMonitorRedisStatusUnavailable,
		DegradedReasons:  []string{ChannelMonitorRedisDegradedReasonRedisUnavailable},
		RealtimeDegraded: true,
	}
	status.RedisPoolStats = common.GetRedisClientPoolStats()
	applyChannelMonitorEventWriterStats(&status)
	applyChannelDailyCostReliableStatus(&status, nil, context.Background())
	return status
}

func applyChannelMonitorEventWriterStats(status *ChannelMonitorRedisRealtimeStatus) {
	if status == nil {
		return
	}
	stats := GetChannelMonitorEventWriterStats()
	status.WriterQueueDepth = stats.QueueDepth
	status.WriterQueueCapacity = stats.QueueCapacity
	status.WriterQueuedEvents = stats.QueuedEvents
	status.WriterDroppedEvents = stats.DroppedEvents
	status.WriterRetryEvents = stats.RetryEvents
	status.WriterOldestQueuedAt = stats.OldestQueuedAt
	status.WriterQueueAgeSeconds = stats.QueueAgeSeconds
	if stats.DroppedEvents > 0 {
		status.DegradedReasons = appendUniqueChannelMonitorRedisDegradedReason(
			status.DegradedReasons,
			ChannelMonitorRedisDegradedReasonWriterQueueFull,
		)
		status.RealtimeDegraded = true
	}
}

func applyChannelDailyCostReliableStatus(status *ChannelMonitorRedisRealtimeStatus, client *redis.Client, ctx context.Context) {
	if status == nil {
		return
	}
	stats := GetChannelDailyCostReliableStats()
	status.CostOutboxPendingCount = stats.OutboxPending
	status.CostOutboxOldestPendingAt = stats.OutboxOldestAt
	status.CostOutboxRetryCount = stats.OutboxRetryCount
	status.CostLedgerFailedCount = stats.LedgerFailed
	status.CostPublishFailedCount = stats.PublishFailed
	status.CostDeadLetterCount = stats.DeadLettered
	if stats.OutboxPending > 0 {
		status.DegradedReasons = appendUniqueChannelMonitorRedisDegradedReason(status.DegradedReasons, ChannelMonitorRedisDegradedReasonCostOutboxBacklog)
	}
	if stats.PublishFailed > 0 {
		status.DegradedReasons = appendUniqueChannelMonitorRedisDegradedReason(status.DegradedReasons, ChannelMonitorRedisDegradedReasonCostPublishFailure)
	}
	if stats.DeadLettered > 0 {
		status.DegradedReasons = appendUniqueChannelMonitorRedisDegradedReason(status.DegradedReasons, ChannelMonitorRedisDegradedReasonCostDeadLetter)
	}
	if client != nil {
		streamLength, err := client.XLen(ctx, ChannelDailyCostRedisStream).Result()
		if err == nil {
			// Query the PEL independently of stream length. Redis can retain
			// pending IDs after an entry was deleted, so XLen == 0 does not
			// prove that the consumer group has no pending work.
			status.CostStreamUnreadCount = max(0, streamLength)
		}
		if pending, pendingErr := client.XPending(ctx, ChannelDailyCostRedisStream, ChannelDailyCostRedisConsumerGroup).Result(); pendingErr == nil {
			status.CostStreamPendingCount = max(0, pending.Count)
			if err == nil {
				status.CostStreamUnreadCount = max(0, streamLength-pending.Count)
			}
		}
	}
	if status.CostStreamPendingCount > 0 || status.CostStreamUnreadCount > 0 {
		status.DegradedReasons = appendUniqueChannelMonitorRedisDegradedReason(status.DegradedReasons, ChannelMonitorRedisDegradedReasonCostStreamBacklog)
	}
	status.RealtimeDegraded = len(status.DegradedReasons) > 0
}

func appendUniqueChannelMonitorRedisDegradedReason(reasons []string, reason string) []string {
	for _, existing := range reasons {
		if existing == reason {
			return reasons
		}
	}
	return append(reasons, reason)
}

func channelMonitorRedisObservationInt64(value string) int64 {
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func channelMonitorRedisLastGeneratedID(ctx context.Context, client *redis.Client) (string, error) {
	result, err := client.Do(ctx, "XINFO", "STREAM", ChannelMonitorRedisEventStream).Result()
	if err != nil {
		return "", err
	}
	fields, ok := result.([]interface{})
	if !ok || len(fields)%2 != 0 {
		return "", errors.New("渠道监控 Redis XINFO STREAM 响应无效")
	}
	for index := 0; index < len(fields); index += 2 {
		field, fieldErr := channelMonitorRedisReplyString(fields[index])
		if fieldErr != nil {
			return "", fieldErr
		}
		if field != "last-generated-id" {
			continue
		}
		return channelMonitorRedisReplyString(fields[index+1])
	}
	return "", errors.New("渠道监控 Redis XINFO STREAM 缺少 last-generated-id")
}

func loadChannelMonitorRedisGroups(ctx context.Context, client *redis.Client) ([]channelMonitorRedisGroupInfo, error) {
	result, err := client.Do(ctx, "XINFO", "GROUPS", ChannelMonitorRedisEventStream).Result()
	if err != nil {
		return nil, err
	}
	rawGroups, ok := result.([]interface{})
	if !ok {
		return nil, fmt.Errorf("渠道监控 Redis XINFO GROUPS 响应无效")
	}
	groups := make([]channelMonitorRedisGroupInfo, 0, len(rawGroups))
	for _, rawGroup := range rawGroups {
		fields, fieldsOK := rawGroup.([]interface{})
		if !fieldsOK || len(fields)%2 != 0 {
			return nil, fmt.Errorf("渠道监控 Redis XINFO GROUPS 条目无效")
		}
		var group channelMonitorRedisGroupInfo
		for index := 0; index < len(fields); index += 2 {
			field, fieldErr := channelMonitorRedisReplyString(fields[index])
			if fieldErr != nil {
				return nil, fieldErr
			}
			switch field {
			case "name":
				group.Name, fieldErr = channelMonitorRedisReplyString(fields[index+1])
			case "pending":
				group.Pending, fieldErr = channelMonitorRedisReplyInt64(fields[index+1])
			case "last-delivered-id":
				group.LastDeliveredID, fieldErr = channelMonitorRedisReplyString(fields[index+1])
			}
			if fieldErr != nil {
				return nil, fieldErr
			}
		}
		if group.Name == "" || group.LastDeliveredID == "" {
			return nil, fmt.Errorf("渠道监控 Redis XINFO GROUPS 缺少必要字段")
		}
		groups = append(groups, group)
	}
	return groups, nil
}

func channelMonitorRedisOldestUnprocessedID(
	ctx context.Context,
	client *redis.Client,
	group channelMonitorRedisGroupInfo,
	lastGeneratedID string,
) (string, error) {
	pending, err := client.XPending(ctx, ChannelMonitorRedisEventStream, group.Name).Result()
	if err != nil {
		return "", err
	}
	if pending.Count > 0 {
		return pending.Lower, nil
	}
	if lastGeneratedID == "" || group.LastDeliveredID == lastGeneratedID {
		return "", nil
	}
	if group.LastDeliveredID != "0-0" {
		less, compareErr := channelMonitorRedisStreamIDLess(group.LastDeliveredID, lastGeneratedID)
		if compareErr != nil {
			return "", compareErr
		}
		if !less {
			return "", nil
		}
	}
	start := group.LastDeliveredID
	entries, err := client.XRangeN(ctx, ChannelMonitorRedisEventStream, start, "+", 2).Result()
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.ID != start {
			return entry.ID, nil
		}
	}
	return "", nil
}

func channelMonitorRedisStreamIDTimestamp(value string) int64 {
	if value == "" || value == "0-0" {
		return 0
	}
	millisecondsText, _, found := strings.Cut(value, "-")
	if !found {
		return 0
	}
	milliseconds, err := strconv.ParseInt(millisecondsText, 10, 64)
	if err != nil || milliseconds <= 0 {
		return 0
	}
	return milliseconds / 1000
}

func refreshChannelMonitorRedisConsumerHeartbeat(ctx context.Context, client *redis.Client, consumerName string) error {
	if client == nil {
		return ErrChannelMonitorRedisConsumerUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	opCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisConsumerOperationTimeout)
	defer cancel()
	return client.Set(
		opCtx,
		ChannelMonitorRedisConsumerHeartbeatKey,
		consumerName,
		channelMonitorRedisAggregatorLeaseTTL,
	).Err()
}

func recordChannelMonitorRedisProcessedAt(client *redis.Client, processedAt int64) {
	if client == nil || processedAt <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelMonitorRedisConsumerOperationTimeout)
	defer cancel()
	_ = client.HSet(
		ctx,
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldLastProcessedAt,
		processedAt,
	).Err()
}

func incrementChannelMonitorRedisObservation(client *redis.Client, field string, amount int64) {
	if client == nil || amount <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelMonitorRedisConsumerOperationTimeout)
	defer cancel()
	_ = client.HIncrBy(ctx, ChannelMonitorRedisObservabilityKey, field, amount).Err()
}
