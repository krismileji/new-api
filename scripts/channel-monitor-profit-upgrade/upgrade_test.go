package upgrade_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Copy this test unchanged to the release checkout and run mode=seed there;
// then run mode=verify in the new checkout against the same databases.
func TestChannelMonitorProfitUpgrade(t *testing.T) {
	mode := os.Getenv("PROFIT_UPGRADE_MODE")
	if mode == "" {
		t.Skip("requires isolated profit verification databases")
	}
	require.Contains(t, []string{"seed", "fresh", "verify"}, mode)
	require.True(t, strings.Contains(os.Getenv("SQL_DSN"), "new_api_profit_") || strings.Contains(os.Getenv("PROFIT_SQLITE_PATH"), "profit-validation"))
	common.IsMasterNode = true
	common.SQLitePath = os.Getenv("PROFIT_SQLITE_PATH")
	require.NoError(t, model.InitDB())
	db := model.DB
	mainSQL, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, mainSQL.Close()) })
	// SQLite's local DSN reads SQLitePath for both configured databases.
	if db.Dialector.Name() == "sqlite" {
		common.SQLitePath = filepath.Join(filepath.Dir(common.SQLitePath), "log-"+filepath.Base(common.SQLitePath))
		t.Setenv("LOG_SQL_DSN", "local")
	}
	require.NoError(t, model.InitLogDB())
	logSQL, err := model.LOG_DB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, logSQL.Close()) })
	if mode != "verify" {
		require.NoError(t, db.Create(&model.User{Id: 9711, Username: "profit-upgrade", Password: "fixture", AffCode: "profit-upgrade", Quota: 123456, UsedQuota: 789}).Error)
		require.NoError(t, db.Create(&model.Channel{Id: 9712, Name: "利润迁移渠道", Key: "fixture", Status: 1}).Error)
		require.NoError(t, model.LOG_DB.Create(&model.Log{Id: 9713, UserId: 9711, ChannelId: 9712, Quota: 789, Content: "利润迁移日志", CreatedAt: 1750000000}).Error)
	}
	var user model.User
	require.NoError(t, db.First(&user, 9711).Error)
	assert.EqualValues(t, 123456, user.Quota)
	assert.EqualValues(t, 789, user.UsedQuota)
	var channel model.Channel
	require.NoError(t, db.First(&channel, 9712).Error)
	assert.Equal(t, "利润迁移渠道", channel.Name)
	var log model.Log
	require.NoError(t, model.LOG_DB.First(&log, 9713).Error)
	assert.Equal(t, "利润迁移日志", log.Content)
	assert.Equal(t, 789, log.Quota)
	assert.True(t, db.Migrator().HasIndex(&model.User{}, "Username"))
	assert.True(t, model.LOG_DB.Migrator().HasIndex(&model.Log{}, "idx_created_at_id"))
	assert.Error(t, db.Create(&model.User{Username: "profit-upgrade", Password: "duplicate", AffCode: "duplicate"}).Error)
	if mode != "seed" {
		for _, column := range []string{"funding_delta", "funding_token_id", "funding_subscription_id"} {
			assert.True(t, db.Migrator().HasColumn("channel_monitor_incomes", column), column)
		}
		assert.True(t, db.Migrator().HasTable("channel_monitor_incomes"))
		assert.True(t, db.Migrator().HasTable("channel_monitor_income_gaps"))
		assert.True(t, db.Migrator().HasIndex("channel_monitor_income_gaps", "idx_channel_monitor_income_gaps_gap_key"))
		assert.True(t, db.Migrator().HasIndex("channel_monitor_incomes", "idx_channel_monitor_incomes_settlement_key"))
		var startedAt int64
		require.NoError(t, db.Table("channel_monitor_income_states").Where("id = 1").Pluck("started_at", &startedAt).Error)
		assert.Positive(t, startedAt)
	}
	versionSQL := "SELECT version()"
	if db.Dialector.Name() == "sqlite" {
		versionSQL = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
	t.Logf("mode=%s database=%s version=%s; main/log data, indexes and uniqueness preserved", mode, db.Dialector.Name(), version)
}
