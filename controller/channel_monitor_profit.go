package controller

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"gorm.io/gorm"
)

type channelMonitorProfitRow struct {
	DayStart                  int64  `gorm:"column:day_start"`
	ChannelID                 int    `gorm:"column:channel_id"`
	UserID                    int    `gorm:"column:user_id"`
	APIKeyID                  int    `gorm:"column:api_key_id"`
	APIKeyKey                 string `gorm:"column:api_key_key"`
	APIKeyName                string `gorm:"column:api_key_name"`
	ModelKey                  string `gorm:"column:model_key"`
	ModelName                 string `gorm:"column:model_name"`
	Cost                      int64  `gorm:"column:cost_nano_cny"`
	ProbeCost                 int64  `gorm:"column:probe_cost_nano_cny"`
	SettledCount              int64  `gorm:"column:settled_count"`
	UnresolvedCount           int64  `gorm:"column:unresolved_count"`
	IncomeNanoCNY             int64  `gorm:"column:income_nano_cny"`
	WalletIncomeNanoCNY       int64  `gorm:"column:wallet_income_nano_cny"`
	SubscriptionIncomeNanoCNY int64  `gorm:"column:subscription_income_nano_cny"`
	PendingIncomeCount        int64  `gorm:"column:pending_income_count"`
	PendingCostCount          int64  `gorm:"column:pending_cost_count"`
	ModelDetectionCostNanoCNY int64  `gorm:"column:model_detection_cost_nano_cny"`
}

// UNION ALL retains both cost-only and income-only dimensions. Aggregation,
// sorting and pagination run in SQL, never over a truncated page of costs.
func channelMonitorProfitFacts(ctx context.Context, db *gorm.DB, query channelMonitorAnalyticsQuery) *gorm.DB {
	income := db.WithContext(ctx).Model(&model.ChannelMonitorIncome{}).
		Where("day_start >= ? AND day_start < ?", query.From, query.To).
		Select("day_start, channel_id, user_id, api_key_id, api_key_key, api_key_name, model_key, model_name, 'business' AS source_kind, CASE WHEN status = 'settled' THEN income_nano_cny ELSE 0 END AS income_nano_cny, CASE WHEN status = 'settled' AND billing_source = 'wallet' THEN income_nano_cny ELSE 0 END AS wallet_income_nano_cny, CASE WHEN status = 'settled' AND billing_source = 'subscription' THEN income_nano_cny ELSE 0 END AS subscription_income_nano_cny, CASE WHEN status = 'settled' THEN 0 ELSE 1 END AS pending_income_count, CASE WHEN cost_recorded = 1 THEN 0 ELSE 1 END AS pending_cost_count, 0 AS cost_nano_cny, 0 AS probe_cost_nano_cny, 0 AS model_detection_cost_nano_cny, 0 AS settled_count, 0 AS unresolved_count")
	cost := db.WithContext(ctx).Model(&model.ChannelMonitorDailyCostDetail{}).
		Where("day_start >= ? AND day_start < ?", query.From, query.To).
		Select("day_start, channel_id, user_id, api_key_id, api_key_key, api_key_name, model_key, model_name, source_kind, 0 AS income_nano_cny, 0 AS wallet_income_nano_cny, 0 AS subscription_income_nano_cny, 0 AS pending_income_count, 0 AS pending_cost_count, cost_nano_cny, probe_cost_nano_cny, CASE WHEN source_kind = 'model_detection' THEN cost_nano_cny ELSE 0 END AS model_detection_cost_nano_cny, settled_count, unresolved_count")
	if (query.GroupBy == "channel" || query.GroupBy == "day") && !query.hasCostDetailFilter() {
		cost = db.WithContext(ctx).Model(&model.ChannelDailyCost{}).
			Where("day_start >= ? AND day_start < ?", query.From, query.To).
			Select("day_start, channel_id, 0 AS user_id, 0 AS api_key_id, '' AS api_key_key, '' AS api_key_name, '' AS model_key, '' AS model_name, '' AS source_kind, 0 AS income_nano_cny, 0 AS wallet_income_nano_cny, 0 AS subscription_income_nano_cny, 0 AS pending_income_count, 0 AS pending_cost_count, cost_nano_cny, probe_cost_nano_cny, model_detection_cost_nano_cny, settled_count, unresolved_count")
	}
	base := db.WithContext(ctx).Table("(? UNION ALL ?) AS facts", income, cost)
	if query.Channel > 0 {
		base = base.Where("channel_id = ?", query.Channel)
	}
	if query.hasUserFilter() {
		base = base.Where("user_id = ?", query.User)
	}
	if query.hasAPIKeyFilter() {
		base = base.Where("api_key_id = ?", query.APIKey)
	}
	if query.APIKeyKey != nil {
		if channelMonitorAnalyticsSystemAPIKey(*query.APIKeyKey) {
			base = base.Where("api_key_id = 0 AND ("+channelMonitorAnalyticsCostAPIKeyGroupSQL+") = ?", *query.APIKeyKey)
		} else {
			base = base.Where("api_key_key = ?", *query.APIKeyKey)
		}
	}
	if query.ModelKey != nil {
		base = base.Where("model_key = ?", *query.ModelKey)
	} else if query.Model != "" {
		base = base.Where("model_name = ? OR model_key = ?", query.Model, query.Model)
	}
	return query.applySearch(base)
}

const channelMonitorProfitSums = "COALESCE(SUM(income_nano_cny),0) AS income_nano_cny, COALESCE(SUM(wallet_income_nano_cny),0) AS wallet_income_nano_cny, COALESCE(SUM(subscription_income_nano_cny),0) AS subscription_income_nano_cny, COALESCE(SUM(pending_income_count),0) AS pending_income_count, COALESCE(SUM(pending_cost_count),0) AS pending_cost_count, COALESCE(SUM(cost_nano_cny),0) AS cost_nano_cny, COALESCE(SUM(probe_cost_nano_cny),0) AS probe_cost_nano_cny, COALESCE(SUM(model_detection_cost_nano_cny),0) AS model_detection_cost_nano_cny, COALESCE(SUM(settled_count),0) AS settled_count, COALESCE(SUM(unresolved_count),0) AS unresolved_count"

func channelMonitorProfitSummary(row channelMonitorProfitRow, complete bool) map[string]any {
	profit := row.IncomeNanoCNY - row.Cost
	var rate *float64
	if row.IncomeNanoCNY > 0 {
		value := float64(profit) / float64(row.IncomeNanoCNY)
		rate = &value
	}
	return map[string]any{
		"income_nano_cny": row.IncomeNanoCNY, "wallet_income_nano_cny": row.WalletIncomeNanoCNY,
		"subscription_income_nano_cny": row.SubscriptionIncomeNanoCNY, "pending_income_count": row.PendingIncomeCount,
		"cost_nano_cny": row.Cost, "probe_cost_nano_cny": row.ProbeCost, "model_detection_cost_nano_cny": row.ModelDetectionCostNanoCNY,
		"profit_nano_cny": profit, "profit_rate": rate, "profit_confirmed": complete && row.PendingIncomeCount == 0 && row.PendingCostCount == 0 && row.UnresolvedCount == 0,
		"settled_count": row.SettledCount, "unresolved_count": row.UnresolvedCount,
	}
}

func queryChannelMonitorProfitAnalytics(ctx context.Context, query channelMonitorAnalyticsQuery) (channelMonitorAnalyticsResponse, error) {
	var response channelMonitorAnalyticsResponse
	options := &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		options = &sql.TxOptions{ReadOnly: true}
	}
	err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		response, err = readChannelMonitorProfitAnalytics(ctx, tx, query)
		return err
	}, options)
	return response, err
}

func readChannelMonitorProfitAnalytics(ctx context.Context, db *gorm.DB, query channelMonitorAnalyticsQuery) (channelMonitorAnalyticsResponse, error) {
	var state model.ChannelMonitorIncomeState
	if err := db.WithContext(ctx).First(&state, 1).Error; err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	columns := channelMonitorAnalyticsCostGroupColumns(query.GroupBy)
	selects := []string{channelMonitorProfitSums}
	for _, dimension := range []string{"day_start", "channel_id", "user_id", "api_key_id", "api_key_key", "api_key_name", "model_key", "model_name"} {
		selects = append(selects, channelMonitorAnalyticsSelectDimension(dimension, columns))
	}
	var summaryRow channelMonitorProfitRow
	if err := channelMonitorProfitFacts(ctx, db, query).Select(channelMonitorProfitSums).Scan(&summaryRow).Error; err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	var reasons []string
	if query.From < state.RetainedFrom {
		reasons = append(reasons, "profit_history_expired")
	}
	if query.From < state.StartedAt {
		reasons = append(reasons, "income_history_unavailable")
	}
	if state.GapSince > 0 && query.To > state.GapSince || model.ChannelMonitorIncomeGapSince() > 0 && query.To > model.ChannelMonitorIncomeGapSince() {
		reasons = append(reasons, "income_recording_gap")
	}
	if (query.GroupBy != "channel" && query.GroupBy != "day") || query.hasCostDetailFilter() {
		coverage, err := channelMonitorHistoricalCostDetailCoverageWithDB(ctx, db, query)
		if err != nil {
			return channelMonitorAnalyticsResponse{}, err
		}
		reasons = append(reasons, coverage.Reasons...)
	}
	var pending int64
	backlog := db.WithContext(ctx).Model(&model.ChannelDailyCostOutbox{}).Where("processed_at = 0 AND occurred_at >= ? AND occurred_at < ?", query.From, query.To)
	if query.Channel > 0 {
		backlog = backlog.Where("channel_id = ?", query.Channel)
	}
	if err := backlog.Count(&pending).Error; err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	if pending > 0 {
		reasons = append(reasons, "cost_projection_pending")
	}
	// A failed attempt may have cost without any income. Check the shared
	// producer queue too, before treating a cost-only dimension as complete.
	if common.RedisEnabled {
		client := common.RedisMonitorReadClient()
		if client == nil {
			reasons = append(reasons, "profit_cost_queue_unavailable")
		} else {
			queueCtx, cancel := context.WithTimeout(ctx, time.Second)
			stream := client.XLen(queueCtx, service.ChannelDailyCostRedisStream)
			dead := client.XLen(queueCtx, service.ChannelDailyCostRedisDeadLetter)
			pendingStream := client.XPending(queueCtx, service.ChannelDailyCostRedisStream, service.ChannelDailyCostRedisConsumerGroup)
			cancel()
			if stream.Err() != nil || dead.Err() != nil || (pendingStream.Err() != nil && !strings.Contains(pendingStream.Err().Error(), "NOGROUP")) {
				reasons = append(reasons, "profit_cost_queue_unavailable")
			} else if stream.Val() > 0 || (pendingStream.Err() == nil && pendingStream.Val().Count > 0) {
				reasons = append(reasons, "cost_projection_pending")
			}
			if dead.Err() == nil && dead.Val() > 0 {
				reasons = append(reasons, "profit_cost_unresolved")
			}
		}
	}
	if !common.GetEnvOrDefaultBool("CHANNEL_DAILY_COST_RELIABLE_OUTBOX", true) {
		reasons = append(reasons, "profit_cost_not_durable")
	}
	rowCoverageComplete := len(reasons) == 0
	if summaryRow.PendingIncomeCount > 0 {
		reasons = append(reasons, "income_settlement_pending")
	}
	if summaryRow.UnresolvedCount > 0 {
		reasons = append(reasons, "profit_cost_unresolved")
	}
	if summaryRow.PendingCostCount > 0 {
		reasons = append(reasons, "cost_projection_pending")
	}
	coverage := service.DeriveChannelMonitorCoverage(true, query.From, query.To, max(query.From, state.StartedAt), query.To, reasons)
	complete := coverage.Status == "complete"
	grouped := channelMonitorProfitFacts(ctx, db, query).Select(strings.Join(selects, ", ")).Group(strings.Join(columns, ", "))
	if query.OnlyLoss {
		grouped = grouped.Having("SUM(income_nano_cny) < SUM(cost_nano_cny) AND SUM(pending_income_count) = 0 AND SUM(pending_cost_count) = 0 AND SUM(unresolved_count) = 0")
		if !rowCoverageComplete {
			grouped = grouped.Where("1 = 0")
		}
	}
	var total int64
	if err := db.WithContext(ctx).Table("(?) AS profit_rows", grouped.Session(&gorm.Session{})).Count(&total).Error; err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	order := "SUM(income_nano_cny) - SUM(cost_nano_cny)"
	switch query.Sort {
	case "income":
		order = "SUM(income_nano_cny)"
	case "cost":
		order = "SUM(cost_nano_cny)"
	case "profit_rate":
		order = "(SUM(income_nano_cny) - SUM(cost_nano_cny)) * 1.0 / NULLIF(SUM(income_nano_cny), 0)"
	}
	if query.Sort == "profit_rate" {
		grouped = grouped.Order("CASE WHEN SUM(income_nano_cny) = 0 THEN 1 ELSE 0 END ASC")
	}
	var rows []channelMonitorProfitRow
	if err := grouped.Order(order + " " + query.Direction + ", " + strings.Join(columns, " ASC, ") + " ASC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Scan(&rows).Error; err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	summary := channelMonitorProfitSummary(summaryRow, complete)
	summary["income_started_at"] = state.StartedAt
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := channelMonitorProfitSummary(row, rowCoverageComplete)
		item["day_start"], item["channel_id"], item["user_id"] = row.DayStart, row.ChannelID, row.UserID
		item["api_key_id"], item["api_key_key"], item["api_key_name"] = row.APIKeyID, row.APIKeyKey, row.APIKeyName
		item["model_key"], item["model_name"] = row.ModelKey, row.ModelName
		channelMonitorAnalyticsGroupItem("cost", query.GroupBy, item)
		items = append(items, item)
	}
	if err := attachChannelMonitorAnalyticsUserNames(ctx, items); err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	return channelMonitorAnalyticsResponse{Source: "database_daily", GroupBy: query.GroupBy, Coverage: coverage, Summary: summary, ScopeSummary: summary, Items: items, Page: query.Page, PageSize: query.PageSize, Total: total, GeneratedAt: common.GetTimestamp()}, nil
}
