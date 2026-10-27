package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
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

func TestChannelMonitorRequestStatsProjectionCountsDispatchedAttemptsAndKnownInputOnly(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	const occurredAt = int64(1_750_000_000)

	belowThreshold := int64(9_999)
	atThreshold := int64(10_000)
	streamSuccess := newChannelMonitorRedisSharedProjectionTestEvent("request-stats-stream-success", occurredAt)
	streamSuccess.EventSequence = 1
	streamSuccess.UserId = 11
	streamSuccess.IsStream = true
	streamSuccess.ReliableInputTokens = &belowThreshold

	nonStreamRetry := newChannelMonitorRedisSharedProjectionTestEvent("request-stats-non-stream-retry", occurredAt+1)
	nonStreamRetry.EventSequence = 2
	nonStreamRetry.UserId = 11
	nonStreamRetry.Outcome = model.ChannelMonitorEventOutcomeFailure
	nonStreamRetry.IsFinalAttempt = false
	nonStreamRetry.IsStream = false

	streamFailure := newChannelMonitorRedisSharedProjectionTestEvent("request-stats-stream-failure", occurredAt+2)
	streamFailure.EventSequence = 3
	streamFailure.UserId = 11
	streamFailure.Outcome = model.ChannelMonitorEventOutcomeFailure
	streamFailure.IsStream = true
	streamFailure.ReliableInputTokens = &atThreshold

	localResponse := newChannelMonitorRedisSharedProjectionTestEvent("request-stats-local-response", occurredAt+3)
	localResponse.EventSequence = 4
	localResponse.Source = model.ChannelMonitorEventSourceLocalResponse
	localResponse.RequestDispatched = false
	localResponse.SchedulingEligible = false

	finalRetrySummary := newChannelMonitorRedisSharedProjectionTestEvent("request-stats-final-retry-summary", occurredAt+4)
	finalRetrySummary.EventSequence = 5
	finalRetrySummary.FinalRetrySummary = true
	finalRetrySummary.RequestDispatched = false
	finalRetrySummary.IsFinalAttempt = false
	finalRetrySummary.Outcome = model.ChannelMonitorEventOutcomeFailure

	events := []model.ChannelMonitorEvent{streamSuccess, nonStreamRetry, streamFailure, localResponse, finalRetrySummary}
	require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), events))
	require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), events))

	view, err := queryChannelMonitorRedisDailySuccessWithClient(context.Background(), client,
		model.ChannelDailyCostDayStart(occurredAt), []string{"fact:*"})
	require.NoError(t, err)
	var distribution channelMonitorRequestDistribution
	for _, entry := range view.Entries {
		if entry.Scope == "fact" {
			distribution, err = mergeRequestStatsDistribution(distribution, entry.Aggregate.RequestStats)
			require.NoError(t, err)
		}
	}
	assert.Equal(t, int64(3), distribution["total"], "replays, local responses, and final retry summaries must not add attempts")
	assert.Equal(t, int64(2), distribution["stream"])
	assert.Equal(t, int64(2), distribution["known"])
	assert.Equal(t, int64(1), requestStatsLowCount(distribution, 10), "10,000 tokens is not strictly below 10K")
	assert.Equal(t, int64(2), requestStatsLowCount(distribution, 11))
	require.NoError(t, validateRequestStatsDistribution(distribution))
}

func TestRequestStatsAutomaticDisableUsesInclusiveRateAndMinimumSamples(t *testing.T) {
	config := model.ChannelMonitorRequestStatsConfig{Enabled: true, DisableRatePercent: 80, MinimumKnownInputRequests: 100}
	for _, test := range []struct {
		name  string
		known int64
		low   int64
		want  bool
	}{
		{name: "rate meets threshold", known: 100, low: 80, want: true},
		{name: "rate below threshold", known: 100, low: 79},
		{name: "sample count below minimum", known: 99, low: 99},
		{name: "no known samples", known: 0, low: 0},
		{name: "invalid low count", known: 100, low: 101},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, requestStatsShouldDisable(test.known, test.low, config))
		})
	}
}

func TestRequestStatsRecentDropOnlyBlocksAffectedDecisionWindow(t *testing.T) {
	const cutoff = int64(1_800_000_000)
	for _, test := range []struct {
		name        string
		lastDropped int64
		wantBlocked bool
	}{
		{name: "no known drop", lastDropped: 0},
		{name: "drop older than previous day", lastDropped: cutoff - 86401},
		{name: "drop during previous day", lastDropped: cutoff - 1, wantBlocked: true},
		{name: "drop while midnight decision is delayed", lastDropped: cutoff + 30, wantBlocked: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.wantBlocked, requestStatsRecentDrop(test.lastDropped, cutoff))
		})
	}
}

func TestRequestStatsQueryDrillsDownAndRecomputesWithCurrentThreshold(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{},
		&model.ChannelMonitorDailySuccessLedger{}, &model.ChannelMonitorDailyCheckpoint{},
		&model.ChannelMonitorRequestStatsConfig{}, &model.ChannelMonitorRequestStatsDisable{}))
	for _, user := range []model.User{
		{Id: 11, Username: "alice", DisplayName: "Alice", Password: "password123", AffCode: "request-stats-alice"},
		{Id: 22, Username: "bob", DisplayName: "Bob", Password: "password123", AffCode: "request-stats-bob"},
		{Id: 33, Username: "carol", DisplayName: "Carol", Password: "password123", AffCode: "request-stats-carol"},
	} {
		require.NoError(t, db.Create(&user).Error)
	}
	for _, token := range []model.Token{
		{Id: 42, UserId: 11, Key: "secret-key-a", Name: "key-a"},
		{Id: 43, UserId: 11, Key: "secret-key-b", Name: "key-b"},
		{Id: 44, UserId: 22, Key: "secret-key-c", Name: "key-c"},
		{Id: 45, UserId: 33, Key: "secret-key-d", Name: "key-d"},
	} {
		require.NoError(t, db.Create(&token).Error)
	}
	for _, channel := range []model.Channel{
		{Id: 7, Key: "upstream-a", Name: "channel-a"},
		{Id: 8, Key: "upstream-b", Name: "channel-b"},
	} {
		require.NoError(t, db.Create(&channel).Error)
	}

	day := model.ChannelDailyCostDayStart(time.Now().Unix()) - 86400
	events := []model.ChannelMonitorEvent{
		{EventId: "stats-a1", OccurredAt: day + 60, ChannelId: 7, UserId: 11, APIKeyId: 42, APIKeyName: "key-a", Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeSuccess, RequestDispatched: true, IsStream: true, ReliableInputTokens: common.GetPointer(int64(9_999))},
		{EventId: "stats-a2", OccurredAt: day + 120, ChannelId: 7, UserId: 11, APIKeyId: 42, APIKeyName: "key-a", Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeSuccess, RequestDispatched: true, ReliableInputTokens: common.GetPointer(int64(10_000))},
		{EventId: "stats-a3", OccurredAt: day + 180, ChannelId: 7, UserId: 11, APIKeyId: 42, APIKeyName: "key-a", Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeFailure, RequestDispatched: true, IsStream: true},
		{EventId: "stats-b1", OccurredAt: day + 240, ChannelId: 7, UserId: 11, APIKeyId: 43, APIKeyName: "key-b", Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeSuccess, RequestDispatched: true, IsStream: true, ReliableInputTokens: common.GetPointer(int64(9_000))},
		{EventId: "stats-c1", OccurredAt: day + 300, ChannelId: 8, UserId: 22, APIKeyId: 44, APIKeyName: "key-c", Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeSuccess, RequestDispatched: true, ReliableInputTokens: common.GetPointer(int64(1_000))},
		{EventId: "stats-d1", OccurredAt: day + 360, ChannelId: 7, UserId: 33, APIKeyId: 45, APIKeyName: "key-d", Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeSuccess, RequestDispatched: true},
	}
	aggregates := make(map[string]ChannelMonitorRedisSharedAggregate)
	for _, event := range events {
		scope := channelMonitorDailyMetricScope(model.ChannelMonitorDailyMetricIdentityFromEvent(event))
		aggregate := aggregates[scope]
		var err error
		aggregate.RequestStats, err = mergeRequestStatsDistribution(aggregate.RequestStats, requestStatsEventMetrics(event))
		require.NoError(t, err)
		aggregate.APIKeyName = event.APIKeyName
		if event.Outcome == model.ChannelMonitorEventOutcomeSuccess {
			aggregate.ActualSuccessCount++
		} else {
			aggregate.ActualFailureCount++
		}
		aggregates[scope] = aggregate
	}
	for scope, aggregate := range aggregates {
		identity, err := channelMonitorDailyMetricIdentity(strings.TrimPrefix(scope, "fact:"))
		require.NoError(t, err)
		row := identity.LedgerRow(day)
		row.APIKeyName = aggregate.APIKeyName
		row.ActualSuccessCount, row.ActualFailureCount = aggregate.ActualSuccessCount, aggregate.ActualFailureCount
		encoded, err := common.Marshal(aggregate)
		require.NoError(t, err)
		row.AggregateJSON = string(encoded)
		require.NoError(t, db.Create(&row).Error)
	}

	date := time.Unix(day, 0).In(requestStatsLocation).Format(time.DateOnly)
	users, err := QueryChannelMonitorRequestStats(t.Context(), RequestStatsQuery{Date: date})
	require.NoError(t, err)
	assert.Equal(t, 10, users.LowInputThresholdK)
	assert.Equal(t, "bob · Bob", users.Items[0].Name, "default order is descending low-input rate")
	assert.Equal(t, "alice · Alice", users.Items[1].Name)
	assert.Equal(t, "carol · Carol", users.Items[2].Name, "rows without a known-input rate stay last")
	assert.Nil(t, users.Items[2].LowInputRate)
	assert.Equal(t, int64(6), users.Summary.RequestCount)
	assert.Equal(t, int64(3), users.Summary.StreamRequestCount)
	assert.Equal(t, int64(3), users.Summary.NonStreamRequestCount)
	assert.Equal(t, int64(1), users.Items[1].UnknownInputRequestCount)

	ascendingUsers, err := QueryChannelMonitorRequestStats(t.Context(), RequestStatsQuery{Date: date, Direction: "asc"})
	require.NoError(t, err)
	assert.Equal(t, "alice · Alice", ascendingUsers.Items[0].Name)
	assert.Equal(t, "bob · Bob", ascendingUsers.Items[1].Name)
	assert.Equal(t, "carol · Carol", ascendingUsers.Items[2].Name, "rows without a known-input rate also stay last in ascending order")

	keys, err := QueryChannelMonitorRequestStats(t.Context(), RequestStatsQuery{Date: date, Level: "api_key", UserID: 11})
	require.NoError(t, err)
	require.Len(t, keys.Items, 2)
	assert.Equal(t, "key-b", keys.Items[0].Name)
	assert.Equal(t, "key-a", keys.Items[1].Name)
	assert.NotContains(t, keys.Items[1].Name, "secret-key-a", "API credentials never appear in statistics responses")

	channels, err := QueryChannelMonitorRequestStats(t.Context(), RequestStatsQuery{Date: date, Level: "channel", UserID: 11, APIKeyID: 42})
	require.NoError(t, err)
	require.Len(t, channels.Items, 1)
	assert.Equal(t, "channel-a", channels.Items[0].Name)
	assert.Equal(t, int64(3), channels.Items[0].RequestCount)
	assert.Equal(t, int64(2), channels.Items[0].StreamRequestCount)

	config, err := model.GetChannelMonitorRequestStatsConfig(t.Context())
	require.NoError(t, err)
	config, err = model.SaveChannelMonitorRequestStatsConfig(t.Context(), config.Revision, map[string]any{"low_input_threshold_k": 5})
	require.NoError(t, err)
	users, err = QueryChannelMonitorRequestStats(t.Context(), RequestStatsQuery{Date: date})
	require.NoError(t, err)
	assert.Equal(t, 5, users.LowInputThresholdK)
	assert.Equal(t, int64(1), users.Summary.LowInputCount, "the current threshold applies to stored historical distributions")
}
