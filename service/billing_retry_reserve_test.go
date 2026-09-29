package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestWalletTrustRequiresEstimateCoverageDatabaseMatrix(t *testing.T) {
	previousUnit, previousTrust := common.QuotaPerUnit, operation_setting.GetQuotaSetting().TrustQuotaUSD
	previousRedis, previousBatch := common.RedisEnabled, common.BatchUpdateEnabled
	common.QuotaPerUnit, common.RedisEnabled, common.BatchUpdateEnabled = 500_000, false, false
	t.Cleanup(func() {
		common.QuotaPerUnit, operation_setting.GetQuotaSetting().TrustQuotaUSD = previousUnit, previousTrust
		common.RedisEnabled, common.BatchUpdateEnabled = previousRedis, previousBatch
	})
	for _, dialect := range []struct {
		kind common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "TEST_BILLING_MYSQL_DSN"},
		{common.DatabaseTypePostgreSQL, "TEST_BILLING_POSTGRES_DSN"},
	} {
		t.Run(string(dialect.kind), func(t *testing.T) {
			var driver gorm.Dialector = sqlite.Open(":memory:")
			if dialect.env != "" {
				dsn := os.Getenv(dialect.env)
				if dsn == "" {
					t.Skip(dialect.env + " is not configured")
				}
				if dialect.kind == common.DatabaseTypeMySQL {
					driver = mysql.Open(dsn)
				} else {
					driver = postgres.Open(dsn)
				}
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{
				TablePrefix: fmt.Sprintf("trust_%x_", time.Now().UnixNano()),
			}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			previousDB, previousType := model.DB, common.MainDatabaseType()
			model.DB = db
			common.SetMainDatabaseType(dialect.kind)
			t.Cleanup(func() { model.DB = previousDB; common.SetMainDatabaseType(previousType) })
			tables := []any{&model.User{}, &model.Token{}, &model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}}
			require.NoError(t, db.AutoMigrate(tables...))
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
			query := "SELECT version()"
			if dialect.kind == common.DatabaseTypeSQLite {
				query = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database: %s", version)

			for index, tc := range []struct {
				name                    string
				threshold               float64
				wallet, estimate, held  int
				reject, force, fallback bool
			}{
				{name: "trusted wallet below estimate", threshold: 10, wallet: 5_500_000, estimate: 6_000_000, reject: true},
				{name: "custom trust threshold below estimate", threshold: 1, wallet: 1_000_000, estimate: 1_500_000, reject: true},
				{name: "disabled trust below estimate", wallet: 5_500_000, estimate: 6_000_000, reject: true},
				{name: "trusted wallet covers estimate", threshold: 10, wallet: 5_500_000, estimate: 5_000_000},
				{name: "trusted wallet equals estimate", threshold: 10, wallet: 5_500_000, estimate: 5_500_000},
				{name: "wallet at threshold still reserves", threshold: 10, wallet: 5_000_000, estimate: 500_000, held: 500_000},
				{name: "disabled trust still reserves", wallet: 5_500_000, estimate: 500_000, held: 500_000},
				{name: "forced request still reserves", threshold: 10, wallet: 5_500_000, estimate: 500_000, held: 500_000, force: true},
				{name: "empty wallet rejects zero estimate", threshold: 10, reject: true},
				{name: "wallet first falls back to subscription", threshold: 10, wallet: 5_500_000, estimate: 6_000_000, held: 6_000_000, fallback: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					operation_setting.GetQuotaSetting().TrustQuotaUSD = tc.threshold
					user := model.User{Username: fmt.Sprintf("trust-user-%d", index), AffCode: fmt.Sprintf("trust-%d", index), Quota: tc.wallet, Status: common.UserStatusEnabled}
					require.NoError(t, db.Create(&user).Error)
					token := model.Token{UserId: user.Id, Key: fmt.Sprintf("trust-token-%d", index), RemainQuota: 20_000_000, Status: common.TokenStatusEnabled}
					require.NoError(t, db.Create(&token).Error)
					info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: token.Id, TokenKey: token.Key, ForcePreConsume: tc.force, RequestId: fmt.Sprintf("trust-request-%d", index), UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
					var subscription model.UserSubscription
					if tc.fallback {
						plan := model.SubscriptionPlan{Title: "Trust fallback", QuotaResetPeriod: model.SubscriptionResetNever}
						require.NoError(t, db.Create(&plan).Error)
						model.InvalidateSubscriptionPlanCache(plan.Id)
						t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })
						subscription = model.UserSubscription{UserId: user.Id, PlanId: plan.Id, AmountTotal: 20_000_000, Status: "active", EndTime: time.Now().Add(time.Hour).Unix()}
						require.NoError(t, db.Create(&subscription).Error)
						info.UserSetting.BillingPreference = "wallet_first"
					}
					ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
					ctx.Set("token_quota", token.RemainQuota)
					session, apiErr := NewBillingSession(ctx, info, tc.estimate)
					if tc.reject {
						assert.Nil(t, session)
						require.NotNil(t, apiErr)
						assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
						assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
					} else {
						require.Nil(t, apiErr)
						require.NotNil(t, session)
					}
					assert.Equal(t, tc.held, info.FinalPreConsumedQuota)
					require.NoError(t, db.First(&user, user.Id).Error)
					require.NoError(t, db.First(&token, token.Id).Error)
					assert.Equal(t, 20_000_000-tc.held, token.RemainQuota)
					assert.Equal(t, tc.held, token.UsedQuota)
					if tc.fallback {
						assert.Equal(t, BillingSourceSubscription, info.BillingSource)
						assert.Equal(t, tc.wallet, user.Quota)
						require.NoError(t, db.First(&subscription, subscription.Id).Error)
						assert.Equal(t, int64(tc.held), subscription.AmountUsed)
						return
					}
					assert.Equal(t, tc.wallet-tc.held, user.Quota)
				})
			}
		})
	}
}

func TestBillingSessionReserveRejectsWalletArrearsForForcedPreConsume(t *testing.T) {
	truncate(t)
	const userID = 702
	seedUser(t, userID, 20_000)

	info := &relaycommon.RelayInfo{UserId: userID, IsPlayground: true, ForcePreConsume: true}
	session := &BillingSession{
		relayInfo:        info,
		funding:          &WalletFunding{userId: userID, consumed: 50_000},
		preConsumedQuota: 50_000,
	}
	err := session.Reserve(100_000)
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.Equal(t, 50_000, session.GetPreConsumedQuota())
	userQuota, queryErr := model.GetUserQuota(userID, false)
	require.NoError(t, queryErr)
	assert.Equal(t, 20_000, userQuota)
}

func TestForcedWalletReserveUsesAtomicDatabaseBalanceWithBatchUpdates(t *testing.T) {
	truncate(t)
	const userID = 703
	seedUser(t, userID, 100)

	previousBatchUpdate := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() {
		common.BatchUpdateEnabled = previousBatchUpdate
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		IsPlayground:    true,
		ForcePreConsume: true,
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
	}
	session, apiErr := NewBillingSession(c, info, 80)
	require.Nil(t, apiErr)
	require.NotNil(t, session)

	userQuota, err := model.GetUserQuota(userID, true)
	require.NoError(t, err)
	assert.Equal(t, 20, userQuota)

	require.NoError(t, session.Reserve(90))
	userQuota, err = model.GetUserQuota(userID, true)
	require.NoError(t, err)
	assert.Equal(t, 10, userQuota)

	err = session.Reserve(110)
	require.Error(t, err)
	var reserveErr *types.NewAPIError
	require.ErrorAs(t, err, &reserveErr)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, reserveErr.GetErrorCode())
	assert.Equal(t, 90, session.GetPreConsumedQuota())
	userQuota, queryErr := model.GetUserQuota(userID, true)
	require.NoError(t, queryErr)
	assert.Equal(t, 10, userQuota)
}

func TestWalletPreConsumeUsesAtomicDatabaseBalanceWithBatchUpdates(t *testing.T) {
	truncate(t)
	const userID = 705
	seedUser(t, userID, 100)

	previousBatchUpdate := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() {
		common.BatchUpdateEnabled = previousBatchUpdate
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:       userID,
		IsPlayground: true,
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
	}
	session := &BillingSession{
		relayInfo: info,
		funding:   &WalletFunding{userId: userID},
	}
	apiErr := session.preConsume(c, 110)
	var reserveErr *types.NewAPIError
	require.ErrorAs(t, apiErr, &reserveErr)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, reserveErr.GetErrorCode())
	assert.Equal(t, 0, session.GetPreConsumedQuota())
	userQuota, err := model.GetUserQuota(userID, true)
	require.NoError(t, err)
	assert.Equal(t, 100, userQuota)
}

func TestForcedWalletPreConsumeIncludesPendingBatchDeductions(t *testing.T) {
	truncate(t)
	const userID = 704
	seedUser(t, userID, 100)

	previousBatchUpdate := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() {
		common.BatchUpdateEnabled = previousBatchUpdate
	})
	require.NoError(t, model.DecreaseUserQuota(userID, 60, false))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		IsPlayground:    true,
		ForcePreConsume: true,
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
	}
	session, apiErr := NewBillingSession(c, info, 50)

	assert.Nil(t, session)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	userQuota, err := model.GetUserQuota(userID, true)
	require.NoError(t, err)
	assert.Equal(t, 100, userQuota)
}
