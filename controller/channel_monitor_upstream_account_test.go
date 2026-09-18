package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpstreamAccountDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelMonitorCustomActionRefreshDB(t, engine)
			disableChannelMonitorSSRFProtection(t)
			require.NoError(t, db.AutoMigrate(&model.ChannelMonitorUpstreamAccount{}))
			require.NoError(t, db.AutoMigrate(&model.ChannelMonitorUpstreamAccount{}, &model.ChannelRatioMonitor{}))
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&model.ChannelMonitorUpstreamAccount{})) })
			var polls, actions atomic.Int32
			balance := 100.0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/reset" {
					actions.Add(1)
					balance = 100
					_, _ = w.Write([]byte(`{"success":true}`))
					return
				}
				polls.Add(1)
				_, _ = fmt.Fprintf(w, `{"balance":%g}`, balance)
			}))
			defer server.Close()
			config := automationTestConfig(server.URL)
			config.CustomConfig.Actions = nil
			for i, ratio := range []float64{0.5, 1.2} {
				id := 101 + i
				custom := config.CustomConfig
				custom.Ratio.FixedValue = common.GetPointer(ratio)
				raw, err := service.MarshalChannelMonitorCustomUpstreamConfig(custom)
				require.NoError(t, err)
				require.NoError(t, db.Create(&model.Channel{Id: id, Name: fmt.Sprint(id), Key: "relay-key", Status: common.ChannelStatusEnabled, Group: "default"}).Error)
				require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: id, Ratio: ratio, UpdatedTime: 1, UpstreamType: "custom", UpstreamBaseURL: server.URL, UpstreamRevision: 1, CustomUpstreamConfig: raw}).Error)
			}
			source, err := model.GetChannelRatioMonitor(101)
			require.NoError(t, err)
			raw, err := common.Marshal(model.ChannelMonitorAccountSettingsFromMonitor(source))
			require.NoError(t, err)
			account := model.ChannelMonitorUpstreamAccount{Name: "同一余额池", Settings: string(raw), RefreshIntervalMinutes: 5}
			members, err := model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, nil, []int{101, 102}, map[int]int64{101: 1, 102: 1})
			require.NoError(t, err)
			require.Len(t, members, 2)
			assert.True(t, (upstreamAccountBalanceTaskHandler{}).Enabled(), "首次余额同步到期")
			ctx := withUpstreamAccountBalanceRound(t.Context())
			for _, m := range members {
				outcome, err := fetchAndRecordChannelMonitorUpstreamRatio(ctx, m, nil, "", time.Second, channelMonitorRefreshOptions{IncludeSeparateBalance: true}, 0, "测试")
				require.NoError(t, err)
				assert.Equal(t, m.Ratio, outcome.Result.Ratio)
				require.NotNil(t, outcome.Result.Balance.Amount)
				assert.Equal(t, 100.0, *outcome.Result.Balance.Amount)
			}
			assert.EqualValues(t, 1, polls.Load(), "两个不同倍率的渠道只查询一次余额")
			assert.False(t, (upstreamAccountBalanceTaskHandler{}).Enabled(), "刷新间隔内不生成空轮询任务")
			for _, id := range []int{101, 102} {
				m, err := model.GetChannelRatioMonitor(id)
				require.NoError(t, err)
				require.NotNil(t, m.UpstreamBalance)
				assert.Equal(t, 100.0, *m.UpstreamBalance)
				assert.Equal(t, account.ID, m.UpstreamAccountID)
			}
			require.Error(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 101}).Error, "渠道唯一约束保留")
			stale := account
			account.Name = "统一账户"
			_, err = model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, &stale, nil)
			require.NoError(t, err)
			_, err = model.SaveChannelMonitorUpstreamAccount(t.Context(), &stale, &stale, nil)
			require.ErrorIs(t, err, model.ErrUpstreamAccountChanged)
			_, err = fetchAndRecordUpstreamAccountBalance(t.Context(), members[0], time.Second)
			require.ErrorIs(t, err, model.ErrUpstreamAccountChanged, "旧轮次不得查询后覆盖新账户配置")
			view, err := channelMonitorAccountView(t.Context(), account)
			require.NoError(t, err)
			assert.ElementsMatch(t, []int{101, 102}, view.ChannelIDs)

			input := automationTestConfig(server.URL)
			input.AccountID = account.ID
			one, err := service.SaveUpstreamAutomation(t.Context(), input)
			require.NoError(t, err)
			// A balance-only account task checks once, independent of member count.
			before := polls.Load()
			_, err = service.RunUpstreamAutomation(t.Context(), one.ID, true, time.Now, refreshUpstreamAutomationChannels)
			require.NoError(t, err)
			assert.EqualValues(t, before+1, polls.Load())
			assert.Zero(t, actions.Load())
			assert.Error(t, model.DeleteChannelMonitorUpstreamAccount(t.Context(), account.ID, account.Revision))
			// Runtime poll history expires independently of durable task rules.
			require.NoError(t, db.Create(&model.SystemTask{TaskID: "expired-account-poll", Type: upstreamAccountBalanceTaskType, Status: model.SystemTaskStatusSucceeded, CreatedAt: 1, UpdatedAt: 1}).Error)
			cleaned, err := model.DeleteChannelMonitorHistoryBeforeWithTaskCutoffs(t.Context(), model.ChannelMonitorHistoryRetentionCutoffs{ExecutionDetail: 200, Task: 100, RatioHistory: 100}, []string{upstreamAccountBalanceTaskType}, map[string]int64{upstreamAccountBalanceTaskType: 100}, 10, model.NewChannelMonitorCleanupBudget(time.Minute))
			require.NoError(t, err)
			assert.EqualValues(t, 1, cleaned.TaskRowsDeleted)
			_, err = model.GetUpstreamAutomation(t.Context(), one.ID)
			require.NoError(t, err)
		})
	}
}

func TestUpstreamAccountProtectsAllMembersAndFencesCredentials(t *testing.T) {
	db := setupChannelMonitorCustomActionRefreshDB(t, "sqlite")
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorUpstreamAccount{}))
	config := automationTestConfig("https://account.example")
	config.CustomConfig.Actions = nil
	config.CustomConfig.Balance = service.ChannelMonitorCustomMetricConfig{Source: "fixed", FixedValue: common.GetPointer(0.5)}
	raw, err := service.MarshalChannelMonitorCustomUpstreamConfig(config.CustomConfig)
	require.NoError(t, err)
	for i, status := range []int{common.ChannelStatusEnabled, common.ChannelStatusEnabled, common.ChannelStatusManuallyDisabled} {
		id := 101 + i
		require.NoError(t, db.Create(&model.Channel{Id: id, Name: fmt.Sprint(id), Key: "key", Status: status, Group: "default"}).Error)
		require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: id, Ratio: 1, UpdatedTime: 1, UpstreamRevision: 1, UpstreamType: "custom", UpstreamBaseURL: config.BaseURL, CustomUpstreamConfig: raw, BalanceAutoDisableThreshold: common.GetPointer(1.0)}).Error)
	}
	source, err := model.GetChannelRatioMonitor(101)
	require.NoError(t, err)
	settings := model.ChannelMonitorAccountSettingsFromMonitor(source)
	encoded, err := common.Marshal(settings)
	require.NoError(t, err)
	account := model.ChannelMonitorUpstreamAccount{Name: "余额保护", Settings: string(encoded)}
	members, err := model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, nil, []int{101, 102, 103})
	require.NoError(t, err)
	_, err = fetchAndRecordUpstreamAccountBalance(t.Context(), members[0], time.Second)
	require.NoError(t, err)
	for _, id := range []int{101, 102} {
		channel, err := model.GetChannelById(id, true)
		require.NoError(t, err)
		assert.True(t, channelMonitorAutoDisabledForLowBalance(channel))
	}
	manual, err := model.GetChannelById(103, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, manual.Status)
	locked, err := model.AcquireUpstreamAccountLease(t.Context(), account.ID, account.Revision, "running", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, locked)
	locked, err = model.AcquireUpstreamAccountLease(t.Context(), account.ID, account.Revision, "other", common.GetTimestamp()+60)
	require.NoError(t, err)
	assert.False(t, locked)
	_, err = model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, &account, nil)
	require.ErrorContains(t, err, "正在执行")
	require.ErrorIs(t, model.RecordUpstreamAccountBalance(t.Context(), account.ID, account.Revision, "other", common.GetPointer(999.0), ""), model.ErrUpstreamAccountChanged)
	settings.UpstreamAccessToken = "rotated-token"
	require.NoError(t, model.RefreshUpstreamAccountCredentials(t.Context(), account, settings))
	require.ErrorIs(t, model.RefreshUpstreamAccountCredentials(t.Context(), account, settings), model.ErrUpstreamAccountChanged)
	for _, id := range []int{101, 102, 103} {
		member, err := model.GetChannelRatioMonitor(id)
		require.NoError(t, err)
		assert.Equal(t, "rotated-token", member.UpstreamAccessToken)
		assert.Equal(t, 1.0, member.Ratio)
	}
}

func TestUpstreamAccountAutomationInheritsBuiltinAuthentication(t *testing.T) {
	db := setupChannelMonitorCustomActionRefreshDB(t, "sqlite")
	disableChannelMonitorSSRFProtection(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorUpstreamAccount{}))
	var balanceReads, actions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":500000}}`))
		case "/api/user/self":
			balanceReads.Add(1)
			_, _ = fmt.Fprintf(w, `{"success":true,"data":{"quota":%d}}`, actions.Load()*50*500000)
		case "/reset":
			assert.Equal(t, "Bearer shared-token", r.Header.Get("Authorization"))
			assert.Equal(t, "7", r.Header.Get("New-Api-User"))
			actions.Add(1)
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	settings, err := common.Marshal(model.ChannelMonitorAccountSettings{UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL, UpstreamAuthType: service.NewAPIUpstreamAuthUser, UpstreamUserId: 7, UpstreamAccessToken: "shared-token"})
	require.NoError(t, err)
	account := model.ChannelMonitorUpstreamAccount{Name: "内置账户认证", Settings: string(settings)}
	_, err = model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, nil, []int{})
	require.NoError(t, err)
	input := automationTestConfig(server.URL)
	input.AccountID = account.ID
	task, err := service.SaveUpstreamAutomation(t.Context(), input)
	require.NoError(t, err)
	row, err := model.GetUpstreamAutomation(t.Context(), task.ID)
	require.NoError(t, err)
	assert.NotContains(t, row.Payload, "shared-token")
	assert.NotContains(t, row.Payload, server.URL)
	_, err = service.RunUpstreamAutomation(t.Context(), task.ID, true, func() time.Time { return time.Date(2026, 9, 17, 4, 0, 0, 0, time.UTC) }, refreshUpstreamAutomationChannels)
	require.NoError(t, err)
	assert.EqualValues(t, 1, actions.Load())
	assert.EqualValues(t, 2, balanceReads.Load(), "触发前检查和触发后刷新各一次")
	account, err = model.GetChannelMonitorUpstreamAccount(t.Context(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, account.Balance)
	assert.Equal(t, 50.0, *account.Balance)
}
