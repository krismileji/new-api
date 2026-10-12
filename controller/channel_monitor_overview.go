package controller

import (
	"context"
	"maps"
	"sort"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

// The two ledgers have different row identities and orders. Join the complete,
// bounded dimension sets before filtering or paging, including request-only
// failures and cost-only probes. Never join two already paginated results.
func queryChannelMonitorOverviewAnalytics(ctx context.Context, query channelMonitorAnalyticsQuery) (channelMonitorAnalyticsResponse, error) {
	profitQuery, requestQuery := query, query
	profitQuery.Metric, profitQuery.Sort = "profit", "profit"
	requestQuery.Metric, requestQuery.Sort = "success", "samples"
	profitQuery.Page, requestQuery.Page = 1, 1
	profitQuery.PageSize, requestQuery.PageSize = channelMonitorAnalyticsMergeLimit, channelMonitorAnalyticsMergeLimit
	profitQuery.OnlyLoss, requestQuery.OnlyLoss = false, false
	if query.APIKey > 0 {
		// Real inbound keys are joined by user and ID, not by the different
		// fingerprints used by the cost and request projections.
		profitQuery.APIKeyKey, requestQuery.APIKeyKey = nil, nil
	}
	if query.ModelKey != nil {
		modelName := query.Model
		if *query.ModelKey == "unknown" {
			modelName = ""
		} else if model.ChannelMonitorDailyCostModelKey(modelName) != *query.ModelKey {
			return channelMonitorAnalyticsResponse{}, &channelMonitorAnalyticsQueryError{"合并概览的模型标识需要对应的模型名称"}
		}
		identity := model.ChannelMonitorDailyMetricIdentity{Model: modelName}
		modelKey := identity.LedgerRow(0).ModelKey
		requestQuery.ModelKey = &modelKey
	}
	profit, err := queryChannelMonitorHistoricalAnalytics(ctx, profitQuery)
	if err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	requests, err := queryChannelMonitorHistoricalAnalytics(ctx, requestQuery)
	if err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	if profit.Total > channelMonitorAnalyticsMergeLimit || requests.Total > channelMonitorAnalyticsMergeLimit {
		return channelMonitorAnalyticsResponse{}, &channelMonitorAnalyticsQueryError{"合并概览维度较多，请缩小日期或搜索范围"}
	}
	groups := make(map[string]map[string]any, len(profit.Items))
	for _, item := range profit.Items {
		if err := channelMonitorAnalyticsMergeValues("success", item, nil); err != nil {
			return channelMonitorAnalyticsResponse{}, err
		}
		item["request_metrics_available"] = requests.Coverage.Status == service.ChannelMonitorCoverageComplete
		groups[channelMonitorAnalyticsGroupItem("cost", query.GroupBy, item)] = item
	}
	for _, item := range requests.Items {
		if query.GroupBy == "model" || query.GroupBy == "channel_model" || query.GroupBy == "api_key_channel_model" {
			modelName, _ := item["model_name"].(string)
			item["model_key"] = model.ChannelMonitorDailyCostModelKey(modelName)
		}
		key := channelMonitorAnalyticsGroupItem("cost", query.GroupBy, item)
		target := groups[key]
		if target == nil {
			target = maps.Clone(item)
			if profit.Coverage.Status == service.ChannelMonitorCoverageComplete {
				maps.Copy(target, channelMonitorProfitSummary(channelMonitorProfitRow{}, true))
			} else {
				// Missing finance rows in partial history do not prove zero cost.
				target["profit_confirmed"] = false
			}
			groups[key] = target
		} else if err := channelMonitorAnalyticsMergeValues("success", target, item); err != nil {
			return channelMonitorAnalyticsResponse{}, err
		}
		target["request_metrics_available"] = requests.Coverage.Status != service.ChannelMonitorCoverageUnavailable
	}
	if len(groups) > channelMonitorAnalyticsMergeLimit {
		return channelMonitorAnalyticsResponse{}, &channelMonitorAnalyticsQueryError{"合并概览维度较多，请缩小日期或搜索范围"}
	}
	rows := make([]map[string]any, 0, len(groups))
	for _, item := range groups {
		if query.OnlyLoss {
			confirmed, _ := item["profit_confirmed"].(bool)
			amount, _ := item["profit_nano_cny"].(int64)
			if !confirmed || amount >= 0 {
				continue
			}
		}
		rows = append(rows, item)
	}
	sortKey := map[string]string{
		"income": "income_nano_cny", "cost": "cost_nano_cny", "profit": "profit_nano_cny", "profit_rate": "profit_rate",
		"samples": "actual_sample_count", "success": "actual_success_count", "failure": "actual_failure_count",
		"success_rate": "actual_success_rate", "cache_tokens": "cache_read_tokens", "cache_utilization": "cache_utilization_rate", "cache_write": "cache_write_request_count",
	}[query.Sort]
	requestSort := channelMonitorAnalyticsSortAllowed("success", query.Sort)
	sort.Slice(rows, func(i, j int) bool {
		left, right := rows[i][sortKey], rows[j][sortKey]
		if requestSort {
			if rows[i]["request_metrics_available"] == false {
				left = nil
			}
			if rows[j]["request_metrics_available"] == false {
				right = nil
			}
		}
		leftValue, leftAvailable := channelMonitorOverviewSortValue(left)
		rightValue, rightAvailable := channelMonitorOverviewSortValue(right)
		if leftAvailable != rightAvailable {
			return leftAvailable
		}
		if leftInteger, ok := left.(int64); ok {
			if rightInteger, ok := right.(int64); ok && leftInteger != rightInteger {
				if query.Direction == "asc" {
					return leftInteger < rightInteger
				}
				return leftInteger > rightInteger
			}
		}
		if leftValue != rightValue {
			if query.Direction == "asc" {
				return leftValue < rightValue
			}
			return leftValue > rightValue
		}
		return rows[i]["key"].(string) < rows[j]["key"].(string)
	})
	pageSize := max(query.PageSize, 1)
	start := min(max(query.Page-1, 0), len(rows)/pageSize+1) * pageSize
	start = min(start, len(rows))
	end := min(start+pageSize, len(rows))
	summary := maps.Clone(profit.ScopeSummary)
	maps.Copy(summary, requests.ScopeSummary)
	summary["request_metrics_available"] = requests.Coverage.Status != service.ChannelMonitorCoverageUnavailable
	profit.ScopeSummary, profit.Summary = summary, summary
	profit.Items, profit.Total = rows[start:end], int64(len(rows))
	profit.Page, profit.PageSize = query.Page, pageSize
	profit.RequestCoverage = &requests.Coverage
	profit.Source, profit.ProcessedAt, profit.SnapshotRevision = requests.Source, requests.ProcessedAt, requests.SnapshotRevision
	return profit, nil
}

func channelMonitorOverviewSortValue(value any) (float64, bool) {
	switch number := value.(type) {
	case int64:
		return float64(number), true
	case float64:
		return number, true
	case *float64:
		if number != nil {
			return *number, true
		}
	}
	return 0, false
}
