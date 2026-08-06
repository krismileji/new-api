package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorEconomicRevisionTracksExactRatioAndCostConversionChanges(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.Create(&Channel{Id: 1801, Name: "economic revision"}).Error)

	initialRevision, err := GetChannelMonitorEconomicRevision()
	require.NoError(t, err)
	assert.Empty(t, initialRevision)

	_, created, changed, err := UpdateChannelRatioMonitor(1801, 1, "initial", 1, "root")
	require.NoError(t, err)
	assert.True(t, created)
	assert.False(t, changed)
	ratioRevision, err := GetChannelMonitorEconomicRevision()
	require.NoError(t, err)
	assert.NotEmpty(t, ratioRevision)
	assert.NotEqual(t, initialRevision, ratioRevision)

	_, _, changed, err = UpdateChannelRatioMonitor(1801, 1+5e-10, "classification boundary", 1, "root")
	require.NoError(t, err)
	assert.False(t, changed)
	preciseRatioRevision, err := GetChannelMonitorEconomicRevision()
	require.NoError(t, err)
	assert.NotEqual(t, ratioRevision, preciseRatioRevision)

	_, err = SaveChannelRatioUpstreamConfig(
		1801, "custom", "", "", "none", 0, "",
		ChannelRatioUpstreamOptions{RatioSyncEnabled: true, BalanceSyncEnabled: true, CostConversion: `{"mode":"none"}`},
	)
	require.NoError(t, err)
	conversionRevision, err := GetChannelMonitorEconomicRevision()
	require.NoError(t, err)
	assert.NotEqual(t, preciseRatioRevision, conversionRevision)
}

func TestApplyChannelSmartScheduleRouteResultsRejectsStaleEconomicRevision(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelRatioHistory{}))
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	common.OptionMapRWMutex.Lock()
	originalOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()
	})
	require.NoError(t, UpdateChannelMonitorGroupRatioOption(`{"vip":1}`))

	priority := int64(5)
	weight := uint(50)
	require.NoError(t, db.Create(&Channel{
		Id: 1802, Name: "stale economics", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1802, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1802, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
	}).Error)
	require.NoError(t, db.Create(&ChannelRatioMonitor{
		ChannelId: 1802, Ratio: 0.8, UpdatedTime: 1,
	}).Error)

	snapshot, err := GetChannelSmartScheduleEconomicSnapshot()
	require.NoError(t, err)
	require.NotEmpty(t, snapshot.Revision)
	controlRevision, err := GetChannelSmartScheduleControlRevision()
	require.NoError(t, err)

	_, _, _, err = UpdateChannelRatioMonitor(1802, 1, "became break even", 1, "root")
	require.NoError(t, err)

	outcomes, err := ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{{
		ChannelId: 1802, Group: "vip", Model: "model-a",
		Status: ChannelSmartScheduleStatusSucceeded, Priority: 10, Weight: 100,
		PoolGuard: true, ExpectedRevision: 1,
		ExpectedControlRevision: controlRevision, ExpectedEconomicRevision: snapshot.Revision,
		ExpectedParticipationSet: true, ExpectedAbilityEnabled: true,
		ExpectedChannelStatus: common.ChannelStatusEnabled,
		ExpectedPriority:      priority, ExpectedWeight: weight, ApplyPriorityWeight: true,
	}})
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.False(t, outcomes[0].Applied)

	var ability Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 1802, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priority, *ability.Priority)
	assert.Equal(t, weight, ability.Weight)
}
