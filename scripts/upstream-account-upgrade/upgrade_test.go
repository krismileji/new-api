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

// This fixture also compiles against the latest upstream release. Seed uses
// only released models; verify exercises the real current startup migration.
func TestUpstreamAccountUpgrade(t *testing.T) {
	mode := os.Getenv("ACCOUNT_UPGRADE_MODE")
	if mode == "" {
		t.Skip("set ACCOUNT_UPGRADE_MODE and isolated SQL_DSN / ACCOUNT_UPGRADE_SQLITE_PATH")
	}
	require.Contains(t, []string{"seed", "fresh", "verify"}, mode)
	common.IsMasterNode = true
	common.SQLitePath = os.Getenv("ACCOUNT_UPGRADE_SQLITE_PATH")
	require.NoError(t, model.InitDB())
	require.NoError(t, model.InitLogDB())
	db := model.DB
	db.Logger = db.Logger.LogMode(logger.Silent)
	if mode == "seed" || mode == "fresh" {
		require.NoError(t, db.Create(&model.User{Id: 912, Username: "account-migration-user", Password: "test-only", Quota: 123456, UsedQuota: 789}).Error)
		require.NoError(t, db.Create(&model.Channel{Id: 913, Name: "迁移保留渠道", Key: "migration-key", Group: "default", Models: "test-model", Status: 1}).Error)
		require.NoError(t, db.Create(&model.Option{Key: "UpstreamAccountUpgradeMarker", Value: "preserve-data"}).Error)
		require.NoError(t, model.LOG_DB.Create(&model.Log{Id: 915, UserId: 912, ChannelId: 913, Quota: 42, Content: "迁移保留消费日志"}).Error)
		if mode == "seed" {
			return
		}
	}
	var user model.User
	require.NoError(t, db.First(&user, 912).Error)
	assert.Equal(t, "account-migration-user", user.Username)
	assert.Equal(t, 123456, user.Quota)
	assert.Equal(t, 789, user.UsedQuota)
	var channel model.Channel
	require.NoError(t, db.First(&channel, 913).Error)
	assert.Equal(t, "迁移保留渠道", channel.Name)
	assert.Equal(t, "migration-key", channel.Key)
	var log model.Log
	require.NoError(t, model.LOG_DB.First(&log, 915).Error)
	assert.Equal(t, 42, log.Quota)
	assert.Equal(t, "迁移保留消费日志", log.Content)
	var option model.Option
	require.NoError(t, db.Where(&model.Option{Key: "UpstreamAccountUpgradeMarker"}).First(&option).Error)
	assert.Equal(t, "preserve-data", option.Value)
	require.Error(t, db.Create(&model.User{Username: "account-migration-user", Password: "duplicate"}).Error)
	require.Error(t, db.Create(&model.Channel{Id: 913, Key: "duplicate"}).Error)
	require.True(t, db.Migrator().HasTable("channel_monitor_upstream_accounts"))
	require.True(t, db.Migrator().HasIndex("channel_ratio_monitors", "idx_channel_ratio_monitors_upstream_account_id"))
	var count int64
	require.NoError(t, db.Table("channel_monitor_upstream_accounts").Where("id = ?", 914).Count(&count).Error)
	const settings = `{"UpstreamType":"new_api","UpstreamBaseURL":"https://upstream.example","UpstreamAccessToken":"migration-account-secret"}`
	if count == 0 {
		require.NoError(t, db.Table("channel_monitor_upstream_accounts").Create(map[string]any{"id": 914, "name": "迁移余额池", "settings": settings, "revision": 2, "balance": 21.5, "refresh_interval_minutes": 5}).Error)
		require.NoError(t, db.Table("channel_ratio_monitors").Create(map[string]any{"channel_id": 913, "ratio": 0.25, "upstream_account_id": 914, "upstream_account_revision": 2, "upstream_revision": 3, "upstream_type": "new_api"}).Error)
	}
	var account struct {
		Settings               string
		Balance                float64
		Revision               int64
		RefreshIntervalMinutes int
	}
	require.NoError(t, db.Table("channel_monitor_upstream_accounts").Where("id = ?", 914).Take(&account).Error)
	assert.Equal(t, settings, account.Settings)
	assert.Equal(t, 21.5, account.Balance)
	assert.EqualValues(t, 2, account.Revision)
	assert.Equal(t, 5, account.RefreshIntervalMinutes)
	var monitor struct {
		Ratio                   float64
		UpstreamAccountID       int
		UpstreamAccountRevision int64
	}
	require.NoError(t, db.Table("channel_ratio_monitors").Where("channel_id = ?", 913).Take(&monitor).Error)
	assert.Equal(t, 0.25, monitor.Ratio)
	assert.Equal(t, 914, monitor.UpstreamAccountID)
	assert.EqualValues(t, 2, monitor.UpstreamAccountRevision)
	require.Error(t, db.Table("channel_monitor_upstream_accounts").Create(map[string]any{"id": 914, "name": "duplicate", "settings": settings, "revision": 1}).Error)
	require.Error(t, db.Table("channel_ratio_monitors").Create(map[string]any{"channel_id": 913}).Error)
	t.Logf("%s %s: real startup, account/channel data, unique keys and log data preserved", db.Dialector.Name(), mode)
}
