package service

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/QuantumNous/new-api/model"
)

var channelMonitorCustomVariableLocks sync.Map

type channelMonitorCustomMetricError struct {
	cause   error
	ratio   bool
	balance bool
}

func (e *channelMonitorCustomMetricError) Error() string { return e.cause.Error() }
func (e *channelMonitorCustomMetricError) Unwrap() error { return e.cause }

// Dependencies are taken only from the metrics being queried. An unrelated
// request must not run because another metric's credentials expired.
func channelMonitorCustomVariableDependencies(config ChannelMonitorCustomUpstreamConfig, ratio, balance bool) map[string]bool {
	names := make(map[string]bool)
	requests := make([]*ChannelMonitorCustomRequestConfig, 0, 2)
	if ratio && config.Ratio.Source == ChannelMonitorCustomSourceHTTP {
		requests = append(requests, config.Ratio.Request)
	}
	if balance && config.Balance.Source == ChannelMonitorCustomSourceHTTP {
		request := config.Balance.Request
		if config.BalanceReuseRatioRequest {
			request = config.Ratio.Request
		}
		requests = append(requests, request)
	}
	for _, request := range requests {
		if request == nil {
			continue
		}
		for _, entries := range [][]ChannelMonitorCustomKeyValue{request.Query, request.Headers} {
			for _, item := range entries {
				for _, match := range channelMonitorCustomVariablePlaceholder.FindAllStringSubmatch(item.ValueTemplate, -1) {
					names[match[1]] = true
				}
			}
		}
	}
	return names
}

type channelMonitorCustomVariableSession struct {
	config       ChannelMonitorCustomUpstreamConfig
	savedRaw     string
	credentialID int
	revision     int64
	refreshed    map[string]bool
}

func (session *channelMonitorCustomVariableSession) refresh(ctx context.Context, client *http.Client, baseURL string, names map[string]bool, afterFailure bool) (bool, error) {
	refreshed := false
	for index, request := range session.config.VariableRequests {
		if session.refreshed[request.ID] {
			continue
		}
		used, missing := false, false
		for _, variable := range request.Variables {
			if names[variable.Name] {
				used = true
				missing = missing || variable.Value == ""
			}
		}
		if !used {
			continue
		}
		if afterFailure && request.RefreshPolicy != ChannelMonitorCustomRefreshOnFailure {
			continue
		}
		if !afterFailure && request.RefreshPolicy != ChannelMonitorCustomRefreshAlways && !missing {
			continue
		}
		session.refreshed[request.ID] = true
		variables, err := fetchChannelMonitorCustomVariables(ctx, client, baseURL, request)
		if err != nil {
			return refreshed, err
		}
		updated := session.config
		updated.VariableRequests = append([]ChannelMonitorCustomVariableRequest(nil), session.config.VariableRequests...)
		updated.VariableRequests[index].Variables = variables
		// A request's mappings are saved together only after every extraction
		// succeeded. Other requests keep their own values and refresh policies.
		raw, err := MarshalChannelMonitorCustomUpstreamConfig(updated)
		if err != nil {
			return refreshed, err
		}
		if session.credentialID > 0 {
			if err := model.UpdateChannelMonitorCustomVariableConfig(ctx, session.credentialID, session.revision, session.savedRaw, raw); err != nil {
				return refreshed, err
			}
		}
		session.config, session.savedRaw = updated, raw
		refreshed = true
	}
	return refreshed, nil
}

// Serialize saved-channel jobs so a simultaneous ratio and balance update can
// reuse refreshed values. Each request runs at most once in this operation.
func withChannelMonitorCustomVariables[T any](ctx context.Context, client *http.Client, config ChannelMonitorUpstreamConfig, balanceOnly bool, fetch func(ChannelMonitorCustomUpstreamConfig) (T, error)) (T, error) {
	var zero T
	normalized, err := NormalizeChannelMonitorCustomUpstreamConfig(config.CustomConfig)
	if err != nil {
		return zero, err
	}
	ratio, balance := !balanceOnly, balanceOnly || !config.SkipBalance
	names := channelMonitorCustomVariableDependencies(normalized, ratio, balance)
	if len(names) == 0 {
		return fetch(normalized)
	}
	if config.CredentialID > 0 {
		lock, _ := channelMonitorCustomVariableLocks.LoadOrStore(config.CredentialID, make(chan struct{}, 1))
		gate := lock.(chan struct{})
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			return zero, ctx.Err()
		}
		defer func() { <-gate }()
	}
	session := channelMonitorCustomVariableSession{config: normalized, credentialID: config.CredentialID, revision: config.Revision, refreshed: make(map[string]bool)}
	if config.CredentialID > 0 {
		monitor, err := model.GetChannelRatioMonitor(config.CredentialID)
		if err != nil {
			return zero, err
		}
		if monitor.UpstreamRevision != config.Revision || monitor.UpstreamType != CustomUpstreamType || monitor.UpstreamBaseURL != config.BaseURL {
			return zero, model.ErrChannelRatioMonitorConfigChanged
		}
		session.savedRaw = monitor.CustomUpstreamConfig
		session.config, err = ParseChannelMonitorCustomUpstreamConfig(session.savedRaw)
		if err != nil {
			return zero, err
		}
	}
	if _, err := session.refresh(ctx, client, config.BaseURL, names, false); err != nil {
		return zero, err
	}
	resolved, err := resolveChannelMonitorCustomTemplates(session.config, ratio, balance)
	if err != nil {
		return zero, err
	}
	result, fetchErr := fetch(resolved)
	if fetchErr == nil || ctx.Err() != nil {
		return result, fetchErr
	}
	var metricErr *channelMonitorCustomMetricError
	if errors.As(fetchErr, &metricErr) {
		ratio, balance = metricErr.ratio, metricErr.balance
	}
	names = channelMonitorCustomVariableDependencies(session.config, ratio, balance)
	refreshed, err := session.refresh(ctx, client, config.BaseURL, names, true)
	if err != nil {
		return result, err
	}
	if !refreshed {
		return result, fetchErr
	}
	resolved, err = resolveChannelMonitorCustomTemplates(session.config, !balanceOnly, balanceOnly || !config.SkipBalance)
	if err != nil {
		return result, err
	}
	return fetch(resolved)
}
