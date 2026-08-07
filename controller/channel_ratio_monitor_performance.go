package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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
	if err := service.EnsureChannelMonitorAggregationFresh(c.Request.Context(), requestedAt); err != nil {
		common.ApiError(c, err)
		return
	}
	generatedAt := requestedAt.Unix()
	metricCoverage, err := channelMonitorPerformanceMetricCoverage(
		c.Request.Context(), generatedAt, minutes,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	metrics, err := model.GetChannelMonitorPerformanceMetricsCached(
		c.Request.Context(),
		generatedAt,
		minutes,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	successMetricsAvailable := common.LogConsumeEnabled && constant.ErrorLogEnabled
	successMetrics := make([]model.ChannelMonitorSuccessMetric, 0)
	groupSuccessMetrics := make([]model.ChannelMonitorGroupSuccessMetric, 0)
	if successMetricsAvailable {
		successMetrics, groupSuccessMetrics, err = model.GetChannelMonitorSuccessMetricsCached(
			c.Request.Context(),
			generatedAt,
			minutes,
		)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	common.ApiSuccess(c, gin.H{
		"range_minutes":             minutes,
		"range_source":              rangeSource,
		"generated_at":              generatedAt,
		"metric_coverage":           metricCoverage,
		"items":                     metrics,
		"success_metrics_available": successMetricsAvailable,
		"success_items":             successMetrics,
		"group_success_items":       groupSuccessMetrics,
	})
}

func GetChannelMonitorSuccessDetail(c *gin.Context) {
	minutes, ok := getChannelMonitorPerformanceMinutes(c)
	if !ok {
		return
	}

	requestedAt := time.Now()
	generatedAt := requestedAt.Unix()
	successMetricsAvailable := common.LogConsumeEnabled && constant.ErrorLogEnabled
	if !successMetricsAvailable {
		common.ApiSuccess(c, gin.H{
			"range_minutes":             minutes,
			"generated_at":              generatedAt,
			"success_metrics_available": false,
			"scope":                     "",
			"detail": model.ChannelMonitorSuccessDetail{
				ChannelItems:      make([]model.ChannelMonitorChannelSuccessMetric, 0),
				APIKeyItems:       make([]model.ChannelMonitorSuccessAPIKeyMetric, 0),
				FailureCategories: make([]model.ChannelMonitorFailureCategory, 0),
			},
		})
		return
	}

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
	if err := service.EnsureChannelMonitorAggregationFresh(c.Request.Context(), requestedAt); err != nil {
		common.ApiError(c, err)
		return
	}

	detail, err := model.GetChannelMonitorSuccessDetail(
		c.Request.Context(),
		generatedAt-int64(minutes*60),
		filter,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"range_minutes":             minutes,
		"generated_at":              generatedAt,
		"success_metrics_available": true,
		"scope":                     scope,
		"detail":                    detail,
	})
}
