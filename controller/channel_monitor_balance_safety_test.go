package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The same persisted balance and channel/ability contracts run on all engines.
// External engines require an empty, disposable new_api_monitor_balance_test DB.
func TestChannelMonitorBalanceSafetyDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			for _, test := range []struct {
				name string
				run  func(*testing.T, *gorm.DB)
			}{
				{"settlement_order", verifyChannelMonitorBalanceSettlementOrder},
				{"missing_balance", verifyChannelMonitorMissingSub2APIBalance},
				{"failed_balance_recovery", verifyChannelMonitorFailedBalanceRecovery},
				{"partial_failure", verifyChannelMonitorPartialBalanceFailure},
				{"retry_guards", verifyChannelMonitorBalanceRetryGuards},
				{"disable_notification", verifyChannelMonitorBalanceDisableNotification},
			} {
				t.Run(test.name, func(t *testing.T) {
					test.run(t, setupChannelMonitorBalanceSafetyDB(t, engine))
				})
			}
		})
	}
}

func setupChannelMonitorBalanceSafetyDB(t *testing.T, engine string) *gorm.DB {
	t.Helper()
	var dialector gorm.Dialector
	databaseType := common.DatabaseTypeSQLite
	switch engine {
	case "mysql":
		dsn := os.Getenv("MONITOR_BALANCE_MYSQL_DSN")
		if dsn == "" {
			t.Skip("需要配置专用的 MONITOR_BALANCE_MYSQL_DSN")
		}
		config, err := mysqlDriver.ParseDSN(dsn)
		require.NoError(t, err)
		require.Equal(t, "new_api_monitor_balance_test", config.DBName)
		require.Equal(t, "tcp", config.Net)
		require.True(t, strings.HasPrefix(config.Addr, "127.0.0.1:"))
		dialector, databaseType = mysql.Open(dsn), common.DatabaseTypeMySQL
	case "postgres":
		dsn := os.Getenv("MONITOR_BALANCE_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("需要配置专用的 MONITOR_BALANCE_POSTGRES_DSN")
		}
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		require.Equal(t, "127.0.0.1", parsed.Hostname())
		require.Equal(t, "/new_api_monitor_balance_test", parsed.Path)
		dialector = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
		databaseType = common.DatabaseTypePostgreSQL
	}
	db := setupChannelMonitorControllerTestDB(t)
	if dialector != nil {
		sqliteDB := db
		var err error
		db, err = gorm.Open(dialector, &gorm.Config{})
		require.NoError(t, err)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		model.DB = db
		common.SetDatabaseTypes(databaseType, databaseType)
		// Initialize the same reserved-column quoting as application startup.
		require.NoError(t, model.InitLogDB())
		t.Cleanup(func() {
			model.DB = sqliteDB
			common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
			assert.NoError(t, model.InitLogDB())
			assert.NoError(t, sqlDB.Close())
		})
		tables := []any{
			&model.Option{}, &model.Log{}, &model.Channel{}, &model.Ability{}, &model.ChannelRatioMonitor{},
			&model.ChannelRatioHistory{}, &model.ChannelDailyCost{},
			&model.ChannelSmartScheduleRouteState{}, &model.ChannelSmartScheduleGroupPause{},
			&model.ChannelSmartScheduleModelSampleState{}, &model.SystemTask{}, &model.SystemTaskLock{},
		}
		for _, table := range tables {
			require.False(t, db.Migrator().HasTable(table), "必须使用空的专用测试数据库")
		}
		t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
		require.NoError(t, db.AutoMigrate(tables...))
	}
	versionQuery := "SELECT version()"
	if engine == "sqlite" {
		versionQuery = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("database=%s version=%s", engine, version)
	return db
}

func verifyChannelMonitorBalanceSettlementOrder(t *testing.T, db *gorm.DB) {
	warning, threshold := 20.0, 7.0
	// Conversion is deliberately non-unit: 4 CNY of local cost is 2 upstream units.
	conversion, err := service.MarshalChannelMonitorCostConversion(service.ChannelMonitorCostConversion{
		Mode: service.ChannelMonitorCostConversionRecharge, PaidCNY: 2, CreditedUSD: 1,
	})
	require.NoError(t, err)
	for index, providerFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("provider_first_%t", providerFirst), func(t *testing.T) {
			id := index + 1
			channel := model.Channel{Id: id, Name: "余额对账", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled}
			require.NoError(t, db.Create(&channel).Error)
			require.NoError(t, db.Create(&model.Ability{Group: "vip", Model: "model-a", ChannelId: id, Enabled: true}).Error)
			initialBalance, baseline := 10.0, int64(0)
			monitor := model.ChannelRatioMonitor{
				ChannelId: id, UpstreamRevision: 1, UpstreamBalance: &initialBalance,
				LastBalanceTime: common.GetTimestamp(), LastBalanceCostNanoCNY: &baseline,
				BalanceWarningThreshold: &warning, BalanceAutoDisableThreshold: &threshold,
				CostConversion: conversion,
			}
			require.NoError(t, db.Create(&monitor).Error)
			observedBalance := 8.0
			if !providerFirst {
				require.NoError(t, model.AddChannelDailyCost(t.Context(), id, common.GetTimestamp(), 4*model.ChannelDailyCostNanoPerCNY, 1, 0))
				observedBalance = initialBalance
			}
			first, applied, err := recordChannelMonitorBalanceUpdate(t.Context(), monitor, &observedBalance, "")
			require.NoError(t, err)
			require.True(t, applied)
			require.NotNil(t, first)
			assert.Equal(t, 8.0, first.EffectiveBalance)
			monitor, err = model.GetChannelRatioMonitor(id)
			require.NoError(t, err)
			if providerFirst {
				// Reload and preserve the upstream debit through a failed refresh too.
				assert.Equal(t, -2.0, monitor.BalancePendingConsumption)
				require.NoError(t, model.RecordChannelRatioMonitorBalance(id, nil, "上游超时"))
				monitor, err = model.GetChannelRatioMonitor(id)
				require.NoError(t, err)
				assert.Equal(t, -2.0, monitor.BalancePendingConsumption)
				require.NoError(t, model.AddChannelDailyCost(t.Context(), id, common.GetTimestamp(), 4*model.ChannelDailyCostNanoPerCNY, 1, 0))
			}
			observedBalance = 8
			second, applied, err := recordChannelMonitorBalanceUpdate(t.Context(), monitor, &observedBalance, "")
			require.NoError(t, err)
			require.True(t, applied)
			require.NotNil(t, second)
			assert.Equal(t, 8.0, second.EffectiveBalance)
			assert.Zero(t, second.EstimatedConsumption)
			disabled, err := autoDisableChannelMonitorAtEffectiveBalance(monitor, &channel, observedBalance, second.EffectiveBalance, second.EstimatedConsumption)
			require.NoError(t, err)
			assert.False(t, disabled, "同一笔支出不能因为记账顺序不同重复扣算")
			// Later unreflected spending must still trigger the configured threshold.
			monitor, err = model.GetChannelRatioMonitor(id)
			require.NoError(t, err)
			require.NoError(t, model.AddChannelDailyCost(t.Context(), id, common.GetTimestamp(), 4*model.ChannelDailyCostNanoPerCNY, 1, 0))
			disabled, err = autoDisableChannelMonitorForLowBalanceWithContext(t.Context(), monitor, &channel, observedBalance)
			require.NoError(t, err)
			assert.True(t, disabled)
			stored, err := model.GetChannelById(id, true)
			require.NoError(t, err)
			assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
			var ability model.Ability
			require.NoError(t, db.First(&ability, "channel_id = ?", id).Error)
			assert.False(t, ability.Enabled)
		})
	}
}

func verifyChannelMonitorMissingSub2APIBalance(t *testing.T, db *gorm.DB) {
	useChannelMonitorOptionMap(t, map[string]string{})
	disableChannelMonitorSSRFProtection(t)
	for index, test := range []struct {
		name, payload string
		valid         bool
	}{
		{"missing", `{"code":0,"data":{}}`, false},
		{"null_balance", `{"code":0,"data":{"balance":null}}`, false},
		{"null_data", `{"code":0,"data":null}`, false},
		{"zero", `{"code":0,"data":{"balance":0}}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := index + 1
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.payload))
			}))
			defer server.Close()
			require.NoError(t, db.Create(&model.Channel{Id: id, Name: "Sub2API 余额", Status: common.ChannelStatusEnabled}).Error)
			balance, threshold := 10.0, 4.0
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{
				ChannelId: id, UpstreamType: service.Sub2APIUpstreamType, UpstreamBaseURL: server.URL,
				UpstreamAuthType: service.Sub2APIAuthToken, UpstreamAccessToken: "test-token",
				UpstreamBalance: &balance, BalanceAutoDisableThreshold: &threshold,
			}).Error)
			ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/upstream/balance/fetch", nil)
			ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(id)}}
			FetchChannelMonitorUpstreamBalance(ctx)
			var response channelMonitorUpstreamBalanceAPIResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Equal(t, test.valid, response.Success)
			channel, err := model.GetChannelById(id, true)
			require.NoError(t, err)
			monitor, err := model.GetChannelRatioMonitor(id)
			require.NoError(t, err)
			require.NotNil(t, monitor.UpstreamBalance)
			if test.valid {
				assert.Zero(t, *monitor.UpstreamBalance)
				assert.Empty(t, monitor.LastBalanceError)
				assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
			} else {
				assert.Equal(t, balance, *monitor.UpstreamBalance)
				assert.NotEmpty(t, monitor.LastBalanceError)
				assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
			}
		})
	}
}

func verifyChannelMonitorFailedBalanceRecovery(t *testing.T, db *gorm.DB) {
	useChannelMonitorOptionMap(t, map[string]string{
		"GroupRatio": `{"vip":1}`,
		channelMonitorAutoEnableOnCostRatioRecoveryOption: "true",
		channelMonitorAutoUpdateRetryCountOption:          "0",
	})
	disableChannelMonitorSSRFProtection(t)
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios)) })
	var balanceHealthy atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"success":true,"data":{"vip":{"ratio":0.8}}}`))
		case "/api/user/self":
			if !balanceHealthy.Load() {
				http.Error(w, "balance unavailable", http.StatusBadGateway)
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":400}}`))
		case "/api/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":100}}`))
		}
	}))
	defer server.Close()
	for index, storedBalance := range []*float64{nil, new(float64)} {
		id := index + 1
		channel := model.Channel{Id: id, Name: "倍率恢复余额保护", Group: "vip", Status: common.ChannelStatusAutoDisabled}
		channel.SetOtherInfo(map[string]interface{}{"status_reason": channelMonitorCostRatioPolicyDisableReason})
		require.NoError(t, db.Create(&channel).Error)
		threshold := 4.0
		require.NoError(t, db.Create(&model.ChannelRatioMonitor{
			ChannelId: id, Ratio: 1.2, UpdatedTime: 1, UpstreamBalance: storedBalance,
			BalanceAutoDisableThreshold: &threshold, UpstreamType: service.NewAPIUpstreamType,
			UpstreamBaseURL: server.URL, UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
			UpstreamUserId: 42, UpstreamAccessToken: "test-token", SingleChannelAction: channelMonitorPolicyActionDisableChannel,
		}).Error)
	}
	summary, err := runChannelRatioMonitorTaskOnce(t.Context(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, summary.Failed)
	assert.Zero(t, summary.ChannelsEnabled)
	for _, id := range []int{1, 2} {
		stored, err := model.GetChannelById(id, true)
		require.NoError(t, err)
		assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
	}
	balanceHealthy.Store(true)
	summary, err = runChannelRatioMonitorTaskOnce(t.Context(), nil, nil)
	require.NoError(t, err)
	assert.Zero(t, summary.Failed)
	assert.Equal(t, 2, summary.ChannelsEnabled, "余额恢复到阈值时允许倍率恢复启用")
}

func verifyChannelMonitorPartialBalanceFailure(t *testing.T, db *gorm.DB) {
	disableChannelMonitorSSRFProtection(t)
	for index, test := range []struct {
		name    string
		recover bool
		disable bool
		manual  bool
	}{
		{"exhausted", false, true, false},
		{"retry_recovers", true, true, false},
		{"disable_off", false, false, false},
		{"manual_disable", false, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			useChannelMonitorOptionMap(t, map[string]string{
				channelMonitorAutoDisableOnUpdateFailureOption:        strconv.FormatBool(test.disable),
				channelMonitorAutoUpdateRetryCountOption:              "2",
				channelMonitorAutoUpdateConsecutiveFailureLimitOption: "0",
			})
			var ratioCalls, balanceCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/user/self/groups":
					ratioCalls.Add(1)
					_, _ = w.Write([]byte(`{"success":true,"data":{"vip":{"ratio":0.8}}}`))
				case "/api/user/self":
					attempt := balanceCalls.Add(1)
					if !test.recover || attempt == 1 {
						http.Error(w, "balance unavailable", http.StatusBadGateway)
						return
					}
					_, _ = w.Write([]byte(`{"success":true,"data":{"quota":500}}`))
				case "/api/status":
					_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":100}}`))
				}
			}))
			defer server.Close()
			id, status := index+1, common.ChannelStatusEnabled
			if test.manual {
				status = common.ChannelStatusManuallyDisabled
			}
			require.NoError(t, db.Create(&model.Channel{Id: id, Name: "余额失败策略", Group: "vip", Status: status}).Error)
			threshold := 4.0
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{
				ChannelId: id, Ratio: 1, UpdatedTime: 1, BalanceAutoDisableThreshold: &threshold,
				UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
				UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
				UpstreamUserId: 42, UpstreamAccessToken: "test-token",
			}).Error)
			defer func() { require.NoError(t, db.Delete(&model.ChannelRatioMonitor{}, "channel_id = ?", id).Error) }()
			summary, err := runChannelRatioMonitorTaskOnce(t.Context(), nil, nil)
			require.NoError(t, err)
			assert.EqualValues(t, 1, ratioCalls.Load(), "成功倍率不能在余额重试时重复获取")
			assert.Equal(t, 1, summary.Updated)
			require.Len(t, summary.ChangedChannels, 1)
			assert.Equal(t, 1.0, summary.ChangedChannels[0].OldRatio)
			monitor, err := model.GetChannelRatioMonitor(id)
			require.NoError(t, err)
			assert.Equal(t, model.ChannelRatioFetchStatusSucceeded, monitor.LastFetchStatus)
			assert.Zero(t, monitor.ConsecutiveFailures)
			if test.recover {
				assert.EqualValues(t, 2, balanceCalls.Load())
				assert.Zero(t, summary.Failed)
				assert.Equal(t, 1, summary.RecoveredAfterRetry)
				assert.Zero(t, monitor.BalanceConsecutiveFailures)
			} else {
				assert.EqualValues(t, 3, balanceCalls.Load())
				assert.Equal(t, 1, summary.Failed)
				require.Len(t, summary.Failures, 1)
				assert.Equal(t, model.ChannelRatioFailureAlertBalance, summary.Failures[0].Kind)
				assert.Zero(t, summary.RecoveredAfterRetry)
				assert.Equal(t, 3, monitor.BalanceConsecutiveFailures)
			}
			if !test.recover && test.disable && !test.manual {
				status = common.ChannelStatusAutoDisabled
				assert.Equal(t, 1, summary.ChannelsDisabled)
			} else {
				assert.Zero(t, summary.ChannelsDisabled)
			}
			stored, err := model.GetChannelById(id, true)
			require.NoError(t, err)
			assert.Equal(t, status, stored.Status)
		})
	}
}

func verifyChannelMonitorBalanceRetryGuards(t *testing.T, db *gorm.DB) {
	disableChannelMonitorSSRFProtection(t)
	for index, authenticationFailure := range []bool{true, false} {
		t.Run(fmt.Sprintf("authentication_failure_%t", authenticationFailure), func(t *testing.T) {
			useChannelMonitorOptionMap(t, map[string]string{
				channelMonitorAutoUpdateRetryCountOption:       "3",
				channelMonitorAutoDisableOnUpdateFailureOption: "true",
			})
			id := index + 1
			var balanceCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v1/sub2api/billing":
					_, _ = w.Write([]byte(`{"object":"sub2api.key_billing","schema_version":1,"billing_scope":"token","effective_rate_multiplier":0.8}`))
				case "/v1/usage":
					attempt := balanceCalls.Add(1)
					if authenticationFailure {
						http.Error(w, "forbidden", http.StatusForbidden)
						return
					}
					if attempt == 1 {
						http.Error(w, "balance unavailable", http.StatusBadGateway)
						return
					}
					// An administrator changed the threshold during the balance retry.
					assert.NoError(t, db.Model(&model.ChannelRatioMonitor{}).Where("channel_id = ?", id).
						Updates(map[string]any{"upstream_revision": 2, "balance_auto_disable_threshold": 0}).Error)
					_, _ = w.Write([]byte(`{"mode":"unrestricted","balance":1}`))
				}
			}))
			defer server.Close()
			require.NoError(t, db.Create(&model.Channel{Id: id, Name: "余额重试保护", Key: "sk-test", Status: common.ChannelStatusEnabled}).Error)
			threshold := 4.0
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{
				ChannelId: id, UpstreamRevision: 1, UpstreamType: service.Sub2APIUpstreamType,
				UpstreamAuthType: service.Sub2APIAuthAPIKey, UpstreamBaseURL: server.URL, UpstreamGroup: "vip",
				BalanceAutoDisableThreshold: &threshold,
			}).Error)
			defer func() { require.NoError(t, db.Delete(&model.ChannelRatioMonitor{}, "channel_id = ?", id).Error) }()
			summary, err := runChannelRatioMonitorTaskOnce(t.Context(), nil, nil)
			require.NoError(t, err)
			channel, err := model.GetChannelById(id, true)
			require.NoError(t, err)
			if authenticationFailure {
				assert.EqualValues(t, 1, balanceCalls.Load())
				assert.Zero(t, summary.Retried)
				assert.Equal(t, 1, summary.Failed)
				assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
			} else {
				assert.EqualValues(t, 2, balanceCalls.Load())
				assert.Equal(t, 1, summary.Skipped)
				assert.Zero(t, summary.ChannelsDisabled)
				assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
				monitor, err := model.GetChannelRatioMonitor(id)
				require.NoError(t, err)
				assert.Nil(t, monitor.UpstreamBalance, "旧配置响应不能写入余额")
			}
		})
	}
}

func verifyChannelMonitorBalanceDisableNotification(t *testing.T, db *gorm.DB) {
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption:       "0",
		channelMonitorEmailNotificationOption:          "true",
		channelMonitorNotificationEmailOption:          "alerts@example.com",
		channelMonitorEmailNotificationTypesOption:     `["channel_disabled"]`,
		channelMonitorAutoDisableOnUpdateFailureOption: "true",
	})
	ratio, balance, threshold := 0.8, 3.0, 4.0
	customConfig, err := service.MarshalChannelMonitorCustomUpstreamConfig(service.ChannelMonitorCustomUpstreamConfig{
		Ratio:   service.ChannelMonitorCustomMetricConfig{Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &ratio},
		Balance: service.ChannelMonitorCustomMetricConfig{Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &balance},
	})
	require.NoError(t, err)
	remark := "余额禁用备注"
	require.NoError(t, db.Create(&model.Channel{Id: 1, Name: "余额禁用通知", Remark: &remark, Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamType: service.CustomUpstreamType, UpstreamBaseURL: "https://custom.example",
		UpstreamAuthType: service.CustomUpstreamAuthType, CustomUpstreamConfig: customConfig,
		BalanceAutoDisableThreshold: &threshold,
	}).Error)
	emailCalls := 0
	sendEmail := func(subject, receiver, body string) error {
		emailCalls++
		assert.Equal(t, "alerts@example.com", receiver)
		assert.Contains(t, body, "余额禁用通知")
		assert.Contains(t, body, remark)
		assert.Contains(t, body, "低于自动禁用阈值 4")
		return nil
	}
	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, sendEmail)
	require.NoError(t, err)
	require.Equal(t, 1, summary.ChannelsDisabled)
	assert.Equal(t, 1, emailCalls)
	assert.Equal(t, "sent", summary.EmailStatus)
	summary, err = runChannelRatioMonitorTaskOnce(context.Background(), nil, sendEmail)
	require.NoError(t, err)
	assert.Zero(t, summary.ChannelsDisabled)
	assert.Equal(t, 1, emailCalls, "持续低余额不能重复通知禁用")
}
