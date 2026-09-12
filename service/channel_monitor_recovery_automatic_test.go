package service

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorRecoveryQuarantineDoesNotClaimLostStatistics(t *testing.T) {
	input := monitoringRecoveryFixture(100)
	input.Realtime.QuarantineCount = 2604
	state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
	assert.Equal(t, ChannelMonitorHealthHealthy, state.Snapshot.Status)
	assert.Equal(t, []string{"events_quarantined"}, state.Snapshot.DataGapReasons)
	assert.Contains(t, state.Snapshot.Message, "历史隔离记录")
	assert.NotContains(t, state.Snapshot.Message, "统计不完整")
	_, body := BuildChannelMonitorRecoveryEmail(state.Snapshot, "recovery", time.Unix(100, 0))
	assert.Contains(t, body, "隔离条数不代表请求失败数或统计丢失数")
	assert.NotContains(t, body, "统计仍不完整")
}

func TestChannelMonitorRecoveryNotifiesNewQuarantineAfterHistoricalNotice(t *testing.T) {
	input := monitoringRecoveryFixture(100)
	input.Realtime.QuarantineCount = 2604
	state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
	require.Equal(t, ChannelMonitorHealthHealthy, state.Snapshot.Status)
	notice := finishChannelMonitorRecoveryNotice(channelMonitorRecoveryNotice{}, state.Snapshot, "gap", 100, nil)
	input.Now = 110
	input.Realtime.QuarantineCount++
	state = deriveChannelMonitorRecovery(input, state)
	assert.Equal(t, "manual_required", state.Snapshot.RecoveryStatus)
	assert.Equal(t, "alert", channelMonitorRecoveryNoticeKind(state.Snapshot, notice, 110))
}

func TestChannelMonitorRecoveryRetriesNewQuarantineNoticeAfterRuntimeRecovers(t *testing.T) {
	input := monitoringRecoveryFixture(100)
	input.Realtime.QuarantineCount = 2604
	state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
	notice := finishChannelMonitorRecoveryNotice(channelMonitorRecoveryNotice{}, state.Snapshot, "gap", 100, nil)
	input.Now = 110
	input.Realtime.QuarantineCount++
	state = deriveChannelMonitorRecovery(input, state)
	notice = finishChannelMonitorRecoveryNotice(notice, state.Snapshot, "alert", 110, assert.AnError)
	for _, now := range []int64{120, 130, 140, 150, 160, 170, 180} {
		input.Now = now
		state = deriveChannelMonitorRecovery(input, state)
	}
	require.Equal(t, ChannelMonitorHealthHealthy, state.Snapshot.Status)
	assert.Equal(t, "gap", channelMonitorRecoveryNoticeKind(state.Snapshot, notice, 180), "unsent new isolation must remain reportable after runtime recovery")
	notice = finishChannelMonitorRecoveryNotice(notice, state.Snapshot, "gap", 180, nil)
	assert.Empty(t, channelMonitorRecoveryNoticeKind(state.Snapshot, notice, 180))
}

func TestChannelMonitorRecoveryHistoryNoticeSurvivesLocalCounterReset(t *testing.T) {
	snapshot := ChannelMonitorRecovery{
		ChannelMonitorMonitoringHealth: ChannelMonitorMonitoringHealth{Status: ChannelMonitorHealthHealthy, DroppedSampleCount: 80},
		CheckedAt:                      100, CostPublishFailedCount: 10,
		DataGapReasons: []string{"samples_dropped", "cost_publish_failure"},
	}
	notice := finishChannelMonitorRecoveryNotice(channelMonitorRecoveryNotice{}, snapshot, "gap", 100, nil)
	snapshot.CheckedAt, snapshot.DroppedSampleCount, snapshot.CostPublishFailedCount = 200, 0, 0
	assert.Empty(t, channelMonitorRecoveryNoticeKind(snapshot, notice, 200), "restarting local counters does not create a new historical incident")
}

func TestChannelMonitorRecoveryLegacyHistoryNoticeContinuesAfterUpgrade(t *testing.T) {
	client := setupMonitorRecoveryRedis(t)
	channelMonitorRecoveryNotices.Lock()
	previousLocal := channelMonitorRecoveryNotices.local
	channelMonitorRecoveryNotices.local = make(map[string]channelMonitorRecoveryNotice)
	channelMonitorRecoveryNotices.Unlock()
	t.Cleanup(func() {
		channelMonitorRecoveryNotices.Lock()
		channelMonitorRecoveryNotices.local = previousLocal
		channelMonitorRecoveryNotices.Unlock()
	})
	previousSend := sendChannelMonitorRecoveryEmail
	t.Cleanup(func() { sendChannelMonitorRecoveryEmail = previousSend })
	calls := 0
	sendChannelMonitorRecoveryEmail = func(subject, receiver, content string) error { calls++; return nil }
	now := time.Now().Unix()
	snapshot := ChannelMonitorRecovery{
		ChannelMonitorMonitoringHealth: ChannelMonitorMonitoringHealth{Status: ChannelMonitorHealthHealthy},
		NodeID:                         "upgrade-test-" + common.GetUUID(), CheckedAt: now,
		QuarantineCount: 2604, DataGapReasons: []string{"events_quarantined"},
	}
	receiver := "monitor@example.test"
	key := fmt.Sprintf("channel_monitor:v1:health:notice:%x", sha256.Sum256([]byte(snapshot.NodeID+"\x00"+receiver)))
	payload, err := common.Marshal(map[string]interface{}{"LastSentAt": now - 100, "GapFingerprint": "events_quarantined"})
	require.NoError(t, err)
	require.NoError(t, client.Set(t.Context(), key, payload, time.Minute).Err())
	deliverChannelMonitorRecoveryNotice(snapshot, receiver)
	assert.Zero(t, calls, "upgrading the notice format must not resend unchanged history")
	channelMonitorRecoveryNotices.Lock()
	channelMonitorRecoveryNotices.local = make(map[string]channelMonitorRecoveryNotice)
	channelMonitorRecoveryNotices.Unlock()
	snapshot.QuarantineCount++
	deliverChannelMonitorRecoveryNotice(snapshot, receiver)
	assert.Equal(t, 1, calls, "the upgraded persisted baseline must still detect new isolation")
	deliverChannelMonitorRecoveryNotice(snapshot, receiver)
	assert.Equal(t, 1, calls, "unchanged history stays acknowledged")
}

func TestChannelMonitorRecoveryRedisOutageDoesNotMakeHistoryNewAgain(t *testing.T) {
	input := monitoringRecoveryFixture(100)
	input.Realtime.QuarantineCount = 2604
	input.Realtime.CostDeadLetterCount = 2
	state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
	input.Now = 110
	input.Realtime.RedisAvailable = false
	input.ObservationComplete = false
	input.Realtime.QuarantineCount, input.Realtime.CostDeadLetterCount = 0, 0
	state = deriveChannelMonitorRecovery(input, state)
	input = monitoringRecoveryFixture(120)
	input.Realtime.QuarantineCount = 2604
	input.Realtime.CostDeadLetterCount = 2
	state = deriveChannelMonitorRecovery(input, state)
	assert.NotContains(t, state.Snapshot.DegradedReasons, "events_quarantined")
	assert.NotContains(t, state.Snapshot.DegradedReasons, "cost_dead_letter")
	assert.Equal(t, int64(2604), state.Snapshot.QuarantineCount)
}

func TestChannelMonitorRecoveryTransientBacklogNeedsNoNotification(t *testing.T) {
	input := monitoringRecoveryFixture(100)
	state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
	input.Now = 110
	input.Realtime.PendingCount = 1
	input.Realtime.ConsumerLagSeconds = 35
	state = deriveChannelMonitorRecovery(input, state)
	require.Equal(t, "retrying", state.Snapshot.RecoveryStatus)
	assert.Empty(t, channelMonitorRecoveryNoticeKind(state.Snapshot, channelMonitorRecoveryNotice{}, input.Now))

	input.Now = 120
	input.Realtime = monitoringRecoveryFixture(120).Realtime
	state = deriveChannelMonitorRecovery(input, state)
	for _, now := range []int64{130, 140, 150, 160, 170, 180} {
		input.Now = now
		state = deriveChannelMonitorRecovery(input, state)
		assert.Empty(t, channelMonitorRecoveryNoticeKind(state.Snapshot, channelMonitorRecoveryNotice{}, now), "automatic recovery must not send an alert/recovery email pair")
	}
	assert.Equal(t, "recovered", state.Snapshot.RecoveryStatus)
}

func TestChannelMonitorRecoveryNotifiesWhenAutomaticRecoveryCannotFinish(t *testing.T) {
	input := monitoringRecoveryFixture(100)
	input.Realtime.PendingCount = 1
	input.Realtime.ConsumerLagSeconds = 40
	state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
	input.Now = 399
	state = deriveChannelMonitorRecovery(input, state)
	assert.Empty(t, channelMonitorRecoveryNoticeKind(state.Snapshot, channelMonitorRecoveryNotice{}, input.Now))
	input.Now = 400
	state = deriveChannelMonitorRecovery(input, state)
	require.Equal(t, "manual_required", state.Snapshot.RecoveryStatus)
	assert.Equal(t, "alert", channelMonitorRecoveryNoticeKind(state.Snapshot, channelMonitorRecoveryNotice{}, input.Now))

	notice := finishChannelMonitorRecoveryNotice(channelMonitorRecoveryNotice{}, state.Snapshot, "alert", 400, nil)
	input = monitoringRecoveryFixture(410)
	for _, now := range []int64{410, 420, 430, 440, 450, 460, 470} {
		input.Now = now
		state = deriveChannelMonitorRecovery(input, state)
	}
	assert.Equal(t, "recovery", channelMonitorRecoveryNoticeKind(state.Snapshot, notice, input.Now))
}

func TestChannelMonitorRecoveryDoesNotDelayActionableFailureNotifications(t *testing.T) {
	for _, reason := range []string{"health_observation_failed", "writer_stopped", "samples_dropped", "cost_publish_failure"} {
		t.Run(reason, func(t *testing.T) {
			snapshot := ChannelMonitorRecovery{
				ChannelMonitorMonitoringHealth: ChannelMonitorMonitoringHealth{
					Status: ChannelMonitorHealthDegraded, DegradedReasons: []string{reason, "event_backlog"},
				},
				CheckedAt: 100, RecoveryStatus: "retrying",
			}
			assert.Equal(t, "alert", channelMonitorRecoveryNoticeKind(snapshot, channelMonitorRecoveryNotice{}, 100))
		})
	}
	snapshot := ChannelMonitorRecovery{
		ChannelMonitorMonitoringHealth: ChannelMonitorMonitoringHealth{Status: ChannelMonitorHealthUnavailable},
		CheckedAt:                      100, RecoveryStatus: "retrying",
	}
	assert.Equal(t, "alert", channelMonitorRecoveryNoticeKind(snapshot, channelMonitorRecoveryNotice{}, 100))
}
