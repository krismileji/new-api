package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelMonitorRedisEvalScriptErrorHook struct {
	script    string
	keyPrefix string
	err       error
	calls     atomic.Int64
}

func (hook *channelMonitorRedisEvalScriptErrorHook) BeforeProcess(
	ctx context.Context,
	command redis.Cmder,
) (context.Context, error) {
	if hook.calls.Load() > 0 || command.Name() != "eval" {
		return ctx, nil
	}
	arguments := command.Args()
	if len(arguments) < 4 || fmt.Sprint(arguments[1]) != hook.script {
		return ctx, nil
	}
	keyCount, err := strconv.Atoi(fmt.Sprint(arguments[2]))
	if err != nil || keyCount <= 0 || len(arguments) < 3+keyCount {
		return ctx, nil
	}
	matched := hook.keyPrefix == ""
	for _, argument := range arguments[3 : 3+keyCount] {
		if strings.HasPrefix(fmt.Sprint(argument), hook.keyPrefix) {
			matched = true
			break
		}
	}
	if !matched {
		return ctx, nil
	}
	hook.calls.Add(1)
	return ctx, hook.err
}

func (hook *channelMonitorRedisEvalScriptErrorHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (hook *channelMonitorRedisEvalScriptErrorHook) BeforeProcessPipeline(
	ctx context.Context,
	_ []redis.Cmder,
) (context.Context, error) {
	return ctx, nil
}

func (hook *channelMonitorRedisEvalScriptErrorHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func channelMonitorRedisAggregatorTestEvent(eventID string, schedulingEligible bool) model.ChannelMonitorEvent {
	event := newChannelMonitorRedisConsumerTestEvent(eventID)
	event.ModelName = "gpt-test"
	event.GroupName = "vip"
	event.RequestDispatched = true
	event.IsFinalAttempt = true
	event.SchedulingEligible = schedulingEligible
	return event
}

func TestChannelMonitorRedisLogicalAggregatorDoesNotScheduleIneligibleEvents(t *testing.T) {
	server, client := useChannelMonitorRedisConsumerTestClient(t)
	event := channelMonitorRedisAggregatorTestEvent("event-ineligible", false)
	server.SetTime(time.Unix(event.OccurredAt, 0))
	var triggerCalls atomic.Int64
	var runtimeCalls atomic.Int64
	aggregator, err := NewChannelMonitorRedisLogicalAggregatorWithClient(
		client,
		func(context.Context, []model.ChannelMonitorEvent) error {
			runtimeCalls.Add(1)
			return nil
		},
		func(context.Context, []model.ChannelMonitorEvent) error {
			triggerCalls.Add(1)
			return nil
		},
	)
	require.NoError(t, err)

	require.NoError(t, aggregator.HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event}))
	assert.Zero(t, triggerCalls.Load())
	assert.Zero(t, runtimeCalls.Load())
	assert.Zero(t, client.Exists(context.Background(), ChannelMonitorRedisSchedulingDedupKey(event.EventId)).Val())

	shared := NewChannelMonitorRedisSharedProjectionWithClient(client)
	view, err := shared.Query(context.Background(), event.OccurredAt-60, event.OccurredAt+60)
	require.NoError(t, err)
	assert.Equal(t, int64(1), view.Summary.EventCount)
	route, err := NewChannelMonitorRedisRouteHealthProjectionForClient(client)
	require.NoError(t, err)
	_, available, err := route.GetRouteHealthWindow(context.Background(), event.ChannelId, event.ModelName)
	require.NoError(t, err)
	assert.False(t, available)
}

func TestChannelMonitorRedisLogicalAggregatorRetriesSchedulingWithoutRepeatingProjection(t *testing.T) {
	server, client := useChannelMonitorRedisConsumerTestClient(t)
	event := channelMonitorRedisAggregatorTestEvent("event-enqueue-retry", true)
	server.SetTime(time.Unix(event.OccurredAt, 0))
	addChannelMonitorRedisConsumerTestEvent(t, client, event)
	var triggerCalls atomic.Int64
	enqueueErr := errors.New("完整调度入队失败")
	aggregator, err := NewChannelMonitorRedisLogicalAggregatorWithClient(
		client,
		func(context.Context, []model.ChannelMonitorEvent) error { return nil },
		func(context.Context, []model.ChannelMonitorEvent) error {
			if triggerCalls.Add(1) == 1 {
				return enqueueErr
			}
			return nil
		},
	)
	require.NoError(t, err)
	config := channelMonitorRedisConsumerTestConfig()
	first := newChannelMonitorRedisConsumerForTest(t, client, "enqueue-fails", aggregator.HandleChannelMonitorEvents, config)

	_, acquired, err := first.consumeOnce(context.Background())
	assert.ErrorIs(t, err, enqueueErr)
	assert.True(t, acquired)
	assert.Equal(t, int64(1), triggerCalls.Load())
	assert.Zero(t, client.Exists(context.Background(), ChannelMonitorRedisSchedulingDedupKey(event.EventId)).Val())
	pending, err := client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), pending.Count)

	shared := NewChannelMonitorRedisSharedProjectionWithClient(client)
	view, err := shared.Query(context.Background(), event.OccurredAt-60, event.OccurredAt+60)
	require.NoError(t, err)
	assert.Equal(t, int64(1), view.Summary.EventCount)
	server.SetTime(time.Unix(event.OccurredAt, 0).Add(2 * time.Second))
	second := newChannelMonitorRedisConsumerForTest(t, client, "enqueue-retry", aggregator.HandleChannelMonitorEvents, config)
	processed, acquired, err := second.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 1, processed)
	assert.Equal(t, int64(2), triggerCalls.Load())

	view, err = shared.Query(context.Background(), event.OccurredAt-60, event.OccurredAt+60)
	require.NoError(t, err)
	assert.Equal(t, int64(1), view.Summary.EventCount)
	assert.Equal(t, int64(1), client.Exists(context.Background(), ChannelMonitorRedisSchedulingDedupKey(event.EventId)).Val())
	pending, err = client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, err)
	assert.Zero(t, pending.Count)
}

func TestChannelMonitorRedisLogicalAggregatorReplayAfterFinalizeFailureIsIdempotent(t *testing.T) {
	server, client := useChannelMonitorRedisConsumerTestClient(t)
	event := channelMonitorRedisAggregatorTestEvent("event-finalize-replay", true)
	baseTime := time.Unix(event.OccurredAt, 0)
	server.SetTime(baseTime)
	addChannelMonitorRedisConsumerTestEvent(t, client, event)
	var triggerCalls atomic.Int64
	var runtimeCalls atomic.Int64
	var projectionVisibleAtTrigger atomic.Bool
	route, err := NewChannelMonitorRedisRouteHealthProjectionForClient(client)
	require.NoError(t, err)
	shared := NewChannelMonitorRedisSharedProjectionWithClient(client)
	aggregator, err := NewChannelMonitorRedisLogicalAggregatorWithClient(
		client,
		func(context.Context, []model.ChannelMonitorEvent) error {
			runtimeCalls.Add(1)
			return nil
		},
		func(ctx context.Context, events []model.ChannelMonitorEvent) error {
			triggerCalls.Add(1)
			_, routeAvailable, routeErr := route.GetRouteHealthWindow(ctx, event.ChannelId, event.ModelName)
			view, sharedErr := shared.Query(ctx, event.OccurredAt-60, event.OccurredAt+60)
			if routeErr == nil && sharedErr == nil && routeAvailable && view.Summary.EventCount == 1 && len(events) == 1 {
				projectionVisibleAtTrigger.Store(true)
			}
			return nil
		},
	)
	require.NoError(t, err)
	config := channelMonitorRedisConsumerTestConfig()
	firstAttempt := true
	first := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"finalize-fails",
		func(ctx context.Context, events []model.ChannelMonitorEvent) error {
			if err := aggregator.HandleChannelMonitorEvents(ctx, events); err != nil {
				return err
			}
			if firstAttempt {
				firstAttempt = false
				return client.Del(ctx, ChannelMonitorRedisAggregatorLeaseKey).Err()
			}
			return nil
		},
		config,
	)

	_, acquired, err := first.consumeOnce(context.Background())
	assert.ErrorIs(t, err, ErrChannelMonitorRedisAggregatorLeaseLost)
	assert.True(t, acquired)
	assert.True(t, projectionVisibleAtTrigger.Load())
	assert.Equal(t, int64(1), triggerCalls.Load())
	assert.Equal(t, int64(1), runtimeCalls.Load())
	pending, err := client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), pending.Count)

	server.SetTime(baseTime.Add(2 * time.Second))
	second := newChannelMonitorRedisConsumerForTest(t, client, "finalize-replay", aggregator.HandleChannelMonitorEvents, config)
	processed, acquired, err := second.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 1, processed)
	assert.Equal(t, int64(1), triggerCalls.Load())
	assert.Equal(t, int64(1), runtimeCalls.Load())
	view, err := shared.Query(context.Background(), event.OccurredAt-60, event.OccurredAt+60)
	require.NoError(t, err)
	assert.Equal(t, int64(1), view.Summary.EventCount)
	window, available, err := route.GetRouteHealthWindow(context.Background(), event.ChannelId, event.ModelName)
	require.NoError(t, err)
	require.True(t, available)
	assert.Len(t, window.Samples, 1)
	pending, err = client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, err)
	assert.Zero(t, pending.Count)
}

func TestChannelMonitorRedisEffectMarkerOwnershipAndTakeover(t *testing.T) {
	assert.Less(t, channelMonitorRedisEffectProcessingTTL, channelMonitorRedisConsumerClaimMinIdle)
	server, client := useChannelMonitorRedisConsumerTestClient(t)
	aggregator, err := NewChannelMonitorRedisLogicalAggregatorWithClient(
		client,
		func(context.Context, []model.ChannelMonitorEvent) error { return nil },
		func(context.Context, []model.ChannelMonitorEvent) error { return nil },
	)
	require.NoError(t, err)
	markerKey := ChannelMonitorRedisRuntimeEffectKey("event-marker-ownership")
	require.NoError(t, client.Set(
		context.Background(), ChannelMonitorRedisAggregatorLeaseKey, "owner-a", time.Minute,
	).Err())
	claimed, err := aggregator.acquireEffectMarkers(context.Background(), "owner-a", []string{markerKey})
	require.NoError(t, err)
	assert.Equal(t, []bool{true}, claimed)

	require.NoError(t, client.Set(
		context.Background(), ChannelMonitorRedisAggregatorLeaseKey, "owner-b", time.Minute,
	).Err())
	_, err = aggregator.acquireEffectMarkers(context.Background(), "owner-b", []string{markerKey})
	assert.ErrorIs(t, err, ErrChannelMonitorRedisEffectProcessing)

	server.FastForward(channelMonitorRedisEffectProcessingTTL + time.Second)
	claimed, err = aggregator.acquireEffectMarkers(context.Background(), "owner-b", []string{markerKey})
	require.NoError(t, err)
	assert.Equal(t, []bool{true}, claimed)
	require.NoError(t, aggregator.completeEffectMarkers(
		context.Background(), "owner-b", []string{markerKey}, channelMonitorRedisRuntimeEffectTTL,
	))

	require.NoError(t, client.Set(
		context.Background(), ChannelMonitorRedisAggregatorLeaseKey, "owner-c", time.Minute,
	).Err())
	claimed, err = aggregator.acquireEffectMarkers(context.Background(), "owner-c", []string{markerKey})
	require.NoError(t, err)
	assert.Equal(t, []bool{false}, claimed)
	assert.Equal(t, channelMonitorRedisEffectDoneValue, client.Get(context.Background(), markerKey).Val())
}

func TestChannelMonitorRedisEffectMarkerCompletesAfterProcessingTTLWhileLeaseIsOwned(t *testing.T) {
	server, client := useChannelMonitorRedisConsumerTestClient(t)
	aggregator, err := NewChannelMonitorRedisLogicalAggregatorWithClient(
		client,
		func(context.Context, []model.ChannelMonitorEvent) error { return nil },
		func(context.Context, []model.ChannelMonitorEvent) error { return nil },
	)
	require.NoError(t, err)
	owner := "slow-owner"
	markerKey := ChannelMonitorRedisRuntimeEffectKey("slow-event")
	require.NoError(t, client.Set(
		context.Background(), ChannelMonitorRedisAggregatorLeaseKey, owner, time.Minute,
	).Err())
	claimed, err := aggregator.acquireEffectMarkers(context.Background(), owner, []string{markerKey})
	require.NoError(t, err)
	assert.Equal(t, []bool{true}, claimed)

	server.FastForward(channelMonitorRedisEffectProcessingTTL + time.Second)
	_, err = client.Get(context.Background(), markerKey).Result()
	assert.ErrorIs(t, err, redis.Nil)
	require.NoError(t, aggregator.completeEffectMarkers(
		context.Background(), owner, []string{markerKey}, channelMonitorRedisRuntimeEffectTTL,
	))
	assert.Equal(t, channelMonitorRedisEffectDoneValue, client.Get(context.Background(), markerKey).Val())
}

func TestChannelMonitorRedisEffectMarkerReplayProtectionTTLBoundary(t *testing.T) {
	server, client := useChannelMonitorRedisConsumerTestClient(t)
	event := channelMonitorRedisAggregatorTestEvent("event-marker-ttl", true)
	event.EventSequence = 77
	server.SetTime(time.Unix(event.OccurredAt, 0))
	aggregator, err := NewChannelMonitorRedisLogicalAggregatorWithClient(
		client,
		func(context.Context, []model.ChannelMonitorEvent) error { return nil },
		func(context.Context, []model.ChannelMonitorEvent) error { return nil },
	)
	require.NoError(t, err)
	owner := "marker-ttl-owner"
	require.NoError(t, client.Set(
		context.Background(),
		ChannelMonitorRedisAggregatorLeaseKey,
		owner,
		channelMonitorRedisReplayProtectionTTL+time.Hour,
	).Err())
	ctx := context.WithValue(
		context.Background(),
		channelMonitorRedisEffectOwnerContextKey{},
		owner,
	)
	var attempts atomic.Int64
	var effective atomic.Int64
	var watermark atomic.Int64
	apply := func(_ context.Context, events []model.ChannelMonitorEvent) error {
		attempts.Add(1)
		sequence := int64(events[0].EventSequence)
		for {
			current := watermark.Load()
			if sequence <= current {
				return nil
			}
			if watermark.CompareAndSwap(current, sequence) {
				effective.Add(1)
				return nil
			}
		}
	}
	applyOnce := func() error {
		return aggregator.applyEffect(
			ctx,
			[]model.ChannelMonitorEvent{event},
			ChannelMonitorRedisRuntimeEffectKey,
			channelMonitorRedisRuntimeEffectTTL,
			ChannelMonitorRedisObservabilityFieldRuntimeMarkerFailureCount,
			apply,
		)
	}

	require.NoError(t, applyOnce())
	assert.Equal(t, int64(1), attempts.Load())
	assert.Equal(t, int64(1), effective.Load())
	server.FastForward(channelMonitorRedisReplayProtectionTTL - time.Second)
	require.NoError(t, applyOnce())
	assert.Equal(t, int64(1), attempts.Load())
	assert.Equal(t, int64(1), effective.Load())

	server.FastForward(2 * time.Second)
	require.NoError(t, applyOnce())
	assert.Equal(t, int64(2), attempts.Load())
	assert.Equal(t, int64(1), effective.Load())
}

func TestChannelMonitorRedisEffectMarkerReleaseFailureIsObservableAndRecovers(t *testing.T) {
	server, client := useChannelMonitorRedisConsumerTestClient(t)
	event := channelMonitorRedisAggregatorTestEvent("event-marker-release-failure", true)
	server.SetTime(time.Unix(event.OccurredAt, 0))
	releaseErr := errors.New("marker release failed")
	hook := &channelMonitorRedisEvalScriptErrorHook{
		script:    channelMonitorRedisReleaseEffectMarkersScript,
		keyPrefix: ChannelMonitorRedisRuntimeEffectPrefix,
		err:       releaseErr,
	}
	client.AddHook(hook)
	aggregator, err := NewChannelMonitorRedisLogicalAggregatorWithClient(
		client,
		func(context.Context, []model.ChannelMonitorEvent) error { return nil },
		func(context.Context, []model.ChannelMonitorEvent) error { return nil },
	)
	require.NoError(t, err)
	owner := "marker-release-owner"
	require.NoError(t, client.Set(
		context.Background(), ChannelMonitorRedisAggregatorLeaseKey, owner, time.Minute,
	).Err())
	ctx := context.WithValue(
		context.Background(),
		channelMonitorRedisEffectOwnerContextKey{},
		owner,
	)
	applyErr := errors.New("runtime effect failed")
	err = aggregator.applyEffect(
		ctx,
		[]model.ChannelMonitorEvent{event},
		ChannelMonitorRedisRuntimeEffectKey,
		channelMonitorRedisRuntimeEffectTTL,
		ChannelMonitorRedisObservabilityFieldRuntimeMarkerFailureCount,
		func(context.Context, []model.ChannelMonitorEvent) error { return applyErr },
	)
	assert.ErrorIs(t, err, applyErr)
	assert.ErrorIs(t, err, releaseErr)
	assert.Equal(t, int64(1), hook.calls.Load())
	assert.Equal(t, "1", client.HGet(
		context.Background(),
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureCount,
	).Val())
	assert.Equal(t, "1", client.HGet(
		context.Background(),
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureActive,
	).Val())

	server.FastForward(channelMonitorRedisEffectProcessingTTL + time.Second)
	err = aggregator.applyEffect(
		ctx,
		[]model.ChannelMonitorEvent{event},
		ChannelMonitorRedisRuntimeEffectKey,
		channelMonitorRedisRuntimeEffectTTL,
		ChannelMonitorRedisObservabilityFieldRuntimeMarkerFailureCount,
		func(context.Context, []model.ChannelMonitorEvent) error { return applyErr },
	)
	assert.ErrorIs(t, err, applyErr)
	assert.Equal(t, "0", client.HGet(
		context.Background(),
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureActive,
	).Val())
	assert.Equal(t, "1", client.HGet(
		context.Background(),
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldMarkerReleaseFailureCount,
	).Val())
}

func TestChannelMonitorRedisLogicalAggregatorRuntimeMarkerAndFinalizeFailuresRemainIdempotent(t *testing.T) {
	server, client := useChannelMonitorRedisConsumerTestClient(t)
	event := channelMonitorRedisAggregatorTestEvent("event-runtime-marker-failure", true)
	server.SetTime(time.Unix(event.OccurredAt, 0))
	addChannelMonitorRedisConsumerTestEvent(t, client, event)
	markerErr := errors.New("runtime marker failed")
	hook := &channelMonitorRedisEvalScriptErrorHook{
		script:    channelMonitorRedisCompleteEffectMarkersScript,
		keyPrefix: ChannelMonitorRedisRuntimeEffectPrefix,
		err:       markerErr,
	}
	client.AddHook(hook)
	var runtimeAttempts atomic.Int64
	var runtimeEffective atomic.Int64
	var runtimeWatermark atomic.Int64
	var triggerCalls atomic.Int64
	aggregator, err := NewChannelMonitorRedisLogicalAggregatorWithClient(
		client,
		func(_ context.Context, events []model.ChannelMonitorEvent) error {
			runtimeAttempts.Add(1)
			sequence := int64(events[0].EventSequence)
			for {
				current := runtimeWatermark.Load()
				if sequence <= current {
					return nil
				}
				if runtimeWatermark.CompareAndSwap(current, sequence) {
					runtimeEffective.Add(1)
					return nil
				}
			}
		},
		func(context.Context, []model.ChannelMonitorEvent) error {
			triggerCalls.Add(1)
			return nil
		},
	)
	require.NoError(t, err)
	config := channelMonitorRedisConsumerTestConfig()
	first := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"runtime-marker-failure",
		aggregator.HandleChannelMonitorEvents,
		config,
	)

	processed, acquired, err := first.consumeOnce(context.Background())
	assert.ErrorIs(t, err, markerErr)
	assert.True(t, acquired)
	assert.Zero(t, processed)
	assert.Equal(t, int64(1), hook.calls.Load())
	assert.Equal(t, int64(1), runtimeAttempts.Load())
	assert.Equal(t, int64(1), runtimeEffective.Load())
	assert.Zero(t, triggerCalls.Load())
	assert.True(t, strings.HasPrefix(
		client.Get(context.Background(), ChannelMonitorRedisRuntimeEffectKey(event.EventId)).Val(),
		channelMonitorRedisEffectProcessingPrefix,
	))
	runtimeMarkerFailures, err := client.HGet(
		context.Background(),
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldRuntimeMarkerFailureCount,
	).Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(1), runtimeMarkerFailures)
	pending, pendingErr := client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, pendingErr)
	assert.Equal(t, int64(1), pending.Count)

	server.FastForward(channelMonitorRedisEffectProcessingTTL + time.Second)
	server.SetTime(time.Unix(event.OccurredAt, 0).Add(channelMonitorRedisEffectProcessingTTL + time.Second))
	second := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"runtime-marker-finalize-failure",
		func(ctx context.Context, events []model.ChannelMonitorEvent) error {
			if err := aggregator.HandleChannelMonitorEvents(ctx, events); err != nil {
				return err
			}
			return client.Del(ctx, ChannelMonitorRedisAggregatorLeaseKey).Err()
		},
		config,
	)
	processed, acquired, err = second.consumeOnce(context.Background())
	assert.ErrorIs(t, err, ErrChannelMonitorRedisAggregatorLeaseLost)
	assert.True(t, acquired)
	assert.Zero(t, processed)
	assert.Equal(t, int64(2), runtimeAttempts.Load())
	assert.Equal(t, int64(1), runtimeEffective.Load())
	assert.Equal(t, int64(1), triggerCalls.Load())
	pending, pendingErr = client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, pendingErr)
	assert.Equal(t, int64(1), pending.Count)

	server.SetTime(time.Unix(event.OccurredAt, 0).Add(
		channelMonitorRedisEffectProcessingTTL + config.ClaimMinIdle + 2*time.Second,
	))
	third := newChannelMonitorRedisConsumerForTest(t, client, "runtime-marker-finalize-replay", aggregator.HandleChannelMonitorEvents, config)
	processed, acquired, err = third.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 1, processed)
	assert.Equal(t, int64(2), runtimeAttempts.Load())
	assert.Equal(t, int64(1), runtimeEffective.Load())
	assert.Equal(t, int64(1), triggerCalls.Load())
}

func TestChannelMonitorRedisLogicalAggregatorScheduleMarkerAndFinalizeFailuresRemainIdempotent(t *testing.T) {
	server, client := useChannelMonitorRedisConsumerTestClient(t)
	event := channelMonitorRedisAggregatorTestEvent("event-schedule-marker-failure", true)
	server.SetTime(time.Unix(event.OccurredAt, 0))
	addChannelMonitorRedisConsumerTestEvent(t, client, event)
	markerErr := errors.New("schedule marker failed")
	hook := &channelMonitorRedisEvalScriptErrorHook{
		script:    channelMonitorRedisCompleteEffectMarkersScript,
		keyPrefix: ChannelMonitorRedisSchedulingDedupPrefix,
		err:       markerErr,
	}
	client.AddHook(hook)
	var runtimeCalls atomic.Int64
	var triggerAttempts atomic.Int64
	var triggerEffective atomic.Int64
	var triggerWatermark atomic.Int64
	aggregator, err := NewChannelMonitorRedisLogicalAggregatorWithClient(
		client,
		func(context.Context, []model.ChannelMonitorEvent) error {
			runtimeCalls.Add(1)
			return nil
		},
		func(_ context.Context, events []model.ChannelMonitorEvent) error {
			triggerAttempts.Add(1)
			sequence := int64(events[0].EventSequence)
			for {
				current := triggerWatermark.Load()
				if sequence <= current {
					return nil
				}
				if triggerWatermark.CompareAndSwap(current, sequence) {
					triggerEffective.Add(1)
					return nil
				}
			}
		},
	)
	require.NoError(t, err)
	config := channelMonitorRedisConsumerTestConfig()
	first := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"schedule-marker-failure",
		aggregator.HandleChannelMonitorEvents,
		config,
	)

	processed, acquired, err := first.consumeOnce(context.Background())
	assert.ErrorIs(t, err, markerErr)
	assert.True(t, acquired)
	assert.Zero(t, processed)
	assert.Equal(t, int64(1), hook.calls.Load())
	assert.Equal(t, int64(1), runtimeCalls.Load())
	assert.Equal(t, int64(1), triggerAttempts.Load())
	assert.Equal(t, int64(1), triggerEffective.Load())
	assert.True(t, strings.HasPrefix(
		client.Get(context.Background(), ChannelMonitorRedisSchedulingDedupKey(event.EventId)).Val(),
		channelMonitorRedisEffectProcessingPrefix,
	))
	scheduleMarkerFailures, err := client.HGet(
		context.Background(),
		ChannelMonitorRedisObservabilityKey,
		ChannelMonitorRedisObservabilityFieldScheduleMarkerFailureCount,
	).Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(1), scheduleMarkerFailures)
	pending, pendingErr := client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, pendingErr)
	assert.Equal(t, int64(1), pending.Count)

	server.FastForward(channelMonitorRedisEffectProcessingTTL + time.Second)
	server.SetTime(time.Unix(event.OccurredAt, 0).Add(channelMonitorRedisEffectProcessingTTL + time.Second))
	second := newChannelMonitorRedisConsumerForTest(
		t,
		client,
		"schedule-marker-finalize-failure",
		func(ctx context.Context, events []model.ChannelMonitorEvent) error {
			if err := aggregator.HandleChannelMonitorEvents(ctx, events); err != nil {
				return err
			}
			return client.Del(ctx, ChannelMonitorRedisAggregatorLeaseKey).Err()
		},
		config,
	)
	processed, acquired, err = second.consumeOnce(context.Background())
	assert.ErrorIs(t, err, ErrChannelMonitorRedisAggregatorLeaseLost)
	assert.True(t, acquired)
	assert.Zero(t, processed)
	assert.Equal(t, int64(1), runtimeCalls.Load())
	assert.Equal(t, int64(2), triggerAttempts.Load())
	assert.Equal(t, int64(1), triggerEffective.Load())
	pending, pendingErr = client.XPending(context.Background(), ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup).Result()
	require.NoError(t, pendingErr)
	assert.Equal(t, int64(1), pending.Count)

	server.SetTime(time.Unix(event.OccurredAt, 0).Add(
		channelMonitorRedisEffectProcessingTTL + config.ClaimMinIdle + 2*time.Second,
	))
	third := newChannelMonitorRedisConsumerForTest(t, client, "schedule-marker-finalize-replay", aggregator.HandleChannelMonitorEvents, config)
	processed, acquired, err = third.consumeOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 1, processed)
	assert.Equal(t, int64(1), runtimeCalls.Load())
	assert.Equal(t, int64(2), triggerAttempts.Load())
	assert.Equal(t, int64(1), triggerEffective.Load())
}
