package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInitChannelCacheKeepsPreviousSnapshotWhenReloadFails(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	originalGroupRoutes := group2model2channels
	originalChannels := channelsIDM
	originalAdvancedConfigs := channel2advancedCustomConfig
	originalSmartRoutes := channelSmartScheduleRouteCache
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		group2model2channels = originalGroupRoutes
		channelsIDM = originalChannels
		channel2advancedCustomConfig = originalAdvancedConfigs
		channelSmartScheduleRouteCache = originalSmartRoutes
		channelSyncLock.Unlock()
	})

	priority := int64(100)
	require.NoError(t, db.Create(&Channel{
		Id: 9391, Name: "cached before failure", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 9391, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: 100,
	}).Error)
	InitChannelCache()

	require.NoError(t, db.Create(&Channel{
		Id: 9392, Name: "must not leak from partial reload", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority,
	}).Error)
	wantErr := errors.New("ability snapshot failed")
	callbackName := "test:fail_channel_cache_ability_snapshot"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Ability" {
			tx.AddError(wantErr)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	InitChannelCache()

	cached, err := CacheGetChannel(9391)
	require.NoError(t, err)
	assert.Equal(t, "cached before failure", cached.Name)
	_, err = CacheGetChannel(9392)
	require.Error(t, err)
}
