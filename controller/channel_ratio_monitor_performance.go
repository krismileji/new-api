package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const (
	defaultChannelMonitorPerformanceMinutes = 15
	minChannelMonitorPerformanceMinutes     = 1
	maxChannelMonitorPerformanceMinutes     = 1440
	channelMonitorPerformanceRangeManual    = "manual"
	channelMonitorPerformanceRangeSmart     = "smart_schedule"
)

func getChannelMonitorPerformanceMinutes(c *gin.Context) (int, bool) {
	minutes := defaultChannelMonitorPerformanceMinutes
	if rawMinutes := c.Query("minutes"); rawMinutes != "" {
		parsedMinutes, err := strconv.Atoi(rawMinutes)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "性能与成功率统计范围必须在 1 到 1440 分钟之间"})
			return 0, false
		}
		minutes = parsedMinutes
	}
	if minutes < minChannelMonitorPerformanceMinutes || minutes > maxChannelMonitorPerformanceMinutes {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "性能与成功率统计范围必须在 1 到 1440 分钟之间"})
		return 0, false
	}
	return minutes, true
}

func getChannelMonitorPerformanceRange(c *gin.Context, settings channelMonitorSettings) (minutes int, source string, ok bool) {
	if settings.SmartScheduleEnabled && len(settings.SmartScheduleGroupPolicies) > 0 {
		return settings.SmartSchedulePerformanceWindowMinutes, channelMonitorPerformanceRangeSmart, true
	}
	minutes, ok = getChannelMonitorPerformanceMinutes(c)
	return minutes, channelMonitorPerformanceRangeManual, ok
}

func GetChannelMonitorPerformance(c *gin.Context) {
	settings, err := loadChannelMonitorSettings(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	minutes, rangeSource, ok := getChannelMonitorPerformanceRange(c, settings)
	if !ok {
		return
	}
	requestedAt := time.Now()
	generatedAt := requestedAt.Unix()
	requestedWindowEnd := generatedAt - generatedAt%60 + 60
	requestedWindowStart := requestedWindowEnd - int64(minutes)*60
	view, err := service.QueryChannelMonitorRealtimePerformanceFromRedis(c.Request.Context(), requestedWindowStart, generatedAt+1)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	metrics, successMetrics := channelMonitorRealtimePerformanceMetrics(view)
	groupSuccessMetrics := channelMonitorRealtimeGroupSuccessMetrics(view)
	metadata := channelMonitorRealtimePageMetadataWithContext(c.Request.Context(), view)
	metricCoverage := channelMonitorPerformanceMetricCoverageResponse{
		AggregationEnabled: true,
		AggregatedFrom:     metadata.WindowStart,
		AggregatedThrough:  metadata.DataCutoffAt,
		WindowStart:        view.WindowStart,
		WindowComplete: !metadata.RealtimeDegraded &&
			metadata.DataCutoffAt >= view.WindowEnd,
	}
	common.ApiSuccess(c, gin.H{
		"range_minutes":                 minutes,
		"range_source":                  rangeSource,
		"generated_at":                  generatedAt,
		"window_start":                  view.WindowStart,
		"data_cutoff_at":                metadata.DataCutoffAt,
		"processed_at":                  metadata.ProcessedAt,
		"projection_started_at":         metadata.ProjectionStartedAt,
		"event_watermark":               metadata.EventWatermark,
		"queue_depth":                   metadata.QueueDepth,
		"redis_status":                  metadata.RedisStatus,
		"redis_available":               metadata.RedisAvailable,
		"redis_pool_isolation":          metadata.RedisPoolIsolation,
		"redis_pool_isolation_mode":     metadata.RedisPoolIsolationMode,
		"redis_pool_shared":             metadata.RedisPoolShared,
		"redis_pool_degraded_roles":     metadata.RedisPoolDegradedRoles,
		"redis_consumer_running":        metadata.RedisConsumerRunning,
		"pending_count":                 metadata.PendingCount,
		"writer_queue_depth":            metadata.WriterQueueDepth,
		"writer_queue_capacity":         metadata.WriterQueueCapacity,
		"writer_queued_events":          metadata.WriterQueuedEvents,
		"writer_dropped_events":         metadata.WriterDroppedEvents,
		"writer_retry_events":           metadata.WriterRetryEvents,
		"writer_oldest_queued_at":       metadata.WriterOldestQueuedAt,
		"writer_queue_age_seconds":      metadata.WriterQueueAgeSeconds,
		"oldest_pending_at":             metadata.OldestPendingAt,
		"consumer_lag_seconds":          metadata.ConsumerLagSeconds,
		"last_published_at":             metadata.LastPublishedAt,
		"last_processed_at":             metadata.LastProcessedAt,
		"retry_count":                   metadata.RetryCount,
		"takeover_count":                metadata.TakeoverCount,
		"quarantine_count":              metadata.QuarantineCount,
		"last_quarantined_at":           metadata.LastQuarantinedAt,
		"runtime_marker_failure_count":  metadata.RuntimeMarkerFailureCount,
		"schedule_marker_failure_count": metadata.ScheduleMarkerFailureCount,
		"cost_stream_pending_count":     metadata.CostStreamPendingCount,
		"cost_stream_unread_count":      metadata.CostStreamUnreadCount,
		"cost_outbox_pending_count":     metadata.CostOutboxPendingCount,
		"cost_outbox_oldest_pending_at": metadata.CostOutboxOldestPendingAt,
		"cost_outbox_retry_count":       metadata.CostOutboxRetryCount,
		"cost_ledger_failed_count":      metadata.CostLedgerFailedCount,
		"cost_publish_failed_count":     metadata.CostPublishFailedCount,
		"cost_dead_letter_count":        metadata.CostDeadLetterCount,
		"marker_release_failure_count":  metadata.MarkerReleaseFailureCount,
		"marker_release_failure_active": metadata.MarkerReleaseFailureActive,
		"stream_trim_failure_count":     metadata.StreamTrimFailureCount,
		"stream_trim_failure_active":    metadata.StreamTrimFailureActive,
		"redis_pool_stats":              metadata.RedisPoolStats,
		"degraded_reasons":              metadata.DegradedReasons,
		"realtime_degraded":             metadata.RealtimeDegraded,
		"metric_coverage":               metricCoverage,
		"items":                         metrics,
		"success_metrics_available":     true,
		"success_items":                 successMetrics,
		"group_success_items":           groupSuccessMetrics,
	})
}

func GetChannelMonitorSuccessDetail(c *gin.Context) {
	minutes, ok := getChannelMonitorPerformanceMinutes(c)
	if !ok {
		return
	}

	requestedAt := time.Now()
	generatedAt := requestedAt.Unix()
	rawChannelId := strings.TrimSpace(c.Query("channel_id"))
	group := strings.TrimSpace(c.Query("group"))
	if (rawChannelId == "" && group == "") || (rawChannelId != "" && group != "") {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "成功率明细必须指定一个渠道或分组"})
		return
	}

	filter := model.ChannelMonitorSuccessFilter{}
	scope := "group"
	if rawChannelId != "" {
		channelId, err := strconv.Atoi(rawChannelId)
		if err != nil || channelId <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道 ID 无效"})
			return
		}
		filter.ChannelId = channelId
		filter.ModelName = strings.TrimSpace(c.Query("model_name"))
		scope = "channel"
	} else {
		filter.Group = group
	}
	requestedWindowEnd := generatedAt - generatedAt%60 + 60
	requestedWindowStart := requestedWindowEnd - int64(minutes)*60
	detailView, err := service.QueryChannelMonitorRealtimeSuccessDetailFromRedis(
		c.Request.Context(),
		requestedWindowStart, generatedAt+1, filter,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	detail := detailView.Detail
	apiKeyItemTotal := len(detail.APIKeyItems)
	apiKeyItemsTruncated := false
	if len(detail.APIKeyItems) > channelMonitorCostAPIKeyMaxRows {
		detail.APIKeyItems = detail.APIKeyItems[:channelMonitorCostAPIKeyMaxRows]
		apiKeyItemsTruncated = true
	}
	failureCategoryTotal := len(detail.FailureCategories)
	failureCategoriesTruncated := false
	if len(detail.FailureCategories) > channelMonitorCostAPIKeyMaxRows {
		detail.FailureCategories = detail.FailureCategories[:channelMonitorCostAPIKeyMaxRows]
		failureCategoriesTruncated = true
	}
	if err := attachChannelMonitorSuccessAPIKeyOwners(c.Request.Context(), &detail.APIKeyItems); err != nil {
		common.ApiError(c, err)
		return
	}
	metadata := channelMonitorRealtimeProjectionMetadata(
		c.Request.Context(), detailView.WindowStart,
		detailView.DataCutoffAt, detailView.ProcessedAt, detailView.EventWatermark,
	)
	common.ApiSuccess(c, gin.H{
		"range_minutes":                 minutes,
		"generated_at":                  generatedAt,
		"window_start":                  detailView.WindowStart,
		"data_cutoff_at":                detailView.DataCutoffAt,
		"processed_at":                  detailView.ProcessedAt,
		"projection_started_at":         metadata.ProjectionStartedAt,
		"event_watermark":               detailView.EventWatermark,
		"queue_depth":                   metadata.QueueDepth,
		"redis_status":                  metadata.RedisStatus,
		"redis_available":               metadata.RedisAvailable,
		"redis_pool_isolation":          metadata.RedisPoolIsolation,
		"redis_pool_isolation_mode":     metadata.RedisPoolIsolationMode,
		"redis_pool_shared":             metadata.RedisPoolShared,
		"redis_pool_degraded_roles":     metadata.RedisPoolDegradedRoles,
		"redis_consumer_running":        metadata.RedisConsumerRunning,
		"pending_count":                 metadata.PendingCount,
		"writer_queue_depth":            metadata.WriterQueueDepth,
		"writer_queue_capacity":         metadata.WriterQueueCapacity,
		"writer_queued_events":          metadata.WriterQueuedEvents,
		"writer_dropped_events":         metadata.WriterDroppedEvents,
		"writer_retry_events":           metadata.WriterRetryEvents,
		"writer_oldest_queued_at":       metadata.WriterOldestQueuedAt,
		"writer_queue_age_seconds":      metadata.WriterQueueAgeSeconds,
		"oldest_pending_at":             metadata.OldestPendingAt,
		"consumer_lag_seconds":          metadata.ConsumerLagSeconds,
		"last_published_at":             metadata.LastPublishedAt,
		"last_processed_at":             metadata.LastProcessedAt,
		"retry_count":                   metadata.RetryCount,
		"takeover_count":                metadata.TakeoverCount,
		"quarantine_count":              metadata.QuarantineCount,
		"last_quarantined_at":           metadata.LastQuarantinedAt,
		"runtime_marker_failure_count":  metadata.RuntimeMarkerFailureCount,
		"schedule_marker_failure_count": metadata.ScheduleMarkerFailureCount,
		"cost_stream_pending_count":     metadata.CostStreamPendingCount,
		"cost_stream_unread_count":      metadata.CostStreamUnreadCount,
		"cost_outbox_pending_count":     metadata.CostOutboxPendingCount,
		"cost_outbox_oldest_pending_at": metadata.CostOutboxOldestPendingAt,
		"cost_outbox_retry_count":       metadata.CostOutboxRetryCount,
		"cost_ledger_failed_count":      metadata.CostLedgerFailedCount,
		"cost_publish_failed_count":     metadata.CostPublishFailedCount,
		"cost_dead_letter_count":        metadata.CostDeadLetterCount,
		"marker_release_failure_count":  metadata.MarkerReleaseFailureCount,
		"marker_release_failure_active": metadata.MarkerReleaseFailureActive,
		"stream_trim_failure_count":     metadata.StreamTrimFailureCount,
		"stream_trim_failure_active":    metadata.StreamTrimFailureActive,
		"redis_pool_stats":              metadata.RedisPoolStats,
		"degraded_reasons":              metadata.DegradedReasons,
		"realtime_degraded":             metadata.RealtimeDegraded || metadata.ProjectionStartedAt > detailView.WindowStart,
		"success_metrics_available":     true,
		"scope":                         scope,
		"api_key_item_total":            apiKeyItemTotal,
		"api_key_items_truncated":       apiKeyItemsTruncated,
		"failure_category_total":        failureCategoryTotal,
		"failure_categories_truncated":  failureCategoriesTruncated,
		"detail":                        detail,
	})
}
