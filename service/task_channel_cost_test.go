package service

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// DSNs must identify isolated test databases; log writes use a separate DB.
func TestTaskExpressionChannelCostDatabaseMatrix(t *testing.T) {
	previousUnit := common.QuotaPerUnit
	previousRedis, previousBatch, previousLog := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	previousIncome := model.ChannelMonitorIncomeReady.Swap(false)
	common.QuotaPerUnit, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = 500000, false, false, true
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
		{common.DatabaseTypeMySQL, "TEST_TASK_COST_MYSQL"},
		{common.DatabaseTypePostgreSQL, "TEST_TASK_COST_POSTGRES"},
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
			t.Cleanup(func() {
				model.DB, model.LOG_DB = previousDB, previousLogDB
				common.SetDatabaseTypes(previousMainType, previousLogType)
			})
			for range 2 {
				require.NoError(t, db.AutoMigrate(&model.Task{}, &model.User{}, &model.Token{}, &model.Channel{},
					&model.ChannelDailyCost{}, &model.ChannelDailyAPIKeyCost{}, &model.ChannelTaskCostEvent{},
					&model.ChannelMonitorDailyCostDetail{}, &model.ChannelDailyCostOutbox{}))
				require.NoError(t, logDB.AutoMigrate(&model.Log{}))
			}
			query := "SELECT version()"
			if dialect.kind == common.DatabaseTypeSQLite {
				query = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database: %s (separate log DB)", version)
			for index, tc := range []struct {
				name                     string
				group, units, actual     float64
				wantQuota                int
				wantCost                 int64
				immediate, failure, late bool
				batch, rollback, legacy  bool
				timeout                  bool
			}{
				{name: "single actual usage", group: 1, units: 2, actual: 5, wantQuota: 25000, wantCost: 200000000},
				{name: "batch actual usage", group: 1, units: 2, actual: 5, wantQuota: 25000, wantCost: 200000000, batch: true},
				{name: "free user group still has upstream cost", group: 0, units: 2, actual: 5, wantCost: 200000000},
				{name: "zero estimate becomes positive", group: 1, units: 0, actual: 3, wantQuota: 15000, wantCost: 120000000},
				{name: "rounded user charge does not scale cost", group: 0.00001, units: 2, actual: 3, wantCost: 120000000},
				{name: "explicit actual zero", group: 1, units: 2, actual: 0},
				{name: "invalid actual usage retains initial cost", group: 1, units: 2, actual: -1, wantQuota: 10000, wantCost: 80000000},
				{name: "immediate completion", group: 1, units: 2, actual: 3, wantQuota: 15000, wantCost: 120000000, immediate: true},
				{name: "immediate explicit zero", group: 1, units: 2, actual: 0, immediate: true},
				{name: "immediate failure", group: 1, units: 2, immediate: true, failure: true},
				{name: "failed task refunds cost", group: 1, units: 2, failure: true},
				{name: "free user failure clears cost", group: 0, units: 2, failure: true},
				{name: "free user timeout clears cost", group: 0, units: 2, failure: true, timeout: true},
				{name: "batch free user failure clears cost", group: 0, units: 2, failure: true, batch: true},
				{name: "settlement precedes registration", group: 0, units: 2, actual: 3, wantCost: 120000000, late: true},
				{name: "failure precedes registration", group: 1, units: 2, failure: true, late: true},
				{name: "cost and accounting rollback together", group: 1, units: 2, actual: 5, wantQuota: 25000, wantCost: 200000000, rollback: true},
				{name: "legacy per call", group: 1, units: 2, wantQuota: 10000, wantCost: 80000000, legacy: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					common.QuotaPerUnit = 500000
					identity := fmt.Sprintf("cost-%d-%d", index, time.Now().UnixNano())
					snap := &billingexpr.BillingSnapshot{ExprString: `u("units") * 0.01`, ExprHash: billingexpr.ExprHashString(`u("units") * 0.01`), TaskUsageBilling: true,
						QuotaPerUnit: 500000, GroupRatio: tc.group, UsageFacts: map[string]any{"units": tc.units}}
					initial, _, err := EvaluateTaskCompletionUsage(snap, nil)
					require.NoError(t, err)
					snap.EstimatedQuotaBeforeGroup, snap.EstimatedQuotaAfterGroup = initial.ActualQuotaBeforeGroup, initial.ActualQuotaAfterGroup
					quota := initial.ActualQuotaAfterGroup
					status := model.TaskStatus(model.TaskStatusInProgress)
					if tc.immediate {
						if tc.failure {
							status, quota = model.TaskStatusFailure, 0
						} else {
							result, facts, err := EvaluateTaskCompletionUsage(snap, map[string]any{"units": tc.actual})
							require.NoError(t, err)
							snap.UsageFacts = facts
							status, quota = model.TaskStatusSuccess, result.ActualQuotaAfterGroup
						}
					}
					const funds = 1000000
					user := model.User{Username: identity, AffCode: identity, Quota: funds - quota, Status: common.UserStatusEnabled}
					require.NoError(t, db.Create(&user).Error)
					token := model.Token{UserId: user.Id, Key: identity, RemainQuota: funds - quota, UsedQuota: quota, Status: common.TokenStatusEnabled}
					require.NoError(t, db.Create(&token).Error)
					baseURL := "https://tasks.example.invalid"
					channel := model.Channel{Name: identity, Key: "unused", BaseURL: &baseURL, Status: common.ChannelStatusEnabled}
					require.NoError(t, db.Create(&channel).Error)
					task := makeTask(user.Id, channel.Id, quota, token.Id, BillingSourceWallet, 0)
					task.TaskID, task.PrivateData.UpstreamTaskID = identity, identity
					task.SubmitTime = time.Now().Add(-24 * time.Hour).Unix()
					task.Status = status
					task.PrivateData.BillingContext = &model.TaskBillingContext{GroupRatio: tc.group, TieredSnapshot: snap, OriginModelName: "test-model"}
					price := types.PriceData{Quota: quota, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: tc.group}}
					if tc.legacy {
						task.PrivateData.BillingContext.TieredSnapshot = nil
						task.PrivateData.BillingContext.PerCallBilling = true
						price.ModelPrice, price.UsePrice = 0.02, true
					}
					ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
					ctx.Request = httptest.NewRequest("POST", "/v1/videos", nil)
					ctx.Set("token_id", token.Id)
					common.SetContextKey(ctx, constant.ContextKeyUserId, user.Id)
					ctx.Set(channelDailyCostSnapshotContextKey, channelDailyCostSnapshot{ChannelId: channel.Id, Configured: true, CostRatioCNY: 4, QuotaPerUnit: 500000})
					BeginChannelDailyCostAttempt(ctx, channel.Id)
					PrepareTaskChannelCost(ctx, task)
					require.NoError(t, db.Create(task).Error)
					t.Cleanup(func() {
						require.NoError(t, logDB.Where("user_id = ?", user.Id).Delete(&model.Log{}).Error)
						for _, row := range []any{&model.ChannelDailyCost{}, &model.ChannelDailyAPIKeyCost{}, &model.ChannelTaskCostEvent{}, &model.ChannelMonitorDailyCostDetail{}, &model.ChannelDailyCostOutbox{}} {
							require.NoError(t, db.Where("channel_id = ?", channel.Id).Delete(row).Error)
						}
						for _, row := range []any{task, &token, &user, &channel} {
							require.NoError(t, db.Unscoped().Delete(row).Error)
						}
					})
					// Configuration changes after submission must not reprice this task.
					common.QuotaPerUnit = 700000
					if !tc.legacy {
						ctx.Set(channelDailyCostSnapshotContextKey, channelDailyCostSnapshot{ChannelId: channel.Id, Configured: true, CostRatioCNY: 9, QuotaPerUnit: 700000})
					}
					info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: token.Id, OriginModelName: "test-model", UsingGroup: "default",
						ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channel.Id}, TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "GENERATE"},
						PriceData: price, TieredBillingSnapshot: task.PrivateData.BillingContext.TieredSnapshot}
					if !tc.late {
						LogTaskConsumption(ctx, info, task)
					} else {
						seedChargedAccounting(t, user.Id, channel.Id, token.Id, quota, 1)
					}
					var loaded model.Task
					require.NoError(t, db.First(&loaded, task.ID).Error)
					var submitted model.Task
					require.NoError(t, db.First(&submitted, task.ID).Error)
					assert.Equal(t, "task:"+identity, loaded.PrivateData.BillingContext.ChannelCostEventId)
					if !tc.legacy {
						require.NotNil(t, loaded.PrivateData.BillingContext.ChannelCostSnapshot)
						assert.Equal(t, 4.0, loaded.PrivateData.BillingContext.ChannelCostSnapshot.CostRatioCNY)
					}
					if tc.timeout {
						previousTimeout := constant.TaskTimeoutMinutes
						constant.TaskTimeoutMinutes = 1
						t.Cleanup(func() { constant.TaskTimeoutMinutes = previousTimeout })
						sweepTimedOutTasks(context.Background())
						sweepTimedOutTasks(context.Background())
					} else if !tc.immediate {
						terminal := model.TaskStatusSuccess
						if tc.failure {
							terminal = model.TaskStatusFailure
						}
						result := &relaycommon.TaskInfo{TaskID: identity, Status: terminal, UsageFacts: map[string]any{"units": tc.actual}}
						var adaptor TaskPollingAdaptor = &scriptedPollingAdaptor{parse: result}
						if tc.batch {
							adaptor = &batchPollingAdaptor{results: map[string]*BatchTaskResult{identity: {TaskInfo: *result}}}
						}
						failBilling := tc.rollback
						if tc.rollback {
							require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:cost_rollback", func(tx *gorm.DB) {
								if updates, ok := tx.Statement.Dest.(map[string]any); failBilling && ok &&
									tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Task" && updates["quota"] != nil {
									tx.AddError(errors.New("forced task accounting failure after cost update"))
								}
							}))
							t.Cleanup(func() { require.NoError(t, db.Callback().Update().Remove("test:cost_rollback")) })
						}
						poll := func(current *model.Task) {
							t.Helper()
							tasks := map[string]*model.Task{identity: current}
							if tc.batch {
								require.NoError(t, updateBatchTasks(context.Background(), adaptor.(BatchTaskPollingAdaptor), channel.Id, []string{identity}, tasks))
							} else {
								require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, &channel, identity, tasks))
							}
						}
						poll(&loaded)
						if tc.rollback {
							require.NoError(t, db.First(&loaded, task.ID).Error)
							assert.EqualValues(t, model.TaskStatusInProgress, loaded.Status)
							assert.Equal(t, quota, loaded.Quota)
							assert.Equal(t, funds-quota, getUserQuota(t, user.Id))
							var event model.ChannelTaskCostEvent
							require.NoError(t, db.Where("cost_event_id = ?", "task:"+identity).First(&event).Error)
							assert.Equal(t, int64(80000000), event.CostNanoCNY)
							failBilling = false
							poll(&loaded)
						}
						poll(task) // stale callback must not repeat accounting or cost.
						require.NoError(t, db.First(&loaded, task.ID).Error)
						poll(&loaded)
					}
					// Replaying registration must preserve the final cost and count.
					for range 2 {
						_, resolved, err := RecordTaskChannelDailyCost(ctx, &submitted, price)
						require.NoError(t, err)
						require.True(t, resolved)
					}
					require.NoError(t, db.First(&loaded, task.ID).Error)
					if tc.failure {
						assert.EqualValues(t, model.TaskStatusFailure, loaded.Status)
					} else {
						assert.EqualValues(t, model.TaskStatusSuccess, loaded.Status)
					}
					assert.Equal(t, tc.wantQuota, loaded.Quota)
					assert.Equal(t, funds-tc.wantQuota, getUserQuota(t, user.Id))
					assert.Equal(t, funds-tc.wantQuota, getTokenRemainQuota(t, token.Id))
					used, requests := getUserUsageAccounting(t, user.Id)
					assert.Equal(t, tc.wantQuota, used)
					assert.Equal(t, 1, requests)
					assert.Equal(t, int64(tc.wantQuota), getChannelUsedQuota(t, channel.Id))
					var logCount int64
					require.NoError(t, logDB.Model(&model.Log{}).Where("user_id = ?", user.Id).Count(&logCount).Error)
					var wantLogs int64
					if !tc.late {
						wantLogs++
					}
					if quota != tc.wantQuota {
						wantLogs++
					}
					assert.Equal(t, wantLogs, logCount)
					var total model.ChannelDailyCost
					require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&total).Error)
					assert.Equal(t, model.ChannelDailyCostDayStart(task.SubmitTime), total.DayStart)
					assert.Equal(t, tc.wantCost, total.CostNanoCNY)
					assert.Equal(t, int64(1), total.SettledCount)
					var keyCost model.ChannelDailyAPIKeyCost
					require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&keyCost).Error)
					assert.Equal(t, tc.wantCost, keyCost.CostNanoCNY)
					var detail model.ChannelMonitorDailyCostDetail
					require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&detail).Error)
					assert.Equal(t, tc.wantCost, detail.CostNanoCNY)
					var projection model.ChannelDailyCostOutbox
					require.NoError(t, db.Where("channel_id = ?", channel.Id).Order("id DESC").First(&projection).Error)
					assert.Equal(t, tc.wantCost, projection.CostNanoCNY)
					assert.True(t, loaded.PrivateData.BillingContext.ChannelCostResolved)
					assert.Equal(t, tc.wantCost, loaded.PrivateData.BillingContext.ChannelCostNanoCNY)
				})
			}
		})
	}
}
