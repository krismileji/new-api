package service

import (
	"slices"
	"strings"
)

const (
	channelMonitorRecoveryStableSeconds    = 60
	channelMonitorRecoveryStaleSeconds     = 30
	channelMonitorRecoveryAttentionSeconds = 300
)

// ChannelMonitorRecovery describes this node's runtime separately from the
// completeness of previously collected statistics.
type ChannelMonitorRecovery struct {
	ChannelMonitorMonitoringHealth
	NodeID            string   `json:"node_id"`
	CheckedAt         int64    `json:"checked_at"`
	RecoveredAt       int64    `json:"recovered_at"`
	RecoveryStatus    string   `json:"recovery_status"`
	DataGapReasons    []string `json:"data_gap_reasons"`
	Message           string   `json:"message"`
	Action            string   `json:"action"`
	NotificationError string   `json:"notification_error,omitempty"`
	RecoveryConfirmed bool     `json:"recovery_confirmed"`
	LastProgressAt    int64    `json:"last_progress_at"`
}

type channelMonitorRecoveryInput struct {
	Now                   int64
	NodeID                string
	Realtime              ChannelMonitorRedisRealtimeStatus
	ObservationComplete   bool
	WriterRunning         bool
	CostWorkerRunning     bool
	EventOutboxPending    int64
	EventOutboxOldest     int64
	CostLedgerApplied     int64
	CostStreamOldest      int64
	CostProjectionPending bool
	ExtraReasons          []string
	DataGapReasons        []string
}

type channelMonitorRecoveryState struct {
	Health                 ChannelMonitorHealthState
	Snapshot               ChannelMonitorRecovery
	HealthySince           int64
	LastProgressAt         int64
	LastProcessedAt        int64
	CostLedgerApplied      int64
	Dropped                int64
	PublishFailed          int64
	BacklogSince           int64
	ProjectionPendingSince int64
}

func deriveChannelMonitorRecovery(input channelMonitorRecoveryInput, previous channelMonitorRecoveryState) channelMonitorRecoveryState {
	raw := input.Realtime
	reasons := append([]string{}, input.ExtraReasons...)
	gaps := append([]string{}, previous.Snapshot.DataGapReasons...)
	gaps = append(gaps, input.DataGapReasons...)
	for _, reason := range raw.DegradedReasons {
		switch reason {
		case ChannelMonitorRedisDegradedReasonWriterQueueFull, ChannelMonitorRedisDegradedReasonCostPublishFailure, ChannelMonitorRedisDegradedReasonCostDeadLetter, "samples_dropped", "daily_replay_incomplete":
			gaps = append(gaps, reason)
		case ChannelMonitorRedisDegradedReasonEventBacklog, ChannelMonitorRedisDegradedReasonCostStreamBacklog:
			// Small in-flight batches are normal. Persistent backlog is assessed below.
		default:
			reasons = append(reasons, reason)
		}
	}
	if raw.WriterDroppedEvents > 0 {
		gaps = append(gaps, "samples_dropped")
	}
	if raw.QuarantineCount > 0 {
		gaps = append(gaps, "events_quarantined")
	}
	if raw.CostDeadLetterCount > 0 {
		gaps = append(gaps, ChannelMonitorRedisDegradedReasonCostDeadLetter)
	}
	if raw.CostPublishFailedCount > 0 {
		gaps = append(gaps, ChannelMonitorRedisDegradedReasonCostPublishFailure)
	}
	if !input.ObservationComplete {
		reasons = append(reasons, "health_observation_failed")
	}
	if !input.WriterRunning {
		reasons = append(reasons, ChannelMonitorRedisDegradedReasonWriterStopped)
	}
	if !input.CostWorkerRunning {
		reasons = append(reasons, "cost_worker_stopped")
	}
	if raw.WriterDroppedEvents > previous.Dropped {
		reasons = append(reasons, "samples_dropped")
	}
	if raw.CostPublishFailedCount > previous.PublishFailed {
		reasons = append(reasons, ChannelMonitorRedisDegradedReasonCostPublishFailure)
	}
	if input.CostStreamOldest > 0 && input.Now-input.CostStreamOldest >= 30 {
		reasons = append(reasons, ChannelMonitorRedisDegradedReasonCostStreamBacklog)
	}
	projectionSince := previous.ProjectionPendingSince
	if input.CostProjectionPending {
		if projectionSince == 0 {
			projectionSince = input.Now
		}
		if input.Now-projectionSince >= 30 {
			reasons = append(reasons, "cost_projection_pending")
		}
	} else {
		projectionSince = 0
	}

	pending := max(0, raw.PendingCount) + int64(max(0, raw.WriterQueueDepth)) + max(0, input.EventOutboxPending) + max(0, raw.CostOutboxPendingCount) + max(0, raw.CostStreamPendingCount) + max(0, raw.CostStreamUnreadCount)
	backlogSince := previous.BacklogSince
	progress := raw.LastProcessedAt > previous.LastProcessedAt || input.CostLedgerApplied > previous.CostLedgerApplied || pending < previous.Snapshot.PendingCount
	backlog := raw.PendingCount > 0 || raw.OldestPendingAt > 0 || raw.WriterQueueDepth > 0 || input.EventOutboxPending > 0 || raw.CostStreamPendingCount+raw.CostStreamUnreadCount > 0
	if backlog {
		if backlogSince == 0 || progress {
			backlogSince = input.Now
		}
		if input.Now-backlogSince >= 30 || raw.ConsumerLagSeconds >= 30 || input.EventOutboxOldest > 0 && input.Now-input.EventOutboxOldest >= 30 {
			reasons = append(reasons, ChannelMonitorRedisDegradedReasonEventBacklog)
		}
	} else {
		backlogSince = 0
	}

	health, base := DeriveChannelMonitorMonitoringHealth(ChannelMonitorHealthInput{
		Now: input.Now, RedisAvailable: raw.RedisAvailable, ConsumerRunning: raw.RedisConsumerRunning,
		DegradedReasons: normalizeChannelMonitorHealthReasons(reasons),
	}, previous.Health)
	health.PendingCount = pending
	health.DroppedSampleCount = max(0, raw.WriterDroppedEvents)
	health.ConsumerLagSeconds = raw.ConsumerLagSeconds
	snapshot := ChannelMonitorRecovery{
		ChannelMonitorMonitoringHealth: health, NodeID: input.NodeID, CheckedAt: input.Now,
		RecoveredAt: previous.Snapshot.RecoveredAt, DataGapReasons: normalizeChannelMonitorHealthReasons(gaps),
	}
	state := channelMonitorRecoveryState{
		Health: base, Snapshot: snapshot, HealthySince: previous.HealthySince,
		LastProgressAt: previous.LastProgressAt, LastProcessedAt: raw.LastProcessedAt,
		CostLedgerApplied: input.CostLedgerApplied, Dropped: raw.WriterDroppedEvents,
		PublishFailed: raw.CostPublishFailedCount, BacklogSince: backlogSince,
		ProjectionPendingSince: projectionSince,
	}
	if progress {
		state.LastProgressAt = input.Now
	}
	state.Snapshot.LastProgressAt = state.LastProgressAt
	wasAbnormal := previous.Snapshot.FirstDegradedAt > 0
	if health.Status != ChannelMonitorHealthHealthy {
		state.HealthySince = 0
		state.Snapshot.RecoveryStatus = "retrying"
		state.Snapshot.Message = "监控异常，等待后台重试"
		state.Snapshot.Action = "若持续出现，请检查 Redis、监控后台任务和数据库连接。"
		// Historical quarantine totals belong to DataGapReasons; they do not
		// establish that a new runtime delay needs manual intervention.
		if !input.ObservationComplete || !input.WriterRunning || !input.CostWorkerRunning || input.Now-health.FirstDegradedAt >= channelMonitorRecoveryAttentionSeconds {
			state.Snapshot.RecoveryStatus = "manual_required"
			state.Snapshot.Message = "需要人工处理"
		} else if input.ObservationComplete && raw.RedisAvailable && raw.RedisConsumerRunning && progress && pending > 0 {
			state.Snapshot.RecoveryStatus = "recovering"
			state.Snapshot.Message = "正在自动恢复"
			state.Snapshot.Action = "后台正在处理积压事件，请留意待处理数量和最近进展。"
		}
		return state
	}
	if state.HealthySince == 0 || input.Now-previous.Snapshot.CheckedAt > channelMonitorRecoveryStaleSeconds {
		state.HealthySince = input.Now
	}
	state.Snapshot.RecoveryConfirmed = input.Now-state.HealthySince >= channelMonitorRecoveryStableSeconds
	if wasAbnormal {
		if state.HealthySince == 0 || input.Now-state.HealthySince < channelMonitorRecoveryStableSeconds {
			state.Health = previous.Health
			state.Snapshot.Status = ChannelMonitorHealthDegraded
			state.Snapshot.FirstDegradedAt = previous.Snapshot.FirstDegradedAt
			state.Snapshot.DegradedReasons = previous.Snapshot.DegradedReasons
			state.Snapshot.RecoveryStatus = "recovering"
			state.Snapshot.Message = "正在确认恢复"
			state.Snapshot.Action = "相关链路需连续正常 60 秒，正常批处理中的新事件仍会继续处理。"
			return state
		}
		state.Snapshot.RecoveredAt = input.Now
	}
	state.Snapshot.RecoveryStatus = "healthy"
	state.Snapshot.Message = "监控运行正常"
	if state.Snapshot.RecoveredAt > 0 {
		state.Snapshot.RecoveryStatus = "recovered"
		state.Snapshot.Message = "运行已恢复"
	}
	if len(state.Snapshot.DataGapReasons) > 0 {
		state.Snapshot.RecoveryStatus = "data_incomplete"
		state.Snapshot.Message = "运行正常，部分历史统计不完整"
		state.Snapshot.Action = "请复核丢弃或隔离记录；运行恢复不代表历史数据已补齐。"
	}
	return state
}

func normalizeChannelMonitorHealthReasons(reasons []string) []string {
	result := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		if reason = strings.TrimSpace(reason); reason != "" {
			result = append(result, reason)
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}
