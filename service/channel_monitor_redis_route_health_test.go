package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelMonitorRedisRouteHealthRefreshHook struct {
	key       string
	payload   []byte
	writer    *redis.Client
	mu        sync.Mutex
	getCount  int
	triggered bool
}

func (hook *channelMonitorRedisRouteHealthRefreshHook) BeforeProcess(
	ctx context.Context,
	cmd redis.Cmder,
) (context.Context, error) {
	if cmd.Name() != "get" || len(cmd.Args()) < 2 || fmt.Sprint(cmd.Args()[1]) != hook.key {
		return ctx, nil
	}
	hook.mu.Lock()
	defer hook.mu.Unlock()
	hook.getCount++
	if hook.getCount != 2 {
		return ctx, nil
	}
	hook.triggered = true
	return ctx, hook.writer.Set(ctx, hook.key, hook.payload, time.Hour).Err()
}

func (hook *channelMonitorRedisRouteHealthRefreshHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (hook *channelMonitorRedisRouteHealthRefreshHook) BeforeProcessPipeline(
	ctx context.Context,
	_ []redis.Cmder,
) (context.Context, error) {
	return ctx, nil
}

func (hook *channelMonitorRedisRouteHealthRefreshHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func useChannelMonitorRedisRouteHealthTestProjection(t *testing.T) (*miniredis.Miniredis, *ChannelMonitorRedisRouteHealthProjection) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	projection, err := NewChannelMonitorRedisRouteHealthProjectionForClient(client)
	require.NoError(t, err)
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
	old := newChannelMonitorRedisRouteHealthTestEvent("old", 7, "model-a", now.Unix()-int64(time.Hour/time.Second)-1, 1)
	latest := newChannelMonitorRedisRouteHealthTestEvent("latest", 7, "model-a", now.Unix()-10, 3)
	middle := newChannelMonitorRedisRouteHealthTestEvent("middle", 7, "model-a", now.Unix()-20, 2)
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
	assert.Equal(t, now.Unix()-20, window.Snapshot.WindowStart)
	assert.Equal(t, now.Unix()-10, window.Snapshot.WindowEnd)
	rawState, err := projection.client.Get(context.Background(), ChannelMonitorRedisRouteHealthWindowKey(7, "model-a")).Result()
	require.NoError(t, err)
	assert.False(t, strings.Contains(rawState, "request-raw-field"))
	assert.False(t, strings.Contains(rawState, "raw\\\":\\\"field"))
}

func TestChannelMonitorRedisRouteHealthWindowEvictsTheOldestAfterOneThousandSamples(t *testing.T) {
	server, projection := useChannelMonitorRedisRouteHealthTestProjection(t)
	now := time.Unix(1_750_000_000, 0)
	server.SetTime(now)
	events := make([]model.ChannelMonitorEvent, 0, channelMonitorRedisRouteHealthSampleLimit+1)
	for index := 0; index <= channelMonitorRedisRouteHealthSampleLimit; index++ {
		events = append(events, newChannelMonitorRedisRouteHealthTestEvent(
			fmt.Sprintf("event-%04d", index), 7, "model-a", now.Unix()-int64(channelMonitorRedisRouteHealthSampleLimit-index), uint64(index+1),
		))
	}
	require.NoError(t, projection.HandleChannelMonitorEvents(context.Background(), events))
	window, available, err := projection.GetRouteHealthWindow(context.Background(), 7, "model-a")
	require.NoError(t, err)
	require.True(t, available)
	require.Len(t, window.Samples, channelMonitorRedisRouteHealthSampleLimit)
	assert.Equal(t, "event-0001", window.Samples[0].EventID)
	assert.Equal(t, "event-1000", window.Samples[len(window.Samples)-1].EventID)
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
		context.Background(),
		ChannelMonitorRedisRouteHealthIndexKey(),
		expiredRoutes...,
	).Err())
	windows, err := projection.ListRouteHealthWindows(context.Background())
	require.NoError(t, err)
	assert.Empty(t, windows)
	indexSize, err := projection.client.ZCard(context.Background(), ChannelMonitorRedisRouteHealthIndexKey()).Result()
	require.NoError(t, err)
	assert.Zero(t, indexSize)
	require.NoError(t, projection.client.ZAdd(
		context.Background(),
		ChannelMonitorRedisRouteHealthIndexKey(),
		expiredRoutes...,
	).Err())

	active := newChannelMonitorRedisRouteHealthTestEvent("active", expiredRouteCount+1, "model-a", now.Unix(), 1)
	require.NoError(t, projection.HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{active}))

	indexedRoutes, err := projection.client.ZRangeWithScores(
		context.Background(),
		ChannelMonitorRedisRouteHealthIndexKey(),
		0,
		-1,
	).Result()
	require.NoError(t, err)
	require.Len(t, indexedRoutes, 1)
	assert.Equal(t, ChannelMonitorRedisRouteHealthWindowKey(expiredRouteCount+1, "model-a"), indexedRoutes[0].Member)
	assert.Equal(t, float64(now.Add(channelMonitorRedisRouteHealthStateTTL).UnixMilli()), indexedRoutes[0].Score)
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

func TestChannelMonitorRedisRouteHealthProjectionReloadsConcurrentRefreshDuringExpiryCleanup(t *testing.T) {
	server, projection := useChannelMonitorRedisRouteHealthTestProjection(t)
	now := time.Unix(1_750_000_000, 0)
	server.SetTime(now)
	redisKey := ChannelMonitorRedisRouteHealthWindowKey(7, "model-a")
	expired := channelMonitorRedisRouteHealthState{
		Version:   channelMonitorRedisRouteHealthVersion,
		ChannelID: 7,
		ModelName: "model-a",
		Window: ChannelMonitorRedisRouteHealthWindow{Samples: []ChannelMonitorRedisRouteHealthSample{
			channelMonitorRedisRouteHealthSampleFromEvent(newChannelMonitorRedisRouteHealthTestEvent(
				"expired", 7, "model-a", now.Add(-time.Hour-time.Second).Unix(), 1,
			)),
		}},
	}
	refreshed := channelMonitorRedisRouteHealthState{
		Version:   channelMonitorRedisRouteHealthVersion,
		ChannelID: 7,
		ModelName: "model-a",
		Window: ChannelMonitorRedisRouteHealthWindow{Samples: []ChannelMonitorRedisRouteHealthSample{
			channelMonitorRedisRouteHealthSampleFromEvent(newChannelMonitorRedisRouteHealthTestEvent(
				"refreshed", 7, "model-a", now.Unix(), 2,
			)),
		}},
	}
	expiredPayload, err := common.Marshal(expired)
	require.NoError(t, err)
	refreshedPayload, err := common.Marshal(refreshed)
	require.NoError(t, err)
	require.NoError(t, projection.client.Set(context.Background(), redisKey, expiredPayload, time.Hour).Err())
	require.NoError(t, projection.client.ZAdd(
		context.Background(),
		ChannelMonitorRedisRouteHealthIndexKey(),
		&redis.Z{Score: float64(now.Add(time.Hour).UnixMilli()), Member: redisKey},
	).Err())
	writer := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = writer.Close() })
	hook := &channelMonitorRedisRouteHealthRefreshHook{
		key: redisKey, payload: refreshedPayload, writer: writer,
	}
	projection.client.AddHook(hook)

	window, available, err := projection.GetRouteHealthWindow(context.Background(), 7, "model-a")
	require.NoError(t, err)
	require.True(t, hook.triggered)
	require.True(t, available)
	require.Len(t, window.Samples, 1)
	assert.Equal(t, "refreshed", window.Samples[0].EventID)
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
