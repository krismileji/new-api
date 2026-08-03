package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChooseChannelByWeightsTreatsZeroAsExplicitExclusion(t *testing.T) {
	channelId, err := chooseChannelByWeights([]int{1, 2}, []uint{0, 2})
	require.NoError(t, err)
	assert.Equal(t, 2, channelId)
}

func TestChooseChannelByWeightsFallsBackToEqualSelectionForAllZeroLayer(t *testing.T) {
	channelId, err := chooseChannelByWeights([]int{1, 2}, []uint{0, 0})
	require.NoError(t, err)
	assert.Contains(t, []int{1, 2}, channelId)
}
