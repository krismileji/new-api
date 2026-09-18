package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSmartScheduleWhitelistedFailuresPreserveSamplesAndProtection(t *testing.T) {
	for _, test := range []struct {
		name       string
		source     string
		whitelist  string
		statusCode int
	}{
		{name: "manual upstream error code", source: "manual", whitelist: " BAD_RESPONSE ", statusCode: 502},
		{name: "status probe HTTP status", source: "status", whitelist: "503", statusCode: 503},
		{name: "scheduled failure", source: "scheduled", whitelist: "503", statusCode: 503},
		{name: "scheduled rate limit", source: "scheduled", whitelist: "429", statusCode: 429},
		{name: "runtime failure", source: "runtime", whitelist: "503", statusCode: 503},
		{name: "runtime rate limit", source: "runtime", whitelist: "429", statusCode: 429},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			withSelfUseModeEnabled(t)
			service.InitHttpClient()
			service.ClearChannelRateLimitCooldowns()
			t.Cleanup(service.ClearChannelRateLimitCooldowns)
			originalErrorLogEnabled := constant.ErrorLogEnabled
			constant.ErrorLogEnabled = true
			t.Cleanup(func() { constant.ErrorLogEnabled = originalErrorLogEnabled })
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
			}))
			t.Cleanup(upstream.Close)
			require.NoError(t, db.Create(&model.User{
				Username: "whitelist-probe-root", Password: "password", Role: common.RoleRootUser,
				Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000,
			}).Error)
			priority := int64(80)
			weight := uint(50)
			channel := model.Channel{
				Id: 2701, Type: constant.ChannelTypeOpenAI, Key: "sk-probe", Name: "whitelisted probe",
				Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer(upstream.URL),
				Models: "gpt-4o-mini", Group: "vip", Priority: &priority, Weight: &weight,
			}
			require.NoError(t, db.Create(&channel).Error)
			require.NoError(t, db.Create(&model.Ability{
				ChannelId: channel.Id, Group: "vip", Model: "gpt-4o-mini", Enabled: true,
				Priority: common.GetPointer(int64(0)), Weight: 0,
			}).Error)
			now := common.GetTimestamp()
			protectedUntil := now + 60
			require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
				ChannelId: channel.Id, GroupName: "vip", ModelName: "gpt-4o-mini", ParticipationSet: true,
				StabilityState: model.ChannelSmartScheduleStabilityDegraded,
				StabilityUntil: protectedUntil, RuntimeProtectionUntil: protectedUntil,
				StabilitySavedPriority: priority, StabilitySavedWeight: weight,
			}).Error)
			require.NoError(t, db.Create(&model.ChannelSmartScheduleModelSampleState{
				ChannelId: channel.Id, ModelName: "gpt-4o-mini", SamplesJSON: "[]",
				RecoverySuccessCount: 1, RecoverySuccessAt: now - 30,
			}).Error)
			policy := channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyFirstToken, true,
				channelMonitorSmartScheduleApplyWeight, nil, 1, 80, 30,
			)
			policy.DegradedProbeEnabled = common.GetPointer(true)
			policy.ConsecutiveFailureThreshold = common.GetPointer(1)
			useChannelMonitorOptionMap(t, map[string]string{
				channelMonitorSmartScheduleEnabledOption:           "true",
				channelMonitorSmartScheduleGroupPoliciesOption:     channelSmartScheduleTestGroupPoliciesJSON(t, policy),
				channelMonitorSmartScheduleRateLimitCooldownOption: "30",
				channelMonitorErrorMessageWhitelistOption:          test.whitelist,
			})
			probeResult := testResult{
				requestDispatched: true, originalModelName: "gpt-4o-mini",
				newAPIError: types.NewErrorWithStatusCode(errors.New("upstream unavailable"), types.ErrorCodeBadResponse, test.statusCode),
			}
			switch test.source {
			case "manual":
				recorded, message := recordManualChannelSmartScheduleProbeResult(&channel, probeResult, 250)
				assert.False(t, recorded)
				assert.Contains(t, message, "错误码白名单")
			case "status":
				recorded, message := recordChannelStatusProbeSmartScheduleResult(&channel, probeResult, 250, "whitelisted-status", now)
				assert.False(t, recorded)
				assert.Contains(t, message, "错误码白名单")
				assert.Equal(t, model.ChannelStatusProbeSampleSkipped, channelStatusProbeSampleDecision(recorded, message))
			case "scheduled":
				result, err := runChannelSmartScheduleProbeOnce(context.Background(), nil)
				require.NoError(t, err)
				assert.Equal(t, 1, result.Probed)
				assert.Equal(t, 1, result.Failed)
				var errorLog model.Log
				require.NoError(t, db.Where("type = ?", model.LogTypeError).First(&errorLog).Error)
				assert.Equal(t, channel.Id, errorLog.ChannelId)
			case "runtime":
				protectChannelSmartScheduleRuntimeFailure(channel.Id, "gpt-4o-mini", probeResult.newAPIError)
			}

			assert.Zero(t, service.ChannelRateLimitCooldownUntil(channel.Id, "gpt-4o-mini"))
			var sample model.ChannelSmartScheduleModelSampleState
			require.NoError(t, db.Where("channel_id = ? AND model_name = ?", channel.Id, "gpt-4o-mini").First(&sample).Error)
			assert.Zero(t, sample.SampleCount)
			assert.Equal(t, 1, sample.RecoverySuccessCount)
			assert.Equal(t, now-30, sample.RecoverySuccessAt)
			var state model.ChannelSmartScheduleRouteState
			require.NoError(t, db.Where("channel_id = ? AND group_name = ? AND model_name = ?", channel.Id, "vip", "gpt-4o-mini").First(&state).Error)
			assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, state.StabilityState)
			assert.Equal(t, protectedUntil, state.StabilityUntil)
			assert.Equal(t, protectedUntil, state.RuntimeProtectionUntil)
			assert.Empty(t, state.LastScheduleError)
		})
	}
}
