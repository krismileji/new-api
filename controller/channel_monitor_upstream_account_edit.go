package controller

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

type upstreamAccountEditTaskHandler struct{}

func init() { service.RegisterSystemTaskHandler(upstreamAccountEditTaskHandler{}) }

func (upstreamAccountEditTaskHandler) Type() string            { return "upstream_account_edit" }
func (upstreamAccountEditTaskHandler) Interval() time.Duration { return 15 * time.Second }
func (upstreamAccountEditTaskHandler) NewPayload() any         { return nil }
func (upstreamAccountEditTaskHandler) Enabled() bool {
	var rows []model.SystemTask
	err := model.DB.Select("id").Where("(type = ? AND status = ?) OR (type = ? AND result <> ?)",
		model.UpstreamAccountEditConfigType, model.SystemTaskStatusPending, model.UpstreamAutomationConfigType, "").Limit(1).Find(&rows).Error
	return err == nil && len(rows) > 0
}

func (upstreamAccountEditTaskHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	members, err := model.ApplyPendingUpstreamAccountEdits(ctx)
	err = errors.Join(err, service.ApplyPendingUpstreamAutomationEdits(ctx))
	for _, member := range members {
		service.InvalidateChannelDailyCostSnapshot(member.ChannelId)
		if common.RedisEnabled {
			_ = service.ConfigureChannelBalanceEstimate(ctx, member)
		}
	}
	if len(members) > 0 {
		service.NotifyChannelModelDetectionOverviewChanged()
	}
	status := model.SystemTaskStatusSucceeded
	if err != nil {
		status = model.SystemTaskStatusFailed
	}
	finishSystemTaskHandler(task, runnerID, status, nil, err)
}
