package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useChannelMonitorRedisRouteHealthTestProjection(
	t *testing.T,
) (*miniredis.Miniredis, *ChannelMonitorRedisRouteHealthProjection) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	projection, err := NewChannelMonitorRedisRouteHealthProjectionForClient(client)
	require.NoError(t, err)
	projection.settingsFn = func() model.ChannelMonitorSmartScheduleRealtimeSettings {
		return model.ChannelMonitorSmartScheduleRealtimeSettings{
			RetentionMinutes: 60,
			SampleLimit:      1000,
		}
	}
	return server, projection
}

func newChannelMonitorRedisRouteHealthTestEvent(
	eventID string,
	channelID int,
	modelName string,
	at int64,
	sequence uint64,
) model.ChannelMonitorEvent {
	return model.ChannelMonitorEvent{
		EventId:            eventID,
		EventSequence:      sequence,
		SchemaVersion:      model.ChannelMonitorEventSchemaVersion,
		OccurredAt:         at,
		CreatedAt:          at + 1,
		ChannelId:          channelID,
		ModelName:          modelName,
		Source:             model.ChannelMonitorEventSourceBusiness,
		Outcome:            model.ChannelMonitorEventOutcomeSuccess,
		CostStatus:         model.ChannelMonitorEventCostNone,
		RequestDispatched:  true,
		SchedulingEligible: true,
	}
}

func TestChannelMonitorRedisRouteHealthWindowOrdersDeduplicatesAndTrims(t *testing.T) {
	server, projection := useChannelMonitorRedisRouteHealthTestProjection(t)
	now := time.Unix(1_750_000_000, 0)
	server.SetTime(now)
	old := newChannelMonitorRedisRouteHealthTestEvent("old", 7, "model-a", now.Add(-time.Hour-time.Second).Unix(), 1)
	latest := newChannelMonitorRedisRouteHealthTestEvent("latest", 7, "model-a", now.Add(-10*time.Second).Unix(), 3)
	middle := newChannelMonitorRedisRouteHealthTestEvent("middle", 7, "model-a", now.Add(-20*time.Second).Unix(), 2)
	middle.RequestId = "request-raw-field"
	middle.OtherJson = `{"raw":"field"}`
	require.NoError(t, projection.HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{latest, old, middle, middle}))

	window, available, err := projection.GetRouteHealthWindow(context.Background(), 7, "model-a")
	require.NoError(t, err)
	require.True(t, available)
	require.Len(t, window.Samples, 2)
	assert.Equal(t, []string{"middle", "latest"}, []string{window.Samples[0].EventID, window.Samples[1].EventID})
	assert.Equal(t, int64(2), window.Snapshot.EventCount)
	assert.Equal(t, uint64(3), window.Snapshot.EventWatermark)
	assert.Equal(t, now.Add(-20*time.Second).Unix(), window.Snapshot.WindowStart)
	assert.Equal(t, now.Add(-10*time.Second).Unix(), window.Snapshot.WindowEnd)
	assert.Equal(t, 60, window.Snapshot.RetentionMinutes)
	assert.Equal(t, 1000, window.Snapshot.SampleLimit)
	rawSamples, err := projection.client.ZRange(context.Background(), ChannelMonitorRedisRouteHealthWindowKey(7, "model-a"), 0, -1).Result()
	require.NoError(t, err)
	assert.Len(t, rawSamples, 2)
	assert.False(t, strings.Contains(strings.Join(rawSamples, ""), "request-raw-field"))
	assert.False(t, strings.Contains(strings.Join(rawSamples, ""), `raw\":\"field`))
}

func TestChannelMonitorRedisRouteHealthWindowUsesConfiguredSampleLimitAndReportsTruncation(t *testing.T) {
	server, projection := useChannelMonitorRedisRouteHealthTestProjection(t)
	now := time.Unix(1_750_000_000, 0)
	server.SetTime(now)
	projection.settingsFn = func() model.ChannelMonitorSmartScheduleRealtimeSettings {
		return model.ChannelMonitorSmartScheduleRealtimeSettings{RetentionMinutes: 60, SampleLimit: 3}
	}
	require.NoError(t, projection.client.Set(
		context.Background(),
		channelMonitorRedisRouteHealthStartedAtKey,
		now.Add(-time.Hour).Unix(),
		0,
	).Err())
	events := make([]model.ChannelMonitorEvent, 0, 5)
	for index := 0; index < 5; index++ {
		events = append(events, newChannelMonitorRedisRouteHealthTestEvent(
			fmt.Sprintf("event-%02d", index), 7, "model-a", now.Add(time.Duration(index-5)*time.Second).Unix(), uint64(index+1),
		))
	}
	require.NoError(t, projection.HandleChannelMonitorEvents(context.Background(), events))

	window, available, err := projection.GetRouteHealthWindow(context.Background(), 7, "model-a")
	require.NoError(t, err)
	require.True(t, available)
	require.Len(t, window.Samples, 3)
	assert.Equal(t, "event-02", window.Samples[0].EventID)
	assert.Equal(t, "event-04", window.Samples[2].EventID)
	assert.True(t, window.Snapshot.SampleLimitTruncated)
	assert.Equal(t, events[1].OccurredAt, window.Snapshot.SampleLimitCutoffAt)
	assert.Equal(t, events[1].OccurredAt+1, window.Snapshot.CoverageStart)
}

func TestChannelMonitorRedisRouteHealthWindowUsesConfiguredRetention(t *testing.T) {
	server, projection := useChannelMonitorRedisRouteHealthTestProjection(t)
	now := time.Unix(1_750_000_000, 0)
	server.SetTime(now)
	projection.settingsFn = func() model.ChannelMonitorSmartScheduleRealtimeSettings {
		return model.ChannelMonitorSmartScheduleRealtimeSettings{RetentionMinutes: 120, SampleLimit: 1000}
	}
	require.NoError(t, projection.client.Set(
		context.Background(),
		channelMonitorRedisRouteHealthStartedAtKey,
		now.Add(-2*time.Hour).Unix(),
		0,
	).Err())
	event := newChannelMonitorRedisRouteHealthTestEvent("retained", 7, "model-a", now.Add(-90*time.Minute).Unix(), 1)
	require.NoError(t, projection.HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event}))

	window, available, err := projection.GetRouteHealthWindow(context.Background(), 7, "model-a")
	require.NoError(t, err)
	require.True(t, available)
	require.Len(t, window.Samples, 1)
	assert.Equal(t, "retained", window.Samples[0].EventID)
	assert.Equal(t, now.Add(-2*time.Hour).Unix(), window.Snapshot.CoverageStart)
	assert.Equal(t, 120, window.Snapshot.RetentionMinutes)
}

func TestChannelMonitorRedisRouteHealthWindowMarksRetentionIncreaseAsIncomplete(t *testing.T) {
	server, projection := useChannelMonitorRedisRouteHealthTestProjection(t)
	now := time.Unix(1_750_000_000, 0)
	server.SetTime(now)
	settings := model.ChannelMonitorSmartScheduleRealtimeSettings{RetentionMinutes: 60, SampleLimit: 1000}
	projection.settingsFn = func() model.ChannelMonitorSmartScheduleRealtimeSettings { return settings }
	require.NoError(t, projection.client.Set(
		context.Background(),
		channelMonitorRedisRouteHealthStartedAtKey,
		now.Add(-2*time.Hour).Unix(),
		0,
	).Err())
	event := newChannelMonitorRedisRouteHealthTestEvent("current", 7, "model-a", now.Add(-30*time.Minute).Unix(), 1)
	require.NoError(t, projection.HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event}))
	settings.RetentionMinutes = 120

	window, available, err := projection.GetRouteHealthWindow(context.Background(), 7, "model-a")
	require.NoError(t, err)
	require.True(t, available)
	assert.Equal(t, now.Add(-time.Hour).Unix(), window.Snapshot.CoverageStart)
	assert.Equal(t, 120, window.Snapshot.RetentionMinutes)
}

func TestChannelMonitorRedisRouteHealthProjectionHasNoGlobalRouteLimitAndReadsConsistently(t *testing.T) {
	server, projection := useChannelMonitorRedisRouteHealthTestProjection(t)
	now := time.Unix(1_750_000_000, 0)
	server.SetTime(now)
	events := make([]model.ChannelMonitorEvent, 0, 513)
	for channelID := 1; channelID <= 513; channelID++ {
		events = append(events, newChannelMonitorRedisRouteHealthTestEvent(
			fmt.Sprintf("route-%04d", channelID), channelID, "model-a", now.Unix(), uint64(channelID),
		))
	}
	require.NoError(t, projection.HandleChannelMonitorEvents(context.Background(), events))
	indexSize, err := projection.client.ZCard(context.Background(), ChannelMonitorRedisRouteHealthIndexKey()).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(513), indexSize)
	windows, err := projection.ListRouteHealthWindows(context.Background())
	require.NoError(t, err)
	assert.Len(t, windows, 513)
	secondProjection, err := NewChannelMonitorRedisRouteHealthProjectionForClient(projection.client)
	require.NoError(t, err)
	secondProjection.settingsFn = projection.settingsFn
	first, firstAvailable, err := projection.GetRouteHealthWindow(context.Background(), 513, "model-a")
	require.NoError(t, err)
	second, secondAvailable, err := secondProjection.GetRouteHealthWindow(context.Background(), 513, "model-a")
	require.NoError(t, err)
	assert.True(t, firstAvailable)
	assert.True(t, secondAvailable)
	assert.Equal(t, first, second)
}

func TestChannelMonitorRedisRouteHealthProjectionPrunesExpiredHighCardinalityIndex(t *testing.T) {
	server, projection := useChannelMonitorRedisRouteHealthTestProjection(t)
	now := time.Unix(1_750_000_000, 0)
	server.SetTime(now)
	const expiredRouteCount = 2048
	expiredRoutes := make([]*redis.Z, 0, expiredRouteCount)
	for channelID := 1; channelID <= expiredRouteCount; channelID++ {
		expiredRoutes = append(expiredRoutes, &redis.Z{
			Score:  float64(now.Add(-time.Minute).UnixMilli()),
			Member: ChannelMonitorRedisRouteHealthWindowKey(channelID, "expired-model"),
		})
	}
	require.NoError(t, projection.client.ZAdd(
		context.Background(), ChannelMonitorRedisRouteHealthIndexKey(), expiredRoutes...,
	).Err())
	windows, err := projection.ListRouteHealthWindows(context.Background())
	require.NoError(t, err)
	assert.Empty(t, windows)
	indexSize, err := projection.client.ZCard(context.Background(), ChannelMonitorRedisRouteHealthIndexKey()).Result()
	require.NoError(t, err)
	assert.Zero(t, indexSize)
}

func TestChannelMonitorRedisRouteHealthProjectionRemovesMissingWindowFromActiveIndex(t *testing.T) {
	server, projection := useChannelMonitorRedisRouteHealthTestProjection(t)
	now := time.Unix(1_750_000_000, 0)
	server.SetTime(now)
	missingKey := ChannelMonitorRedisRouteHealthWindowKey(99, "missing-model")
	require.NoError(t, projection.client.ZAdd(
		context.Background(),
		ChannelMonitorRedisRouteHealthIndexKey(),
		&redis.Z{Score: float64(now.Add(time.Hour).UnixMilli()), Member: missingKey},
	).Err())

	windows, err := projection.ListRouteHealthWindows(context.Background())
	require.NoError(t, err)
	assert.Empty(t, windows)
	indexSize, err := projection.client.ZCard(context.Background(), ChannelMonitorRedisRouteHealthIndexKey()).Result()
	require.NoError(t, err)
	assert.Zero(t, indexSize)
}

func TestChannelMonitorRedisRouteHealthProjectionRemovesCorruptRouteFromActiveIndex(t *testing.T) {
	server, projection := useChannelMonitorRedisRouteHealthTestProjection(t)
	now := time.Unix(1_750_000_000, 0)
	server.SetTime(now)
	corruptKey := ChannelMonitorRedisRouteHealthWindowKey(99, "corrupt-model")
	metaKey := channelMonitorRedisRouteHealthMetaKeyFromWindowKey(corruptKey)
	require.NoError(t, projection.client.ZAdd(
		context.Background(),
		corruptKey,
		&redis.Z{Score: float64(now.Unix()), Member: `{}`},
	).Err())
	require.NoError(t, projection.client.HSet(context.Background(), metaKey, map[string]interface{}{
		"channel_id": "invalid",
		"model_name": "corrupt-model",
	}).Err())
	require.NoError(t, projection.client.ZAdd(
		context.Background(),
		ChannelMonitorRedisRouteHealthIndexKey(),
		&redis.Z{Score: float64(now.Add(time.Hour).UnixMilli()), Member: corruptKey},
	).Err())

	windows, err := projection.ListRouteHealthWindows(context.Background())
	require.NoError(t, err)
	assert.Empty(t, windows)
	existingKeys, err := projection.client.Exists(context.Background(), corruptKey, metaKey).Result()
	require.NoError(t, err)
	assert.Zero(t, existingKeys)
	indexSize, err := projection.client.ZCard(context.Background(), ChannelMonitorRedisRouteHealthIndexKey()).Result()
	require.NoError(t, err)
	assert.Zero(t, indexSize)
}

func TestChannelMonitorRedisRouteHealthProjectionConcurrentWritesConverge(t *testing.T) {
	server, projection := useChannelMonitorRedisRouteHealthTestProjection(t)
	now := time.Unix(1_750_000_000, 0)
	server.SetTime(now)
	const eventCount = 32
	errs := make(chan error, eventCount)
	var waitGroup sync.WaitGroup
	for index := 0; index < eventCount; index++ {
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			event := newChannelMonitorRedisRouteHealthTestEvent(
				fmt.Sprintf("concurrent-%02d", index), 7, "model-a", now.Unix()-int64(index), uint64(index+1),
			)
			errs <- projection.HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event})
		}()
	}
	waitGroup.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	window, available, err := projection.GetRouteHealthWindow(context.Background(), 7, "model-a")
	require.NoError(t, err)
	require.True(t, available)
	assert.Len(t, window.Samples, eventCount)
	assert.Equal(t, "concurrent-31", window.Samples[0].EventID)
	assert.Equal(t, "concurrent-00", window.Samples[len(window.Samples)-1].EventID)
}

func TestChannelMonitorRedisRouteHealthProjectionSkipsNonSchedulingEvents(t *testing.T) {
	server, projection := useChannelMonitorRedisRouteHealthTestProjection(t)
	now := time.Unix(1_750_000_000, 0)
	server.SetTime(now)
	event := newChannelMonitorRedisRouteHealthTestEvent("not-eligible", 7, "model-a", now.Unix(), 1)
	event.SchedulingEligible = false
	require.NoError(t, projection.HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event}))
	_, available, err := projection.GetRouteHealthWindow(context.Background(), 7, "model-a")
	require.NoError(t, err)
	assert.False(t, available)
}
