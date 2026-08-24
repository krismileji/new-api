package controller

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type channelMonitorTodaySuccessChannel struct {
	ChannelName   string `json:"channel_name"`
	ChannelRemark string `json:"channel_remark"`
	model.ChannelMonitorChannelSuccessMetric
}

type channelMonitorTodayCacheWriteChannel struct {
	ChannelName   string `json:"channel_name"`
	ChannelRemark string `json:"channel_remark"`
	model.ChannelMonitorTodayCacheWriteMetric
}

type channelMonitorDailySuccessChartItem struct {
	Date                   string  `json:"date"`
	StartAt                int64   `json:"start_at"`
	RequestCount           int64   `json:"request_count"`
	SuccessRate            float64 `json:"success_rate"`
	CacheSampleCount       int64   `json:"cache_sample_count"`
	CacheRate              float64 `json:"cache_rate"`
	CacheReadTokens        int64   `json:"cache_read_tokens"`
	InputTokens            int64   `json:"input_tokens"`
	CacheUtilizationRate   float64 `json:"cache_utilization_rate"`
	CacheWriteChannelCount int     `json:"cache_write_channel_count"`
	CacheWriteRequestCount int64   `json:"cache_write_request_count"`
}

type channelMonitorTodaySuccessOverview struct {
	Days                       int                                                    `json:"days"`
	GeneratedAt                int64                                                  `json:"generated_at"`
	DataCutoffAt               int64                                                  `json:"data_cutoff_at"`
	ProcessedAt                int64                                                  `json:"processed_at"`
	ProjectionStartedAt        int64                                                  `json:"projection_started_at"`
	EventWatermark             uint64                                                 `json:"event_watermark"`
	QueueDepth                 int                                                    `json:"queue_depth"`
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
	RealtimeDegraded           bool                                                   `json:"realtime_degraded"`
	DayStart                   int64                                                  `json:"day_start"`
	DetailDate                 string                                                 `json:"detail_date"`
	SuccessMetricsAvailable    bool                                                   `json:"success_metrics_available"`
	CacheWriteMetricsAvailable bool                                                   `json:"cache_write_metrics_available"`
	Summary                    model.ChannelMonitorSuccessSummary                     `json:"summary"`
	ChannelItems               []channelMonitorTodaySuccessChannel                    `json:"channel_items"`
	APIKeyItems                []model.ChannelMonitorSuccessAPIKeyMetric              `json:"api_key_items"`
	CacheWriteItems            []channelMonitorTodayCacheWriteChannel                 `json:"cache_write_items"`
	ChartItems                 []channelMonitorDailySuccessChartItem                  `json:"chart_items"`
}

func GetChannelMonitorTodaySuccess(c *gin.Context) {
	if serveChannelMonitorPageSnapshot(c, channelMonitorPageSnapshotSuccess, GetChannelMonitorTodaySuccess) {
		return
	}
	days := 1
	if rawDays := c.Query("days"); rawDays != "" {
		parsedDays, err := strconv.Atoi(rawDays)
		if err != nil || parsedDays < 1 || parsedDays > channelMonitorCostMaxDays {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "统计天数必须在 1 到 90 之间"})
			return
		}
		days = parsedDays
	}

	requestedAt := time.Now()
	generatedAt := requestedAt.Unix()
	todayStart := model.ChannelDailyCostDayStart(generatedAt)
	detailDayStart := todayStart
	if rawDetailDate := c.Query("date"); rawDetailDate != "" {
		parsedDayStart, parseErr := channelMonitorCostDateStart(rawDetailDate)
		rangeStart := todayStart - int64(days-1)*channelMonitorCostDaySeconds
		if parseErr != nil || parsedDayStart < rangeStart || parsedDayStart > todayStart {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "统计日期必须在所选时间范围内"})
			return
		}
		detailDayStart = parsedDayStart
	}
	rangeStart := todayStart - int64(days-1)*channelMonitorCostDaySeconds
	overview := channelMonitorTodaySuccessOverview{
		Days:                       days,
		GeneratedAt:                generatedAt,
		DayStart:                   detailDayStart,
		DetailDate:                 channelMonitorCostDate(detailDayStart),
		SuccessMetricsAvailable:    true,
		CacheWriteMetricsAvailable: true,
		ChannelItems:               make([]channelMonitorTodaySuccessChannel, 0),
		APIKeyItems:                make([]model.ChannelMonitorSuccessAPIKeyMetric, 0),
		CacheWriteItems:            make([]channelMonitorTodayCacheWriteChannel, 0),
		ChartItems:                 channelMonitorDailySuccessChartItems(rangeStart, days, nil),
	}
	todayView, err := service.QueryChannelMonitorRealtimePageFromRedis(c.Request.Context(), todayStart, todayStart+channelMonitorCostDaySeconds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	metadata := channelMonitorRealtimePageMetadata(todayView)
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

	todayMetrics := channelMonitorRealtimeTodaySuccessMetrics(todayView)
	metrics := todayMetrics
	if detailDayStart != todayStart {
		metrics, err = model.GetChannelMonitorSuccessMetricsForDayCached(c.Request.Context(), detailDayStart)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	dailyMetrics := make([]model.ChannelMonitorDailySuccessMetric, 0, days)
	if days > 1 {
		dailyMetrics, err = model.GetChannelMonitorDailySuccessMetricsCached(c.Request.Context(), rangeStart, todayStart)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	todayDailyMetric := model.ChannelMonitorDailySuccessMetric{
		DayStart:               todayStart,
		Summary:                todayMetrics.Summary,
		CacheWriteChannelCount: len(todayMetrics.CacheWriteItems),
	}
	for _, item := range todayMetrics.CacheWriteItems {
		todayDailyMetric.CacheWriteRequestCount += item.RequestCount
	}
	dailyMetrics = append(dailyMetrics, todayDailyMetric)
	overview.ChartItems = channelMonitorDailySuccessChartItems(rangeStart, days, dailyMetrics)
	channels, err := model.GetAllChannelsForMonitor()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	itemsByChannelId := make(map[int]channelMonitorTodaySuccessChannel, len(channels)+len(metrics.ChannelItems))
	for _, channel := range channels {
		remark := ""
		if channel.Remark != nil {
			remark = strings.TrimSpace(*channel.Remark)
		}
		itemsByChannelId[channel.Id] = channelMonitorTodaySuccessChannel{
			ChannelName:   channel.Name,
			ChannelRemark: remark,
			ChannelMonitorChannelSuccessMetric: model.ChannelMonitorChannelSuccessMetric{
				ChannelId: channel.Id,
			},
		}
	}

	overview.Summary = metrics.Summary
	overview.APIKeyItems = metrics.APIKeyItems
	for _, item := range metrics.ChannelItems {
		channelItem := itemsByChannelId[item.ChannelId]
		channelItem.ChannelMonitorChannelSuccessMetric = item
		itemsByChannelId[item.ChannelId] = channelItem
	}

	channelIds := make([]int, 0, len(itemsByChannelId))
	for channelId := range itemsByChannelId {
		channelIds = append(channelIds, channelId)
	}
	sort.Ints(channelIds)
	overview.ChannelItems = make([]channelMonitorTodaySuccessChannel, 0, len(channelIds))
	for _, channelId := range channelIds {
		overview.ChannelItems = append(overview.ChannelItems, itemsByChannelId[channelId])
	}
	for _, item := range metrics.CacheWriteItems {
		channelItem := itemsByChannelId[item.ChannelId]
		overview.CacheWriteItems = append(overview.CacheWriteItems, channelMonitorTodayCacheWriteChannel{
			ChannelName:                         channelItem.ChannelName,
			ChannelRemark:                       channelItem.ChannelRemark,
			ChannelMonitorTodayCacheWriteMetric: item,
		})
	}
	common.ApiSuccess(c, overview)
}

func channelMonitorDailySuccessChartItems(startTimestamp int64, days int, metrics []model.ChannelMonitorDailySuccessMetric) []channelMonitorDailySuccessChartItem {
	metricsByDay := make(map[int64]model.ChannelMonitorDailySuccessMetric, len(metrics))
	for _, metric := range metrics {
		metricsByDay[metric.DayStart] = metric
	}
	items := make([]channelMonitorDailySuccessChartItem, 0, days)
	for index := 0; index < days; index++ {
		dayStart := startTimestamp + int64(index)*channelMonitorCostDaySeconds
		metric := metricsByDay[dayStart]
		items = append(items, channelMonitorDailySuccessChartItem{
			Date:                   channelMonitorCostDate(dayStart),
			StartAt:                dayStart,
			RequestCount:           metric.Summary.ActualSampleCount,
			SuccessRate:            metric.Summary.ActualSuccessRate,
			CacheSampleCount:       metric.Summary.CacheSampleCount,
			CacheRate:              metric.Summary.CacheHitRate,
			CacheReadTokens:        metric.Summary.CacheReadTokens,
			InputTokens:            metric.Summary.InputTokens,
			CacheUtilizationRate:   metric.Summary.CacheUtilization,
			CacheWriteChannelCount: metric.CacheWriteChannelCount,
			CacheWriteRequestCount: metric.CacheWriteRequestCount,
		})
	}
	return items
}
