package model

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDeleteChannelMonitorHistoryBeforeHonorsRetentionAndTaskGuards(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-history-retention.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(
		&SystemTask{},
		&ChannelSmartScheduleExecutionDetail{},
		&ChannelRatioHistory{},
	))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	const (
		monitoredTaskType = "channel_smart_schedule"
		ignoredTaskType   = "unrelated_task"
		detailCutoff      = int64(20_000)
		taskCutoff        = int64(10_000)
		ratioCutoff       = int64(30_000)
	)
	tasks := []SystemTask{
		{TaskID: "recent-terminal", Type: monitoredTaskType, Status: SystemTaskStatusSucceeded, CreatedAt: 1, UpdatedAt: taskCutoff},
		{TaskID: "old-pending", Type: monitoredTaskType, Status: SystemTaskStatusPending, CreatedAt: 1, UpdatedAt: 1},
		{TaskID: "old-running", Type: monitoredTaskType, Status: SystemTaskStatusRunning, CreatedAt: 1, UpdatedAt: 1},
		{TaskID: "old-delete", Type: monitoredTaskType, Status: SystemTaskStatusFailed, CreatedAt: 1, UpdatedAt: taskCutoff - 1},
		{TaskID: "old-protected-a", Type: monitoredTaskType, Status: SystemTaskStatusSucceeded, CreatedAt: 1, UpdatedAt: taskCutoff - 2},
		{TaskID: "old-protected-b", Type: monitoredTaskType, Status: SystemTaskStatusFailed, CreatedAt: 1, UpdatedAt: taskCutoff - 3},
		{TaskID: "ignored-old", Type: ignoredTaskType, Status: SystemTaskStatusSucceeded, CreatedAt: 1, UpdatedAt: 1},
	}
	require.NoError(t, db.Create(&tasks).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleExecutionDetail{
		{TaskId: "old-delete", AdjustmentIndex: 0, Payload: `{}`, CreatedAt: detailCutoff},
		{TaskId: "old-protected-a", AdjustmentIndex: 0, Payload: `{}`, CreatedAt: detailCutoff - 1},
		{TaskId: "old-protected-b", AdjustmentIndex: 0, Payload: `{}`, CreatedAt: detailCutoff},
		{TaskId: "missing-task", AdjustmentIndex: 0, Payload: `{}`, CreatedAt: detailCutoff - 2},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelRatioHistory{
		{ChannelId: 1, OldRatio: 1, NewRatio: 2, CreatedTime: ratioCutoff - 1},
		{ChannelId: 2, OldRatio: 2, NewRatio: 3, CreatedTime: ratioCutoff - 2},
		{ChannelId: 3, OldRatio: 3, NewRatio: 4, CreatedTime: ratioCutoff},
	}).Error)

	exhaustedBudget := ChannelMonitorCleanupBudget{deadline: time.Now().Add(-time.Second)}
	result, err := DeleteChannelMonitorHistoryBefore(
		context.Background(),
		ChannelMonitorHistoryRetentionCutoffs{
			ExecutionDetail: detailCutoff,
			Task:            taskCutoff,
			RatioHistory:    ratioCutoff,
		},
		[]string{monitoredTaskType},
		2,
		1,
		exhaustedBudget,
	)
	require.NoError(t, err)
	assert.True(t, result.Incomplete)
	assert.Zero(t, result.ExecutionDetailRowsDeleted)
	assert.Zero(t, result.TaskRowsDeleted)
	assert.Zero(t, result.RatioHistoryRowsDeleted)
	var beforeResume int64
	require.NoError(t, db.Model(&ChannelRatioHistory{}).Count(&beforeResume).Error)
	assert.EqualValues(t, 3, beforeResume)

	result, err = DeleteChannelMonitorHistoryBefore(
		context.Background(),
		ChannelMonitorHistoryRetentionCutoffs{
			ExecutionDetail: detailCutoff,
			Task:            taskCutoff,
			RatioHistory:    ratioCutoff,
		},
		[]string{monitoredTaskType},
		2,
		1,
		ChannelMonitorCleanupBudget{},
	)
	require.NoError(t, err)
	assert.False(t, result.Incomplete)
	assert.Equal(t, int64(3), result.ExecutionDetailRowsDeleted)
	assert.Equal(t, int64(1), result.TaskRowsDeleted)
	assert.Equal(t, int64(2), result.RatioHistoryRowsDeleted)

	var remainingTasks []SystemTask
	require.NoError(t, db.Order("id ASC").Find(&remainingTasks).Error)
	remainingTaskIDs := make([]string, 0, len(remainingTasks))
	for _, task := range remainingTasks {
		remainingTaskIDs = append(remainingTaskIDs, task.TaskID)
	}
	assert.Equal(t, []string{
		"recent-terminal",
		"old-pending",
		"old-running",
		"old-protected-a",
		"old-protected-b",
		"ignored-old",
	}, remainingTaskIDs)

	var remainingDetails []ChannelSmartScheduleExecutionDetail
	require.NoError(t, db.Order("id ASC").Find(&remainingDetails).Error)
	require.Len(t, remainingDetails, 1)
	assert.Equal(t, "old-protected-b", remainingDetails[0].TaskId)

	var remainingRatioHistory []ChannelRatioHistory
	require.NoError(t, db.Find(&remainingRatioHistory).Error)
	require.Len(t, remainingRatioHistory, 1)
	assert.Equal(t, 3, remainingRatioHistory[0].ChannelId)
}

func TestDeleteChannelMonitorHistoryBeforeRejectsInvalidArguments(t *testing.T) {
	validCutoffs := ChannelMonitorHistoryRetentionCutoffs{
		ExecutionDetail: 1,
		Task:            1,
		RatioHistory:    1,
	}
	tests := []struct {
		name       string
		cutoffs    ChannelMonitorHistoryRetentionCutoffs
		taskTypes  []string
		keepLatest int
		batchSize  int
	}{
		{name: "invalid execution detail cutoff", cutoffs: ChannelMonitorHistoryRetentionCutoffs{Task: 1, RatioHistory: 1}, taskTypes: []string{"task"}, keepLatest: 1, batchSize: 1},
		{name: "invalid task cutoff", cutoffs: ChannelMonitorHistoryRetentionCutoffs{ExecutionDetail: 1, RatioHistory: 1}, taskTypes: []string{"task"}, keepLatest: 1, batchSize: 1},
		{name: "invalid ratio history cutoff", cutoffs: ChannelMonitorHistoryRetentionCutoffs{ExecutionDetail: 1, Task: 1}, taskTypes: []string{"task"}, keepLatest: 1, batchSize: 1},
		{name: "task retention shorter than execution details", cutoffs: ChannelMonitorHistoryRetentionCutoffs{ExecutionDetail: 1, Task: 2, RatioHistory: 1}, taskTypes: []string{"task"}, keepLatest: 1, batchSize: 1},
		{name: "missing task types", cutoffs: validCutoffs, taskTypes: []string{" "}, keepLatest: 1, batchSize: 1},
		{name: "invalid keep latest", cutoffs: validCutoffs, taskTypes: []string{"task"}, batchSize: 1},
		{name: "invalid batch size", cutoffs: validCutoffs, taskTypes: []string{"task"}, keepLatest: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DeleteChannelMonitorHistoryBefore(
				context.Background(),
				test.cutoffs,
				test.taskTypes,
				test.keepLatest,
				test.batchSize,
				ChannelMonitorCleanupBudget{},
			)
			assert.Error(t, err)
		})
	}
}
