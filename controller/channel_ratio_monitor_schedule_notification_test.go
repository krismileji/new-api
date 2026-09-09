package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelSmartScheduleNotificationRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	original := common.RDBMonitorWrite
	common.RDBMonitorWrite = client
	t.Cleanup(func() {
		common.RDBMonitorWrite = original
		assert.NoError(t, client.Close())
	})
	return server
}

func TestChannelSmartScheduleFailureNotificationRespectsSelection(t *testing.T) {
	for _, test := range []struct {
		name    string
		enabled bool
		types   []string
		failed  int
		err     error
		want    int
	}{
		{name: "selected", enabled: true, types: []string{"smart_schedule_failed"}, err: errors.New("读取快照失败"), want: 1},
		{name: "partial_failure", enabled: true, types: []string{"smart_schedule_failed"}, failed: 1, want: 1},
		{name: "disabled", types: []string{"smart_schedule_failed"}, err: assert.AnError},
		{name: "unselected", enabled: true, types: []string{"task_failed"}, err: assert.AnError},
		{name: "no_selection", enabled: true, err: assert.AnError},
		{name: "success", enabled: true, types: []string{"smart_schedule_failed"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupChannelSmartScheduleNotificationRedis(t)
			var notifier channelSmartScheduleFailureNotifier
			calls := 0
			err := notifier.notify(context.Background(), channelMonitorSettings{
				EmailNotificationEnabled: test.enabled, NotificationEmail: "admin@example.com", EmailNotificationTypes: test.types,
			}, "task-a", channelSmartScheduleTaskResult{Failed: test.failed}, test.err, time.Unix(100, 0),
				func(subject, receiver, content string) error {
					calls++
					assert.Equal(t, "admin@example.com", receiver)
					assert.Contains(t, subject, "智能调度失败")
					assert.Contains(t, content, "task-a")
					return nil
				})
			require.NoError(t, err)
			assert.Equal(t, test.want, calls)
		})
	}
}

func TestChannelSmartScheduleFailureNotificationCooldownAndRetry(t *testing.T) {
	server := setupChannelSmartScheduleNotificationRedis(t)
	settings := channelMonitorSettings{EmailNotificationEnabled: true, NotificationEmail: "admin@example.com", EmailNotificationTypes: []string{"smart_schedule_failed"}}
	var first, second channelSmartScheduleFailureNotifier
	now := time.Unix(100, 0)
	calls := 0
	send := func(string, string, string) error {
		calls++
		if calls == 1 {
			return errors.New("SMTP unavailable")
		}
		return nil
	}
	require.ErrorContains(t, first.notify(context.Background(), settings, "first", channelSmartScheduleTaskResult{}, assert.AnError, now, send), "SMTP unavailable")
	require.NoError(t, first.notify(context.Background(), settings, "retry-too-soon", channelSmartScheduleTaskResult{}, assert.AnError, now.Add(30*time.Second), send))
	require.NoError(t, second.notify(context.Background(), settings, "another-node", channelSmartScheduleTaskResult{}, assert.AnError, now, send))
	assert.Equal(t, 1, calls)
	server.FastForward(time.Minute)
	now = now.Add(time.Minute)
	require.NoError(t, first.notify(context.Background(), settings, "retry", channelSmartScheduleTaskResult{}, assert.AnError, now, send))
	require.NoError(t, second.notify(context.Background(), settings, "duplicate", channelSmartScheduleTaskResult{}, assert.AnError, now, send))
	assert.Equal(t, 2, calls)
	server.FastForward(15 * time.Minute)
	require.NoError(t, second.notify(context.Background(), settings, "still-failing", channelSmartScheduleTaskResult{}, assert.AnError, now.Add(15*time.Minute), send))
	assert.Equal(t, 3, calls)
}

func TestChannelSmartScheduleFailureNotificationIncludesEscapedFailureDetails(t *testing.T) {
	section := channelSmartScheduleFailureEmailSection("task<script>", channelSmartScheduleTaskResult{
		Total: 20, Failed: 20, FailureDetailsTruncated: true,
		Failures: []channelSmartScheduleTaskFailure{{ChannelId: 12, ChannelName: "<channel>", Group: "vip", Model: "model-a",
			Stage: "configuration_conflict", Error: "路由优先级已变化（快照 10，当前 20）"}},
	}, errors.New("<upstream error>"))
	assert.Contains(t, section.HTML, "&lt;channel&gt;")
	assert.Contains(t, section.HTML, "&lt;upstream error&gt;")
	assert.NotContains(t, section.HTML, "<script>")
	for _, text := range []string{"失败 20 条", "vip", "model-a", "配置冲突", "快照 10，当前 20", "前 1 条失败明细"} {
		assert.Contains(t, section.HTML, text)
	}
}

func TestChannelSmartScheduleTaskHandlerNotifiesPersistedRouteFailures(t *testing.T) {
	channelSmartScheduleFailureNotifications.Lock()
	originalReceiver, originalNext := channelSmartScheduleFailureNotifications.receiver, channelSmartScheduleFailureNotifications.nextAttemptAt
	channelSmartScheduleFailureNotifications.Unlock()
	t.Cleanup(func() {
		channelSmartScheduleFailureNotifications.Lock()
		channelSmartScheduleFailureNotifications.receiver, channelSmartScheduleFailureNotifications.nextAttemptAt = originalReceiver, originalNext
		channelSmartScheduleFailureNotifications.Unlock()
	})
	for _, stage := range []string{"write", "configuration_conflict"} {
		t.Run(stage, func(t *testing.T) { runChannelSmartScheduleTaskFailureNotification(t, stage) })
	}
}

func runChannelSmartScheduleTaskFailureNotification(t *testing.T, stage string) {
	t.Helper()
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelSmartScheduleExecutionDetail{}))
	setupChannelSmartScheduleNotificationRedis(t)
	useChannelSmartScheduleGroupRatio(t, `{"vip":100}`)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorEmailNotificationOption: "true", channelMonitorNotificationEmailOption: stage + "-handler@example.com",
		channelMonitorEmailNotificationTypesOption: `["smart_schedule_failed"]`,
		channelMonitorSmartScheduleEnabledOption:   "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			channelSmartScheduleTestGroupPolicy("vip", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 1, 90, 30)),
	})
	priority := int64(80)
	require.NoError(t, db.Create(&model.Channel{Id: 1101, Name: "test channel", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 1101, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 50}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{ChannelId: 1101, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 1101, Ratio: 1, UpdatedTime: 1}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 1102, Name: "second channel", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 1102, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 50}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{ChannelId: 1102, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 1102, Ratio: 2, UpdatedTime: 1}).Error)
	const callback = "test:schedule_notification_write_failure"
	if stage == "write" {
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Table == "abilities" {
				tx.AddError(errors.New("模拟路由写入失败"))
			}
		}))
		t.Cleanup(func() { assert.NoError(t, db.Callback().Update().Remove(callback)) })
	} else {
		changed := false
		require.NoError(t, db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
			if changed || tx.Statement.Table != "channel_ratio_monitors" {
				return
			}
			changed = true
			tx.AddError(tx.Session(&gorm.Session{NewDB: true}).Model(&model.Ability{}).Where("channel_id = ?", 1102).Update("weight", 70).Error)
		}))
		t.Cleanup(func() { assert.NoError(t, db.Callback().Query().Remove(callback)) })
	}
	task, err := model.CreateSystemTask(channelMonitorSmartScheduleTaskType, channelSmartScheduleTaskPayload{}, nil)
	require.NoError(t, err)
	claimed, ok, err := model.ClaimSystemTask(task.ID, task.Type, "notification-runner", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)
	calls := 0
	handler := channelSmartScheduleTaskHandler{sendEmail: func(subject, receiver, content string) error {
		calls++
		stored, readErr := model.GetSystemTaskByTaskID(task.TaskID)
		require.NoError(t, readErr)
		assert.Equal(t, model.SystemTaskStatusFailed, stored.Status)
		assert.Contains(t, subject, "智能调度失败")
		if stage == "write" {
			assert.Contains(t, content, "模拟路由写入失败")
			assert.Contains(t, content, "结果写入")
		} else {
			assert.Contains(t, content, "渠道 1102")
			assert.Contains(t, content, "路由权重已变化（快照 50，当前 70）")
			assert.Contains(t, content, "配置冲突")
			assert.Contains(t, stored.Error, "路由权重已变化")
		}
		return nil
	}}
	handler.Run(context.Background(), claimed, "notification-runner")
	stored, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.SystemTaskStatusFailed, stored.Status)
	assert.Equal(t, 1, calls)
}
