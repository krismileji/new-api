package controller

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type channelMonitorRealtimeResponseMetadata struct {
	DataCutoffAt               int64                                                  `json:"data_cutoff_at"`
	ProcessedAt                int64                                                  `json:"processed_at"`
	ProjectionStartedAt        int64                                                  `json:"projection_started_at"`
	EventWatermark             uint64                                                 `json:"event_watermark"`
	QueueDepth                 int                                                    `json:"queue_depth"`
	RedisStatus                string                                                 `json:"redis_status"`
	RedisAvailable             bool                                                   `json:"redis_available"`
	RedisPoolIsolation         bool                                                   `json:"redis_pool_isolation"`
	RedisPoolIsolationMode     string                                                 `json:"redis_pool_isolation_mode"`
	RedisPoolShared            bool                                                   `json:"redis_pool_shared"`
	RedisPoolDegradedRoles     []service.ChannelMonitorRedisPoolDegradedRole          `json:"redis_pool_degraded_roles"`
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
	CostStreamPendingCount     int64                                                  `json:"cost_stream_pending_count"`
	CostStreamUnreadCount      int64                                                  `json:"cost_stream_unread_count"`
	CostOutboxPendingCount     int64                                                  `json:"cost_outbox_pending_count"`
	CostOutboxOldestPendingAt  int64                                                  `json:"cost_outbox_oldest_pending_at"`
	CostOutboxRetryCount       int64                                                  `json:"cost_outbox_retry_count"`
	CostLedgerFailedCount      int64                                                  `json:"cost_ledger_failed_count"`
	CostPublishFailedCount     int64                                                  `json:"cost_publish_failed_count"`
	CostDeadLetterCount        int64                                                  `json:"cost_dead_letter_count"`
	MarkerReleaseFailureCount  int64                                                  `json:"marker_release_failure_count"`
	MarkerReleaseFailureActive bool                                                   `json:"marker_release_failure_active"`
	StreamTrimFailureCount     int64                                                  `json:"stream_trim_failure_count"`
	StreamTrimFailureActive    bool                                                   `json:"stream_trim_failure_active"`
	RedisPoolStats             map[common.RedisClientRole]common.RedisClientPoolStats `json:"redis_pool_stats"`
	DegradedReasons            []string                                               `json:"degraded_reasons"`
	RealtimeDegraded           bool                                                   `json:"realtime_degraded"`
	WindowStart                int64                                                  `json:"-"`
}

func channelMonitorRealtimeMetadata(windowStart int64) channelMonitorRealtimeResponseMetadata {
	return channelMonitorRealtimeMetadataWithContext(context.Background(), windowStart)
}

func channelMonitorRealtimeMetadataWithContext(ctx context.Context, windowStart int64) channelMonitorRealtimeResponseMetadata {
	now := time.Now().Unix()
	dataCutoffAt, processedAt, eventWatermark, projectionErr := service.GetChannelMonitorRedisSharedProjectionMetadata(ctx, windowStart, now+1)
	if projectionErr != nil {
		dataCutoffAt, processedAt, eventWatermark = 0, 0, 0
	}
	return channelMonitorRealtimeProjectionMetadata(ctx, windowStart, dataCutoffAt, processedAt, eventWatermark)
}

func channelMonitorRealtimeProjectionMetadata(
	ctx context.Context,
	windowStart int64,
	dataCutoffAt int64,
	processedAt int64,
	eventWatermark uint64,
) channelMonitorRealtimeResponseMetadata {
	redisStatus := service.GetChannelMonitorRedisRealtimeStatus(ctx)
	return channelMonitorRealtimeResponseMetadata{
		DataCutoffAt:               dataCutoffAt,
		ProcessedAt:                processedAt,
		ProjectionStartedAt:        0,
		EventWatermark:             eventWatermark,
		QueueDepth:                 int(redisStatus.PendingCount),
		RedisStatus:                redisStatus.RedisStatus,
		RedisAvailable:             redisStatus.RedisAvailable,
		RedisPoolIsolation:         redisStatus.RedisPoolIsolation,
		RedisPoolIsolationMode:     redisStatus.RedisPoolIsolationMode,
		RedisPoolShared:            redisStatus.RedisPoolShared,
		RedisPoolDegradedRoles:     redisStatus.RedisPoolDegradedRoles,
		RedisConsumerRunning:       redisStatus.RedisConsumerRunning,
		PendingCount:               redisStatus.PendingCount,
		WriterQueueDepth:           redisStatus.WriterQueueDepth,
		WriterQueueCapacity:        redisStatus.WriterQueueCapacity,
		WriterQueuedEvents:         redisStatus.WriterQueuedEvents,
		WriterDroppedEvents:        redisStatus.WriterDroppedEvents,
		WriterRetryEvents:          redisStatus.WriterRetryEvents,
		WriterOldestQueuedAt:       redisStatus.WriterOldestQueuedAt,
		WriterQueueAgeSeconds:      redisStatus.WriterQueueAgeSeconds,
		OldestPendingAt:            redisStatus.OldestPendingAt,
		ConsumerLagSeconds:         redisStatus.ConsumerLagSeconds,
		LastPublishedAt:            redisStatus.LastPublishedAt,
		LastProcessedAt:            redisStatus.LastProcessedAt,
		RetryCount:                 redisStatus.RetryCount,
		TakeoverCount:              redisStatus.TakeoverCount,
		QuarantineCount:            redisStatus.QuarantineCount,
		LastQuarantinedAt:          redisStatus.LastQuarantinedAt,
		RuntimeMarkerFailureCount:  redisStatus.RuntimeMarkerFailureCount,
		ScheduleMarkerFailureCount: redisStatus.ScheduleMarkerFailureCount,
		CostStreamPendingCount:     redisStatus.CostStreamPendingCount,
		CostStreamUnreadCount:      redisStatus.CostStreamUnreadCount,
		CostOutboxPendingCount:     redisStatus.CostOutboxPendingCount,
		CostOutboxOldestPendingAt:  redisStatus.CostOutboxOldestPendingAt,
		CostOutboxRetryCount:       redisStatus.CostOutboxRetryCount,
		CostLedgerFailedCount:      redisStatus.CostLedgerFailedCount,
		CostPublishFailedCount:     redisStatus.CostPublishFailedCount,
		CostDeadLetterCount:        redisStatus.CostDeadLetterCount,
		MarkerReleaseFailureCount:  redisStatus.MarkerReleaseFailureCount,
		MarkerReleaseFailureActive: redisStatus.MarkerReleaseFailureActive,
		StreamTrimFailureCount:     redisStatus.StreamTrimFailureCount,
		StreamTrimFailureActive:    redisStatus.StreamTrimFailureActive,
		RedisPoolStats:             redisStatus.RedisPoolStats,
		DegradedReasons:            redisStatus.DegradedReasons,
		RealtimeDegraded:           redisStatus.RealtimeDegraded,
		WindowStart:                windowStart,
	}
}

func channelMonitorRealtimePageMetadata(
	view service.ChannelMonitorRealtimePageView,
) channelMonitorRealtimeResponseMetadata {
	return channelMonitorRealtimePageMetadataWithContext(context.Background(), view)
}

func channelMonitorRealtimePageMetadataWithContext(
	ctx context.Context,
	view service.ChannelMonitorRealtimePageView,
) channelMonitorRealtimeResponseMetadata {
	return channelMonitorRealtimeProjectionMetadata(
		ctx, view.WindowStart, view.DataCutoffAt, view.ProcessedAt, view.EventWatermark,
	)
}

func channelMonitorRealtimePerformanceMetrics(
	view service.ChannelMonitorRealtimePageView,
) ([]model.ChannelMonitorPerformanceMetric, []model.ChannelMonitorSuccessMetric) {
	performance := make([]model.ChannelMonitorPerformanceMetric, 0, len(view.Routes))
	success := make([]model.ChannelMonitorSuccessMetric, 0, len(view.Routes))
	for _, route := range view.Routes {
		performance = append(performance, model.ChannelMonitorPerformanceMetric{
			ChannelId:               route.ChannelId,
			ModelName:               route.ModelName,
			SampleCount:             route.SampleCount,
			FirstTokenSampleCount:   route.FirstTokenSampleCount,
			TPSSampleCount:          route.TPSSampleCount,
			TPSOutputTokens:         route.TPSOutputTokens,
			TPSGenerationDurationMs: route.TPSGenerationDurationMs,
			AverageFirstTokenMs:     route.AverageFirstTokenMs,
			AverageTPS:              route.AverageTPS,
			LatestFirstTokenMs:      route.LatestFirstTokenMs,
			LatestTPS:               route.LatestTPS,
			LastUsedTime:            route.LastUsedTime,
		})
		success = append(success, model.ChannelMonitorSuccessMetric{
			ChannelId:                    route.ChannelId,
			ModelName:                    route.ModelName,
			ChannelMonitorSuccessSummary: route.Summary,
		})
	}
	return performance, success
}

func channelMonitorRealtimeGroupSuccessMetrics(
	view service.ChannelMonitorRealtimePageView,
) []model.ChannelMonitorGroupSuccessMetric {
	items := make([]model.ChannelMonitorGroupSuccessMetric, 0, len(view.Groups))
	for _, group := range view.Groups {
		items = append(items, model.ChannelMonitorGroupSuccessMetric{
			Group:                        group.GroupName,
			ChannelMonitorSuccessSummary: group.Summary,
		})
	}
	return items
}

func channelMonitorRealtimeTodaySuccessMetrics(
	view service.ChannelMonitorRealtimePageView,
) model.ChannelMonitorTodaySuccessMetrics {
	metrics := model.ChannelMonitorTodaySuccessMetrics{
		Summary:         view.Summary.Summary,
		ChannelItems:    make([]model.ChannelMonitorChannelSuccessMetric, 0, len(view.Channels)),
		APIKeyItems:     make([]model.ChannelMonitorSuccessAPIKeyMetric, 0, len(view.APIKeys)),
		CacheWriteItems: make([]model.ChannelMonitorTodayCacheWriteMetric, 0, len(view.Channels)),
	}
	for _, channel := range view.Channels {
		metrics.ChannelItems = append(metrics.ChannelItems, model.ChannelMonitorChannelSuccessMetric{
			ChannelId:                    channel.ChannelId,
			ChannelMonitorSuccessSummary: channel.Summary,
		})
		if channel.CacheWriteRequestCount > 0 {
			metrics.CacheWriteItems = append(metrics.CacheWriteItems, model.ChannelMonitorTodayCacheWriteMetric{
				ChannelId:    channel.ChannelId,
				RequestCount: channel.CacheWriteRequestCount,
			})
		}
	}
	for _, apiKey := range view.APIKeys {
		metrics.APIKeyItems = append(metrics.APIKeyItems, model.ChannelMonitorSuccessAPIKeyMetric{
			APIKeyId:                     apiKey.APIKeyId,
			APIKeyName:                   apiKey.APIKeyName,
			ChannelMonitorSuccessSummary: apiKey.Summary,
		})
	}
	return metrics
}
