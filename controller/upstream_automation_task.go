package controller

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

const upstreamAutomationTaskType = "upstream_automation"

func init() { service.RegisterSystemTaskHandler(upstreamAutomationTaskHandler{}) }

type upstreamAutomationTaskPayload struct {
	IDs []string `json:"ids"`
}

func (payload upstreamAutomationTaskPayload) MergeRequiredSystemTaskPayload(existing string) (string, error) {
	var previous upstreamAutomationTaskPayload
	if existing != "" {
		if err := common.UnmarshalJsonStr(existing, &previous); err != nil {
			return "", err
		}
	}
	seen := make(map[string]bool)
	ids := make([]string, 0, len(previous.IDs)+len(payload.IDs))
	for _, id := range append(previous.IDs, payload.IDs...) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	encoded, err := common.Marshal(upstreamAutomationTaskPayload{IDs: ids})
	return string(encoded), err
}

type upstreamAutomationTaskHandler struct{}

func (upstreamAutomationTaskHandler) Type() string            { return upstreamAutomationTaskType }
func (upstreamAutomationTaskHandler) Interval() time.Duration { return 30 * time.Second }
func (upstreamAutomationTaskHandler) NewPayload() any         { return upstreamAutomationTaskPayload{} }
func (upstreamAutomationTaskHandler) Enabled() bool {
	rows, err := model.ListUpstreamAutomations(context.Background())
	if err != nil {
		return false
	}
	for _, row := range rows {
		view, err := service.UpstreamAutomationResponse(row)
		if err == nil && view.Enabled {
			return true
		}
	}
	monitors, err := model.GetChannelRatioMonitors()
	if err != nil {
		return false
	}
	for _, monitor := range monitors {
		if monitor.UpstreamType != service.CustomUpstreamType {
			continue
		}
		config, err := service.ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
		if err == nil && len(config.Actions) > 0 {
			return true
		}
	}
	return false
}

func (upstreamAutomationTaskHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	var payload upstreamAutomationTaskPayload
	err := task.DecodePayload(&payload)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	migrationErr := service.MigrateUpstreamAutomations(ctx, getChannelMonitorSettings().AutoUpdateIntervalMinutes)
	rows, err := model.ListUpstreamAutomations(ctx)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	forced := make(map[string]bool)
	for _, id := range payload.IDs {
		forced[id] = true
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	gate := make(chan struct{}, 4)
	var failures []error
	if migrationErr != nil {
		failures = append(failures, migrationErr)
	}
	checked := 0
	for _, row := range rows {
		if ctx.Err() != nil {
			break
		}
		gate <- struct{}{}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			defer func() { <-gate }()
			_, err := service.RunUpstreamAutomation(ctx, id, forced[id], time.Now, refreshUpstreamAutomationChannels)
			mu.Lock()
			defer mu.Unlock()
			if errors.Is(err, service.ErrUpstreamAutomationNotDue) {
				return
			}
			checked++
			if err != nil {
				failures = append(failures, err)
			}
		}(row.TaskID)
	}
	wg.Wait()
	err = errors.Join(append(failures, ctx.Err())...)
	status := model.SystemTaskStatusSucceeded
	if err != nil {
		status = model.SystemTaskStatusFailed
	}
	finishSystemTaskHandler(task, runnerID, status, map[string]any{"checked": checked, "failed": len(failures)}, err)
}
