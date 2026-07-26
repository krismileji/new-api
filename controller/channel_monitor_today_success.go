package controller

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

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
	CacheWriteChannelCount int     `json:"cache_write_channel_count"`
	CacheWriteRequestCount int64   `json:"cache_write_request_count"`
}

type channelMonitorTodaySuccessOverview struct {
	Days                       int                                       `json:"days"`
	GeneratedAt                int64                                     `json:"generated_at"`
	DayStart                   int64                                     `json:"day_start"`
	DetailDate                 string                                    `json:"detail_date"`
	SuccessMetricsAvailable    bool                                      `json:"success_metrics_available"`
	CacheWriteMetricsAvailable bool                                      `json:"cache_write_metrics_available"`
	Summary                    model.ChannelMonitorSuccessSummary        `json:"summary"`
	ChannelItems               []channelMonitorTodaySuccessChannel       `json:"channel_items"`
	APIKeyItems                []model.ChannelMonitorSuccessAPIKeyMetric `json:"api_key_items"`
	CacheWriteItems            []channelMonitorTodayCacheWriteChannel    `json:"cache_write_items"`
	ChartItems                 []channelMonitorDailySuccessChartItem     `json:"chart_items"`
}

func GetChannelMonitorTodaySuccess(c *gin.Context) {
	days := 1
	if rawDays := c.Query("days"); rawDays != "" {
		parsedDays, err := strconv.Atoi(rawDays)
		if err != nil || parsedDays < 1 || parsedDays > channelMonitorCostMaxDays {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "统计天数必须在 1 到 90 之间"})
			return
		}
		days = parsedDays
	}

	generatedAt := common.GetTimestamp()
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
	rangeEnd := todayStart + channelMonitorCostDaySeconds
	overview := channelMonitorTodaySuccessOverview{
		Days:                       days,
		GeneratedAt:                generatedAt,
		DayStart:                   detailDayStart,
		DetailDate:                 channelMonitorCostDate(detailDayStart),
		SuccessMetricsAvailable:    common.LogConsumeEnabled && constant.ErrorLogEnabled,
		CacheWriteMetricsAvailable: common.LogConsumeEnabled,
		ChannelItems:               make([]channelMonitorTodaySuccessChannel, 0),
		APIKeyItems:                make([]model.ChannelMonitorSuccessAPIKeyMetric, 0),
		CacheWriteItems:            make([]channelMonitorTodayCacheWriteChannel, 0),
		ChartItems:                 channelMonitorDailySuccessChartItems(rangeStart, days, nil),
	}
	if !overview.SuccessMetricsAvailable && !overview.CacheWriteMetricsAvailable {
		common.ApiSuccess(c, overview)
		return
	}

	metrics, err := model.GetChannelMonitorSuccessMetricsForDayCached(c.Request.Context(), detailDayStart)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	dailyMetrics := []model.ChannelMonitorDailySuccessMetric{{
		DayStart:               detailDayStart,
		Summary:                metrics.Summary,
		CacheWriteChannelCount: len(metrics.CacheWriteItems),
	}}
	for _, item := range metrics.CacheWriteItems {
		dailyMetrics[0].CacheWriteRequestCount += item.RequestCount
	}
	if days > 1 {
		dailyMetrics, err = model.GetChannelMonitorDailySuccessMetricsCached(c.Request.Context(), rangeStart, rangeEnd)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
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
			CacheWriteChannelCount: metric.CacheWriteChannelCount,
			CacheWriteRequestCount: metric.CacheWriteRequestCount,
		})
	}
	return items
}
