package service

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// ChannelMonitorRedisKeyPrefix is the versioned prefix owned by the
	// channel-monitor Redis Streams implementation.
	ChannelMonitorRedisKeyPrefix = "channel_monitor:v1"

	ChannelMonitorRedisEventStream    = ChannelMonitorRedisKeyPrefix + ":events"
	ChannelMonitorRedisConsumerGroup  = ChannelMonitorRedisKeyPrefix + ":aggregators"
	ChannelMonitorRedisConsumerPrefix = ChannelMonitorRedisKeyPrefix + ":consumer:"

	ChannelMonitorRedisEventFieldEventID = "event_id"
	ChannelMonitorRedisEventFieldPayload = "payload"

	ChannelMonitorRedisAggregatorLeaseKey   = ChannelMonitorRedisKeyPrefix + ":aggregator:lease"
	ChannelMonitorRedisConsumerHeartbeatKey = ChannelMonitorRedisKeyPrefix + ":consumer:heartbeat"
	ChannelMonitorRedisObservabilityKey     = ChannelMonitorRedisKeyPrefix + ":observability"

	ChannelMonitorRedisObservabilityFieldLastProcessedAt            = "last_processed_at"
	ChannelMonitorRedisObservabilityFieldRetryCount                 = "retry_count"
	ChannelMonitorRedisObservabilityFieldTakeoverCount              = "takeover_count"
	ChannelMonitorRedisObservabilityFieldRuntimeMarkerFailureCount  = "runtime_marker_failure_count"
	ChannelMonitorRedisObservabilityFieldScheduleMarkerFailureCount = "schedule_marker_failure_count"
	ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureCount  = "marker_release_failure_count"
	ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureActive = "marker_release_failure_active"
	ChannelMonitorRedisObservabilityFieldStreamTrimFailureCount     = "stream_trim_failure_count"
	ChannelMonitorRedisObservabilityFieldStreamTrimFailureActive    = "stream_trim_failure_active"

	ChannelMonitorRedisProjectionPrefix          = ChannelMonitorRedisKeyPrefix + ":projection:"
	ChannelMonitorRedisRouteProjectionPrefix     = ChannelMonitorRedisProjectionPrefix + "route:"
	ChannelMonitorRedisDashboardProjectionPrefix = ChannelMonitorRedisProjectionPrefix + "dashboard:"
	ChannelMonitorRedisCostProjectionPrefix      = ChannelMonitorRedisProjectionPrefix + "cost:"
	ChannelMonitorRedisDedupProjectionPrefix     = ChannelMonitorRedisProjectionPrefix + "dedup:"
	ChannelMonitorRedisSharedEventPrefix         = ChannelMonitorRedisProjectionPrefix + "shared:event:"
	ChannelMonitorRedisRuntimeEffectPrefix       = ChannelMonitorRedisProjectionPrefix + "runtime:event:"
	ChannelMonitorRedisSchedulingDedupPrefix     = ChannelMonitorRedisProjectionPrefix + "schedule:event:"
)

// ChannelMonitorRedisConsumerName returns a stable, prefix-scoped consumer
// name for a node or process identity.
func ChannelMonitorRedisConsumerName(identity string) string {
	return ChannelMonitorRedisConsumerPrefix + channelMonitorRedisKeyPart(identity, "node")
}

// ChannelMonitorRedisProjectionKey builds a shared projection key. The kind
// and identity are escaped so neither can escape the versioned prefix or
// collide with another value.
func ChannelMonitorRedisProjectionKey(kind, identity string) string {
	return ChannelMonitorRedisProjectionPrefix +
		channelMonitorRedisKeyPart(kind, "shared") + ":" +
		channelMonitorRedisKeyPart(identity, "default")
}

func channelMonitorRedisKeyPart(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}

	var builder strings.Builder
	for _, b := range []byte(value) {
		switch {
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b == '.', b == '-', b == '_':
			builder.WriteByte(b)
		default:
			builder.WriteString(fmt.Sprintf("~%02X", b))
		}
	}
	if builder.Len() == 0 {
		return fallback
	}
	return builder.String()
}

// ChannelMonitorRedisProjectionKeyForRoute is the stable key form reserved
// for REDIS-04 route health projections.
func ChannelMonitorRedisProjectionKeyForRoute(routeIdentity string) string {
	return fmt.Sprintf("%s%s", ChannelMonitorRedisRouteProjectionPrefix, channelMonitorRedisKeyPart(routeIdentity, "default"))
}

// ChannelMonitorRedisProjectionKeyForDashboard is the stable key form
// reserved for REDIS-05 dashboard projections.
func ChannelMonitorRedisProjectionKeyForDashboard(scope string) string {
	return fmt.Sprintf("%s%s", ChannelMonitorRedisDashboardProjectionPrefix, channelMonitorRedisKeyPart(scope, "global"))
}

// ChannelMonitorRedisProjectionKeyForCost is the stable key form reserved for
// REDIS-05 daily cost projections.
func ChannelMonitorRedisProjectionKeyForCost(scope string) string {
	return fmt.Sprintf("%s%s", ChannelMonitorRedisCostProjectionPrefix, channelMonitorRedisKeyPart(scope, "global"))
}

// ChannelMonitorRedisDashboardMinuteKey returns the compact dashboard hash
// for one UTC minute.
func ChannelMonitorRedisDashboardMinuteKey(minuteStart int64) string {
	return ChannelMonitorRedisDashboardProjectionPrefix + "minute:" + strconv.FormatInt(minuteStart, 10)
}

// ChannelMonitorRedisCostDayKey returns the compact daily-cost hash for one
// UTC day.
func ChannelMonitorRedisCostDayKey(dayStart int64) string {
	return ChannelMonitorRedisCostProjectionPrefix + "day:" + strconv.FormatInt(dayStart, 10)
}

// ChannelMonitorRedisCostEventStateKey returns the compact state key used to
// replace an unresolved cost with its settled value exactly once.
func ChannelMonitorRedisCostEventStateKey(costEventID string) string {
	return ChannelMonitorRedisCostProjectionPrefix + "event:" + channelMonitorRedisKeyPart(costEventID, "default")
}

// ChannelMonitorRedisProjectionDedupKey is the stable key form reserved for
// REDIS-03 event idempotency markers.
func ChannelMonitorRedisProjectionDedupKey(eventID string) string {
	return fmt.Sprintf("%s%s", ChannelMonitorRedisDedupProjectionPrefix, channelMonitorRedisKeyPart(eventID, "default"))
}

// ChannelMonitorRedisSharedEventKey marks a shared dashboard/cost projection
// event after its aggregate deltas have committed atomically.
func ChannelMonitorRedisSharedEventKey(eventID string) string {
	return fmt.Sprintf("%s%s", ChannelMonitorRedisSharedEventPrefix, channelMonitorRedisKeyPart(eventID, "default"))
}

// ChannelMonitorRedisRuntimeEffectKey marks an event after the logical
// aggregator has handed it to the existing runtime protection/refresh hook.
func ChannelMonitorRedisRuntimeEffectKey(eventID string) string {
	return fmt.Sprintf("%s%s", ChannelMonitorRedisRuntimeEffectPrefix, channelMonitorRedisKeyPart(eventID, "default"))
}

// ChannelMonitorRedisSchedulingDedupKey marks a scheduling-eligible event
// after the logical aggregator has successfully requested a full schedule.
func ChannelMonitorRedisSchedulingDedupKey(eventID string) string {
	return fmt.Sprintf("%s%s", ChannelMonitorRedisSchedulingDedupPrefix, channelMonitorRedisKeyPart(eventID, "default"))
}
