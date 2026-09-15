package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestChannelMonitorCustomActionRefreshDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			for _, tc := range []struct {
				name            string
				entry           string
				metric          string
				shared          bool
				ratioDisabled   bool
				balanceDisabled bool
				refreshFailures int32
				changeRevision  bool
			}{
				{name: "手动倍率刷新时余额触发并复用指标请求", entry: "ratio", metric: "balance", shared: true},
				{name: "手动余额刷新后同步独立倍率并执行策略", entry: "balance", metric: "balance"},
				{name: "手动倍率触发后补取独立余额", entry: "ratio", metric: "ratio"},
				{name: "自动更新使用重置后的余额和倍率", entry: "auto", metric: "balance"},
				{name: "关闭倍率同步只补取余额", entry: "balance", metric: "balance", ratioDisabled: true},
				{name: "关闭余额同步只补取倍率", entry: "ratio", metric: "ratio", balanceDisabled: true},
				{name: "手动倍率刷新保留关闭同步时的单次授权", entry: "ratio", metric: "balance", shared: true, ratioDisabled: true},
				{name: "手动余额刷新保留关闭同步时的单次授权", entry: "balance", metric: "ratio", shared: true, balanceDisabled: true},
				{name: "补充刷新失败保留接口成功状态", entry: "balance", metric: "balance", refreshFailures: 1},
				{name: "自动重试补充刷新不再次执行触发接口", entry: "auto", metric: "balance", refreshFailures: 1},
				{name: "触发后配置变更丢弃旧结果", entry: "ratio", metric: "balance", shared: true, changeRevision: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					db := setupChannelMonitorCustomActionRefreshDB(t, engine)
					disableChannelMonitorSSRFProtection(t)
					useChannelMonitorOptionMap(t, map[string]string{
						"GroupRatio":                                    `{"default":1}`,
						channelMonitorAutoUpdateRetryCountOption:        "1",
						channelMonitorAutoUpdateRetryDelaySecondsOption: "0",
						channelMonitorAutoDisableOnUpdateFailureOption:  "false",
					})
					previousGroupRatios := ratio_setting.GroupRatio2JSONString()
					require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
					t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios)) })
					var resetCalls, extraCalls, ratioCalls, balanceCalls, refreshFailures atomic.Int32
					refreshFailures.Store(tc.refreshFailures)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						switch r.URL.Path {
						case "/reset":
							resetCalls.Add(1)
							if tc.changeRevision {
								assert.NoError(t, db.Model(&model.ChannelRatioMonitor{}).Where("channel_id = ?", 81).
									Updates(map[string]any{"upstream_revision": 2, "upstream_balance": 200, "ratio": 8}).Error)
							}
							_, _ = w.Write([]byte(`{"success":true}`))
							return
						case "/extra":
							extraCalls.Add(1)
							_, _ = w.Write([]byte(`{"success":true}`))
							return
						case "/ratio":
							ratioCalls.Add(1)
						case "/balance":
							balanceCalls.Add(1)
						default:
							http.NotFound(w, r)
							return
						}
						if resetCalls.Load() == 0 {
							_, _ = w.Write([]byte(`{"data":{"ratio":2,"balance":5}}`))
							return
						}
						if r.URL.Path == "/balance" && refreshFailures.CompareAndSwap(1, 0) {
							http.Error(w, "余额暂未就绪", http.StatusServiceUnavailable)
							return
						}
						_, _ = w.Write([]byte(`{"data":{"ratio":4,"balance":100}}`))
					}))
					defer server.Close()
					baseURL := server.URL
					channel := model.Channel{Id: 81, Name: "额度重置", Key: "test-key", Group: "default", Models: "test-model", Status: common.ChannelStatusEnabled, BaseURL: &baseURL}
					require.NoError(t, db.Create(&channel).Error)
					threshold, balanceDisableThreshold, originalBalance := 10.0, 10.0, 20.0
					action := service.ChannelMonitorCustomAction{
						ID: "reset", Name: "额度重置", Enabled: true, Metric: tc.metric, Operator: "lt", Threshold: &threshold,
						Timezone: fmt.Sprintf("Etc/GMT%+d", time.Now().UTC().Hour()-12), StartTime: "00:00", EndTime: "23:00",
						DailyLimit: 3, CooldownMinutes: 1,
						Request:     service.ChannelMonitorCustomRequestConfig{Method: "POST", Path: "/reset", BodyType: "none"},
						SuccessPath: "success", SuccessValue: "true",
					}
					if tc.metric == "ratio" {
						threshold, action.Operator = 2, "gte"
					}
					// One rule matches the stale sample; another matches only after
					// reset. Neither may execute during the follow-up refresh.
					staleAction := action
					staleAction.ID, staleAction.Name, staleAction.Request.Path = "stale", "旧指标规则", "/extra"
					freshAction := staleAction
					freshThreshold := 3.0
					freshAction.ID, freshAction.Name, freshAction.Metric, freshAction.Operator, freshAction.Threshold = "fresh", "新指标规则", "ratio", "gte", &freshThreshold
					config := service.ChannelMonitorCustomUpstreamConfig{
						Ratio:                    service.ChannelMonitorCustomMetricConfig{Source: "http", Request: &service.ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/ratio"}, Result: &service.ChannelMonitorCustomResultConfig{ResponseType: "json", ValuePath: "data.ratio", Multiplier: 1}},
						Balance:                  service.ChannelMonitorCustomMetricConfig{Source: "http", Request: &service.ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/balance"}, Result: &service.ChannelMonitorCustomResultConfig{ResponseType: "json", ValuePath: "data.balance", Multiplier: 1}},
						BalanceReuseRatioRequest: tc.shared, Actions: []service.ChannelMonitorCustomAction{action, staleAction, freshAction},
					}
					if tc.shared {
						config.Balance.Request = nil
					}
					raw, err := service.MarshalChannelMonitorCustomUpstreamConfig(config)
					require.NoError(t, err)
					monitor := model.ChannelRatioMonitor{
						ChannelId: 81, UpstreamType: "custom", UpstreamBaseURL: baseURL, UpstreamRevision: 1,
						CustomUpstreamConfig: raw, Ratio: 1, UpdatedTime: 1, UpstreamBalance: &originalBalance,
						UpstreamRatioSyncDisabled: tc.ratioDisabled, UpstreamBalanceSyncDisabled: tc.balanceDisabled,
						BalanceAutoDisableThreshold: &balanceDisableThreshold, SingleChannelAction: channelMonitorPolicyActionUpdateGroupRatio,
					}
					require.NoError(t, db.Create(&monitor).Error)
					if tc.entry == "auto" {
						summary, err := runChannelRatioMonitorTaskOnce(t.Context(), nil, func(string, string, string) error { return nil })
						require.NoError(t, err)
						assert.Zero(t, summary.Failed)
						assert.Zero(t, summary.ChannelsDisabled)
						assert.Equal(t, 1, summary.BalanceUpdated)
						require.Len(t, summary.BalanceUpdates, 1)
						assert.Equal(t, 100.0, summary.BalanceUpdates[0].Balance)
					} else {
						ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/refresh", nil)
						ctx.Params = gin.Params{{Key: "id", Value: "81"}}
						if tc.entry == "ratio" {
							FetchChannelMonitorUpstreamRatio(ctx)
						} else {
							FetchChannelMonitorUpstreamBalance(ctx)
						}
						var response struct {
							Success bool   `json:"success"`
							Message string `json:"message"`
							Data    struct {
								Amount *float64 `json:"amount"`
								Result struct {
									Ratio   float64 `json:"ratio"`
									Balance struct {
										Amount *float64 `json:"amount"`
									} `json:"balance"`
								} `json:"result"`
							} `json:"data"`
						}
						require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
						if tc.refreshFailures > 0 || tc.changeRevision {
							require.False(t, response.Success)
							assert.Contains(t, response.Message, "触发接口已成功")
						} else {
							require.True(t, response.Success, recorder.Body.String())
							if tc.entry == "ratio" {
								assert.Equal(t, 4.0, response.Data.Result.Ratio)
								if !tc.balanceDisabled {
									require.NotNil(t, response.Data.Result.Balance.Amount)
									assert.Equal(t, 100.0, *response.Data.Result.Balance.Amount)
								}
							} else {
								require.NotNil(t, response.Data.Amount)
								assert.Equal(t, 100.0, *response.Data.Amount)
							}
						}
					}
					assert.EqualValues(t, 1, resetCalls.Load())
					assert.Zero(t, extraCalls.Load(), "旧指标和补充刷新均不能再次调用触发接口")
					saved, err := model.GetChannelRatioMonitor(81)
					require.NoError(t, err)
					require.NotNil(t, saved.UpstreamBalance)
					states, err := model.GetChannelMonitorCustomActionStates(81)
					require.NoError(t, err)
					assert.Equal(t, "succeeded", states["reset"].Status)
					assert.Equal(t, 1, states["reset"].Attempts)
					if tc.changeRevision {
						assert.Equal(t, int64(2), saved.UpstreamRevision)
						assert.Equal(t, 8.0, saved.Ratio)
						assert.Equal(t, 200.0, *saved.UpstreamBalance)
						assert.EqualValues(t, 1, ratioCalls.Load())
						return
					}
					if tc.refreshFailures > 0 && tc.entry != "auto" {
						assert.NotEmpty(t, saved.LastBalanceError)
						assert.True(t, states["reset"].Triggered)
						return
					}
					if tc.ratioDisabled && tc.entry != "ratio" {
						assert.Equal(t, 1.0, saved.Ratio)
						assert.Zero(t, ratioCalls.Load())
					} else {
						assert.Equal(t, 4.0, saved.Ratio)
						assert.Equal(t, 4.0, ratio_setting.GetGroupRatio("default"), "分组策略使用重置后的倍率")
					}
					if tc.balanceDisabled && tc.entry != "balance" {
						assert.Equal(t, originalBalance, *saved.UpstreamBalance)
						assert.Zero(t, balanceCalls.Load())
					} else {
						assert.Equal(t, 100.0, *saved.UpstreamBalance)
					}
					if tc.metric == "balance" {
						assert.False(t, states["reset"].Triggered, "补充刷新确认余额恢复后应解除已触发状态")
					} else {
						assert.True(t, states["reset"].Triggered, "持续满足条件时保持已触发状态")
					}
					storedChannel, err := model.GetChannelById(81, true)
					require.NoError(t, err)
					assert.Equal(t, common.ChannelStatusEnabled, storedChannel.Status, "不能根据重置前的低余额禁用渠道")
					if tc.shared {
						assert.EqualValues(t, 2, ratioCalls.Load(), "共用指标接口仅需一次补充请求")
					}
				})
			}
		})
	}
}

func setupChannelMonitorCustomActionRefreshDB(t *testing.T, engine string) *gorm.DB {
	t.Helper()
	var dialector gorm.Dialector
	switch engine {
	case "sqlite":
		dialector = sqlite.Open(filepath.Join(t.TempDir(), "refresh.db"))
	case "mysql":
		dsn := os.Getenv("TEST_CUSTOM_ACTION_MYSQL_DSN")
		if dsn == "" {
			t.Skip("未配置 TEST_CUSTOM_ACTION_MYSQL_DSN")
		}
		config, err := mysqlDriver.ParseDSN(dsn)
		require.NoError(t, err)
		require.Equal(t, "new_api_custom_action_test", config.DBName)
		dialector = mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_CUSTOM_ACTION_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("未配置 TEST_CUSTOM_ACTION_POSTGRES_DSN")
		}
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		require.Equal(t, "/new_api_custom_action_test", parsed.Path)
		dialector = postgres.Open(dsn)
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	originalDB, originalLogDB := model.DB, model.LOG_DB
	originalMain, originalLog := common.MainDatabaseType(), common.LogDatabaseType()
	originalCache, originalRedis := common.MemoryCacheEnabled, common.RedisEnabled
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseType(engine), common.DatabaseType(engine))
	common.MemoryCacheEnabled, common.RedisEnabled = false, false
	gin.SetMode(gin.TestMode)
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	t.Cleanup(func() {
		model.DB, model.LOG_DB = originalDB, originalLogDB
		common.SetDatabaseTypes(originalMain, originalLog)
		if originalDB != nil {
			assert.NoError(t, model.InitLogDB())
			model.LOG_DB = originalLogDB
			common.SetDatabaseTypes(originalMain, originalLog)
		}
		common.MemoryCacheEnabled, common.RedisEnabled = originalCache, originalRedis
	})
	tables := []any{&model.Option{}, &model.Channel{}, &model.Ability{}, &model.ChannelRatioMonitor{}, &model.ChannelRatioHistory{}, &model.SystemTask{}, &model.Log{}, &model.ChannelSmartScheduleRouteState{}, &model.ChannelSmartScheduleGroupPause{}}
	for _, table := range tables {
		require.False(t, db.Migrator().HasTable(table), "请使用空的专用测试数据库")
	}
	t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
	require.NoError(t, db.AutoMigrate(tables...))
	versionQuery := "SELECT version()"
	if engine == "sqlite" {
		versionQuery = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("数据库版本: %s", version)
	return db
}
