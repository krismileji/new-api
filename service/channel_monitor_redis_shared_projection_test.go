package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newChannelMonitorRedisSharedProjectionTestClient(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return server, client
}

func newChannelMonitorRedisSharedProjectionTestEvent(eventID string, occurredAt int64) model.ChannelMonitorEvent {
	return model.ChannelMonitorEvent{
		EventId:            eventID,
		EventSequence:      1,
		SchemaVersion:      model.ChannelMonitorEventSchemaVersion,
		OccurredAt:         occurredAt,
		CreatedAt:          occurredAt + 1,
		ChannelId:          7,
		GroupName:          "vip",
		ModelName:          "gpt-test",
		APIKeyId:           42,
		APIKeyName:         "生产 Key",
		Source:             model.ChannelMonitorEventSourceBusiness,
		Outcome:            model.ChannelMonitorEventOutcomeSuccess,
		CostStatus:         model.ChannelMonitorEventCostNone,
		RequestDispatched:  true,
		IsFinalAttempt:     true,
		SchedulingEligible: true,
	}
}

func TestChannelMonitorRedisSharedProjectionAggregatesAcrossScopesAndNodes(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	first := NewChannelMonitorRedisSharedProjectionWithClient(client)
	second := NewChannelMonitorRedisSharedProjectionWithClient(client)
	occurredAt := int64(1_750_000_000)
	firstToken := 120.5
	tps := 8.25
	inputTokens := int64(900)
	cacheReadTokens := int64(300)
	cacheWriteTokens := int64(40)
	duration := int64(840)
	event := newChannelMonitorRedisSharedProjectionTestEvent("event-1", occurredAt)
	event.FirstTokenMs = &firstToken
	event.TPS = &tps
	event.InputTokens = &inputTokens
	event.CacheReadTokens = &cacheReadTokens
	event.CacheWriteTokens = &cacheWriteTokens
	event.AttemptDurationMs = &duration

	require.NoError(t, first.HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event}))
	start := occurredAt - 60
	end := occurredAt + 60
	firstView, err := first.Query(context.Background(), start, end)
	require.NoError(t, err)
	secondView, err := second.QueryChannelMonitorRedisSharedProjection(context.Background(), start, end)
	require.NoError(t, err)
	assert.Equal(t, firstView, secondView)

	assert.Equal(t, int64(1), firstView.Summary.EventCount)
	assert.Equal(t, int64(1), firstView.Summary.BusinessRequestCount)
	assert.Equal(t, int64(1), firstView.Summary.ActualSuccessCount)
	assert.Equal(t, int64(1), firstView.Summary.FinalSuccessCount)
	assert.Equal(t, int64(1), firstView.Summary.FirstTokenSampleCount)
	assert.Equal(t, firstToken, firstView.Performance.FirstTokenTotalMs)
	assert.Equal(t, int64(1), firstView.Performance.AttemptDurationSampleCount)
	assert.Equal(t, duration, firstView.Performance.AttemptDurationTotalMs)
	assert.Equal(t, inputTokens, firstView.Summary.InputTokens)
	assert.Equal(t, cacheReadTokens, firstView.Summary.CacheReadTokens)
	assert.Equal(t, int64(1), firstView.Summary.CacheWriteRequestCount)
	assert.Equal(t, cacheWriteTokens, firstView.Summary.CacheWriteTokens)

	channelAggregate, ok := firstView.Channels[event.ChannelId]
	require.True(t, ok)
	assert.Equal(t, int64(1), channelAggregate.EventCount)
	modelAggregate, ok := firstView.Models[event.ModelName]
	require.True(t, ok)
	assert.Equal(t, int64(1), modelAggregate.EventCount)
	groupAggregate, ok := firstView.Groups[event.GroupName]
	require.True(t, ok)
	assert.Equal(t, int64(1), groupAggregate.EventCount)
	keyAggregate, ok := firstView.APIKeys[event.APIKeyId]
	require.True(t, ok)
	assert.Equal(t, event.APIKeyName, keyAggregate.APIKeyName)
	assert.Equal(t, int64(1), keyAggregate.EventCount)

	minuteValues, err := client.HGetAll(context.Background(), ChannelMonitorRedisDashboardMinuteKey(occurredAt-occurredAt%60)).Result()
	require.NoError(t, err)
	assert.NotEmpty(t, minuteValues)
	assert.Equal(t, event.APIKeyName, minuteValues[channelMonitorRedisSharedScopeAPIKey+":42:"+channelMonitorRedisSharedMetricAPIKeyName])
}

func TestMergeChannelMonitorRedisSharedAggregateRejectsInt64Overflow(t *testing.T) {
	tests := []struct {
		name   string
		target ChannelMonitorRedisSharedAggregate
		source ChannelMonitorRedisSharedAggregate
		value  func(ChannelMonitorRedisSharedAggregate) int64
	}{
		{
			name:   "settled cost",
			target: ChannelMonitorRedisSharedAggregate{SettledCostNanoCNY: math.MaxInt64},
			source: ChannelMonitorRedisSharedAggregate{SettledCostNanoCNY: 1},
			value:  func(item ChannelMonitorRedisSharedAggregate) int64 { return item.SettledCostNanoCNY },
		},
		{
			name:   "event count",
			target: ChannelMonitorRedisSharedAggregate{EventCount: math.MaxInt64},
			source: ChannelMonitorRedisSharedAggregate{EventCount: 1},
			value:  func(item ChannelMonitorRedisSharedAggregate) int64 { return item.EventCount },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := mergeChannelMonitorRedisSharedAggregate(&test.target, test.source)
			require.ErrorContains(t, err, "超过 int64 范围")
			assert.Equal(t, int64(math.MaxInt64), test.value(test.target))
		})
	}
}

func TestChannelMonitorRedisSharedSuccessSummaryRejectsCombinedCountOverflow(t *testing.T) {
	_, err := channelMonitorRedisSharedSuccessSummary(ChannelMonitorRedisSharedAggregate{
		ActualSuccessCount: math.MaxInt64,
		ActualFailureCount: 1,
	})
	require.ErrorContains(t, err, "超过 int64 范围")
}

func TestChannelMonitorRedisSharedCountToIntRejectsInvalidValues(t *testing.T) {
	_, err := channelMonitorRedisSharedCountToInt(-1)
	require.ErrorContains(t, err, "不能为负数")

	maximum := int64(math.MaxInt64)
	value, err := channelMonitorRedisSharedCountToInt(maximum)
	if int64(int(maximum)) == maximum {
		require.NoError(t, err)
		assert.Equal(t, int64(math.MaxInt64), int64(value))
	} else {
		require.ErrorContains(t, err, "超过 int 范围")
	}
}

func TestChannelMonitorRedisSharedProjectionUsesEventIDForReplayIdempotency(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	occurredAt := int64(1_750_000_000)
	event := newChannelMonitorRedisSharedProjectionTestEvent("event-replayed", occurredAt)

	require.NoError(t, projection.HandleChannelMonitorEvents(
		context.Background(),
		[]model.ChannelMonitorEvent{event, event},
	))
	require.NoError(t, projection.HandleChannelMonitorEvents(
		context.Background(),
		[]model.ChannelMonitorEvent{event},
	))

	view, err := projection.Query(context.Background(), occurredAt-60, occurredAt+60)
	require.NoError(t, err)
	assert.Equal(t, int64(1), view.Summary.EventCount)
	assert.Equal(t, int64(1), view.Summary.BusinessRequestCount)
	markerTTL, err := client.TTL(context.Background(), ChannelMonitorRedisSharedEventKey(event.EventId)).Result()
	require.NoError(t, err)
	assert.Positive(t, markerTTL)
}

func TestChannelMonitorRedisSharedProjectionReplacesUnresolvedCostWithoutDoubleCounting(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	occurredAt := int64(1_750_000_000)
	unresolved := newChannelMonitorRedisSharedProjectionTestEvent("cost-unresolved", occurredAt)
	unresolved.CostStatus = model.ChannelMonitorEventCostUnresolved
	unresolved.UnresolvedCostNanoCNY = 800
	unresolved.OtherJson = `{"cost_event_id":"cost-1"}`

	settled := unresolved
	settled.EventId = "cost-settled"
	settled.EventSequence = 2
	settled.CreatedAt++
	settled.RequestDispatched = false
	settled.CostStatus = model.ChannelMonitorEventCostSettled
	settled.UnresolvedCostNanoCNY = 0
	settled.SettledCostNanoCNY = 650

	require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{unresolved}))
	require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{settled}))
	view, err := projection.Query(context.Background(), occurredAt-60, occurredAt+60)
	require.NoError(t, err)
	assert.Zero(t, view.Summary.UnresolvedCostNanoCNY)
	assert.Zero(t, view.Summary.UnresolvedRequestCount)
	assert.Equal(t, int64(650), view.Summary.SettledCostNanoCNY)
	assert.Equal(t, int64(1), view.Summary.SettledRequestCount)
	assert.Equal(t, int64(650), view.DailyCosts[model.ChannelDailyCostDayStart(occurredAt)].Global.SettledCostNanoCNY)

	state, err := client.HGetAll(context.Background(), ChannelMonitorRedisCostEventStateKey("business:cost-1")).Result()
	require.NoError(t, err)
	assert.Equal(t, string(model.ChannelMonitorEventCostSettled), state[channelMonitorRedisSharedCostStatus])
	assert.Equal(t, "650", state[channelMonitorRedisSharedCostSettled])
	assert.NotContains(t, state, "other_json")
	assert.NotContains(t, state, "error_message")
}

func TestChannelMonitorRedisSharedProjectionSeparatesDaysAndExpiresCompactKeys(t *testing.T) {
	server, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	dayOne := model.ChannelDailyCostDayStart(1_750_000_000)
	dayTwo := dayOne + 24*60*60
	first := newChannelMonitorRedisSharedProjectionTestEvent("day-one", dayOne+120)
	first.CostStatus = model.ChannelMonitorEventCostSettled
	first.SettledCostNanoCNY = 100
	first.OtherJson = `{"cost_event_id":"day-one"}`
	second := newChannelMonitorRedisSharedProjectionTestEvent("day-two", dayTwo+120)
	second.CostStatus = model.ChannelMonitorEventCostSettled
	second.SettledCostNanoCNY = 250
	second.OtherJson = `{"cost_event_id":"day-two"}`

	require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{first, second}))
	view, err := projection.Query(context.Background(), dayOne, dayTwo+300)
	require.NoError(t, err)
	assert.Equal(t, int64(100), view.DailyCosts[dayOne].Global.SettledCostNanoCNY)
	assert.Equal(t, int64(250), view.DailyCosts[dayTwo].Global.SettledCostNanoCNY)
	assert.NotEqual(t, view.DailyCosts[dayOne].Global.SettledCostNanoCNY, view.DailyCosts[dayTwo].Global.SettledCostNanoCNY)

	minuteKey := ChannelMonitorRedisDashboardMinuteKey(first.OccurredAt - first.OccurredAt%60)
	dayKey := ChannelMonitorRedisCostDayKey(dayOne)
	stateKey := ChannelMonitorRedisCostEventStateKey("business:day-one")
	assert.Equal(t, int64(1), client.Exists(context.Background(), minuteKey).Val())
	assert.Equal(t, int64(1), client.Exists(context.Background(), dayKey).Val())
	assert.Equal(t, int64(1), client.Exists(context.Background(), stateKey).Val())

	server.FastForward(channelMonitorRedisSharedMinuteTTL + time.Second)
	assert.Zero(t, client.Exists(context.Background(), minuteKey).Val())
	assert.Zero(t, client.Exists(context.Background(), dayKey).Val())
	assert.Zero(t, client.Exists(context.Background(), stateKey).Val())
}

func TestChannelMonitorRedisSharedProjectionRejectsInvalidBatchBeforeWriting(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	valid := newChannelMonitorRedisSharedProjectionTestEvent("valid", 1_750_000_000)
	invalid := valid
	invalid.EventId = "invalid"
	invalid.SchemaVersion++

	err := projection.WriteChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{valid, invalid})
	require.Error(t, err)
	assert.Zero(t, client.Exists(context.Background(), ChannelMonitorRedisDashboardMinuteKey(valid.OccurredAt-valid.OccurredAt%60)).Val())
	assert.Zero(t, client.Exists(context.Background(), ChannelMonitorRedisCostEventStateKey("business:valid")).Val())
}

func TestChannelMonitorRedisSharedProjectionBuildsCompositeScopesAndWindowMetadata(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	projection.now = func() time.Time { return time.Unix(1_800_000_000, 0) }
	base := int64(1_750_000_000)
	firstTokenOld := 120.0
	firstTokenNew := 80.0
	first := newChannelMonitorRedisSharedProjectionTestEvent("composite-first", base+10)
	first.EventSequence = 7
	first.ModelName = "gpt-4o-gizmo-custom"
	first.FirstTokenMs = &firstTokenOld
	first.ErrorMessage = ""
	second := newChannelMonitorRedisSharedProjectionTestEvent("composite-second", base+50)
	second.EventSequence = 11
	second.ModelName = first.ModelName
	second.Outcome = model.ChannelMonitorEventOutcomeFailure
	second.FirstTokenMs = &firstTokenNew
	second.StatusCode = intPointer(429)
	second.ErrorType = "rate_limit"
	second.ErrorCode = "quota"
	second.ErrorMessage = "newest failure"
	second.IsFinalAttempt = false
	final := second
	final.EventId = "composite-final"
	final.EventSequence = 12
	final.OccurredAt = base + 55
	final.RequestDispatched = false
	final.FinalRetrySummary = true

	// Process the newest event before the older event to verify that latest
	// values and failure samples use occurrence time, not stream processing order.
	require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{second, first, final}))
	view, err := projection.Query(context.Background(), base, base+60)
	require.NoError(t, err)

	require.Len(t, view.Routes, 1)
	assert.Equal(t, "gpt-4o-gizmo-*", view.Routes[0].ModelName)
	assert.Equal(t, int64(2), view.Routes[0].EventCount)
	assert.Equal(t, int64(1), view.Routes[0].ActualSuccessCount)
	assert.Equal(t, int64(1), view.Routes[0].ActualFailureCount)
	assert.Equal(t, int64(1), view.Routes[0].FinalFailureCount)
	require.NotNil(t, view.Routes[0].LatestFirstTokenMs)
	assert.Equal(t, firstTokenNew, *view.Routes[0].LatestFirstTokenMs)
	assert.Equal(t, base+50, view.Routes[0].LastUsedTime)

	require.Len(t, view.GroupChannels, 1)
	assert.Equal(t, "vip", view.GroupChannels[0].GroupName)
	require.Len(t, view.APIKeyScopes, 1)
	assert.Equal(t, first.APIKeyId, view.APIKeyScopes[0].APIKeyID)
	assert.Equal(t, first.APIKeyName, view.APIKeyScopes[0].APIKeyName)
	assert.Equal(t, first.ChannelId, view.APIKeyScopes[0].ChannelID)

	require.Len(t, view.Failures, 1)
	assert.Equal(t, int64(1), view.Failures[0].ActualCount)
	assert.Equal(t, int64(1), view.Failures[0].FinalCount)
	assert.Equal(t, "newest failure", view.Failures[0].SampleContent)
	assert.Equal(t, base+55, view.Failures[0].LastOccurred)

	assert.Equal(t, base+55, view.DataCutoffAt)
	assert.Equal(t, int64(1_800_000_000), view.ProcessedAt)
	assert.Equal(t, uint64(12), view.EventWatermark)
}

func TestChannelMonitorRedisSharedProjectionSplitsProbeAndDetectionCosts(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	base := int64(1_750_000_000)
	probe := newChannelMonitorRedisSharedProjectionTestEvent("probe-cost", base+1)
	probe.EventSequence = 20
	probe.Source = model.ChannelMonitorEventSourceStatusProbe
	probe.RequestDispatched = false
	probe.CostStatus = model.ChannelMonitorEventCostSettled
	probe.SettledCostNanoCNY = 110
	probe.OtherJson = `{"cost_event_id":"probe-cost"}`
	detectionUnresolved := probe
	detectionUnresolved.EventId = "detection-unresolved"
	detectionUnresolved.EventSequence = 21
	detectionUnresolved.Source = model.ChannelMonitorEventSourceModelDetection
	detectionUnresolved.CostStatus = model.ChannelMonitorEventCostUnresolved
	detectionUnresolved.SettledCostNanoCNY = 0
	detectionUnresolved.UnresolvedCostNanoCNY = 500
	detectionUnresolved.OtherJson = `{"cost_event_id":"detection-cost"}`
	detectionSettled := detectionUnresolved
	detectionSettled.EventId = "detection-settled"
	detectionSettled.EventSequence = 22
	detectionSettled.CreatedAt++
	detectionSettled.CostStatus = model.ChannelMonitorEventCostSettled
	detectionSettled.UnresolvedCostNanoCNY = 0
	detectionSettled.SettledCostNanoCNY = 220
	manual := probe
	manual.EventId = "manual-cost"
	manual.EventSequence = 23
	manual.Source = model.ChannelMonitorEventSourceManualTest
	manual.SettledCostNanoCNY = 330
	manual.OtherJson = `{"cost_event_id":"manual-cost"}`

	require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{probe, detectionUnresolved, manual}))
	require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{detectionSettled}))
	view, err := projection.Query(context.Background(), base, base+60)
	require.NoError(t, err)
	costs := view.DailyCosts[model.ChannelDailyCostDayStart(base)]
	assert.Equal(t, int64(660), costs.Global.SettledCostNanoCNY)
	assert.Equal(t, int64(110), costs.Global.ProbeSettledCostNanoCNY)
	assert.Equal(t, int64(220), costs.Global.ModelDetectionSettledCostNanoCNY)
	assert.Zero(t, costs.Global.UnresolvedCostNanoCNY)
	assert.Equal(t, int64(660), costs.Channels[probe.ChannelId].SettledCostNanoCNY)
}

func intPointer(value int) *int {
	return &value
}
