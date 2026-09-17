package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func loadUpstreamAutomationVariableSession(ctx context.Context, id string, revision int64) (channelMonitorCustomVariableSession, error) {
	var session channelMonitorCustomVariableSession
	row, err := model.GetUpstreamAutomation(ctx, id)
	if err != nil {
		return session, err
	}
	config, _, err := decodeUpstreamAutomation(row)
	if err != nil {
		return session, err
	}
	if config.Revision != revision {
		return session, errors.New("任务配置已变化")
	}
	if config.AccountID > 0 {
		account, err := model.GetChannelMonitorUpstreamAccount(ctx, config.AccountID)
		if err != nil {
			return session, err
		}
		settings, err := account.MonitorSettings()
		if err != nil {
			return session, err
		}
		if settings.UpstreamType == CustomUpstreamType {
			custom, err := ParseChannelMonitorCustomUpstreamConfig(settings.CustomUpstreamConfig)
			if err != nil {
				return session, err
			}
			session = channelMonitorCustomVariableSession{config: custom, savedRaw: settings.CustomUpstreamConfig, account: &account, revision: revision, refreshed: make(map[string]bool)}
			return session, nil
		}
		config, err = ResolveUpstreamAccountAutomation(ctx, config)
		if err != nil {
			return session, err
		}
	}
	raw, err := MarshalChannelMonitorCustomUpstreamConfig(config.CustomConfig)
	session = channelMonitorCustomVariableSession{config: config.CustomConfig, savedRaw: raw, automationID: id, revision: revision, refreshed: make(map[string]bool)}
	return session, err
}

func persistUpstreamAutomationVariables(ctx context.Context, id string, revision int64, previous, updated string) error {
	_, err := model.MutateUpstreamAutomation(ctx, id, false, 0, 0, func(row *model.SystemTask) error {
		config, _, err := decodeUpstreamAutomation(*row)
		if err != nil {
			return err
		}
		raw, err := MarshalChannelMonitorCustomUpstreamConfig(config.CustomConfig)
		if err != nil {
			return err
		}
		if config.Revision != revision || raw != previous {
			return errors.New("任务凭据已变化，请等待下次检查")
		}
		config.CustomConfig, err = ParseChannelMonitorCustomUpstreamConfig(updated)
		if err != nil {
			return err
		}
		encoded, err := common.Marshal(config)
		row.Payload = string(encoded)
		return err
	})
	return err
}
