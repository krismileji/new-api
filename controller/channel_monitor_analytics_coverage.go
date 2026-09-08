package controller

import (
	"context"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

// Reconcile each physical channel/day before calling attributed history
// complete. A global SUM alone can hide opposite gaps on two channels.
func channelMonitorHistoricalCostDetailCoverage(ctx context.Context, query channelMonitorAnalyticsQuery) (service.ChannelMonitorCoverage, error) {
	columns := "channel_id, day_start, SUM(cost_nano_cny) AS cost, SUM(settled_count) AS settled, SUM(unresolved_count) AS unresolved, SUM(probe_cost_nano_cny) AS probe, SUM(group_probe_cost_nano_cny) AS group_probe"
	ledger := model.DB.WithContext(ctx).Model(&model.ChannelDailyCost{}).
		Where("day_start >= ? AND day_start < ?", query.From, query.To)
	detail := model.DB.WithContext(ctx).Model(&model.ChannelMonitorDailyCostDetail{}).
		Where("day_start >= ? AND day_start < ?", query.From, query.To)
	if query.Channel > 0 {
		ledger = ledger.Where("channel_id = ?", query.Channel)
		detail = detail.Where("channel_id = ?", query.Channel)
	}
	ledger = ledger.Select(columns).Group("channel_id, day_start")
	detail = detail.Select(columns).Group("channel_id, day_start")
	var gaps int64
	err := model.DB.WithContext(ctx).Table("(?) AS ledger", ledger).
		Joins("LEFT JOIN (?) AS detail ON detail.channel_id = ledger.channel_id AND detail.day_start = ledger.day_start", detail).
		Where("ledger.cost <> COALESCE(detail.cost, 0) OR ledger.settled <> COALESCE(detail.settled, 0) OR ledger.unresolved <> COALESCE(detail.unresolved, 0) OR ledger.probe <> COALESCE(detail.probe, 0) OR ledger.group_probe <> COALESCE(detail.group_probe, 0)").
		Count(&gaps).Error
	if err != nil {
		return service.ChannelMonitorCoverage{}, err
	}
	if gaps == 0 {
		err = model.DB.WithContext(ctx).Table("(?) AS detail", detail).
			Joins("LEFT JOIN (?) AS ledger ON detail.channel_id = ledger.channel_id AND detail.day_start = ledger.day_start", ledger).
			Where("ledger.channel_id IS NULL AND (detail.cost <> 0 OR detail.settled <> 0 OR detail.unresolved <> 0 OR detail.probe <> 0 OR detail.group_probe <> 0)").
			Count(&gaps).Error
		if err != nil {
			return service.ChannelMonitorCoverage{}, err
		}
	}
	var reasons []string
	if gaps > 0 {
		reasons = append(reasons, "cost_attribution_incomplete")
	}
	return service.DeriveChannelMonitorCoverage(true, query.From, query.To, query.From, query.To, reasons), nil
}

// A checkpoint can prove that an idle day was observed. No checkpoint cannot
// prove zero traffic, and the last business sample is not a wall-clock SLA.
func channelMonitorHistoricalSuccessCoverage(ctx context.Context, query channelMonitorAnalyticsQuery) (service.ChannelMonitorCoverage, int64, error) {
	var checkpoints []model.ChannelMonitorDailyCheckpoint
	if model.DB.Migrator().HasTable(&model.ChannelMonitorDailyCheckpoint{}) {
		if err := model.DB.WithContext(ctx).Where("day_start >= ? AND day_start < ?", query.From, query.To).
			Find(&checkpoints).Error; err != nil {
			return service.ChannelMonitorCoverage{}, 0, err
		}
	}
	byDay := make(map[int64]model.ChannelMonitorDailyCheckpoint, len(checkpoints))
	var processedAt int64
	for _, checkpoint := range checkpoints {
		byDay[checkpoint.DayStart] = checkpoint
		processedAt = max(processedAt, checkpoint.UpdatedAt)
	}
	var from, through int64
	missing, partial := false, false
	for day := query.From; day < query.To; day += 86400 {
		checkpoint, exists := byDay[day]
		partial = partial || checkpoint.CoveragePartial
		if !exists || checkpoint.Revision <= 0 {
			missing = true
			continue
		}
		if from == 0 {
			from, through = day, day+86400
		} else if day == through {
			through = day + 86400
		}
	}
	var reasons []string
	if missing {
		reasons = append(reasons, "daily_checkpoint_missing")
	}
	if partial {
		reasons = append(reasons, "daily_replay_incomplete")
	}
	return service.DeriveChannelMonitorCoverage(true, query.From, query.To, from, through, reasons), processedAt, nil
}
