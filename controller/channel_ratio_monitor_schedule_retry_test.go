package controller

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelSmartScheduleConflictRetryPersistsBoundedBackoff(t *testing.T) {
	now := time.Unix(1789314167, 0)
	for _, test := range []struct {
		previous int
		attempt  int
		delay    time.Duration
	}{
		{-1, 1, 5 * time.Second},
		{0, 1, 5 * time.Second},
		{1, 2, 10 * time.Second},
		{2, 3, 20 * time.Second},
		{3, 4, 40 * time.Second},
		{4, 5, time.Minute},
		{math.MaxInt, 5, time.Minute},
	} {
		payload := newChannelSmartScheduleConflictRetry(channelSmartScheduleTaskPayload{
			ConflictRetryAttempt: test.previous,
		}, now)
		encoded, err := common.Marshal(payload)
		require.NoError(t, err)
		var restored channelSmartScheduleTaskPayload
		require.NoError(t, common.Unmarshal(encoded, &restored))
		assert.Equal(t, test.attempt, restored.ConflictRetryAttempt)
		assert.Equal(t, now.Add(test.delay).Unix(), restored.RetryNotBefore)
		assert.Equal(t, "channel_monitor.schedule_conflict", restored.TriggerSource)
		assert.Equal(t, []string{"route_configuration_conflict"}, restored.DirtyReasons)
	}
}

func TestChannelSmartScheduleConflictRetryMergesWithoutDelayingFreshInput(t *testing.T) {
	retry := newChannelSmartScheduleConflictRetry(channelSmartScheduleTaskPayload{}, time.Unix(100, 0))
	laterRetry := newChannelSmartScheduleConflictRetry(retry, time.Unix(101, 0))
	fresh := newChannelSmartScheduleTaskPayload("channel.status_update", "channel_status_changed")
	fresh.ForceReset = true
	for _, test := range []struct {
		name     string
		pending  channelSmartScheduleTaskPayload
		incoming channelSmartScheduleTaskPayload
		attempt  int
		due      int64
	}{
		{"fresh input accelerates pending retry", retry, fresh, 0, 0},
		{"retry does not delay pending input", fresh, retry, 0, 0},
		{"coalesced retries retain earliest deadline", retry, laterRetry, 2, 105},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := common.Marshal(test.pending)
			require.NoError(t, err)
			merged, err := test.incoming.MergeRequiredSystemTaskPayload(string(raw))
			require.NoError(t, err)
			var payload channelSmartScheduleTaskPayload
			require.NoError(t, common.UnmarshalJsonStr(merged, &payload))
			assert.Equal(t, test.attempt, payload.ConflictRetryAttempt)
			assert.Equal(t, test.due, payload.RetryNotBefore)
			assert.Equal(t, test.pending.ForceReset || test.incoming.ForceReset, payload.ForceReset)
		})
	}
}

func TestChannelSmartScheduleConflictRetryPreservesPoolAndRecovers(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelSmartScheduleExecutionDetail{}))
	useChannelSmartScheduleGroupRatio(t, `{"vip":100}`)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			channelSmartScheduleTestGroupPolicy("vip", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 1, 80, 30),
		),
	})
	priority, weight := int64(80), uint(50)
	for _, id := range []int{1710, 1711} {
		require.NoError(t, db.Create(&model.Channel{
			Id: id, Name: "冲突恢复", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled,
			Priority: &priority, Weight: &weight,
		}).Error)
		require.NoError(t, db.Create(&model.Ability{
			ChannelId: id, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight,
		}).Error)
		require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: id, Ratio: 1, UpdatedTime: 1}).Error)
		require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
			ChannelId: id, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1,
		}).Error)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changed := false
	const callback = "test:exclude_route_after_schedule_read"
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if changed || tx.Statement.Context != ctx || tx.Statement.Table != "channel_smart_schedule_route_states" {
			return
		}
		changed = true
		tx.AddError(db.Model(&model.ChannelSmartScheduleRouteState{}).Where("channel_id = ?", 1711).
			Updates(map[string]any{"excluded": true, "revision": 2}).Error)
	}))
	t.Cleanup(func() { assert.NoError(t, db.Callback().Query().Remove(callback)) })
	task, err := model.CreateSystemTask(channelMonitorSmartScheduleTaskType, newChannelSmartScheduleTaskPayload("manual_run"), nil)
	require.NoError(t, err)
	claimed, ok, err := model.ClaimSystemTask(task.ID, task.Type, "conflict-runner", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)
	channelSmartScheduleTaskHandler{}.Run(ctx, claimed, "conflict-runner")
	require.True(t, changed)
	stored, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.Equal(t, model.SystemTaskStatusFailed, stored.Status)
	assert.Contains(t, stored.Error, "排除调度设置已变化")
	var states []model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Order("channel_id ASC").Find(&states).Error)
	require.Len(t, states, 2)
	assert.Equal(t, int64(1), states[0].Revision)
	assert.Equal(t, int64(2), states[1].Revision)
	assert.True(t, states[1].Excluded)
	for _, state := range states {
		assert.Zero(t, state.LastScheduleTime, "the entire conflicted pool must retain its previous result")
	}
	var abilities []model.Ability
	require.NoError(t, db.Find(&abilities).Error)
	for _, ability := range abilities {
		assert.Equal(t, weight, ability.Weight)
	}
	retryTask, err := model.GetActiveSystemTask(task.Type)
	require.NoError(t, err)
	require.NotNil(t, retryTask)
	require.Equal(t, model.SystemTaskStatusPending, retryTask.Status)
	var payload channelSmartScheduleTaskPayload
	require.NoError(t, retryTask.DecodePayload(&payload))
	assert.Equal(t, 1, payload.ConflictRetryAttempt)
	assert.Greater(t, payload.RetryNotBefore, retryTask.CreatedAt)
	assert.Equal(t, "channel_monitor.schedule_conflict", payload.TriggerSource)

	// Make the persisted deadline due, without a sleep or a background timer.
	payload.RetryNotBefore = common.GetTimestamp() - 1
	raw, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.SystemTask{}).Where("id = ?", retryTask.ID).Update("payload", string(raw)).Error)
	recovered, ok, err := model.ClaimSystemTask(retryTask.ID, task.Type, "recovery-runner", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)
	channelSmartScheduleTaskHandler{}.Run(context.Background(), recovered, "recovery-runner")
	stored, err = model.GetSystemTaskByTaskID(retryTask.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.SystemTaskStatusSucceeded, stored.Status)
	require.NoError(t, db.Order("channel_id ASC").Find(&states).Error)
	assert.Positive(t, states[0].LastScheduleTime)
	assert.True(t, states[1].Excluded, "recovery must respect the concurrent configuration change")
	active, err := model.GetActiveSystemTask(task.Type)
	require.NoError(t, err)
	assert.Nil(t, active, "a recovered task must not schedule another retry")
}

func TestChannelSmartScheduleConflictRetryHonorsCancellationBeforeCalculation(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	payload := newChannelSmartScheduleConflictRetry(channelSmartScheduleTaskPayload{}, time.Now())
	task, err := model.CreateSystemTask(channelMonitorSmartScheduleTaskType, payload, nil)
	require.NoError(t, err)
	claimed, ok, err := model.ClaimSystemTask(task.ID, task.Type, "cancelled-runner", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	channelSmartScheduleTaskHandler{}.Run(ctx, claimed, "cancelled-runner")
	stored, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.SystemTaskStatusFailed, stored.Status)
	assert.Equal(t, context.Canceled.Error(), stored.Error)
}
