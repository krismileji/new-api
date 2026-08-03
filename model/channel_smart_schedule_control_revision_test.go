package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChannelSmartScheduleControlRevisionCreatesLockableOption(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.Where("key = ?", ChannelSmartScheduleControlRevisionOption).Delete(&Option{}).Error)

	revision, err := GetChannelSmartScheduleControlRevision()
	require.NoError(t, err)
	assert.Empty(t, revision)

	var option Option
	require.NoError(t, db.Where("key = ?", ChannelSmartScheduleControlRevisionOption).First(&option).Error)
	assert.Empty(t, option.Value)

	priority := int64(5)
	weight := uint(50)
	require.NoError(t, db.Create(&Channel{
		Id: 1701, Name: "first-use revision guard", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1701, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1701, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
	}).Error)
	require.NoError(t, db.Model(&Option{}).
		Where("key = ?", ChannelSmartScheduleControlRevisionOption).
		Update("value", "revision-after-plan").Error)

	outcomes, err := ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{{
		ChannelId: 1701, Group: "vip", Model: "model-a",
		Status: ChannelSmartScheduleStatusSucceeded, Priority: 10, Weight: 100,
		PoolGuard: true, ExpectedRevision: 1, ExpectedControlRevision: revision,
		ExpectedParticipationSet: true, ExpectedAbilityEnabled: true,
		ExpectedChannelStatus: common.ChannelStatusEnabled,
		ExpectedPriority:      priority, ExpectedWeight: weight, ApplyPriorityWeight: true,
	}})
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.False(t, outcomes[0].Applied)
}

func TestApplyChannelSmartScheduleRouteResultsCreatesControlRevisionOnFirstUse(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.Where("key = ?", ChannelSmartScheduleControlRevisionOption).Delete(&Option{}).Error)

	priority := int64(5)
	weight := uint(50)
	require.NoError(t, db.Create(&Channel{
		Id: 1702, Name: "scheduler first-use revision", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1702, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1702, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
	}).Error)

	outcomes, err := ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{{
		ChannelId: 1702, Group: "vip", Model: "model-a",
		Status: ChannelSmartScheduleStatusSucceeded, Priority: 10, Weight: 100,
		PoolGuard: true, ExpectedRevision: 1, ExpectedControlRevision: "",
		ExpectedParticipationSet: true, ExpectedAbilityEnabled: true,
		ExpectedChannelStatus: common.ChannelStatusEnabled,
		ExpectedPriority:      priority, ExpectedWeight: weight, ApplyPriorityWeight: true,
	}})
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.True(t, outcomes[0].Applied)

	revision, err := GetChannelSmartScheduleControlRevision()
	require.NoError(t, err)
	assert.Empty(t, revision)
	var optionCount int64
	require.NoError(t, db.Model(&Option{}).
		Where("key = ?", ChannelSmartScheduleControlRevisionOption).
		Count(&optionCount).Error)
	assert.Equal(t, int64(1), optionCount)
}
