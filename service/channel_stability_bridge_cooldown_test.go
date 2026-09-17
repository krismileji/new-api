package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelStabilityBridgePreservesCooldownSources(t *testing.T) {
	for _, storage := range []string{"local", "redis"} {
		t.Run(storage, func(t *testing.T) {
			if storage == "redis" {
				useChannelRateLimitCooldownRedis(t)
			} else {
				originalRedis := common.RedisEnabled
				common.RedisEnabled = false
				t.Cleanup(func() { common.RedisEnabled = originalRedis })
				ClearChannelRateLimitCooldowns()
				t.Cleanup(ClearChannelRateLimitCooldowns)
			}
			setChannelRateLimitCooldownControlRevision(t, "cooldown-sources")
			ctx := context.Background()
			now := common.GetTimestamp()
			bridgeUntil := now + 90
			accepted, err := StartChannelStabilityBridgeCooldownUntilIfControlRevision(
				ctx, 71, "model-a", bridgeUntil, "cooldown-sources", 0,
			)
			require.NoError(t, err)
			require.True(t, accepted)
			assert.Equal(t, []int{71}, applyChannelRateLimitCooldowns(
				"model-a", model.ChannelSelectionOptions{},
			).ExcludedChannelIds)
			for _, routeModel := range []string{"model-a", "model-*"} {
				assert.Equal(t, bridgeUntil, ChannelRateLimitCooldownUntilMatching(71, routeModel))
				assert.Zero(t, ChannelUpstreamRateLimitCooldownUntilMatching(71, routeModel))
				if storage == "redis" {
					sharedUntil, readErr := ChannelRateLimitCooldownUntilMatchingFromRedis(ctx, 71, routeModel)
					require.NoError(t, readErr)
					assert.Equal(t, bridgeUntil, sharedUntil)
				}
			}

			// A real 429 can overlap with a longer stability bridge without inheriting its deadline.
			require.True(t, StartChannelRateLimitCooldownIfControlRevision(71, "model-a", 30, "cooldown-sources"))
			rateLimitUntil := ChannelUpstreamRateLimitCooldownUntilMatching(71, "model-a")
			assert.Greater(t, rateLimitUntil, now)
			assert.Less(t, rateLimitUntil, bridgeUntil)
			accepted, err = StartChannelStabilityBridgeCooldownUntilIfControlRevision(
				ctx, 71, "model-a", bridgeUntil+30, "cooldown-sources", 0,
			)
			require.NoError(t, err)
			require.True(t, accepted)
			assert.Equal(t, rateLimitUntil, ChannelUpstreamRateLimitCooldownUntilMatching(71, "model-a"))

			if storage == "redis" {
				// Restore the process cache solely from shared Redis, as another gateway instance would.
				resetChannelRateLimitCooldownLocalState()
				syncChannelRateLimitCooldownsFromRedis(ctx, common.RDB)
			}
			for _, routeModel := range []string{"model-a", "model-*"} {
				assert.Equal(t, bridgeUntil+30, ChannelRateLimitCooldownUntilMatching(71, routeModel))
				assert.Equal(t, rateLimitUntil, ChannelUpstreamRateLimitCooldownUntilMatching(71, routeModel))
			}

			// A pause must clear both sources and prevent either from being restored by a late event.
			_, err = UpdateChannelRateLimitBypass(ctx, 71, "model-*", 60)
			require.NoError(t, err)
			t.Cleanup(ClearChannelRateLimitBypasses)
			accepted, err = StartChannelStabilityBridgeCooldownUntilIfControlRevision(
				ctx, 71, "model-a", bridgeUntil+60, "cooldown-sources", 0,
			)
			require.NoError(t, err)
			assert.False(t, accepted)
			assert.False(t, StartChannelRateLimitCooldownIfControlRevision(71, "model-a", 30, "cooldown-sources"))
			_, err = UpdateChannelRateLimitBypass(ctx, 71, "model-*", 0)
			require.NoError(t, err)
			if storage == "redis" {
				resetChannelRateLimitCooldownLocalState()
				syncChannelRateLimitCooldownsFromRedis(ctx, common.RDB)
			}
			assert.Zero(t, ChannelRateLimitCooldownUntilMatching(71, "model-a"))
			assert.Zero(t, ChannelUpstreamRateLimitCooldownUntilMatching(71, "model-*"))
		})
	}
}

func TestChannelStabilityBridgeRedisEventsDoNotSuppress429Events(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)
	setChannelRateLimitCooldownControlRevision(t, "cooldown-event-sources")
	ctx := context.Background()
	now := common.GetTimestamp()
	accepted, err := StartChannelStabilityBridgeCooldownUntilIfControlRevision(
		ctx, 72, "model-a", now+90, "cooldown-event-sources", 20,
	)
	require.NoError(t, err)
	require.True(t, accepted)
	// Replaying an earlier 429 must still preserve its own deadline and event order.
	accepted, err = StartChannelRateLimitCooldownUntilIfControlRevision(
		ctx, 72, "model-a", now+30, "cooldown-event-sources", 19,
	)
	require.NoError(t, err)
	require.True(t, accepted)
	assert.Equal(t, now+30, ChannelUpstreamRateLimitCooldownUntilMatching(72, "model-a"))
	accepted, err = StartChannelStabilityBridgeCooldownUntilIfControlRevision(
		ctx, 72, "model-a", now+120, "cooldown-event-sources", 20,
	)
	require.NoError(t, err)
	require.True(t, accepted)
	assert.Equal(t, now+90, ChannelRateLimitCooldownUntilMatching(72, "model-a"))
	assert.Equal(t, now+30, ChannelUpstreamRateLimitCooldownUntilMatching(72, "model-a"))

	result, err := ClearChannelRateLimitCooldownRoute(ctx, 72, "model-*")
	require.NoError(t, err)
	assert.True(t, result.Changed)
	resetChannelRateLimitCooldownLocalState()
	syncChannelRateLimitCooldownsFromRedis(ctx, common.RDB)
	assert.Zero(t, ChannelRateLimitCooldownUntilMatching(72, "model-a"))
	assert.Zero(t, ChannelUpstreamRateLimitCooldownUntilMatching(72, "model-*"))
}
