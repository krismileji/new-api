package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/model"
)

type channelGroupMonitorCachePolicy struct {
	Revision    int64
	MinContextK int
	Generation  string
	StartedAt   int64
}

func init() {
	model.ChannelGroupMonitorExecutionEvent = func(row model.ChannelGroupMonitorExecution) *model.ChannelMonitorEvent {
		policy := channelGroupMonitorCachePolicyState.Load()
		if policy == nil || policy.Generation == "" || row.ConfigRevision != policy.Revision || row.StartedAt < policy.StartedAt {
			return nil
		}
		probe := model.ChannelGroupMonitorExecution{Id: row.Id, RunId: row.RunId, GroupName: row.GroupName, ConfigRevision: row.ConfigRevision,
			ProbeModel: row.ProbeModel, Result: row.Result, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
			FirstTokenMs: row.FirstTokenMs, TPS: row.TPS, ResponseTimeMs: row.ResponseTimeMs}
		event := model.NewChannelMonitorEvent(0, model.ChannelMonitorEventSourceGroupSummary, model.ChannelMonitorEventOutcomeSuccess, row.FinishedAt)
		event.EventId = fmt.Sprintf("group:%s:%d", policy.Generation, row.Id)
		event.GroupName, event.GroupMonitorGeneration, event.GroupMonitorProbe = row.GroupName, policy.Generation, &probe
		return &event
	}
}

var channelGroupMonitorCachePolicyState atomic.Pointer[channelGroupMonitorCachePolicy]
var channelGroupMonitorCachePolicyMu sync.Mutex

// UpdateChannelGroupMonitorCachePolicy publishes committed configuration without
// adding database or Redis IO to the request path. A late refresh cannot undo a save.
func UpdateChannelGroupMonitorCachePolicy(config model.ChannelGroupMonitorConfig) error {
	channelGroupMonitorCachePolicyMu.Lock()
	defer channelGroupMonitorCachePolicyMu.Unlock()
	if previous := channelGroupMonitorCachePolicyState.Load(); previous != nil && previous.Revision > config.Revision {
		return nil
	}
	minimum, err := config.CacheMinContextK()
	if err != nil {
		return err
	}
	next := &channelGroupMonitorCachePolicy{Revision: config.Revision, MinContextK: minimum}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	generation, syncErr := SyncChannelGroupMonitorGeneration(ctx, config)
	if syncErr == nil && generation.Revision == config.Revision {
		next.Generation, next.StartedAt = generation.ID, generation.StartedAt
	}
	for {
		previous := channelGroupMonitorCachePolicyState.Load()
		if previous != nil && previous.Revision > next.Revision {
			return nil
		}
		if channelGroupMonitorCachePolicyState.CompareAndSwap(previous, next) {
			return syncErr
		}
	}
}

func refreshChannelGroupMonitorCachePolicy(ctx context.Context) error {
	if model.DB == nil {
		return errors.New("分组缓存率配置数据库不可用")
	}
	config, err := model.GetChannelGroupMonitorConfigOrDefaultWithContext(ctx)
	if err != nil {
		return err
	}
	return UpdateChannelGroupMonitorCachePolicy(config)
}

// Freeze the decision before serialization. Outbox retries and stream replay
// retain it; events written by older versions keep the original unfiltered rule.
func captureChannelGroupMonitorCachePolicy(event *model.ChannelMonitorEvent) {
	if event.GroupCacheExcluded != nil || event.Source != model.ChannelMonitorEventSourceBusiness {
		return
	}
	policy := channelGroupMonitorCachePolicyState.Load()
	if policy != nil {
		event.GroupMonitorGeneration = policy.Generation
	}
	excluded := policy == nil || !event.IsStream // Do not invent samples before configuration is loaded.
	if policy != nil && policy.MinContextK > 0 {
		input := event.InputTokens
		if input == nil {
			input = event.PromptTokens
		}
		excluded = !event.IsStream || input == nil || *input < int64(policy.MinContextK)*1000
	}
	event.GroupCacheExcluded = &excluded
}
