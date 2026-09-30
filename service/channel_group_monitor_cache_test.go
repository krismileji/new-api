package service

import (
	"context"
	"encoding/base64"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelGroupMonitorIncrementalWindowReplayAndRetirement(t *testing.T) {
	server, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	if address := os.Getenv("GROUP_MONITOR_TEST_REDIS_ADDR"); address != "" {
		client = redis.NewClient(&redis.Options{Addr: address, DB: 1})
		t.Cleanup(func() { require.NoError(t, client.Close()) })
	}
	oldWrite, oldEnabled := common.RDBMonitorWrite, common.RedisEnabled
	common.RDBMonitorWrite, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDBMonitorWrite, common.RedisEnabled = oldWrite, oldEnabled })
	ctx := t.Context()
	now := time.Now().Unix()
	day := model.ChannelStatusProbeDisplayBucketStart(now, "day")
	config := model.ChannelGroupMonitorConfig{Revision: now, Enabled: true, DisplayValue: 3, DisplayUnit: "day", GroupsJSON: `{"groups":[{"group_name":"vip","probe_model":"gpt-test"}]}`}
	generation, err := SyncChannelGroupMonitorGeneration(ctx, config)
	require.NoError(t, err)
	generation.StartedAt = day - 3*86400
	payload, err := common.Marshal(generation)
	require.NoError(t, err)
	require.NoError(t, client.Set(ctx, channelGroupMonitorConfigurationKey, payload, 0).Err())
	var events []model.ChannelMonitorEvent
	for _, fixture := range []struct {
		id              string
		key             int
		at, input, read int64
	}{
		{"old-key-a", 1, day - 2*86400, 100, 100},
		{"new-key-a", 1, day, 300, 0},
		{"key-b", 2, day, 100, 50},
		{"unknown", 0, day, 100, 100},
	} {
		event := newChannelMonitorRedisSharedProjectionTestEvent(fixture.id, fixture.at)
		event.CreatedAt, event.APIKeyId = now, fixture.key
		event.GroupMonitorGeneration, event.GroupCacheExcluded, event.IsStream = generation.ID, common.GetPointer(false), true
		event.InputTokens, event.CacheReadTokens = common.GetPointer(fixture.input), common.GetPointer(fixture.read)
		events = append(events, event)
	}
	// Overlapping delivery by two owners remains exactly once, including a lost
	// script cache after Redis failover and an older event delivered last.
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errors <- ProjectChannelGroupMonitorEvents(ctx, client, events, now) })
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	require.NoError(t, client.ScriptFlush(ctx).Err())
	require.NoError(t, ProjectChannelGroupMonitorEvents(ctx, client, events, now))
	groups, err := ReadChannelGroupMonitorProjection(ctx, generation, now)
	require.NoError(t, err)
	assert.InDelta(t, 37.5, *groups["vip"].Cache.APIKeyAverage, 1e-9)
	assert.InDelta(t, 50, *groups["vip"].Cache.APIKeyMax, 1e-9)
	assert.InDelta(t, 250.0/600*100, *groups["vip"].Cache.Weighted, 1e-9)
	// One bucket leaves: Key A falls to zero but still counts in the average.
	groups, err = ReadChannelGroupMonitorProjection(ctx, generation, day+86400)
	require.NoError(t, err)
	assert.InDelta(t, 25, *groups["vip"].Cache.APIKeyAverage, 1e-9)
	assert.InDelta(t, 50, *groups["vip"].Cache.APIKeyMax, 1e-9)
	assert.InDelta(t, 30, *groups["vip"].Cache.Weighted, 1e-9)
	// A paused reader cannot move the window backwards and resurrect a retired bucket.
	late := events[0].Clone()
	late.EventId = "late-retired"
	require.NoError(t, ProjectChannelGroupMonitorEvents(ctx, client, []model.ChannelMonitorEvent{late}, now))
	groups, err = ReadChannelGroupMonitorProjection(ctx, generation, day+86400)
	require.NoError(t, err)
	assert.InDelta(t, 25, *groups["vip"].Cache.APIKeyAverage, 1e-9)
	// Expiring dedup markers must not allow old outbox/stream payloads to count again.
	if os.Getenv("GROUP_MONITOR_TEST_REDIS_ADDR") == "" {
		server.FastForward(50 * time.Hour)
	} else {
		seen := generation.Key("seen:" + strconv.FormatInt(now/3600*3600, 10))
		require.NoError(t, client.Del(ctx, seen).Err())
	}
	require.NoError(t, ProjectChannelGroupMonitorEvents(ctx, client, events, now+50*3600))
	groups, err = ReadChannelGroupMonitorProjection(ctx, generation, day+2*86400)
	require.NoError(t, err)
	assert.InDelta(t, 25, *groups["vip"].Cache.APIKeyAverage, 1e-9)
	// Long downtime is recovered by subtracting remaining buckets exactly once.
	groups, err = ReadChannelGroupMonitorProjection(ctx, generation, day+10*86400)
	require.NoError(t, err)
	assert.Nil(t, groups["vip"].Cache.APIKeyAverage)
	assert.Nil(t, groups["vip"].Cache.APIKeyMax)
	assert.Nil(t, groups["vip"].Cache.Weighted)
	prefix := generation.Key("group:" + base64.RawURLEncoding.EncodeToString([]byte("vip")) + ":")
	assert.Zero(t, client.HLen(ctx, prefix+"totals").Val())
	assert.Zero(t, client.ZCard(ctx, prefix+"buckets").Val())
	// A bucket larger than one retirement chunk can resume after a worker dies
	// between commits. All Keys disappear, with no negative or leftover sums.
	later := day + 10*86400
	var many []model.ChannelMonitorEvent
	// 600 fields exceed Redis's default compact-hash threshold, exercising
	// cursor traversal through a shrinking hash table rather than one listpack.
	for key := range 300 {
		event := events[0].Clone()
		event.EventId, event.APIKeyId = "retirement-"+strconv.Itoa(key), key+1
		event.CreatedAt, event.OccurredAt = later, later
		many = append(many, event)
	}
	require.NoError(t, ProjectChannelGroupMonitorEvents(ctx, client, many, later))
	_, err = expireChannelGroupMonitorCacheChunk.Run(ctx, client, []string{
		channelGroupMonitorGenerationIDKey, prefix + "totals", prefix + "rates", prefix + "summary",
		prefix + "buckets", prefix + "floor", generation.Key("keys"), prefix + "retirement",
	}, generation.ID, later+86400).Int()
	require.NoError(t, err)
	groups, err = ReadChannelGroupMonitorProjection(ctx, generation, later+3*86400)
	require.NoError(t, err)
	assert.Nil(t, groups["vip"].Cache.APIKeyAverage)
	assert.Nil(t, groups["vip"].Cache.Weighted)
	assert.Zero(t, client.HLen(ctx, prefix+"totals").Val())
	assert.Zero(t, client.HLen(ctx, prefix+"retirement").Val())
}

func TestChannelGroupMonitorRedisProjectionLifecycle(t *testing.T) {
	server, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	if address := os.Getenv("GROUP_MONITOR_TEST_REDIS_ADDR"); address != "" {
		client = redis.NewClient(&redis.Options{Addr: address})
		t.Cleanup(func() { require.NoError(t, client.Close()) })
	}
	ctx := t.Context()
	oldWrite, oldRead, oldEnabled, oldPolicy := common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled, channelGroupMonitorCachePolicyState.Load()
	common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled = client, client, true
	channelGroupMonitorCachePolicyState.Store(nil)
	t.Cleanup(func() {
		common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled = oldWrite, oldRead, oldEnabled
		channelGroupMonitorCachePolicyState.Store(oldPolicy)
	})
	config := model.ChannelGroupMonitorConfig{Revision: 1, Enabled: true, DisplayValue: 2, DisplayUnit: "minute", GroupsJSON: `{"groups":[{"group_name":"vip","probe_model":"gpt-test"}]}`}
	require.NoError(t, UpdateChannelGroupMonitorCachePolicy(config))
	generation, err := SyncChannelGroupMonitorGeneration(ctx, config)
	require.NoError(t, err)
	now := generation.StartedAt
	var events []model.ChannelMonitorEvent
	for _, sample := range []struct {
		id          string
		key         int
		input, read int64
	}{
		{"a1", 1, 100, 20}, {"a2", 1, 100, 80}, {"b", 2, 10, 0}, {"unknown", 0, 100, 100},
	} {
		event := newChannelMonitorRedisSharedProjectionTestEvent(sample.id, now)
		event.APIKeyId, event.IsStream = sample.key, true
		event.InputTokens, event.CacheReadTokens = &sample.input, &sample.read
		captureChannelGroupMonitorCachePolicy(&event)
		events = append(events, event)
	}
	// The same key combines different routes before its percentage is calculated.
	events[1].ChannelId, events[1].ModelName = 8, "other-model"
	for _, id := range []string{"non-stream", "filtered", "other-group", "physical-probe"} {
		event := events[0].Clone()
		event.EventId = id
		switch id {
		case "non-stream":
			event.IsStream = false
		case "filtered":
			event.GroupCacheExcluded = common.GetPointer(true)
		case "other-group":
			event.GroupName = "other"
		case "physical-probe":
			event.Source = model.ChannelMonitorEventSourceGroupProbe
		}
		events = append(events, event)
	}
	require.NoError(t, ProjectChannelGroupMonitorEvents(ctx, client, events, now))
	require.NoError(t, ProjectChannelGroupMonitorEvents(ctx, client, events, now))
	groups, err := ReadChannelGroupMonitorProjection(ctx, generation, now)
	require.NoError(t, err)
	require.NotNil(t, groups["vip"].Cache.APIKeyAverage)
	assert.Equal(t, 25.0, *groups["vip"].Cache.APIKeyAverage)
	assert.Equal(t, 50.0, *groups["vip"].Cache.APIKeyMax)
	assert.InDelta(t, 200.0/310*100, *groups["vip"].Cache.Weighted, 0.000001)
	assert.NotContains(t, groups, "other")

	// A logical result is one sample, independent of its physical attempts;
	// late results and skipped probes cannot replace the latest health state.
	for _, sample := range []struct {
		id     int64
		at     int64
		result string
	}{
		{3, now + 3, model.ChannelGroupMonitorResultRateLimited},
		{1, now + 1, model.ChannelGroupMonitorResultSuccess},
		{4, now + 4, model.ChannelGroupMonitorResultSkipped},
	} {
		probe := model.ChannelGroupMonitorExecution{Id: sample.id, RunId: strconv.FormatInt(sample.id, 10), GroupName: "vip", ConfigRevision: 1, StartedAt: now, FinishedAt: sample.at, Result: sample.result}
		event := model.ChannelGroupMonitorExecutionEvent(probe)
		require.NotNil(t, event)
		require.NoError(t, event.Validate())
		require.NoError(t, ProjectChannelGroupMonitorEvents(ctx, client, []model.ChannelMonitorEvent{*event, *event}, now+4))
	}
	groups, err = ReadChannelGroupMonitorProjection(ctx, generation, now+4)
	require.NoError(t, err)
	require.NotNil(t, groups["vip"].State)
	assert.Equal(t, model.ChannelGroupMonitorResultRateLimited, groups["vip"].State.Result)
	var successes, failures, skips float64
	for _, bucket := range groups["vip"].Buckets {
		successes += bucket.Counts["success"]
		failures += bucket.Counts["rate_limited"]
		skips += bucket.Counts["skipped"]
	}
	assert.Equal(t, 1.0, successes)
	assert.Equal(t, 1.0, failures)
	assert.Equal(t, 1.0, skips)
	probeEvent := model.ChannelGroupMonitorExecutionEvent(model.ChannelGroupMonitorExecution{Id: 5, RunId: "timeout", GroupName: "vip", ConfigRevision: 1, StartedAt: now, FinishedAt: now + 4, Result: model.ChannelGroupMonitorResultTimeout})
	require.NotNil(t, probeEvent)
	called := false
	channelHandler := ChannelMonitorRedisEventHandlerFunc(func(context.Context, []model.ChannelMonitorEvent) error { called = true; return nil })
	aggregator, err := newChannelMonitorRedisLogicalAggregator(client, channelHandler, channelHandler, func(context.Context, []model.ChannelMonitorEvent) error { called = true; return nil }, nil)
	require.NoError(t, err)
	// Use a completed probe at the current wall clock for the real consumer entry.
	probeEvent.OccurredAt, probeEvent.GroupMonitorProbe.FinishedAt = now, now
	require.NoError(t, aggregator.HandleChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{*probeEvent}))
	assert.False(t, called, "logical probes must never enter channel health, billing or scheduling handlers")

	require.NoError(t, PublishChannelGroupMonitorSnapshot(ctx, generation, map[string]any{"ready": true}))
	var snapshot map[string]any
	require.NoError(t, ReadChannelGroupMonitorSnapshot(ctx, &snapshot))
	assert.Equal(t, true, snapshot["ready"])
	version, err := ChannelGroupMonitorProjectionVersion(ctx, generation)
	require.NoError(t, err)
	lease := ChannelGroupMonitorRedisPrefix + "snapshot_lease"
	deadline := time.Now().Unix() + 600
	require.NoError(t, client.Set(ctx, lease, "owner", 15*time.Second).Err())
	require.NoError(t, PublishChannelGroupMonitorSnapshot(ctx, generation, map[string]any{"event_version": version, "next_refresh_at": deadline}, lease, "owner"))
	renewed, err := RenewChannelGroupMonitorSnapshot(ctx, generation, version, deadline, lease, "owner")
	require.NoError(t, err)
	assert.True(t, renewed)
	assert.Positive(t, client.TTL(ctx, ChannelGroupMonitorSnapshotKey).Val())
	renewed, err = RenewChannelGroupMonitorSnapshot(ctx, generation, version, deadline, lease, "other-owner")
	require.NoError(t, err)
	assert.False(t, renewed)
	same, err := SyncChannelGroupMonitorGeneration(ctx, config)
	require.NoError(t, err)
	assert.Equal(t, generation.ID, same.ID)
	// Windows expire without new events; snapshots also expire if the worker stops.
	groups, err = ReadChannelGroupMonitorProjection(ctx, generation, now+180)
	require.NoError(t, err)
	assert.Nil(t, groups["vip"].Cache.APIKeyMax)
	if os.Getenv("GROUP_MONITOR_TEST_REDIS_ADDR") == "" {
		server.FastForward(31 * time.Second)
		assert.ErrorIs(t, ReadChannelGroupMonitorSnapshot(ctx, &snapshot), ErrChannelGroupMonitorSnapshotPending)
	}

	config.Revision = 2
	require.NoError(t, UpdateChannelGroupMonitorCachePolicy(config))
	next, err := SyncChannelGroupMonitorGeneration(ctx, config)
	require.NoError(t, err)
	assert.NotEqual(t, generation.ID, next.ID)
	assert.ErrorIs(t, ReadChannelGroupMonitorSnapshot(ctx, &snapshot), ErrChannelGroupMonitorSnapshotPending)
	assert.ErrorIs(t, PublishChannelGroupMonitorSnapshot(ctx, generation, map[string]any{"stale": true}), ErrChannelGroupMonitorSnapshotPending)
	oldKeys, err := client.Keys(ctx, generation.Key("*")).Result()
	require.NoError(t, err)
	assert.Empty(t, oldKeys)
	require.NoError(t, ProjectChannelGroupMonitorEvents(ctx, client, events, now+4))
	groups, err = ReadChannelGroupMonitorProjection(ctx, next, now+4)
	require.NoError(t, err)
	assert.Nil(t, groups["vip"].Cache.Weighted)
	assert.Nil(t, groups["vip"].State)
	// A fresh event is attributed only once to the new generation.
	fresh := events[0].Clone()
	fresh.EventId = "new-generation"
	fresh.GroupCacheExcluded = nil
	fresh.OccurredAt = next.StartedAt
	captureChannelGroupMonitorCachePolicy(&fresh)
	require.NoError(t, ProjectChannelGroupMonitorEvents(ctx, client, []model.ChannelMonitorEvent{fresh}, next.StartedAt))
	groups, err = ReadChannelGroupMonitorProjection(ctx, next, next.StartedAt)
	require.NoError(t, err)
	assert.Equal(t, 20.0, *groups["vip"].Cache.APIKeyAverage)
	// Route fingerprint initialization keeps samples; a membership change resets.
	bound, err := SyncChannelGroupMonitorGeneration(ctx, config, "routes-a")
	require.NoError(t, err)
	assert.Equal(t, next.ID, bound.ID)
	changed, err := SyncChannelGroupMonitorGeneration(ctx, config, "routes-b")
	require.NoError(t, err)
	assert.NotEqual(t, next.ID, changed.ID)
	// A schema upgrade must reset the old layout even with the same revision.
	legacy := changed
	legacy.Schema = 1
	legacyPayload, err := common.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, client.Set(ctx, channelGroupMonitorConfigurationKey, legacyPayload, 0).Err())
	upgraded, err := SyncChannelGroupMonitorGeneration(ctx, config)
	require.NoError(t, err)
	assert.NotEqual(t, changed.ID, upgraded.ID)
	// Redis config loss starts a fresh generation; retained stream data is fenced.
	require.NoError(t, client.Del(ctx, channelGroupMonitorConfigurationKey).Err())
	recovered, err := SyncChannelGroupMonitorGeneration(ctx, config)
	require.NoError(t, err)
	assert.NotEqual(t, upgraded.ID, recovered.ID)
}

func TestChannelGroupMonitorRedisProjectionDisplayWindows(t *testing.T) {
	for _, unit := range []string{"minute", "hour", "day"} {
		t.Run(unit, func(t *testing.T) {
			_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
			oldWrite, oldEnabled := common.RDBMonitorWrite, common.RedisEnabled
			common.RDBMonitorWrite, common.RedisEnabled = client, true
			t.Cleanup(func() { common.RDBMonitorWrite, common.RedisEnabled = oldWrite, oldEnabled })
			config := model.ChannelGroupMonitorConfig{Revision: 1, Enabled: true, DisplayValue: 30, DisplayUnit: unit, GroupsJSON: `{"groups":[{"group_name":"vip","probe_model":"gpt-test"}]}`}
			if unit == "hour" {
				config.DisplayValue = 24
			}
			generation, err := SyncChannelGroupMonitorGeneration(t.Context(), config)
			require.NoError(t, err)
			now := time.Now().Unix()
			seconds := model.ChannelStatusProbeDisplayBucketSeconds(unit)
			start := model.ChannelStatusProbeDisplayBucketStart(now, unit) - int64(config.DisplayValue-1)*seconds
			generation.StartedAt = start - 1
			payload, err := common.Marshal(generation)
			require.NoError(t, err)
			require.NoError(t, client.Set(t.Context(), channelGroupMonitorConfigurationKey, payload, 0).Err())
			var events []model.ChannelMonitorEvent
			for _, sample := range []struct {
				id       string
				at, read int64
			}{{"expired", start - 1, 100}, {"first", start, 0}, {"last", now, 100}} {
				event := newChannelMonitorRedisSharedProjectionTestEvent(sample.id, sample.at)
				event.CreatedAt = now
				event.GroupMonitorGeneration, event.GroupCacheExcluded = generation.ID, common.GetPointer(false)
				event.IsStream = true
				event.InputTokens = common.GetPointer(int64(100))
				event.CacheReadTokens = &sample.read
				events = append(events, event)
			}
			require.NoError(t, ProjectChannelGroupMonitorEvents(t.Context(), client, events, now))
			groups, err := ReadChannelGroupMonitorProjection(t.Context(), generation, now)
			require.NoError(t, err)
			require.NotNil(t, groups["vip"].Cache.APIKeyAverage)
			assert.Equal(t, 50.0, *groups["vip"].Cache.APIKeyAverage)
			assert.Len(t, groups["vip"].Buckets, config.DisplayValue)
			assert.Equal(t, start, groups["vip"].Buckets[0].StartedAt)
		})
	}
}

func TestChannelGroupMonitorSnapshotHeartbeatPreservesQuietDataAndPendingEvents(t *testing.T) {
	server, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	oldWrite, oldRead, oldEnabled := common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled
	common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled = client, client, true
	t.Cleanup(func() {
		common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled = oldWrite, oldRead, oldEnabled
	})
	config := model.ChannelGroupMonitorConfig{Revision: 1, Enabled: true, DisplayValue: 24, DisplayUnit: "hour", GroupsJSON: `{"groups":[{"group_name":"vip","probe_model":"gpt-test"}]}`}
	generation, err := SyncChannelGroupMonitorGeneration(t.Context(), config)
	require.NoError(t, err)
	now := time.Now().Unix()
	server.SetTime(time.Unix(now, 0))
	lease := ChannelGroupMonitorRedisPrefix + "snapshot_lease"
	snapshot := map[string]any{"event_version": "0", "next_refresh_at": now + 600, "ready": true}
	require.NoError(t, PublishChannelGroupMonitorSnapshot(t.Context(), generation, snapshot))
	server.FastForward(20 * time.Second)
	require.NoError(t, client.Set(t.Context(), lease, "owner", 15*time.Second).Err())
	renewed, err := RenewChannelGroupMonitorSnapshot(t.Context(), generation, "0", now+600, lease, "owner")
	require.NoError(t, err)
	assert.True(t, renewed)
	server.FastForward(20 * time.Second)
	var read map[string]any
	require.NoError(t, ReadChannelGroupMonitorSnapshot(t.Context(), &read), "quiet data survives beyond its initial TTL while checks succeed")
	event := newChannelMonitorRedisSharedProjectionTestEvent("new", now)
	event.GroupMonitorGeneration, event.GroupCacheExcluded = generation.ID, common.GetPointer(false)
	event.IsStream, event.InputTokens = true, common.GetPointer(int64(100))
	require.NoError(t, ProjectChannelGroupMonitorEvents(t.Context(), client, []model.ChannelMonitorEvent{event, event}, now))
	version, err := ChannelGroupMonitorProjectionVersion(t.Context(), generation)
	require.NoError(t, err)
	assert.Equal(t, "1", version, "replay must not dirty the snapshot twice")
	// Simulate a builder publishing a version captured before a concurrent event.
	require.NoError(t, PublishChannelGroupMonitorSnapshot(t.Context(), generation, snapshot))
	require.NoError(t, client.Set(t.Context(), lease, "owner", 15*time.Second).Err())
	renewed, err = RenewChannelGroupMonitorSnapshot(t.Context(), generation, "0", now+600, lease, "owner")
	require.NoError(t, err)
	assert.False(t, renewed, "a new event remains pending after an older build")
	snapshot["event_version"] = "1"
	require.NoError(t, PublishChannelGroupMonitorSnapshot(t.Context(), generation, snapshot))
	renewed, err = RenewChannelGroupMonitorSnapshot(t.Context(), generation, "1", now+600, lease, "replaced-owner")
	require.NoError(t, err)
	assert.False(t, renewed)
	server.SetTime(time.Unix(now+600, 0))
	renewed, err = RenewChannelGroupMonitorSnapshot(t.Context(), generation, "1", now+600, lease, "owner")
	require.NoError(t, err)
	assert.False(t, renewed, "due state cannot be kept alive by a heartbeat")
	server.FastForward(31 * time.Second)
	assert.ErrorIs(t, ReadChannelGroupMonitorSnapshot(t.Context(), &read), ErrChannelGroupMonitorSnapshotPending)
}

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
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	oldWrite, oldEnabled := common.RDBMonitorWrite, common.RedisEnabled
	common.RDBMonitorWrite, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDBMonitorWrite, common.RedisEnabled = oldWrite, oldEnabled })
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

// Real Redis only: compare snapshot read cost as Key cardinality and group count
// grow. Fixture ingestion is outside the timed section; this is not a gateway QPS test.
func BenchmarkChannelGroupMonitorIncrementalRead(b *testing.B) {
	address := os.Getenv("GROUP_MONITOR_TEST_REDIS_ADDR")
	if address == "" {
		b.Skip("requires disposable Redis")
	}
	client := redis.NewClient(&redis.Options{Addr: address, DB: 2})
	b.Cleanup(func() { require.NoError(b, client.Close()) })
	oldWrite, oldEnabled := common.RDBMonitorWrite, common.RedisEnabled
	common.RDBMonitorWrite, common.RedisEnabled = client, true
	b.Cleanup(func() { common.RDBMonitorWrite, common.RedisEnabled = oldWrite, oldEnabled })
	for _, fixture := range []struct {
		name         string
		groups, keys int
	}{
		{"1group_100keys", 1, 100}, {"1group_10000keys", 1, 10000}, {"100groups_100keys", 100, 100},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			now := time.Now().Unix()
			groups := make([]model.ChannelGroupMonitorGroup, fixture.groups)
			for i := range groups {
				groups[i] = model.ChannelGroupMonitorGroup{GroupName: "bench-" + strconv.Itoa(i), ProbeModel: "gpt-test"}
			}
			payload, err := common.Marshal(map[string]any{"groups": groups})
			require.NoError(b, err)
			config := model.ChannelGroupMonitorConfig{Revision: time.Now().UnixMilli(), Enabled: true, DisplayValue: 24, DisplayUnit: "hour", GroupsJSON: string(payload)}
			generation, err := SyncChannelGroupMonitorGeneration(b.Context(), config)
			require.NoError(b, err)
			generation.StartedAt = now - 7200
			payload, err = common.Marshal(generation)
			require.NoError(b, err)
			require.NoError(b, client.Set(b.Context(), channelGroupMonitorConfigurationKey, payload, 0).Err())
			batch := make([]model.ChannelMonitorEvent, 0, 100)
			for groupIndex, group := range groups {
				for key := range fixture.keys {
					for bucket := range 2 {
						event := newChannelMonitorRedisSharedProjectionTestEvent(strconv.Itoa(groupIndex)+":"+strconv.Itoa(key)+":"+strconv.Itoa(bucket), now-int64(bucket)*3600)
						event.CreatedAt, event.GroupName, event.APIKeyId = now, group.GroupName, key+1
						event.GroupMonitorGeneration, event.GroupCacheExcluded, event.IsStream = generation.ID, common.GetPointer(false), true
						event.InputTokens, event.CacheReadTokens = common.GetPointer(int64(100)), common.GetPointer(int64(key%101))
						batch = append(batch, event)
						if len(batch) == cap(batch) {
							require.NoError(b, ProjectChannelGroupMonitorEvents(b.Context(), client, batch, now))
							batch = batch[:0]
						}
					}
				}
			}
			require.NoError(b, ProjectChannelGroupMonitorEvents(b.Context(), client, batch, now))
			_, err = ReadChannelGroupMonitorProjection(b.Context(), generation, now)
			require.NoError(b, err)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				result, err := ReadChannelGroupMonitorProjection(b.Context(), generation, now)
				require.NoError(b, err)
				require.Len(b, result, fixture.groups)
			}
		})
	}
}
