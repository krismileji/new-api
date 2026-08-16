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

func getChannelMonitorPerformanceRange(c *gin.Context) (minutes int, source string, ok bool) {
	settings := getChannelMonitorSettings()
	if settings.SmartScheduleEnabled && len(settings.SmartScheduleGroupPolicies) > 0 {
		return settings.SmartSchedulePerformanceWindowMinutes, channelMonitorPerformanceRangeSmart, true
	}
	minutes, ok = getChannelMonitorPerformanceMinutes(c)
	return minutes, channelMonitorPerformanceRangeManual, ok
}

func GetChannelMonitorPerformance(c *gin.Context) {
	minutes, rangeSource, ok := getChannelMonitorPerformanceRange(c)
	if !ok {
		return
	}
	requestedAt := time.Now()
	generatedAt := requestedAt.Unix()
	requestedWindowStart := generatedAt - int64(minutes*60)
	view, err := service.QueryChannelMonitorRealtimePageFromRedis(c.Request.Context(), requestedWindowStart, generatedAt+1)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	metrics, successMetrics := channelMonitorRealtimePerformanceMetrics(view)
	groupSuccessMetrics := channelMonitorRealtimeGroupSuccessMetrics(view)
	metadata := channelMonitorRealtimePageMetadata(view)
	metricCoverage := channelMonitorPerformanceMetricCoverageResponse{
		AggregationEnabled: true,
		AggregatedFrom:     metadata.WindowStart,
		AggregatedThrough:  metadata.DataCutoffAt,
		WindowStart:        view.WindowStart,
		WindowComplete:     !metadata.RealtimeDegraded && metadata.WindowStart > 0 && metadata.WindowStart <= view.WindowStart,
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
		"redis_consumer_running":        metadata.RedisConsumerRunning,
		"pending_count":                 metadata.PendingCount,
		"oldest_pending_at":             metadata.OldestPendingAt,
		"consumer_lag_seconds":          metadata.ConsumerLagSeconds,
		"last_published_at":             metadata.LastPublishedAt,
		"last_processed_at":             metadata.LastProcessedAt,
		"retry_count":                   metadata.RetryCount,
		"takeover_count":                metadata.TakeoverCount,
		"marker_release_failure_count":  metadata.MarkerReleaseFailureCount,
		"marker_release_failure_active": metadata.MarkerReleaseFailureActive,
		"stream_trim_failure_count":     metadata.StreamTrimFailureCount,
		"stream_trim_failure_active":    metadata.StreamTrimFailureActive,
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
	requestedWindowStart := generatedAt - int64(minutes*60)
	detailView, err := service.QueryChannelMonitorRealtimeSuccessDetailFromRedis(
		c.Request.Context(),
		requestedWindowStart, generatedAt+1, filter,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	metadata := channelMonitorRealtimeMetadata(detailView.WindowStart)
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
		"redis_consumer_running":        metadata.RedisConsumerRunning,
		"pending_count":                 metadata.PendingCount,
		"oldest_pending_at":             metadata.OldestPendingAt,
		"consumer_lag_seconds":          metadata.ConsumerLagSeconds,
		"last_published_at":             metadata.LastPublishedAt,
		"last_processed_at":             metadata.LastProcessedAt,
		"retry_count":                   metadata.RetryCount,
		"takeover_count":                metadata.TakeoverCount,
		"marker_release_failure_count":  metadata.MarkerReleaseFailureCount,
		"marker_release_failure_active": metadata.MarkerReleaseFailureActive,
		"stream_trim_failure_count":     metadata.StreamTrimFailureCount,
		"stream_trim_failure_active":    metadata.StreamTrimFailureActive,
		"realtime_degraded":             metadata.RealtimeDegraded || metadata.ProjectionStartedAt > detailView.WindowStart,
		"success_metrics_available":     true,
		"scope":                         scope,
		"detail":                        detailView.Detail,
	})
}
