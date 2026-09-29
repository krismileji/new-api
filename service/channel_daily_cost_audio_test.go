package service

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/types"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// External DSNs must point at isolated test databases, as in the fixed-price
// accounting matrix. Logs use a separate database for every dialect.
func TestAudioChannelDailyCostUsageAuthorityDatabaseMatrix(t *testing.T) {
	previousUnit := common.QuotaPerUnit
	previousRedis, previousBatch, previousLog := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	previousIncome := model.ChannelMonitorIncomeReady.Swap(false)
	common.QuotaPerUnit, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = 500_000, false, false, true
	t.Cleanup(func() {
		common.QuotaPerUnit = previousUnit
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = previousRedis, previousBatch, previousLog
		model.ChannelMonitorIncomeReady.Store(previousIncome)
	})
	for _, dialect := range []struct {
		kind common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "TEST_FIXED_MYSQL"},
		{common.DatabaseTypePostgreSQL, "TEST_FIXED_POSTGRES"},
	} {
		t.Run(string(dialect.kind), func(t *testing.T) {
			var databases [2]*gorm.DB
			for index, suffix := range []string{"_DSN", "_LOG_DSN"} {
				var driver gorm.Dialector = sqlite.Open(":memory:")
				if dialect.env != "" {
					dsn := os.Getenv(dialect.env + suffix)
					if dsn == "" {
						t.Skip(dialect.env + suffix + " is not configured")
					}
					if dialect.kind == common.DatabaseTypeMySQL {
						driver = mysql.Open(dsn)
					} else {
						driver = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
					}
				}
				db, err := gorm.Open(driver, &gorm.Config{})
				require.NoError(t, err)
				sqlDB, err := db.DB()
				require.NoError(t, err)
				sqlDB.SetMaxOpenConns(1)
				t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
				databases[index] = db
			}
			db, logDB := databases[0], databases[1]
			previousDB, previousLogDB := model.DB, model.LOG_DB
			previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
			model.DB, model.LOG_DB = db, logDB
			common.SetDatabaseTypes(dialect.kind, dialect.kind)
			ResetChannelDailyCostSnapshotCache()
			resetChannelDailyCostBatcherForTest(channelDailyCostBatcherConfig{
				MaxPending: 64, MaxBatchSize: 16, FlushInterval: time.Hour,
				DBTimeout: time.Second, MaxAttempts: 3,
			}, model.AddChannelDailyCostBatch)
			t.Cleanup(func() {
				resetChannelDailyCostBatcherForTest(defaultChannelDailyCostBatcherConfig(), model.AddChannelDailyCostBatch)
				ResetChannelDailyCostSnapshotCache()
				model.DB, model.LOG_DB = previousDB, previousLogDB
				common.SetDatabaseTypes(previousMainType, previousLogType)
			})
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.ChannelRatioMonitor{}, &model.ChannelDailyCost{}, &model.ChannelDailyAPIKeyCost{}))
			require.NoError(t, logDB.AutoMigrate(&model.Log{}))
			query := "SELECT version()"
			if dialect.kind == common.DatabaseTypeSQLite {
				query = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database: %s", version)

			const flat = `tier("request", fixed(0.01))`
			const mixed = `len <= 32000 ? tier("short", fixed(0.01)) : tier("long", p * 2)`
			const startingQuota = 2_000_000
			for index, tc := range []struct {
				name, expression string
				usage            *dto.Usage
				estimate, quota  int
				cost             int64
				unresolved       bool
			}{
				{name: "missing usage keeps fixed expression cost unresolved", expression: flat, quota: 5000, unresolved: true},
				{name: "missing usage keeps estimated token fallback unresolved", expression: mixed, estimate: 50000, quota: 50000, unresolved: true},
				{name: "reported zero usage resolves fixed expression cost", expression: flat, usage: &dto.Usage{}, quota: 5000, cost: 10_000_000},
				{name: "estimated usage remains unresolved", expression: mixed, usage: &dto.Usage{PromptTokens: 50000, TotalTokens: 50000, BillingUsage: &dto.BillingUsage{Estimated: true}}, quota: 50000, unresolved: true},
				{name: "reported usage resolves token fallback cost", expression: mixed, usage: &dto.Usage{PromptTokens: 50000, TotalTokens: 50000}, quota: 50000, cost: 100_000_000},
				{name: "legacy per-call cost does not require usage", estimate: 10, quota: 5000, cost: 10_000_000},
			} {
				t.Run(tc.name, func(t *testing.T) {
					user := model.User{Username: fmt.Sprintf("audio_cost_%d", index), Quota: startingQuota, Status: common.UserStatusEnabled}
					require.NoError(t, db.Create(&user).Error)
					token := model.Token{UserId: user.Id, Key: fmt.Sprintf("audio-cost-test-%d", index), RemainQuota: startingQuota, Status: common.TokenStatusEnabled}
					require.NoError(t, db.Create(&token).Error)
					channel := model.Channel{Name: "audio-cost", Key: "unused", Status: common.ChannelStatusEnabled}
					require.NoError(t, db.Create(&channel).Error)
					t.Cleanup(func() {
						require.NoError(t, logDB.Where("user_id = ?", user.Id).Delete(&model.Log{}).Error)
						for _, table := range []any{&model.ChannelRatioMonitor{}, &model.ChannelDailyCost{}, &model.ChannelDailyAPIKeyCost{}} {
							require.NoError(t, db.Where("channel_id = ?", channel.Id).Delete(table).Error)
						}
						require.NoError(t, db.Unscoped().Delete(&token).Error)
						require.NoError(t, db.Unscoped().Delete(&user).Error)
						require.NoError(t, db.Unscoped().Delete(&channel).Error)
					})
					createChannelDailyCostMonitor(t, db, channel.Id, 0.2)
					ctx := newChannelDailyCostTestContext()
					CaptureChannelDailyCostSnapshot(ctx, channel.Id)
					BeginChannelDailyCostAttempt(ctx, channel.Id)
					info := &relaycommon.RelayInfo{
						UserId: user.Id, TokenId: token.Id, TokenKey: token.Key,
						ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channel.Id},
						OriginModelName: "gpt-4o-audio-test", UsingGroup: "default", UserGroup: "default",
						UserSetting:     dto.UserSetting{BillingPreference: "wallet_only"},
						ForcePreConsume: true, StartTime: time.Now(), RelayFormat: relaytypes.RelayFormatOpenAI,
						PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
					}
					info.SetEstimatePromptTokens(tc.estimate)
					if tc.expression != "" {
						info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{
							BillingMode: "tiered_expr", ExprString: tc.expression, ExprHash: billingexpr.ExprHashString(tc.expression),
							QuotaPerUnit: common.QuotaPerUnit, GroupRatio: 1, EstimatedQuotaAfterGroup: tc.quota,
						}
					} else {
						info.PriceData.UsePrice, info.PriceData.ModelPrice = true, 0.01
					}
					require.Nil(t, PreConsumeBilling(ctx, tc.quota, info))
					PostAudioConsumeQuota(ctx, info, tc.usage, "")
					flushChannelDailyCostEvents(t)

					var cost model.ChannelDailyCost
					require.NoError(t, db.Where("channel_id = ?", channel.Id).Take(&cost).Error)
					assert.Equal(t, tc.cost, cost.CostNanoCNY)
					if tc.unresolved {
						assert.Equal(t, int64(1), cost.UnresolvedCount)
						assert.Zero(t, cost.SettledCount)
						assert.Nil(t, ChannelDailyCostAttemptSettledCost(ctx, channel.Id))
					} else {
						assert.Zero(t, cost.UnresolvedCount)
						assert.Equal(t, int64(1), cost.SettledCount)
						require.NotNil(t, ChannelDailyCostAttemptSettledCost(ctx, channel.Id))
						assert.Equal(t, tc.cost, *ChannelDailyCostAttemptSettledCost(ctx, channel.Id))
					}
					require.NoError(t, db.First(&user, user.Id).Error)
					require.NoError(t, db.First(&token, token.Id).Error)
					assert.Equal(t, startingQuota-tc.quota, user.Quota)
					assert.Equal(t, startingQuota-tc.quota, token.RemainQuota)
					var log model.Log
					require.NoError(t, logDB.Where("user_id = ?", user.Id).Take(&log).Error)
					assert.Equal(t, tc.quota, log.Quota)
				})
			}
		})
	}
}
