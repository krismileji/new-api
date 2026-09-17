package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// Account-owned queries serialize rotating credentials across nodes. A task
// passes its existing lease, keeping metric reads and side effects in one scope.
func LockUpstreamAccountRequest(ctx context.Context, config ChannelMonitorUpstreamConfig) (string, func(), error) {
	if config.AccountID == 0 || (config.Type == CustomUpstreamType && config.CustomConfig.Balance.Source == ChannelMonitorCustomSourceAccount) {
		return config.AccountLeaseID, func() {}, nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, channelMonitorUpstreamRequestTimeout(config.RequestTimeout))
	defer cancel()
	token := common.GetUUID()
	for {
		account, err := model.GetChannelMonitorUpstreamAccount(requestCtx, config.AccountID)
		if err != nil {
			return "", nil, err
		}
		if config.AccountRevision > 0 && account.Revision != config.AccountRevision {
			return "", nil, model.ErrUpstreamAccountChanged
		}
		if config.AccountLeaseID != "" {
			if account.LeaseID != config.AccountLeaseID || account.LeaseUntil <= common.GetTimestamp() {
				return "", nil, model.ErrUpstreamAccountChanged
			}
			return config.AccountLeaseID, func() {}, nil
		}
		leaseDuration := max(channelMonitorUpstreamRequestTimeout(config.RequestTimeout), upstreamGroupApplyTimeout) + 15*time.Second
		locked, err := model.AcquireUpstreamAccountLease(requestCtx, account.ID, account.Revision, token, time.Now().Add(leaseDuration).Unix())
		if err != nil {
			return "", nil, err
		}
		if locked {
			return token, func() {
				finish, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer done()
				_ = model.ReleaseUpstreamAccountLease(finish, account.ID, token)
			}, nil
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-requestCtx.Done():
			timer.Stop()
			return "", nil, requestCtx.Err()
		case <-timer.C:
		}
	}
}

// ResolveUpstreamAccountRequest inherits credentials and balance configuration,
// while preserving the channel's ratio selector and original result revision.
func ResolveUpstreamAccountRequest(ctx context.Context, config ChannelMonitorUpstreamConfig) (ChannelMonitorUpstreamConfig, error) {
	if config.Type == CustomUpstreamType && config.CustomConfig.Balance.Source == ChannelMonitorCustomSourceAccount {
		config.AccountID = 0
		return ResolveChannelMonitorBalanceSource(ctx, config)
	}
	if config.AccountID == 0 {
		return config, nil
	}
	account, err := model.GetChannelMonitorUpstreamAccount(ctx, config.AccountID)
	if err != nil {
		return config, err
	}
	if config.AccountRevision > 0 && config.AccountRevision != account.Revision {
		return config, model.ErrUpstreamAccountChanged
	}
	settings, err := account.MonitorSettings()
	if err != nil {
		return config, err
	}
	config.Type, config.BaseURL, config.Proxy = settings.UpstreamType, settings.UpstreamBaseURL, account.Proxy
	config.AuthType, config.UserID = settings.UpstreamAuthType, settings.UpstreamUserId
	config.AccessToken, config.RefreshToken = settings.UpstreamAccessToken, settings.UpstreamRefreshToken
	config.RefreshTokenStoredSeparately = settings.UpstreamRefreshToken != ""
	config.Account, config.Password = settings.UpstreamAccount, settings.UpstreamPassword
	config.AccountRevision = account.Revision
	config.Revision, config.CredentialID = account.Revision, -account.ID
	config.CostConversion, err = ParseChannelMonitorCostConversion(settings.CostConversion)
	if err != nil {
		return config, err
	}
	if config.Type == Sub2APIUpstreamType && config.AuthType == Sub2APIAuthAPIKey {
		if account.BalanceKey == "" {
			return config, errors.New("账户未配置余额查询 API Key")
		}
		if !config.SkipBalance {
			config.ChannelKeys = []string{account.BalanceKey}
		}
	}
	if config.Type == CustomUpstreamType {
		shared, err := ParseChannelMonitorCustomUpstreamConfig(settings.CustomUpstreamConfig)
		if err != nil {
			return config, err
		}
		if config.CustomConfig.Ratio.Source != "" {
			shared.Ratio = config.CustomConfig.Ratio
		}
		shared.Actions = config.CustomConfig.Actions
		config.CustomConfig = shared
	}
	return config, nil
}
