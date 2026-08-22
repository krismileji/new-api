package controller

import (
	"context"
	"errors"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
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
	Date                  string  `json:"date"`
	StartAt               int64   `json:"start_at"`
	CostCNY               float64 `json:"cost_cny"`
	ProbeCostCNY          float64 `json:"probe_cost_cny"`
	GroupProbeCostCNY     float64 `json:"group_probe_cost_cny"`
	ModelDetectionCostCNY float64 `json:"model_detection_cost_cny"`
	SettledCount          int64   `json:"settled_count"`
	UnresolvedCount       int64   `json:"unresolved_count"`
}

type channelMonitorCostChannel struct {
	ChannelId             int      `json:"channel_id"`
	ChannelName           string   `json:"channel_name"`
	ChannelRemark         string   `json:"channel_remark"`
	Status                int      `json:"status"`
	CostRatio             *float64 `json:"cost_ratio"`
	CostCNY               float64  `json:"cost_cny"`
	ProbeCostCNY          float64  `json:"probe_cost_cny"`
	GroupProbeCostCNY     float64  `json:"group_probe_cost_cny"`
	ModelDetectionCostCNY float64  `json:"model_detection_cost_cny"`
	SettledCount          int64    `json:"settled_count"`
	UnresolvedCount       int64    `json:"unresolved_count"`
}

type channelMonitorCostAPIKeyChannel struct {
	ChannelId       int     `json:"channel_id"`
	ChannelName     string  `json:"channel_name"`
	ChannelRemark   string  `json:"channel_remark"`
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
	IncludedChannelCount          int   `json:"included_channel_count"`
	UnresolvedChannelCount        int   `json:"unresolved_channel_count"`
	MissingCostConfigChannelCount int   `json:"missing_cost_config_channel_count"`
	FreeGroupChannelCount         int   `json:"free_group_channel_count"`
	SettledCount                  int64 `json:"settled_count"`
	UnresolvedCount               int64 `json:"unresolved_count"`
}

type channelMonitorCostOverview struct {
	Days                           int                         `json:"days"`
	GeneratedAt                    int64                       `json:"generated_at"`
	DataCutoffAt                   int64                       `json:"data_cutoff_at"`
	ProcessedAt                    int64                       `json:"processed_at"`
	ProjectionStartedAt            int64                       `json:"projection_started_at"`
	EventWatermark                 uint64                      `json:"event_watermark"`
	QueueDepth                     int                         `json:"queue_depth"`
	RedisStatus                    string                      `json:"redis_status"`
	RedisAvailable                 bool                        `json:"redis_available"`
	RedisConsumerRunning           bool                        `json:"redis_consumer_running"`
	PendingCount                   int64                       `json:"pending_count"`
	OldestPendingAt                int64                       `json:"oldest_pending_at"`
	ConsumerLagSeconds             int64                       `json:"consumer_lag_seconds"`
	LastPublishedAt                int64                       `json:"last_published_at"`
	LastProcessedAt                int64                       `json:"last_processed_at"`
	RetryCount                     int64                       `json:"retry_count"`
	TakeoverCount                  int64                       `json:"takeover_count"`
	CostQueuePendingCount          int                         `json:"cost_queue_pending_count"`
	MarkerReleaseFailureCount      int64                       `json:"marker_release_failure_count"`
	MarkerReleaseFailureActive     bool                        `json:"marker_release_failure_active"`
	StreamTrimFailureCount         int64                       `json:"stream_trim_failure_count"`
	StreamTrimFailureActive        bool                        `json:"stream_trim_failure_active"`
	RealtimeDegraded               bool                        `json:"realtime_degraded"`
	DetailDate                     string                      `json:"detail_date"`
	TodayCostCNY                   float64                     `json:"today_cost_cny"`
	TodayProbeCostCNY              float64                     `json:"today_probe_cost_cny"`
	TodayGroupProbeCostCNY         float64                     `json:"today_group_probe_cost_cny"`
	TodayModelDetectionCostCNY     float64                     `json:"today_model_detection_cost_cny"`
	YesterdayCostCNY               float64                     `json:"yesterday_cost_cny"`
	YesterdayProbeCostCNY          float64                     `json:"yesterday_probe_cost_cny"`
	YesterdayGroupProbeCostCNY     float64                     `json:"yesterday_group_probe_cost_cny"`
	YesterdayModelDetectionCostCNY float64                     `json:"yesterday_model_detection_cost_cny"`
	TotalCostCNY                   float64                     `json:"total_cost_cny"`
	TotalProbeCostCNY              float64                     `json:"total_probe_cost_cny"`
	TotalGroupProbeCostCNY         float64                     `json:"total_group_probe_cost_cny"`
	TotalModelDetectionCostCNY     float64                     `json:"total_model_detection_cost_cny"`
	Coverage                       channelMonitorCostCoverage  `json:"coverage"`
	Items                          []channelMonitorCostDay     `json:"items"`
	ChartItems                     []channelMonitorCostDay     `json:"chart_items"`
	ItemTotal                      int                         `json:"item_total"`
	ItemPage                       int                         `json:"item_page"`
	ItemPageSize                   int                         `json:"item_page_size"`
	ItemPageCount                  int                         `json:"item_page_count"`
	Channels                       []channelMonitorCostChannel `json:"channels"`
	APIKeys                        []channelMonitorCostAPIKey  `json:"api_keys"`
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
	summaryOnly := false
	if rawSummaryOnly := c.Query("summary_only"); rawSummaryOnly != "" {
		parsedSummaryOnly, err := strconv.ParseBool(rawSummaryOnly)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "摘要模式参数必须为布尔值"})
			return
		}
		summaryOnly = parsedSummaryOnly
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

	now := common.GetTimestamp()
	todayStart := channelMonitorCostDayStart(now)
	detailDayStart := int64(0)
	if rawDetailDate := c.Query("date"); rawDetailDate != "" {
		parsedDayStart, parseErr := channelMonitorCostDateStart(rawDetailDate)
		rangeStart := todayStart - int64(days-1)*channelMonitorCostDaySeconds
		if parseErr != nil || parsedDayStart < rangeStart || parsedDayStart > todayStart {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "统计日期必须在所选时间范围内"})
			return
		}
		detailDayStart = parsedDayStart
	}
	var overview channelMonitorCostOverview
	var err error
	if summaryOnly {
		overview, err = getChannelMonitorCostSummary(c.Request.Context(), days, now, channelId)
	} else if channelId > 0 {
		overview, err = getChannelMonitorCostOverviewForChannelPageAtDay(c.Request.Context(), days, now, channelId, page, channelMonitorCostDatePageSize, detailDayStart)
	} else {
		overview, err = getChannelMonitorCostOverviewForChannelPageAtDay(c.Request.Context(), days, now, 0, page, channelMonitorCostDatePageSize, detailDayStart)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := applyChannelMonitorRealtimeCost(
		c.Request.Context(), &overview, days, now, channelId, detailDayStart, summaryOnly,
	); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}

func getChannelMonitorCostSummary(ctx context.Context, days int, now int64, channelId int) (channelMonitorCostOverview, error) {
	todayStart := channelMonitorCostDayStart(now)
	startTimestamp := todayStart - int64(days-1)*channelMonitorCostDaySeconds
	endTimestamp := todayStart + channelMonitorCostDaySeconds
	rows, err := model.GetChannelDailyCostsForChannel(ctx, startTimestamp, endTimestamp, channelId)
	if err != nil {
		return channelMonitorCostOverview{}, err
	}

	totalsByDay := make(map[int64]*model.ChannelDailyCostDayTotal, days)
	includedChannels := make(map[int]struct{})
	unresolvedChannels := make(map[int]struct{})
	for _, row := range rows {
		total := totalsByDay[row.DayStart]
		if total == nil {
			total = &model.ChannelDailyCostDayTotal{DayStart: row.DayStart}
			totalsByDay[row.DayStart] = total
		}
		for _, value := range []struct {
			target *int64
			delta  int64
		}{
			{&total.CostNanoCNY, row.CostNanoCNY},
			{&total.ProbeCostNanoCNY, row.ProbeCostNanoCNY},
			{&total.GroupProbeCostNanoCNY, row.GroupProbeCostNanoCNY},
			{&total.ModelDetectionCostNanoCNY, row.ModelDetectionCostNanoCNY},
			{&total.SettledCount, row.SettledCount},
			{&total.UnresolvedCount, row.UnresolvedCount},
		} {
			if err := channelMonitorAddNonNegativeInt64(value.target, value.delta); err != nil {
				return channelMonitorCostOverview{}, err
			}
		}
		if row.SettledCount > 0 {
			includedChannels[row.ChannelId] = struct{}{}
		}
		if row.UnresolvedCount > 0 {
			unresolvedChannels[row.ChannelId] = struct{}{}
		}
	}

	totals := make([]model.ChannelDailyCostDayTotal, 0, len(totalsByDay))
	for _, total := range totalsByDay {
		totals = append(totals, *total)
	}
	items := channelMonitorCostDaysFromTotals(startTimestamp, endTimestamp, totals)
	var totalCostNanoCNY int64
	var totalProbeCostNanoCNY int64
	var totalGroupProbeCostNanoCNY int64
	var totalModelDetectionCostNanoCNY int64
	var settledCount int64
	var unresolvedCount int64
	for _, total := range totals {
		for _, value := range []struct {
			target *int64
			delta  int64
		}{
			{&totalCostNanoCNY, total.CostNanoCNY},
			{&totalProbeCostNanoCNY, total.ProbeCostNanoCNY},
			{&totalGroupProbeCostNanoCNY, total.GroupProbeCostNanoCNY},
			{&totalModelDetectionCostNanoCNY, total.ModelDetectionCostNanoCNY},
			{&settledCount, total.SettledCount},
			{&unresolvedCount, total.UnresolvedCount},
		} {
			if err := channelMonitorAddNonNegativeInt64(value.target, value.delta); err != nil {
				return channelMonitorCostOverview{}, err
			}
		}
	}
	overview := channelMonitorCostOverview{
		Days:                       days,
		GeneratedAt:                now,
		TotalCostCNY:               channelMonitorCostCNY(totalCostNanoCNY),
		TotalProbeCostCNY:          channelMonitorCostCNY(totalProbeCostNanoCNY),
		TotalGroupProbeCostCNY:     channelMonitorCostCNY(totalGroupProbeCostNanoCNY),
		TotalModelDetectionCostCNY: channelMonitorCostCNY(totalModelDetectionCostNanoCNY),
		Items:                      items,
		ChartItems:                 items,
		ItemTotal:                  days,
		ItemPage:                   1,
		ItemPageSize:               days,
		ItemPageCount:              1,
		Channels:                   make([]channelMonitorCostChannel, 0),
		APIKeys:                    make([]channelMonitorCostAPIKey, 0),
		Coverage: channelMonitorCostCoverage{
			IncludedChannelCount:   len(includedChannels),
			UnresolvedChannelCount: len(unresolvedChannels),
			SettledCount:           settledCount,
			UnresolvedCount:        unresolvedCount,
		},
	}
	if len(items) > 0 {
		overview.TodayCostCNY = items[len(items)-1].CostCNY
		overview.TodayProbeCostCNY = items[len(items)-1].ProbeCostCNY
		overview.TodayGroupProbeCostCNY = items[len(items)-1].GroupProbeCostCNY
		overview.TodayModelDetectionCostCNY = items[len(items)-1].ModelDetectionCostCNY
	}
	if len(items) > 1 {
		overview.YesterdayCostCNY = items[len(items)-2].CostCNY
		overview.YesterdayProbeCostCNY = items[len(items)-2].ProbeCostCNY
		overview.YesterdayGroupProbeCostCNY = items[len(items)-2].GroupProbeCostCNY
		overview.YesterdayModelDetectionCostCNY = items[len(items)-2].ModelDetectionCostCNY
	}
	return overview, nil
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
	return getChannelMonitorCostOverviewForChannelPageAtDay(ctx, days, now, channelId, page, pageSize, 0)
}

func getChannelMonitorCostOverviewForChannelPageAtDay(ctx context.Context, days int, now int64, channelId int, page int, pageSize int, detailDayStart int64) (channelMonitorCostOverview, error) {
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
	detailStartTimestamp := startTimestamp
	detailEndTimestamp := endTimestamp
	if detailDayStart > 0 {
		detailStartTimestamp = detailDayStart
		detailEndTimestamp = detailDayStart + channelMonitorCostDaySeconds
	}
	apiKeyRows, err := model.GetChannelDailyAPIKeyCostTotalsForChannel(ctx, detailStartTimestamp, detailEndTimestamp, channelId)
	if err != nil {
		return channelMonitorCostOverview{}, err
	}
	channels, err := model.GetAllChannelsForMonitor()
	if err != nil {
		return channelMonitorCostOverview{}, err
	}
	monitors, err := model.GetChannelRatioMonitorCostMetadata()
	if err != nil {
		return channelMonitorCostOverview{}, err
	}

	channelNames := make(map[int]string, len(channels))
	channelRemarks := make(map[int]string, len(channels))
	channelStatuses := make(map[int]int, len(channels))
	for _, channel := range channels {
		channelNames[channel.Id] = channel.Name
		channelStatuses[channel.Id] = channel.Status
		if channel.Remark != nil {
			channelRemarks[channel.Id] = strings.TrimSpace(*channel.Remark)
		}
	}
	channelCostRatios := make(map[int]float64, len(monitors))
	for _, monitor := range monitors {
		if monitor.UpdatedTime <= 0 {
			continue
		}
		costRatio, _, conversionErr := channelMonitorCostRatioFromModel(monitor, monitor.Ratio)
		if conversionErr == nil {
			channelCostRatios[monitor.ChannelId] = costRatio
		}
	}

	type channelCostSummary struct {
		CostNanoCNY               int64
		ProbeCostNanoCNY          int64
		GroupProbeCostNanoCNY     int64
		ModelDetectionCostNanoCNY int64
		SettledCount              int64
		UnresolvedCount           int64
	}
	channelCosts := make(map[int]*channelCostSummary)
	includedChannels := make(map[int]struct{})
	unresolvedChannels := make(map[int]struct{})
	missingCostConfigChannels := make(map[int]struct{})
	var settledCount int64
	var unresolvedCount int64
	for _, row := range rows {
		if err := channelMonitorAddNonNegativeInt64(&settledCount, row.SettledCount); err != nil {
			return channelMonitorCostOverview{}, err
		}
		if err := channelMonitorAddNonNegativeInt64(&unresolvedCount, row.UnresolvedCount); err != nil {
			return channelMonitorCostOverview{}, err
		}
		if row.SettledCount > 0 {
			includedChannels[row.ChannelId] = struct{}{}
		}
		if row.UnresolvedCount > 0 {
			unresolvedChannels[row.ChannelId] = struct{}{}
		}
		if _, configured := channelCostRatios[row.ChannelId]; !configured {
			missingCostConfigChannels[row.ChannelId] = struct{}{}
		}
	}
	for _, row := range rows {
		if detailDayStart > 0 && row.DayStart != detailDayStart {
			continue
		}
		summary := channelCosts[row.ChannelId]
		if summary == nil {
			summary = &channelCostSummary{}
			channelCosts[row.ChannelId] = summary
		}
		for _, value := range []struct {
			target *int64
			delta  int64
		}{
			{&summary.CostNanoCNY, row.CostNanoCNY},
			{&summary.ProbeCostNanoCNY, row.ProbeCostNanoCNY},
			{&summary.GroupProbeCostNanoCNY, row.GroupProbeCostNanoCNY},
			{&summary.ModelDetectionCostNanoCNY, row.ModelDetectionCostNanoCNY},
			{&summary.SettledCount, row.SettledCount},
			{&summary.UnresolvedCount, row.UnresolvedCount},
		} {
			if err := channelMonitorAddNonNegativeInt64(value.target, value.delta); err != nil {
				return channelMonitorCostOverview{}, err
			}
		}
	}

	chartRows, err := model.GetChannelDailyCostDayTotals(ctx, startTimestamp, endTimestamp, channelId)
	if err != nil {
		return channelMonitorCostOverview{}, err
	}
	chartItems := channelMonitorCostDaysFromTotals(startTimestamp, endTimestamp, chartRows)
	var totalCostNanoCNY int64
	var totalProbeCostNanoCNY int64
	var totalGroupProbeCostNanoCNY int64
	var totalModelDetectionCostNanoCNY int64
	for _, row := range chartRows {
		for _, value := range []struct {
			target *int64
			delta  int64
		}{
			{&totalCostNanoCNY, row.CostNanoCNY},
			{&totalProbeCostNanoCNY, row.ProbeCostNanoCNY},
			{&totalGroupProbeCostNanoCNY, row.GroupProbeCostNanoCNY},
			{&totalModelDetectionCostNanoCNY, row.ModelDetectionCostNanoCNY},
		} {
			if err := channelMonitorAddNonNegativeInt64(value.target, value.delta); err != nil {
				return channelMonitorCostOverview{}, err
			}
		}
	}

	costChannels := make([]channelMonitorCostChannel, 0, len(channelCosts))
	for channelId, summary := range channelCosts {
		channelName := channelNames[channelId]
		if channelName == "" {
			channelName = "已删除渠道"
		}
		var costRatio *float64
		if value, exists := channelCostRatios[channelId]; exists {
			costRatio = &value
		}
		costChannels = append(costChannels, channelMonitorCostChannel{
			ChannelId:             channelId,
			ChannelName:           channelName,
			ChannelRemark:         channelRemarks[channelId],
			Status:                channelStatuses[channelId],
			CostRatio:             costRatio,
			CostCNY:               channelMonitorCostCNY(summary.CostNanoCNY),
			ProbeCostCNY:          channelMonitorCostCNY(summary.ProbeCostNanoCNY),
			GroupProbeCostCNY:     channelMonitorCostCNY(summary.GroupProbeCostNanoCNY),
			ModelDetectionCostCNY: channelMonitorCostCNY(summary.ModelDetectionCostNanoCNY),
			SettledCount:          summary.SettledCount,
			UnresolvedCount:       summary.UnresolvedCount,
		})
	}
	sort.Slice(costChannels, func(i int, j int) bool {
		firstEnabled := costChannels[i].Status == common.ChannelStatusEnabled
		secondEnabled := costChannels[j].Status == common.ChannelStatusEnabled
		if firstEnabled != secondEnabled {
			return firstEnabled
		}
		if costChannels[i].CostRatio == nil && costChannels[j].CostRatio != nil {
			return false
		}
		if costChannels[i].CostRatio != nil && costChannels[j].CostRatio == nil {
			return true
		}
		if costChannels[i].CostRatio != nil && costChannels[j].CostRatio != nil && *costChannels[i].CostRatio != *costChannels[j].CostRatio {
			return *costChannels[i].CostRatio < *costChannels[j].CostRatio
		}
		if costChannels[i].ChannelName != costChannels[j].ChannelName {
			return costChannels[i].ChannelName < costChannels[j].ChannelName
		}
		return costChannels[i].ChannelId < costChannels[j].ChannelId
	})

	type apiKeyCostKey struct {
		APIKeyId       int
		KeyFingerprint string
	}
	type apiKeyChannelSummary struct {
		ChannelName     string
		ChannelRemark   string
		CostNanoCNY     int64
		SettledCount    int64
		UnresolvedCount int64
	}
	type apiKeyCostSummary struct {
		Id              int64
		APIKeyId        int
		APIKeyName      string
		KeyDisplay      string
		CostNanoCNY     int64
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
		if err := channelMonitorAddNonNegativeInt64(&summary.CostNanoCNY, row.CostNanoCNY); err != nil {
			return channelMonitorCostOverview{}, err
		}
		if err := channelMonitorAddNonNegativeInt64(&summary.SettledCount, row.SettledCount); err != nil {
			return channelMonitorCostOverview{}, err
		}
		if err := channelMonitorAddNonNegativeInt64(&summary.UnresolvedCount, row.UnresolvedCount); err != nil {
			return channelMonitorCostOverview{}, err
		}
		channelSummary := summary.Channels[row.ChannelId]
		if channelSummary == nil {
			channelName := channelNames[row.ChannelId]
			if channelName == "" {
				channelName = "已删除渠道"
			}
			channelSummary = &apiKeyChannelSummary{
				ChannelName:   channelName,
				ChannelRemark: channelRemarks[row.ChannelId],
			}
			summary.Channels[row.ChannelId] = channelSummary
		}
		if err := channelMonitorAddNonNegativeInt64(&channelSummary.CostNanoCNY, row.CostNanoCNY); err != nil {
			return channelMonitorCostOverview{}, err
		}
		if err := channelMonitorAddNonNegativeInt64(&channelSummary.SettledCount, row.SettledCount); err != nil {
			return channelMonitorCostOverview{}, err
		}
		if err := channelMonitorAddNonNegativeInt64(&channelSummary.UnresolvedCount, row.UnresolvedCount); err != nil {
			return channelMonitorCostOverview{}, err
		}
		channelTotal := apiKeyChannelTotals[row.ChannelId]
		if channelTotal == nil {
			channelTotal = &apiKeyChannelTotal{}
			apiKeyChannelTotals[row.ChannelId] = channelTotal
		}
		if err := channelMonitorAddNonNegativeInt64(&channelTotal.CostNanoCNY, row.CostNanoCNY); err != nil {
			return channelMonitorCostOverview{}, err
		}
		if err := channelMonitorAddNonNegativeInt64(&channelTotal.SettledCount, row.SettledCount); err != nil {
			return channelMonitorCostOverview{}, err
		}
		if err := channelMonitorAddNonNegativeInt64(&channelTotal.UnresolvedCount, row.UnresolvedCount); err != nil {
			return channelMonitorCostOverview{}, err
		}
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
		if unattributedCost < 0 || unattributedSettled < 0 || unattributedUnresolved < 0 {
			return channelMonitorCostOverview{}, errors.New("渠道监控 API Key 成本归属超过渠道总额")
		}
		if unattributedCost <= 0 && unattributedSettled <= 0 && unattributedUnresolved <= 0 {
			continue
		}
		summary := apiKeyCosts[unattributedKey]
		if summary == nil {
			summary = &apiKeyCostSummary{
				APIKeyName: "未识别 API Key",
				Channels:   make(map[int]*apiKeyChannelSummary),
			}
			apiKeyCosts[unattributedKey] = summary
		}
		if err := channelMonitorAddNonNegativeInt64(&summary.CostNanoCNY, unattributedCost); err != nil {
			return channelMonitorCostOverview{}, err
		}
		if err := channelMonitorAddNonNegativeInt64(&summary.SettledCount, unattributedSettled); err != nil {
			return channelMonitorCostOverview{}, err
		}
		if err := channelMonitorAddNonNegativeInt64(&summary.UnresolvedCount, unattributedUnresolved); err != nil {
			return channelMonitorCostOverview{}, err
		}
		channelName := channelNames[channelId]
		if channelName == "" {
			channelName = "已删除渠道"
		}
		channelDetail := summary.Channels[channelId]
		if channelDetail == nil {
			channelDetail = &apiKeyChannelSummary{
				ChannelName:   channelName,
				ChannelRemark: channelRemarks[channelId],
			}
			summary.Channels[channelId] = channelDetail
		}
		if err := channelMonitorAddNonNegativeInt64(&channelDetail.CostNanoCNY, unattributedCost); err != nil {
			return channelMonitorCostOverview{}, err
		}
		if err := channelMonitorAddNonNegativeInt64(&channelDetail.SettledCount, unattributedSettled); err != nil {
			return channelMonitorCostOverview{}, err
		}
		if err := channelMonitorAddNonNegativeInt64(&channelDetail.UnresolvedCount, unattributedUnresolved); err != nil {
			return channelMonitorCostOverview{}, err
		}
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
				ChannelRemark:   channelSummary.ChannelRemark,
				CostCNY:         channelMonitorCostCNY(channelSummary.CostNanoCNY),
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
			CostCNY:         channelMonitorCostCNY(summary.CostNanoCNY),
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
		Days:                       days,
		GeneratedAt:                now,
		TotalCostCNY:               channelMonitorCostCNY(totalCostNanoCNY),
		TotalProbeCostCNY:          channelMonitorCostCNY(totalProbeCostNanoCNY),
		TotalGroupProbeCostCNY:     channelMonitorCostCNY(totalGroupProbeCostNanoCNY),
		TotalModelDetectionCostCNY: channelMonitorCostCNY(totalModelDetectionCostNanoCNY),
		Coverage: channelMonitorCostCoverage{
			IncludedChannelCount:          len(includedChannels),
			UnresolvedChannelCount:        len(unresolvedChannels),
			MissingCostConfigChannelCount: len(missingCostConfigChannels),
			SettledCount:                  settledCount,
			UnresolvedCount:               unresolvedCount,
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
	if detailDayStart > 0 {
		overview.DetailDate = channelMonitorCostDate(detailDayStart)
	}
	if len(chartItems) > 0 {
		overview.TodayCostCNY = chartItems[len(chartItems)-1].CostCNY
		overview.TodayProbeCostCNY = chartItems[len(chartItems)-1].ProbeCostCNY
		overview.TodayGroupProbeCostCNY = chartItems[len(chartItems)-1].GroupProbeCostCNY
		overview.TodayModelDetectionCostCNY = chartItems[len(chartItems)-1].ModelDetectionCostCNY
	}
	if len(chartItems) > 1 {
		overview.YesterdayCostCNY = chartItems[len(chartItems)-2].CostCNY
		overview.YesterdayProbeCostCNY = chartItems[len(chartItems)-2].ProbeCostCNY
		overview.YesterdayGroupProbeCostCNY = chartItems[len(chartItems)-2].GroupProbeCostCNY
		overview.YesterdayModelDetectionCostCNY = chartItems[len(chartItems)-2].ModelDetectionCostCNY
	}
	return overview, nil
}

func channelMonitorCostDaysFromTotals(startTimestamp int64, endTimestamp int64, rows []model.ChannelDailyCostDayTotal) []channelMonitorCostDay {
	dailyCosts := make(map[int64]channelMonitorCostDay, len(rows))
	for _, row := range rows {
		dailyCosts[row.DayStart] = channelMonitorCostDay{
			Date:                  channelMonitorCostDate(row.DayStart),
			StartAt:               row.DayStart,
			CostCNY:               channelMonitorCostCNY(row.CostNanoCNY),
			ProbeCostCNY:          channelMonitorCostCNY(row.ProbeCostNanoCNY),
			GroupProbeCostCNY:     channelMonitorCostCNY(row.GroupProbeCostNanoCNY),
			ModelDetectionCostCNY: channelMonitorCostCNY(row.ModelDetectionCostNanoCNY),
			SettledCount:          row.SettledCount,
			UnresolvedCount:       row.UnresolvedCount,
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

func channelMonitorCostDateStart(date string) (int64, error) {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(date))
	if err != nil {
		return 0, err
	}
	return parsed.Unix() - channelMonitorCostOffset, nil
}

func channelMonitorCostCNY(costNanoCNY int64) float64 {
	return float64(costNanoCNY) / float64(model.ChannelDailyCostNanoPerCNY)
}

func channelMonitorAddNonNegativeInt64(target *int64, delta int64) error {
	if delta < 0 || *target < 0 || *target > math.MaxInt64-delta {
		return errors.New("渠道监控成本汇总超过 int64 范围")
	}
	*target += delta
	return nil
}
