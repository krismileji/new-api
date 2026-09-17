package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func monitoringCongestedPoolFixture(now int64) channelMonitorRecoveryInput {
	input := monitoringRecoveryFixture(now)
	input.Realtime.LastProcessedAt = now
	input.Realtime.PendingCount = 1
	input.Realtime.DegradedReasons = []string{ChannelMonitorRedisDegradedReasonPoolCongested}
	input.Realtime.RedisPoolStats = map[common.RedisClientRole]common.RedisClientPoolStats{
		common.RedisClientRoleMonitorConsumer: {
			PoolSize: 4, InUse: 4, PoolCongested: true,
			DegradedReason: common.RedisClientPoolDegradedReasonPoolCongested,
		},
	}
	return input
}

func TestChannelMonitorRecoveryTransientPoolCongestionSendsNoAlertOrRecovery(t *testing.T) {
	var state channelMonitorRecoveryState
	for _, now := range []int64{100, 110, 120, 130, 140, 150, 160, 170, 180, 190, 200, 210, 220} {
		input := monitoringCongestedPoolFixture(now)
		if now >= 160 {
			input.Realtime.DegradedReasons = nil
			input.Realtime.RedisPoolStats[common.RedisClientRoleMonitorConsumer] = common.RedisClientPoolStats{PoolSize: 4}
		}
		state = deriveChannelMonitorRecovery(input, state)
		assert.Empty(t, channelMonitorRecoveryNoticeKind(state.Snapshot, channelMonitorRecoveryNotice{}, now), "at %d", now)
	}
	assert.Equal(t, ChannelMonitorHealthHealthy, state.Snapshot.Status)
	assert.True(t, state.Snapshot.RecoveryConfirmed)
}

func TestChannelMonitorRecoverySustainedPoolCongestionAlertsAfterOneMinute(t *testing.T) {
	var state channelMonitorRecoveryState
	for _, now := range []int64{100, 110, 120, 130, 140, 150, 159} {
		state = deriveChannelMonitorRecovery(monitoringCongestedPoolFixture(now), state)
		assert.Empty(t, channelMonitorRecoveryNoticeKind(state.Snapshot, channelMonitorRecoveryNotice{}, now), "at %d", now)
	}
	state = deriveChannelMonitorRecovery(monitoringCongestedPoolFixture(160), state)
	require.Equal(t, "alert", channelMonitorRecoveryNoticeKind(state.Snapshot, channelMonitorRecoveryNotice{}, 160))
	notice := finishChannelMonitorRecoveryNotice(channelMonitorRecoveryNotice{}, state.Snapshot, "alert", 160, nil)
	for _, now := range []int64{170, 180, 190, 200, 210, 220} {
		state = deriveChannelMonitorRecovery(monitoringRecoveryFixture(now), state)
		assert.Empty(t, channelMonitorRecoveryNoticeKind(state.Snapshot, notice, now))
	}
	state = deriveChannelMonitorRecovery(monitoringRecoveryFixture(230), state)
	assert.Equal(t, "recovery", channelMonitorRecoveryNoticeKind(state.Snapshot, notice, 230))
}

func TestChannelMonitorRecoveryRecurringShortPoolPeaksDoNotBecomeManualIncidents(t *testing.T) {
	var state channelMonitorRecoveryState
	// Cover more than the five-minute escalation window with alternating
	// busy and idle samples, without advancing wall-clock time or sleeping.
	for now := int64(100); now <= 460; now += 20 {
		input := monitoringCongestedPoolFixture(now)
		if (now-100)%40 != 0 {
			input.Realtime.DegradedReasons = nil
			input.Realtime.RedisPoolStats[common.RedisClientRoleMonitorConsumer] = common.RedisClientPoolStats{PoolSize: 4}
		}
		state = deriveChannelMonitorRecovery(input, state)
		assert.Equal(t, ChannelMonitorHealthHealthy, state.Snapshot.Status, "at %d", now)
		assert.Empty(t, channelMonitorRecoveryNoticeKind(state.Snapshot, channelMonitorRecoveryNotice{}, now))
	}
}

func TestChannelMonitorRecoveryPoolCongestionRequiresComparableContinuousSamples(t *testing.T) {
	for _, change := range []string{"idle interval", "different pool", "stale observation", "different node", "reset counters", "incomplete observation"} {
		t.Run(change, func(t *testing.T) {
			var state channelMonitorRecoveryState
			for _, now := range []int64{100, 110, 120, 130, 140, 150} {
				input := monitoringCongestedPoolFixture(now)
				pool := input.Realtime.RedisPoolStats[common.RedisClientRoleMonitorConsumer]
				pool.CommandCount = 20
				input.Realtime.RedisPoolStats[common.RedisClientRoleMonitorConsumer] = pool
				state = deriveChannelMonitorRecovery(input, state)
			}
			input := monitoringCongestedPoolFixture(160)
			pool := input.Realtime.RedisPoolStats[common.RedisClientRoleMonitorConsumer]
			pool.CommandCount = 21
			switch change {
			case "idle interval":
				input.Realtime.DegradedReasons = nil
				pool.PoolCongested, pool.InUse, pool.DegradedReason = false, 0, ""
			case "different pool":
				input.Realtime.RedisPoolStats = map[common.RedisClientRole]common.RedisClientPoolStats{common.RedisClientRoleMonitorRead: pool}
			case "stale observation":
				input.Now = 181
			case "different node":
				input.NodeID = "node-b"
			case "reset counters":
				pool.CommandCount = 1
			case "incomplete observation":
				input.ObservationComplete = false
			}
			if change != "different pool" {
				input.Realtime.RedisPoolStats[common.RedisClientRoleMonitorConsumer] = pool
			}
			state = deriveChannelMonitorRecovery(input, state)
			assert.False(t, state.Snapshot.Diagnostics.PoolCongestionConfirmed)
			if change == "incomplete observation" {
				assert.Equal(t, "alert", channelMonitorRecoveryNoticeKind(state.Snapshot, channelMonitorRecoveryNotice{}, input.Now), "missing observations must still alert")
			} else {
				assert.Empty(t, channelMonitorRecoveryNoticeKind(state.Snapshot, channelMonitorRecoveryNotice{}, input.Now))
			}
		})
	}
}

func TestChannelMonitorRecoveryPoolWarmupDoesNotDelayActualFailures(t *testing.T) {
	for _, reason := range []string{
		ChannelMonitorRedisDegradedReasonPoolTimeout, ChannelMonitorRedisDegradedReasonContextDeadline,
		ChannelMonitorRedisDegradedReasonPublisherUnavailable, ChannelMonitorRedisDegradedReasonWriterStopped,
		ChannelMonitorRedisDegradedReasonConsumerStopped, ChannelMonitorRedisDegradedReasonRedisUnavailable,
		"samples_dropped", "health_observation_failed",
	} {
		t.Run(reason, func(t *testing.T) {
			input := monitoringCongestedPoolFixture(100)
			switch reason {
			case ChannelMonitorRedisDegradedReasonRedisUnavailable:
				input.Realtime.RedisAvailable = false
			case "samples_dropped":
				input.Realtime.WriterDroppedEvents = 1
			default:
				input.Realtime.DegradedReasons = append(input.Realtime.DegradedReasons, reason)
			}
			state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
			assert.Equal(t, "alert", channelMonitorRecoveryNoticeKind(state.Snapshot, channelMonitorRecoveryNotice{}, input.Now))
		})
	}
}

func TestChannelMonitorRecoveryPoolTimeoutDeltasHandleMissingAndResetBaselines(t *testing.T) {
	input := monitoringCongestedPoolFixture(100)
	pool := input.Realtime.RedisPoolStats[common.RedisClientRoleMonitorConsumer]
	pool.PoolTimeoutCount, pool.ContextDeadlineCount = 8, 4
	input.Realtime.RedisPoolStats[common.RedisClientRoleMonitorConsumer] = pool
	state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
	require.Len(t, state.Snapshot.Diagnostics.RedisPools, 1)
	assert.Zero(t, state.Snapshot.Diagnostics.RedisPools[0].ComparedAt)
	_, body := BuildChannelMonitorRecoveryEmail(state.Snapshot, "alert", time.Unix(input.Now, 0))
	assert.Contains(t, body, "新增超时：尚无连续采样基线")
	assert.Contains(t, body, "当前进程累计：等待连接超时 8 次，操作超时 4 次")

	input.Now = 110
	pool.PoolTimeoutCount, pool.ContextDeadlineCount = 10, 5
	input.Realtime.RedisPoolStats[common.RedisClientRoleMonitorConsumer] = pool
	state = deriveChannelMonitorRecovery(input, state)
	observed := state.Snapshot.Diagnostics.RedisPools[0]
	assert.Equal(t, int64(100), observed.ComparedAt)
	assert.Equal(t, uint64(2), observed.PoolTimeoutDelta)
	assert.Equal(t, uint64(1), observed.ContextDeadlineDelta)

	input.Now = 120
	pool.PoolTimeoutCount, pool.ContextDeadlineCount = 1, 0
	input.Realtime.RedisPoolStats[common.RedisClientRoleMonitorConsumer] = pool
	state = deriveChannelMonitorRecovery(input, state)
	observed = state.Snapshot.Diagnostics.RedisPools[0]
	assert.Zero(t, observed.ComparedAt)
	assert.Zero(t, observed.PoolTimeoutDelta, "counter reset must not underflow into a fabricated timeout delta")
	assert.Zero(t, observed.ContextDeadlineDelta)
}

func TestBuildChannelMonitorRecoveryEmailIncludesSampledPoolAndQueueDiagnostics(t *testing.T) {
	zone := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 9, 17, 23, 0, 0, 0, zone)
	input := monitoringCongestedPoolFixture(now.Unix() - 10)
	input.NodeID = "<node&>"
	pool := input.Realtime.RedisPoolStats[common.RedisClientRoleMonitorConsumer]
	pool.PoolTimeoutCount, pool.ContextDeadlineCount = 3, 1
	pool.Shared, pool.SharedWith = true, common.RedisClientRoleUser
	input.Realtime.RedisPoolStats[common.RedisClientRoleMonitorConsumer] = pool
	previous := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
	input.Now = now.Unix()
	input.Realtime.LastProcessedAt = input.Now
	input.Realtime.WriterQueueDepth, input.EventOutboxPending = 2, 3
	input.Realtime.CostOutboxPendingCount, input.Realtime.CostStreamPendingCount, input.Realtime.CostStreamUnreadCount = 4, 5, 6
	input.Realtime.CostOutboxOldestPendingAt = input.Now - 40
	pool.PoolTimeoutCount, pool.ContextDeadlineCount = 5, 2
	pool.DegradedReason = common.RedisClientPoolDegradedReasonPoolTimeout
	input.Realtime.RedisPoolStats[common.RedisClientRoleMonitorConsumer] = pool
	input.Realtime.DegradedReasons = []string{ChannelMonitorRedisDegradedReasonPoolTimeout}
	state := deriveChannelMonitorRecovery(input, previous)
	subject, body := BuildChannelMonitorRecoveryEmail(state.Snapshot, "alert", now.Add(5*time.Second))
	assert.Equal(t, "渠道监控异常：等待监控连接超时", subject)
	for _, detail := range []string{
		"后台采样：</strong>2026-09-17 23:00:00 UTC+08:00",
		"队列待处理合计：21 条；事件处理延迟：0 秒",
		"事件消费 1 条，监控采集 2 条，事件补偿 3 条；成本待记账 4 条，成本待确认 5 条，成本未读取 6 条",
		"最近处理进展：2026-09-17 23:00:00（距本次采样 0 秒）",
		"最早待记账成本：2026-09-17 22:59:20（已等待 40 秒）",
		"监控消费（monitor_consumer）：等待连接超时，连接使用 4 / 4",
		"与 user 共用连接池",
		"近 10 秒新增：等待连接超时 2 次，操作超时 1 次",
		"当前进程累计：等待连接超时 5 次，操作超时 2 次",
		"&lt;node&amp;&gt;",
	} {
		assert.Contains(t, body, detail)
	}
	assert.NotContains(t, body, input.NodeID)
}

func TestBuildChannelMonitorRecoveryEmailDoesNotPresentMissingQueueDataAsZero(t *testing.T) {
	input := monitoringRecoveryFixture(100)
	input.ObservationComplete = false
	input.Realtime.RedisAvailable = false
	state := deriveChannelMonitorRecovery(input, channelMonitorRecoveryState{})
	_, body := BuildChannelMonitorRecoveryEmail(state.Snapshot, "alert", time.Unix(100, 0))
	assert.Contains(t, body, "本次健康检查未完成，待处理数量和处理延迟尚未确认")
	assert.Contains(t, body, "本次未取得 Redis 连接池采样")
	assert.NotContains(t, body, "队列待处理合计：0 条")
	assert.NotContains(t, body, "事件处理延迟：0 秒")
	assert.NotContains(t, body, "成本待记账 0 条")
}
