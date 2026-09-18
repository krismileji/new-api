package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type UpstreamAutomationConfig struct {
	AccountID       int                                `json:"account_id,omitempty"`
	RatioChannelID  int                                `json:"ratio_channel_id,omitempty"`
	AccountRevision int64                              `json:"-"`
	AccountLeaseID  string                             `json:"-"`
	ID              string                             `json:"id"`
	Revision        int64                              `json:"revision"`
	Name            string                             `json:"name"`
	Enabled         bool                               `json:"enabled"`
	BaseURL         string                             `json:"base_url"`
	Proxy           string                             `json:"proxy"`
	IntervalMinutes int                                `json:"interval_minutes"`
	RequestTimeout  int                                `json:"request_timeout"`
	ChannelIDs      []int                              `json:"channel_ids"`
	CustomConfig    ChannelMonitorCustomUpstreamConfig `json:"custom_config"`
}

type UpstreamAutomationView struct {
	UpstreamAutomationConfig
	State model.UpstreamAutomationState `json:"state"`
}

func decodeUpstreamAutomation(row model.SystemTask) (UpstreamAutomationConfig, model.UpstreamAutomationState, error) {
	var config UpstreamAutomationConfig
	var state model.UpstreamAutomationState
	if err := common.UnmarshalJsonStr(row.Payload, &config); err != nil {
		return config, state, err
	}
	if err := common.UnmarshalJsonStr(row.State, &state); err != nil {
		return config, state, err
	}
	config.ID, config.Revision = row.TaskID, state.Revision
	if state.Actions == nil {
		state.Actions = make(map[string]model.ChannelMonitorCustomActionState)
	}
	return config, state, nil
}

func UpstreamAutomationResponse(row model.SystemTask) (UpstreamAutomationView, error) {
	config, state, err := decodeUpstreamAutomation(row)
	if err == nil && config.AccountID > 0 {
		config, err = ResolveUpstreamAccountAutomation(context.Background(), config)
	}
	config.CustomConfig = SanitizeChannelMonitorCustomUpstreamConfig(config.CustomConfig)
	state.LeaseID = ""
	return UpstreamAutomationView{UpstreamAutomationConfig: config, State: state}, err
}

func SaveUpstreamAutomation(ctx context.Context, input UpstreamAutomationConfig) (UpstreamAutomationView, error) {
	if input.AccountID > 0 {
		var err error
		input, err = ResolveUpstreamAccountAutomation(ctx, input)
		if err != nil {
			return UpstreamAutomationView{}, err
		}
	}
	input.Name, input.Proxy = strings.TrimSpace(input.Name), strings.TrimSpace(input.Proxy)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 80 {
		return UpstreamAutomationView{}, errors.New("任务名称须为 1 到 80 个字符")
	}
	if input.IntervalMinutes < 1 || input.IntervalMinutes > 10080 || input.RequestTimeout < 1 || input.RequestTimeout > 120 {
		return UpstreamAutomationView{}, errors.New("检查间隔须为 1 到 10080 分钟，请求超时须为 1 到 120 秒")
	}
	if _, _, err := common.ParseProxyURLRuntime(input.Proxy); err != nil || len(input.Proxy) > 2048 {
		return UpstreamAutomationView{}, errors.New("请求代理地址无效")
	}
	baseURL, err := NormalizeChannelMonitorCustomBaseURL(input.BaseURL)
	if err != nil {
		return UpstreamAutomationView{}, err
	}
	input.BaseURL = baseURL
	if err := ValidateChannelMonitorVariableGroup(ctx, &input.CustomConfig); err != nil {
		return UpstreamAutomationView{}, err
	}
	if len(input.ChannelIDs) > 100 {
		return UpstreamAutomationView{}, errors.New("最多关联 100 个渠道")
	}
	seen := make(map[int]bool)
	for _, id := range input.ChannelIDs {
		if id <= 0 || seen[id] {
			return UpstreamAutomationView{}, errors.New("关联渠道无效或重复")
		}
		seen[id] = true
		if _, err := model.GetChannelById(id, false); err != nil {
			return UpstreamAutomationView{}, fmt.Errorf("关联渠道 %d 不存在", id)
		}
	}
	create := input.ID == ""
	if create {
		input.ID, err = model.GenerateSystemTaskID()
		if err != nil {
			return UpstreamAutomationView{}, err
		}
	}
	groupID, groupRevision := input.CustomConfig.VariableGroupID, input.CustomConfig.VariableGroupRevision
	balanceAccountID := 0
	if input.CustomConfig.Balance.Source == ChannelMonitorCustomSourceAccount {
		balanceAccountID = input.CustomConfig.Balance.AccountID
	}
	if input.AccountID > 0 {
		groupID, groupRevision = 0, 0
	}
	row, err := model.MutateUpstreamAutomation(ctx, input.ID, create, groupID, groupRevision, func(row *model.SystemTask) error {
		state := model.UpstreamAutomationState{Actions: map[string]model.ChannelMonitorCustomActionState{}}
		var existing *ChannelMonitorCustomUpstreamConfig
		if !create {
			current, savedState, err := decodeUpstreamAutomation(*row)
			if err != nil {
				return err
			}
			state = savedState
			if current.AccountID > 0 && current.AccountID != input.AccountID {
				return errors.New("账户任务不能直接改绑，请先处理原账户任务")
			}
			if current.Revision != input.Revision || state.LeaseUntil > common.GetTimestamp() {
				return errors.New("任务正在执行或配置已变化，请刷新后重试")
			}
			if current.AccountID == input.AccountID && (input.AccountID > 0 || (current.BaseURL == input.BaseURL && current.Proxy == input.Proxy)) {
				existing = &current.CustomConfig
			}
		}
		config, err := NormalizeChannelMonitorCustomUpstreamConfigWithExisting(input.CustomConfig, existing)
		if err != nil {
			return err
		}
		if len(config.Actions) == 0 {
			return errors.New("请至少配置一条触发规则")
		}
		input.CustomConfig = config
		state.Revision++
		input.Revision = state.Revision
		state.NextCheck, state.Status, state.Message = 0, "waiting", "配置已保存，等待检查"
		payload, err := common.Marshal(upstreamAccountAutomationStoredConfig(input))
		if err != nil {
			return err
		}
		if len(payload) > 60<<10 {
			return errors.New("自动任务配置过大，请减少请求参数或规则")
		}
		encodedState, err := common.Marshal(state)
		row.Payload, row.State = string(payload), string(encodedState)
		return err
	}, input.AccountID, balanceAccountID)
	if err != nil {
		return UpstreamAutomationView{}, err
	}
	return UpstreamAutomationResponse(row)
}

func MigrateUpstreamAutomations(ctx context.Context, intervalMinutes int) error {
	monitors, err := model.GetChannelRatioMonitorsWithContext(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, monitor := range monitors {
		if monitor.UpstreamType != CustomUpstreamType || strings.TrimSpace(monitor.CustomUpstreamConfig) == "" {
			continue
		}
		config, err := ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
		if err != nil {
			failures = append(failures, fmt.Errorf("渠道 %d 配置无效，无法迁移: %w", monitor.ChannelId, err))
			continue
		}
		if len(config.Actions) == 0 {
			continue
		}
		channel, err := model.GetChannelById(monitor.ChannelId, true)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		// Preserve an explicitly paused scheduler/metric on migration. The
		// independent task can subsequently be enabled without either switch.
		enabled, hasEnabledRule := intervalMinutes > 0, false
		for _, action := range config.Actions {
			if !action.Enabled {
				continue
			}
			hasEnabledRule = true
			if (action.Metric == "ratio" && monitor.UpstreamRatioSyncDisabled) || (action.Metric == "balance" && monitor.UpstreamBalanceSyncDisabled) {
				enabled = false
			}
		}
		automation := UpstreamAutomationConfig{
			Name: channel.Name + " · 上游自动任务", Enabled: enabled && hasEnabledRule, Revision: 1,
			BaseURL: monitor.UpstreamBaseURL, Proxy: channel.GetSetting().Proxy,
			IntervalMinutes: max(1, min(intervalMinutes, 10080)), RequestTimeout: 30, ChannelIDs: []int{monitor.ChannelId}, CustomConfig: config,
		}
		if err := ValidateChannelMonitorVariableGroup(ctx, &automation.CustomConfig); err != nil {
			failures = append(failures, err)
			continue
		}
		payload, err := common.Marshal(automation)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		config.Actions = nil
		remaining, err := MarshalChannelMonitorCustomUpstreamConfig(config)
		if err == nil {
			err = model.ImportChannelUpstreamAutomation(ctx, monitor, automation.CustomConfig.VariableGroupRevision, string(payload), remaining)
		}
		if err != nil && !errors.Is(err, model.ErrChannelRatioMonitorConfigChanged) {
			failures = append(failures, fmt.Errorf("渠道 %d 迁移失败: %w", monitor.ChannelId, err))
		}
	}
	return errors.Join(failures...)
}

func updateUpstreamAutomationState(ctx context.Context, id string, revision int64, leaseID string, update func(*model.UpstreamAutomationState) error) error {
	_, err := model.MutateUpstreamAutomation(ctx, id, false, 0, 0, func(row *model.SystemTask) error {
		_, state, err := decodeUpstreamAutomation(*row)
		if err != nil {
			return err
		}
		if state.Revision != revision || (leaseID != "" && state.LeaseID != leaseID) {
			return errors.New("任务配置或执行租约已变化")
		}
		if err := update(&state); err != nil {
			return err
		}
		encoded, err := common.Marshal(state)
		row.State = string(encoded)
		return err
	})
	return err
}

// Acknowledgement only rearms a confirmed outcome; it never clears counters or
// cooldown, nor does it issue a request. Match the exact attempt shown in the UI.
func AcknowledgeUpstreamAutomationAction(ctx context.Context, id, actionID, attemptID string, revision int64) error {
	return updateUpstreamAutomationState(ctx, id, revision, "", func(state *model.UpstreamAutomationState) error {
		action, exists := state.Actions[actionID]
		if !exists || !action.NeedsConfirmation || attemptID == "" || action.AttemptID != attemptID || state.LeaseUntil > common.GetTimestamp() {
			return errors.New("执行记录已变化或任务仍在运行，请刷新后重试")
		}
		action.NeedsConfirmation, action.Triggered = false, false
		action.Status, action.Message = "acknowledged", "已人工核对上次结果，允许后续按规则检查"
		state.Actions[actionID] = action
		state.NextCheck = 0
		return nil
	})
}

func ResetUpstreamAutomationAttempts(ctx context.Context, id, actionID string, revision int64, request ChannelMonitorCustomActionResetRequest) error {
	row, err := model.GetUpstreamAutomation(ctx, id)
	if err != nil {
		return err
	}
	config, _, err := decodeUpstreamAutomation(row)
	if err != nil {
		return err
	}
	for _, action := range config.CustomConfig.Actions {
		if action.ID != actionID {
			continue
		}
		day, _, _ := action.executionWindow(time.Now())
		return updateUpstreamAutomationState(ctx, id, revision, "", func(state *model.UpstreamAutomationState) error {
			current := state.Actions[actionID]
			if day != request.Day || current.Day != request.Day || current.Attempts != request.Attempts || current.LastAttempt != request.LastAttempt || current.Attempts <= 0 || state.LeaseUntil > common.GetTimestamp() {
				return errors.New("调用记录已变化或任务正在执行，请刷新后重试")
			}
			current.Attempts = 0
			state.Actions[actionID] = current
			return nil
		})
	}
	return errors.New("触发规则不存在")
}

// Draft reads restore masked credentials only from the same saved origin and
// never reserve/execute actions or mutate saved task state.
func PrepareUpstreamAutomationDraft(ctx context.Context, input UpstreamAutomationConfig) (UpstreamAutomationConfig, error) {
	if input.AccountID > 0 {
		return ResolveUpstreamAccountAutomation(ctx, input)
	}
	var existing *ChannelMonitorCustomUpstreamConfig
	baseURL, err := NormalizeChannelMonitorCustomBaseURL(input.BaseURL)
	if err != nil {
		return input, err
	}
	input.BaseURL = baseURL
	if input.RequestTimeout < 1 || input.RequestTimeout > 120 {
		return input, errors.New("请求超时须为 1 到 120 秒")
	}
	if input.ID != "" {
		row, err := model.GetUpstreamAutomation(ctx, input.ID)
		if err != nil {
			return input, err
		}
		current, _, err := decodeUpstreamAutomation(row)
		if err != nil {
			return input, err
		}
		if current.Revision != input.Revision {
			return input, errors.New("任务配置已变化，请重新打开")
		}
		if current.BaseURL == input.BaseURL && current.Proxy == input.Proxy {
			existing = &current.CustomConfig
		}
	}
	input.CustomConfig, err = NormalizeChannelMonitorCustomUpstreamConfigWithExisting(input.CustomConfig, existing)
	return input, err
}

func FetchUpstreamAutomationDraftVariables(ctx context.Context, input UpstreamAutomationConfig, requestID string) ([]ChannelMonitorCustomVariable, error) {
	config, err := PrepareUpstreamAutomationDraft(ctx, input)
	if err != nil {
		return nil, err
	}
	client, err := NewSSRFProtectedHTTPClientWithProxy(config.Proxy)
	if err != nil {
		return nil, err
	}
	requestContext, cancel := context.WithTimeout(ctx, time.Duration(config.RequestTimeout)*time.Second)
	defer cancel()
	for _, request := range config.CustomConfig.VariableRequests {
		if request.ID == requestID {
			return fetchChannelMonitorCustomVariables(requestContext, client, config.BaseURL, request)
		}
	}
	return nil, errors.New("独立请求不存在")
}
