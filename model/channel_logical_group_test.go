package model

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelLogicalGroupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-logical-group.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ChannelLogicalGroup{}, &ChannelLogicalGroupMember{}))
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, originalLogDatabaseType)
	t.Cleanup(func() {
		DB = originalDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			assert.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func TestChannelLogicalGroupBeforeCreateDefaultsAndValidates(t *testing.T) {
	db := setupChannelLogicalGroupTestDB(t)
	group := &ChannelLogicalGroup{Name: "same-upstream"}
	require.NoError(t, db.Create(group).Error)
	assert.Equal(t, ChannelLogicalGroupStatusEnabled, group.Status)
	assert.EqualValues(t, 1, group.Revision)
	assert.Positive(t, group.CreatedAt)
	assert.Equal(t, group.CreatedAt, group.UpdatedAt)

	invalid := &ChannelLogicalGroup{Name: " "}
	err := db.Create(invalid).Error
	assert.ErrorIs(t, err, ErrChannelLogicalGroupInvalidName)
}

func TestChannelLogicalGroupMemberUniqueChannelAndWeightBound(t *testing.T) {
	db := setupChannelLogicalGroupTestDB(t)
	groupA := &ChannelLogicalGroup{Name: "group-a"}
	groupB := &ChannelLogicalGroup{Name: "group-b"}
	require.NoError(t, db.Create(groupA).Error)
	require.NoError(t, db.Create(groupB).Error)
	fingerprint := strings.Repeat("a", 64)
	member := &ChannelLogicalGroupMember{
		LogicalGroupID: groupA.Id, ChannelID: 101, Weight: 0, AddressFingerprint: fingerprint,
	}
	require.NoError(t, db.Create(member).Error)
	assert.EqualValues(t, groupA.Id, member.LogicalGroupID)

	duplicate := &ChannelLogicalGroupMember{
		LogicalGroupID: groupB.Id, ChannelID: 101, Weight: 1, AddressFingerprint: fingerprint,
	}
	assert.Error(t, db.Create(duplicate).Error, "a physical channel must belong to at most one group")

	tooLarge := &ChannelLogicalGroupMember{
		LogicalGroupID: groupA.Id, ChannelID: 102, Weight: ChannelLogicalGroupMaxMemberWeight + 1, AddressFingerprint: fingerprint,
	}
	assert.ErrorIs(t, db.Create(tooLarge).Error, ErrChannelLogicalGroupInvalidWeight)

	badFingerprint := &ChannelLogicalGroupMember{
		LogicalGroupID: groupA.Id, ChannelID: 103, Weight: 1, AddressFingerprint: "contains-key",
	}
	assert.ErrorIs(t, db.Create(badFingerprint).Error, ErrChannelLogicalGroupInvalidFingerprint)
}

func TestChannelLogicalGroupMemberSetAndRevisionHelpers(t *testing.T) {
	fingerprint := strings.Repeat("b", 64)
	members := []ChannelLogicalGroupMember{
		{LogicalGroupID: 1, ChannelID: 1, Weight: 3, AddressFingerprint: fingerprint},
		{LogicalGroupID: 1, ChannelID: 2, Weight: 1, AddressFingerprint: fingerprint},
	}
	require.NoError(t, ValidateChannelLogicalGroupMembers(members))
	duplicate := append([]ChannelLogicalGroupMember{}, members...)
	duplicate[1].ChannelID = duplicate[0].ChannelID
	assert.ErrorIs(t, ValidateChannelLogicalGroupMembers(duplicate), ErrChannelLogicalGroupDuplicateMember)
	assert.ErrorIs(t, ValidateChannelLogicalGroupMembers(nil), ErrChannelLogicalGroupEmptyMembers)

	weight, err := NormalizeChannelLogicalGroupMemberWeight(nil)
	require.NoError(t, err)
	assert.Equal(t, ChannelLogicalGroupDefaultMemberWeight, weight)
	zero := uint(0)
	weight, err = NormalizeChannelLogicalGroupMemberWeight(&zero)
	require.NoError(t, err)
	assert.Zero(t, weight, "explicit zero weight must remain zero")
	tooLarge := ChannelLogicalGroupMaxMemberWeight + 1
	_, err = NormalizeChannelLogicalGroupMemberWeight(&tooLarge)
	assert.ErrorIs(t, err, ErrChannelLogicalGroupInvalidWeight)

	group := ChannelLogicalGroup{Revision: 4}
	require.NoError(t, group.BumpRevision(123))
	assert.EqualValues(t, 5, group.Revision)
	assert.EqualValues(t, 123, group.UpdatedAt)
	assert.NoError(t, CheckChannelLogicalGroupRevision(5, 5))
	assert.ErrorIs(t, CheckChannelLogicalGroupRevision(6, 5), ErrChannelLogicalGroupRevisionConflict)
	assert.ErrorIs(t, CheckChannelLogicalGroupRevision(1, 0), ErrChannelLogicalGroupRevisionConflict)
	group.Revision = 0
	assert.ErrorIs(t, group.BumpRevision(0), ErrChannelLogicalGroupInvalidRevision)
	group.Revision = int64(^uint64(0) >> 1)
	assert.ErrorIs(t, group.BumpRevision(0), ErrChannelLogicalGroupInvalidRevision)
}
