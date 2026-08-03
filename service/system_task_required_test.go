package service

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type requiredSystemTaskPayload struct {
	ForceReset bool `json:"force_reset,omitempty"`
}

func TestEnqueueRequiredSystemTaskUpgradesPendingPayload(t *testing.T) {
	truncate(t)
	pending, err := model.CreateSystemTask("required_pending", nil, nil)
	require.NoError(t, err)

	queued, created, err := EnqueueRequiredSystemTask(
		pending.Type,
		requiredSystemTaskPayload{ForceReset: true},
	)
	require.NoError(t, err)
	require.False(t, created)
	require.NotNil(t, queued)
	assert.Equal(t, pending.TaskID, queued.TaskID)

	payload := requiredSystemTaskPayload{}
	require.NoError(t, queued.DecodePayload(&payload))
	assert.True(t, payload.ForceReset)
}

func TestEnqueueRequiredSystemTaskQueuesOneSuccessorBehindRunningTask(t *testing.T) {
	truncate(t)
	running, err := model.CreateSystemTask("required_running", nil, nil)
	require.NoError(t, err)
	const runnerID = "required-runner"
	running, claimed, err := model.ClaimSystemTask(
		running.ID,
		running.Type,
		runnerID,
		common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	require.True(t, claimed)

	const callers = 12
	start := make(chan struct{})
	type enqueueResult struct {
		task    *model.SystemTask
		created bool
		err     error
	}
	results := make(chan enqueueResult, callers)
	var calls sync.WaitGroup
	for range callers {
		calls.Add(1)
		go func() {
			defer calls.Done()
			<-start
			task, created, enqueueErr := EnqueueRequiredSystemTask(
				running.Type,
				requiredSystemTaskPayload{ForceReset: true},
			)
			results <- enqueueResult{task: task, created: created, err: enqueueErr}
		}()
	}
	close(start)
	calls.Wait()
	close(results)

	createdCount := 0
	successorTaskID := ""
	for result := range results {
		require.NoError(t, result.err)
		require.NotNil(t, result.task)
		if result.created {
			createdCount++
		}
		if successorTaskID == "" {
			successorTaskID = result.task.TaskID
		}
		assert.Equal(t, successorTaskID, result.task.TaskID)
	}
	assert.Equal(t, 1, createdCount)

	var tasks []*model.SystemTask
	require.NoError(t, model.DB.Where("type = ?", running.Type).Order("id asc").Find(&tasks).Error)
	require.Len(t, tasks, 2)
	assert.Equal(t, model.SystemTaskStatusRunning, tasks[0].Status)
	assert.Nil(t, tasks[0].ActiveKey)
	assert.Equal(t, model.SystemTaskStatusPending, tasks[1].Status)
	require.NotNil(t, tasks[1].ActiveKey)
	assert.Equal(t, running.Type, *tasks[1].ActiveKey)
	payload := requiredSystemTaskPayload{}
	require.NoError(t, tasks[1].DecodePayload(&payload))
	assert.True(t, payload.ForceReset)

	require.NoError(t, model.FinishSystemTask(
		running.TaskID,
		runnerID,
		model.SystemTaskStatusSucceeded,
		nil,
		"",
	))
	successor, claimed, err := model.ClaimSystemTask(
		tasks[1].ID,
		tasks[1].Type,
		"successor-runner",
		common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	assert.True(t, claimed)
	require.NotNil(t, successor)
}
