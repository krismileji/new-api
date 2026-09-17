package service

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// Balance-only members retain their own protection thresholds. Cache the small
// policy set when configuring a pool, so requests detect transitions in Redis
// without reading SQL or replacing another channel's policy.
func channelBalanceSourcePolicies(ctx context.Context, config ChannelBalanceConfig) (string, error) {
	if config.AccountID == 0 || model.DB == nil {
		return "[]", nil
	}
	members, err := model.GetUpstreamAccountMonitors(ctx, config.AccountID)
	if err != nil {
		return "", err
	}
	independent := false
	for _, member := range members {
		if member.UpstreamAccountRevision != config.Revision {
			return "", model.ErrUpstreamAccountChanged
		}
		independent = independent || member.UsesIndependentUpstreamConfig()
	}
	if !independent {
		return "[]", nil
	}
	type policy struct {
		ChannelID int    `json:"id"`
		Enabled   bool   `json:"enabled"`
		Warning   *int64 `json:"warning"`
		Threshold *int64 `json:"threshold"`
	}
	policies := make([]policy, 0, len(members))
	for _, member := range members {
		item := policy{ChannelID: member.ChannelId, Enabled: !member.UpstreamBalanceSyncDisabled}
		if member.BalanceWarningThreshold != nil {
			value, err := channelBalanceMicro(*member.BalanceWarningThreshold)
			if err != nil {
				return "", err
			}
			item.Warning = &value
		}
		if member.BalanceAutoDisableThreshold != nil {
			value, err := channelBalanceMicro(*member.BalanceAutoDisableThreshold)
			if err != nil {
				return "", err
			}
			item.Threshold = &value
		}
		policies = append(policies, item)
	}
	raw, err := common.Marshal(policies)
	return string(raw), err
}
