package service

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const channelMonitorPoolCongestionAlertSeconds = 60

// Diagnostics use the health worker's samples, not page refreshes. Counters
// stay cumulative; a delta is reported only across comparable fresh samples.
type ChannelMonitorRecoveryDiagnostics struct {
	ObservationComplete        bool                              `json:"observation_complete"`
	RedisPools                 []ChannelMonitorRecoveryRedisPool `json:"redis_pools"`
	PoolCongestionConfirmed    bool                              `json:"pool_congestion_confirmed"`
	EventPendingCount          int64                             `json:"event_pending_count"`
	WriterQueueDepth           int                               `json:"writer_queue_depth"`
	EventOutboxPendingCount    int64                             `json:"event_outbox_pending_count"`
	CostOutboxPendingCount     int64                             `json:"cost_outbox_pending_count"`
	CostStreamPendingCount     int64                             `json:"cost_stream_pending_count"`
	CostStreamUnreadCount      int64                             `json:"cost_stream_unread_count"`
	OldestEventPendingAt       int64                             `json:"oldest_event_pending_at"`
	EventOutboxOldestPendingAt int64                             `json:"event_outbox_oldest_pending_at"`
	CostOutboxOldestPendingAt  int64                             `json:"cost_outbox_oldest_pending_at"`
}

type ChannelMonitorRecoveryRedisPool struct {
	common.RedisClientPoolStats
	CongestedSince       int64  `json:"congested_since"`
	ComparedAt           int64  `json:"compared_at"`
	PoolTimeoutDelta     uint64 `json:"pool_timeout_delta"`
	ContextDeadlineDelta uint64 `json:"context_deadline_delta"`
}

func deriveChannelMonitorRecoveryDiagnostics(input channelMonitorRecoveryInput, previous ChannelMonitorRecovery) *ChannelMonitorRecoveryDiagnostics {
	raw := input.Realtime
	diagnostics := &ChannelMonitorRecoveryDiagnostics{
		ObservationComplete:        input.ObservationComplete && raw.RedisAvailable,
		RedisPools:                 make([]ChannelMonitorRecoveryRedisPool, 0, len(raw.RedisPoolStats)),
		EventPendingCount:          max(0, raw.PendingCount),
		WriterQueueDepth:           max(0, raw.WriterQueueDepth),
		EventOutboxPendingCount:    max(0, input.EventOutboxPending),
		CostOutboxPendingCount:     max(0, raw.CostOutboxPendingCount),
		CostStreamPendingCount:     max(0, raw.CostStreamPendingCount),
		CostStreamUnreadCount:      max(0, raw.CostStreamUnreadCount),
		OldestEventPendingAt:       raw.OldestPendingAt,
		EventOutboxOldestPendingAt: input.EventOutboxOldest,
		CostOutboxOldestPendingAt:  raw.CostOutboxOldestPendingAt,
	}
	fresh := previous.NodeID == input.NodeID && previous.CheckedAt > 0 && input.Now > previous.CheckedAt &&
		input.Now-previous.CheckedAt <= channelMonitorRecoveryStaleSeconds
	for _, role := range []common.RedisClientRole{
		common.RedisClientRoleUser, common.RedisClientRoleMonitorWrite,
		common.RedisClientRoleMonitorRead, common.RedisClientRoleMonitorConsumer,
	} {
		stats, ok := raw.RedisPoolStats[role]
		if !ok {
			continue
		}
		stats.Role = role
		pool := ChannelMonitorRecoveryRedisPool{RedisClientPoolStats: stats}
		if stats.PoolCongested && !stats.Unavailable && input.ObservationComplete && raw.RedisAvailable {
			pool.CongestedSince = input.Now
		}
		if fresh && !stats.Unavailable && previous.Diagnostics != nil {
			for _, prior := range previous.Diagnostics.RedisPools {
				if prior.Role != role || prior.Unavailable || prior.PoolSize != stats.PoolSize ||
					prior.Shared != stats.Shared || prior.SharedWith != stats.SharedWith ||
					stats.PoolTimeoutCount < prior.PoolTimeoutCount || stats.ContextDeadlineCount < prior.ContextDeadlineCount ||
					stats.CommandCount < prior.CommandCount {
					continue
				}
				pool.ComparedAt = previous.CheckedAt
				pool.PoolTimeoutDelta = stats.PoolTimeoutCount - prior.PoolTimeoutCount
				pool.ContextDeadlineDelta = stats.ContextDeadlineCount - prior.ContextDeadlineCount
				if pool.CongestedSince > 0 && prior.CongestedSince > 0 && prior.CongestedSince <= previous.CheckedAt && prior.PoolCongested {
					pool.CongestedSince = prior.CongestedSince
				}
				break
			}
		}
		if pool.CongestedSince > 0 && input.Now-pool.CongestedSince >= channelMonitorPoolCongestionAlertSeconds {
			diagnostics.PoolCongestionConfirmed = true
		}
		diagnostics.RedisPools = append(diagnostics.RedisPools, pool)
	}
	return diagnostics
}

func writeChannelMonitorRecoveryDiagnostics(content *strings.Builder, snapshot ChannelMonitorRecovery, location *time.Location) {
	if snapshot.CheckedAt <= 0 {
		return
	}
	fmt.Fprintf(content, "<p><strong>后台采样：</strong>%s</p>",
		html.EscapeString(time.Unix(snapshot.CheckedAt, 0).In(location).Format("2006-01-02 15:04:05 UTC-07:00")))
	if snapshot.LastProgressAt > 0 {
		fmt.Fprintf(content, "<p>最近处理进展：%s（距本次采样 %d 秒）。</p>",
			html.EscapeString(time.Unix(snapshot.LastProgressAt, 0).In(location).Format("2006-01-02 15:04:05")),
			max(0, snapshot.CheckedAt-snapshot.LastProgressAt))
	} else {
		content.WriteString("<p>最近处理进展：尚未观察到。</p>")
	}
	diagnostics := snapshot.Diagnostics
	if diagnostics == nil {
		content.WriteString("<p>本次未取得详细诊断数据。</p>")
		return
	}
	if diagnostics.ObservationComplete {
		fmt.Fprintf(content, "<p>队列待处理合计：%d 条；事件处理延迟：%d 秒。</p>", snapshot.PendingCount, snapshot.ConsumerLagSeconds)
		fmt.Fprintf(content, "<p><strong>待处理明细：</strong>事件消费 %d 条，监控采集 %d 条，事件补偿 %d 条；成本待记账 %d 条，成本待确认 %d 条，成本未读取 %d 条。各处理阶段合计不代表失败请求数。</p>",
			diagnostics.EventPendingCount, diagnostics.WriterQueueDepth, diagnostics.EventOutboxPendingCount,
			diagnostics.CostOutboxPendingCount, diagnostics.CostStreamPendingCount, diagnostics.CostStreamUnreadCount)
		for _, oldest := range []struct {
			label string
			at    int64
		}{
			{"最早待处理事件", diagnostics.OldestEventPendingAt},
			{"最早待补偿事件", diagnostics.EventOutboxOldestPendingAt},
			{"最早待记账成本", diagnostics.CostOutboxOldestPendingAt},
		} {
			if oldest.at > 0 {
				fmt.Fprintf(content, "<p>%s：%s（已等待 %d 秒）。</p>", oldest.label,
					html.EscapeString(time.Unix(oldest.at, 0).In(location).Format("2006-01-02 15:04:05")), max(0, snapshot.CheckedAt-oldest.at))
			}
		}
	} else {
		content.WriteString("<p>本次健康检查未完成，待处理数量和处理延迟尚未确认。</p>")
	}
	if len(diagnostics.RedisPools) == 0 {
		content.WriteString("<p>本次未取得 Redis 连接池采样。</p>")
		return
	}
	poolLabels := map[common.RedisClientRole]string{
		common.RedisClientRoleUser: "业务请求", common.RedisClientRoleMonitorWrite: "监控写入",
		common.RedisClientRoleMonitorRead: "监控读取", common.RedisClientRoleMonitorConsumer: "监控消费",
	}
	content.WriteString("<p><strong>Redis 连接池：</strong></p><ul>")
	for _, pool := range diagnostics.RedisPools {
		label := poolLabels[pool.Role]
		if label == "" {
			label = string(pool.Role)
		}
		status := "未见满池或超时"
		switch {
		case pool.Unavailable:
			status = "不可用"
		case pool.DegradedReason == common.RedisClientPoolDegradedReasonPoolTimeout:
			status = "等待连接超时"
		case pool.DegradedReason == common.RedisClientPoolDegradedReasonContextDeadline:
			status = "操作超时"
		case pool.PoolCongested:
			status = "连接繁忙"
		case pool.DegradedReason != "":
			status = "异常：" + pool.DegradedReason
		}
		fmt.Fprintf(content, "<li>%s（%s）：%s，连接使用 %d / %d。",
			html.EscapeString(label), html.EscapeString(string(pool.Role)), html.EscapeString(status), pool.InUse, pool.PoolSize)
		if pool.Shared {
			fmt.Fprintf(content, "与 %s 共用连接池。", html.EscapeString(string(pool.SharedWith)))
		}
		if pool.CongestedSince > 0 {
			fmt.Fprintf(content, "连续采样满池 %d 秒。", max(0, snapshot.CheckedAt-pool.CongestedSince))
		}
		if pool.ComparedAt > 0 && snapshot.CheckedAt > pool.ComparedAt {
			fmt.Fprintf(content, "近 %d 秒新增：等待连接超时 %d 次，操作超时 %d 次。",
				snapshot.CheckedAt-pool.ComparedAt, pool.PoolTimeoutDelta, pool.ContextDeadlineDelta)
		} else {
			content.WriteString("新增超时：尚无连续采样基线。")
		}
		fmt.Fprintf(content, "当前进程累计：等待连接超时 %d 次，操作超时 %d 次。</li>", pool.PoolTimeoutCount, pool.ContextDeadlineCount)
	}
	content.WriteString("</ul>")
}
