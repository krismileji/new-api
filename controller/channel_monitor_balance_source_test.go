package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func saveBalanceSourceChannel(t *testing.T, channelID int, request channelMonitorUpstreamRequest) {
	t.Helper()
	raw, err := common.Marshal(request)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 1)
	c.Set("username", "余额来源测试")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(channelID)}}
	c.Request = httptest.NewRequest(http.MethodPut, "/upstream", bytes.NewReader(raw))
	SaveChannelMonitorUpstreamConfig(c)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
}

func TestChannelMonitorBalanceSourceDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelMonitorCustomActionRefreshDB(t, engine)
			disableChannelMonitorSSRFProtection(t)
			require.NoError(t, db.AutoMigrate(&model.ChannelMonitorUpstreamAccount{}))
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&model.ChannelMonitorUpstreamAccount{})) })
			var balanceCalls, ratioCalls atomic.Int32
			wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/user/self":
					assert.Equal(t, "Bearer wallet-secret", r.Header.Get("Authorization"))
					balanceCalls.Add(1)
					_, _ = w.Write([]byte(`{"success":true,"data":{"quota":500}}`))
				case "/api/status":
					_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":100}}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer wallet.Close()
			customServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/ratio", r.URL.Path)
				assert.Equal(t, "Bearer channel-secret", r.Header.Get("Authorization"))
				ratioCalls.Add(1)
				_, _ = w.Write([]byte(`{"ratio":2}`))
			}))
			defer customServer.Close()
			settings := model.ChannelMonitorAccountSettings{UpstreamType: "new_api", UpstreamBaseURL: wallet.URL, UpstreamAuthType: "user", UpstreamUserId: 8, UpstreamAccessToken: "wallet-secret", CostConversion: `{"mode":"recharge","paid_cny":6,"credited_usd":2}`}
			raw, err := common.Marshal(settings)
			require.NoError(t, err)
			account := model.ChannelMonitorUpstreamAccount{Name: "已有余额账户", Settings: string(raw), RefreshIntervalMinutes: 5}
			_, err = model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, nil, nil)
			require.NoError(t, err)
			channel := model.Channel{Id: 201, Name: "独立倍率", Status: common.ChannelStatusEnabled, Key: "relay", Group: "default"}
			require.NoError(t, db.Create(&channel).Error)
			custom := service.ChannelMonitorCustomUpstreamConfig{
				Ratio:   service.ChannelMonitorCustomMetricConfig{Source: "http", Request: &service.ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/ratio", BodyType: "none", Headers: []service.ChannelMonitorCustomKeyValue{{Key: "Authorization", Value: "Bearer channel-secret", Secret: true}}}, Result: &service.ChannelMonitorCustomResultConfig{ResponseType: "json", ValuePath: "ratio", Multiplier: 1}},
				Balance: service.ChannelMonitorCustomMetricConfig{Source: "account", AccountID: account.ID},
			}
			request := channelMonitorUpstreamRequest{Type: "custom", BaseURL: customServer.URL, AuthType: "custom", CustomConfig: &custom, CostConversion: &service.ChannelMonitorCostConversion{Mode: "none"}}
			saveBalanceSourceChannel(t, channel.Id, request)
			monitor, err := model.GetChannelRatioMonitor(channel.Id)
			require.NoError(t, err)
			assert.Equal(t, account.ID, monitor.UpstreamAccountID)
			assert.Equal(t, customServer.URL, monitor.UpstreamBaseURL)
			assert.JSONEq(t, settings.CostConversion, monitor.CostConversion)
			outcome, err := fetchAndRecordChannelMonitorUpstreamRatio(t.Context(), monitor, nil, "", time.Second, channelMonitorRefreshOptions{IncludeSeparateBalance: true, SkipCustomActions: true}, 0, "测试")
			require.NoError(t, err)
			assert.Equal(t, 2.0, outcome.Result.Ratio)
			assert.Equal(t, 6.0, outcome.Result.CostRatio)
			require.NotNil(t, outcome.Result.Balance.Amount)
			assert.Equal(t, 5.0, *outcome.Result.Balance.Amount)
			assert.EqualValues(t, 1, balanceCalls.Load())
			assert.EqualValues(t, 1, ratioCalls.Load())

			// Updating the wallet conversion must not replace a member's local
			// request, credentials, or threshold policy, even across provider types.
			account, err = model.GetChannelMonitorUpstreamAccount(t.Context(), account.ID)
			require.NoError(t, err)
			expected := account
			settings.CostConversion = `{"mode":"recharge","paid_cny":8,"credited_usd":2}`
			settings.BalanceWarningThreshold = common.GetPointer(99.0)
			raw, err = common.Marshal(settings)
			require.NoError(t, err)
			account.Settings = string(raw)
			_, err = model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, &expected, nil)
			require.NoError(t, err)
			monitor, err = model.GetChannelRatioMonitor(channel.Id)
			require.NoError(t, err)
			assert.JSONEq(t, settings.CostConversion, monitor.CostConversion)
			assert.Nil(t, monitor.BalanceWarningThreshold)
			storedCustom, err := service.ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
			require.NoError(t, err)
			assert.Equal(t, "Bearer channel-secret", storedCustom.Ratio.Request.Headers[0].Value)
			assert.Equal(t, customServer.URL, monitor.UpstreamBaseURL)

			// Switching back to custom balance detaches the reference atomically.
			custom.Balance = service.ChannelMonitorCustomMetricConfig{Source: "fixed", FixedValue: common.GetPointer(17.0)}
			saveBalanceSourceChannel(t, channel.Id, request)
			monitor, err = model.GetChannelRatioMonitor(channel.Id)
			require.NoError(t, err)
			assert.Zero(t, monitor.UpstreamAccountID)
			assert.Equal(t, 17.0, *monitor.UpstreamBalance)
			account, err = model.GetChannelMonitorUpstreamAccount(t.Context(), account.ID)
			require.NoError(t, err)
			require.NoError(t, model.DeleteChannelMonitorUpstreamAccount(t.Context(), account.ID, account.Revision))
		})
	}
}

func TestUpstreamAutomationBalanceSourceKeepsIndependentCredentials(t *testing.T) {
	db := setupChannelMonitorCustomActionRefreshDB(t, "sqlite")
	disableChannelMonitorSSRFProtection(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorUpstreamAccount{}))
	var polls, actions atomic.Int32
	wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":100}}`))
			return
		}
		assert.Equal(t, "/api/user/self", r.URL.Path)
		assert.Equal(t, "Bearer wallet-secret", r.Header.Get("Authorization"))
		polls.Add(1)
		_, _ = w.Write([]byte(`{"success":true,"data":{"quota":0}}`))
	}))
	defer wallet.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/reset", r.URL.Path)
		assert.Equal(t, "Bearer action-secret", r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("New-Api-User"))
		actions.Add(1)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	raw, err := common.Marshal(model.ChannelMonitorAccountSettings{UpstreamType: "new_api", UpstreamBaseURL: wallet.URL, UpstreamAuthType: "user", UpstreamUserId: 8, UpstreamAccessToken: "wallet-secret"})
	require.NoError(t, err)
	account := model.ChannelMonitorUpstreamAccount{Name: "任务余额来源", Settings: string(raw)}
	_, err = model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, nil, nil)
	require.NoError(t, err)
	config := automationTestConfig(server.URL)
	config.CustomConfig.Balance = service.ChannelMonitorCustomMetricConfig{Source: "account", AccountID: account.ID}
	config.CustomConfig.Actions[0].Request.Headers = []service.ChannelMonitorCustomKeyValue{{Key: "Authorization", Value: "Bearer action-secret", Secret: true}}
	view, err := service.SaveUpstreamAutomation(t.Context(), config)
	require.NoError(t, err)
	assert.Zero(t, view.AccountID, "余额引用不改变任务归属")
	require.ErrorContains(t, model.DeleteChannelMonitorUpstreamAccount(t.Context(), account.ID, account.Revision), "关联自动任务")
	draft, err := service.FetchChannelMonitorUpstreamGroupRatio(t.Context(), service.ChannelMonitorUpstreamConfig{Type: "custom", BaseURL: server.URL, CustomConfig: config.CustomConfig})
	require.NoError(t, err)
	assert.Equal(t, 0.5, draft.Ratio)
	assert.Equal(t, 0.0, *draft.Balance.Amount)
	unchanged, err := model.GetChannelMonitorUpstreamAccount(t.Context(), account.ID)
	require.NoError(t, err)
	assert.Nil(t, unchanged.Balance, "测试获取不写入账户快照或触发渠道策略")
	assert.Zero(t, actions.Load())
	polls.Store(0)
	view, err = service.RunUpstreamAutomation(t.Context(), view.ID, true, func() time.Time { return time.Date(2026, 9, 18, 4, 0, 0, 0, time.UTC) }, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 1, actions.Load())
	assert.EqualValues(t, 2, polls.Load(), "动作前和动作后均重新查询账户余额")
	row, err := model.GetUpstreamAutomation(t.Context(), view.ID)
	require.NoError(t, err)
	assert.NotContains(t, row.Payload, "wallet-secret")
	assert.Contains(t, row.Payload, "action-secret")
	assert.Equal(t, server.URL, view.BaseURL)
	config.CustomConfig.Balance.AccountID = account.ID + 100
	_, err = service.SaveUpstreamAutomation(t.Context(), config)
	require.ErrorContains(t, err, "不存在")
}

func TestChannelMonitorBalanceSourceRetainsPerChannelProtection(t *testing.T) {
	db := setupChannelMonitorCustomActionRefreshDB(t, "sqlite")
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorUpstreamAccount{}, &model.User{}))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "balance-source-test", AuthVersion: 1}).Error)
	address := os.Getenv("TEST_CHANNEL_BALANCE_REDIS_ADDR")
	if address == "" {
		address = miniredis.RunT(t).Addr()
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	originalWrite, originalRead, originalRDB := common.RDBMonitorWrite, common.RDBMonitorRead, common.RDB
	common.RDBMonitorWrite, common.RDBMonitorRead, common.RDB, common.RedisEnabled = client, client, client, true
	t.Cleanup(func() {
		for _, pattern := range []string{"upstream_balance:{account_1}:*", "channel_balance:{301}:*", "channel_balance:{302}:*", "user:1*"} {
			keys, err := client.Keys(context.Background(), pattern).Result()
			assert.NoError(t, err)
			if len(keys) > 0 {
				assert.NoError(t, client.Del(context.Background(), keys...).Err())
			}
		}
		common.RDBMonitorWrite, common.RDBMonitorRead, common.RDB = originalWrite, originalRead, originalRDB
		assert.NoError(t, client.Close())
	})
	custom := service.ChannelMonitorCustomUpstreamConfig{Ratio: service.ChannelMonitorCustomMetricConfig{Source: "fixed", FixedValue: common.GetPointer(1.0)}, Balance: service.ChannelMonitorCustomMetricConfig{Source: "fixed", FixedValue: common.GetPointer(3.0)}}
	rawConfig, err := service.MarshalChannelMonitorCustomUpstreamConfig(custom)
	require.NoError(t, err)
	raw, err := common.Marshal(model.ChannelMonitorAccountSettings{UpstreamType: "custom", UpstreamBaseURL: "https://wallet.example", CustomUpstreamConfig: rawConfig})
	require.NoError(t, err)
	account := model.ChannelMonitorUpstreamAccount{Name: "不同保护阈值", Settings: string(raw)}
	_, err = model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, account.ID, "独立测试数据库")
	custom.Balance = service.ChannelMonitorCustomMetricConfig{Source: "account", AccountID: account.ID}
	for i, threshold := range []float64{4, 2} {
		id := 301 + i
		require.NoError(t, db.Create(&model.Channel{Id: id, Name: fmt.Sprint(id), Group: "default", Key: "relay", Status: common.ChannelStatusEnabled}).Error)
		saveBalanceSourceChannel(t, id, channelMonitorUpstreamRequest{Type: "custom", BaseURL: "https://independent.example", AuthType: "custom", CustomConfig: &custom, BalanceWarningThreshold: []byte("10"), BalanceAutoDisableThreshold: []byte(fmt.Sprint(threshold))})
	}
	first, err := model.GetChannelRatioMonitor(301)
	require.NoError(t, err)
	second, err := model.GetChannelRatioMonitor(302)
	require.NoError(t, err)
	token, err := service.BeginChannelBalanceSync(t.Context(), first)
	require.NoError(t, err)
	estimate, err := service.CommitChannelBalanceSync(t.Context(), token, 3, true)
	require.NoError(t, err)
	assert.Equal(t, "301:low,302:ok", estimate.Decision)
	handled, err := applyChannelBalanceRealtimePolicy(t.Context(), service.ChannelBalanceConfigForMonitor(first), estimate)
	require.NoError(t, err)
	require.True(t, handled)
	for _, expected := range []struct {
		id     int
		status int
	}{{301, common.ChannelStatusAutoDisabled}, {302, common.ChannelStatusEnabled}} {
		channel, err := model.GetChannelById(expected.id, false)
		require.NoError(t, err)
		assert.Equal(t, expected.status, channel.Status)
	}
	token, err = service.BeginChannelBalanceSync(t.Context(), second)
	require.NoError(t, err)
	estimate, err = service.CommitChannelBalanceSync(t.Context(), token, 1, true)
	require.NoError(t, err)
	assert.Equal(t, "301:low,302:low", estimate.Decision, "第二渠道跨过自己的阈值时仍产生新保护事件")
	handled, err = applyChannelBalanceRealtimePolicy(t.Context(), service.ChannelBalanceConfigForMonitor(second), estimate)
	require.NoError(t, err)
	require.True(t, handled)
	channel, err := model.GetChannelById(302, false)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
}
