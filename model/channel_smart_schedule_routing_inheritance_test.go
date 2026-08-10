package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExcludeChannelSmartScheduleRouteAlwaysClearsOverride(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	channelPriority := int64(80)
	channelWeight := uint(50)
	overridePriority := int64(120)
	require.NoError(t, db.Create(&Channel{
		Id: 5101, Name: "继承默认值", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 5101, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &overridePriority, Weight: 700,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 5101, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
	}).Error)

	state, routingChanged, err := SaveChannelSmartScheduleRouteConfig(5101, "vip", "model-a", true)
	require.NoError(t, err)
	assert.True(t, routingChanged)
	assert.False(t, state.Participates())

	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 5101, Group: "vip", Model: "model-a"}).First(&ability).Error)
	assert.Nil(t, ability.Priority)
	assert.Zero(t, ability.Weight)

	routes, err := GetChannelSmartScheduleRoutes()
	require.NoError(t, err)
	require.Len(t, routes, 1)
	assert.Equal(t, channelPriority, routes[0].Priority)
	assert.Equal(t, channelWeight, routes[0].Weight)

	channelPriority = 65
	channelWeight = 25
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 5101).Updates(map[string]any{
		"priority": channelPriority,
		"weight":   channelWeight,
	}).Error)
	routes, err = GetChannelSmartScheduleRoutes()
	require.NoError(t, err)
	require.Len(t, routes, 1)
	assert.Equal(t, channelPriority, routes[0].Priority)
	assert.Equal(t, channelWeight, routes[0].Weight)

	stalePriority := int64(999)
	require.NoError(t, db.Model(&Ability{}).
		Where(&Ability{ChannelId: 5101, Group: "vip", Model: "model-a"}).
		Updates(map[string]any{"priority": stalePriority, "weight": uint(999)}).Error)
	_, routingChanged, err = SaveChannelSmartScheduleRouteConfig(5101, "vip", "model-a", true)
	require.NoError(t, err)
	assert.True(t, routingChanged)
	require.NoError(t, db.Where(&Ability{ChannelId: 5101, Group: "vip", Model: "model-a"}).First(&ability).Error)
	assert.Nil(t, ability.Priority)
	assert.Zero(t, ability.Weight)

	_, routingChanged, err = SaveChannelSmartScheduleRouteConfig(5101, "vip", "model-a", false)
	require.NoError(t, err)
	assert.True(t, routingChanged)
	require.NoError(t, db.Where(&Ability{ChannelId: 5101, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, channelPriority, *ability.Priority)
	assert.Equal(t, channelWeight, ability.Weight)

	require.NoError(t, db.Model(&Ability{}).
		Where(&Ability{ChannelId: 5101, Group: "vip", Model: "model-a"}).
		Updates(map[string]any{"priority": stalePriority, "weight": uint(999)}).Error)
	_, routingChanged, err = SaveChannelSmartScheduleRouteConfig(5101, "vip", "model-a", true)
	require.NoError(t, err)
	assert.True(t, routingChanged)
	require.NoError(t, db.Where(&Ability{ChannelId: 5101, Group: "vip", Model: "model-a"}).First(&ability).Error)
	assert.Nil(t, ability.Priority)
	assert.Zero(t, ability.Weight)
}

func TestIncludeChannelSmartScheduleRouteCreatesAndPreservesOverride(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	channelPriority := int64(70)
	channelWeight := uint(30)
	require.NoError(t, db.Create(&Channel{
		Id: 5102, Name: "参与时创建覆盖", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 5102, Group: "vip", Model: "model-a", Enabled: true,
	}).Error)
	require.NoError(t, clearChannelSmartScheduleAbilityRoutingTx(
		db, channelSmartScheduleRouteKey(5102, "vip", "model-a"),
	))
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 5102, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Excluded: true, Revision: 1,
	}).Error)

	state, routingChanged, err := SaveChannelSmartScheduleRouteConfig(5102, "vip", "model-a", false)
	require.NoError(t, err)
	assert.True(t, routingChanged)
	assert.True(t, state.Participates())

	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 5102, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, channelPriority, *ability.Priority)
	assert.Equal(t, channelWeight, ability.Weight)

	scheduledPriority := int64(95)
	require.NoError(t, db.Model(&Ability{}).
		Where(&Ability{ChannelId: 5102, Group: "vip", Model: "model-a"}).
		Updates(map[string]any{"priority": scheduledPriority, "weight": uint(900)}).Error)
	_, routingChanged, err = SaveChannelSmartScheduleRouteConfig(5102, "vip", "model-a", false)
	require.NoError(t, err)
	assert.False(t, routingChanged)
	require.NoError(t, db.Where(&Ability{ChannelId: 5102, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, scheduledPriority, *ability.Priority)
	assert.Equal(t, uint(900), ability.Weight)

	require.NoError(t, clearChannelSmartScheduleAbilityRoutingTx(
		db, channelSmartScheduleRouteKey(5102, "vip", "model-a"),
	))
	_, routingChanged, err = SaveChannelSmartScheduleRouteConfig(5102, "vip", "model-a", false)
	require.NoError(t, err)
	assert.True(t, routingChanged)
	require.NoError(t, db.Where(&Ability{ChannelId: 5102, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, channelPriority, *ability.Priority)
	assert.Equal(t, channelWeight, ability.Weight)
}

func TestDatabaseSelectionUsesChannelRoutingWhenGroupOverrideIsMissing(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	defaultPriority := int64(100)
	defaultWeight := uint(10)
	overrideChannelPriority := int64(10)
	overridePriority := int64(90)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 5103, Name: "默认值优先", Status: common.ChannelStatusEnabled, Priority: &defaultPriority, Weight: &defaultWeight},
		{Id: 5104, Name: "分组覆盖次之", Status: common.ChannelStatusEnabled, Priority: &overrideChannelPriority, Weight: &defaultWeight},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 5103, Group: "vip", Model: "model-a", Enabled: true},
		{ChannelId: 5104, Group: "vip", Model: "model-a", Enabled: true, Priority: &overridePriority, Weight: 1000},
	}).Error)
	require.NoError(t, clearChannelSmartScheduleAbilityRoutingTx(
		db, channelSmartScheduleRouteKey(5103, "vip", "model-a"),
	))

	channel, err := getChannelFromDatabasePool(
		"vip", "model-a", "model-a", 0, "", ChannelSelectionOptions{},
	)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 5103, channel.Id)
}

func TestRouteCacheSelectionUsesChannelRoutingWhenGroupOverrideIsMissing(t *testing.T) {
	originalChannels := channelsIDM
	originalAdvancedConfigs := channel2advancedCustomConfig
	originalRouteCache := channelSmartScheduleRouteCache
	t.Cleanup(func() {
		channelsIDM = originalChannels
		channel2advancedCustomConfig = originalAdvancedConfigs
		channelSmartScheduleRouteCache = originalRouteCache
	})

	defaultPriority := int64(100)
	secondaryChannelPriority := int64(10)
	groupPriority := int64(90)
	defaultWeight := uint(10)
	channelsIDM = map[int]*Channel{
		5105: {
			Id:       5105,
			Status:   common.ChannelStatusEnabled,
			Priority: &defaultPriority,
			Weight:   &defaultWeight,
		},
		5106: {
			Id:       5106,
			Status:   common.ChannelStatusEnabled,
			Priority: &secondaryChannelPriority,
			Weight:   &defaultWeight,
		},
	}
	channel2advancedCustomConfig = map[int]*dto.AdvancedCustomConfig{}
	abilities := []*Ability{
		{ChannelId: 5105, Group: "vip", Model: "model-a", Enabled: true},
		{ChannelId: 5106, Group: "vip", Model: "model-a", Enabled: true, Priority: &groupPriority, Weight: 1000},
	}
	channelSmartScheduleRouteCache = buildChannelSmartScheduleRouteCacheWithStates(
		abilities,
		channelsIDM,
		nil,
	)

	channel, handled, err := getRandomSatisfiedChannelByAbility(
		"vip", "model-a", 0, "", ChannelSelectionOptions{},
	)
	require.NoError(t, err)
	assert.True(t, handled)
	require.NotNil(t, channel)
	assert.Equal(t, 5105, channel.Id)
}
