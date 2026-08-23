package service

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupLogicalChannelGroupServiceDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	previousMain := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "logical-group-service.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{}, &model.ChannelLogicalGroup{}, &model.ChannelLogicalGroupMember{},
		&model.ChannelModelDetectionConfig{}, &model.ChannelModelDetectionTarget{},
		&model.ChannelModelDetectionLogicalConfig{}, &model.ChannelModelDetectionLogicalTarget{},
	))
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, previousLog)
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetDatabaseTypes(previousMain, previousLog)
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			assert.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func logicalGroupTestChannel(id int, address, name string) *model.Channel {
	return &model.Channel{Id: id, Type: 1, Key: "secret-key-" + name, Name: name, Status: common.ChannelStatusEnabled, BaseURL: &address}
}

func TestLogicalChannelGroupServiceLifecycleAndRevision(t *testing.T) {
	db := setupLogicalChannelGroupServiceDB(t)
	address := "https://API.Example.com:443/v1/"
	address2 := "https://api.example.com/v1"
	c1 := logicalGroupTestChannel(101, address, "first")
	c2 := logicalGroupTestChannel(102, address2, "second")
	c3Address := "https://other.example.com/v1"
	c3 := logicalGroupTestChannel(103, c3Address, "other")
	require.NoError(t, db.Create(c1).Error)
	require.NoError(t, db.Create(c2).Error)
	require.NoError(t, db.Create(c3).Error)

	zero := uint(0)
	group, err := CreateLogicalChannelGroup("same upstream", "remark", 0, []LogicalChannelGroupMemberInput{
		{ChannelID: c1.Id}, {ChannelID: c2.Id, Weight: &zero},
	})
	require.NoError(t, err)
	require.Len(t, group.Members, 2)
	assert.EqualValues(t, 1, group.Members[0].Weight)
	assert.Zero(t, group.Members[1].Weight)
	assert.Equal(t, group.Members[0].AddressFingerprint, group.Members[1].AddressFingerprint)

	var stored model.Channel
	require.NoError(t, db.First(&stored, c1.Id).Error)
	assert.Equal(t, group.ID, *stored.LogicalChannelID)

	_, err = ReplaceLogicalChannelGroupMembers(group.ID, group.Revision+99, []LogicalChannelGroupMemberInput{{ChannelID: c1.Id}})
	assert.ErrorIs(t, err, model.ErrChannelLogicalGroupRevisionConflict)

	replacement, err := ReplaceLogicalChannelGroupMembers(group.ID, group.Revision, []LogicalChannelGroupMemberInput{{ChannelID: c2.Id}})
	require.NoError(t, err)
	assert.EqualValues(t, group.Revision+1, replacement.Revision)
	var old model.Channel
	require.NoError(t, db.First(&old, c1.Id).Error)
	assert.Nil(t, old.LogicalChannelID, "removed member must be unassigned")
	stored = model.Channel{}
	require.NoError(t, db.First(&stored, c2.Id).Error)
	assert.Equal(t, replacement.ID, *stored.LogicalChannelID)

	_, err = CreateLogicalChannelGroup("other", "", 0, []LogicalChannelGroupMemberInput{{ChannelID: c2.Id}})
	assert.ErrorIs(t, err, ErrLogicalChannelGroupAlreadyGrouped)

	groupID := replacement.ID
	require.NoError(t, DeleteLogicalChannelGroup(groupID, replacement.Revision))
	var count int64
	db.Model(&model.ChannelLogicalGroup{}).Where("id = ?", groupID).Count(&count)
	assert.Zero(t, count)
	stored = model.Channel{}
	require.NoError(t, db.First(&stored, c2.Id).Error)
	assert.Nil(t, stored.LogicalChannelID)
	assert.Equal(t, "secret-key-second", stored.Key, "deleting a group must not delete the physical channel")
}

func TestLogicalChannelGroupServiceRejectsAddressMismatchAndEmptyMembers(t *testing.T) {
	db := setupLogicalChannelGroupServiceDB(t)
	a := "https://one.example.com/v1"
	b := "https://two.example.com/v1"
	require.NoError(t, db.Create(logicalGroupTestChannel(201, a, "one")).Error)
	require.NoError(t, db.Create(logicalGroupTestChannel(202, b, "two")).Error)
	_, err := CreateLogicalChannelGroup("bad", "", 0, []LogicalChannelGroupMemberInput{{ChannelID: 201}, {ChannelID: 202}})
	assert.ErrorIs(t, err, ErrLogicalChannelGroupAddressMismatch)
	_, err = CreateLogicalChannelGroup("empty", "", 0, nil)
	assert.ErrorIs(t, err, model.ErrChannelLogicalGroupEmptyMembers)
}
