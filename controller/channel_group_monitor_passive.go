package controller

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

// Public monitoring exposes group aggregates without physical IDs or policy text.
type channelGroupPassiveResponse struct {
	Source          string                       `json:"source"`
	Scope           string                       `json:"scope"`
	IntervalSeconds int                          `json:"interval_seconds"`
	Period          service.ChannelPassivePeriod `json:"period"`
}

func applyChannelGroupPassiveOverview(ctx context.Context, items []channelGroupMonitorItemResponse) {
	views, members := service.ReadChannelPassiveGroupSummaries(ctx)
	for index := range items {
		item := &items[index]
		if item.Status == channelGroupMonitorHealthPaused {
			continue
		}
		item.PassiveMembers = members[item.Group]
		view, exists := views[item.Group]
		if exists && view.Target.ModelName == item.ProbeModel {
			if len(view.Periods) == 0 {
				continue
			}
			// Admin API supplies member details through /passive. Keep this public
			// extension small and unambiguous for mixed groups.
			if view.Target.Scope != "group_final" {
				item.PassiveMembers = true
				continue
			}
			period := view.Periods[0]
			item.Passive = &channelGroupPassiveResponse{Source: "redis_business", Scope: "group_final", IntervalSeconds: view.Target.IntervalSeconds, Period: period}
			item.LatestFirstTokenMs = period.AverageFirstTokenMs
			item.CacheRate = nil
			item.SuccessRate = nil
			if period.SuccessRate != nil {
				value := *period.SuccessRate * 100
				item.SuccessRate = &value
			}
			item.SuccessCount = int(period.Success)
			item.CompletedCount = int(period.Success + period.Failure)
			item.LastFinishedAt = period.PeriodEnd
			item.Status = channelGroupMonitorHealthPending
			if period.Coverage != "complete" {
				item.Status = channelGroupMonitorHealthStale
			} else if period.Success+period.Failure > 0 {
				item.Status = channelGroupMonitorHealthHealthy
				if period.Failure > 0 {
					item.Status = channelGroupMonitorHealthUnhealthy
				}
			}
		}
	}
}

// Only a scheduled group consisting entirely of disabled physical channels can
// finish before resolving a pricing user. Manual runs retain the ordinary path.
func channelGroupUsesOnlyPassiveMonitoring(ctx context.Context, group, modelName string) (bool, error) {
	var abilities []model.Ability
	if err := model.DB.WithContext(ctx).Select("channel_id").Where(&model.Ability{Group: group, Model: modelName}).Find(&abilities).Error; err != nil {
		return false, err
	}
	if len(abilities) == 0 {
		return false, nil
	}
	ids := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		ids = append(ids, ability.ChannelId)
	}
	var disabled int64
	err := model.DB.WithContext(ctx).Model(&model.ChannelRatioMonitor{}).Where("channel_id IN ? AND auto_probe_disabled = ?", ids, true).Count(&disabled).Error
	return disabled == int64(len(ids)), err
}

func refreshChannelPassiveConfiguration() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = service.RefreshChannelPassiveTargets(ctx)
}
