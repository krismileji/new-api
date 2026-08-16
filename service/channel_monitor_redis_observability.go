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
)

// ChannelMonitorRedisRealtimeStatus is the shared status contract returned by
// channel-monitor realtime APIs. PendingCount is the Redis consumer-group
// pending count; queue_depth remains an API alias for existing callers.
type ChannelMonitorRedisRealtimeStatus struct {
	RedisStatus                string   `json:"redis_status"`
	RedisAvailable             bool     `json:"redis_available"`
	RedisConsumerRunning       bool     `json:"redis_consumer_running"`
	PendingCount               int64    `json:"pending_count"`
	OldestPendingAt            int64    `json:"oldest_pending_at"`
	ConsumerLagSeconds         int64    `json:"consumer_lag_seconds"`
	LastPublishedAt            int64    `json:"last_published_at"`
	LastProcessedAt            int64    `json:"last_processed_at"`
	RetryCount                 int64    `json:"retry_count"`
	TakeoverCount              int64    `json:"takeover_count"`
	MarkerReleaseFailureCount  int64    `json:"marker_release_failure_count"`
	MarkerReleaseFailureActive bool     `json:"marker_release_failure_active"`
	StreamTrimFailureCount     int64    `json:"stream_trim_failure_count"`
	StreamTrimFailureActive    bool     `json:"stream_trim_failure_active"`
	DegradedReasons            []string `json:"degraded_reasons"`
	RealtimeDegraded           bool     `json:"realtime_degraded"`
}

func GetChannelMonitorRedisRealtimeStatus(ctx context.Context) ChannelMonitorRedisRealtimeStatus {
	status := channelMonitorRedisUnavailableStatus()
	if !common.RedisEnabled || common.RDB == nil {
		return status
	}
	if ctx == nil {
		ctx = context.Background()
	}
	queryCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisObservabilityTimeout)
	defer cancel()
	return getChannelMonitorRedisRealtimeStatus(queryCtx, common.RDB, time.Now())
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
	return status
}

func channelMonitorRedisUnavailableStatus() ChannelMonitorRedisRealtimeStatus {
	return ChannelMonitorRedisRealtimeStatus{
		RedisStatus:      ChannelMonitorRedisStatusUnavailable,
		DegradedReasons:  []string{ChannelMonitorRedisDegradedReasonRedisUnavailable},
		RealtimeDegraded: true,
	}
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
