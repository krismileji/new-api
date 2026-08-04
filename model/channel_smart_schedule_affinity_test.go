package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestIsChannelSmartScheduleAffinityEligibleRequiresHighestPriority(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	highPriority := int64(100)
	lowPriority := int64(80)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1711, Name: "primary", Status: common.ChannelStatusEnabled},
		{Id: 1712, Name: "backup", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1711, Group: "vip", Model: "model-a", Enabled: true, Priority: &highPriority, Weight: 100},
		{ChannelId: 1712, Group: "vip", Model: "model-a", Enabled: true, Priority: &lowPriority, Weight: 100},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 1711, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 1712, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)

	assert.Equal(t, ChannelSmartScheduleAffinityEligible, ChannelSmartScheduleAffinityEligibility("vip", "model-a", 1711, "/v1/chat/completions"))
	assert.Equal(t, ChannelSmartScheduleAffinityTemporarilyUnavailable, ChannelSmartScheduleAffinityEligibility("vip", "model-a", 1712, "/v1/chat/completions"))

	require.NoError(t, db.Model(&Ability{}).
		Where(&Ability{ChannelId: 1711, Group: "vip", Model: "model-a"}).
		Updates(map[string]any{"priority": 0, "weight": 0}).Error)
	assert.Equal(t, ChannelSmartScheduleAffinityTemporarilyUnavailable, ChannelSmartScheduleAffinityEligibility("vip", "model-a", 1711, "/v1/chat/completions"))
	assert.Equal(t, ChannelSmartScheduleAffinityEligible, ChannelSmartScheduleAffinityEligibility("vip", "model-a", 1712, "/v1/chat/completions"))
}

func TestIsChannelSmartScheduleAffinityEligibleKeepsUnmanagedPool(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(80)
	require.NoError(t, db.Create(&Channel{
		Id: 1721, Name: "unmanaged", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1721, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: 100,
	}).Error)

	assert.Equal(t, ChannelSmartScheduleAffinityEligible, ChannelSmartScheduleAffinityEligibility("vip", "model-a", 1721, "/v1/chat/completions"))
}

func TestChannelSmartScheduleAffinityEligibilityIgnoresExcludedOnlyPool(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	highPriority := int64(100)
	lowPriority := int64(80)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1722, Name: "primary", Status: common.ChannelStatusEnabled},
		{Id: 1723, Name: "excluded backup", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1722, Group: "vip", Model: "model-a", Enabled: true, Priority: &highPriority, Weight: 100},
		{ChannelId: 1723, Group: "vip", Model: "model-a", Enabled: true, Priority: &lowPriority, Weight: 100},
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1723, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Excluded: true,
	}).Error)

	assert.Equal(t, ChannelSmartScheduleAffinityEligible,
		ChannelSmartScheduleAffinityEligibility("vip", "model-a", 1723, "/v1/chat/completions"))
}

func TestChannelSmartScheduleAffinityEligibilityUsesActivePoolCacheWithoutDatabaseRead(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	originalGroupCache := group2model2channels
	originalChannelCache := channelsIDM
	originalAdvancedCustomCache := channel2advancedCustomConfig
	originalRouteCache := channelSmartScheduleRouteCache
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		group2model2channels = originalGroupCache
		channelsIDM = originalChannelCache
		channel2advancedCustomConfig = originalAdvancedCustomCache
		channelSmartScheduleRouteCache = originalRouteCache
		channelSyncLock.Unlock()
	})

	highPriority := int64(100)
	lowPriority := int64(80)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1724, Name: "cached primary", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a"},
		{Id: 1725, Name: "cached backup", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a"},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1724, Group: "vip", Model: "model-a", Enabled: true, Priority: &highPriority, Weight: 100},
		{ChannelId: 1725, Group: "vip", Model: "model-a", Enabled: true, Priority: &lowPriority, Weight: 100},
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1724, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)
	InitChannelCache()

	queryCount := 0
	callbackName := "test:count_affinity_hot_path_queries"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		queryCount++
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	assert.Equal(t, ChannelSmartScheduleAffinityEligible,
		ChannelSmartScheduleAffinityEligibility("vip", "model-a", 1724, "/v1/chat/completions"))
	assert.Equal(t, ChannelSmartScheduleAffinityTemporarilyUnavailable,
		ChannelSmartScheduleAffinityEligibility("vip", "model-a", 1725, "/v1/chat/completions"))
	assert.Zero(t, queryCount)
}

func TestChannelSmartScheduleAffinityEligibilityKeepsStableAffinityForLargeRequest(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	channelSyncLock.Lock()
	originalRouteCache := channelSmartScheduleRouteCache
	originalChannelsIDM := channelsIDM
	channelSyncLock.Unlock()
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		channelSmartScheduleRouteCache = originalRouteCache
		channelsIDM = originalChannelsIDM
		channelSyncLock.Unlock()
	})

	explorationPriority := int64(100)
	stablePriority := int64(80)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1726, Name: "exploration", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a"},
		{Id: 1727, Name: "stable", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a"},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1726, Group: "vip", Model: "model-a", Enabled: true, Priority: &explorationPriority, Weight: 100},
		{ChannelId: 1727, Group: "vip", Model: "model-a", Enabled: true, Priority: &stablePriority, Weight: 100},
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1726, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		TemporaryTrafficKind:       ChannelSmartScheduleTemporaryTrafficExploration,
		ExplorationMaxPromptTokens: 100,
	}).Error)
	InitChannelCache()

	largeRequest := ChannelSelectionOptions{EstimatedPromptTokens: 101}
	assert.Equal(t, ChannelSmartScheduleAffinityEligible,
		ChannelSmartScheduleAffinityEligibility("vip", "model-a", 1727, "/v1/chat/completions", largeRequest))
	assert.Equal(t, ChannelSmartScheduleAffinityTemporarilyUnavailable,
		ChannelSmartScheduleAffinityEligibility("vip", "model-a", 1726, "/v1/chat/completions", largeRequest))
}

func TestChannelSmartScheduleAffinityEligibilitySkipsDisabledHigherPriority(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	highPriority := int64(100)
	lowPriority := int64(80)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1731, Name: "disabled-primary", Status: common.ChannelStatusManuallyDisabled},
		{Id: 1732, Name: "enabled-backup", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1731, Group: "vip", Model: "model-a", Enabled: true, Priority: &highPriority, Weight: 100},
		{ChannelId: 1732, Group: "vip", Model: "model-a", Enabled: true, Priority: &lowPriority, Weight: 100},
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1732, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)

	assert.Equal(t, ChannelSmartScheduleAffinityEligible, ChannelSmartScheduleAffinityEligibility("vip", "model-a", 1732, "/v1/chat/completions"))
}

func TestChannelSmartScheduleAffinityEligibilityPrefersExactParameterizedPool(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	const exactModel = "gemini-2.5-pro-thinking-2048"
	const wildcardModel = "gemini-2.5-pro-thinking-*"
	priority := int64(100)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1741, Name: "exact", Status: common.ChannelStatusEnabled},
		{Id: 1742, Name: "wildcard", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1741, Group: "vip", Model: exactModel, Enabled: true, Priority: &priority, Weight: 100},
		{ChannelId: 1742, Group: "vip", Model: wildcardModel, Enabled: true, Priority: &priority, Weight: 100},
	}).Error)
	// A wildcard pool can be managed independently. It must not make a request
	// with an exact configured route use the wildcard pool's affinity rules.
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1742, GroupName: "vip", ModelName: wildcardModel, ParticipationSet: true,
	}).Error)

	assert.Equal(t, ChannelSmartScheduleAffinityEligible,
		ChannelSmartScheduleAffinityEligibility("vip", exactModel, 1741, "/v1/chat/completions"))
	assert.Equal(t, ChannelSmartScheduleAffinityInvalid,
		ChannelSmartScheduleAffinityEligibility("vip", exactModel, 1742, "/v1/chat/completions"))
}

func TestGetChannelSmartScheduleRuntimeTemporaryRoutesUsesSelectedRouteModel(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	const exactModel = "gemini-2.5-pro-thinking-2048"
	const wildcardModel = "gemini-2.5-pro-thinking-*"
	priority := int64(100)
	require.NoError(t, db.Create(&Channel{
		Id: 1751, Name: "runtime route", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1751, Group: "exact", Model: exactModel, Enabled: true, Priority: &priority, Weight: 100},
		{ChannelId: 1751, Group: "exact", Model: wildcardModel, Enabled: true, Priority: &priority, Weight: 100},
		{ChannelId: 1751, Group: "wildcard", Model: wildcardModel, Enabled: true, Priority: &priority, Weight: 100},
		{ChannelId: 1751, Group: "inactive-exact", Model: exactModel, Enabled: true, Priority: &priority, Weight: 100},
		{ChannelId: 1751, Group: "inactive-exact", Model: wildcardModel, Enabled: true, Priority: &priority, Weight: 100},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: 1751, GroupName: "exact", ModelName: exactModel, ParticipationSet: true,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficExploration, TemporaryTrafficSince: 101,
		},
		{
			ChannelId: 1751, GroupName: "exact", ModelName: wildcardModel, ParticipationSet: true,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficExploration, TemporaryTrafficSince: 102,
		},
		{
			ChannelId: 1751, GroupName: "wildcard", ModelName: wildcardModel, ParticipationSet: true,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficExploration, TemporaryTrafficSince: 103,
		},
		{
			ChannelId: 1751, GroupName: "inactive-exact", ModelName: wildcardModel, ParticipationSet: true,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficExploration, TemporaryTrafficSince: 104,
		},
	}).Error)

	routes, err := GetChannelSmartScheduleRuntimeTemporaryRoutes(1751, exactModel)
	require.NoError(t, err)
	assert.Equal(t, map[string]ChannelSmartScheduleRuntimeTemporaryRoute{
		"exact":    {ModelName: exactModel, SampleSince: 101},
		"wildcard": {ModelName: wildcardModel, SampleSince: 103},
	}, routes)
}

func TestGetChannelSmartScheduleRuntimeTemporaryRoutesIncludesFixedPrimaryOnlyWhenDegradeAllowed(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(101)
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&Channel{
		Id: 1752, Name: "fixed runtime route", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1752, Group: "allow", Model: "model-a", Enabled: true, Priority: &priority, Weight: 1000},
		{ChannelId: 1752, Group: "strict", Model: "model-a", Enabled: true, Priority: &priority, Weight: 1000},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: 1752, GroupName: "allow", ModelName: "model-a", ParticipationSet: true,
			ManualPrimaryUntil: now + 600, ManualPrimaryAllowStabilityDegrade: true,
		},
		{
			ChannelId: 1752, GroupName: "strict", ModelName: "model-a", ParticipationSet: true,
			ManualPrimaryUntil: now + 600, ManualPrimaryAllowStabilityDegrade: false,
		},
	}).Error)

	routes, err := GetChannelSmartScheduleRuntimeTemporaryRoutes(1752, "model-a")
	require.NoError(t, err)
	assert.Equal(t, map[string]ChannelSmartScheduleRuntimeTemporaryRoute{
		"allow": {ModelName: "model-a"},
	}, routes)
}
