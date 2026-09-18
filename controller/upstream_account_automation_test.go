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

func createAutomationTestAccount(t *testing.T, baseURL string) model.ChannelMonitorUpstreamAccount {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.ChannelMonitorUpstreamAccount{}))
	t.Cleanup(func() { assert.NoError(t, model.DB.Migrator().DropTable(&model.ChannelMonitorUpstreamAccount{})) })
	config := automationTestConfig(baseURL).CustomConfig
	config.Actions = nil
	raw, err := service.MarshalChannelMonitorCustomUpstreamConfig(config)
	require.NoError(t, err)
	settings, err := common.Marshal(model.ChannelMonitorAccountSettings{
		UpstreamType: service.CustomUpstreamType, UpstreamBaseURL: baseURL, CustomUpstreamConfig: raw,
	})
	require.NoError(t, err)
	account := model.ChannelMonitorUpstreamAccount{Name: "共享账户", Settings: string(settings)}
	_, err = model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, nil, nil)
	require.NoError(t, err)
	return account
}

func TestUpstreamAccountIndependentAutomationsDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			setupChannelMonitorCustomActionRefreshDB(t, engine)
			disableChannelMonitorSSRFProtection(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/reset" {
					calls.Add(1)
					_, _ = w.Write([]byte(`{"success":true}`))
					return
				}
				_, _ = w.Write([]byte(`{"balance":0}`))
			}))
			defer server.Close()
			account := createAutomationTestAccount(t, server.URL)
			first := automationTestConfig(server.URL)
			first.AccountID, first.CustomConfig.Actions[0].DailyLimit = account.ID, 1
			one, err := service.SaveUpstreamAutomation(t.Context(), first)
			require.NoError(t, err)
			second := automationTestConfig(server.URL)
			second.Name, second.AccountID, second.IntervalMinutes = "第二任务", account.ID, 7
			second.CustomConfig.Actions[0].CooldownMinutes = 3
			two, err := service.SaveUpstreamAutomation(t.Context(), second)
			require.NoError(t, err, "同一账户允许保存多个任务")
			now := time.Date(2026, 9, 18, 4, 0, 0, 0, time.UTC)
			clock := func() time.Time { return now }
			one, err = service.RunUpstreamAutomation(t.Context(), one.ID, false, clock, nil)
			require.NoError(t, err)
			assert.EqualValues(t, 1, calls.Load())
			assert.Equal(t, now.Add(time.Minute).Unix(), one.State.NextCheck)
			two, err = service.RunUpstreamAutomation(t.Context(), two.ID, false, clock, nil)
			require.NoError(t, err)
			assert.EqualValues(t, 2, calls.Load(), "相同规则在各任务中分别执行")
			assert.Equal(t, 1, two.State.Actions["reset"].Attempts)
			assert.Equal(t, now.Add(7*time.Minute).Unix(), two.State.NextCheck)
			row, err := model.GetUpstreamAutomation(t.Context(), one.ID)
			require.NoError(t, err)
			unchanged, err := service.UpstreamAutomationResponse(row)
			require.NoError(t, err)
			assert.Equal(t, one.State, unchanged.State, "其他任务执行不改变本任务的计数、调度或历史")
			assert.NotContains(t, row.Payload, server.URL, "账户认证及地址仍由账户统一维护")
			now = now.Add(2 * time.Minute)
			one, err = service.RunUpstreamAutomation(t.Context(), one.ID, false, clock, nil)
			require.NoError(t, err)
			assert.Equal(t, "当日调用次数已用完", one.State.Actions["reset"].SkipReason)
			two, err = service.RunUpstreamAutomation(t.Context(), two.ID, true, clock, nil)
			require.NoError(t, err)
			assert.Equal(t, "尚在冷却时间内", two.State.Actions["reset"].SkipReason)
			assert.EqualValues(t, 2, calls.Load())
			now = now.Add(time.Minute)
			two, err = service.RunUpstreamAutomation(t.Context(), two.ID, true, clock, nil)
			require.NoError(t, err)
			assert.EqualValues(t, 3, calls.Load())
			assert.Equal(t, 2, two.State.Actions["reset"].Attempts)
			assert.Equal(t, 1, one.State.Actions["reset"].Attempts)
		})
	}
}

func TestUpstreamAccountBusyDoesNotConsumeAutomationSchedule(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			setupChannelMonitorCustomActionRefreshDB(t, engine)
			disableChannelMonitorSSRFProtection(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, `{"balance":0,"success":true}`)
			}))
			defer server.Close()
			account := createAutomationTestAccount(t, server.URL)
			input := automationTestConfig(server.URL)
			input.AccountID = account.ID
			task, err := service.SaveUpstreamAutomation(t.Context(), input)
			require.NoError(t, err)
			locked, err := model.AcquireUpstreamAccountLease(t.Context(), account.ID, account.Revision, "other-task", common.GetTimestamp()+300)
			require.NoError(t, err)
			require.True(t, locked)
			clock := func() time.Time { return time.Date(2026, 9, 18, 4, 0, 0, 0, time.UTC) }
			deferred, err := service.RunUpstreamAutomation(t.Context(), task.ID, false, clock, nil)
			require.ErrorIs(t, err, service.ErrUpstreamAutomationNotDue)
			assert.Equal(t, task.State, deferred.State, "账户占用不消耗检查间隔、不累计失败或调用次数")
			require.NoError(t, model.ReleaseUpstreamAccountLease(t.Context(), account.ID, "other-task"))
			finished, err := service.RunUpstreamAutomation(t.Context(), task.ID, false, clock, nil)
			require.NoError(t, err, "账户空闲后下轮调度仍可立即执行")
			assert.Equal(t, 1, finished.State.Actions["reset"].Attempts)
		})
	}
}

func TestUpstreamAccountAssociationPreservesChannelAutomations(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelMonitorCustomActionRefreshDB(t, engine)
			disableChannelMonitorSSRFProtection(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/reset" {
					calls.Add(1)
				}
				_, _ = w.Write([]byte(`{"balance":0,"success":true}`))
			}))
			defer server.Close()
			input := automationTestConfig(server.URL)
			custom := input.CustomConfig
			custom.Actions = nil
			raw, err := service.MarshalChannelMonitorCustomUpstreamConfig(custom)
			require.NoError(t, err)
			require.NoError(t, db.Create(&model.Channel{Id: 101, Name: "原渠道", Key: "relay-key", Status: common.ChannelStatusEnabled, Group: "default"}).Error)
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 101, Ratio: 1, UpdatedTime: 1, UpstreamType: "custom", UpstreamBaseURL: server.URL, UpstreamRevision: 1, CustomUpstreamConfig: raw}).Error)
			input.ChannelIDs = []int{101}
			legacy, err := service.SaveUpstreamAutomation(t.Context(), input)
			require.NoError(t, err)
			now := time.Date(2026, 9, 18, 4, 0, 0, 0, time.UTC)
			clock := func() time.Time { return now }
			legacy, err = service.RunUpstreamAutomation(t.Context(), legacy.ID, false, clock, refreshUpstreamAutomationChannels)
			require.NoError(t, err)
			assert.EqualValues(t, 1, calls.Load())
			account := createAutomationTestAccount(t, server.URL)
			previous := account
			_, err = model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, &previous, []int{101})
			require.NoError(t, err)
			row, err := model.GetUpstreamAutomation(t.Context(), legacy.ID)
			require.NoError(t, err)
			unchanged, err := service.UpstreamAutomationResponse(row)
			require.NoError(t, err)
			assert.Equal(t, legacy, unchanged, "关联账户保留原任务规则、计数及历史")
			now = now.Add(time.Minute)
			legacy, err = service.RunUpstreamAutomation(t.Context(), legacy.ID, false, clock, refreshUpstreamAutomationChannels)
			require.NoError(t, err, "关联账户后旧任务仍独立执行")
			assert.EqualValues(t, 2, calls.Load())
			assert.Equal(t, 2, legacy.State.Actions["reset"].Attempts)
			input.AccountID = account.ID
			fresh, err := service.SaveUpstreamAutomation(t.Context(), input)
			require.NoError(t, err, "旧任务不阻止创建账户任务")
			fresh, err = service.RunUpstreamAutomation(t.Context(), fresh.ID, false, clock, refreshUpstreamAutomationChannels)
			require.NoError(t, err)
			assert.EqualValues(t, 3, calls.Load())
			assert.Equal(t, 1, fresh.State.Actions["reset"].Attempts)
		})
	}
}

func TestUpstreamAutomationLegacyDisabledTaskRemainsEditable(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelMonitorCustomActionRefreshDB(t, engine)
			input := automationTestConfig("https://upstream.example")
			input.Enabled = false
			task, err := service.SaveUpstreamAutomation(t.Context(), input)
			require.NoError(t, err)
			row, err := model.GetUpstreamAutomation(t.Context(), task.ID)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, common.UnmarshalJsonStr(row.Payload, &payload))
			payload["merged_into"] = "previous-target"
			encoded, err := common.Marshal(payload)
			require.NoError(t, err)
			state := task.State
			state.Actions["reset"] = model.ChannelMonitorCustomActionState{Attempts: 2, LastAttempt: 100, NeedsConfirmation: true}
			state.History = []model.UpstreamAutomationEvent{{ID: "previous-event", Time: 100, Status: "merged", Message: "旧任务历史"}}
			encodedState, err := common.Marshal(state)
			require.NoError(t, err)
			require.NoError(t, db.Model(&row).Updates(map[string]any{"payload": string(encoded), "state": string(encodedState)}).Error)
			_, err = service.RunUpstreamAutomation(t.Context(), task.ID, true, time.Now, nil)
			require.ErrorIs(t, err, service.ErrUpstreamAutomationNotDue, "历史任务不会自动恢复执行")
			task.Name = "重新整理的任务"
			saved, err := service.SaveUpstreamAutomation(t.Context(), task.UpstreamAutomationConfig)
			require.NoError(t, err)
			assert.False(t, saved.Enabled)
			assert.Equal(t, state.Actions, saved.State.Actions)
			assert.Equal(t, state.History, saved.State.History)
			row, err = model.GetUpstreamAutomation(t.Context(), task.ID)
			require.NoError(t, err)
			assert.NotContains(t, row.Payload, "merged_into")
		})
	}
}
