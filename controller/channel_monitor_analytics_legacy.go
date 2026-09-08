package controller

import (
	"context"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

// Old hashes contain overlapping rollups rather than frozen facts. Select
// exactly one rollup level, then group its contributions for the requested
// view. Missing historical ownership must never widen a scoped query.
func queryChannelMonitorLegacySuccessAnalytics(ctx context.Context, query channelMonitorAnalyticsQuery, view service.ChannelMonitorRedisDailySuccessAnalyticsView) (channelMonitorAnalyticsResponse, error) {
	type route struct {
		channel, key int
		model        string
	}
	userScopedRoutes := make(map[route]bool)
	if query.GroupBy == "api_key_channel_model" {
		for _, row := range view.Rows {
			if row.UserID > 0 && row.APIKeyID > 0 && row.ChannelID > 0 && row.ModelName != "" {
				userScopedRoutes[route{row.ChannelID, row.APIKeyID, row.ModelName}] = true
			}
		}
	}
	for _, row := range view.Rows {
		if row.UserID == 0 && userScopedRoutes[route{row.ChannelID, row.APIKeyID, row.ModelName}] {
			continue
		}
		if row.ModelName != "" {
			identity := model.ChannelMonitorDailyMetricIdentity{Model: row.ModelName}
			row.ModelKey = identity.LedgerRow(view.DayStart).ModelKey
		}
		if row.APIKeyID > 0 {
			row.APIKeyKey = model.ChannelMonitorDailyMetricIdentityFromEvent(model.ChannelMonitorEvent{APIKeyId: row.APIKeyID, APIKeyName: row.APIKeyName}).APIKeyKey
		}
		if channelMonitorAnalyticsCurrentRowMatches(query, row) {
			view.Facts = append(view.Facts, row)
		}
	}
	response, err := queryChannelMonitorCurrentSuccessFacts(ctx, query, view)
	if err != nil {
		return response, err
	}
	response.Coverage.Status = service.ChannelMonitorCoveragePartial
	response.Coverage.Reasons = append(response.Coverage.Reasons, "daily_legacy_attribution_incomplete")
	return response, nil
}
