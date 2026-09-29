package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealtimeDrainWaitsForAllAdmittedResponses(t *testing.T) {
	var closed []string
	d := &realtimeChannelDrain{active: make(map[string]struct{}), close: func(reason string) { closed = append(closed, reason) }}
	require.True(t, d.admit("response.create", "create-1"))
	d.observeStart([]byte(`{"type":"response.created","response":{"id":"r1"}}`))
	require.True(t, d.admit("response.create", "create-2"))
	d.drain("disabled")
	assert.Empty(t, closed)
	assert.False(t, d.admit("response.create", "create-3"))
	assert.False(t, d.admit("input_audio_buffer.append", "audio-1"))
	assert.False(t, d.admit("session.update", "settings-1"))
	assert.True(t, d.admit("response.cancel", "cancel-1"))
	assert.True(t, d.admit("conversation.item.truncate", "truncate-1"))
	d.observeFinished([]byte(`{"type":"response.done","response":{"id":"r1","status":"cancelled"}}`))
	d.observeFinished([]byte(`{"type":"response.done","response":{"id":"r1","status":"cancelled"}}`))
	assert.Empty(t, closed, "duplicate terminal must not consume an unrelated pending generation")
	d.observeFinished([]byte(`{"type":"error","error":{"event_id":"cancel-1"}}`))
	assert.Empty(t, closed, "a control error must not consume a pending create")
	d.observeFinished([]byte(`{"type":"error","error":{"event_id":"create-2"}}`))
	assert.Equal(t, []string{"disabled"}, closed)
}

func TestRealtimeDrainTracksServerVADAndIdle(t *testing.T) {
	for _, vad := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "server-vad"}[vad], func(t *testing.T) {
			var closed bool
			d := &realtimeChannelDrain{active: make(map[string]struct{}), close: func(string) { closed = true }}
			if vad {
				d.observeStart([]byte(`{"type":"response.created","response":{"id":"vad"}}`))
			}
			d.drain("disabled")
			assert.Equal(t, !vad, closed)
			if vad {
				d.observeFinished([]byte(`{"type":"response.done","response":{"id":"vad","status":"completed"}}`))
				assert.True(t, closed)
			}
		})
	}
}
