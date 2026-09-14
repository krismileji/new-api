package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateChannelMonitorSettingsAcceptsZeroConsecutiveFailureLimit(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	usePersistedChannelMonitorOptions(t, db, map[string]string{
		channelMonitorAutoUpdateConsecutiveFailureLimitOption: "3",
	})
	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default",
	}).Error)
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"auto_update_consecutive_failure_limit": 0,
	})

	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var response channelMonitorSettingsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	assert.Zero(t, response.Data.AutoUpdateConsecutiveFailureLimit)

	var option model.Option
	require.NoError(t, db.Where("key = ?", channelMonitorAutoUpdateConsecutiveFailureLimitOption).First(&option).Error)
	assert.Equal(t, "0", option.Value)
	assert.Zero(t, getChannelMonitorSettings().AutoUpdateConsecutiveFailureLimit)
}

func TestRunChannelRatioMonitorTaskZeroFailureLimitKeepsRetrying(t *testing.T) {
	for _, failureType := range []string{model.ChannelRatioFailureAlertRatio, model.ChannelRatioFailureAlertBalance} {
		t.Run(failureType, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			useChannelMonitorOptionMap(t, map[string]string{
				channelMonitorAutoUpdateRetryCountOption:              "2",
				channelMonitorAutoUpdateConsecutiveFailureLimitOption: "0",
			})
			disableChannelMonitorSSRFProtection(t)

			var requestCount atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requestCount.Add(1)
				w.WriteHeader(http.StatusBadGateway)
			}))
			defer server.Close()
			require.NoError(t, db.Create(&model.Channel{
				Id: 1, Name: "持续同步", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
			}).Error)
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{
				ChannelId: 1, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
				UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
				UpstreamUserId: 42, UpstreamAccessToken: "test-token",
				UpstreamRatioSyncDisabled:   failureType != model.ChannelRatioFailureAlertRatio,
				UpstreamBalanceSyncDisabled: failureType != model.ChannelRatioFailureAlertBalance,
				ConsecutiveFailures:         100, BalanceConsecutiveFailures: 100,
				LastFetchStatus: model.ChannelRatioFetchStatusFailed,
				LastFetchError:  "此前倍率同步失败", LastBalanceError: "此前余额同步失败",
			}).Error)

			previousRequests := int32(0)
			for _, wantFailures := range []int{103, 106} {
				summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
				require.NoError(t, err)
				assert.Equal(t, 1, summary.Failed)
				assert.Equal(t, 2, summary.Retried)
				assert.Zero(t, summary.Skipped)
				assert.Greater(t, requestCount.Load(), previousRequests)
				previousRequests = requestCount.Load()
				monitor, err := model.GetChannelRatioMonitor(1)
				require.NoError(t, err)
				if failureType == model.ChannelRatioFailureAlertRatio {
					assert.Equal(t, wantFailures, monitor.ConsecutiveFailures)
					assert.Equal(t, 100, monitor.BalanceConsecutiveFailures)
				} else {
					assert.Equal(t, 100, monitor.ConsecutiveFailures)
					assert.Equal(t, wantFailures, monitor.BalanceConsecutiveFailures)
				}
			}
		})
	}
}

func TestRunChannelRatioMonitorTaskConfiguredFailureAlertNotifiesOncePerFailureEpisode(t *testing.T) {
	for _, test := range []struct {
		failureType string
		stopLimit   int
	}{
		{failureType: model.ChannelRatioFailureAlertRatio, stopLimit: 0},
		{failureType: model.ChannelRatioFailureAlertBalance, stopLimit: 0},
		{failureType: model.ChannelRatioFailureAlertRatio, stopLimit: 20},
		{failureType: model.ChannelRatioFailureAlertBalance, stopLimit: 20},
	} {
		t.Run(test.failureType+"/stop_"+strconv.Itoa(test.stopLimit), func(t *testing.T) {
			failureType := test.failureType
			db := setupChannelMonitorControllerTestDB(t)
			disableChannelMonitorSSRFProtection(t)
			var upstreamHealthy atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !upstreamHealthy.Load() {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/user/self/groups":
					_, _ = w.Write([]byte(`{"success":true,"data":{"vip":{"ratio":1.25}}}`))
				case "/api/user/self":
					_, _ = w.Write([]byte(`{"success":true,"data":{"quota":500}}`))
				case "/api/status":
					_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":100}}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			require.NoError(t, db.Create(&model.Channel{
				Id: 1, Name: "持续同步并通知", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
			}).Error)
			monitor := model.ChannelRatioMonitor{
				ChannelId: 1, Ratio: 1, UpdatedTime: 1,
				UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
				UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
				UpstreamUserId: 42, UpstreamAccessToken: "test-token",
				UpstreamRatioSyncDisabled:   failureType != model.ChannelRatioFailureAlertRatio,
				UpstreamBalanceSyncDisabled: failureType != model.ChannelRatioFailureAlertBalance,
			}
			if failureType == model.ChannelRatioFailureAlertRatio {
				monitor.ConsecutiveFailures = 1
				monitor.LastFetchStatus = model.ChannelRatioFetchStatusFailed
				monitor.LastFetchError = "此前倍率同步失败"
			} else {
				monitor.BalanceConsecutiveFailures = 1
				monitor.LastBalanceError = "此前余额同步失败"
			}
			require.NoError(t, db.Create(&monitor).Error)

			emailAttempts := 0
			var emailSendError error
			sendEmail := func(subject string, receiver string, content string) error {
				emailAttempts++
				assert.Contains(t, subject, "1 项更新失败")
				assert.Equal(t, "alerts@example.com", receiver)
				assert.Contains(t, content, "上游同步失败")
				return emailSendError
			}
			for _, step := range []struct {
				name              string
				healthy           bool
				retryCount        int
				sendError         error
				wantFailures      int
				wantEmailAttempts int
				wantEmailStatus   string
				wantNotified      bool
			}{
				{name: "未达告警阈值", wantFailures: 2},
				{name: "达到配置的三次但邮件发送失败", sendError: errors.New("smtp unavailable"), wantFailures: 3, wantEmailAttempts: 1, wantEmailStatus: "failed"},
				{name: "继续更新并重试未送达邮件", wantFailures: 4, wantEmailAttempts: 2, wantEmailStatus: "sent", wantNotified: true},
				{name: "持续失败不重复通知", wantFailures: 5, wantEmailAttempts: 2, wantNotified: true},
				{name: "同步恢复后清除通知状态", healthy: true, wantEmailAttempts: 2},
				{name: "恢复后再次达到配置次数重新通知", retryCount: 2, wantFailures: 3, wantEmailAttempts: 3, wantEmailStatus: "sent", wantNotified: true},
				{name: "新一轮失败仍只通知一次", wantFailures: 4, wantEmailAttempts: 3, wantNotified: true},
			} {
				useChannelMonitorOptionMap(t, map[string]string{
					channelMonitorAutoUpdateRetryCountOption:              strconv.Itoa(step.retryCount),
					channelMonitorAutoUpdateConsecutiveFailureLimitOption: strconv.Itoa(test.stopLimit),
					channelMonitorSyncFailureAlertThresholdOption:         "3",
					channelMonitorEmailNotificationOption:                 "true",
					channelMonitorNotificationEmailOption:                 "alerts@example.com",
					channelMonitorEmailNotificationTypesOption:            `["upstream_sync_failed"]`,
				})
				upstreamHealthy.Store(step.healthy)
				emailSendError = step.sendError
				summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, sendEmail)
				require.NoError(t, err, step.name)
				assert.Equal(t, step.wantEmailAttempts, emailAttempts, step.name)
				assert.Equal(t, step.wantEmailStatus, summary.EmailStatus, step.name)
				assert.Equal(t, step.retryCount, summary.Retried, step.name)
				assert.Zero(t, summary.Skipped, step.name)
				if step.healthy {
					assert.Equal(t, 1, summary.Updated, step.name)
					assert.Zero(t, summary.Failed, step.name)
				} else {
					assert.Equal(t, 1, summary.Failed, step.name)
				}
				stored, err := model.GetChannelRatioMonitor(1)
				require.NoError(t, err, step.name)
				if failureType == model.ChannelRatioFailureAlertRatio {
					assert.Equal(t, step.wantFailures, stored.ConsecutiveFailures, step.name)
					assert.Equal(t, step.wantNotified, stored.FetchFailureAlertNotified, step.name)
					assert.False(t, stored.BalanceFailureAlertNotified, step.name)
				} else {
					assert.Equal(t, step.wantFailures, stored.BalanceConsecutiveFailures, step.name)
					assert.Equal(t, step.wantNotified, stored.BalanceFailureAlertNotified, step.name)
					assert.False(t, stored.FetchFailureAlertNotified, step.name)
				}
			}
		})
	}
}

func TestRunChannelRatioMonitorTaskZeroFailureLimitResumesOnlyEnabledSyncs(t *testing.T) {
	for _, test := range []struct {
		name            string
		ratioDisabled   bool
		balanceDisabled bool
	}{
		{name: "恢复倍率和余额同步"},
		{name: "保留手动关闭余额同步", balanceDisabled: true},
		{name: "保留手动关闭倍率同步", ratioDisabled: true},
		{name: "保留手动关闭全部同步", ratioDisabled: true, balanceDisabled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			useChannelMonitorOptionMap(t, map[string]string{
				channelMonitorAutoUpdateConsecutiveFailureLimitOption: "0",
			})
			disableChannelMonitorSSRFProtection(t)
			var ratioRequests, balanceRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/user/self/groups":
					ratioRequests.Add(1)
					_, _ = w.Write([]byte(`{"success":true,"data":{"vip":{"ratio":1.25}}}`))
				case "/api/user/self":
					balanceRequests.Add(1)
					_, _ = w.Write([]byte(`{"success":true,"data":{"quota":500}}`))
				case "/api/status":
					_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":100}}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			require.NoError(t, db.Create(&model.Channel{
				Id: 1, Name: "恢复同步", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
			}).Error)
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{
				ChannelId: 1, Ratio: 1, UpdatedTime: 1,
				UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
				UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
				UpstreamUserId: 42, UpstreamAccessToken: "test-token",
				ConsecutiveFailures: 100, BalanceConsecutiveFailures: 100,
				LastFetchStatus: model.ChannelRatioFetchStatusFailed,
				LastFetchError:  "此前倍率同步失败", LastBalanceError: "此前余额同步失败",
				UpstreamRatioSyncDisabled: test.ratioDisabled, UpstreamBalanceSyncDisabled: test.balanceDisabled,
			}).Error)

			summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
			require.NoError(t, err)
			assert.Zero(t, summary.Failed)
			if test.ratioDisabled && test.balanceDisabled {
				assert.Equal(t, 1, summary.Skipped)
				assert.Zero(t, summary.Updated)
			} else {
				assert.Zero(t, summary.Skipped)
				assert.Equal(t, 1, summary.Updated)
			}
			monitor, err := model.GetChannelRatioMonitor(1)
			require.NoError(t, err)
			assert.Equal(t, test.ratioDisabled, monitor.UpstreamRatioSyncDisabled)
			assert.Equal(t, test.balanceDisabled, monitor.UpstreamBalanceSyncDisabled)
			if test.ratioDisabled {
				assert.Zero(t, ratioRequests.Load())
				assert.Equal(t, 100, monitor.ConsecutiveFailures)
			} else {
				assert.EqualValues(t, 1, ratioRequests.Load())
				assert.Zero(t, monitor.ConsecutiveFailures)
				assert.Equal(t, 1.25, monitor.Ratio)
			}
			if test.balanceDisabled {
				assert.Zero(t, balanceRequests.Load())
				assert.Equal(t, 100, monitor.BalanceConsecutiveFailures)
			} else {
				assert.EqualValues(t, 1, balanceRequests.Load())
				assert.Zero(t, monitor.BalanceConsecutiveFailures)
				require.NotNil(t, monitor.UpstreamBalance)
				assert.Equal(t, 5.0, *monitor.UpstreamBalance)
			}
		})
	}
}

func TestRunChannelRatioMonitorTaskStopsRatioAtConfiguredConsecutiveFailureLimit(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption:              "10",
		channelMonitorAutoUpdateConsecutiveFailureLimitOption: "3",
	})
	disableChannelMonitorSSRFProtection(t)

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "ratio failure", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic,
		UpstreamBalanceSyncDisabled: true,
	}).Error)

	firstSummary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, firstSummary.Failed)
	assert.Equal(t, 2, firstSummary.Retried)
	monitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.Equal(t, 3, monitor.ConsecutiveFailures)
	requestsAfterFailureLimit := requestCount.Load()
	assert.Positive(t, requestsAfterFailureLimit)

	secondSummary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Zero(t, secondSummary.Skipped)
	assert.Equal(t, 1, secondSummary.Failed)
	require.Len(t, secondSummary.Failures, 1)
	assert.Contains(t, secondSummary.Failures[0].Error, "502 Bad Gateway")
	assert.Equal(t, requestsAfterFailureLimit, requestCount.Load())
}

func TestRunChannelRatioMonitorTaskStopsBalanceAtConfiguredConsecutiveFailureLimit(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption:              "10",
		channelMonitorAutoUpdateConsecutiveFailureLimitOption: "3",
	})
	disableChannelMonitorSSRFProtection(t)

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "balance failure", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamAuthType: service.NewAPIUpstreamAuthUser, UpstreamUserId: 42, UpstreamAccessToken: "test-token",
		UpstreamRatioSyncDisabled: true,
	}).Error)

	firstSummary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, firstSummary.Failed)
	assert.Equal(t, 2, firstSummary.Retried)
	assert.EqualValues(t, 3, requestCount.Load())
	monitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.Equal(t, 3, monitor.BalanceConsecutiveFailures)

	secondSummary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Zero(t, secondSummary.Skipped)
	assert.Equal(t, 1, secondSummary.Failed)
	assert.EqualValues(t, 3, requestCount.Load())
}

func TestRunChannelRatioMonitorTaskAutoDisablesChannelWhenStoppedRatioSyncFails(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption:              "0",
		channelMonitorAutoUpdateConsecutiveFailureLimitOption: "3",
		channelMonitorAutoDisableOnUpdateFailureOption:        "true",
	})
	disableChannelMonitorSSRFProtection(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/self":
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":500}}`))
		case "/api/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":100}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "stopped ratio sync", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
		UpstreamUserId: 42, UpstreamAccessToken: "test-token",
		ConsecutiveFailures: 3,
	}).Error)

	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Updated)
	assert.Equal(t, 1, summary.Failed)
	assert.Equal(t, 1, summary.BalanceUpdated)
	assert.Equal(t, 1, summary.ChannelsDisabled)

	channel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
	assert.Equal(t, channelMonitorUpdateFailureDisableReason, channel.GetOtherInfo()["status_reason"])
}

func TestRunChannelRatioMonitorTaskKeepsHealthyUpstreamMetricRunning(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption:              "0",
		channelMonitorAutoUpdateConsecutiveFailureLimitOption: "3",
	})
	disableChannelMonitorSSRFProtection(t)

	var ratioRequests atomic.Int32
	var balanceRequests atomic.Int32
	var statusRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/self/groups":
			ratioRequests.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"vip":{"ratio":1.25}}}`))
		case "/api/user/self":
			balanceRequests.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":500}}`))
		case "/api/status":
			statusRequests.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":100}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1, Name: "ratio remains active", Key: "ratio-key", Group: "vip", Status: common.ChannelStatusEnabled},
		{Id: 2, Name: "balance remains active", Key: "balance-key", Group: "vip", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{
			ChannelId: 1, Ratio: 1, UpdatedTime: 1,
			UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
			UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
			UpstreamUserId: 41, UpstreamAccessToken: "ratio-token",
			BalanceConsecutiveFailures: 3,
		},
		{
			ChannelId:    2,
			UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
			UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
			UpstreamUserId: 42, UpstreamAccessToken: "balance-token",
			ConsecutiveFailures: 3,
		},
	}).Error)

	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, summary.Updated)
	assert.Equal(t, 1, summary.BalanceUpdated)
	assert.Equal(t, 2, summary.Failed)
	assert.EqualValues(t, 1, ratioRequests.Load())
	assert.EqualValues(t, 1, balanceRequests.Load())
	assert.EqualValues(t, 1, statusRequests.Load())

	ratioMonitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.Zero(t, ratioMonitor.ConsecutiveFailures)
	assert.Equal(t, 3, ratioMonitor.BalanceConsecutiveFailures)
	balanceMonitor, err := model.GetChannelRatioMonitor(2)
	require.NoError(t, err)
	assert.Equal(t, 3, balanceMonitor.ConsecutiveFailures)
	assert.Zero(t, balanceMonitor.BalanceConsecutiveFailures)
	require.NotNil(t, balanceMonitor.UpstreamBalance)
	assert.InDelta(t, 5, *balanceMonitor.UpstreamBalance, 1e-9)
}
