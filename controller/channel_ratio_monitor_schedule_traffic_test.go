package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
)

func TestSmartScheduleActualTrafficCountsBusinessAttemptsWithoutScoringFilters(t *testing.T) {
	route := model.ChannelSmartScheduleRoute{ChannelId: 1, Group: "vip", Model: "model-a"}
	base := service.ChannelMonitorRedisRouteHealthSample{EventID: "first", RequestFingerprint: "r1", GroupName: "vip", OccurredAt: 120,
		Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeFailure, RequestDispatched: true,
		SchedulingEligible: false, Routing: &model.ChannelRoutingDecision{Source: "smart_schedule", Model: "model-a"}}
	retry := base
	retry.EventID, retry.OccurredAt, retry.EventSequence = "retry", 140, 3
	retry.IsRetryAttempt, retry.IsFinalAttempt, retry.Outcome = true, true, model.ChannelMonitorEventOutcomeSuccess
	retry.Routing = &model.ChannelRoutingDecision{Source: "concurrency_fallback", Model: "model-a", LogicalChannelID: 20, RateLimitFallback: true}
	probe, otherGroup, summary, notDispatched, old := base, base, base, base, base
	probe.EventID, probe.Source = "probe", model.ChannelMonitorEventSourceSmartProbe
	otherGroup.EventID, otherGroup.GroupName = "other-group", "other"
	summary.EventID, summary.FinalRetrySummary = "summary", true
	notDispatched.EventID, notDispatched.RequestDispatched = "local-error", false
	old.EventID, old.OccurredAt = "old", 99
	window := service.ChannelMonitorRedisRouteHealthWindow{
		Snapshot: service.ChannelMonitorRedisRouteHealthSnapshot{CoverageStart: 80},
		Samples:  []service.ChannelMonitorRedisRouteHealthSample{base, retry, retry, probe, otherGroup, summary, notDispatched, old},
	}
	got := channelSmartScheduleActualTraffic(route, window, 100, 200, 90)
	assert.True(t, got.Complete)
	assert.EqualValues(t, 2, got.AttemptCount)
	assert.EqualValues(t, 1, got.FinalSuccessCount)
	assert.EqualValues(t, 1, got.RetryRequestCount)
	assert.Equal(t, map[string]int64{"smart_schedule": 1, "concurrency_fallback": 1}, got.SourceCounts)
	assert.EqualValues(t, 1, got.LogicalMemberCount)
	assert.EqualValues(t, 1, got.RateLimitFallbackCount)
	assert.EqualValues(t, 3, got.EventWatermark)
}

func TestSmartScheduleActualTrafficPreservesMissingAndTruncatedCoverage(t *testing.T) {
	route := model.ChannelSmartScheduleRoute{ChannelId: 1, Group: "vip", Model: "model-a"}
	empty := channelSmartScheduleActualTraffic(route, service.ChannelMonitorRedisRouteHealthWindow{}, 100, 200, 0)
	assert.False(t, empty.Available)
	assert.False(t, empty.Complete)
	window := service.ChannelMonitorRedisRouteHealthWindow{Snapshot: service.ChannelMonitorRedisRouteHealthSnapshot{
		CoverageStart: 80, SampleLimitTruncated: true,
	}}
	got := channelSmartScheduleActualTraffic(route, window, 100, 200, 90)
	assert.True(t, got.Available)
	assert.False(t, got.Complete)
}

func TestSmartScheduleActualTrafficSeparatesExactAndNormalizedModelRoutes(t *testing.T) {
	exact := "gemini-2.5-pro-thinking-2048"
	window := service.ChannelMonitorRedisRouteHealthWindow{
		Snapshot: service.ChannelMonitorRedisRouteHealthSnapshot{CoverageStart: 80},
		Samples: []service.ChannelMonitorRedisRouteHealthSample{{
			EventID: "exact", RequestFingerprint: "request", RequestModel: exact, GroupName: "vip", OccurredAt: 120,
			Source: model.ChannelMonitorEventSourceBusiness, Outcome: model.ChannelMonitorEventOutcomeSuccess,
			RequestDispatched: true, IsFinalAttempt: true,
			Routing: &model.ChannelRoutingDecision{Source: "smart_schedule", Model: exact},
		}},
	}
	got := channelSmartScheduleActualTraffic(model.ChannelSmartScheduleRoute{ChannelId: 1, Group: "vip", Model: exact}, window, 100, 200, 90)
	assert.EqualValues(t, 1, got.FinalSuccessCount)
	normalized := channelSmartScheduleActualTraffic(model.ChannelSmartScheduleRoute{ChannelId: 1, Group: "vip", Model: "gemini-2.5-pro-thinking-*"}, window, 100, 200, 90)
	assert.Zero(t, normalized.AttemptCount)
}
