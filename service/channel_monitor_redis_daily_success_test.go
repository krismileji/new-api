package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorRedisDailySuccessProjectionUsesOneDayKeyAndUserScopes(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	occurredAt := int64(1_750_000_000)
	event := newChannelMonitorRedisSharedProjectionTestEvent("daily-success", occurredAt)
	event.UserId = 11
	event.UserAttribution = model.ChannelMonitorEventUserAttributionRequest
	event.IsStream = true
	inputTokens := int64(100)
	cacheReadTokens := int64(25)
	event.InputTokens = &inputTokens
	event.CacheReadTokens = &cacheReadTokens

	require.NoError(t, projection.HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event}))
	require.NoError(t, projection.HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event}))

	dayStart := model.ChannelDailyCostDayStart(occurredAt)
	dayKey := ChannelMonitorRedisSuccessDayKey(dayStart)
	assert.Equal(t, int64(1), client.Exists(context.Background(), dayKey).Val())

	view, err := queryChannelMonitorRedisDailySuccessWithClient(
		context.Background(), client, dayStart,
		[]string{"meta:*", "global:*", "channel:*", "user:*", "channel_user:*", "apikey:*", "user_api_key:*", "channel_user_api_key:*", "user_api_key_route:*", "route:*"},
	)
	require.NoError(t, err)
	assert.Equal(t, dayStart, view.DayStart)
	assert.Positive(t, view.ProcessedAt)

	byScope := make(map[string]ChannelMonitorRedisDailySuccessEntry, len(view.Entries))
	for _, entry := range view.Entries {
		byScope[entry.Scope+":"+entry.Identity] = entry
	}
	assert.Equal(t, int64(1), byScope["global:"].Aggregate.ActualSuccessCount)
	assert.Equal(t, int64(1), byScope["channel:7"].Aggregate.ActualSuccessCount)
	assert.Equal(t, int64(1), byScope["user:11"].Aggregate.ActualSuccessCount)
	assert.Equal(t, int64(1), byScope["channel_user:7.11"].Aggregate.ActualSuccessCount)
	assert.Equal(t, int64(1), byScope["apikey:42"].Aggregate.ActualSuccessCount)
	assert.Equal(t, int64(1), byScope["channel_user_api_key:7.11.42"].Aggregate.ActualSuccessCount)
	assert.Equal(t, int64(1), byScope["user_api_key_route:11.42.7.Z3B0LXRlc3Q"].Aggregate.ActualSuccessCount)
	assert.Equal(t, int64(1), byScope["route:7.Z3B0LXRlc3Q"].Aggregate.ActualSuccessCount)
	assert.Equal(t, int64(1), byScope["global:"].Aggregate.CacheHitCount)
	assert.Equal(t, inputTokens, byScope["global:"].Aggregate.InputTokens)
}
