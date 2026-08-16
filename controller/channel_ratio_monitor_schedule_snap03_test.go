package controller

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSmartScheduleTaskPayloadMergesTriggerAttribution(t *testing.T) {
	first := newChannelSmartScheduleTaskPayload("channel.create", "channel_added", "channel_added")
	second := newChannelSmartScheduleTaskPayload("channel.status_update", "channel_status_changed")
	second.ForceReset = true

	firstJSON, err := common.Marshal(first)
	require.NoError(t, err)
	mergedJSON, err := second.MergeRequiredSystemTaskPayload(string(firstJSON))
	require.NoError(t, err)
	var merged channelSmartScheduleTaskPayload
	require.NoError(t, common.UnmarshalJsonStr(mergedJSON, &merged))

	assert.Equal(t, "channel.create", merged.TriggerSource)
	assert.Equal(t, 2, merged.TriggerCount)
	assert.Equal(t, first.FirstRequestedAt, merged.FirstRequestedAt)
	assert.Equal(t, second.LastRequestedAt, merged.LastRequestedAt)
	assert.True(t, merged.ForceReset)
	assert.Equal(t, []string{"channel_added", "channel_status_changed"}, merged.DirtyReasons)
}

func TestEnqueueRequiredSmartScheduleTaskAggregatesPendingTriggers(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	first := newChannelSmartScheduleTaskPayload("channel.create", "channel_added")
	second := newChannelSmartScheduleTaskPayload("channel.delete", "channel_removed")

	task, created, err := service.EnqueueRequiredSystemTask(channelMonitorSmartScheduleTaskType, first)
	require.NoError(t, err)
	assert.True(t, created)
	secondTask, created, err := service.EnqueueRequiredSystemTask(channelMonitorSmartScheduleTaskType, second)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, task.TaskID, secondTask.TaskID)

	stored, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	var payload channelSmartScheduleTaskPayload
	require.NoError(t, stored.DecodePayload(&payload))
	assert.Equal(t, 2, payload.TriggerCount)
	assert.Equal(t, "channel.create", payload.TriggerSource)
	assert.Equal(t, []string{"channel_added", "channel_removed"}, payload.DirtyReasons)
	assert.Equal(t, first.FirstRequestedAt, payload.FirstRequestedAt)
	assert.Equal(t, second.LastRequestedAt, payload.LastRequestedAt)
}

func TestChannelSmartScheduleTaskPayloadUsesFallbackAttribution(t *testing.T) {
	payload := newChannelSmartScheduleTaskPayload("", "")
	assert.Equal(t, channelSmartScheduleTriggerFallback, payload.TriggerSource)
	assert.Equal(t, 1, payload.TriggerCount)
	assert.Equal(t, []string{"unspecified"}, payload.DirtyReasons)
}

func TestChannelSmartScheduleTriggerAttributionDoesNotChangeCompleteResult(t *testing.T) {
	tests := []struct {
		name    string
		payload channelSmartScheduleTaskPayload
	}{
		{
			name: "without attribution",
			payload: channelSmartScheduleTaskPayload{
				ForceReset: true,
			},
		},
		{
			name: "with attribution",
			payload: channelSmartScheduleTaskPayload{
				ForceReset:       true,
				TriggerSource:    "channel.status_update",
				TriggerCount:     7,
				FirstRequestedAt: 1_700_000_000,
				LastRequestedAt:  1_700_000_123,
				DirtyReasons:     []string{"channel_status_changed", "route_health_changed"},
			},
		},
	}
	results := make([]channelSmartScheduleTaskResult, len(tests))
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.ChannelSmartScheduleExecutionDetail{}))
			useChannelSmartScheduleGroupRatio(t, `{"vip":100}`)
			useChannelMonitorOptionMap(t, map[string]string{
				channelMonitorSmartScheduleEnabledOption: "true",
				channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
					t,
					channelSmartScheduleTestGroupPolicy(
						"vip", channelMonitorSmartScheduleStrategyRatio, false,
						channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 1, 90, 30,
					),
				),
			})
			priority := int64(80)
			weight := uint(50)
			require.NoError(t, db.Create(&[]model.Channel{
				{Id: 3101, Name: "low cost", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
				{Id: 3102, Name: "high cost", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
			}).Error)
			require.NoError(t, db.Create(&[]model.Ability{
				{ChannelId: 3101, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
				{ChannelId: 3102, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
			}).Error)
			require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
				{ChannelId: 3101, Ratio: 1, UpdatedTime: 1},
				{ChannelId: 3102, Ratio: 2, UpdatedTime: 1},
			}).Error)
			require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
				{ChannelId: 3101, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
				{ChannelId: 3102, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
			}).Error)

			task, err := model.CreateSystemTask(channelMonitorSmartScheduleTaskType, test.payload, nil)
			require.NoError(t, err)
			const runnerID = "snap03-attribution-runner"
			claimedTask, claimed, err := model.ClaimSystemTask(
				task.ID,
				task.Type,
				runnerID,
				common.GetTimestamp()+60,
			)
			require.NoError(t, err)
			require.True(t, claimed)
			channelSmartScheduleTaskHandler{}.Run(context.Background(), claimedTask, runnerID)

			storedTask, err := model.GetSystemTaskByTaskID(task.TaskID)
			require.NoError(t, err)
			require.NotNil(t, storedTask)
			require.Equal(t, model.SystemTaskStatusSucceeded, storedTask.Status)
			var result channelSmartScheduleTaskResult
			require.NoError(t, common.UnmarshalJsonStr(storedTask.Result, &result))
			detailsByTask, err := model.GetChannelSmartScheduleExecutionDetails([]string{task.TaskID})
			require.NoError(t, err)
			for _, detail := range detailsByTask[task.TaskID] {
				var adjustment channelSmartScheduleTaskAdjustment
				require.NoError(t, common.UnmarshalJsonStr(detail.Payload, &adjustment))
				result.Adjustments = append(result.Adjustments, adjustment)
			}
			require.Len(t, result.Adjustments, 2)
			for adjustmentIndex := range result.Adjustments {
				details := result.Adjustments[adjustmentIndex].ScoreDetails
				if details == nil {
					continue
				}
				details.WindowStart = 0
				details.WindowEnd = 0
				details.DataCutoffAt = 0
			}
			results[index] = result
		})
	}

	assert.Equal(t, results[0], results[1])
}
