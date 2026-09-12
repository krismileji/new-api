package service

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func monitoringRecoveryFixture(now int64) channelMonitorRecoveryInput {
	return channelMonitorRecoveryInput{Now: now, NodeID: "node-a", ObservationComplete: true, WriterRunning: true, CostWorkerRunning: true,
		Realtime: ChannelMonitorRedisRealtimeStatus{RedisAvailable: true, RedisConsumerRunning: true}}
}

func TestChannelMonitorRecoveryWaitsForStableObservations(t *testing.T) {
	input := monitoringRecoveryFixture(100)
	input.Realtime.RedisAvailable = false
	state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
	require.Equal(t, ChannelMonitorHealthUnavailable, state.Snapshot.Status)
	input = monitoringRecoveryFixture(110)
	state = deriveChannelMonitorRecovery(input, state)
	require.Equal(t, "recovering", state.Snapshot.RecoveryStatus)
	for _, now := range []int64{120, 130, 140, 150, 160} {
		input.Now = now
		state = deriveChannelMonitorRecovery(input, state)
		assert.Equal(t, ChannelMonitorHealthDegraded, state.Snapshot.Status)
	}
	input.Now = 170
	input.Realtime.CostOutboxPendingCount = 4
	state = deriveChannelMonitorRecovery(input, state)
	assert.Equal(t, ChannelMonitorHealthHealthy, state.Snapshot.Status)
	assert.True(t, state.Snapshot.RecoveryConfirmed, "new events in the normal minute batch do not block recovery")
	assert.Equal(t, int64(170), state.Snapshot.RecoveredAt)
	assert.Equal(t, "recovered", state.Snapshot.RecoveryStatus)
}

func TestChannelMonitorRecoveryMissingObservationsCannotConfirmRecovery(t *testing.T) {
	input := monitoringRecoveryFixture(100)
	input.Realtime.RedisAvailable = false
	state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
	input = monitoringRecoveryFixture(110)
	state = deriveChannelMonitorRecovery(input, state)
	input.Now = 1000
	state = deriveChannelMonitorRecovery(input, state)
	assert.Equal(t, "recovering", state.Snapshot.RecoveryStatus)
	assert.Zero(t, state.Snapshot.RecoveredAt)
	input.Now = 1010
	input.ObservationComplete = false
	state = deriveChannelMonitorRecovery(input, state)
	assert.Equal(t, "manual_required", state.Snapshot.RecoveryStatus)
	assert.Contains(t, state.Snapshot.DegradedReasons, "health_observation_failed")
}

func TestChannelMonitorRecoveryKeepsHistoricalGapsAfterRuntimeRecovers(t *testing.T) {
	input := monitoringRecoveryFixture(100)
	input.Realtime.WriterDroppedEvents = 2
	input.Realtime.CostPublishFailedCount = 1
	input.Realtime.QuarantineCount = 1
	state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
	require.NotEqual(t, ChannelMonitorHealthHealthy, state.Snapshot.Status)
	for _, now := range []int64{110, 120, 130, 140, 150, 160, 170} {
		input.Now = now
		state = deriveChannelMonitorRecovery(input, state)
	}
	assert.Equal(t, ChannelMonitorHealthHealthy, state.Snapshot.Status)
	assert.Equal(t, "data_incomplete", state.Snapshot.RecoveryStatus)
	assert.Contains(t, state.Snapshot.DataGapReasons, "samples_dropped")
	assert.Contains(t, state.Snapshot.DataGapReasons, "events_quarantined")
	assert.Contains(t, state.Snapshot.DataGapReasons, ChannelMonitorRedisDegradedReasonCostPublishFailure)
	input.Now = 180
	input.Realtime.WriterDroppedEvents = 3
	state = deriveChannelMonitorRecovery(input, state)
	assert.Equal(t, ChannelMonitorHealthDegraded, state.Snapshot.Status, "new losses start a new incident")
}

func TestChannelMonitorRecoveryDistinguishesNormalBatchesFromStalledBacklog(t *testing.T) {
	input := monitoringRecoveryFixture(100)
	input.Realtime.CostOutboxPendingCount = 10
	state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
	assert.Equal(t, ChannelMonitorHealthHealthy, state.Snapshot.Status)
	input.Now = 110
	input.Realtime.PendingCount = 3
	state = deriveChannelMonitorRecovery(input, state)
	assert.Equal(t, ChannelMonitorHealthHealthy, state.Snapshot.Status)
	input.Now = 140
	state = deriveChannelMonitorRecovery(input, state)
	assert.Equal(t, ChannelMonitorHealthDegraded, state.Snapshot.Status)
	assert.NotEqual(t, "recovering", state.Snapshot.RecoveryStatus, "pending count alone is not progress")
	input.Now = 150
	input.Realtime.PendingCount = 2
	input.Realtime.ConsumerLagSeconds = 40
	state = deriveChannelMonitorRecovery(input, state)
	assert.Equal(t, "recovering", state.Snapshot.RecoveryStatus)
	input.Now = 500
	state = deriveChannelMonitorRecovery(input, state)
	assert.Equal(t, "manual_required", state.Snapshot.RecoveryStatus)
}

func TestChannelMonitorRecoveryNoticesRespectDeliveryAndRecovery(t *testing.T) {
	snapshot := ChannelMonitorRecovery{ChannelMonitorMonitoringHealth: ChannelMonitorMonitoringHealth{Status: ChannelMonitorHealthDegraded}, CheckedAt: 100}
	var state channelMonitorRecoveryNotice
	require.Equal(t, "alert", channelMonitorRecoveryNoticeKind(snapshot, state, 100))
	state = finishChannelMonitorRecoveryNotice(state, snapshot, "alert", 100, errors.New("SMTP unavailable"))
	assert.False(t, state.Alerted)
	assert.Zero(t, state.LastSentAt)
	assert.Empty(t, channelMonitorRecoveryNoticeKind(snapshot, state, 110))
	snapshot.CheckedAt = 130
	assert.Equal(t, "alert", channelMonitorRecoveryNoticeKind(snapshot, state, 130))
	state = finishChannelMonitorRecoveryNotice(state, snapshot, "alert", 130, nil)
	snapshot.CheckedAt = 200
	snapshot.DegradedReasons = []string{"cost_outbox_backlog", "redis_context_deadline"}
	assert.Empty(t, channelMonitorRecoveryNoticeKind(snapshot, state, 200), "changing reason order or combination does not bypass cooldown")
	snapshot.Status = ChannelMonitorHealthHealthy
	snapshot.RecoveryConfirmed = true
	assert.Equal(t, "recovery", channelMonitorRecoveryNoticeKind(snapshot, state, 200))
	state = finishChannelMonitorRecoveryNotice(state, snapshot, "recovery", 200, nil)
	assert.Empty(t, channelMonitorRecoveryNoticeKind(snapshot, state, 200))
	snapshot.Status = ChannelMonitorHealthUnavailable
	assert.Equal(t, "alert", channelMonitorRecoveryNoticeKind(snapshot, state, 200), "new incidents do not inherit the recovered incident cooldown")
}

func TestChannelMonitorRecoveryNoticeHistoricalGapsAreNotRepeated(t *testing.T) {
	snapshot := ChannelMonitorRecovery{ChannelMonitorMonitoringHealth: ChannelMonitorMonitoringHealth{Status: ChannelMonitorHealthHealthy}, CheckedAt: 100, DataGapReasons: []string{"samples_dropped"}}
	var state channelMonitorRecoveryNotice
	require.Equal(t, "gap", channelMonitorRecoveryNoticeKind(snapshot, state, 100))
	state = finishChannelMonitorRecoveryNotice(state, snapshot, "gap", 100, nil)
	assert.Empty(t, channelMonitorRecoveryNoticeKind(snapshot, state, 100))
	snapshot.CheckedAt = 10000
	assert.Empty(t, channelMonitorRecoveryNoticeKind(snapshot, state, 10000))
	state.Alerted = true
	assert.Empty(t, channelMonitorRecoveryNoticeKind(snapshot, state, 10100), "stale state cannot send recovery")
}

func TestChannelMonitorRecoveryCostBacklogIsIndependentOfMonitorProgress(t *testing.T) {
	input := monitoringRecoveryFixture(100)
	input.CostProjectionPending = true
	state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
	assert.Equal(t, ChannelMonitorHealthHealthy, state.Snapshot.Status)
	input.Now = 130
	input.Realtime.LastProcessedAt = 130
	input.CostStreamOldest = 90
	state = deriveChannelMonitorRecovery(input, state)
	assert.Equal(t, ChannelMonitorHealthDegraded, state.Snapshot.Status)
	assert.Contains(t, state.Snapshot.DegradedReasons, "cost_projection_pending")
	assert.Contains(t, state.Snapshot.DegradedReasons, ChannelMonitorRedisDegradedReasonCostStreamBacklog)
}

func TestChannelMonitorRecoveryHistoricalGapsDoNotEscalateNewBacklog(t *testing.T) {
	for _, reason := range []string{"events_quarantined", ChannelMonitorRedisDegradedReasonCostDeadLetter} {
		t.Run(reason, func(t *testing.T) {
			input := monitoringRecoveryFixture(100)
			if reason == "events_quarantined" {
				input.Realtime.QuarantineCount = 10
			} else {
				input.Realtime.CostDeadLetterCount = 10
			}
			state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
			require.Equal(t, "data_incomplete", state.Snapshot.RecoveryStatus)

			input.Now = 110
			input.Realtime.PendingCount = 2
			input.Realtime.ConsumerLagSeconds = 40
			state = deriveChannelMonitorRecovery(input, state)
			require.Equal(t, ChannelMonitorHealthDegraded, state.Snapshot.Status)
			assert.Equal(t, "retrying", state.Snapshot.RecoveryStatus)
			assert.Contains(t, state.Snapshot.DataGapReasons, reason)

			input.Now = 120
			input.Realtime.PendingCount = 1
			state = deriveChannelMonitorRecovery(input, state)
			assert.Equal(t, "recovering", state.Snapshot.RecoveryStatus)
			assert.Contains(t, state.Snapshot.DataGapReasons, reason)

			input.Now = 410
			state = deriveChannelMonitorRecovery(input, state)
			assert.Equal(t, "manual_required", state.Snapshot.RecoveryStatus, "persistent current backlog still requires attention")
		})
	}
}

func TestBuildChannelMonitorRecoveryEmailDoesNotPromiseHistoricalRepair(t *testing.T) {
	snapshot := ChannelMonitorRecovery{NodeID: "<node>", DataGapReasons: []string{"samples_dropped"}}
	subject, body := BuildChannelMonitorRecoveryEmail(snapshot, "recovery", time.Unix(100, 0))
	assert.Equal(t, "渠道监控运行已恢复", subject)
	assert.Contains(t, body, "部分历史统计仍不完整")
	assert.NotContains(t, body, "全部补齐")
	assert.Contains(t, body, "&lt;node&gt;")
	assert.NotContains(t, body, "<node>")
}
