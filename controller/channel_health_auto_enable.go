package controller

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"gorm.io/gorm"
)

func channelMonitorAllowsHealthCheckAutoEnable(channelId int) (bool, error) {
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		return false, fmt.Errorf("读取渠道状态失败: %w", err)
	}
	if channel.Status != common.ChannelStatusAutoDisabled {
		return false, nil
	}

	monitor, err := model.GetChannelRatioMonitor(channelId)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取渠道监控配置失败: %w", err)
	}

	statusReason, _ := channel.GetOtherInfo()["status_reason"].(string)
	statusReason = strings.TrimSpace(statusReason)
	ratioSyncEnabled := !monitor.UpstreamRatioSyncDisabled
	balanceSyncEnabled := !monitor.UpstreamBalanceSyncDisabled
	// Reuse persisted monitor snapshots here; a health check must not issue a
	// second upstream balance/ratio request just to decide whether to enable.
	ratioAvailable := monitor.UpdatedTime > 0 &&
		validateChannelMonitorRatio(&monitor.Ratio) &&
		monitor.LastFetchStatus != model.ChannelRatioFetchStatusFailed &&
		strings.TrimSpace(monitor.LastFetchError) == ""
	balanceAvailable := monitor.UpstreamBalance != nil &&
		!math.IsNaN(*monitor.UpstreamBalance) &&
		!math.IsInf(*monitor.UpstreamBalance, 0) &&
		strings.TrimSpace(monitor.LastBalanceError) == ""

	if statusReason == channelMonitorUpdateFailureDisableReason {
		if ratioSyncEnabled && !ratioAvailable {
			return false, nil
		}
		if balanceSyncEnabled && !balanceAvailable {
			return false, nil
		}
	}

	if balanceSyncEnabled && monitor.BalanceAutoDisableThreshold != nil {
		threshold := *monitor.BalanceAutoDisableThreshold
		if !balanceAvailable || math.IsNaN(threshold) || math.IsInf(threshold, 0) ||
			threshold < 0 || threshold > maxChannelMonitorBalanceThreshold {
			return false, nil
		}
		effectiveBalance := *monitor.UpstreamBalance
		if monitor.BalanceWarningThreshold != nil && effectiveBalance < *monitor.BalanceWarningThreshold {
			evaluation, estimateErr := evaluateChannelMonitorBalance(
				context.Background(), monitor, effectiveBalance,
			)
			if estimateErr != nil {
				return false, fmt.Errorf("计算余额消费估算失败: %w", estimateErr)
			}
			effectiveBalance = evaluation.EffectiveBalance
		}
		if effectiveBalance < threshold {
			return false, nil
		}
	}

	singleChannelAction := normalizeChannelMonitorPolicyAction(monitor.SingleChannelAction)
	multipleChannelsAction := normalizeChannelMonitorPolicyAction(monitor.MultipleChannelsAction)
	ratioAutoDisableEnabled := ratioSyncEnabled &&
		(singleChannelAction == channelMonitorPolicyActionDisableChannel ||
			multipleChannelsAction == channelMonitorPolicyActionDisableChannel)
	if !ratioAutoDisableEnabled {
		return true, nil
	}
	if !ratioAvailable {
		return false, nil
	}
	costRatio, _, err := channelMonitorCostRatioFromModel(monitor, monitor.Ratio)
	if err != nil {
		return false, fmt.Errorf("计算渠道成本倍率失败: %w", err)
	}
	allowEqual := statusReason != channelMonitorCostRatioPolicyDisableReason
	return channelMonitorCostRatioMeetsEveryGroup(
		channel,
		costRatio,
		ratio_setting.GetGroupRatioCopy(),
		getChannelMonitorGroupCoefficients(),
		allowEqual,
	), nil
}
