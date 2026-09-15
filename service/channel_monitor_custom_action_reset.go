package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/model"
)

type ChannelMonitorCustomActionResetRequest struct {
	Day         string `json:"day"`
	Attempts    int    `json:"attempts"`
	LastAttempt int64  `json:"last_attempt"`
}

// Reset only the displayed day's attempts. Keep the latch, cooldown and outcome
// intact, and reject stale confirmations so a new attempt is never cleared unseen.
func ResetChannelMonitorCustomActionAttempts(ctx context.Context, channelID int, actionID string, request ChannelMonitorCustomActionResetRequest, now func() time.Time) (model.ChannelMonitorCustomActionState, error) {
	var result model.ChannelMonitorCustomActionState
	if request.Day == "" || request.Attempts <= 0 || request.LastAttempt <= 0 {
		return result, errors.New("请提供待重置的调用记录")
	}
	err := model.UpdateChannelMonitorCustomActionState(ctx, channelID, nil, func(monitor model.ChannelRatioMonitor, states map[string]model.ChannelMonitorCustomActionState) (bool, error) {
		if monitor.UpstreamType != CustomUpstreamType {
			return false, errors.New("只有已保存的自定义上游规则可以重置次数")
		}
		config, err := ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
		if err != nil {
			return false, err
		}
		for _, action := range config.Actions {
			if action.ID != actionID {
				continue
			}
			day, _, _ := action.executionWindow(now())
			if day != request.Day {
				return false, errors.New("规则的统计日期已变化，请刷新后重试")
			}
			state := states[actionID]
			if state.Status == "running" {
				return false, errors.New("接口正在执行或结果尚未确认，暂不能重置次数")
			}
			if state.Day != request.Day || state.Attempts != request.Attempts || state.LastAttempt != request.LastAttempt {
				return false, errors.New("调用记录已变化，请刷新后重试")
			}
			state.Attempts = 0
			states[actionID], result = state, state
			return true, nil
		}
		return false, errors.New("触发规则不存在，请保存规则后重试")
	})
	return result, err
}
