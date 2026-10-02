package controller

import (
	"context"
	"database/sql"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"gorm.io/gorm"
)

type channelMonitorProfitRow struct {
	GroupProbeCost            int64  `gorm:"column:group_probe_cost_nano_cny"`
	CoverageIncomplete        int64  `gorm:"column:coverage_incomplete"`
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
		Select("day_start, channel_id, user_id, api_key_id, api_key_key, api_key_name, model_key, model_name, 'business' AS source_kind, CASE WHEN status = 'settled' THEN income_nano_cny ELSE 0 END AS income_nano_cny, CASE WHEN status = 'settled' AND billing_source = 'wallet' THEN income_nano_cny ELSE 0 END AS wallet_income_nano_cny, CASE WHEN status = 'settled' AND billing_source = 'subscription' THEN income_nano_cny ELSE 0 END AS subscription_income_nano_cny, CASE WHEN status = 'settled' THEN 0 ELSE 1 END AS pending_income_count, CASE WHEN cost_recorded = 1 THEN 0 ELSE 1 END AS pending_cost_count, 0 AS cost_nano_cny, 0 AS probe_cost_nano_cny, 0 AS group_probe_cost_nano_cny, 0 AS model_detection_cost_nano_cny, 0 AS settled_count, 0 AS unresolved_count")
	cost := db.WithContext(ctx).Model(&model.ChannelMonitorDailyCostDetail{}).
		Where("day_start >= ? AND day_start < ?", query.From, query.To).
		Select("day_start, channel_id, user_id, api_key_id, api_key_key, api_key_name, model_key, model_name, source_kind, 0 AS income_nano_cny, 0 AS wallet_income_nano_cny, 0 AS subscription_income_nano_cny, 0 AS pending_income_count, 0 AS pending_cost_count, cost_nano_cny, probe_cost_nano_cny, group_probe_cost_nano_cny, CASE WHEN source_kind = 'model_detection' THEN cost_nano_cny ELSE 0 END AS model_detection_cost_nano_cny, settled_count, unresolved_count")
	if (query.GroupBy == "channel" || query.GroupBy == "day") && !query.hasCostDetailFilter() {
		cost = db.WithContext(ctx).Model(&model.ChannelDailyCost{}).
			Where("day_start >= ? AND day_start < ?", query.From, query.To).
			Select("day_start, channel_id, 0 AS user_id, 0 AS api_key_id, '' AS api_key_key, '' AS api_key_name, '' AS model_key, '' AS model_name, '' AS source_kind, 0 AS income_nano_cny, 0 AS wallet_income_nano_cny, 0 AS subscription_income_nano_cny, 0 AS pending_income_count, 0 AS pending_cost_count, cost_nano_cny, probe_cost_nano_cny, group_probe_cost_nano_cny, model_detection_cost_nano_cny, settled_count, unresolved_count")
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

const channelMonitorProfitSums = "COALESCE(SUM(income_nano_cny),0) AS income_nano_cny, COALESCE(SUM(wallet_income_nano_cny),0) AS wallet_income_nano_cny, COALESCE(SUM(subscription_income_nano_cny),0) AS subscription_income_nano_cny, COALESCE(SUM(pending_income_count),0) AS pending_income_count, COALESCE(SUM(pending_cost_count),0) AS pending_cost_count, COALESCE(SUM(cost_nano_cny),0) AS cost_nano_cny, COALESCE(SUM(probe_cost_nano_cny),0) AS probe_cost_nano_cny, COALESCE(SUM(group_probe_cost_nano_cny),0) AS group_probe_cost_nano_cny, COALESCE(SUM(model_detection_cost_nano_cny),0) AS model_detection_cost_nano_cny, COALESCE(SUM(settled_count),0) AS settled_count, COALESCE(SUM(unresolved_count),0) AS unresolved_count"

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
		"cost_nano_cny": row.Cost, "probe_cost_nano_cny": row.ProbeCost, "group_probe_cost_nano_cny": row.GroupProbeCost, "model_detection_cost_nano_cny": row.ModelDetectionCostNanoCNY,
		"profit_nano_cny": profit, "profit_rate": rate, "profit_confirmed": complete && row.PendingIncomeCount == 0 && row.PendingCostCount == 0 && row.UnresolvedCount == 0,
		"settled_count": row.SettledCount, "unresolved_count": row.UnresolvedCount,
	}
}

func queryChannelMonitorProfitAnalytics(ctx context.Context, query channelMonitorAnalyticsQuery) (channelMonitorAnalyticsResponse, error) {
	queue := service.ReadChannelMonitorProfitCostQueue(ctx)
	journal, journalErr := model.ReadChannelMonitorIncomeGapJournal()
	if journalErr != nil {
		queue = append(queue, service.ChannelMonitorProfitBlock{Reason: "income_recording_gap"})
	}
	for _, gap := range journal {
		queue = append(queue, service.ChannelMonitorProfitBlock{From: gap.From, To: gap.To, ChannelID: gap.ChannelID, Reason: "income_recording_gap"})
	}
	// Capture pending markers before the DB snapshot too: flushing one while
	// reading must not hide it from both memory and the transaction's view.
	for _, gap := range model.PendingChannelMonitorIncomeGaps() {
		queue = append(queue, service.ChannelMonitorProfitBlock{From: gap.From, To: gap.To, ChannelID: gap.ChannelID, Reason: "income_recording_gap"})
	}
	var response channelMonitorAnalyticsResponse
	options := &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		options = &sql.TxOptions{ReadOnly: true}
	}
	err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		response, err = readChannelMonitorProfitAnalytics(ctx, tx, query, queue)
		return err
	}, options)
	return response, err
}

func readChannelMonitorProfitAnalytics(ctx context.Context, db *gorm.DB, query channelMonitorAnalyticsQuery, queue []service.ChannelMonitorProfitBlock) (channelMonitorAnalyticsResponse, error) {
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
	reasons, incompleteSQL, err := channelMonitorProfitCoverage(ctx, db, query, state, queue)
	if err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	selects = append(selects, incompleteSQL+" AS coverage_incomplete")
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
		if incompleteSQL != "0" {
			grouped = grouped.Having(incompleteSQL + " = 0")
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
		item := channelMonitorProfitSummary(row, row.CoverageIncomplete == 0)
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
