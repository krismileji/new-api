package controller

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

const channelMonitorAnalyticsMergeLimit = 20000

func channelMonitorAnalyticsCurrentMatch(query channelMonitorAnalyticsQuery, channelID, userID, keyID int, keyName, key, modelName, modelKey string) bool {
	if query.Channel > 0 && channelID != query.Channel || query.hasUserFilter() && userID != query.User || query.hasAPIKeyFilter() && keyID != query.APIKey {
		return false
	}
	if query.APIKeyKey != nil && key != *query.APIKeyKey {
		return false
	}
	if query.ModelKey != nil {
		if modelKey != *query.ModelKey {
			return false
		}
	} else if query.Model != "" && query.Model != modelName && query.Model != modelKey {
		return false
	}
	if query.Search == "" {
		return true
	}
	search := strings.ToLower(query.Search)
	if exact, err := strconv.Atoi(query.Search); err == nil && exact >= 0 && (channelID == exact || userID == exact || keyID == exact) {
		return true
	}
	if slices.Contains(query.SearchUserIDs, userID) || slices.Contains(query.SearchChannelIDs, channelID) {
		return true
	}
	for _, value := range []string{keyName, key, modelName} {
		if strings.Contains(strings.ToLower(value), search) {
			return true
		}
	}
	return false
}

func channelMonitorAnalyticsGroupItem(metric, group string, item map[string]any) string {
	apiKeyID, _ := item["api_key_id"].(int)
	inboundCostKey := metric == "cost" && (group == "api_key" || group == "api_key_channel_model") && apiKeyID > 0
	if inboundCostKey {
		item["api_key_key"] = ""
	}
	var columns []string
	switch group {
	case "day":
		columns = []string{"day_start"}
	case "channel":
		columns = []string{"channel_id"}
	case "user":
		columns = []string{"user_id"}
	case "api_key":
		columns = []string{"api_key_id", "api_key_key", "user_id"}
	case "model":
		columns = []string{"model_key"}
	case "channel_model":
		columns = []string{"channel_id", "model_key"}
	case "api_key_channel_model":
		columns = []string{"api_key_id", "api_key_key", "user_id", "channel_id", "model_key"}
	}
	parts := make([]string, 0, len(columns))
	for _, column := range columns {
		parts = append(parts, fmt.Sprint(item[column]))
	}
	for _, column := range []string{"day_start", "channel_id", "user_id", "api_key_id", "api_key_key", "model_key"} {
		selected := false
		for _, groupColumn := range columns {
			if column == groupColumn {
				selected = true
				break
			}
		}
		if selected {
			continue
		}
		switch column {
		case "api_key_key":
			item[column] = ""
		case "model_key":
			item[column] = ""
			item["model_name"] = ""
		case "api_key_id":
			item[column] = 0
			item["api_key_name"] = ""
		case "day_start":
			item[column] = int64(0)
		default:
			item[column] = 0
		}
	}
	key := strings.Join(parts, ":")
	item["key"] = key
	item["group_by"] = group
	if inboundCostKey {
		delete(item, "api_key_key")
	}
	return key
}

func channelMonitorAnalyticsMergeValues(metric string, target, source map[string]any) error {
	fields := []string{"cost_nano_cny", "probe_cost_nano_cny", "group_probe_cost_nano_cny", "model_detection_cost_nano_cny", "settled_count", "unresolved_count"}
	if metric == "success" {
		fields = []string{"actual_success_count", "actual_failure_count", "final_success_count", "final_failure_count", "cache_hit_count", "cache_sample_count", "cache_read_tokens", "input_tokens", "cache_write_request_count"}
	}
	for _, field := range fields {
		if target[field] == nil {
			target[field] = int64(0)
		}
		if source[field] == nil {
			continue
		}
		delta, ok := source[field].(int64)
		if !ok {
			return fmt.Errorf("渠道监控聚合指标类型无效: %s", field)
		}
		total, _ := target[field].(int64)
		if err := channelMonitorAddNonNegativeInt64(&total, delta); err != nil {
			return err
		}
		target[field] = total
	}
	if metric == "success" {
		actual, _ := target["actual_success_count"].(int64)
		actualFailure, _ := target["actual_failure_count"].(int64)
		final, _ := target["final_success_count"].(int64)
		finalFailure, _ := target["final_failure_count"].(int64)
		actualTotal, finalTotal := actual, final
		if err := channelMonitorAddNonNegativeInt64(&actualTotal, actualFailure); err != nil {
			return err
		}
		if err := channelMonitorAddNonNegativeInt64(&finalTotal, finalFailure); err != nil {
			return err
		}
		target["actual_sample_count"], target["final_sample_count"] = actualTotal, finalTotal
		target["actual_success_rate"] = channelMonitorAnalyticsRate(actual, actualTotal)
		target["final_success_rate"] = channelMonitorAnalyticsRate(final, finalTotal)
		hits, _ := target["cache_hit_count"].(int64)
		samples, _ := target["cache_sample_count"].(int64)
		read, _ := target["cache_read_tokens"].(int64)
		input, _ := target["input_tokens"].(int64)
		target["cache_hit_rate"] = channelMonitorAnalyticsRate(hits, samples)
		target["cache_utilization_rate"] = channelMonitorAnalyticsRate(read, input)
	}
	return nil
}

func channelMonitorAnalyticsPage(ctx context.Context, query channelMonitorAnalyticsQuery, rows []map[string]any, summary map[string]any, source string, coverage service.ChannelMonitorCoverage) (channelMonitorAnalyticsResponse, error) {
	if err := channelMonitorAnalyticsMergeValues(query.Metric, summary, nil); err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	sortKey := query.Sort
	aliases := map[string]string{"samples": "actual_sample_count", "success": "actual_success_count", "failure": "actual_failure_count", "success_rate": "actual_success_rate", "cache_tokens": "cache_read_tokens", "cache_utilization": "cache_utilization_rate", "cache_write": "cache_write_request_count", "cost": "cost_nano_cny", "settled": "settled_count", "unresolved": "unresolved_count"}
	if field, exists := aliases[sortKey]; exists {
		sortKey = field
	}
	if query.Metric == "success" && query.SuccessMode == "final" && strings.HasPrefix(sortKey, "actual_") {
		sortKey = "final_" + strings.TrimPrefix(sortKey, "actual_")
	}
	if sortKey == "" {
		sortKey = "cost_nano_cny"
		if query.Metric == "success" {
			sortKey = "actual_sample_count"
		}
	}
	if query.Metric == "cost" && sortKey == "resolution_rate" {
		for _, row := range rows {
			settled, _ := row["settled_count"].(int64)
			unresolved, _ := row["unresolved_count"].(int64)
			total := settled
			if err := channelMonitorAddNonNegativeInt64(&total, unresolved); err != nil {
				return channelMonitorAnalyticsResponse{}, err
			}
			row["resolution_rate"] = channelMonitorAnalyticsRate(settled, total)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		left, li := rows[i][sortKey].(int64)
		right, ri := rows[j][sortKey].(int64)
		if li && ri && left != right {
			if query.Direction == "asc" {
				return left < right
			}
			return left > right
		}
		lf, _ := rows[i][sortKey].(float64)
		rf, _ := rows[j][sortKey].(float64)
		if lf != rf {
			if query.Direction == "asc" {
				return lf < rf
			}
			return lf > rf
		}
		return fmt.Sprint(rows[i]["key"]) < fmt.Sprint(rows[j]["key"])
	})
	total := len(rows)
	pageSize := max(query.PageSize, 1)
	start := min(max(query.Page-1, 0)*pageSize, total)
	end := min(start+pageSize, total)
	items := rows[start:end]
	if err := attachChannelMonitorAnalyticsUserNames(ctx, items); err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	return channelMonitorAnalyticsResponse{
		Source: source, GroupBy: query.GroupBy, Coverage: coverage, Summary: summary, ScopeSummary: summary,
		Items: items, Page: query.Page, PageSize: query.PageSize, Total: int64(total), GeneratedAt: common.GetTimestamp(),
	}, nil
}

func queryChannelMonitorCurrentCostAnalytics(ctx context.Context, query channelMonitorAnalyticsQuery) (channelMonitorAnalyticsResponse, error) {
	view, err := service.QueryChannelMonitorRedisDailyCosts(ctx, query.From)
	if err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	details := view.Details
	channelTotals := false
	// Unfiltered channel/day totals include legacy amounts without a detailed
	// attribution. Filtered drill-downs always use the frozen detailed scopes.
	if (query.GroupBy == "day" || query.GroupBy == "channel") && !query.hasCostDetailFilter() {
		channelTotals = true
		details = make([]model.ChannelMonitorDailyCostDetail, 0, len(view.Channels))
		for id, total := range view.Channels {
			details = append(details, model.ChannelMonitorDailyCostDetail{
				DayStart: query.From, ChannelId: id, CostNanoCNY: total.SettledCostNanoCNY,
				ProbeCostNanoCNY: total.ProbeSettledCostNanoCNY, GroupProbeCostNanoCNY: total.GroupProbeSettledCostNanoCNY,
				SettledCount: total.SettledRequestCount, UnresolvedCount: total.UnresolvedRequestCount,
			})
		}
	}
	groups := make(map[string]map[string]any)
	summary := make(map[string]any)
	matchQuery := query
	probeSource := ""
	if query.APIKeyKey != nil && channelMonitorAnalyticsSystemAPIKey(*query.APIKeyKey) {
		probeSource = *query.APIKeyKey
		matchQuery.APIKeyKey = nil
	}
	for _, row := range details {
		if probeSource != "" && (row.APIKeyId != 0 || row.SourceKind != probeSource) {
			continue
		}
		if !channelMonitorAnalyticsCurrentMatch(matchQuery, row.ChannelId, row.UserId, row.APIKeyId, row.APIKeyName, row.APIKeyKey, row.ModelName, row.ModelKey) {
			continue
		}
		detectionCost := int64(0)
		if channelTotals {
			detectionCost = view.Channels[row.ChannelId].ModelDetectionSettledCostNanoCNY
		} else if row.SourceKind == string(model.ChannelMonitorEventSourceModelDetection) {
			detectionCost = row.CostNanoCNY
		}
		item := map[string]any{
			"day_start": row.DayStart, "channel_id": row.ChannelId, "user_id": row.UserId,
			"user_attribution": row.UserAttribution, "api_key_id": row.APIKeyId,
			"api_key_key":  channelMonitorAnalyticsCostAPIKeyIdentity(row.APIKeyId, row.APIKeyKey, row.SourceKind),
			"api_key_name": row.APIKeyName, "model_key": row.ModelKey, "model_name": row.ModelName,
			"cost_nano_cny": row.CostNanoCNY, "probe_cost_nano_cny": row.ProbeCostNanoCNY,
			"group_probe_cost_nano_cny":     row.GroupProbeCostNanoCNY,
			"model_detection_cost_nano_cny": detectionCost,
			"settled_count":                 row.SettledCount, "unresolved_count": row.UnresolvedCount,
		}
		key := channelMonitorAnalyticsGroupItem("cost", query.GroupBy, item)
		if previous := groups[key]; previous != nil {
			if err := channelMonitorAnalyticsMergeValues("cost", previous, item); err != nil {
				return channelMonitorAnalyticsResponse{}, err
			}
		} else {
			groups[key] = item
		}
		if err := channelMonitorAnalyticsMergeValues("cost", summary, item); err != nil {
			return channelMonitorAnalyticsResponse{}, err
		}
	}
	rows := make([]map[string]any, 0, len(groups))
	for _, row := range groups {
		rows = append(rows, row)
	}
	through := max(query.From, view.DataCutoffAt)
	var reasons []string
	if view.Projection.Failed || view.Projection.CheckedAt == 0 || common.GetTimestamp()-view.Projection.CheckedAt > 10 {
		reasons = append(reasons, "cost_projection_unavailable")
	} else if view.Projection.Pending {
		reasons = append(reasons, "cost_projection_pending")
	}
	if !channelTotals && view.AttributionPartial {
		reasons = append(reasons, "cost_attribution_incomplete")
	}
	coverage := service.DeriveChannelMonitorCoverage(true, query.From, through, query.From, through, reasons)
	response, err := channelMonitorAnalyticsPage(ctx, query, rows, summary, "redis_daily", coverage)
	response.SnapshotRevision, response.ProcessedAt = view.Revision, view.ProcessedAt
	return response, err
}

func queryChannelMonitorCurrentSuccessFacts(ctx context.Context, query channelMonitorAnalyticsQuery, view service.ChannelMonitorRedisDailySuccessAnalyticsView) (channelMonitorAnalyticsResponse, error) {
	groups := make(map[string]map[string]any)
	summary := make(map[string]any)
	for _, row := range view.Facts {
		if !channelMonitorAnalyticsCurrentMatch(query, row.ChannelID, row.UserID, row.APIKeyID, row.APIKeyName, row.APIKeyKey, row.ModelName, row.ModelKey) {
			continue
		}
		value := row.Aggregate
		item := channelMonitorAnalyticsSuccessItem(query.GroupBy, channelMonitorAnalyticsSuccessRow{
			DayStart: query.From, ChannelID: row.ChannelID, UserID: row.UserID, UserAttribution: row.UserAttribution,
			APIKeyID: row.APIKeyID, APIKeyKey: row.APIKeyKey, APIKeyName: row.APIKeyName, ModelKey: row.ModelKey, ModelName: row.ModelName,
			ActualSuccess: value.ActualSuccessCount, ActualFailure: value.ActualFailureCount,
			FinalSuccess: value.FinalSuccessCount, FinalFailure: value.FinalFailureCount,
			CacheHit: value.CacheHitCount, CacheSample: value.CacheSampleCount, CacheReadTokens: value.CacheReadTokens,
			InputTokens: value.InputTokens, CacheWriteCount: value.CacheWriteRequestCount,
		})
		key := channelMonitorAnalyticsGroupItem("success", query.GroupBy, item)
		if previous := groups[key]; previous != nil {
			if err := channelMonitorAnalyticsMergeValues("success", previous, item); err != nil {
				return channelMonitorAnalyticsResponse{}, err
			}
		} else {
			groups[key] = item
		}
		if err := channelMonitorAnalyticsMergeValues("success", summary, item); err != nil {
			return channelMonitorAnalyticsResponse{}, err
		}
	}
	rows := make([]map[string]any, 0, len(groups))
	for _, row := range groups {
		rows = append(rows, row)
	}
	response, err := channelMonitorAnalyticsPage(ctx, query, rows, summary, "redis_daily", channelMonitorCurrentDayCoverage(ctx, query.From, view.DataCutoffAt, view.CoveragePartial))
	response.SnapshotRevision, response.ProcessedAt = view.Revision, view.ProcessedAt
	return response, err
}

func queryChannelMonitorMixedAnalytics(ctx context.Context, query channelMonitorAnalyticsQuery, today int64) (channelMonitorAnalyticsResponse, error) {
	historyQuery := query
	historyQuery.To = today
	historyQuery.Page, historyQuery.PageSize = 1, channelMonitorAnalyticsMergeLimit
	var history channelMonitorAnalyticsResponse
	var err error
	if query.Metric == "cost" {
		history, err = queryChannelMonitorHistoricalCostAnalytics(ctx, historyQuery)
	} else {
		history, err = queryChannelMonitorHistoricalSuccessAnalytics(ctx, historyQuery)
	}
	if err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	currentQuery := historyQuery
	currentQuery.From, currentQuery.To = today, query.To
	var current channelMonitorAnalyticsResponse
	if query.Metric == "cost" {
		current, err = queryChannelMonitorCurrentCostAnalytics(ctx, currentQuery)
	} else {
		current, err = queryChannelMonitorCurrentSuccessAnalytics(ctx, currentQuery)
	}
	if err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	if history.Total > channelMonitorAnalyticsMergeLimit || current.Total > channelMonitorAnalyticsMergeLimit {
		return channelMonitorAnalyticsResponse{}, &channelMonitorAnalyticsQueryError{"跨日分析维度较多，请缩小筛选范围"}
	}
	groups := make(map[string]map[string]any)
	for _, row := range append(history.Items, current.Items...) {
		if query.GroupBy == "day" && row["day_start"] == nil {
			day, err := strconv.ParseInt(fmt.Sprint(row["key"]), 10, 64)
			if err != nil {
				return channelMonitorAnalyticsResponse{}, err
			}
			row["day_start"] = day
		}
		key := channelMonitorAnalyticsGroupItem(query.Metric, query.GroupBy, row)
		if previous := groups[key]; previous != nil {
			if err := channelMonitorAnalyticsMergeValues(query.Metric, previous, row); err != nil {
				return channelMonitorAnalyticsResponse{}, err
			}
		} else {
			groups[key] = row
		}
	}
	summary := make(map[string]any)
	if err := channelMonitorAnalyticsMergeValues(query.Metric, summary, history.Summary); err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	if err := channelMonitorAnalyticsMergeValues(query.Metric, summary, current.Summary); err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	rows := make([]map[string]any, 0, len(groups))
	for _, row := range groups {
		rows = append(rows, row)
	}
	coverage := current.Coverage
	coverage.CoveredFrom = history.Coverage.CoveredFrom
	coverage.Reasons = append(append([]string(nil), history.Coverage.Reasons...), current.Coverage.Reasons...)
	if history.Coverage.Status != service.ChannelMonitorCoverageComplete || len(coverage.Reasons) > 0 {
		coverage.Status = service.ChannelMonitorCoveragePartial
	}
	response, err := channelMonitorAnalyticsPage(ctx, query, rows, summary, "redis_and_database_daily", coverage)
	response.SnapshotRevision, response.ProcessedAt = current.SnapshotRevision, current.ProcessedAt
	return response, err
}
