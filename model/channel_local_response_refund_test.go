package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestChannelSmallInputResponseRefundDatabase(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(filepath.Join(t.TempDir(), "refund.db"))
			case "mysql":
				dsn := os.Getenv("TEST_PROBE_POLICY_MYSQL_DSN")
				if dsn == "" {
					t.Skip("MySQL not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_PROBE_POLICY_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("PostgreSQL not configured")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			oldDB, oldType, oldBatch, oldRedis, oldRDB := DB, common.MainDatabaseType(), common.BatchUpdateEnabled, common.RedisEnabled, common.RDB
			DB = db
			common.SetDatabaseTypes(common.DatabaseType(dialect), common.LogDatabaseType())
			common.BatchUpdateEnabled = false
			common.RedisEnabled = false
			t.Cleanup(func() {
				DB = oldDB
				common.SetDatabaseTypes(oldType, common.LogDatabaseType())
				common.BatchUpdateEnabled = oldBatch
				common.RedisEnabled = oldRedis
				common.RDB = oldRDB
				sqlDB, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.Migrator().DropTable(&ChannelLocalResponseRefund{}, &SubscriptionPreConsumeRecord{}, &UserSubscription{}, &Token{}, &User{}))
			// Existing account data is created before adding the new outbox table.
			require.NoError(t, db.AutoMigrate(&channelProbeReleasedUser{}, &channelProbeReleasedToken{}, &channelProbeReleasedUserSubscription{}, &channelProbeReleasedSubscriptionPreConsumeRecord{}))
			require.NoError(t, db.Create(&channelProbeReleasedUser{Id: 17, Username: "local-refund", Quota: 1000}).Error)
			require.NoError(t, db.Create(&channelProbeReleasedToken{Id: 18, UserId: 17, Key: "local-refund-test", RemainQuota: 1000}).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{}, &ChannelLocalResponseRefund{}))
			}
			for _, batch := range []bool{false, true} {
				t.Run(map[bool]string{false: "direct", true: "pending batch"}[batch], func(t *testing.T) {
					common.BatchUpdateEnabled = batch
					requestID := "refund-direct"
					if batch {
						requestID = "refund-batch"
					}
					if batch {
						addNewRecord(BatchUpdateTypeUserQuota, 17, -125)
						addNewRecord(BatchUpdateTypeTokenQuota, 18, -125)
					} else {
						ok, err := TryReserveUserQuota(17, 125)
						require.NoError(t, err)
						require.True(t, ok)
						ok, err = TryReserveTokenQuota(18, "local-refund-test", 125, false)
						require.NoError(t, err)
						require.True(t, ok)
					}
					refund := ChannelLocalResponseRefund{RequestID: requestID, UserID: 17, TokenID: 18, WalletQuota: 125, TokenQuota: 125}
					require.NoError(t, QueueChannelLocalResponseRefund(t.Context(), &refund))
					var user User
					require.NoError(t, db.First(&user, 17).Error)
					assert.Equal(t, 875, user.Quota, "queued intent has a durable debit")
					require.Empty(t, batchUpdateStores[BatchUpdateTypeUserQuota])
					for range 2 {
						require.NoError(t, ApplyChannelLocalResponseRefund(t.Context(), requestID))
					}
					require.NoError(t, db.First(&user, 17).Error)
					assert.Equal(t, 1000, user.Quota)
					var token Token
					require.NoError(t, db.First(&token, 18).Error)
					assert.Equal(t, 1000, token.RemainQuota)
					assert.Zero(t, token.UsedQuota)
					conflict := ChannelLocalResponseRefund{RequestID: requestID, UserID: 17, WalletQuota: 126}
					require.Error(t, QueueChannelLocalResponseRefund(t.Context(), &conflict))
				})
			}
			common.BatchUpdateEnabled = false
			require.NoError(t, db.Create(&UserSubscription{Id: 19, UserId: 17, AmountTotal: 1000, AmountUsed: 150}).Error)
			require.NoError(t, db.Create(&SubscriptionPreConsumeRecord{RequestId: "refund-sub", UserId: 17, UserSubscriptionId: 19, PreConsumed: 100, Status: "consumed"}).Error)
			refund := ChannelLocalResponseRefund{RequestID: "refund-sub", UserID: 17, SubscriptionID: 19, SubscriptionQuota: 150}
			require.NoError(t, QueueChannelLocalResponseRefund(t.Context(), &refund))
			require.NoError(t, ApplyChannelLocalResponseRefund(t.Context(), refund.RequestID))
			require.NoError(t, ApplyChannelLocalResponseRefund(t.Context(), refund.RequestID))
			var sub UserSubscription
			require.NoError(t, db.First(&sub, 19).Error)
			assert.Zero(t, sub.AmountUsed, "initial and extra subscription reservations refunded")
			// A missing token rolls the wallet mutation back; the pending record can
			// be retried after restoring the token, without a duplicate wallet credit.
			require.NoError(t, db.Model(&User{}).Where("id = ?", 17).Update("quota", 900).Error)
			refund = ChannelLocalResponseRefund{RequestID: "refund-retry", UserID: 17, TokenID: 20, WalletQuota: 100, TokenQuota: 100}
			require.NoError(t, QueueChannelLocalResponseRefund(t.Context(), &refund))
			require.Error(t, ApplyChannelLocalResponseRefund(t.Context(), refund.RequestID))
			var user User
			require.NoError(t, db.First(&user, 17).Error)
			assert.Equal(t, 900, user.Quota)
			require.NoError(t, db.Create(&Token{Id: 20, UserId: 17, Key: "refund-retry", RemainQuota: 900, UsedQuota: 100}).Error)
			require.NoError(t, ApplyChannelLocalResponseRefund(t.Context(), refund.RequestID))
			require.NoError(t, db.First(&user, 17).Error)
			assert.Equal(t, 1000, user.Quota)
			if address := os.Getenv("TEST_PROBE_POLICY_REDIS_ADDR"); address != "" {
				client := redis.NewClient(&redis.Options{Addr: address, DB: 14})
				require.NoError(t, client.Ping(t.Context()).Err())
				t.Cleanup(func() { require.NoError(t, client.Close()) })
				common.RDB = client
				common.RedisEnabled = true
				userKey, tokenKey := getUserCacheKey(17), getTokenCacheKey("local-refund-test")
				require.NoError(t, client.HSet(t.Context(), userKey, "Id", 17, "Quota", 900).Err())
				require.NoError(t, client.HSet(t.Context(), tokenKey, "Id", 18, "RemainQuota", 900, "UsedQuota", 100).Err())
				t.Cleanup(func() { client.Del(t.Context(), userKey, tokenKey) })
				require.NoError(t, db.Model(&User{}).Where("id = ?", 17).Update("quota", 900).Error)
				require.NoError(t, db.Model(&Token{}).Where("id = ?", 18).Updates(map[string]any{"remain_quota": 900, "used_quota": 100}).Error)
				refund = ChannelLocalResponseRefund{RequestID: "refund-cache-" + common.GetUUID(), UserID: 17, TokenID: 18, WalletQuota: 100, TokenQuota: 100}
				require.NoError(t, QueueChannelLocalResponseRefund(t.Context(), &refund))
				require.NoError(t, ApplyChannelLocalResponseRefund(t.Context(), refund.RequestID))
				require.NoError(t, ApplyChannelLocalResponseRefund(t.Context(), refund.RequestID))
				assert.Equal(t, "1000", client.HGet(t.Context(), userKey, "Quota").Val())
				assert.Equal(t, "1000", client.HGet(t.Context(), tokenKey, "RemainQuota").Val())
				assert.Equal(t, "0", client.HGet(t.Context(), tokenKey, "UsedQuota").Val())
				require.NoError(t, client.Del(t.Context(), "local_response_refund:user:"+refund.RequestID, "local_response_refund:token:"+refund.RequestID).Err())
			}
		})
	}
}
