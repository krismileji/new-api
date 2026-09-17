package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/model"
)

// A balance reference never supplies credentials or variables to the caller's
// ratio and action requests. Only the canonical account poll uses its secrets.
func fetchChannelMonitorAccountBalanceSource(ctx context.Context, id int, record bool) (ChannelMonitorUpstreamBalanceResult, error) {
	account, err := model.GetChannelMonitorUpstreamAccount(ctx, id)
	if err != nil {
		return ChannelMonitorUpstreamBalanceResult{}, err
	}
	token, release, err := LockUpstreamAccountRequest(ctx, ChannelMonitorUpstreamConfig{AccountID: id, AccountRevision: account.Revision})
	if err != nil {
		return ChannelMonitorUpstreamBalanceResult{}, err
	}
	defer release()
	if !record {
		return FetchChannelMonitorUpstreamBalance(ctx, ChannelMonitorUpstreamConfig{AccountID: id, AccountRevision: account.Revision, AccountLeaseID: token})
	}
	fetch := upstreamAccountBalanceFetcher.Load()
	if fetch == nil {
		return ChannelMonitorUpstreamBalanceResult{}, errors.New("账户余额同步尚未初始化")
	}
	balance, err := (*fetch)(ctx, UpstreamAutomationConfig{AccountID: id, AccountRevision: account.Revision, AccountLeaseID: token})
	return ChannelMonitorUpstreamBalanceResult{Amount: balance, Endpoint: "关联上游账户"}, err
}

func ResolveChannelMonitorBalanceSource(ctx context.Context, config ChannelMonitorUpstreamConfig) (ChannelMonitorUpstreamConfig, error) {
	if config.Type != CustomUpstreamType || config.CustomConfig.Balance.Source != ChannelMonitorCustomSourceAccount {
		return config, nil
	}
	account, err := model.GetChannelMonitorUpstreamAccount(ctx, config.CustomConfig.Balance.AccountID)
	if err != nil {
		return config, errors.New("关联的上游账户不存在，请重新选择余额来源")
	}
	if config.AccountRevision > 0 && config.AccountRevision != account.Revision {
		return config, model.ErrUpstreamAccountChanged
	}
	settings, err := account.MonitorSettings()
	if err != nil {
		return config, err
	}
	config.AccountRevision = account.Revision
	config.CostConversion, err = ParseChannelMonitorCostConversion(settings.CostConversion)
	return config, err
}
