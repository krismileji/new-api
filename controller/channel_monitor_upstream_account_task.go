package controller

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type upstreamAccountBalanceTaskHandler struct{}

const upstreamAccountBalanceTaskType = "upstream_account_balance"

func init()                                                       { service.RegisterSystemTaskHandler(upstreamAccountBalanceTaskHandler{}) }
func (upstreamAccountBalanceTaskHandler) Type() string            { return upstreamAccountBalanceTaskType }
func (upstreamAccountBalanceTaskHandler) Interval() time.Duration { return 30 * time.Second }
func (upstreamAccountBalanceTaskHandler) NewPayload() any         { return struct{}{} }
func (upstreamAccountBalanceTaskHandler) Enabled() bool {
	if model.DB == nil {
		return false
	}
	accounts, err := model.ListChannelMonitorUpstreamAccounts(context.Background())
	if err != nil {
		return false
	}
	for _, account := range accounts {
		if account.RefreshIntervalMinutes <= 0 || common.GetTimestamp() < account.LastBalanceCheck+int64(account.RefreshIntervalMinutes)*60 {
			continue
		}
		settings, err := account.MonitorSettings()
		if err != nil || settings.UpstreamBalanceSyncDisabled {
			continue
		}
		members, err := model.GetUpstreamAccountMonitors(context.Background(), account.ID)
		if err == nil && len(members) > 0 {
			return true
		}
	}
	return false
}
func (upstreamAccountBalanceTaskHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	accounts, err := model.ListChannelMonitorUpstreamAccounts(ctx)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	var failures []error
	checked := 0
	ctx = withUpstreamAccountBalanceRound(ctx)
	for _, account := range accounts {
		if ctx.Err() != nil {
			failures = append(failures, ctx.Err())
			break
		}
		if account.RefreshIntervalMinutes <= 0 || common.GetTimestamp() < account.LastBalanceCheck+int64(account.RefreshIntervalMinutes)*60 {
			continue
		}
		settings, err := account.MonitorSettings()
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if settings.UpstreamBalanceSyncDisabled {
			continue
		}
		members, err := model.GetUpstreamAccountMonitors(ctx, account.ID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if len(members) == 0 {
			continue
		}
		_, err = fetchAndRecordUpstreamAccountBalance(ctx, members[0], getChannelMonitorSettings().upstreamRequestTimeout())
		checked++
		if err != nil {
			failures = append(failures, err)
		}
	}
	status := model.SystemTaskStatusSucceeded
	if len(failures) > 0 {
		status = model.SystemTaskStatusFailed
	}
	finishSystemTaskHandler(task, runnerID, status, map[string]any{"checked": checked, "failed": len(failures)}, errors.Join(failures...))
}
