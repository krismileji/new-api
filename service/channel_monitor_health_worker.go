package service

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

const channelMonitorHealthPollInterval = 10 * time.Second

var channelMonitorHealthWorkerState struct {
	sync.RWMutex
	state channelMonitorRecoveryState
}

var channelMonitorHealthWake = make(chan struct{}, 1)

func GetChannelMonitorRecovery() ChannelMonitorRecovery {
	if !common.RedisEnabled {
		return ChannelMonitorRecovery{ChannelMonitorMonitoringHealth: ChannelMonitorMonitoringHealth{Status: ChannelMonitorHealthUnavailable}, NodeID: channelMonitorRedisInstanceIdentity(), RecoveryStatus: "disabled", Message: "实时监控未启用", Action: "Redis 未启用，请检查监控部署配置。", DataGapReasons: []string{}}
	}
	channelMonitorHealthWorkerState.RLock()
	snapshot := channelMonitorHealthWorkerState.state.Snapshot
	channelMonitorHealthWorkerState.RUnlock()
	snapshot.DegradedReasons = append([]string{}, snapshot.DegradedReasons...)
	snapshot.DataGapReasons = append([]string{}, snapshot.DataGapReasons...)
	if snapshot.CheckedAt == 0 || time.Now().Unix()-snapshot.CheckedAt > channelMonitorRecoveryStaleSeconds {
		snapshot.Status = ChannelMonitorHealthUnavailable
		snapshot.RecoveryStatus = "checking"
		snapshot.Message = "监控状态待确认"
		snapshot.Action = "后台健康检查尚未完成或结果已过期。"
		snapshot.RecoveryConfirmed = false
	}
	return snapshot
}

func (runtime *ChannelDailyCostOutboxRuntime) runHealthMonitor(ctx context.Context, startupDelay time.Duration) {
	if !common.RedisEnabled {
		return
	}
	startup := time.NewTimer(startupDelay)
	defer startup.Stop()
	select {
	case <-ctx.Done():
		return
	case <-startup.C:
	}
	ticker := time.NewTicker(channelMonitorHealthPollInterval)
	defer ticker.Stop()
	nodeID := channelMonitorRedisInstanceIdentity()
	stateKey := "channel_monitor:v1:health:node:" + channelMonitorRedisKeyPart(nodeID, "node")
	var previous channelMonitorRecoveryState
	var loaded bool
	var lastCheck time.Time
	for ctx.Err() == nil {
		if lastCheck.IsZero() || time.Since(lastCheck) >= channelMonitorHealthPollInterval {
			lastCheck = time.Now()
			input := runtime.observeMonitoringHealth(ctx, nodeID)
			client := common.RedisMonitorWriteClient()
			if client != nil && input.Realtime.RedisAvailable && !loaded {
				opCtx, cancel := context.WithTimeout(ctx, time.Second)
				payload, err := client.Get(opCtx, stateKey).Bytes()
				cancel()
				if err == nil {
					var stored channelMonitorRecoveryState
					if err := common.Unmarshal(payload, &stored); err == nil {
						// Local counters restart with the process; historical gaps do not.
						stored.Dropped, stored.PublishFailed, stored.CostLedgerApplied = 0, 0, 0
						stored.HealthySince = 0
						if previous.Snapshot.CheckedAt > stored.Snapshot.CheckedAt {
							previous.Snapshot.DataGapReasons = normalizeChannelMonitorHealthReasons(append(previous.Snapshot.DataGapReasons, stored.Snapshot.DataGapReasons...))
						} else {
							previous = stored
						}
						loaded = true
					}
				} else if errors.Is(err, redis.Nil) {
					loaded = true
				}
			}
			if !loaded {
				input.ObservationComplete = false
			}
			previous = deriveChannelMonitorRecovery(input, previous)
			if loaded && client != nil && input.Realtime.RedisAvailable {
				opCtx, cancel := context.WithTimeout(ctx, time.Second)
				if payload, err := common.Marshal(previous); err == nil {
					if err := client.Set(opCtx, stateKey, payload, 30*24*time.Hour).Err(); err != nil {
						common.SysError("渠道监控恢复状态保存失败: " + err.Error())
						input.ObservationComplete = false
						previous = deriveChannelMonitorRecovery(input, previous)
					}
				}
				cancel()
			}
			channelMonitorHealthWorkerState.Lock()
			previous.Snapshot.NotificationError = channelMonitorHealthWorkerState.state.Snapshot.NotificationError
			channelMonitorHealthWorkerState.state = previous
			channelMonitorHealthWorkerState.Unlock()
			notifyChannelMonitorRecovery(previous.Snapshot)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-channelMonitorHealthWake:
		}
	}
}

func (runtime *ChannelDailyCostOutboxRuntime) observeMonitoringHealth(ctx context.Context, nodeID string) channelMonitorRecoveryInput {
	input := channelMonitorRecoveryInput{Now: time.Now().Unix(), NodeID: nodeID, ObservationComplete: true}
	input.CostWorkerRunning = runtime.lastDBRecoveryAt.Load() > 0 && input.Now-runtime.lastDBRecoveryAt.Load() <= 120 && runtime.lastRedisConsumerAt.Load() > 0 && input.Now-runtime.lastRedisConsumerAt.Load() <= 30
	// Probe each effective client independently, including idle writers. A
	// write probe checks the write role without publishing a monitoring event.
	for _, client := range []*redis.Client{common.RDB, common.RedisMonitorReadClient(), common.RedisMonitorConsumerClient(), common.RedisMonitorWriteClient()} {
		if client == nil {
			input.ObservationComplete = false
			continue
		}
		opCtx, cancel := context.WithTimeout(ctx, time.Second)
		err := client.Ping(opCtx).Err()
		if err == nil && client == common.RedisMonitorWriteClient() {
			err = client.Set(opCtx, "channel_monitor:v1:health:probe:"+channelMonitorRedisKeyPart(nodeID, "node"), input.Now, time.Minute).Err()
		}
		cancel()
		if err != nil {
			input.ObservationComplete = false
		}
	}
	input.Realtime = GetChannelMonitorRedisRealtimeStatus(ctx)
	channelMonitorEventWriterState.RLock()
	input.WriterRunning = channelMonitorEventWriterState.writer != nil && channelMonitorEventWriterState.writer.runStarted.Load() && !channelMonitorEventWriterState.writer.stopping.Load()
	channelMonitorEventWriterState.RUnlock()
	input.Realtime.WriterDroppedEvents = GetChannelMonitorEventPublishStats().DroppedEvents
	opCtx, cancel := context.WithTimeout(ctx, channelDailyCostOutboxDBOperationTimeout)
	outbox, eventErr := model.GetChannelMonitorEventOutboxStats(opCtx)
	cost, costErr := model.GetChannelDailyCostOutboxStats(opCtx)
	cancel()
	if eventErr != nil || costErr != nil {
		input.ObservationComplete = false
	} else {
		input.EventOutboxPending = outbox.PendingCount
		input.EventOutboxOldest = outbox.OldestPending
		input.Realtime.CostOutboxPendingCount = cost.PendingCount
		input.Realtime.CostOutboxOldestPendingAt = cost.OldestPending
		input.Realtime.CostOutboxRetryCount = cost.RetryCount
		input.Realtime.DegradedReasons = slices.DeleteFunc(input.Realtime.DegradedReasons, func(reason string) bool { return reason == ChannelMonitorRedisDegradedReasonCostOutboxBacklog })
		if cost.PendingCount > 0 && (cost.RetryCount > 0 || cost.OldestPending > 0 && input.Now-cost.OldestPending > 120) {
			input.ExtraReasons = append(input.ExtraReasons, ChannelMonitorRedisDegradedReasonCostOutboxBacklog)
		}
	}
	input.CostLedgerApplied = GetChannelDailyCostReliableStats().LedgerApplied
	client := common.RedisMonitorReadClient()
	if client != nil && input.Realtime.RedisAvailable {
		opCtx, cancel := context.WithTimeout(ctx, time.Second)
		payload, err := client.Get(opCtx, channelMonitorReliableCostStatusKey).Bytes()
		var projection ChannelMonitorReliableCostStatus
		if err != nil || common.Unmarshal(payload, &projection) != nil || projection.Failed || input.Now-projection.CheckedAt > 30 {
			input.ExtraReasons = append(input.ExtraReasons, "cost_projection_unavailable")
		}
		input.CostProjectionPending = projection.Pending
		partial, err := client.HGet(opCtx, ChannelMonitorRedisSuccessDayKey(model.ChannelDailyCostDayStart(input.Now)), "meta:coverage_partial").Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			input.ObservationComplete = false
		}
		if partial == "1" {
			input.DataGapReasons = append(input.DataGapReasons, "daily_replay_incomplete")
		}
		if _, err := client.XLen(opCtx, ChannelDailyCostRedisStream).Result(); err != nil {
			input.ObservationComplete = false
		}
		if oldest, err := client.XRangeN(opCtx, ChannelDailyCostRedisStream, "-", "+", 1).Result(); err != nil {
			input.ObservationComplete = false
		} else if len(oldest) > 0 {
			input.CostStreamOldest = channelMonitorRedisStreamIDTimestamp(oldest[0].ID)
		}
		if _, err := client.XPending(opCtx, ChannelDailyCostRedisStream, ChannelDailyCostRedisConsumerGroup).Result(); err != nil {
			input.ObservationComplete = false
		}
		cancel()
	}
	if input.ObservationComplete && input.EventOutboxPending == 0 && input.Realtime.WriterQueueDepth == 0 {
		input.Realtime.DegradedReasons = slices.DeleteFunc(input.Realtime.DegradedReasons, func(reason string) bool { return reason == ChannelMonitorRedisDegradedReasonPublisherUnavailable })
	}
	return input
}
