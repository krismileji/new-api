package controller

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisHealthEventsRequestExistingFullSchedule(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.ChannelMonitorRedisEffectState{}))
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t,
			channelSmartScheduleTestGroupPolicy(
				"vip",
				channelMonitorSmartScheduleStrategyRatio,
				false,
				channelMonitorSmartScheduleApplyWeight,
				[]string{"model-a"},
				2,
				80,
				30,
			),
		),
	})
	events := []model.ChannelMonitorEvent{
		{EventId: "redis-health-1", EventSequence: 101, SchedulingEligible: true},
		{EventId: "redis-health-2", EventSequence: 102, SchedulingEligible: true},
	}

	require.NoError(t, requestChannelSmartScheduleRunForRedisEvents(context.Background(), events))
	task, err := model.GetActiveSystemTask(channelMonitorSmartScheduleTaskType)
	require.NoError(t, err)
	require.NotNil(t, task)
	var payload channelSmartScheduleTaskPayload
	require.NoError(t, task.DecodePayload(&payload))
	assert.Equal(t, channelSmartScheduleRedisTriggerSource, payload.TriggerSource)
	assert.Equal(t, 2, payload.TriggerCount)
	assert.Equal(t, []string{channelSmartScheduleRedisDirtyReason}, payload.DirtyReasons)

	require.NoError(t, requestChannelSmartScheduleRunForRedisEvents(context.Background(), events))
	var taskCount int64
	require.NoError(t, model.DB.Model(&model.SystemTask{}).
		Where("type = ?", channelMonitorSmartScheduleTaskType).
		Count(&taskCount).Error)
	assert.Equal(t, int64(1), taskCount)
}

func TestRedisRuntimeEffectPositionUsesHighestSequenceEventTime(t *testing.T) {
	effectAt, sequence, err := channelSmartScheduleRedisRuntimeEffectPosition([]model.ChannelMonitorEvent{
		{EventId: "later-time", EventSequence: 10, OccurredAt: 200},
		{EventId: "higher-sequence", EventSequence: 11, OccurredAt: 150},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(150), effectAt)
	assert.Equal(t, int64(11), sequence)
}

func TestRedisRuntimeFailuresUseEventSequenceAsPrimaryOrder(t *testing.T) {
	rateLimitedStatus := http.StatusTooManyRequests
	failureStatus := http.StatusBadGateway
	rateLimited, failure := channelSmartScheduleRedisRuntimeFailures([]model.ChannelMonitorEvent{
		{
			EventId: "rate-limited-later-time", EventSequence: 10, OccurredAt: 300,
			Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeFailure,
			RuntimeProtectionEligible: true, RequestDispatched: true, StatusCode: &rateLimitedStatus,
		},
		{
			EventId: "rate-limited-higher-sequence", EventSequence: 11, OccurredAt: 100,
			Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeFailure,
			RuntimeProtectionEligible: true, RequestDispatched: true, StatusCode: &rateLimitedStatus,
		},
		{
			EventId: "failure-later-time", EventSequence: 20, OccurredAt: 300,
			Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeFailure,
			RuntimeProtectionEligible: true, RequestDispatched: true, StatusCode: &failureStatus,
		},
		{
			EventId: "failure-higher-sequence", EventSequence: 21, OccurredAt: 100,
			Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeFailure,
			RuntimeProtectionEligible: true, RequestDispatched: true, StatusCode: &failureStatus,
		},
	})
	require.NotNil(t, rateLimited)
	require.NotNil(t, failure)
	assert.Equal(t, uint64(11), rateLimited.EventSequence)
	assert.Equal(t, int64(100), rateLimited.OccurredAt)
	assert.Equal(t, uint64(21), failure.EventSequence)
	assert.Equal(t, int64(100), failure.OccurredAt)
}

func TestRedisAdaptiveRefreshPropagatesCooldownReadError(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(10)
	weight := uint(1000)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1701, Name: "redis cooldown read", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1701, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1701, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1, BaseRank: 1,
		BasePriority: priority, BaseWeight: weight,
	}).Error)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 2, 80, 30,
	).policy()
	readErr := errors.New("redis cooldown read failed")
	metricCalled := false
	conflict, err := refreshChannelSmartScheduleAdaptivePoolWithMetricReader(
		context.Background(),
		channelSmartScheduleRoutePoolKey{group: "vip", model: "model-a"},
		policy,
		"revision",
		model.DB,
		func(
			context.Context, int, string, int64, int64, int, float64, float64,
		) (model.ChannelSmartScheduleAdaptiveHealthMetric, error) {
			metricCalled = true
			return model.ChannelSmartScheduleAdaptiveHealthMetric{}, nil
		},
		func(context.Context, int, string) (int64, error) {
			return 0, readErr
		},
		common.GetTimestamp(),
		1,
	)
	assert.ErrorIs(t, err, readErr)
	assert.False(t, conflict)
	assert.False(t, metricCalled)
}

func TestRedisRuntimeWindowDoesNotReadFutureSequenceSamples(t *testing.T) {
	window := service.ChannelMonitorRedisRouteHealthWindow{
		Snapshot: service.ChannelMonitorRedisRouteHealthSnapshot{CoverageStart: 100},
		Samples: []service.ChannelMonitorRedisRouteHealthSample{
			{
				EventID: "first", EventSequence: 10, OccurredAt: 110,
				Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeSuccess,
				RequestDispatched: true, SchedulingEligible: true,
			},
			{
				EventID: "batch", EventSequence: 11, OccurredAt: 120,
				Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeFailure,
				RequestDispatched: true, SchedulingEligible: true,
			},
			{
				EventID: "future", EventSequence: 12, OccurredAt: 121,
				Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeSuccess,
				RequestDispatched: true, SchedulingEligible: true,
			},
		},
	}

	events := channelSmartScheduleRedisWindowEvents(window, 7, "model-a", 100, 0, 10, 11)
	require.Len(t, events, 2)
	assert.Equal(t, []uint64{10, 11}, []uint64{events[0].EventSequence, events[1].EventSequence})
}
