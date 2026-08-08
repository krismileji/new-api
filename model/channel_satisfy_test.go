package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
)

func TestIsChannelEnabledForGroupModelHonorsRuntimeChannelStatus(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalRoutes := channelSmartScheduleRouteCache
	originalChannels := channelsIDM
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSmartScheduleRouteCache = originalRoutes
		channelsIDM = originalChannels
	})

	common.MemoryCacheEnabled = true
	channelSmartScheduleRouteCache = map[string]map[string][]channelSmartScheduleCachedRoute{
		"default": {
			"model-a": {{channelId: 1}},
		},
	}
	channelsIDM = map[int]*Channel{
		1: {Id: 1, Status: common.ChannelStatusAutoDisabled},
	}

	assert.False(t, IsChannelEnabledForGroupModel("default", "model-a", 1))

	channelsIDM[1].Status = common.ChannelStatusEnabled
	assert.True(t, IsChannelEnabledForGroupModel("default", "model-a", 1))
}
