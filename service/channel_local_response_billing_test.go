package service

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelSmallInputResponseBillingSessionClosesOnceAfterCancellation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "session.db")), &gorm.Config{})
	require.NoError(t, err)
	previous, redisEnabled, batchEnabled, dbType := model.DB, common.RedisEnabled, common.BatchUpdateEnabled, common.MainDatabaseType()
	model.DB = db
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.LogDatabaseType())
	t.Cleanup(func() {
		model.DB = previous
		common.RedisEnabled = redisEnabled
		common.BatchUpdateEnabled = batchEnabled
		common.SetDatabaseTypes(dbType, common.LogDatabaseType())
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.ChannelLocalResponseRefund{}))
	require.NoError(t, db.Create(&model.User{Id: 71, Username: "billing-local", Quota: 1000}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 72, UserId: 71, Key: "local-billing", RemainQuota: 1000}).Error)
	info := &relaycommon.RelayInfo{UserId: 71, TokenId: 72, TokenKey: "local-billing", RequestId: "local-session", ForcePreConsume: true}
	session := &BillingSession{relayInfo: info, funding: &WalletFunding{userId: 71, directQuota: true}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.Nil(t, session.preConsume(c, 100))
	require.NoError(t, session.Reserve(150))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, session.FinishWithoutCharge(ctx))
	require.NoError(t, session.FinishWithoutCharge(ctx))
	session.Refund(c)
	var user model.User
	require.NoError(t, db.First(&user, 71).Error)
	assert.Equal(t, 1000, user.Quota)
	var token model.Token
	require.NoError(t, db.First(&token, 72).Error)
	assert.Equal(t, 1000, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
	var count int64
	require.NoError(t, db.Model(&model.ChannelLocalResponseRefund{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}
