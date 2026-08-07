package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveChannelSmartScheduleGroupPauseAppliesToEveryModelInOnlyOneGroup(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(80)
	weight := uint(100)
	channel := Channel{
		Id: 2601, Name: "group pause", Status: common.ChannelStatusEnabled,
		Group: "vip,standard", Models: "model-a,model-b", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: channel.Id, Group: "vip", Model: "model-b", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: channel.Id, Group: "standard", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{ChannelId: channel.Id, GroupName: "vip", ModelName: "model-b", ParticipationSet: true, Revision: 2},
		{ChannelId: channel.Id, GroupName: "standard", ModelName: "model-a", ParticipationSet: true, Revision: 3},
	}).Error)

	before := common.GetTimestamp()
	paused, err := SaveChannelSmartScheduleGroupPause(channel.Id, "vip", 30)
	require.NoError(t, err)
	assert.True(t, paused.Changed)
	assert.Equal(t, 2, paused.AffectedRoutes)
	assert.GreaterOrEqual(t, paused.PausedUntil, before+30*60)

	routes, err := GetChannelSmartScheduleRouteSummaries()
	require.NoError(t, err)
	require.Len(t, routes, 3)
	for _, route := range routes {
		if route.Group == "vip" {
			assert.Equal(t, paused.PausedUntil, route.TrafficPausedUntil)
			continue
		}
		assert.Zero(t, route.TrafficPausedUntil)
	}

	var states []ChannelSmartScheduleRouteState
	require.NoError(t, db.Order("group_name ASC, model_name ASC").Find(&states).Error)
	revisions := make(map[ChannelSmartScheduleRouteKey]int64, len(states))
	for _, state := range states {
		revisions[channelSmartScheduleRouteKey(state.ChannelId, state.GroupName, state.ModelName)] = state.Revision
	}
	assert.Equal(t, int64(3), revisions[channelSmartScheduleRouteKey(channel.Id, "standard", "model-a")])
	assert.Equal(t, int64(2), revisions[channelSmartScheduleRouteKey(channel.Id, "vip", "model-a")])
	assert.Equal(t, int64(3), revisions[channelSmartScheduleRouteKey(channel.Id, "vip", "model-b")])

	resumed, err := SaveChannelSmartScheduleGroupPause(channel.Id, "vip", 0)
	require.NoError(t, err)
	assert.True(t, resumed.Changed)
	assert.Zero(t, resumed.PausedUntil)
	var pauseCount int64
	require.NoError(t, db.Model(&ChannelSmartScheduleGroupPause{}).
		Where("channel_id = ? AND group_name = ?", channel.Id, "vip").
		Count(&pauseCount).Error)
	assert.Zero(t, pauseCount)
}

func TestChannelSmartScheduleGroupPauseBlocksSelectionAndAffinity(t *testing.T) {
	for _, memoryCacheEnabled := range []bool{false, true} {
		name := "database"
		if memoryCacheEnabled {
			name = "memory_cache"
		}
		t.Run(name, func(t *testing.T) {
			db := setupChannelSmartScheduleRouteTestDB(t)
			originalMemoryCacheEnabled := common.MemoryCacheEnabled
			common.MemoryCacheEnabled = memoryCacheEnabled
			channelSyncLock.Lock()
			originalGroupRoutes := group2model2channels
			originalChannels := channelsIDM
			originalAdvancedConfigs := channel2advancedCustomConfig
			originalSmartRoutes := channelSmartScheduleRouteCache
			channelSyncLock.Unlock()
			t.Cleanup(func() {
				common.MemoryCacheEnabled = originalMemoryCacheEnabled
				channelSyncLock.Lock()
				group2model2channels = originalGroupRoutes
				channelsIDM = originalChannels
				channel2advancedCustomConfig = originalAdvancedConfigs
				channelSmartScheduleRouteCache = originalSmartRoutes
				channelSyncLock.Unlock()
			})

			highPriority := int64(100)
			lowPriority := int64(80)
			require.NoError(t, db.Create(&[]Channel{
				{Id: 2611, Name: "paused primary", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a"},
				{Id: 2612, Name: "active backup", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a"},
			}).Error)
			require.NoError(t, db.Create(&[]Ability{
				{ChannelId: 2611, Group: "vip", Model: "model-a", Enabled: true, Priority: &highPriority, Weight: 100},
				{ChannelId: 2612, Group: "vip", Model: "model-a", Enabled: true, Priority: &lowPriority, Weight: 100},
			}).Error)
			require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
				{ChannelId: 2611, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
				{ChannelId: 2612, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
			}).Error)

			_, err := SaveChannelSmartScheduleGroupPause(2611, "vip", 60)
			require.NoError(t, err)
			if memoryCacheEnabled {
				InitChannelCache()
			}

			selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, "")
			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, 2612, selected.Id)
			assert.Equal(t, ChannelSmartScheduleAffinityTemporarilyUnavailable,
				ChannelSmartScheduleAffinityEligibility("vip", "model-a", 2611, "/v1/chat/completions"))
			assert.Equal(t, ChannelSmartScheduleAffinityEligible,
				ChannelSmartScheduleAffinityEligibility("vip", "model-a", 2612, "/v1/chat/completions"))

			_, err = SaveChannelSmartScheduleGroupPause(2611, "vip", 0)
			require.NoError(t, err)
			if memoryCacheEnabled {
				InitChannelCache()
			}
			selected, err = GetRandomSatisfiedChannel("vip", "model-a", 0, "")
			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, 2611, selected.Id)
		})
	}
}

func TestExpiredChannelSmartScheduleGroupPauseDoesNotBlockRouting(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	originalGroupRoutes := group2model2channels
	originalChannels := channelsIDM
	originalAdvancedConfigs := channel2advancedCustomConfig
	originalSmartRoutes := channelSmartScheduleRouteCache
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		group2model2channels = originalGroupRoutes
		channelsIDM = originalChannels
		channel2advancedCustomConfig = originalAdvancedConfigs
		channelSmartScheduleRouteCache = originalSmartRoutes
		channelSyncLock.Unlock()
	})

	priority := int64(100)
	require.NoError(t, db.Create(&Channel{
		Id: 2621, Name: "expired pause", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a",
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 2621, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: 100,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 2621, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleGroupPause{
		ChannelId: 2621, GroupName: "vip", PausedUntil: common.GetTimestamp() - 1,
	}).Error)

	InitChannelCache()
	selected, err := GetRandomSatisfiedChannel("vip", "model-a", 0, "")
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 2621, selected.Id)
}
