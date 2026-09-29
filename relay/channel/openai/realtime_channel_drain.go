package openai

import (
	"slices"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/pkg/wsmanager"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"
)

// Realtime can have both client-created and server-VAD-created responses.
// Keep response IDs until their terminal event has been billed and forwarded.
type realtimeChannelDrain struct {
	mu      sync.Mutex
	reason  string
	pending []string
	active  map[string]struct{}
	close   func(string)
}

func newRealtimeChannelDrain(info *relaycommon.RelayInfo) (*realtimeChannelDrain, func()) {
	var once sync.Once
	d := &realtimeChannelDrain{active: make(map[string]struct{})}
	d.close = func(reason string) {
		once.Do(func() {
			payload := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, reason)
			deadline := time.Now().Add(time.Second)
			_ = info.ClientWs.WriteControl(websocket.CloseMessage, payload, deadline)
			_ = info.TargetWs.WriteControl(websocket.CloseMessage, payload, deadline)
			_ = info.ClientWs.Close()
			_ = info.TargetWs.Close()
		})
	}
	channelID := 0
	if info.ChannelMeta != nil {
		channelID = info.ChannelId
	}
	unregister := wsmanager.RegisterChannelDrain(channelID, d.drain)
	service.EnforceChannelWebSocketStatus(channelID, d.drain, d.close)
	return d, unregister
}

func (d *realtimeChannelDrain) drain(reason string) {
	d.mu.Lock()
	if d.reason == "" {
		d.reason = reason
	}
	idle := len(d.pending) == 0 && len(d.active) == 0
	d.mu.Unlock()
	if idle {
		d.close(reason)
	}
}

func (d *realtimeChannelDrain) admit(eventType, eventID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.reason != "" {
		// Controls may finish the current response; new audio and session
		// changes must not start another automatic VAD generation.
		return eventType == "response.cancel" || eventType == "conversation.item.truncate"
	}
	if eventType == "response.create" {
		d.pending = append(d.pending, eventID)
	}
	return true
}

func (d *realtimeChannelDrain) observeStart(message []byte) {
	if gjson.GetBytes(message, "type").String() != "response.created" {
		return
	}
	id := gjson.GetBytes(message, "response.id").String()
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.active[id]; exists {
		return
	}
	if len(d.pending) > 0 {
		d.pending = d.pending[1:]
	}
	d.active[id] = struct{}{}
}

func (d *realtimeChannelDrain) observeFinished(message []byte) {
	eventType := gjson.GetBytes(message, "type").String()
	if eventType != "response.done" && eventType != "error" {
		return
	}
	d.mu.Lock()
	if eventType == "response.done" {
		id := gjson.GetBytes(message, "response.id").String()
		if _, exists := d.active[id]; exists {
			delete(d.active, id)
		}
	} else {
		eventID := gjson.GetBytes(message, "error.event_id").String()
		if index := slices.Index(d.pending, eventID); index >= 0 {
			d.pending = slices.Delete(d.pending, index, index+1)
		}
	}
	reason := d.reason
	idle := len(d.pending) == 0 && len(d.active) == 0
	d.mu.Unlock()
	if reason != "" && idle {
		d.close(reason)
	}
}
