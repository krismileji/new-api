package model

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelSmartScheduleRouteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := DB
	originalLogDB := LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	require.NoError(t, db.AutoMigrate(
		&Option{},
		&Channel{},
		&Ability{},
		&ChannelRatioMonitor{},
		&ChannelSmartScheduleRouteState{},
	))
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func preserveChannelSmartScheduleRouteOptions(t *testing.T) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	original := make(map[string]string, len(common.OptionMap))
	for key, value := range common.OptionMap {
		original[key] = value
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = original
		common.OptionMapRWMutex.Unlock()
	})
}

func TestInitializeChannelSmartScheduleRouteStatesAdoptsLegacyStateOnce(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	preserveChannelSmartScheduleRouteOptions(t)
	priority := int64(90)
	weight := uint(40)
	require.NoError(t, db.Create(&Channel{
		Id: 1001, Name: "legacy", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1001, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	score := 0.72
	require.NoError(t, db.Create(&ChannelRatioMonitor{
		ChannelId: 1001, SmartScheduleParticipationSet: true,
		LastScheduleStatus: ChannelSmartScheduleStatusSucceeded,
		LastScheduleScore:  &score, LastSchedulePriority: 90,
		LastScheduleWeight: 40, LastScheduleTime: 123,
		SmartScheduleStabilityState: ChannelSmartScheduleStabilityDegraded,
		SmartScheduleStabilityUntil: 456, SmartScheduleStabilitySince: 120,
		SmartScheduleSavedPriority: 90, SmartScheduleSavedWeight: 40,
	}).Error)

	require.NoError(t, InitializeChannelSmartScheduleRouteStates())
	var adopted ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1001, "vip", "model-a",
	).First(&adopted).Error)
	assert.True(t, adopted.ParticipationSet)
	assert.False(t, adopted.Excluded)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, adopted.StabilityState)
	assert.Equal(t, int64(90), adopted.StabilitySavedPriority)
	assert.Equal(t, uint(40), adopted.StabilitySavedWeight)
	require.NotNil(t, adopted.LastScheduleScore)
	assert.InDelta(t, score, *adopted.LastScheduleScore, 1e-9)

	require.NoError(t, db.Create(&Ability{
		ChannelId: 1001, Group: "standard", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, InitializeChannelSmartScheduleRouteStates())
	var laterRoute ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1001, "standard", "model-a",
	).First(&laterRoute).Error)
	assert.True(t, laterRoute.ParticipationSet)
	assert.True(t, laterRoute.Excluded)
	assert.Empty(t, laterRoute.StabilityState)
}

func TestApplyChannelSmartScheduleRouteResultOnlyChangesTargetAbility(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	channelPriority := int64(70)
	channelWeight := uint(30)
	routePriority := int64(80)
	routeWeight := uint(50)
	require.NoError(t, db.Create(&Channel{
		Id: 1002, Name: "multi-group", Status: common.ChannelStatusEnabled,
		Group: "vip,standard", Models: "model-a",
		Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1002, Group: "vip", Model: "model-a", Enabled: true, Priority: &routePriority, Weight: routeWeight},
		{ChannelId: 1002, Group: "standard", Model: "model-a", Enabled: true, Priority: &routePriority, Weight: routeWeight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 1002, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{ChannelId: 1002, GroupName: "standard", ModelName: "model-a", ParticipationSet: true, Revision: 1},
	}).Error)

	outcomes, err := ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{{
		ChannelId: 1002, Group: "vip", Model: "model-a",
		Status: ChannelSmartScheduleStatusSucceeded, Priority: 100, Weight: 90,
		ExpectedPriority: routePriority, ExpectedWeight: routeWeight,
		ApplyPriorityWeight: true,
	}})
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.True(t, outcomes[0].Applied)
	assert.True(t, outcomes[0].RoutingChanged)

	var vip Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1002, Group: "vip", Model: "model-a"}).First(&vip).Error)
	require.NotNil(t, vip.Priority)
	assert.Equal(t, int64(100), *vip.Priority)
	assert.Equal(t, uint(90), vip.Weight)
	var standard Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1002, Group: "standard", Model: "model-a"}).First(&standard).Error)
	require.NotNil(t, standard.Priority)
	assert.Equal(t, routePriority, *standard.Priority)
	assert.Equal(t, routeWeight, standard.Weight)
	var channel Channel
	require.NoError(t, db.First(&channel, 1002).Error)
	assert.Equal(t, channelPriority, channel.GetPriority())
	assert.Equal(t, int(channelWeight), channel.GetWeight())
}

func TestChannelSmartScheduleRouteScopeSwitchPreservesIsolatedRouting(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	channelPriority := int64(80)
	channelWeight := uint(50)
	isolatedPriority := int64(95)
	degradedPriority := int64(0)
	require.NoError(t, db.Create(&Channel{
		Id: 1004, Name: "scope-switch", Status: common.ChannelStatusEnabled,
		Group: "vip,standard", Models: "model-a",
		Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1004, Group: "vip", Model: "model-a", Enabled: true, Priority: &isolatedPriority, Weight: 70},
		{ChannelId: 1004, Group: "standard", Model: "model-a", Enabled: true, Priority: &degradedPriority, Weight: 0},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 1004, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
		{
			ChannelId: 1004, GroupName: "standard", ModelName: "model-a",
			ParticipationSet: true, Revision: 1,
			StabilityState:         ChannelSmartScheduleStabilityDegraded,
			StabilitySavedPriority: 90, StabilitySavedWeight: 40,
		},
	}).Error)

	changed, err := SuspendChannelSmartScheduleRouteRouting()
	require.NoError(t, err)
	assert.True(t, changed)
	var suspended []Ability
	require.NoError(t, db.Where("channel_id = ?", 1004).Find(&suspended).Error)
	require.Len(t, suspended, 2)
	for _, ability := range suspended {
		require.NotNil(t, ability.Priority)
		assert.Equal(t, channelPriority, *ability.Priority)
		assert.Equal(t, channelWeight, ability.Weight)
	}
	var degradedState ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1004, "standard", "model-a",
	).First(&degradedState).Error)
	assert.True(t, degradedState.ScopeRoutingSaved)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, degradedState.StabilityState)
	assert.Equal(t, int64(0), degradedState.ScopeSavedPriority)
	assert.Equal(t, uint(0), degradedState.ScopeSavedWeight)

	changed, err = ResumeChannelSmartScheduleRouteRouting()
	require.NoError(t, err)
	assert.True(t, changed)
	var vip Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1004, Group: "vip", Model: "model-a"}).First(&vip).Error)
	require.NotNil(t, vip.Priority)
	assert.Equal(t, isolatedPriority, *vip.Priority)
	assert.Equal(t, uint(70), vip.Weight)
	var standard Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1004, Group: "standard", Model: "model-a"}).First(&standard).Error)
	require.NotNil(t, standard.Priority)
	assert.Equal(t, degradedPriority, *standard.Priority)
	assert.Equal(t, uint(0), standard.Weight)
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 1004, "standard", "model-a",
	).First(&degradedState).Error)
	assert.False(t, degradedState.ScopeRoutingSaved)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, degradedState.StabilityState)
}

func TestChannelSmartScheduleScopeOptionRollsBackWhenRoutingSwitchFails(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	preserveChannelSmartScheduleRouteOptions(t)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[channelSmartScheduleScopeOption] = channelSmartScheduleScopeGroupModel
	common.OptionMap[ChannelSmartScheduleControlRevisionOption] = "old-revision"
	common.OptionMapRWMutex.Unlock()
	require.NoError(t, db.Create(&[]Option{
		{Key: channelSmartScheduleScopeOption, Value: channelSmartScheduleScopeGroupModel},
		{Key: ChannelSmartScheduleControlRevisionOption, Value: "old-revision"},
	}).Error)
	channelPriority := int64(80)
	channelWeight := uint(50)
	routePriority := int64(95)
	require.NoError(t, db.Create(&Channel{
		Id: 1005, Name: "rollback", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1005, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &routePriority, Weight: 70,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1005, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: math.MaxInt64,
	}).Error)

	changed, err := UpdateOptionsBulkWithChannelSmartScheduleScope(map[string]string{
		channelSmartScheduleScopeOption:           channelSmartScheduleScopeChannel,
		ChannelSmartScheduleControlRevisionOption: "new-revision",
	}, channelSmartScheduleScopeChannel)
	require.Error(t, err)
	assert.False(t, changed)

	var scopeOption Option
	require.NoError(t, db.First(&scopeOption, "key = ?", channelSmartScheduleScopeOption).Error)
	assert.Equal(t, channelSmartScheduleScopeGroupModel, scopeOption.Value)
	var controlOption Option
	require.NoError(t, db.First(&controlOption, "key = ?", ChannelSmartScheduleControlRevisionOption).Error)
	assert.Equal(t, "old-revision", controlOption.Value)
	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1005, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, routePriority, *ability.Priority)
	assert.Equal(t, uint(70), ability.Weight)
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, channelSmartScheduleScopeGroupModel, common.OptionMap[channelSmartScheduleScopeOption])
	assert.Equal(t, "old-revision", common.OptionMap[ChannelSmartScheduleControlRevisionOption])
	common.OptionMapRWMutex.RUnlock()
}

func TestClearChannelSmartScheduleRouteStabilityRestoresOnlyTargetRoute(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	channelPriority := int64(80)
	channelWeight := uint(50)
	degradedPriority := int64(0)
	otherPriority := int64(70)
	require.NoError(t, db.Create(&Channel{
		Id: 1003, Name: "protected", Status: common.ChannelStatusEnabled,
		Group: "vip,standard", Models: "model-a",
		Priority: &channelPriority, Weight: &channelWeight,
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1003, Group: "vip", Model: "model-a", Enabled: true, Priority: &degradedPriority, Weight: 0},
		{ChannelId: 1003, Group: "standard", Model: "model-a", Enabled: true, Priority: &otherPriority, Weight: 25},
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1003, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		StabilityState:         ChannelSmartScheduleStabilityDegraded,
		StabilitySavedPriority: 95, StabilitySavedWeight: 45,
	}).Error)

	result, err := ClearChannelSmartScheduleRouteStability(1003, "vip", "model-a", 80, 10)
	require.NoError(t, err)
	assert.True(t, result.Cleared)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, result.PreviousState)
	assert.Equal(t, int64(95), result.Priority)
	assert.Equal(t, uint(45), result.Weight)

	var vip Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1003, Group: "vip", Model: "model-a"}).First(&vip).Error)
	require.NotNil(t, vip.Priority)
	assert.Equal(t, int64(95), *vip.Priority)
	assert.Equal(t, uint(45), vip.Weight)
	var standard Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1003, Group: "standard", Model: "model-a"}).First(&standard).Error)
	require.NotNil(t, standard.Priority)
	assert.Equal(t, otherPriority, *standard.Priority)
	assert.Equal(t, uint(25), standard.Weight)
}

func TestRouteCacheSelectsAbilityPriorityWithinGroupAndModel(t *testing.T) {
	originalChannels := channelsIDM
	originalAdvancedConfigs := channel2advancedCustomConfig
	originalRouteCache := channelSmartScheduleRouteCache
	t.Cleanup(func() {
		channelsIDM = originalChannels
		channel2advancedCustomConfig = originalAdvancedConfigs
		channelSmartScheduleRouteCache = originalRouteCache
	})
	channelPriorityHigh := int64(100)
	channelPriorityLow := int64(10)
	weight := uint(50)
	channelsIDM = map[int]*Channel{
		1: {Id: 1, Status: common.ChannelStatusEnabled, Priority: &channelPriorityHigh, Weight: &weight},
		2: {Id: 2, Status: common.ChannelStatusEnabled, Priority: &channelPriorityLow, Weight: &weight},
	}
	channel2advancedCustomConfig = map[int]*dto.AdvancedCustomConfig{}
	routePriorityLow := int64(80)
	routePriorityHigh := int64(100)
	abilities := []*Ability{
		{ChannelId: 1, Group: "vip", Model: "model-a", Enabled: true, Priority: &routePriorityLow, Weight: 50},
		{ChannelId: 2, Group: "vip", Model: "model-a", Enabled: true, Priority: &routePriorityHigh, Weight: 50},
	}
	channelSmartScheduleRouteCache = buildChannelSmartScheduleRouteCache(abilities, channelsIDM)

	channel, handled, err := getRandomSatisfiedChannelByAbility(
		"vip", "model-a", 0, "", ChannelSelectionOptions{},
	)
	require.NoError(t, err)
	assert.True(t, handled)
	require.NotNil(t, channel)
	assert.Equal(t, 2, channel.Id)
}
