package controller

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"gorm.io/gorm"
)

func channelMonitorProfitCoverage(ctx context.Context, db *gorm.DB, query channelMonitorAnalyticsQuery, state model.ChannelMonitorIncomeState, queue []service.ChannelMonitorProfitBlock) ([]string, string, error) {
	blocks := slices.Clone(queue)
	blocks = append(blocks,
		service.ChannelMonitorProfitBlock{To: state.RetainedFrom, Reason: "profit_history_expired"},
		service.ChannelMonitorProfitBlock{To: state.StartedAt, Reason: "income_history_unavailable"})
	// A legacy node may still write the old unbounded marker during rollout.
	since := state.GapSince
	if pendingSince := model.ChannelMonitorIncomeGapSince(); pendingSince > 0 && (since == 0 || pendingSince < since) {
		since = pendingSince
	}
	if since > 0 {
		blocks = append(blocks, service.ChannelMonitorProfitBlock{From: model.ChannelDailyCostDayStart(since), Reason: "income_recording_gap"})
	}
	var gaps []model.ChannelMonitorIncomeGap
	if err := db.WithContext(ctx).Where("from_at < ? AND to_at > ?", query.To, query.From).Find(&gaps).Error; err != nil {
		return nil, "", err
	}
	for _, gap := range gaps {
		blocks = append(blocks, service.ChannelMonitorProfitBlock{From: gap.From, To: gap.To, ChannelID: gap.ChannelID, Reason: "income_recording_gap"})
	}
	// Modulo is supported by all three SQL dialects and keeps the exact
	// Beijing day, unlike a MIN/MAX interval spanning several pending days.
	const daySQL = "occurred_at - ((occurred_at + 28800) % 86400)"
	var pending []struct {
		ChannelID int
		DayStart  int64
	}
	backlog := db.WithContext(ctx).Model(&model.ChannelDailyCostOutbox{}).
		Where("processed_at = 0 AND occurred_at >= ? AND occurred_at < ?", query.From, query.To)
	if query.Channel > 0 {
		backlog = backlog.Where("channel_id = ?", query.Channel)
	}
	if err := backlog.Select("channel_id, " + daySQL + " AS day_start").Group("channel_id, " + daySQL).Scan(&pending).Error; err != nil {
		return nil, "", err
	}
	for _, row := range pending {
		blocks = append(blocks, service.ChannelMonitorProfitBlock{From: row.DayStart, To: row.DayStart + 86400, ChannelID: row.ChannelID, Reason: "cost_projection_pending"})
	}
	if (query.GroupBy != "channel" && query.GroupBy != "day") || query.hasCostDetailFilter() {
		gaps, err := channelMonitorHistoricalCostDetailGaps(ctx, db, query)
		if err != nil {
			return nil, "", err
		}
		blocks = append(blocks, gaps...)
	}
	if !common.GetEnvOrDefaultBool("CHANNEL_DAILY_COST_RELIABLE_OUTBOX", true) {
		blocks = append(blocks, service.ChannelMonitorProfitBlock{Reason: "profit_cost_not_durable"})
	}
	var reasons, predicates []string
	blockedChannels := make(map[int]bool)
	allRowsBlocked := false
	for _, block := range blocks {
		if block.Reason == "profit_history_expired" && block.To == 0 || block.Reason == "income_history_unavailable" && block.To == 0 {
			continue
		}
		if block.To > 0 && block.To <= query.From || block.From >= query.To || query.Channel > 0 && block.ChannelID > 0 && query.Channel != block.ChannelID {
			continue
		}
		if !slices.Contains(reasons, block.Reason) {
			reasons = append(reasons, block.Reason)
		}
		var conditions []string
		if query.GroupBy == "day" {
			if block.From > 0 {
				conditions = append(conditions, fmt.Sprintf("day_start >= %d", model.ChannelDailyCostDayStart(block.From)))
			}
			if block.To > 0 {
				conditions = append(conditions, fmt.Sprintf("day_start < %d", block.To))
			}
		}
		if block.ChannelID > 0 && (query.GroupBy == "channel" || query.GroupBy == "channel_model" || query.GroupBy == "api_key_channel_model") {
			blockedChannels[block.ChannelID] = true
			continue
		}
		if len(conditions) == 0 {
			allRowsBlocked = true
			continue
		}
		condition := strings.Join(conditions, " AND ")
		if !slices.Contains(predicates, condition) {
			predicates = append(predicates, condition)
		}
	}
	if allRowsBlocked {
		return reasons, "1", nil
	}
	if len(blockedChannels) > 0 {
		ids := make([]int, 0, len(blockedChannels))
		for id := range blockedChannels {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		values := make([]string, 0, len(ids))
		for _, id := range ids {
			values = append(values, strconv.Itoa(id))
		}
		predicates = append(predicates, "channel_id IN ("+strings.Join(values, ",")+")")
	}
	if len(predicates) == 0 {
		return reasons, "0", nil
	}
	return reasons, "MAX(CASE WHEN (" + strings.Join(predicates, ") OR (") + ") THEN 1 ELSE 0 END)", nil
}
