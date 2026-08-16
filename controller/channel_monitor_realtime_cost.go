package controller

import (
	"context"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type channelMonitorRealtimeChannelCost struct {
	CostNanoCNY               int64
	ProbeCostNanoCNY          int64
	ModelDetectionCostNanoCNY int64
	SettledCount              int64
	UnresolvedCount           int64
}

func channelMonitorRealtimeTodayCosts(ctx context.Context, channelId int, dayStart int64) (map[int]channelMonitorRealtimeChannelCost, error) {
	costs := make(map[int]channelMonitorRealtimeChannelCost)
	shared, err := service.QueryChannelMonitorRedisSharedProjectionForCosts(ctx, dayStart, dayStart+channelMonitorCostDaySeconds)
	if err != nil {
		return nil, err
	}
	for itemChannelID, aggregate := range shared {
		if channelId > 0 && itemChannelID != channelId {
			continue
		}
		costs[itemChannelID] = channelMonitorRealtimeChannelCost{
			CostNanoCNY:               aggregate.SettledCostNanoCNY,
			ProbeCostNanoCNY:          aggregate.ProbeSettledCostNanoCNY,
			ModelDetectionCostNanoCNY: aggregate.ModelDetectionSettledCostNanoCNY,
			SettledCount:              aggregate.SettledRequestCount,
			UnresolvedCount:           aggregate.UnresolvedRequestCount,
		}
	}
	return costs, nil
}

func applyChannelMonitorRealtimeCost(
	ctx context.Context,
	overview *channelMonitorCostOverview,
	days int,
	now int64,
	channelId int,
	detailDayStart int64,
	summaryOnly bool,
) error {
	todayStart := channelMonitorCostDayStart(now)
	realtimeCosts, err := channelMonitorRealtimeTodayCosts(ctx, channelId, todayStart)
	if err != nil {
		return err
	}
	today := channelMonitorCostDay{Date: channelMonitorCostDate(todayStart), StartAt: todayStart}
	for _, cost := range realtimeCosts {
		today.CostCNY += channelMonitorCostCNY(cost.CostNanoCNY)
		today.ProbeCostCNY += channelMonitorCostCNY(cost.ProbeCostNanoCNY)
		today.ModelDetectionCostCNY += channelMonitorCostCNY(cost.ModelDetectionCostNanoCNY)
		today.SettledCount += cost.SettledCount
		today.UnresolvedCount += cost.UnresolvedCount
	}
	overview.ChartItems = channelMonitorReplaceCostDay(overview.ChartItems, today)
	overview.Items = channelMonitorReplaceCostDay(overview.Items, today)
	overview.TodayCostCNY = today.CostCNY
	overview.TodayProbeCostCNY = today.ProbeCostCNY
	overview.TodayModelDetectionCostCNY = today.ModelDetectionCostCNY
	overview.TotalCostCNY = 0
	overview.TotalProbeCostCNY = 0
	overview.TotalModelDetectionCostCNY = 0
	for _, item := range overview.ChartItems {
		overview.TotalCostCNY += item.CostCNY
		overview.TotalProbeCostCNY += item.ProbeCostCNY
		overview.TotalModelDetectionCostCNY += item.ModelDetectionCostCNY
	}
	metadata := channelMonitorRealtimeMetadata(todayStart)
	overview.DataCutoffAt = metadata.DataCutoffAt
	overview.ProcessedAt = metadata.ProcessedAt
	overview.ProjectionStartedAt = metadata.ProjectionStartedAt
	overview.EventWatermark = metadata.EventWatermark
	overview.QueueDepth = metadata.QueueDepth
	overview.RedisStatus = metadata.RedisStatus
	overview.RedisAvailable = metadata.RedisAvailable
	overview.RedisConsumerRunning = metadata.RedisConsumerRunning
	overview.PendingCount = metadata.PendingCount
	overview.OldestPendingAt = metadata.OldestPendingAt
	overview.ConsumerLagSeconds = metadata.ConsumerLagSeconds
	overview.LastPublishedAt = metadata.LastPublishedAt
	overview.LastProcessedAt = metadata.LastProcessedAt
	overview.RetryCount = metadata.RetryCount
	overview.TakeoverCount = metadata.TakeoverCount
	overview.MarkerReleaseFailureCount = metadata.MarkerReleaseFailureCount
	overview.MarkerReleaseFailureActive = metadata.MarkerReleaseFailureActive
	overview.StreamTrimFailureCount = metadata.StreamTrimFailureCount
	overview.StreamTrimFailureActive = metadata.StreamTrimFailureActive
	overview.RealtimeDegraded = metadata.RealtimeDegraded

	startTimestamp := todayStart - int64(days-1)*channelMonitorCostDaySeconds
	historicalRows, err := model.GetChannelDailyCostsForChannel(ctx, startTimestamp, todayStart, channelId)
	if err != nil {
		return err
	}
	overview.Coverage = channelMonitorRealtimeCostCoverage(historicalRows, realtimeCosts)
	if summaryOnly {
		return nil
	}
	if detailDayStart > 0 && detailDayStart < todayStart {
		return nil
	}
	overview.Channels, err = channelMonitorRealtimeCostChannels(ctx, historicalRows, realtimeCosts, detailDayStart == todayStart)
	if err != nil {
		return err
	}
	pageView, err := service.QueryChannelMonitorRealtimePageFromRedis(ctx, todayStart, todayStart+channelMonitorCostDaySeconds)
	if err != nil {
		return err
	}
	realtimeAPIKeys := channelMonitorRealtimeCostAPIKeys(pageView)
	if detailDayStart == todayStart {
		overview.APIKeys = realtimeAPIKeys
	} else if channelId == 0 && len(realtimeAPIKeys) > 0 {
		byId := make(map[int]channelMonitorCostAPIKey, len(overview.APIKeys)+len(realtimeAPIKeys))
		for _, item := range overview.APIKeys {
			byId[item.APIKeyId] = item
		}
		for _, item := range realtimeAPIKeys {
			byId[item.APIKeyId] = item
		}
		overview.APIKeys = overview.APIKeys[:0]
		for _, item := range byId {
			overview.APIKeys = append(overview.APIKeys, item)
		}
		sort.Slice(overview.APIKeys, func(i int, j int) bool { return overview.APIKeys[i].CostCNY > overview.APIKeys[j].CostCNY })
	}
	return nil
}

func channelMonitorRealtimeCostAPIKeys(view service.ChannelMonitorRealtimePageView) []channelMonitorCostAPIKey {
	items := make([]channelMonitorCostAPIKey, 0, len(view.APIKeys))
	for _, apiKey := range view.APIKeys {
		items = append(items, channelMonitorCostAPIKey{
			APIKeyId:        apiKey.APIKeyId,
			APIKeyName:      apiKey.APIKeyName,
			CostCNY:         channelMonitorCostCNY(apiKey.SettledCostNanoCNY),
			SettledCount:    apiKey.SettledRequestCount,
			UnresolvedCount: apiKey.UnresolvedRequestCount,
		})
	}
	sort.Slice(items, func(i int, j int) bool {
		if items[i].CostCNY != items[j].CostCNY {
			return items[i].CostCNY > items[j].CostCNY
		}
		return items[i].APIKeyId < items[j].APIKeyId
	})
	return items
}

func channelMonitorReplaceCostDay(items []channelMonitorCostDay, replacement channelMonitorCostDay) []channelMonitorCostDay {
	for index := range items {
		if items[index].StartAt == replacement.StartAt {
			items[index] = replacement
			return items
		}
	}
	return items
}

func channelMonitorRealtimeCostCoverage(
	historicalRows []model.ChannelDailyCost,
	realtimeCosts map[int]channelMonitorRealtimeChannelCost,
) channelMonitorCostCoverage {
	included := make(map[int]struct{})
	unresolved := make(map[int]struct{})
	coverage := channelMonitorCostCoverage{}
	for _, row := range historicalRows {
		coverage.SettledCount += row.SettledCount
		coverage.UnresolvedCount += row.UnresolvedCount
		if row.SettledCount > 0 {
			included[row.ChannelId] = struct{}{}
		}
		if row.UnresolvedCount > 0 {
			unresolved[row.ChannelId] = struct{}{}
		}
	}
	for channelId, cost := range realtimeCosts {
		coverage.SettledCount += cost.SettledCount
		coverage.UnresolvedCount += cost.UnresolvedCount
		if cost.SettledCount > 0 {
			included[channelId] = struct{}{}
		}
		if cost.UnresolvedCount > 0 {
			unresolved[channelId] = struct{}{}
		}
	}
	coverage.IncludedChannelCount = len(included)
	coverage.UnresolvedChannelCount = len(unresolved)
	return coverage
}

func channelMonitorRealtimeCostChannels(
	ctx context.Context,
	historicalRows []model.ChannelDailyCost,
	realtimeCosts map[int]channelMonitorRealtimeChannelCost,
	todayOnly bool,
) ([]channelMonitorCostChannel, error) {
	channels, err := model.GetAllChannelsForMonitor()
	if err != nil {
		return nil, err
	}
	monitors, err := model.GetChannelRatioMonitorCostMetadata()
	if err != nil {
		return nil, err
	}
	type channelMetadata struct {
		name   string
		remark string
		status int
		ratio  *float64
	}
	metadata := make(map[int]channelMetadata, len(channels))
	for _, channel := range channels {
		remark := ""
		if channel.Remark != nil {
			remark = strings.TrimSpace(*channel.Remark)
		}
		metadata[channel.Id] = channelMetadata{name: channel.Name, remark: remark, status: channel.Status}
	}
	for _, monitor := range monitors {
		if monitor.UpdatedTime <= 0 {
			continue
		}
		costRatio, _, conversionErr := channelMonitorCostRatioFromModel(monitor, monitor.Ratio)
		if conversionErr != nil {
			continue
		}
		item := metadata[monitor.ChannelId]
		item.ratio = &costRatio
		metadata[monitor.ChannelId] = item
	}
	costs := make(map[int]channelMonitorRealtimeChannelCost)
	if !todayOnly {
		for _, row := range historicalRows {
			cost := costs[row.ChannelId]
			cost.CostNanoCNY += row.CostNanoCNY
			cost.ProbeCostNanoCNY += row.ProbeCostNanoCNY
			cost.ModelDetectionCostNanoCNY += row.ModelDetectionCostNanoCNY
			cost.SettledCount += row.SettledCount
			cost.UnresolvedCount += row.UnresolvedCount
			costs[row.ChannelId] = cost
		}
	}
	for channelId, realtimeCost := range realtimeCosts {
		cost := costs[channelId]
		cost.CostNanoCNY += realtimeCost.CostNanoCNY
		cost.ProbeCostNanoCNY += realtimeCost.ProbeCostNanoCNY
		cost.ModelDetectionCostNanoCNY += realtimeCost.ModelDetectionCostNanoCNY
		cost.SettledCount += realtimeCost.SettledCount
		cost.UnresolvedCount += realtimeCost.UnresolvedCount
		costs[channelId] = cost
	}
	items := make([]channelMonitorCostChannel, 0, len(costs))
	for channelId, cost := range costs {
		channel := metadata[channelId]
		name := channel.name
		if name == "" {
			name = "已删除渠道"
		}
		items = append(items, channelMonitorCostChannel{
			ChannelId:             channelId,
			ChannelName:           name,
			ChannelRemark:         channel.remark,
			Status:                channel.status,
			CostRatio:             channel.ratio,
			CostCNY:               channelMonitorCostCNY(cost.CostNanoCNY),
			ProbeCostCNY:          channelMonitorCostCNY(cost.ProbeCostNanoCNY),
			ModelDetectionCostCNY: channelMonitorCostCNY(cost.ModelDetectionCostNanoCNY),
			SettledCount:          cost.SettledCount,
			UnresolvedCount:       cost.UnresolvedCount,
		})
	}
	sort.Slice(items, func(i int, j int) bool {
		firstEnabled := items[i].Status == common.ChannelStatusEnabled
		secondEnabled := items[j].Status == common.ChannelStatusEnabled
		if firstEnabled != secondEnabled {
			return firstEnabled
		}
		if items[i].CostRatio == nil || items[j].CostRatio == nil {
			return items[i].CostRatio != nil
		}
		if *items[i].CostRatio != *items[j].CostRatio {
			return *items[i].CostRatio < *items[j].CostRatio
		}
		if items[i].ChannelName != items[j].ChannelName {
			return items[i].ChannelName < items[j].ChannelName
		}
		return items[i].ChannelId < items[j].ChannelId
	})
	return items, nil
}
