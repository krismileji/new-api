package controller

import (
	"sort"
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

type channelMonitorTodaySuccessOverview struct {
	GeneratedAt                int64                                     `json:"generated_at"`
	DayStart                   int64                                     `json:"day_start"`
	SuccessMetricsAvailable    bool                                      `json:"success_metrics_available"`
	CacheWriteMetricsAvailable bool                                      `json:"cache_write_metrics_available"`
	Summary                    model.ChannelMonitorSuccessSummary        `json:"summary"`
	ChannelItems               []channelMonitorTodaySuccessChannel       `json:"channel_items"`
	APIKeyItems                []model.ChannelMonitorSuccessAPIKeyMetric `json:"api_key_items"`
	CacheWriteItems            []channelMonitorTodayCacheWriteChannel    `json:"cache_write_items"`
}

func GetChannelMonitorTodaySuccess(c *gin.Context) {
	generatedAt := common.GetTimestamp()
	overview := channelMonitorTodaySuccessOverview{
		GeneratedAt:                generatedAt,
		DayStart:                   model.ChannelDailyCostDayStart(generatedAt),
		SuccessMetricsAvailable:    common.LogConsumeEnabled && constant.ErrorLogEnabled,
		CacheWriteMetricsAvailable: common.LogConsumeEnabled,
		ChannelItems:               make([]channelMonitorTodaySuccessChannel, 0),
		APIKeyItems:                make([]model.ChannelMonitorSuccessAPIKeyMetric, 0),
		CacheWriteItems:            make([]channelMonitorTodayCacheWriteChannel, 0),
	}
	if !overview.SuccessMetricsAvailable && !overview.CacheWriteMetricsAvailable {
		common.ApiSuccess(c, overview)
		return
	}

	metrics, err := model.GetChannelMonitorTodaySuccessMetricsCached(c.Request.Context(), generatedAt)
	if err != nil {
		common.ApiError(c, err)
		return
	}
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
