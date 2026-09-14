package upgrade_test

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"
)

func TestSharedVariableUpgradeStartup(t *testing.T) {
	mode := os.Getenv("VARIABLE_UPGRADE_MODE")
	if mode == "" {
		t.Skip("set VARIABLE_UPGRADE_MODE and an isolated SQL_DSN or VARIABLE_UPGRADE_SQLITE_PATH to verify release upgrades")
	}
	require.Contains(t, []string{"seed", "fresh", "verify"}, mode)
	common.IsMasterNode = true
	common.SQLitePath = os.Getenv("VARIABLE_UPGRADE_SQLITE_PATH")
	require.NoError(t, model.InitDB())
	db := model.DB
	db.Logger = db.Logger.LogMode(logger.Silent)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	defer sqlDB.Close()
	if mode == "seed" || mode == "fresh" {
		require.NoError(t, db.Create(&model.User{Id: 912, Username: "migration-user", Password: "test-only-password", Quota: 123456, UsedQuota: 789}).Error)
		require.NoError(t, db.Create(&model.Channel{Id: 913, Name: "迁移保留渠道", Key: "migration-channel-key", Group: "default", Models: "test-model", Status: 1}).Error)
		require.NoError(t, db.Create(&model.Option{Key: "SharedVariablesUpgradeMarker", Value: "preserve-existing-data"}).Error)
		if mode == "seed" {
			return
		}
	}
	var user model.User
	require.NoError(t, db.First(&user, 912).Error)
	assert.Equal(t, "migration-user", user.Username)
	assert.EqualValues(t, 123456, user.Quota)
	assert.EqualValues(t, 789, user.UsedQuota)
	var channel model.Channel
	require.NoError(t, db.First(&channel, 913).Error)
	assert.Equal(t, "迁移保留渠道", channel.Name)
	assert.Equal(t, "migration-channel-key", channel.Key)
	var option model.Option
	require.NoError(t, db.Where(&model.Option{Key: "SharedVariablesUpgradeMarker"}).First(&option).Error)
	assert.Equal(t, "preserve-existing-data", option.Value)
	require.Error(t, db.Create(&model.User{Username: "migration-user", Password: "duplicate"}).Error)
	require.Error(t, db.Create(&model.Channel{Id: 913, Name: "duplicate", Key: "duplicate"}).Error)
	require.True(t, db.Migrator().HasTable("channel_monitor_variable_groups"))
	const config = `{"version":1,"ratio":{"source":"fixed","fixed_value":1},"balance":{"source":"fixed","fixed_value":0},"variable_requests":[{"id":"login","name":"登录","request":{"method":"GET","path":"/token","body_type":"none"},"refresh_policy":"on_failure","response_type":"json","variables":[{"name":"token","value_path":"token","value":"persisted-shared-value"}]}]}`
	var count int64
	require.NoError(t, db.Table("channel_monitor_variable_groups").Count(&count).Error)
	if count == 0 {
		require.NoError(t, db.Table("channel_monitor_variable_groups").Create(map[string]any{"id": 914, "name": "迁移共享配置", "base_url": "https://upstream.example", "config": config, "revision": 1, "request_timeout": 30}).Error)
	}
	var saved struct {
		ID       int
		Name     string
		Config   string
		Revision int64
	}
	require.NoError(t, db.Table("channel_monitor_variable_groups").Where("id = ?", 914).Take(&saved).Error)
	assert.Equal(t, "迁移共享配置", saved.Name)
	assert.Equal(t, config, saved.Config)
	assert.EqualValues(t, 1, saved.Revision)
	require.Error(t, db.Table("channel_monitor_variable_groups").Create(map[string]any{"id": 914, "name": "duplicate", "base_url": "https://upstream.example", "config": config, "revision": 1}).Error)
	t.Logf("%s startup %s: existing data, uniqueness, and shared configuration preserved", db.Dialector.Name(), mode)
}
