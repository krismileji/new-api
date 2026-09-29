package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskSubmissionPolicyMatchesActualRetry(t *testing.T) {
	mainDB, dialect := openTaskDialectDatabase(t, &model.User{}, &model.Channel{}, &model.ChannelRatioMonitor{})
	logDB, _ := openTaskDialectDatabase(t, &model.Log{})
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRetries, previousErrorLog := common.RetryTimes, constant.ErrorLogEnabled
	previousRedis, previousMemory := common.RedisEnabled, common.MemoryCacheEnabled
	model.DB, model.LOG_DB = mainDB, logDB
	common.SetDatabaseTypes(dialect, dialect)
	constant.ErrorLogEnabled = true
	common.RedisEnabled, common.MemoryCacheEnabled = false, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.RetryTimes, constant.ErrorLogEnabled = previousRetries, previousErrorLog
		common.RedisEnabled, common.MemoryCacheEnabled = previousRedis, previousMemory
	})
	user := model.User{Username: "task-policy", AffCode: "task-policy"}
	require.NoError(t, mainDB.Create(&user).Error)
	channel := model.Channel{Type: constant.ChannelTypeTaskPlugin, Name: "task-policy", Key: "test-key", Status: common.ChannelStatusEnabled, AutoBan: common.GetPointer(0)}
	require.NoError(t, mainDB.Create(&channel).Error)

	for _, tc := range []struct {
		name, reason                             string
		written, skip, specific, local, accepted bool
		retries, attempts                        int
		fast                                     bool
	}{
		{name: "wrapped skip-retry error", skip: true, retries: 2, attempts: 1, reason: "non_retryable_error"},
		{name: "response already started", written: true, retries: 2, attempts: 1, reason: "non_retryable_error"},
		{name: "specific channel", specific: true, retries: 2, attempts: 1, reason: "non_retryable_error"},
		{name: "local rejection", local: true, retries: 2, attempts: 1, reason: "local_rejection"},
		{name: "accepted task", accepted: true, retries: 2, attempts: 1, reason: "task_accepted"},
		{name: "exhausted budget", attempts: 1, reason: "attempt_budget_exhausted"},
		{name: "ordinary retry", retries: 1, attempts: 2, reason: "attempt_budget_exhausted"},
		{name: "fast retry without ordinary budget", fast: true, attempts: 2, reason: "attempt_budget_exhausted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.RetryTimes = tc.retries
			options := map[string]string{"ChannelMonitorSmartScheduleEnabled": "false"}
			if tc.fast {
				options["ChannelMonitorSmartScheduleEnabled"] = "true"
				policy := channelSmartScheduleTestGroupPolicy("default", channelMonitorSmartScheduleStrategySmart, false, channelMonitorSmartScheduleApplyPriorityWeight, []string{"plugin-model"}, 5, 80, 30)
				policy.FastFailureSeconds, policy.SlowFailureSeconds = common.GetPointer(30.0), common.GetPointer(60.0)
				policy.FastFailureSameChannelRetryCount, policy.FastFailureRetryDelayMs = common.GetPointer(1), common.GetPointer(0)
				options["ChannelMonitorSmartScheduleGroupPolicies"] = channelSmartScheduleTestGroupPoliciesJSON(t, policy)
			}
			useChannelMonitorOptionMap(t, options)
			events := []string{}
			billing := &taskSubmissionTestBilling{events: &events}
			c := taskSubmissionTestContext()
			c.Set("id", user.Id)
			c.Set(common.RequestIdKey, tc.name)
			if tc.specific {
				c.Set("specific_channel_id", channel.Id)
			}
			info := taskSubmissionRelayInfo(billing)
			info.LockedChannel, info.TokenGroup = &channel, "default"
			attempts := 0
			outcome, taskErr := executeTaskSubmissionWith(c, info, func(c *gin.Context, _ *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
				attempts++
				if tc.written {
					_, err := c.Writer.Write([]byte("data: partial\n\n"))
					require.NoError(t, err)
				}
				var cause error = errors.New("upstream unavailable")
				if tc.skip {
					cause = types.NewErrorWithStatusCode(cause, types.ErrorCodeDoRequestFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
				}
				return nil, &dto.TaskError{StatusCode: http.StatusServiceUnavailable, Error: cause, Message: "upstream unavailable", LocalError: tc.local, NoRetry: tc.accepted}
			})
			require.Nil(t, outcome)
			require.NotNil(t, taskErr)
			assert.Equal(t, tc.attempts, attempts)
			assert.Equal(t, 1, billing.refunds)
			policyEvents := service.RequestPolicy(c).Events()
			require.Len(t, policyEvents, tc.attempts*3)
			for attempt := 0; attempt < tc.attempts; attempt++ {
				decision := policyEvents[attempt*3+2].Decision
				if attempt+1 < tc.attempts {
					assert.Equal(t, "retry", decision.Action)
					assert.Equal(t, "retry_status_matched", decision.Reason)
				} else {
					assert.Equal(t, "stop", decision.Action)
					assert.Equal(t, tc.reason, decision.Reason)
				}
			}
			var logs []model.Log
			require.NoError(t, logDB.Where("request_id = ?", tc.name).Order("id").Find(&logs).Error)
			if tc.local {
				assert.Empty(t, logs)
				return
			}
			require.Len(t, logs, tc.attempts)
			for attempt, log := range logs {
				var other struct {
					AdminInfo struct {
						RequestPolicy []service.PolicyEvent `json:"request_policy"`
					} `json:"admin_info"`
				}
				require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
				assert.Equal(t, policyEvents[:(attempt+1)*3], other.AdminInfo.RequestPolicy)
				assert.Equal(t, attempt+1 < tc.attempts, log.IsRetryAttempt)
			}
		})
	}
}
