package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorRealtimeProjectionAggregatesScopesAndSources(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	projection := newChannelMonitorRealtimeProjection(channelMonitorRealtimeProjectionConfig{
		eventsPerRoute: 10,
		maxRoutes:      10,
		dedupCapacity:  20,
		dailyCostDays:  4,
		now:            func() time.Time { return clock },
	})

	firstToken := 120.0
	tps := 24.0
	input := int64(100)
	cacheRead := int64(40)
	cacheWrite := int64(10)
	settled := int64(3_000)
	failedDuration := int64(500)
	events := []model.ChannelMonitorEvent{
		realtimeProjectionTestEvent("business-success", 7, "gemini-2.5-flash-thinking-1024", 100, 1, model.ChannelMonitorEventOutcomeSuccess, true),
		realtimeProjectionTestEvent("business-retry", 7, "gemini-2.5-flash-thinking-1024", 101, 2, model.ChannelMonitorEventOutcomeFailure, false),
		realtimeProjectionTestEvent("business-final", 7, "gemini-2.5-flash-thinking-1024", 102, 3, model.ChannelMonitorEventOutcomeSuccess, true),
		realtimeProjectionTestEvent("probe", 7, "gemini-2.5-flash-thinking-1024", 103, 4, model.ChannelMonitorEventOutcomeFailure, true),
	}
	events[0].FirstTokenMs = &firstToken
	events[0].TPS = &tps
	events[0].InputTokens = &input
	events[0].CacheReadTokens = &cacheRead
	events[0].CacheWriteTokens = &cacheWrite
	events[0].SettledCostNanoCNY = settled
	events[0].CostStatus = model.ChannelMonitorEventCostSettled
	events[1].AttemptDurationMs = &failedDuration
	events[1].IsRetryAttempt = true
	events[2].SettledCostNanoCNY = 2_000
	events[2].CostStatus = model.ChannelMonitorEventCostSettled
	events[3].Source = model.ChannelMonitorEventSourceSmartProbe
	events[3].RequestDispatched = true

	require.NoError(t, projection.consume(context.Background(), events))

	route, ok := projection.route(7, "gemini-2.5-flash-thinking-*")
	require.True(t, ok)
	assert.Equal(t, int64(4), route.EventCount)
	assert.Equal(t, int64(2), route.ActualSuccessCount)
	assert.Equal(t, int64(1), route.ActualFailureCount)
	assert.Equal(t, int64(3), route.ActualSampleCount)
	assert.InDelta(t, 2.0/3.0, route.ActualSuccessRate, 1e-12)
	assert.Equal(t, int64(2), route.FinalSuccessCount)
	assert.Equal(t, int64(0), route.FinalFailureCount)
	assert.Equal(t, int64(2), route.FinalSampleCount)
	assert.Equal(t, int64(1), route.FirstTokenSampleCount)
	assert.InDelta(t, firstToken, *route.AverageFirstTokenMs, 1e-12)
	assert.InDelta(t, tps, *route.AverageTPS, 1e-12)
	assert.Equal(t, int64(40), route.CacheReadTokens)
	assert.Equal(t, int64(10), route.CacheWriteTokens)
	assert.Equal(t, int64(100), route.InputTokens)
	assert.Equal(t, settled+2_000, route.SettledCostNanoCNY)
	assert.Equal(t, int64(3), route.SourceCounts[model.ChannelMonitorEventSourceBusiness])
	assert.Equal(t, int64(1), route.SourceCounts[model.ChannelMonitorEventSourceSmartProbe])
	assert.Equal(t, clock.Unix(), route.ProcessedAt)
	assert.Equal(t, uint64(4), route.EventWatermark)

	channel, ok := projection.channel(7)
	require.True(t, ok)
	assert.Equal(t, route.EventCount, channel.EventCount)
	modelSnapshot, ok := projection.model("gemini-2.5-flash-thinking-1024")
	require.True(t, ok)
	assert.Equal(t, route.EventCount, modelSnapshot.EventCount)
	global := projection.global()
	assert.Equal(t, route.SettledCostNanoCNY, global.SettledCostNanoCNY)
}

func TestChannelMonitorRealtimeProjectionDeduplicatesAndOrdersBoundedWindow(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	projection := newChannelMonitorRealtimeProjection(channelMonitorRealtimeProjectionConfig{
		eventsPerRoute: 2,
		maxRoutes:      2,
		dedupCapacity:  10,
		dailyCostDays:  2,
		now:            func() time.Time { return clock },
	})

	old := realtimeProjectionTestEvent("old", 1, "model-a", 10, 1, model.ChannelMonitorEventOutcomeSuccess, true)
	middle := realtimeProjectionTestEvent("middle", 1, "model-a", 20, 3, model.ChannelMonitorEventOutcomeSuccess, true)
	latest := realtimeProjectionTestEvent("latest", 1, "model-a", 30, 2, model.ChannelMonitorEventOutcomeSuccess, true)
	old.SettledCostNanoCNY = 100
	old.CostStatus = model.ChannelMonitorEventCostSettled
	middle.SettledCostNanoCNY = 300
	middle.CostStatus = model.ChannelMonitorEventCostSettled
	latest.SettledCostNanoCNY = 200
	latest.CostStatus = model.ChannelMonitorEventCostSettled
	require.NoError(t, projection.consume(context.Background(), []model.ChannelMonitorEvent{latest, old, middle, latest}))

	window, ok := projection.window(1, "model-a")
	require.True(t, ok)
	require.Len(t, window.Events, 2)
	assert.Equal(t, int64(20), window.Events[0].OccurredAt)
	assert.Equal(t, int64(30), window.Events[1].OccurredAt)
	assert.Equal(t, int64(30), window.Snapshot.WindowEnd)
	assert.Equal(t, int64(30), window.Snapshot.DataCutoffAt)
	assert.Equal(t, uint64(3), window.Snapshot.EventWatermark)
	assert.Equal(t, int64(500), window.Snapshot.SettledCostNanoCNY)

	thirdRoute := realtimeProjectionTestEvent("third-route", 3, "model-c", 40, 4, model.ChannelMonitorEventOutcomeSuccess, true)
	require.NoError(t, projection.consume(context.Background(), []model.ChannelMonitorEvent{thirdRoute}))
	_, ok = projection.route(1, "model-a")
	assert.True(t, ok)
	_, ok = projection.route(3, "model-c")
	assert.True(t, ok)
	other := realtimeProjectionTestEvent("other-route", 2, "model-b", 50, 5, model.ChannelMonitorEventOutcomeSuccess, true)
	require.NoError(t, projection.consume(context.Background(), []model.ChannelMonitorEvent{other}))
	_, ok = projection.route(1, "model-a")
	assert.False(t, ok)
	_, ok = projection.route(3, "model-c")
	assert.True(t, ok)
}

func TestChannelMonitorRealtimeProjectionTracksCoverageAfterTruncationAndEviction(t *testing.T) {
	clock := time.Unix(100, 0)
	projection := newChannelMonitorRealtimeProjection(channelMonitorRealtimeProjectionConfig{
		eventsPerRoute: 2,
		maxRoutes:      2,
		dedupCapacity:  10,
		dailyCostDays:  2,
		now:            func() time.Time { return clock },
	})

	first := realtimeProjectionTestEvent("first", 1, "model-a", 101, 1, model.ChannelMonitorEventOutcomeSuccess, true)
	second := realtimeProjectionTestEvent("second", 1, "model-a", 102, 2, model.ChannelMonitorEventOutcomeSuccess, true)
	third := realtimeProjectionTestEvent("third", 1, "model-a", 103, 3, model.ChannelMonitorEventOutcomeSuccess, true)
	require.NoError(t, projection.consume(context.Background(), []model.ChannelMonitorEvent{first, second, third}))

	window, ok := projection.window(1, "model-a")
	require.True(t, ok)
	assert.Equal(t, int64(102), window.Snapshot.CoverageStart)

	other := realtimeProjectionTestEvent("other", 2, "model-b", 104, 4, model.ChannelMonitorEventOutcomeSuccess, true)
	require.NoError(t, projection.consume(context.Background(), []model.ChannelMonitorEvent{other}))
	latest := realtimeProjectionTestEvent("latest", 3, "model-c", 105, 5, model.ChannelMonitorEventOutcomeSuccess, true)
	require.NoError(t, projection.consume(context.Background(), []model.ChannelMonitorEvent{latest}))

	window, ok = projection.window(2, "model-b")
	require.True(t, ok)
	assert.Equal(t, int64(104), window.Snapshot.CoverageStart)
	assert.Equal(t, int64(104), projection.routeCoverageStart(nil))
}

func TestChannelMonitorRealtimeProjectionReplacesTaskCostWithoutDuplicatingRequest(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	projection := newChannelMonitorRealtimeProjection(channelMonitorRealtimeProjectionConfig{
		eventsPerRoute: 10,
		maxRoutes:      10,
		dedupCapacity:  10,
		dailyCostDays:  2,
		now:            func() time.Time { return clock },
	})
	page := newChannelMonitorRealtimePageProjection()

	initial := realtimeProjectionTestEvent(
		"task-initial", 7, "model-a", clock.Unix(), 1,
		model.ChannelMonitorEventOutcomeSuccess, true,
	)
	initial.GroupName = "vip"
	initial.APIKeyId = 9
	initial.CostStatus = model.ChannelMonitorEventCostSettled
	initial.SettledCostNanoCNY = 100
	initial.OtherJson = `{"cost_event_id":"task:task-1"}`
	correction := realtimeProjectionTestEvent(
		"task-correction", 7, "model-a", clock.Unix(), 2,
		model.ChannelMonitorEventOutcomeSuccess, true,
	)
	correction.GroupName = "vip"
	correction.APIKeyId = 9
	correction.RequestDispatched = false
	correction.SchedulingEligible = false
	correction.CostStatus = model.ChannelMonitorEventCostSettled
	correction.SettledCostNanoCNY = 250
	correction.OtherJson = initial.OtherJson

	require.NoError(t, projection.consume(context.Background(), []model.ChannelMonitorEvent{initial, correction}))
	page.consume([]model.ChannelMonitorEvent{initial, correction}, clock.Unix())

	window, ok := projection.window(7, "model-a")
	require.True(t, ok)
	require.Len(t, window.Events, 1)
	assert.Equal(t, int64(250), window.Snapshot.TodaySettledCostNanoCNY)
	assert.Equal(t, int64(1), window.Snapshot.ActualSuccessCount)
	view := page.query(clock.Unix()-60, clock.Unix()+60)
	assert.Equal(t, int64(250), view.Summary.SettledCostNanoCNY)
	assert.Equal(t, 1, view.Summary.SampleCount)
}

func TestChannelMonitorRealtimeProjectionUsesBeijingDayForCostsAndReturnsCopies(t *testing.T) {
	beijing := time.FixedZone("Asia/Shanghai", 8*60*60)
	processingAt := time.Date(2026, 8, 15, 0, 2, 0, 0, beijing)
	projection := newChannelMonitorRealtimeProjection(channelMonitorRealtimeProjectionConfig{
		eventsPerRoute: 10,
		maxRoutes:      10,
		dedupCapacity:  10,
		dailyCostDays:  4,
		now:            func() time.Time { return processingAt },
	})
	previousDay := processingAt.Add(-5 * time.Minute).Unix()
	currentDay := processingAt.Add(2 * time.Minute).Unix()
	first := realtimeProjectionTestEvent("previous-day", 9, "model", previousDay, 1, model.ChannelMonitorEventOutcomeSuccess, true)
	first.SettledCostNanoCNY = 1_000
	first.CostStatus = model.ChannelMonitorEventCostSettled
	firstPromptTokens := int64(1)
	first.PromptTokens = &firstPromptTokens
	second := realtimeProjectionTestEvent("current-day", 9, "model", currentDay, 2, model.ChannelMonitorEventOutcomeSuccess, true)
	second.SettledCostNanoCNY = 2_000
	second.CostStatus = model.ChannelMonitorEventCostSettled
	require.NoError(t, projection.consume(context.Background(), []model.ChannelMonitorEvent{first, second}))

	snapshot, ok := projection.channel(9)
	require.True(t, ok)
	assert.Equal(t, model.ChannelDailyCostDayStart(currentDay), snapshot.TodayDayStart)
	assert.Equal(t, int64(2_000), snapshot.TodaySettledCostNanoCNY)
	require.Len(t, snapshot.DailyCosts, 2)
	assert.Equal(t, int64(1_000), snapshot.DailyCosts[0].SettledCostNanoCNY)
	assert.Equal(t, int64(2_000), snapshot.DailyCosts[1].SettledCostNanoCNY)

	snapshot.SourceCounts[model.ChannelMonitorEventSourceBusiness] = 999
	snapshot.DailyCosts[0].SettledCostNanoCNY = 999
	retrieved, ok := projection.channel(9)
	require.True(t, ok)
	assert.Equal(t, int64(2), retrieved.SourceCounts[model.ChannelMonitorEventSourceBusiness])
	assert.Equal(t, int64(1_000), retrieved.DailyCosts[0].SettledCostNanoCNY)

	window, ok := projection.window(9, "model")
	require.True(t, ok)
	*window.Events[0].PromptTokens = 123
	retrievedWindow, ok := projection.window(9, "model")
	require.True(t, ok)
	assert.NotEqual(t, int64(123), *retrievedWindow.Events[0].PromptTokens)

	processingAt = processingAt.Add(24 * time.Hour)
	nextDay, ok := projection.channel(9)
	require.True(t, ok)
	assert.Equal(t, model.ChannelDailyCostDayStart(processingAt.Unix()), nextDay.TodayDayStart)
	assert.Zero(t, nextDay.TodaySettledCostNanoCNY)
}

func TestChannelMonitorRealtimeProjectionReplacesModelDetectionCostState(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	projection := newChannelMonitorRealtimeProjection(channelMonitorRealtimeProjectionConfig{
		eventsPerRoute: 10,
		maxRoutes:      10,
		dedupCapacity:  10,
		dailyCostDays:  4,
		now:            func() time.Time { return clock },
	})
	unresolved := realtimeProjectionTestEvent("model-detection:cost-1:unresolved", 9, "model", clock.Unix(), 1, model.ChannelMonitorEventOutcomeUnresolved, true)
	unresolved.Source = model.ChannelMonitorEventSourceModelDetection
	unresolved.CostStatus = model.ChannelMonitorEventCostUnresolved
	unresolved.UnresolvedCostNanoCNY = 800
	unresolved.OtherJson = `{"cost_event_id":"cost-1"}`
	settled := realtimeProjectionTestEvent("model-detection:cost-1:settled", 9, "model", clock.Unix()+1, 2, model.ChannelMonitorEventOutcomeSuccess, true)
	settled.Source = model.ChannelMonitorEventSourceModelDetection
	settled.CostStatus = model.ChannelMonitorEventCostSettled
	settled.SettledCostNanoCNY = 650
	settled.OtherJson = `{"cost_event_id":"cost-1"}`

	require.NoError(t, projection.consume(context.Background(), []model.ChannelMonitorEvent{unresolved, settled}))
	snapshot, ok := projection.route(9, "model")
	require.True(t, ok)
	assert.Zero(t, snapshot.TodayUnresolvedRequestCount)
	assert.Equal(t, int64(1), snapshot.TodaySettledRequestCount)
	assert.Equal(t, int64(650), snapshot.TodaySettledCostNanoCNY)
	require.Len(t, snapshot.DailyCosts, 1)
	assert.Equal(t, int64(650), snapshot.DailyCosts[0].ModelDetectionSettledCostNanoCNY)
	window, ok := projection.window(9, "model")
	require.True(t, ok)
	require.Len(t, window.Events, 1)
	assert.Equal(t, settled.EventId, window.Events[0].EventId)
}

func TestChannelMonitorRealtimeProjectionFinalRetrySummaryOnlyCountsFinalFailure(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	projection := newChannelMonitorRealtimeProjection(channelMonitorRealtimeProjectionConfig{
		eventsPerRoute: 10,
		maxRoutes:      10,
		dedupCapacity:  10,
		dailyCostDays:  4,
		now:            func() time.Time { return clock },
	})
	attempt := realtimeProjectionTestEvent("attempt", 7, "model", clock.Unix(), 1, model.ChannelMonitorEventOutcomeFailure, false)
	summary := realtimeProjectionTestEvent("summary", 7, "model", clock.Unix()+1, 2, model.ChannelMonitorEventOutcomeFailure, true)
	summary.FinalRetrySummary = true
	summary.RequestDispatched = false

	require.NoError(t, projection.consume(context.Background(), []model.ChannelMonitorEvent{attempt, summary}))
	snapshot, ok := projection.route(7, "model")
	require.True(t, ok)
	assert.Equal(t, int64(1), snapshot.ActualFailureCount)
	assert.Equal(t, int64(1), snapshot.ActualSampleCount)
	assert.Equal(t, int64(1), snapshot.FinalFailureCount)
	assert.Equal(t, int64(1), snapshot.FinalSampleCount)
	assert.Equal(t, int64(1), snapshot.BusinessRequestCount)
}

func realtimeProjectionTestEvent(
	eventId string,
	channelId int,
	modelName string,
	occurredAt int64,
	sequence uint64,
	outcome model.ChannelMonitorEventOutcome,
	final bool,
) model.ChannelMonitorEvent {
	return model.ChannelMonitorEvent{
		EventId:           eventId,
		EventSequence:     sequence,
		SchemaVersion:     model.ChannelMonitorEventSchemaVersion,
		OccurredAt:        occurredAt,
		CreatedAt:         occurredAt,
		ChannelId:         channelId,
		ModelName:         modelName,
		Source:            model.ChannelMonitorEventSourceBusiness,
		Outcome:           outcome,
		CostStatus:        model.ChannelMonitorEventCostNone,
		RequestId:         eventId,
		RequestDispatched: true,
		IsFinalAttempt:    final,
	}
}
