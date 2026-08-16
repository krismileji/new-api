package service

import "github.com/QuantumNous/new-api/model"

// EnqueueRequiredSystemTask wakes the runner after ensuring that the supplied
// payload is attached to a pending task, including behind an active run.
func EnqueueRequiredSystemTask(taskType string, payload any) (*model.SystemTask, bool, error) {
	task, created, err := model.EnqueueRequiredSystemTask(taskType, payload)
	if err != nil {
		return nil, false, err
	}
	notifySystemTaskRunner()
	return task, created, nil
}

func EnqueueRequiredSystemTaskAfterRedisSequence(
	taskType string,
	payload any,
	eventSequence int64,
) (*model.SystemTask, bool, bool, error) {
	task, created, applied, err := model.EnqueueRequiredSystemTaskAfterRedisSequence(
		taskType,
		payload,
		eventSequence,
	)
	if err != nil {
		return nil, false, false, err
	}
	notifySystemTaskRunner()
	return task, created, applied, nil
}
