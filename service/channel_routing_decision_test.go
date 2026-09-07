package service

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelRoutingAttemptsKeepIndependentProvenance(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyChannelId, 1)
	observeChannelRouting(c, false)(model.ChannelRoutingDecision{Source: "affinity", ChannelID: 1, CandidateChannelID: 1, SnapshotRevision: 7})
	BeginChannelMonitorPerformanceAttempt(c, time.Now())
	first := channelRoutingDecisionForAttempt(c, 1)
	require.NotNil(t, first)
	assert.Equal(t, "affinity", first.Source)
	assert.Equal(t, 1, first.AttemptNumber)
	common.SetContextKey(c, constant.ContextKeyChannelId, 2)
	observeChannelRouting(c, true)(model.ChannelRoutingDecision{Source: "smart_schedule", ChannelID: 2, CandidateChannelID: 2, SnapshotRevision: 8, RateLimitFallback: true})
	MarkChannelRoutingConcurrencyFallback(c)
	BeginChannelMonitorPerformanceAttempt(c, time.Now())
	second := channelRoutingDecisionForAttempt(c, 2)
	require.NotNil(t, second)
	assert.Equal(t, "concurrency_fallback", second.Source)
	assert.True(t, second.Retry)
	assert.True(t, second.RateLimitFallback)
	assert.Equal(t, 2, second.AttemptNumber)
	assert.Equal(t, "affinity", first.Source)
	adminInfo := make(map[string]interface{})
	appendChannelRoutingAdminInfo(c, adminInfo)
	attempts, ok := adminInfo["channel_routing"].([]model.ChannelRoutingDecision)
	require.True(t, ok)
	require.Len(t, attempts, 2)
	assert.EqualValues(t, 7, attempts[0].SnapshotRevision)
	assert.EqualValues(t, 8, attempts[1].SnapshotRevision)
	GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: 2})
	assert.Equal(t, "specific_channel", channelRoutingDecisionForAttempt(c, 2).Source)
}

func TestChannelMonitorTrafficRetainsUnscoredBusinessWithoutChangingHealth(t *testing.T) {
	_, projection := useChannelMonitorRedisRouteHealthTestProjection(t)
	event := newChannelMonitorRedisRouteHealthTestEvent("bypass", 7, "model-a", time.Now().Unix(), 1)
	event.RequestId, event.GroupName, event.SchedulingEligible = "request-1", "vip", false
	event.Routing = &model.ChannelRoutingDecision{Source: "retry", ChannelID: 7, AttemptNumber: 2}
	require.NoError(t, projection.HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event}))
	batch, err := projection.GetRouteHealthWindows(context.Background(), []ChannelMonitorRedisRouteHealthRouteKey{{ChannelID: 7, ModelName: "model-a"}})
	require.NoError(t, err)
	window := batch.Windows[ChannelMonitorRedisRouteHealthRouteKey{ChannelID: 7, ModelName: "model-a"}]
	require.Len(t, window.Samples, 1)
	assert.Len(t, window.Samples[0].RequestFingerprint, 64)
	assert.NotEqual(t, "request-1", window.Samples[0].RequestFingerprint)
	assert.Equal(t, "retry", window.Samples[0].Routing.Source)
	assert.Zero(t, window.Snapshot.BusinessRequestCount)
	assert.Positive(t, batch.TrafficCoverageStart)
}

func TestChannelMonitorTrafficRedisCompatibility(t *testing.T) {
	address := os.Getenv("SCHEDULE_FIX_REDIS_ADDR")
	if address == "" {
		t.Skip("SCHEDULE_FIX_REDIS_ADDR is not set; use a disposable Redis instance")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	projection, err := NewChannelMonitorRedisRouteHealthProjectionForClient(client)
	require.NoError(t, err)
	event := model.NewChannelMonitorEvent(9481, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, time.Now().Unix())
	event.EventSequence, event.ModelName, event.GroupName, event.RequestId = 1, "routing-"+event.EventId, "vip", "request-1"
	event.RequestDispatched, event.IsFinalAttempt = true, true
	event.Routing = &model.ChannelRoutingDecision{Source: "affinity", ChannelID: 9481, CandidateChannelID: 9481, SnapshotRevision: 12, Model: event.ModelName}
	require.NoError(t, projection.HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event, event}))
	batch, err := projection.GetRouteHealthWindows(context.Background(), []ChannelMonitorRedisRouteHealthRouteKey{{ChannelID: 9481, ModelName: event.ModelName}})
	require.NoError(t, err)
	window := batch.Windows[ChannelMonitorRedisRouteHealthRouteKey{ChannelID: 9481, ModelName: event.ModelName}]
	require.Len(t, window.Samples, 1)
	assert.Positive(t, batch.TrafficCoverageStart)
	assert.Len(t, window.Samples[0].RequestFingerprint, 64)
	require.NotNil(t, window.Samples[0].Routing)
	assert.EqualValues(t, 12, window.Samples[0].Routing.SnapshotRevision)
	assert.EqualValues(t, 1, window.Snapshot.FinalSuccessCount)
}
