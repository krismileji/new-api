package controller

import (
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRemovedSmartSchedulePolicyDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupImmediateEjectionDatabase(t, engine)
			require.NoError(t, db.AutoMigrate(&model.ChannelLogicalSmartScheduleSampleState{}))
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&model.ChannelLogicalSmartScheduleSampleState{})) })
			removedPolicy := channelSmartScheduleTestGroupPolicy(
				"removed", channelMonitorSmartScheduleStrategyRatio, true,
				channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 100, 80, 30,
			)
			keptPolicy := removedPolicy
			keptPolicy.Group = "kept"
			originalPolicies := channelSmartScheduleTestGroupPoliciesJSON(t, removedPolicy, keptPolicy)
			keptPolicies := channelSmartScheduleTestGroupPoliciesJSON(t, keptPolicy)
			usePersistedChannelMonitorOptions(t, db, map[string]string{
				channelMonitorSmartScheduleEnabledOption:         "true",
				channelMonitorSmartScheduleGroupPoliciesOption:   originalPolicies,
				channelMonitorSmartScheduleControlRevisionOption: "before-remove",
			})
			now := common.GetTimestamp()
			channels := []model.Channel{
				{Id: 9701, Name: "default-primary", Group: "removed,kept", Models: "model-a,model-b", Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(80)), Weight: common.GetPointer(uint(40))},
				{Id: 9702, Name: "old-fixed-primary", Group: "removed", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(20)), Weight: common.GetPointer(uint(10))},
				{Id: 9703, Name: "disabled", Group: "removed", Models: "model-a", Status: common.ChannelStatusManuallyDisabled, Priority: common.GetPointer(int64(100)), Weight: common.GetPointer(uint(50))},
			}
			require.NoError(t, db.Create(&channels).Error)
			abilities := []model.Ability{
				{ChannelId: 9701, Group: "removed", Model: "model-a", Enabled: true, Priority: common.GetPointer(int64(0))},
				{ChannelId: 9702, Group: "removed", Model: "model-a", Enabled: true, Priority: common.GetPointer(int64(500)), Weight: 1000},
				{ChannelId: 9703, Group: "removed", Model: "model-a", Enabled: false, Priority: common.GetPointer(int64(0))},
				{ChannelId: 9701, Group: "removed", Model: "model-b", Enabled: true, Priority: common.GetPointer(int64(3)), Weight: 20},
				{ChannelId: 9701, Group: "kept", Model: "model-a", Enabled: true, Priority: common.GetPointer(int64(7)), Weight: 60},
			}
			require.NoError(t, db.Create(&abilities).Error)
			states := make([]model.ChannelSmartScheduleRouteState, len(abilities))
			for i, ability := range abilities {
				states[i] = model.ChannelSmartScheduleRouteState{
					ChannelId: ability.ChannelId, GroupName: ability.Group, ModelName: ability.Model,
					ParticipationSet: true, Revision: 9, Excluded: i == 2,
					LastScheduleStatus: model.ChannelSmartScheduleStatusSucceeded, LastScheduleError: "旧状态",
					LastScheduleScore: common.GetPointer(0.5), LastSchedulePriority: *ability.Priority, LastScheduleWeight: ability.Weight, LastScheduleTime: now,
					BaseRank: 1, BasePriority: 7, BaseWeight: 60,
					StabilityState: model.ChannelSmartScheduleStabilityDegraded, StabilityUntil: now + 600,
					RuntimeProtectionUntil: now + 600, StabilitySavedPriority: 9, StabilitySavedWeight: 80,
					AdaptiveHealthState: "high_risk", AdaptiveHealthPressure: 1,
					RollingStabilityScore: common.GetPointer(0.1), SamplingCandidate: true, SamplingDebt: 20,
					ManualPrimaryUntil: now + 600, ManualPrimarySaved: true, ManualPrimarySavedPriority: 2, ManualPrimarySavedWeight: 80,
				}
				if i == 0 {
					states[i].StabilityState = model.ChannelSmartScheduleStabilityProbing
					states[i].StabilityReleaseMaxPromptTokens = 1000
					states[i].TemporaryTrafficKind = model.ChannelSmartScheduleTemporaryTrafficAdaptive
					states[i].TemporaryTrafficTargetPercent = 5
				}
				require.NoError(t, db.Create(&states[i]).Error)
				require.NoError(t, db.Create(&model.ChannelSmartScheduleGroupPause{
					ChannelId: ability.ChannelId, GroupName: ability.Group, ModelName: ability.Model, PausedUntil: now + 600,
				}).Error)
			}
			for _, group := range []string{"removed", "kept"} {
				raw, err := common.Marshal(map[string]any{
					"state": states[0], "effective_routing_set": true, "effective_priority": 0, "effective_weight": 0,
				})
				require.NoError(t, err)
				require.NoError(t, db.Create(&model.ChannelLogicalSmartScheduleRouteState{
					LogicalGroupID: 10001, LogicalRevision: 1, GroupName: group, ModelName: "model-a", StateRevision: 9,
					StateJSON: model.ChannelSmartScheduleSamplesJSON(raw),
				}).Error)
				require.NoError(t, db.Create(&model.ChannelLogicalSmartScheduleSampleState{
					LogicalGroupID: 10001, LogicalRevision: 1, GroupName: group, ModelName: "model-a", ObservationSince: now,
					RecoverySuccessCount: 1,
				}).Error)
			}
			// These observations are shared with the retained group and ordinary monitoring.
			sharedSamples := model.ChannelSmartScheduleModelSampleState{ChannelId: 9701, ModelName: "model-a", ObservationSince: now, SampleCount: 10}
			require.NoError(t, db.Create(&sharedSamples).Error)
			values := map[string]string{
				channelMonitorSmartScheduleGroupPoliciesOption:   keptPolicies,
				channelMonitorSmartScheduleControlRevisionOption: "after-remove",
			}
			changed, err := model.UpdateChannelMonitorSettingsOptions(values, true, common.GetPointer("stale"))
			require.ErrorIs(t, err, model.ErrChannelMonitorSettingsChanged)
			assert.False(t, changed)

			cleanupFailure := errors.New("policy cleanup unavailable")
			callbackName := "test:removed_policy_cleanup_failure"
			require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
				if tx.Statement.Table == "channel_logical_smart_schedule_route_states" {
					tx.AddError(cleanupFailure)
				}
			}))
			changed, err = model.UpdateChannelMonitorSettingsOptions(values, true, common.GetPointer("before-remove"))
			require.NoError(t, db.Callback().Delete().Remove(callbackName))
			require.ErrorIs(t, err, cleanupFailure)
			assert.False(t, changed)
			var savedOption model.Option
			require.NoError(t, db.Where(&model.Option{Key: channelMonitorSmartScheduleGroupPoliciesOption}).First(&savedOption).Error)
			assert.Equal(t, originalPolicies, savedOption.Value)
			var unchangedStates []model.ChannelSmartScheduleRouteState
			require.NoError(t, db.Order("id ASC").Find(&unchangedStates).Error)
			assert.Equal(t, states, unchangedStates)
			var rolledBackAbility model.Ability
			require.NoError(t, db.Where(&model.Ability{ChannelId: 9702, Group: "removed", Model: "model-a"}).First(&rolledBackAbility).Error)
			assert.Equal(t, int64(500), *rolledBackAbility.Priority)
			assert.Equal(t, uint(1000), rolledBackAbility.Weight)

			changed, err = model.UpdateChannelMonitorSettingsOptions(values, true, common.GetPointer("before-remove"))
			require.NoError(t, err)
			assert.True(t, changed, "cleanup must request a routing-cache refresh even without remaining temporary traffic")
			for _, table := range []any{
				&model.ChannelSmartScheduleRouteState{}, &model.ChannelSmartScheduleGroupPause{},
				&model.ChannelLogicalSmartScheduleRouteState{}, &model.ChannelLogicalSmartScheduleSampleState{},
			} {
				var count int64
				require.NoError(t, db.Model(table).Where("group_name = ?", "removed").Count(&count).Error)
				assert.Zero(t, count)
				require.NoError(t, db.Model(table).Where("group_name = ?", "kept").Count(&count).Error)
				assert.EqualValues(t, 1, count)
			}
			for _, original := range abilities {
				var ability model.Ability
				require.NoError(t, db.Where(&model.Ability{ChannelId: original.ChannelId, Group: original.Group, Model: original.Model}).First(&ability).Error)
				assert.Equal(t, original.Enabled, ability.Enabled)
				if original.Group == "kept" {
					assert.Equal(t, original, ability)
					continue
				}
				assert.Nil(t, ability.Priority)
				assert.Zero(t, ability.Weight)
			}
			var keptState model.ChannelSmartScheduleRouteState
			require.NoError(t, db.First(&keptState, states[4].Id).Error)
			assert.Equal(t, states[4], keptState)
			var samples model.ChannelSmartScheduleModelSampleState
			require.NoError(t, db.First(&samples, sharedSamples.Id).Error)
			assert.Equal(t, sharedSamples, samples)
			var unchangedChannels []model.Channel
			require.NoError(t, db.Order("id ASC").Find(&unchangedChannels).Error)
			assert.Equal(t, channels, unchangedChannels)
			selected, err := model.GetRandomSatisfiedChannel("removed", "model-a", 0, nil)
			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, 9701, selected.Id, "ordinary channel priority wins after removing fixed/degraded overrides and pauses")

			changed, err = model.UpdateChannelMonitorSettingsOptions(values, true, common.GetPointer("after-remove"))
			require.NoError(t, err)
			assert.False(t, changed, "repeated saves are idempotent")
			staleResult, err := model.ProtectChannelSmartScheduleRouteOnShortTermFailure(
				9701, "removed", "model-a", now+600, "旧任务", "before-remove",
			)
			require.NoError(t, err)
			assert.False(t, staleResult.Handled)

			// Adding the policy again must not revive its former participation or protection.
			values[channelMonitorSmartScheduleGroupPoliciesOption] = originalPolicies
			values[channelMonitorSmartScheduleControlRevisionOption] = "re-added"
			_, err = model.UpdateChannelMonitorSettingsOptions(values, true, common.GetPointer("after-remove"))
			require.NoError(t, err)
			require.NoError(t, model.InitializeChannelSmartScheduleRouteStates())
			var recreated model.ChannelSmartScheduleRouteState
			require.NoError(t, db.Where(&model.ChannelSmartScheduleRouteState{ChannelId: 9701, GroupName: "removed", ModelName: "model-a"}).First(&recreated).Error)
			assert.Equal(t, model.ChannelSmartScheduleRouteState{
				Id: recreated.Id, ChannelId: 9701, GroupName: "removed", ModelName: "model-a",
				ParticipationSet: true, Excluded: true, Revision: 1,
			}, recreated)
			values[channelMonitorSmartScheduleGroupPoliciesOption] = "[]"
			values[channelMonitorSmartScheduleEnabledOption] = "false"
			values[channelMonitorSmartScheduleControlRevisionOption] = "removed-all"
			_, err = model.UpdateChannelMonitorSettingsOptions(values, true, common.GetPointer("re-added"))
			require.NoError(t, err)
			var count int64
			require.NoError(t, db.Model(&model.ChannelSmartScheduleRouteState{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestRemovedSmartSchedulePolicyRejectsStaleLogicalWorkDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupImmediateEjectionDatabase(t, engine)
			t.Setenv(model.ChannelLogicalGroupGlobalEnableEnv, "true")
			tables := []any{&model.ChannelLogicalGroup{}, &model.ChannelLogicalGroupMember{}, &model.ChannelLogicalSmartScheduleSampleState{}}
			require.NoError(t, db.AutoMigrate(tables...))
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
			const policies = `[{"group":"vip","models":["model-a"]}]`
			usePersistedChannelMonitorOptions(t, db, map[string]string{
				channelMonitorSmartScheduleGroupPoliciesOption:   policies,
				channelMonitorSmartScheduleControlRevisionOption: "before-remove",
			})
			logical := model.ChannelLogicalGroup{Name: "shared", Revision: 1}
			require.NoError(t, db.Create(&logical).Error)
			for _, channelID := range []int{9801, 9802} {
				require.NoError(t, db.Create(&model.Channel{
					Id: channelID, Status: common.ChannelStatusEnabled, LogicalChannelID: &logical.Id,
				}).Error)
				require.NoError(t, db.Create(&model.ChannelLogicalGroupMember{
					ChannelID: channelID, LogicalGroupID: logical.Id, Weight: 1, AddressFingerprint: strings.Repeat("a", 64),
				}).Error)
				require.NoError(t, db.Create(&model.Ability{ChannelId: channelID, Group: "vip", Model: "model-a", Enabled: true}).Error)
				require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
					ChannelId: channelID, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1,
					StabilityState: model.ChannelSmartScheduleStabilityDegraded, RuntimeProtectionUntil: common.GetTimestamp() + 600,
				}).Error)
			}
			staleRoutes, err := model.GetChannelSmartScheduleRoutes()
			require.NoError(t, err)
			_, err = model.CoalesceChannelSmartScheduleSchedulingRoutes(staleRoutes, "before-remove")
			require.NoError(t, err)
			values := map[string]string{
				channelMonitorSmartScheduleGroupPoliciesOption:   "[]",
				channelMonitorSmartScheduleControlRevisionOption: "after-remove",
			}
			_, err = model.UpdateChannelMonitorSettingsOptions(values, true, common.GetPointer("before-remove"))
			require.NoError(t, err)
			identity := model.LogicalChannelIdentity{ChannelID: 9801, LogicalChannelID: logical.Id, Revision: logical.Revision}
			for _, readd := range []bool{false, true} {
				if readd {
					values[channelMonitorSmartScheduleGroupPoliciesOption] = policies
					values[channelMonitorSmartScheduleControlRevisionOption] = "re-added"
					_, err = model.UpdateChannelMonitorSettingsOptions(values, true, common.GetPointer("after-remove"))
					require.NoError(t, err)
					require.NoError(t, model.InitializeChannelSmartScheduleRouteStates())
				}
				_, err = model.CoalesceChannelSmartScheduleSchedulingRoutes(staleRoutes, "before-remove")
				require.ErrorIs(t, err, model.ErrChannelMonitorSettingsChanged)
				_, err = model.SaveLogicalChannelSmartScheduleModelSample(identity, "vip", model.ChannelSmartScheduleModelSampleResult{
					ChannelId: 9801, Model: "model-a", SampleId: "old-probe", Success: true, Time: common.GetTimestamp(),
				}, "before-remove")
				require.ErrorIs(t, err, model.ErrChannelMonitorSettingsChanged)
				for _, table := range []any{&model.ChannelLogicalSmartScheduleRouteState{}, &model.ChannelLogicalSmartScheduleSampleState{}} {
					var count int64
					require.NoError(t, db.Model(table).Count(&count).Error)
					assert.Zero(t, count, "stale work cannot recreate deleted logical state, including after re-adding the policy")
				}
			}
			freshRoutes, err := model.GetChannelSmartScheduleRoutes()
			require.NoError(t, err)
			freshRoutes, err = model.CoalesceChannelSmartScheduleSchedulingRoutes(freshRoutes, "re-added")
			require.NoError(t, err)
			require.Len(t, freshRoutes, 1)
			assert.True(t, freshRoutes[0].State.Excluded)
			assert.Empty(t, freshRoutes[0].State.StabilityState)
			assert.Zero(t, freshRoutes[0].State.RuntimeProtectionUntil)
			outcomes, err := model.ApplyChannelSmartScheduleRouteResults([]model.ChannelSmartScheduleRouteResultUpdate{{
				ChannelId: 9801, LogicalChannelId: logical.Id, LogicalRevision: logical.Revision,
				ExpectedLogicalStateRevision: freshRoutes[0].State.Revision,
				ExpectedControlRevision:      "re-added", Group: "vip", Model: "model-a",
				Status: model.ChannelSmartScheduleStatusSucceeded,
			}})
			require.NoError(t, err)
			require.Len(t, outcomes, 1)
			assert.True(t, outcomes[0].Applied)
			_, err = model.SaveLogicalChannelSmartScheduleModelSample(identity, "vip", model.ChannelSmartScheduleModelSampleResult{
				ChannelId: 9801, Model: "model-a", SampleId: "new-probe", Success: true, Time: common.GetTimestamp(),
			}, "re-added")
			require.NoError(t, err)
		})
	}
}
