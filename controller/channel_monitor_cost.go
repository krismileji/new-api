package controller

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const (
	channelMonitorCostDefaultDays = 30
	channelMonitorCostMaxDays     = 90
	channelMonitorCostDaySeconds  = int64(24 * 60 * 60)
	channelMonitorCostOffset      = int64(8 * 60 * 60)
)

type channelMonitorCostDay struct {
	Date            string  `json:"date"`
	StartAt         int64   `json:"start_at"`
	CostCNY         float64 `json:"cost_cny"`
	UnresolvedCount int64   `json:"unresolved_count"`
}

type channelMonitorCostChannel struct {
	ChannelId       int     `json:"channel_id"`
	ChannelName     string  `json:"channel_name"`
	CostCNY         float64 `json:"cost_cny"`
	SettledCount    int64   `json:"settled_count"`
	UnresolvedCount int64   `json:"unresolved_count"`
}

type channelMonitorCostCoverage struct {
	IncludedChannelCount   int `json:"included_channel_count"`
	UnresolvedChannelCount int `json:"unresolved_channel_count"`
	FreeGroupChannelCount  int `json:"free_group_channel_count"`
}

type channelMonitorCostOverview struct {
	Days             int                         `json:"days"`
	GeneratedAt      int64                       `json:"generated_at"`
	TodayCostCNY     float64                     `json:"today_cost_cny"`
	YesterdayCostCNY float64                     `json:"yesterday_cost_cny"`
	TotalCostCNY     float64                     `json:"total_cost_cny"`
	Coverage         channelMonitorCostCoverage  `json:"coverage"`
	Items            []channelMonitorCostDay     `json:"items"`
	Channels         []channelMonitorCostChannel `json:"channels"`
}

func GetChannelMonitorCostOverview(c *gin.Context) {
	days := channelMonitorCostDefaultDays
	if rawDays := c.Query("days"); rawDays != "" {
		parsedDays, err := strconv.Atoi(rawDays)
		if err != nil || parsedDays < 1 || parsedDays > channelMonitorCostMaxDays {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "统计天数必须在 1 到 90 之间"})
			return
		}
		days = parsedDays
	}

	overview, err := getChannelMonitorCostOverview(c.Request.Context(), days, common.GetTimestamp())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}

func getChannelMonitorCostOverview(ctx context.Context, days int, now int64) (channelMonitorCostOverview, error) {
	todayStart := channelMonitorCostDayStart(now)
	startTimestamp := todayStart - int64(days-1)*channelMonitorCostDaySeconds
	endTimestamp := todayStart + channelMonitorCostDaySeconds

	rows, err := model.GetChannelDailyCosts(ctx, startTimestamp, endTimestamp)
	if err != nil {
		return channelMonitorCostOverview{}, err
	}
	channels, err := model.GetAllChannelsForMonitor()
	if err != nil {
		return channelMonitorCostOverview{}, err
	}

	channelNames := make(map[int]string, len(channels))
	for _, channel := range channels {
		channelNames[channel.Id] = channel.Name
	}

	type channelCostSummary struct {
		CostCNY         float64
		SettledCount    int64
		UnresolvedCount int64
	}
	dailyCosts := make(map[int64]float64, days)
	dailyUnresolved := make(map[int64]int64, days)
	channelCosts := make(map[int]*channelCostSummary)
	includedChannels := make(map[int]struct{})
	unresolvedChannels := make(map[int]struct{})
	for _, row := range rows {
		costCNY := channelMonitorCostCNY(row.CostNanoCNY)
		dailyCosts[row.DayStart] += costCNY
		dailyUnresolved[row.DayStart] += row.UnresolvedCount
		summary := channelCosts[row.ChannelId]
		if summary == nil {
			summary = &channelCostSummary{}
			channelCosts[row.ChannelId] = summary
		}
		summary.CostCNY += costCNY
		summary.SettledCount += row.SettledCount
		summary.UnresolvedCount += row.UnresolvedCount
		if row.SettledCount > 0 {
			includedChannels[row.ChannelId] = struct{}{}
		}
		if row.UnresolvedCount > 0 {
			unresolvedChannels[row.ChannelId] = struct{}{}
		}
	}

	items := make([]channelMonitorCostDay, 0, days)
	totalCostCNY := 0.0
	for dayStart := startTimestamp; dayStart < endTimestamp; dayStart += channelMonitorCostDaySeconds {
		costCNY := dailyCosts[dayStart]
		items = append(items, channelMonitorCostDay{
			Date:            channelMonitorCostDate(dayStart),
			StartAt:         dayStart,
			CostCNY:         costCNY,
			UnresolvedCount: dailyUnresolved[dayStart],
		})
		totalCostCNY += costCNY
	}

	costChannels := make([]channelMonitorCostChannel, 0, len(channelCosts))
	for channelId, summary := range channelCosts {
		channelName := channelNames[channelId]
		if channelName == "" {
			channelName = "已删除渠道"
		}
		costChannels = append(costChannels, channelMonitorCostChannel{
			ChannelId:       channelId,
			ChannelName:     channelName,
			CostCNY:         summary.CostCNY,
			SettledCount:    summary.SettledCount,
			UnresolvedCount: summary.UnresolvedCount,
		})
	}
	sort.Slice(costChannels, func(i int, j int) bool {
		if costChannels[i].CostCNY == costChannels[j].CostCNY {
			return costChannels[i].ChannelId < costChannels[j].ChannelId
		}
		return costChannels[i].CostCNY > costChannels[j].CostCNY
	})

	overview := channelMonitorCostOverview{
		Days:         days,
		GeneratedAt:  now,
		TotalCostCNY: totalCostCNY,
		Coverage: channelMonitorCostCoverage{
			IncludedChannelCount:   len(includedChannels),
			UnresolvedChannelCount: len(unresolvedChannels),
		},
		Items:    items,
		Channels: costChannels,
	}
	if len(items) > 0 {
		overview.TodayCostCNY = items[len(items)-1].CostCNY
	}
	if len(items) > 1 {
		overview.YesterdayCostCNY = items[len(items)-2].CostCNY
	}
	return overview, nil
}

func channelMonitorCostDayStart(timestamp int64) int64 {
	return model.ChannelDailyCostDayStart(timestamp)
}

func channelMonitorCostDate(dayStart int64) string {
	return time.Unix(dayStart+channelMonitorCostOffset, 0).UTC().Format("2006-01-02")
}

func channelMonitorCostCNY(costNanoCNY int64) float64 {
	return float64(costNanoCNY) / float64(model.ChannelDailyCostNanoPerCNY)
}
