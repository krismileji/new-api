package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAllChannelsKeepsEnabledChannelsBeforeSortAndPagination(t *testing.T) {
	truncateTables(t)

	priority100 := int64(100)
	priority200 := int64(200)
	priority300 := int64(300)
	priority400 := int64(400)
	channels := []*Channel{
		{
			Id: 1, Name: "auto-disabled-high-priority", Key: "key-1",
			Status: common.ChannelStatusAutoDisabled, Priority: &priority300,
		},
		{
			Id: 2, Name: "enabled-low-priority", Key: "key-2",
			Status: common.ChannelStatusEnabled, Priority: &priority100,
		},
		{
			Id: 3, Name: "enabled-high-priority", Key: "key-3",
			Status: common.ChannelStatusEnabled, Priority: &priority200,
		},
		{
			Id: 4, Name: "manual-disabled-top-priority", Key: "key-4",
			Status: common.ChannelStatusManuallyDisabled, Priority: &priority400,
		},
	}
	require.NoError(t, DB.Create(&channels).Error)

	pageOne, err := GetAllChannels(0, 2, false, false, NewChannelSortOptions("priority", "desc", false))
	require.NoError(t, err)
	pageTwo, err := GetAllChannels(2, 2, false, false, NewChannelSortOptions("priority", "desc", false))
	require.NoError(t, err)

	assert.Equal(t, []int{3, 2}, collectChannelIDs(pageOne))
	assert.Equal(t, []int{4, 1}, collectChannelIDs(pageTwo))
}

func collectChannelIDs(channels []*Channel) []int {
	ids := make([]int, 0, len(channels))
	for _, channel := range channels {
		ids = append(ids, channel.Id)
	}
	return ids
}
