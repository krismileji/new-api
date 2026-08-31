package controller

import (
	"context"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type channelMonitorRealtimeChannelCost struct {
	CostNanoCNY               int64
	ProbeCostNanoCNY          int64
	ModelDetectionCostNanoCNY int64
	SettledCount              int64
	UnresolvedCount           int64
}

// channelMonitorRealtimeTodayCosts keeps its existing caller-facing name but
// reads the committed daily ledger. Redis remains monitoring metadata only.
func channelMonitorRealtimeTodayCosts(ctx context.Context, channelId int, dayStart int64) (map[int]channelMonitorRealtimeChannelCost, error) {
	rows, err := model.GetChannelDailyCostsForChannel(ctx, dayStart, dayStart+channelMonitorCostDaySeconds, channelId)
	if err != nil {
		return nil, err
	}
	costs := make(map[int]channelMonitorRealtimeChannelCost, len(rows))
	for _, row := range rows {
		costs[row.ChannelId] = channelMonitorRealtimeChannelCost{
			CostNanoCNY:               row.CostNanoCNY,
			ProbeCostNanoCNY:          row.ProbeCostNanoCNY,
			ModelDetectionCostNanoCNY: row.ModelDetectionCostNanoCNY,
			SettledCount:              row.SettledCount,
			UnresolvedCount:           row.UnresolvedCount,
		}
	}
	return costs, nil
}

// applyChannelMonitorRealtimeCost only attaches Redis projection health
// metadata. Cost amounts and counts come exclusively from the persisted daily
// cost tables so every filter and historical query reads the same ledger. The
// daily writer's roughly one-second flush delay is preferable to showing an
// uncommitted Redis amount that can disagree with later historical results.
func applyChannelMonitorRealtimeCost(
	ctx context.Context,
	overview *channelMonitorCostOverview,
	_ int,
	now int64,
	_ int,
	_ int64,
	_ bool,
) error {
	todayStart := channelMonitorCostDayStart(now)
	metadata := channelMonitorRealtimeMetadataWithContext(ctx, todayStart)
	overview.DataCutoffAt = metadata.DataCutoffAt
	overview.ProcessedAt = metadata.ProcessedAt
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
	return nil
}
