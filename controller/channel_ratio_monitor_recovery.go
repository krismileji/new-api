package controller

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

const (
	channelMonitorUpdateFailureDisableReason   = "渠道监控：上游倍率或余额更新失败"
	channelMonitorCostRatioPolicyDisableReason = "渠道监控：成本倍率高于分组倍率"
)

func channelMonitorUpdateFailureRecovered(
	monitor model.ChannelRatioMonitor,
	channel *model.Channel,
	balance *float64,
) bool {
	if !channelMonitorAutoDisabledForReason(channel, channelMonitorUpdateFailureDisableReason) {
		return false
	}
	if balance == nil || monitor.BalanceAutoDisableThreshold == nil {
		return true
	}
	return *balance >= *monitor.BalanceAutoDisableThreshold
}

func channelMonitorAutoDisabledForReason(channel *model.Channel, reason string) bool {
	if channel == nil || channel.Status != common.ChannelStatusAutoDisabled {
		return false
	}
	statusReason, ok := channel.GetOtherInfo()["status_reason"].(string)
	return ok && statusReason == reason
}

func autoEnableChannelsAfterCostRatioRecovery(
	ctx context.Context,
	channels []*model.Channel,
	policyInputs map[int]channelMonitorPolicyInput,
	groupRatios map[string]float64,
	coefficients map[string]float64,
) ([]int, error) {
	enabledChannelIds := make([]int, 0)
	for _, channel := range channels {
		if ctx != nil && ctx.Err() != nil {
			return enabledChannelIds, ctx.Err()
		}
		if !channelMonitorAutoDisabledForReason(channel, channelMonitorCostRatioPolicyDisableReason) {
			continue
		}
		input, exists := policyInputs[channel.Id]
		if !exists || input.BalanceBelowAutoDisableThreshold {
			continue
		}
		currentChannel, err := model.GetChannelById(channel.Id, true)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf(
				"channel ratio monitor: channel_id=%d cost ratio recovery status lookup failed: %v",
				channel.Id,
				err,
			))
			continue
		}
		if !channelMonitorAutoDisabledForReason(currentChannel, channelMonitorCostRatioPolicyDisableReason) {
			continue
		}

		hasGroup := false
		recovered := true
		seenGroups := make(map[string]struct{})
		for _, group := range currentChannel.GetGroups() {
			if group == "" {
				continue
			}
			if _, seen := seenGroups[group]; seen {
				continue
			}
			seenGroups[group] = struct{}{}
			hasGroup = true

			groupRatio, exists := groupRatios[group]
			if !exists {
				groupRatio = 1
			}
			targetRatio := input.CostRatio * getChannelMonitorGroupCoefficient(coefficients, group)
			if !validateChannelMonitorRatio(&groupRatio) ||
				!validateChannelMonitorRatio(&targetRatio) ||
				targetRatio-groupRatio > channelMonitorRatioEpsilon {
				recovered = false
				break
			}
		}
		if !hasGroup || !recovered {
			continue
		}
		if model.UpdateChannelStatus(channel.Id, "", common.ChannelStatusEnabled, "") {
			channel.Status = common.ChannelStatusEnabled
			enabledChannelIds = append(enabledChannelIds, channel.Id)
		}
	}
	return enabledChannelIds, nil
}
