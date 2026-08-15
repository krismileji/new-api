package service

import (
	"context"
	"sync/atomic"

	"github.com/QuantumNous/new-api/model"
)

// ChannelMonitorEventProjectedHandler is called after a batch has been
// committed to the shared realtime projection. Implementations must return
// quickly and hand slower work to their own bounded background worker.
type ChannelMonitorEventProjectedHandler func([]model.ChannelMonitorEvent)

type channelMonitorEventProjectedHandlerHolder struct {
	handle ChannelMonitorEventProjectedHandler
}

var channelMonitorEventProjectedHandler atomic.Pointer[channelMonitorEventProjectedHandlerHolder]

func init() {
	SetChannelMonitorEventConsumer(consumeChannelMonitorEventProjectionBatch)
}

func consumeChannelMonitorEventProjectionBatch(
	ctx context.Context,
	events []model.ChannelMonitorEvent,
) error {
	if err := consumeChannelMonitorEventBatch(ctx, events); err != nil {
		return err
	}
	handler := channelMonitorEventProjectedHandler.Load()
	if handler != nil {
		handler.handle(events)
	}
	return nil
}

// RegisterChannelMonitorEventProjectedHandler installs the downstream hook
// that reacts to already-projected channel monitor events.
func RegisterChannelMonitorEventProjectedHandler(handle ChannelMonitorEventProjectedHandler) bool {
	if handle == nil {
		return false
	}
	channelMonitorEventProjectedHandler.Store(&channelMonitorEventProjectedHandlerHolder{handle: handle})
	return true
}
