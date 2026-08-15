package controller

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type channelMonitorRealtimeResponseMetadata struct {
	DataCutoffAt        int64  `json:"data_cutoff_at"`
	ProcessedAt         int64  `json:"processed_at"`
	ProjectionStartedAt int64  `json:"projection_started_at"`
	EventWatermark      uint64 `json:"event_watermark"`
	QueueDepth          int    `json:"queue_depth"`
	RealtimeDegraded    bool   `json:"realtime_degraded"`
	WindowStart         int64  `json:"-"`
}

func channelMonitorRealtimeMetadata(windowStart int64) channelMonitorRealtimeResponseMetadata {
	snapshot := service.GetChannelMonitorRealtimeGlobalSnapshot()
	queueStats := service.GetChannelMonitorEventQueueStats()
	projectionStartedAt := service.GetChannelMonitorRealtimeProjectionStartedAt()
	windowIncomplete := windowStart > 0 && projectionStartedAt > windowStart
	return channelMonitorRealtimeResponseMetadata{
		DataCutoffAt:        snapshot.DataCutoffAt,
		ProcessedAt:         snapshot.ProcessedAt,
		ProjectionStartedAt: projectionStartedAt,
		EventWatermark:      snapshot.EventWatermark,
		QueueDepth:          queueStats.QueueDepth,
		RealtimeDegraded:    windowIncomplete || queueStats.DroppedEvents > 0 || queueStats.FailedEvents > 0,
		WindowStart:         max(windowStart, projectionStartedAt),
	}
}

func channelMonitorRealtimePageMetadata(
	view service.ChannelMonitorRealtimePageView,
) channelMonitorRealtimeResponseMetadata {
	queueStats := service.GetChannelMonitorEventQueueStats()
	projectionStartedAt := service.GetChannelMonitorRealtimeProjectionStartedAt()
	return channelMonitorRealtimeResponseMetadata{
		DataCutoffAt:        view.DataCutoffAt,
		ProcessedAt:         view.ProcessedAt,
		ProjectionStartedAt: projectionStartedAt,
		EventWatermark:      view.EventWatermark,
		QueueDepth:          queueStats.QueueDepth,
		RealtimeDegraded:    projectionStartedAt > view.WindowStart || queueStats.DroppedEvents > 0 || queueStats.FailedEvents > 0,
		WindowStart:         max(view.WindowStart, projectionStartedAt),
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
