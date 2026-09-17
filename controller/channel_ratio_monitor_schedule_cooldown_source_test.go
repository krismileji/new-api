package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSmartScheduleCooldownResponseDistinguishesStabilityProtectionFrom429(t *testing.T) {
	for _, source := range []struct {
		name          string
		eventSequence int64
	}{
		{name: "direct"},
		{name: "redis_event", eventSequence: 1},
	} {
		for _, failure := range []struct {
			name       string
			statusCode int
		}{
			{name: "stability_protection", statusCode: http.StatusServiceUnavailable},
			{name: "upstream_rate_limit", statusCode: http.StatusTooManyRequests},
		} {
			t.Run(source.name+"/"+failure.name, func(t *testing.T) {
				db := setupChannelMonitorControllerTestDB(t)
				service.ClearChannelRateLimitCooldowns()
				t.Cleanup(service.ClearChannelRateLimitCooldowns)
				policy := channelSmartScheduleTestGroupPolicy(
					"vip", channelMonitorSmartScheduleStrategyRatio, true,
					channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 1, 80, 30,
				)
				failureThreshold := 1
				policy.ConsecutiveFailureThreshold = &failureThreshold
				useChannelMonitorOptionMap(t, map[string]string{
					channelMonitorSmartScheduleEnabledOption:           "true",
					channelMonitorSmartScheduleGroupPoliciesOption:     channelSmartScheduleTestGroupPoliciesJSON(t, policy),
					channelMonitorSmartScheduleRateLimitCooldownOption: "30",
				})

				priority, weight := int64(100), uint(20)
				require.NoError(t, db.Create(&model.Channel{
					Id: 1904, Name: "cooldown source", Status: common.ChannelStatusEnabled,
				}).Error)
				require.NoError(t, db.Create(&model.Ability{
					ChannelId: 1904, Group: "vip", Model: "model-a", Enabled: true,
					Priority: &priority, Weight: weight,
				}).Error)
				require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
					ChannelId: 1904, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
				}).Error)
				now := common.GetTimestamp()
				health := &channelSmartScheduleRuntimeHealthSnapshot{
					RequestEvents: []channelSmartScheduleRuntimeRequestEvent{{Timestamp: now, Failure: true}},
				}
				runtimeError := types.NewErrorWithStatusCode(
					errors.New("upstream failure"), types.ErrorCodeBadResponseStatusCode, failure.statusCode,
				)
				require.NoError(t, applyChannelSmartScheduleRuntimeFailureWithSource(
					1904, "model-a", runtimeError, false, false, false, health, false, now, source.eventSequence,
				))

				// Both failures must still defer traffic while their respective protection is active.
				assert.Greater(t, service.ChannelRateLimitCooldownUntilMatching(1904, "model-a"), now)
				var state model.ChannelSmartScheduleRouteState
				require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{
					ChannelId: 1904, GroupName: "vip", ModelName: "model-a",
				}).First(&state).Error)
				cooling := true
				runtimeViews := map[model.ChannelSmartScheduleRouteKey]model.ChannelSmartScheduleRouteRuntimeView{
					{ChannelId: 1904, Group: "vip", Model: "model-a"}: {RateLimitCoolingDown: &cooling},
					{ChannelId: 1904, Group: "vip", Model: "model-*"}: {RateLimitCoolingDown: &cooling},
				}
				responses := channelSmartScheduleRouteResponses([]model.ChannelSmartScheduleRoute{
					{ChannelId: 1904, Group: "vip", Model: "model-a", State: state},
					{ChannelId: 1904, Group: "vip", Model: "model-*", State: state},
				}, runtimeViews)
				require.Len(t, responses, 2)
				for _, response := range responses {
					require.NotNil(t, response.RateLimitCoolingDown)
					if failure.statusCode == http.StatusTooManyRequests {
						assert.Greater(t, response.RateLimitCooldownUntil, now)
						assert.True(t, *response.RateLimitCoolingDown)
						assert.Empty(t, response.State.StabilityState)
					} else {
						assert.Zero(t, response.RateLimitCooldownUntil, "a stability bridge is not an upstream 429")
						assert.False(t, *response.RateLimitCoolingDown)
						assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, response.State.StabilityState)
					}
				}
			})
		}
	}
}
