package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelSmartScheduleRuntimeRouteCachePreservesEffectiveRouteSemantics(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	originalGroupCache := group2model2channels
	originalChannelCache := channelsIDM
	originalAdvancedCustomCache := channel2advancedCustomConfig
	originalRouteCache := channelSmartScheduleRouteCache
	originalRuntimeRouteIndex := channelSmartScheduleRuntimeRouteIndexCache.Load()
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		group2model2channels = originalGroupCache
		channelsIDM = originalChannelCache
		channel2advancedCustomConfig = originalAdvancedCustomCache
		channelSmartScheduleRouteCache = originalRouteCache
		channelSmartScheduleRuntimeRouteIndexCache.Store(originalRuntimeRouteIndex)
		channelSyncLock.Unlock()
	})

	const (
		channelId     = 7801
		exactModel    = "gemini-2.5-pro-thinking-2048"
		wildcardModel = "gemini-2.5-pro-thinking-*"
	)
	priority := int64(100)
	require.NoError(t, db.Create(&Channel{
		Id: channelId, Name: "runtime cache", Status: common.ChannelStatusEnabled,
		Group: "exact,wildcard,inactive-exact", Models: exactModel + "," + wildcardModel,
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: channelId, Group: "exact", Model: exactModel, Enabled: true, Priority: &priority, Weight: 100},
		{ChannelId: channelId, Group: "exact", Model: wildcardModel, Enabled: true, Priority: &priority, Weight: 100},
		{ChannelId: channelId, Group: "wildcard", Model: wildcardModel, Enabled: true, Priority: &priority, Weight: 100},
		{ChannelId: channelId, Group: "inactive-exact", Model: exactModel, Enabled: true, Priority: &priority, Weight: 100},
		{ChannelId: channelId, Group: "inactive-exact", Model: wildcardModel, Enabled: true, Priority: &priority, Weight: 100},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: channelId, GroupName: "exact", ModelName: exactModel, ParticipationSet: true,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficExploration, TemporaryTrafficSince: 101,
			StabilityState: ChannelSmartScheduleStabilityProbing, StabilitySince: 105,
		},
		{
			ChannelId: channelId, GroupName: "exact", ModelName: wildcardModel, ParticipationSet: true,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficAdaptive, TemporaryTrafficSince: 102,
		},
		{
			ChannelId: channelId, GroupName: "wildcard", ModelName: wildcardModel, ParticipationSet: true,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficAdaptive, TemporaryTrafficSince: 103,
		},
		{
			ChannelId: channelId, GroupName: "inactive-exact", ModelName: wildcardModel, ParticipationSet: true,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficAdaptive, TemporaryTrafficSince: 104,
		},
	}).Error)
	InitChannelCache()

	queryCount := 0
	callbackName := "test:count_runtime_route_cache_queries"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(*gorm.DB) {
		queryCount++
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	participating, err := GetChannelSmartScheduleRuntimeParticipatingRoutes(channelId, exactModel)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"exact":    exactModel,
		"wildcard": wildcardModel,
	}, participating)

	routes, err := GetChannelSmartScheduleRuntimeRoutes(channelId, exactModel)
	require.NoError(t, err)
	assert.Equal(t, map[string]ChannelSmartScheduleRuntimeRoute{
		"exact": {
			ModelName: exactModel, SampleSince: 105,
			StabilityState:       ChannelSmartScheduleStabilityProbing,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficExploration,
		},
		"wildcard": {
			ModelName: wildcardModel, SampleSince: 103,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficAdaptive,
		},
	}, routes)
	assert.Zero(t, queryCount)
	participates, cacheEnabled := CachedChannelSmartScheduleRuntimeParticipates(channelId, exactModel)
	assert.True(t, cacheEnabled)
	assert.True(t, participates)

	delete(participating, "exact")
	routes["exact"] = ChannelSmartScheduleRuntimeRoute{ModelName: "mutated"}
	participatingAgain, err := GetChannelSmartScheduleRuntimeParticipatingRoutes(channelId, exactModel)
	require.NoError(t, err)
	routesAgain, err := GetChannelSmartScheduleRuntimeRoutes(channelId, exactModel)
	require.NoError(t, err)
	assert.Equal(t, exactModel, participatingAgain["exact"])
	assert.Equal(t, exactModel, routesAgain["exact"].ModelName)
	assert.Zero(t, queryCount)

	common.MemoryCacheEnabled = false
	fromDatabase, err := GetChannelSmartScheduleRuntimeRoutes(channelId, exactModel)
	require.NoError(t, err)
	assert.Equal(t, routesAgain, fromDatabase)
	assert.Positive(t, queryCount)
}

func TestCachedChannelSmartScheduleRuntimeParticipatesPreservesExactModelPrecedence(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalRuntimeRouteIndex := channelSmartScheduleRuntimeRouteIndexCache.Load()
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSmartScheduleRuntimeRouteIndexCache.Store(originalRuntimeRouteIndex)
	})

	const (
		exactModel    = "gemini-2.5-pro-thinking-2048"
		wildcardModel = "gemini-2.5-pro-thinking-*"
	)
	publishChannelSmartScheduleRuntimeRouteIndex(buildChannelSmartScheduleRuntimeRouteIndex(
		map[string]map[string][]channelSmartScheduleCachedRoute{
			"vip": {
				exactModel:    {{channelId: 7803, participates: false}},
				wildcardModel: {{channelId: 7803, participates: true}, {channelId: 7804, participates: true}},
			},
		},
	))

	participates, cacheEnabled := CachedChannelSmartScheduleRuntimeParticipates(7803, exactModel)
	assert.True(t, cacheEnabled)
	assert.False(t, participates)
	participates, cacheEnabled = CachedChannelSmartScheduleRuntimeParticipates(7804, exactModel)
	assert.True(t, cacheEnabled)
	assert.True(t, participates)
}

func TestRefreshChannelSmartScheduleRoutePoolCacheRefreshesRuntimeRouteIndex(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	originalGroupCache := group2model2channels
	originalChannelCache := channelsIDM
	originalAdvancedCustomCache := channel2advancedCustomConfig
	originalRouteCache := channelSmartScheduleRouteCache
	originalRuntimeRouteIndex := channelSmartScheduleRuntimeRouteIndexCache.Load()
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		group2model2channels = originalGroupCache
		channelsIDM = originalChannelCache
		channel2advancedCustomConfig = originalAdvancedCustomCache
		channelSmartScheduleRouteCache = originalRouteCache
		channelSmartScheduleRuntimeRouteIndexCache.Store(originalRuntimeRouteIndex)
		channelSyncLock.Unlock()
	})

	const channelId = 7802
	priority := int64(100)
	require.NoError(t, db.Create(&Channel{
		Id: channelId, Name: "runtime refresh", Status: common.ChannelStatusEnabled,
		Group: "vip,other", Models: "model-a,model-b",
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: channelId, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 100},
		{ChannelId: channelId, Group: "other", Model: "model-b", Enabled: true, Priority: &priority, Weight: 100},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: channelId, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficExploration, TemporaryTrafficSince: 201,
		},
		{
			ChannelId: channelId, GroupName: "other", ModelName: "model-b", ParticipationSet: true,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficAdaptive, TemporaryTrafficSince: 301,
		},
	}).Error)
	InitChannelCache()

	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).
		Where("channel_id = ? AND group_name = ? AND model_name = ?", channelId, "vip", "model-a").
		Updates(map[string]any{
			"temporary_traffic_kind":  ChannelSmartScheduleTemporaryTrafficAdaptive,
			"temporary_traffic_since": int64(202),
			"stability_state":         ChannelSmartScheduleStabilityProbing,
			"stability_since":         int64(205),
		}).Error)
	require.NoError(t, RefreshChannelSmartScheduleRoutePoolCache("vip", "model-a"))

	routes, err := GetChannelSmartScheduleRuntimeRoutes(channelId, "model-a")
	require.NoError(t, err)
	assert.Equal(t, map[string]ChannelSmartScheduleRuntimeRoute{
		"vip": {
			ModelName: "model-a", SampleSince: 205,
			StabilityState:       ChannelSmartScheduleStabilityProbing,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficAdaptive,
		},
	}, routes)
	unrelated, err := GetChannelSmartScheduleRuntimeRoutes(channelId, "model-b")
	require.NoError(t, err)
	assert.Equal(t, map[string]ChannelSmartScheduleRuntimeRoute{
		"other": {
			ModelName: "model-b", SampleSince: 301,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficAdaptive,
		},
	}, unrelated)

	require.NoError(t, db.Where(&Ability{
		ChannelId: channelId, Group: "vip", Model: "model-a",
	}).Delete(&Ability{}).Error)
	require.NoError(t, RefreshChannelSmartScheduleRoutePoolCache("vip", "model-a"))
	routes, err = GetChannelSmartScheduleRuntimeRoutes(channelId, "model-a")
	require.NoError(t, err)
	assert.Empty(t, routes)
	unrelated, err = GetChannelSmartScheduleRuntimeRoutes(channelId, "model-b")
	require.NoError(t, err)
	assert.Contains(t, unrelated, "other")
}
