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
