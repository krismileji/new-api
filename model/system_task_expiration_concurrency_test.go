package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestExpireStaleSystemTaskLocksKeepsConcurrentlyRenewedLease(t *testing.T) {
	truncateTables(t)

	task, err := CreateSystemTask("renewed-during-expiration", nil, nil)
	require.NoError(t, err)
	const runnerID = "renewed-runner"
	now := common.GetTimestamp()
	task, claimed, err := ClaimSystemTask(task.ID, task.Type, runnerID, now-1)
	require.NoError(t, err)
	require.True(t, claimed)

	callbackName := "test:renew_system_task_after_stale_snapshot"
	var renewOnce sync.Once
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "SystemTaskLock" {
			return
		}
		renewOnce.Do(func() {
			require.NoError(t, DB.Model(&SystemTaskLock{}).
				Where("task_id = ? AND locked_by = ?", task.TaskID, runnerID).
				Update("locked_until", now+60).Error)
		})
	}))
	t.Cleanup(func() { _ = DB.Callback().Query().Remove(callbackName) })

	require.NoError(t, ExpireStaleSystemTaskLocks(now))

	current, err := GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, current)
	assert.Equal(t, SystemTaskStatusRunning, current.Status)
	var lock SystemTaskLock
	require.NoError(t, DB.Where("task_id = ?", task.TaskID).First(&lock).Error)
	assert.Equal(t, now+60, lock.LockedUntil)
}

func TestExpireStaleSystemTaskLocksDoesNotOverwriteTerminalTask(t *testing.T) {
	truncateTables(t)

	task, err := CreateSystemTask("completed-before-expiration", nil, nil)
	require.NoError(t, err)
	const runnerID = "completed-runner"
	now := common.GetTimestamp()
	_, claimed, err := ClaimSystemTask(task.ID, task.Type, runnerID, now-1)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, DB.Model(&SystemTask{}).
		Where("task_id = ?", task.TaskID).
		Updates(map[string]any{
			"status":     SystemTaskStatusSucceeded,
			"active_key": nil,
			"error":      "",
		}).Error)

	require.NoError(t, ExpireStaleSystemTaskLocks(now))

	current, err := GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, current)
	assert.Equal(t, SystemTaskStatusSucceeded, current.Status)
	assert.Empty(t, current.Error)
	var lockCount int64
	require.NoError(t, DB.Model(&SystemTaskLock{}).
		Where("task_id = ?", task.TaskID).
		Count(&lockCount).Error)
	assert.Zero(t, lockCount)
}
