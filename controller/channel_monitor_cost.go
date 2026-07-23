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
	channelMonitorCostDefaultDays  = 30
	channelMonitorCostMaxDays      = 90
	channelMonitorCostDatePageSize = 7
	channelMonitorCostDaySeconds   = int64(24 * 60 * 60)
	channelMonitorCostOffset       = int64(8 * 60 * 60)
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

type channelMonitorCostAPIKeyChannel struct {
	ChannelId       int     `json:"channel_id"`
	ChannelName     string  `json:"channel_name"`
	CostCNY         float64 `json:"cost_cny"`
	SettledCount    int64   `json:"settled_count"`
	UnresolvedCount int64   `json:"unresolved_count"`
}

type channelMonitorCostAPIKey struct {
	Id              int64                             `json:"id"`
	APIKeyId        int                               `json:"api_key_id"`
	APIKeyName      string                            `json:"api_key_name"`
	APIKey          string                            `json:"api_key"`
	CostCNY         float64                           `json:"cost_cny"`
	SettledCount    int64                             `json:"settled_count"`
	UnresolvedCount int64                             `json:"unresolved_count"`
	Channels        []channelMonitorCostAPIKeyChannel `json:"channels"`
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
	ChartItems       []channelMonitorCostDay     `json:"chart_items"`
	ItemTotal        int                         `json:"item_total"`
	ItemPage         int                         `json:"item_page"`
	ItemPageSize     int                         `json:"item_page_size"`
	ItemPageCount    int                         `json:"item_page_count"`
	Channels         []channelMonitorCostChannel `json:"channels"`
	APIKeys          []channelMonitorCostAPIKey  `json:"api_keys"`
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
	channelId := 0
	if rawChannelId := c.Query("channel_id"); rawChannelId != "" {
		parsedChannelId, err := strconv.Atoi(rawChannelId)
		if err != nil || parsedChannelId <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道 ID 必须为正整数"})
			return
		}
		channelId = parsedChannelId
	}
	page := 1
	if rawPage := c.Query("page"); rawPage != "" {
		parsedPage, err := strconv.Atoi(rawPage)
		if err != nil || parsedPage <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "页码必须为正整数"})
			return
		}
		page = parsedPage
	}

	var overview channelMonitorCostOverview
	var err error
	if channelId > 0 {
		overview, err = getChannelMonitorCostOverviewForChannelPage(c.Request.Context(), days, common.GetTimestamp(), channelId, page, channelMonitorCostDatePageSize)
	} else {
		overview, err = getChannelMonitorCostOverviewPage(c.Request.Context(), days, common.GetTimestamp(), page, channelMonitorCostDatePageSize)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}

func getChannelMonitorCostOverview(ctx context.Context, days int, now int64) (channelMonitorCostOverview, error) {
	return getChannelMonitorCostOverviewPage(ctx, days, now, 1, days)
}

func getChannelMonitorCostOverviewForChannel(ctx context.Context, days int, now int64, channelId int) (channelMonitorCostOverview, error) {
	return getChannelMonitorCostOverviewForChannelPage(ctx, days, now, channelId, 1, days)
}

func getChannelMonitorCostOverviewPage(ctx context.Context, days int, now int64, page int, pageSize int) (channelMonitorCostOverview, error) {
	return getChannelMonitorCostOverviewForChannelPage(ctx, days, now, 0, page, pageSize)
}

func getChannelMonitorCostOverviewForChannelPage(ctx context.Context, days int, now int64, channelId int, page int, pageSize int) (channelMonitorCostOverview, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = channelMonitorCostDatePageSize
	}
	todayStart := channelMonitorCostDayStart(now)
	startTimestamp := todayStart - int64(days-1)*channelMonitorCostDaySeconds
	endTimestamp := todayStart + channelMonitorCostDaySeconds

	rows, err := model.GetChannelDailyCostsForChannel(ctx, startTimestamp, endTimestamp, channelId)
	if err != nil {
		return channelMonitorCostOverview{}, err
	}
	apiKeyRows, err := model.GetChannelDailyAPIKeyCostsForChannel(ctx, startTimestamp, endTimestamp, channelId)
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
		CostNanoCNY     int64
		CostCNY         float64
		SettledCount    int64
		UnresolvedCount int64
	}
	channelCosts := make(map[int]*channelCostSummary)
	includedChannels := make(map[int]struct{})
	unresolvedChannels := make(map[int]struct{})
	for _, row := range rows {
		costCNY := channelMonitorCostCNY(row.CostNanoCNY)
		summary := channelCosts[row.ChannelId]
		if summary == nil {
			summary = &channelCostSummary{}
			channelCosts[row.ChannelId] = summary
		}
		summary.CostNanoCNY += row.CostNanoCNY
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

	chartRows, err := model.GetChannelDailyCostDayTotals(ctx, startTimestamp, endTimestamp, channelId)
	if err != nil {
		return channelMonitorCostOverview{}, err
	}
	chartItems := channelMonitorCostDaysFromTotals(startTimestamp, endTimestamp, chartRows)
	totalCostCNY := 0.0
	for _, item := range chartItems {
		totalCostCNY += item.CostCNY
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

	type apiKeyCostKey struct {
		APIKeyId       int
		KeyFingerprint string
	}
	type apiKeyChannelSummary struct {
		ChannelName     string
		CostCNY         float64
		SettledCount    int64
		UnresolvedCount int64
	}
	type apiKeyCostSummary struct {
		Id              int64
		APIKeyId        int
		APIKeyName      string
		KeyDisplay      string
		CostCNY         float64
		SettledCount    int64
		UnresolvedCount int64
		Channels        map[int]*apiKeyChannelSummary
	}
	type apiKeyChannelTotal struct {
		CostNanoCNY     int64
		SettledCount    int64
		UnresolvedCount int64
	}
	apiKeyCosts := make(map[apiKeyCostKey]*apiKeyCostSummary)
	apiKeyChannelTotals := make(map[int]*apiKeyChannelTotal)
	for _, row := range apiKeyRows {
		key := apiKeyCostKey{APIKeyId: row.APIKeyId}
		if row.APIKeyId == 0 {
			key.KeyFingerprint = row.KeyFingerprint
		}
		summary := apiKeyCosts[key]
		if summary == nil {
			summary = &apiKeyCostSummary{
				Id:         row.Id,
				APIKeyId:   row.APIKeyId,
				APIKeyName: row.APIKeyName,
				KeyDisplay: row.KeyDisplay,
				Channels:   make(map[int]*apiKeyChannelSummary),
			}
			apiKeyCosts[key] = summary
		}
		if row.APIKeyName != "" {
			summary.APIKeyName = row.APIKeyName
		}
		summary.KeyDisplay = row.KeyDisplay
		summary.CostCNY += channelMonitorCostCNY(row.CostNanoCNY)
		summary.SettledCount += row.SettledCount
		summary.UnresolvedCount += row.UnresolvedCount
		channelSummary := summary.Channels[row.ChannelId]
		if channelSummary == nil {
			channelName := channelNames[row.ChannelId]
			if channelName == "" {
				channelName = "已删除渠道"
			}
			channelSummary = &apiKeyChannelSummary{ChannelName: channelName}
			summary.Channels[row.ChannelId] = channelSummary
		}
		channelSummary.CostCNY += channelMonitorCostCNY(row.CostNanoCNY)
		channelSummary.SettledCount += row.SettledCount
		channelSummary.UnresolvedCount += row.UnresolvedCount
		channelTotal := apiKeyChannelTotals[row.ChannelId]
		if channelTotal == nil {
			channelTotal = &apiKeyChannelTotal{}
			apiKeyChannelTotals[row.ChannelId] = channelTotal
		}
		channelTotal.CostNanoCNY += row.CostNanoCNY
		channelTotal.SettledCount += row.SettledCount
		channelTotal.UnresolvedCount += row.UnresolvedCount
	}

	// Older daily totals and admin channel-test requests may not have an
	// inbound API Key. Keep their cost visible instead of silently dropping the
	// channel from the API Key view.
	unattributedKey := apiKeyCostKey{APIKeyId: 0, KeyFingerprint: "__unattributed__"}
	for channelId, channelSummary := range channelCosts {
		attributed := apiKeyChannelTotals[channelId]
		var attributedCost, attributedSettled, attributedUnresolved int64
		if attributed != nil {
			attributedCost = attributed.CostNanoCNY
			attributedSettled = attributed.SettledCount
			attributedUnresolved = attributed.UnresolvedCount
		}
		unattributedCost := channelSummary.CostNanoCNY - attributedCost
		unattributedSettled := channelSummary.SettledCount - attributedSettled
		unattributedUnresolved := channelSummary.UnresolvedCount - attributedUnresolved
		if unattributedCost <= 0 && unattributedSettled <= 0 && unattributedUnresolved <= 0 {
			continue
		}
		if unattributedCost < 0 {
			unattributedCost = 0
		}
		if unattributedSettled < 0 {
			unattributedSettled = 0
		}
		if unattributedUnresolved < 0 {
			unattributedUnresolved = 0
		}
		summary := apiKeyCosts[unattributedKey]
		if summary == nil {
			summary = &apiKeyCostSummary{
				APIKeyName: "未识别 API Key",
				Channels:   make(map[int]*apiKeyChannelSummary),
			}
			apiKeyCosts[unattributedKey] = summary
		}
		summary.CostCNY += channelMonitorCostCNY(unattributedCost)
		summary.SettledCount += unattributedSettled
		summary.UnresolvedCount += unattributedUnresolved
		channelName := channelNames[channelId]
		if channelName == "" {
			channelName = "已删除渠道"
		}
		channelDetail := summary.Channels[channelId]
		if channelDetail == nil {
			channelDetail = &apiKeyChannelSummary{ChannelName: channelName}
			summary.Channels[channelId] = channelDetail
		}
		channelDetail.CostCNY += channelMonitorCostCNY(unattributedCost)
		channelDetail.SettledCount += unattributedSettled
		channelDetail.UnresolvedCount += unattributedUnresolved
	}

	costAPIKeys := make([]channelMonitorCostAPIKey, 0, len(apiKeyCosts))
	for _, summary := range apiKeyCosts {
		apiKeyName := summary.APIKeyName
		if apiKeyName == "" {
			switch {
			case summary.APIKeyId > 0:
				apiKeyName = "未命名 API Key #" + strconv.Itoa(summary.APIKeyId)
			case summary.KeyDisplay != "":
				apiKeyName = "上游 Key " + summary.KeyDisplay
			default:
				apiKeyName = "未识别 API Key"
			}
		}
		channelsByCost := make([]channelMonitorCostAPIKeyChannel, 0, len(summary.Channels))
		for channelId, channelSummary := range summary.Channels {
			channelsByCost = append(channelsByCost, channelMonitorCostAPIKeyChannel{
				ChannelId:       channelId,
				ChannelName:     channelSummary.ChannelName,
				CostCNY:         channelSummary.CostCNY,
				SettledCount:    channelSummary.SettledCount,
				UnresolvedCount: channelSummary.UnresolvedCount,
			})
		}
		sort.Slice(channelsByCost, func(i int, j int) bool {
			if channelsByCost[i].CostCNY != channelsByCost[j].CostCNY {
				return channelsByCost[i].CostCNY > channelsByCost[j].CostCNY
			}
			return channelsByCost[i].ChannelId < channelsByCost[j].ChannelId
		})
		costAPIKeys = append(costAPIKeys, channelMonitorCostAPIKey{
			Id:              summary.Id,
			APIKeyId:        summary.APIKeyId,
			APIKeyName:      apiKeyName,
			APIKey:          summary.KeyDisplay,
			CostCNY:         summary.CostCNY,
			SettledCount:    summary.SettledCount,
			UnresolvedCount: summary.UnresolvedCount,
			Channels:        channelsByCost,
		})
	}
	sort.Slice(costAPIKeys, func(i int, j int) bool {
		if costAPIKeys[i].CostCNY != costAPIKeys[j].CostCNY {
			return costAPIKeys[i].CostCNY > costAPIKeys[j].CostCNY
		}
		if costAPIKeys[i].Id != costAPIKeys[j].Id {
			return costAPIKeys[i].Id < costAPIKeys[j].Id
		}
		return costAPIKeys[i].APIKeyName < costAPIKeys[j].APIKeyName
	})

	itemTotal := days
	itemPageCount := (itemTotal + pageSize - 1) / pageSize
	if itemPageCount == 0 {
		itemPageCount = 1
	}
	if page > itemPageCount {
		page = itemPageCount
	}
	pageOffset := (page - 1) * pageSize
	pageItemCount := itemTotal - pageOffset
	if pageItemCount > pageSize {
		pageItemCount = pageSize
	}
	pageEndTimestamp := endTimestamp - int64(pageOffset)*channelMonitorCostDaySeconds
	pageStartTimestamp := pageEndTimestamp - int64(pageItemCount)*channelMonitorCostDaySeconds
	pageRows, err := model.GetChannelDailyCostDayTotalsPage(ctx, pageStartTimestamp, pageEndTimestamp, channelId, pageItemCount)
	if err != nil {
		return channelMonitorCostOverview{}, err
	}
	items := channelMonitorCostDaysFromTotals(pageStartTimestamp, pageEndTimestamp, pageRows)

	overview := channelMonitorCostOverview{
		Days:         days,
		GeneratedAt:  now,
		TotalCostCNY: totalCostCNY,
		Coverage: channelMonitorCostCoverage{
			IncludedChannelCount:   len(includedChannels),
			UnresolvedChannelCount: len(unresolvedChannels),
		},
		Items:         items,
		ChartItems:    chartItems,
		ItemTotal:     itemTotal,
		ItemPage:      page,
		ItemPageSize:  pageSize,
		ItemPageCount: itemPageCount,
		Channels:      costChannels,
		APIKeys:       costAPIKeys,
	}
	if len(chartItems) > 0 {
		overview.TodayCostCNY = chartItems[len(chartItems)-1].CostCNY
	}
	if len(chartItems) > 1 {
		overview.YesterdayCostCNY = chartItems[len(chartItems)-2].CostCNY
	}
	return overview, nil
}

func channelMonitorCostDaysFromTotals(startTimestamp int64, endTimestamp int64, rows []model.ChannelDailyCostDayTotal) []channelMonitorCostDay {
	dailyCosts := make(map[int64]channelMonitorCostDay, len(rows))
	for _, row := range rows {
		dailyCosts[row.DayStart] = channelMonitorCostDay{
			Date:            channelMonitorCostDate(row.DayStart),
			StartAt:         row.DayStart,
			CostCNY:         channelMonitorCostCNY(row.CostNanoCNY),
			UnresolvedCount: row.UnresolvedCount,
		}
	}

	items := make([]channelMonitorCostDay, 0, (endTimestamp-startTimestamp)/channelMonitorCostDaySeconds)
	for dayStart := startTimestamp; dayStart < endTimestamp; dayStart += channelMonitorCostDaySeconds {
		item, exists := dailyCosts[dayStart]
		if !exists {
			item = channelMonitorCostDay{
				Date:    channelMonitorCostDate(dayStart),
				StartAt: dayStart,
			}
		}
		items = append(items, item)
	}
	return items
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
