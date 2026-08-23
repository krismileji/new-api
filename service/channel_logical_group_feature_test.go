package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpdateLogicalChannelGroupStatusUsesRevisionAndRestoresPhysicalIdentity(t *testing.T) {
	db := setupLogicalChannelGroupServiceDB(t)
	t.Setenv(model.ChannelLogicalGroupGlobalEnableEnv, "true")
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCache })

	address := "https://api.example.com/v1"
	require.NoError(t, db.Create(logicalGroupTestChannel(1301, address, "feature-a")).Error)
	require.NoError(t, db.Create(logicalGroupTestChannel(1302, address, "feature-b")).Error)
	group, err := CreateLogicalChannelGroup("feature-status", "", 0, []LogicalChannelGroupMemberInput{
		{ChannelID: 1301}, {ChannelID: 1302},
	})
	require.NoError(t, err)

	disabled, err := UpdateLogicalChannelGroupStatus(group.ID, group.Revision, model.ChannelLogicalGroupStatusDisabled)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelLogicalGroupStatusDisabled, disabled.Status)
	assert.Equal(t, group.Revision+1, disabled.Revision)
	assert.Len(t, disabled.Members, 2)

	identity, err := model.ResolveChannelLogicalIdentity(1301)
	require.NoError(t, err)
	assert.EqualValues(t, 1301, identity.LogicalChannelID)
	assert.Zero(t, identity.Revision)

	_, err = UpdateLogicalChannelGroupStatus(group.ID, group.Revision, model.ChannelLogicalGroupStatusEnabled)
	assert.ErrorIs(t, err, model.ErrChannelLogicalGroupRevisionConflict)

	reenabled, err := UpdateLogicalChannelGroupStatus(group.ID, disabled.Revision, model.ChannelLogicalGroupStatusEnabled)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelLogicalGroupStatusEnabled, reenabled.Status)
	identity, err = model.ResolveChannelLogicalIdentity(1301)
	require.NoError(t, err)
	assert.EqualValues(t, group.ID, identity.LogicalChannelID)
	assert.Equal(t, reenabled.Revision, identity.Revision)
}

func TestUpdateLogicalChannelGroupStatusRejectsInvalidStatus(t *testing.T) {
	setupLogicalChannelGroupServiceDB(t)
	_, err := UpdateLogicalChannelGroupStatus(1, 1, 99)
	assert.ErrorIs(t, err, model.ErrChannelLogicalGroupInvalidStatus)
}

func TestChannelModelDetectionIdentityUsesPhysicalChannelWhenGlobalSwitchIsOff(t *testing.T) {
	db := setupLogicalChannelGroupServiceDB(t)
	address := "https://api.example.com/v1"
	require.NoError(t, db.Create(logicalGroupTestChannel(1601, address, "detector-a")).Error)
	require.NoError(t, db.Create(logicalGroupTestChannel(1602, address, "detector-b")).Error)
	group, err := CreateLogicalChannelGroup("detector-feature", "", 0, []LogicalChannelGroupMemberInput{
		{ChannelID: 1601}, {ChannelID: 1602},
	})
	require.NoError(t, err)
	t.Setenv(model.ChannelLogicalGroupGlobalEnableEnv, "false")

	identity, err := channelModelDetectionLogicalIdentity(db.Session(&gorm.Session{}), 1601, false)
	require.NoError(t, err)
	assert.EqualValues(t, 1601, identity.LogicalChannelID)
	assert.Zero(t, identity.Revision)

	var memberCount int64
	require.NoError(t, db.Model(&model.ChannelLogicalGroupMember{}).Where("logical_group_id = ?", group.ID).Count(&memberCount).Error)
	assert.EqualValues(t, 2, memberCount)
}
