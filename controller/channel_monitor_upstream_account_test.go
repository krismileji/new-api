package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
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
			useChannelMonitorOptionMap(t, map[string]string{channelMonitorAutoUpdateIntervalOption: "5"})
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
			account := model.ChannelMonitorUpstreamAccount{Name: "同一余额池", Settings: string(raw), RefreshIntervalMinutes: 0}
			members, err := model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, nil, []int{101, 102}, map[int]int64{101: 1, 102: 1})
			require.NoError(t, err)
			require.Len(t, members, 2)
			ctx := withUpstreamAccountBalanceRound(t.Context())
			for _, m := range members {
				outcome, err := fetchAndRecordChannelMonitorUpstreamRatio(ctx, m, nil, "", time.Second, channelMonitorRefreshOptions{IncludeSeparateBalance: true}, 0, "测试")
				require.NoError(t, err)
				assert.Equal(t, m.Ratio, outcome.Result.Ratio)
				require.NotNil(t, outcome.Result.Balance.Amount)
				assert.Equal(t, 100.0, *outcome.Result.Balance.Amount)
			}
			assert.EqualValues(t, 1, polls.Load(), "两个不同倍率的渠道只查询一次余额")
			checkTime := time.Now().Truncate(5 * time.Minute).Add(3 * time.Minute)
			for _, tc := range []struct {
				name           string
				globalInterval string
				legacyInterval int
				manual         bool
				forced         bool
				wantFetch      bool
			}{
				{name: "全局关闭覆盖旧账户间隔", globalInterval: "0", legacyInterval: 1},
				{name: "延长全局间隔后复用余额", globalInterval: "5", legacyInterval: 1},
				{name: "缩短全局间隔后刷新余额", globalInterval: "1", legacyInterval: 60, wantFetch: true},
				{name: "旧账户关闭值不阻止全局刷新", globalInterval: "1", legacyInterval: 0, wantFetch: true},
				{name: "全局关闭后仍可手动刷新", globalInterval: "0", manual: true, wantFetch: true},
				{name: "全局关闭后自动任务仍可按需刷新", globalInterval: "0", forced: true, wantFetch: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					useChannelMonitorOptionMap(t, map[string]string{channelMonitorAutoUpdateIntervalOption: tc.globalInterval})
					intervalMinutes, err := strconv.Atoi(tc.globalInterval)
					require.NoError(t, err)
					require.NoError(t, db.Model(&model.ChannelMonitorUpstreamAccount{}).Where("id = ?", account.ID).Updates(map[string]any{
						"refresh_interval_minutes": tc.legacyInterval,
						"last_balance_check":       checkTime.Add(-2 * time.Minute).Unix(),
					}).Error)
					pollContext := t.Context()
					if !tc.manual {
						pollContext = withUpstreamAccountBalanceRound(pollContext)
						round := pollContext.Value(upstreamAccountBalanceRoundKey{}).(*upstreamAccountBalanceRound)
						round.forceRefresh = tc.forced
						round.start = checkTime.Truncate(time.Duration(intervalMinutes) * time.Minute).Unix()
					}
					before := polls.Load()
					outcome, err := fetchAndRecordUpstreamAccountBalance(pollContext, members[0], time.Second)
					require.NoError(t, err)
					require.NotNil(t, outcome.Result.Balance.Amount)
					assert.Equal(t, 100.0, *outcome.Result.Balance.Amount)
					expectedPolls := before
					if tc.wantFetch {
						expectedPolls++
					}
					assert.Equal(t, expectedPolls, polls.Load())
				})
			}
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
			viewJSON, err := common.Marshal(view)
			require.NoError(t, err)
			assert.NotContains(t, string(viewJSON), "refresh_interval_minutes")

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

func TestChannelMonitorSharedAccountPollsBalanceOnceWhileRatiosRun(t *testing.T) {
	db := setupChannelMonitorBalanceSafetyDB(t, "sqlite")
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorUpstreamAccount{}))
	disableChannelMonitorSSRFProtection(t)
	useChannelMonitorOptionMap(t, map[string]string{channelMonitorAutoUpdateRetryCountOption: "2"})
	ratioRequests := make(chan struct{}, 2)
	balanceRequests := make(chan struct{}, 2)
	releaseRequests := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/self/groups":
			ratioRequests <- struct{}{}
			select {
			case <-releaseRequests:
				_, _ = w.Write([]byte(`{"success":true,"data":{"cheap":{"ratio":1.2},"vip":{"ratio":1.8}}}`))
			case <-r.Context().Done():
			}
		case "/api/user/self":
			balanceRequests <- struct{}{}
			select {
			case <-releaseRequests:
				_, _ = w.Write([]byte(`{"success":true,"data":{"quota":900}}`))
			case <-r.Context().Done():
			}
		case "/api/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":100}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	accountSettings := model.ChannelMonitorAccountSettings{
		UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL, UpstreamAuthType: service.NewAPIUpstreamAuthUser,
		UpstreamUserId: 1, UpstreamAccessToken: "service-account",
	}
	raw, err := common.Marshal(accountSettings)
	require.NoError(t, err)
	account := model.ChannelMonitorUpstreamAccount{Name: "共用余额", Revision: 1, Settings: string(raw)}
	require.NoError(t, db.Create(&account).Error)
	for i, group := range []string{"cheap", "vip"} {
		id := i + 1
		require.NoError(t, db.Create(&model.Channel{Id: id, Name: group, Status: common.ChannelStatusEnabled}).Error)
		require.NoError(t, db.Create(&model.ChannelRatioMonitor{
			ChannelId: id, UpstreamRevision: 1, UpstreamAccountID: account.ID, UpstreamAccountRevision: account.Revision,
			UpstreamType: service.NewAPIUpstreamType, UpstreamAuthType: service.NewAPIUpstreamAuthUser, UpstreamGroup: group,
		}).Error)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type outcome struct {
		summary channelRatioMonitorTaskResult
		err     error
	}
	results := make(chan outcome, 1)
	go func() {
		summary, runErr := collectChannelRatioMonitorRound(ctx, nil, 0)
		results <- outcome{summary: summary, err: runErr}
	}()
	for _, started := range []<-chan struct{}{balanceRequests, ratioRequests, ratioRequests} {
		select {
		case <-started:
		case <-ctx.Done():
			cancel()
			<-results
			t.Fatal("余额和两个渠道的倍率应当可以同时查询")
		}
	}
	close(releaseRequests)
	result := <-results
	require.NoError(t, result.err)
	assert.Zero(t, result.summary.Failed)
	assert.Equal(t, 2, result.summary.BalanceUpdated)
	assert.Empty(t, balanceRequests, "每个账户每轮只查询一次余额")
	for i, wantRatio := range []float64{1.2, 1.8} {
		monitor, err := model.GetChannelRatioMonitor(i + 1)
		require.NoError(t, err)
		assert.Equal(t, wantRatio, monitor.Ratio)
		require.NotNil(t, monitor.UpstreamBalance)
		assert.Equal(t, 9.0, *monitor.UpstreamBalance)
	}
}

func TestUpstreamAccountProtectsAllMembersAndFencesCredentials(t *testing.T) {
	db := setupChannelMonitorCustomActionRefreshDB(t, "sqlite")
	disableChannelMonitorSSRFProtection(t)
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
	require.NoError(t, err)
	assert.True(t, account.PendingSave)
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

func TestUpstreamConfigurationDeferredEditsDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelMonitorCustomActionRefreshDB(t, engine)
			disableChannelMonitorSSRFProtection(t)
			require.NoError(t, db.AutoMigrate(&model.ChannelMonitorUpstreamAccount{}))
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&model.ChannelMonitorUpstreamAccount{})) })
			settings := model.ChannelMonitorAccountSettings{UpstreamType: "new_api", UpstreamBaseURL: "https://account.example", UpstreamAccessToken: "stored-secret"}
			raw, err := common.Marshal(settings)
			require.NoError(t, err)
			account := model.ChannelMonitorUpstreamAccount{Name: "原配置", Settings: string(raw)}
			require.NoError(t, db.Create(&model.Channel{Id: 101, Name: "关联渠道", Key: "key", Group: "default"}).Error)
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 101, Ratio: 1, UpstreamRevision: 1}).Error)
			_, err = model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, nil, []int{101})
			require.NoError(t, err)
			expected := account
			locked, err := model.AcquireUpstreamAccountLease(t.Context(), account.ID, account.Revision, "running", common.GetTimestamp()+60)
			require.NoError(t, err)
			require.True(t, locked)
			account.Name = "暂存配置"
			_, err = model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, &expected, nil)
			require.NoError(t, err)
			assert.True(t, account.PendingSave)
			account.Name = "最新暂存"
			_, err = model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, &expected, nil)
			require.NoError(t, err)
			_, err = model.ApplyPendingUpstreamAccountEdits(t.Context())
			require.NoError(t, err)
			current, err := model.GetChannelMonitorUpstreamAccount(t.Context(), account.ID)
			require.NoError(t, err)
			assert.Equal(t, "原配置", current.Name)
			assert.Equal(t, "running", current.LeaseID)
			view, err := channelMonitorAccountView(t.Context(), current)
			require.NoError(t, err)
			assert.True(t, view.PendingSave)
			rows, _, err := model.ListSystemTasks(model.SystemTaskFilter{}, 0, 100)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			response, err := common.Marshal(rows[0].ToResponse())
			require.NoError(t, err)
			assert.NotContains(t, string(response), "stored-secret")
			settings.UpstreamAccessToken = "rotated-secret"
			require.NoError(t, model.RefreshUpstreamAccountCredentials(t.Context(), current, settings))
			require.NoError(t, model.ReleaseUpstreamAccountLease(t.Context(), account.ID, "running"))
			_, err = model.ApplyPendingUpstreamAccountEdits(t.Context())
			require.NoError(t, err)
			current, err = model.GetChannelMonitorUpstreamAccount(t.Context(), account.ID)
			require.NoError(t, err)
			assert.Equal(t, "最新暂存", current.Name)
			assert.Equal(t, expected.Revision+1, current.Revision)
			assert.Contains(t, current.Settings, "rotated-secret")
			member, err := model.GetChannelRatioMonitor(101)
			require.NoError(t, err)
			assert.Equal(t, "rotated-secret", member.UpstreamAccessToken)
			_, err = model.ApplyPendingUpstreamAccountEdits(t.Context())
			require.NoError(t, err)
			status, _, err := model.UpstreamAccountEditStatus(t.Context(), account.ID)
			require.NoError(t, err)
			assert.Empty(t, status)
			_, err = model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, &expected, nil)
			require.ErrorIs(t, err, model.ErrUpstreamAccountChanged)

			// A setting edit keeps an unrelated token rotation from the run.
			expected = current
			account = current
			desired, err := account.MonitorSettings()
			require.NoError(t, err)
			desired.BalanceWarningThreshold = common.GetPointer(5.0)
			raw, err = common.Marshal(desired)
			require.NoError(t, err)
			account.Settings = string(raw)
			locked, err = model.AcquireUpstreamAccountLease(t.Context(), account.ID, account.Revision, "second-run", common.GetTimestamp()+60)
			require.NoError(t, err)
			require.True(t, locked)
			_, err = model.SaveChannelMonitorUpstreamAccount(t.Context(), &account, &expected, nil)
			require.NoError(t, err)
			latest, err := current.MonitorSettings()
			require.NoError(t, err)
			latest.UpstreamAccessToken = "latest-rotation"
			require.NoError(t, model.RefreshUpstreamAccountCredentials(t.Context(), current, latest))
			require.NoError(t, model.ReleaseUpstreamAccountLease(t.Context(), account.ID, "second-run"))
			_, err = model.ApplyPendingUpstreamAccountEdits(t.Context())
			require.NoError(t, err)
			current, err = model.GetChannelMonitorUpstreamAccount(t.Context(), account.ID)
			require.NoError(t, err)
			appliedSettings, err := current.MonitorSettings()
			require.NoError(t, err)
			assert.Equal(t, common.GetPointer(5.0), appliedSettings.BalanceWarningThreshold)
			assert.Equal(t, "latest-rotation", appliedSettings.UpstreamAccessToken)

			config := automationTestConfig("https://automation.example")
			task, err := service.SaveUpstreamAutomation(t.Context(), config)
			require.NoError(t, err)
			_, err = model.MutateUpstreamAutomation(t.Context(), task.ID, false, 0, 0, func(row *model.SystemTask) error {
				task.State.LeaseID, task.State.LeaseUntil = "task-running", common.GetTimestamp()+60
				task.State.Actions["reset"] = model.ChannelMonitorCustomActionState{Attempts: 2}
				encoded, err := common.Marshal(task.State)
				row.State = string(encoded)
				return err
			})
			require.NoError(t, err)
			draft := task.UpstreamAutomationConfig
			draft.Name, draft.Enabled = "执行完暂停", false
			pending, err := service.SaveUpstreamAutomation(t.Context(), draft)
			require.NoError(t, err)
			assert.True(t, pending.State.PendingSave)
			invalid := draft
			invalid.IntervalMinutes = 0
			_, err = service.SaveUpstreamAutomation(t.Context(), invalid)
			require.Error(t, err)
			stale := draft
			stale.Revision--
			_, err = service.SaveUpstreamAutomation(t.Context(), stale)
			require.ErrorContains(t, err, "配置已变化")
			require.NoError(t, service.ApplyPendingUpstreamAutomationEdits(t.Context()))
			stored, err := model.GetUpstreamAutomation(t.Context(), task.ID)
			require.NoError(t, err)
			assert.Contains(t, stored.Payload, `"enabled":true`)
			assert.NotEmpty(t, stored.Result)
			_, err = model.MutateUpstreamAutomation(t.Context(), task.ID, false, 0, 0, func(row *model.SystemTask) error {
				var state model.UpstreamAutomationState
				if err := common.UnmarshalJsonStr(row.State, &state); err != nil {
					return err
				}
				state.LeaseID, state.LeaseUntil = "", 0
				encoded, err := common.Marshal(state)
				row.State = string(encoded)
				return err
			})
			require.NoError(t, err)
			require.NoError(t, service.ApplyPendingUpstreamAutomationEdits(t.Context()))
			stored, err = model.GetUpstreamAutomation(t.Context(), task.ID)
			require.NoError(t, err)
			applied, err := service.UpstreamAutomationResponse(stored)
			require.NoError(t, err)
			assert.False(t, applied.Enabled)
			assert.False(t, applied.State.PendingSave)
			assert.Equal(t, "执行完暂停", applied.Name)
			assert.Equal(t, 2, applied.State.Actions["reset"].Attempts)
			assert.Empty(t, stored.Result)
		})
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
