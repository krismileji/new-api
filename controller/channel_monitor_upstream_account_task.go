package controller

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type upstreamAccountBalanceTaskHandler struct{}

const upstreamAccountBalanceTaskType = "upstream_account_balance"

func init()                                               { service.RegisterSystemTaskHandler(upstreamAccountBalanceTaskHandler{}) }
func (upstreamAccountBalanceTaskHandler) Type() string    { return upstreamAccountBalanceTaskType }
func (upstreamAccountBalanceTaskHandler) NewPayload() any { return struct{}{} }
func (upstreamAccountBalanceTaskHandler) Interval() time.Duration {
	return channelRatioMonitorTaskHandler{}.Interval()
}

func (upstreamAccountBalanceTaskHandler) NewScheduledPayload(start, deadline time.Time) any {
	return channelMonitorUpdateWindow{Start: start.Unix(), Deadline: deadline.Unix()}
}

func (upstreamAccountBalanceTaskHandler) Enabled() bool {
	return upstreamAccountBalanceTaskHandler{}.enabledAt(time.Now())
}

func (upstreamAccountBalanceTaskHandler) enabledAt(now time.Time) bool {
	intervalMinutes := getChannelMonitorSettings().AutoUpdateIntervalMinutes
	if model.DB == nil || intervalMinutes <= 0 {
		return false
	}
	accounts, err := model.ListChannelMonitorUpstreamAccounts(context.Background())
	if err != nil {
		return false
	}
	roundStart := now.Truncate(time.Duration(intervalMinutes) * time.Minute).Unix()
	for _, account := range accounts {
		if upstreamAccountBalanceChecked(account.LastBalanceCheck, roundStart, intervalMinutes) {
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
	ctx, cancel, err := channelMonitorUpdateContext(ctx, task)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	defer cancel()
	monitorSettings := getChannelMonitorSettings()
	accounts, err := model.ListChannelMonitorUpstreamAccounts(ctx)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	var failures []error
	checked := 0
	round, _ := ctx.Value(upstreamAccountBalanceRoundKey{}).(*upstreamAccountBalanceRound)
	for _, account := range accounts {
		if ctx.Err() != nil {
			failures = append(failures, ctx.Err())
			break
		}
		if monitorSettings.AutoUpdateIntervalMinutes <= 0 || upstreamAccountBalanceChecked(account.LastBalanceCheck, round.start, monitorSettings.AutoUpdateIntervalMinutes) {
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
		_, err = fetchAndRecordUpstreamAccountBalance(ctx, members[0], monitorSettings.upstreamRequestTimeout())
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
