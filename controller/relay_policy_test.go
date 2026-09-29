package controller

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPPolicyMatchesActualRetry(t *testing.T) {
	dialect, dsn := os.Getenv("TEST_TASK_DB_DIALECT"), ""
	switch dialect {
	case "", "sqlite":
		dialect = "sqlite"
	case "mysql":
		dsn = os.Getenv("TEST_MYSQL_DSN")
		require.NotEmpty(t, dsn)
	case "postgres":
		dsn = os.Getenv("TEST_POSTGRES_DSN")
		require.NotEmpty(t, dsn)
	default:
		t.Fatalf("unsupported test dialect %q", dialect)
	}
	// InitDB in this fixture initializes the dialect-specific routing columns.
	db := modelManagementDB(t, dialect, dsn)
	require.NoError(t, db.AutoMigrate(&model.ChannelRatioMonitor{}, &model.ChannelSmartScheduleRouteState{}, &model.Log{}))
	oldRetries, oldErrorLog := common.RetryTimes, constant.ErrorLogEnabled
	oldCount, oldSensitive := constant.CountToken, setting.CheckSensitiveEnabled
	oldFree := operation_setting.GetQuotaSetting().EnableFreeModelPreConsume
	oldRanges := operation_setting.AutomaticRetryStatusCodeRanges
	constant.ErrorLogEnabled, constant.CountToken, setting.CheckSensitiveEnabled = true, false, false
	operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = false
	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: 503, End: 503}}
	t.Cleanup(func() {
		common.RetryTimes, constant.ErrorLogEnabled = oldRetries, oldErrorLog
		constant.CountToken, setting.CheckSensitiveEnabled = oldCount, oldSensitive
		operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = oldFree
		operation_setting.AutomaticRetryStatusCodeRanges = oldRanges
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"policy-audit-model":0}`))
	user := model.User{Username: "policy-http", AffCode: "policy-http"}
	require.NoError(t, db.Create(&user).Error)
	for _, tc := range []struct {
		name                      string
		status, retries, attempts int
		capacity, specific, fast  bool
		decision                  service.PolicyDecision
	}{
		{"capacity returned as 200", 200, 1, 2, true, false, false, service.PolicyDecision{Action: "retry", Reason: "模型容量不足", Source: "system"}},
		{"capacity returned as 400", 400, 1, 2, true, false, false, service.PolicyDecision{Action: "retry", Reason: "模型容量不足", Source: "system"}},
		{"specific channel", 503, 1, 1, false, true, false, service.PolicyDecision{Action: "stop", Reason: "non_retryable_error", Source: "system"}},
		{"ordinary status retry", 503, 1, 2, false, false, false, service.PolicyDecision{Action: "retry", Reason: "retry_status_matched", Source: "global"}},
		{"nonretryable status", 400, 1, 1, false, false, false, service.PolicyDecision{Action: "stop", Reason: "status_not_retryable", Source: "global"}},
		{"capacity after budget exhausted", 200, 0, 1, true, false, false, service.PolicyDecision{Action: "stop", Reason: "attempt_budget_exhausted", Source: "global"}},
		{"fast retry without ordinary budget", 503, 0, 2, false, false, true, service.PolicyDecision{Action: "retry", Reason: "retry_status_matched", Source: "system"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.RetryTimes = tc.retries
			options := map[string]string{"ChannelMonitorSmartScheduleEnabled": "false"}
			if tc.fast {
				policy := channelSmartScheduleTestGroupPolicy("default", channelMonitorSmartScheduleStrategySmart, false, channelMonitorSmartScheduleApplyPriorityWeight, []string{"policy-audit-model"}, 5, 80, 30)
				policy.FastFailureSeconds, policy.SlowFailureSeconds = common.GetPointer(30.0), common.GetPointer(60.0)
				policy.FastFailureSameChannelRetryCount, policy.FastFailureRetryDelayMs = common.GetPointer(1), common.GetPointer(0)
				options["ChannelMonitorSmartScheduleEnabled"] = "true"
				options["ChannelMonitorSmartScheduleGroupPolicies"] = channelSmartScheduleTestGroupPoliciesJSON(t, policy)
			}
			useChannelMonitorOptionMap(t, options)
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				body := `{"error":{"message":"upstream unavailable","type":"server_error","code":"upstream_error"}}`
				if tc.capacity {
					body = `{"error":{"message":"Selected model is at capacity. Please try a different model.","type":"server_error","code":"server_is_overloaded"}}`
				}
				_, err := w.Write([]byte(body))
				assert.NoError(t, err)
			}))
			defer upstream.Close()
			channels := []model.Channel{
				{Name: "policy-first", Type: constant.ChannelTypeOpenAI, Key: "key", Status: common.ChannelStatusEnabled, Group: "default", Models: "policy-audit-model", BaseURL: &upstream.URL, AutoBan: common.GetPointer(0)},
				{Name: "policy-second", Type: constant.ChannelTypeOpenAI, Key: "key", Status: common.ChannelStatusEnabled, Group: "default", Models: "policy-audit-model", BaseURL: &upstream.URL, AutoBan: common.GetPointer(0)},
			}
			require.NoError(t, db.Create(&channels).Error)
			for _, channel := range channels {
				require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Model: "policy-audit-model", Group: "default", Enabled: true}).Error)
				require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{ChannelId: channel.Id, GroupName: "default", ModelName: "policy-audit-model", ParticipationSet: true}).Error)
			}
			t.Cleanup(func() {
				for _, channel := range channels {
					require.NoError(t, db.Where("channel_id = ?", channel.Id).Delete(&model.ChannelSmartScheduleRouteState{}).Error)
					require.NoError(t, db.Where("channel_id = ?", channel.Id).Delete(&model.Ability{}).Error)
					require.NoError(t, db.Delete(&channel).Error)
				}
			})
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"policy-audit-model","messages":[{"role":"user","content":"hello"}]}`))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, constant.ContextKeyUserId, user.Id)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			c.Set(common.RequestIdKey, tc.name)
			require.Nil(t, middleware.SetupContextForSelectedChannel(c, &channels[0], "policy-audit-model"))
			if tc.specific {
				c.Set("specific_channel_id", channels[0].Id)
			}
			Relay(c, types.RelayFormatOpenAI)
			assert.EqualValues(t, tc.attempts, calls.Load(), recorder.Body.String())
			events := service.RequestPolicy(c).Events()
			require.Len(t, events, tc.attempts*3)
			assert.Equal(t, tc.decision, events[2].Decision)
			assert.Equal(t, "stop", events[len(events)-1].Decision.Action)
			var logs []model.Log
			require.NoError(t, db.Where("request_id = ?", tc.name).Order("id").Find(&logs).Error)
			require.Len(t, logs, tc.attempts)
			for attempt, log := range logs {
				var other struct {
					AdminInfo struct {
						RequestPolicy []service.PolicyEvent `json:"request_policy"`
					} `json:"admin_info"`
				}
				require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
				assert.Equal(t, events[:(attempt+1)*3], other.AdminInfo.RequestPolicy)
				assert.Equal(t, attempt+1 < tc.attempts, log.IsRetryAttempt)
			}
		})
	}
}
