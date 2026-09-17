package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// Explicit links authorize a fresh read independent of channel status and the
// legacy consecutive-failure stop. Channel recovery still uses existing policy.
func refreshUpstreamAutomationChannels(ctx context.Context, config service.UpstreamAutomationConfig) error {
	var failures []error
	for _, id := range config.ChannelIDs {
		if ctx.Err() != nil {
			return errors.Join(append(failures, ctx.Err())...)
		}
		channel, err := model.GetChannelById(id, true)
		if err != nil {
			failures = append(failures, fmt.Errorf("渠道 %d 不存在或无法读取", id))
			continue
		}
		monitor, err := model.GetChannelRatioMonitorWithContext(ctx, id)
		if err != nil {
			failures = append(failures, fmt.Errorf("渠道 %d 未配置上游监控", id))
			continue
		}
		// A one-off linked refresh has the same scope as manually refreshing the
		// saved channel. No channel-owned action may execute from this path.
		monitor.UpstreamBalanceSyncDisabled = false
		options := channelMonitorRefreshOptions{IncludeSeparateBalance: true, SkipCustomActions: true}
		var outcome channelMonitorFetchOutcome
		if monitor.UpstreamRatioSyncDisabled {
			outcome, err = fetchAndRecordChannelMonitorUpstreamBalance(ctx, monitor, channel.GetKeys(), channel.GetSetting().Proxy, time.Duration(config.RequestTimeout)*time.Second, options)
		} else {
			outcome, err = fetchAndRecordChannelMonitorUpstreamRatio(ctx, monitor, channel.GetKeys(), channel.GetSetting().Proxy, time.Duration(config.RequestTimeout)*time.Second, options, 0, "上游自动任务")
		}
		if err != nil || outcome.Result.Balance.Amount == nil || outcome.Result.Balance.Error != "" {
			failures = append(failures, fmt.Errorf("渠道 %d 上游指标刷新失败", id))
			continue
		}
		balance := *outcome.Result.Balance.Amount
		complete := true
		if outcome.BalanceEvaluation != nil {
			balance = outcome.BalanceEvaluation.EffectiveBalance
			complete = outcome.BalanceEvaluation.Complete
		}
		if !complete {
			failures = append(failures, fmt.Errorf("渠道 %d 余额预估尚不完整，暂不自动恢复", id))
			continue
		}
		if _, err := autoDisableChannelMonitorForLowBalanceWithContext(ctx, monitor, channel, *outcome.Result.Balance.Amount); err != nil {
			failures = append(failures, err)
			continue
		}
		if outcome.RatioRecorded {
			monitor = outcome.Monitor
			if err := applyChannelMonitorRatioPolicy(ctx, monitor); err != nil {
				failures = append(failures, err)
				continue
			}
		}
		channel, err = model.GetChannelById(id, true)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if channelMonitorUpdateFailureRecovered(monitor, channel, &balance) {
			_, _, _, err = model.UpdateChannelMonitorStatusIfSnapshotRevision(id, monitor.UpstreamRevision, model.CaptureChannelMonitorStatus(channel), common.ChannelStatusEnabled, "")
			if err != nil {
				failures = append(failures, err)
			}
		}
		// Balance recovery still requires an actual cost sample, just like the
		// regular monitor. A zero-value, never-fetched ratio is not evidence.
		if monitor.UpdatedTime <= 0 {
			continue
		}
		costRatio, _, err := channelMonitorCostRatioFromModel(monitor, monitor.Ratio)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		inputs := map[int]channelMonitorPolicyInput{id: {
			UpstreamRevision: monitor.UpstreamRevision, CostRatio: costRatio,
			BalanceBelowAutoDisableThreshold: monitor.BalanceAutoDisableThreshold != nil && balance < *monitor.BalanceAutoDisableThreshold,
		}}
		settings := getChannelMonitorSettings()
		if settings.AutoEnableOnBalanceRecovery {
			_, err = autoEnableChannelsAfterBalanceRecovery(ctx, []*model.Channel{channel}, inputs, ratio_setting.GetGroupRatioCopy(), getChannelMonitorGroupCoefficients())
			if err != nil {
				failures = append(failures, err)
			}
		}
		if settings.AutoEnableOnCostRatioRecovery {
			_, err = autoEnableChannelsAfterCostRatioRecovery(ctx, []*model.Channel{channel}, inputs, ratio_setting.GetGroupRatioCopy(), getChannelMonitorGroupCoefficients())
			if err != nil {
				failures = append(failures, err)
			}
		}
	}
	if len(config.ChannelIDs) > 0 {
		_ = requestChannelSmartScheduleRun(ctx)
	}
	return errors.Join(failures...)
}
