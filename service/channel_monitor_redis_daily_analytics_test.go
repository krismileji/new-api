package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestQueryChannelMonitorRedisDailySuccessAnalyticsBuildsDrilldownRows(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	event := newChannelMonitorRedisSharedProjectionTestEvent("daily-analytics", 1_750_000_000)
	event.UserId = 31
	event.UserAttribution = model.ChannelMonitorEventUserAttributionRequest
	event.APIKeyId = 201
	event.APIKeyName = "生产 Key"
	event.ModelName = "gpt-4.1"
	require.NoError(t, projection.HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event}))

	view, err := queryChannelMonitorRedisDailySuccessAnalyticsWithClient(
		context.Background(), client, model.ChannelDailyCostDayStart(event.OccurredAt),
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), view.Summary.ActualSuccessCount)

	var foundChannelUserKey bool
	var foundAPIKeyRoute bool
	for _, row := range view.Rows {
		if row.ChannelID == event.ChannelId && row.UserID == event.UserId && row.APIKeyID == event.APIKeyId && row.ModelName == "" {
			foundChannelUserKey = true
			require.Equal(t, int64(1), row.Aggregate.ActualSuccessCount)
		}
		if row.ChannelID == event.ChannelId && row.UserID == event.UserId && row.APIKeyID == event.APIKeyId && row.ModelName == event.ModelName {
			foundAPIKeyRoute = true
			require.Equal(t, event.APIKeyName, row.APIKeyName)
			require.Equal(t, int64(1), row.Aggregate.ActualSuccessCount)
		}
	}
	require.True(t, foundChannelUserKey)
	require.True(t, foundAPIKeyRoute)
}

func TestQueryChannelMonitorRedisDailySuccessAnalyticsIgnoresEmptyModelRoutes(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	event := newChannelMonitorRedisSharedProjectionTestEvent("daily-empty-model", 1_750_000_000)
	event.UserId = 31
	event.APIKeyId = 201
	event.APIKeyName = "生产 Key"
	event.ModelName = ""
	require.NoError(t, projection.HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event}))

	view, err := queryChannelMonitorRedisDailySuccessAnalyticsWithClient(
		context.Background(), client, model.ChannelDailyCostDayStart(event.OccurredAt),
	)
	require.NoError(t, err)

	channelRows := 0
	apiKeyRows := 0
	for _, row := range view.Rows {
		if row.ChannelID == event.ChannelId && row.UserID == 0 && row.APIKeyID == 0 && row.ModelName == "" {
			channelRows++
		}
		if row.ChannelID == 0 && row.UserID == 0 && row.APIKeyID == event.APIKeyId && row.ModelName == "" {
			apiKeyRows++
		}
		require.False(t, row.ChannelID == event.ChannelId && row.APIKeyID == event.APIKeyId && row.ModelName != "", "empty model event produced a route row: %+v", row)
	}
	require.Equal(t, 1, channelRows)
	require.Equal(t, 1, apiKeyRows)
}
