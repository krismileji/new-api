package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useChannelSmartScheduleTrafficPolicy(t *testing.T, enabled bool, policies string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	originalEnabled, hadEnabled := common.OptionMap[channelMonitorSmartScheduleEnabledOption]
	originalPolicies, hadPolicies := common.OptionMap[channelMonitorSmartScheduleGroupPoliciesOption]
	common.OptionMap[channelMonitorSmartScheduleEnabledOption] = map[bool]string{true: "true", false: "false"}[enabled]
	common.OptionMap[channelMonitorSmartScheduleGroupPoliciesOption] = policies
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		if hadEnabled {
			common.OptionMap[channelMonitorSmartScheduleEnabledOption] = originalEnabled
		} else {
			delete(common.OptionMap, channelMonitorSmartScheduleEnabledOption)
		}
		if hadPolicies {
			common.OptionMap[channelMonitorSmartScheduleGroupPoliciesOption] = originalPolicies
		} else {
			delete(common.OptionMap, channelMonitorSmartScheduleGroupPoliciesOption)
		}
		common.OptionMapRWMutex.Unlock()
		channelSmartScheduleTrafficPolicyCache.Store(nil)
	})
	channelSmartScheduleTrafficPolicyCache.Store(nil)
}

func TestChannelSmartScheduleTrafficPolicyDatabaseSelectionScopesParticipationToManagedPools(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	useDatabaseChannelSelection(t)
	useChannelSmartScheduleTrafficPolicy(t, true, `[{"group":"vip","models":["model-a"]}]`)

	highPriority := int64(100)
	participatingPriority := int64(10)
	rejectedPriority := int64(200)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 5201, Name: "未参与高优先级", Status: common.ChannelStatusEnabled},
		{Id: 5202, Name: "参与低优先级", Status: common.ChannelStatusEnabled},
		{Id: 5203, Name: "已取消参与", Status: common.ChannelStatusEnabled},
		{Id: 5204, Name: "未配置分组", Status: common.ChannelStatusEnabled},
		{Id: 5205, Name: "策略外模型", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 5201, Group: "vip", Model: "model-a", Enabled: true, Priority: &highPriority, Weight: 1000},
		{ChannelId: 5202, Group: "vip", Model: "model-a", Enabled: true, Priority: &participatingPriority, Weight: 100},
		{ChannelId: 5203, Group: "vip", Model: "model-a", Enabled: true, Priority: &rejectedPriority, Weight: 1000},
		{ChannelId: 5204, Group: "unconfigured", Model: "model-a", Enabled: true, Priority: &highPriority, Weight: 1000},
		{ChannelId: 5205, Group: "vip", Model: "model-b", Enabled: true, Priority: &highPriority, Weight: 1000},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 5202, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 5203, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Excluded: true},
	}).Error)

	channel, err := GetRandomSatisfiedChannel("vip", "model-a", 0, "")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5202, channel.Id)

	channel, err = GetRandomSatisfiedChannel("unconfigured", "model-a", 0, "")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5204, channel.Id)

	channel, err = GetRandomSatisfiedChannel("vip", "model-b", 0, "")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5205, channel.Id)
}

func TestChannelSmartScheduleManagedPoolIgnoresChannelDefaultRouting(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	useDatabaseChannelSelection(t)
	useChannelSmartScheduleTrafficPolicy(t, true, `[{"group":"vip","models":["model-a"]}]`)
	defaultPriority := int64(1000)
	defaultWeight := uint(1000)
	lowDefaultPriority := int64(1)
	lowDefaultWeight := uint(1)
	abilityPriority := int64(0)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 5221, Name: "高默认值", Status: common.ChannelStatusEnabled, Priority: &defaultPriority, Weight: &defaultWeight},
		{Id: 5222, Name: "智能调度值", Status: common.ChannelStatusEnabled, Priority: &lowDefaultPriority, Weight: &lowDefaultWeight},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 5221, Group: "vip", Model: "model-a", Enabled: true},
		{ChannelId: 5222, Group: "vip", Model: "model-a", Enabled: true, Priority: &abilityPriority, Weight: 100},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 5221, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 5222, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)

	channel, err := GetRandomSatisfiedChannel("vip", "model-a", 0, "")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5222, channel.Id)
}

func TestChannelSmartScheduleTrafficPolicyCacheSelectionFailsClosedAndFallsBackToEligibleWildcard(t *testing.T) {
	setupChannelSmartScheduleRouteTestDB(t)
	const exactModel = "gemini-2.5-pro-thinking-2048"
	const wildcardModel = "gemini-2.5-pro-thinking-*"
	useChannelSmartScheduleTrafficPolicy(t, true, `[{"group":"vip","models":["gemini-2.5-pro-thinking-2048","gemini-2.5-pro-thinking-*"]}]`)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	originalGroupCache := group2model2channels
	originalChannelCache := channelsIDM
	originalAdvancedCustomCache := channel2advancedCustomConfig
	originalRouteCache := channelSmartScheduleRouteCache
	originalLogicalRuntime := logicalChannelRuntimeCache
	originalLogicalDirty := logicalChannelRuntimeDirty
	logicalChannelRuntimeCache = nil
	logicalChannelRuntimeDirty = false
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		group2model2channels = originalGroupCache
		channelsIDM = originalChannelCache
		channel2advancedCustomConfig = originalAdvancedCustomCache
		channelSmartScheduleRouteCache = originalRouteCache
		logicalChannelRuntimeCache = originalLogicalRuntime
		logicalChannelRuntimeDirty = originalLogicalDirty
		channelSyncLock.Unlock()
	})

	highPriority := int64(100)
	channelsIDM = map[int]*Channel{
		5211: {Id: 5211, Name: "未参与精确模型", Status: common.ChannelStatusEnabled},
		5212: {Id: 5212, Name: "参与通配模型", Status: common.ChannelStatusEnabled},
		5213: {Id: 5213, Name: "未配置分组", Status: common.ChannelStatusEnabled},
		5214: {Id: 5214, Name: "策略外模型", Status: common.ChannelStatusEnabled},
	}
	group2model2channels = map[string]map[string][]int{
		"vip":          {exactModel: {5211}, wildcardModel: {5212}, "model-b": {5214}},
		"unconfigured": {"model-a": {5213}},
	}
	channel2advancedCustomConfig = nil
	channelSmartScheduleRouteCache = buildChannelSmartScheduleRouteCacheFromStates(
		[]*Ability{
			{ChannelId: 5211, Group: "vip", Model: exactModel, Enabled: true, Priority: &highPriority, Weight: 1000},
			{ChannelId: 5212, Group: "vip", Model: wildcardModel, Enabled: true, Priority: &highPriority, Weight: 100},
			{ChannelId: 5213, Group: "unconfigured", Model: "model-a", Enabled: true, Priority: &highPriority, Weight: 100},
			{ChannelId: 5214, Group: "vip", Model: "model-b", Enabled: true, Priority: &highPriority, Weight: 100},
		},
		channelsIDM,
		[]ChannelSmartScheduleRouteState{
			{ChannelId: 5212, GroupName: "vip", ModelName: wildcardModel, ParticipationSet: true},
		},
	)

	channel, err := GetRandomSatisfiedChannel("vip", exactModel, 0, "")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5212, channel.Id)

	channel, err = GetRandomSatisfiedChannel("unconfigured", "model-a", 0, "")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5213, channel.Id)

	channel, err = GetRandomSatisfiedChannel("vip", "model-b", 0, "")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5214, channel.Id)

	channelSmartScheduleRouteCache = nil
	channel, err = GetRandomSatisfiedChannel("vip", exactModel, 0, "")
	require.NoError(t, err)
	assert.Nil(t, channel)

	channel, err = GetRandomSatisfiedChannel("unconfigured", "model-a", 0, "")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5213, channel.Id)
}

func TestChannelSmartScheduleTrafficPolicyDisabledRestoresOfficialCandidates(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	useDatabaseChannelSelection(t)
	useChannelSmartScheduleTrafficPolicy(t, false, `[]`)
	priority := int64(100)
	require.NoError(t, db.Create(&Channel{
		Id: 5221, Name: "官方候选", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 5221, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 100,
	}).Error)

	channel, err := GetRandomSatisfiedChannel("vip", "model-a", 0, "")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5221, channel.Id)
}

func TestChannelSmartScheduleTrafficPolicyInvalidConfigFailsClosedToParticipatingRoutes(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	useDatabaseChannelSelection(t)
	useChannelSmartScheduleTrafficPolicy(t, true, `{`)
	priority := int64(100)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 5226, Name: "未参与渠道", Status: common.ChannelStatusEnabled},
		{Id: 5227, Name: "已有参与状态渠道", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 5226, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 1000},
		{ChannelId: 5227, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 100},
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 5227, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)

	channel, err := GetRandomSatisfiedChannel("vip", "model-a", 0, "")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5227, channel.Id)
}

func TestChannelSmartScheduleTrafficPolicySelectionSkipsDegradedRouteUntilRetry(t *testing.T) {
	for _, memoryCacheEnabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "database", true: "cache"}[memoryCacheEnabled], func(t *testing.T) {
			db := setupChannelSmartScheduleRouteTestDB(t)
			useChannelSmartScheduleTrafficPolicy(t, true, `[{"group":"vip","models":["model-a"]}]`)
			originalMemoryCacheEnabled := common.MemoryCacheEnabled
			channelSyncLock.Lock()
			originalGroupCache := group2model2channels
			originalChannelCache := channelsIDM
			originalAdvancedCustomCache := channel2advancedCustomConfig
			originalRouteCache := channelSmartScheduleRouteCache
			channelSyncLock.Unlock()
			common.MemoryCacheEnabled = memoryCacheEnabled
			t.Cleanup(func() {
				common.MemoryCacheEnabled = originalMemoryCacheEnabled
				channelSyncLock.Lock()
				group2model2channels = originalGroupCache
				channelsIDM = originalChannelCache
				channel2advancedCustomConfig = originalAdvancedCustomCache
				channelSmartScheduleRouteCache = originalRouteCache
				channelSyncLock.Unlock()
			})

			degradedPriority := int64(0)
			require.NoError(t, db.Create(&Channel{
				Id: 5222, Name: "稳定性降级渠道", Status: common.ChannelStatusEnabled,
				Group: "vip", Models: "model-a",
			}).Error)
			require.NoError(t, db.Create(&Ability{
				ChannelId: 5222, Group: "vip", Model: "model-a", Enabled: true,
				Priority: &degradedPriority, Weight: 0,
			}).Error)
			require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
				ChannelId: 5222, GroupName: "vip", ModelName: "model-a",
				ParticipationSet: true, StabilityState: ChannelSmartScheduleStabilityDegraded,
			}).Error)
			if memoryCacheEnabled {
				InitChannelCache()
			}

			assert.Equal(t, ChannelSmartScheduleAffinityInvalid,
				ChannelSmartScheduleAffinityEligibility("vip", "model-a", 5222, ""))

			channel, err := GetRandomSatisfiedChannel("vip", "model-a", 0, "")
			require.NoError(t, err)
			assert.Nil(t, channel)

			channel, err = GetRandomSatisfiedChannel("vip", "model-a", 1, "")
			require.NoError(t, err)
			require.NotNil(t, channel)
			assert.Equal(t, 5222, channel.Id)
		})
	}
}

func TestAddAbilitiesClearsNonparticipatingRouteOverride(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	channelPriority := int64(70)
	channelWeight := uint(30)
	stalePriority := int64(900)
	channel := Channel{
		Id: 5231, Name: "未参与路由", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &channelPriority, Weight: &channelWeight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &stalePriority, Weight: 900,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Excluded: true,
	}).Error)

	require.NoError(t, channel.AddAbilities(nil))
	var ability Ability
	require.NoError(t, db.Where(&Ability{
		ChannelId: channel.Id, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	assert.Nil(t, ability.Priority)
	assert.Zero(t, ability.Weight)
}
