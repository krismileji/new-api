package controller

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type channelMonitorRealtimeResponseMetadata struct {
	DataCutoffAt               int64    `json:"data_cutoff_at"`
	ProcessedAt                int64    `json:"processed_at"`
	ProjectionStartedAt        int64    `json:"projection_started_at"`
	EventWatermark             uint64   `json:"event_watermark"`
	QueueDepth                 int      `json:"queue_depth"`
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
	WindowStart                int64    `json:"-"`
}

func channelMonitorRealtimeMetadata(windowStart int64) channelMonitorRealtimeResponseMetadata {
	now := time.Now().Unix()
	dataCutoffAt, processedAt, eventWatermark, projectionErr := service.GetChannelMonitorRedisSharedProjectionMetadata(context.Background(), windowStart, now+1)
	redisStatus := service.GetChannelMonitorRedisRealtimeStatus(context.Background())
	if projectionErr != nil {
		dataCutoffAt, processedAt, eventWatermark = 0, 0, 0
	}
	return channelMonitorRealtimeResponseMetadata{
		DataCutoffAt:               dataCutoffAt,
		ProcessedAt:                processedAt,
		ProjectionStartedAt:        0,
		EventWatermark:             eventWatermark,
		QueueDepth:                 int(redisStatus.PendingCount),
		RedisStatus:                redisStatus.RedisStatus,
		RedisAvailable:             redisStatus.RedisAvailable,
		RedisConsumerRunning:       redisStatus.RedisConsumerRunning,
		PendingCount:               redisStatus.PendingCount,
		OldestPendingAt:            redisStatus.OldestPendingAt,
		ConsumerLagSeconds:         redisStatus.ConsumerLagSeconds,
		LastPublishedAt:            redisStatus.LastPublishedAt,
		LastProcessedAt:            redisStatus.LastProcessedAt,
		RetryCount:                 redisStatus.RetryCount,
		TakeoverCount:              redisStatus.TakeoverCount,
		MarkerReleaseFailureCount:  redisStatus.MarkerReleaseFailureCount,
		MarkerReleaseFailureActive: redisStatus.MarkerReleaseFailureActive,
		StreamTrimFailureCount:     redisStatus.StreamTrimFailureCount,
		StreamTrimFailureActive:    redisStatus.StreamTrimFailureActive,
		DegradedReasons:            redisStatus.DegradedReasons,
		RealtimeDegraded:           redisStatus.RealtimeDegraded,
		WindowStart:                windowStart,
	}
}

func channelMonitorRealtimePageMetadata(
	view service.ChannelMonitorRealtimePageView,
) channelMonitorRealtimeResponseMetadata {
	redisStatus := service.GetChannelMonitorRedisRealtimeStatus(context.Background())
	projectionStartedAt := int64(0)
	return channelMonitorRealtimeResponseMetadata{
		DataCutoffAt:               view.DataCutoffAt,
		ProcessedAt:                view.ProcessedAt,
		ProjectionStartedAt:        projectionStartedAt,
		EventWatermark:             view.EventWatermark,
		QueueDepth:                 int(redisStatus.PendingCount),
		RedisStatus:                redisStatus.RedisStatus,
		RedisAvailable:             redisStatus.RedisAvailable,
		RedisConsumerRunning:       redisStatus.RedisConsumerRunning,
		PendingCount:               redisStatus.PendingCount,
		OldestPendingAt:            redisStatus.OldestPendingAt,
		ConsumerLagSeconds:         redisStatus.ConsumerLagSeconds,
		LastPublishedAt:            redisStatus.LastPublishedAt,
		LastProcessedAt:            redisStatus.LastProcessedAt,
		RetryCount:                 redisStatus.RetryCount,
		TakeoverCount:              redisStatus.TakeoverCount,
		MarkerReleaseFailureCount:  redisStatus.MarkerReleaseFailureCount,
		MarkerReleaseFailureActive: redisStatus.MarkerReleaseFailureActive,
		StreamTrimFailureCount:     redisStatus.StreamTrimFailureCount,
		StreamTrimFailureActive:    redisStatus.StreamTrimFailureActive,
		DegradedReasons:            redisStatus.DegradedReasons,
		RealtimeDegraded:           redisStatus.RealtimeDegraded,
		WindowStart:                view.WindowStart,
	}
}

func channelMonitorRealtimePerformanceMetrics(
	view service.ChannelMonitorRealtimePageView,
) ([]model.ChannelMonitorPerformanceMetric, []model.ChannelMonitorSuccessMetric) {
	performance := make([]model.ChannelMonitorPerformanceMetric, 0, len(view.Routes))
	success := make([]model.ChannelMonitorSuccessMetric, 0, len(view.Routes))
	for _, route := range view.Routes {
		performance = append(performance, model.ChannelMonitorPerformanceMetric{
			ChannelId:             route.ChannelId,
			ModelName:             route.ModelName,
			SampleCount:           route.SampleCount,
			FirstTokenSampleCount: route.FirstTokenSampleCount,
			TPSSampleCount:        route.TPSSampleCount,
			AverageFirstTokenMs:   route.AverageFirstTokenMs,
			AverageTPS:            route.AverageTPS,
			LatestFirstTokenMs:    route.LatestFirstTokenMs,
			LatestTPS:             route.LatestTPS,
			LastUsedTime:          route.LastUsedTime,
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
