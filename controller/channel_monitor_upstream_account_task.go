package controller

import (
	"context"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type upstreamAccountBalanceTaskHandler struct{}

const upstreamAccountBalanceTaskType = "upstream_account_balance"

func init() { service.RegisterSystemTaskHandler(upstreamAccountBalanceTaskHandler{}) }

func (upstreamAccountBalanceTaskHandler) Type() string { return upstreamAccountBalanceTaskType }

func (upstreamAccountBalanceTaskHandler) Run(_ context.Context, task *model.SystemTask, runnerID string) {
	// Drain pending tasks left by older versions. The combined round owns all
	// scheduled balance requests after the upgrade.
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, map[string]any{
		"checked": 0, "failed": 0, "message": "共享余额已并入倍率与余额更新任务",
	}, nil)
}
