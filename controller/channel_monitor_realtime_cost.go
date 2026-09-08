package controller

import (
	"context"
	"github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type channelMonitorRealtimeChannelCost struct {
	CostNanoCNY               int64
	ProbeCostNanoCNY          int64
	GroupProbeCostNanoCNY     int64
	ModelDetectionCostNanoCNY int64
	SettledCount              int64
	UnresolvedCount           int64
}

// channelMonitorRealtimeTodayCosts keeps its existing caller-facing name. The
// current Beijing day is served from the reliable Redis cost projection.
// A Redis outage must not silently switch the page to a different ledger view.
func channelMonitorRealtimeTodayCosts(ctx context.Context, channelId int, dayStart int64) (map[int]channelMonitorRealtimeChannelCost, service.ChannelMonitorRedisSharedDailyCostView, error) {
	view, redisErr := service.QueryChannelMonitorRedisDailyCostTotals(ctx, dayStart)
	redisCosts := view.Channels
	if redisErr == nil {
		costs := make(map[int]channelMonitorRealtimeChannelCost, len(redisCosts))
		for id, aggregate := range redisCosts {
			if channelId > 0 && id != channelId {
				continue
			}
			costs[id] = channelMonitorRealtimeChannelCost{
				CostNanoCNY:               aggregate.SettledCostNanoCNY,
				ProbeCostNanoCNY:          aggregate.ProbeSettledCostNanoCNY,
				GroupProbeCostNanoCNY:     aggregate.GroupProbeSettledCostNanoCNY,
				ModelDetectionCostNanoCNY: aggregate.ModelDetectionSettledCostNanoCNY,
				SettledCount:              aggregate.SettledRequestCount,
				UnresolvedCount:           aggregate.UnresolvedRequestCount,
			}
		}
		return costs, view, nil
	}
	if common.RedisEnabled {
		return nil, view, redisErr
	}

	rows, err := model.GetChannelDailyCostsForChannel(ctx, dayStart, dayStart+channelMonitorCostDaySeconds, channelId)
	if err != nil {
		return nil, view, err
	}
	costs := make(map[int]channelMonitorRealtimeChannelCost, len(rows))
	for _, row := range rows {
		costs[row.ChannelId] = channelMonitorRealtimeChannelCost{
			CostNanoCNY:               row.CostNanoCNY,
			ProbeCostNanoCNY:          row.ProbeCostNanoCNY,
			GroupProbeCostNanoCNY:     row.GroupProbeCostNanoCNY,
			ModelDetectionCostNanoCNY: row.ModelDetectionCostNanoCNY,
			SettledCount:              row.SettledCount,
			UnresolvedCount:           row.UnresolvedCount,
		}
	}
	return costs, view, nil
}

// Cost readers have already combined historical daily rows with one Redis
// snapshot. Attach runtime health without replacing that cost snapshot's own
// processing timestamp with the ordinary monitoring consumer's timestamp.
func applyChannelMonitorRealtimeCost(
	ctx context.Context,
	overview *channelMonitorCostOverview,
	days int,
	now int64,
	channelId int,
	detailDayStart int64,
	summaryOnly bool,
) error {
	_ = days
	_ = detailDayStart
	_ = summaryOnly
	todayStart := channelMonitorCostDayStart(now)
	metadata := channelMonitorRealtimeMetadataWithContext(ctx, todayStart)
	if overview.CostSource != "redis_daily" {
		overview.DataCutoffAt = metadata.DataCutoffAt
		overview.ProcessedAt = metadata.ProcessedAt
	}
	overview.ProjectionStartedAt = metadata.ProjectionStartedAt
	overview.EventWatermark = metadata.EventWatermark
	overview.QueueDepth = metadata.QueueDepth
	overview.RedisStatus = metadata.RedisStatus
	overview.RedisAvailable = metadata.RedisAvailable
	overview.RedisConsumerRunning = metadata.RedisConsumerRunning
	overview.PendingCount = metadata.PendingCount
	overview.WriterQueueDepth = metadata.WriterQueueDepth
	overview.WriterQueueCapacity = metadata.WriterQueueCapacity
	overview.WriterQueuedEvents = metadata.WriterQueuedEvents
	overview.WriterDroppedEvents = metadata.WriterDroppedEvents
	overview.WriterRetryEvents = metadata.WriterRetryEvents
	overview.WriterOldestQueuedAt = metadata.WriterOldestQueuedAt
	overview.WriterQueueAgeSeconds = metadata.WriterQueueAgeSeconds
	overview.OldestPendingAt = metadata.OldestPendingAt
	overview.ConsumerLagSeconds = metadata.ConsumerLagSeconds
	overview.LastPublishedAt = metadata.LastPublishedAt
	overview.LastProcessedAt = metadata.LastProcessedAt
	overview.RetryCount = metadata.RetryCount
	overview.TakeoverCount = metadata.TakeoverCount
	overview.QuarantineCount = metadata.QuarantineCount
	overview.LastQuarantinedAt = metadata.LastQuarantinedAt
	overview.RuntimeMarkerFailureCount = metadata.RuntimeMarkerFailureCount
	overview.ScheduleMarkerFailureCount = metadata.ScheduleMarkerFailureCount
	overview.CostQueuePendingCount = service.GetChannelDailyCostPendingCount()
	overview.CostStreamPendingCount = metadata.CostStreamPendingCount
	overview.CostStreamUnreadCount = metadata.CostStreamUnreadCount
	overview.CostOutboxPendingCount = metadata.CostOutboxPendingCount
	overview.CostOutboxOldestPendingAt = metadata.CostOutboxOldestPendingAt
	overview.CostOutboxRetryCount = metadata.CostOutboxRetryCount
	overview.CostLedgerFailedCount = metadata.CostLedgerFailedCount
	overview.CostPublishFailedCount = metadata.CostPublishFailedCount
	overview.CostDeadLetterCount = metadata.CostDeadLetterCount
	overview.MarkerReleaseFailureCount = metadata.MarkerReleaseFailureCount
	overview.MarkerReleaseFailureActive = metadata.MarkerReleaseFailureActive
	overview.StreamTrimFailureCount = metadata.StreamTrimFailureCount
	overview.StreamTrimFailureActive = metadata.StreamTrimFailureActive
	overview.RedisPoolStats = metadata.RedisPoolStats
	overview.RealtimeDegraded = metadata.RealtimeDegraded
	overview.DegradedReasons = metadata.DegradedReasons
	if overview.CostSource == "redis_daily" && (overview.CostProjection.Failed || overview.CostProjection.CheckedAt == 0 || now-overview.CostProjection.CheckedAt > 10) {
		overview.RealtimeDegraded = true
		overview.DegradedReasons = append(overview.DegradedReasons, "cost_projection_unavailable")
	} else if overview.CostProjection.Pending {
		overview.RealtimeDegraded = true
		overview.DegradedReasons = append(overview.DegradedReasons, "cost_projection_pending")
	}
	settings := getChannelMonitorSettings()
	service.NotifyChannelMonitorHealthAsync(settings.EmailNotificationEnabled, settings.NotificationEmail, metadata.RedisStatus, metadata.DegradedReasons, metadata.WriterDroppedEvents, settings.EmailNotificationTypes...)
	return nil
}
