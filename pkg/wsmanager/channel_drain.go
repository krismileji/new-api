package wsmanager

import (
	"context"
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// Draining has a separate registry so an immediate administrative close can
// still reach a connection that is finishing its current generation.
var channelDrains = struct {
	sync.Mutex
	next    uint64
	entries map[int]map[uint64]func(string)
}{entries: make(map[int]map[uint64]func(string))}

func RegisterChannelDrain(channelID int, drain func(string)) func() {
	if channelID <= 0 || drain == nil {
		return func() {}
	}
	channelDrains.Lock()
	channelDrains.next++
	id := channelDrains.next
	if channelDrains.entries[channelID] == nil {
		channelDrains.entries[channelID] = make(map[uint64]func(string))
	}
	channelDrains.entries[channelID][id] = drain
	channelDrains.Unlock()
	return func() {
		channelDrains.Lock()
		defer channelDrains.Unlock()
		delete(channelDrains.entries[channelID], id)
		if len(channelDrains.entries[channelID]) == 0 {
			delete(channelDrains.entries, channelID)
		}
	}
}

func DrainChannels(channelIDs []int, reason string) int {
	var callbacks []func(string)
	channelDrains.Lock()
	for _, channelID := range uniqueChannelIDs(channelIDs) {
		for _, drain := range channelDrains.entries[channelID] {
			callbacks = append(callbacks, drain)
		}
	}
	channelDrains.Unlock()
	for _, drain := range callbacks {
		drain(normalizeReason(reason))
	}
	return len(callbacks)
}

func DrainChannelsAndBroadcast(channelIDs []int, reason string) int {
	count := DrainChannels(channelIDs, reason)
	if err := publishChannelEvent(context.Background(), channelIDs, reason, true); err != nil {
		common.SysLog(fmt.Sprintf("failed to publish websocket drain event: %v", err))
	}
	return count
}
