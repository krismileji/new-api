package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
)

func TestChannelRateLimitCooldownAppliesOnlyToMatchingModelAndPreservesExclusions(t *testing.T) {
	ClearChannelRateLimitCooldowns()
	t.Cleanup(ClearChannelRateLimitCooldowns)

	StartChannelRateLimitCooldown(12, "model-a", 30)
	StartChannelRateLimitCooldown(13, "model-b", 30)

	options := applyChannelRateLimitCooldowns("model-a", model.ChannelSelectionOptions{
		ExcludedChannelIds: []int{14, 12},
	})
	assert.Equal(t, []int{12, 14}, options.ExcludedChannelIds)

	otherModelOptions := applyChannelRateLimitCooldowns("model-c", model.ChannelSelectionOptions{
		ExcludedChannelIds: []int{14},
	})
	assert.Equal(t, []int{14}, otherModelOptions.ExcludedChannelIds)
}

func TestChannelRateLimitCooldownExpiresAndCannotBeShortened(t *testing.T) {
	ClearChannelRateLimitCooldowns()
	t.Cleanup(ClearChannelRateLimitCooldowns)

	StartChannelRateLimitCooldown(21, "model-a", 60)
	firstUntil := ChannelRateLimitCooldownUntil(21, "model-a")
	StartChannelRateLimitCooldown(21, "model-a", 10)
	assert.Equal(t, firstUntil, ChannelRateLimitCooldownUntil(21, "model-a"))

	assert.Empty(t, channelRateLimitCooldownChannelIds("model-a", common.GetTimestamp()+61))
	assert.Zero(t, ChannelRateLimitCooldownUntil(21, "model-a"))
}
