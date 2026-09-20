package service

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/QuantumNous/new-api/model"
)

type channelGroupMonitorCachePolicy struct {
	Revision    int64
	MinContextK int
}

var channelGroupMonitorCachePolicyState atomic.Pointer[channelGroupMonitorCachePolicy]

// UpdateChannelGroupMonitorCachePolicy publishes committed configuration without
// adding database or Redis IO to the request path. A late refresh cannot undo a save.
func UpdateChannelGroupMonitorCachePolicy(config model.ChannelGroupMonitorConfig) error {
	minimum, err := config.CacheMinContextK()
	if err != nil {
		return err
	}
	next := &channelGroupMonitorCachePolicy{Revision: config.Revision, MinContextK: minimum}
	for {
		previous := channelGroupMonitorCachePolicyState.Load()
		if previous != nil && previous.Revision > next.Revision {
			return nil
		}
		if channelGroupMonitorCachePolicyState.CompareAndSwap(previous, next) {
			return nil
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
	excluded := policy == nil // Do not invent samples before configuration is loaded.
	if policy != nil && policy.MinContextK > 0 {
		input := event.InputTokens
		if input == nil {
			input = event.PromptTokens
		}
		excluded = !event.IsStream || input == nil || *input < int64(policy.MinContextK)*1000
	}
	event.GroupCacheExcluded = &excluded
}
