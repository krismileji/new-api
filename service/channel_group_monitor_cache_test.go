package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChannelGroupMonitorCacheRatesUsesBusinessSamplesInDisplayWindow(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	originalClient, originalEnabled := common.RDBMonitorRead, common.RedisEnabled
	common.RDBMonitorRead, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDBMonitorRead, common.RedisEnabled = originalClient, originalEnabled })
	now := int64(1_750_032_000)
	windowStart := now - now%60 - 14*60
	var events []model.ChannelMonitorEvent
	for _, fixture := range []struct {
		id     string
		group  string
		at     int64
		cache  *int64
		input  int64
		write  int64
		stream bool
		probe  bool
	}{
		{"hit", "vip", now - 120, common.GetPointer(int64(600)), 1000, 300, true, false},
		{"write-only", "vip", now - 60, common.GetPointer(int64(0)), 2000, 2000, true, false},
		{"missing-usage", "vip", now - 60, nil, 0, 0, true, false},
		{"missing-input", "vip", now - 60, common.GetPointer(int64(500)), 0, 0, true, false},
		{"non-stream", "vip", now - 60, common.GetPointer(int64(3000)), 3000, 0, false, false},
		{"outside-window", "vip", windowStart - 1, common.GetPointer(int64(30)), 100, 0, true, false},
		{"probe", "vip", now - 60, common.GetPointer(int64(20)), 100, 0, true, true},
		{"zero", "zero", now - 60, common.GetPointer(int64(0)), 100, 100, true, false},
		{"unknown", "unknown", now - 60, nil, 0, 0, true, false},
		{"private", "private", now - 60, common.GetPointer(int64(20)), 100, 0, true, false},
	} {
		event := newChannelMonitorRedisSharedProjectionTestEvent(fixture.id, fixture.at)
		event.GroupName, event.CacheReadTokens = fixture.group, fixture.cache
		event.IsStream, event.CacheWriteTokens = fixture.stream, &fixture.write
		if fixture.input > 0 {
			event.InputTokens = &fixture.input
		}
		if fixture.probe {
			event.Source = model.ChannelMonitorEventSourceGroupProbe
		}
		events = append(events, event)
	}
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), events))
	rates, err := GetChannelGroupMonitorCacheRates(context.Background(), []string{"vip", "zero", "unknown", "empty"}, windowStart, now+1)
	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"vip": 20, "zero": 0}, rates)
}

func TestGetChannelGroupMonitorCacheRatesWithoutRedisReturnsUnavailable(t *testing.T) {
	originalEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalEnabled })
	rates, err := GetChannelGroupMonitorCacheRates(context.Background(), []string{"vip"}, 1_750_031_100, 1_750_032_001)
	assert.ErrorIs(t, err, ErrChannelMonitorRedisSharedProjectionUnavailable)
	assert.Nil(t, rates)
	rates, err = GetChannelGroupMonitorCacheRates(context.Background(), nil, 1_750_031_100, 1_750_032_001)
	require.NoError(t, err)
	assert.Empty(t, rates)
}

func TestGetChannelGroupMonitorCacheRatesDoesNotMixLegacyBucketsWithTokenTotals(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	previousClient, previousEnabled := common.RDBMonitorRead, common.RedisEnabled
	common.RDBMonitorRead, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDBMonitorRead, common.RedisEnabled = previousClient, previousEnabled })
	ctx := t.Context()
	const now = int64(1_750_032_000)
	legacy := newChannelMonitorRedisSharedProjectionTestEvent("legacy", now)
	legacy.IsStream = true
	legacy.InputTokens, legacy.CacheReadTokens = common.GetPointer(int64(10000)), common.GetPointer(int64(10000))
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{legacy}))
	// An old writer populated only unfiltered tokens and request counters.
	scope := channelMonitorRedisSharedScopeGroup + ":" + channelMonitorRedisSharedDimension("vip") + ":"
	require.NoError(t, client.HDel(ctx, ChannelMonitorRedisDashboardMinuteKey(now),
		scope+channelMonitorRedisSharedMetricGroupCacheReadTokens,
		scope+channelMonitorRedisSharedMetricGroupCacheInputTokens,
	).Err())
	rates, err := GetChannelGroupMonitorCacheRates(ctx, []string{"vip"}, now-60, now+1)
	require.NoError(t, err)
	assert.Empty(t, rates, "old request counts cannot stand in for eligible token totals")
	next := legacy.Clone()
	next.EventId = "token-weighted"
	next.InputTokens, next.CacheReadTokens = common.GetPointer(int64(1000)), common.GetPointer(int64(600))
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{next}))
	rates, err = GetChannelGroupMonitorCacheRates(ctx, []string{"vip"}, now-60, now+1)
	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"vip": 60}, rates)
}

func TestGetChannelGroupMonitorCacheRatesFiltersStreamContextAtInclusiveBoundary(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	originalClient, originalEnabled := common.RDBMonitorRead, common.RedisEnabled
	common.RDBMonitorRead, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDBMonitorRead, common.RedisEnabled = originalClient, originalEnabled })
	previousPolicy := channelGroupMonitorCachePolicyState.Load()
	t.Cleanup(func() { channelGroupMonitorCachePolicyState.Store(previousPolicy) })
	channelGroupMonitorCachePolicyState.Store(&channelGroupMonitorCachePolicy{Revision: 1, MinContextK: 10})
	const now = int64(1_750_032_000)
	var events []model.ChannelMonitorEvent
	for _, fixture := range []struct {
		id     string
		input  *int64
		cache  int64
		stream bool
	}{
		{"below", common.GetPointer(int64(9999)), 0, true},
		{"at-boundary", common.GetPointer(int64(10000)), 4000, true},
		{"above", common.GetPointer(int64(20000)), 0, true},
		{"non-stream", common.GetPointer(int64(20000)), 10000, false},
		{"unknown-input", nil, 5000, true},
		{"zero-input", common.GetPointer(int64(0)), 5000, true},
	} {
		event := newChannelMonitorRedisSharedProjectionTestEvent(fixture.id, now-60)
		event.InputTokens, event.CacheReadTokens, event.IsStream = fixture.input, &fixture.cache, fixture.stream
		events = append(events, event)
	}
	probe := events[1]
	probe.EventId, probe.Source = "probe", model.ChannelMonitorEventSourceGroupProbe
	outside := events[1]
	outside.EventId, outside.OccurredAt = "outside", now-901
	private := events[1]
	private.EventId, private.GroupName = "private", "private"
	zero := events[2]
	zero.EventId, zero.GroupName = "zero", "zero"
	fallback := events[1]
	fallback.EventId, fallback.GroupName, fallback.InputTokens, fallback.PromptTokens = "fallback", "fallback", nil, common.GetPointer(int64(10000))
	events = append(events, probe, outside, private, zero, fallback)
	for index := range events {
		captureChannelGroupMonitorCachePolicy(&events[index])
	}
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), events))
	require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), events))
	for _, tc := range []struct {
		min  int
		want map[string]float64
	}{
		{10, map[string]float64{"vip": 4000.0 / 30000 * 100, "zero": 0, "fallback": 40}},
		{11, map[string]float64{"vip": 4000.0 / 30000 * 100, "zero": 0, "fallback": 40}},
		{21, map[string]float64{"vip": 4000.0 / 30000 * 100, "zero": 0, "fallback": 40}},
	} {
		channelGroupMonitorCachePolicyState.Store(&channelGroupMonitorCachePolicy{Revision: int64(tc.min), MinContextK: tc.min})
		rates, err := GetChannelGroupMonitorCacheRates(context.Background(), []string{"vip", "zero", "fallback", "empty"}, now-900, now+1)
		require.NoError(t, err)
		assert.Equal(t, tc.want, rates)
	}
	large := events[1]
	large.EventId, large.GroupName, large.InputTokens = "over-one-million", "large", common.GetPointer(int64(1_000_001))
	belowCap := large
	belowCap.EventId, belowCap.InputTokens, belowCap.CacheReadTokens = "below-one-million", common.GetPointer(int64(999_999)), common.GetPointer(int64(0))
	channelGroupMonitorCachePolicyState.Store(&channelGroupMonitorCachePolicy{MinContextK: model.ChannelGroupMonitorMaxCacheContextK})
	large.GroupCacheExcluded, belowCap.GroupCacheExcluded = nil, nil
	captureChannelGroupMonitorCachePolicy(&large)
	captureChannelGroupMonitorCachePolicy(&belowCap)
	require.NoError(t, projection.WriteChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{large, belowCap}))
	rates, err := GetChannelGroupMonitorCacheRates(context.Background(), []string{"large"}, now-900, now+1)
	require.NoError(t, err)
	assert.InDelta(t, 4000.0/1_000_001*100, rates["large"], 0.000001)
}

func TestChannelGroupMonitorCachePolicyFreezesQueuedSamples(t *testing.T) {
	previous := channelGroupMonitorCachePolicyState.Load()
	t.Cleanup(func() { channelGroupMonitorCachePolicyState.Store(previous) })
	channelGroupMonitorCachePolicyState.Store(nil)
	require.NoError(t, UpdateChannelGroupMonitorCachePolicy(model.ChannelGroupMonitorConfig{Revision: 1, GroupsJSON: `{"cache_min_context_k":10}`}))
	queued := newChannelMonitorRedisSharedProjectionTestEvent("queued", 1_750_032_000)
	queued.IsStream, queued.InputTokens = true, common.GetPointer(int64(10000))
	captureChannelGroupMonitorCachePolicy(&queued)
	require.NotNil(t, queued.GroupCacheExcluded)
	assert.False(t, *queued.GroupCacheExcluded)
	require.NoError(t, UpdateChannelGroupMonitorCachePolicy(model.ChannelGroupMonitorConfig{Revision: 2, GroupsJSON: `{"cache_min_context_k":20}`}))
	require.NoError(t, UpdateChannelGroupMonitorCachePolicy(model.ChannelGroupMonitorConfig{Revision: 1, GroupsJSON: `{"cache_min_context_k":0}`}))
	captureChannelGroupMonitorCachePolicy(&queued)
	assert.False(t, *queued.GroupCacheExcluded, "changing configuration cannot alter an event already queued")
	next := queued.Clone()
	next.GroupCacheExcluded = nil
	captureChannelGroupMonitorCachePolicy(&next)
	require.NotNil(t, next.GroupCacheExcluded)
	assert.True(t, *next.GroupCacheExcluded, "subsequent samples use the new threshold")
	payload, err := queued.Marshal()
	require.NoError(t, err)
	var replay model.ChannelMonitorEvent
	require.NoError(t, common.Unmarshal(payload, &replay))
	captureChannelGroupMonitorCachePolicy(&replay)
	require.NotNil(t, replay.GroupCacheExcluded)
	assert.False(t, *replay.GroupCacheExcluded, "serialized queue/outbox events keep their decision")
	require.NoError(t, UpdateChannelGroupMonitorCachePolicy(model.ChannelGroupMonitorConfig{Revision: 3, GroupsJSON: `{"cache_min_context_k":0}`}))
	next.IsStream, next.GroupCacheExcluded = false, nil
	captureChannelGroupMonitorCachePolicy(&next)
	assert.True(t, *next.GroupCacheExcluded, "zero removes the context threshold but still excludes non-stream samples")
}
