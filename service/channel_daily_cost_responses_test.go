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
func TestResponsesChannelDailyCostUsageAuthorityDatabaseMatrix(t *testing.T) {
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
		{common.DatabaseTypeMySQL, "TEST_RESPONSES_MYSQL"},
		{common.DatabaseTypePostgreSQL, "TEST_RESPONSES_POSTGRES"},
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
			const startingQuota = 2_000_000
			for index, tc := range []struct {
				name, terminal, delta, expression, mode string
				estimate, prompt, completion, quota     int
				cost                                    int64
				unresolved, perCall                     bool
			}{
				{name: "disconnect without usage", delta: "hello", estimate: 100, prompt: 100, completion: 1, quota: 101, unresolved: true},
				{name: "completed without usage", terminal: `{"type":"response.completed","response":{}}`, delta: "hello", estimate: 100, prompt: 100, completion: 1, quota: 101, unresolved: true},
				{name: "started prompt fallback", terminal: `{"type":"response.created","response":{}}`, estimate: 100, prompt: 100, quota: 100, unresolved: true},
				{name: "reported input with estimated output", terminal: `{"type":"response.completed","response":{"usage":{"input_tokens":20}}}`, delta: "hello", estimate: 100, prompt: 20, completion: 1, quota: 21, unresolved: true},
				{name: "reported output with estimated input", terminal: `{"type":"response.completed","response":{"usage":{"output_tokens":5}}}`, delta: "hello", estimate: 100, prompt: 100, completion: 5, quota: 105, unresolved: true},
				{name: "native reported usage without sidecar", terminal: `{"type":"response.completed","response":{"usage":{"input_tokens":20,"output_tokens":5}}}`, delta: "hello", estimate: 100, prompt: 20, completion: 5, quota: 25, cost: 50_000},
				{name: "reported zero fixed expression", terminal: `{"type":"response.completed","response":{"usage":{"input_tokens":0,"output_tokens":0}}}`, expression: flat, quota: 5000, cost: 10_000_000},
				{name: "missing zero fixed expression", expression: flat, quota: 5000, unresolved: true},
				{name: "reported fixed expression", terminal: `{"type":"response.completed","response":{"usage":{"input_tokens":20,"output_tokens":5}}}`, expression: flat, prompt: 20, completion: 5, quota: 5000, cost: 10_000_000},
				{name: "legacy per-call without usage", delta: "hello", estimate: 100, prompt: 100, completion: 1, perCall: true, quota: 5000, cost: 10_000_000},
				{name: "audio fixed expression estimated", mode: "audio", expression: flat, delta: "hello", estimate: 100, prompt: 100, completion: 1, quota: 5000, unresolved: true},
				{name: "audio fixed expression reported", mode: "audio", expression: flat, terminal: `{"type":"response.completed","response":{"usage":{"input_tokens":20,"output_tokens":5}}}`, prompt: 20, completion: 5, quota: 5000, cost: 10_000_000},
				{name: "channel test estimated", mode: "probe", delta: "hello", estimate: 100, prompt: 100, completion: 1, quota: 101, unresolved: true},
				{name: "channel test reported", mode: "probe", terminal: `{"type":"response.completed","response":{"usage":{"input_tokens":20,"output_tokens":5}}}`, prompt: 20, completion: 5, quota: 25, cost: 50_000},
			} {
				t.Run(tc.name, func(t *testing.T) {
					user := model.User{Username: fmt.Sprintf("responses_cost_%d", index), Quota: startingQuota, Status: common.UserStatusEnabled}
					require.NoError(t, db.Create(&user).Error)
					token := model.Token{UserId: user.Id, Key: fmt.Sprintf("responses-cost-test-%d", index), RemainQuota: startingQuota, Status: common.TokenStatusEnabled}
					require.NoError(t, db.Create(&token).Error)
					channel := model.Channel{Name: "responses-cost", Key: "unused", Status: common.ChannelStatusEnabled}
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
					ctx.Request.URL.Path = "/v1/responses"
					CaptureChannelDailyCostSnapshot(ctx, channel.Id)
					BeginChannelDailyCostAttempt(ctx, channel.Id)
					info := &relaycommon.RelayInfo{
						UserId: user.Id, TokenId: token.Id, TokenKey: token.Key,
						ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channel.Id, UpstreamModelName: "gpt-4o"},
						OriginModelName: "gpt-4o", UsingGroup: "default", UserGroup: "default",
						UserSetting:     dto.UserSetting{BillingPreference: "wallet_only"},
						ForcePreConsume: true, StartTime: time.Now(), FirstResponseTime: time.Now(), RelayFormat: relaytypes.RelayFormatOpenAIResponses,
						IsStream: true, StreamStatus: relaycommon.NewStreamStatus(),
						PriceData: types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
					}
					info.SetEstimatePromptTokens(tc.estimate)
					if tc.expression != "" {
						info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{
							BillingMode: "tiered_expr", ExprString: tc.expression, ExprHash: billingexpr.ExprHashString(tc.expression),
							QuotaPerUnit: common.QuotaPerUnit, GroupRatio: 1, EstimatedQuotaAfterGroup: tc.quota,
						}
					} else if tc.perCall {
						info.PriceData.UsePrice, info.PriceData.ModelPrice = true, 0.01
					}
					accumulator := NewResponsesUsageAccumulator(info)
					if tc.delta != "" {
						accumulator.Observe(&dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: tc.delta})
					}
					if tc.terminal != "" {
						var terminal dto.ResponsesStreamResponse
						require.NoError(t, common.UnmarshalJsonStr(tc.terminal, &terminal))
						accumulator.Observe(&terminal)
					}
					usage := accumulator.Finish()
					assert.Equal(t, tc.prompt, usage.PromptTokens)
					assert.Equal(t, tc.completion, usage.CompletionTokens)
					// Native Responses must not gain a conversion sidecar merely to
					// control downstream cost confidence or change user settlement.
					assert.Nil(t, usage.BillingUsage)
					if tc.mode == "probe" {
						calculated := CalculateChannelModelDetectionQuota(ctx, info, usage)
						assert.Equal(t, !tc.unresolved, calculated.Reliable)
						RecordChannelTestDailyCost(ctx, info, tc.quota, nil, usage, true)
					} else {
						require.Nil(t, PreConsumeBilling(ctx, tc.quota, info))
						if tc.mode == "audio" {
							PostAudioConsumeQuota(ctx, info, usage, "")
						} else {
							PostTextConsumeQuota(ctx, info, usage, nil)
						}
					}
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
					if tc.mode != "probe" {
						require.NoError(t, db.First(&user, user.Id).Error)
						require.NoError(t, db.First(&token, token.Id).Error)
						assert.Equal(t, startingQuota-tc.quota, user.Quota)
						assert.Equal(t, startingQuota-tc.quota, token.RemainQuota)
						var log model.Log
						require.NoError(t, logDB.Where("user_id = ?", user.Id).Take(&log).Error)
						assert.Equal(t, tc.quota, log.Quota)
					}
				})
			}
		})
	}
}

func TestResponsesCostConfidenceDoesNotLeakIntoNextCall(t *testing.T) {
	ctx := newChannelDailyCostTestContext()
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"},
		PriceData:   types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}
	info.SetEstimatePromptTokens(100)
	first := NewResponsesUsageAccumulator(info)
	first.Observe(&dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "hello"})
	firstUsage := first.Finish()
	assert.False(t, CalculateChannelModelDetectionQuota(ctx, info, firstUsage).Reliable)
	assert.Same(t, firstUsage, first.Finish())
	assert.False(t, CalculateChannelModelDetectionQuota(ctx, info, firstUsage).Reliable)

	next := NewResponsesUsageAccumulator(info)
	next.Observe(&dto.ResponsesStreamResponse{Type: "response.completed", Response: &dto.OpenAIResponsesResponse{
		Usage: &dto.Usage{InputTokens: 20, OutputTokens: 5, TotalTokens: 25},
	}})
	result := CalculateChannelModelDetectionQuota(ctx, info, next.Finish())
	assert.True(t, result.Reliable)
	assert.Equal(t, int64(25), result.CostBasisQuota)

	// A channel retry can return through a different provider and need not use
	// a Responses accumulator at all.
	missing := NewResponsesUsageAccumulator(info)
	assert.False(t, CalculateChannelModelDetectionQuota(ctx, info, missing.Finish()).Reliable)
	info.InitChannelMeta(ctx)
	result = CalculateChannelModelDetectionQuota(ctx, info, &dto.Usage{PromptTokens: 20, CompletionTokens: 5, TotalTokens: 25})
	assert.True(t, result.Reliable)
	assert.Equal(t, int64(25), result.CostBasisQuota)
}
