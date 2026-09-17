package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/tidwall/gjson"
)

var ErrUpstreamAutomationNotDue = errors.New("任务尚未到检查时间、已暂停或正在执行")

// RunUpstreamAutomation owns one durable lease from metric fetch to linked
// channel recovery. Manual checks bypass only the schedule, never rule limits.
func RunUpstreamAutomation(ctx context.Context, id string, force bool, now func() time.Time, refreshChannels func(context.Context, UpstreamAutomationConfig) error) (view UpstreamAutomationView, runErr error) {
	leaseID, err := model.GenerateSystemTaskID()
	if err != nil {
		return view, err
	}
	var config UpstreamAutomationConfig
	_, err = model.MutateUpstreamAutomation(ctx, id, false, 0, 0, func(row *model.SystemTask) error {
		var state model.UpstreamAutomationState
		var err error
		config, state, err = decodeUpstreamAutomation(*row)
		if err != nil {
			return err
		}
		timestamp := now().Unix()
		if config.MergedInto != "" || !config.Enabled || state.LeaseUntil > timestamp || (!force && state.NextCheck > timestamp) {
			return ErrUpstreamAutomationNotDue
		}
		state.LeaseID, state.LeaseUntil = leaseID, timestamp+300
		state.LastCheck, state.NextCheck = timestamp, timestamp+int64(config.IntervalMinutes)*60
		state.Status, state.Message = "checking", "正在独立查询上游指标"
		for id, action := range state.Actions {
			if action.Status == "running" {
				action.NeedsConfirmation = true
				action.Message = "上次执行结果未确认，请核对上游后解除暂停"
				state.Actions[id] = action
			}
		}
		encoded, err := common.Marshal(state)
		row.State = string(encoded)
		return err
	})
	if err != nil {
		return view, err
	}
	runContext, cancel := context.WithTimeout(ctx, time.Duration(2*config.RequestTimeout+30)*time.Second)
	defer cancel()
	status, message := "checked", "检查完成，未执行接口"
	defer func() {
		if runErr != nil && status == "checked" {
			status, message = "failed", "任务执行失败："+runErr.Error()
		}
		finishContext, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer finishCancel()
		finishErr := updateUpstreamAutomationState(finishContext, id, config.Revision, leaseID, func(state *model.UpstreamAutomationState) error {
			state.LeaseID, state.LeaseUntil = "", 0
			state.Status, state.Message = status, message
			if runErr != nil {
				state.Failures = min(state.Failures+1, 10)
				// Query failures never permanently stop the independent task.
				backoff := min(int64(config.IntervalMinutes)*60*(1<<min(state.Failures, 4)), int64(3600))
				state.NextCheck = now().Unix() + max(int64(config.IntervalMinutes)*60, backoff)
			} else {
				state.Failures = 0
				state.NextCheck = now().Unix() + int64(config.IntervalMinutes)*60
			}
			state.History = append(state.History, model.UpstreamAutomationEvent{ID: leaseID, Time: now().Unix(), Status: status, Message: message})
			if len(state.History) > 30 {
				state.History = state.History[len(state.History)-30:]
			}
			return nil
		})
		runErr = errors.Join(runErr, finishErr)
		row, readErr := model.GetUpstreamAutomation(finishContext, id)
		if readErr == nil {
			view, readErr = UpstreamAutomationResponse(row)
		}
		runErr = errors.Join(runErr, readErr)
	}()

	if config.AccountID > 0 {
		config, err = ResolveUpstreamAccountAutomation(runContext, config)
		if err != nil {
			return view, err
		}
		locked, lockErr := model.AcquireUpstreamAccountLease(runContext, config.AccountID, config.AccountRevision, leaseID, time.Now().Add(5*time.Minute).Unix())
		if lockErr != nil {
			return view, lockErr
		}
		if !locked {
			return view, ErrUpstreamAutomationNotDue
		}
		config.AccountLeaseID = leaseID
		defer func() {
			finish, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer done()
			_ = model.ReleaseUpstreamAccountLease(finish, config.AccountID, leaseID)
		}()
	} else {
		for _, channelID := range config.ChannelIDs {
			monitor, readErr := model.GetChannelRatioMonitorWithContext(runContext, channelID)
			if readErr == nil && monitor.UpstreamAccountID > 0 &&
				(config.CustomConfig.Balance.Source != ChannelMonitorCustomSourceAccount || config.CustomConfig.Balance.AccountID != monitor.UpstreamAccountID) {
				return view, errors.New("关联渠道已使用共享账户，请先在账户管理中合并此任务")
			}
		}
	}
	ratio, balance, err := fetchUpstreamAutomationMetrics(runContext, config)
	if err != nil {
		status, message = "fetch_failed", "指标查询失败，将退避后继续检查："+err.Error()
		return view, err
	}
	if err := updateUpstreamAutomationState(runContext, id, config.Revision, leaseID, func(state *model.UpstreamAutomationState) error {
		state.Ratio, state.Balance = ratio, balance
		return nil
	}); err != nil {
		return view, err
	}
	actionSucceeded := false
	for _, action := range config.CustomConfig.Actions {
		value := ratio
		if action.Metric == "balance" {
			value = balance
		}
		attemptID, err := model.GenerateSystemTaskID()
		if err != nil {
			return view, err
		}
		claimed := false
		err = updateUpstreamAutomationState(runContext, id, config.Revision, leaseID, func(state *model.UpstreamAutomationState) error {
			current := state.Actions[action.ID]
			current.LastChecked = now().Unix()
			day, _, inWindow := action.executionWindow(now())
			current.SkipReason = ""
			switch {
			case !action.Enabled:
				current.SkipReason = "规则已关闭"
			case value == nil:
				current.SkipReason = "本次没有取得有效指标"
			case current.NeedsConfirmation || current.Status == "running":
				current.SkipReason = "上次执行结果待人工确认"
			case !action.matches(*value):
				current.Triggered = false
				current.SkipReason = "当前指标不满足条件"
			case actionSucceeded:
				current.SkipReason = "本轮已执行接口，使用复查指标等待下次检查"
			case !inWindow:
				current.SkipReason = "不在允许执行时段"
			case action.TriggerMode != "repeat" && current.Triggered:
				current.SkipReason = "首次满足模式：等待指标退出触发条件"
			case current.LastAttempt > 0 && now().Unix()-current.LastAttempt < int64(action.CooldownMinutes)*60:
				current.SkipReason = "尚在冷却时间内"
			case current.Day == day && current.Attempts >= action.DailyLimit:
				current.SkipReason = "当日调用次数已用完"
			default:
				if current.Day != day {
					current.Day, current.Attempts = day, 0
				}
				current.Attempts++
				current.AttemptID, current.LastAttempt, current.LastValue = attemptID, now().Unix(), *value
				current.Triggered, current.Status, current.Message = true, "running", "执行已登记，等待上游结果"
				claimed = true
			}
			state.Actions[action.ID] = current
			return nil
		})
		if err != nil {
			return view, err
		}
		if !claimed {
			continue
		}
		uncertain, actionErr := executeUpstreamAutomationAction(runContext, config, action, leaseID, now)
		finishContext, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = updateUpstreamAutomationState(finishContext, id, config.Revision, leaseID, func(state *model.UpstreamAutomationState) error {
			current := state.Actions[action.ID]
			if current.AttemptID != attemptID {
				return errors.New("接口执行记录已变化")
			}
			current.Status, current.Message = "succeeded", "接口执行成功"
			current.NeedsConfirmation = uncertain
			if actionErr != nil {
				current.Status, current.Message = "failed", actionErr.Error()
				if !uncertain {
					// A failure before dispatch is safe to retry after cooldown.
					current.Triggered = false
					current.Attempts--
				}
			}
			state.Actions[action.ID] = current
			return nil
		})
		finishCancel()
		if err != nil {
			return view, errors.Join(actionErr, err)
		}
		if actionErr != nil {
			status, message = "action_failed", "规则 "+action.Name+"："+actionErr.Error()
			return view, actionErr
		}
		actionSucceeded = true
		status, message = "succeeded", "规则 "+action.Name+" 执行成功，已复查上游指标"
		ratio, balance, err = fetchUpstreamAutomationMetrics(runContext, config)
		if err != nil {
			status, message = "refresh_failed", "接口已成功，指标复查失败；不会重放该次调用："+err.Error()
			return view, err
		}
		if err := updateUpstreamAutomationState(runContext, id, config.Revision, leaseID, func(state *model.UpstreamAutomationState) error {
			state.Ratio, state.Balance = ratio, balance
			for _, rule := range config.CustomConfig.Actions {
				fresh := ratio
				if rule.Metric == "balance" {
					fresh = balance
				}
				current := state.Actions[rule.ID]
				if fresh != nil && !rule.matches(*fresh) {
					current.Triggered = false
					state.Actions[rule.ID] = current
				}
			}
			return nil
		}); err != nil {
			return view, err
		}
	}
	if refreshChannels != nil && len(config.ChannelIDs) > 0 && (ratio != nil || balance != nil) {
		if err := refreshChannels(runContext, config); err != nil {
			// Channel recovery has its own safety gates. A linked refresh must
			// not overwrite the action outcome or back off independent checks.
			message += "；关联渠道刷新提示：" + err.Error() + "（自动任务仍按原间隔检查）"
		}
	}
	return view, nil
}

func fetchUpstreamAutomationMetrics(ctx context.Context, config UpstreamAutomationConfig) (*float64, *float64, error) {
	wantRatio, wantBalance := false, false
	for _, action := range config.CustomConfig.Actions {
		if action.Enabled {
			wantRatio = wantRatio || action.Metric == "ratio"
			wantBalance = wantBalance || action.Metric == "balance"
		}
	}
	if config.AccountID > 0 {
		return fetchUpstreamAccountAutomationMetrics(ctx, config, wantRatio, wantBalance)
	}
	request := ChannelMonitorUpstreamConfig{
		AccountID: config.AccountID, AccountRevision: config.AccountRevision,
		Type: CustomUpstreamType, BaseURL: config.BaseURL, Proxy: config.Proxy,
		AutomationID: config.ID, Revision: config.Revision, CustomConfig: config.CustomConfig,
		RequestTimeout: time.Duration(config.RequestTimeout) * time.Second, SkipBalance: !wantBalance,
	}
	if wantRatio {
		result, err := FetchChannelMonitorUpstreamGroupRatio(ctx, request)
		if err != nil {
			return nil, nil, err
		}
		return &result.Ratio, result.Balance.Amount, nil
	}
	if wantBalance {
		result, err := FetchChannelMonitorUpstreamBalance(ctx, request)
		return nil, result.Amount, err
	}
	return nil, nil, nil
}

func executeUpstreamAutomationAction(ctx context.Context, config UpstreamAutomationConfig, action ChannelMonitorCustomAction, leaseID string, now func() time.Time) (bool, error) {
	_, cutoff, allowed := action.executionWindow(now())
	if !allowed {
		return false, errors.New("已超出执行时段，未调用接口")
	}
	requestContext, cancel := context.WithTimeout(ctx, min(time.Duration(config.RequestTimeout)*time.Second, cutoff.Sub(now())))
	defer cancel()
	session, err := loadUpstreamAutomationVariableSession(requestContext, config.ID, config.Revision)
	if err != nil {
		return false, err
	}
	client, err := NewSSRFProtectedHTTPClientWithProxy(config.Proxy)
	if err != nil {
		return false, errors.New("无法创建上游连接")
	}
	release, err := session.loadShared(requestContext)
	if err != nil {
		return false, err
	}
	defer release()
	metricConfig := session.config
	metricConfig.Ratio = ChannelMonitorCustomMetricConfig{Source: ChannelMonitorCustomSourceHTTP, Request: &action.Request}
	names := channelMonitorCustomVariableDependencies(metricConfig, true, false)
	if _, err := session.refresh(requestContext, client, config.BaseURL, names, false); err != nil {
		return false, errors.New("准备接口凭据失败，未调用接口")
	}
	metricConfig.VariableRequests = session.config.VariableRequests
	resolved, err := resolveChannelMonitorCustomTemplates(metricConfig, true, false)
	if err != nil {
		return false, errors.New("解析接口凭据失败，未调用接口")
	}
	if err := updateUpstreamAutomationState(requestContext, config.ID, config.Revision, leaseID, func(*model.UpstreamAutomationState) error { return nil }); err != nil {
		return false, err
	}
	if _, _, allowed := action.executionWindow(now()); !allowed || requestContext.Err() != nil {
		return false, errors.New("已超出执行时段或请求已取消，未调用接口")
	}
	actionClient, closeClient, err := newChannelMonitorCustomActionClient(config.Proxy)
	if err != nil {
		return false, errors.New("无法创建触发接口连接")
	}
	defer closeClient()
	baseURL := config.BaseURL
	if action.BaseURL != "" {
		baseURL = action.BaseURL
	}
	request := *resolved.Ratio.Request
	if config.AccountID > 0 && (action.BaseURL == "" || action.BaseURL == config.BaseURL) {
		request, err = authorizeUpstreamAccountAction(requestContext, client, config, request)
		if err != nil {
			return false, errors.New("准备账户认证失败，未调用接口")
		}
	}
	request.HideResponse = true
	response, err := requestChannelMonitorCustomUpstream(requestContext, actionClient, baseURL, request, false)
	if err != nil {
		if response.debug != nil {
			return true, fmt.Errorf("接口返回 HTTP %d，请核对上游结果后解除暂停", response.debug.StatusCode)
		}
		return true, errors.New("接口调用失败或结果未知，请核对上游结果后解除暂停")
	}
	if action.SuccessPath != "" {
		result := gjson.GetBytes(response.body, action.SuccessPath)
		if !gjson.ValidBytes(response.body) || !result.Exists() || result.IsObject() || result.IsArray() || result.Type == gjson.Null || result.String() != action.SuccessValue {
			return true, errors.New("接口响应未满足成功判定，请核对上游结果后解除暂停")
		}
	}
	return false, nil
}
