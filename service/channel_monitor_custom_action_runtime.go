package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/tidwall/gjson"
)

// RunChannelMonitorCustomActions is called only after a saved metric refresh.
// Draft tests and configuration saves never invoke an action. When execution is
// disabled, refreshed samples can rearm rules without sending another request.
// A success invalidates this sample, so the caller must refresh before any more actions.
func RunChannelMonitorCustomActions(ctx context.Context, monitor model.ChannelRatioMonitor, metric string, value float64, proxy string, timeout time.Duration, allowExecution bool) (bool, error) {
	if monitor.UpstreamType != CustomUpstreamType {
		return false, nil
	}
	config, err := ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
	if err != nil || len(config.Actions) == 0 {
		return false, err
	}
	var failures []error
	for _, action := range config.Actions {
		if !action.Enabled || action.Metric != metric {
			continue
		}
		succeeded, err := runChannelMonitorCustomAction(ctx, monitor, action, value, proxy, timeout, allowExecution, time.Now)
		if err != nil {
			failures = append(failures, fmt.Errorf("规则 %s: %w", action.Name, err))
		}
		if succeeded {
			return true, errors.Join(failures...)
		}
	}
	return false, errors.Join(failures...)
}

func channelMonitorCustomActionSampleCurrent(monitor model.ChannelRatioMonitor, metric string, value float64) bool {
	if metric == "balance" {
		return !monitor.UpstreamBalanceSyncDisabled && monitor.LastBalanceError == "" && monitor.UpstreamBalance != nil && *monitor.UpstreamBalance == value
	}
	return !monitor.UpstreamRatioSyncDisabled && monitor.LastFetchStatus == model.ChannelRatioFetchStatusSucceeded && monitor.Ratio == value
}

func runChannelMonitorCustomAction(ctx context.Context, monitor model.ChannelRatioMonitor, action ChannelMonitorCustomAction, value float64, proxy string, timeout time.Duration, allowExecution bool, now func() time.Time) (bool, error) {
	claimed := false
	attemptID, err := model.GenerateSystemTaskID()
	if err != nil {
		return false, err
	}
	err = model.UpdateChannelMonitorCustomActionState(ctx, monitor.ChannelId, &monitor.UpstreamRevision, func(current model.ChannelRatioMonitor, states map[string]model.ChannelMonitorCustomActionState) (bool, error) {
		if !channelMonitorCustomActionSampleCurrent(current, action.Metric, value) {
			return false, nil
		}
		state := states[action.ID]
		if !action.matches(value) {
			if !state.Triggered {
				return false, nil
			}
			state.Triggered = false
			states[action.ID] = state
			return true, nil
		}
		if !allowExecution {
			return false, nil
		}
		timestamp := now()
		day, _, allowed := action.executionWindow(timestamp)
		if state.Triggered || state.Status == "running" || !allowed || (state.LastAttempt > 0 && timestamp.Unix()-state.LastAttempt < int64(action.CooldownMinutes)*60) {
			return false, nil
		}
		if state.Day != day {
			state.Day, state.Attempts = day, 0
		}
		if state.Attempts >= action.DailyLimit {
			return false, nil
		}
		state.Triggered, state.LastAttempt, state.LastValue = true, timestamp.Unix(), value
		state.Attempts++
		state.AttemptID, state.Status, state.Message = attemptID, "running", "执行已登记，结果待确认；不会自动重试"
		states[action.ID] = state
		claimed = true
		return true, nil
	})
	if err != nil || !claimed {
		return false, err
	}

	executionErr := executeChannelMonitorCustomAction(ctx, monitor, action, value, proxy, timeout, now)
	status, message := "succeeded", "接口执行成功"
	if executionErr != nil {
		status, message = "failed", executionErr.Error()
	}
	// Preserve the attempt even when the caller disconnects. A failed completion
	// write leaves it pending confirmation and therefore ineligible for replay.
	finishContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	finishErr := model.UpdateChannelMonitorCustomActionState(finishContext, monitor.ChannelId, nil, func(_ model.ChannelRatioMonitor, states map[string]model.ChannelMonitorCustomActionState) (bool, error) {
		state := states[action.ID]
		if state.AttemptID != attemptID {
			return false, nil
		}
		state.Status, state.Message = status, message
		states[action.ID] = state
		return true, nil
	})
	return executionErr == nil, errors.Join(executionErr, finishErr)
}

func executeChannelMonitorCustomAction(ctx context.Context, monitor model.ChannelRatioMonitor, action ChannelMonitorCustomAction, value float64, proxy string, timeout time.Duration, now func() time.Time) error {
	_, cutoff, allowed := action.executionWindow(now())
	if !allowed {
		return errors.New("已超出允许执行时段，未调用接口")
	}
	duration := min(channelMonitorUpstreamRequestTimeout(timeout), cutoff.Sub(now()))
	requestContext, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	client, err := NewSSRFProtectedHTTPClientWithProxy(proxy)
	if err != nil {
		return errors.New("无法创建自定义接口连接")
	}
	actionClient, closeClient, err := newChannelMonitorCustomActionClient(proxy)
	if err != nil {
		return errors.New("无法创建自定义接口连接")
	}
	defer closeClient()
	request, err := prepareChannelMonitorCustomActionRequest(requestContext, client, monitor, action)
	if err != nil {
		return errors.New("准备触发接口凭据失败，未调用接口；请检查独立请求配置")
	}
	current, err := model.GetChannelRatioMonitorWithContext(requestContext, monitor.ChannelId)
	if err != nil || current.UpstreamRevision != monitor.UpstreamRevision || current.UpstreamType != CustomUpstreamType {
		return errors.New("渠道配置已变更或无法读取，未调用接口")
	}
	if !channelMonitorCustomActionSampleCurrent(current, action.Metric, value) {
		return errors.New("指标已更新，未调用接口")
	}
	if _, _, allowed := action.executionWindow(now()); !allowed || requestContext.Err() != nil {
		return errors.New("已超出允许执行时段或请求已取消，未调用接口")
	}
	baseURL := monitor.UpstreamBaseURL
	if action.BaseURL != "" {
		baseURL = action.BaseURL
	}
	request.HideResponse = true
	response, err := requestChannelMonitorCustomUpstream(requestContext, actionClient, baseURL, request, false)
	if err != nil {
		if response.debug != nil {
			return fmt.Errorf("接口返回 HTTP %d，未自动重试，请核对上游结果", response.debug.StatusCode)
		}
		return errors.New("接口调用失败或结果未知，未自动重试，请核对上游结果")
	}
	if action.SuccessPath != "" {
		result := gjson.GetBytes(response.body, action.SuccessPath)
		if !gjson.ValidBytes(response.body) || !result.Exists() || result.IsObject() || result.IsArray() || result.Type == gjson.Null || result.String() != action.SuccessValue {
			return errors.New("接口响应未满足成功判定，未自动重试，请核对上游结果")
		}
	}
	return nil
}

// A fresh connection pool prevents net/http from replaying an idempotent-looking
// GET reset on a broken reused connection. Variables use a separate client.
func newChannelMonitorCustomActionClient(proxy string) (*http.Client, func(), error) {
	parsedProxy, _, err := common.ParseProxyURLRuntime(proxy)
	if err != nil {
		return nil, nil, err
	}
	client := newProtectedFetchHTTPClient()
	closeClient := client.CloseIdleConnections
	if parsedProxy != nil {
		proxyClient, err := newProxyHTTPClient(parsedProxy)
		if err != nil {
			return nil, nil, err
		}
		client = &http.Client{Transport: &ssrfProtectedProxyRoundTripper{base: proxyClient.Transport}, Timeout: proxyClient.Timeout}
		closeClient = proxyClient.CloseIdleConnections
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return client, closeClient, nil
}

func prepareChannelMonitorCustomActionRequest(ctx context.Context, client *http.Client, monitor model.ChannelRatioMonitor, action ChannelMonitorCustomAction) (ChannelMonitorCustomRequestConfig, error) {
	lock, _ := channelMonitorCustomVariableLocks.LoadOrStore(monitor.ChannelId, make(chan struct{}, 1))
	gate := lock.(chan struct{})
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return action.Request, ctx.Err()
	}
	defer func() { <-gate }()
	current, err := model.GetChannelRatioMonitorWithContext(ctx, monitor.ChannelId)
	if err != nil {
		return action.Request, err
	}
	if current.UpstreamRevision != monitor.UpstreamRevision || current.UpstreamType != CustomUpstreamType {
		return action.Request, model.ErrChannelRatioMonitorConfigChanged
	}
	config, err := ParseChannelMonitorCustomUpstreamConfig(current.CustomUpstreamConfig)
	if err != nil {
		return action.Request, err
	}
	session := channelMonitorCustomVariableSession{config: config, savedRaw: current.CustomUpstreamConfig, credentialID: monitor.ChannelId, revision: monitor.UpstreamRevision, refreshed: make(map[string]bool)}
	releaseShared, err := session.loadShared(ctx)
	if err != nil {
		return action.Request, err
	}
	defer releaseShared()
	metricConfig := config
	metricConfig.Ratio = ChannelMonitorCustomMetricConfig{Source: ChannelMonitorCustomSourceHTTP, Request: &action.Request}
	names := channelMonitorCustomVariableDependencies(metricConfig, true, false)
	if _, err := session.refresh(ctx, client, monitor.UpstreamBaseURL, names, false); err != nil {
		return action.Request, err
	}
	metricConfig.VariableRequests = session.config.VariableRequests
	resolved, err := resolveChannelMonitorCustomTemplates(metricConfig, true, false)
	if err != nil {
		return action.Request, err
	}
	return *resolved.Ratio.Request, nil
}
