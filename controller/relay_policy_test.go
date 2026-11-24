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
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

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
	require.NoError(t, db.AutoMigrate(&model.ChannelRatioMonitor{}, &model.ChannelSmartScheduleRouteState{}, &model.Log{}, &model.Token{}))
	oldRetries, oldErrorLog := common.RetryTimes, constant.ErrorLogEnabled
	oldConsumeLog := common.LogConsumeEnabled
	oldCount := constant.CountToken
	oldTimeout := constant.StreamingTimeout
	oldFree := operation_setting.GetQuotaSetting().EnableFreeModelPreConsume
	oldRanges := operation_setting.AutomaticRetryStatusCodeRanges
	constant.ErrorLogEnabled, constant.CountToken = true, false
	constant.StreamingTimeout = 30
	common.LogConsumeEnabled = true
	operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = false
	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: 502, End: 503}}
	t.Cleanup(func() {
		common.RetryTimes, constant.ErrorLogEnabled = oldRetries, oldErrorLog
		common.LogConsumeEnabled = oldConsumeLog
		constant.CountToken = oldCount
		constant.StreamingTimeout = oldTimeout
		operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = oldFree
		operation_setting.AutomaticRetryStatusCodeRanges = oldRanges
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"policy-audit-model":0}`))
	const initialQuota = 1_000_000
	user := model.User{Username: "policy-http", AffCode: "policy-http", Quota: initialQuota}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "relay-policy-test-token", RemainQuota: initialQuota}
	require.NoError(t, db.Create(&token).Error)
	const emptyResponsesStream = `data: {"type":"response.created","response":{"id":"empty-attempt","status":"in_progress","output":[]}}

data: {"type":"response.output_item.added","item":{"type":"message","role":"assistant","content":[]}}

data: {"type":"response.completed","response":{"id":"empty-attempt","status":"completed","output":[],"usage":{"input_tokens":75613,"output_tokens":0,"total_tokens":75613}}}

`
	for _, tc := range []struct {
		name                      string
		status, retries, attempts int
		capacity, specific, fast  bool
		decision                  service.PolicyDecision
		firstBody, recoveredBody  string
	}{
		{"capacity returned as 200", 200, 1, 2, true, false, false, service.PolicyDecision{Action: "retry", Reason: "模型容量不足", Source: "system"}, "", ""},
		{"capacity returned as 400", 400, 1, 2, true, false, false, service.PolicyDecision{Action: "retry", Reason: "模型容量不足", Source: "system"}, "", ""},
		{"specific channel", 503, 1, 1, false, true, false, service.PolicyDecision{Action: "stop", Reason: "non_retryable_error", Source: "system"}, "", ""},
		{"ordinary status retry", 503, 1, 2, false, false, false, service.PolicyDecision{Action: "retry", Reason: "retry_status_matched", Source: "global"}, "", ""},
		{"nonretryable status", 400, 1, 1, false, false, false, service.PolicyDecision{Action: "stop", Reason: "status_not_retryable", Source: "global"}, "", ""},
		{"capacity after budget exhausted", 200, 0, 1, true, false, false, service.PolicyDecision{Action: "stop", Reason: "attempt_budget_exhausted", Source: "global"}, "", ""},
		{"fast retry without ordinary budget", 503, 0, 2, false, false, true, service.PolicyDecision{Action: "retry", Reason: "retry_status_matched", Source: "system"}, "", ""},
		{"empty chat response", 200, 1, 2, false, false, false, service.PolicyDecision{Action: "retry", Reason: "retry_status_matched", Source: "global"},
			`{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":0,"total_tokens":3}}`,
			`{"choices":[{"message":{"role":"assistant","content":"recovered"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":0,"total_tokens":3}}`},
		{"empty chat stream", 200, 1, 2, false, false, false, service.PolicyDecision{Action: "retry", Reason: "retry_status_matched", Source: "global"},
			`data: {"id":"empty-attempt","choices":[{"delta":{"role":"assistant","content":""}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: {"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":0,"total_tokens":3}}

data: [DONE]

`,
			`data: {"choices":[{"delta":{"role":"assistant"}}]}

data: {"choices":[{"delta":{"tool_calls":[{"id":"recovered","type":"function","function":{"name":"lookup","arguments":"{}"}}]}}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`},
		{"empty chat stream with retries disabled", 200, 0, 1, false, false, false, service.PolicyDecision{},
			"data: [DONE]\n\n", ""},
		{"empty responses stream", 200, 1, 2, false, false, false, service.PolicyDecision{Action: "retry", Reason: "retry_status_matched", Source: "global"},
			emptyResponsesStream,
			`data: {"type":"response.created","response":{"id":"recovered","status":"in_progress","output":[]}}

data: {"type":"response.output_text.delta","delta":"hello"}

data: {"type":"response.completed","response":{"id":"recovered","status":"completed","output":[],"usage":{"input_tokens":75613,"output_tokens":0,"total_tokens":75613}}}

`},
		{"all responses attempts are empty", 200, 1, 2, false, false, false, service.PolicyDecision{Action: "retry", Reason: "retry_status_matched", Source: "global"},
			emptyResponsesStream, ""},
		{"empty responses with no available retry channel", 200, 1, 1, false, false, false, service.PolicyDecision{Action: "retry", Reason: "retry_status_matched", Source: "global"},
			emptyResponsesStream, ""},
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
			stream := strings.HasPrefix(tc.firstBody, "data:")
			responses := strings.HasPrefix(tc.firstBody, `data: {"type":"response.`)
			noCandidate := responses && tc.attempts == 1
			paid := responses && tc.recoveredBody == ""
			if paid {
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"policy-audit-model":1}`))
				t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"policy-audit-model":0}`)) })
			}
			balanceBefore, err := model.GetUserQuota(user.Id, true)
			require.NoError(t, err)
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempt := calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
				}
				w.WriteHeader(tc.status)
				body := `{"error":{"message":"upstream unavailable","type":"server_error","code":"upstream_error"}}`
				if tc.capacity {
					body = `{"error":{"message":"Selected model is at capacity. Please try a different model.","type":"server_error","code":"server_is_overloaded"}}`
				}
				if tc.firstBody != "" {
					body = tc.firstBody
				}
				if noCandidate && attempt == 1 {
					assert.NoError(t, db.Model(&model.Ability{}).Where("model = ?", "policy-audit-model").Update("enabled", false).Error)
				}
				if responses && attempt > 1 && tc.recoveredBody == "" {
					body = strings.ReplaceAll(body, "empty-attempt", "last-empty-attempt")
				}
				if attempt > 1 && tc.recoveredBody != "" {
					body = tc.recoveredBody
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
			requestBody := `{"model":"policy-audit-model","messages":[{"role":"user","content":"hello"}]}`
			path, format := "/v1/chat/completions", types.RelayFormatOpenAI
			if responses {
				path, format = "/v1/responses", types.RelayFormatOpenAIResponses
				requestBody = `{"model":"policy-audit-model","stream":true,"input":"hello"}`
			} else if stream {
				requestBody = `{"model":"policy-audit-model","stream":true,"messages":[{"role":"user","content":"hello"}]}`
			}
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(requestBody))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, constant.ContextKeyUserId, user.Id)
			common.SetContextKey(c, constant.ContextKeyTokenId, token.Id)
			common.SetContextKey(c, constant.ContextKeyTokenKey, token.Key)
			common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			c.Set(common.RequestIdKey, tc.name)
			require.Nil(t, middleware.SetupContextForSelectedChannel(c, &channels[0], "policy-audit-model"))
			if tc.specific {
				c.Set("specific_channel_id", channels[0].Id)
			}
			Relay(c, format)
			assert.EqualValues(t, tc.attempts, calls.Load(), recorder.Body.String())
			events := service.RequestPolicy(c).Events()
			if tc.decision.Action != "" {
				require.GreaterOrEqual(t, len(events), 3)
				assert.Equal(t, tc.decision, events[2].Decision)
			}
			var logs []model.Log
			require.NoError(t, db.Where("request_id = ?", tc.name).Order("id").Find(&logs).Error)
			if tc.firstBody != "" {
				if tc.recoveredBody == "" {
					assert.Equal(t, http.StatusOK, recorder.Code)
					assert.Equal(t, "success", events[len(events)-1].Decision.Action)
					if !responses {
						require.Len(t, logs, 1)
						assert.Equal(t, model.LogTypeConsume, logs[0].Type)
						assert.Contains(t, recorder.Body.String(), "[DONE]")
						return
					}
					require.Len(t, logs, 2)
					assert.Equal(t, model.LogTypeError, logs[0].Type)
					assert.True(t, logs[0].IsRetryAttempt)
					assert.Equal(t, model.LogTypeConsume, logs[1].Type)
					assert.Equal(t, 75613, logs[1].PromptTokens)
					assert.Zero(t, logs[1].CompletionTokens)
					assert.Equal(t, 75613, logs[1].Quota)
					balanceAfter, err := model.GetUserQuota(user.Id, true)
					require.NoError(t, err)
					assert.Equal(t, 75613, balanceBefore-balanceAfter)
					assert.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.completed"))
					if noCandidate {
						assert.Contains(t, recorder.Body.String(), `"id":"empty-attempt"`)
						assert.Equal(t, logs[0].ChannelId, logs[1].ChannelId)
					} else {
						assert.Contains(t, recorder.Body.String(), `"id":"last-empty-attempt"`)
						assert.NotContains(t, recorder.Body.String(), `"id":"empty-attempt"`)
					}
					return
				}
				require.Len(t, logs, tc.attempts)
				assert.Equal(t, "empty_response", events[1].ErrorCode)
				assert.Equal(t, model.LogTypeError, logs[0].Type)
				assert.Contains(t, logs[0].Other, `"error_code":"empty_response"`)
				if tc.recoveredBody != "" {
					assert.Equal(t, http.StatusOK, recorder.Code)
					assert.Contains(t, recorder.Body.String(), "recovered")
					assert.NotContains(t, recorder.Body.String(), "empty-attempt")
					if responses {
						assert.Contains(t, recorder.Body.String(), `"delta":"hello"`)
						assert.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.completed"))
					} else if stream {
						assert.Equal(t, 1, strings.Count(recorder.Body.String(), "[DONE]"))
					}
					assert.True(t, logs[0].IsRetryAttempt)
					assert.Equal(t, model.LogTypeConsume, logs[1].Type)
					assert.NotEqual(t, logs[0].ChannelId, logs[1].ChannelId)
					assert.Equal(t, "success", events[len(events)-1].Decision.Action)
					return
				}
			}
			require.Len(t, logs, tc.attempts)
			require.Len(t, events, tc.attempts*3)
			assert.Equal(t, "stop", events[len(events)-1].Decision.Action)
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
