package controller

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisabledStabilitySettingsDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupImmediateEjectionDatabase(t, engine)
			policy := channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 100, 80, 30,
			)
			enabledPolicy := policy
			enabledPolicy.Group = "enabled"
			enabledPolicy.StabilityEnabled = common.GetPointer(true)
			policies := channelSmartScheduleTestGroupPoliciesJSON(t, policy, enabledPolicy)
			usePersistedChannelMonitorOptions(t, db, map[string]string{
				channelMonitorSmartScheduleEnabledOption:         "true",
				channelMonitorSmartScheduleGroupPoliciesOption:   policies,
				channelMonitorSmartScheduleControlRevisionOption: "before-disable",
			})
			now := common.GetTimestamp()
			cases := []struct {
				group, modelName, state string
				fixed, clear            bool
				disabled, excluded      bool
			}{
				{group: "vip", modelName: "model-a", state: model.ChannelSmartScheduleStabilityDegraded, clear: true},
				{group: "vip", modelName: "model-a", state: model.ChannelSmartScheduleStabilityProbing, fixed: true, clear: true},
				{group: "vip", modelName: "model-b", state: model.ChannelSmartScheduleStabilityDegraded},
				{group: "enabled", modelName: "model-a", state: model.ChannelSmartScheduleStabilityDegraded},
				{group: "vip", modelName: "model-a", state: model.ChannelSmartScheduleStabilityDegraded, disabled: true, clear: true},
				{group: "vip", modelName: "model-a", state: model.ChannelSmartScheduleStabilityDegraded, excluded: true, clear: true},
			}
			states := make([]model.ChannelSmartScheduleRouteState, len(cases))
			logicalStates := make([]model.ChannelLogicalSmartScheduleRouteState, len(cases))
			for i, tc := range cases {
				id := 9500 + i
				status := common.ChannelStatusEnabled
				if tc.disabled {
					status = common.ChannelStatusManuallyDisabled
				}
				require.NoError(t, db.Create(&model.Channel{
					Id: id, Name: "stability setting regression", Group: tc.group, Models: tc.modelName,
					Status: status, Priority: common.GetPointer(int64(80)), Weight: common.GetPointer(uint(40)),
				}).Error)
				require.NoError(t, db.Create(&model.Ability{
					ChannelId: id, Group: tc.group, Model: tc.modelName, Enabled: !tc.disabled, Priority: common.GetPointer(int64(0)),
				}).Error)
				states[i] = model.ChannelSmartScheduleRouteState{
					ChannelId: id, GroupName: tc.group, ModelName: tc.modelName, ParticipationSet: true, Revision: 1,
					Excluded:       tc.excluded,
					StabilityState: tc.state, StabilityUntil: now + 1800, RuntimeProtectionUntil: now + 1800,
					StabilitySavedPriority: 80, StabilitySavedWeight: 40, BasePriority: 80, BaseWeight: 40,
					AdaptiveHealthState: "high_risk", AdaptiveHealthPressure: 1,
				}
				if tc.fixed {
					states[i].ManualPrimaryUntil = now + 3600
					states[i].ManualPrimaryAllowStabilityDegrade = true
					states[i].ManualPrimarySaved = true
					states[i].ManualPrimarySavedPriority = 80
					states[i].ManualPrimarySavedWeight = 40
					states[i].StabilityReleaseMaxPromptTokens = 1024
				}
				require.NoError(t, db.Create(&states[i]).Error)
				raw, err := common.Marshal(map[string]any{
					"state": states[i], "effective_routing_set": true, "effective_priority": 0, "effective_weight": 0,
				})
				require.NoError(t, err)
				logicalStates[i] = model.ChannelLogicalSmartScheduleRouteState{
					LogicalGroupID: int64(10000 + i), LogicalRevision: 1,
					GroupName: tc.group, ModelName: tc.modelName, StateRevision: 1,
					StateJSON: model.ChannelSmartScheduleSamplesJSON(raw),
				}
				require.NoError(t, db.Create(&logicalStates[i]).Error)
			}
			values := map[string]string{
				channelMonitorSmartScheduleGroupPoliciesOption:   policies,
				channelMonitorSmartScheduleControlRevisionOption: "after-disable",
			}
			changed, err := model.UpdateChannelMonitorSettingsOptions(values, true, common.GetPointer("stale-revision"))
			require.ErrorIs(t, err, model.ErrChannelMonitorSettingsChanged)
			assert.False(t, changed)
			var unchanged model.ChannelSmartScheduleRouteState
			require.NoError(t, db.First(&unchanged, states[0].Id).Error)
			assert.Equal(t, states[0], unchanged)

			require.NoError(t, db.Model(&model.ChannelSmartScheduleRouteState{}).
				Where("id = ?", states[0].Id).Update("revision", int64(math.MaxInt64)).Error)
			changed, err = model.UpdateChannelMonitorSettingsOptions(values, true, common.GetPointer("before-disable"))
			require.Error(t, err)
			assert.False(t, changed)
			var revision model.Option
			require.NoError(t, db.Where(&model.Option{Key: channelMonitorSmartScheduleControlRevisionOption}).First(&revision).Error)
			assert.Equal(t, "before-disable", revision.Value, "cleanup and the switch must commit atomically")
			require.NoError(t, db.Model(&model.ChannelSmartScheduleRouteState{}).
				Where("id = ?", states[0].Id).Update("revision", states[0].Revision).Error)

			changed, err = model.UpdateChannelMonitorSettingsOptions(values, true, common.GetPointer("before-disable"))
			require.NoError(t, err)
			assert.True(t, changed)
			for i, tc := range cases {
				var state model.ChannelSmartScheduleRouteState
				var ability model.Ability
				require.NoError(t, db.First(&state, states[i].Id).Error)
				require.NoError(t, db.Where(&model.Ability{ChannelId: state.ChannelId, Group: tc.group, Model: tc.modelName}).First(&ability).Error)
				var logical model.ChannelLogicalSmartScheduleRouteState
				require.NoError(t, db.First(&logical, logicalStates[i].Id).Error)
				if !tc.clear {
					assert.Equal(t, states[i], state)
					assert.Equal(t, logicalStates[i], logical)
					assert.Zero(t, *ability.Priority)
					assert.Zero(t, ability.Weight)
					continue
				}
				assert.Empty(t, state.StabilityState)
				assert.Zero(t, state.RuntimeProtectionUntil)
				assert.Zero(t, state.StabilityUntil)
				assert.Zero(t, state.StabilityReleaseMaxPromptTokens)
				assert.Equal(t, "high_risk", state.AdaptiveHealthState)
				assert.Equal(t, states[i].ManualPrimaryUntil, state.ManualPrimaryUntil)
				assert.Equal(t, !tc.disabled, ability.Enabled)
				assert.Equal(t, tc.excluded, state.Excluded)
				var channel model.Channel
				require.NoError(t, db.First(&channel, state.ChannelId).Error)
				if tc.disabled {
					assert.Equal(t, common.ChannelStatusManuallyDisabled, channel.Status)
				} else {
					assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
				}
				var payload struct {
					State             model.ChannelSmartScheduleRouteState
					EffectivePriority int64 `json:"effective_priority"`
					EffectiveWeight   uint  `json:"effective_weight"`
				}
				require.NoError(t, common.UnmarshalJsonStr(string(logical.StateJSON), &payload))
				assert.Empty(t, payload.State.StabilityState)
				assert.Zero(t, payload.State.RuntimeProtectionUntil)
				assert.Equal(t, int64(80), payload.EffectivePriority)
				assert.Equal(t, uint(40), payload.EffectiveWeight)
				assert.Equal(t, int64(2), logical.StateRevision)
				if tc.excluded {
					assert.Nil(t, ability.Priority)
					assert.Zero(t, ability.Weight)
				} else if tc.fixed {
					assert.Greater(t, *ability.Priority, int64(80))
					assert.Equal(t, uint(1000), ability.Weight)
				} else {
					assert.Equal(t, int64(80), *ability.Priority)
					assert.Equal(t, uint(40), ability.Weight)
				}
			}
			changed, err = model.UpdateChannelMonitorSettingsOptions(values, true, common.GetPointer("after-disable"))
			require.NoError(t, err)
			assert.False(t, changed, "saving an already disabled policy must be idempotent")
		})
	}
}
