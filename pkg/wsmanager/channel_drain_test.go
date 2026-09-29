package wsmanager

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelDrainKeepsImmediateCloseAndUnregisters(t *testing.T) {
	var drained, closed int
	unregister := Register(710, KindResponses, func(string) { closed++ })
	t.Cleanup(unregister)
	unregisterDrain := RegisterChannelDrain(710, func(reason string) {
		assert.Equal(t, "drain", reason)
		drained++
		// Registration is safe from inside a callback (no registry lock held).
		RegisterChannelDrain(711, func(string) {})()
	})
	t.Cleanup(unregisterDrain)
	assert.Equal(t, 1, DrainChannels([]int{710, 710, -1}, "drain"))
	assert.Equal(t, 1, drained)
	assert.Zero(t, closed)
	assert.Equal(t, 1, CloseChannel(710, "manual"))
	assert.Equal(t, 1, closed)
	unregisterDrain()
	unregisterDrain()
	assert.Zero(t, DrainChannels([]int{710}, "drain"))
}

func TestRedisChannelDrainKeepsPolicyAndDatabaseIsolation(t *testing.T) {
	previousEnabled, previousClient := common.RedisEnabled, common.RDB
	t.Cleanup(func() { common.RedisEnabled, common.RDB = previousEnabled, previousClient })
	address := os.Getenv("TEST_WS_MANAGER_REDIS_ADDR")
	if address == "" {
		address = miniredis.RunT(t).Addr()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	local := redis.NewClient(&redis.Options{Addr: address, DB: 0})
	other := redis.NewClient(&redis.Options{Addr: address, DB: 1})
	for _, client := range []*redis.Client{local, other} {
		require.NoError(t, client.Ping(ctx).Err())
		t.Cleanup(func() { require.NoError(t, client.Close()) })
	}
	pubsub := local.Subscribe(ctx, channelCloseTopic(0))
	t.Cleanup(func() { require.NoError(t, pubsub.Close()) })
	_, err := pubsub.Receive(ctx)
	require.NoError(t, err)
	otherSub := other.Subscribe(ctx, channelCloseTopic(1))
	t.Cleanup(func() { require.NoError(t, otherSub.Close()) })
	_, err = otherSub.Receive(ctx)
	require.NoError(t, err)
	drained, closed := make(chan string, 3), make(chan string, 3)
	t.Cleanup(RegisterChannelDrain(720, func(reason string) { drained <- reason }))
	t.Cleanup(Register(720, KindResponses, func(reason string) { closed <- reason }))
	done := make(chan struct{})
	go func() { defer close(done); receiveChannelCloseEvents(ctx, pubsub.Channel(), "remote-node") }()
	t.Cleanup(func() { cancel(); <-done })
	common.RedisEnabled, common.RDB = true, other
	require.NoError(t, publishChannelEvent(ctx, []int{720}, "other database", true))
	message, err := otherSub.ReceiveMessage(ctx)
	require.NoError(t, err)
	var event closeEvent
	require.NoError(t, common.Unmarshal([]byte(message.Payload), &event))
	assert.True(t, event.Drain)
	common.RDB = local
	require.NoError(t, publishChannelEvent(ctx, []int{720}, "finish current", true))
	select {
	case reason := <-drained:
		assert.Equal(t, "finish current", reason)
	case <-ctx.Done():
		t.Fatal("drain broadcast was not delivered")
	}
	require.NoError(t, PublishCloseChannels(ctx, []int{720}, "manual close"))
	select {
	case reason := <-closed:
		assert.Equal(t, "manual close", reason, "drain must not consume the immediate-close registration")
	case <-ctx.Done():
		t.Fatal("immediate broadcast was not delivered")
	}
	assert.Empty(t, drained, "other logical database must not drain this connection")
}
