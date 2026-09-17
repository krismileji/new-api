package upgrade_test

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Copy this unchanged into the latest release checkout for the seed phase.
func TestTokenProtectionUpgrade(t *testing.T) {
	mode := os.Getenv("TOKEN_UPGRADE_MODE")
	if mode == "" {
		t.Skip("TOKEN_UPGRADE_MODE and an isolated SQL_DSN/SQLITE_PATH are required")
	}
	require.Contains(t, []string{"seed", "fresh", "verify"}, mode)
	common.IsMasterNode = true
	common.SQLitePath = os.Getenv("SQLITE_PATH")
	require.NoError(t, model.InitDB())
	db := model.DB
	connection, err := db.DB()
	require.NoError(t, err)
	defer connection.Close()
	if mode == "seed" || mode == "fresh" {
		require.NoError(t, db.Create(&model.Token{Id: 97901, UserId: 97900, Key: "upgrade-preserve-key", Name: "升级保留令牌", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 12345, UsedQuota: 678}).Error)
		require.NoError(t, db.Create(&model.Option{Key: "TokenProtectionUpgradeMarker", Value: "preserve-existing-option"}).Error)
		if mode == "seed" {
			return
		}
	}
	var token model.Token
	require.NoError(t, db.First(&token, 97901).Error)
	assert.Equal(t, "升级保留令牌", token.Name)
	assert.Equal(t, "upgrade-preserve-key", token.Key)
	assert.Equal(t, 12345, token.RemainQuota)
	assert.Equal(t, 678, token.UsedQuota)
	var marker model.Option
	require.NoError(t, db.Where(&model.Option{Key: "TokenProtectionUpgradeMarker"}).First(&marker).Error)
	assert.Equal(t, "preserve-existing-option", marker.Value)
	require.Error(t, db.Create(&model.Token{Key: token.Key}).Error)
	for _, table := range []string{"token_auto_disable_configs", "token_auto_disable_records"} {
		require.True(t, db.Migrator().HasTable(table), table)
	}
	var count int64
	require.NoError(t, db.Table("token_auto_disable_records").Count(&count).Error)
	if count == 0 {
		require.NoError(t, db.Table("token_auto_disable_records").Create(map[string]any{"id": "upgrade-incident", "token_id": 97901, "user_id": 97900, "response_status": 451, "response_message": "升级后仍然禁用", "released_at": 0}).Error)
	}
	var record struct {
		TokenId         int
		ResponseStatus  int
		ResponseMessage string
		ReleasedAt      int64
	}
	require.NoError(t, db.Table("token_auto_disable_records").Where("id = ?", "upgrade-incident").Take(&record).Error)
	assert.Equal(t, 97901, record.TokenId)
	assert.Equal(t, 451, record.ResponseStatus)
	assert.Equal(t, "升级后仍然禁用", record.ResponseMessage)
	assert.Zero(t, record.ReleasedAt)
	require.Error(t, db.Table("token_auto_disable_records").Create(map[string]any{"id": "upgrade-incident"}).Error)
	t.Logf("%s %s startup: existing token, quota, options, uniqueness and protection record preserved", db.Dialector.Name(), mode)
}
