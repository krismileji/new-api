package service

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/model"
)

type UpstreamAccountBalanceFetcher func(context.Context, UpstreamAutomationConfig) (*float64, error)

var upstreamAccountBalanceFetcher atomic.Pointer[UpstreamAccountBalanceFetcher]

func RegisterUpstreamAccountBalanceFetcher(fetch UpstreamAccountBalanceFetcher) {
	upstreamAccountBalanceFetcher.Store(&fetch)
}

func fetchUpstreamAccountAutomationMetrics(ctx context.Context, config UpstreamAutomationConfig, wantRatio, wantBalance bool) (*float64, *float64, error) {
	var ratio, balance *float64
	if wantBalance {
		fetch := upstreamAccountBalanceFetcher.Load()
		if fetch == nil {
			return nil, nil, errors.New("账户余额同步尚未初始化")
		}
		var err error
		balance, err = (*fetch)(ctx, config)
		if err != nil {
			return nil, nil, err
		}
	}
	if wantRatio {
		monitor, err := model.GetChannelRatioMonitorWithContext(ctx, config.RatioChannelID)
		if err != nil {
			return nil, balance, err
		}
		if monitor.UpstreamAccountID != config.AccountID {
			return nil, balance, model.ErrUpstreamAccountChanged
		}
		channel, err := model.GetChannelById(monitor.ChannelId, true)
		if err != nil {
			return nil, balance, err
		}
		request := ChannelMonitorUpstreamConfig{
			AccountID: config.AccountID, AccountRevision: config.AccountRevision, AccountLeaseID: config.AccountLeaseID, Group: monitor.UpstreamGroup,
			ChannelKeys: channel.GetKeys(), CustomConfig: config.CustomConfig, SkipBalance: true, RequestTimeout: time.Duration(config.RequestTimeout) * time.Second,
		}
		if monitor.UsesIndependentUpstreamConfig() {
			request.Type, request.BaseURL, request.Proxy = CustomUpstreamType, monitor.UpstreamBaseURL, channel.GetSetting().Proxy
			request.CredentialID, request.Revision = monitor.ChannelId, monitor.UpstreamRevision
			request.CustomConfig, err = ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
			if err != nil {
				return nil, balance, err
			}
		}
		result, err := FetchChannelMonitorUpstreamGroupRatio(ctx, request)
		if err != nil {
			return nil, balance, err
		}
		ratio = &result.Ratio
	}
	return ratio, balance, nil
}

func ResolveUpstreamAccountAutomation(ctx context.Context, config UpstreamAutomationConfig) (UpstreamAutomationConfig, error) {
	account, err := model.GetChannelMonitorUpstreamAccount(ctx, config.AccountID)
	if err != nil {
		return config, err
	}
	settings, err := account.MonitorSettings()
	if err != nil {
		return config, err
	}
	for _, action := range config.CustomConfig.Actions {
		if action.Metric == "ratio" && config.RatioChannelID <= 0 {
			return config, errors.New("倍率规则必须选择账户内的倍率来源渠道")
		}
	}
	actions := config.CustomConfig.Actions
	if settings.UpstreamType == CustomUpstreamType {
		config.CustomConfig, err = ParseChannelMonitorCustomUpstreamConfig(settings.CustomUpstreamConfig)
		if err != nil {
			return config, err
		}
		config.CustomConfig.Actions = actions
	} else {
		ratio, balance := 1.0, 0.0
		config.CustomConfig = ChannelMonitorCustomUpstreamConfig{Version: 1,
			Ratio:   ChannelMonitorCustomMetricConfig{Source: ChannelMonitorCustomSourceFixed, FixedValue: &ratio},
			Balance: ChannelMonitorCustomMetricConfig{Source: ChannelMonitorCustomSourceFixed, FixedValue: &balance}, Actions: actions}
	}
	config.BaseURL, config.Proxy, config.AccountRevision = settings.UpstreamBaseURL, account.Proxy, account.Revision
	members, err := model.GetUpstreamAccountMonitors(ctx, account.ID)
	if err != nil {
		return config, err
	}
	config.ChannelIDs = make([]int, 0, len(members))
	found := config.RatioChannelID == 0
	for _, member := range members {
		config.ChannelIDs = append(config.ChannelIDs, member.ChannelId)
		if member.ChannelId != config.RatioChannelID {
			continue
		}
		found = true
		if settings.UpstreamType == CustomUpstreamType {
			custom, err := ParseChannelMonitorCustomUpstreamConfig(member.CustomUpstreamConfig)
			if err != nil {
				return config, err
			}
			config.CustomConfig.Ratio = custom.Ratio
		}
	}
	if !found {
		return config, errors.New("倍率来源渠道不属于此账户")
	}
	return config, nil
}

// Persist only task-owned rules. Account credentials and metric definitions are
// resolved immediately before use and never copied into a second durable owner.
func upstreamAccountAutomationStoredConfig(config UpstreamAutomationConfig) UpstreamAutomationConfig {
	if config.AccountID > 0 {
		config.BaseURL, config.Proxy, config.ChannelIDs = "", "", nil
		config.CustomConfig = ChannelMonitorCustomUpstreamConfig{Version: 1, Actions: config.CustomConfig.Actions}
	}
	return config
}
