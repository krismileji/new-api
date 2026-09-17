package service

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type UpstreamAccountAutomationMergeRequest struct {
	AccountID       int              `json:"account_id"`
	AccountRevision int64            `json:"account_revision"`
	TargetID        string           `json:"target_id"`
	TaskRevisions   map[string]int64 `json:"task_revisions"`
	RatioChannelID  int              `json:"ratio_channel_id"`
	Preview         bool             `json:"preview"`
}

type UpstreamAccountAutomationMergePreview struct {
	TargetID          string   `json:"target_id"`
	TaskNames         []string `json:"task_names"`
	RuleNames         []string `json:"rule_names"`
	NeedsConfirmation bool     `json:"needs_confirmation"`
}

// Merge retains source rows as disabled history. Equivalent rules share their
// accumulated limits; the latest dispatch controls cooldown. No counter is reset
// and an unknown outcome remains blocked until explicitly acknowledged.
func MergeUpstreamAccountAutomations(ctx context.Context, input UpstreamAccountAutomationMergeRequest) (preview UpstreamAccountAutomationMergePreview, err error) {
	if input.AccountID <= 0 || input.AccountRevision <= 0 || len(input.TaskRevisions) == 0 || len(input.TaskRevisions) > 20 || input.TaskRevisions[input.TargetID] <= 0 {
		return preview, errors.New("请选择目标任务和最多 20 个待合并任务")
	}
	members, err := model.GetUpstreamAccountMonitors(ctx, input.AccountID)
	if err != nil {
		return preview, err
	}
	channelIDs := make(map[int]bool)
	for _, member := range members {
		channelIDs[member.ChannelId] = true
	}
	err = model.MergeUpstreamAccountAutomations(ctx, input.AccountID, input.AccountRevision, func(account model.ChannelMonitorUpstreamAccount, rows []model.SystemTask) ([]model.SystemTask, error) {
		configs := make(map[string]UpstreamAutomationConfig)
		states := make(map[string]model.UpstreamAutomationState)
		selected := make([]model.SystemTask, 0, len(input.TaskRevisions))
		for _, row := range rows {
			config, state, err := decodeUpstreamAutomation(row)
			if err != nil {
				return nil, err
			}
			_, included := input.TaskRevisions[row.TaskID]
			if config.AccountID == account.ID && !included {
				return nil, errors.New("必须包含此账户已有的自动任务")
			}
			if !included {
				continue
			}
			if config.CustomConfig.Balance.Source == ChannelMonitorCustomSourceAccount {
				return nil, errors.New("仅关联余额的自定义任务保留独立配置，请在自动任务中编辑")
			}
			if config.Revision != input.TaskRevisions[row.TaskID] || state.Revision == math.MaxInt64 || state.LeaseUntil > common.GetTimestamp() || config.MergedInto != "" {
				return nil, errors.New("任务已变化、正在执行或已经合并，请刷新后重试")
			}
			if config.AccountID != 0 && config.AccountID != account.ID {
				return nil, errors.New("不能合并其他账户的任务")
			}
			if len(config.ChannelIDs) == 0 && config.AccountID == 0 {
				return nil, errors.New("请先将独立任务关联到此账户的渠道，再进行合并")
			}
			for _, id := range config.ChannelIDs {
				if !channelIDs[id] {
					return nil, errors.New("待合并任务仍关联账户以外的渠道")
				}
			}
			configs[row.TaskID], states[row.TaskID] = config, state
			selected = append(selected, row)
		}
		if len(selected) != len(input.TaskRevisions) {
			return nil, errors.New("部分待合并任务已不存在")
		}
		target := configs[input.TargetID]
		target.AccountID, target.RatioChannelID = account.ID, input.RatioChannelID
		target.ChannelIDs = make([]int, 0, len(channelIDs))
		for id := range channelIDs {
			target.ChannelIDs = append(target.ChannelIDs, id)
		}
		sort.Ints(target.ChannelIDs)
		target.CustomConfig.Actions = nil
		targetState := states[input.TargetID]
		targetState.Actions = make(map[string]model.ChannelMonitorCustomActionState)
		preview = UpstreamAccountAutomationMergePreview{TargetID: input.TargetID, TaskNames: []string{}, RuleNames: []string{}}
		rules := make(map[string]int)
		for _, row := range selected {
			config, state := configs[row.TaskID], states[row.TaskID]
			preview.TaskNames = append(preview.TaskNames, config.Name)
			for _, rule := range config.CustomConfig.Actions {
				identity := rule
				identity.ID, identity.Name = "", ""
				raw, err := common.Marshal(identity)
				if err != nil {
					return nil, err
				}
				index, exists := rules[string(raw)]
				if !exists {
					index = len(target.CustomConfig.Actions)
					rules[string(raw)] = index
					copy := rule
					copy.ID = "merged_" + common.GetUUID()[:12]
					target.CustomConfig.Actions = append(target.CustomConfig.Actions, copy)
					preview.RuleNames = append(preview.RuleNames, copy.Name)
				}
				id := target.CustomConfig.Actions[index].ID
				old, merged := state.Actions[rule.ID], targetState.Actions[id]
				day, _, _ := rule.executionWindow(time.Now())
				attempts := merged.Attempts
				if old.Day == day {
					attempts = min(1_000_000, attempts+max(0, min(1_000_000, old.Attempts)))
				}
				triggered := merged.Triggered || old.Triggered
				uncertain := merged.NeedsConfirmation || old.NeedsConfirmation || old.Status == "running"
				if old.LastAttempt > merged.LastAttempt {
					merged = old
				}
				merged.Day, merged.Attempts, merged.Triggered, merged.NeedsConfirmation = day, attempts, triggered, uncertain
				if uncertain {
					merged.Status = "failed"
					merged.Message = "合并前存在未确认执行，请核对原任务历史后解除暂停"
				}
				targetState.Actions[id] = merged
				preview.NeedsConfirmation = preview.NeedsConfirmation || uncertain
			}
		}
		settings, err := account.MonitorSettings()
		if err != nil {
			return nil, err
		}
		target.BaseURL, target.Proxy = settings.UpstreamBaseURL, account.Proxy
		if settings.UpstreamType == CustomUpstreamType {
			shared, err := ParseChannelMonitorCustomUpstreamConfig(settings.CustomUpstreamConfig)
			if err != nil {
				return nil, err
			}
			shared.Actions = target.CustomConfig.Actions
			target.CustomConfig = shared
		} else {
			ratio, balance := 1.0, 0.0
			target.CustomConfig = ChannelMonitorCustomUpstreamConfig{Version: 1,
				Ratio:   ChannelMonitorCustomMetricConfig{Source: ChannelMonitorCustomSourceFixed, FixedValue: &ratio},
				Balance: ChannelMonitorCustomMetricConfig{Source: ChannelMonitorCustomSourceFixed, FixedValue: &balance}, Actions: target.CustomConfig.Actions}
		}
		for _, rule := range target.CustomConfig.Actions {
			if rule.Metric == "ratio" && !channelIDs[input.RatioChannelID] {
				return nil, errors.New("合并倍率规则时必须选择账户内的倍率来源渠道")
			}
		}
		if _, err := NormalizeChannelMonitorCustomUpstreamConfig(target.CustomConfig); err != nil {
			return nil, err
		}
		if input.Preview {
			return nil, nil
		}
		for i := range selected {
			row := &selected[i]
			config, state := configs[row.TaskID], states[row.TaskID]
			if row.TaskID == input.TargetID {
				config, state = target, targetState
			} else {
				config.Enabled, config.MergedInto, config.AccountID = false, input.TargetID, 0
			}
			state.Revision++
			config.Revision = state.Revision
			state.NextCheck, state.Status, state.Message = 0, "waiting", "已合并到账户任务，原执行限制保留"
			state.History = append(state.History, model.UpstreamAutomationEvent{ID: common.GetUUID(), Time: common.GetTimestamp(), Status: "merged", Message: "合并目标：" + input.TargetID})
			payload, err := common.Marshal(upstreamAccountAutomationStoredConfig(config))
			if err != nil {
				return nil, err
			}
			encoded, err := common.Marshal(state)
			if err != nil {
				return nil, err
			}
			if len(payload) > 60<<10 {
				return nil, errors.New("合并后的任务配置过大")
			}
			row.Payload, row.State = string(payload), string(encoded)
		}
		return selected, nil
	})
	return preview, err
}
