package controller

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

func TestChannelStatusProbeUsesPhysicalMemberWhenGlobalSwitchIsOff(t *testing.T) {
	previousDB := model.DB
	previousMemoryCache := common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "status-probe-feature.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelLogicalGroup{}, &model.ChannelLogicalGroupMember{}))
	model.DB = db
	common.MemoryCacheEnabled = false
	t.Setenv(model.ChannelLogicalGroupGlobalEnableEnv, "false")
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCache
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	groupID := int64(9600)
	require.NoError(t, db.Create(&model.ChannelLogicalGroup{Id: groupID, Name: "probe-feature"}).Error)
	channels := []model.Channel{
		{Id: 1701, Name: "probe-a", Key: "key-a", Status: common.ChannelStatusEnabled, LogicalChannelID: &groupID},
		{Id: 1702, Name: "probe-b", Key: "key-b", Status: common.ChannelStatusEnabled, LogicalChannelID: &groupID},
	}
	require.NoError(t, db.Create(&channels).Error)
	fingerprint := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	require.NoError(t, db.Create(&[]model.ChannelLogicalGroupMember{
		{LogicalGroupID: groupID, ChannelID: 1701, Weight: 1, AddressFingerprint: fingerprint},
		{LogicalGroupID: groupID, ChannelID: 1702, Weight: 1, AddressFingerprint: fingerprint},
	}).Error)

	identity, snapshot, members, err := resolveChannelStatusProbeMembers(1701)
	require.NoError(t, err)
	assert.EqualValues(t, 1701, identity.LogicalChannelID)
	assert.Zero(t, identity.Revision)
	require.Len(t, snapshot.Members, 1)
	assert.Equal(t, 1701, snapshot.Members[0].ChannelID)
	assert.Contains(t, members, 1701)
	assert.NotContains(t, members, 1702)
}
